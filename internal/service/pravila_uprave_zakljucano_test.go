package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"

	"github.com/google/uuid"
)

// Zaključava današnja pravila dodjele dužnosti i upravljanja računima
// (user_rules.go, password_reset.go) za rubne i neispravne unose. Gdje je
// ponašanje upitno, test ga bilježi onakvo kakvo jest, uz napomenu.

func ocekujOdbijeno(t *testing.T, err error, dio string) {
	t.Helper()
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("očekivano odbijanje (ErrUnauthorized), dobiveno %v", err)
	}
	if dio != "" && !strings.Contains(err.Error(), dio) {
		t.Errorf("poruka %q ne sadrži %q", err.Error(), dio)
	}
}

func TestRazinaUpraveActora(t *testing.T) {
	if actorRank(nil) != 0 {
		t.Error("nil ovlasti nemaju razinu")
	}
	if r := actorRank(permsWith(models.Duty{Role: models.RoleViewer, ScopeType: models.ScopeAll})); r != 0 {
		t.Errorf("preglednik upravlja s razine %d", r)
	}
	if r := actorRank(permsWith(models.Duty{Role: models.RoleOperator, SectorID: strp("B")})); r != 0 {
		t.Errorf("operater upravlja s razine %d", r)
	}
	// Vrijedi samo najviša razina: uprava sektora D i uprava područja 16
	// (sektor B) zajedno daju razinu 2, pa područje 16 ispada iz dosega.
	mjesovita := permsWith(
		models.Duty{Role: models.RoleSectorLeader, SectorID: strp("D")},
		models.Duty{Role: models.RoleAreaLeader, SectorID: strp("B"), AreaID: intp(16)},
	)
	if r := actorRank(mjesovita); r != 2 {
		t.Fatalf("mješovita uprava: razina %d, očekivano 2", r)
	}
	ocekujOdbijeno(t, mayAssign(mjesovita, models.RoleAreaDeputy, strp("B"), intp(16), sectorsOf), "izvan")
	if err := mayAssign(mjesovita, models.RoleOperator, strp("D"), nil, sectorsOf); err != nil {
		t.Errorf("u svom sektoru D smije: %v", err)
	}
}

func TestDodjelaRubniSlucajevi(t *testing.T) {
	sektorB := permsWith(models.Duty{Role: models.RoleSectorLeader, SectorID: strp("B")})
	podrucje16 := permsWith(models.Duty{Role: models.RoleAreaLeader, SectorID: strp("B"), AreaID: intp(16)})
	preglednik := permsWith(models.Duty{Role: models.RoleViewer, ScopeType: models.ScopeAll})

	ocekujOdbijeno(t, mayAssign(preglednik, models.RoleWaterGuard, strp("B"), intp(16), sectorsOf), "")
	ocekujOdbijeno(t, mayAssign(nil, models.RoleWaterGuard, strp("B"), intp(16), sectorsOf), "")

	// globalnog administratora daje samo razina 1, bez dodatne poruke
	if err := mayAssign(sektorB, models.RoleGlobalAdmin, nil, nil, sectorsOf); err != ErrUnauthorized {
		t.Errorf("uprava sektora dijeli globalnog administratora: %v", err)
	}
	ocekujOdbijeno(t, mayAssign(sektorB, models.RoleSectorMainDeputy, strp("B"), nil, sectorsOf), "više razine")
	if err := mayAssign(sektorB, models.RoleSectorDeputy, strp("B"), nil, sectorsOf); err != nil {
		t.Errorf("ista razina smije dodijeliti: %v", err)
	}
	ocekujOdbijeno(t, mayAssign(sektorB, models.RoleOperator, strp("D"), nil, sectorsOf), "izvan")
	// područje iz svog sektora uz upisan tuđi sektor ne prolazi
	ocekujOdbijeno(t, mayAssign(sektorB, models.RoleAreaLeader, strp("D"), intp(16), sectorsOf), "izvan")

	// uprava područja ne dijeli zamjenika rukovoditelja sektora za područje
	// (dodjeljuje se s razine 2), ni u svom području
	ocekujOdbijeno(t, mayAssign(podrucje16, models.RoleSectorAreaDeputy, strp("B"), intp(16), sectorsOf), "više razine")
	ocekujOdbijeno(t, mayAssign(podrucje16, models.RoleAreaDeputy, strp("D"), intp(16), sectorsOf), "izvan")
	ocekujOdbijeno(t, mayAssign(podrucje16, models.RoleAreaDeputy, strp("B"), nil, sectorsOf), "izvan")
	// nepoznatu ulogu ne dodjeljuje nitko, ni globalni administrator
	ocekujOdbijeno(t, mayAssign(podrucje16, models.Role("NEPOZNATA"), strp("B"), intp(16), sectorsOf), "nepoznata uloga")
	ocekujOdbijeno(t, mayAssign(permsWith(models.Duty{Role: models.RoleGlobalAdmin}), models.Role(""), nil, nil, sectorsOf), "nepoznata uloga")
}

func TestUpravljanjeTudjimRacunom(t *testing.T) {
	sektorB := permsWith(models.Duty{Role: models.RoleSectorLeader, SectorID: strp("B")})
	admin := &models.User{ID: uuid.New(), IsGlobalAdmin: true, Duties: []models.Duty{
		{Role: models.RoleWaterGuard, SectorID: strp("B"), AreaID: intp(16), IsActive: true}}}
	ocekujOdbijeno(t, mayManage(sektorB, admin, sectorsOf), "")
	if mayManage(permsWith(models.Duty{Role: models.RoleGlobalAdmin}), admin, sectorsOf) != nil {
		t.Error("razina 1 upravlja svakim računom")
	}
	bez := &models.User{ID: uuid.New(), IsActive: true}
	ocekujOdbijeno(t, mayManage(sektorB, bez, sectorsOf), "nema dužnosti")

	if err := zaduzujeTudji(nil); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("nepostojeći račun: %v", err)
	}

	// pregled stanja računa (canManageTarget) traži samo jednu dužnost u dosegu
	pperic := &models.User{ID: uuid.New(), Username: "pperic", FullName: "Pero Perić", Duties: []models.Duty{
		{Role: models.RoleSectorLeader, SectorID: strp("D"), IsActive: true},
		{Role: models.RoleWaterGuard, SectorID: strp("B"), AreaID: intp(16), IsActive: true},
	}}
	podrucje16 := permsWith(models.Duty{Role: models.RoleAreaLeader, SectorID: strp("B"), AreaID: intp(16)})
	if !canManageTarget(podrucje16, pperic) {
		t.Error("uprava područja 16 vidi stanje računa osobe s dužnošću u području 16")
	}
	// ...a lozinku mu ne smije poništiti jer ne upravlja svim njegovim dužnostima
	ocekujOdbijeno(t, smijePonistiti(podrucje16, pperic, sectorsOf), "")
	if canManageTarget(nil, pperic) || canManageTarget(podrucje16, nil) {
		t.Error("bez ovlasti ili bez računa nema pregleda")
	}
}

func TestPonistenjeLozinkeOperatoru(t *testing.T) {
	sektorB := permsWith(models.Duty{Role: models.RoleSectorLeader, SectorID: strp("B")})
	vodocuvar := &models.User{ID: uuid.New(), Duties: []models.Duty{
		{Role: models.RoleWaterGuard, SectorID: strp("B"), AreaID: intp(16), IsActive: true}}}
	if err := smijePonistiti(sektorB, vodocuvar, sectorsOf); err != nil {
		t.Errorf("uprava sektora poništava lozinku vodočuvaru svog sektora: %v", err)
	}
	// Operater ničim ne upravlja, ali je na razini 2 kataloga, pa ga uprava
	// sektora uređuje, a lozinku mu ne smije poništiti.
	operater := &models.User{ID: uuid.New(), Duties: []models.Duty{
		{Role: models.RoleOperator, SectorID: strp("B"), IsActive: true}}}
	if err := mayManage(sektorB, operater, sectorsOf); err != nil {
		t.Fatalf("uprava sektora uređuje operatera svog sektora: %v", err)
	}
	ocekujOdbijeno(t, smijePonistiti(sektorB, operater, sectorsOf), "vašoj razini")
}

func TestDosegIzUlogeRubniSlucajevi(t *testing.T) {
	t.Run("dionica izvan registra", func(t *testing.T) {
		_, _, _, err := normalizeScope(models.RoleSectionLeader, nil, nil, "X.1.1", sectorsOf, dioniceOf)
		if !errors.Is(err, ErrInvalidUserData) || !strings.Contains(err.Error(), "nije u registru") {
			t.Errorf("dionica izvan registra: %v", err)
		}
	})
	t.Run("dionice iz dva područja", func(t *testing.T) {
		_, _, _, err := normalizeScope(models.RoleSectionLeader, nil, nil, "B.16.1, B.18.1", sectorsOf, dioniceOf)
		if !errors.Is(err, ErrInvalidUserData) || !strings.Contains(err.Error(), "nije u području 16") {
			t.Errorf("dionice iz dva područja: %v", err)
		}
	})
	t.Run("nepoznato područje", func(t *testing.T) {
		_, _, _, err := normalizeScope(models.RoleAreaLeader, nil, intp(99), "", sectorsOf, dioniceOf)
		if !errors.Is(err, ErrInvalidUserData) || !strings.Contains(err.Error(), "područje 99 nije u registru") {
			t.Errorf("nepoznato područje: %v", err)
		}
	})
	t.Run("upisani sektor se ne slaže s područjem", func(t *testing.T) {
		_, _, _, err := normalizeScope(models.RoleAreaLeader, strp("D"), intp(16), "", sectorsOf, dioniceOf)
		if !errors.Is(err, ErrInvalidUserData) || !strings.Contains(err.Error(), "nije u sektoru D") {
			t.Errorf("tuđi sektor uz područje: %v", err)
		}
	})
	t.Run("područje 0 i prazan sektor kao da ih nema", func(t *testing.T) {
		_, _, _, err := normalizeScope(models.RoleOperator, strp("  "), intp(0), "", sectorsOf, dioniceOf)
		if !errors.Is(err, ErrInvalidUserData) {
			t.Errorf("uloga sektora bez sektora mora biti odbijena: %v", err)
		}
	})
	t.Run("uloga sektora zadržava upisano područje", func(t *testing.T) {
		doseg, s, a, err := normalizeScope(models.RoleOperator, nil, intp(16), "", sectorsOf, dioniceOf)
		if err != nil || doseg != models.ScopeSector || s == nil || *s != "B" || a == nil || *a != 16 {
			t.Errorf("operator s područjem: %s %v %v %v", doseg, s, a, err)
		}
	})
	t.Run("dionica određuje područje i sektor", func(t *testing.T) {
		doseg, s, a, err := normalizeScope(models.RoleSectionLeader, nil, nil, "B.16.2", sectorsOf, dioniceOf)
		if err != nil || doseg != models.ScopeSection || s == nil || *s != "B" || a == nil || *a != 16 {
			t.Errorf("dionica B.16.2: %s %v %v %v", doseg, s, a, err)
		}
	})
	t.Run("terenska uloga bez dionica postaje dužnost područja", func(t *testing.T) {
		doseg, _, _, err := normalizeScope(models.RoleWaterGuard, nil, intp(16), "", sectorsOf, dioniceOf)
		if err != nil || doseg != models.ScopeArea {
			t.Errorf("vodočuvar bez dionica: %s %v", doseg, err)
		}
	})
	t.Run("uloga dosega ALL ne provjerava ništa", func(t *testing.T) {
		// Sektor, područje i dionice se odbacuju, a dionice se ne provjeravaju;
		// pozivatelj (AddDuty, UpdateDuty) zadrži upisane šifre.
		doseg, s, a, err := normalizeScope(models.RoleNationalLeader, strp("D"), intp(99), "X.9.9", sectorsOf, dioniceOf)
		if err != nil || doseg != models.ScopeAll || s != nil || a != nil {
			t.Errorf("uloga dosega ALL: %s %v %v %v", doseg, s, a, err)
		}
	})
}

func TestRokPrivremeneUprave(t *testing.T) {
	sad := time.Now()
	prekosutra := sad.Add(48 * time.Hour)
	sutra := sad.Add(24 * time.Hour)
	jucer := sad.Add(-24 * time.Hour)

	// od dvije privremene uprave istog sektora vrijedi dulja
	p := permsWith(
		models.Duty{Role: models.RoleSectorDeputy, SectorID: strp("B"), ExpiresAt: &sutra},
		models.Duty{Role: models.RoleSectorLeader, SectorID: strp("B"), ExpiresAt: &prekosutra},
	)
	if r := rokUprave(p, strp("B"), nil, sectorsOf); r == nil || !r.Equal(prekosutra) {
		t.Errorf("rok uprave: %v, očekivano %v", r, prekosutra)
	}
	// rok se traži za sektor područja, a ne za upisani sektor
	if r := rokUprave(p, strp("D"), intp(16), sectorsOf); r == nil || !r.Equal(prekosutra) {
		t.Errorf("rok uprave za područje 16 sektora B: %v", r)
	}

	// istekla uprava se ne broji; uz stalnu je uprava stalna
	stalna := permsWith(
		models.Duty{Role: models.RoleSectorLeader, SectorID: strp("B"), ExpiresAt: &jucer},
		models.Duty{Role: models.RoleSectorDeputy, SectorID: strp("B")},
	)
	if r := rokUprave(stalna, strp("B"), nil, sectorsOf); r != nil {
		t.Errorf("stalna uprava ima rok %v", r)
	}

	// Uprava područja bez cilja područja ne nađe dužnost i vrati nil, što
	// znači „stalna”, iako je jedina uprava privremena.
	podrucje := permsWith(models.Duty{Role: models.RoleAreaLeader, SectorID: strp("B"), AreaID: intp(16), ExpiresAt: &sutra})
	if r := rokUprave(podrucje, nil, nil, sectorsOf); r != nil {
		t.Errorf("bez područja danas nema roka, a dobiveno %v", r)
	}

	// ograniciRok: traženi kraći rok ostaje, dulji se skraćuje na rok uprave
	kratki := sad.Add(time.Hour)
	if priv, rok := ograniciRok(p, models.RoleSectorDeputy, strp("B"), nil, "", sectorsOf, false, &kratki, nil); !priv || rok == nil || !rok.Equal(kratki) {
		t.Errorf("kraći rok mora ostati: %v %v", priv, rok)
	}
	dugi := sad.Add(30 * 24 * time.Hour)
	if priv, rok := ograniciRok(p, models.RoleSectorDeputy, strp("B"), nil, "", sectorsOf, false, &dugi, nil); !priv || rok == nil || !rok.Equal(prekosutra) {
		t.Errorf("dulji rok mora pasti na rok uprave: %v %v", priv, rok)
	}
	// izmjena iste dužnosti zadržava njezin dulji dosadašnji rok
	dosad := &models.Duty{Role: models.RoleSectorDeputy, SectorID: strp("B"), ExpiresAt: &dugi}
	if _, rok := ograniciRok(p, models.RoleSectorDeputy, strp("B"), nil, "", sectorsOf, true, &dugi, dosad); rok == nil || !rok.Equal(dugi) {
		t.Errorf("dosadašnji dulji rok iste dužnosti mora ostati: %v", rok)
	}
	// uloga koja ne upravlja ne skraćuje se
	if priv, rok := ograniciRok(p, models.RoleWaterGuard, strp("B"), intp(16), "", sectorsOf, false, nil, nil); priv || rok != nil {
		t.Errorf("vodočuvar ne dobiva rok uprave: %v %v", priv, rok)
	}
}
