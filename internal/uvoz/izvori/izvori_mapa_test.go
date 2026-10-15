package izvori

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gocop/internal/arhiva"
)

// hisDalj je najmanji izvoz HIS-2000 s dnevnim vodostajima
var hisDalj = []Datoteka{{Ime: "vodostaj.csv",
	Sadrzaj: []byte("Dnevni podaci postaje DALJ - DUNAV,  VODOSTAJ  (cm)\r\n01.01.2001;176;\r\n")}}

// izMapeBaze pokrene test iz mape data unutar mape projekta, kao Postava, s
// mapom vodostaji u mapi projekta kad je stablo iznad; vraća mapu projekta
func izMapeBaze(t *testing.T, stabloIznad bool) string {
	t.Helper()
	projekt := t.TempDir()
	mape := []string{"data"}
	if stabloIznad {
		mape = append(mape, "vodostaji")
	}
	for _, m := range mape {
		if err := os.MkdirAll(filepath.Join(projekt, m), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(filepath.Join(projekt, "data"))
	return projekt
}

// HIS-2000 i godišnjak u stablo pišu sami, mimo upisa niza. Kad je relativno
// stablo drugdje, oba se odbiju već na pregledu, kao i svaki drugi izvor, i u
// mapi baze ne nastane ništa.
func TestUvozIzvoraUStabloKojeJeDrugdje(t *testing.T) {
	izMapeBaze(t, true)
	for _, s := range []struct {
		kod string
		dat []Datoteka
		z   Zadatak
	}{
		{"his2000", hisDalj, Zadatak{Koren: "vodostaji", Sliv: "dunav", Letva: "dalj"}},
		{"godisnjak", []Datoteka{{Ime: "1991.pdf", Sadrzaj: []byte("%PDF")}},
			Zadatak{Koren: "vodostaji", Postaje: "42010=bezdan"}},
		{"arso", []Datoteka{{Ime: "a.csv", Sadrzaj: []byte(arsoGlava + "01.01.2024;123;;\n")}},
			Zadatak{Koren: "vodostaji", Sliv: "drava", Letva: "ptuj"}},
	} {
		for _, probno := range []bool{true, false} {
			s.z.Probno = probno
			_, _, err := uvezi(t, s.kod, s.dat, s.z)
			var nm *arhiva.NemaMape
			if !errors.As(err, &nm) || nm.Kljuc != "podaci" {
				t.Errorf("%s, probno %v: %v", s.kod, probno, err)
			}
		}
	}
	if _, err := os.Stat("vodostaji"); !os.IsNotExist(err) {
		t.Errorf("stablo je nastalo u mapi baze: %v", err)
	}
}

// Novi čvor: relativnog stabla još nema ni iznad, pa ga uvoz iz HIS-2000
// počne u mapi baze, kao i dosad.
func TestUvozHISNaNovomCvoruPocinjeStablo(t *testing.T) {
	projekt := izMapeBaze(t, false)
	if _, _, err := uvezi(t, "his2000", hisDalj, Zadatak{Koren: "vodostaji", Sliv: "dunav", Letva: "dalj"}); err != nil {
		t.Fatal(err)
	}
	if d := datoteke(t, filepath.Join(projekt, "data", "vodostaji")); len(d) != 1 {
		t.Errorf("zapisano %v", d)
	}
}
