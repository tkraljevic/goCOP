package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Ovlasti u knjizi vodočuvara po Odlukama (8. 10. 2026.), pitanje 1. Podaci
// su izmišljeni: sektor P, područja 1 i 2. Pero Perić je vodočuvar dionice
// P.1.1, Ana Anić vodočuvar cijelog područja 1 (bez upisanih dionica).

type knjigaProba struct {
	o          *okolinaVodocuvara
	pero, ana  *models.User
	listPere   *models.VodocuvarskiList
	listAne    *models.VodocuvarskiList
	sektor     string
	p1, p2     int
	obranaNaPS map[string]models.DefensePhase // stadij po dionici; nil = bez izvora
}

func novaKnjigaProba(t *testing.T) *knjigaProba {
	t.Helper()
	o := novaOkolinaVodocuvara(t)
	users := repository.NewUserRepository(o.baza, ledger.New(o.baza, "test"))
	o.vs.users = NewUserService(users, nil, nil)
	k := &knjigaProba{o: o, sektor: "P", p1: 1, p2: 2}
	vodocuvar := func(korisnik, ime, dionice string) *models.User {
		t.Helper()
		u := &models.User{Username: korisnik, FullName: ime, IsActive: true}
		d := models.Duty{Title: "Vodočuvar", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, SectorID: &k.sektor, AreaID: &k.p1,
			SectionCodes: dionice, IsActive: true, IsPrimary: true}
		if dionice == "" {
			d.ScopeType = models.ScopeArea
		}
		if err := users.CreateUser(u, &d); err != nil {
			t.Fatal(err)
		}
		return u
	}
	k.pero = vodocuvar("pperic", "Pero Perić", "P.1.1")
	k.ana = vodocuvar("aanic", "Ana Anić", "")
	sad := time.Now()
	k.listPere = &models.VodocuvarskiList{UserID: k.pero.ID.String(), Ime: k.pero.FullName, Sektor: "P", AreaID: 1, PredanoAt: &sad}
	k.listAne = &models.VodocuvarskiList{UserID: k.ana.ID.String(), Ime: k.ana.FullName, Sektor: "P", AreaID: 1, PredanoAt: &sad}
	return k
}

// obrana postavlja izvor stanja obrane: zadani stadij na dionici
func (k *knjigaProba) obrana(stadiji map[string]models.DefensePhase) {
	k.obranaNaPS = stadiji
	k.o.vs.SetStanjeObrane(func(_ context.Context, sektor, dionica string, _ time.Time) (models.StanjeObrane, []models.Akt, error) {
		if sektor != "P" {
			return models.StanjeObrane{}, nil, errors.New("nepoznat sektor")
		}
		if dionica == "GRESKA" {
			return models.StanjeObrane{}, nil, errors.New("akti se ne čitaju")
		}
		st := models.StanjeObrane{IzAkata: true}
		if f, ima := k.obranaNaPS[dionica]; ima {
			st.Aktivni = []models.AktivniStadij{{Stupanj: f}}
		}
		return st, nil, nil
	})
}

// osoba je Marko Markić s jednom aktivnom dužnošću u sektoru P
func (k *knjigaProba) osoba(d models.Duty) *models.UserPermissions {
	d.IsActive, d.SectorID = true, &k.sektor
	return models.NewUserPermissions(models.User{ID: uuid.New(), Username: "mmarkic", FullName: "Marko Markić", IsActive: true, Duties: []models.Duty{d}})
}

func (k *knjigaProba) dionicar(uloga models.Role, podrucje *int, dionice string) *models.UserPermissions {
	return k.osoba(models.Duty{Role: uloga, ScopeType: models.ScopeSection, AreaID: podrucje, SectionCodes: dionice})
}

type ocekivanjeKnjige struct {
	vidi, parafira, biljeska, zadatak bool
}

func (k *knjigaProba) provjeri(t *testing.T, naziv string, perms *models.UserPermissions, l *models.VodocuvarskiList, o ocekivanjeKnjige) {
	t.Helper()
	ctx := context.Background()
	vs := k.o.vs
	if got := vs.SmijeVidjeti(perms, l); got != o.vidi {
		t.Errorf("%s (%s): vidi %v, očekivano %v", naziv, l.Ime, got, o.vidi)
	}
	if got := vs.SmijeParafirati(perms, l); got != o.parafira {
		t.Errorf("%s (%s): parafira %v, očekivano %v", naziv, l.Ime, got, o.parafira)
	}
	if got := vs.SmijeUpisati(perms, l); got != o.biljeska {
		t.Errorf("%s (%s): bilješka %v, očekivano %v", naziv, l.Ime, got, o.biljeska)
	}
	if got := vs.SmijeZadati(ctx, perms, l); got != o.zadatak {
		t.Errorf("%s (%s): zadatak %v, očekivano %v", naziv, l.Ime, got, o.zadatak)
	}
}

// Tablica odluke: operater sektora čita i upisuje bilješku, ali ne
// parafira i ne zadaje; poslovođa ništa; rukovoditelj BP, ovlaštenici A2 i
// A3, zamjenici i uprava sektora sve; rukovoditelj dionice (i zamjenik) čita
// listove svog područja, a parafira i upisuje samo na list vezan uz svoju
// dionicu; rukovoditelj dionice drugog područja ništa.
func TestKnjigaVodocuvaraPoOdlukama(t *testing.T) {
	k := novaKnjigaProba(t)
	k.obrana(map[string]models.DefensePhase{"P.1.1": models.PhaseRegular, "P.1.2": models.PhaseRegular})
	sve := ocekivanjeKnjige{true, true, true, true}
	nista := ocekivanjeKnjige{}
	slucajevi := []struct {
		naziv     string
		perms     *models.UserPermissions
		pero, ana ocekivanjeKnjige
	}{
		{"dežurni operater sektora", k.osoba(models.Duty{Role: models.RoleOperator, ScopeType: models.ScopeSector}),
			ocekivanjeKnjige{vidi: true, biljeska: true}, ocekivanjeKnjige{vidi: true, biljeska: true}},
		{"dežurni operater drugog sektora", models.NewUserPermissions(models.User{ID: uuid.New(), Duties: []models.Duty{{IsActive: true, Role: models.RoleOperator, ScopeType: models.ScopeSector, SectorID: strp("Q")}}}),
			nista, nista},
		{"poslovođa izvođača na području", k.osoba(models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, AreaID: &k.p1}), nista, nista},
		{"rukovoditelj područja", k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &k.p1}), sve, sve},
		{"zamjenik rukovoditelja područja", k.osoba(models.Duty{Role: models.RoleAreaDeputy, ScopeType: models.ScopeArea, AreaID: &k.p1}), sve, sve},
		{"rukovoditelj drugog područja", k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &k.p2}), nista, nista},
		{"ovlaštenik A3", k.osoba(models.Duty{Role: models.RoleContractOfficerA3, ScopeType: models.ScopeArea, AreaID: &k.p1}), sve, sve},
		{"ovlaštenik A2", k.osoba(models.Duty{Role: models.RoleContractOfficerA2, ScopeType: models.ScopeArea, AreaID: &k.p1}), sve, sve},
		{"zamjenik ovlaštenika A2", k.osoba(models.Duty{Role: models.RoleContractDeputyA2, ScopeType: models.ScopeArea, AreaID: &k.p1}), sve, sve},
		{"zamjenik ovlaštenika A3", k.osoba(models.Duty{Role: models.RoleContractDeputyA3, ScopeType: models.ScopeArea, AreaID: &k.p1}), sve, sve},
		{"rukovoditelj sektora", k.osoba(models.Duty{Role: models.RoleSectorLeader, ScopeType: models.ScopeSector}), sve, sve},
		{"voditelj COP-a", k.osoba(models.Duty{Role: models.RoleCopLeader, ScopeType: models.ScopeSector}), sve, sve},
		{"rukovoditelj dionice P.1.1", k.dionicar(models.RoleSectionLeader, &k.p1, "P.1.1"), sve, sve},
		{"zamjenik rukovoditelja dionice P.1.1", k.dionicar(models.RoleSectionDeputy, &k.p1, "P.1.1"), sve, sve},
		{"rukovoditelj dionice P.1.2", k.dionicar(models.RoleSectionLeader, &k.p1, "P.1.2"), ocekivanjeKnjige{vidi: true}, sve},
		{"rukovoditelj dionice bez upisanih dionica", k.dionicar(models.RoleSectionLeader, &k.p1, ""), ocekivanjeKnjige{vidi: true}, ocekivanjeKnjige{vidi: true}},
		{"rukovoditelj dionice drugog područja", k.dionicar(models.RoleSectionLeader, &k.p2, "P.2.1"), nista, nista},
		{"vodočuvar istog područja", k.osoba(models.Duty{Role: models.RoleWaterGuard, ScopeType: models.ScopeArea, AreaID: &k.p1}), nista, nista},
	}
	for _, s := range slucajevi {
		k.provjeri(t, s.naziv, s.perms, k.listPere, s.pero)
		k.provjeri(t, s.naziv, s.perms, k.listAne, s.ana)
	}
	// vodočuvar čita svoju knjigu, ali u nju ne upisuje kao rukovoditelj
	pero, err := k.o.vs.users.GetUserByID(k.pero.ID)
	if err != nil || pero == nil {
		t.Fatalf("Pero Perić: %v", err)
	}
	k.provjeri(t, "vlastita knjiga", models.NewUserPermissions(*pero), k.listPere, ocekivanjeKnjige{vidi: true})
	// bez ovlasti nema ničega
	k.provjeri(t, "bez ovlasti", nil, k.listPere, nista)
	if k.o.vs.SmijeVidjeti(k.osoba(models.Duty{Role: models.RoleAreaLeader, AreaID: &k.p1}), nil) {
		t.Error("bez lista nema čitanja")
	}
}

// Rukovoditelj dionice zadaje zadatak vodočuvaru svoje dionice samo dok na
// njoj traje obrana od poplava: redovna ili izvanredna obrana ili
// izvanredno stanje iz ovjerenih akata. Pripremno stanje nije obrana; bez
// izvora stanja obrane, uz grešku čitanja akata ili kad obrana traje samo na
// drugoj dionici, zadatka nema. Parafa i bilješka ne ovise o obrani.
func TestRukovoditeljDioniceZadajeUObrani(t *testing.T) {
	k := novaKnjigaProba(t)
	dionicar := k.dionicar(models.RoleSectionLeader, &k.p1, "P.1.1, P.1.3")
	zadaje := func() bool { return k.o.vs.SmijeZadati(context.Background(), dionicar, k.listPere) }

	if zadaje() {
		t.Error("bez izvora stanja obrane rukovoditelj dionice zadaje zadatak")
	}
	slucajevi := []struct {
		naziv   string
		stadiji map[string]models.DefensePhase
		zadaje  bool
	}{
		{"bez obrane", map[string]models.DefensePhase{}, false},
		{"pripremno stanje", map[string]models.DefensePhase{"P.1.1": models.PhasePrep}, false},
		{"redovna obrana", map[string]models.DefensePhase{"P.1.1": models.PhaseRegular}, true},
		{"izvanredna obrana", map[string]models.DefensePhase{"P.1.1": models.PhaseEmergency}, true},
		{"izvanredno stanje", map[string]models.DefensePhase{"P.1.1": models.PhaseState}, true},
		// P.1.3 je njegova, ali je Pero ne čuva: list s njom nema veze
		{"obrana samo na dionici bez veze s listom", map[string]models.DefensePhase{"P.1.3": models.PhaseRegular}, false},
	}
	for _, s := range slucajevi {
		k.obrana(s.stadiji)
		if got := zadaje(); got != s.zadaje {
			t.Errorf("%s: zadaje %v, očekivano %v", s.naziv, got, s.zadaje)
		}
		if !k.o.vs.SmijeParafirati(dionicar, k.listPere) || !k.o.vs.SmijeUpisati(dionicar, k.listPere) {
			t.Errorf("%s: parafa i bilješka ovise o obrani", s.naziv)
		}
	}
	// greška čitanja akata: obrana se ne pretpostavlja
	k.obrana(map[string]models.DefensePhase{"P.1.1": models.PhaseRegular})
	if k.o.vs.obranaNaDionicama(context.Background(), "P", []string{"GRESKA"}) {
		t.Error("uz grešku čitanja akata obrana se pretpostavlja")
	}
	if k.o.vs.obranaNaDionicama(context.Background(), "Q", []string{"P.1.1"}) {
		t.Error("obrana iz tuđeg sektora")
	}
}

// Prijave s terena: rukovoditelj dionice čita samo prijave koje navode
// njegovu dionicu (bez obzira na velika slova i razmake, i kad ih je
// navedeno više); dežurni operater sektora čita sve, poslovođa nijednu, a
// nacrt čita samo vodočuvar.
func TestPrijaveRukovoditeljaDionice(t *testing.T) {
	k := novaKnjigaProba(t)
	prijave := &PrijavaService{vodocuvar: k.o.vs}
	prijava := func(dionica string) *models.PrijavaSTerena {
		return &models.PrijavaSTerena{UserID: k.ana.ID.String(), Sektor: "P", AreaID: 1, DionicaCode: dionica, Status: models.PrijavaObjavljena}
	}
	dionicar := k.dionicar(models.RoleSectionLeader, &k.p1, "P.1.1")
	zamjenik := k.dionicar(models.RoleSectionDeputy, &k.p1, "P.1.1")
	operater := k.osoba(models.Duty{Role: models.RoleOperator, ScopeType: models.ScopeSector})
	poslovodja := k.osoba(models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, AreaID: &k.p1})
	slucajevi := []struct {
		naziv, dionica              string
		dionicar, operater, poslovo bool
	}{
		{"prijava njegove dionice", "P.1.1", true, true, false},
		{"malim slovima i s razmakom", " p.1.1 ", true, true, false},
		{"više dionica", "P.1.2, P.1.1", true, true, false},
		{"druga dionica", "P.1.2", false, true, false},
		{"bez dionice", "", false, true, false},
	}
	for _, s := range slucajevi {
		p := prijava(s.dionica)
		if got := prijave.SmijeVidjeti(dionicar, p); got != s.dionicar {
			t.Errorf("%s: rukovoditelj dionice vidi %v, očekivano %v", s.naziv, got, s.dionicar)
		}
		if got := prijave.SmijeVidjeti(zamjenik, p); got != s.dionicar {
			t.Errorf("%s: zamjenik rukovoditelja dionice vidi %v, očekivano %v", s.naziv, got, s.dionicar)
		}
		if got := prijave.SmijeVidjeti(operater, p); got != s.operater {
			t.Errorf("%s: operater vidi %v, očekivano %v", s.naziv, got, s.operater)
		}
		if got := prijave.SmijeVidjeti(poslovodja, p); got != s.poslovo {
			t.Errorf("%s: poslovođa vidi %v, očekivano %v", s.naziv, got, s.poslovo)
		}
		// riješenom je ni operater ni rukovoditelj dionice ne označuju
		if prijave.SmijeRijesiti(operater, p) || prijave.SmijeRijesiti(dionicar, p) {
			t.Errorf("%s: operater ili rukovoditelj dionice rješava prijavu", s.naziv)
		}
	}
	nacrt := prijava("P.1.1")
	nacrt.Status = models.PrijavaNacrt
	if prijave.SmijeVidjeti(dionicar, nacrt) {
		t.Error("rukovoditelj dionice vidi nacrt prijave")
	}
}

// Kroz servis: operater upisuje bilješku, ali ne parafira i ne zadaje;
// rukovoditelj dionice zadaje vodočuvaru svoje dionice samo u obrani, a na
// popisu za zadatke i bilješke su samo oni koje smije.
func TestKnjigaKrozServis(t *testing.T) {
	k := novaKnjigaProba(t)
	ctx := context.Background()
	vs := k.o.vs
	dan := vdDan(time.March, 10)
	operater := k.osoba(models.Duty{Role: models.RoleOperator, ScopeType: models.ScopeSector})
	if _, err := vs.Upisi(ctx, operater, &operater.User, k.pero.ID.String(), dan, "Javljeno iz COP-a u 2 h"); err != nil {
		t.Fatalf("bilješka operatera: %v", err)
	}
	if _, err := vs.ZadajZadatak(ctx, operater, &operater.User, k.pero.ID.String(), "pregledati nasip", time.Time{}); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("operater zadaje zadatak: %v", err)
	}
	l := k.o.vdListIzBaze(t, k.pero, dan)
	if l == nil || len(l.Upisi) != 1 || l.Upisi[0].Ime != "Marko Markić" {
		t.Fatalf("bilješka operatera na listu: %+v", l)
	}
	l.PredanoAt = &dan
	if err := k.o.repo.Save(ctx, l); err != nil {
		t.Fatal(err)
	}
	if _, err := vs.Parafiraj(ctx, operater, &operater.User, l.ID); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("operater parafira: %v", err)
	}
	if len(vs.Vodocuvari(ctx, operater)) != 0 {
		t.Error("operater ima popis vodočuvara za zadatke")
	}
	if n := len(vs.VodocuvariZaBiljesku(ctx, operater)); n != 2 {
		t.Errorf("operater upisuje bilješke za %d vodočuvara, očekivano 2", n)
	}

	dionicar := k.dionicar(models.RoleSectionLeader, &k.p1, "P.1.2")
	if _, err := vs.Upisi(ctx, dionicar, &dionicar.User, k.pero.ID.String(), dan, "nije moja dionica"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("rukovoditelj druge dionice upisuje bilješku: %v", err)
	}
	if _, err := vs.ZadajZadatak(ctx, dionicar, &dionicar.User, k.ana.ID.String(), "pregledati propust", time.Time{}); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("rukovoditelj dionice zadaje izvan obrane: %v", err)
	}
	if n := len(vs.Vodocuvari(ctx, dionicar)); n != 0 {
		t.Errorf("izvan obrane rukovoditelj dionice zadaje %d vodočuvara", n)
	}
	k.obrana(map[string]models.DefensePhase{"P.1.2": models.PhaseEmergency})
	if _, err := vs.ZadajZadatak(ctx, dionicar, &dionicar.User, k.ana.ID.String(), "pregledati propust", time.Time{}); err != nil {
		t.Errorf("rukovoditelj dionice u obrani: %v", err)
	}
	popis := vs.Vodocuvari(ctx, dionicar)
	if len(popis) != 1 || popis[0].ID != k.ana.ID {
		t.Errorf("u obrani rukovoditelj dionice P.1.2 zadaje samo Ani Anić: %+v", popis)
	}
	if popis := vs.VodocuvariZaBiljesku(ctx, dionicar); len(popis) != 1 || popis[0].ID != k.ana.ID {
		t.Errorf("rukovoditelj dionice P.1.2 upisuje samo Ani Anić: %+v", popis)
	}
}

// Vodočuvar lista čita se po listu kad ga pozivatelj nema; neispravna
// oznaka, nepoznata osoba i servis bez korisnika ne daju nikoga.
func TestVodocuvarLista(t *testing.T) {
	k := novaKnjigaProba(t)
	vs := k.o.vs
	if v := vs.vodocuvarLista(k.listPere, nil); v == nil || v.ID != k.pero.ID {
		t.Errorf("vodočuvar lista: %+v", v)
	}
	if v := vs.vodocuvarLista(&models.VodocuvarskiList{UserID: "nije-oznaka"}, nil); v != nil {
		t.Errorf("neispravna oznaka: %+v", v)
	}
	if v := vs.vodocuvarLista(&models.VodocuvarskiList{UserID: uuid.NewString()}, nil); v != nil {
		t.Errorf("nepoznata osoba: %+v", v)
	}
	if v := (&VodocuvarService{}).vodocuvarLista(k.listPere, nil); v != nil {
		t.Errorf("bez korisnika: %+v", v)
	}
	if v := vs.vodocuvarLista(k.listPere, k.ana); v != k.ana {
		t.Error("zadani vodočuvar se ne poštuje")
	}
}
