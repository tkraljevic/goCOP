package web

import (
	"context"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// Poslovođa izvođača u Dnevnicima usluga A.02 i A.03 vidi samo svoj
// dnevnik: dnevnik svoje firme na branjenom području svoje dužnosti, a ne
// dnevnike drugih firmi, ni na drugim područjima ni na svom, ni na popisu
// (ni kad područje zada u adresi), ni izravnom adresom (Odluke (8. 10.
// 2026.), pitanje 1). Dnevnik je firmin kad naslovnica kao izvođača navodi
// firmu s računa poslovođe ili njega kao voditelja usluga; dnevnik bez
// izvođača na naslovnici nije ničiji. Rukovoditelj područja čita dnevnike
// usluga kao i dosad. Podaci su izmišljeni.
func TestPoslovodjaVidiSamoSvojDnevnikUsluge(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "usluge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
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
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	journalRepo := repository.NewJournalRepository(baza, rec)
	journals := service.NewJournalService(journalRepo, nil, nil)
	ctx := context.Background()
	p, prvo, drugo := "P", 1, 2
	osoba := func(korisnik, ime string, d models.Duty) *models.User {
		t.Helper()
		u := &models.User{ID: uuid.New(), Username: korisnik, FullName: ime, IsActive: true}
		d.SectorID, d.IsPrimary = &p, true
		if err := userRepo.CreateUser(u, &d); err != nil {
			t.Fatal(err)
		}
		return u
	}
	poslovodja := osoba("mmarkic", "Marko Markić", models.Duty{Title: "Poslovođa Primjer d.o.o.", Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, AreaID: &prvo})
	if _, err := baza.Exec(`UPDATE users SET org_name = 'Primjer d.o.o.' WHERE id = ?`, poslovodja.ID.String()); err != nil {
		t.Fatal(err)
	}
	rukovoditelj := osoba("iivic", "Ivo Ivić", models.Duty{Title: "Rukovoditelj BP 1", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &prvo})
	dnevnik := func(podrucje int, vrsta, naslov, izvodjac, voditelj string) *models.Journal {
		t.Helper()
		j := &models.Journal{AreaID: podrucje, Kind: vrsta, Title: naslov, Year: 2026, Contractor: izvodjac, ContractorLead: voditelj}
		if err := journalRepo.SaveJournal(ctx, j); err != nil {
			t.Fatal(err)
		}
		return j
	}
	svoj := dnevnik(prvo, models.JournalKindMaintenanceA02, "A.02 Primjer d.o.o.", "PRIMJER d.o.o., Primjerska 1, Primjerica", "")
	tudji := dnevnik(drugo, models.JournalKindMaintenanceA02, "A.02 Probni radovi d.o.o.", "Probni radovi d.o.o.", "")
	// na njegovu području: dnevnik druge firme, dnevnik u kojem je on
	// voditelj usluga i dnevnik bez izvođača na naslovnici
	drugaFirma := dnevnik(prvo, models.JournalKindMaintenanceA03, "A.03 Drugi radovi d.o.o.", "Drugi radovi d.o.o.", "Ivo Ivić")
	voditelj := dnevnik(prvo, models.JournalKindMaintenanceA03, "A.03 Treći radovi", "Treći radovi d.o.o.", "Marko Markić, ing.")
	bezIzvodjaca := dnevnik(prvo, models.JournalKindMaintenanceA03, "A.03 Bez naslovnice", "", "")

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewJournalsHandler(journals, users, nil, nil, nil,
		tmpl("dnevnici_izbor.html"), tmpl("dnevnici.html"), tmpl("dnevnik_form.html"), tmpl("dnevnik.html"),
		tmpl("dnevnik_cop.html"), tmpl("dnevnik_cop_form.html"), tmpl("dnevnik_list.html"), nil, tmpl("dnevnik_obracun.html"), tmpl("dnevnik_dezurstva.html"), tmpl("dnevnik_iors.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /dnevnici/popis", h.ShowJournals)
	mux.HandleFunc("GET /dnevnici/{id}", h.ShowJournal)
	zovi := func(u *models.User, putanja string) *httptest.ResponseRecorder {
		t.Helper()
		cijeli, err := users.GetUserByID(u.ID)
		if err != nil || cijeli == nil {
			t.Fatalf("%s: %v", u.FullName, err)
		}
		r := httptest.NewRequest(http.MethodGet, putanja, nil)
		c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, cijeli), contextKeyPerms, models.NewUserPermissions(*cijeli))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
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

// Izbor područja na popisu dnevnika usluga: bez ijednog vidljivog područja
// nema ni odabranog.
func TestVidljivaPodrucjaBezIjednog(t *testing.T) {
	h := &JournalsHandler{journals: service.NewJournalService(nil, nil, nil)}
	podrucja := []models.Area{{ID: 1, SectorID: "P"}}
	if a, sva := h.vidljivaPodrucja(nil, &podrucja[0], podrucja); a != nil || len(sva) != 0 {
		t.Errorf("bez ovlasti: %+v %+v", a, sva)
	}
}
