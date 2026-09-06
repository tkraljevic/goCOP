package models

import "testing"

func kota(v float64) *float64 { return &v }

// Kota nule vrijedi od datuma promjene do sljedeće. Očitanje iz 2005. mjereno
// je starom kotom i kad letva danas nosi novu; bez toga se stara evidencija
// ne može ispravno protumačiti.
func TestZeroDatumAtVracaKotuKojaJeTadaVrijedila(t *testing.T) {
	st := Station{ZeroDatumHistory: []ZeroDatumChange{
		{ValidFrom: "", Datum: kota(80.45), System: "TRST"},
		{ValidFrom: "2017-06-16", Datum: kota(80.30), System: "TRST", Note: "radar OTT RLS"},
		{ValidFrom: "2024-09-10", Datum: kota(80.189), System: "HVRS71"},
	}}
	for dan, want := range map[string]float64{
		"1985-01-01": 80.45,
		"2017-06-15": 80.45,
		"2017-06-16": 80.30,
		"2024-09-09": 80.30,
		"2024-12-31": 80.189,
	} {
		got := st.ZeroDatumAt(dan)
		if got == nil || got.Datum == nil || *got.Datum != want {
			t.Errorf("%s: kota %v, očekivano %v", dan, got, want)
		}
	}
}

// Letva bez upisane povijesti ne smije izmisliti kotu.
func TestZeroDatumAtBezPovijestiVracaNista(t *testing.T) {
	if (Station{}).ZeroDatumAt("2013-06-14") != nil {
		t.Error("bez povijesti nema kote")
	}
	st := Station{ZeroDatumHistory: []ZeroDatumChange{{ValidFrom: "2017-06-16", Datum: kota(80.30)}}}
	if st.ZeroDatumAt("2013-06-14") != nil {
		t.Error("prije prve upisane promjene kota se ne zna")
	}
}
