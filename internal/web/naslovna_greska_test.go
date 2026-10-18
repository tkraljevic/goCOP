package web

import (
	"bytes"
	"errors"
	"html/template"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// zaglavljaOdgovora broji koliko je puta odgovor pokušao poslati zaglavlje:
// prvi Write ga pošalje sam, a svaki WriteHeader poslije toga net/http
// odbije porukom „superfluous response.WriteHeader call”
type zaglavljaOdgovora struct {
	*httptest.ResponseRecorder
	poslano, suvisno int
}

func (z *zaglavljaOdgovora) WriteHeader(kod int) {
	if z.poslano > 0 {
		z.suvisno++
		return
	}
	z.poslano++
	z.ResponseRecorder.WriteHeader(kod)
}

func (z *zaglavljaOdgovora) Write(b []byte) (int, error) {
	if z.poslano == 0 {
		z.poslano++
	}
	return z.ResponseRecorder.Write(b)
}

// Predložak naslovne koji padne usred iscrtavanja: preglednik dobije grešku
// 500, a ne pola stranice s porukom na dnu, zaglavlje ide jednom (u
// dnevniku kućnog servera stajalo je „superfluous response.WriteHeader
// call” iz ShowDashboard), a greška se vidi u dnevniku.
func TestNaslovnaKadPredlozakPadne(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "naslovna.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	userRepo := repository.NewUserRepository(baza, ledger.New(baza, "test"))
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	tmpl := template.Must(template.New("dashboard.html").
		Funcs(template.FuncMap{"padni": func() (string, error) { return "", errors.New("namjerno pao") }}).
		Parse(`<p>Početak naslovne</p>{{padni}}<p>kraj</p>`))
	var dnevnik bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&dnevnik)
	defer log.SetOutput(stari)

	w := &zaglavljaOdgovora{ResponseRecorder: httptest.NewRecorder()}
	NewDashboardHandler(users, tmpl, nil).ShowDashboard(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if w.Code != http.StatusInternalServerError || w.suvisno != 0 {
		t.Errorf("kod %d, suvišnih zaglavlja %d; želim 500 i nijedno", w.Code, w.suvisno)
	}
	if s := w.Body.String(); strings.Contains(s, "Početak naslovne") || !strings.Contains(s, "namjerno pao") {
		t.Errorf("tijelo odgovora: %q", s)
	}
	if !strings.Contains(dnevnik.String(), "početna stranica: ") {
		t.Errorf("dnevnik: %q", dnevnik.String())
	}
}
