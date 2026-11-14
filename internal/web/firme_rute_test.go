package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Popis licenciranih firmi po crtežu (17f, 7f), kroz pravi poslužitelj: uprava područja
// vidi Novu firmu i poveznicu na firmu svog područja, a ne na tuđu; firma na cijelom
// sektoru ima oznaku sektora; prazna polja pišu „nije upisan” i „nije upisana”.
func TestFirmePopisPravaKrozRute(t *testing.T) {
	o := novaOkolinaSastavljaca(t)
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('Q', 'Sektor Q', 'VGO Drugo', 'COP Drugo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (2, 'Q', 'Drugi sliv', 'VGI Drugo')`,
	} {
		if _, err := o.baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	org := service.NewOrgService(repository.NewOrgRepository(o.baza, ledger.New(o.baza, "test")), service.NewSSEBroker())
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	firme := map[string]*models.Contractor{
		"svoja":  {Name: "Primjer gradnja d.o.o.", OIB: "12345678903", Address: "Ulica 1, Primjerovo", Phone: "031/000-010", Email: "ured@primjer.hr", Contact: "Ivo Poslovođa", Active: true},
		"tudja":  {Name: "Drugi nasip d.o.o.", Active: true},
		"sektor": {Name: "Sektorska d.d.", Active: false},
	}
	mjesta := map[string][]models.ContractorAssignment{
		"svoja": {{SectorID: "P", AreaID: 1}}, "tudja": {{SectorID: "Q", AreaID: 2}}, "sektor": {{SectorID: "P"}},
	}
	for k, c := range firme {
		if err := org.SaveContractor(context.Background(), admin, c, mjesta[k]); err != nil {
			t.Fatalf("%s: %v", k, err)
		}
	}
	s, err := NewServer("", o.auth, o.users, nil, nil, o.stations, nil, o.structures, o.readings, nil, o.moduli, nil, nil, org,
		SupportContact{}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	stranica := func(tko string) string {
		t.Helper()
		sesija := &models.Session{UserID: o.osobe[tko].ID, IPAddress: "192.168.1.50", UserAgent: "test", ExpiresAt: time.Now().Add(time.Hour)}
		if err := o.sesije.CreateSession(sesija); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodGet, "/firme", nil)
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija.ID.String()})
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: /firme %d", tko, w.Code)
		}
		return w.Body.String()
	}
	uredi := func(k string) string { return `href="/firme/` + firme[k].ID + `/edit"` }

	o.osobe["administrator"] = &models.User{ID: uuid.New(), Username: "aadmin", FullName: "Ante Antić", IsGlobalAdmin: true, IsActive: true}
	if err := o.userRepo.CreateUser(o.osobe["administrator"], nil); err != nil {
		t.Fatal(err)
	}
	for tko, zeli := range map[string]map[string]bool{
		"administrator": {"svoja": true, "tudja": true, "sektor": true, "nova": true},
		"rukovoditelj":  {"svoja": true, "tudja": false, "sektor": false, "nova": true},
		"dežurni":       {"svoja": false, "tudja": false, "sektor": false, "nova": false},
	} {
		html := stranica(tko)
		for _, mora := range []string{
			`<a href="/registri" class="povratna-poveznica">← Registri</a>`, `href="/firme.csv"`,
			`<table class="data-table kartice-na-mobitelu dio-tablica fir-tablica">`,
			`<span class="oznaka-stanja obrub">BP 1</span>`, `<span class="oznaka-stanja obrub">Sektor P</span>`,
			`<span class="fir-prazno">nije upisan</span>`, `<span class="fir-prazno">nije upisana</span>`,
			`ured@primjer.hr`, `osoba za obranu: Ivo Poslovođa`, `<span class="oznaka-stanja">neaktivna</span>`,
		} {
			if !strings.Contains(html, mora) {
				t.Errorf("%s: nema %q", tko, mora)
			}
		}
		for _, k := range []string{"svoja", "tudja", "sektor"} {
			if strings.Contains(html, uredi(k)) != zeli[k] {
				t.Errorf("%s: poveznica na %s vidljiva %v, želi %v", tko, k, !zeli[k], zeli[k])
			}
		}
		if strings.Contains(html, `href="/firme/new"`) != zeli["nova"] {
			t.Errorf("%s: Nova firma vidljiva %v, želi %v", tko, !zeli["nova"], zeli["nova"])
		}
	}
}
