package service

import (
	"testing"

	"gocop/internal/models"
)

func TestImenaCitaObaOblikaZapisa(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"; Draž: Draž, Gajić, Topolje, Batina", []string{"Draž", "Draž", "Gajić", "Topolje", "Batina"}},
		{": Račinovci, Đurići, Drenovci Gunja", []string{"Račinovci", "Đurići", "Drenovci Gunja"}},
		{": Cerna", []string{"Cerna"}},
		{"", nil},
	}
	for _, c := range cases {
		got := imena(c.in)
		if len(got) != len(c.want) {
			t.Errorf("imena(%q) = %v, očekivano %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("imena(%q)[%d] = %q, očekivano %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestKljucIzjednacavaZapise(t *testing.T) {
	if kljuc(" Osječko-baranjska  županija ") != kljuc("osječko-baranjska županija") {
		t.Error("ključ ne izjednačava velika slova i razmake")
	}
	if kljuc("Draž") == kljuc("Draz") {
		t.Error("ključ ne smije brisati dijakritiku — Draž i Draz nisu isto mjesto")
	}
}

// Zapis "Draž: Draž, Gajić" spominje Draž i kao općinu i kao naselje; u popis
// smije ući jednom.
func TestKljucTeritorijaGledaVrijednostiNePokazivace(t *testing.T) {
	a, b := 12, 12
	prvi := models.PartTerritory{CountyID: 14, MunicipalityID: 3, SettlementID: &a}
	drugi := models.PartTerritory{CountyID: 14, MunicipalityID: 3, SettlementID: &b}
	if kljucTeritorija(prvi) != kljucTeritorija(drugi) {
		t.Error("isto naselje u dva zapisa daje različit ključ")
	}
	cijela := models.PartTerritory{CountyID: 14, MunicipalityID: 3}
	if kljucTeritorija(cijela) == kljucTeritorija(prvi) {
		t.Error("cijela općina i pojedino naselje ne smiju imati isti ključ")
	}
}
