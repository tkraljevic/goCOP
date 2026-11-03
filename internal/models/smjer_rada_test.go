package models

import "testing"

// Smjer rada: upisani smjer, a stariji zapis bez smjera po oznaci
// Reverzibilna; upis smjera usklađuje oznaku za starije izdanje
func TestSmjerRada(t *testing.T) {
	for _, s := range []struct {
		st                Structure
		smjer, naziv      string
		samoNavodnjavanje bool
	}{
		{Structure{}, SmjerOdvodnja, "odvodnja", false},
		{Structure{Reverzibilna: true}, SmjerOdvodnjaINavodnjavanje, "odvodnja i navodnjavanje", false},
		{Structure{Kind: StructureKindPumpingStation, SmjerRada: SmjerNavodnjavanje}, SmjerNavodnjavanje, "navodnjavanje", true},
		{Structure{Kind: StructureKindSluice, SmjerRada: SmjerNavodnjavanje}, SmjerNavodnjavanje, "navodnjavanje", false},
		{Structure{SmjerRada: "NEPOZNATO", Reverzibilna: true}, SmjerOdvodnjaINavodnjavanje, "odvodnja i navodnjavanje", false},
	} {
		if s.st.Smjer() != s.smjer || s.st.SmjerNaziv() != s.naziv || s.st.SamoNavodnjava() != s.samoNavodnjavanje {
			t.Errorf("%+v: %s %q %v", s.st, s.st.Smjer(), s.st.SmjerNaziv(), s.st.SamoNavodnjava())
		}
	}
	var st Structure
	st.PostaviSmjer(SmjerOdvodnjaINavodnjavanje)
	if !st.Reverzibilna {
		t.Error("oba smjera bez oznake reverzibilna")
	}
	st.PostaviSmjer(SmjerNavodnjavanje)
	if st.Reverzibilna || st.ProvjeriSmjerRada() != nil {
		t.Errorf("navodnjavanje: %+v", st)
	}
	if (Structure{SmjerRada: "UZVODNO"}).ProvjeriSmjerRada() == nil {
		t.Error("nepoznat smjer je prihvaćen")
	}
}
