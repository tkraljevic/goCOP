package hydro

import "testing"

func intPtr(v int) *int { return &v }

func TestParseThresholdCm(t *testing.T) {
	tests := []struct {
		raw  string
		want *int
	}{
		{"+ 600", intPtr(600)},
		{"+1080", intPtr(1080)},
		{"580", intPtr(580)},
		{"- 20", intPtr(-20)},
		{"", nil},
		{"206,30 m n. m.", nil},
		{"+96,00 mnm", nil},
		{"+150 (normalna usporna razina vode 93,50)", nil},
		{"Prema Pravilniku akumulacije Borovik i prema", nil},
		{"hidrometeorološka prognoza", nil},
	}

	for _, tc := range tests {
		got := ParseThresholdCm(tc.raw)
		switch {
		case tc.want == nil && got != nil:
			t.Errorf("ParseThresholdCm(%q) = %d, očekivano nil (prag nije u centimetrima)", tc.raw, *got)
		case tc.want != nil && got == nil:
			t.Errorf("ParseThresholdCm(%q) = nil, očekivano %d", tc.raw, *tc.want)
		case tc.want != nil && got != nil && *got != *tc.want:
			t.Errorf("ParseThresholdCm(%q) = %d, očekivano %d", tc.raw, *got, *tc.want)
		}
	}
}

func TestParseStationName(t *testing.T) {
	tests := []struct {
		raw            string
		wantName       string
		wantStationing string
	}{
		{"Županja, rkm 271+900 (76,28)", "Županja", "rkm 271+900"},
		{"Sava - Jasenovac rkm 525+200 (86,82)", "Sava - Jasenovac", "rkm 525+200"},
		{"Prkovci, pkm 12+160 (79,14)", "Prkovci", "pkm 12+160"},
		{"Sutlansko jezero, km 60+486 (196,00)", "Sutlansko jezero", "km 60+486"},
		{"Gliboki-Mlačine , 27+760 km", "Gliboki-Mlačine", "27+760 km"},
		// Tisućice i decimale zajedno: stacionaža se ne smije odsjeći na "1.333"
		{"Vukovar , rkm 1.333,45 (76,19)", "Vukovar", "rkm 1.333,45"},
		{"Batina , rkm 1.424,85 (80,450)", "Batina", "rkm 1.424,85"},
		{"brana Letaj", "brana Letaj", ""},
	}

	for _, tc := range tests {
		name, stationing := ParseStationName(tc.raw)
		if name != tc.wantName {
			t.Errorf("ParseStationName(%q) naziv = %q, očekivano %q", tc.raw, name, tc.wantName)
		}
		if stationing != tc.wantStationing {
			t.Errorf("ParseStationName(%q) stacionaža = %q, očekivano %q", tc.raw, stationing, tc.wantStationing)
		}
	}
}

func TestParseZeroDatum(t *testing.T) {
	if got := ParseZeroDatum("Županja, rkm 271+900 (76,28)"); got == nil || *got != 76.28 {
		t.Errorf("kota nule za Županju nije pročitana: %v", got)
	}
	// Kota 0,00 znači "nije upisano" — ne smije se prikazati kao stvarna kota
	if got := ParseZeroDatum("Brezovica, rkm 26+171 (0,00)"); got != nil {
		t.Errorf("kota nule 0,00 mora se čitati kao nepoznata, dobiveno %v", *got)
	}
	if got := ParseZeroDatum("brana Letaj"); got != nil {
		t.Errorf("naziv bez kote ne smije dati kotu, dobiveno %v", *got)
	}
}

func TestStationKeySpajaVarijanteIstogVodomjera(t *testing.T) {
	variants := []string{
		"Ustava Trebež",
		"Sava Ustava Trebež",
		"Sava - ustava Trebež",
	}

	want := StationKey(variants[0])
	for _, v := range variants[1:] {
		if got := StationKey(v); got != want {
			t.Errorf("StationKey(%q) = %q, očekivano %q — varijante istog vodomjera moraju dati isti ključ", v, got, want)
		}
	}

	if StationKey("Županja") == StationKey("Osijek") {
		t.Error("različiti vodomjeri ne smiju dijeliti ključ")
	}
}

func TestParseKm(t *testing.T) {
	tests := []struct {
		raw  string
		want float64
	}{
		{"271+900", 271.9},
		{"1.424,85", 1424.85},
		{"1424,85", 1424.85},
		{"19,10", 19.1},
		{"0+000", 0},
	}

	for _, tc := range tests {
		got, ok := ParseKm(tc.raw)
		if !ok {
			t.Errorf("ParseKm(%q) nije pročitan", tc.raw)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseKm(%q) = %v, očekivano %v", tc.raw, got, tc.want)
		}
	}
}

// Naziv vodotoka mora podnijeti dijakritiku — rezanje po bajtu razbija "Česma"
func TestCapitalizeFirstNeLomiDijakritiku(t *testing.T) {
	tests := []struct{ in, want string }{
		{"česma", "Česma"},
		{"šumetlica", "Šumetlica"},
		{"đurđevac", "Đurđevac"},
		{"sava", "Sava"},
		{"", ""},
	}

	for _, tc := range tests {
		if got := CapitalizeFirst(tc.in); got != tc.want {
			t.Errorf("CapitalizeFirst(%q) = %q, očekivano %q", tc.in, got, tc.want)
		}
	}
}

// Vodotok postaje utvrđuje se samo kad ga dokumentacija tvrdi
func TestResolveStationWatercourse(t *testing.T) {
	dunav := []string{
		"r. Dunav, d.o.; Državna granica s Mađarskom – Zeleni otok; rkm 1425+770 - 1423+770",
		"p. Karašica, l.o. i d.o.; Ušće u r. Dunav kod Batine; km 0+000 - 12+500",
	}

	name, source := ResolveStationWatercourse("Batina , rkm 1424,85 (80,450)", "rkm 1424,85", dunav)
	if name != "Dunav" || source != "STACIONAŽA" {
		t.Errorf("Batina → (%q, %q), očekivano (Dunav, STACIONAŽA)", name, source)
	}

	name, source = ResolveStationWatercourse("Korana - Karlovac rkm 3+020 (103,36)", "rkm 3+020", nil)
	if name != "Korana" || source != "NAZIV" {
		t.Errorf("Karlovac → (%q, %q), očekivano (Korana, NAZIV)", name, source)
	}

	// Stacionaža upada u raspone dviju različitih voda — mora ostati prazno
	dvosmisleno := []string{
		"r. Drava, l.o.; Ušće u Dunav – željeznički most; rkm 0+000 - 25+000",
		"Poganovačko-kravički kanal, l.o. i d.o.; km 0+000 - 30+000",
	}
	if name, _ := ResolveStationWatercourse("Osijek , rkm 19,10 (81,480)", "rkm 19,10", dvosmisleno); name != "" {
		t.Errorf("Osijek → %q, očekivano prazno jer stacionaža nije jednoznačna", name)
	}
}
