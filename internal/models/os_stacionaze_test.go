package models

import (
	"strings"
	"testing"
)

// Na istoj dionici stoje obje osi: crpna stanica ima rkm uz rijeku i kkm uz
// kanal, a nasip svoj nkm. Boja ih razdvaja — voda plava, nasip zelen — pa se
// 1419+900 i 1+650 ne čitaju kao jedan niz.
func TestOsStacionazeRazlikujeVoduOdNasipa(t *testing.T) {
	slucajevi := []struct{ oznaka, zelim string }{
		{"rkm 1419+900", "voda"}, // rijeka
		{"pkm 3+200", "voda"},    // potok
		{"bkm 0+450", "voda"},    // bujica
		{"kkm 1+650", "voda"},    // kanal
		{"nkm 0+000 - 18+000", "nasip"},
		{"km 19+550", "nasip"}, // goli kilometar je uz nasip, kako stoji u Privitku
		{"RKM 1424+850", "voda"},
		{"  kkm 5+436  ", "voda"},
		{"(18,000 km)", ""}, // duljina, ne stacionaža
		{"", ""},
		{"1424+850", ""}, // bez oznake osi se ne pogađa
	}
	for _, s := range slucajevi {
		if got := OsStacionaze(s.oznaka); got != s.zelim {
			t.Errorf("%q: dobiveno %q, očekivano %q", s.oznaka, got, s.zelim)
		}
	}
}

// Odsjek nasipa nosi obje osi u jednom nizu znakova; za prikaz se mora razložiti
// da svaki komad dobije boju svoje osi.
func TestRangeDijeloviRazlazeOsi(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	e := PartEmbankment{
		WaterKind: "rkm", WaterFrom: f(1403), WaterTo: f(1421),
		EmbFrom: f(0), EmbTo: f(18), LengthKm: f(18),
	}
	d := e.RangeDijelovi()
	// Dva komada: uz vodu i uz nasip. Duljina ispada — nije stacionaža i u
	// zaglavlju kartice stoji zasebno, pa bi se inače ispisala dvaput.
	if len(d) != 2 {
		t.Fatalf("razloženo %d dijelova: %q", len(d), d)
	}
	if OsStacionaze(d[0]) != "voda" {
		t.Errorf("prvi dio %q nije prepoznat kao voda", d[0])
	}
	if OsStacionaze(d[1]) != "nasip" {
		t.Errorf("drugi dio %q nije prepoznat kao nasip", d[1])
	}
	for _, x := range d {
		if strings.Contains(x, "km)") {
			t.Errorf("duljina %q je ostala među stacionažama", x)
		}
	}
	// Prazan nasip ne daje komade.
	if d := (PartEmbankment{}).RangeDijelovi(); len(d) != 0 {
		t.Errorf("prazan nasip dao %q", d)
	}
}
