package seba

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Zaključava kako čitanje izvoza SEBA odgovara na neispravnu datoteku: što
// zaustavlja čitanje, a što se samo prebroji u izvještaju.

func izvoz(t *testing.T, sadrzaj string) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "seba.csv")
	if err := os.WriteFile(put, []byte(sadrzaj), 0o644); err != nil {
		t.Fatal(err)
	}
	return put
}

// Nepostojeća datoteka, prazna datoteka, izvoz bez traženih stupaca,
// pokvaren CSV i nečitljivo vrijeme zaustavljaju čitanje cijelog izvoza.
func TestCitajGreske(t *testing.T) {
	slucajevi := map[string]struct {
		put    string
		greska string
	}{
		"nema datoteke":      {filepath.Join(t.TempDir(), "nema.csv"), "no such file"},
		"prazna":             {izvoz(t, ""), "zaglavlje"},
		"bez stupaca":        {izvoz(t, "Time,Device Temperature [°C]\n"), "nedostaje stupac"},
		"pokvaren CSV":       {izvoz(t, glava+"2025-01-01 12:00,5,\"1.2,8\n"), "redak 2"},
		"nečitljivo vrijeme": {izvoz(t, glava+"01.01.2025 12:00,5,1.234,8.2\n"), "vrijeme \"01.01.2025 12:00\" nije čitljivo"},
	}
	for ime, s := range slucajevi {
		_, err := Citaj([]string{s.put})
		if err == nil || !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: %v", ime, err)
		}
	}
}

// Kratak redak, nečitljiv broj i vrijednost izvan mogućeg raspona ne
// zaustavljaju čitanje; broje se u izvještaju. Vrijeme sa sekundama se
// prihvaća.
func TestCitajBrojiNeispravneVrijednosti(t *testing.T) {
	put := izvoz(t, glava+
		"2025-01-01 12:00:00,5\n"+
		"2025-01-01 13:00,5,nije broj,x\n"+
		"2025-01-01 14:00,5,250,-80\n"+
		"2025-01-01 15:00,5,1.5,8.5\n"+
		"2025-01-01 15:00,5,1.6,8.6\n")
	r, err := Citaj([]string{put})
	if err != nil {
		t.Fatal(err)
	}
	iz := r.Izvjestaj
	if iz.Redaka != 5 || iz.BezVodostaja != 1 || iz.BezTemperature != 1 ||
		iz.NeispravnihVodostaja != 2 || iz.NeispravnihTemperatura != 2 || iz.Duplikata != 1 || iz.Sukoba != 2 {
		t.Errorf("izvještaj: %+v", iz)
	}
	if len(r.Vodostaji) != 1 || r.Vodostaji[0].Vrijednost != 160 || len(r.Temperature) != 1 || r.Temperature[0].Vrijednost != 8.6 {
		t.Errorf("vrijednosti: %+v %+v — zadnja valjana pobjeđuje", r.Vodostaji, r.Temperature)
	}
}
