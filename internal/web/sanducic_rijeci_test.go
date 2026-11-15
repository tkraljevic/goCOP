package web

import (
	"io/fs"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	webassets "gocop/web"
)

// Plan Sandučića, koraci 2–8: isti naziv „Lozinka e-pošte”, poslužitelj
// tvrtke umjesto Exchangea u tekstu, veličina privitka u kB/MB, povratak u
// mapu iz koje se došlo i upozorenje umjesto greške kad je lozinka spremljena.
func TestSanducicSitniPopravci(t *testing.T) {
	r := httptest.NewRequest("POST", "/posta/pismo", strings.NewReader(url.Values{"mapa": {"sentitems"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if g := mapaPovratka(r); g != "/posta?mapa=sentitems" {
		t.Errorf("povratak u mapu: %q", g)
	}
	if g := mapaPovratka(httptest.NewRequest("POST", "/posta/pismo", nil)); g != "/posta" {
		t.Errorf("bez mape: %q", g)
	}
	for _, p := range []string{"posta_sanducic.html", "posta_pismo.html", "posta_novo.html", "posta_racun.html", "akt.html", "imenik_exchange.html", "base.html"} {
		b, err := fs.ReadFile(webassets.Files, "templates/"+p)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, staro := range []string{"E-pošta za slanje akata", "iz Exchangea", "preko Exchangea", "Lozinku možete upisati i sada", "OdabraniAkt", "{{.Velicina}} B"} {
			if strings.Contains(s, staro) {
				t.Errorf("%s još piše %q", p, staro)
			}
		}
	}
	b, err := fs.ReadFile(webassets.Files, "templates/posta_racun.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `{{with .Upozorenje}}<div class="alert alert-warning">`) {
		t.Error("spremljena lozinka bez provjere prijave mora biti upozorenje, ne greška")
	}
}
