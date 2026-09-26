package izvori

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sebaGlava = "Time,Geolux SmartObserver - Device Temperature [°C],Vodostaj Seba - Average Water Level [m],HydroTemp - Water Temperature [°C]\n"

func datoteke(t *testing.T, koren string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(koren, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			r, _ := filepath.Rel(koren, p)
			out = append(out, r)
		}
		return nil
	})
	return out
}

// SEBA iz dvije datoteke daje dva satna niza pod zadanim izvorom; probni
// prolaz ne zapisuje ništa.
func TestSEBAZapisujeDvaNiza(t *testing.T) {
	f, _ := Nadji("seba")
	koren := t.TempDir()
	dat := []Datoteka{
		{Ime: "prva.csv", Sadrzaj: []byte(sebaGlava + "2025-01-01 12:00,5,1.234,8.2\n")},
		{Ime: "druga.csv", Sadrzaj: []byte(sebaGlava + "2025-01-01 13:00,5,1.240,8.4\n")},
	}
	z := Zadatak{Koren: koren, Sliv: "dunav", Letva: "tikves", Probno: true}
	var dnevnik strings.Builder
	ishod, err := f.Uvezi(dat, z, &dnevnik)
	if err != nil {
		t.Fatal(err)
	}
	if len(datoteke(t, koren)) != 0 || len(ishod.Zapisano) != 0 {
		t.Fatal("probni prolaz je zapisao")
	}
	if !strings.Contains(dnevnik.String(), "vodostaja: 2") {
		t.Errorf("dnevnik: %s", dnevnik.String())
	}
	z.Probno = false
	ishod, err = f.Uvezi(dat, z, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(datoteke(t, koren), " ")
	for _, dio := range []string{"tikves_geolux-seba_vodostaj_satni_", "tikves_geolux-seba_temperatura_satni_"} {
		if !strings.Contains(got, dio) {
			t.Errorf("nema %s u %s", dio, got)
		}
	}
	if len(ishod.Letve) != 1 || ishod.Letve[0] != "tikves" {
		t.Errorf("letve: %v", ishod.Letve)
	}
}

func TestPegelonlineUzimaPuniSat(t *testing.T) {
	f, _ := Nadji("pegelonline")
	koren := t.TempDir()
	json := `[{"timestamp":"2026-08-21T02:00:00+02:00","value":253},{"timestamp":"2026-08-21T02:15:00+02:00","value":254}]`
	if _, err := f.Uvezi([]Datoteka{{Ime: "k.json", Sadrzaj: []byte(json)}},
		Zadatak{Koren: koren, Letva: "kienstock"}, nil); err != nil {
		t.Fatal(err)
	}
	d := datoteke(t, koren)
	if len(d) != 1 || !strings.HasPrefix(d[0], filepath.Join("dunav", "kienstock", "kienstock_pegelonline_vodostaj_satni_")) {
		t.Fatalf("zapisano: %v (sliv mora biti zadani dunav)", d)
	}
}

// Zadatak se provjerava prije ikakva čitanja.
func TestProvjeraZadatka(t *testing.T) {
	koren := t.TempDir()
	csv := []Datoteka{{Ime: "a.csv", Sadrzaj: []byte("x")}}
	slucajevi := []struct {
		format string
		dat    []Datoteka
		z      Zadatak
		greska string
	}{
		{"arso", csv, Zadatak{Koren: koren, Sliv: "drava"}, "treba letva"},
		{"arso", []Datoteka{{Ime: "a.json"}}, Zadatak{Koren: koren, Sliv: "drava", Letva: "ptuj"}, "ne prima datoteke"},
		{"ehyd", csv, Zadatak{Koren: koren, Sliv: "dunav", Letva: "angern"}, "treba veličina"},
		{"ehyd", append(csv, csv...), Zadatak{Koren: koren, Sliv: "dunav", Letva: "angern", Velicina: "vodostaj"}, "prima jednu datoteku"},
		{"arso", csv, Zadatak{Koren: koren, Letva: "ptuj"}, "treba sliv"},
		{"godisnjak", []Datoteka{{Ime: "1991.pdf"}}, Zadatak{Koren: koren}, "šifra=letva"},
		{"arso", csv, Zadatak{Koren: koren, Sliv: "drava", Letva: "../x"}, ""},
		{"arso", nil, Zadatak{Koren: koren, Sliv: "drava", Letva: "ptuj"}, "nijedna datoteka"},
	}
	for _, s := range slucajevi {
		f, _ := Nadji(s.format)
		_, err := f.Uvezi(s.dat, s.z, nil)
		if err == nil {
			t.Errorf("%s %+v: prošlo bez greške", s.format, s.z)
			continue
		}
		if s.greska != "" && !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: greška %q, očekivano %q", s.format, err, s.greska)
		}
	}
	if len(datoteke(t, koren)) != 0 {
		t.Error("neispravan zadatak je zapisao nešto")
	}
}

func TestParsirajPostaje(t *testing.T) {
	m, err := ParsirajPostaje(" 42010=bezdan, 42015 = apatin ,")
	if err != nil || m["42010"] != "bezdan" || m["42015"] != "apatin" || len(m) != 2 {
		t.Fatalf("%v %v", m, err)
	}
	for _, los := range []string{"", "42010", "=bezdan", "42010="} {
		if _, err := ParsirajPostaje(los); err == nil {
			t.Errorf("%q prošlo", los)
		}
	}
	f, _ := Nadji("godisnjak")
	l, err := f.LetveZadatka(Zadatak{Postaje: "2=b,1=a"})
	if err != nil || strings.Join(l, ",") != "a,b" {
		t.Errorf("letve godišnjaka: %v %v", l, err)
	}
}
