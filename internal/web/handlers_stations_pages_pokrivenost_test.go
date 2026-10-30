package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/arhiva"
	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// okolinaStraniceLetve je letva Primjerovo na dionici P.1.1 s pragovima,
// položajem i arhivom: satni niz vodostaja iz lipnja 2013. koji prelazi
// pripremno stanje i redovnu obranu, snimka korita i HQ krivulja. Uz nju su
// letve Drugovo, koja u arhivi ima svoj niz, i Trećevo, koja nema ništa.
type okolinaStraniceLetve struct {
	baza      *sql.DB
	h         *StationsHandler
	letva     *models.Station
	drugovo   *models.Station
	trecevo   *models.Station
	arhivaPut string
	ugradjeno []*arhiva.Sadrzaj
	ugradnja  error // što ugradnja vraća
}

// pocetakVala je prvi sat probnog niza, 14. 6. 2013. u 7 h UTC
var pocetakVala = time.Date(2013, 6, 14, 7, 0, 0, 0, time.UTC)

func probnaArhivaLetve(t *testing.T) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "arhiva.db")
	a, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := arhiva.PripremiPraznu(a); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO nizovi (id, zona, sliv, letva, izvor, velicina, vrsta, zapisa, od, do_)
			VALUES (1, 'UTC', 'drava', 'primjerovo', 'letva-hv', 'vodostaj', 'satni', 6, '2013-06-14', '2013-06-14')`,
		`INSERT INTO nizovi (id, zona, sliv, letva, izvor, velicina, vrsta, zapisa, od, do_)
			VALUES (2, 'UTC', 'drava', 'primjerovo', 'letva-hv', 'protok', 'dnevni', 1, '2013-06-14', '2013-06-14')`,
		`INSERT INTO nizovi (id, zona, sliv, letva, izvor, velicina, vrsta, zapisa, od, do_)
			VALUES (3, 'UTC', 'drava', 'drugovo', 'letva-hv', 'vodostaj', 'satni', 2, '2013-06-14', '2013-06-14')`,
		`INSERT INTO profili (id, letva, datum, vodostaj, kota_nule) VALUES (1, 'primjerovo', '2020-03-01', 120, 100.0)`,
		`INSERT INTO profil_tocke (profil, stacionaza, visina) VALUES (1, 0, 102.5), (1, 5, 99.0), (1, 10, 98.2), (1, 15, 99.1), (1, 20, 102.8)`,
		`INSERT INTO hq_krivulje (id, letva, vrijedi_od, vrijedi_do, izvor) VALUES (1, 'primjerovo', '2001-01-01', '', 'DHMZ')`,
		`INSERT INTO hq_odsjecci (krivulja, od_cm, do_cm, oblik, p1, p2, p3, p4) VALUES (1, 0, 300, 'polinom', 0, 100, 0, 0)`,
	} {
		if _, err := a.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for i, v := range []float64{100, 160, 210, 160, 100, 90} {
		kad := pocetakVala.Add(time.Duration(i) * time.Hour).Unix()
		if _, err := a.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (1, ?, ?)`, kad, v); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Exec(`INSERT INTO spoj (letva, velicina, korak, vrijeme, vrijednost, izvor, vrsta, tocnost)
			VALUES ('primjerovo', 'vodostaj', 'satni', ?, ?, 'letva-hv', 'trenutna', 1)`, kad, v); err != nil {
			t.Fatal(err)
		}
	}
	// dnevni srednjak istog dana; satni ga u sažetku izoštri na 210 i 90 cm
	if _, err := a.Exec(`INSERT INTO spoj (letva, velicina, korak, vrijeme, vrijednost, izvor, vrsta, tocnost)
		VALUES ('primjerovo', 'vodostaj', 'dnevni', ?, 140, 'letva-hv', 'srednjak', 1)`,
		time.Date(2013, 6, 14, 0, 0, 0, 0, time.UTC).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (2, ?, 95.5)`, pocetakVala.Unix()); err != nil {
		t.Fatal(err)
	}
	for i, v := range []float64{50, 55} {
		if _, err := a.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (3, ?, ?)`,
			pocetakVala.Add(time.Duration(i)*time.Hour).Unix(), v); err != nil {
			t.Fatal(err)
		}
	}
	return put
}

func novaOkolinaStraniceLetve(t *testing.T) *okolinaStraniceLetve {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "letva.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'prva', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.2', 1, 'P', 'druga', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	ctx := context.Background()
	stationRepo := repository.NewStationRepository(baza, rec)
	sections := service.NewSectionService(repository.NewSectionRepository(baza, rec), service.NewSSEBroker())
	stations := service.NewStationService(stationRepo, sections, service.NewSSEBroker())
	waters := service.NewWatercourseService(repository.NewWatercourseRepository(baza, rec))

	sirina, duzina, nula := 45.8, 18.85, 100.0
	letva := &models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo", Watercourse: "Primjerica",
		Latitude: &sirina, Longitude: &duzina, ZeroDatum: &nula,
		Prep: models.Threshold{Cm: cmPtr(150)}, Regular: models.Threshold{Cm: cmPtr(200)}}
	drugovo := &models.Station{ID: uuid.New(), Code: "drugovo", Name: "Drugovo"}
	trecevo := &models.Station{ID: uuid.New(), Code: "trecevo", Name: "Trećevo"}
	for _, st := range []*models.Station{letva, drugovo, trecevo} {
		if err := stationRepo.CreateStation(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES ('veza-1', 'P.1.1', ?, '2026-01-01 00:00:00')`,
		letva.ID.String()); err != nil {
		t.Fatal(err)
	}

	o := &okolinaStraniceLetve{baza: baza, letva: letva, drugovo: drugovo, trecevo: trecevo,
		arhivaPut: probnaArhivaLetve(t)}
	arh, err := repository.OpenArhiva(o.arhivaPut)
	if err != nil || arh == nil {
		t.Fatalf("arhiva: %v", err)
	}
	t.Cleanup(func() { arh.Close() })

	h := NewStationsHandler(stations, nil)
	h.SetPageTemplates(predlozakStranice(t, "station_detail.html"), predlozakStranice(t, "station_form.html"),
		predlozakStranice(t, "station_history.html"), predlozakStranice(t, "station_history_form.html"), sections, waters)
	h.SetArhiva(func() *repository.ArhivaRepository { return arh })
	h.SetPaket(func() string { return o.arhivaPut }, func() string { return "cop-primjerovo" },
		func(s *arhiva.Sadrzaj) error {
			if o.ugradnja != nil {
				return o.ugradnja
			}
			o.ugradjeno = append(o.ugradjeno, s)
			return nil
		}, predlozakStranice(t, "paket_pregled.html"))
	o.h = h
	return o
}

// zovi šalje zahtjev rukovatelju u ime korisnika s danim pravima
func zoviLetvu(t *testing.T, fn http.HandlerFunc, r *http.Request, id string, perms *models.UserPermissions) *httptest.ResponseRecorder {
	t.Helper()
	if id != "" {
		r.SetPathValue("id", id)
	}
	r = zahtjevSKorisnikom(r, &models.User{ID: uuid.New(), FullName: "Iva Ivić"}, perms)
	w := httptest.NewRecorder()
	fn(w, r)
	return w
}

func dohvatiLetvu(t *testing.T, fn http.HandlerFunc, put, id string, perms *models.UserPermissions) *httptest.ResponseRecorder {
	t.Helper()
	return zoviLetvu(t, fn, httptest.NewRequest(http.MethodGet, put, nil), id, perms)
}

// Obrazac nove letve otvara samo onaj tko smije pisati na nekoj dionici;
// postojeću samo onaj tko smije mijenjati baš nju. Kriva ili nepostojeća
// letva je 404, ne prazan obrazac.
func TestObrazacLetvePrava(t *testing.T) {
	o := novaOkolinaStraniceLetve(t)
	id := o.letva.ID.String()
	admin := &models.UserPermissions{IsGlobalAdmin: true}

	w := dohvatiLetvu(t, o.h.ShowStationForm, "/stations/new", "", admin)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Nova vodomjerna postaja") {
		t.Errorf("nova letva za administratora: %d", w.Code)
	}
	if w := dohvatiLetvu(t, o.h.ShowStationForm, "/stations/new", "", sDioniceLetve()); w.Code != http.StatusOK {
		t.Errorf("nova letva za operatera dionice: %d", w.Code)
	}
	if w := dohvatiLetvu(t, o.h.ShowStationForm, "/stations/new", "", &models.UserPermissions{}); w.Code != http.StatusForbidden ||
		!strings.Contains(w.Body.String(), "Nemate pravo dodavati postaje") {
		t.Errorf("nova letva bez ijedne dionice: %d %s", w.Code, w.Body.String())
	}

	for _, s := range []struct {
		opis   string
		id     string
		perms  *models.UserPermissions
		status int
	}{
		{"kriva oznaka", "nije-uuid", admin, http.StatusNotFound},
		{"nepostojeća letva", uuid.NewString(), admin, http.StatusNotFound},
		{"promatrač", id, promatracLetve(), http.StatusForbidden},
		{"operater s druge dionice", id, pravaDuznosti(models.RoleOperator, models.ScopeSection, "P", 1, "P.1.2"), http.StatusForbidden},
		{"operater s dionice letve", id, sDioniceLetve(), http.StatusOK},
	} {
		w := dohvatiLetvu(t, o.h.ShowStationForm, "/stations/"+s.id+"/edit", s.id, s.perms)
		if w.Code != s.status {
			t.Errorf("%s: %d, očekivano %d", s.opis, w.Code, s.status)
		}
	}
}

// Obrazac postojeće letve nosi njezino ime, prazne popise kao "[]" i
// krajnosti iz arhive na preuzimanje; letva bez arhive nema što preuzeti.
func TestObrazacPostojeceLetve(t *testing.T) {
	o := novaOkolinaStraniceLetve(t)
	id := o.letva.ID.String()
	w := dohvatiLetvu(t, o.h.ShowStationForm, "/stations/"+id+"/edit", id, sDioniceLetve())
	if w.Code != http.StatusOK {
		t.Fatalf("obrazac: %d %s", w.Code, w.Body.String())
	}
	html := w.Body.String()
	imaSve(t, html, "obrazac letve", []string{"Uređivanje postaje", "Primjerovo", "/api/stations/update",
		"const EXTREMES = [];", "Preuzmi iz podataka",
		`{"Kind":"MAX","LevelCm":210,"OnDate":"2013-06-14","Izvor":"letva-hv","Odakle":"arhiva"}`,
		`{"Kind":"MIN","LevelCm":90,"OnDate":"2013-06-14","Izvor":"letva-hv","Odakle":"arhiva"}`})
	if strings.Contains(html, "Čita se računom") {
		t.Error("obrazac kaže da je račun telemetrije upisan, a nije")
	}

	// Trećevo nema ništa u arhivi: prazan popis, bez gumba za preuzimanje
	id = o.trecevo.ID.String()
	w = dohvatiLetvu(t, o.h.ShowStationForm, "/stations/"+id+"/edit", id, &models.UserPermissions{IsGlobalAdmin: true})
	if w.Code != http.StatusOK {
		t.Fatalf("obrazac Trećeva: %d %s", w.Code, w.Body.String())
	}
	html = w.Body.String()
	imaSve(t, html, "obrazac Trećeva", []string{"Trećevo", "const EXTREMES = [];", "const KRAJNOSTI = [];"})
	if strings.Contains(html, "Preuzmi iz podataka") {
		t.Error("Trećevo nudi preuzimanje krajnosti kojih nema")
	}
}

// Račun za telemetriju: bez spremišta, bez računa i bez letve nema ničega;
// letva bez svojeg računa čita se računom čvora, a s njim svojim.
func TestRacunTelemetrijeLetve(t *testing.T) {
	o := novaOkolinaStraniceLetve(t)
	ctx := context.Background()
	if got := o.h.racunHidroView(ctx, "primjerovo"); got != "" {
		t.Errorf("bez spremišta: %q", got)
	}
	o.h.SetHidroView(func() *repository.HidroViewRepository { return nil }, func() []byte { return nil })
	if got := o.h.racunHidroView(ctx, "primjerovo"); got != "" {
		t.Errorf("spremište nije otvoreno: %q", got)
	}
	racuni := repository.NewHidroViewRepository(o.baza)
	o.h.SetHidroView(func() *repository.HidroViewRepository { return racuni }, func() []byte { return nil })
	if got := o.h.racunHidroView(ctx, "primjerovo"); got != "" {
		t.Errorf("bez upisanog računa: %q", got)
	}
	if err := racuni.Spremi(ctx, &repository.RacunHidroView{Korisnik: "cvor.primjerovo", Lozinka: []byte{1}}); err != nil {
		t.Fatal(err)
	}
	if got := o.h.racunHidroView(ctx, "primjerovo"); got != "cvor.primjerovo" {
		t.Errorf("račun čvora: %q", got)
	}
	if err := racuni.Spremi(ctx, &repository.RacunHidroView{Letva: "primjerovo", Korisnik: "letva.primjerovo", Lozinka: []byte{2}}); err != nil {
		t.Fatal(err)
	}
	if got := o.h.racunHidroView(ctx, "primjerovo"); got != "letva.primjerovo" {
		t.Errorf("račun letve: %q", got)
	}

	// obrazac pokazuje kojim se računom čita, a lozinku ne
	id := o.letva.ID.String()
	html := dohvatiLetvu(t, o.h.ShowStationForm, "/stations/"+id+"/edit", id, sDioniceLetve()).Body.String()
	if !strings.Contains(html, "Čita se računom <strong>letva.primjerovo</strong>") {
		t.Error("obrazac ne kaže kojim se računom letva čita")
	}
}

// Obrazac historijata otvara samo onaj tko smije mijenjati letvu; prazni
// popisi dolaze skripti kao "[]".
func TestObrazacHistorijataPrava(t *testing.T) {
	o := novaOkolinaStraniceLetve(t)
	id := o.letva.ID.String()
	put := "/stations/" + id + "/historijat/uredi"
	if w := dohvatiLetvu(t, o.h.ObrazacHistorijata, put, "nije-uuid", sDioniceLetve()); w.Code != http.StatusNotFound {
		t.Errorf("kriva oznaka: %d", w.Code)
	}
	if w := dohvatiLetvu(t, o.h.ObrazacHistorijata, put, uuid.NewString(), sDioniceLetve()); w.Code != http.StatusNotFound {
		t.Errorf("nepostojeća letva: %d", w.Code)
	}
	if w := dohvatiLetvu(t, o.h.ObrazacHistorijata, put, id, promatracLetve()); w.Code != http.StatusForbidden {
		t.Errorf("promatrač: %d", w.Code)
	}
	w := dohvatiLetvu(t, o.h.ObrazacHistorijata, put, id, sDioniceLetve())
	if w.Code != http.StatusOK {
		t.Fatalf("operater dionice: %d %s", w.Code, w.Body.String())
	}
	imaSve(t, w.Body.String(), "obrazac historijata", []string{
		"const RETURN_LEVELS = [];", "const ZERO_HISTORY = [];", "const OGRADE = [];"})
}

// Historijat letve s arhivom pokazuje valove izračunate iz spojenog niza:
// 210 cm prelazi i pripremno stanje i redovnu obranu.
func TestHistorijatLetveIzArhive(t *testing.T) {
	o := novaOkolinaStraniceLetve(t)
	o.h.SetIspravci(func() *repository.IspravakRepository { return nil })
	o.h.SetBiljeske(func() *repository.BiljeskaRepository { return nil })
	id := o.letva.ID.String()
	w := dohvatiLetvu(t, o.h.HistorijatLetve, "/stations/"+id+"/historijat?niz=1", id, promatracLetve())
	if w.Code != http.StatusOK {
		t.Fatalf("historijat: %d %s", w.Code, w.Body.String())
	}
	html := w.Body.String()
	imaSve(t, html, "historijat", []string{"Valovi obrane u nizu", "&#43;150 cm", "&#43;200 cm"})
	nemaNista(t, html, "historijat", []string{
		"nijednom nije dosegnula", "nema niza vodostaja", "nijedan prag obrane nije upisan"})

	if w := dohvatiLetvu(t, o.h.HistorijatLetve, "/stations/x/historijat", "x", promatracLetve()); w.Code != http.StatusNotFound {
		t.Errorf("kriva oznaka: %d", w.Code)
	}
}

// Podaci letve iz arhive: odabrani niz, zadnja vrijednost spojenog niza,
// korito, protok po krivulji i pragovi u koti i protoku.
func TestPodaciLetveIzArhive(t *testing.T) {
	o := novaOkolinaStraniceLetve(t)
	id := o.letva.ID.String()
	r := httptest.NewRequest(http.MethodGet, "/stations/"+id+"?niz=2", nil)
	r.SetPathValue("id", id)
	r = zahtjevSKorisnikom(r, &models.User{FullName: "Iva Ivić"}, sDioniceLetve())
	data, ok := o.h.podaciLetve(httptest.NewRecorder(), r)
	if !ok {
		t.Fatal("podaci letve nisu skupljeni")
	}
	if !data.CanEdit {
		t.Error("operater dionice ne smije mijenjati svoju letvu")
	}
	if len(data.Nizovi) != 2 || data.NizID != 2 || data.Pregled == nil {
		t.Errorf("nizovi %d, odabran %d, pregled %v", len(data.Nizovi), data.NizID, data.Pregled)
	}
	if data.Zadnji == nil || data.Zadnji.Vrijednost != 90 || data.ZadnjiIzvor != "letva-hv" {
		t.Fatalf("zadnja vrijednost: %+v (%s)", data.Zadnji, data.ZadnjiIzvor)
	}
	// Q = 100·H, H u metrima: 90 cm daje 90 m³/s, unutar raspona krivulje
	if data.ZadnjiProtok != 90 || data.ZadnjiProtokIzvan {
		t.Errorf("protok %.2f, izvan %v", data.ZadnjiProtok, data.ZadnjiProtokIzvan)
	}
	if data.Profil == nil || data.Crtez == nil || data.CrtezUzak == nil {
		t.Error("korito nije nacrtano")
	}
	if len(data.ValoviPragovi) != 2 || len(data.ValoviSvi) == 0 || len(data.Valovi) == 0 {
		t.Errorf("pragova %d, valova %d (%d na stranici)", len(data.ValoviPragovi), len(data.ValoviSvi), len(data.Valovi))
	}
	if len(data.PragoviKote) != 2 || len(data.PragoviKote[0].Kote) == 0 {
		t.Errorf("pragovi u koti: %+v", data.PragoviKote)
	}
	if len(data.WaterRegistry) != 0 {
		t.Errorf("registar voda nije prazan: %v", data.WaterRegistry)
	}
	// krajnosti iz arhive: dnevni dan izoštren satnim nizom istog vala
	if k := data.KrajnostiIzNiza; len(k) != 2 || k[0].Kind != models.ExtremeMax || k[0].LevelCm != 210 ||
		k[1].Kind != models.ExtremeMin || k[1].LevelCm != 90 || k[0].Odakle != "arhiva" {
		t.Errorf("krajnosti iz niza: %+v", k)
	}

	// niz iz upita koji letva nema ne bira se: uzima se vodostaj
	r2 := httptest.NewRequest(http.MethodGet, "/stations/"+id+"?niz=3", nil)
	r2.SetPathValue("id", id)
	r2 = zahtjevSKorisnikom(r2, &models.User{FullName: "Iva Ivić"}, promatracLetve())
	if d2, _ := o.h.podaciLetve(httptest.NewRecorder(), r2); d2.NizID != 1 || d2.CanEdit {
		t.Errorf("tuđi niz iz upita: odabran %d, smije mijenjati %v", d2.NizID, d2.CanEdit)
	}
}

// Kartica letve: položaj ide karti, a gumb za unos u arhivu vidi samo onaj
// tko letvu smije mijenjati, i to na čvoru koji drži njezine datoteke.
func TestKarticaLetveKartaIUnosUArhivu(t *testing.T) {
	o := novaOkolinaStraniceLetve(t)
	o.h.SetUnosUArhivu(func(letva string) bool { return letva == "primjerovo" })
	o.h.SetKarta(func() KartaPostavke { return KartaPostavke{Plocice: "http://localhost/{z}/{x}/{y}.png"} })
	id := o.letva.ID.String()

	w := dohvatiLetvu(t, o.h.ShowStation, "/stations/"+id, id, sDioniceLetve())
	if w.Code != http.StatusOK {
		t.Fatalf("kartica: %d %s", w.Code, w.Body.String())
	}
	imaSve(t, w.Body.String(), "kartica", []string{`karta-letva-podaci`, `"code":"primjerovo"`,
		"/administracija/uvoz-niza?letva=primjerovo"})

	w = dohvatiLetvu(t, o.h.ShowStation, "/stations/"+id, id, promatracLetve())
	if w.Code != http.StatusOK {
		t.Fatalf("kartica promatrača: %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "/administracija/uvoz-niza") {
		t.Error("promatraču se nudi unos u arhivu")
	}

	// letva bez položaja nema podataka za kartu
	tid := o.trecevo.ID.String()
	w = dohvatiLetvu(t, o.h.ShowStation, "/stations/"+tid, tid, &models.UserPermissions{IsGlobalAdmin: true})
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "karta-letva-podaci") {
		t.Errorf("letva bez položaja: %d, ima podatke za kartu: %v", w.Code, strings.Contains(w.Body.String(), "karta-letva-podaci"))
	}
	if w := dohvatiLetvu(t, o.h.ShowStation, "/stations/"+uuid.NewString(), uuid.NewString(), promatracLetve()); w.Code != http.StatusNotFound {
		t.Errorf("nepostojeća letva: %d", w.Code)
	}
}

// Niz koji se prikazuje: onaj iz upita ako ga letva ima, inače prvi
// vodostaj, inače prvi niz; bez nizova nijedan.
func TestOdabraniNiz(t *testing.T) {
	nizovi := []models.HidroNiz{{ID: 7, Velicina: "protok"}, {ID: 8, Velicina: "vodostaj"}, {ID: 9, Velicina: "vodostaj"}}
	for _, s := range []struct {
		upit   string
		nizovi []models.HidroNiz
		want   int64
	}{
		{"?niz=9", nizovi, 9},
		{"?niz=7", nizovi, 7},
		{"?niz=42", nizovi, 8},
		{"?niz=devet", nizovi, 8},
		{"", nizovi, 8},
		{"", nizovi[:1], 7},
		{"?niz=7", nil, 0},
	} {
		r := httptest.NewRequest(http.MethodGet, "/stations/x"+s.upit, nil)
		if got := odabraniNiz(r, s.nizovi); got != s.want {
			t.Errorf("upit %q, %d nizova: %d, očekivano %d", s.upit, len(s.nizovi), got, s.want)
		}
	}
}

// Upozorenje o višem vrhu dolazi samo iz sažetka vodostaja.
func TestVisiVrhIzSazetka(t *testing.T) {
	vrh := &models.VisiVrh{Vrijednost: 812, Kad: "2013-06-09 14:00", Izvor: "cop", Sati: 3}
	if got := visiVrhIzSazetka([]models.SazetakVelicine{{Velicina: "protok", Visi: &models.VisiVrh{}}, {Velicina: "vodostaj", Visi: vrh}}); got != vrh {
		t.Errorf("vodostaj s višim vrhom: %+v", got)
	}
	if got := visiVrhIzSazetka([]models.SazetakVelicine{{Velicina: "protok", Visi: vrh}}); got != nil {
		t.Errorf("bez vodostaja: %+v", got)
	}
	if got := visiVrhIzSazetka(nil); got != nil {
		t.Errorf("bez sažetka: %+v", got)
	}
}

// Obrazac dobiva JS literal: prazan i nečitljiv popis su "[]".
func TestJSONZaObrazac(t *testing.T) {
	if got := jsonZaObrazac([]models.StationReturnLevel(nil)); got != "[]" {
		t.Errorf("prazan popis: %s", got)
	}
	if got := jsonZaObrazac(make(chan int)); got != "[]" {
		t.Errorf("nečitljivo: %s", got)
	}
	if got := jsonZaObrazac([]int{1, 2}); got != "[1,2]" {
		t.Errorf("popis: %s", got)
	}
}

// Bez prava nema izmjene letve, ni kad letva nema dionica.
func TestCanEditStationBezPrava(t *testing.T) {
	h := &StationsHandler{}
	st := models.Station{SectionCodes: []string{"P.1.1"}}
	if h.canEditStation(nil, st) {
		t.Error("bez prava smije mijenjati letvu")
	}
	if h.canEditStation(&models.UserPermissions{AllowedSections: map[string]bool{"P.1.1": true}}, models.Station{}) {
		t.Error("letvu bez dionica smije mijenjati operater dionice")
	}
}

// Upisani ekstremi dolaze u obrazac letve, a povratni vodostaji i promjene
// kote nule u obrazac historijata, kao JS literal koji skripta prikaže.
func TestObrazacLetveNosiUpisanePopise(t *testing.T) {
	o := novaOkolinaStraniceLetve(t)
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	st, err := o.h.stationService.GetStation(ctx, o.trecevo.ID)
	if err != nil || st == nil {
		t.Fatalf("letva: %v", err)
	}
	kota := 87.5
	st.Extremes = []models.StationExtreme{{Kind: models.ExtremeMax, LevelCm: cmPtr(612), OnDate: "1965-06-20", Source: "DHMZ"}}
	st.ReturnLevels = []models.StationReturnLevel{{Years: 100, LevelCm: cmPtr(640)}}
	st.ZeroDatumHistory = []models.ZeroDatumChange{{ValidFrom: "1970-01-01", Datum: &kota, System: "TRST"}}
	if err := o.h.stationService.UpdateStation(ctx, admin, st); err != nil {
		t.Fatal(err)
	}
	id := st.ID.String()
	w := dohvatiLetvu(t, o.h.ShowStationForm, "/stations/"+id+"/edit", id, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("obrazac: %d %s", w.Code, w.Body.String())
	}
	imaSve(t, w.Body.String(), "obrazac letve", []string{`"level_cm":612`, `"on_date":"1965-06-20"`})
	if strings.Contains(w.Body.String(), "const EXTREMES = [];") {
		t.Error("ekstremi nisu došli u obrazac")
	}
	// povratni vodostaji i kota nule uređuju se na obrascu historijata
	w = dohvatiLetvu(t, o.h.ObrazacHistorijata, "/stations/"+id+"/historijat/uredi", id, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("obrazac historijata: %d", w.Code)
	}
	imaSve(t, w.Body.String(), "obrazac historijata", []string{`"years":100`, `"valid_from":"1970-01-01"`, `"system":"TRST"`})
}
