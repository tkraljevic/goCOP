package web

import (
	"context"
	"database/sql"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// okolinaSredstava je sektor B s područjem 34, dionicom B.34.1 i skladištem
// Bilje u kojem je zatečeno 12 lopata i 100 praznih vreća. Sektor ima tri
// dnevnika COP-a: otvoren, završen i prijepis. Stranice se iscrtavaju
// probnim predloškom koji ispisuje što je rukovatelj skupio, pa se vidi
// sam rukovatelj, a ne izgled stranice.
type okolinaSredstava struct {
	baza    *sql.DB
	svc     *service.MtsService
	h       *MtsHandler
	mux     *http.ServeMux
	sk      *models.Skladiste
	otvoren *models.Journal
	osoba   *models.User
}

// probniIspis su polja koja probni predložak ispisuje, redom
const probniIspis = `Sektor={{.Sektor}};IsEdit={{.IsEdit}};SmijePisati={{.SmijePisati}};SmijeUrediti={{.SmijeUrediti}};` +
	`Podrucje={{if .Skladiste}}{{.Skladiste.AreaID}}{{end}};Skladiste={{if .Skladiste}}{{.Skladiste.Naziv}}{{end}};` +
	`Vrsta={{.Zahvat.Vrsta}};Oblik={{.Zahvat.Oblik}};Sredstvo={{.Zahvat.VrstaID}};` +
	`Dionica={{len .Dionice}};Obrane={{len .Obrane}};Skladista={{len .Skladista}};Promet={{len .Promet}};` +
	`Od={{.Od}};Do={{.Do}};VrstaID={{.VrstaID}};Godina={{.Godina}};` +
	`Popis={{if .Popis}}{{.Popis.ID}}|{{len .Popis.Stavke}}{{end}};Uredi={{if .Vrsta}}{{.Vrsta.ID}}{{end}};` +
	`GdjeIma={{len .GdjeIma}};PotrebeSektora={{len .PotrebeSektora}};Stanje={{len .Stanje}};`

func probniPredlozakSredstava(stranica string) *template.Template {
	return template.Must(template.New(stranica).Parse(`{{define "` + stranica + `"}}` + probniIspis + `{{end}}`))
}

func novaOkolinaSredstava(t *testing.T) *okolinaSredstava {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "mts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (34, 'B', 'Drava i Dunav', 'COP', 'Osijek')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.34.1', 34, 'B', 'Dunav d.o.', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	rec := ledger.New(baza, "test")
	repo := repository.NewMtsRepository(baza, rec)
	if err := repo.OsigurajKatalog(ctx); err != nil {
		t.Fatal(err)
	}
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	svc := service.NewMtsService(repo, repository.NewSectionRepository(baza, rec), userRepo)
	sections := service.NewSectionService(repository.NewSectionRepository(baza, rec), service.NewSSEBroker())
	journalRepo := repository.NewJournalRepository(baza, rec)
	journals := service.NewJournalService(journalRepo, nil, nil)

	o := &okolinaSredstava{baza: baza, svc: svc, osoba: &models.User{ID: uuid.New(), FullName: "Petra Perić"}}
	o.sk = &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Skladište Bilje", Aktivno: true}
	if err := svc.SpremiSkladiste(ctx, upravaSredstava(), o.sk); err != nil {
		t.Fatal(err)
	}
	dan := time.Date(2025, 3, 1, 0, 0, 0, 0, models.Zagreb)
	for _, z := range []service.Zahvat{
		{Vrsta: models.PrometPocetno, SkladisteID: o.sk.ID, VrstaID: "lopata", Kolicina: 12, Datum: dan},
		{Vrsta: models.PrometPocetno, SkladisteID: o.sk.ID, VrstaID: "vrece-50x80", Oblik: models.OblikPrazno, Kolicina: 100, Datum: dan},
	} {
		if _, err := svc.Provedi(ctx, o.osoba, upravaSredstava(), z); err != nil {
			t.Fatal(err)
		}
	}
	pocetak := time.Date(2025, 4, 1, 8, 0, 0, 0, models.Zagreb)
	kraj := pocetak.Add(72 * time.Hour)
	o.otvoren = &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "B", Title: "Dnevnik COP-a, travanj 2025.",
		Year: 2025, StartedAt: &pocetak}
	for _, j := range []*models.Journal{
		o.otvoren,
		{Kind: models.JournalKindDefense, CentarSektor: "B", Title: "Dnevnik COP-a, ožujak 2025.", Year: 2025, StartedAt: &pocetak, EndedAt: &kraj},
		{Kind: models.JournalKindDefense, CentarSektor: "B", Title: "Dnevnik COP-a, lipanj 2009.", Year: 2009, StartedAt: &pocetak, Reconstruction: true},
	} {
		if err := journalRepo.SaveJournal(ctx, j); err != nil {
			t.Fatal(err)
		}
	}

	o.h = NewMtsHandler(func() *service.MtsService { return svc }, users, sections, journals, probniPredlozakSredstava)
	o.h.SetStructures(service.NewStructureService(repository.NewStructureRepository(baza, rec)))
	o.mux = rutaSredstava(o.h)
	return o
}

func rutaSredstava(h *MtsHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sredstva", h.ShowPregled)
	mux.HandleFunc("GET /sredstva/promet", h.ShowPromet)
	mux.HandleFunc("GET /sredstva/katalog", h.ShowKatalog)
	mux.HandleFunc("POST /sredstva/katalog", h.HandleSaveVrsta)
	mux.HandleFunc("POST /sredstva/katalog/{id}", h.HandleSaveVrsta)
	mux.HandleFunc("POST /sredstva/katalog/{id}/obrisi", h.HandleObrisiVrstu)
	mux.HandleFunc("GET /sredstva/na-terenu", h.ShowNaTerenu)
	mux.HandleFunc("GET /sredstva/gdje/{vrsta}", h.ShowGdjeIma)
	mux.HandleFunc("GET /sredstva/skladista/novo", h.ShowSkladisteForm)
	mux.HandleFunc("POST /sredstva/skladista", h.HandleSaveSkladiste)
	mux.HandleFunc("GET /sredstva/skladista/{id}", h.ShowSkladiste)
	mux.HandleFunc("GET /sredstva/skladista/{id}/uredi", h.ShowSkladisteForm)
	mux.HandleFunc("POST /sredstva/skladista/{id}", h.HandleSaveSkladiste)
	mux.HandleFunc("GET /sredstva/skladista/{id}/promet/novo", h.ShowPrometForm)
	mux.HandleFunc("POST /sredstva/skladista/{id}/promet", h.HandleSavePromet)
	mux.HandleFunc("GET /sredstva/skladista/{id}/popis", h.ShowPopisNovo)
	mux.HandleFunc("GET /sredstva/skladista/{id}/potrebe", h.ShowPotrebe)
	mux.HandleFunc("POST /sredstva/skladista/{id}/potrebe", h.HandleSavePotrebe)
	mux.HandleFunc("GET /sredstva/potrebe", h.ShowPotrebeSektora)
	mux.HandleFunc("GET /sredstva/popisi", h.ShowPopisi)
	mux.HandleFunc("POST /sredstva/popisi", h.HandleSavePopis)
	mux.HandleFunc("GET /sredstva/popisi/{id}", h.ShowPopis)
	mux.HandleFunc("GET /sredstva/popisi/{id}/uredi", h.ShowPopisUredi)
	mux.HandleFunc("POST /sredstva/popisi/{id}", h.HandleSavePopis)
	mux.HandleFunc("POST /sredstva/popisi/{id}/zakljuci", h.HandleZakljuciPopis)
	return mux
}

// upravaSredstava su prava uprave sektora B
func upravaSredstava() *models.UserPermissions {
	return &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}}
}

// promatracSredstava vidi sredstva, a ne upisuje ih
func promatracSredstava() *models.UserPermissions {
	return &models.UserPermissions{AllowedSectors: map[string]bool{"B": true}}
}

// zovi šalje zahtjev kroz rute; obrazac nil je zahtjev bez tijela
func (o *okolinaSredstava) zovi(t *testing.T, perms *models.UserPermissions, metoda, putanja string, obrazac url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if obrazac != nil {
		r = httptest.NewRequest(metoda, putanja, strings.NewReader(obrazac.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(metoda, putanja, nil)
	}
	w := httptest.NewRecorder()
	o.mux.ServeHTTP(w, zahtjevSKorisnikom(r, o.osoba, perms))
	return w
}

// ispis vraća ispis probnog predloška; stranica mora proći s 200
func (o *okolinaSredstava) ispis(t *testing.T, perms *models.UserPermissions, putanja string) string {
	t.Helper()
	w := o.zovi(t, perms, http.MethodGet, putanja, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", putanja, w.Code, w.Body.String())
	}
	return w.Body.String()
}

// preusmjerenjeSredstava vraća put i poruku zadanog ključa iz odgovora 303
func preusmjerenjeSredstava(t *testing.T, w *httptest.ResponseRecorder, kljuc string) (string, string) {
	t.Helper()
	if w.Code != http.StatusSeeOther {
		t.Fatalf("očekivano preusmjeravanje, dobiveno %d %s", w.Code, w.Body.String())
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Path, u.Query().Get(kljuc)
}

func mora(t *testing.T, ispis, opis string, dijelovi ...string) {
	t.Helper()
	for _, d := range dijelovi {
		if !strings.Contains(ispis, d) {
			t.Errorf("%s: nema %q u %s", opis, d, ispis)
		}
	}
}

// Zaglavlje tablice stanja ima svaki oblik jednom, abecedno, bez osnovnog.
func TestOblicIStanjaPoGrupama(t *testing.T) {
	vrece := models.VrstaSredstva{ID: "vrece", Grupa: models.GrupaMaterijal, Oblici: []string{models.OblikPunjeno, models.OblikPrazno}}
	jumbo := models.VrstaSredstva{ID: "jumbo", Grupa: models.GrupaMaterijal, Oblici: []string{models.OblikPrazno, models.OblikPunjeno}}
	lopata := models.VrstaSredstva{ID: "lopata", Grupa: models.GrupaAlat}
	d := MtsPageData{Stanje: []models.StanjeVrste{{Vrsta: lopata}, {Vrsta: vrece}, {Vrsta: jumbo}}}
	if got := strings.Join(d.Oblici(), ","); got != models.OblikPrazno+","+models.OblikPunjeno {
		t.Errorf("oblici: %s", got)
	}
	if got := (MtsPageData{Stanje: []models.StanjeVrste{{Vrsta: lopata}}}).Oblici(); len(got) != 0 {
		t.Errorf("sredstvo bez oblika daje oblike: %v", got)
	}
	g := d.PoGrupama()
	if len(g) != 2 || g[0].ID != models.GrupaAlat || g[1].ID != models.GrupaMaterijal || len(g[1].Redci) != 2 {
		t.Errorf("po skupinama: %+v", g)
	}
	if d.StavkePoGrupama() != nil {
		t.Error("stavke bez popisa")
	}
	if d.PotrebaU("sk", "lopata") != 0 || d.Potreba("lopata").Kolicina != 0 {
		t.Error("potreba bez upisa nije nula")
	}
}

// Bez sektora u zahtjevu pokazuje se sektor u kojem osoba radi; inače prvi
// sektor koji nije direkcija.
func TestSektorKorisnikaBezSvogSektora(t *testing.T) {
	direkcija := models.Sector{ID: "DIREKCIJA"}
	if got := sektorKorisnika(nil, []models.Sector{{ID: "B"}}); got != "" {
		t.Errorf("bez prava: %q", got)
	}
	if got := sektorKorisnika(&models.UserPermissions{}, []models.Sector{direkcija}); got != "" {
		t.Errorf("samo direkcija: %q", got)
	}
	if got := sektorKorisnika(&models.UserPermissions{}, []models.Sector{direkcija, {ID: "C"}, {ID: "B"}}); got != "C" {
		t.Errorf("prvi sektor koji nije direkcija: %q", got)
	}
	if got := sektorKorisnika(&models.UserPermissions{AdminSectors: map[string]bool{"D": true, "A": true}}, nil); got != "A" {
		t.Errorf("svoj sektor: %q", got)
	}
}

// Na teren idu punjene vreće, u skladište prazne; količina se piše
// hrvatski, s tvrdim razmakom tisućica i pravim minusom.
func TestZadaniOblikIKolicina(t *testing.T) {
	for vrsta, want := range map[string]string{
		models.PrometIzdano: models.OblikPunjeno, models.PrometPovrat: models.OblikPunjeno, models.PrometUtrosak: models.OblikPunjeno,
		models.PrometPrimka: models.OblikPrazno, models.PrometPrijenos: models.OblikPrazno, "": models.OblikPrazno,
	} {
		if got := zadaniOblik(vrsta); got != want {
			t.Errorf("zadaniOblik(%q) = %q, očekivano %q", vrsta, got, want)
		}
	}
	for v, want := range map[float64]string{-1234.5: "−1\u00a0234,5", -3: "−3", 0: "0", 1000000: "1\u00a0000\u00a0000", 0.125: "0,125"} {
		if got := kolicinaHR(v); got != want {
			t.Errorf("kolicinaHR(%v) = %q, očekivano %q", v, got, want)
		}
	}
	if got := kolicinaSaZnakom(-2.5); got != "−2,5" {
		t.Errorf("kolicinaSaZnakom(-2,5) = %q", got)
	}
	if got := kolicinaSaZnakom(0); got != "0" {
		t.Errorf("kolicinaSaZnakom(0) = %q", got)
	}
}

// Obrasci nude samo otvorene obrane sektora: završena i prijepis ne.
func TestObraneSektoraSamoOtvorene(t *testing.T) {
	o := novaOkolinaSredstava(t)
	ctx := context.Background()
	if obraneSektora(ctx, nil, "B") != nil {
		t.Error("bez dnevnika ima obrana")
	}
	obrane := obraneSektora(ctx, o.h.journals, "B")
	if len(obrane) != 1 || obrane[0].ID != o.otvoren.ID {
		t.Errorf("otvorene obrane: %+v", obrane)
	}
	if got := obraneSektora(ctx, o.h.journals, "C"); len(got) != 0 {
		t.Errorf("tuđi sektor: %+v", got)
	}
	o.baza.Close()
	if got := obraneSektora(ctx, o.h.journals, "B"); got != nil {
		t.Errorf("nečitljiva baza: %+v", got)
	}
}

// Kad se baza ne čita, stranica javlja grešku, a ne prazno stanje koje bi
// izgledalo kao da sredstava nema.
func TestSredstvaBezBazeJavljajuGresku(t *testing.T) {
	o := novaOkolinaSredstava(t)
	putevi := []string{
		"/sredstva?sektor=B",
		"/sredstva/skladista/" + o.sk.ID,
		"/sredstva/promet?sektor=B",
		"/sredstva/gdje/lopata",
		"/sredstva/na-terenu?sektor=B",
		"/sredstva/popisi?sektor=B",
		"/sredstva/popisi/neki",
		"/sredstva/katalog",
		"/sredstva/potrebe?sektor=B&godina=2026",
	}
	o.baza.Close()
	for _, p := range putevi {
		if w := o.zovi(t, upravaSredstava(), http.MethodGet, p, nil); w.Code != http.StatusInternalServerError {
			t.Errorf("%s: %d, očekivano 500", p, w.Code)
		}
	}

	bez := NewMtsHandler(func() *service.MtsService { return nil }, o.h.users, nil, nil, probniPredlozakSredstava)
	w := httptest.NewRecorder()
	bez.ShowPregled(w, zahtjevSKorisnikom(httptest.NewRequest(http.MethodGet, "/sredstva", nil), o.osoba, upravaSredstava()))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("bez sredstava: %d", w.Code)
	}
}

// Predložak koji se ne iscrta daje grešku poslužitelja.
func TestSredstvaPredlozakKojiPuca(t *testing.T) {
	o := novaOkolinaSredstava(t)
	o.h.tmpl = func(string) *template.Template { return template.Must(template.New("prazan").Parse("")) }
	if w := o.zovi(t, upravaSredstava(), http.MethodGet, "/sredstva?sektor=B", nil); w.Code != http.StatusInternalServerError {
		t.Errorf("predložak bez stranice: %d", w.Code)
	}
}

// Nepostojeće skladište, popis i vrsta su 404 na svakoj stranici koja ih
// otvara.
func TestSredstvaNepostojeceJe404(t *testing.T) {
	o := novaOkolinaSredstava(t)
	for _, s := range []struct{ metoda, put string }{
		{http.MethodGet, "/sredstva/skladista/nema"},
		{http.MethodGet, "/sredstva/skladista/nema/uredi"},
		{http.MethodGet, "/sredstva/skladista/nema/promet/novo"},
		{http.MethodGet, "/sredstva/skladista/nema/popis"},
		{http.MethodGet, "/sredstva/skladista/nema/potrebe"},
		{http.MethodGet, "/sredstva/popisi/nema"},
		{http.MethodGet, "/sredstva/popisi/nema/uredi"},
		{http.MethodGet, "/sredstva/gdje/nema"},
	} {
		if w := o.zovi(t, upravaSredstava(), s.metoda, s.put, nil); w.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d", s.metoda, s.put, w.Code)
		}
	}
	if w := o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/skladista/nema",
		url.Values{"naziv": {"Novo ime"}, "sektor": {"B"}}); w.Code != http.StatusNotFound {
		t.Errorf("izmjena nepostojećeg skladišta: %d", w.Code)
	}
	if w := o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/katalog/nema",
		url.Values{"naziv": {"Nešto"}, "jedinica": {"kom"}, "grupa": {models.GrupaAlat}}); w.Code != http.StatusNotFound {
		t.Errorf("izmjena nepostojeće vrste: %d", w.Code)
	}
}

// Promatrač vidi, ali ne otvara obrasce i ne mijenja katalog.
func TestSredstvaPromatracNePise(t *testing.T) {
	o := novaOkolinaSredstava(t)
	for _, s := range []struct {
		metoda, put string
		obrazac     url.Values
	}{
		{http.MethodGet, "/sredstva/skladista/" + o.sk.ID + "/uredi", nil},
		{http.MethodGet, "/sredstva/skladista/" + o.sk.ID + "/promet/novo", nil},
		{http.MethodGet, "/sredstva/skladista/" + o.sk.ID + "/popis", nil},
		{http.MethodPost, "/sredstva/katalog", url.Values{"naziv": {"Kanta"}}},
		{http.MethodPost, "/sredstva/katalog/lopata/obrisi", url.Values{}},
	} {
		if w := o.zovi(t, promatracSredstava(), s.metoda, s.put, s.obrazac); w.Code != http.StatusForbidden {
			t.Errorf("%s %s: %d, očekivano 403", s.metoda, s.put, w.Code)
		}
	}
	mora(t, o.ispis(t, promatracSredstava(), "/sredstva/skladista/"+o.sk.ID), "kartica skladišta",
		"SmijePisati=false", "SmijeUrediti=false", "Skladiste=Skladište Bilje", "Promet=2;")
	if v, _ := o.svc.Vrsta(context.Background(), "lopata"); v == nil {
		t.Error("promatrač je obrisao vrstu")
	}
}

// Obrazac skladišta: novo nosi područje iz upita, postojeće je izmjena.
func TestObrazacSkladista(t *testing.T) {
	o := novaOkolinaSredstava(t)
	mora(t, o.ispis(t, upravaSredstava(), "/sredstva/skladista/novo?sektor=B&area=34"), "novo skladište",
		"Sektor=B;IsEdit=false;", "SmijeUrediti=true", "Podrucje=34;")
	mora(t, o.ispis(t, upravaSredstava(), "/sredstva/skladista/novo?sektor=B&area=nije"), "novo bez područja", "Podrucje=0;")
	mora(t, o.ispis(t, upravaSredstava(), "/sredstva/skladista/"+o.sk.ID+"/uredi"), "izmjena",
		"IsEdit=true", "Skladiste=Skladište Bilje")
}

// Izmjena skladišta čuva datum nastanka; neuspjelo spremanje vraća na
// obrazac s kojeg je došlo, s razlogom.
func TestSpremanjeSkladista(t *testing.T) {
	o := novaOkolinaSredstava(t)
	ctx := context.Background()
	prije, _ := o.svc.Skladiste(ctx, o.sk.ID)

	put, poruka := preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/skladista/"+o.sk.ID,
		url.Values{"naziv": {"Skladište Bilje II"}, "sektor": {"B"}, "area_id": {"34"}, "adresa": {"Bilje 1"}}), "success")
	if put != "/sredstva/skladista/"+o.sk.ID || poruka != "Skladište je upisano." {
		t.Errorf("izmjena: %s %q", put, poruka)
	}
	poslije, _ := o.svc.Skladiste(ctx, o.sk.ID)
	if poslije.Naziv != "Skladište Bilje II" || poslije.Adresa != "Bilje 1" || !poslije.CreatedAt.Equal(prije.CreatedAt) || !poslije.Aktivno {
		t.Errorf("poslije izmjene: %+v (nastalo prije %v)", poslije, prije.CreatedAt)
	}

	put, poruka = preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/skladista/"+o.sk.ID,
		url.Values{"naziv": {" "}, "sektor": {"B"}, "area_id": {"34"}}), "error")
	if put != "/sredstva/skladista/"+o.sk.ID+"/uredi" || poruka != "skladište mora imati naziv" {
		t.Errorf("izmjena bez naziva: %s %q", put, poruka)
	}
	put, poruka = preusmjerenjeSredstava(t, o.zovi(t, promatracSredstava(), http.MethodPost, "/sredstva/skladista",
		url.Values{"naziv": {"Skladište Darda"}, "sektor": {"B"}, "area_id": {"34"}}), "error")
	if put != "/sredstva/skladista/novo" || !strings.Contains(poruka, "uprava") {
		t.Errorf("novo bez prava: %s %q", put, poruka)
	}

	r := httptest.NewRequest(http.MethodPost, "/sredstva/skladista", strings.NewReader("naziv=%zz"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	o.mux.ServeHTTP(w, zahtjevSKorisnikom(r, o.osoba, upravaSredstava()))
	if _, poruka := preusmjerenjeSredstava(t, w, "error"); poruka != "Neispravan zahtjev" {
		t.Errorf("pokvaren obrazac: %q", poruka)
	}
}

// pokvarenObrazac šalje tijelo koje se ne da pročitati kao obrazac
func (o *okolinaSredstava) pokvarenObrazac(t *testing.T, putanja string) string {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, putanja, strings.NewReader("a=%zz"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	o.mux.ServeHTTP(w, zahtjevSKorisnikom(r, o.osoba, upravaSredstava()))
	_, poruka := preusmjerenjeSredstava(t, w, "error")
	return poruka
}

// Svaki obrazac koji se ne da pročitati vraća istu poruku, ne grešku 500.
func TestSredstvaPokvarenObrazac(t *testing.T) {
	o := novaOkolinaSredstava(t)
	for _, p := range []string{
		"/sredstva/skladista/" + o.sk.ID + "/promet",
		"/sredstva/popisi",
		"/sredstva/katalog",
		"/sredstva/skladista/" + o.sk.ID + "/potrebe",
	} {
		if poruka := o.pokvarenObrazac(t, p); poruka != "Neispravan zahtjev" {
			t.Errorf("%s: %q", p, poruka)
		}
	}
}

// Obrazac zahvata: bez vrste je primka praznih vreća, izdavanje nudi
// punjene; nudi dionice sektora, otvorene obrane i druga skladišta.
func TestObrazacZahvata(t *testing.T) {
	o := novaOkolinaSredstava(t)
	osnova := "/sredstva/skladista/" + o.sk.ID + "/promet/novo"
	mora(t, o.ispis(t, upravaSredstava(), osnova), "bez vrste",
		"Vrsta="+models.PrometPrimka+";Oblik="+models.OblikPrazno+";", "Dionica=1;Obrane=1;Skladista=1;")
	mora(t, o.ispis(t, upravaSredstava(), osnova+"?vrsta="+models.PrometIzdano+"&sredstvo=vrece-50x80"), "izdavanje",
		"Vrsta="+models.PrometIzdano+";Oblik="+models.OblikPunjeno+";Sredstvo=vrece-50x80;")
	mora(t, o.ispis(t, upravaSredstava(), osnova+"?vrsta="+models.PrometIzdano+"&oblik="+models.OblikPrazno), "zadan oblik",
		"Oblik="+models.OblikPrazno+";")
}

// Zahvat: količina koja nije broj i zahvat koji servis odbije vraćaju na
// obrazac s istom vrstom; „još jedan” vraća na novi obrazac s potvrdom, a
// bez njega zahvat završava na kartici skladišta.
func TestSpremanjeZahvata(t *testing.T) {
	o := novaOkolinaSredstava(t)
	put := "/sredstva/skladista/" + o.sk.ID + "/promet"
	natrag := "/sredstva/skladista/" + o.sk.ID + "/promet/novo"

	p, poruka := preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, put, url.Values{
		"vrsta": {models.PrometPrimka}, "sredstvo": {"lopata"}, "kolicina": {"tri"}}), "error")
	if p != natrag || poruka != "Količina nije broj" {
		t.Errorf("količina slovima: %s %q", p, poruka)
	}
	p, poruka = preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, put, url.Values{
		"vrsta": {models.PrometPrimka}, "sredstvo": {"lopata"}, "kolicina": {"0"}, "datum": {"2025-05-01"}}), "error")
	if p != natrag || poruka != "količina mora biti veća od nule" {
		t.Errorf("nula: %s %q", p, poruka)
	}

	w := o.zovi(t, upravaSredstava(), http.MethodPost, put, url.Values{
		"vrsta": {models.PrometPrimka}, "sredstvo": {"lopata"}, "kolicina": {"1 500"}, "datum": {"2025-05-01"},
		"dokument": {"Primka 7/2025"}, "jos": {"1"}})
	p, poruka = preusmjerenjeSredstava(t, w, "success")
	if p != natrag || !strings.HasPrefix(poruka, "Primka: 1\u00a0500") || !strings.HasSuffix(poruka, " je upisano; sljedeći zahvat.") {
		t.Errorf("još jedan: %s %q", p, poruka)
	}
	u, _ := url.Parse(w.Header().Get("Location"))
	if u.Query().Get("vrsta") != models.PrometPrimka || u.Query().Get("potvrda") == "" {
		t.Errorf("novi obrazac nema vrstu ili potvrdu: %s", u.RawQuery)
	}
	promet, _ := o.svc.Promet(context.Background(), repository.FiltarPrometa{SkladisteID: o.sk.ID, VrstaID: "lopata"})
	nadeno := false
	for _, x := range promet {
		if x.Vrsta == models.PrometPrimka && x.Kolicina == 1500 && x.Datum.Format("2006-01-02") == "2025-05-01" {
			nadeno = true
		}
	}
	if !nadeno {
		t.Errorf("primka nije upisana s danim datumom: %+v", promet)
	}

	// bez „još jedan” vraća na karticu skladišta s potvrdom zahvata
	w = o.zovi(t, upravaSredstava(), http.MethodPost, put, url.Values{
		"vrsta": {models.PrometPrimka}, "sredstvo": {"lopata"}, "kolicina": {"2"}, "datum": {"2025-05-02"}})
	p, poruka = preusmjerenjeSredstava(t, w, "success")
	if p != "/sredstva/skladista/"+o.sk.ID || !strings.HasPrefix(poruka, "Primka: 2") ||
		!strings.HasSuffix(poruka, " je upisano.") || strings.Contains(poruka, "sljedeći") {
		t.Errorf("bez još jednog: %s %q", p, poruka)
	}
	u, _ = url.Parse(w.Header().Get("Location"))
	if u.Query().Get("potvrda") == "" || u.Query().Get("vrsta") != "" {
		t.Errorf("kartica skladišta: potvrda prazna ili vrsta zaostala: %s", u.RawQuery)
	}
	promet, _ = o.svc.Promet(context.Background(), repository.FiltarPrometa{SkladisteID: o.sk.ID, VrstaID: "lopata"})
	nadeno = false
	for _, x := range promet {
		if x.Vrsta == models.PrometPrimka && x.Kolicina == 2 && x.Datum.Format("2006-01-02") == "2025-05-02" {
			nadeno = true
		}
	}
	if !nadeno {
		t.Errorf("druga primka nije upisana: %+v", promet)
	}
}

// Knjiga prometa po filtru: razdoblje, skladište i vrsta ostaju u obrascu
// filtra, a razdoblje izvan zahvata daje praznu knjigu.
func TestKnjigaPrometaPoFiltru(t *testing.T) {
	o := novaOkolinaSredstava(t)
	mora(t, o.ispis(t, upravaSredstava(), "/sredstva/promet?sektor=B&od=2025-01-01&do=2025-12-31&skladiste="+o.sk.ID+"&vrsta=lopata"),
		"filtar", "Od=2025-01-01;Do=2025-12-31;VrstaID=lopata;", "Skladiste=Skladište Bilje", "Promet=1;")
	mora(t, o.ispis(t, upravaSredstava(), "/sredstva/promet?sektor=B&od=2026-01-01"), "kasnije razdoblje", "Promet=0;", "Od=2026-01-01;Do=;")
	mora(t, o.ispis(t, upravaSredstava(), "/sredstva/promet?sektor=B&od=nije-datum"), "krivi datum", "Od=;", "Promet=2;")
}

// Gdje ima: nađe skladište sa zalihom te vrste.
func TestGdjeImaSredstva(t *testing.T) {
	o := novaOkolinaSredstava(t)
	mora(t, o.ispis(t, promatracSredstava(), "/sredstva/gdje/lopata"), "gdje ima lopata", "Uredi=lopata;", "GdjeIma=1;")
}

// Na terenu: sve obrane sektora za izbor, a uz odabranu i njezin promet.
func TestNaTerenuPoObrani(t *testing.T) {
	o := novaOkolinaSredstava(t)
	mora(t, o.ispis(t, promatracSredstava(), "/sredstva/na-terenu?sektor=B"), "bez obrane", "Obrane=3;", "VrstaID=;", "Promet=0;")
	mora(t, o.ispis(t, promatracSredstava(), "/sredstva/na-terenu?sektor=B&obrana="+o.otvoren.ID), "odabrana obrana",
		"Obrane=3;", "VrstaID="+o.otvoren.ID+";")
}

// Inventura: predložak za dan, spremanje nacrta, ponovno otvaranje istog
// dana vodi na spremljeni, uređivanje, zaključenje — i poslije njega nema
// izmjene.
func TestInventuraKrozRute(t *testing.T) {
	o := novaOkolinaSredstava(t)
	novi := "/sredstva/skladista/" + o.sk.ID + "/popis?dan=2025-12-31"
	ispis := o.ispis(t, upravaSredstava(), novi)
	mora(t, ispis, "predložak", "Popis=|")

	// prazan dan i tuđe skladište: spremanje ne prolazi i vraća na predložak
	p, poruka := preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/popisi",
		url.Values{"skladiste": {o.sk.ID}}), "error")
	if p != "/sredstva/skladista/"+o.sk.ID+"/popis" || poruka != "popis mora imati dan" {
		t.Errorf("bez dana: %s %q", p, poruka)
	}

	w := o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/popisi", url.Values{
		"skladiste": {o.sk.ID}, "dan": {"2025-12-31"}, "u:lopata:": {"11"}, "k:lopata:": {"12"}, "n:lopata:": {"jedna slomljena"}})
	p, poruka = preusmjerenjeSredstava(t, w, "success")
	if !strings.HasPrefix(p, "/sredstva/popisi/") || poruka != "Inventura je spremljena kao nacrt." {
		t.Fatalf("nacrt: %s %q", p, poruka)
	}
	id := strings.TrimPrefix(p, "/sredstva/popisi/")
	popis, _ := o.svc.Popis(context.Background(), id)
	if popis == nil || popis.Zakljucen() || popis.Napomena != "" {
		t.Fatalf("spremljeni nacrt: %+v", popis)
	}
	if st := stavkaPopisa(popis, "lopata"); st == nil || st.Utvrdjeno != 11 || st.Napomena != "jedna slomljena" {
		t.Errorf("stavka lopata: %+v", st)
	}

	// isti dan ponovno: vodi na spremljeni
	if w := o.zovi(t, upravaSredstava(), http.MethodGet, novi, nil); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/sredstva/popisi/"+id {
		t.Errorf("isti dan: %d %s", w.Code, w.Header().Get("Location"))
	}
	mora(t, o.ispis(t, upravaSredstava(), "/sredstva/popisi/"+id+"/uredi"), "uređivanje", "IsEdit=true", "Popis="+id+"|")
	mora(t, o.ispis(t, promatracSredstava(), "/sredstva/popisi/"+id), "promatrač vidi", "SmijePisati=false", "Popis="+id+"|")
	if w := o.zovi(t, promatracSredstava(), http.MethodGet, "/sredstva/popisi/"+id+"/uredi", nil); w.Code != http.StatusForbidden {
		t.Errorf("promatrač uređuje: %d", w.Code)
	}

	// izmjena bez dana vraća na obrazac izmjene
	p, _ = preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/popisi/"+id,
		url.Values{"skladiste": {o.sk.ID}}), "error")
	if p != "/sredstva/popisi/"+id+"/uredi" {
		t.Errorf("izmjena bez dana: %s", p)
	}

	// zaključenje iz obrasca
	p, poruka = preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/popisi/"+id, url.Values{
		"skladiste": {o.sk.ID}, "dan": {"2025-12-31"}, "u:lopata:": {"11"}, "zakljuci": {"1"}}), "success")
	if p != "/sredstva/popisi/"+id || !strings.HasPrefix(poruka, "Inventura je zaključena") {
		t.Errorf("zaključenje: %s %q", p, poruka)
	}
	if w := o.zovi(t, upravaSredstava(), http.MethodGet, "/sredstva/popisi/"+id+"/uredi", nil); w.Code != http.StatusForbidden {
		t.Errorf("zaključena se uređuje: %d", w.Code)
	}

	// ponovno zaključenje i nepostojeći popis javljaju razlog
	if _, poruka := preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/popisi/nema/zakljuci", url.Values{}), "error"); poruka != "popis nije pronađen" {
		t.Errorf("nepostojeći popis: %q", poruka)
	}
}

func stavkaPopisa(p *models.Popis, vrsta string) *models.PopisnaStavka {
	for i := range p.Stavke {
		if p.Stavke[i].VrstaID == vrsta && p.Stavke[i].Oblik == models.OblikOsnovni {
			return &p.Stavke[i]
		}
	}
	return nil
}

// Popis čije skladište više ne postoji ne otvara se.
func TestPopisBezSkladista(t *testing.T) {
	o := novaOkolinaSredstava(t)
	ctx := context.Background()
	p, err := o.svc.PredlozakPopisa(ctx, o.sk.ID, time.Date(2025, 12, 31, 0, 0, 0, 0, models.Zagreb))
	if err != nil {
		t.Fatal(err)
	}
	if err := o.svc.SpremiPopis(ctx, o.osoba, upravaSredstava(), p); err != nil {
		t.Fatal(err)
	}
	// skladiste_id nema stranog ključa, pa se popis da odvojiti izravno
	res, err := o.baza.Exec(`UPDATE mts_popisi SET skladiste_id = 'nema' WHERE id = ?`, p.ID)
	if err != nil {
		t.Fatalf("popis se ne da odvojiti od skladišta: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("odvojeno popisa: %d, očekivan jedan", n)
	}
	if w := o.zovi(t, upravaSredstava(), http.MethodGet, "/sredstva/popisi/"+p.ID, nil); w.Code != http.StatusNotFound {
		t.Errorf("popis bez skladišta: %d", w.Code)
	}
}

// Katalog: uprava otvara vrstu za izmjenu, promatrač ne; nova vrsta ide na
// kraj svoje skupine, a izmjena bez rednog broja čuva stari.
func TestKatalogSredstava(t *testing.T) {
	o := novaOkolinaSredstava(t)
	ctx := context.Background()
	mora(t, o.ispis(t, upravaSredstava(), "/sredstva/katalog?uredi=lopata"), "uprava", "SmijeUrediti=true", "Uredi=lopata;")
	mora(t, o.ispis(t, promatracSredstava(), "/sredstva/katalog?uredi=lopata"), "promatrač", "SmijeUrediti=false", "Uredi=;")

	najvisi := 0
	sve, _ := o.svc.SveVrste(ctx)
	for _, v := range sve {
		if v.Grupa == models.GrupaMaterijal && v.Redoslijed > najvisi {
			najvisi = v.Redoslijed
		}
	}
	_, poruka := preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/katalog", url.Values{
		"grupa": {models.GrupaMaterijal}, "naziv": {"Vreće 40x60 cm"}, "jedinica": {"kom"}, "oblici": {"vrece"}}), "success")
	if poruka != "Vrsta „Vreće 40x60 cm“ je upisana." {
		t.Errorf("nova vrsta: %q", poruka)
	}
	var nova *models.VrstaSredstva
	sve, _ = o.svc.SveVrste(ctx)
	for i := range sve {
		if sve[i].Naziv == "Vreće 40x60 cm" {
			nova = &sve[i]
		}
	}
	if nova == nil || nova.Redoslijed != najvisi+1 || len(nova.Oblici) != 2 || !nova.Aktivna {
		t.Fatalf("nova vrsta: %+v (najviši redni broj %d)", nova, najvisi)
	}

	lopata, _ := o.svc.Vrsta(ctx, "lopata")
	if _, poruka := preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/katalog/lopata", url.Values{
		"grupa": {models.GrupaAlat}, "naziv": {"Lopata široka"}, "jedinica": {"kom"}, "aktivna": {"0"}}), "success"); poruka == "" {
		t.Error("izmjena vrste nije prošla")
	}
	poslije, _ := o.svc.Vrsta(ctx, "lopata")
	if poslije.Naziv != "Lopata široka" || poslije.Redoslijed != lopata.Redoslijed || poslije.Aktivna {
		t.Errorf("izmijenjena lopata: %+v (redni broj prije %d)", poslije, lopata.Redoslijed)
	}

	if _, poruka := preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/katalog", url.Values{
		"grupa": {models.GrupaAlat}, "naziv": {"Kanta"}}), "error"); poruka != "vrsta sredstva mora imati jedinicu mjere" {
		t.Errorf("bez jedinice: %q", poruka)
	}
	if _, poruka := preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/katalog/nema/obrisi", url.Values{}), "error"); poruka != "vrsta nije pronađena" {
		t.Errorf("brisanje nepostojeće: %q", poruka)
	}
}

// Potrebe za nabavom: obrazac skladišta za zadanu godinu, upis samo onome
// tko vodi skladište, i zbroj po sektoru.
func TestPotrebeZaNabavom(t *testing.T) {
	o := novaOkolinaSredstava(t)
	osnova := "/sredstva/skladista/" + o.sk.ID + "/potrebe"
	mora(t, o.ispis(t, upravaSredstava(), osnova+"?godina=2027"), "obrazac 2027.", "Godina=2027;", "SmijePisati=true;")
	if ispis := o.ispis(t, upravaSredstava(), osnova); strings.Contains(ispis, "Godina=0;") {
		t.Errorf("bez godine u upitu godina je 0: %s", ispis)
	}

	p, poruka := preusmjerenjeSredstava(t, o.zovi(t, promatracSredstava(), http.MethodPost, osnova,
		url.Values{"godina": {"2027"}, "pt:lopata": {"20"}}), "error")
	if p != osnova || poruka == "" {
		t.Errorf("promatrač upisuje potrebe: %s %q", p, poruka)
	}
	_, poruka = preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, osnova,
		url.Values{"godina": {"2027"}, "pt:lopata": {"20"}, "pn:lopata": {"za novi nasip"}}), "success")
	if poruka != "Potrebe za nabavom u 2027. su upisane." {
		t.Errorf("upis potreba: %q", poruka)
	}

	mora(t, o.ispis(t, promatracSredstava(), "/sredstva/potrebe?sektor=B&godina=2027"), "potrebe sektora", "Godina=2027;", "PotrebeSektora=1;", "Skladista=1;")
	if ispis := o.ispis(t, promatracSredstava(), "/sredstva/potrebe?sektor=B"); strings.Contains(ispis, "Godina=0;") {
		t.Errorf("potrebe sektora bez godine: %s", ispis)
	}
	potrebe, err := o.svc.PotrebeSektora(context.Background(), "B", 2027)
	if err != nil {
		t.Fatal(err)
	}
	d := MtsPageData{PotrebeSektora: potrebe}
	if d.PotrebaU(o.sk.ID, "lopata") != 20 || d.UkupnoPotreba("lopata") != 20 || d.PotrebaU("drugo", "lopata") != 0 {
		t.Errorf("potrebe sektora: %+v", potrebe)
	}
}
