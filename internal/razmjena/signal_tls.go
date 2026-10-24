package razmjena

// TLS signala (plan WEB synca, F2b). Unutar WebSocketa na /razmjena/signal
// ide TLS 1.3 s ključevima čvorova, kao u tunelu, uz ALPN
// „<protokol>-signal/1”, pa se veza signala ne da zamijeniti razmjenom ni
// obrnuto. Sastajalište kao poslužitelj šalje certifikat samo s ključem:
// članstvo i ovlast idu samo u certifikatu klijenta, pa ih strani klijent ne
// vidi. Rukovanje sastajališta traži isti ALPN, a članstvo klijenta
// sastajalište provjeri odmah nakon rukovanja, prije prvog čitanja, i
// nečlanu pošalje odbijenicu (sastajaliste.go). Klijent stane prije svog
// certifikata kad sastajalište nema očekivani ključ (provjeriDrugu), kao
// DialExchangeOn.

import (
	"crypto/ed25519"
	"crypto/tls"
	"errors"
)

// errDrugiALPN: druga strana nije dogovorila ALPN signala
var errDrugiALPN = errors.New("druga strana ne govori signal sastajališta")

// alpnSignala je ALPN signala za ime protokola
func alpnSignala(protokol string) string { return protokol + "-signal/1" }

// tlsPosluziteljaSignala je TLS sastajališta: certifikat bez vjerodajnica, a
// klijent mora dogovoriti ALPN signala i pokazati certifikat čvora
func tlsPosluziteljaSignala(priv ed25519.PrivateKey, protokol string) (*tls.Config, error) {
	cfg, err := tlsConfig(priv, protokol)
	if err != nil {
		return nil, err
	}
	cfg.NextProtos = []string{alpnSignala(protokol)}
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if cs.NegotiatedProtocol != alpnSignala(protokol) {
			return errDrugiALPN
		}
		_, _, err := dokazaniKljuc(cs.PeerCertificates, protokol)
		return err
	}
	return cfg, nil
}

// tlsKlijentaSignala je TLS računala koje se prijavljuje: s vjerodajnicama,
// a sastajalište mora dokazati očekivani ključ i dogovoriti ALPN signala
func tlsKlijentaSignala(priv ed25519.PrivateKey, protokol string, vj func() []byte, addr string, expect ed25519.PublicKey) (*tls.Config, error) {
	cfg, err := tlsConfigS(priv, protokol, vj)
	if err != nil {
		return nil, err
	}
	cfg.NextProtos = []string{alpnSignala(protokol)}
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if cs.NegotiatedProtocol != alpnSignala(protokol) {
			return errDrugiALPN
		}
		_, _, err := provjeriDrugu(cs.PeerCertificates, protokol, addr, expect)
		return err
	}
	return cfg, nil
}
