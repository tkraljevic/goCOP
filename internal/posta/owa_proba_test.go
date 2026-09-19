package posta

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"regexp"
	"strings"
	"testing"
)

// Proba prema pravom poslužitelju s izmišljenom lozinkom: razgovor mora
// proći do kraja i završiti odbijenom prijavom. Pokreće se samo ručno.
func TestProbaOWA(t *testing.T) {
	host := os.Getenv("GOCOP_PROBA_OWA")
	if host == "" {
		t.Skip()
	}
	p := Postavke{Nacin: NacinEWS, Posluzitelj: host}
	_, z, err := ewsPozoviIzazov(context.Background(), p, Racun{Korisnik: "netko@voda.hr", Lozinka: "izmisljena"}, ewsMapaPoslano)
	t.Logf("greška: %v", err)
	if z != nil {
		t.Logf("domena: %q, DNS: %q, KEY_EXCH: %v", z.NetBIOSDomena(), z.tekst(avDnsDomain), z.Zastavice&ntlmKeyExch != 0)
	}
	if !errors.Is(err, ErrPrijava) || z == nil {
		t.Fatal("očekivana odbijena prijava nakon cijelog razgovora")
	}
	ime, err := ewsPrijavi(context.Background(), p, Racun{Korisnik: "netko@voda.hr", Lozinka: "izmisljena"})
	t.Logf("prijavi: %q %v", ime, err)
	// veliko tijelo: drugi i treći korak moraju ići istom vezom
	var dnevnik bytes.Buffer
	log.SetOutput(&dnevnik)
	defer log.SetOutput(os.Stderr)
	veliko := ewsMapaPoslano + "<!-- " + strings.Repeat("x", 400000) + " -->"
	_, _, err = ewsPozoviIzazov(context.Background(), p, Racun{Korisnik: "netko@voda.hr", Lozinka: "izmisljena"}, veliko)
	t.Logf("veliko tijelo: %v", err)
	trag := dnevnik.String()
	if !errors.Is(err, ErrPrijava) || strings.Count(trag, "ponovno=true") < 1 {
		t.Fatalf("treći korak mora ići istom vezom kao drugi:\n%s", trag)
	}
	t.Logf("trag: %s", regexp.MustCompile(`TlRMTVNT[A-Za-z0-9+/=]*`).ReplaceAllString(trag, "<izazov>"))
}
