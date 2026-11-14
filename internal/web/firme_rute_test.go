package web

import (
	"context"
	gohtml "html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
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

// Obrazac firme po crtežu (7g): sva polja s istim imenima, Aktivna kao prekidač na
// polju active (uključen za novu firmu), područja i cijeli sektor kao čipovi; sektor
// s označenim mjestom rada je otvoren, ostali sklopljeni; Obriši samo pri uređivanju.
func TestObrazacFirmeCuvaPolja(t *testing.T) {
	sektori := []SectorView{
		{Sector: models.Sector{ID: "P", Name: "Primjerica"}, SectorAdmin: true, Areas: []AreaView{{Area: models.Area{ID: 1, Name: "Mali sliv Primjerica"}}}},
		{Sector: models.Sector{ID: "Q", Name: "Drugo"}, SectorAdmin: true, Areas: []AreaView{{Area: models.Area{ID: 2, Name: "Drugi sliv"}}}},
	}
	c := models.Contractor{ID: "f1", Name: "Primjer gradnja d.o.o.", ShortName: "Primjer", OIB: "12345678903", Address: "Ulica 1",
		Phone: "031/000-010", Email: "ured@primjer.hr", Contact: "Ivo Poslovođa", Notes: "ugovor 2024.–2028.", Active: false}
	html := iscrtajStranicu(t, "contractor_form.html", OrgPageData{
		Permissions: &models.UserPermissions{IsGlobalAdmin: true}, Contractor: c, IsEdit: true, Sectors: sektori,
		SectorChecked: map[string]bool{}, AreaChecked: map[int]bool{1: true},
	})
	zeli := url.Values{"id": {"f1"}, "name": {c.Name}, "short_name": {c.ShortName}, "oib": {c.OIB}, "address": {c.Address},
		"phone": {c.Phone}, "email": {c.Email}, "contact": {c.Contact}, "area": {"1"}}
	glavni := regexp.MustCompile(`(?s)action="/firme/save">.*?</form>`).FindString(html)
	if dobio := poslanaPolja(glavni); !reflect.DeepEqual(dobio, zeli) {
		t.Errorf("polja %v, želi %v", dobio, zeli)
	}
	for _, mora := range []string{
		`>ugovor 2024.–2028.</textarea>`, `<input type="checkbox" class="prekidac" role="switch" name="active" value="1" >`,
		`<input type="checkbox" name="area" value="1" checked>`, `<input type="checkbox" name="area" value="2" >`,
		`<details class="fir-sektor" open>`, `<details class="fir-sektor">`,
		`form="obrisi-firmu"`, `<form id="obrisi-firmu" method="POST" action="/firme/delete"`, `action="/firme/save"`,
	} {
		if !strings.Contains(html, mora) {
			t.Errorf("uređivanje: nema %q", mora)
		}
	}
	html = iscrtajStranicu(t, "contractor_form.html", OrgPageData{
		Permissions: &models.UserPermissions{IsGlobalAdmin: true}, Sectors: sektori[:1], SectorChecked: map[string]bool{}, AreaChecked: map[int]bool{},
	})
	if !strings.Contains(html, `name="active" value="1" checked>`) || strings.Contains(html, "obrisi-firmu") || !strings.Contains(html, `<details class="fir-sektor" open>`) {
		t.Error("nova firma: Aktivna nije uključena, Obriši je vidljiv ili jedini sektor nije otvoren")
	}
}

// poslanaPolja čita polja <input> onako kako bi ih preglednik poslao: neoznačene
// kvačice izostaju, a isto ime s više vrijednosti daje popis.
func poslanaPolja(html string) url.Values {
	polja := url.Values{}
	for _, in := range regexp.MustCompile(`<input[^>]*>`).FindAllString(html, -1) {
		ime := regexp.MustCompile(`name="([^"]+)"`).FindStringSubmatch(in)
		vr := regexp.MustCompile(`value="([^"]*)"`).FindStringSubmatch(in)
		if ime == nil || vr == nil {
			continue
		}
		if strings.Contains(in, `type="checkbox"`) && !strings.Contains(in, " checked") {
			continue
		}
		polja.Add(ime[1], gohtml.UnescapeString(vr[1]))
	}
	return polja
}
