package service

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Dnevnike usluga A.02 i A.03 izvođač čita samo na branjenom području svoje
// dužnosti, a ne dnevnike drugih firmi na drugim područjima (Odluke (8. 10.
// 2026.), pitanje 1); ostali ih čitaju kao i dosad. Istekla dužnost
// poslovođe ne otvara ni njegovo područje; bez ovlasti nema čitanja.
func TestIzvodjacVidiSamoSvojDnevnikUsluge(t *testing.T) {
	s := &JournalService{}
	sektor := "P"
	p1, p2 := 1, 2
	jucer := time.Now().Add(-time.Hour)
	osoba := func(duznosti ...models.Duty) *models.UserPermissions {
		for i := range duznosti {
			duznosti[i].IsActive, duznosti[i].SectorID = true, &sektor
		}
		return models.NewUserPermissions(models.User{ID: uuid.New(), FullName: "Marko Markić", Duties: duznosti})
	}
	poslovodja := models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, AreaID: &p1}
	istekliPoslovodja := models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, AreaID: &p1, ExpiresAt: &jucer}
	podrucje1 := models.OpsegPodrucja(models.Area{ID: 1, SectorID: "P"})
	podrucje2 := models.OpsegPodrucja(models.Area{ID: 2, SectorID: "P"})
	administrator := osoba(poslovodja)
	administrator.IsGlobalAdmin = true
	slucajevi := []struct {
		naziv       string
		perms       *models.UserPermissions
		prvo, drugo bool
	}{
		{"poslovođa na području 1", osoba(poslovodja), true, false},
		{"istekli poslovođa", osoba(istekliPoslovodja), false, false},
		{"poslovođa koji je i vodočuvar područja 2", osoba(poslovodja, models.Duty{Role: models.RoleWaterGuard, ScopeType: models.ScopeArea, AreaID: &p2}), true, true},
		{"poslovođa s administracijom", administrator, true, true},
		{"rukovoditelj područja 2", osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &p2}), true, true},
		{"gost", osoba(models.Duty{Role: models.RoleGuest, ScopeType: models.ScopeAll}), true, true},
		{"bez ovlasti", nil, false, false},
	}
	for _, sl := range slucajevi {
		if got := s.VidiUslugePodrucja(sl.perms, podrucje1); got != sl.prvo {
			t.Errorf("%s: dnevnik područja 1 %v, očekivano %v", sl.naziv, got, sl.prvo)
		}
		if got := s.VidiUslugePodrucja(sl.perms, podrucje2); got != sl.drugo {
			t.Errorf("%s: dnevnik područja 2 %v, očekivano %v", sl.naziv, got, sl.drugo)
		}
	}
}

// Na svom području izvođač čita samo dnevnik svoje firme: naslovnica kao
// izvođača navodi firmu s njegova računa. Dnevnik druge firme, i kad je on
// na njoj upisan kao voditelj usluga, i dnevnik bez izvođača na naslovnici
// ne čita (Odluke (8. 10. 2026.), pitanje 1); administrator i HV strana
// čitaju svaki. Podaci su izmišljeni.
func TestIzvodjacVidiSamoDnevnikSvojeFirme(t *testing.T) {
	_, repo := uslugeBaza(t)
	s := NewJournalService(repo, nil, nil)
	ctx := context.Background()
	sektor := "P"
	p1 := 1
	podrucje1 := models.OpsegPodrucja(models.Area{ID: 1, SectorID: "P"})
	osoba := func(firma string, d models.Duty) *models.UserPermissions {
		d.IsActive, d.SectorID, d.AreaID = true, &sektor, &p1
		return models.NewUserPermissions(models.User{ID: uuid.New(), FullName: "Marko Markić", OrgName: firma, Duties: []models.Duty{d}})
	}
	poslovodja := osoba("Primjer d.o.o.", models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea})
	bezFirme := osoba("", models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea})
	administrator := osoba("Primjer d.o.o.", models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea})
	administrator.IsGlobalAdmin = true
	rukovoditelj := osoba("Hrvatske vode", models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea})
	svoj := &models.Journal{AreaID: 1, Contractor: "PRIMJER d.o.o., Primjerska 1"}
	vodi := &models.Journal{AreaID: 1, Contractor: "Treći radovi d.o.o.", ContractorLead: "Marko Markić"}
	drugi := &models.Journal{AreaID: 1, Contractor: "Drugi radovi d.o.o."}
	prazan := &models.Journal{AreaID: 1}
	slucajevi := []struct {
		naziv                     string
		perms                     *models.UserPermissions
		svoj, vodi, drugi, prazan bool
	}{
		{"poslovođa firme Primjer", poslovodja, true, false, false, false},
		{"poslovođa bez firme na računu", bezFirme, false, false, false, false},
		{"poslovođa s administracijom", administrator, true, true, true, true},
		{"rukovoditelj područja", rukovoditelj, true, true, true, true},
		{"bez ovlasti", nil, false, false, false, false},
	}
	for _, sl := range slucajevi {
		for _, x := range []struct {
			j    *models.Journal
			vidi bool
		}{{svoj, sl.svoj}, {vodi, sl.vodi}, {drugi, sl.drugi}, {prazan, sl.prazan}} {
			if got := s.VidiDnevnikUsluge(ctx, sl.perms, podrucje1, x.j); got != x.vidi {
				t.Errorf("%s: dnevnik izvođača %q (voditelj %q) %v, očekivano %v", sl.naziv, x.j.Contractor, x.j.ContractorLead, got, x.vidi)
			}
		}
	}
	// svoj dnevnik na tuđem području ne čita; bez dnevnika nema čitanja
	if s.VidiDnevnikUsluge(ctx, poslovodja, models.OpsegPodrucja(models.Area{ID: 2, SectorID: "P"}), svoj) {
		t.Error("poslovođa čita dnevnik svoje firme na području bez dužnosti")
	}
	if s.VidiDnevnikUsluge(ctx, poslovodja, podrucje1, nil) {
		t.Error("poslovođa čita nepoznat dnevnik")
	}
}

// uslugeBaza je baza sa sektorom P i područjem 1 i repozitorij dnevnika
// nad njom
func uslugeBaza(t *testing.T) (*sql.DB, *repository.JournalRepository) {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "usluge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerica', 'COP Primjerica')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Primjerica', 'VGI Primjerica', 'Primjerica')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return baza, repository.NewJournalRepository(baza, ledger.New(baza, "test"))
}

// Dnevnik je izvođačev i kad ga naslovnica ne navodi, ako ga je izvođač
// otvorio ili u njemu otvorio list ili upisao zapis; tuđi upis ga ne čini
// njegovim, a bez baze (greška čitanja) nije njegov. Podaci su izmišljeni.
func TestDnevnikIzvodjacaPoUpisima(t *testing.T) {
	baza, repo := uslugeBaza(t)
	s := NewJournalService(repo, nil, nil)
	ctx := context.Background()
	pero := &models.User{ID: uuid.New(), FullName: "Pero Perić"}
	ana := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	novi := func(otvorio string) *models.Journal {
		t.Helper()
		j := &models.Journal{AreaID: 1, Kind: models.JournalKindMaintenanceA03, Year: 2026, Contractor: "Primjer d.o.o.", CreatedBy: otvorio}
		if err := repo.SaveJournal(ctx, j); err != nil {
			t.Fatal(err)
		}
		return j
	}
	dan := time.Date(2026, 10, 8, 0, 0, 0, 0, models.Zagreb)
	listom, upisom, otvorio, prazan := novi(""), novi(""), novi(pero.ID.String()), novi("")
	if err := repo.SaveSheet(ctx, &models.JournalSheet{JournalID: listom.ID, Date: dan, CreatedBy: pero.ID.String()}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveEntry(ctx, &models.JournalEntry{JournalID: upisom.ID, Date: dan, Kind: models.EntryKindWork, Text: "košnja", UserID: pero.ID.String()}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveEntry(ctx, &models.JournalEntry{JournalID: prazan.ID, Date: dan, Kind: models.EntryKindNote, Text: "napomena", UserID: ana.ID.String()}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		ime    string
		j      *models.Journal
		njegov bool
	}{{"otvorio list", listom, true}, {"upisao zapis", upisom, true}, {"otvorio dnevnik", otvorio, true}, {"upisao drugi", prazan, false}, {"bez dnevnika", nil, false}} {
		if got := s.dnevnikIzvodjaca(ctx, pero, x.j); got != x.njegov {
			t.Errorf("%s: %v, očekivano %v", x.ime, got, x.njegov)
		}
	}
	if s.dnevnikIzvodjaca(ctx, ana, listom) {
		t.Error("dnevnik je izvođačev po tuđem listu")
	}
	if _, err := baza.Exec(`DROP TABLE journal_entries`); err != nil {
		t.Fatal(err)
	}
	if s.dnevnikIzvodjaca(ctx, pero, upisom) {
		t.Error("uz grešku čitanja dnevnik je izvođačev")
	}
}

// Firmu na svom računu izvođač ne mijenja sam: po njoj vidi dnevnike
// usluga svoje firme (Odluke (8. 10. 2026.), pitanje 1). Prazna ili ista
// firma ne mijenja ništa; tko nije izvođač mijenja je kao i dosad. Podaci
// su izmišljeni.
func TestFirmaIzvodjacaNaSebi(t *testing.T) {
	poslovodja := &models.User{FullName: "Marko Markić", OrgName: "Alfa d.o.o.", Duties: []models.Duty{{IsActive: true, Role: models.RoleServiceLeaderForeman}}}
	rukovoditelj := &models.User{FullName: "Ana Anić", OrgName: "VGO Primjerica", Duties: []models.Duty{{IsActive: true, Role: models.RoleAreaLeader}}}
	for _, s := range []struct {
		naziv  string
		u      *models.User
		firma  string
		odbija bool
	}{
		{"izvođač, prazna firma iz obrasca", poslovodja, "", false},
		{"izvođač, ista firma s razmacima", poslovodja, " Alfa d.o.o. ", false},
		{"izvođač, druga firma", poslovodja, "Beta d.o.o.", true},
		{"nije izvođač, druga organizacija", rukovoditelj, "VGO Probnica", false},
	} {
		err := firmaIzvodjacaNaSebi(s.u, s.firma, IsContractor(s.u))
		if s.odbija != errors.Is(err, ErrUnauthorized) {
			t.Errorf("%s: %v", s.naziv, err)
		}
	}
	if err := zabranjenoNaSebi(poslovodja, UpdateUserRequest{OrgName: "Beta d.o.o."}, true); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("izmjena vlastitog računa mijenja firmu izvođača: %v", err)
	}
}

// Poslovođa ostaje izvođač za dnevnike usluga i kad mu je dužnost opozvana
// ili izmijenjena u drugu ulogu izvan ustroja obrane: ne otvaraju mu se
// dnevnici drugih firmi ni na jednom području (Odluke (8. 10. 2026.),
// pitanje 1). Opozvani poslovođa bez druge dužnosti ne vidi nijedan, kao i
// onaj kojemu je dužnost istekla. Tko je bio poslovođa, a sada ima dužnost u
// ustroju obrane (razine 1 do 4), i tko poslovođa nikad nije bio, vide ih
// kao i dosad. Kad se povijest ne da pročitati, osoba bez dužnosti u
// ustroju gleda se kao izvođač. Podaci su izmišljeni.
func TestBivsiPoslovodjaNeVidiDnevnikeDrugihFirmi(t *testing.T) {
	baza, repo := uslugeBaza(t)
	if _, err := baza.Exec(`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (2, 'P', 'Drugo', 'VGI Drugo', 'Drugo')`); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(baza, ledger.New(baza, "test"))
	s := NewJournalService(repo, nil, nil)
	s.SetImaoUlogu(NewUserService(users, nil, nil).ImaoUlogu)
	ctx := context.Background()
	sektor, p1 := "P", 1
	duznost := func(uloga models.Role) *models.Duty {
		return &models.Duty{Title: "dužnost", Role: uloga, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &p1}
	}
	osoba := func(korisnik, ime string, d *models.Duty) *models.User {
		t.Helper()
		u := &models.User{Username: korisnik, PasswordHash: "x", FullName: ime, OrgName: "Alfa d.o.o.", IsActive: true}
		if err := users.CreateUser(u, d); err != nil {
			t.Fatal(err)
		}
		return u
	}
	ucitaj := func(u *models.User) *models.UserPermissions {
		t.Helper()
		x, err := users.GetUserByID(u.ID)
		if err != nil || x == nil {
			t.Fatal(err)
		}
		return models.NewUserPermissions(*x)
	}
	opozvaniD := duznost(models.RoleServiceLeaderForeman)
	opozvani := osoba("mmarkic", "Marko Markić", opozvaniD)
	if err := users.RevokeDuty(opozvaniD.ID); err != nil {
		t.Fatal(err)
	}
	premjesteniD := duznost(models.RoleServiceLeaderForeman)
	premjesteni := osoba("iivic", "Ivo Ivić", premjesteniD)
	premjesteniD.Role = models.RoleWaterGuard
	if err := users.UpdateDuty(premjesteniD); err != nil {
		t.Fatal(err)
	}
	uUstrojuD := duznost(models.RoleServiceLeaderForeman)
	uUstroju := osoba("iivic2", "Iva Ivić", uUstrojuD)
	uUstrojuD.Role = models.RoleAreaLeader
	if err := users.UpdateDuty(uUstrojuD); err != nil {
		t.Fatal(err)
	}
	vodocuvar := osoba("pperic", "Pero Perić", duznost(models.RoleWaterGuard))

	svoj := &models.Journal{AreaID: 1, Contractor: "Alfa d.o.o."}
	drugi := &models.Journal{AreaID: 1, Contractor: "Beta d.o.o."}
	podrucje1 := models.OpsegPodrucja(models.Area{ID: 1, SectorID: "P"})
	podrucje2 := models.OpsegPodrucja(models.Area{ID: 2, SectorID: "P"})
	slucajevi := []struct {
		naziv                        string
		perms                        *models.UserPermissions
		izvodjac, drugo, svoj, drugi bool
	}{
		{"opozvani poslovođa", ucitaj(opozvani), true, false, false, false},
		{"poslovođa premješten u vodočuvara", ucitaj(premjesteni), true, false, true, false},
		{"bivši poslovođa u ustroju obrane", ucitaj(uUstroju), false, true, true, true},
		{"vodočuvar koji nije bio poslovođa", ucitaj(vodocuvar), false, true, true, true},
	}
	for _, sl := range slucajevi {
		if got := s.IzvodjacZaDnevnike(&sl.perms.User); got != sl.izvodjac {
			t.Errorf("%s: izvođač %v, očekivano %v", sl.naziv, got, sl.izvodjac)
		}
		if got := s.VidiUslugePodrucja(sl.perms, podrucje2); got != sl.drugo {
			t.Errorf("%s: područje 2 %v, očekivano %v", sl.naziv, got, sl.drugo)
		}
		if got := s.VidiDnevnikUsluge(ctx, sl.perms, podrucje1, svoj); got != sl.svoj {
			t.Errorf("%s: dnevnik svoje firme %v, očekivano %v", sl.naziv, got, sl.svoj)
		}
		if got := s.VidiDnevnikUsluge(ctx, sl.perms, podrucje1, drugi); got != sl.drugi {
			t.Errorf("%s: dnevnik druge firme %v, očekivano %v", sl.naziv, got, sl.drugi)
		}
	}
	if s.IzvodjacZaDnevnike(nil) {
		t.Error("nitko je izvođač")
	}
	// bez izvora povijesti (stariji poziv) gleda se samo valjana dužnost
	bezPovijesti := NewJournalService(repo, nil, nil)
	if bezPovijesti.IzvodjacZaDnevnike(&ucitaj(opozvani).User) {
		t.Error("bez povijesti opozvani poslovođa je izvođač")
	}
	// povijest se ne čita: osoba bez dužnosti u ustroju je izvođač
	if _, err := baza.Exec(`DROP TABLE record_versions`); err != nil {
		t.Fatal(err)
	}
	if !s.IzvodjacZaDnevnike(&ucitaj(vodocuvar).User) {
		t.Error("uz grešku čitanja povijesti vodočuvar nije izvođač")
	}
}

// Ni poslovođa kojemu je dužnost izmijenjena u ulogu izvan ustroja obrane
// ne mijenja sam firmu na svom računu: za dnevnike usluga i dalje je
// izvođač (IzvodjacZaDnevnike), pa bi mu druga firma otvorila njezine
// dnevnike (Odluke (8. 10. 2026.), pitanje 1). Podaci su izmišljeni.
func TestBivsiPoslovodjaNeMijenjaFirmuNaSebi(t *testing.T) {
	baza, _ := uslugeBaza(t)
	users := repository.NewUserRepository(baza, ledger.New(baza, "test"))
	us := NewUserService(users, nil, NewSSEBroker())
	sektor, p1 := "P", 1
	d := &models.Duty{Title: "Poslovođa", Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &p1}
	u := &models.User{Username: "mmarkic", PasswordHash: "x", FullName: "Marko Markić", OrgName: "Alfa d.o.o.", IsActive: true}
	if err := users.CreateUser(u, d); err != nil {
		t.Fatal(err)
	}
	d.Role = models.RoleWaterGuard
	if err := users.UpdateDuty(d); err != nil {
		t.Fatal(err)
	}
	perms, err := users.GetUserPermissions(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	req := UpdateUserRequest{ID: u.ID, Username: u.Username, FullName: u.FullName, OrgName: "Beta d.o.o.", IsActive: true}
	if _, err := us.UpdateUser(perms, req); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("bivši poslovođa sam mijenja firmu: %v", err)
	}
	if x, _ := users.GetUserByID(u.ID); x.OrgName != "Alfa d.o.o." {
		t.Errorf("firma na računu: %q", x.OrgName)
	}
}
