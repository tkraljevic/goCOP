package service

import (
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
// (čuva P.1.1) s objektom na P.1.3 vezana je uz P.1.3, i uz P.1.1 jer je
// izvan njegova zaduženja (Odluke (8. 10. 2026.), pitanje 1). Podaci su
// izmišljeni.
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
		{"Pero s objektom na P.1.3", prijava(k.pero, "", "CRPNA STANICA PRVA", ""), true, true},
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
