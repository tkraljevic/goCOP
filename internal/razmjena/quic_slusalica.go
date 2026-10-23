package razmjena

// Slušalica i pozivanje QUIC-a (plan WEB synca, F3a). Zasad samo ono što
// traži proba prije F1: jedna razmjena po vezi, istim vratima i istom
// provjerom druge strane kao na portu, bez sesije i kanala.

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"log"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

// postavkeQUIC su postavke veze WEB synca (plan, F3a): rukovanje 5 s,
// mirovanje 60 s, održavanje veze svakih 15 s, 8 dolaznih tokova, bez
// jednosmjernih tokova, 0-RTT-a i datagrama
func postavkeQUIC() *quic.Config {
	return &quic.Config{
		HandshakeIdleTimeout:  5 * time.Second,
		MaxIdleTimeout:        60 * time.Second,
		KeepAlivePeriod:       15 * time.Second,
		MaxIncomingStreams:    8,
		MaxIncomingUniStreams: -1,
	}
}

// tlsZaQUIC je tlsConfigS s ALPN-om WEB synca
func tlsZaQUIC(priv ed25519.PrivateKey, protocol string, vj func() []byte) (*tls.Config, error) {
	cfg, err := tlsConfigS(priv, protocol, vj)
	if err != nil {
		return nil, err
	}
	cfg.NextProtos = []string{alpnWeb(protocol)}
	return cfg, nil
}

// ServeExchangeQUIC prima razmjene preko QUIC-a na prijenosu tr dok ctx
// traje, istim vratima kao port razmjene (primiVezu): ključ i ime u
// certifikatu, članstvo, pa mjesto za razmjenu. Svaka veza nosi jedan tok
// razmjene. Ograda (po želji) je ista kao uz ServeExchange.
func ServeExchangeQUIC(ctx context.Context, tr *quic.Transport, priv ed25519.PrivateKey, protocol string, vj func() []byte, trusted KeyChecker, handle func(*Conn), o ...*Ograda) error {
	cfg, err := tlsZaQUIC(priv, protocol, vj)
	if err != nil {
		return err
	}
	ln, err := tr.Listen(cfg, postavkeQUIC())
	if err != nil {
		return err
	}
	v := &vrata{protokol: protocol, trusted: trusted, handle: handle, o: ogradaIz(o)}
	go func() {
		<-ctx.Done()
		_ = ln.Close() // slušalica staje s ctx; greška zatvaranja nema kome reći
	}()
	for {
		veza, err := ln.Accept(ctx)
		if err != nil {
			return greskaSlusalice(ctx, err)
		}
		if !v.uzmiRukovanje(ctx) {
			_ = veza.CloseWithError(0, "") // slušalica staje; druga strana pokuša sljedećim krugom
			return nil
		}
		go v.primiQUIC(ctx, veza)
	}
}

// greskaSlusalice: greška slušalice nakon kraja ctx je uredan kraj
func greskaSlusalice(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// uzmiRukovanje čeka mjesto za rukovanje u ogradi; false kad ctx završi
func (v *vrata) uzmiRukovanje(ctx context.Context) bool {
	select {
	case v.o.rukovanja <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// primiQUIC čeka tok razmjene na vezi koju je slušalica primila, u roku
// ograde, i preda ga na provjeru. Rukovanje QUIC-a je tada već gotovo, pa
// mjesto u ogradi drži onaj tko je nazvao, a tok još nije otvorio. Veza bez
// toka se zatvara.
func (v *vrata) primiQUIC(ctx context.Context, veza *quic.Conn) {
	// greška u jednoj vezi ne smije srušiti čvor, kao uz primiTLS
	defer func() {
		if r := recover(); r != nil {
			log.Printf("razmjena: veza %s prekinuta zbog greške: %v", veza.RemoteAddr(), r)
			_ = veza.CloseWithError(0, "") // veza je ionako izgubljena
		}
	}()
	pustiRukovanje := sync.OnceFunc(func() { <-v.o.rukovanja })
	defer pustiRukovanje()
	ctxToka, otkazi := context.WithTimeout(ctx, v.o.rok)
	tok, err := veza.AcceptStream(ctxToka)
	otkazi()
	if err != nil {
		v.o.odbij(VrataRukovanje, nil)
		_ = veza.CloseWithError(0, "") // tko ne otvori tok u roku, gubi vezu
		return
	}
	v.primiVezu(noviTokQUIC(veza, tok), veza.ConnectionState().TLS.PeerCertificates, pustiRukovanje)
}

// DialExchangeQUIC otvara razmjenu preko QUIC-a na prijenosu tr. Drugu stranu
// provjeri usred rukovanja, prije nego što pošalje svoj certifikat, kao
// DialExchangeOn (provjeriDrugu), pa otvori tok razmjene. RTT je trajanje
// rukovanja QUIC-a. Kad tok ne može početi, veza se zatvara; inače ostaje
// otvorena dok je ne zatvori prijenos ili istek mirovanja.
func DialExchangeQUIC(ctx context.Context, tr *quic.Transport, addr net.Addr, priv ed25519.PrivateKey, protocol string, vj func() []byte, expect ed25519.PublicKey) (*Conn, error) {
	cfg, err := tlsZaQUIC(priv, protocol, vj)
	if err != nil {
		return nil, err
	}
	var key ed25519.PublicKey
	var vjDruge []byte
	cfg.VerifyConnection = func(cs tls.ConnectionState) (err error) {
		key, vjDruge, err = provjeriDrugu(cs.PeerCertificates, protocol, addr.String(), expect)
		return err
	}
	pocetak := time.Now()
	veza, err := tr.Dial(ctx, addr, cfg, postavkeQUIC())
	if err != nil {
		return nil, err
	}
	rtt := time.Since(pocetak)
	tok, err := veza.OpenStreamSync(ctx)
	if err != nil {
		_ = veza.CloseWithError(0, "") // razmjena nije počela, veza nikome ne treba
		return nil, err
	}
	conn := newConnKljuc(noviTokQUIC(veza, tok), key, vjDruge)
	conn.RTT = rtt
	return conn, nil
}
