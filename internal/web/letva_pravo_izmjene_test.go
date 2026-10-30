package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/javnivodostaji"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// letvaSPravima je registar s letvom Primjerovo na dionici P.1.1 sektora P
// i s rijekom Primjericom u registru voda
type letvaSPravima struct {
	stationRepo *repository.StationRepository
	stations    *service.StationService
	waters      *service.WatercourseService
	letva       *models.Station
}

func novaLetvaSPravima(t *testing.T) *letvaSPravima {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "letva_prava.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	rec := ledger.New(baza, "test")
	stationRepo := repository.NewStationRepository(baza, rec)
	stations := service.NewStationService(stationRepo,
		service.NewSectionService(repository.NewSectionRepository(baza, rec), service.NewSSEBroker()), service.NewSSEBroker())
	waterRepo := repository.NewWatercourseRepository(baza, rec)
	waters := service.NewWatercourseService(waterRepo)
	waters.SetPostaje(stations)
	ctx := context.Background()
	if err := waterRepo.CreateWatercourse(ctx, &models.Watercourse{Code: "rijeka-primjerica", OfficialName: "rijeka Primjerica", Name: "Primjerica"}); err != nil {
		t.Fatal(err)
	}
	letva := &models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo"}
	if err := stationRepo.CreateStation(ctx, letva); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'prva', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.2', 1, 'P', 'druga', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES ('veza-1', 'P.1.1', '` + letva.ID.String() + `', '2026-01-01 00:00:00')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return &letvaSPravima{stationRepo: stationRepo, stations: stations, waters: waters, letva: letva}
}

func (l *letvaSPravima) ucitaj(t *testing.T) *models.Station {
	t.Helper()
	st, err := l.stationRepo.GetStationByID(context.Background(), l.letva.ID)
	if err != nil || st == nil {
		t.Fatalf("letva se ne čita: %v", err)
	}
	return st
}

// jsonZahtjev šalje JSON tijelo u ime korisnika s danim pravima
func jsonZahtjev(t *testing.T, put string, tijelo any, perms *models.UserPermissions) *http.Request {
	t.Helper()
	b, err := json.Marshal(tijelo)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, put, strings.NewReader(string(b)))
	r.Header.Set("Content-Type", "application/json")
	return zahtjevSKorisnikom(r, &models.User{ID: uuid.New(), FullName: "Probni Korisnik"}, perms)
}

// korisnikLetve je opis korisnika i prava kojima šalje zahtjev
type korisnikLetve struct {
	opis  string
	perms *models.UserPermissions
}

// pravaDuznosti gradi prava iz jedne aktivne dužnosti, kao što ih program
// gradi pri prijavi
func pravaDuznosti(uloga models.Role, doseg models.ScopeType, sektor string, podrucje int, dionice string) *models.UserPermissions {
	d := models.Duty{ID: uuid.New(), Role: uloga, ScopeType: doseg, SectionCodes: dionice, IsActive: true}
	if sektor != "" {
		d.SectorID = &sektor
	}
	if podrucje > 0 {
		d.AreaID = &podrucje
	}
	return models.NewUserPermissions(models.User{ID: uuid.New(), Duties: []models.Duty{d}})
}

// korisniciLetve dijeli korisnike prema letvi Primjerovo na dionici P.1.1
// (područje 1, sektor P). Bez prava su promatrač sektora P, uprava drugog
// sektora i operater s druge dionice istog područja. Pravo imaju operater s
// dionice letve, uprava sektora P, uprava područja 1 i globalni
// administrator; uprava sektora i područja ima i oblik samo s upravom, bez
// prava upisa, da se vidi da je dovoljna sama uprava.
func korisniciLetve() (bezPrava, sPravom []korisnikLetve) {
	bezPrava = []korisnikLetve{
		{"promatrač sektora P", pravaDuznosti(models.RoleViewer, models.ScopeSector, "P", 0, "")},
		{"uprava sektora Q", pravaDuznosti(models.RoleSectorLeader, models.ScopeSector, "Q", 0, "")},
		{"operater s dionice P.1.2", pravaDuznosti(models.RoleOperator, models.ScopeSection, "P", 1, "P.1.2")},
	}
	sPravom = []korisnikLetve{
		{"operater s dionice P.1.1", pravaDuznosti(models.RoleOperator, models.ScopeSection, "P", 1, "P.1.1")},
		{"uprava sektora P", pravaDuznosti(models.RoleSectorLeader, models.ScopeSector, "P", 0, "")},
		{"uprava područja 1", pravaDuznosti(models.RoleAreaLeader, models.ScopeArea, "P", 1, "")},
		{"samo uprava sektora P", &models.UserPermissions{AdminSectors: map[string]bool{"P": true}}},
		{"samo uprava područja 1", &models.UserPermissions{AdminAreas: map[int]bool{1: true}}},
		{"globalni administrator", &models.UserPermissions{IsGlobalAdmin: true}},
	}
	return
}

// sDioniceLetve su prava operatera s dionice letve
func sDioniceLetve() *models.UserPermissions {
	return pravaDuznosti(models.RoleOperator, models.ScopeSection, "P", 1, "P.1.1")
}

// promatracLetve su prava promatrača sektora letve
func promatracLetve() *models.UserPermissions {
	return pravaDuznosti(models.RoleViewer, models.ScopeSector, "P", 0, "")
}

// Vodotok letve mijenja samo onaj tko smije mijenjati i samu letvu. Tko
// nema pravo na letvu, ne može ni postaviti ni obrisati vodotok i dobije
// 403; tko ga ima, može oboje. Voda koje nema u registru je neispravan unos
// (400), a ne pitanje prava.
func TestVodotokLetveTraziPravoNaLetvu(t *testing.T) {
	l := novaLetvaSPravima(t)
	h := NewWatercoursesHandler(l.waters, nil, nil)
	bezPrava, sPravom := korisniciLetve()

	posalji := func(perms *models.UserPermissions, letva, voda string) int {
		w := httptest.NewRecorder()
		h.HandleAssignStationWatercourseAPI(w, jsonZahtjev(t, "/api/stations/watercourse",
			map[string]string{"station_id": letva, "watercourse_code": voda}, perms))
		return w.Code
	}
	letva := l.letva.ID.String()

	for _, k := range bezPrava {
		for _, voda := range []string{"rijeka-primjerica", ""} {
			if kod := posalji(k.perms, letva, voda); kod != http.StatusForbidden {
				t.Errorf("%s, voda %q: %d", k.opis, voda, kod)
			}
		}
		if st := l.ucitaj(t); st.WatercourseCode != "" || st.Watercourse != "" {
			t.Fatalf("%s je promijenio vodotok: %q %q", k.opis, st.WatercourseCode, st.Watercourse)
		}
	}

	for _, k := range sPravom {
		if kod := posalji(k.perms, letva, "rijeka-primjerica"); kod != http.StatusOK {
			t.Fatalf("%s, postavljanje: %d", k.opis, kod)
		}
		if st := l.ucitaj(t); st.WatercourseCode != "rijeka-primjerica" || st.WatercourseSource != models.WatercourseFromOperator {
			t.Errorf("%s: vodotok nije upisan: %q %q", k.opis, st.WatercourseCode, st.WatercourseSource)
		}
		if kod := posalji(k.perms, letva, ""); kod != http.StatusOK {
			t.Fatalf("%s, brisanje: %d", k.opis, kod)
		}
		if st := l.ucitaj(t); st.WatercourseCode != "" {
			t.Errorf("%s nije obrisao vodotok: %q", k.opis, st.WatercourseCode)
		}
	}
	if kod := posalji(sDioniceLetve(), letva, "nema-je"); kod != http.StatusBadRequest {
		t.Errorf("voda koje nema u registru: %d", kod)
	}
}

// Identifikator letve zapisan velikim slovima ili u vitičastim zagradama
// odnosi se na istu letvu: pravo se provjeri i vodotok upiše baš na nju.
func TestVodotokLetveDrugiZapisIdentifikatora(t *testing.T) {
	l := novaLetvaSPravima(t)
	h := NewWatercoursesHandler(l.waters, nil, nil)
	for _, zapis := range []string{strings.ToUpper(l.letva.ID.String()), "{" + l.letva.ID.String() + "}"} {
		w := httptest.NewRecorder()
		h.HandleAssignStationWatercourseAPI(w, jsonZahtjev(t, "/api/stations/watercourse",
			map[string]string{"station_id": zapis, "watercourse_code": "rijeka-primjerica"}, sDioniceLetve()))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", zapis, w.Code, w.Body.String())
		}
		if st := l.ucitaj(t); st.WatercourseCode != "rijeka-primjerica" {
			t.Errorf("%s: vodotok nije upisan na letvu: %q", zapis, st.WatercourseCode)
		}
		if err := l.waters.SetStationWatercourse(context.Background(), &models.UserPermissions{IsGlobalAdmin: true}, l.letva.ID.String(), ""); err != nil {
			t.Fatal(err)
		}
	}
}

// Bez provjere prava nad letvama servis vodotok letve dopušta samo
// globalnom administratoru, a bez prijave nikome.
func TestVodotokLetveBezProvjerePostajaSamoAdministrator(t *testing.T) {
	l := novaLetvaSPravima(t)
	samo := service.NewWatercourseService(repository.NewWatercourseRepository(nil, nil))
	sDionice := sDioniceLetve()
	ctx := context.Background()
	if err := samo.SetStationWatercourse(ctx, nil, l.letva.ID.String(), ""); err == nil {
		t.Error("bez prijave prošlo")
	}
	if err := samo.SetStationWatercourse(ctx, sDionice, l.letva.ID.String(), ""); err == nil {
		t.Error("bez provjere postaja prošla je osoba s dionice")
	}
	if _, err := l.stations.PostajaZaIzmjenu(ctx, sDionice, "nije-uuid"); err == nil {
		t.Error("neispravan identifikator letve prošao")
	}
	if _, err := l.stations.PostajaZaIzmjenu(ctx, sDionice, uuid.NewString()); err == nil {
		t.Error("letva koje nema prošla")
	}
	if _, err := l.stations.PostajaZaIzmjenu(ctx, promatracLetve(), l.letva.ID.String()); !errors.Is(err, service.ErrNemaOvlasti) {
		t.Errorf("promatrač: %v", err)
	}
}

// Izmjena letve ne mijenja njezinu šifru, ni kad obrazac pošalje drugu:
// šifra je identitet letve. Ostale izmjene se spremaju.
func TestIzmjenaLetveNeMijenjaSifru(t *testing.T) {
	l := novaLetvaSPravima(t)
	h := NewStationsHandler(l.stations, nil)
	sDionice := sDioniceLetve()

	w := httptest.NewRecorder()
	h.HandleUpdateStationAPI(w, jsonZahtjev(t, "/api/stations/update", map[string]string{
		"id": l.letva.ID.String(), "code": "druga-sifra", "name": "Primjerovo gornje",
	}, sDionice))
	if w.Code != http.StatusOK {
		t.Fatalf("izmjena: %d %s", w.Code, w.Body.String())
	}
	st := l.ucitaj(t)
	if st.Code != "primjerovo" {
		t.Errorf("šifra promijenjena: %q", st.Code)
	}
	if st.Name != "Primjerovo gornje" {
		t.Errorf("naziv nije spremljen: %q", st.Name)
	}

	// i servis sam: šifra koja stigne mimo obrasca ne upisuje se
	st.Code = "treca-sifra"
	if err := l.stations.UpdateStation(context.Background(), &models.UserPermissions{IsGlobalAdmin: true}, st); err != nil {
		t.Fatal(err)
	}
	if st := l.ucitaj(t); st.Code != "primjerovo" {
		t.Errorf("servis je upisao novu šifru: %q", st.Code)
	}
}

// Tko ne smije mijenjati ili unijeti letvu, dobije 403 prije nego što se
// pozovu javni popis i telemetrija. Ovlaštenom se oni i dalje pozivaju.
// Dionica koje nema je neispravan unos (400), i to se javi prije javnog
// popisa.
func TestLetvaPravoPrijeVanjskihPoziva(t *testing.T) {
	l := novaLetvaSPravima(t)
	h := NewStationsHandler(l.stations, nil)
	javni, telemetrija := 0, 0
	h.SetJavniUvoz(func() *javnivodostaji.Uvoznik { javni++; return nil })
	h.SetHidroView(func() *repository.HidroViewRepository { telemetrija++; return nil }, func() []byte { return nil })
	bezPrava, sPravom := korisniciLetve()

	izmjena := func(perms *models.UserPermissions) int {
		w := httptest.NewRecorder()
		h.HandleUpdateStationAPI(w, jsonZahtjev(t, "/api/stations/update", map[string]string{
			"id": l.letva.ID.String(), "name": "Primjerovo", "javni_url": javnivodostaji.AdresaStranice + "?postajaID=123", "telemetrija_uvoz": "1",
		}, perms))
		return w.Code
	}
	unos := func(perms *models.UserPermissions, dionica string) int {
		w := httptest.NewRecorder()
		h.HandleCreateStationAPI(w, jsonZahtjev(t, "/api/stations/create", map[string]string{
			"section_code": dionica, "name": "Nova letva " + uuid.NewString()[:8], "javni_url": javnivodostaji.AdresaStranice + "?postajaID=456",
		}, perms))
		return w.Code
	}

	for _, k := range bezPrava {
		if kod := izmjena(k.perms); kod != http.StatusForbidden {
			t.Errorf("%s, izmjena: %d", k.opis, kod)
		}
		if kod := unos(k.perms, "P.1.1"); kod != http.StatusForbidden {
			t.Errorf("%s, unos na dionicu: %d", k.opis, kod)
		}
		if kod := unos(k.perms, ""); kod != http.StatusForbidden {
			t.Errorf("%s, unos izvan dionice: %d", k.opis, kod)
		}
	}
	if javni != 0 || telemetrija != 0 {
		t.Fatalf("vanjski pozivi bez prava: javni %d, telemetrija %d", javni, telemetrija)
	}

	for i, k := range sPravom {
		if kod := izmjena(k.perms); kod != http.StatusOK {
			t.Fatalf("%s, izmjena: %d", k.opis, kod)
		}
		if javni != 2*i+1 || telemetrija != i+1 {
			t.Errorf("%s, izmjena: javni %d, telemetrija %d", k.opis, javni, telemetrija)
		}
		if kod := unos(k.perms, "P.1.1"); kod != http.StatusOK {
			t.Fatalf("%s, unos: %d", k.opis, kod)
		}
		if javni != 2*i+2 {
			t.Errorf("%s, unos: javni %d", k.opis, javni)
		}
	}
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	if kod := unos(admin, ""); kod != http.StatusOK {
		t.Fatalf("administrator, unos izvan dionice: %d", kod)
	}
	prije := javni
	if kod := unos(admin, "P.9.9"); kod != http.StatusBadRequest {
		t.Errorf("administrator, dionica koje nema: %d", kod)
	}
	if javni != prije {
		t.Errorf("dionica koje nema pozvala je javni popis")
	}
}

// Obrazac na stranici bez prava vraća se na obrazac s porukom, a ne na
// prazan odgovor.
func TestLetvaBezPravaObrazacSPorukom(t *testing.T) {
	l := novaLetvaSPravima(t)
	h := NewStationsHandler(l.stations, nil)
	promatrac := promatracLetve()
	for _, x := range []struct {
		obrazac, natrag string
	}{
		{"kartica", "/stations/" + l.letva.ID.String() + "/edit"},
		{"historijat", "/stations/" + l.letva.ID.String() + "/historijat/uredi"},
	} {
		r := httptest.NewRequest(http.MethodPost, "/api/stations/update",
			strings.NewReader("id="+l.letva.ID.String()+"&name=Primjerovo&obrazac="+x.obrazac))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h.HandleUpdateStationAPI(w, zahtjevSKorisnikom(r, &models.User{ID: uuid.New()}, promatrac))
		if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), x.natrag+"?error=") {
			t.Errorf("%s: %d %q", x.obrazac, w.Code, w.Header().Get("Location"))
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/api/stations/create", strings.NewReader("section_code=P.1.1&name=Nova"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.HandleCreateStationAPI(w, zahtjevSKorisnikom(r, &models.User{ID: uuid.New()}, promatrac))
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/stations/new?error=") {
		t.Errorf("unos: %d %q", w.Code, w.Header().Get("Location"))
	}
}
