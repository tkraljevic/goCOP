package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gocop/internal/models"
)

// Veza lista i prijave s dionicom gleda zaduženja vodočuvara za dan lista
// ili prijave, a ne samo ono što vrijedi sada (Odluke (8. 10. 2026.),
// pitanje 1: rukovoditelj dionice „osobito u obrani”). Privremeni
// vodočuvar Pero Perić čuva P.1.1 dok traje obrana; kad zaduženje istekne,
// njegov jučerašnji list i prijava ostaju vezani uz P.1.1, a list za dan
// nakon isteka nije. Isto vrijedi kad zaduženje bude opozvano jer je
// vodočuvar prešao na drugu dionicu. Podaci su izmišljeni.
func TestVezaSDionicomPoZaduzenjuNaDanLista(t *testing.T) {
	k := novaKnjigaProba(t)
	ctx := context.Background()
	vs := k.o.vs
	prijave := &PrijavaService{vodocuvar: vs}
	sad := time.Now()
	danas := sad.In(models.Zagreb)
	jucer := time.Date(danas.Year(), danas.Month(), danas.Day()-1, 0, 0, 0, 0, models.Zagreb)
	sutra := time.Date(danas.Year(), danas.Month(), danas.Day()+1, 0, 0, 0, 0, models.Zagreb)
	list := func(dan time.Time) *models.VodocuvarskiList {
		return &models.VodocuvarskiList{UserID: k.pero.ID.String(), Ime: k.pero.FullName, Sektor: "P", AreaID: 1, Datum: dan, PredanoAt: &sad}
	}
	prijava := func(dan time.Time) *models.PrijavaSTerena {
		return &models.PrijavaSTerena{UserID: k.pero.ID.String(), Sektor: "P", AreaID: 1, Datum: dan, Status: models.PrijavaObjavljena}
	}
	dionicar := k.dionicar(models.RoleSectionLeader, &k.p1, "P.1.1")
	rukovoditelj := k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &k.p1})
	type ocekivano struct{ jucer, sutra bool }
	provjeri := func(stanje string, o ocekivano) {
		t.Helper()
		for _, x := range []struct {
			dan  time.Time
			veza bool
		}{{jucer, o.jucer}, {sutra, o.sutra}} {
			dan := x.dan.Format("2.1.")
			if !vs.SmijeVidjeti(dionicar, list(x.dan)) {
				t.Errorf("%s: rukovoditelj dionice ne čita list za %s", stanje, dan)
			}
			if got := vs.SmijeParafirati(dionicar, list(x.dan)); got != x.veza {
				t.Errorf("%s: parafa lista za %s = %v, očekivano %v", stanje, dan, got, x.veza)
			}
			if got := vs.SmijeUpisati(dionicar, list(x.dan)); got != x.veza {
				t.Errorf("%s: bilješka na list za %s = %v, očekivano %v", stanje, dan, got, x.veza)
			}
			if got := prijave.SmijeVidjeti(dionicar, prijava(x.dan)); got != x.veza {
				t.Errorf("%s: prijava bez dionice za %s = %v, očekivano %v", stanje, dan, got, x.veza)
			}
			if !vs.SmijeParafirati(rukovoditelj, list(x.dan)) || !prijave.SmijeVidjeti(rukovoditelj, prijava(x.dan)) {
				t.Errorf("%s: rukovoditelj područja za %s", stanje, dan)
			}
		}
	}
	provjeri("valjano zaduženje", ocekivano{true, true})

	// privremeno zaduženje isteklo je prije sat vremena (kraj obrane)
	if _, err := k.o.baza.Exec(`UPDATE duties SET is_temporary = 1, expires_at = ? WHERE user_id = ?`, sad.Add(-time.Hour).UTC(), k.pero.ID.String()); err != nil {
		t.Fatal(err)
	}
	provjeri("isteklo zaduženje", ocekivano{true, false})
	// zadatak gleda samo ono što vrijedi sada
	k.obrana(map[string]models.DefensePhase{"P.1.1": models.PhaseRegular})
	if vs.SmijeZadati(ctx, dionicar, list(jucer)) {
		t.Error("rukovoditelj dionice zadaje zadatak vodočuvaru kojemu je zaduženje isteklo")
	}

	// Ana Anić prelazi s cijelog područja na dionicu P.1.2: staro zaduženje
	// se opoziva. Njezin jučerašnji list veže se i uz P.1.1, sutrašnji samo
	// uz P.1.2; bilješka kroz servis gleda isti dan.
	stara, err := k.users.GetDutiesForUser(k.ana.ID)
	if err != nil || len(stara) != 1 {
		t.Fatalf("Anina zaduženja: %v %+v", err, stara)
	}
	if err := k.users.RevokeDuty(stara[0].ID); err != nil {
		t.Fatal(err)
	}
	nova := models.Duty{UserID: k.ana.ID, Title: "Vodočuvar P.1.2", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, SectorID: &k.sektor, AreaID: &k.p1, SectionCodes: "P.1.2", IsActive: true, IsPrimary: true}
	if err := k.users.AddDuty(&nova); err != nil {
		t.Fatal(err)
	}
	listAne := func(dan time.Time) *models.VodocuvarskiList {
		return &models.VodocuvarskiList{UserID: k.ana.ID.String(), Ime: k.ana.FullName, Sektor: "P", AreaID: 1, Datum: dan, PredanoAt: &sad}
	}
	if !vs.SmijeParafirati(dionicar, listAne(jucer)) || vs.SmijeParafirati(dionicar, listAne(sutra)) {
		t.Error("veza Anina lista s P.1.1 ne prati opoziv zaduženja")
	}
	drugi := k.dionicar(models.RoleSectionLeader, &k.p1, "P.1.2")
	if !vs.SmijeParafirati(drugi, listAne(sutra)) {
		t.Error("Anin list nakon prelaska nije vezan uz P.1.2")
	}
	if _, err := vs.Upisi(ctx, dionicar, &dionicar.User, k.ana.ID.String(), jucer, "Javljeno s dionice P.1.1"); err != nil {
		t.Errorf("bilješka za dan prije prelaska: %v", err)
	}
	if _, err := vs.Upisi(ctx, dionicar, &dionicar.User, k.ana.ID.String(), sutra, "Javljeno s dionice P.1.1"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("bilješka za dan nakon prelaska: %v", err)
	}
}

// Prošla zaduženja vodočuvara čitaju se samo kad je dan zadan, i to
// jednom; bez vodočuvara nema zaduženja.
func TestZaduzenjaCuvara(t *testing.T) {
	k := novaKnjigaProba(t)
	vs := k.o.vs
	if got := vs.zaduzenjaCuvara(nil, time.Now()); got != nil {
		t.Errorf("bez čuvara: %+v", got)
	}
	if got := vs.zaduzenjaCuvara(&cuvar{}, time.Now()); got != nil {
		t.Errorf("bez vodočuvara: %+v", got)
	}
	pero, _ := vs.users.GetUserByID(k.pero.ID)
	c := &cuvar{v: pero}
	if got := vs.zaduzenjaCuvara(c, time.Time{}); len(got) != 1 || c.procitana {
		t.Errorf("bez dana: %d zaduženja, prošla pročitana %v", len(got), c.procitana)
	}
	if got := vs.zaduzenjaCuvara(c, time.Now()); len(got) != 1 || !c.procitana {
		t.Errorf("s danom: %d zaduženja, prošla pročitana %v", len(got), c.procitana)
	}
	c.prosla = []models.PrijasnjeZaduzenje{{Duty: models.Duty{Role: models.RoleWaterGuard}, OpozvanoAt: &time.Time{}}}
	if got := vs.zaduzenjaCuvara(c, time.Now()); len(got) != 1 {
		t.Errorf("prošla se čitaju ponovno: %d", len(got))
	}
}
