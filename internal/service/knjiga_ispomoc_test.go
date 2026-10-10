package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// ispomoc daje Peri Periću, vodočuvaru dionice P.1.1 (glavno zaduženje na
// području 1, od prije mjesec dana), privremenu ispomoć na dionici P.2.1
// područja 2, od sada; vraća zaduženje ispomoći i dan prije dva dana, kad
// ispomoći još nije bilo. Kad taj dan pada u prošlu godinu, test se
// preskače.
func (k *knjigaProba) ispomoc(t *testing.T) (*models.Duty, time.Time) {
	t.Helper()
	sad := time.Now().In(models.Zagreb)
	prije := time.Date(sad.Year(), sad.Month(), sad.Day(), 0, 0, 0, 0, models.Zagreb).AddDate(0, 0, -2)
	if prije.Year() != sad.Year() {
		t.Skip("dan prije dva dana pada u prošlu godinu")
	}
	if _, err := k.o.baza.Exec(`UPDATE duties SET created_at = ? WHERE user_id = ?`, sad.AddDate(0, -1, 0).UTC(), k.pero.ID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := k.o.baza.Exec(`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES
		('P.2.2', 2, 'P', 'kanal Drugi', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	d := &models.Duty{UserID: k.pero.ID, Title: "Privremena ispomoć", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection,
		SectorID: &k.sektor, AreaID: &k.p2, SectionCodes: "P.2.1", IsTemporary: true}
	if err := k.users.AddDuty(d); err != nil {
		t.Fatal(err)
	}
	return d, prije
}

// imenaOsoba su imena osoba s popisa, abecedno
func imenaOsoba(osobe []models.User) string {
	var out []string
	for _, u := range osobe {
		out = append(out, u.FullName)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// Vodočuvar na privremenoj ispomoći na drugom branjenom području ulazi u
// doseg tog područja (Odluke (8. 10. 2026.), pitanje 1, „posebno kad je
// obrana od poplava”): rukovoditelj tog područja čita, parafira i upisuje
// bilješku na njegove listove iz vremena ispomoći i zadaje mu zadatke, a
// rukovoditelj dionice ispomoći čini isto uz vezu s dionicom, zadatke dok
// na njoj traje obrana. List iz vremena prije ispomoći ostaje izvan
// njihova dosega, i u knjizi; ovjera ostaje području lista. Podaci su
// izmišljeni.
func TestIspomocNaDrugomPodrucju(t *testing.T) {
	k := novaKnjigaProba(t)
	ctx := context.Background()
	vs := k.o.vs
	k.obrana(map[string]models.DefensePhase{"P.2.1": models.PhaseRegular})
	ispomoc, prije := k.ispomoc(t)
	sad := time.Now()
	danas := time.Date(sad.In(models.Zagreb).Year(), sad.In(models.Zagreb).Month(), sad.In(models.Zagreb).Day(), 0, 0, 0, 0, models.Zagreb)
	list := func(dan time.Time) *models.VodocuvarskiList {
		return &models.VodocuvarskiList{UserID: k.pero.ID.String(), Ime: k.pero.FullName, Sektor: "P", AreaID: 1, Datum: dan, PredanoAt: &sad}
	}
	rukovoditelj2 := k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &k.p2})
	dionicar21 := k.dionicar(models.RoleSectionLeader, &k.p2, "P.2.1")
	dionicar22 := k.dionicar(models.RoleSectionLeader, &k.p2, "P.2.2")
	sve := ocekivanjeKnjige{true, true, true, true}
	k.provjeri(t, "rukovoditelj područja ispomoći", rukovoditelj2, list(danas), sve)
	k.provjeri(t, "rukovoditelj dionice ispomoći u obrani", dionicar21, list(danas), sve)
	k.provjeri(t, "rukovoditelj druge dionice područja ispomoći", dionicar22, list(danas), ocekivanjeKnjige{vidi: true})
	k.provjeri(t, "rukovoditelj područja ispomoći, list prije ispomoći", rukovoditelj2, list(prije), ocekivanjeKnjige{zadatak: true})
	k.provjeri(t, "rukovoditelj dionice ispomoći, list prije ispomoći", dionicar21, list(prije), ocekivanjeKnjige{zadatak: true})
	if vs.SmijeOvjeriti(rukovoditelj2, list(danas)) {
		t.Error("rukovoditelj područja ispomoći ovjerava list područja 1")
	}
	for _, x := range []struct {
		ime   string
		perms *models.UserPermissions
	}{{"rukovoditelj područja ispomoći", rukovoditelj2}, {"rukovoditelj dionice ispomoći", dionicar21}} {
		if got := imenaOsoba(vs.Vodocuvari(ctx, x.perms)); got != "Pero Perić" {
			t.Errorf("%s zadaje zadatke: %q", x.ime, got)
		}
		if _, err := vs.ZadajZadatak(ctx, x.perms, &x.perms.User, k.pero.ID.String(), "pregledati propust na P.2.1", time.Time{}); err != nil {
			t.Errorf("%s zadaje zadatak: %v", x.ime, err)
		}
		if got := imenaOsoba(vs.VodocuvariZaBiljesku(ctx, x.perms)); got != "Pero Perić" {
			t.Errorf("%s upisuje bilješke: %q", x.ime, got)
		}
	}
	if got := imenaOsoba(vs.Vodocuvari(ctx, dionicar22)); got != "" {
		t.Errorf("rukovoditelj druge dionice zadaje zadatke: %q", got)
	}
	if _, err := vs.Upisi(ctx, dionicar21, &dionicar21.User, k.pero.ID.String(), danas, "javljeno s P.2.1"); err != nil {
		t.Errorf("bilješka rukovoditelja dionice ispomoći: %v", err)
	}
	if _, err := vs.Upisi(ctx, rukovoditelj2, &rukovoditelj2.User, k.pero.ID.String(), prije, "prije ispomoći"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("bilješka za dan prije ispomoći: %v", err)
	}

	// knjiga i popis listova: samo listovi iz vremena ispomoći
	for _, dan := range []time.Time{prije, danas} {
		l := list(dan)
		l.Broj = 1
		if dan.Equal(danas) {
			if l = k.o.vdListIzBaze(t, k.pero, danas); l == nil {
				t.Fatal("bilješka nije otvorila današnji list")
			}
		}
		if err := k.o.repo.Save(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	knjiga, err := vs.Knjiga(ctx, rukovoditelj2, k.pero.ID.String(), danas.Year())
	if err != nil || len(knjiga) != 1 || !knjiga[0].Datum.Equal(danas) {
		t.Errorf("knjiga za rukovoditelja područja ispomoći: %v, %d listova", err, len(knjiga))
	}
	if svi, err := vs.Knjiga(ctx, k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &k.p1}), k.pero.ID.String(), danas.Year()); err != nil || len(svi) != 2 {
		t.Errorf("knjiga za rukovoditelja područja 1: %v, %d listova", err, len(svi))
	}
	tudji, err := vs.Tudji(ctx, rukovoditelj2, repository.FiltarListova{Godina: danas.Year()})
	if err != nil || len(tudji) != 1 || !tudji[0].Datum.Equal(danas) {
		t.Errorf("popis listova za rukovoditelja područja ispomoći: %v, %d listova", err, len(tudji))
	}

	// ispomoć je opozvana: današnji list ostaje u dosegu, zadaci ne
	if err := k.users.RevokeDuty(ispomoc.ID); err != nil {
		t.Fatal(err)
	}
	k.provjeri(t, "rukovoditelj dionice nakon opoziva ispomoći", dionicar21, list(danas), ocekivanjeKnjige{vidi: true, parafira: true, biljeska: true})
	if got := imenaOsoba(vs.Vodocuvari(ctx, rukovoditelj2)); got != "" {
		t.Errorf("nakon opoziva ispomoći rukovoditelj područja 2 zadaje: %q", got)
	}
}

// Prijava vodočuvara s ispomoći: rukovoditelj područja ispomoći čita
// prijave iz vremena ispomoći, a rukovoditelj dionice ispomoći one vezane
// uz njegovu dionicu: koja navodi njegovu dionicu, ili ne navodi nijednu
// (vodočuvar je toga dana čuvao njegovu dionicu). Prijava koja navodi
// dionicu područja 1 nije vezana uz njega; prijava od prije ispomoći ni uz
// jednoga. Podaci su izmišljeni.
func TestPrijaveIspomociNaDrugomPodrucju(t *testing.T) {
	k := novaKnjigaProba(t)
	ctx := context.Background()
	_, prije := k.ispomoc(t)
	prijave := &PrijavaService{repo: repository.NewPrijavaRepository(k.o.baza, ledger.New(k.o.baza, "test")), vodocuvar: k.o.vs}
	sad := time.Now().In(models.Zagreb)
	for _, x := range []struct {
		naslov, dionica string
		dan             time.Time
	}{
		{"Na P.2.1", "P.2.1", sad},
		{"Na P.1.1", "P.1.1", sad},
		{"Bez dionice", "", sad},
		{"Prije ispomoći", "", prije},
	} {
		p := &models.PrijavaSTerena{UserID: k.pero.ID.String(), Ime: k.pero.FullName, Sektor: "P", AreaID: 1, Vrsta: models.PrijavaObavijest,
			Naslov: x.naslov, Opis: "opis", DionicaCode: x.dionica, Datum: x.dan, Status: models.PrijavaObjavljena}
		if err := prijave.repo.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	naslovi := func(perms *models.UserPermissions) string {
		t.Helper()
		popis, err := prijave.List(ctx, perms, repository.FiltarPrijava{})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, p := range popis {
			out = append(out, p.Naslov)
			if !prijave.SmijeVidjeti(perms, &p) {
				t.Errorf("prijava %q je na popisu, a pojedinačno se ne čita", p.Naslov)
			}
		}
		sort.Strings(out)
		return strings.Join(out, ", ")
	}
	if got := naslovi(k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &k.p2})); got != "Bez dionice, Na P.1.1, Na P.2.1" {
		t.Errorf("rukovoditelj područja ispomoći: %s", got)
	}
	if got := naslovi(k.dionicar(models.RoleSectionLeader, &k.p2, "P.2.1")); got != "Bez dionice, Na P.2.1" {
		t.Errorf("rukovoditelj dionice ispomoći: %s", got)
	}
	if got := naslovi(k.dionicar(models.RoleSectionLeader, &k.p1, "P.1.1")); got != "Bez dionice, Na P.1.1, Prije ispomoći" {
		t.Errorf("rukovoditelj dionice P.1.1: %s", got)
	}
}

// Mjesto zaduženja bez sektora ili područja i dužnost bez područja ili
// sektora: doseg i veza s dionicom gledaju ono što postoji, a ostatak uzimaju
// s lista.
func TestDosegBezMjesta(t *testing.T) {
	p, jedan := "P", 1
	vodocuvarBezMjesta := models.Duty{Role: models.RoleWaterGuard}
	sektora := models.Duty{Role: models.RoleOperator, ScopeType: models.ScopeSector, SectorID: &p}
	if cuvaUDosegu(sektora, []models.Duty{vodocuvarBezMjesta}) {
		t.Error("vodočuvar bez mjesta je u dosegu sektora")
	}
	if !cuvaUDosegu(sektora, []models.Duty{{Role: models.RoleWaterGuard, SectorID: &p}}) {
		t.Error("vodočuvar sektora nije u dosegu sektora")
	}
	if cuvaUDosegu(models.Duty{Role: models.RoleAreaLeader, AreaID: &jedan}, []models.Duty{{Role: models.RoleMachinist, AreaID: &jedan}}) {
		t.Error("strojar je u dosegu kao vodočuvar")
	}
	l := &models.VodocuvarskiList{Sektor: "Q", AreaID: 7}
	if podrucjeDuznosti(sektora, l) != 7 || sektorDuznosti(models.Duty{AreaID: &jedan}, l) != "Q" {
		t.Error("dužnost bez područja ili sektora ne uzima ga s lista")
	}
	if podrucjeDuznosti(models.Duty{AreaID: &jedan}, l) != 1 || sektorDuznosti(sektora, l) != "P" {
		t.Error("dužnost s područjem i sektorom ne gleda svoje")
	}
}

// Knjiga i popis listova: bez listova knjiga je prazna i nije zabranjena;
// kad se listovi ne daju pročitati, greška.
func TestKnjigaBezListova(t *testing.T) {
	k := novaKnjigaProba(t)
	ctx := context.Background()
	vs := k.o.vs
	rukovoditelj := k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &k.p2})
	if l, err := vs.Knjiga(ctx, rukovoditelj, k.pero.ID.String(), 2026); err != nil || len(l) != 0 {
		t.Errorf("knjiga bez listova: %v %d", err, len(l))
	}
	if _, err := k.o.baza.Exec(`DROP TABLE vodocuvarski_listovi`); err != nil {
		t.Fatal(err)
	}
	if _, err := vs.Knjiga(ctx, rukovoditelj, k.pero.ID.String(), 2026); err == nil {
		t.Error("knjiga bez tablice listova")
	}
	if _, err := vs.Tudji(ctx, rukovoditelj, repository.FiltarListova{}); err == nil {
		t.Error("popis bez tablice listova")
	}
}
