package web

import (
	"context"
	"database/sql"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
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
	webassets "gocop/web"
)

// uslugeProba je baza sa sektorom P i područjima 1 i 2 te rutama dnevnika
// usluga, za provjeru tko koji dnevnik vidi
type uslugeProba struct {
	baza        *sql.DB
	userRepo    *repository.UserRepository
	users       *service.UserService
	journalRepo *repository.JournalRepository
	mux         *http.ServeMux
}

func novaUslugeProba(t *testing.T) *uslugeProba {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "usluge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerica', 'COP Primjerica')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Primjerica', 'VGI Primjerica', 'Primjerica'),
			(2, 'P', 'Probnica', 'VGI Probnica', 'Probnica')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	p := &uslugeProba{baza: baza, userRepo: repository.NewUserRepository(baza, rec), journalRepo: repository.NewJournalRepository(baza, rec)}
	p.users = service.NewUserService(p.userRepo, service.NewAuthService(p.userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewJournalsHandler(service.NewJournalService(p.journalRepo, nil, nil), p.users, nil, nil, nil,
		tmpl("dnevnici_izbor.html"), tmpl("dnevnici.html"), tmpl("dnevnik_form.html"), tmpl("dnevnik.html"),
		tmpl("dnevnik_cop.html"), tmpl("dnevnik_cop_form.html"), tmpl("dnevnik_list.html"), nil, tmpl("dnevnik_obracun.html"), tmpl("dnevnik_dezurstva.html"), tmpl("dnevnik_iors.html"))
	p.mux = http.NewServeMux()
	p.mux.HandleFunc("GET /dnevnici/popis", h.ShowJournals)
	p.mux.HandleFunc("GET /dnevnici/{id}", h.ShowJournal)
	p.mux.HandleFunc("POST /dnevnici/{id}/listovi", h.HandleOpenSheet)
	return p
}

// osoba upisuje osobu s jednom dužnošću u sektoru P i firmom na računu
func (p *uslugeProba) osoba(t *testing.T, korisnik, ime, firma string, d models.Duty) *models.User {
	t.Helper()
	sektor := "P"
	u := &models.User{ID: uuid.New(), Username: korisnik, FullName: ime, OrgName: firma, IsActive: true}
	d.SectorID, d.IsPrimary = &sektor, true
	if err := p.userRepo.CreateUser(u, &d); err != nil {
		t.Fatal(err)
	}
	if _, err := p.baza.Exec(`UPDATE users SET org_name = ? WHERE id = ?`, firma, u.ID.String()); err != nil {
		t.Fatal(err)
	}
	return u
}

// poslovodja je voditelj usluga / poslovođa izvođača na području 1
func (p *uslugeProba) poslovodja(t *testing.T, korisnik, ime, firma string) *models.User {
	t.Helper()
	prvo := 1
	return p.osoba(t, korisnik, ime, firma, models.Duty{Title: "Poslovođa", Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, AreaID: &prvo})
}

// dnevnik upisuje dnevnik usluge s naslovnicom; otvorio je tko je zadan
func (p *uslugeProba) dnevnik(t *testing.T, podrucje int, vrsta, naslov, izvodjac, voditelj string, otvorio *models.User) *models.Journal {
	t.Helper()
	j := &models.Journal{AreaID: podrucje, Kind: vrsta, Title: naslov, Year: 2026, Contractor: izvodjac, ContractorLead: voditelj}
	if otvorio != nil {
		j.CreatedBy = otvorio.ID.String()
	}
	if err := p.journalRepo.SaveJournal(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	return j
}

// zovi šalje zahtjev u ime osobe
func (p *uslugeProba) zovi(t *testing.T, u *models.User, metoda, putanja string) *httptest.ResponseRecorder {
	t.Helper()
	cijeli, err := p.users.GetUserByID(u.ID)
	if err != nil || cijeli == nil {
		t.Fatalf("%s: %v", u.FullName, err)
	}
	r := httptest.NewRequest(metoda, putanja, nil)
	if metoda == http.MethodPost {
		r = httptest.NewRequest(metoda, putanja, strings.NewReader("date=2026-10-09"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, cijeli), contextKeyPerms, models.NewUserPermissions(*cijeli))
	w := httptest.NewRecorder()
	p.mux.ServeHTTP(w, r.WithContext(c))
	return w
}

// Poslovođa izvođača u Dnevnicima usluga A.02 i A.03 vidi samo svoj
// dnevnik: dnevnik svoje firme na branjenom području svoje dužnosti, a ne
// dnevnike drugih firmi, ni na drugim područjima ni na svom, ni na popisu
// (ni kad područje zada u adresi), ni izravnom adresom (Odluke (8. 10.
// 2026.), pitanje 1). Dnevnik je firmin kad naslovnica kao izvođača navodi
// firmu s računa poslovođe ili njega kao voditelja usluga; dnevnik bez
// izvođača na naslovnici u kojem nitko nije pisao nije ničiji. Rukovoditelj područja čita dnevnike
// usluga kao i dosad. Podaci su izmišljeni.
func TestPoslovodjaVidiSamoSvojDnevnikUsluge(t *testing.T) {
	p := novaUslugeProba(t)
	prvo, drugo := 1, 2
	poslovodja := p.poslovodja(t, "mmarkic", "Marko Markić", "Primjer d.o.o.")
	rukovoditelj := p.osoba(t, "iivic", "Ivo Ivić", "", models.Duty{Title: "Rukovoditelj BP 1", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &prvo})
	svoj := p.dnevnik(t, prvo, models.JournalKindMaintenanceA02, "A.02 Primjer d.o.o.", "PRIMJER d.o.o., Primjerska 1, Primjerica", "", nil)
	tudji := p.dnevnik(t, drugo, models.JournalKindMaintenanceA02, "A.02 Probni radovi d.o.o.", "Probni radovi d.o.o.", "", nil)
	// na njegovu području: dnevnik druge firme, dnevnik u kojem je on
	// voditelj usluga i dnevnik bez izvođača na naslovnici
	drugaFirma := p.dnevnik(t, prvo, models.JournalKindMaintenanceA03, "A.03 Drugi radovi d.o.o.", "Drugi radovi d.o.o.", "Ivo Ivić", nil)
	voditelj := p.dnevnik(t, prvo, models.JournalKindMaintenanceA03, "A.03 Treći radovi", "Treći radovi d.o.o.", "Marko Markić, ing.", nil)
	bezIzvodjaca := p.dnevnik(t, prvo, models.JournalKindMaintenanceA03, "A.03 Bez naslovnice", "", "", nil)
	zovi := func(u *models.User, putanja string) *httptest.ResponseRecorder {
		t.Helper()
		return p.zovi(t, u, http.MethodGet, putanja)
	}

	for _, putanja := range []string{"/dnevnici/popis?vrsta=ODRZAVANJE_A02", "/dnevnici/popis?vrsta=ODRZAVANJE_A02&area=2"} {
		w := zovi(poslovodja, putanja)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "A.02 Primjer d.o.o.") {
			t.Errorf("%s: poslovođa ne vidi svoj dnevnik: %d", putanja, w.Code)
		}
		if strings.Contains(w.Body.String(), "Probni radovi") || strings.Contains(w.Body.String(), "Probnica") {
			t.Errorf("%s: poslovođa vidi dnevnik ili područje druge firme", putanja)
		}
	}
	if w := zovi(poslovodja, "/dnevnici/"+svoj.ID); w.Code != http.StatusOK {
		t.Errorf("poslovođa na svom dnevniku: %d", w.Code)
	}
	if w := zovi(poslovodja, "/dnevnici/"+tudji.ID); w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "Probni radovi") {
		t.Errorf("poslovođa na dnevniku druge firme: %d", w.Code)
	}
	// na svom području: druga firma i dnevnik bez izvođača zabranjeni, a
	// dnevnik u kojem je voditelj usluga njegov
	popisA03 := zovi(poslovodja, "/dnevnici/popis?vrsta=ODRZAVANJE_A03").Body.String()
	if strings.Contains(popisA03, "Drugi radovi") || strings.Contains(popisA03, "Bez naslovnice") || !strings.Contains(popisA03, "A.03 Treći radovi") {
		t.Error("popis A.03 na području poslovođe: vidi tuđe ili ne vidi svoj dnevnik")
	}
	for _, j := range []*models.Journal{drugaFirma, bezIzvodjaca} {
		if w := zovi(poslovodja, "/dnevnici/"+j.ID); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "svoje firme") {
			t.Errorf("poslovođa na dnevniku %s: %d %.120s", j.Title, w.Code, w.Body.String())
		}
	}
	if w := zovi(poslovodja, "/dnevnici/"+voditelj.ID); w.Code != http.StatusOK {
		t.Errorf("poslovođa na dnevniku u kojem je voditelj usluga: %d", w.Code)
	}
	if w := zovi(rukovoditelj, "/dnevnici/"+drugaFirma.ID); w.Code != http.StatusOK {
		t.Errorf("rukovoditelj na dnevniku druge firme svog područja: %d", w.Code)
	}
	// rukovoditelj područja 1 čita dnevnike usluga kao i dosad
	if w := zovi(rukovoditelj, "/dnevnici/"+tudji.ID); w.Code != http.StatusOK {
		t.Errorf("rukovoditelj na dnevniku područja 2: %d", w.Code)
	}
	if w := zovi(rukovoditelj, "/dnevnici/popis?vrsta=ODRZAVANJE_A02&area=2"); !strings.Contains(w.Body.String(), "Probni radovi") {
		t.Errorf("rukovoditelj na popisu područja 2: %d", w.Code)
	}
}

// Dnevnik u kojem poslovođa piše njegov je i kad naslovnica ne navodi
// firmu s njegova računa ni njega (račun bez firme, naslovnica bez
// izvođača ili s nazivom napisanim drukčije): otvorio je dnevnik, list ili
// upisao zapis. Takav dnevnik i dalje vidi na popisu i otvara, i u njemu
// otvara novi list. Poslovođa koji u dnevniku nije pisao i ne navodi ga
// naslovnica ne vidi ga. Podaci su izmišljeni.
func TestPoslovodjaZadrzavaDnevnikUKojemPise(t *testing.T) {
	p := novaUslugeProba(t)
	ctx := context.Background()
	prvo := 1
	pero := p.poslovodja(t, "pperic", "Pero Perić", "")
	ana := p.poslovodja(t, "aanic", "Ana Anić", "Primjer d.d.")
	iva := p.poslovodja(t, "iivic", "Iva Ivić", "Treći radovi d.o.o.")
	listom := p.dnevnik(t, prvo, models.JournalKindMaintenanceA02, "A.02 Kanali", "PRIMJER DD Primjerica", "", nil)
	if err := p.journalRepo.SaveSheet(ctx, &models.JournalSheet{JournalID: listom.ID, Date: time.Date(2026, 10, 8, 0, 0, 0, 0, models.Zagreb), CreatedBy: pero.ID.String()}); err != nil {
		t.Fatal(err)
	}
	upisom := p.dnevnik(t, prvo, models.JournalKindMaintenanceA03, "A.03 Kanali", "", "", nil)
	if err := p.journalRepo.SaveEntry(ctx, &models.JournalEntry{JournalID: upisom.ID, Date: time.Date(2026, 10, 8, 0, 0, 0, 0, models.Zagreb), Kind: models.EntryKindWork, Text: "košnja", UserID: ana.ID.String(), UserName: ana.FullName}); err != nil {
		t.Fatal(err)
	}
	otvorio := p.dnevnik(t, prvo, models.JournalKindMaintenanceA03, "A.03 Odvodnja", "", "", pero)

	for _, x := range []struct {
		u     *models.User
		j     *models.Journal
		vrsta string
		vidi  bool
	}{
		{pero, listom, "ODRZAVANJE_A02", true},
		{ana, listom, "ODRZAVANJE_A02", true}, // naslovnica navodi njezinu firmu
		{iva, listom, "ODRZAVANJE_A02", false},
		{ana, upisom, "ODRZAVANJE_A03", true},
		{pero, upisom, "ODRZAVANJE_A03", false},
		{pero, otvorio, "ODRZAVANJE_A03", true},
		{iva, otvorio, "ODRZAVANJE_A03", false},
	} {
		w := p.zovi(t, x.u, http.MethodGet, "/dnevnici/"+x.j.ID)
		if (w.Code == http.StatusOK) != x.vidi || (!x.vidi && w.Code != http.StatusForbidden) {
			t.Errorf("%s na dnevniku %s: %d, očekivano da vidi %v", x.u.FullName, x.j.Title, w.Code, x.vidi)
		}
		popis := p.zovi(t, x.u, http.MethodGet, "/dnevnici/popis?vrsta="+x.vrsta).Body.String()
		if strings.Contains(popis, x.j.Title) != x.vidi {
			t.Errorf("%s na popisu: dnevnik %s vidljiv %v, očekivano %v", x.u.FullName, x.j.Title, !x.vidi, x.vidi)
		}
	}
	if w := p.zovi(t, pero, http.MethodPost, "/dnevnici/"+listom.ID+"/listovi"); w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "/listovi/") {
		t.Errorf("poslovođa ne otvara list u dnevniku u kojem piše: %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := p.zovi(t, iva, http.MethodPost, "/dnevnici/"+listom.ID+"/listovi"); w.Code != http.StatusForbidden {
		t.Errorf("poslovođa druge firme otvara list: %d", w.Code)
	}
}

// Izbor područja na popisu dnevnika usluga: bez ijednog vidljivog područja
// nema ni odabranog.
func TestVidljivaPodrucjaBezIjednog(t *testing.T) {
	h := &JournalsHandler{journals: service.NewJournalService(nil, nil, nil)}
	podrucja := []models.Area{{ID: 1, SectorID: "P"}}
	if a, sva := h.vidljivaPodrucja(nil, &podrucja[0], podrucja); a != nil || len(sva) != 0 {
		t.Errorf("bez ovlasti: %+v %+v", a, sva)
	}
}
