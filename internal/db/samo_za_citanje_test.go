package db

import (
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Zaključava da je veza otvorena za čitanje stvarno samo za čitanje, na
// običnom putu i na putu s razmakom, „#”, „%” i „?”, i uz dopisanu postavku.
// S „?mode=ro” iza običnog puta upis je prolazio, a baza koje nema nastajala
// je prazna.
func TestSamoZaCitanjeNePise(t *testing.T) {
	for _, ime := range []string{"podaci", "Moji podaci #1 %20?da"} {
		for _, dodatak := range []string{"", "&_pragma=busy_timeout(3000)"} {
			mapa := filepath.Join(t.TempDir(), ime)
			put := bazaUMapi(t, mapa)
			ro, err := sql.Open("sqlite", SamoZaCitanje(put)+dodatak)
			if err != nil {
				t.Fatal(err)
			}
			defer ro.Close()
			var letva string
			if err := ro.QueryRow(`SELECT letva FROM nizovi`).Scan(&letva); err != nil || letva != "vodograd" {
				t.Fatalf("%s%s: čitanje: %q, %v", ime, dodatak, letva, err)
			}
			if _, err := ro.Exec(`INSERT INTO nizovi VALUES ('drugovo')`); err == nil || !strings.Contains(err.Error(), "readonly") {
				t.Errorf("%s%s: upis kroz vezu za čitanje: %v", ime, dodatak, err)
			}
			if ima, _ := os.ReadDir(filepath.Dir(mapa)); len(ima) != 1 {
				t.Errorf("%s%s: uz mapu je nastalo još nešto: %v", ime, dodatak, ima)
			}

			nema := filepath.Join(mapa, "nema.db")
			prazna, err := sql.Open("sqlite", SamoZaCitanje(nema)+dodatak)
			if err != nil {
				t.Fatal(err)
			}
			defer prazna.Close()
			if err := prazna.Ping(); err == nil {
				t.Errorf("%s%s: baza koje nema otvorila se za čitanje", ime, dodatak)
			}
			if _, err := os.Stat(nema); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("%s%s: veza za čitanje napravila je bazu koje nije bilo: %v", ime, dodatak, err)
			}
		}
	}
}

// Zaključava razlog zbog kojeg SamoZaCitanje postoji: „?mode=ro” iza običnog
// puta driver odbaci, pa veza i piše. Kad ovaj test padne, driver je počeo
// poštovati „?mode=ro” i na običnom putu, pa komentar uz SamoZaCitanje treba
// ispraviti.
func TestModeRoNaObicnomPutuNeBraniUpis(t *testing.T) {
	put := bazaUMapi(t, filepath.Join(t.TempDir(), "podaci"))
	veza, err := sql.Open("sqlite", put+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer veza.Close()
	if _, err := veza.Exec(`INSERT INTO nizovi VALUES ('drugovo')`); err != nil {
		t.Errorf("upis kroz „?mode=ro” na običnom putu odbijen je (%v): komentar uz SamoZaCitanje više ne vrijedi", err)
	}
}

// bazaUMapi slaže malu bazu s jednim nizom u zadanoj mapi. Baza nastaje na
// običnom putu pa se premjesti, jer bi običan put s „?” otvorio drugu
// datoteku.
func bazaUMapi(t *testing.T, mapa string) string {
	t.Helper()
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		t.Fatal(err)
	}
	privremena := filepath.Join(t.TempDir(), "arhiva.db")
	baza, err := sql.Open("sqlite", privremena)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if _, err := baza.Exec(`CREATE TABLE nizovi (letva TEXT); INSERT INTO nizovi VALUES ('vodograd')`); err != nil {
		t.Fatal(err)
	}
	baza.Close()
	put := filepath.Join(mapa, "arhiva.db")
	if err := os.Rename(privremena, put); err != nil {
		t.Fatal(err)
	}
	return put
}

// Zaključava da program bazu za čitanje otvara samo preko SamoZaCitanje, u
// cmd/ i u internal/: „?mode=ro” dopisan običnom putu ne brani upis. Do
// 13. 10. 2026. tako su se otvarale arhiva i baza prognoza na poslužitelju,
// arhiva pri ulaganju i u izdavanju paketa.
func TestBazaZaCitanjeSamoPrekoPomocnika(t *testing.T) {
	pregledano := 0
	for _, mapa := range []string{filepath.Join("..", "..", "cmd"), filepath.Join("..", "..", "internal")} {
		err := filepath.WalkDir(mapa, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") ||
				filepath.Base(p) == "samo_za_citanje.go" {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			pregledano++
			if strings.Contains(string(b), `"?mode=ro`) {
				t.Errorf("%s otvara bazu s „?mode=ro” iza običnog puta; treba db.SamoZaCitanje", p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if pregledano < 100 {
		t.Errorf("pregledano je samo %d datoteka; test ne gleda kamo treba", pregledano)
	}
}
