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
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
	"gocop/internal/service"
	webassets "gocop/web"
)

// probaPrijava je čvor s prijavama s terena za provjeru pojedinačne prijave
// kroz rutu: sektor P, područje 1 s dionicama P.1.1 i P.1.2. Podaci su
// izmišljeni.
type probaPrijava struct {
	userRepo *repository.UserRepository
	users    *service.UserService
	repo     *repository.PrijavaRepository
	prijave  *service.PrijavaService
	mux      *http.ServeMux
	exec     func(q string, args ...any)
}

func novaProbaPrijava(t *testing.T) *probaPrijava {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "prijave.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	spremiste, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	prijasnje := repository.Spremiste()
	repository.SetSpremiste(spremiste)
	t.Cleanup(func() {
		repository.SetSpremiste(prijasnje)
		if err := spremiste.Zatvori(); err != nil {
			t.Error(err)
		}
	})
	pr := &probaPrijava{exec: func(q string, args ...any) {
		t.Helper()
		if _, err := baza.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}}
	pr.exec(`INSERT INTO sectors (id, name, vgo_name, center_cop, address, phone, email) VALUES ('P', 'Sektor P', 'VGO Primjerica', 'COP Primjerica', 'Primjerska 1', '01/000-000', 'cop@primjer.hr')`)
	pr.exec(`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Primjerica', 'VGI Primjerica', 'Primjerica')`)
	pr.exec(`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES
		('P.1.1', 1, 'P', 'rijeka Primjerica', '2026-01-01', '2026-01-01'), ('P.1.2', 1, 'P', 'potok Prvi', '2026-01-01', '2026-01-01')`)
	rec := ledger.New(baza, "cop-primjer")
	pr.userRepo = repository.NewUserRepository(baza, rec)
	pr.users = service.NewUserService(pr.userRepo, service.NewAuthService(pr.userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	orgRepo := repository.NewOrgRepository(baza, rec)
	vod := service.NewVodocuvarService(repository.NewVodocuvarRepository(baza, rec), pr.users, "cop-primjer")
	vod.SetOrg(orgRepo)
	vod.SetWeather(nil)
	pr.repo = repository.NewPrijavaRepository(baza, rec)
	pr.prijave = service.NewPrijavaService(pr.repo, pr.users, vod, "cop-primjer")

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	vodH := NewVodocuvarHandler(func() *service.VodocuvarService { return vod }, pr.users, func() *repository.OrgRepository { return orgRepo }, tmpl("vodocuvar.html"), tmpl("vodocuvar_list.html"))
	vodH.SetOpcije(func(context.Context) models.Opcije { return models.Opcije{} })
	h := NewPrijaveHandler(func() *service.PrijavaService { return pr.prijave }, pr.users, vodH, tmpl("prijave.html"), tmpl("prijava_form.html"), tmpl("prijava.html"))
	pr.mux = http.NewServeMux()
	pr.mux.HandleFunc("GET /prijave/{id}", h.ShowPrijava)
	pr.mux.HandleFunc("GET /prijave/{id}/prijava.pdf", h.IzvoziPDF)
	return pr
}

// osoba upisuje osobu s jednom dužnošću u sektoru P
func (pr *probaPrijava) osoba(t *testing.T, korisnik, ime string, d models.Duty) *models.User {
	t.Helper()
	u := &models.User{ID: uuid.New(), Username: korisnik, FullName: ime, IsActive: true}
	p := "P"
	d.SectorID, d.IsPrimary = &p, true
	if err := pr.userRepo.CreateUser(u, &d); err != nil {
		t.Fatal(err)
	}
	return u
}

// objavljena upisuje objavljenu prijavu vodočuvara v na području 1
func (pr *probaPrijava) objavljena(t *testing.T, v *models.User, naslov, dionica string) *models.PrijavaSTerena {
	t.Helper()
	sad := time.Now()
	p := &models.PrijavaSTerena{UserID: v.ID.String(), Ime: v.FullName, Sektor: "P", AreaID: 1, Vrsta: models.PrijavaObavijest,
		Naslov: naslov, Opis: "opis", Datum: sad, DionicaCode: dionica, Status: models.PrijavaObjavljena, ObjavljenoAt: &sad, Broj: 1, Godina: sad.Year(), Rekonstrukcija: true}
	if err := pr.repo.Save(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

// get otvara putanju kao osoba u, s pravima kako ih čvor sada čita
func (pr *probaPrijava) get(t *testing.T, u *models.User, putanja string) *httptest.ResponseRecorder {
	t.Helper()
	cijeli, err := pr.users.GetUserByID(u.ID)
	if err != nil || cijeli == nil {
		t.Fatalf("%s: %v", u.FullName, err)
	}
	r := httptest.NewRequest(http.MethodGet, putanja, nil)
	c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, cijeli), contextKeyPerms, models.NewUserPermissions(*cijeli))
	w := httptest.NewRecorder()
	pr.mux.ServeHTTP(w, r.WithContext(c))
	return w
}

// Vodočuvar uvijek otvara svoju prijavu, objavljenu i nacrt, i kad mu je
// zaduženje isteklo ili je opozvano: vlastita knjiga ostaje njegova
// (Odluke (8. 10. 2026.), pitanje 1), kao i prije. Zabranu s objašnjenjem
// dobiva samo tko u dnevnike vodočuvara ne gleda, a tuđu prijavu otvara.
func TestAutorOtvaraSvojuPrijavuBezZaduzenja(t *testing.T) {
	pr := novaProbaPrijava(t)
	bp := 1
	pero := pr.osoba(t, "pperic", "Pero Perić", models.Duty{Title: "Vodočuvar Primjerica", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, AreaID: &bp, SectionCodes: "P.1.1"})
	poslovodja := pr.osoba(t, "mmarkic", "Marko Markić", models.Duty{Title: "Poslovođa izvođača", Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, AreaID: &bp})
	objavljena := pr.objavljena(t, pero, "Oštećena rampa na nasipu", "P.1.1")
	cijeli, _ := pr.users.GetUserByID(pero.ID)
	nacrt, err := pr.prijave.Spremi(context.Background(), cijeli, "", service.UnosPrijave{Vrsta: models.PrijavaObavijest, Naslov: "Odron uz potok", Opis: "opis", Datum: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	otvara := func(stanje string) {
		t.Helper()
		if w := pr.get(t, pero, "/prijave/"+objavljena.ID); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Oštećena rampa") {
			t.Errorf("%s: vodočuvar na svojoj objavljenoj prijavi: %d %.200s", stanje, w.Code, w.Body.String())
		}
		if w := pr.get(t, pero, "/prijave/"+nacrt.ID); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Odron uz potok") {
			t.Errorf("%s: vodočuvar na svom nacrtu: %d %.200s", stanje, w.Code, w.Body.String())
		}
		if w := pr.get(t, pero, "/prijave/"+objavljena.ID+"/prijava.pdf"); w.Code != http.StatusOK {
			t.Errorf("%s: vodočuvar na PDF-u svoje prijave: %d", stanje, w.Code)
		}
	}
	otvara("valjano zaduženje")

	// zaduženje je isteklo (jučer): u dnevnike više ne gleda, ali svoje prijave otvara
	pr.exec(`UPDATE duties SET expires_at = ? WHERE user_id = ?`, time.Now().AddDate(0, 0, -1).UTC(), pero.ID.String())
	otvara("isteklo zaduženje")
	// zaduženje je opozvano
	pr.exec(`UPDATE duties SET expires_at = NULL, is_active = 0 WHERE user_id = ?`, pero.ID.String())
	otvara("opozvano zaduženje")

	// poslovođa izvođača tuđu prijavu ne otvara: zabrana s objašnjenjem
	if w := pr.get(t, poslovodja, "/prijave/"+objavljena.ID); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "izvođači dnevnike usluga") || strings.Contains(w.Body.String(), "Oštećena rampa") {
		t.Errorf("poslovođa na tuđoj prijavi: %d %.200s", w.Code, w.Body.String())
	}
	// nepostojeća prijava je nepostojeća
	if w := pr.get(t, pero, "/prijave/"+uuid.NewString()); w.Code != http.StatusNotFound {
		t.Errorf("nepostojeća prijava: %d", w.Code)
	}
}
