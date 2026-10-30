package arhiva

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Sliv je jedna obična mapa ispod korijena stabla.
func TestProvjeriSliv(t *testing.T) {
	for _, sliv := range []string{"dunav", "drava", "mali-sliv", "jadran-jug", "đakovačko.područje", "sliv2"} {
		if err := ProvjeriSliv(sliv); err != nil {
			t.Errorf("%q: %v", sliv, err)
		}
	}
	for _, sliv := range []string{"", " ", ".", "..", "../izvan", "a/b", `a\b`, "do_nja", "a b",
		"*", "a?", "[a]", ".skriveno", "kraj.", "PRISTUP", "PRISTUP-dunav", "a\nb", "a\tb", "a:b", "a|b", `a"b`, "a<b"} {
		if err := ProvjeriSliv(sliv); err == nil {
			t.Errorf("%q je prošao", sliv)
		}
	}
}

// Mapa letve je <koren>/<sliv>/<letva>; sliv ili letva koji bi izašli iz
// stabla ili zašli u drugu mapu odbijaju se.
func TestMapaLetve(t *testing.T) {
	koren := t.TempDir()
	mapa, err := MapaLetve(koren, "dunav", "batina")
	if err != nil || mapa != filepath.Join(koren, "dunav", "batina") {
		t.Errorf("ispravna mapa: %q, %v", mapa, err)
	}
	for _, s := range [][2]string{{"..", "batina"}, {"dunav", ".."}, {"dunav", "."}, {"dunav", "../batina"}, {"dunav", ""}} {
		if m, err := MapaLetve(koren, s[0], s[1]); err == nil {
			t.Errorf("sliv %q, letva %q: prošlo kao %s", s[0], s[1], m)
		}
	}
}

// stabloUMapi slaže korijen stabla dvije razine ispod privremene mape i u
// mapu iznad korijena stavlja niz letve, kakav bi sliv „..” čitao.
func stabloUMapi(t *testing.T) (baza, koren string) {
	t.Helper()
	baza = t.TempDir()
	koren = filepath.Join(baza, "a", "vodostaji")
	iznad := filepath.Join(baza, "a", "batina")
	if err := os.MkdirAll(koren, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(iznad, 0o755); err != nil {
		t.Fatal(err)
	}
	stari := "vrijeme_utc;vodostaj_cm\n2001-01-01 00:00:00;100\n"
	if err := os.WriteFile(filepath.Join(iznad, "batina_test_vodostaj_satni_2001.csv"), []byte(stari), 0o644); err != nil {
		t.Fatal(err)
	}
	return baza, koren
}

func popisDatoteka(t *testing.T, baza string) []string {
	t.Helper()
	var out []string
	if err := filepath.WalkDir(baza, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			r, _ := filepath.Rel(baza, p) // put je ispod baze, iz WalkDir
			out = append(out, r)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// Upisi i Dopuni odbijaju sliv koji nije mapa u stablu, a datoteku iznad
// stabla ne čitaju ni ne diraju. Isti niz u ispravan sliv upisuje se kao i
// prije.
func TestUpisiIDopuniOstajuUStablu(t *testing.T) {
	redci := []Redak{{Vrijeme: time.Date(2001, 1, 1, 1, 0, 0, 0, time.UTC), Vrijednost: 120}}
	for ime, upis := range map[string]func(koren, sliv, letva, izvor, velicina, vrsta string, redci []Redak) (string, error){
		"Upisi": Upisi, "Dopuni": Dopuni,
	} {
		t.Run(ime, func(t *testing.T) {
			baza, koren := stabloUMapi(t)
			for _, sliv := range []string{"..", ".", "../a"} {
				if put, err := upis(koren, sliv, "batina", "test", "vodostaj", "satni", redci); err == nil ||
					!strings.Contains(err.Error(), "nije upotrebljiv kao naziv mape") {
					t.Errorf("sliv %q: %s, %v", sliv, put, err)
				}
			}
			if d := popisDatoteka(t, baza); len(d) != 1 || d[0] != filepath.Join("a", "batina", "batina_test_vodostaj_satni_2001.csv") {
				t.Errorf("datoteke nakon odbijenog upisa: %v", d)
			}
			put, err := upis(koren, "dunav", "batina", "test", "vodostaj", "satni", redci)
			if err != nil || put != filepath.Join(koren, "dunav", "batina", "batina_test_vodostaj_satni_2001.csv") {
				t.Errorf("ispravan sliv: %s, %v", put, err)
			}
		})
	}
}

// PostojeciRedci za sliv izvan stabla ne čita ništa; za ispravan sliv čita niz.
func TestPostojeciRedciSamoUStablu(t *testing.T) {
	_, koren := stabloUMapi(t)
	if r, err := PostojeciRedci(koren, "..", "batina", "test", "vodostaj", "satni"); err != nil || len(r) != 0 {
		t.Errorf("sliv ..: %v, %v", r, err)
	}
	if _, err := Upisi(koren, "dunav", "batina", "test", "vodostaj", "satni",
		[]Redak{{Vrijeme: time.Date(2001, 1, 1, 1, 0, 0, 0, time.UTC), Vrijednost: 120}}); err != nil {
		t.Fatal(err)
	}
	if r, err := PostojeciRedci(koren, "dunav", "batina", "test", "vodostaj", "satni"); err != nil || len(r) != 1 {
		t.Errorf("ispravan sliv: %v, %v", r, err)
	}
}

// Izvor, letva ili sliv sa znakovima traženja odbijaju se prije upisa, pa
// nizovi drugih izvora iste letve ostaju. Ispravan izvor i dalje zamjenjuje
// samo svoj stari niz pod drugim razdobljem.
func TestUpisiOdbijaZnakoveTrazenja(t *testing.T) {
	koren := t.TempDir()
	mapa := filepath.Join(koren, "drava", "x")
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		t.Fatal(err)
	}
	niz := "vrijeme_utc;vodostaj_cm\n2020-01-01 00:00:00;100\n"
	for _, ime := range []string{"x_hv_vodostaj_satni_2020.csv", "x_cop_vodostaj_satni_2019.csv"} {
		if err := os.WriteFile(filepath.Join(mapa, ime), []byte(niz), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	redci := []Redak{{Vrijeme: time.Date(2021, 1, 1, 1, 0, 0, 0, time.UTC), Vrijednost: 120}}
	for ime, upis := range map[string]func(koren, sliv, letva, izvor, velicina, vrsta string, redci []Redak) (string, error){
		"Upisi": Upisi, "Dopuni": Dopuni,
	} {
		for _, s := range [][3]string{{"drava", "x", "*"}, {"drava", "x", "h?"}, {"drava", "x", "[hc]*"},
			{"*", "x", "cop"}, {"drava", "*", "cop"}} {
			if put, err := upis(koren, s[0], s[1], s[2], "vodostaj", "satni", redci); err == nil {
				t.Errorf("%s sliv %q, letva %q, izvor %q: prošlo kao %s", ime, s[0], s[1], s[2], put)
			}
		}
		if r, err := PostojeciRedci(koren, "drava", "x", "*", "vodostaj", "satni"); err != nil || len(r) != 0 {
			t.Errorf("%s: PostojeciRedci uz izvor *: %v, %v", ime, r, err)
		}
	}
	if d := popisDatoteka(t, koren); len(d) != 2 {
		t.Fatalf("nakon odbijenih upisa: %v", d)
	}
	if _, err := Upisi(koren, "drava", "x", "cop", "vodostaj", "satni", redci); err != nil {
		t.Fatal(err)
	}
	d := popisDatoteka(t, koren)
	if len(d) != 2 || d[0] != filepath.Join("drava", "x", "x_cop_vodostaj_satni_2021.csv") ||
		d[1] != filepath.Join("drava", "x", "x_hv_vodostaj_satni_2020.csv") {
		t.Errorf("nakon ispravnog upisa: %v", d)
	}
}

// Stablo kojeg još nema daje prazan popis letvi; mapa sliva koja se ne da
// pročitati je greška, jer se tada ne zna gdje letva stoji.
func TestLetveStabla(t *testing.T) {
	if l, err := LetveStabla(filepath.Join(t.TempDir(), "nema")); err != nil || len(l) != 0 {
		t.Errorf("stablo kojeg nema: %v, %v", l, err)
	}
	koren := t.TempDir()
	if err := os.MkdirAll(filepath.Join(koren, "dunav", "batina"), 0o755); err != nil {
		t.Fatal(err)
	}
	if l, err := LetveStabla(koren); err != nil || l["batina"] != "dunav" {
		t.Errorf("stablo: %v, %v", l, err)
	}
	if os.Geteuid() == 0 {
		return // administrator sustava čita mapu i bez prava
	}
	sliv := filepath.Join(koren, "dunav")
	if err := os.Chmod(sliv, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(sliv, 0o755) //nolint:errcheck // vraća prava da TempDir može počistiti
	if _, err := LetveStabla(koren); err == nil {
		t.Error("nečitljiva mapa sliva je prošla")
	}
}

// Mapa mora biti točno zadani dio ispod korijena.
func TestUStablu(t *testing.T) {
	koren := t.TempDir()
	if err := uStablu(koren, filepath.Join(koren, "dunav", "batina"), filepath.Join("dunav", "batina")); err != nil {
		t.Error(err)
	}
	if err := uStablu(koren, filepath.Join(filepath.Dir(koren), "batina"), filepath.Join("dunav", "batina")); err == nil {
		t.Error("mapa izvan korijena je prošla")
	}
}
