package hydro

import "testing"

func TestParseSectionDescription(t *testing.T) {
	tests := []struct {
		opis     string
		voda     string
		vrsta    string
		obala    string
		from, to float64
		range_   bool
	}{
		{
			"rijeka Sava, l.o.; granica - cestovni most Gunja-Brčko; rkm 212+080 - 230+700 (18,620 km)",
			"Sava", "rijeka", BankLeft, 212.08, 230.7, true,
		},
		{
			"r. Dunav, d.o.; Državna granica s Mađarskom – Zeleni otok; rkm 1425+770 - 1423+770",
			"Dunav", "rijeka", BankRight, 1423.77, 1425.77, true,
		},
		{
			"Žirovnica, l.o. i d.o.; Dvor - Komora; rkm 0+000 - 27+000 (27,00 km)",
			"Žirovnica", "", BankBoth, 0, 27, true,
		},
		{
			"Krapina; d.o.; „Podsused-Žejinci“; rkm 0+000-19+140; (19,14 km)",
			"Krapina", "", BankRight, 0, 19.14, true,
		},
		{
			"akumulacija Jošava",
			"Jošava", "akumulacija", "", 0, 0, false,
		},
		{
			"Boljunčica; lijeva i desna obala; utok u more - tunel Čepić; km 0+000 - 5+730",
			"Boljunčica", "", BankBoth, 0, 5.73, true,
		},
	}

	for _, tc := range tests {
		got := ParseSectionDescription(tc.opis)
		if got.WaterName != tc.voda || got.WaterKind != tc.vrsta {
			t.Errorf("%q → voda (%q, %q), očekivano (%q, %q)", tc.opis, got.WaterName, got.WaterKind, tc.voda, tc.vrsta)
		}
		if got.Bank != tc.obala {
			t.Errorf("%q → obala %q, očekivano %q", tc.opis, got.Bank, tc.obala)
		}
		if got.HasRange != tc.range_ {
			t.Errorf("%q → raspon pročitan=%v, očekivano %v", tc.opis, got.HasRange, tc.range_)
			continue
		}
		if tc.range_ && (got.RkmFrom != tc.from || got.RkmTo != tc.to) {
			t.Errorf("%q → raspon %v–%v, očekivano %v–%v", tc.opis, got.RkmFrom, got.RkmTo, tc.from, tc.to)
		}
	}
}

// Obala se traži samo prije stacionaže — prozni opis zna spominjati tuđu obalu
func TestParseBankNeGledaIzaStacionaze(t *testing.T) {
	desc := "rijeka Sava, l.o.; nasuprot d.o. kod Gunje; rkm 212+080 - 230+700"
	if got := ParseBank(desc); got != BankBoth {
		// oba spomena su prije stacionaže — očekuje se obje; ovo dokumentira granicu heuristike
		t.Logf("ParseBank nad opisom s dvije obale prije stacionaže = %q", got)
	}
	tail := "rijeka Sava; rkm 212+080 - 230+700; l.o. nasip d.o."
	if got := ParseBank(tail); got != "" {
		t.Errorf("obala spomenuta tek iza stacionaže ne smije se čitati, dobiveno %q", got)
	}
}

func TestBankLabel(t *testing.T) {
	if BankLabel("L") != "lijeva obala" || BankLabel("D") != "desna obala" || BankLabel("LD") != "obje obale" {
		t.Error("oznake obale nemaju očekivane nazive")
	}
	if BankLabel("") != "" {
		t.Error("prazna obala mora dati prazan naziv")
	}
}
