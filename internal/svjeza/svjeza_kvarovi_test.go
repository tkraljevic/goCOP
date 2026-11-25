package svjeza

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/javnivodostaji"

	_ "modernc.org/sqlite"
)

// Brojevi u ovim provjerama su izmišljeni.

func praznaBaza(t *testing.T, upiti ...string) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	for _, q := range upiti {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	return d
}

// Kvar baze, stabla ili tablice prekida ulaganje greškom, a ne tiho.
func TestUlaganjeJaviKvar(t *testing.T) {
	sada := zg(2026, 11, 25, 1)
	zatvorena := praznaBaza(t)
	_ = zatvorena.Close() // baza je namjerno zatvorena prije ulaganja
	if _, err := (&Ulagac{Baza: zatvorena}).Ulozi(sada, true, true); err == nil {
		t.Error("zatvorena baza bez greške")
	}
	if _, err := NaRedu(zatvorena, sada); err == nil {
		t.Error("na redu iz zatvorene baze bez greške")
	}
	if err := Izdano(zatvorena, []string{"a"}, 1, sada); err == nil {
		t.Error("izdano u zatvorenu bazu bez greške")
	}
	if err := Izdano(zatvorena, nil, 0, sada); err != nil {
		t.Error("prazno izdavanje ne dira bazu")
	}

	u, koren := cvor(t)
	datoteka := filepath.Join(t.TempDir(), "datoteka")
	if err := os.WriteFile(datoteka, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Ulagac{Baza: u.Baza, Koren: datoteka}).Ulozi(sada, true, false); err == nil {
		t.Error("stablo koje je datoteka bez greške")
	}

	// zatečeni niz u stablu koji se ne da pročitati: dopuna odbije upis
	dani(t, u, "proba", javnivodostaji.Podrijetlo, zg(2026, 11, 20, 0), 100)
	pokvaren := filepath.Join(koren, "dunav", "proba", "proba_hv-uzivo_vodostaj_satni_2026.csv")
	if err := os.WriteFile(pokvaren, []byte("vrijeme_utc;vodostaj_cm\nnije-datum;1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Ulozi(sada, true, false); err == nil || !strings.Contains(err.Error(), "proba") {
		t.Errorf("pokvaren niz: %v", err)
	}

	// točka: pokvaren zatečeni niz isto tako
	if _, err := u.Oborine.Exec(`INSERT INTO satne (kisomjer, sat, oborina, snijeg, temperatura, prognoza, preuzeto)
		VALUES ('k-proba-1', ?, 1, 0, 3, 0, 0)`, zg(2026, 11, 24, 10).Unix()/3600); err != nil {
		t.Fatal(err)
	}
	pokvarena := filepath.Join(koren, "drava", "k-proba-1", "k-proba-1_openmeteo_oborina_satni_2026.csv")
	if err := os.WriteFile(pokvarena, []byte("vrijeme_utc;oborina_mm\nnije-datum;1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Ulozi(sada, false, true); err == nil {
		t.Error("pokvaren niz točke bez greške")
	}
}

// Tablice koje nisu kakve ulaganje očekuje javljaju se greškom.
func TestUlaganjeTabliceSKvarom(t *testing.T) {
	sada := zg(2026, 11, 25, 1)
	koren := t.TempDir()
	if err := os.MkdirAll(filepath.Join(koren, "dunav", "proba"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(koren, "drava", "k-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	ocitanja := []string{
		`CREATE TABLE stations (id TEXT, code TEXT)`,
		`INSERT INTO stations VALUES ('s1', 'proba')`,
		`CREATE TABLE readings (station_id TEXT, source TEXT, origin TEXT, measured_at TEXT)`,
		`INSERT INTO readings VALUES ('s1', 'UVOZ', 'vodostaji.voda.hr', '2026-11-24')`,
	}
	// bez tablice očitanja
	if _, err := (&Ulagac{Baza: praznaBaza(t), Koren: koren}).Ulozi(sada, true, false); err == nil {
		t.Error("bez tablice očitanja bez greške")
	}
	// očitanja bez stupaca vrijednosti
	if _, err := (&Ulagac{Baza: praznaBaza(t, ocitanja...), Koren: koren}).Ulozi(sada, true, false); err == nil {
		t.Error("očitanja bez stupaca bez greške")
	}
	// evidencija ulaganja s drugim stupcima
	staraEvidencija := `CREATE TABLE svjeza_arhiva (kljuc TEXT)`
	if _, err := (&Ulagac{Baza: praznaBaza(t, append(ocitanja, staraEvidencija)...), Koren: koren}).Ulozi(sada, true, false); err == nil {
		t.Error("kriva evidencija bez greške")
	}
	// točke: bez tablice sati, sati bez stupaca, kriva evidencija
	bezSati := praznaBaza(t)
	if _, err := (&Ulagac{Baza: praznaBaza(t), Oborine: bezSati, Koren: koren}).Ulozi(sada, false, true); err == nil {
		t.Error("bez tablice sati bez greške")
	}
	satiBezStupaca := praznaBaza(t, `CREATE TABLE satne (kisomjer TEXT, sat INTEGER, prognoza INTEGER)`,
		`INSERT INTO satne VALUES ('k-1', 1, 0)`)
	if _, err := (&Ulagac{Baza: praznaBaza(t), Oborine: satiBezStupaca, Koren: koren}).Ulozi(sada, false, true); err == nil {
		t.Error("sati bez stupaca bez greške")
	}
	if _, err := (&Ulagac{Baza: praznaBaza(t, staraEvidencija), Oborine: satiBezStupaca, Koren: koren}).Ulozi(sada, false, true); err == nil {
		t.Error("kriva evidencija točke bez greške")
	}
	// točka bez mape u stablu se preskače
	tockaBezMape := praznaBaza(t, `CREATE TABLE satne (kisomjer TEXT, sat INTEGER, prognoza INTEGER)`,
		`INSERT INTO satne VALUES ('k-2', 1, 0)`)
	iz, err := (&Ulagac{Baza: praznaBaza(t), Oborine: tockaBezMape, Koren: koren}).Ulozi(sada, false, true)
	if err != nil || len(iz.BezMape) != 1 {
		t.Errorf("točka bez mape: %+v %v", iz, err)
	}
	// izdavanje u evidenciju bez tablica
	if err := Izdano(praznaBaza(t), []string{"a"}, 1, sada); err == nil {
		t.Error("izdano bez evidencije bez greške")
	}
	if err := Izdano(praznaBaza(t, `CREATE TABLE svjeza_arhiva (kljuc TEXT, izdano INTEGER, ceka INTEGER, velika INTEGER)`),
		[]string{"a"}, 1, sada); err == nil {
		t.Error("izdano bez sažetka bez greške")
	}
	// sažetak bez tablice je prazan, a ne greška
	if s, err := ProcitajStanje(praznaBaza(t)); err != nil || !s.Ulozeno.IsZero() {
		t.Errorf("stanje bez tablice: %+v %v", s, err)
	}
}

// Letva kojoj je sve već uloženo ručno nema što uložiti i ne bilježi se.
func TestLetvaBezNovogaSePreskace(t *testing.T) {
	u, _ := cvor(t)
	upisi(t, u, "proba", javnivodostaji.Podrijetlo, zg(2026, 11, 24, 10), 210, nil, nil, "")
	if _, err := u.Baza.Exec(`UPDATE readings SET izdanje = '2026-11-24'`); err != nil {
		t.Fatal(err)
	}
	iz, err := u.Ulozi(zg(2026, 11, 25, 1), true, false)
	if err != nil || len(iz.Letve) != 0 {
		t.Errorf("uloženo %v %v", iz.Letve, err)
	}
}
