package web

import (
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Obrazac kišomjera po crtežu 17e, 6f: sva polja u pločama, ploča „Što
// predstavlja u međuslivu” skrivena je za stvarni kišomjer (polja ostaju u
// obrascu), a Obriši je zaseban obrazac.
func TestObrazacKisomjeraPoCrtezu(t *testing.T) {
	polja := []string{"naziv", "code", "vrsta", "korak", "izvor", "izvor_sifra", "sliv", "pojas", "lat", "lon",
		"visina", "srednja_visina", "km2", "tezina", "napomena", "aktivan"}
	d := SlivoviPageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Tocka:  models.Kisomjer{Code: "kis-proba", Naziv: "Proba", Vrsta: models.KisomjerIzvedeni, Aktivan: true},
		IsEdit: true,
	}
	html := iscrtaj(t, "kisomjer_form.html", d)
	glavni := izmedju(html, `<form class="form-page form-wide"`, "</form>")
	for _, p := range polja {
		if n := strings.Count(glavni, `name="`+p+`"`); n != 1 {
			t.Errorf("polje %s je u obrascu %d puta", p, n)
		}
	}
	for _, want := range []string{`class="povratna-poveznica">← Proba`, "Uredi kišomjer", "<h3>Točka</h3>",
		"<h3>Izvor podataka</h3>", "<h3>Položaj</h3>", `<section class="list-kartica" id="kis-izvedena" >`,
		`<form id="obrisi-kisomjer" method="POST" action="/api/slivovi/kisomjer/delete"`, `form="obrisi-kisomjer"`} {
		if !strings.Contains(html, want) {
			t.Errorf("obrazac kišomjera nema %q", want)
		}
	}
	if strings.Contains(html, "style=") {
		t.Error("obrazac kišomjera ima stil u retku")
	}

	d.Tocka.Vrsta, d.IsEdit = models.KisomjerStvarni, false
	html = iscrtaj(t, "kisomjer_form.html", d)
	if !strings.Contains(html, `id="kis-izvedena" hidden>`) {
		t.Error("ploča izvedene točke vidi se uz stvarni kišomjer")
	}
	if !strings.Contains(izmedju(html, `<form class="form-page form-wide"`, "</form>"), `name="tezina"`) {
		t.Error("polje težine nije u obrascu stvarnog kišomjera")
	}
	for _, ne := range []string{"obrisi-kisomjer", "Uredi kišomjer"} {
		if strings.Contains(html, ne) {
			t.Errorf("novi kišomjer ima %q", ne)
		}
	}
	if !strings.Contains(html, "Novi stvarni kišomjer") || !strings.Contains(html, `href="/slivovi" class="povratna-poveznica"`) {
		t.Error("novi stvarni kišomjer nema naslov ili povratak na Slivove")
	}
}

// Brojevi koje obrazac ispiše moraju se spremiti nepromijenjeni: s točkom za
// tisućice površina od 2993 km² vraćala se kao 2,993, a visina od 1234,5 m
// kao 1,235.
func TestObrazacKisomjeraVracaIsteBrojeve(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	k := models.Kisomjer{Code: "kis-proba", Naziv: "Proba", Vrsta: models.KisomjerIzvedeni, Latitude: 46.7, Longitude: 14,
		Visina: f(1234.5), SrednjaVisina: f(1728), Km2: f(2993), Tezina: f(0.2043)}
	html := iscrtaj(t, "kisomjer_form.html", SlivoviPageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true}, Tocka: k, IsEdit: true,
	})
	obrazac := url.Values{}
	for _, m := range regexp.MustCompile(`name="(\w+)"[^>]*value="([^"]*)"`).FindAllStringSubmatch(izmedju(html, `<form class="form-page form-wide"`, "</form>"), -1) {
		obrazac.Set(m[1], m[2])
	}
	r := httptest.NewRequest("POST", "/api/slivovi/kisomjer/update", strings.NewReader(obrazac.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	natrag, err := kisomjerIzZahtjeva(r)
	if err != nil {
		t.Fatal(err)
	}
	for ime, par := range map[string][2]*float64{"visina": {k.Visina, natrag.Visina}, "srednja_visina": {k.SrednjaVisina, natrag.SrednjaVisina},
		"km2": {k.Km2, natrag.Km2}, "tezina": {k.Tezina, natrag.Tezina}} {
		if par[1] == nil || *par[1] != *par[0] {
			t.Errorf("%s: upisano %v, vraćeno %q", ime, *par[0], unosD(par[1], -1))
		}
	}
}
