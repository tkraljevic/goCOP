package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// Sektori i područja koji letvu prate (Odluke (8. 10. 2026.), pitanje 8):
// obrazac letve ih nosi kvačicama, kartica ih pokazuje nazivima.

func TestPracenjeIzObrascaLetve(t *testing.T) {
	// drugi obrazac (historijat, stari klijent) polja ne nosi: ostaju kakva jesu
	if s, p := pracenjeIzObrasca(url.Values{"prate_sektor": {"P"}}); s != nil || p != nil {
		t.Errorf("bez oznake obrasca: %v %v", s, p)
	}
	// obrazac kartice bez ijedne kvačice briše praćenje
	s, p := pracenjeIzObrasca(url.Values{"prate_obrazac": {"1"}})
	if s == nil || p == nil || len(*s) != 0 || len(*p) != 0 {
		t.Errorf("prazan obrazac kartice: %v %v", s, p)
	}
	s, p = pracenjeIzObrasca(url.Values{"prate_obrazac": {"1"}, "prate_sektor": {"P", "Q"}, "prate_podrucje": {"1", "x", " 3 "}})
	if !reflect.DeepEqual(*s, []string{"P", "Q"}) || !reflect.DeepEqual(*p, []int{1, 3}) {
		t.Errorf("kvačice: %v %v", *s, *p)
	}

	st := models.Station{PrateSektori: []string{"E"}, PratePodrucja: []int{2}}
	stationForm{Obrazac: obrazacKartica, PrateSektori: s, PratePodrucja: p}.primijeni(&st)
	if !reflect.DeepEqual(st.PrateSektori, []string{"P", "Q"}) || !reflect.DeepEqual(st.PratePodrucja, []int{1, 3}) {
		t.Errorf("kartica prenosi praćenje: %v %v", st.PrateSektori, st.PratePodrucja)
	}
	stationForm{Obrazac: obrazacKartica, Name: "Mohács"}.primijeni(&st)
	stationForm{Obrazac: obrazacHistorijat, PrateSektori: &[]string{}}.primijeni(&st)
	if !reflect.DeepEqual(st.PrateSektori, []string{"P", "Q"}) {
		t.Errorf("zahtjev bez praćenja ili historijat ga ne smije dirati: %v", st.PrateSektori)
	}
	if nova := (stationForm{Name: "Barcs", PrateSektori: &[]string{"P"}}).toStation(); !reflect.DeepEqual(nova.PrateSektori, []string{"P"}) {
		t.Errorf("nova letva: %v", nova.PrateSektori)
	}
}

func TestPracenjeIzZahtjevaLetve(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/stations/update", strings.NewReader(`{"name":"Mohács","prate_sektori":["P"],"prate_podrucja":[3]}`))
	r.Header.Set("Content-Type", "application/json")
	f, err := decodeStationForm(r)
	if err != nil || f.PrateSektori == nil || !reflect.DeepEqual(*f.PrateSektori, []string{"P"}) || !reflect.DeepEqual(*f.PratePodrucja, []int{3}) {
		t.Fatalf("JSON s praćenjem: %+v, %v", f, err)
	}
	r = httptest.NewRequest(http.MethodPost, "/api/stations/update", strings.NewReader(`{"name":"Mohács"}`))
	r.Header.Set("Content-Type", "application/json")
	if f, _ := decodeStationForm(r); f.PrateSektori != nil || f.PratePodrucja != nil {
		t.Errorf("JSON bez praćenja ga ne smije brisati: %+v", f)
	}
	r = httptest.NewRequest(http.MethodPost, "/api/stations/update", strings.NewReader("name=Moh%C3%A1cs&prate_obrazac=1&prate_sektor=P&prate_podrucje=3"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if f, err := decodeStationForm(r); err != nil || f.PrateSektori == nil || !reflect.DeepEqual(*f.PratePodrucja, []int{3}) {
		t.Errorf("obrazac s praćenjem: %+v, %v", f, err)
	}
}

func TestIzborIOpisPracenja(t *testing.T) {
	sektori := []models.Sector{{ID: "P", Name: "Sektor P — Primjerski sliv"}, {ID: "Q"}}
	podrucja := []models.Area{{ID: 1, SectorID: "P", Name: "Mali sliv Primjerica"}, {ID: 2, SectorID: "P"}, {ID: 3, SectorID: "Q", Name: "Mali sliv Drugdje"}}
	izbor := izborPracenja(sektori, podrucja)
	if len(izbor) != 2 || len(izbor[0].Podrucja) != 2 || len(izbor[1].Podrucja) != 1 || izbor[1].Podrucja[0].ID != 3 {
		t.Errorf("izbor po sektorima: %+v", izbor)
	}
	st := models.Station{PrateSektori: []string{"P", "Q", "Z"}, PratePodrucja: []int{3, 2, 99}}
	want := []string{"Sektor P — Primjerski sliv", "Sektor Q", "Sektor Z", "BP 3 Mali sliv Drugdje", "BP 2", "BP 99"}
	if got := opisPracenja(st, sektori, podrucja); !reflect.DeepEqual(got, want) {
		t.Errorf("opis praćenja: %q", got)
	}
	if got := opisPracenja(models.Station{}, sektori, podrucja); got != nil {
		t.Errorf("letva bez praćenja: %q", got)
	}

	h := &StationsHandler{}
	if s, a := h.sektoriIPodrucja(); s != nil || a != nil || h.pracenjeLetve(st)[0] != "Sektor P" {
		t.Error("bez dohvatnika ustroja nema izbora, a opis ostaje šifra")
	}
	h.SetUstroj(func() ([]models.Sector, []models.Area) { return sektori, podrucja })
	if got := h.pracenjeLetve(st); got[0] != "Sektor P — Primjerski sliv" {
		t.Errorf("opis s dohvatnikom: %q", got)
	}
}

func TestUstrojIzRegistra(t *testing.T) {
	if s, a := (&Server{}).ustroj(); s != nil || a != nil {
		t.Error("bez servisa korisnika nema ustroja")
	}
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "ustroj.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO', 'COP')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{userService: service.NewUserService(repository.NewUserRepository(baza, ledger.New(baza, "test")), nil, service.NewSSEBroker())}
	sektori, podrucja := s.ustroj()
	if len(sektori) != 1 || sektori[0].ID != "P" || len(podrucja) != 1 || podrucja[0].ID != 1 {
		t.Errorf("ustroj: %+v %+v", sektori, podrucja)
	}
}

func TestPracenjeNaObrascuIKarticiLetve(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Mohács", Code: "mohacs", PrateSektori: []string{"P"}, PratePodrucja: []int{3}}
	izbor := izborPracenja([]models.Sector{{ID: "P", Name: "Sektor P"}, {ID: "Q", Name: "Sektor Q"}},
		[]models.Area{{ID: 1, SectorID: "P", Name: "Mali sliv Primjerica"}, {ID: 3, SectorID: "Q", Name: "Mali sliv Drugdje"}})
	data := StationPageData{CurrentUser: &models.User{FullName: "Ivo Ivić"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station: st, IsEdit: true, IzborPracenja: izbor}
	html := iscrtaj(t, "station_form.html", data)
	for _, dio := range []string{`name="prate_obrazac"`, `name="prate_sektor" value="P" checked`, `name="prate_sektor" value="Q" >`,
		`name="prate_podrucje" value="3" checked`, `name="prate_podrucje" value="1" >`, "BP 3 Mali sliv Drugdje"} {
		if !strings.Contains(html, dio) {
			t.Errorf("obrazac letve nema %s", dio)
		}
	}
	// bez registra obrazac ne nosi polja praćenja, pa spremanje ne briše upisano
	data.IzborPracenja = nil
	if html := iscrtaj(t, "station_form.html", data); strings.Contains(html, "prate_obrazac") {
		t.Error("obrazac bez izbora nosi oznaku praćenja")
	}

	kartica := iscrtaj(t, "station_detail.html", StationPageData{CurrentUser: &models.User{FullName: "Ivo Ivić"},
		Permissions: &models.UserPermissions{}, Station: st, LetvaStranica: "kartica",
		Pracenje: []string{"Sektor P", "BP 3 Mali sliv Drugdje"}})
	if !strings.Contains(kartica, "Prate je: Sektor P, BP 3 Mali sliv Drugdje") {
		t.Error("kartica letve ne pokazuje tko je prati")
	}
}
