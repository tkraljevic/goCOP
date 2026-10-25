package razmjena

// Slušalica i pozivanje QUIC-a (plan WEB synca, F3a). ServeExchangeQUIC i
// DialExchangeQUIC su iz probe prije F1: jedna razmjena po vezi, istim
// vratima i istom provjerom druge strane kao na portu, bez sesije i kanala,
// s vlastitim ALPN-om (alpnProbe). Nemaju vrata WEB synca, pa nisu za
// utičnicu WEB synca; program ih ne zove.
//
// Slušalica WEB synca (F3a-2, slusajWeb) je na istoj UDP utičnici kao STUN
// i udarci, a pokreće je samo WebSync.Radi, koji program zove samo uz
// web_sync = true. Postavke (plan, „Postavke QUIC-a”):
//   - rukovanje 5 s, mirovanje 60 s, održavanje veze svakih 15 s, 8
//     dolaznih tokova, bez jednosmjernih tokova, 0-RTT-a i datagrama, bez
//     nastavka TLS sesije (svako rukovanje pokazuje certifikat);
//   - VerifySourceAddress uvijek vraća true, pa svatko dobiva Retry: do
//     ConnContext dolazi samo adresa koja je dokazala da prima pakete;
//   - ConnContext pušta samo adresu s koje je u zadnjih 30 s stigao valjan
//     udarac ili odgovor druge strane (Udarci.Pusta) i uz vezu zapamti
//     pripreme koje su je pustile;
//   - certifikat klijenta mora nositi ključ druge strane jedne od tih
//     priprema (expect), nikad vlastiti ključ (sesija sa samim sobom), a
//     to se provjerava usred rukovanja (VerifyConnection);
//   - StatelessResetKey iz ključa čvora (kljucReseta, NoviUDPCvora);
//   - zove računalo s manjim ključem (A), a ALPN je <protokol>-web/1.
// Slušalica kao poslužitelj šalje certifikat samo s ključem: članstvo ide
// samo u certifikatu klijenta, pa ga strani klijent ne vidi (kao signal).
// A članstvo druge strane zato uzima iz njezinog provjerenog odziva
// (SesijaWeb.vjDruge).

import (
	"context"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/sha256"
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

// tlsZaQUIC je tlsConfigS s ALPN-om alpn
func tlsZaQUIC(priv ed25519.PrivateKey, protocol string, vj func() []byte, alpn string) (*tls.Config, error) {
	cfg, err := tlsConfigS(priv, protocol, vj)
	if err != nil {
		return nil, err
	}
	cfg.NextProtos = []string{alpn}
	return cfg, nil
}

// ServeExchangeQUIC prima razmjene preko QUIC-a na prijenosu tr dok ctx
// traje, istim vratima kao port razmjene (primiVezu): ključ i ime u
// certifikatu, članstvo, pa mjesto za razmjenu. Svaka veza nosi jedan tok
// razmjene. Ograda (po želji) je ista kao uz ServeExchange.
func ServeExchangeQUIC(ctx context.Context, tr *quic.Transport, priv ed25519.PrivateKey, protocol string, vj func() []byte, trusted KeyChecker, handle func(*Conn), o ...*Ograda) error {
	cfg, err := tlsZaQUIC(priv, protocol, vj, alpnProbe(protocol))
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
	cfg, err := tlsZaQUIC(priv, protocol, vj, alpnProbe(protocol))
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

// oznakaReseta je oznaka HKDF-a za ključ resetiranja QUIC veza; ne ide na
// žicu, pa ne ovisi o imenu protokola
const oznakaReseta = "razmjena-quic-reset-v1"

// kljucReseta je ključ za resetiranje QUIC veza izveden iz sjemena ključa
// čvora; bez ključa čvora ga nema (resetiranje je tada isključeno)
func kljucReseta(priv ed25519.PrivateKey) *quic.StatelessResetKey {
	if len(priv) != ed25519.PrivateKeySize {
		return nil
	}
	b, _ := hkdf.Key(sha256.New, priv.Seed(), nil, oznakaReseta, 32) // 32 B je daleko ispod granice HKDF-a, pa greške nema
	k := quic.StatelessResetKey(b)
	return &k
}

// provjeraKlijenta provjeri certifikat klijenta usred rukovanja, s
// kontekstom veze iz ConnContext
type provjeraKlijenta func(ctx context.Context, cs tls.ConnectionState) error

// tlsPosluziteljaWeb je TLS slušalice WEB synca: certifikat bez
// vjerodajnica, ALPN WEB synca, bez nastavka sesije, a klijenta za svaku
// vezu provjeri provjeri, s kontekstom koji je dao ConnContext. ALPN ne
// treba provjeravati posebno: QUIC bez dogovorenog ALPN-a ne uspostavlja
// vezu, a ovdje ga nudi samo jedan.
func tlsPosluziteljaWeb(priv ed25519.PrivateKey, protokol string, provjeri provjeraKlijenta) (*tls.Config, error) {
	cfg, err := tlsConfig(priv, protokol)
	if err != nil {
		return nil, err
	}
	cfg.NextProtos = []string{alpnWeb(protokol)}
	cfg.SessionTicketsDisabled = true
	osnovni := cfg.Clone()
	cfg.GetConfigForClient = func(chi *tls.ClientHelloInfo) (*tls.Config, error) {
		c, ctx := osnovni.Clone(), chi.Context()
		c.VerifyConnection = func(cs tls.ConnectionState) error { return provjeri(ctx, cs) }
		return c, nil
	}
	return cfg, nil
}

// tlsKlijentaWeb je TLS računala koje zove (A): s vjerodajnicama, a druga
// strana mora dokazati očekivani ključ prije nego što ovo računalo pošalje
// svoj certifikat (provjeriDrugu). ALPN WEB synca nudi samo on, pa ga
// druga strana ili dogovori ili veze nema.
func tlsKlijentaWeb(priv ed25519.PrivateKey, protokol string, vj func() []byte, addr string, expect ed25519.PublicKey) (*tls.Config, error) {
	cfg, err := tlsZaQUIC(priv, protokol, vj, alpnWeb(protokol))
	if err != nil {
		return nil, err
	}
	cfg.SessionTicketsDisabled = true
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		_, _, err := provjeriDrugu(cs.PeerCertificates, protokol, addr, expect)
		return err
	}
	return cfg, nil
}

// slusajWeb otvori slušalicu WEB synca na prijenosu tr: svatko dobiva
// Retry, a do rukovanja dolazi samo adresa koju pusti vrata (ConnContext).
// Postavke prijenosa zadaju se prije Listen, u istoj gorutini, pa ih
// quic-go čita tek kad su postavljene.
func slusajWeb(tr *quic.Transport, cfg *tls.Config, vrata func(context.Context, *quic.ClientInfo) (context.Context, error)) (*quic.Listener, error) {
	tr.VerifySourceAddress = func(net.Addr) bool { return true }
	tr.ConnContext = vrata
	return tr.Listen(cfg, postavkeQUIC())
}
