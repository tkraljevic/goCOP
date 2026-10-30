package izvori

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/raspakiraj"
)

const dnevniDalj = "Dnevni podaci postaje DALJ - DUNAV,  VODOSTAJ  (cm)\r\n01.01.2001;176;\r\n02.01.2001;186;\r\n"

// sveIzvanKorena vraća datoteke koje stoje u baza, a ne pod koren.
func sveIzvanKorena(t *testing.T, baza, koren string) []string {
	t.Helper()
	var out []string
	for _, p := range datoteke(t, baza) {
		if !strings.HasPrefix(filepath.Join(baza, p), koren+string(filepath.Separator)) {
			out = append(out, p)
		}
	}
	return out
}

// Sliv koji nije obična mapa ispod korijena odbija se za svaki format, prije
// čitanja datoteka, i u probi i u upisu. Ništa ne nastaje ni u stablu ni oko
// njega.
func TestSlivMoraBitiMapaUStablu(t *testing.T) {
	for _, f := range Formati {
		for _, sliv := range []string{"..", ".", "../../izvan", "a/b", `a\b`} {
			t.Run(f.Kod+" "+sliv, func(t *testing.T) {
				baza := t.TempDir()
				koren := filepath.Join(baza, "a", "vodostaji")
				if err := os.MkdirAll(koren, 0o755); err != nil {
					t.Fatal(err)
				}
				z := Zadatak{Koren: koren, Sliv: sliv, Letva: "dalj", Velicina: "vodostaj", Postaje: "42010=dalj"}
				dat := []Datoteka{{Ime: "dalj" + f.Nastavci[0], Sadrzaj: []byte(dnevniDalj)}}
				_, _, err := uvezi(t, f.Kod, dat, z)
				if err == nil || !strings.Contains(err.Error(), "nije upotrebljiv kao naziv mape") {
					t.Errorf("sliv %q je prošao: %v", sliv, err)
				}
				if d := datoteke(t, baza); len(d) != 0 {
					t.Errorf("zapisano %v", d)
				}
			})
		}
	}
}

// HIS-2000 u ispravan sliv i dalje piše pod korijenom, a ništa izvan njega.
func TestHISPiseSamoUStablo(t *testing.T) {
	baza := t.TempDir()
	koren := filepath.Join(baza, "a", "vodostaji")
	if err := os.MkdirAll(koren, 0o755); err != nil {
		t.Fatal(err)
	}
	dat := []Datoteka{{Ime: "vodostaj.csv", Sadrzaj: []byte(dnevniDalj)}}
	if _, _, err := uvezi(t, "his2000", dat, Zadatak{Koren: koren, Sliv: "dunav", Letva: "dalj"}); err != nil {
		t.Fatal(err)
	}
	if izvan := sveIzvanKorena(t, baza, koren); len(izvan) != 0 {
		t.Errorf("izvan stabla: %v", izvan)
	}
	if d := datoteke(t, koren); len(d) != 1 || d[0] != filepath.Join("dunav", "dalj", "dalj_his2000_vodostaj_srednjak_2001-2001.csv") {
		t.Errorf("u stablu: %v", d)
	}
}

// uveziHIS sam provjerava mapu letve, i kad ga netko zove mimo Uvezi.
func TestUveziHISOdbijaMapuIzvanStabla(t *testing.T) {
	baza := t.TempDir()
	koren := filepath.Join(baza, "a", "vodostaji")
	if err := os.MkdirAll(koren, 0o755); err != nil {
		t.Fatal(err)
	}
	put := filepath.Join(t.TempDir(), "vodostaj.csv")
	if err := os.WriteFile(put, []byte(dnevniDalj), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := uveziHIS([]string{put}, Zadatak{Koren: koren, Sliv: "..", Letva: "dalj", Izvor: "his2000"}, nil); err == nil {
		t.Error("sliv .. je prošao")
	}
	if d := datoteke(t, baza); len(d) != 0 {
		t.Errorf("zapisano %v", d)
	}
}

// zipStavki slaže ZIP s n stavki zadanog sadržaja.
func zipStavki(t *testing.T, n int, sadrzaj string) []byte {
	t.Helper()
	m := map[string]string{}
	for i := 0; i < n; i++ {
		m[fmt.Sprintf("izvoz/%04d.csv", i)] = sadrzaj
	}
	return zipSa(t, m)
}

// ZIP smije imati ograničen broj stavki i ograničen zbroj raspakiranih
// bajtova. ZIP unutar granica čita se cijeli, bez mapa i skrivenih datoteka.
func TestIzZIPGranice(t *testing.T) {
	for _, s := range []struct {
		opis   string
		zip    []byte
		greska string
	}{
		{"previše stavki", zipStavki(t, 4, "x"), "jedan uvoz prima najviše 3"},
		{"zajedno preveliko", zipStavki(t, 3, strings.Repeat("x", 40)), "zajedno su veće od"},
		{"jedna stavka veća od granice", zipStavki(t, 1, strings.Repeat("x", 101)), "kad se raspakiraju"},
	} {
		t.Run(s.opis, func(t *testing.T) {
			if _, err := izZIP(s.zip, raspakiraj.Nova(3, 100)); err == nil || !strings.Contains(err.Error(), s.greska) {
				t.Errorf("greška %v, očekivano %q", err, s.greska)
			}
		})
	}

	dat, err := izZIP(zipSa(t, map[string]string{"izvoz/a.csv": strings.Repeat("x", 33), "izvoz/.skriveno": "x",
		"izvoz/b.csv": strings.Repeat("x", 33)}), raspakiraj.Nova(3, 100))
	if err != nil || len(dat) != 2 {
		t.Fatalf("ZIP unutar granica: %d datoteka, %v", len(dat), err)
	}
	for _, d := range dat {
		if len(d.Sadrzaj) != 33 {
			t.Errorf("%s: %d bajtova", d.Ime, len(d.Sadrzaj))
		}
	}
}

// granicaOd postavlja granicu uvoza HIS-2000 za jedan test.
func granicaOd(t *testing.T, stavki int, bajtova int64) {
	t.Helper()
	stara := granicaUvoza
	granicaUvoza = func() *raspakiraj.Granica { return raspakiraj.Nova(stavki, bajtova) }
	t.Cleanup(func() { granicaUvoza = stara })
}

// Granica vrijedi za cijeli uvoz: dva ZIP-a koja su svaki ispod granice, a
// zajedno preko nje, odbijaju se, i po bajtovima i po broju stavki. Isto
// vrijedi za ZIP i običnu datoteku zajedno. Jedan ZIP ispod granice prolazi.
func TestUvozHISGranicaZaSveZIPove(t *testing.T) {
	granicaOd(t, 1000, 1000)
	pola := zipSa(t, map[string]string{"izvoz/vodostaj.csv": dnevniDalj + strings.Repeat(";\r\n", 200)})
	for _, s := range []struct {
		opis   string
		dat    []Datoteka
		greska string
	}{
		{"dva ZIP-a preko bajtova", []Datoteka{{Ime: "a.zip", Sadrzaj: pola}, {Ime: "b.zip", Sadrzaj: pola}},
			"zajedno su veće od"},
		{"ZIP i datoteka preko bajtova", []Datoteka{{Ime: "a.zip", Sadrzaj: pola},
			{Ime: "vodostaj.csv", Sadrzaj: []byte(strings.Repeat("x", 400))}}, "zajedno su veće od"},
		{"dva ZIP-a preko broja stavki", []Datoteka{{Ime: "a.zip", Sadrzaj: zipStavki(t, 600, "")},
			{Ime: "b.zip", Sadrzaj: zipStavki(t, 600, "")}}, "najviše 1000"},
	} {
		t.Run(s.opis, func(t *testing.T) {
			koren := t.TempDir()
			_, _, err := uvezi(t, "his2000", s.dat, Zadatak{Koren: koren, Sliv: "dunav", Letva: "dalj"})
			if err == nil || !strings.Contains(err.Error(), s.greska) {
				t.Errorf("greška %v, očekivano %q", err, s.greska)
			}
			if d := datoteke(t, koren); len(d) != 0 {
				t.Errorf("zapisano %v", d)
			}
		})
	}

	koren := t.TempDir()
	if _, _, err := uvezi(t, "his2000", []Datoteka{{Ime: "a.zip", Sadrzaj: pola}},
		Zadatak{Koren: koren, Sliv: "dunav", Letva: "dalj"}); err != nil {
		t.Fatalf("jedan ZIP ispod granice: %v", err)
	}
	if d := datoteke(t, koren); len(d) != 1 {
		t.Errorf("jedan ZIP ispod granice: %v", d)
	}
}

// Uvoz HIS-2000 odbija ZIP s više stavki nego što zadana granica prima, a
// ništa ne zapiše.
func TestUvozHISZIPSPreviseStavki(t *testing.T) {
	koren := t.TempDir()
	dat := []Datoteka{{Ime: "dalj.zip", Sadrzaj: zipStavki(t, raspakiraj.ZadanoStavki+1, "")}}
	_, _, err := uvezi(t, "his2000", dat, Zadatak{Koren: koren, Sliv: "dunav", Letva: "dalj"})
	if err == nil || !strings.Contains(err.Error(), "jedan uvoz prima najviše 1000") {
		t.Errorf("ZIP s previše stavki: %v", err)
	}
	if d := datoteke(t, koren); len(d) != 0 {
		t.Errorf("zapisano %v", d)
	}
}

// Datoteka koje nema javlja se, i ništa se ne zapiše.
func TestUveziHISBezDatoteke(t *testing.T) {
	koren := t.TempDir()
	put := filepath.Join(t.TempDir(), "nema.csv")
	if _, err := uveziHIS([]string{put}, Zadatak{Koren: koren, Sliv: "dunav", Letva: "dalj", Izvor: "his2000"}, nil); err == nil {
		t.Error("datoteka koje nema je prošla")
	}
	if d := datoteke(t, koren); len(d) != 0 {
		t.Errorf("zapisano %v", d)
	}
}

// Godišnjak se ne uvozi kad se stablo ne da pročitati: ne zna se u kojem
// slivu letve već stoje.
func TestGodisnjakUzNecitljivoStablo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("administrator sustava čita mapu i bez prava")
	}
	koren := t.TempDir()
	sliv := filepath.Join(koren, "dunav")
	if err := os.MkdirAll(filepath.Join(sliv, "dalj"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sliv, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(sliv, 0o755) //nolint:errcheck // vraća prava da TempDir može počistiti
	if _, err := uveziGodisnjak(nil, Zadatak{Koren: koren, Sliv: "dunav", Postaje: "42010=dalj"}, nil); err == nil {
		t.Error("nečitljivo stablo je prošlo")
	}
}
