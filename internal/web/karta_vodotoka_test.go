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

// Crta se samo na Karti (Odluke (4. 11. 2026.)): stranica vode nosi samo
// učitavanje datoteke i gumb prema Karti, bez Geomana i mjere
var skripteNaKarti = []string{
	"vendor/geoman/leaflet-geoman.min.js",
	"vendor/geoman/leaflet-geoman.css",
	"js/mjera-karte.js",
	"js/karta-uredjivanje.js",
}

// ostaci starih uređivača toka koji ne smiju ostati na stranici vode
var stariUredjivacToka = []string{"karta-ponisti-btn", "karta-editor-traka", "karta-editor-status", "Poništi", "Spremi geometriju",
	"karta-uredi-btn", "karta-spremi-btn", "karta-odustani-btn", "uredjivac-"}

// alat stranice vode: datoteka i put na Kartu
var alatStraniceVode = []string{"js/datoteka-karte.js", `href="/karta#uredi=vodotok:potok-proba"`, `Uredi tok na Karti`, `ucitajTokVode`, `.kml`}

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

// Administrator dobije kartu, učitavanje datoteke i put na Kartu i kad tok
// još nije ucrtan, uz uputu kako ga ucrtati.
func TestKartaVodotokaAlatBezToka(t *testing.T) {
	html := iscrtajVodotok(t, true, "")
	imaSve(t, html, "administrator, bez toka", append([]string{
		`class="karta-vodotoka"`, `Učitaj KML ili GeoJSON`,
		`Tok ove vode još nije ucrtan. Nacrtajte ga na Karti gumbom „Uredi tok na Karti” ili učitajte KML ili GeoJSON`,
		`{"type":"FeatureCollection","features":[]}`,
	}, alatStraniceVode...))
	nemaNista(t, html, "administrator, bez toka", append(append([]string{`Zamijeni iz`, `data-ima="1"`}, stariUredjivacToka...), skripteNaKarti...))
}

// S ucrtanim tokom datoteka zamjenjuje tok, a crta se i dalje na Karti.
func TestKartaVodotokaAlatSTokom(t *testing.T) {
	html := iscrtajVodotok(t, true, `{"type":"FeatureCollection","features":[]}`)
	imaSve(t, html, "administrator, s tokom", append([]string{`Zamijeni iz KML-a ili GeoJSON-a`, `data-ima="1"`}, alatStraniceVode...))
	nemaNista(t, html, "administrator, s tokom", append(append([]string{`Tok ove vode još nije ucrtan`, `Učitaj KML ili GeoJSON`}, stariUredjivacToka...), skripteNaKarti...))
}

// Korisnik bez prava vidi kartu samo kad tok postoji, i to bez alata.
func TestKartaVodotokaBezPrava(t *testing.T) {
	bezAlata := append(append([]string{`GeoJSON`, `/karta#uredi`}, alatStraniceVode...), skripteNaKarti...)

	sTokom := iscrtajVodotok(t, false, `{"type":"FeatureCollection","features":[]}`)
	imaSve(t, sTokom, "bez prava, s tokom", []string{`class="karta-vodotoka"`, `Karta vodotoka`})
	nemaNista(t, sTokom, "bez prava, s tokom", append(bezAlata, stariUredjivacToka...))

	bezToka := iscrtajVodotok(t, false, "")
	nemaNista(t, bezToka, "bez prava, bez toka", append([]string{`karta-vodotoka`, `Karta vodotoka`}, bezAlata...))
}

// Bez pločica karte administrator i dalje može učitati tok iz datoteke ili
// ga nacrtati na Karti.
func TestKartaVodotokaBezPlocica(t *testing.T) {
	html := iscrtaj(t, "watercourse_detail.html", WatercoursePageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		CanEdit:     true,
		Water:       models.Watercourse{Code: "potok-proba", Name: "Proba"},
	})
	imaSve(t, html, "bez pločica", append([]string{`Učitaj KML ili GeoJSON`}, alatStraniceVode...))
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
	imaSve(t, admin, "globalni administrator", []string{`href="/karta#uredi=vodotok:potok-bez-toka"`, `class="karta-vodotoka"`, "js/datoteka-karte.js"})

	ostali := stranica(&models.UserPermissions{AdminSectors: map[string]bool{"B": true}})
	nemaNista(t, ostali, "administrator sektora", []string{`/karta#uredi`, `karta-vodotoka`, "js/datoteka-karte.js"})
}

// Alat za crtanje je samo na Karti: hrvatski natpisi su na jednom mjestu, a
// Karta učita Geoman, mjeru i čitanje datoteke prije uređivača, a uređivač
// prije karta.js. Stranice objekta i vode Geoman ne učitavaju.
func TestAlatKarteSamoNaKarti(t *testing.T) {
	procitaj := func(put string) string {
		b, err := fs.ReadFile(webassets.Files, put)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	ured := procitaj("static/js/karta-uredjivanje.js")
	for _, s := range []string{"buttonTitles", "removeLastVertex", "mjeraKarte.prati(karta, grupa)", "datotekaKarte.procitaj"} {
		if !strings.Contains(ured, s) {
			t.Errorf("uređivač na Karti: nema %q", s)
		}
	}
	for _, put := range []string{"static/js/app.js", "static/js/karta.js"} {
		if strings.Contains(procitaj(put), "removeLastVertex") {
			t.Errorf("%s ima vlastite natpise alata karte", put)
		}
	}
	karta := procitaj("templates/karta.html")
	redom := []string{"leaflet-geoman.min.js", "js/mjera-karte.js", "js/datoteka-karte.js", "js/karta-uredjivanje.js", "js/karta.js"}
	for i := 1; i < len(redom); i++ {
		a, b := strings.Index(karta, redom[i-1]), strings.Index(karta, redom[i])
		if a < 0 || b < 0 || a > b {
			t.Errorf("karta.html: %s nije učitan prije %s", redom[i-1], redom[i])
		}
	}
	for _, stranica := range []string{"templates/structure_detail.html", "templates/watercourse_detail.html"} {
		html := procitaj(stranica)
		for _, s := range []string{"geoman", "mjera-karte.js", "uredjivac-", "karta-uredi-btn"} {
			if strings.Contains(html, s) {
				t.Errorf("%s: ima %q, a crta se samo na Karti", stranica, s)
			}
		}
		if !strings.Contains(html, "js/datoteka-karte.js") || !strings.Contains(html, "/karta#uredi=") {
			t.Errorf("%s: nema učitavanja datoteke ili puta na Kartu", stranica)
		}
	}
}

// Kanal II. reda pokazuje svoju skupinu uz naziv, a natpis „Skupina u
// Odluci” ostaje samo vodama s popisa voda I. reda.
func TestStranicaVodotokaKanalIIReda(t *testing.T) {
	kanal := iscrtaj(t, "watercourse_detail.html", WatercoursePageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{},
		Water:       models.Watercourse{Code: "kanal-proba", Name: "Proba", OfficialName: "kanal Proba", Kind: "kanal", Subcategory: "kanal II. reda"},
	})
	imaSve(t, kanal, "kanal II. reda", []string{"kanal II. reda"})
	nemaNista(t, kanal, "kanal II. reda", []string{"Skupina u Odluci", "nije voda I. reda"})

	odluka := iscrtaj(t, "watercourse_detail.html", WatercoursePageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{},
		Water:       models.Watercourse{Code: "rijeka-proba", Name: "Proba", OfficialName: "rijeka Proba", Category: "DRUGE VEĆE VODE I KANALI", Subcategory: "vodotoci"},
	})
	imaSve(t, odluka, "voda I. reda", []string{"Druga veća voda ili kanal", "Skupina u Odluci", "vodotoci"})
}
