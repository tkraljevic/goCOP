package web

import (
	"context"
	"errors"
	"html/template"
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
	"gocop/internal/prognoza"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Kiša po slivovima: međusliv dobije ime iz registra, letve imena i
// najveći porast iz dnevne prognoze, a upozorenja su samo slivovi iznad
// praga. Bez registra ostaju oznake, a greška izvora ide u poruku.
func TestPodaciKiseSlivova(t *testing.T) {
	ctx := context.Background()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "kisa.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	rec := ledger.New(baza, "test")
	kisRepo := repository.NewKisomjerRepository(baza, rec)
	if err := kisRepo.UpsertSliv(ctx, &models.Sliv{Oznaka: "vuka", Naziv: "Vuka"}); err != nil {
		t.Fatal(err)
	}
	stationRepo := repository.NewStationRepository(baza, rec)
	if err := stationRepo.CreateStation(ctx, &models.Station{ID: uuid.New(), Code: "vukovar", Name: "Vukovar"}); err != nil {
		t.Fatal(err)
	}
	izdano := int64(490000)
	dnevne := map[string][]prognoza.DnevnaIzdana{"vukovar": {{Izdano: izdano, Dan: 0, Vrijednost: 100}, {Izdano: izdano, Dan: 2, Vrijednost: 145}}}
	stanja := []prognoza.StanjeSliva{
		{Sliv: "vuka", Razina: 2, Letve: []string{"vukovar", "nepoznata"}},
		{Sliv: "karasica", Razina: 0},
	}
	s := &Server{kisaSlivova: func(context.Context) ([]prognoza.StanjeSliva, map[string][]prognoza.DnevnaIzdana, error) {
		return stanja, dnevne, nil
	}}
	if p := s.podaciKiseSlivova(ctx); len(p.Slivovi) != 2 || p.Upozorenja[0].Naziv != "" || p.Upozorenja[0].Letve[0].Naziv != "vukovar" {
		t.Errorf("bez registara: %+v", p)
	}
	s.kisomjeri = service.NewKisomjerService(kisRepo)
	s.stationService = service.NewStationService(stationRepo, service.NewSectionService(repository.NewSectionRepository(baza, rec), service.NewSSEBroker()), service.NewSSEBroker())
	p := s.podaciKiseSlivova(ctx)
	if len(p.Upozorenja) != 1 {
		t.Fatalf("upozorenja: %+v", p.Upozorenja)
	}
	u := p.Upozorenja[0]
	if u.Naziv != "Vuka" || u.Klasa != "kisa-narancasto" || u.Oznaka != "jaka kiša" || len(u.Letve) != 2 ||
		u.Letve[0].Naziv != "Vukovar" || u.Letve[0].Cm != 45 || u.Letve[0].Dan != 2 || !u.Letve[0].Ima ||
		u.Letve[1].Naziv != "nepoznata" || u.Letve[1].Ima || !p.Izdano.Equal(time.Unix(izdano*3600, 0)) {
		t.Errorf("sliv u kiši: %+v, izdano %v", u, p.Izdano)
	}
	if p.Slivovi[1].Klasa != "" {
		t.Errorf("sliv bez kiše ima razred: %+v", p.Slivovi[1])
	}

	w := httptest.NewRecorder()
	s.ShowKisaSlivova(w, httptest.NewRequest(http.MethodGet, "/vrijeme/kisa-slivova", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Vuka") {
		t.Errorf("pločica: %d %s", w.Code, w.Body.String())
	}
	s.kisaSlivova = func(context.Context) ([]prognoza.StanjeSliva, map[string][]prognoza.DnevnaIzdana, error) {
		return nil, nil, errors.New("baza zauzeta")
	}
	if p := s.podaciKiseSlivova(ctx); p.Greska == "" || strings.Contains(p.Greska, "baza") {
		t.Errorf("greška izvora: %q", p.Greska)
	}
	s.kisaSlivova = nil
	if p := s.podaciKiseSlivova(ctx); len(p.Slivovi) != 0 || p.Greska != "" {
		t.Errorf("bez izvora: %+v", p)
	}
	w = httptest.NewRecorder()
	s.ShowKisaSlivova(w, httptest.NewRequest(http.MethodGet, "/vrijeme/kisa-slivova", nil))
	if w.Code != http.StatusNoContent {
		t.Errorf("pločica bez izvora: %d", w.Code)
	}
}

// Dio stranice koji se ne da iscrtati je greška poslužitelja, bez pola
// pločice
func TestPosaljiDioGreskaPredloska(t *testing.T) {
	w := httptest.NewRecorder()
	posaljiDio(w, template.Must(template.New("x").Parse(`pola{{.Nema.Polje}}`)), struct{}{})
	if w.Code != http.StatusInternalServerError || strings.HasPrefix(w.Body.String(), "pola") {
		t.Errorf("greška predloška: %d %q", w.Code, w.Body.String())
	}
}

// Brojke za Naslovnu dolaze iz predmemorije, a brojanje po desetljećima
// ide u pozadini i stigne pri idućem čitanju
func TestBrojkePodatakaIzPredmemorije(t *testing.T) {
	s := posluziteljSaSvime(t)
	ctx := context.Background()
	prve := s.brojkePodataka(ctx)
	if prve == nil || prve.Zapisa == 0 {
		t.Fatalf("brojke: %+v", prve)
	}
	if druge := s.brojkePodataka(ctx); druge.Izracunato != prve.Izracunato {
		t.Error("drugo čitanje nije iz predmemorije")
	}
	rok := time.Now().Add(10 * time.Second)
	for time.Now().Before(rok) {
		s.brojkeStanje.mu.Lock()
		gotovo := !s.brojkeStanje.broji && !s.brojkeStanje.desetljecaKad.IsZero()
		s.brojkeStanje.mu.Unlock()
		if gotovo {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.brojkeStanje.mu.Lock()
	kad := s.brojkeStanje.desetljecaKad
	s.brojkeStanje.mu.Unlock()
	if kad.IsZero() {
		t.Error("brojanje po desetljećima nije završilo")
	}
	if b := s.brojkePodataka(ctx); b.Zapisa != prve.Zapisa {
		t.Errorf("brojke nakon brojanja: %+v", b)
	}
}
