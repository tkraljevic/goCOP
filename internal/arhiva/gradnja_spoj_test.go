package arhiva

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// upisiSatniNiz upisuje izmišljeni satni niz s vrijednostima od zadanog
// trenutka, sat po sat.
func upisiSatniNiz(t *testing.T, db *sql.DB, letva, izvor, velicina string, od time.Time, v ...float64) {
	t.Helper()
	res, err := db.Exec(`INSERT INTO nizovi (zona, sliv, letva, izvor, velicina, vrsta, zapisa)
		VALUES ('UTC','drava',?,?,?,'satni',?)`, letva, izvor, velicina, len(v))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	for i, x := range v {
		if _, err := db.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,?,?)`,
			id, od.Add(time.Duration(i)*time.Hour).Unix(), x); err != nil {
			t.Fatal(err)
		}
	}
}

func ponovi(v float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// dnevnaVrijednost čita dnevnu vrijednost spojenog niza; ok je false kad je nema.
func dnevnaVrijednost(t *testing.T, db *sql.DB, letva, velicina string, dan time.Time) (v float64, vrsta string, ok bool) {
	t.Helper()
	err := db.QueryRow(`SELECT vrijednost, vrsta FROM spoj WHERE letva=? AND velicina=? AND korak='dnevni' AND vrijeme=?`,
		letva, velicina, dan.Unix()).Scan(&v, &vrsta)
	if err == sql.ErrNoRows {
		return 0, "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return v, vrsta, true
}

// Zaključava dnevnu vrijednost iz satnog niza: oborina se po danu zbraja i
// traži svih 24 sata, jer bi zbroj nepotpunog dana bio premalen, a vodostaj
// je srednjak i zadovoljava se s 20 sati.
func TestSpojDnevnuOborinuZbrajaAVodostajUsrednjava(t *testing.T) {
	db := praznaArhiva(t)
	dan1 := time.Date(2024, 9, 14, 0, 0, 0, 0, time.UTC)
	dan2 := dan1.AddDate(0, 0, 1)
	// prvi dan pun, drugi bez jednog sata
	upisiSatniNiz(t, db, "osijek", "cop", "oborina", dan1, ponovi(0.5, 24)...)
	upisiSatniNiz(t, db, "osijek-ks", "cop", "oborina", dan2, ponovi(2, 23)...)
	upisiSatniNiz(t, db, "osijek", "cop", "vodostaj", dan1, append(ponovi(200, 10), ponovi(230, 10)...)...)

	n, err := Spoji(db, "osijek")
	if err != nil {
		t.Fatal(err)
	}
	if n != 24+20+2 {
		t.Errorf("spojeno %d vrijednosti, očekivano %d", n, 24+20+2)
	}
	if v, vrsta, ok := dnevnaVrijednost(t, db, "osijek", "oborina", dan1); !ok || v != 12 || vrsta != "zbroj" {
		t.Errorf("oborina punog dana: %v %q (ima %v), očekivano 12 mm kao zbroj", v, vrsta, ok)
	}
	if v, vrsta, ok := dnevnaVrijednost(t, db, "osijek", "vodostaj", dan1); !ok || v != 215 || vrsta != "srednjak" {
		t.Errorf("vodostaj od 20 sati: %v %q (ima %v), očekivano srednjak 215", v, vrsta, ok)
	}

	if _, err := Spoji(db, "osijek-ks"); err != nil {
		t.Fatal(err)
	}
	if v, _, ok := dnevnaVrijednost(t, db, "osijek-ks", "oborina", dan2); ok {
		t.Errorf("dan s 23 sata oborine dobio je zbroj %v", v)
	}
}

// Zaključava da spajanje koje pukne usred upisa ne ostavlja pola spoja:
// stari spojeni niz ostaje kakav je bio, a greška kaže koja letva i veličina.
func TestSpajanjeKojePukneCuvaStariSpoj(t *testing.T) {
	db := praznaArhiva(t)
	dan := time.Date(2024, 9, 14, 0, 0, 0, 0, time.UTC)
	upisiSatniNiz(t, db, "osijek", "cop", "vodostaj", dan, 200, 210, 220)
	if _, err := Spoji(db, "osijek"); err != nil {
		t.Fatal(err)
	}
	var prije int
	if err := db.QueryRow(`SELECT count(*) FROM spoj WHERE letva='osijek'`).Scan(&prije); err != nil || prije != 3 {
		t.Fatalf("prvi spoj: %d vrijednosti, %v", prije, err)
	}

	// novo očitanje koje upis u spoj odbija
	if _, err := db.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost)
		SELECT id, ?, 9999 FROM nizovi WHERE letva='osijek'`, dan.Add(3*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER odbij BEFORE INSERT ON spoj WHEN NEW.vrijednost > 5000
		BEGIN SELECT RAISE(ABORT, 'odbijeno'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := Spoji(db, "osijek")
	if err == nil || !strings.Contains(err.Error(), "osijek/vodostaj") {
		t.Fatalf("puklo spajanje: %v", err)
	}
	var poslije int
	if err := db.QueryRow(`SELECT count(*) FROM spoj WHERE letva='osijek'`).Scan(&poslije); err != nil {
		t.Fatal(err)
	}
	if poslije != prije {
		t.Errorf("nakon puknutog spajanja u spoju je %d vrijednosti, prije %d", poslije, prije)
	}
}

// Zaključava da se bez popisa izvora ne spaja: red povjerenja ne smije se
// izmisliti, pa spajanje stane s greškom.
func TestSpajanjeBezPopisaIzvora(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bez-izvora.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := Spoji(db, "osijek"); err == nil || !strings.Contains(err.Error(), "čitanje izvora") {
		t.Errorf("spajanje bez izvora: %v", err)
	}
}

// biljeznik pamti korake koje mu gradnja javi.
type biljeznik struct {
	io.Writer
	koraci []string
}

func (b *biljeznik) Korak(sto string, gotovo, ukupno int) {
	b.koraci = append(b.koraci, sto)
}

// Zaključava javljanje napretka: korak stiže odredištu koje ga zna primiti,
// a obično odredište zapisa javljanje ne dobiva i ne smeta mu.
func TestJavljanjeKorakaSamoKadOdredisteZna(t *testing.T) {
	b := &biljeznik{Writer: io.Discard}
	javi(b, "spajam izvore u jedan niz", 3, 3)
	if len(b.koraci) != 1 || b.koraci[0] != "spajam izvore u jedan niz" {
		t.Errorf("javljeni koraci: %q", b.koraci)
	}
	var obicno strings.Builder
	javi(&obicno, "popisujem datoteke", 0, 0)
	if obicno.Len() != 0 {
		t.Errorf("obično odredište dobilo je %q", obicno.String())
	}
}

// Zaključava čitanje promjena kote nule iz stabla: izvor i napomena nisu
// obavezni stupci i bez njih se upisuju prazni, a stablo bez datoteke
// promjena nije greška.
func TestPromjeneKoteBezIzvoraINapomene(t *testing.T) {
	db := praznaArhiva(t)
	koren := t.TempDir()
	if p, err := promjeneKote(db, koren, io.Discard); err != nil || p != nil {
		t.Fatalf("bez datoteke promjena: %v, %v", p, err)
	}
	csv := "letva;datum;pomak_cm;izvor;napomena\n" +
		"batina;1943-01-01;200;elaborat;nula spuštena\n" +
		"vukovar;1950-01-01;30\n" +
		"kratko;1960-01-01\n"
	if err := os.WriteFile(filepath.Join(koren, "promjene-kote.csv"), []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := promjeneKote(db, koren, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 2 || len(p["batina"]) != 1 || p["batina"][0].pomak != 200 || len(p["vukovar"]) != 1 {
		t.Errorf("promjene po letvi: %+v", p)
	}
	for _, s := range []struct{ letva, izvor, napomena string }{
		{"batina", "elaborat", "nula spuštena"},
		{"vukovar", "", ""},
	} {
		var izvor, napomena string
		if err := db.QueryRow(`SELECT izvor, napomena FROM promjene_kote WHERE letva=?`, s.letva).
			Scan(&izvor, &napomena); err != nil {
			t.Fatalf("%s: %v", s.letva, err)
		}
		if izvor != s.izvor || napomena != s.napomena {
			t.Errorf("%s: izvor %q napomena %q, očekivano %q %q", s.letva, izvor, napomena, s.izvor, s.napomena)
		}
	}
}
