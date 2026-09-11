package web

import (
	"context"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
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

// Put dnevnika COP-a kroz prave rute, kao prijavljeni voditelj centra — isto
// što bi se kliknulo u pregledniku: obrazac, otvaranje, dnevnik s obrascem
// za zapis, zapis, storno. Prijave nema: korisnik se stavlja u kontekst kao
// što to radi authMiddleware poslije provjere sesije.
func TestDnevnikCOPKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "rute.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (15, 'B', 'Vuka', 'VGI Vuka', 'Osijek')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	journals := service.NewJournalService(repository.NewJournalRepository(baza, rec), nil, nil)

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
		tmpl("dnevnik_cop.html"), tmpl("dnevnik_cop_form.html"), tmpl("dnevnik_list.html"), nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /dnevnici/popis", h.ShowJournals)
	mux.HandleFunc("GET /dnevnici/novi-cop", h.ShowCOPJournalForm)
	mux.HandleFunc("POST /dnevnici/novi-cop", h.HandleSaveCOPJournal)
	mux.HandleFunc("GET /dnevnici/{id}", h.ShowJournal)
	mux.HandleFunc("POST /dnevnici/{id}/zapisi", h.HandleAddCOPEntry)
	mux.HandleFunc("POST /dnevnici/{id}/upisi/{entry}/storno", h.HandleVoidEntry)

	voditelj := &models.User{ID: uuid.New(), FullName: "Voditelj Centra"}
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}}
	zovi := func(metoda, putanja string, obrazac url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if obrazac != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(obrazac.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(metoda, putanja, nil)
		}
		ctx := context.WithValue(r.Context(), contextKeyUser, voditelj)
		ctx = context.WithValue(ctx, contextKeyPerms, uprava)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(ctx))
		return w
	}
	mora := func(w *httptest.ResponseRecorder, kod int, sto string, dijelovi ...string) {
		t.Helper()
		if w.Code != kod {
			t.Fatalf("%s: %d, a mora biti %d\n%s", sto, w.Code, kod, w.Body.String())
		}
		for _, d := range dijelovi {
			if !strings.Contains(w.Body.String(), d) {
				t.Errorf("%s: nema %q", sto, d)
			}
		}
	}

	// Popis nudi gumb, obrazac nudi centar.
	mora(zovi(http.MethodGet, "/dnevnici/popis?vrsta=OBRANA", nil), http.StatusOK, "popis", `href="/dnevnici/novi-cop"`, "Još nema nijednog dnevnika COP-a")
	mora(zovi(http.MethodGet, "/dnevnici/novi-cop", nil), http.StatusOK, "obrazac", `<option value="B" selected>COP Osijek`)

	// Otvaranje vodi na dnevnik.
	w := zovi(http.MethodPost, "/dnevnici/novi-cop", url.Values{"centar": {"B"}, "started_at": {"2026-09-11"}})
	mora(w, http.StatusSeeOther, "otvaranje")
	kamo := w.Header().Get("Location")
	if !strings.HasPrefix(kamo, "/dnevnici/") || strings.Contains(kamo, "error=") {
		t.Fatalf("otvaranje vodi na %q", kamo)
	}
	dnevnik := strings.SplitN(strings.TrimPrefix(kamo, "/dnevnici/"), "?", 2)[0]
	mora(zovi(http.MethodGet, "/dnevnici/"+dnevnik, nil), http.StatusOK, "dnevnik",
		"Dnevnik COP-a, 2026.", "COP Osijek", `id="novi-zapis"`, "još nema zapisa", "otvoren")

	// Zapis ulazi i vidi se s vremenom i onim tko je javio.
	w = zovi(http.MethodPost, "/dnevnici/"+dnevnik+"/zapisi", url.Values{
		"date": {"2026-09-11"}, "time": {"07:15"}, "kind": {models.EntryKindReport},
		"reported_by": {"Sa porte"}, "text": {"vodostaj Batina u 07:00 +551"}})
	mora(w, http.StatusSeeOther, "zapis")
	if l := w.Header().Get("Location"); !strings.Contains(l, "success=") {
		t.Fatalf("zapis nije prošao: %s", l)
	}
	w = zovi(http.MethodGet, "/dnevnici/"+dnevnik, nil)
	mora(w, http.StatusOK, "dnevnik sa zapisom", "07:15", "Sa porte", "vodostaj Batina u 07:00 +551", "upisao Voditelj Centra", "/storno")

	// Storno: zapis ostaje, prekrižen, s razlogom.
	m := regexp.MustCompile(`/upisi/([^/]+)/storno`).FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatal("na stranici nema gumba za storno")
	}
	w = zovi(http.MethodPost, "/dnevnici/"+dnevnik+"/upisi/"+m[1]+"/storno", url.Values{"reason": {"krivo očitano"}})
	mora(w, http.StatusSeeOther, "storno")
	mora(zovi(http.MethodGet, "/dnevnici/"+dnevnik, nil), http.StatusOK, "poslije storna",
		"zapis-storniran", "storniran: krivo očitano", "vodostaj Batina u 07:00 +551")

	// Građevinska vrsta u zapisnik dežurstva ne ulazi.
	w = zovi(http.MethodPost, "/dnevnici/"+dnevnik+"/zapisi", url.Values{
		"date": {"2026-09-11"}, "kind": {models.EntryKindWork}, "text": {"košnja"}})
	if l := w.Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("rad izvođača ušao u dnevnik COP-a: %s", l)
	}
}
