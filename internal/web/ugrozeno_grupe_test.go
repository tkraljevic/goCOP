package web

import (
	"testing"

	"gocop/internal/models"
)

func ugrozenoNaselje(zup int, zupN string, opc int, opcN, opcT string, id int, ime string) models.SectionTerritory {
	return models.SectionTerritory{
		CountyID: zup, CountyName: zupN,
		MunicipalityID: opc, MunicipalityName: opcN, MunicipalityType: opcT,
		SettlementID: &id, SettlementName: ime,
	}
}

// Privitak piše ugroženo područje kao županija, pa općina, pa naselja. Ravan
// popis gubi ono što je ondje nosivo: Kneževi Vinogradi su općina, a Zmajevac
// naselje u njoj — kao dvije značke izgledaju jednakovrijedno.
func TestUgrozenoSeSlazeKaoUPrivitku(t *testing.T) {
	const ob = "Osječko-baranjska"
	popis := []models.SectionTerritory{
		ugrozenoNaselje(14, ob, 1, "Kneževi Vinogradi", "OPCINA", 1, "Zmajevac"),
		ugrozenoNaselje(14, ob, 1, "Kneževi Vinogradi", "OPCINA", 2, "Suza"),
		ugrozenoNaselje(14, ob, 3, "Bilje", "OPCINA", 3, "Tikveš"),
		ugrozenoNaselje(14, ob, 2, "Čeminac", "OPCINA", 4, "Grabovac"),
		ugrozenoNaselje(14, ob, 1, "Kneževi Vinogradi", "OPCINA", 5, "Mirkovac"),
	}
	z := grupirajUgrozeno(popis)
	if len(z) != 1 {
		t.Fatalf("županija: %d", len(z))
	}
	if z[0].Naziv != ob {
		t.Errorf("županija %q", z[0].Naziv)
	}
	// Općine ostaju kako su upisane: u Privitku idu uz nasip, ne abecedno.
	var imena []string
	for _, o := range z[0].Opcine {
		imena = append(imena, o.Naziv)
	}
	if len(imena) != 3 || imena[0] != "Kneževi Vinogradi" || imena[1] != "Bilje" || imena[2] != "Čeminac" {
		t.Errorf("općine %q — redoslijed iz Privitka se ne smije presložiti", imena)
	}
	// Naselja unutar općine ostaju kako su upisana: u Privitku nisu abecedno
	// nego kako idu uz nasip.
	kv := z[0].Opcine[0]
	if len(kv.Naselja) != 3 || kv.Naselja[0].Naziv != "Zmajevac" ||
		kv.Naselja[1].Naziv != "Suza" || kv.Naselja[2].Naziv != "Mirkovac" {
		t.Errorf("naselja Kneževih Vinograda: %+v", kv.Naselja)
	}
}

// Dvije županije se razdvajaju, kao u drugom primjeru iz Privitka.
func TestDvijeZupanijeStojeOdvojeno(t *testing.T) {
	popis := []models.SectionTerritory{
		ugrozenoNaselje(14, "Osječko-baranjska", 1, "Viljevo", "OPCINA", 1, "Kapelna"),
		ugrozenoNaselje(10, "Virovitičko-podravska", 2, "Crnac", "OPCINA", 2, "Breštanovci"),
		ugrozenoNaselje(10, "Virovitičko-podravska", 2, "Crnac", "OPCINA", 3, "Krivaja Pustara"),
	}
	z := grupirajUgrozeno(popis)
	if len(z) != 2 {
		t.Fatalf("županija: %d", len(z))
	}
	if z[0].Naziv != "Osječko-baranjska" || z[1].Naziv != "Virovitičko-podravska" {
		t.Errorf("poredak županija: %q, %q", z[0].Naziv, z[1].Naziv)
	}
	if len(z[1].Opcine[0].Naselja) != 2 {
		t.Errorf("Crnac ima %d naselja", len(z[1].Opcine[0].Naselja))
	}
}

// Bez izdvojenog naselja brani se cijela općina; to se mora vidjeti, a ne
// pretvoriti u naselje istog imena.
func TestCijelaOpcinaSeOznacava(t *testing.T) {
	z := grupirajUgrozeno([]models.SectionTerritory{{
		CountyID: 14, CountyName: "Osječko-baranjska",
		MunicipalityID: 1, MunicipalityName: "Draž", MunicipalityType: "OPCINA",
	}})
	if len(z) != 1 || len(z[0].Opcine) != 1 || len(z[0].Opcine[0].Naselja) != 1 {
		t.Fatalf("dobiveno %+v", z)
	}
	n := z[0].Opcine[0].Naselja[0]
	if !n.CijelaOpcina {
		t.Error("cijela općina nije označena kao takva")
	}
	if n.Naziv != "Draž" {
		t.Errorf("naziv %q", n.Naziv)
	}
}

// Prazan popis ne daje prazne skupine.
func TestPrazanPopisNeDajeSkupine(t *testing.T) {
	if z := grupirajUgrozeno(nil); z != nil {
		t.Errorf("prazan popis dao %+v", z)
	}
}
