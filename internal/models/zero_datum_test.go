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

// Vrijednost koja nije izmjerena na ovoj letvi ne smije se predstaviti kao
// mjerenje: Batina najviši vodostaj iz 1965. ima preračunat iz Bezdana.
func TestEkstremZnaJeLiIzmjerenNaOvojLetvi(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := Station{Extremes: []StationExtreme{
		{Kind: ExtremeMax, LevelCm: cm(775), OnDate: "2013-06-14", Quality: QualityMeasured},
		{Kind: ExtremeMax, LevelCm: cm(795), OnDate: "1965-06-24", Quality: QualityReconstructed, Source: "postaja Bezdan"},
		{Kind: ExtremeMin, LevelCm: cm(-127), OnDate: "1909-01-07"},
	}}
	maxi := st.ExtremesOf(ExtremeMax)
	if len(maxi) != 2 {
		t.Fatalf("najviših ekstrema %d, očekivano 2", len(maxi))
	}
	if !maxi[0].IsMeasured() {
		t.Error("+775 iz 2013. je izmjeren")
	}
	if maxi[1].IsMeasured() {
		t.Error("+795 iz 1965. nije izmjeren na Batini")
	}
	if maxi[1].Label() != "+795 cm" {
		t.Errorf("natpis ekstrema: %q", maxi[1].Label())
	}
	// zatečeni zapisi nemaju upisanu kvalitetu; oni su mjerenja
	if !st.ExtremesOf(ExtremeMin)[0].IsMeasured() {
		t.Error("zapis bez upisane kvalitete je mjerenje")
	}
	if QualityLabel(QualityReconstructed) != "rekonstruirano" {
		t.Error("natpis podrijetla")
	}
}

// Očitanje bez upisane kvalitete je mjerenje; rekonstruirano se ne smije
// predstaviti kao mjerenje svoje letve.
func TestOcitanjeZnaJeLiIzmjereno(t *testing.T) {
	if !(Reading{}).IsMeasured() {
		t.Error("zatečeno očitanje bez kvalitete je mjerenje")
	}
	if !(Reading{Quality: QualityMeasured}).IsMeasured() {
		t.Error("izmjereno je izmjereno")
	}
	if (Reading{Quality: QualityReconstructed, DerivedFrom: "postaja Bezdan"}).IsMeasured() {
		t.Error("rekonstruirano nije mjerenje")
	}
}
