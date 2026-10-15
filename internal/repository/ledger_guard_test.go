package repository

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Sinkronizirane tablice: svaki upis u njih mora ostaviti verziju u knjizi.
// Test čita izvorni kod repozitorija i pada ako datoteka mijenja neku od njih
// bez ijednog poziva Recordera — repozitorij koji zaobiđe knjigu ne prolazi.
func TestUpisiUSinkroniziraneTabliceIduKrozRecorder(t *testing.T) {
	// section_stations, section_structures i section_territories su kazala
	// izvedena iz dionice i ne putuju zasebno
	synced := []string{"sectors", "areas", "org_terms", "contractors", "contractor_assignments", "stations", "sections", "watercourses",
		"counties", "municipalities", "settlements", "users", "duties",
		"structures", "readings", "role_modules", "user_modules",
		"maintained_waters", "work_items", "journals", "journal_sheets", "journal_entries",
		"odbacena_ocitanja"}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	primjenaPise := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		code := string(src)

		for _, table := range synced {
			writes := regexp.MustCompile(`(?i)(INSERT INTO|UPDATE|DELETE FROM)\s+` + table + `\b`)
			if !writes.MatchString(code) {
				continue
			}
			if datotekePrimjene[file] {
				primjenaPise[file] = true
				continue
			}
			if !strings.Contains(code, "rec.Record") && !strings.Contains(code, "rec.Archive") {
				t.Errorf("%s piše u tablicu %s, a nijednom ne poziva Recorder — verzija se ne bilježi", file, table)
			}
		}
	}
	// iznimka koja više ne treba ruši test, da popis ne zastari
	for file := range datotekePrimjene {
		if !primjenaPise[file] {
			t.Errorf("%s ne piše u sinkronizirane tablice ili je nema: maknuti je s popisa datotekePrimjene", file)
		}
	}
}

// datotekePrimjene su datoteke primjena po skupinama koje pišu u
// sinkronizirane tablice verzijama primljenima s drugog čvora. Verzije već
// postoje u knjizi, a stvaranje novih bi ih umnožilo među čvorovima, pa ih
// test ne gleda. Ostale datoteke primjene (apply.go, apply_akti.go,
// apply_obracun.go, apply_mreza.go) u te tablice ne pišu i test ih gleda kao
// svaku drugu datoteku. Skupina koja počne pisati u sinkroniziranu tablicu
// ide na ovaj popis, a datoteka koja prestane, s njega.
var datotekePrimjene = map[string]bool{
	"apply_organizacija.go": true,
	"apply_registri.go":     true,
	"apply_hidrologija.go":  true,
	"apply_dnevnik.go":      true,
}
