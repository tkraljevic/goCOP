package arso

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Zaključava čitanje ARSO izvoza kojem nedostaju stupci i odgovor na
// datoteku koja se ne da pročitati.

func izvozARSO(t *testing.T, sadrzaj string) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "arso.csv")
	if err := os.WriteFile(put, []byte(sadrzaj), 0o644); err != nil {
		t.Fatal(err)
	}
	return put
}

// Izvoz samo s protokom daje samo protok; vodostaj i temperatura se broje
// kao prazni, a ne kao neispravni.
func TestCitajSamoProtok(t *testing.T) {
	r, err := Citaj([]string{izvozARSO(t, "\ufeffDatum;pretok (m3/s)\n01.01.2024;254\n2.1.2024.;-1\n")})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Protoci) != 1 || len(r.Vodostaji) != 0 || len(r.Temperature) != 0 {
		t.Errorf("nizovi: %+v", r)
	}
	iz := r.Izvjestaj
	if iz.BezVodostaja != 2 || iz.BezTemperature != 2 || iz.NeispravnihProtoka != 1 || iz.NeispravnihVodostaja != 0 {
		t.Errorf("izvještaj: %+v", iz)
	}
}

// Datoteka koje nema, prazna datoteka, izvoz bez datuma ili bez ijedne
// veličine i pokvaren CSV zaustavljaju čitanje.
func TestCitajGreske(t *testing.T) {
	slucajevi := map[string]struct {
		put    string
		greska string
	}{
		"nema datoteke":  {filepath.Join(t.TempDir(), "nema.csv"), "no such file"},
		"prazna":         {izvozARSO(t, ""), "zaglavlje"},
		"bez datuma":     {izvozARSO(t, "Dan;vodostaj (cm)\n"), "nisu pronađeni"},
		"bez veličina":   {izvozARSO(t, "Datum;opomba\n"), "nisu pronađeni"},
		"pokvaren redak": {izvozARSO(t, "Datum;vodostaj (cm)\n01.01.2024;\"12\n"), "redak 2"},
	}
	for ime, s := range slucajevi {
		_, err := Citaj([]string{s.put})
		if err == nil || !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: %v", ime, err)
		}
	}
}
