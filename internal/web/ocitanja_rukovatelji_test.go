package web

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// posaljiObrazac šalje obrazac rukovatelju očitanja u ime osobe
func posaljiObrazac(h http.HandlerFunc, u *models.User, perms *models.UserPermissions, putanja string, forma url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, putanja, strings.NewReader(forma.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c := context.WithValue(r.Context(), contextKeyUser, u)
	c = context.WithValue(c, contextKeyPerms, perms)
	w := httptest.NewRecorder()
	h(w, r.WithContext(c))
	return w
}

// vodostajOcitanja čita očitanje iz baze; nil kad ga nema
func (o *okolinaPrava) vodostajOcitanja(t *testing.T, id uuid.UUID) *int {
	t.Helper()
	rd, err := o.ocitanja.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if rd == nil {
		return nil
	}
	return rd.LevelCm
}

// preusmjerenje vraća putanju i poruku iz odgovora koji preusmjerava
func preusmjerenje(t *testing.T, w *httptest.ResponseRecorder) (putanja string, upit url.Values) {
	t.Helper()
	if w.Code != http.StatusSeeOther {
		t.Fatalf("očekivano preusmjerenje, dobiveno %d %q", w.Code, w.Body.String())
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Path, u.Query()
}

// Izmjena očitanja (Odluke (8. 10. 2026.), pitanje 9): uprava sektora
// ispravlja i tuđe očitanje letve, operaterka tuđe ne dira — odbijena
// izmjena vraća na obrazac s greškom, a vrijednost ostaje kakva je bila.
func TestIzmjenaOcitanjaPoPravu(t *testing.T) {
	o := novaOkolinaPrava(t)
	forma := func(rd models.Reading, cm int) url.Values {
		return url.Values{"id": {rd.ID.String()}, "level_cm": {strconv.Itoa(cm)},
			"measured_at": {rd.LocalTime().Format("2006-01-02T15:04")}}
	}

	w := posaljiObrazac(o.h.HandleUpdate, o.ana, o.anaOvl, "/readings/update", forma(o.perino, 150))
	putanja, upit := preusmjerenje(t, w)
	if putanja != "/readings/edit/"+o.perino.ID.String() || !strings.Contains(upit.Get("error"), "nemate pravo mijenjati") {
		t.Errorf("operaterka mijenja tuđe: %s %v", putanja, upit)
	}
	if cm := o.vodostajOcitanja(t, o.perino.ID); cm == nil || *cm != 102 {
		t.Errorf("odbijena izmjena promijenila je vodostaj: %v", cm)
	}

	w = posaljiObrazac(o.h.HandleUpdate, o.pero, o.peroOvl, "/readings/update", forma(o.anino, 155))
	putanja, upit = preusmjerenje(t, w)
	if putanja != "/readings/station/"+o.st.ID.String() || upit.Get("success") != "Očitanje je izmijenjeno." {
		t.Errorf("uprava sektora ispravlja: %s %v", putanja, upit)
	}
	if cm := o.vodostajOcitanja(t, o.anino.ID); cm == nil || *cm != 155 {
		t.Errorf("izmjena nije upisana: %v", cm)
	}
}

// Obrazac izmjene bez ispravnog identifikatora ne dira ništa: nečitljiv
// identifikator vraća na obrazac, a bez njega je zahtjev loš.
func TestIzmjenaOcitanjaBezIdentifikatora(t *testing.T) {
	o := novaOkolinaPrava(t)
	w := posaljiObrazac(o.h.HandleUpdate, o.pero, o.peroOvl, "/readings/update", url.Values{"id": {"nije-uuid"}, "level_cm": {"150"}})
	putanja, upit := preusmjerenje(t, w)
	if putanja != "/readings/edit/nije-uuid" || upit.Get("error") != "neispravan identifikator očitanja" {
		t.Errorf("nečitljiv identifikator: %s %v", putanja, upit)
	}
	w = posaljiObrazac(o.h.HandleUpdate, o.pero, o.peroOvl, "/readings/update", url.Values{"level_cm": {"150"}})
	if w.Code != http.StatusBadRequest {
		t.Errorf("bez identifikatora: %d", w.Code)
	}
	for _, rd := range []models.Reading{o.anino, o.perino} {
		if cm := o.vodostajOcitanja(t, rd.ID); cm == nil || *cm != *rd.LevelCm {
			t.Errorf("neispravan obrazac promijenio je očitanje %s: %v", rd.ID, cm)
		}
	}
}

// Brisanje očitanja (Odluke (8. 10. 2026.), pitanje 9): briše samo tko
// odgovara za letvu, pa operaterka ne briše ni svoje; uprava sektora briše i
// vraća na stranicu letve.
func TestBrisanjeOcitanjaPoPravu(t *testing.T) {
	o := novaOkolinaPrava(t)
	w := posaljiObrazac(o.h.HandleDelete, o.ana, o.anaOvl, "/readings/delete", url.Values{"id": {o.anino.ID.String()}})
	putanja, upit := preusmjerenje(t, w)
	if putanja != "/readings" || !strings.Contains(upit.Get("error"), "nemate pravo brisati") {
		t.Errorf("operaterka briše: %s %v", putanja, upit)
	}
	if o.vodostajOcitanja(t, o.anino.ID) == nil {
		t.Error("odbijeno brisanje obrisalo je očitanje")
	}

	w = posaljiObrazac(o.h.HandleDelete, o.pero, o.peroOvl, "/readings/delete", url.Values{"id": {o.anino.ID.String()}})
	putanja, upit = preusmjerenje(t, w)
	if putanja != "/readings/station/"+o.st.ID.String() || !strings.Contains(upit.Get("success"), "Očitanje je obrisano") {
		t.Errorf("uprava sektora briše: %s %v", putanja, upit)
	}
	if o.vodostajOcitanja(t, o.anino.ID) != nil {
		t.Error("očitanje je ostalo nakon brisanja")
	}

	w = posaljiObrazac(o.h.HandleDelete, o.pero, o.peroOvl, "/readings/delete", url.Values{"id": {"nije-uuid"}})
	if w.Code != http.StatusBadRequest {
		t.Errorf("nečitljiv identifikator: %d", w.Code)
	}
	if o.vodostajOcitanja(t, o.perino.ID) == nil {
		t.Error("loš zahtjev obrisao je očitanje")
	}
}

// Upis koji servis odbije vraća na obrazac nove izmjere iste letve, s
// greškom i povratkom; ništa se ne upisuje.
func TestNovoOcitanjeOdbijenoVracaNaObrazac(t *testing.T) {
	o := novaOkolinaPrava(t)
	w := posaljiObrazac(o.h.HandleCreate, o.ana, o.anaOvl, "/readings/create", url.Values{
		"station_id": {o.st.ID.String()}, "measured_at": {o.anino.LocalTime().Format("2006-01-02T15:04")},
		"back": {"/readings?sector=P"}})
	putanja, upit := preusmjerenje(t, w)
	if putanja != "/readings/new" || upit.Get("station") != o.st.ID.String() || upit.Get("back") != "/readings?sector=P" ||
		!strings.Contains(upit.Get("error"), "upišite vodostaj") {
		t.Errorf("odbijen upis: %s %v", putanja, upit)
	}
	if ukupno, _, _, err := o.ocitanja.Stats(context.Background()); err != nil || ukupno != 2 {
		t.Errorf("odbijen upis ostavio je trag: %d očitanja, %v", ukupno, err)
	}
}

// Stranica letve koje nema je 404, a ne prazna stranica.
func TestStranicaNepostojeceLetve(t *testing.T) {
	o := novaOkolinaPrava(t)
	w := o.zovi(o.pero, o.peroOvl, httptest.NewRequest(http.MethodGet, "/readings/station/"+uuid.NewString(), nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("nepostojeća letva: %d", w.Code)
	}
}

// Čuvanje cijele povijesti letve na čvoru odlučuje samo administrator;
// uključenje i isključenje zapisuju se i javljaju poslužitelju, povratak
// ide samo na stranicu ovog programa.
func TestPracenjeLetveNaCvoru(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "pracenje.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewFollowRepository(baza)
	javljeno := 0
	h := &ReadingsHandler{}
	h.SetFollow(repo, func() { javljeno++ })
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	pracene := func() map[string]bool {
		t.Helper()
		k, err := repo.Keys(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	kljuc := "station:" + uuid.NewString()

	for _, perms := range []*models.UserPermissions{nil, {AllowedSections: map[string]bool{"P": true}}} {
		w := posaljiObrazac(h.HandleFollow, nil, perms, "/readings/follow", url.Values{"gauge_key": {kljuc}, "follow": {"1"}})
		if w.Code != http.StatusForbidden {
			t.Errorf("praćenje bez administratora: %d", w.Code)
		}
	}
	if len(pracene()) != 0 || javljeno != 0 {
		t.Fatal("odbijeno praćenje je zapisano")
	}

	w := posaljiObrazac(h.HandleFollow, nil, admin, "/readings/follow",
		url.Values{"gauge_key": {" " + kljuc + " "}, "name": {"Primjerovo"}, "follow": {"1"}, "back": {"/readings/station/x"}})
	putanja, upit := preusmjerenje(t, w)
	if putanja != "/readings/station/x" || !strings.Contains(upit.Get("success"), "čuva cijelu povijest letve Primjerovo") ||
		!pracene()[kljuc] || javljeno != 1 {
		t.Errorf("uključenje: %s %v, praćene %v, javljeno %d", putanja, upit, pracene(), javljeno)
	}

	w = posaljiObrazac(h.HandleFollow, nil, admin, "/readings/follow",
		url.Values{"gauge_key": {kljuc}, "name": {"Primjerovo"}, "back": {"https://primjer.test/zlo"}})
	putanja, upit = preusmjerenje(t, w)
	if putanja != "/readings" || !strings.Contains(upit.Get("success"), "više ne dovlači") || pracene()[kljuc] || javljeno != 2 {
		t.Errorf("isključenje: %s %v, praćene %v, javljeno %d", putanja, upit, pracene(), javljeno)
	}

	bezPracenja := &ReadingsHandler{}
	if w := posaljiObrazac(bezPracenja.HandleFollow, nil, admin, "/readings/follow", url.Values{"follow": {"1"}}); w.Code != http.StatusInternalServerError {
		t.Errorf("čvor bez praćenja: %d", w.Code)
	}

	// Kad se odluka ne da zapisati, javlja se greška i poslužitelj se ne budi.
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	w = posaljiObrazac(h.HandleFollow, nil, admin, "/readings/follow", url.Values{"gauge_key": {kljuc}, "follow": {"1"}})
	if _, upit := preusmjerenje(t, w); upit.Get("error") == "" || javljeno != 2 {
		t.Errorf("neuspjelo zapisivanje: %v, javljeno %d", upit, javljeno)
	}
}

// Filtar pregleda: objekt pripada jednom sektoru i jednom području, letva
// postaje svima kroz čije dionice prolazi.
func TestLetvaUSektoruIPodrucju(t *testing.T) {
	objekt := models.GaugeSummary{StructureID: "o1", SectorID: "P", AreaID: 1, SectorIDs: []string{"R"}, AreaIDs: []int{2}}
	postaja := models.GaugeSummary{StationID: "s1", SectorIDs: []string{"P", "R"}, AreaIDs: []int{1, 2}}
	for _, s := range []struct {
		opis    string
		g       models.GaugeSummary
		sektor  string
		podr    int
		uSektor bool
		uPodr   bool
	}{
		{"objekt u svom sektoru", objekt, "P", 1, true, true},
		{"objekt ne gleda popis dionica", objekt, "R", 2, false, false},
		{"postaja na dvije dionice", postaja, "R", 2, true, true},
		{"postaja izvan sektora", postaja, "S", 3, false, false},
	} {
		if gaugeInSector(s.g, s.sektor) != s.uSektor || gaugeInArea(s.g, s.podr) != s.uPodr {
			t.Errorf("%s: sektor %v, područje %v", s.opis, gaugeInSector(s.g, s.sektor), gaugeInArea(s.g, s.podr))
		}
	}
}

// Naslov stranice letve: objekt nosi vrstu i područje, postaja vodotok i
// stacionažu bez viška točaka, a bez ičega naslova nema.
func TestNaslovLetve(t *testing.T) {
	for _, s := range []struct {
		opis     string
		st       *models.Station
		obj      *models.Structure
		ime, pod string
	}{
		{"objekt", nil, &models.Structure{Name: "CS Primjerovo", AreaID: 1}, "CS Primjerovo", " · BP 1"},
		{"postaja", &models.Station{Name: "Primjerovo", Watercourse: "Primjerica", Stationing: "rkm 12+300"}, nil, "Primjerovo", "Primjerica · rkm 12+300"},
		{"postaja bez stacionaže", &models.Station{Name: "Primjerovo", Watercourse: "Primjerica"}, nil, "Primjerovo", "Primjerica"},
		{"ništa", nil, nil, "", ""},
	} {
		ime, pod := gaugeNames(s.st, s.obj)
		if ime != s.ime || !strings.HasSuffix(pod, s.pod) || (s.obj == nil && pod != s.pod) {
			t.Errorf("%s: %q / %q", s.opis, ime, pod)
		}
	}
}

// Bilješka svjedoči, ne mijenja: uz vrijednost dolazi tekst, tko ju je
// ostavio i vrsta, a granična ili procijenjena vrijednost nije pouzdana.
func TestBiljeskeUzVrijednosti(t *testing.T) {
	kad := time.Date(2026, 3, 1, 6, 0, 0, 0, time.UTC)
	vals := []models.SpojenaVrijednost{{Kad: kad, Vrijednost: 120}, {Kad: kad.Add(time.Hour), Vrijednost: 125, Ispravljeno: true, Izvorno: 124}}
	primijeniBiljeske(vals, nil)
	if vals[0].Biljeska != "" || vals[1].Biljeska != "" {
		t.Fatal("bez bilježaka nešto je dopisano")
	}
	primijeniBiljeske(vals, map[int64]models.ArhivaBiljeska{
		kad.Add(time.Hour).Unix(): {Tekst: "voda preko letve", Tko: "Ivo Ivić", Vrsta: models.BiljeskaIznad},
	})
	if vals[0].Biljeska != "" {
		t.Error("bilješka je pala na vrijednost bez bilješke")
	}
	b := vals[1]
	if b.Biljeska != "voda preko letve" || b.BiljeskaTko != "Ivo Ivić" || b.BiljeskaVrsta != models.BiljeskaIznad || b.BiljeskaPouzdana {
		t.Errorf("bilješka: %+v", b)
	}
	if b.Vrijednost != 125 || !b.Ispravljeno || b.Izvorno != 124 {
		t.Errorf("bilješka je dirala vrijednost ili ispravak: %+v", b)
	}
	primijeniBiljeske(vals, map[int64]models.ArhivaBiljeska{kad.Unix(): {Tekst: "očitan maksimum", Vrsta: models.BiljeskaVrh}})
	if !vals[0].BiljeskaPouzdana {
		t.Error("vrh je pouzdana vrijednost")
	}
}

// Korak osi grafa je 1, 2 ili 5 puta potencija deset; nevaljao raspon daje
// korak 1, a ono što nije broj ostaje nebroj.
func TestKorakOsiGrafa(t *testing.T) {
	for raw, zeli := range map[float64]float64{0: 1, -3: 1, 0.7: 1, 1: 1, 1.5: 2, 3: 5, 7: 10, 12: 20, 45: 50, 230: 500} {
		if got := niceStep(raw); math.Abs(got-zeli) > 1e-9 {
			t.Errorf("korak za %g: %g, očekivano %g", raw, got, zeli)
		}
	}
	if !math.IsNaN(niceStep(math.NaN())) {
		t.Error("nebroj mora ostati nebroj")
	}
}

// Prazno polje broja je „nije upisano”, decimalni zarez vrijedi, a tekst je
// greška koja kaže što je upisano.
func TestCijeliBrojIzObrasca(t *testing.T) {
	if v, err := parseOptionalInt("  "); v != nil || err != nil {
		t.Errorf("prazno: %v %v", v, err)
	}
	if v, err := parseOptionalInt("123,6"); err != nil || v == nil || *v != 124 {
		t.Errorf("decimalni zarez: %v %v", v, err)
	}
	if _, err := parseOptionalInt(" sto "); err == nil || !strings.Contains(err.Error(), "„sto“ nije broj") {
		t.Errorf("tekst umjesto broja: %v", err)
	}
}

// Protok iz krivulje: bez vodostaja ili krivulje za taj dan nema ga, unutar
// raspona je procjena, malo izvan raspona produljenje s upozorenjem, a daleko
// izvan raspona ništa.
func TestProtokIzKrivuljeLetve(t *testing.T) {
	krivulje := []models.HQKrivulja{{VrijediOd: "2026-01-01", Odsjecci: []models.HQOdsjecak{
		{OdCm: 100, DoCm: 300, Oblik: models.OblikPolinom, P1: 0, P2: 100, P3: 0},
	}}}
	kad := time.Date(2026, 3, 1, 6, 0, 0, 0, models.Zagreb)
	cm := func(v int) *int { return &v }
	for _, s := range []struct {
		opis  string
		kad   time.Time
		cm    *int
		q     string
		izvan bool
	}{
		{"bez vodostaja", kad, nil, "", false},
		{"prije krivulje", time.Date(2025, 12, 31, 6, 0, 0, 0, models.Zagreb), cm(200), "", false},
		{"unutar raspona", kad, cm(200), "200", false},
		{"malo iznad raspona", kad, cm(300 + models.ProsirenjeKrivuljeCm), strconv.Itoa(300 + models.ProsirenjeKrivuljeCm), true},
		{"daleko iznad raspona", kad, cm(300 + 10*models.ProsirenjeKrivuljeCm), "", false},
	} {
		q, izvan := protokIzKrivulje(krivulje, s.kad, s.cm)
		if q != s.q || izvan != s.izvan {
			t.Errorf("%s: %q izvan=%v, očekivano %q izvan=%v", s.opis, q, izvan, s.q, s.izvan)
		}
	}
}

// Bez čitača prognoza, bez letve ili bez šifre letve crta prognoze ne
// postoji; čitač bez baze također ne daje ništa.
func TestPrognozaZaLetvu(t *testing.T) {
	st := &models.Station{Code: "primjerovo"}
	h := &ReadingsHandler{}
	if h.prognozaZa(st, "vodostaj") != nil {
		t.Error("bez čitača ima prognoze")
	}
	h.SetPrognoze(func() *CitacPrognoza { return nil })
	if h.prognozaZa(st, "vodostaj") != nil {
		t.Error("prazan čitač dao je prognozu")
	}
	h.SetPrognoze(func() *CitacPrognoza { return &CitacPrognoza{} })
	for _, s := range []*models.Station{nil, {}, st} {
		if h.prognozaZa(s, "vodostaj") != nil {
			t.Errorf("čitač bez baze dao je prognozu za %+v", s)
		}
	}
	if h.sektorZaLetvu(context.Background(), st) != nil {
		t.Error("bez dohvatnika nema memoranduma")
	}
	sektor := &models.Sector{ID: "P"}
	h.SetSektorZaLetvu(func(context.Context, *models.Station) *models.Sector { return sektor })
	if h.sektorZaLetvu(context.Background(), st) != sektor {
		t.Error("dohvatnik centra se ne pita")
	}
}
