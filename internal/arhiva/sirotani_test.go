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

// Micanje niza odnosi ono što je od njega ušlo u spojeni niz — ali ne smije
// odnijeti ono što ondje stoji od sestrinskog niza istog izvora.
//
// Prva izvedba je brisala po letvi, izvoru i veličini. Micanje suvišnog
// "cop-rucno · jutarnji" na Batini tako je iz spoja odnijelo i svih deset
// ručnih očitanja, među njima i zabilježeni minimum, iako im je niz ostao
// netaknut.
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

	// Sestrinski niz istog izvora ima i svoju vrijednost, drugog dana.
	drugiDan := kad.AddDate(0, 0, -1)
	if _, err := Upisi(koren, "dunav", "batina", "cop-rucno", "vodostaj", "satni",
		[]Redak{{Vrijeme: kad, Vrijednost: -160}, {Vrijeme: drugiDan, Vrijednost: -159}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Izgradi(koren, baza, "batina", nil); err != nil {
		t.Fatal(err)
	}

	obrisano, err := MakniNiz(db, baza, "batina", "cop-rucno", "vodostaj", "jutarnji")
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
	// Ono što dolazi od sestrinskog niza mora ostati u spoju.
	var uSpoju int
	if err := db.QueryRow(`SELECT count(*) FROM spoj WHERE letva='batina' AND izvor='cop-rucno'`).
		Scan(&uSpoju); err != nil {
		t.Fatal(err)
	}
	if uSpoju == 0 {
		t.Error("micanje jednog niza odnijelo je iz spoja i ono što dolazi od sestrinskog")
	}
	var najnize float64
	if err := db.QueryRow(`SELECT min(vrijednost) FROM spoj WHERE letva='batina'
		AND izvor='cop-rucno'`).Scan(&najnize); err != nil {
		t.Fatal(err)
	}
	if najnize != -160 {
		t.Errorf("u spoju je najniže %.0f, a ručno očitanje kaže -160", najnize)
	}

	// Nepostojeći niz se javlja greškom, ne tiho.
	if _, err := MakniNiz(db, baza, "batina", "cop-rucno", "vodostaj", "jutarnji"); err == nil {
		t.Error("micanje nepostojećeg niza je prošlo bez greške")
	}
}

// Sirotani se moraju vidjeti i kad se ništa ne gradi: datoteka se može maknuti
// i izravno iz stabla, a stranica se otvara češće nego što se gradi.
func TestSirotaniBezGradnje(t *testing.T) {
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

	// Dok su obje datoteke tu, sirotana nema.
	o, err := Sirotani(db, koren)
	if err != nil {
		t.Fatal(err)
	}
	if len(o) != 0 {
		t.Fatalf("javljeno %d sirotana iako su sve datoteke na mjestu: %+v", len(o), o)
	}

	stare, _ := filepath.Glob(filepath.Join(koren, "dunav", "batina",
		"batina_cop-rucno_vodostaj_jutarnji_*.csv"))
	if err := os.Remove(stare[0]); err != nil {
		t.Fatal(err)
	}

	// Bez ijedne gradnje, samo pitanjem.
	o, err = Sirotani(db, koren)
	if err != nil {
		t.Fatal(err)
	}
	if len(o) != 1 || o[0].Vrsta != "jutarnji" {
		t.Fatalf("bez gradnje javljeno %+v", o)
	}
	if o[0].USpoju == 0 {
		t.Error("ne javlja koliko je sirotanovih vrijednosti u spojenom nizu")
	}
}

// Rekonstrukcija izvan mjerenog odnosa ne smije nadjačati onu unutar njega,
// bez obzira na to koja je prije upisana.
//
// To drži zadanaTocnost: nastavak "-izvan" nosi ±150 cm umjesto ±14, pa gubi
// svaki sudar. Na Batini su te dvije verzije istih dana razmaknute 137 cm —
// siječanj 1909. je -108 po odnosu unutar raspona, a -245 po onome izvan njega,
// što je ostatak od prije ispravka kote iz 1943. Da granica padne, spojeni niz
// bi za te dane pokazao vrijednost ispod zabilježenog minimuma Batine.
func TestRekonstrukcijaIzvanRasponaNeNadjacava(t *testing.T) {
	koren := t.TempDir()
	baza := filepath.Join(t.TempDir(), "arhiva.db")
	kad := time.Date(1909, 1, 7, 0, 0, 0, 0, time.UTC)

	// Onaj izvan raspona upisan je prvi, pa ima manji rowid.
	if _, err := Upisi(koren, "dunav", "batina", "preracun-mohacs-izvan", "vodostaj", "srednjak",
		[]Redak{{Vrijeme: kad, Vrijednost: -245, PoDanu: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Izgradi(koren, baza, "batina", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Upisi(koren, "dunav", "batina", "preracun-mohacs", "vodostaj", "srednjak",
		[]Redak{{Vrijeme: kad, Vrijednost: -108, PoDanu: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Izgradi(koren, baza, "batina", nil); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", baza+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var raniji, kasniji int64
	_ = db.QueryRow(`SELECT id FROM nizovi WHERE izvor='preracun-mohacs-izvan'`).Scan(&raniji)
	_ = db.QueryRow(`SELECT id FROM nizovi WHERE izvor='preracun-mohacs'`).Scan(&kasniji)
	if raniji >= kasniji {
		t.Fatalf("test ne postavlja zamku: id %d i %d", raniji, kasniji)
	}

	var v float64
	var izvor string
	if err := db.QueryRow(`SELECT vrijednost, izvor FROM spoj WHERE letva='batina'
		AND velicina='vodostaj' AND korak='dnevni'`).Scan(&v, &izvor); err != nil {
		t.Fatal(err)
	}
	if v != -108 || izvor != "preracun-mohacs" {
		t.Errorf("u spoju stoji %.0f iz %s, a mora stajati -108 iz preracun-mohacs", v, izvor)
	}
}
