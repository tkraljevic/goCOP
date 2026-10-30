package web

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

func fptr(v float64) *float64 { return &v }

// posluziteljSlivova je poslužitelj s registrom kišomjera: međusliv B (u
// dnevnoj prognozi, s poligonom kao značajkom), međusliv Z (izvan prognoze,
// gola geometrija), točke u njima i jedna točka nepoznatog međusliva, letva
// Goričan s koordinatama i vodotok Primjerica s tokom.
func posluziteljSlivova(t *testing.T) *posluziteljOcitanja {
	t.Helper()
	p := noviPosluziteljOcitanja(t)
	p.s.SetKisomjeri(p.kisomjeri)
	p.s.SetKarta("https://plocice.primjer/{z}/{x}/{y}.png", "© Primjer", 15)
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	for _, m := range []models.Sliv{
		{Oznaka: "B", Naziv: "Mura do Goričana", Km2: fptr(10500), Napomena: "izmišljeni opis",
			Geometry: `{"type":"Feature","properties":{},"geometry":{"type":"Polygon","coordinates":[[[16,46],[16.5,46],[16.5,46.5],[16,46]]]}}`},
		{Oznaka: "Z", Naziv: "Zapadni primjer", Geometry: `{"type":"Polygon","coordinates":[[[15,45],[15.5,45],[15.5,45.5],[15,45]]]}`},
	} {
		if err := p.kisomjeri.UpsertSliv(ctx, admin, &m); err != nil {
			t.Fatal(err)
		}
	}
	for _, k := range []models.Kisomjer{
		{Naziv: "B1 ravnica", Sliv: "B", Pojas: "ravnica (<300 m)", Latitude: 46.3, Longitude: 16.4, Tezina: fptr(0.6), Aktivan: true},
		{Naziv: "B2 pobrđe", Sliv: "B", Latitude: 46.4, Longitude: 16.2, Tezina: fptr(0.3), Aktivan: true, Visina: fptr(420)},
		{Naziv: "B3 ugašena", Sliv: "B", Latitude: 46.45, Longitude: 16.1, Tezina: fptr(0.1)},
		{Naziv: "Čakovec", Sliv: "B", Vrsta: models.KisomjerStvarni, Izvor: "dhmz", IzvorSifra: "123", Latitude: 46.38, Longitude: 16.43, Aktivan: true},
		{Naziv: "Z1", Sliv: "Z", Latitude: 45.2, Longitude: 15.2, Tezina: fptr(0.5), Aktivan: true},
		{Naziv: "Lutalica", Sliv: "nepoznat", Latitude: 45.9, Longitude: 16.9, Tezina: fptr(0.2), Aktivan: true},
	} {
		if err := p.kisomjeri.CreateKisomjer(ctx, admin, &k); err != nil {
			t.Fatalf("%s: %v", k.Naziv, err)
		}
	}
	st := &models.Station{ID: uuid.New(), Code: "gorican", Name: "Goričan", Watercourse: "Mura", Latitude: fptr(46.38), Longitude: fptr(16.68)}
	if err := repository.NewStationRepository(p.baza, ledger.New(p.baza, "test")).CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	if err := p.vode.CreateWatercourse(ctx, admin, &models.Watercourse{Code: "primjerica", OfficialName: "rijeka Primjerica", Name: "Primjerica",
		Kind: "rijeka", Geometry: `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"ime":"Primjerica"},"geometry":{"type":"LineString","coordinates":[[16,46],[16.1,46.1]]}}]}`}); err != nil {
		t.Fatal(err)
	}
	return p
}

// Registar slivova vidi svaki prijavljeni, a bez prijave vodi na prijavu.
// Točke stoje po međuslivu, s brojem u prognozi, razlogom kad ne ulaze i
// kartom s točkama, međuslivovima, rijekama i letvama.
func TestStranicaSlivovaKrozPosluzitelj(t *testing.T) {
	p := posluziteljSlivova(t)
	if w := p.posalji("", http.MethodGet, "/slivovi", nil, ""); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("bez prijave: %d %q", w.Code, w.Header().Get("Location"))
	}
	w := p.posalji("dežurni", http.MethodGet, "/slivovi?success=Spremljeno", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("stranica: %d %s", w.Code, w.Body.String())
	}
	html := w.Body.String()
	for _, zeli := range []string{
		"2 međusliva", "5 izvedenih točaka", "1 pravi kišomjer", "2 u prognozi", "Spremljeno",
		`id="sliv-B"`, "Mura do Goričana", "10.500 km²", "izmišljeni opis",
		"ulazi u dnevnu prognozu za:", "mursko-sredisce, Goričan, letenye", "iz 2 izvedene točke",
		`id="sliv-Z"`, "Kiša ovog međusliva zasad ne ulazi u dnevnu prognozu.", "kiša međusliva Z ne ulazi u dnevnu prognozu",
		"Bez međusliva", "Lutalica", "točka nije aktivna", "pravi kišomjer služi za usporedbu",
		"Σ težina 0,90</span>", `class="reg-chip empty"`,
		`class="karta-slivovi"`, `data-plocice="https://plocice.primjer/{z}/{x}/{y}.png"`,
		`"code":"b-b1-ravnica"`, `"edit_url":"/slivovi/kisomjer/b-b1-ravnica/edit"`, `"vrsta":"stvarni"`, `"izvor":"dhmz"`,
		`class="karta-geometrija-podaci"`, `"LineString"`,
		`class="karta-slivovi-podaci"`, `"oznaka":"B"`, `"km2":10500`,
		`class="karta-postaje-podaci"`, `"name":"Goričan"`,
	} {
		if !strings.Contains(html, zeli) {
			t.Errorf("stranica slivova nema %q", zeli)
		}
	}
	// dežurni ne uređuje registar
	if strings.Contains(html, "Nova izvedena točka") || strings.Contains(html, "Premjesti kišomjere") {
		t.Error("dežurnom se nudi uređivanje registra")
	}
	if html := p.posalji("uprava", http.MethodGet, "/slivovi", nil, "").Body.String(); !strings.Contains(html, "Nova izvedena točka") ||
		!strings.Contains(html, "Premjesti kišomjere") {
		t.Error("upravi se ne nudi uređivanje registra")
	}
}

// Čvor bez registra kišomjera to kaže, i na stranici i u popisu.
func TestSlivoviBezRegistra(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	for _, put := range []string{"/slivovi", "/slivovi/kisomjer/new", "/api/slivovi"} {
		w := p.posalji("uprava", http.MethodGet, put, nil, "")
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "Registar kišomjera nije uključen") {
			t.Errorf("%s: %d %q", put, w.Code, w.Body.String())
		}
	}
}

// Obrazac točke otvara samo uprava: nova točka uzima međusliv i vrstu iz
// upita, postojeća svoje podatke, a nepostojeća ne postoji.
func TestObrazacKisomjera(t *testing.T) {
	p := posluziteljSlivova(t)
	if w := p.posalji("dežurni", http.MethodGet, "/slivovi/kisomjer/new", nil, ""); w.Code != http.StatusForbidden {
		t.Errorf("dežurni: %d", w.Code)
	}
	w := p.posalji("uprava", http.MethodGet, "/slivovi/kisomjer/new?sliv=B&vrsta=stvarni", nil, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Novi stvarni kišomjer") || !strings.Contains(w.Body.String(), `value="B" selected`) ||
		!strings.Contains(w.Body.String(), `action="/api/slivovi/kisomjer/create"`) {
		t.Errorf("nova točka: %d", w.Code)
	}
	if html := p.posalji("uprava", http.MethodGet, "/slivovi/kisomjer/new", nil, "").Body.String(); !strings.Contains(html, "Nova izvedena točka") {
		t.Error("bez vrste u upitu nova točka je izvedena")
	}
	w = p.posalji("uprava", http.MethodGet, "/slivovi/kisomjer/b-b2-pobrde/edit", nil, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `value="B2 pobrđe"`) || !strings.Contains(w.Body.String(), "Spremi izmjene") ||
		!strings.Contains(w.Body.String(), `action="/api/slivovi/kisomjer/update"`) {
		t.Errorf("izmjena: %d", w.Code)
	}
	if w := p.posalji("uprava", http.MethodGet, "/slivovi/kisomjer/nema-je/edit", nil, ""); w.Code != http.StatusNotFound {
		t.Errorf("nepostojeća točka: %d", w.Code)
	}
}

// Izmjena točke obrascem i JSON-om: uspjeh vodi na registar, greška na
// obrazac točke ili, kad se točka ne da ni pročitati, na registar.
func TestIzmjenaKisomjera(t *testing.T) {
	p := posluziteljSlivova(t)
	ctx := context.Background()
	put := "/api/slivovi/kisomjer/update"
	forma := url.Values{"code": {"b-b1-ravnica"}, "naziv": {"B1 ravnica"}, "sliv": {"B"}, "pojas": {"ravnica (<300 m)"},
		"lat": {"46,31"}, "lon": {"16.41"}, "visina": {"150"}, "srednja_visina": {"210,5"}, "km2": {"3200"}, "tezina": {"0,7"},
		"aktivan": {"1"}, "napomena": {"premještena"}}
	putanja, poruka := porukaPreusmjerenja(t, p.obrazac("uprava", put, forma), "success")
	if putanja != "/slivovi" || poruka != "Kišomjer je izmijenjen." {
		t.Errorf("izmjena: %s %q", putanja, poruka)
	}
	k, err := p.kisomjeri.GetKisomjer(ctx, "b-b1-ravnica")
	if err != nil || k == nil {
		t.Fatal(err)
	}
	if k.Latitude != 46.31 || k.Longitude != 16.41 || k.Tezina == nil || *k.Tezina != 0.7 || k.Visina == nil || *k.Visina != 150 ||
		k.SrednjaVisina == nil || *k.SrednjaVisina != 210.5 || k.Km2 == nil || *k.Km2 != 3200 || !k.Aktivan || k.Napomena != "premještena" {
		t.Errorf("izmijenjena točka: %+v", k)
	}

	// "aktivan=0" gasi točku
	forma.Set("aktivan", "0")
	porukaPreusmjerenja(t, p.obrazac("uprava", put, forma), "success")
	if k, err := p.kisomjeri.GetKisomjer(ctx, "b-b1-ravnica"); err != nil || k == nil || k.Aktivan {
		t.Error("točka nije ugašena")
	}

	// težina izvan 0–1: natrag na obrazac točke
	forma.Set("tezina", "2")
	putanja, poruka = porukaPreusmjerenja(t, p.obrazac("uprava", put, forma), "error")
	if putanja != "/slivovi/kisomjer/b-b1-ravnica/edit" || poruka != "težina mora biti između 0 i 1" {
		t.Errorf("težina: %s %q", putanja, poruka)
	}
	// broj koji nije broj: točka se ne da pročitati, natrag na registar
	for polje, zeli := range map[string]string{"lat": "polje lat nije broj", "lon": "polje lon nije broj", "visina": "polje visina nije broj", "tezina": "polje tezina nije broj"} {
		f := url.Values{"code": {"b-b1-ravnica"}, "naziv": {"B1"}, "lat": {"46"}, "lon": {"16"}}
		f.Set(polje, "sjever")
		putanja, poruka = porukaPreusmjerenja(t, p.obrazac("uprava", put, f), "error")
		if putanja != "/slivovi" || poruka != zeli {
			t.Errorf("%s: %s %q", polje, putanja, poruka)
		}
	}

	// JSON: odgovor je JSON, greška je 400
	w := p.posalji("uprava", http.MethodPost, put, strings.NewReader(`{"code":"b-b1-ravnica","naziv":"B1 ravnica","sliv":"B","latitude":46.32,"longitude":16.42,"aktivan":true}`), "application/json")
	var odg struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &odg); err != nil || w.Code != http.StatusOK || !odg.Success || odg.Message != "Kišomjer je izmijenjen." {
		t.Errorf("JSON izmjena: %d %s", w.Code, w.Body.String())
	}
	if w := p.posalji("uprava", http.MethodPost, put, strings.NewReader(`{"code":`), "application/json"); w.Code != http.StatusBadRequest ||
		strings.TrimSpace(w.Body.String()) != "neispravan JSON" {
		t.Errorf("neispravan JSON: %d %q", w.Code, w.Body.String())
	}
	if w := p.posalji("dežurni", http.MethodPost, put, strings.NewReader(`{"code":"b-b1-ravnica","naziv":"x","latitude":46,"longitude":16}`), "application/json"); w.Code != http.StatusBadRequest ||
		strings.TrimSpace(w.Body.String()) != "izmjenu kišomjera dopušteno je samo globalnom administratoru" {
		t.Errorf("dežurni mijenja: %d %q", w.Code, w.Body.String())
	}
	if k, err := p.kisomjeri.GetKisomjer(ctx, "b-b1-ravnica"); err != nil || k == nil || k.Naziv != "B1 ravnica" {
		t.Error("dežurni je promijenio točku")
	}
}

// Brisanje točke JSON-om, upis i brisanje međusliva, i greške koje se tu
// mogu dogoditi.
func TestBrisanjeKisomjeraIMedjusliva(t *testing.T) {
	p := posluziteljSlivova(t)
	ctx := context.Background()

	// dežurni ne briše točku
	w := p.posalji("dežurni", http.MethodPost, "/api/slivovi/kisomjer/delete", strings.NewReader(`{"code":"b-b2-pobrde"}`), "application/json")
	if w.Code != http.StatusBadRequest || strings.TrimSpace(w.Body.String()) != "brisanje kišomjera dopušteno je samo globalnom administratoru" {
		t.Errorf("dežurni briše točku: %d %q", w.Code, w.Body.String())
	}
	if k, err := p.kisomjeri.GetKisomjer(ctx, "b-b2-pobrde"); err != nil || k == nil {
		t.Error("dežurni je obrisao točku")
	}

	w = p.posalji("uprava", http.MethodPost, "/api/slivovi/kisomjer/delete", strings.NewReader(`{"code":"z-z1"}`), "application/json")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Kišomjer je obrisan.") {
		t.Errorf("JSON brisanje: %d %s", w.Code, w.Body.String())
	}
	if k, err := p.kisomjeri.GetKisomjer(ctx, "z-z1"); err != nil || k != nil {
		t.Error("točka nije obrisana")
	}

	// međusliv JSON-om, s neispravnim GeoJSON-om i neispravnim tijelom
	sliv := "/api/slivovi/sliv"
	if w := p.posalji("uprava", http.MethodPost, sliv, strings.NewReader(`{"oznaka":"Y","naziv":"Ypsilon","geometry":"{nije"}`), "application/json"); w.Code != http.StatusBadRequest ||
		strings.TrimSpace(w.Body.String()) != "Neispravan GeoJSON sliva" {
		t.Errorf("neispravan GeoJSON: %d %q", w.Code, w.Body.String())
	}
	if w := p.posalji("uprava", http.MethodPost, sliv, strings.NewReader(`[`), "application/json"); w.Code != http.StatusBadRequest ||
		strings.TrimSpace(w.Body.String()) != "Neispravan JSON" {
		t.Errorf("neispravan JSON: %d %q", w.Code, w.Body.String())
	}
	w = p.posalji("uprava", http.MethodPost, sliv, strings.NewReader(`{"oznaka":"Y","naziv":"Ypsilon","km2":12.5}`), "application/json")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Sliv je upisan.") {
		t.Errorf("JSON sliv: %d %s", w.Code, w.Body.String())
	}
	// površina koja nije broj ne upisuje se
	porukaPreusmjerenja(t, p.obrazac("uprava", sliv, url.Values{"oznaka": {"Y"}, "naziv": {"Ypsilon"}, "km2": {"puno"}}), "success")
	medjuslivY := func() *models.Sliv {
		t.Helper()
		slivovi, err := p.kisomjeri.ListSlivovi(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i := range slivovi {
			if slivovi[i].Oznaka == "Y" {
				return &slivovi[i]
			}
		}
		return nil
	}
	if y := medjuslivY(); y == nil || y.Km2 != nil {
		t.Errorf("međusliv Y: %+v", y)
	}

	// brisanje međusliva: dežurni ne smije, uprava smije
	putanja, poruka := porukaPreusmjerenja(t, p.obrazac("dežurni", "/api/slivovi/sliv/delete", url.Values{"oznaka": {"Y"}}), "error")
	if putanja != "/slivovi" || poruka != "brisanje sliva dopušteno je samo globalnom administratoru" {
		t.Errorf("dežurni briše međusliv: %s %q", putanja, poruka)
	}
	if medjuslivY() == nil {
		t.Error("dežurni je obrisao međusliv")
	}
	putanja, poruka = porukaPreusmjerenja(t, p.obrazac("uprava", "/api/slivovi/sliv/delete", url.Values{"oznaka": {"Y"}}), "success")
	if putanja != "/slivovi" || poruka != "Sliv je obrisan." {
		t.Errorf("brisanje međusliva: %s %q", putanja, poruka)
	}
	if slivovi, err := p.kisomjeri.ListSlivovi(ctx); err != nil || len(slivovi) != 2 {
		t.Errorf("nakon brisanja %d međusliva", len(slivovi))
	}
}

// Položaji s karte stižu samo kao JSON i sprema ih samo uprava; mijenjaju
// se koordinate premještenih točaka.
func TestPolozajiKisomjeraSamoJSON(t *testing.T) {
	p := posluziteljSlivova(t)
	ctx := context.Background()
	put := "/api/slivovi/kisomjer/polozaji"
	polozaj := func(code string) (float64, float64) {
		t.Helper()
		k, err := p.kisomjeri.GetKisomjer(ctx, code)
		if err != nil || k == nil {
			t.Fatalf("%s: %v", code, err)
		}
		return k.Latitude, k.Longitude
	}
	tijelo := `{"tocke":[{"code":"b-b2-pobrde","lat":46.41,"lon":16.21},{"code":"z-z1","lat":45.21,"lon":15.21}]}`
	if w := p.posalji("dežurni", http.MethodPost, put, strings.NewReader(tijelo), "application/json"); w.Code != http.StatusBadRequest ||
		strings.TrimSpace(w.Body.String()) != "premještanje kišomjera dopušteno je samo globalnom administratoru" {
		t.Errorf("dežurni premješta: %d %q", w.Code, w.Body.String())
	}
	if lat, lon := polozaj("b-b2-pobrde"); lat != 46.4 || lon != 16.2 {
		t.Errorf("dežurni je pomaknuo točku na %v, %v", lat, lon)
	}
	if w := p.obrazac("uprava", put, url.Values{"tocke": {"x"}}); w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("obrazac: %d", w.Code)
	}
	if w := p.posalji("uprava", http.MethodPost, put, strings.NewReader(`{"tocke":`), "application/json"); w.Code != http.StatusBadRequest ||
		strings.TrimSpace(w.Body.String()) != "Neispravan JSON" {
		t.Errorf("neispravan JSON: %d %q", w.Code, w.Body.String())
	}
	w := p.posalji("uprava", http.MethodPost, put, strings.NewReader(tijelo), "application/json")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Spremljeno: 2 premještene točke.") {
		t.Errorf("dvije točke: %d %s", w.Code, w.Body.String())
	}
	if lat, lon := polozaj("b-b2-pobrde"); lat != 46.41 || lon != 16.21 {
		t.Errorf("b-b2-pobrde na %v, %v", lat, lon)
	}
	if lat, lon := polozaj("z-z1"); lat != 45.21 || lon != 15.21 {
		t.Errorf("z-z1 na %v, %v", lat, lon)
	}
	if k, err := p.kisomjeri.GetKisomjer(ctx, "b-b2-pobrde"); err != nil || k == nil || k.Naziv != "B2 pobrđe" || k.Visina == nil || *k.Visina != 420 {
		t.Errorf("premještanje je promijenilo i drugo: %+v", k)
	}
}

// Popis za preuzimanje oborina: bez geometrije međusliva, osim kad se traži.
func TestPopisKisomjeraSGeometrijom(t *testing.T) {
	p := posluziteljSlivova(t)
	popis := func(upit string) (tocke int, geometrija map[string]string) {
		t.Helper()
		w := p.posalji("dežurni", http.MethodGet, "/api/slivovi"+upit, nil, "")
		var odg struct {
			Kisomjeri []models.Kisomjer `json:"kisomjeri"`
			Slivovi   []models.Sliv     `json:"slivovi"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &odg); err != nil {
			t.Fatalf("popis %q: %d %v", upit, w.Code, err)
		}
		geometrija = map[string]string{}
		for _, m := range odg.Slivovi {
			geometrija[m.Oznaka] = m.Geometry
		}
		return len(odg.Kisomjeri), geometrija
	}
	n, g := popis("")
	if n != 6 || len(g) != 2 || g["B"] != "" || g["Z"] != "" {
		t.Errorf("bez geometrije: %d točaka, %v", n, g)
	}
	_, g = popis("?geometrija=1")
	if !strings.Contains(g["B"], `"Feature"`) || !strings.Contains(g["Z"], `"Polygon"`) {
		t.Errorf("s geometrijom: %v", g)
	}
	if w := p.posalji("", http.MethodGet, "/api/slivovi", nil, ""); w.Code != http.StatusSeeOther {
		t.Errorf("bez prijave: %d", w.Code)
	}
}

// Poligoni međusliva za kartu: značajka i gola geometrija postaju značajke s
// oznakom i nazivom; neispravna geometrija i međusliv bez nje se preskaču.
func TestSlivoviJSONZaKartu(t *testing.T) {
	if got := slivoviJSON(nil); got != "" {
		t.Errorf("bez međusliva: %q", got)
	}
	if got := slivoviJSON([]models.Sliv{{Oznaka: "A", Geometry: "{nije"}, {Oznaka: "C"}}); got != "" {
		t.Errorf("bez ispravne geometrije: %q", got)
	}
	got := slivoviJSON([]models.Sliv{
		{Oznaka: "A", Naziv: "Gornja", Km2: fptr(14649), Geometry: `{"type":"Feature","geometry":{"type":"Point","coordinates":[14,46.7]}}`},
		{Oznaka: "C", Naziv: "Donja", Geometry: `{"type":"Point","coordinates":[15,46]}`},
	})
	var fc struct {
		Type     string `json:"type"`
		Features []struct {
			Properties map[string]any `json:"properties"`
			Geometry   struct {
				Type string `json:"type"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal([]byte(got), &fc); err != nil || fc.Type != "FeatureCollection" || len(fc.Features) != 2 {
		t.Fatalf("zbirka: %v %s", err, got)
	}
	if f := fc.Features[0]; f.Geometry.Type != "Point" || f.Properties["oznaka"] != "A" || f.Properties["km2"] != 14649.0 {
		t.Errorf("značajka A: %+v", f)
	}
	if f := fc.Features[1]; f.Geometry.Type != "Point" || f.Properties["naziv"] != "Donja" || f.Properties["km2"] != nil {
		t.Errorf("značajka C: %+v", f)
	}
}

// Rijeke i letve na karti: bez registra ili bez ičega ucrtanog nema podataka.
func TestRijekeILetveZaKartu(t *testing.T) {
	h := NewSlivoviHandler(nil, nil, nil, func(string) *template.Template { return nil }, nil)
	r, _ := http.NewRequest(http.MethodGet, "/slivovi", nil) // probni podaci su ispravni; kvar bi pokazala provjera niže
	if h.rijekeJSON(r) != "" || h.letveJSON(r) != "" || len(h.imenaLetvi(r)) != 0 {
		t.Error("bez registara nešto je ucrtano")
	}
	p := noviPosluziteljOcitanja(t) // letva Primjerovo nema koordinate, vodotoka nema
	h = NewSlivoviHandler(nil, p.vode, p.stations, nil, nil)
	if h.rijekeJSON(r) != "" || h.letveJSON(r) != "" {
		t.Error("bez geometrije i koordinata nešto je ucrtano")
	}
	if imena := h.imenaLetvi(r); imena["primjerovo"] != "Primjerovo" {
		t.Errorf("imena letvi: %v", imena)
	}
	if vrstaZaKartu(models.Kisomjer{}) != models.KisomjerIzvedeni || vrstaZaKartu(models.Kisomjer{Vrsta: models.KisomjerStvarni}) != models.KisomjerStvarni {
		t.Error("vrsta točke za kartu")
	}
}
