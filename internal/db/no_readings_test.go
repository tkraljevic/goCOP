package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Očitanja vodostaja ne smiju ući u program. To su mjerenja Hrvatskih voda,
// koja na letvama očitavaju vodočuvari i strojari, i ne objavljuju se u
// javnom repozitoriju ni u binarnoj datoteci. Isto vrijedi za mjerenja
// DHMZ-a, koja Hrvatske vode koriste po ugovoru o uzajamnom korištenju.
// Očitanja žive samo u bazama čvorova: upisuju se s terena ili uvoze
// naredbom iz datoteke uz bazu, kao imenik.
//
// Ovaj test čuva to pravilo: sve u internal/db se ugrađuje u program.
func TestUgradeniPodaciNemajuOcitanja(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(".", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("nema ugrađenih datoteka")
	}
	// polja po kojima se prepoznaje zapis očitanja
	tragovi := []string{"measured_at", "vodostaj_uzvodni", "vodostaj_nizvodni", "level_cm", "\"vodostaji\"", "stanje_cs"}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		s := string(raw)
		for _, trag := range tragovi {
			if strings.Contains(s, trag) {
				t.Errorf("%s sadrži %q — očitanja vodostaja ne idu u program ni u repozitorij; "+
					"uvoze se iz datoteke uz bazu (vidi NOTICE)", f, trag)
			}
		}
	}
}
