package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// uredjivanjeZaProbu: prijavljeni smije objekt s imenom „Nasip Proba” i
// letvu „Letva Proba”, a položaj letve se pamti
type uredjivanjeZaProbu struct {
	objekti  []models.Structure
	vodotoci []models.Watercourse
	polozaj  struct {
		id       uuid.UUID
		lat, lon float64
	}
	greska error
}

func (p *uredjivanjeZaProbu) ured() KartaUredjivanje {
	return KartaUredjivanje{
		SmijeObjekt: func(_ *models.UserPermissions, s *models.Structure) bool { return s.Name == "Nasip Proba" },
		SmijeLetvu:  func(_ *models.UserPermissions, l *models.Station) bool { return l.Name == "Letva Proba" },
		Objekt: func(_ context.Context, id uuid.UUID) (*models.Structure, error) {
			for i := range p.objekti {
				if p.objekti[i].ID == id {
					return &p.objekti[i], nil
				}
			}
			return nil, nil
		},
		Tok: func(_ context.Context, kod string) (*models.Watercourse, error) {
			for i := range p.vodotoci {
				if p.vodotoci[i].Code == kod {
					return &p.vodotoci[i], nil
				}
			}
			return nil, nil
		},
		PolozajLetve: func(_ context.Context, _ *models.UserPermissions, id uuid.UUID, lat, lon float64) error {
			p.polozaj.id, p.polozaj.lat, p.polozaj.lon = id, lat, lon
			return p.greska
		},
	}
}

func kartaSUredjivanjem(t *testing.T, dionice []models.Section) (*KartaHandler, *uredjivanjeZaProbu) {
	t.Helper()
	o, v, l := probniIzvori()
	objekti, _ := o(context.Background())  // probni izvor ne vraća grešku
	vodotoci, _ := v(context.Background()) // probni izvor ne vraća grešku
	p := &uredjivanjeZaProbu{objekti: objekti, vodotoci: vodotoci}
	h := NewKartaHandler(o, v, l, func(context.Context) ([]models.Section, error) { return dionice, nil }, nil, predlozakKarte(t))
	h.SetUredjivanje(p.ured())
	return h, p
}

func sPravima(r *http.Request, perms *models.UserPermissions) *http.Request {
	ctx := context.WithValue(context.WithValue(r.Context(), contextKeyPerms, perms), contextKeyUser, &models.User{FullName: "Ana Proba"})
	return r.WithContext(ctx)
}

// Karta uz svaki objekt, tok, letvu i redak popisa kaže smije li ga
// prijavljeni ucrtati; brojevi slojeva idu uz podatke.
func TestKartaKazeStoSeSmijeUcrtati(t *testing.T) {
	h, _ := kartaSUredjivanjem(t, nil)
	k, err := h.slozi(sPravima(httptest.NewRequest("GET", "/", nil), &models.UserPermissions{}).Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range k.zaKartu.Objekti.Features {
		if f.Properties["id"] == "" {
			t.Errorf("značajka bez id-a: %v", f.Properties)
		}
		if smije := f.Properties["uredjuje"] == true; smije != (f.Properties["naziv"] == "Nasip Proba") {
			t.Errorf("%v: uredjuje %v", f.Properties["naziv"], smije)
		}
	}
	if len(k.zaKartu.Vodotoci.Features) != 1 || k.zaKartu.Vodotoci.Features[0].Properties["uredjuje"] != nil {
		t.Errorf("tok smije mijenjati samo globalni administrator: %v", k.zaKartu.Vodotoci.Features[0].Properties)
	}
	if !k.zaKartu.Letve[0].Uredjuje {
		t.Error("letva Proba se smije pomaknuti")
	}
	// smije samo Nasip Proba i Letva Proba, a oni su već ucrtani
	for _, r := range k.zaKartu.Nedostaje {
		if r.Uredjuje {
			t.Errorf("%s: smije se ucrtati, a ne bi smio", r.Naziv)
		}
		if r.ID == "" && r.Kod == "" {
			t.Errorf("%s: redak bez id-a i šifre", r.Naziv)
		}
	}
	if !k.uredjuje {
		t.Error("prijavljeni smije barem nasip, pa Karta treba alat")
	}
	// ucrtani su u svom popisu, odvojeno od onoga što nedostaje
	ucrtani := map[string]bool{}
	for _, r := range k.zaKartu.Ucrtano {
		ucrtani[r.Naziv] = r.Uredjuje
	}
	if len(ucrtani) != 5 || !ucrtani["Nasip Proba"] || !ucrtani["Letva Proba"] || ucrtani["Ustava Proba"] {
		t.Errorf("ucrtano: %v", ucrtani)
	}
	if _, ima := ucrtani["potok Proba"]; !ima {
		t.Error("ucrtani tok mora biti u popisu ucrtanog")
	}
	if got := k.zaKartu.Brojevi["nasipi"]; got != [2]int{1, 2} {
		t.Errorf("broj nasipa: %v", got)
	}

	admin, err := h.slozi(sPravima(httptest.NewRequest("GET", "/", nil), &models.UserPermissions{IsGlobalAdmin: true}).Context())
	if err != nil {
		t.Fatal(err)
	}
	if admin.zaKartu.Vodotoci.Features[0].Properties["uredjuje"] != true || redak(t, admin.zaKartu.Nedostaje, "Bez toka").Uredjuje != true {
		t.Error("globalni administrator smije ucrtati tok")
	}
}

// Bez prava na išta stranica nema alat za crtanje ni ploču ucrtavanja.
func TestKartaAlatSamoSPravom(t *testing.T) {
	stranica := func(h *KartaHandler, perms *models.UserPermissions) string {
		w := httptest.NewRecorder()
		h.ShowKarta(w, sPravima(httptest.NewRequest("GET", "/karta", nil), perms))
		return w.Body.String()
	}
	alat := []string{"leaflet-geoman.min.js", "js/karta-uredjivanje.js", "js/datoteka-karte.js", `class="karta-pogled-ucrtavanje"`, "Generiraj stacionažu"}

	h, _ := kartaSUredjivanjem(t, nil)
	imaSve(t, stranica(h, &models.UserPermissions{}), "smije nasip", alat)

	o, v, l := probniIzvori()
	samoPrikaz := NewKartaHandler(o, v, l, nil, nil, predlozakKarte(t))
	html := stranica(samoPrikaz, &models.UserPermissions{})
	nemaNista(t, html, "bez prava", alat)
	imaSve(t, html, "bez prava", []string{"js/karta.js"})
}

// Objekt za uređivanje dolazi cijel, sa stranicom i službenom duljinom iz
// dionica; nepoznat ili pogrešan id je 404.
func TestKartaObjektZaUredjivanje(t *testing.T) {
	duljina := 1.2
	od, do := 18.142, 19.342
	h, p := kartaSUredjivanjem(t, nil)
	nasip := p.objekti[0]
	h.dionice = func(context.Context) ([]models.Section, error) {
		return []models.Section{
			{Code: "B.16.6", Parts: []models.SectionPart{{Embankments: []models.PartEmbankment{{StructureID: nasip.ID.String(), EmbFrom: &od, EmbTo: &do}}}}},
			{Code: "B.16.7", Parts: []models.SectionPart{{Embankments: []models.PartEmbankment{{StructureID: nasip.ID.String(), LengthKm: &duljina}, {StructureID: uuid.NewString(), LengthKm: &duljina}}}}},
		}, nil
	}
	dohvati := func(id string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/karta/objekt/"+id, nil)
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		h.HandleObjekt(w, r)
		return w
	}
	w := dohvati(nasip.ID.String())
	if w.Code != 200 {
		t.Fatalf("objekt: %d %s", w.Code, w.Body.String())
	}
	var o struct {
		ID, Naziv, Vrsta, Adresa, GeoJSON, Gdje string
		DuljinaKm                               float64  `json:"duljina_km"`
		OdKm                                    *float64 `json:"od_km"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatal(err)
	}
	if o.Adresa != nasip.Path() || o.Vrsta != models.StructureKindEmbankment || !strings.Contains(o.GeoJSON, `"km":"0+000"`) {
		t.Errorf("objekt: %+v", o)
	}
	if o.DuljinaKm != 2.4 || o.OdKm == nil || *o.OdKm != 18.142 || o.Gdje != "B.16.6 · km 18+142 – 19+342; B.16.7" {
		t.Errorf("stacionaža: %v km od %v, %q", o.DuljinaKm, o.OdKm, o.Gdje)
	}
	if !strings.Contains(w.Body.String(), `"odsjeci":[{"dionica":"B.16.6","od":18.142,"do":19.342}]`) {
		t.Errorf("odsjeci: %s", w.Body.String())
	}
	for _, id := range []string{"nije-id", uuid.NewString()} {
		if w := dohvati(id); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d", id, w.Code)
		}
	}
}

// Odsjeci idu od najmanjeg km, i kad su dionice upisane drugim redom ili
// odsjek ide od većeg km prema manjem.
func TestOdsjeciNasipaRedom(t *testing.T) {
	km := func(v float64) *float64 { return &v }
	id := uuid.NewString()
	st := stacionazaObjekta([]models.Section{
		{Code: "B.34.3", Parts: []models.SectionPart{{Embankments: []models.PartEmbankment{{StructureID: id, EmbFrom: km(31.17), EmbTo: km(18)}}}}},
		{Code: "B.34.2", Parts: []models.SectionPart{{Embankments: []models.PartEmbankment{{StructureID: id, EmbFrom: km(0), EmbTo: km(18)}}}}},
	}, id)
	if len(st.Odsjeci) != 2 || st.Odsjeci[0] != (odsjekNasipa{"B.34.2", 0, 18}) || st.Odsjeci[1] != (odsjekNasipa{"B.34.3", 18, 31.17}) {
		t.Errorf("odsjeci: %+v", st.Odsjeci)
	}
	if st.DuljinaKm != 31.17 || *st.OdKm != 0 {
		t.Errorf("duljina %v od %v", st.DuljinaKm, *st.OdKm)
	}
}

func TestDuljinaOdsjeka(t *testing.T) {
	a, b, c := 2.0, 5.5, 1.25
	for _, s := range []struct {
		e    models.PartEmbankment
		htio float64
	}{
		{models.PartEmbankment{LengthKm: &c, EmbFrom: &a, EmbTo: &b}, 1.25},
		{models.PartEmbankment{EmbFrom: &b, EmbTo: &a, WaterFrom: &a, WaterTo: &a}, 3.5},
		{models.PartEmbankment{WaterFrom: &b, WaterTo: &a}, 3.5},
		{models.PartEmbankment{}, 0},
	} {
		if got := duljinaOdsjeka(s.e); got != s.htio {
			t.Errorf("%+v: %v, htio %v", s.e, got, s.htio)
		}
	}
}

// Tok za uređivanje dolazi cijel; Dunav i Dravu Karta ne stacionira, jer im
// stacionažu računa ENC.
func TestKartaTokZaUredjivanje(t *testing.T) {
	h, p := kartaSUredjivanjem(t, nil)
	p.vodotoci = append(p.vodotoci, models.Watercourse{Code: "rijeka-dunav", Name: "Dunav", Geometry: `{"type":"FeatureCollection","features":[]}`})
	h.SetUredjivanje(p.ured())
	tok := func(kod string) (int, map[string]any) {
		r := httptest.NewRequest("GET", "/karta/vodotok/"+kod, nil)
		r.SetPathValue("kod", kod)
		w := httptest.NewRecorder()
		h.HandleTok(w, r)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out) // 404 nije JSON
		return w.Code, out
	}
	if kod, d := tok("potok-proba"); kod != 200 || d["naziv"] != "potok Proba" || d["adresa"] != "/watercourses/potok-proba" ||
		!strings.Contains(d["geojson"].(string), `"tip":"rkm"`) || d["stacionaza_enc"] != false {
		t.Errorf("tok: %d %v", kod, d)
	}
	if _, d := tok("rijeka-dunav"); d["stacionaza_enc"] != true {
		t.Errorf("Dunav: %v", d)
	}
	if kod, _ := tok("nema-ga"); kod != http.StatusNotFound {
		t.Errorf("nepoznat tok: %d", kod)
	}
	u := p.ured()
	u.Tok = func(context.Context, string) (*models.Watercourse, error) { return nil, errors.New("baza zaključana") }
	h.SetUredjivanje(u)
	if kod, _ := tok("potok-proba"); kod != http.StatusInternalServerError {
		t.Errorf("greška baze: %d", kod)
	}
	h.SetUredjivanje(KartaUredjivanje{})
	if kod, _ := tok("potok-proba"); kod != http.StatusNotFound {
		t.Errorf("bez uređivanja: %d", kod)
	}
}

// Položaj letve dolazi kao JSON, provjeren i zaokružen na šest decimala.
func TestKartaPolozajLetve(t *testing.T) {
	h, p := kartaSUredjivanjem(t, nil)
	id := uuid.New()
	posalji := func(vrsta, tijelo string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/karta/letva/"+id.String()+"/polozaj", strings.NewReader(tijelo))
		r.SetPathValue("id", id.String())
		r.Header.Set("Content-Type", vrsta)
		w := httptest.NewRecorder()
		h.HandlePolozajLetve(w, sPravima(r, &models.UserPermissions{}))
		return w
	}
	if w := posalji("application/json", `{"lat":45.12345678,"lon":18.87654321}`); w.Code != 200 || !strings.Contains(w.Body.String(), "Položaj letve je spremljen.") {
		t.Fatalf("spremanje: %d %s", w.Code, w.Body.String())
	}
	if p.polozaj.id != id || p.polozaj.lat != 45.123457 || p.polozaj.lon != 18.876543 {
		t.Errorf("upisano: %+v", p.polozaj)
	}
	for _, s := range []struct {
		vrsta, tijelo string
		kod           int
	}{
		{"application/x-www-form-urlencoded", `lat=45&lon=18`, http.StatusUnsupportedMediaType},
		{"application/json", `{"lat":`, http.StatusBadRequest},
		{"application/json", `{"lat":0,"lon":0}`, http.StatusBadRequest},
		{"application/json", `{"lat":95,"lon":18}`, http.StatusBadRequest},
	} {
		if w := posalji(s.vrsta, s.tijelo); w.Code != s.kod {
			t.Errorf("%s %s: %d, htio %d", s.vrsta, s.tijelo, w.Code, s.kod)
		}
	}
	p.greska = errors.New("nemate pravo mijenjati ovu letvu")
	h.SetUredjivanje(p.ured())
	if w := posalji("application/json", `{"lat":45.5,"lon":18.5}`); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "nemate pravo") {
		t.Errorf("bez prava: %d %s", w.Code, w.Body.String())
	}
}

func TestPolozajNaKarti(t *testing.T) {
	for _, s := range []struct {
		lat, lon float64
		htio     bool
	}{{45.5, 18.6, true}, {0, 0, false}, {0, 18, true}, {-91, 0, false}, {45, 181, false}} {
		if got := polozajNaKarti(s.lat, s.lon); got != s.htio {
			t.Errorf("%v, %v: %v", s.lat, s.lon, got)
		}
	}
}
