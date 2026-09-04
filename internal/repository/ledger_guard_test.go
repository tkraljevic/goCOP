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
	synced := []string{"stations", "section_stations", "sections", "watercourses",
		"counties", "municipalities", "settlements", "section_territories", "users", "duties",
		"structures", "section_structures", "readings", "role_modules", "user_modules"}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	for _, file := range files {
		// apply.go primjenjuje verzije primljene s drugog čvora na površinu —
		// verzije već postoje, stvaranje novih bi ih umnožilo među čvorovima
		if strings.HasSuffix(file, "_test.go") || file == "apply.go" {
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
			if !strings.Contains(code, "rec.Record(") && !strings.Contains(code, "rec.Archive(") {
				t.Errorf("%s piše u tablicu %s, a nijednom ne poziva Recorder — verzija se ne bilježi", file, table)
			}
		}
	}
}
