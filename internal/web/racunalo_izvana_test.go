package web

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/peers"
)

// Adresa za prijavu izvana je https adresa tunela, bez puta, svaka jednom;
// adrese bez https ne vode na prijavu (Odluke (18. 11. 2026.),
// Administracija, novo ponašanje 7)
func TestAdresePrijaveIzvana(t *testing.T) {
	d := SettingsPageData{
		Self: peers.Peer{Name: "Ured", Addresses: []string{"192.168.1.20", "https://gocop.primjer.hr/razmjena/tunel"}},
		Peers: []peers.Peer{
			{Name: "Laptop", Addresses: []string{"https://gocop.primjer.hr"}},
			{NodeID: "kucni", Addresses: []string{"https://kuca.primjer.hr", "http://neispravno.primjer.hr", "%zlo"}},
		},
	}
	got := d.AdresePrijaveIzvana()
	if len(got) != 2 || got[0] != (AdresaPrijave{"Ured (ovo računalo)", "https://gocop.primjer.hr"}) || got[1] != (AdresaPrijave{"kucni", "https://kuca.primjer.hr"}) {
		t.Errorf("%+v", got)
	}
	if (SettingsPageData{}).AdresePrijaveIzvana() != nil {
		t.Error("bez adresa ne smije biti adrese za prijavu")
	}
}

// Izvana su kvačice uloga onemogućene, a umjesto gumba stoji zašto; uz
// ulogu preuzimanja piše minuta u satu
func TestUlogeIzvanaIMinutaPreuzimanja(t *testing.T) {
	prikaz := func(izvana bool, postavke *PostavkeStranice) string {
		t.Helper()
		var b bytes.Buffer
		data := SettingsPageData{CurrentUser: adminIvic, Permissions: &models.UserPermissions{IsGlobalAdmin: true}, Izvana: izvana, Postavke: postavke,
			Self: peers.Peer{Name: "Ured", Addresses: []string{"https://gocop.primjer.hr"}}}
		if err := predlozakStranice(t, "settings.html").ExecuteTemplate(&b, "settings.html", data); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	kvacica := regexp.MustCompile(`<input type="checkbox" name="(preuzima|izdaje)"[^>]*>`)

	html := prikaz(true, nil)
	for _, k := range kvacica.FindAllString(html, -1) {
		if !strings.Contains(k, "disabled") {
			t.Errorf("izvana kvačica nije onemogućena: %s", k)
		}
	}
	if !strings.Contains(html, `id="uloge-izvana"`) || strings.Contains(html, "Spremi uloge") {
		t.Error("izvana mora pisati zašto, bez gumba Spremi uloge")
	}
	if !strings.Contains(html, `<a class="mono" href="https://gocop.primjer.hr" rel="noopener">`) {
		t.Error("nema adrese za prijavu izvana")
	}

	postavke := &PostavkeStranice{blokovi: map[string]*BlokPostavki{"cvor": {Stavke: []StavkaPostavke{{Kljuc: "vodostaji.minuta_preuzimanja", Prikaz: "45"}}}}}
	html = prikaz(false, postavke)
	if !strings.Contains(html, "svaki sat, u 45. minuti") || !strings.Contains(html, "Spremi uloge") {
		t.Error("lokalno: minuta preuzimanja i gumb")
	}
	for _, k := range kvacica.FindAllString(html, -1) {
		if strings.Contains(k, "disabled") {
			t.Errorf("lokalno kvačica onemogućena: %s", k)
		}
	}
	if postavke.Vrijednost("nema.toga") != "" || (*PostavkeStranice)(nil).Vrijednost("x") != "" {
		t.Error("nepoznata postavka mora biti prazna")
	}
}
