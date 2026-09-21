package javnivodostaji

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// Uz grešku provjere certifikata mora pisati tko ga je potpisao. Poruka
// „signed by unknown authority“ inače ne razlikuje manjkav lanac na tuđoj
// stranici od mreže koja promet presreće i potpisuje ga svojim certifikatom.
func TestGreskaCertifikataImenujePotpisnika(t *testing.T) {
	c := &x509.Certificate{Issuer: pkix.Name{CommonName: "Sluzbeni proxy CA"}}
	izvorna := &url.Error{Op: "Get", URL: "https://gis.lfrz.gv.at/x",
		Err: fmt.Errorf("tls: failed to verify certificate: %w", x509.UnknownAuthorityError{Cert: c})}
	poruka := objasniTLS(izvorna).Error()
	if !strings.Contains(poruka, "Sluzbeni proxy CA") {
		t.Errorf("poruka ne imenuje potpisnika: %s", poruka)
	}
	if !strings.Contains(poruka, "gis.lfrz.gv.at") {
		t.Errorf("poruka je izgubila izvornu grešku: %s", poruka)
	}
}

// Greška koja nema veze s certifikatom mora proći nedirnuta.
func TestGreskaBezCertifikataOstajeIsta(t *testing.T) {
	izvorna := fmt.Errorf("veza prekinuta")
	if objasniTLS(izvorna) != izvorna {
		t.Error("greška bez certifikata ne smije se mijenjati")
	}
}
