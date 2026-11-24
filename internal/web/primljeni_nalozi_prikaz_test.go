package web

import (
	"context"
	"errors"
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
)

// Nalozi izvođačima izvan dnevnika (Odluke (22. 11. 2026.)): kartica
// „Nalozi koje sam dao” na Naslovnoj i odjeljak „Radovi izvođača na
// dionici” na stranici dionice. Podaci su izmišljeni.

// primljeniNalog je primljeni nalog firme Primjer d.o.o. koji je dao Ivo Ivić
func primljeniNalog(dan time.Time, ceka bool) models.NalogIzvodjacu {
	e := models.JournalEntry{ID: "n1", JournalID: "j1", SheetID: "l1", Number: 3, Date: dan, Kind: models.EntryKindTask, Side: models.EntrySideContractor,
		Status: models.TaskOpen, SectionCode: "P.2.1", Text: "postaviti vreće", Davatelj: "Ivo Ivić", UlogaDavatelja: "Rukovoditelj dionice", DavateljID: "d1"}
	if !ceka {
		e.Odgovor = models.OdgovorPotvrden
	}
	return models.NalogIzvodjacu{JournalEntry: e, Firma: "Primjer d.o.o.", Podrucje: 1}
}

func TestKarticaNalogaKojeSamDao(t *testing.T) {
	sad := time.Date(2026, 11, 23, 10, 0, 0, 0, models.Zagreb)
	svoj := models.NalogIzvodjacu{JournalEntry: models.JournalEntry{ID: "n2", JournalID: "j1", SheetID: "l0", Date: sad.AddDate(0, 0, -2),
		Kind: models.EntryKindTask, Side: models.EntrySideSupervisor, Status: models.TaskOpen, Text: "dovesti bager"}}
	k := karticaNalogaKojeSamDao([]models.NalogIzvodjacu{svoj, primljeniNalog(sad, true)}, sad)
	if k.Naslov != "Nalozi koje sam dao" || k.Broj != 1 || len(k.Redovi) != 2 {
		t.Fatalf("kartica: %+v", k)
	}
	prvi := k.Redovi[0]
	if !prvi.Hitno || prvi.URL != "/dnevnici/j1/listovi/l1" || prvi.Sporedno != "Primjer d.o.o. · P.2.1 · zadan danas · čeka vašu potvrdu" {
		t.Errorf("primljeni nalog koji čeka potvrdu nije prvi: %+v", prvi)
	}
	if k.Redovi[1].Hitno || k.Redovi[1].Glavno != "dovesti bager" {
		t.Errorf("svoj nalog: %+v", k.Redovi[1])
	}
	if k := karticaNalogaKojeSamDao(nil, sad); k.Prazno == nil || k.Broj != 0 {
		t.Errorf("prazna kartica: %+v", k)
	}
}

func TestKarticaNalogaDavateljaIzvori(t *testing.T) {
	sad := time.Now()
	r := httptest.NewRequest("GET", "/naslovna/kartica/"+kNaloziDao, nil)
	perms := &models.UserPermissions{User: models.User{ID: uuid.New()}}
	h := &DashboardHandler{}
	if k := h.karticaNalogaDavatelja(r, perms, kNaloziDao, sad); k.Prazno == nil {
		t.Error("bez izvora kartica nije nedostupna")
	}
	h.izvori.NaloziDavatelja = func(context.Context, *models.User) ([]models.NalogIzvodjacu, error) {
		return nil, errors.New("baza ne odgovara")
	}
	if k := h.karticaNalogaDavatelja(r, perms, kNaloziDao, sad); k.Prazno == nil || len(k.Redovi) != 0 {
		t.Error("uz grešku kartica nije nedostupna")
	}
	h.izvori.NaloziDavatelja = func(_ context.Context, u *models.User) ([]models.NalogIzvodjacu, error) {
		if u.ID != perms.User.ID {
			t.Error("izvor ne dobiva osobu")
		}
		return []models.NalogIzvodjacu{primljeniNalog(sad, true)}, nil
	}
	if k := h.karticaNalogaDavatelja(r, perms, kNaloziDao, sad); k.Broj != 1 {
		t.Errorf("kartica: %+v", k)
	}
	// kartica je u rasporedu rukovoditelja dionice, područja i sektora
	for _, uloga := range []string{uDionica, uPodrucje, uSektor} {
		if !strings.Contains(strings.Join(karticeZaUlogu[uloga], ","), kNaloziDao) {
			t.Errorf("uloga %s nema karticu naloga koje je dala", uloga)
		}
	}
}

// Poslužitelj kartici daje naloge iz dnevnika
func TestNaloziDavateljaNaslovne(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "nalozi.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	s := &Server{journalService: service.NewJournalService(repository.NewJournalRepository(baza, ledger.New(baza, "test")), nil, nil)}
	if n, err := s.naloziDavateljaNaslovne(context.Background(), &models.User{ID: uuid.New()}); err != nil || len(n) != 0 {
		t.Errorf("nalozi bez dnevnika: %v %v", n, err)
	}
}

// Stranica dionice pokazuje naloge izvođačima s poveznicom na list, a bez
// naloga odjeljka nema
func TestRadoviIzvodjacaNaDionici(t *testing.T) {
	dan := time.Date(2026, 11, 22, 0, 0, 0, 0, models.Zagreb)
	data := SectionPageData{
		CurrentUser: &models.User{FullName: "Pero Perić"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Section:     models.Section{Code: "P.2.1", AreaID: 2, SectorID: "P"},
	}
	if html := iscrtaj(t, "section_detail.html", data); strings.Contains(html, "Radovi izvođača na dionici") {
		t.Error("odjeljak bez naloga")
	}
	n := primljeniNalog(dan, false)
	data.NaloziIzvodjaca = []models.NalogIzvodjacu{n}
	html := iscrtaj(t, "section_detail.html", data)
	for _, treba := range []string{"Radovi izvođača na dionici", `href="/dnevnici/j1/listovi/l1"`, "Primjer d.o.o. · Primljeni nalog: Ivo Ivić, rukovoditelj dionice",
		"davatelj je potvrdio nalog"} {
		if !strings.Contains(html, treba) {
			t.Errorf("stranica dionice nema %q", treba)
		}
	}

	h := &SectionsHandler{}
	ctx := context.Background()
	if h.naloziIzvodjaca(ctx, data.Permissions, "P.2.1") != nil {
		t.Error("nalozi bez izvora")
	}
	h.naloziNaDionici = func(context.Context, *models.UserPermissions, string) ([]models.NalogIzvodjacu, error) {
		return nil, errors.New("baza ne odgovara")
	}
	if h.naloziIzvodjaca(ctx, data.Permissions, "P.2.1") != nil {
		t.Error("nalozi uz grešku")
	}
	h.naloziNaDionici = func(_ context.Context, _ *models.UserPermissions, dionica string) ([]models.NalogIzvodjacu, error) {
		return []models.NalogIzvodjacu{n}, nil
	}
	if len(h.naloziIzvodjaca(ctx, data.Permissions, "P.2.1")) != 1 {
		t.Error("nalozi s dionice")
	}
}
