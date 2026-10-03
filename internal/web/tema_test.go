package web

import (
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gocop/internal/models"
	webassets "gocop/web"
)

// Bez promjene teme stranica ne traži /tema.css; s promjenom ga traži s
// otiskom, koji se smije držati u pregledniku jer se mijenja s temom
func TestStilTemePosluzenSOtiskom(t *testing.T) {
	t.Cleanup(func() { models.SetTema(models.Tema{}) })
	models.SetTema(models.Tema{})
	if a := temaCSSAdresa(); a != "" {
		t.Errorf("zadana tema ne treba /tema.css, a adresa je %q", a)
	}

	tema := models.Tema{Svijetla: models.BojeTeme{Glavna: "#1f4f8f"}}
	models.SetTema(tema)
	adresa := temaCSSAdresa()
	if adresa != "/tema.css?v="+tema.Verzija() {
		t.Fatalf("adresa %q", adresa)
	}
	w := httptest.NewRecorder()
	ServeTemaCSS(w, httptest.NewRequest(http.MethodGet, adresa, nil))
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("Content-Type %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("adresa s otiskom: Cache-Control %q", cc)
	}
	if !strings.Contains(w.Body.String(), "--primary: #1f4f8f;") {
		t.Errorf("stil nema glavne boje: %s", w.Body.String())
	}

	// stari otisak (stranica otvorena prije promjene) ne smije zapeti
	w = httptest.NewRecorder()
	ServeTemaCSS(w, httptest.NewRequest(http.MethodGet, "/tema.css?v=star", nil))
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("stari otisak: Cache-Control %q", cc)
	}
}

// Obrazac šalje sve boje; one jednake zadanima spremaju se kao prazne, pa
// spremanje bez promjene ne stvara temu, a gumb svijetle teme i dalje prati
// glavnu boju
func TestObrazacTemeSpremaSamoPromjene(t *testing.T) {
	zahtjev := func(v url.Values) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/administracija/tema", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}
	zadano := url.Values{
		"svijetla_glavna": {"#173e74"}, "svijetla_naglasak": {"#20ba70"}, "svijetla_gumb": {"#173e74"},
		"tamna_glavna": {"#6f9bd9"}, "tamna_naglasak": {"#2fd08a"}, "tamna_gumb": {"#3a6db5"},
	}
	tm, err := temaIzObrasca(zahtjev(zadano))
	if err != nil || !tm.Prazna() {
		t.Fatalf("zadane boje dale temu %+v, %v", tm, err)
	}

	// nova glavna boja; gumb ju je pratio pa i on nosi novu
	v := url.Values{}
	for k, x := range zadano {
		v[k] = x
	}
	v.Set("svijetla_glavna", "1F4F8F")
	v.Set("svijetla_gumb", "#1f4f8f")
	tm, err = temaIzObrasca(zahtjev(v))
	if err != nil {
		t.Fatal(err)
	}
	if tm.Svijetla.Glavna != "#1f4f8f" || tm.Svijetla.Gumb != "" || !(tm.Tamna == models.BojeTeme{}) {
		t.Errorf("očekivana samo glavna boja svijetle teme: %+v", tm)
	}

	v.Set("tamna_gumb", "crvena")
	if _, err := temaIzObrasca(zahtjev(v)); err == nil {
		t.Error("neispravna boja prošla")
	}
}

// Okviri pregleda na stranici teme od prvog iscrtavanja nose spremljenu
// temu, ne zadane boje iz style.css
func TestPregledTemeNosiSpremljenuTemu(t *testing.T) {
	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("administracija_tema.html")...)
	if err != nil {
		t.Fatal(err)
	}
	tm := models.Tema{Svijetla: models.BojeTeme{Glavna: "#7a1f5c"}}
	d := TemaData{CurrentUser: &models.User{FullName: "Uprava"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Tema: tm, Svijetla: tm.Svijetla.Popunjeno(models.ZadanaSvijetla, true), Tamna: tm.Tamna.Popunjeno(models.ZadanaTamna, false),
		Provjere: tm.Provjere(), Najmanje: models.NajmanjiDopusteniKontrast,
		PregledCSS: template.CSS(tm.CSSZa(`[data-tema-pregled="light"]`, `[data-tema-pregled="dark"]`))}
	var b strings.Builder
	if err := tp.ExecuteTemplate(&b, "administracija_tema.html", d); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	i := strings.Index(s, `<style id="tema-pregled-stil">`)
	if i < 0 {
		t.Fatal("nema stila pregleda")
	}
	stil := s[i:]
	stil = stil[:strings.Index(stil, "</style>")]
	if !strings.Contains(stil, `[data-tema-pregled="light"] {`) || !strings.Contains(stil, "--primary: #7a1f5c;") {
		t.Errorf("pregled ne nosi spremljenu temu: %q", stil)
	}
}
