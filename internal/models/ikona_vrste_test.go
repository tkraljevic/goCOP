package models

import "testing"

// Naslov stranice objekta nosi ikonu vrste: iz postavki karte kad je znak iz
// zbirke, a ikonu registra kad je na karti krug, posebna letva ili vlastita
// ikona (nasip je na karti zadano krug, pa u naslovu dobije znak nasipa).
func TestIkonaVrste(t *testing.T) {
	stare := PostavkeKartePrograma()
	t.Cleanup(func() { SetPostavkeKarte(stare) })
	SetPostavkeKarte(PostavkeKarte{Objekti: map[string]StilObjekta{
		StructureKindDam:    {Ikona: "v-moja-brana"},
		StructureKindSluice: {Ikona: IkonaLetva},
	}})
	for kind, want := range map[string]string{
		StructureKindEmbankment:     "nasip",
		StructureKindPumpingStation: "crpna-stanica",
		StructureKindCulvert:        "propust",
		StructureKindDam:            "brana",
		StructureKindSluice:         "ustava",
		"NEPOZNATA":                 "construction",
	} {
		if got := IkonaVrste(kind); got != want {
			t.Errorf("%s: %q, očekivano %q", kind, got, want)
		}
	}
}
