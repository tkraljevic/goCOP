package razmjena

// Provjera veze s drugim računalom (plan WEB synca, F2a, gumb „Provjeri vezu
// s računalom”). Rukovanje ide dok druga strana ne dokaže ključ i tu stane.
// U TLS-u 1.3 to je trenutak kad druga strana potpiše rukovanje svojim
// ključem (CertificateVerify), završi svoj dio (Finished) i zatraži
// certifikat ovog računala. Ovo ga ne pošalje, a ne pošalje ni potvrdu
// članstva, pa ga druga strana ne pusti dalje od rukovanja. Kod nje je to
// rukovanje koje nije završilo (VrataRukovanje), a ne neuspjela razmjena koja
// bi odgodila iduću. Ranije, odmah nakon certifikata, ključ još nije dokazan:
// certifikat s tuđim javnim ključem može pokazati bilo tko.

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"errors"
	"net"
	"time"
)

// errKljucDokazan prekida rukovanje provjere kad druga strana zatraži
// certifikat ovog računala
var errKljucDokazan = errors.New("ključ druge strane je dokazan")

// ProvjeriKljucOn provjeri ključ druge strane preko otvorene veze i zatvori
// je. Vraća vrijeme od početka rukovanja do dokaza ključa, otprilike jedan
// obilazak. Druga strana provjerava se kao u DialExchangeOn (provjeriDrugu),
// a addr je samo za poruku o grešci.
func ProvjeriKljucOn(ctx context.Context, raw net.Conn, priv ed25519.PrivateKey, protocol, addr string, expect ed25519.PublicKey) (time.Duration, error) {
	defer func() { _ = raw.Close() }() // provjera je gotova, a greška zatvaranja ne mijenja ishod
	cfg, err := tlsConfig(priv, protocol)
	if err != nil {
		return 0, err
	}
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		_, _, err := provjeriDrugu(cs.PeerCertificates, protocol, addr, expect)
		return err
	}
	cfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return nil, errKljucDokazan }
	pocetak := time.Now()
	// rukovanje bez zahtjeva za certifikat (nije goCOP čvor) završi samo,
	// uz isti dokaz ključa
	if err := tls.Client(raw, cfg).HandshakeContext(ctx); err != nil && !errors.Is(err, errKljucDokazan) {
		return 0, err
	}
	return time.Since(pocetak), nil
}

// ProvjeriTunel je ProvjeriKljucOn kroz tunel (https://domena); greška nosi
// adresu tunela, kao uz DialTunel
func (k *KlijentTunela) ProvjeriTunel(ctx context.Context, priv ed25519.PrivateKey, protocol, adresa string, expect ed25519.PublicKey) (time.Duration, error) {
	nc, baza, err := k.uTunel(ctx, adresa, PutTunela)
	if err != nil {
		return 0, err
	}
	rtt, err := ProvjeriKljucOn(ctx, nc, priv, protocol, baza, expect)
	if err != nil {
		return 0, porukaTunela(baza, err)
	}
	return rtt, nil
}
