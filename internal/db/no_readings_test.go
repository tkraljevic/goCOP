package db

import (
	"path/filepath"
	"strings"
	"testing"
)

// U internal/db ne smije biti podataka: sve odande ugrađuje se u program,
// a repozitorij nosi samo program i shemu baze. Registri (dionice, vode,
// teritorij, objekti), imenik i očitanja stoje uz bazu, u mapi data/, i
// čitaju se samo pri prvom punjenju prvog čvora.
func TestUInternalDbNemaPodatkovnihDatoteka(t *testing.T) {
	for _, pattern := range []string{"*.json", "*.csv", "*.xlsx", "*.db"} {
		files, err := filepath.Glob(filepath.Join(".", pattern))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			t.Errorf("%s: podatkovne datoteke ne idu u internal/db ni u repozitorij; stoje uz bazu u data/ (vidi NOTICE)", f)
		}
	}
}

// Očitanja vodostaja ne smiju ući ni u datoteke uz bazu koje program čita
// kao registre: to su mjerenja Hrvatskih voda, koja na letvama očitavaju
// vodočuvari i strojari. Uvoze se zasebno, naredbom iz datoteke uz bazu.
func TestRegistriUzBazuNemajuOcitanja(t *testing.T) {
	if !UseRepoData() {
		t.Skip("data/ s registrima nije dostupan")
	}
	tragovi := []string{"measured_at", "vodostaj_uzvodni", "vodostaj_nizvodni", "level_cm", "\"vodostaji\"", "stanje_cs"}
	for _, name := range []string{"sections.json", "watercourses.json", "territories.json", "section_territories.json", "objekti_bp16.json"} {
		raw, err := readDataFile(name)
		if err != nil {
			continue
		}
		s := string(raw)
		for _, trag := range tragovi {
			if strings.Contains(s, trag) {
				t.Errorf("%s sadrži %q — očitanja vodostaja se uvoze zasebno (vidi NOTICE)", name, trag)
			}
		}
	}
}
