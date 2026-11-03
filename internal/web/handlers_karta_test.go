package web

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	webassets "gocop/web"

	"gocop/internal/models"
)

func kartaTocka(v float64) *float64 { return &v }

// probniIzvori: nasip s crtom i točkom stacionaže, nasip bez crte, crpna
// stanica samo sa širinom i dužinom, ustava s ucrtanom točkom, sifon bez
// ičega; vodotok s tokom i rkm točkom, vodotok bez toka; letva s položajem i
// bez njega
func probniIzvori() (func(context.Context) ([]models.Structure, error), func(context.Context) ([]models.Watercourse, error), func(context.Context) ([]models.Station, error)) {
	nasip := `{"type":"FeatureCollection","features":[
		{"type":"Feature","properties":{"tip":"nasip"},"geometry":{"type":"LineString","coordinates":[[18.80,45.80],[18.81,45.81]]}},
		{"type":"Feature","properties":{"tip":"km","km":"0+000"},"geometry":{"type":"Point","coordinates":[18.80,45.80]}}]}`
	ustava := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"Point","coordinates":[18.7,45.6]}}]}`
	objekti := []models.Structure{
		{ID: uuid.New(), Name: "Nasip Proba", Kind: models.StructureKindEmbankment, Geometry: nasip},
		{ID: uuid.New(), Name: "Nasip bez crte", Kind: models.StructureKindEmbankment},
		{ID: uuid.New(), Name: "CS Proba", Kind: models.StructureKindPumpingStation, Latitude: kartaTocka(45.7), Longitude: kartaTocka(18.9)},
		{ID: uuid.New(), Name: "Ustava Proba", Kind: models.StructureKindSluice, Geometry: ustava},
		{ID: uuid.New(), Name: "Sifon Proba", Kind: models.StructureKindSiphon},
	}
	tok := `{"type":"FeatureCollection","features":[
		{"type":"Feature","properties":{"tip":"vodotok"},"geometry":{"type":"LineString","coordinates":[[18.0,45.0],[18.1,45.000001],[18.2,45.0]]}},
		{"type":"Feature","properties":{"tip":"rkm"},"geometry":{"type":"Point","coordinates":[18.0,45.0]}}]}`
	vodotoci := []models.Watercourse{
		{Code: "potok-proba", Name: "Proba", OfficialName: "potok Proba", Geometry: tok},
		{Code: "kanal-bez-toka", Name: "Bez toka"},
	}
	letve := []models.Station{
		{ID: uuid.New(), Name: "Letva Proba", Latitude: kartaTocka(45.5), Longitude: kartaTocka(18.6)},
		{ID: uuid.New(), Name: "Letva bez položaja"},
	}
	return func(context.Context) ([]models.Structure, error) { return objekti, nil },
		func(context.Context) ([]models.Watercourse, error) { return vodotoci, nil },
		func(context.Context) ([]models.Station, error) { return letve, nil }
}

func sloj(t *testing.T, skupine []SkupinaSlojeva, kljuc string) SlojKarte {
	t.Helper()
	for _, s := range skupine {
		for _, l := range s.Slojevi {
			if l.Kljuc == kljuc {
				return l
			}
		}
	}
	t.Fatalf("nema sloja %q", kljuc)
	return SlojKarte{}
}

// Uz svaki sloj piše koliko ga je ucrtano: nasip samo s crtom, objekt s
// točkom ili širinom i dužinom, vodotok s tokom, letva s položajem.
func TestKartaBrojiUcrtano(t *testing.T) {
	o, v, l := probniIzvori()
	h := NewKartaHandler(o, v, l, nil, nil, nil)
	k, err := h.slozi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		kljuc           string
		ucrtano, ukupno int
	}{
		{"nasipi", 1, 2}, {"crpne-stanice", 1, 1}, {"ustave", 1, 1}, {"brane", 0, 0}, {"ostali-objekti", 0, 1},
		{"vodotoci", 1, 2}, {"letve", 1, 2},
	} {
		if got := sloj(t, k.skupine, s.kljuc); got.Ucrtano != s.ucrtano || got.Ukupno != s.ukupno {
			t.Errorf("%s: %d od %d, htio %d od %d", s.kljuc, got.Ucrtano, got.Ukupno, s.ucrtano, s.ukupno)
		}
	}
	if !sloj(t, k.skupine, "nasipi").Crta || sloj(t, k.skupine, "nasipi").Ikona != "" || sloj(t, k.skupine, "ustave").Ikona == "" {
		t.Error("nasip treba znak crte, a ustava ikonu")
	}
	if z := sloj(t, k.skupine, "zupanije"); !z.Ukljucen {
		t.Error("županije trebaju biti uključene kad se Karta otvori")
	}
}

// Nasip na kartu ide samo crtom, a točke stacionaže ostaju na njegovoj
// stranici; crpna stanica bez ucrtanog dobije točku iz širine i dužine.
func TestKartaZnacajkeObjekata(t *testing.T) {
	o, v, l := probniIzvori()
	k, err := NewKartaHandler(o, v, l, nil, nil, nil).slozi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vrste := map[string][]string{}
	for _, f := range k.zaKartu.Objekti.Features {
		var g struct {
			Type        string    `json:"type"`
			Coordinates []float64 `json:"coordinates"`
		}
		_ = json.Unmarshal(f.Geometry, &g) // crta nema koordinata kao niz brojeva
		vrste[f.Properties["sloj"].(string)] = append(vrste[f.Properties["sloj"].(string)], g.Type)
		if f.Properties["sloj"] == "crpne-stanice" && (len(g.Coordinates) != 2 || g.Coordinates[0] != 18.9 || g.Coordinates[1] != 45.7) {
			t.Errorf("crpna stanica: točka %v, a treba dužina pa širina [18.9 45.7]", g.Coordinates)
		}
		if f.Properties["adresa"] == "" || f.Properties["naziv"] == "" {
			t.Errorf("značajka bez naziva ili adrese: %v", f.Properties)
		}
	}
	if got := strings.Join(vrste["nasipi"], ","); got != "LineString" {
		t.Errorf("nasip na karti: %s, a treba samo crta", got)
	}
	if got := strings.Join(vrste["ustave"], ","); got != "Point" {
		t.Errorf("ustava na karti: %s", got)
	}
	if len(vrste["ostali-objekti"]) != 0 {
		t.Error("sifon bez ičega ne smije na kartu")
	}
}

// Tok vode ide na kartu bez točaka stacionaže, pojednostavljen.
func TestKartaVodotociBezStacionaze(t *testing.T) {
	o, v, l := probniIzvori()
	k, err := NewKartaHandler(o, v, l, nil, nil, nil).slozi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n := len(k.zaKartu.Vodotoci.Features); n != 1 {
		t.Fatalf("vodotoka na karti: %d", n)
	}
	f := k.zaKartu.Vodotoci.Features[0]
	var g struct {
		Type        string         `json:"type"`
		Coordinates [][][2]float64 `json:"coordinates"`
	}
	if err := json.Unmarshal(f.Geometry, &g); err != nil {
		t.Fatal(err)
	}
	if g.Type != "MultiLineString" || len(g.Coordinates) != 1 || len(g.Coordinates[0]) != 2 {
		t.Errorf("tok: %s %v, a treba jedna crta od dviju točaka (srednja je na spojnici)", g.Type, g.Coordinates)
	}
	if f.Properties["naziv"] != "potok Proba" || f.Properties["adresa"] != "/watercourses/potok-proba" {
		t.Errorf("svojstva toka: %v", f.Properties)
	}
	if len(k.zaKartu.Letve) != 1 || k.zaKartu.Letve[0].Adresa == "" {
		t.Errorf("letve na karti: %+v", k.zaKartu.Letve)
	}
}

func TestPojednostaviCrtu(t *testing.T) {
	ravna := [][2]float64{{0, 0}, {1, 0.000001}, {2, 0}, {3, 0}}
	if got := pojednostaviCrtu(ravna, 0.00005); len(got) != 2 {
		t.Errorf("ravna crta: %v, a trebaju ostati krajevi", got)
	}
	cikcak := [][2]float64{{0, 0}, {1, 1}, {2, 0}, {3, 1}}
	if got := pojednostaviCrtu(cikcak, 0.00005); len(got) != 4 {
		t.Errorf("cik-cak: %v, a sve točke moraju ostati", got)
	}
	kratka := [][2]float64{{0, 0}, {1, 1}}
	if got := pojednostaviCrtu(kratka, 1); len(got) != 2 {
		t.Errorf("crta od dviju točaka: %v", got)
	}
	// zatvoreni prsten: prva i zadnja točka iste, a srednje se ne smiju izgubiti
	prsten := [][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 0}}
	if got := pojednostaviCrtu(prsten, 0.00005); len(got) != 4 {
		t.Errorf("zatvoreni prsten: %v", got)
	}
	// koordinate se zaokružuju na šest decimala, i u kratkoj crti
	if got := pojednostaviCrtu([][2]float64{{18.1234567, 45.7654321}, {18.2, 45.8}}, 0); got[0] != [2]float64{18.123457, 45.765432} {
		t.Errorf("zaokruženo: %v", got[0])
	}
}

// Što se ne učita, karta prešuti, a stranica kaže što nije stiglo.
func TestKartaGreskaIzvora(t *testing.T) {
	o, v, _ := probniIzvori()
	h := NewKartaHandler(o, v, func(context.Context) ([]models.Station, error) { return nil, errors.New("baza zaključana") }, nil, nil, nil)
	k, err := h.slozi(context.Background())
	if err == nil || !strings.Contains(err.Error(), "letve: baza zaključana") {
		t.Errorf("greška: %v", err)
	}
	if sloj(t, k.skupine, "nasipi").Ucrtano != 1 {
		t.Error("objekti se moraju prikazati i kad letve ne stignu")
	}
	w := httptest.NewRecorder()
	h.HandlePodaci(w, httptest.NewRequest("GET", "/karta/podaci.json", nil))
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "baza zaključana") {
		t.Errorf("podaci uz grešku: %d %s", w.Code, w.Body.String())
	}
}

func TestKartaPodaciJSON(t *testing.T) {
	o, v, l := probniIzvori()
	w := httptest.NewRecorder()
	NewKartaHandler(o, v, l, nil, nil, nil).HandlePodaci(w, httptest.NewRequest("GET", "/karta/podaci.json", nil))
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status %d, vrsta %q", w.Code, w.Header().Get("Content-Type"))
	}
	var d struct {
		Objekti  geoZbirka      `json:"objekti"`
		Vodotoci geoZbirka      `json:"vodotoci"`
		Letve    []letvaNaKarti `json:"letve"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Objekti.Type != "FeatureCollection" || len(d.Objekti.Features) != 3 || len(d.Vodotoci.Features) != 1 || len(d.Letve) != 1 {
		t.Errorf("podaci: %d objekata, %d vodotoka, %d letvi", len(d.Objekti.Features), len(d.Vodotoci.Features), len(d.Letve))
	}
}

// Prazan izvor daje prazne zbirke, ne null, da karta.js ne mora pitati.
func TestKartaPraznaBaza(t *testing.T) {
	prazno := NewKartaHandler(
		func(context.Context) ([]models.Structure, error) { return nil, nil },
		func(context.Context) ([]models.Watercourse, error) { return nil, nil },
		func(context.Context) ([]models.Station, error) { return nil, nil }, nil, nil, nil)
	w := httptest.NewRecorder()
	prazno.HandlePodaci(w, httptest.NewRequest("GET", "/karta/podaci.json", nil))
	if got := strings.TrimSpace(w.Body.String()); got != `{"objekti":{"type":"FeatureCollection","features":[]},"vodotoci":{"type":"FeatureCollection","features":[]},"letve":[],"nedostaje":[],"priblizno":{"type":"FeatureCollection","features":[]}}` {
		t.Errorf("prazna karta: %s", got)
	}
}

func predlozakKarte(t *testing.T) *template.Template {
	t.Helper()
	predlosci, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(predlosci, DijeloviPredloska("karta.html")...)
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

// Stranica nosi ploču slojeva s brojevima ucrtanog i prekidačima, a stavka
// Karta je u izborniku i ističe se.
func TestStranicaKarta(t *testing.T) {
	o, v, l := probniIzvori()
	h := NewKartaHandler(o, v, l, nil, func() KartaPostavke { return KartaPostavke{Plocice: "https://pločice/{z}/{x}/{y}.png", NajviseZ: 17} }, predlozakKarte(t))
	req := httptest.NewRequest("GET", "/karta", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyUser, &models.User{FullName: "Ana Proba"}))
	w := httptest.NewRecorder()
	h.ShowKarta(w, req)
	html := w.Body.String()
	for _, trazi := range []string{
		`class="karta-stranica"`, `data-podaci="/karta/podaci.json"`, `data-zupanije="/territories/zupanije.geojson"`,
		`data-sloj="nasipi"`, `1 od 2 ucrtano`, `data-sloj="letve"`, `1 od 2 s položajem`, `nema ih u registru`,
		`data-sloj="zupanije"`, `granice i imena`, `data-skupina-prekidac`, `Vrati zadano`, `js/karta.js`,
		`href="/karta" class="nav-link active"`, `<h1 class="vrh-naslov">Karta</h1>`,
		`aria-label="Što nedostaje: Nasipi" data-nedostaje="nasipi" data-naziv="Nasipi" data-crta="true" data-ukupno="2"`, `data-nedostaje="letve"`,
		`class="karta-pogled-nedostaje" hidden`, `Što nedostaje`, `Preuzmi popis`,
	} {
		if !strings.Contains(html, trazi) {
			t.Errorf("stranica nema %q", trazi)
		}
	}
	// sve ucrtano i prazan registar nemaju popis onoga što nedostaje
	for _, nema := range []string{`data-nedostaje="crpne-stanice"`, `data-nedostaje="brane"`, `data-nedostaje="zupanije"`} {
		if strings.Contains(html, nema) {
			t.Errorf("stranica ne treba %q", nema)
		}
	}
	if strings.Count(html, `class="prekidac"`) != strings.Count(html, `data-sloj="`)+3 {
		t.Error("svaki sloj i svaka skupina trebaju svoj prekidač")
	}
}

// Bez prijave izbornik nema Kartu; s prijavom je ima i obični djelatnik.
func TestKartaUIzborniku(t *testing.T) {
	gost := iscrtajSkoljku(t, DashboardViewData{ActiveNav: "registers"})
	if strings.Contains(gost, `href="/karta"`) {
		t.Error("Karta u izborniku bez prijave")
	}
	prijavljen := iscrtajSkoljku(t, DashboardViewData{ActiveNav: "registers", CurrentUser: &models.User{FullName: "Ivo Proba"}})
	if !strings.Contains(prijavljen, `href="/karta"`) {
		t.Error("prijavljeni djelatnik nema Kartu u izborniku")
	}
}

// Karta dobiva iste kontrole kao ostale karte (puni zaslon, položaj, pomak).
func TestKartaImaZajednickeKontrole(t *testing.T) {
	b, err := fs.ReadFile(webassets.Files, "static/js/karta.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	if strings.Count(js, "L.map(") != 1 || !strings.Contains(js, "dodajKontroleKarte(karta, platno") {
		t.Error("Karta mora dobiti zajedničke kontrole karte")
	}
	if strings.Contains(js, "scrollWheelZoom: false") {
		t.Error("Karta ne smije isključiti zumiranje kotačićem")
	}
}

// Kad dio podataka ne stigne, stranica se ipak otvori i kaže što nije
// stiglo, a slojevi koji jesu ostaju u ploči.
func TestStranicaKartaUzGreskuIzvora(t *testing.T) {
	o, v, _ := probniIzvori()
	h := NewKartaHandler(o, v, func(context.Context) ([]models.Station, error) { return nil, errors.New("baza zaključana") }, nil, nil, predlozakKarte(t))
	req := httptest.NewRequest("GET", "/karta", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyUser, &models.User{FullName: "Ana Proba"}))
	w := httptest.NewRecorder()
	h.ShowKarta(w, req)
	html := w.Body.String()
	for _, trazi := range []string{"Dio karte se nije učitao: letve: baza zaključana", `data-sloj="nasipi"`, `data-plocice=""`} {
		if !strings.Contains(html, trazi) {
			t.Errorf("stranica uz grešku nema %q", trazi)
		}
	}
}
