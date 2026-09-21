package hidroview

import (
	"testing"
	"time"
)

// Dohvat podataka ne vraća JSON nego CSV: prvi stupac je vrijeme u
// sekundama, drugi vrijednost u osnovnoj jedinici. Vodostaj ondje stoji u
// metrima — 0,42948 je 42,948 cm, kako ga i njihovo sučelje prikazuje.
func TestCitajCSV(t *testing.T) {
	b := []byte("timestamp,5QpcTwBap96-GpsvHLvB\n" +
		"1790005556,0.42401\n" +
		"1789833656,0.42206\n" +
		"\n")
	v, err := CitajCSV(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 2 {
		t.Fatalf("redaka %d, očekivano 2", len(v))
	}
	// najstariji prvi, bez obzira kako su stigli
	if !v[0].Kad.Equal(time.Unix(1789833656, 0).UTC()) || v[0].Vrijednost != 0.42206 {
		t.Errorf("prvi redak: %+v", v[0])
	}
	if v[1].Vrijednost != 0.42401 {
		t.Errorf("drugi redak: %+v", v[1])
	}
}

// Kad nešto pođe po zlu, poslužitelj odgovori JSON-om. To ne smije proći
// kao prazan niz, jer bi letva izgledala kao da nema novih podataka.
func TestCitajCSVJavljaGresku(t *testing.T) {
	if _, err := CitajCSV([]byte(`{"status":"forbidden"}`)); err == nil {
		t.Error("odgovor sa statusom mora javiti grešku")
	}
	if _, err := CitajCSV([]byte("nešto deseto")); err == nil {
		t.Error("neprepoznat odgovor mora javiti grešku")
	}
}
