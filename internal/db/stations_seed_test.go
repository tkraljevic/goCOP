package db

import (
	"strings"
	"testing"

	"gocop/internal/hydro"
)

// Testovi u ovom paketu provjeravaju seed nad CIJELOM dokumentacijom dionica
// (sections.json). Jedinični testovi parsera žive u internal/hydro.

func loadSections(t *testing.T) []seedSectionGauges {
	t.Helper()
	if !UseRepoData() {
		t.Skip("data/sections.json nije dostupan — registri stoje izvan repozitorija")
	}
	embedded, err := LoadSections()
	if err != nil {
		t.Fatalf("sections.json se ne može pročitati: %v", err)
	}
	return gaugesFromSections(embedded)
}

// TestBuildStationDraftsNaStvarnimPodacima provjerava parser na cijeloj
// dokumentaciji dionica, a ne samo na odabranim primjerima
func TestBuildStationDraftsNaStvarnimPodacima(t *testing.T) {
	sections := loadSections(t)
	drafts, skipped := buildStationDrafts(sections)

	if len(drafts) == 0 {
		t.Fatal("iz dokumentacije nije izvedena nijedna postaja")
	}

	totalGauges := 0
	for _, s := range sections {
		totalGauges += len(s.Gauges)
	}

	links := 0
	codes := make(map[string]string)
	withThresholds := 0
	needsReview := 0

	for _, d := range drafts {
		links += len(d.SectionCodes)

		if prev, dup := codes[d.Code]; dup {
			t.Errorf("šifra postaje %q dodijeljena je dvaput (%q i %q)", d.Code, prev, d.Name)
		}
		codes[d.Code] = d.Name

		if d.Name == "" {
			t.Errorf("postaja bez naziva iz zapisa %q", d.SourceName)
		}
		if d.Prep.Cm != nil || d.Regular.Cm != nil || d.Emergency.Cm != nil || d.State.Cm != nil {
			withThresholds++
		}
		if d.NeedsReview {
			needsReview++
		}
	}

	// Registar mora biti znatno manji od broja zapisa (isti vodomjer ponavlja se
	// na svim dionicama za koje je mjerodavan), a veza mora biti barem po jedna
	// za svaki iskorišteni zapis.
	if len(drafts) >= totalGauges {
		t.Errorf("nema sažimanja duplikata: %d postaja iz %d zapisa", len(drafts), totalGauges)
	}
	if links < len(drafts) {
		t.Errorf("manje veza (%d) nego postaja (%d)", links, len(drafts))
	}
	if len(skipped)+links < totalGauges {
		t.Errorf("izgubljeni zapisi: %d zapisa, %d veza + %d preskočeno", totalGauges, links, len(skipped))
	}

	t.Logf("zapisa vodomjera: %d → postaja: %d, veza s dionicama: %d, preskočeno: %d",
		totalGauges, len(drafts), links, len(skipped))
	t.Logf("postaja s barem jednim pragom u cm: %d, označeno za pregled: %d", withThresholds, needsReview)
}

// Nijedan izvedeni vodotok ne smije zadržati oznaku obale
func TestNijedanVodotokNemaOznakuObale(t *testing.T) {
	drafts, _ := buildStationDrafts(loadSections(t))

	for _, d := range drafts {
		lower := strings.ToLower(d.Watercourse)
		for _, bad := range []string{"l.o", "d.o", " l o", " d o"} {
			if strings.Contains(lower, bad) {
				t.Errorf("vodotok %q sadrži oznaku obale", d.Watercourse)
			}
		}
	}
}

// Vodomjer stoji na jednoj vodi, a mjerodavan je i za dionice drugih voda.
// Batina stoji na Dunavu i mjerodavna je za dionice potoka Karašice na ušću;
// lokacija se ne smije preuzeti iz dionice za koju je postaja mjerodavna.
func TestVodotokPostajeNijeVodotokDionice(t *testing.T) {
	drafts, _ := buildStationDrafts(loadSections(t))
	byName := map[string]*stationDraft{}
	for _, d := range drafts {
		byName[d.Name] = d
	}

	tests := []struct {
		postaja string
		want    string
		zasto   string
	}{
		{"Batina", "Dunav", "stoji na Dunavu, mjerodavna i za potok Karašicu"},
		{"Vukovar", "Dunav", "stoji na Dunavu, mjerodavna i za rijeku Vuku"},
		{"Županja", "Sava", "rkm 271+900 upada samo u savske raspone"},
	}

	for _, tc := range tests {
		d, ok := byName[tc.postaja]
		if !ok {
			t.Errorf("postaja %q nije u registru", tc.postaja)
			continue
		}
		if d.Watercourse != tc.want {
			t.Errorf("%s → vodotok %q, očekivano %q (%s); dionice: %v",
				tc.postaja, d.Watercourse, tc.want, tc.zasto, d.SectionCodes)
		}
	}

	// Osijek rkm 19,10 upada i u dravske raspone i u raspon
	// Poganovačko-kravičkog kanala — vodotok mora ostati neodređen
	if d, ok := byName["Osijek"]; ok && d.Watercourse != "" {
		t.Errorf("Osijek → %q, očekivano prazno jer stacionaža nije jednoznačna", d.Watercourse)
	}
}

// Vodotok se upisuje samo kad ga dokumentacija tvrdi — nikad pogađanjem
func TestVodotokSeNeNagadja(t *testing.T) {
	drafts, _ := buildStationDrafts(loadSections(t))

	utvrdjeno, prazno := 0, 0
	for _, d := range drafts {
		if d.Watercourse == "" {
			prazno++
			if d.WatercourseSource != "" {
				t.Errorf("postaja %q nema vodotok, a ima naveden izvor %q", d.Name, d.WatercourseSource)
			}
			continue
		}
		utvrdjeno++
		if d.WatercourseSource == "" {
			t.Errorf("postaja %q ima vodotok %q bez naznake odakle je utvrđen", d.Name, d.Watercourse)
		}
	}

	t.Logf("vodotok utvrđen: %d, neodređen: %d od %d postaja", utvrdjeno, prazno, len(drafts))
}

// Popis mjerodavnih vodomjera po dionicama je službeni podatak iz Privitka 1
// (Hrvatske vode, teritorijalne jedinice, ožujak 2018., izmjena veljača 2022.).
//
// sections.json je vjeran prijepis tog dokumenta — usporedbom s izvornim
// wiki zapisima potvrđeno je 423 dionice i 534 retka vodomjera, bez razlike.
// Svaki redak iz dokumentacije mora završiti kao veza dionice i postaje —
// osim redaka koji nisu vodomjeri nego upute ("Prema Pravilniku akumulacije").
func TestSvakiVodomjerIzDokumentacijeJePovezanSaSvojomDionicom(t *testing.T) {
	sections := loadSections(t)
	drafts, skipped := buildStationDrafts(sections)

	linked := map[string]map[string]bool{}
	for _, d := range drafts {
		for _, code := range d.SectionCodes {
			if linked[code] == nil {
				linked[code] = map[string]bool{}
			}
			linked[code][d.Key] = true
		}
	}

	skippedRows := map[string]bool{}
	for _, s := range skipped {
		skippedRows[s] = true
	}

	missing := 0
	for _, section := range sections {
		for _, gauge := range section.Gauges {
			source := strings.TrimSpace(gauge.StationName)
			if source == "" || skippedRows[section.Code+": "+source] {
				continue
			}

			name, _ := hydro.ParseStationName(source)
			key := hydro.StationKey(name)
			if key == "" {
				key = hydro.StationKey(source)
			}

			if !linked[section.Code][key] {
				missing++
				if missing <= 10 {
					t.Errorf("dionica %s: vodomjer %q iz dokumentacije nije povezan", section.Code, source)
				}
			}
		}
	}

	if missing > 0 {
		t.Errorf("ukupno nepovezanih redaka iz dokumentacije: %d", missing)
	}

	t.Logf("preskočeno redaka koji nisu vodomjeri nego kriteriji: %d", len(skipped))
}
