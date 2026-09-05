package db

import (
	"encoding/json"
	"strings"
	"testing"

	"gocop/internal/hydro"
)

func TestRegistarVodnihTijelaJeCjelovit(t *testing.T) {
	if !UseRepoData() {
		t.Skip("data/ s registrima nije dostupan — registri stoje izvan repozitorija")
	}
	raw, err := readDataFile("watercourses.json")
	if err != nil {
		t.Fatalf("watercourses.json se ne može pročitati: %v", err)
	}
	var waters []seedWatercourse
	if err := json.Unmarshal(raw, &waters); err != nil {
		t.Fatalf("watercourses.json se ne može pročitati: %v", err)
	}

	if len(waters) < 400 {
		t.Errorf("registar ima samo %d vodnih tijela — očekuje se cijeli popis", len(waters))
	}

	codes := map[string]string{}
	firstOrder := 0
	for _, w := range waters {
		if strings.TrimSpace(w.OfficialName) == "" {
			t.Error("vodno tijelo bez službenog naziva")
			continue
		}
		// Nepotpuno raščlanjena wiki-poveznica bi ostavila zagrade u nazivu
		if strings.Contains(w.OfficialName, "[[") || strings.Contains(w.Name, "[[") {
			t.Errorf("naziv %q nosi ostatak wiki-poveznice", w.OfficialName)
		}

		code := hydro.WatercourseCode(w.OfficialName)
		if prev, dup := codes[code]; dup {
			t.Errorf("šifra %q dodijeljena dvaput: %q i %q", code, prev, w.OfficialName)
		}
		codes[code] = w.OfficialName

		if w.Category != "" {
			firstOrder++
		}
	}

	t.Logf("vodnih tijela: %d, od toga na popisu voda I. reda: %d", len(waters), firstOrder)
}
