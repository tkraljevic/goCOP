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

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || datotekePrimjene[file] {
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
			if !strings.Contains(code, "rec.Record") && !strings.Contains(code, "rec.Archive") {
				t.Errorf("%s piše u tablicu %s, a nijednom ne poziva Recorder — verzija se ne bilježi", file, table)
			}
		}
	}
}

// datotekePrimjene primjenjuju verzije primljene s drugog čvora na površinu
// (applyOne, removeFromSurface i primjene po skupinama). Verzije već postoje
// u knjizi, a stvaranje novih bi ih umnožilo među čvorovima, pa straže knjige
// ove datoteke ne gledaju. Svaka druga datoteka koja piše u sinkroniziranu
// tablicu mora bilježiti verziju.
var datotekePrimjene = map[string]bool{
	"apply.go":              true,
	"apply_mreza.go":        true,
	"apply_organizacija.go": true,
}
