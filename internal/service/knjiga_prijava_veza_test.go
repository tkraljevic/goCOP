package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// objekt upisuje objekt u registar na području i veže ga uz dionice
func (k *knjigaProba) objekt(t *testing.T, naziv string, podrucje int, dionice ...string) string {
	t.Helper()
	id := uuid.New().String()
	sad := time.Now().UTC()
	if _, err := k.o.baza.Exec(`INSERT INTO structures (id, code, name, kind, sector_id, area_id, created_at, updated_at) VALUES (?, ?, ?, ?, 'P', ?, ?, ?)`,
		id, "objekt-"+id, naziv, models.StructureKindPumpingStation, podrucje, sad, sad); err != nil {
		t.Fatal(err)
	}
	for _, d := range dionice {
		if _, err := k.o.baza.Exec(`INSERT INTO section_structures (id, section_code, structure_id, created_at) VALUES (?, ?, ?, ?)`, uuid.New().String(), d, id, sad); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// Prijava je vezana uz dionicu rukovoditelja dionice kad pokazuje na nju,
// upisanom dionicom ili objektom na njoj iz registra (po šifri ili po
// nazivu, na području prijave). Prijava koja ne pokazuje ni na jednu
// dionicu vezana je kao list njezina vodočuvara: Ana Anić čuva cijelo
// područje, pa je njezina prijava bez mjesta vezana uz svaku dionicu, kao
// i njezin list koji rukovoditelj dionice parafira. Prijava Pere Perića
// (čuva P.1.1) s objektom na P.1.3 vezana je samo uz P.1.3, iako je izvan
// njegova zaduženja (dosad i uz P.1.1; Odluke (8. 10. 2026.), pitanje 1:
// prijava vezana uz njegovu dionicu). Podaci su izmišljeni.
func TestPrijavaVezanaKaoListIPoObjektu(t *testing.T) {
	k := novaKnjigaProba(t)
	vs := k.o.vs
	vs.SetObjekti(repository.NewStructureRepository(k.o.baza, ledger.New(k.o.baza, "test")))
	prijave := &PrijavaService{vodocuvar: vs}
	crpna := k.objekt(t, "Crpna stanica Prva", k.p1, "P.1.3")
	k.objekt(t, "Ustava Prva", k.p1)
	k.objekt(t, "Crpna stanica Druga", k.p2, "P.2.1")
	prvi := k.dionicar(models.RoleSectionLeader, &k.p1, "P.1.1")
	treci := k.dionicar(models.RoleSectionLeader, &k.p1, "P.1.3")
	if !vs.SmijeParafirati(prvi, k.listAne) || !vs.SmijeParafirati(treci, k.listAne) {
		t.Fatal("list vodočuvarke cijelog područja nije vezan uz svaku dionicu")
	}
	prijava := func(u *models.User, dionica, objekt, objektID string) *models.PrijavaSTerena {
		return &models.PrijavaSTerena{UserID: u.ID.String(), Sektor: "P", AreaID: 1, Status: models.PrijavaObjavljena, DionicaCode: dionica, Objekt: objekt, ObjektID: objektID}
	}
	for _, s := range []struct {
		ime         string
		p           *models.PrijavaSTerena
		prvi, treci bool
	}{
		{"Ana bez dionice i objekta", prijava(k.ana, "", "", ""), true, true},
		{"Ana s objektom na P.1.3", prijava(k.ana, "", "Crpna stanica Prva", ""), false, true},
		{"Ana s objektom po šifri", prijava(k.ana, "", "", crpna), false, true},
		{"Ana s objektom bez dionice", prijava(k.ana, "", "Ustava Prva", ""), true, true},
		{"Ana s objektom drugog područja", prijava(k.ana, "", "Crpna stanica Druga", ""), true, true},
		{"Ana s dionicom P.1.1 i objektom na P.1.3", prijava(k.ana, "P.1.1", " crpna stanica prva ", ""), true, true},
		{"Pero bez dionice i objekta", prijava(k.pero, "", "", ""), true, false},
		{"Pero s objektom na P.1.3", prijava(k.pero, "", "CRPNA STANICA PRVA", ""), false, true},
	} {
		if got := prijave.SmijeVidjeti(prvi, s.p); got != s.prvi {
			t.Errorf("%s: rukovoditelj P.1.1 čita %v, očekivano %v", s.ime, got, s.prvi)
		}
		if got := prijave.SmijeVidjeti(treci, s.p); got != s.treci {
			t.Errorf("%s: rukovoditelj P.1.3 čita %v, očekivano %v", s.ime, got, s.treci)
		}
	}
	// kad se registar objekata ne da pročitati, ne zna se na koje dionice
	// prijava pokazuje, pa nije vezana; prijava koja navodi njegovu
	// dionicu i dalje jest
	if _, err := k.o.baza.Exec(`DROP TABLE section_structures`); err != nil {
		t.Fatal(err)
	}
	if prijave.SmijeVidjeti(prvi, prijava(k.pero, "", "", "")) {
		t.Error("uz grešku registra objekata prijava je vezana")
	}
	if !prijave.SmijeVidjeti(prvi, prijava(k.pero, "P.1.1", "", "")) {
		t.Error("uz grešku registra objekata prijava njegove dionice nije vezana")
	}
}

// Polje Dionica u prijavi je slobodan upis. Prijava pokazuje na dionice
// područja iz registra koje upis navodi, bez obzira na velika slova,
// razmake, zareze, veznike i točku na kraju; riječ koja nije šifra dionice
// tog područja (pogrešno upisana šifra, dionica drugog područja) ne
// pokazuje ni na jednu. Prijava koja pokazuje na dionicu vezana je samo uz
// nju, i kad je njezin vodočuvar ne čuva; ona koja ne pokazuje ni na jednu
// vezana je kao vodočuvarov list (Odluke (8. 10. 2026.), pitanje 1:
// rukovoditelj dionice čita samo prijave vezane uz svoju dionicu). Pero
// Perić čuva P.1.1, Ana Anić cijelo područje 1. Podaci su izmišljeni.
func TestPrijavaPokazujeNaDioniceIzRegistra(t *testing.T) {
	k := novaKnjigaProba(t)
	prijave := &PrijavaService{vodocuvar: k.o.vs}
	dionicari := map[string]*models.UserPermissions{}
	for _, d := range []string{"P.1.1", "P.1.2", "P.1.3"} {
		dionicari[d] = k.dionicar(models.RoleSectionLeader, &k.p1, d)
	}
	sve := map[string]bool{"P.1.1": true, "P.1.2": true, "P.1.3": true}
	for _, s := range []struct {
		ime    string
		v      *models.User
		upis   string
		vezana map[string]bool
	}{
		{"Pero, dionica koju ne čuva", k.pero, "P.1.2", map[string]bool{"P.1.2": true}},
		{"Pero, pogrešno upisana šifra", k.pero, "P 1.2", map[string]bool{"P.1.1": true}},
		{"Ana, bez upisa", k.ana, "", sve},
		{"Ana, šifra s razmakom", k.ana, "P 1.1", sve},
		{"Ana, dionica drugog područja", k.ana, "P.2.1", sve},
		{"Ana, dvije dionice s veznikom", k.ana, "P.1.1 i P.1.2", map[string]bool{"P.1.1": true, "P.1.2": true}},
		{"Ana, malim slovima", k.ana, "p.1.1", map[string]bool{"P.1.1": true}},
		{"Ana, s točkom na kraju rečenice", k.ana, "dionica P.1.3.", map[string]bool{"P.1.3": true}},
	} {
		p := &models.PrijavaSTerena{UserID: s.v.ID.String(), Sektor: "P", AreaID: 1, DionicaCode: s.upis, Status: models.PrijavaObjavljena}
		for d, perms := range dionicari {
			if got := prijave.SmijeVidjeti(perms, p); got != s.vezana[d] {
				t.Errorf("%s (%q): rukovoditelj %s čita %v, očekivano %v", s.ime, s.upis, d, got, s.vezana[d])
			}
		}
	}
	// prazan upis ne pokazuje ni na što; bez dionica područja u registru
	// svaka riječ upisa uzima se kao šifra
	if got := k.o.vs.dioniceUpisa(" , ", 1); got != nil {
		t.Errorf("prazan upis: %q", got)
	}
	if got := strings.Join((&VodocuvarService{}).dioniceUpisa("P.1.1 i (P.1.2).", 1), "|"); got != "P.1.1|i|P.1.2" {
		t.Errorf("bez registra: %q", got)
	}
	if got := strings.Join(k.o.vs.dioniceUpisa("Q.3.1", 3), "|"); got != "Q.3.1" {
		t.Errorf("područje bez dionica u registru: %q", got)
	}
}
