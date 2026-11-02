package models

import "fmt"

// Smjer rada crpne stanice: odvodnja (crpi iz kanala u vodotok),
// navodnjavanje (puni kanalsku mrežu iz vodotoka) ili oboje. Prazno znači
// zapis iz vremena kad se znalo samo za reverzibilnu stanicu, pa smjer
// slijedi oznaku Reverzibilna.
const (
	SmjerOdvodnja               = "ODVODNJA"
	SmjerNavodnjavanje          = "NAVODNJAVANJE"
	SmjerOdvodnjaINavodnjavanje = "ODVODNJA_I_NAVODNJAVANJE"
)

// SmjeroviRada su smjerovi rada crpne stanice za izbor u obrascu, redom
var SmjeroviRada = []struct{ ID, Naziv string }{
	{SmjerOdvodnja, naziviSmjera[SmjerOdvodnja]},
	{SmjerNavodnjavanje, naziviSmjera[SmjerNavodnjavanje]},
	{SmjerOdvodnjaINavodnjavanje, naziviSmjera[SmjerOdvodnjaINavodnjavanje]},
}

// Smjer je smjer rada crpne stanice; stariji zapis bez smjera čita se iz
// oznake Reverzibilna
func (s Structure) Smjer() string {
	switch s.SmjerRada {
	case SmjerOdvodnja, SmjerNavodnjavanje, SmjerOdvodnjaINavodnjavanje:
		return s.SmjerRada
	}
	if s.Reverzibilna {
		return SmjerOdvodnjaINavodnjavanje
	}
	return SmjerOdvodnja
}

// SmjerNaziv je smjer rada riječima: „odvodnja”, „navodnjavanje”,
// „odvodnja i navodnjavanje”
func (s Structure) SmjerNaziv() string { return naziviSmjera[s.Smjer()] }

// naziviSmjera su nazivi smjerova rada po oznaci
var naziviSmjera = map[string]string{
	SmjerOdvodnja:               "odvodnja",
	SmjerNavodnjavanje:          "navodnjavanje",
	SmjerOdvodnjaINavodnjavanje: "odvodnja i navodnjavanje",
}

// SamoNavodnjava: crpna stanica crpi samo za navodnjavanje, pa joj je
// dovodna strana vodotok (vanjska), a ne kanal
func (s Structure) SamoNavodnjava() bool {
	return s.IsPumpingStation() && s.Smjer() == SmjerNavodnjavanje
}

// PostaviSmjer upisuje smjer rada i usklađuje oznaku Reverzibilna, koju
// čita program starijeg izdanja
func (s *Structure) PostaviSmjer(smjer string) {
	s.SmjerRada = smjer
	s.Reverzibilna = smjer == SmjerOdvodnjaINavodnjavanje
}

// ProvjeriSmjerRada odbija smjer koji program ne poznaje; prazno je
// dopušteno (stariji zapis)
func (s Structure) ProvjeriSmjerRada() error {
	switch s.SmjerRada {
	case "", SmjerOdvodnja, SmjerNavodnjavanje, SmjerOdvodnjaINavodnjavanje:
		return nil
	}
	return fmt.Errorf("smjer rada %q nije poznat: odaberite odvodnju, navodnjavanje ili oboje", s.SmjerRada)
}
