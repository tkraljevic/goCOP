package models

import "testing"

func cmp(v int) *int { return &v }

// Povratno razdoblje se čita s brojem godina, a hrvatski broj traži tri
// oblika imenice. „25 godine" ili „2 godina" odmah odaju da brojku ispisuje
// stroj koji ne zna jezik.
func TestGodineLabelSlazeImenicuSBrojem(t *testing.T) {
	for _, s := range []struct {
		god  int
		want string
	}{
		{1, "1 godina"}, {2, "2 godine"}, {3, "3 godine"}, {4, "4 godine"},
		{5, "5 godina"}, {11, "11 godina"}, {12, "12 godina"}, {14, "14 godina"},
		{21, "21 godina"}, {22, "22 godine"}, {25, "25 godina"},
		{50, "50 godina"}, {100, "100 godina"}, {102, "102 godine"}, {1000, "1000 godina"},
	} {
		if got := (StationReturnLevel{Years: s.god}).GodineLabel(); got != s.want {
			t.Errorf("T=%d → %q, očekivano %q", s.god, got, s.want)
		}
	}
	if got := (StationReturnLevel{Years: 0}).GodineLabel(); got != "—" {
		t.Errorf("bez povratnog razdoblja: %q", got)
	}
}

// „Jednom u 100 godina" ljudi redovito čitaju kao „neće se ponoviti idućih
// sto godina". Ista brojka kao postotak po godini ne dopušta to čitanje, pa
// stoji uz svaki povratni vodostaj.
func TestSansaLabelPretvaraPovratnoRazdobljeUPostotak(t *testing.T) {
	for _, s := range []struct {
		god  int
		want string
	}{
		{2, "50 % svake godine"}, {25, "4 % svake godine"},
		{50, "2 % svake godine"}, {100, "1 % svake godine"},
		{3, "33,3 % svake godine"}, {1000, "0,1 % svake godine"},
	} {
		if got := (StationReturnLevel{Years: s.god}).SansaLabel(); got != s.want {
			t.Errorf("T=%d → %q, očekivano %q", s.god, got, s.want)
		}
	}
	if got := (StationReturnLevel{Years: 0}).SansaLabel(); got != "" {
		t.Errorf("bez povratnog razdoblja ne smije tvrditi postotak: %q", got)
	}
}

// Granice pouzdanosti znače nešto samo u paru. Jedna sama tvrdila bi interval
// koji nije izračunat, pa se raspon prikazuje cijeli ili nikako.
func TestRasponSePrikazujeSamoUParu(t *testing.T) {
	pun := StationReturnLevel{Years: 100, LevelCm: cmp(795), LowCm: cmp(762), HighCm: cmp(813)}
	if !pun.ImaRaspon() || pun.RasponLabel() != "+762 do +813 cm" {
		t.Errorf("puni raspon: %v %q", pun.ImaRaspon(), pun.RasponLabel())
	}
	pola := StationReturnLevel{Years: 100, LevelCm: cmp(795), LowCm: cmp(762)}
	if pola.ImaRaspon() || pola.RasponLabel() != "" {
		t.Errorf("polovica raspona se ne smije prikazati: %v %q", pola.ImaRaspon(), pola.RasponLabel())
	}
}

// Povratni vodostaji se čitaju od najčešćeg prema najrjeđem bez obzira na to
// kojim su redom upisani.
func TestPovratniVodostajiIduPoRazdoblju(t *testing.T) {
	st := Station{ReturnLevels: []StationReturnLevel{
		{Years: 100, LevelCm: cmp(795)}, {Years: 25, LevelCm: cmp(755)}, {Years: 50, LevelCm: cmp(778)},
	}}
	if !st.ImaPovratne() {
		t.Fatal("postaja ima povratne vodostaje")
	}
	got := st.PovratniVodostaji()
	for i, want := range []int{25, 50, 100} {
		if got[i].Years != want {
			t.Errorf("na %d. mjestu T=%d, očekivano %d", i, got[i].Years, want)
		}
	}
	// izvorni popis ostaje netaknut
	if st.ReturnLevels[0].Years != 100 {
		t.Error("slaganje po razdoblju ne smije mijenjati zapis postaje")
	}
	if (Station{}).ImaPovratne() {
		t.Error("postaja bez izračuna nema povratne vodostaje")
	}
}

// Povratni vodostaj nije prag obrane. Prag je propisan, povratni vodostaj
// proračunat — pa se ne smije naći među pragovima koji ulaze u fazu obrane.
func TestPovratniVodostajNijePrag(t *testing.T) {
	st := Station{ReturnLevels: []StationReturnLevel{{Years: 100, LevelCm: cmp(795)}}}
	if st.HasUsableThresholds() {
		t.Error("sam povratni vodostaj ne smije postaju učiniti pragovanom")
	}
	if f := st.CalculateDefensePhase(795); f != PhaseUnknown {
		t.Errorf("faza iz povratnog vodostaja: %v", f)
	}
}

// Uz naziv vode stoji i kako je utvrđena. Nije svejedno je li vodu potvrdio
// operater ili ju je program pogodio iz naziva vodomjera, a šifra izvora
// („STACIONAŽA") čitatelju ne znači ništa.
func TestPodrijetloVodotokaSeCitaRijecima(t *testing.T) {
	for izvor, want := range map[string]string{
		WatercourseFromName:       "iz naziva vodomjera",
		WatercourseFromStationing: "iz stacionaže",
		WatercourseFromSections:   "izvedeno iz dionica",
		WatercourseFromOperator:   "potvrdio operater",
		WatercourseUndetermined:   "",
	} {
		if got := (Station{WatercourseSource: izvor}).PodrijetloVodotoka(); got != want {
			t.Errorf("izvor %q → %q, očekivano %q", izvor, got, want)
		}
	}
	// Nepoznata oznaka se ne izmišlja niti guta — prolazi kakva jest.
	if got := (Station{WatercourseSource: "ELABORAT"}).PodrijetloVodotoka(); got != "ELABORAT" {
		t.Errorf("nepoznat izvor: %q", got)
	}
}
