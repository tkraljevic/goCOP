package service

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Dnevnik je vodočuvarev: kolega s istog područja ga ne vidi, iako na tom
// području smije pisati. Vide ga vodočuvar, oni koji ga ovjeravaju i
// parafiraju, i administracija.
func TestTudjiDnevnikNeVidiKolegaSIstogPodrucja(t *testing.T) {
	bp := 16
	sektor := "B"
	vodocuvarID := uuid.New()
	list := &models.VodocuvarskiList{UserID: vodocuvarID.String(), Sektor: sektor, AreaID: bp}
	s := &VodocuvarService{}

	osoba := func(id uuid.UUID, uloga models.Role) *models.UserPermissions {
		u := models.User{ID: id, FullName: "proba", Duties: []models.Duty{{
			IsActive: true, Role: uloga, ScopeType: models.ScopeArea,
			SectorID: &sektor, AreaID: &bp,
		}}}
		p := models.NewUserPermissions(u)
		return p
	}
	sam := osoba(vodocuvarID, models.RoleWaterGuard)
	if !s.SmijeVidjeti(sam, list) {
		t.Error("vodočuvar ne vidi vlastiti list")
	}
	kolega := osoba(uuid.New(), models.RoleWaterGuard)
	if s.SmijeVidjeti(kolega, list) {
		t.Error("kolega s istog područja vidi tuđi dnevnik")
	}
	ruk := osoba(uuid.New(), models.RoleAreaLeader)
	if !s.SmijeVidjeti(ruk, list) {
		t.Error("rukovoditelj branjenog područja ne vidi list")
	}
}

// Prenesen list zaključen je prijenosom: ne mijenja se, ne predaje, ne
// ovjerava i ne parafira, jer ga nitko nikad neće potpisati.
func TestPreneseniListJeZakljucen(t *testing.T) {
	l := &models.VodocuvarskiList{Rekonstrukcija: true, Sektor: "B", AreaID: 16}
	if !l.Zakljucen() {
		t.Error("prenesen list nije zaključen")
	}
	if l.Stanje() != "ovjereno prijenosom" {
		t.Errorf("stanje: %q", l.Stanje())
	}
	// predan list je isto zaključen, a nacrt nije
	if !(&models.VodocuvarskiList{PredanoAt: &vrijemeProbe}).Zakljucen() {
		t.Error("predan list nije zaključen")
	}
	if (&models.VodocuvarskiList{}).Zakljucen() {
		t.Error("nacrt je zaključen")
	}
}

var vrijemeProbe = time.Now()

// Prijave s terena idu s dnevnikom: tko ne vidi dnevnik vodočuvara, ne vidi
// ni njegove objavljene prijave. Dežurni operater sektora i poslovođa
// izvođača na području vide oboje, kao prije prvog kruga, dok im dužnost
// vrijedi; s isteklom ne vide ništa. Pravilo za operatera i poslovođu je
// otvoreno pitanje: dok se ne odluči, ostaje kako je bilo. Gost i
// preglednik s dužnošću na području ne vide ni dnevnik ni prijave.
func TestPrijaveIduSDnevnikom(t *testing.T) {
	podrucje := 1
	sektor := "P"
	vodocuvarID := uuid.New()
	list := &models.VodocuvarskiList{UserID: vodocuvarID.String(), Sektor: sektor, AreaID: podrucje}
	prijava := &models.PrijavaSTerena{UserID: vodocuvarID.String(), Sektor: sektor, AreaID: podrucje, Status: models.PrijavaObjavljena}
	vod := &VodocuvarService{}
	prijave := &PrijavaService{vodocuvar: vod}

	osoba := func(d models.Duty) *models.UserPermissions {
		d.IsActive, d.SectorID = true, &sektor
		return models.NewUserPermissions(models.User{ID: uuid.New(), FullName: "Pero Perić", Duties: []models.Duty{d}})
	}
	jucer := time.Now().Add(-time.Hour)
	slucajevi := []struct {
		naziv string
		perms *models.UserPermissions
		vidi  bool
	}{
		{"rukovoditelj područja", osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &podrucje}), true},
		{"operater sektora", osoba(models.Duty{Role: models.RoleOperator, ScopeType: models.ScopeSector}), true},
		{"poslovođa izvođača", osoba(models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, AreaID: &podrucje}), true},
		{"istekli operater sektora", osoba(models.Duty{Role: models.RoleOperator, ScopeType: models.ScopeSector, ExpiresAt: &jucer}), false},
		{"istekli poslovođa izvođača", osoba(models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, AreaID: &podrucje, ExpiresAt: &jucer}), false},
		{"gost na području", osoba(models.Duty{Role: models.RoleGuest, ScopeType: models.ScopeArea, AreaID: &podrucje}), false},
		{"preglednik na području", osoba(models.Duty{Role: models.RoleViewer, ScopeType: models.ScopeArea, AreaID: &podrucje}), false},
	}
	for _, s := range slucajevi {
		if got := vod.SmijeVidjeti(s.perms, list); got != s.vidi {
			t.Errorf("%s: dnevnik %v, očekivano %v", s.naziv, got, s.vidi)
		}
		if got := prijave.SmijeVidjeti(s.perms, prijava); got != s.vidi {
			t.Errorf("%s: prijava %v, očekivano %v", s.naziv, got, s.vidi)
		}
	}
}

// Tko ne smije čitati dnevnik, ne smije ga ni parafirati, upisivati u njega
// ni zadavati zadatke vodočuvaru: gost i preglednik s dužnošću na području,
// ni osoba čija je dužnost istekla. Rukovoditelj područja, dežurni operater
// sektora i poslovođa izvođača (njih dvojica dok se ne odluči) smiju.
func TestParafiraSamoTkoVidiDnevnik(t *testing.T) {
	podrucje := 1
	sektor := "P"
	vodocuvarID := uuid.New()
	list := &models.VodocuvarskiList{UserID: vodocuvarID.String(), Sektor: sektor, AreaID: podrucje}
	vod := &VodocuvarService{}
	jucer := time.Now().Add(-time.Hour)

	osoba := func(d models.Duty) *models.UserPermissions {
		d.IsActive, d.SectorID = true, &sektor
		return models.NewUserPermissions(models.User{ID: uuid.New(), Username: "pperic", FullName: "Pero Perić", Duties: []models.Duty{d}})
	}
	slucajevi := []struct {
		naziv    string
		perms    *models.UserPermissions
		parafira bool
	}{
		{"rukovoditelj područja", osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &podrucje}), true},
		{"operater sektora", osoba(models.Duty{Role: models.RoleOperator, ScopeType: models.ScopeSector}), true},
		{"poslovođa izvođača", osoba(models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, AreaID: &podrucje}), true},
		{"gost na području", osoba(models.Duty{Role: models.RoleGuest, ScopeType: models.ScopeArea, AreaID: &podrucje}), false},
		{"preglednik na području", osoba(models.Duty{Role: models.RoleViewer, ScopeType: models.ScopeArea, AreaID: &podrucje}), false},
		{"preglednik sektora", osoba(models.Duty{Role: models.RoleViewer, ScopeType: models.ScopeSector}), false},
		{"nepoznata uloga na području", osoba(models.Duty{Role: models.Role("NEPOZNATA"), ScopeType: models.ScopeArea, AreaID: &podrucje}), false},
		{"istekli ovlaštenik na području", osoba(models.Duty{Role: models.RoleContractOfficerA3, ScopeType: models.ScopeArea, AreaID: &podrucje, ExpiresAt: &jucer}), false},
		{"istekli poslovođa izvođača", osoba(models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, AreaID: &podrucje, ExpiresAt: &jucer}), false},
	}
	for _, s := range slucajevi {
		if got := vod.SmijeParafirati(s.perms, list); got != s.parafira {
			t.Errorf("%s: parafira %v, očekivano %v", s.naziv, got, s.parafira)
		}
		// tko parafira, taj i čita; tko ne čita, ne parafira
		if got := vod.SmijeVidjeti(s.perms, list); got != s.parafira {
			t.Errorf("%s: vidi %v, očekivano %v", s.naziv, got, s.parafira)
		}
	}
}
