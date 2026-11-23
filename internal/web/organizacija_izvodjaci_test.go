package web

import (
	"errors"
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

// Osobe i firme su izmišljene.

// Redak Priloga 2 kroz pravi poslužitelj: aktivna firma područja s adresom,
// telefonom i e-poštom, ispod rukovoditelj obrane izvođača i dva zamjenika
// iz aktivnih dužnosti. Neaktivna firma se ne pokazuje, a područje bez firme
// i rukovoditelja kaže što nedostaje.
func TestOrganizacijaPokazujeIzvodjacaPodrucja(t *testing.T) {
	o := novaOkolinaSastavljaca(t)
	for _, q := range []string{
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (2, 'P', 'Drugi sliv Primjerice', 'VGI Drugovo')`,
		`INSERT INTO contractors (id, name, short_name, address, phone, email, active, updated_at)
		 VALUES ('f1', 'Primjer gradnja d.d. Primjerovo', 'Primjer gradnja', 'Primjerska 1, 31000 Primjerovo', '031/000-100', 'obrana@primjer.example', 1, '2026-01-01'),
		        ('f2', 'Stara gradnja d.d. Primjerovo', 'Stara gradnja', 'Stara 2', '031/000-200', '', 0, '2026-01-01')`,
		`INSERT INTO contractor_assignments (id, contractor_id, sector_id, area_id, updated_at)
		 VALUES ('v1', 'f1', 'P', 1, '2026-01-01'), ('v2', 'f2', 'P', 1, '2026-01-01')`,
	} {
		if _, err := o.baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	sektor, podrucje := "P", 1
	duznost := func(role models.Role) *models.Duty {
		return &models.Duty{Title: role.Label() + " BP 1", Role: role, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje, IsPrimary: true}
	}
	novi := func(korisnik, ime, zvanje, firma string, role models.Role) *models.User {
		t.Helper()
		u := &models.User{ID: uuid.New(), Username: korisnik, FullName: ime, Title: zvanje, IsActive: true,
			OrgType: models.OrgPravnaOsoba, OrgName: firma}
		if err := o.userRepo.CreateUser(u, duznost(role)); err != nil {
			t.Fatal(err)
		}
		return u
	}
	ruk := novi("ppetric", "Petar Petrić", "dipl. ing. građ.", "Primjer gradnja d.d.", models.RoleContractorAreaLeader)
	novi("llukic", "Lana Lukić", "", "Primjer gradnja d.d.", models.RoleContractorAreaDeputy)
	novi("mmatic", "Mato Matić", "", "Primjer gradnja", models.RoleContractorAreaDeputy)
	novi("ttomic", "Tea Tomić", "", "Treća firma d.o.o.", models.RoleContractorAreaDeputy)

	admin := &models.User{ID: uuid.New(), Username: "aadmin", FullName: "Ante Antić", IsGlobalAdmin: true, IsActive: true}
	if err := o.userRepo.CreateUser(admin, nil); err != nil {
		t.Fatal(err)
	}
	org := service.NewOrgService(repository.NewOrgRepository(o.baza, ledger.New(o.baza, "test")), service.NewSSEBroker())
	s, err := NewServer("", o.auth, o.users, nil, nil, o.stations, nil, o.structures, o.readings, nil, o.moduli, nil, nil, org,
		SupportContact{}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sesija := &models.Session{UserID: admin.ID, IPAddress: "192.168.1.50", UserAgent: "test", ExpiresAt: time.Now().Add(time.Hour)}
	if err := o.sesije.CreateSession(sesija); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/organizacija", nil)
	r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija.ID.String()})
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("/organizacija %d", w.Code)
	}
	html := w.Body.String()
	i := strings.Index(html, `id="izvodjaci"`)
	if i < 0 {
		t.Fatal("nema odjeljka s pravnom osobom za provedbu mjera obrane")
	}
	odjeljak := html[i:]
	bp1, bp2, _ := strings.Cut(odjeljak, "BP 2 · Drugi sliv Primjerice")
	for _, mora := range []string{
		"Pravna osoba za provedbu mjera obrane od poplava i rukovoditelji na branjenom području",
		"BP 1 · Mali sliv Primjerica",
		"Primjer gradnja d.d. Primjerovo", "Primjerska 1, 31000 Primjerovo", `href="tel:031/000-100"`, `href="mailto:obrana@primjer.example"`,
		`<a href="/users/` + ruk.ID.String() + `">Petar Petrić</a>, dipl. ing. građ.`,
		"Zamjenici rukovoditelja obrane od poplava", "Lana Lukić</a>; ", "Mato Matić</a>",
		"Tea Tomić</a> ima dužnost zamjenika rukovoditelja obrane izvođača na ovom području, ali firma s računa (Treća firma d.o.o.) nije među aktivnim firmama područja.",
	} {
		if !strings.Contains(bp1, mora) {
			t.Errorf("BP 1: nema %q", mora)
		}
	}
	if strings.Contains(odjeljak, "Stara gradnja") {
		t.Error("neaktivna firma se pokazuje")
	}
	if !strings.Contains(bp2, "Nijedna aktivna firma nije vezana na ovo područje") {
		t.Error("BP 2: nema rečenice da firma nije vezana")
	}
}

func TestIzvodjacPodrucjaSlazeOsobeUzFirmu(t *testing.T) {
	prva := models.Contractor{ID: "a", Name: "Prva gradnja d.d. Primjerovo", ShortName: "Prva gradnja", Active: true}
	druga := models.Contractor{ID: "b", Name: "Druga d.o.o.", Active: true}
	osobe := []OsobaIzvodjaca{
		{Ime: "Iva", Firma: "PRVA GRADNJA DD"},
		{Ime: "Ivo", Firma: "Druga d.o.o., Primjerska 2", Zamjenik: true},
		{Ime: "Ana", Firma: "Prva gradnja", Zamjenik: true},
		{Ime: "Eva", Firma: ""},
	}
	got := izvodjacPodrucja([]models.Contractor{prva, druga, prva}, osobe)
	if len(got.Firme) != 2 {
		t.Fatalf("firme %d, želi 2 (ista firma dvaput vezana jednom)", len(got.Firme))
	}
	imena := func(o []OsobaIzvodjaca) string {
		var s []string
		for _, x := range o {
			s = append(s, x.Ime)
		}
		return strings.Join(s, ",")
	}
	for opis, par := range map[string][2]string{
		"prva rukovoditelji":  {imena(got.Firme[0].Rukovoditelji), "Iva"},
		"prva zamjenici":      {imena(got.Firme[0].Zamjenici), "Ana"},
		"druga rukovoditelji": {imena(got.Firme[1].Rukovoditelji), ""},
		"druga zamjenici":     {imena(got.Firme[1].Zamjenici), "Ivo"},
		"bez firme":           {imena(got.BezFirme), "Eva"},
	} {
		if par[0] != par[1] {
			t.Errorf("%s: %q, želi %q", opis, par[0], par[1])
		}
	}
	if prazno := izvodjacPodrucja(nil, osobe[:1]); len(prazno.Firme) != 0 || len(prazno.BezFirme) != 1 {
		t.Errorf("bez firmi: %+v", prazno)
	}
}

func TestDodajDuznostiIzvodjacaSamoAktivneNaPodrucju(t *testing.T) {
	jedan, dva := 1, 2
	u := models.User{ID: uuid.New(), FullName: "Ema Emić", Title: "mag. ing.", OrgName: "Prva d.d.", Duties: []models.Duty{
		{Role: models.RoleContractorAreaDeputy, AreaID: &jedan, IsActive: true},
		{Role: models.RoleContractorAreaDeputy, AreaID: &jedan, IsActive: true}, // ista dužnost dvaput
		{Role: models.RoleContractorAreaDeputy, AreaID: &dva, IsActive: false},
		{Role: models.RoleContractorAreaDeputy, IsActive: true}, // bez područja
		{Role: models.RoleServiceLeaderForeman, AreaID: &dva, IsActive: true},
	}}
	po := map[int][]OsobaIzvodjaca{}
	dodajDuznostiIzvodjaca(po, u, models.RoleContractorAreaDeputy)
	if len(po) != 1 || len(po[1]) != 1 {
		t.Fatalf("po područjima %+v", po)
	}
	if o := po[1][0]; o.ID != u.ID || o.Ime != "Ema Emić" || o.Zvanje != "mag. ing." || o.Firma != "Prva d.d." || !o.Zamjenik {
		t.Errorf("osoba %+v", o)
	}
}

func TestOsobeObraneIzvodjacaIzvor(t *testing.T) {
	h := &OrgHandler{}
	if po, err := h.osobeObraneIzvodjaca(); err != nil || len(po) != 0 {
		t.Errorf("bez izvora: %v %v", po, err)
	}
	jedan := 1
	pitane := []models.Role{}
	h.osobeUloge = func(r models.Role) ([]models.User, error) {
		pitane = append(pitane, r)
		return []models.User{{FullName: "Iva Ivić", Duties: []models.Duty{{Role: r, AreaID: &jedan, IsActive: true}}}}, nil
	}
	po, err := h.osobeObraneIzvodjaca()
	if err != nil || len(po[1]) != 2 || po[1][0].Zamjenik || !po[1][1].Zamjenik {
		t.Errorf("rukovoditelj pa zamjenik: %+v %v", po, err)
	}
	if len(pitane) != 2 || pitane[0] != models.RoleContractorAreaLeader || pitane[1] != models.RoleContractorAreaDeputy {
		t.Errorf("pitane uloge %v", pitane)
	}

	h.osobeUloge = func(models.Role) ([]models.User, error) { return nil, errors.New("baza zaključana") }
	data := OrgPageData{Sectors: []SectorView{{Areas: []AreaView{{Area: models.Area{ID: 1}}}}}}
	h.dodajIzvodjace(&data, repository.ContractorIndex{ByArea: map[int][]models.Contractor{1: {{ID: "a", Name: "Prva d.d.", Active: true}}}})
	if !data.IzvodjaciNepoznati {
		t.Error("greška djelatnika nije javljena")
	}
	if f := data.Sectors[0].Areas[0].Izvodjac.Firme; len(f) != 1 || f[0].Name != "Prva d.d." {
		t.Errorf("firma se ne vidi kad djelatnici nisu pročitani: %+v", f)
	}
}

func TestOsobeUlogeBezServisa(t *testing.T) {
	if u, err := (&Server{}).osobeUloge(models.RoleContractorAreaLeader); u != nil || err != nil {
		t.Errorf("%v %v", u, err)
	}
}
