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
	// Doseg uprave je unija upravnih dužnosti, svaka na svojoj razini:
	// uprava sektora D i uprava područja 16 (sektor B) upravlja sektorom D
	// s razine 2, a područjem 16 s razine 3. Najviša razina je 2.
	mjesovita := permsWith(
		models.Duty{Role: models.RoleSectorLeader, SectorID: strp("D")},
		models.Duty{Role: models.RoleAreaLeader, SectorID: strp("B"), AreaID: intp(16)},
	)
	if r := actorRank(mjesovita); r != 2 {
		t.Fatalf("mješovita uprava: razina %d, očekivano 2", r)
	}
	if err := mayAssign(mjesovita, models.RoleAreaDeputy, strp("B"), intp(16), sectorsOf); err != nil {
		t.Errorf("u svom području 16 smije s razine 3: %v", err)
	}
	if r, err := razinaDodjele(mjesovita, models.RoleWaterGuard, strp("B"), intp(16), sectorsOf); err != nil || r != 3 {
		t.Errorf("vodočuvar u području 16 dodjeljuje se s razine 3, a dobiveno %d (%v)", r, err)
	}
	if err := mayAssign(mjesovita, models.RoleOperator, strp("D"), nil, sectorsOf); err != nil {
		t.Errorf("u svom sektoru D smije: %v", err)
	}
	// ...ali s razine 3 ne dijeli uloge sektora, a izvan oba dosega ništa;
	// greška je ona s najviše razine
	ocekujOdbijeno(t, mayAssign(mjesovita, models.RoleOperator, strp("B"), intp(16), sectorsOf), "izvan")
	ocekujOdbijeno(t, mayAssign(mjesovita, models.RoleAreaDeputy, strp("B"), intp(18), sectorsOf), "izvan")
	ocekujOdbijeno(t, mayAssign(mjesovita, models.RoleSectorMainDeputy, strp("D"), nil, sectorsOf), "više razine")

	// račun s dužnostima u oba dosega uređuje; lozinku poništava samo kad
	// je svaka dužnost niže od razine s koje njome upravlja
	pperic := &models.User{ID: uuid.New(), Username: "pperic", FullName: "Pero Perić", Duties: []models.Duty{
		{Role: models.RoleOperator, SectorID: strp("D"), IsActive: true},
		{Role: models.RoleWaterGuard, SectorID: strp("B"), AreaID: intp(16), IsActive: true},
	}}
	if err := mayManage(mjesovita, pperic, sectorsOf); err != nil {
		t.Errorf("osoba u sektoru D i području 16: %v", err)
	}
	ocekujOdbijeno(t, smijePonistiti(mjesovita, pperic, sectorsOf), "vašoj razini")
	pperic.Duties[0] = models.Duty{Role: models.RoleWaterGuard, SectorID: strp("D"), AreaID: intp(10), IsActive: true}
	if err := smijePonistiti(mjesovita, pperic, sectorsOf); err != nil {
		t.Errorf("vodočuvarima u sektoru D i području 16 poništava: %v", err)
	}
	// zamjenik rukovoditelja područja 16 je na razini 3, s koje mješovita
	// uprava upravlja područjem 16: uređuje ga, a lozinku mu ne poništava
	zamjenik := &models.User{ID: uuid.New(), Duties: []models.Duty{
		{Role: models.RoleAreaDeputy, SectorID: strp("B"), AreaID: intp(16), IsActive: true}}}
	if err := mayManage(mjesovita, zamjenik, sectorsOf); err != nil {
		t.Errorf("zamjenik rukovoditelja područja 16: %v", err)
	}
	ocekujOdbijeno(t, smijePonistiti(mjesovita, zamjenik, sectorsOf), "vašoj razini")
}

// Rok uprave ide po razini s koje se uloga dodjeljuje: privremena uprava
// sektora uz stalnu upravu područja daje stalnu dužnost u tom području, ali
// privremenu u ostatku sektora i za uloge sektora
func TestRokMjesoviteUprave(t *testing.T) {
	sutra := time.Now().Add(24 * time.Hour)
	sektor := models.Duty{ID: uuid.New(), Role: models.RoleSectorLeader, SectorID: strp("B"), ExpiresAt: &sutra}
	p := permsWith(sektor, models.Duty{Role: models.RoleAreaLeader, SectorID: strp("B"), AreaID: intp(16)})
	if r, izvor := rokUprave(p, models.RoleAreaDeputy, strp("B"), intp(16), sectorsOf); r != nil || izvor != nil {
		t.Errorf("u području 16 uprava je stalna: %v (%v)", r, izvor)
	}
	if r, izvor := rokUprave(p, models.RoleAreaDeputy, strp("B"), intp(18), sectorsOf); izvor == nil || izvor.ID != sektor.ID || r == nil || !r.Equal(sutra) {
		t.Errorf("u području 18 uprava je privremena uprava sektora: %v (%v)", r, izvor)
	}
	// uloga sektora dodjeljuje se s razine 2: uprava područja je ne čini stalnom
	if r, izvor := rokUprave(p, models.RoleSectorDeputy, strp("B"), intp(16), sectorsOf); izvor == nil || izvor.ID != sektor.ID || r == nil {
		t.Errorf("uloga sektora: %v (%v)", r, izvor)
	}
	if razineUprave(nil) != nil || len(razineUprave(permsWith(models.Duty{Role: models.RoleWaterGuard, AreaID: intp(16)}))) != 0 {
		t.Error("bez uprave nema razina")
	}
	if r := razineUprave(permsWith(models.Duty{Role: models.RoleNationalLeader}, models.Duty{Role: models.RoleAreaLeader, AreaID: intp(16)})); len(r) != 1 || r[0] != 1 {
		t.Errorf("uprava organizacije upravlja s razine 1: %v", r)
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
	if r, _ := rokUprave(p, models.RoleSectorDeputy, strp("B"), nil, sectorsOf); r == nil || !r.Equal(prekosutra) {
		t.Errorf("rok uprave: %v, očekivano %v", r, prekosutra)
	}
	// rok se traži za sektor područja, a ne za upisani sektor
	if r, _ := rokUprave(p, models.RoleAreaDeputy, strp("D"), intp(16), sectorsOf); r == nil || !r.Equal(prekosutra) {
		t.Errorf("rok uprave za područje 16 sektora B: %v", r)
	}

	// istekla uprava se ne broji; uz stalnu je uprava stalna
	stalna := permsWith(
		models.Duty{Role: models.RoleSectorLeader, SectorID: strp("B"), ExpiresAt: &jucer},
		models.Duty{Role: models.RoleSectorDeputy, SectorID: strp("B")},
	)
	if r, izvor := rokUprave(stalna, models.RoleSectorDeputy, strp("B"), nil, sectorsOf); r != nil || izvor != nil {
		t.Errorf("stalna uprava ima rok %v (%v)", r, izvor)
	}

	// Bez cilja gledaju se sve upravne dužnosti na toj razini: jedina
	// uprava područja je privremena, pa je i uprava privremena...
	podrucje := permsWith(models.Duty{Role: models.RoleAreaLeader, SectorID: strp("B"), AreaID: intp(16), ExpiresAt: &sutra})
	if r, izvor := rokUprave(podrucje, models.RoleAreaDeputy, nil, nil, sectorsOf); r == nil || !r.Equal(sutra) || izvor == nil {
		t.Errorf("uprava područja bez cilja: %v (%v), očekivano %v", r, izvor, sutra)
	}
	if r, izvor := rokUprave(p, models.RoleSectorDeputy, nil, nil, sectorsOf); r == nil || !r.Equal(prekosutra) || izvor == nil {
		t.Errorf("uprava sektora bez cilja: %v (%v), očekivano %v", r, izvor, prekosutra)
	}
	// ...a uz ijednu stalnu na toj razini je stalna
	dvije := permsWith(
		models.Duty{Role: models.RoleAreaLeader, SectorID: strp("B"), AreaID: intp(16), ExpiresAt: &sutra},
		models.Duty{Role: models.RoleAreaDeputy, SectorID: strp("B"), AreaID: intp(17)},
	)
	if r, izvor := rokUprave(dvije, models.RoleAreaDeputy, nil, nil, sectorsOf); r != nil || izvor != nil {
		t.Errorf("uz stalnu upravu područja bez cilja: %v (%v)", r, izvor)
	}

	// ograniciRok: traženi kraći rok ostaje, dulji se skraćuje na rok uprave
	kratki := sad.Add(time.Hour)
	if x := ograniciRok(p, models.RoleSectorDeputy, strp("B"), nil, "", sectorsOf, privremenost{rok: &kratki}, nil); !x.privremena || x.rok == nil || !x.rok.Equal(kratki) {
		t.Errorf("kraći rok mora ostati: %+v", x)
	}
	dugi := sad.Add(30 * 24 * time.Hour)
	if x := ograniciRok(p, models.RoleSectorDeputy, strp("B"), nil, "", sectorsOf, privremenost{rok: &dugi}, nil); !x.privremena || x.rok == nil || !x.rok.Equal(prekosutra) {
		t.Errorf("dulji rok mora pasti na rok uprave: %+v", x)
	}
	// izmjena iste dužnosti zadržava njezin dulji dosadašnji rok
	dosad := &models.Duty{Role: models.RoleSectorDeputy, SectorID: strp("B"), ExpiresAt: &dugi}
	if x := ograniciRok(p, models.RoleSectorDeputy, strp("B"), nil, "", sectorsOf, privremenost{privremena: true, rok: &dugi}, dosad); x.rok == nil || !x.rok.Equal(dugi) {
		t.Errorf("dosadašnji dulji rok iste dužnosti mora ostati: %v", x.rok)
	}
	// uloga koja ne upravlja ne skraćuje se
	if x := ograniciRok(p, models.RoleWaterGuard, strp("B"), intp(16), "", sectorsOf, privremenost{}, nil); x != (privremenost{}) {
		t.Errorf("vodočuvar ne dobiva rok uprave: %+v", x)
	}
}

// Privremeno imenovanje bez poznatog kraja (obrana još traje) je privremena
// uprava: što dodijeli na razini uprave ovisi o njoj i ističe s njom
func TestPrivremenaUpravaBezKraja(t *testing.T) {
	sad := time.Now()
	sutra := sad.Add(24 * time.Hour)
	imenovanje := models.Duty{ID: uuid.New(), Role: models.RoleAreaLeader, SectorID: strp("B"), AreaID: intp(16),
		IsTemporary: true, IsticeSObranom: true}
	p := permsWith(imenovanje)
	r, izvor := rokUprave(p, models.RoleAreaDeputy, strp("B"), intp(16), sectorsOf)
	if r != nil || izvor == nil || izvor.ID != imenovanje.ID {
		t.Fatalf("privremena uprava bez kraja: %v %+v", r, izvor)
	}
	// dodijeljena uprava područja: privremena, ovisi o imenovanju, zadani rok ostaje
	x := ograniciRok(p, models.RoleAreaDeputy, strp("B"), intp(16), "", sectorsOf, privremenost{rok: &sutra, sObranom: true}, nil)
	if !x.privremena || x.rok == nil || !x.rok.Equal(sutra) || !x.sObranom || x.ovisiO == nil || *x.ovisiO != imenovanje.ID {
		t.Errorf("dodijeljena uprava: %+v", x)
	}
	// bez zadanog roka nema ni roka: ističe s imenovanjem
	if x := ograniciRok(p, models.RoleAreaDeputy, strp("B"), intp(16), "", sectorsOf, privremenost{}, nil); !x.privremena || x.rok != nil || x.ovisiO == nil {
		t.Errorf("dodijeljena uprava bez roka: %+v", x)
	}
	// izmjena dosadašnje stalne dužnosti iste uloge i dosega je ne skraćuje
	stalna := &models.Duty{Role: models.RoleAreaDeputy, SectorID: strp("B"), AreaID: intp(16)}
	if x := ograniciRok(p, models.RoleAreaDeputy, strp("B"), intp(16), "", sectorsOf, privremenost{}, stalna); x != (privremenost{}) {
		t.Errorf("stalna ostaje stalna: %+v", x)
	}
	// dosadašnja privremena bez kraja ostaje bez kraja: spremanje je ne veže
	// za upravu koja je sprema
	otvorena := &models.Duty{Role: models.RoleAreaDeputy, SectorID: strp("B"), AreaID: intp(16), IsTemporary: true}
	if x := ograniciRok(p, models.RoleAreaDeputy, strp("B"), intp(16), "", sectorsOf, privremenost{privremena: true}, otvorena); !x.privremena || x.rok != nil || x.ovisiO != nil {
		t.Errorf("otvorena ostaje otvorena: %+v", x)
	}

	// od dvije privremene vrijedi ona bez kraja
	sDatumom := models.Duty{ID: uuid.New(), Role: models.RoleAreaLeader, SectorID: strp("B"), AreaID: intp(16), IsTemporary: true, ExpiresAt: &sutra}
	if r, izvor := rokUprave(permsWith(sDatumom, imenovanje), models.RoleAreaDeputy, strp("B"), intp(16), sectorsOf); r != nil || izvor == nil || izvor.ID != imenovanje.ID {
		t.Errorf("dulja je ona bez kraja: %v %+v", r, izvor)
	}
	if r, izvor := rokUprave(permsWith(imenovanje, sDatumom), models.RoleAreaDeputy, strp("B"), intp(16), sectorsOf); r != nil || izvor == nil || izvor.ID != imenovanje.ID {
		t.Errorf("dulja je ona bez kraja (obrnut red): %v %+v", r, izvor)
	}
	// izmjena zadržava dosadašnju ovisnost i istek s obranom (privremena
	// uprava ga ne skida); zadani rok ne ide preko kasnijeg od kraja uprave i
	// dosadašnjeg roka, a uprava bez poznatog kraja ga ne ograničava
	drugi := uuid.New()
	vezana := &models.Duty{Role: models.RoleAreaDeputy, SectorID: strp("B"), AreaID: intp(16), IsTemporary: true,
		Rok: &sutra, IsticeSObranom: true, OvisiO: &drugi}
	prekosutra := sutra.Add(24 * time.Hour)
	trazeno := privremenost{privremena: true, rok: &prekosutra}
	x = ograniciRok(permsWith(sDatumom), models.RoleAreaDeputy, strp("B"), intp(16), "", sectorsOf, trazeno, vezana)
	if !x.privremena || x.rok == nil || !x.rok.Equal(sutra) || !x.sObranom || x.ovisiO == nil || *x.ovisiO != drugi {
		t.Errorf("izmjena vezane, uprava do sutra: %+v", x)
	}
	x = ograniciRok(p, models.RoleAreaDeputy, strp("B"), intp(16), "", sectorsOf, trazeno, vezana)
	if x.rok == nil || !x.rok.Equal(prekosutra) || !x.sObranom || x.ovisiO == nil || *x.ovisiO != drugi {
		t.Errorf("izmjena vezane, uprava bez kraja: %+v", x)
	}
	// privremeni globalni administrator nije stalna uprava organizacije
	privremeniAdmin := permsWith(models.Duty{Role: models.RoleNationalLeader, IsTemporary: true})
	privremeniAdmin.IsGlobalAdmin = true
	if stalnaUpravaOrganizacije(privremeniAdmin) {
		t.Errorf("privremena uprava organizacije ne smije biti stalna")
	}
	if stalnaUpravaOrganizacije(nil) {
		t.Errorf("bez ovlasti nema stalne uprave")
	}
}

// Kraj od dva roka: nil je „bez kraja”
func TestRaniji(t *testing.T) {
	a, b := time.Now(), time.Now().Add(time.Hour)
	for _, tc := range []struct {
		x, y, raniji, kasniji *time.Time
	}{
		{nil, nil, nil, nil},
		{&a, nil, &a, nil},
		{nil, &b, &b, nil},
		{&a, &b, &a, &b},
		{&b, &a, &a, &b},
	} {
		if r := raniji(tc.x, tc.y); !istiKraj(r, tc.raniji) {
			t.Errorf("raniji(%v, %v) = %v", tc.x, tc.y, r)
		}
		if r := kasniji(tc.x, tc.y); !istiKraj(r, tc.kasniji) {
			t.Errorf("kasniji(%v, %v) = %v", tc.x, tc.y, r)
		}
	}
}
