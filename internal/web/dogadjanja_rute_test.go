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
	"gocop/internal/service"
	webassets "gocop/web"
)

// Naslovna pokazuje traku stanja, prečace i zid; Događanja isti zid s
// filtrima i izvozom u Excel.
func TestNaslovnaIDogadjanjaKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "zid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (34, 'B', 'Drava i Dunav', 'COP', 'Osijek')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	journals := repository.NewJournalRepository(baza, rec)
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	mtsRepo := repository.NewMtsRepository(baza, rec)
	_ = mtsRepo.OsigurajKatalog(context.Background())
	zid := service.NewZidService(rec, journals, repository.NewSectionRepository(baza, rec), mtsRepo, userRepo, nil, repository.NewEpisodeRepository(baza, rec))
	mts := service.NewMtsService(mtsRepo, repository.NewSectionRepository(baza, rec), userRepo)

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	dashH := NewDashboardHandler(users, tmpl("dashboard.html"), tmpl("registri.html"))
	dashH.SetZid(func() *service.ZidService { return zid }, nil, func() *service.MtsService { return mts })
	dogH := NewDogadjanjaHandler(func() *service.ZidService { return zid }, users, tmpl("dogadjanja.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", dashH.ShowDashboard)
	mux.HandleFunc("GET /dogadjanja", dogH.ShowDogadjanja)
	mux.HandleFunc("GET /dogadjanja.xlsx", dogH.IzvoziDogadjanja)

	voditelj := &models.User{ID: uuid.New(), FullName: "Voditelj Centra", OrgName: "Hrvatske vode"}
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}, User: *voditelj}
	zovi := func(putanja string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, putanja, nil)
		ctx := context.WithValue(r.Context(), contextKeyUser, voditelj)
		ctx = context.WithValue(ctx, contextKeyPerms, uprava)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(ctx))
		return w
	}
	mora := func(w *httptest.ResponseRecorder, sto string, dijelovi ...string) {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d\n%s", sto, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "template:") {
			t.Errorf("%s: predložak puknuo: %s", sto, w.Body.String()[strings.Index(w.Body.String(), "template:"):])
		}
		for _, d := range dijelovi {
			if !strings.Contains(w.Body.String(), d) {
				t.Errorf("%s: nema %q", sto, d)
			}
		}
	}

	// mirno: bez dnevnika i događanja
	mora(zovi("/"), "prazna naslovna", "Mirno: nema proglašene obrane", "Još nema događanja", "Dnevnik COP-a", "Događanja", "Dokumentacija")

	// dnevnik sa zapisom i skladište s primkom
	ctx := context.Background()
	sad := time.Now().In(models.Zagreb)
	pocetak := time.Date(sad.Year(), sad.Month(), sad.Day(), 0, 0, 0, 0, models.Zagreb)
	j := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "B", Title: "Dnevnik COP-a, proba", Year: pocetak.Year(), StartedAt: &pocetak}
	if err := journals.SaveJournal(ctx, j); err != nil {
		t.Fatal(err)
	}
	js := service.NewJournalService(journals, nil, nil)
	if err := js.DodajZapisCOP(ctx, voditelj, uprava, models.Opseg{Sektor: "B", Podrucja: []int{34}}, j,
		&models.JournalEntry{JournalID: j.ID, Date: pocetak, Kind: models.EntryKindNotice, Text: "Proba zida."}); err != nil {
		t.Fatal(err)
	}
	sk := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Centralno skladište Osijek", Aktivno: true}
	if err := mts.SpremiSkladiste(ctx, uprava, sk); err != nil {
		t.Fatal(err)
	}
	if _, err := mts.Provedi(ctx, voditelj, uprava, service.Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: "lopata", Kolicina: 12, Preuzeo: "Alati d.o.o."}); err != nil {
		t.Fatal(err)
	}
	mora(zovi("/"), "naslovna", "Dnevnik COP-a, proba", "nitko ne dežura", "Proba zida.", "Otvoren dnevnik COP-a", "12 kom Lopata", "Novo skladište")
	mora(zovi("/?modul=sredstva"), "naslovna po modulu", "12 kom Lopata")
	if w := zovi("/?modul=sredstva"); strings.Contains(w.Body.String(), "Proba zida.") {
		t.Error("filtar modula na naslovnoj propušta dnevnik")
	}
	mora(zovi("/dogadjanja?sektor=B"), "događanja", "Kronologija", "Proba zida.", "Primka", "Voditelj Centra")
	mora(zovi("/dogadjanja?modul=dnevnik&osoba=voditelj"), "događanja filtar", "Proba zida.")
	w := zovi("/dogadjanja.xlsx?sektor=B")
	if w.Code != http.StatusOK {
		t.Fatalf("izvoz: %d", w.Code)
	}
	if r, err := procitajXLSX(w.Body.Bytes()); err != nil {
		t.Fatal(err)
	} else {
		var sve []string
		for _, x := range r {
			sve = append(sve, strings.Join(x, "|"))
		}
		list := strings.Join(sve, "\n")
		for _, want := range []string{"KRONOLOGIJA DOGAĐANJA", "|Dnevnik COP-a|Obavijest|Proba zida.|Voditelj Centra|B|", "|Sredstva|Primka|12 kom Lopata · Centralno skladište Osijek · dopremio Alati d.o.o.|"} {
			if !strings.Contains(list, want) {
				t.Errorf("izvoz nema %q\n%s", want, list)
			}
		}
	}
}
