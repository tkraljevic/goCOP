package web

import "testing"

// Stupac "Gdje radi" izvoz piše kao "BP 34, Sektor B"; uvoz ga mora pročitati
// natrag, s nazivima razina kako ih organizacija zove.
func TestParseWhereCitaIzvoz(t *testing.T) {
	got := parseWhere("BP 34, Sektor B, bp 16, nešto nepoznato")
	if len(got) != 3 {
		t.Fatalf("mjesta rada: %v, očekivano 3", got)
	}
	if got[0].AreaID != 34 || got[1].SectorID != "B" || got[2].AreaID != 16 {
		t.Errorf("mjesta rada krivo pročitana: %+v", got)
	}
	if len(parseWhere("")) != 0 {
		t.Error("prazan stupac mora dati prazan popis")
	}
}

// Izvoz OIB oblaže s ="…" zbog Excela; uvoz ga mora skinuti.
func TestCsvPlainTextSkidaOmot(t *testing.T) {
	for in, want := range map[string]string{`="03674958581"`: "03674958581", "03674958581": "03674958581", ` ="1" `: "1", `=""`: ""} {
		if got := csvPlainText(in); got != want {
			t.Errorf("csvPlainText(%q) = %q, očekivano %q", in, got, want)
		}
	}
}
