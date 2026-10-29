package web

import (
	"context"
	"html/template"
	"io/fs"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
	webassets "gocop/web"
)

var kartaZaVodotok = KartaPostavke{
	Plocice:  "https://maps.wikimedia.org/osm-intl/{z}/{x}/{y}.png",
	Zasluge:  "pločice Wikimedia",
	NajviseZ: 17,
}

// alat karte vodotoka: Geoman, zajednički natpisi i dio za tok
var skripteAlataVodotoka = []string{
	"vendor/geoman/leaflet-geoman.min.js",
	"vendor/geoman/leaflet-geoman.css",
	"js/uredjivac-karte.js",
	"js/uredjivac-vodotoka.js",
}

// ostaci starog uređivača toka koji ne smiju ostati uz novi alat
var stariUredjivacToka = []string{"karta-ponisti-btn", "karta-editor-traka", "karta-editor-status", "Poništi", "Spremi geometriju"}

func iscrtajVodotok(t *testing.T, admin bool, geometrija template.JS) string {
	t.Helper()
	return iscrtaj(t, "watercourse_detail.html", WatercoursePageData{
		CurrentUser:  &models.User{FullName: "P"},
		Permissions:  &models.UserPermissions{IsGlobalAdmin: admin},
		CanEdit:      admin,
		Water:        models.Watercourse{Code: "potok-proba", Name: "Proba", OfficialName: "potok Proba"},
		Karta:        kartaZaVodotok,
		GeometryJSON: geometrija,
	})
}

func imaSve(t *testing.T, html, opis string, trazi []string) {
	t.Helper()
	for _, s := range trazi {
		if !strings.Contains(html, s) {
			t.Errorf("%s: nema %q", opis, s)
		}
	}
}

func nemaNista(t *testing.T, html, opis string, trazi []string) {
	t.Helper()
	for _, s := range trazi {
		if strings.Contains(html, s) {
			t.Errorf("%s: ima %q", opis, s)
		}
	}
}

// Administrator dobije kartu i alat i kad tok još nije ucrtan: karta je
// prazna, uz uputu kako ga ucrtati.
func TestKartaVodotokaAlatBezToka(t *testing.T) {
	html := iscrtajVodotok(t, true, "")
	imaSve(t, html, "administrator, bez toka", append([]string{
		`class="karta-vodotoka"`,
		`karta-uredi-btn`, `Uredi tok na karti`, `karta-spremi-btn`, `karta-odustani-btn`,
		`Ucrtaj iz GeoJSON-a`,
		`Tok ove vode još nije ucrtan. Nacrtajte ga gumbom „Uredi tok na karti” ili učitajte GeoJSON iz QGIS-a`,
		`{"type":"FeatureCollection","features":[]}`,
	}, skripteAlataVodotoka...))
	nemaNista(t, html, "administrator, bez toka", append([]string{`Zamijeni iz GeoJSON-a`, `data-ima="1"`}, stariUredjivacToka...))
}

// S ucrtanim tokom administrator mijenja tok istim alatom, a datoteka ga zamjenjuje.
func TestKartaVodotokaAlatSTokom(t *testing.T) {
	html := iscrtajVodotok(t, true, `{"type":"FeatureCollection","features":[]}`)
	imaSve(t, html, "administrator, s tokom", append([]string{
		`karta-uredi-btn`, `Zamijeni iz GeoJSON-a`, `data-ima="1"`,
	}, skripteAlataVodotoka...))
	nemaNista(t, html, "administrator, s tokom", append([]string{`Tok ove vode još nije ucrtan`, `Ucrtaj iz GeoJSON-a`}, stariUredjivacToka...))
}

// Korisnik bez prava vidi kartu samo kad tok postoji, i to bez alata.
func TestKartaVodotokaBezPrava(t *testing.T) {
	bezAlata := append([]string{`karta-uredi-btn`, `karta-spremi-btn`, `ucitajTokVode`, `GeoJSON-a`}, skripteAlataVodotoka...)

	sTokom := iscrtajVodotok(t, false, `{"type":"FeatureCollection","features":[]}`)
	imaSve(t, sTokom, "bez prava, s tokom", []string{`class="karta-vodotoka"`, `Karta vodotoka`})
	nemaNista(t, sTokom, "bez prava, s tokom", append(bezAlata, stariUredjivacToka...))

	bezToka := iscrtajVodotok(t, false, "")
	nemaNista(t, bezToka, "bez prava, bez toka", append([]string{`karta-vodotoka`, `Karta vodotoka`}, bezAlata...))
}

// Bez pločica karte administrator i dalje može učitati tok iz datoteke.
func TestKartaVodotokaBezPlocica(t *testing.T) {
	html := iscrtaj(t, "watercourse_detail.html", WatercoursePageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		CanEdit:     true,
		Water:       models.Watercourse{Code: "potok-proba", Name: "Proba"},
	})
	imaSve(t, html, "bez pločica", []string{`Ucrtaj iz GeoJSON-a`, `ucitajTokVode`})
	nemaNista(t, html, "bez pločica", []string{`class="karta-vodotoka"`, `karta-uredi-btn`})
}

// Pravo na alat daje stranica vode: globalni administrator da, ostali ne.
func TestStranicaVodotokaPravoNaAlat(t *testing.T) {
	_, repo, h := bazaVoda(t)
	if err := repo.CreateWatercourse(context.Background(), &models.Watercourse{Code: "potok-bez-toka", OfficialName: "potok Bez toka", Name: "Bez toka"}); err != nil {
		t.Fatal(err)
	}
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("watercourse_detail.html")...)
	if err != nil {
		t.Fatal(err)
	}
	h.SetPageTemplates(tmpl, nil, nil)
	h.karta = func() KartaPostavke { return kartaZaVodotok }

	stranica := func(perms *models.UserPermissions) string {
		r := httptest.NewRequest("GET", "/watercourses/potok-bez-toka", nil)
		r.SetPathValue("code", "potok-bez-toka")
		r = r.WithContext(context.WithValue(context.WithValue(r.Context(), contextKeyPerms, perms), contextKeyUser, &models.User{ID: uuid.New(), FullName: "P"}))
		w := httptest.NewRecorder()
		h.ShowWatercourse(w, r)
		if w.Code != 200 {
			t.Fatalf("stranica vode: %d %.300s", w.Code, w.Body.String())
		}
		return w.Body.String()
	}

	admin := stranica(&models.UserPermissions{IsGlobalAdmin: true})
	imaSve(t, admin, "globalni administrator", append([]string{`karta-uredi-btn`, `class="karta-vodotoka"`}, skripteAlataVodotoka...))

	ostali := stranica(&models.UserPermissions{AdminSectors: map[string]bool{"B": true}})
	nemaNista(t, ostali, "administrator sektora", append([]string{`karta-uredi-btn`, `karta-vodotoka`}, skripteAlataVodotoka...))
}

// Hrvatski natpisi alata karte postoje samo na jednom mjestu, a obje karte
// ga učitaju prije svog dijela; vodotok crta samo crte.
func TestAlatKarteZajednickiNatpisi(t *testing.T) {
	procitaj := func(put string) string {
		b, err := fs.ReadFile(webassets.Files, put)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if !strings.Contains(procitaj("static/js/uredjivac-karte.js"), "buttonTitles") {
		t.Error("zajednički dio nema hrvatske natpise")
	}
	for _, put := range []string{"static/js/uredjivac-objekta.js", "static/js/uredjivac-vodotoka.js", "static/js/app.js"} {
		if strings.Contains(procitaj(put), "removeLastVertex") {
			t.Errorf("%s ima vlastite natpise alata karte", put)
		}
	}
	vodotok := procitaj("static/js/uredjivac-vodotoka.js")
	for _, s := range []string{"drawPolyline: true", "drawMarker: false", "drawPolygon: false", "drawCircleMarker: false", "drawRectangle: false", "drawText: false"} {
		if !strings.Contains(vodotok, s) {
			t.Errorf("alat karte vodotoka: nema %q", s)
		}
	}
	for _, stranica := range []string{"templates/structure_detail.html", "templates/watercourse_detail.html"} {
		html := procitaj(stranica)
		zajednicki := strings.Index(html, "js/uredjivac-karte.js")
		geoman := strings.Index(html, "leaflet-geoman.min.js")
		if zajednicki < 0 || geoman < 0 || geoman > zajednicki {
			t.Errorf("%s: Geoman i zajednički dio alata nisu učitani redom", stranica)
		}
	}
	objekt := procitaj("templates/structure_detail.html")
	if strings.Index(objekt, "js/uredjivac-karte.js") > strings.Index(objekt, "js/uredjivac-objekta.js") {
		t.Error("karta objekta učitava svoj dio prije zajedničkog")
	}
}
