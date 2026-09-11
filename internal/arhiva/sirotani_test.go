package arhiva

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Niz čija je datoteka nestala iz stabla ostaje u arhivi i dalje sudjeluje u
// spoju. Na Batini je tako jedno ručno očitanje, uloženo prije ispravka kao
// „jutarnji“, donosilo rekordni minimum kao dnevnu vrijednost u ponoć — uz
// ispravno satno očitanje istoga dana.
//
// Gradnja ga ne smije obrisati sama: izvor sa svojom mapom na vanjskom disku
// izgleda isto tako kad disk nije priključen. Mora ga javiti.
func TestGradnjaJavljaNizBezDatoteke(t *testing.T) {
	koren := t.TempDir()
	baza := filepath.Join(t.TempDir(), "arhiva.db")

	zapisi := func(vrsta string, kad time.Time, v float64) {
		redci := []Redak{{Vrijeme: kad, Vrijednost: v}}
		if _, err := Upisi(koren, "dunav", "batina", "cop-rucno", "vodostaj", vrsta, redci); err != nil {
			t.Fatal(err)
		}
	}
	kad := time.Date(2026, 9, 11, 7, 0, 0, 0, time.UTC)
	zapisi("satni", kad, -160)
	zapisi("jutarnji", kad, -160)

	if _, err := Izgradi(koren, baza, "batina", nil); err != nil {
		t.Fatal(err)
	}

	// Datoteka „jutarnji“ nestane iz stabla — kao kad se ulaganje ispravi.
	stare, _ := filepath.Glob(filepath.Join(koren, "dunav", "batina",
		"batina_cop-rucno_vodostaj_jutarnji_*.csv"))
	if len(stare) != 1 {
		t.Fatalf("očekivana jedna datoteka, nađeno %d", len(stare))
	}
	if err := os.Remove(stare[0]); err != nil {
		t.Fatal(err)
	}

	iz, err := Izgradi(koren, baza, "batina", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(iz.Sirotani) != 1 {
		t.Fatalf("javljeno %d sirotana, a datoteka jednog niza je nestala: %+v", len(iz.Sirotani), iz.Sirotani)
	}
	o := iz.Sirotani[0]
	if o.Letva != "batina" || o.Izvor != "cop-rucno" || o.Vrsta != "jutarnji" {
		t.Errorf("javljen krivi sirotan: %+v", o)
	}
	if o.Zapisa != 1 {
		t.Errorf("sirotan ima %d zapisa", o.Zapisa)
	}

	// Niz koji ima datoteku ne smije biti proglašen sirotanom.
	for _, s := range iz.Sirotani {
		if s.Vrsta == "satni" {
			t.Error("niz koji ima datoteku javljen kao sirotan")
		}
	}
}

// Micanje niza odnosi i ono što je od njega ušlo u spojeni niz.
func TestMicanjeNizaCistiISpoj(t *testing.T) {
	koren := t.TempDir()
	baza := filepath.Join(t.TempDir(), "arhiva.db")
	kad := time.Date(2026, 9, 11, 7, 0, 0, 0, time.UTC)
	for _, vrsta := range []string{"satni", "jutarnji"} {
		if _, err := Upisi(koren, "dunav", "batina", "cop-rucno", "vodostaj", vrsta,
			[]Redak{{Vrijeme: kad, Vrijednost: -160}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Izgradi(koren, baza, "batina", nil); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", baza)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	obrisano, err := MakniNiz(db, "batina", "cop-rucno", "vodostaj", "jutarnji")
	if err != nil {
		t.Fatal(err)
	}
	if obrisano != 1 {
		t.Errorf("obrisano %d očitanja", obrisano)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM nizovi WHERE letva='batina' AND izvor='cop-rucno'
		AND vrsta='jutarnji'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("niz je ostao u arhivi")
	}
	// Nepostojeći niz se javlja greškom, ne tiho.
	if _, err := MakniNiz(db, "batina", "cop-rucno", "vodostaj", "jutarnji"); err == nil {
		t.Error("micanje nepostojećeg niza je prošlo bez greške")
	}
}
