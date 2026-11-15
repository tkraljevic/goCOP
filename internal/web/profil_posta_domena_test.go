package web

import (
	"io/fs"
	"strings"
	"testing"

	"gocop/internal/models"
	webassets "gocop/web"
)

// Odluke (15. 11. 2026.): Sandučić i e-pošta tvrtke rade samo s Exchangeom
// tvrtke. Osoba s adresom izvan domene (npr. izvođač s Gmailom) na Profilu
// umjesto obrasca vidi rečenicu; bez upisane adrese obrazac ostaje.
func TestProfilPostaSamoZaDomenuTvrtke(t *testing.T) {
	za := func(adresa string, p *ProfilPrijaveIzvana) string {
		return UserPageData{User: &models.User{Email: adresa}, PrijavaIzvana: p}.DomenaPoste()
	}
	if d := za("ivo.primjer@gmail.com", nil); d != "voda.hr" {
		t.Errorf("Gmail: %q", d)
	}
	for _, adresa := range []string{"ana@voda.hr", " Ana@VODA.hr ", ""} {
		if d := za(adresa, nil); d != "" {
			t.Errorf("%q ima Sandučić, a dobio je %q", adresa, d)
		}
	}
	if d := za("ana@voda.hr", &ProfilPrijaveIzvana{Domena: "firma.hr"}); d != "firma.hr" {
		t.Errorf("domena iz postavki: %q", d)
	}
	if d := (UserPageData{}).DomenaPoste(); d != "" {
		t.Errorf("bez računa: %q", d)
	}
	b, err := fs.ReadFile(webassets.Files, "templates/profile.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Sandučić i e-pošta tvrtke rade samo s adresom @{{.}}, pa ih s vašom adresom nema.") ||
		!strings.Contains(string(b), `{{if not .DomenaPoste}}<a href="/posta"`) {
		t.Error("Profil osobi izvan domene mora reći da Sandučića nema i ne nuditi gumb")
	}
}
