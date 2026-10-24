package razmjena

// Provjera veze s drugim računalom (plan WEB synca, F2a, gumb „Provjeri vezu
// s računalom”). Rukovanje ide samo dok druga strana ne dokaže ključ i tu
// stane: ovaj čvor ne pošalje svoj certifikat ni potvrdu članstva, pa ga
// druga strana ne pusti dalje od rukovanja. Kod nje je to rukovanje koje nije
// završilo (VrataRukovanje), a ne neuspjela razmjena koja bi odgodila iduću.

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"errors"
	"net"
	"time"
)

// errKljucDokazan prekida rukovanje provjere čim druga strana dokaže ključ
var errKljucDokazan = errors.New("ključ druge strane je dokazan")

// errBezDokaza: rukovanje je završilo, a provjera ključa nije ni pitana
var errBezDokaza = errors.New("rukovanje je završilo bez provjere ključa druge strane")

// ProvjeriKljucOn provjeri ključ druge strane preko otvorene veze i zatvori
// je. Vraća vrijeme od početka rukovanja do dokaza ključa, otprilike jedan
// obilazak. Druga strana provjerava se kao u DialExchangeOn (provjeriDrugu),
// a addr je samo za poruku o grešci.
func ProvjeriKljucOn(ctx context.Context, raw net.Conn, priv ed25519.PrivateKey, protocol, addr string, expect ed25519.PublicKey) (time.Duration, error) {
	defer raw.Close() //nolint:errcheck // provjera je gotova, a greška zatvaranja ne mijenja ishod
	cfg, err := tlsConfig(priv, protocol)
	if err != nil {
		return 0, err
	}
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if _, _, err := provjeriDrugu(cs.PeerCertificates, protocol, addr, expect); err != nil {
			return err
		}
		return errKljucDokazan
	}
	pocetak := time.Now()
	if err := tls.Client(raw, cfg).HandshakeContext(ctx); !errors.Is(err, errKljucDokazan) {
		return 0, cmp.Or(err, errBezDokaza)
	}
	return time.Since(pocetak), nil
}

// ProvjeriTunel je ProvjeriKljucOn kroz tunel (https://domena); greška nosi
// adresu tunela, kao uz DialTunel
func (k *KlijentTunela) ProvjeriTunel(ctx context.Context, priv ed25519.PrivateKey, protocol, adresa string, expect ed25519.PublicKey) (time.Duration, error) {
	nc, baza, err := k.uTunel(ctx, adresa)
	if err != nil {
		return 0, err
	}
	rtt, err := ProvjeriKljucOn(ctx, nc, priv, protocol, baza, expect)
	if err != nil {
		return 0, porukaTunela(baza, err)
	}
	return rtt, nil
}
