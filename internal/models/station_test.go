package models

import "testing"

func cm(v int) Threshold { return Threshold{Cm: &v} }

// zupanja je stvarna postaja iz dokumentacije dionice D.1.1
func zupanja() Station {
	return Station{
		Name:      "Županja",
		Prep:      cm(600),
		Regular:   cm(880),
		Emergency: cm(980),
		State:     cm(1080),
	}
}

func TestCalculateDefensePhaseNaGranicama(t *testing.T) {
	st := zupanja()

	tests := []struct {
		levelCm int
		want    DefensePhase
	}{
		{0, PhaseNormal},
		{599, PhaseNormal},
		{600, PhasePrep}, // prag je uključen
		{879, PhasePrep},
		{880, PhaseRegular},
		{979, PhaseRegular},
		{980, PhaseEmergency},
		{1079, PhaseEmergency},
		{1080, PhaseState},
		{1191, PhaseState}, // najviši zabilježeni vodostaj, 17.5.2014.
	}

	for _, tc := range tests {
		if got := st.CalculateDefensePhase(tc.levelCm); got != tc.want {
			t.Errorf("vodostaj %d cm → faza %q, očekivano %q", tc.levelCm, got, tc.want)
		}
	}
}

// Postaja bez ijednog praga u centimetrima ne smije prijaviti redovno stanje —
// pragovi zapisani kao kota ili uputa iz pravilnika nisu strojno usporedivi
func TestPostajaBezPragovaNePrijavljujeRedovnoStanje(t *testing.T) {
	st := Station{
		Name:    "Sutlansko jezero",
		Prep:    Threshold{Raw: "206,30 m n. m."},
		Regular: Threshold{Raw: "207,30 m n. m."},
	}

	if st.HasUsableThresholds() {
		t.Fatal("postaja s pragovima u m n. m. ne smije se smatrati strojno usporedivom")
	}

	for _, level := range []int{0, 500, 5000} {
		if got := st.CalculateDefensePhase(level); got != PhaseUnknown {
			t.Errorf("vodostaj %d cm → faza %q, očekivano %q", level, got, PhaseUnknown)
		}
	}
}

// Kad su upisani samo neki pragovi, računa se prema onima koji postoje
func TestDjelomicniPragovi(t *testing.T) {
	st := Station{Name: "Djelomična", Regular: cm(500)}

	if got := st.CalculateDefensePhase(499); got != PhaseNormal {
		t.Errorf("ispod jedinog praga očekivano %q, dobiveno %q", PhaseNormal, got)
	}
	if got := st.CalculateDefensePhase(500); got != PhaseRegular {
		t.Errorf("na jedinom pragu očekivano %q, dobiveno %q", PhaseRegular, got)
	}
	// Bez praga za izvanredno stanje ne smije se preskočiti u težu fazu
	if got := st.CalculateDefensePhase(99999); got != PhaseRegular {
		t.Errorf("bez viših pragova očekivano %q, dobiveno %q", PhaseRegular, got)
	}
}

// Nepoznata faza ne smije se u sortiranju izjednačiti s redovnim stanjem
func TestSeverityNepoznateFaze(t *testing.T) {
	if PhaseUnknown.Severity() >= PhaseNormal.Severity() {
		t.Errorf("nepoznata faza (%d) mora biti ispod redovnog stanja (%d)",
			PhaseUnknown.Severity(), PhaseNormal.Severity())
	}

	ordered := []DefensePhase{PhaseUnknown, PhaseNormal, PhasePrep, PhaseRegular, PhaseEmergency, PhaseState}
	for i := 1; i < len(ordered); i++ {
		if ordered[i-1].Severity() >= ordered[i].Severity() {
			t.Errorf("faza %q nije kritičnija od %q", ordered[i], ordered[i-1])
		}
	}
}

func TestThresholdLabel(t *testing.T) {
	if got := cm(880).Label(); got != "+880 cm" {
		t.Errorf("prag u cm → %q, očekivano %q", got, "+880 cm")
	}
	if got := (Threshold{Raw: "206,30 m n. m."}).Label(); got != "206,30 m n. m." {
		t.Errorf("prag u koti mora zadržati izvorni zapis, dobiveno %q", got)
	}
	if got := (Threshold{}).Label(); got != "—" {
		t.Errorf("prazan prag → %q, očekivano %q", got, "—")
	}
}

func TestHasNewZeroDatum(t *testing.T) {
	staraKota := 76.28
	st := Station{ZeroDatum: &staraKota, ZeroDatumSystem: ZeroDatumSystemOld}

	if st.HasNewZeroDatum() {
		t.Error("kota u novom sustavu ne smije se podrazumijevati iz stare")
	}

	novaKota := 76.50
	st.ZeroDatumNew = &novaKota
	if !st.HasNewZeroDatum() {
		t.Error("upisana kota u novom sustavu nije prepoznata")
	}
}
