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
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "usluge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
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
		err := firmaIzvodjacaNaSebi(s.u, s.firma)
		if s.odbija != errors.Is(err, ErrUnauthorized) {
			t.Errorf("%s: %v", s.naziv, err)
		}
	}
	if err := zabranjenoNaSebi(poslovodja, UpdateUserRequest{OrgName: "Beta d.o.o."}); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("izmjena vlastitog računa mijenja firmu izvođača: %v", err)
	}
}
