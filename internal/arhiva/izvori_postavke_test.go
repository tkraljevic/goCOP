package arhiva

import (
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Zaključava izmjenu izvora: upisuje se sve, a letve za ponovno spajanje
// javljaju se samo kad se promijeni nešto što mijenja brojeve, i to svaka
// letva jednom i po redu.
func TestPostavljanjeIzvoraJavljaLetveZaSpajanje(t *testing.T) {
	db := praznaArhiva(t)
	for _, n := range [][2]string{{"vodograd", "satni"}, {"rijecica", "satni"}, {"vodograd", "jutarnji"}} {
		upisiNiz(t, db, n[0], "letva-hv", "vodostaj", n[1], zapis{t: 0, v: 100})
	}
	upisiNiz(t, db, "kamenjak", "cop", "vodostaj", "satni", zapis{t: 0, v: 100})
	var prije Izvor
	for _, i := range mustIzvori(t, db) {
		if i.Naziv == "letva-hv" {
			prije = i
		}
	}

	// Napomena i mapa ne mijenjaju nijedan broj dok se arhiva ne izgradi.
	samoOpis := prije
	samoOpis.Napomena = "zamrzava se zimi"
	samoOpis.Mapa = "/mnt/vanjski/letva-hv"
	letve, err := PostaviIzvor(db, samoOpis)
	if err != nil || letve != nil {
		t.Errorf("izmjena napomene i mape: letve %v, %v", letve, err)
	}
	var napomena, mapa string
	if err := db.QueryRow(`SELECT napomena, mapa FROM izvori WHERE naziv='letva-hv'`).Scan(&napomena, &mapa); err != nil {
		t.Fatal(err)
	}
	if napomena != samoOpis.Napomena || mapa != samoOpis.Mapa {
		t.Errorf("upisano: napomena %q, mapa %q", napomena, mapa)
	}

	for _, s := range []struct {
		ime      string
		izmijeni func(*Izvor)
	}{
		{"točnost", func(i *Izvor) { i.Tocnost = 2 }},
		{"red", func(i *Izvor) { i.Red = 15 }},
		{"uključenost", func(i *Izvor) { i.Ukljucen = !i.Ukljucen }},
	} {
		novo := samoOpis
		s.izmijeni(&novo)
		letve, err := PostaviIzvor(db, novo)
		if err != nil {
			t.Fatalf("%s: %v", s.ime, err)
		}
		if want := []string{"rijecica", "vodograd"}; !slices.Equal(letve, want) {
			t.Errorf("%s: letve %v, očekivano %v", s.ime, letve, want)
		}
		samoOpis = novo
	}
	var tocnost float64
	var red int
	var ukljucen bool
	if err := db.QueryRow(`SELECT tocnost, red, ukljucen FROM izvori WHERE naziv='letva-hv'`).
		Scan(&tocnost, &red, &ukljucen); err != nil {
		t.Fatal(err)
	}
	if tocnost != 2 || red != 15 || ukljucen == prije.Ukljucen {
		t.Errorf("upisano: točnost %v, red %d, uključen %v", tocnost, red, ukljucen)
	}

	// Izvor bez ijednog niza ne traži spajanje nijedne letve.
	var arso Izvor
	for _, i := range mustIzvori(t, db) {
		if i.Naziv == "arso" {
			arso = i
		}
	}
	arso.Tocnost = 9
	if letve, err := PostaviIzvor(db, arso); err != nil || len(letve) != 0 {
		t.Errorf("izvor bez nizova: letve %v, %v", letve, err)
	}

	// Izvor kojeg nema ne upisuje se tiho.
	_, err = PostaviIzvor(db, Izvor{Naziv: "nepostojeci", Tocnost: 1})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("nepostojeći izvor: %v", err)
	}
	greskaSadrzi(t, "nepostojeći izvor", err, `izvor "nepostojeci": `)
}

func mustIzvori(t *testing.T, db *sql.DB) []Izvor {
	t.Helper()
	izvori, err := Izvori(db)
	if err != nil {
		t.Fatal(err)
	}
	return izvori
}

// Zaključava da se izmjena izvora koja ne uspije javlja greškom.
func TestPostavljanjeIzvoraKojeNeUspije(t *testing.T) {
	db := praznaArhiva(t)
	kvar(t, db, "UPDATE", "izvori", "")
	_, err := PostaviIzvor(db, Izvor{Naziv: "cop", Tocnost: 1, Red: 20, Ukljucen: true})
	greskaSadrzi(t, "upis", err, `upis izvora "cop": `)

	// Arhiva bez tablice nizova ne zna koje letve treba spojiti.
	krnja := func(t *testing.T, nizovi string) *sql.DB {
		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "krnja.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		if _, err := db.Exec(shemaIzvora + nizovi +
			`INSERT INTO izvori (naziv, tocnost, red, ukljucen) VALUES ('cop', 3, 20, 1);`); err != nil {
			t.Fatal(err)
		}
		return db
	}
	_, err = PostaviIzvor(krnja(t, ""), Izvor{Naziv: "cop", Tocnost: 1, Red: 20, Ukljucen: true})
	greskaSadrzi(t, "bez nizova", err, "no such table: nizovi")

	// Niz bez letve ne može se javiti kao letva.
	db2 := krnja(t, `CREATE TABLE nizovi (letva TEXT, izvor TEXT); INSERT INTO nizovi VALUES (NULL, 'cop');`)
	_, err = PostaviIzvor(db2, Izvor{Naziv: "cop", Tocnost: 1, Red: 20, Ukljucen: true})
	greskaSadrzi(t, "niz bez letve", err, "converting NULL to string")
}

// Zaključava sirotane uz izvore s vlastitom mapom: niz iz dostupne mape nije
// sirotan, izvor čija mapa nije priključena ne sudi se, a niz kojemu je
// datoteka nestala iz vlastite mape javlja se.
func TestSirotaniUzVlastiteMapeIzvora(t *testing.T) {
	koren := t.TempDir()
	vlastita := t.TempDir()
	baza := filepath.Join(t.TempDir(), "arhiva.db")
	db, err := sql.Open("sqlite", baza)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}
	zapisiDatoteku(t, filepath.Join(koren, "dunav", "vodograd", "vodograd_cop_vodostaj_jutarnji_2024.csv"),
		"datum;vodostaj_cm\n2024-03-01;120\n")
	uMapi := filepath.Join(vlastita, "dunav", "vodograd", "vodograd_vituki_vodostaj_satni_2024.csv")
	zapisiDatoteku(t, uMapi, "vrijeme_utc;vodostaj_cm\n2024-03-01 07:00:00;121\n")
	if _, err := db.Exec(`UPDATE izvori SET mapa=? WHERE naziv='vituki'`, vlastita); err != nil {
		t.Fatal(err)
	}
	uhvatiIspis(t, &os.Stdout, func() {
		if _, err := Izgradi(koren, baza, "vodograd", io.Discard); err != nil {
			t.Fatal(err)
		}
	})
	if n := broj(t, db, `SELECT count(*) FROM nizovi WHERE letva='vodograd'`); n != 2 {
		t.Fatalf("izgrađeno %d nizova, očekivana dva", n)
	}
	// Izvor kojemu mapa nije priključena: niz mu je u arhivi, datoteke nigdje.
	if _, err := db.Exec(`UPDATE izvori SET mapa=? WHERE naziv='arso'`, filepath.Join(vlastita, "nema")); err != nil {
		t.Fatal(err)
	}
	upisiNiz(t, db, "vodograd", "arso", "vodostaj", "srednjak", zapis{t: 0, v: 99})

	o, err := Sirotani(db, koren)
	if err != nil {
		t.Fatal(err)
	}
	if len(o) != 0 {
		t.Fatalf("javljeni sirotani iako su datoteke na mjestu ili mapa nije priključena: %+v", o)
	}

	if err := os.Remove(uMapi); err != nil {
		t.Fatal(err)
	}
	o, err = Sirotani(db, koren)
	if err != nil {
		t.Fatal(err)
	}
	if len(o) != 1 || o[0].Izvor != "vituki" || o[0].Vrsta != "satni" || o[0].Zapisa != 1 {
		t.Errorf("nakon micanja datoteke iz vlastite mape: %+v", o)
	}
}

// Zaključava da se sirotani ne javljaju kad se stablo ili popis izvora ne
// daju pročitati: prazan popis bi značio da ih nema.
func TestSirotaniKadSeNestoNeDaProcitati(t *testing.T) {
	db := praznaArhiva(t)
	if _, err := Sirotani(db, filepath.Join(t.TempDir(), "nema")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stablo kojeg nema: %v", err)
	}
	bez, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bez-izvora.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bez.Close()
	_, err = Sirotani(bez, t.TempDir())
	greskaSadrzi(t, "bez izvora", err, "čitanje izvora")
	_, err = dodajIzVlastitihMapa(bez, map[string]*niz{}, "", io.Discard)
	greskaSadrzi(t, "vlastite mape bez izvora", err, "čitanje izvora")

	// Popis izvora postoji, ali nizova nema.
	if _, err := bez.Exec(shemaIzvora); err != nil {
		t.Fatal(err)
	}
	_, err = Sirotani(bez, t.TempDir())
	greskaSadrzi(t, "bez nizova", err, "no such table: nizovi")
}

// Zaključava čitanje popisa izvora iz pokvarene tablice: greška, ne popis s
// rupom.
func TestPokvarenPopisIzvora(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE izvori (naziv TEXT, tocnost, red INTEGER, ukljucen INTEGER, mapa TEXT, napomena TEXT);
		INSERT INTO izvori VALUES ('cop', 'tri', 20, 1, '', NULL);`); err != nil {
		t.Fatal(err)
	}
	if _, err := Izvori(db); err == nil {
		t.Error("popis s nečitljivim izvorom pročitan je bez greške")
	}
	if _, _, err := citajIzvore(db); err == nil {
		t.Error("red spajanja s nečitljivom točnošću pročitan je bez greške")
	}
}

// Zaključava usporedbu postavki iz paketa s onima na čvoru: bez paketa ili
// bez postavki u njemu nema razlika, izvor kojeg čvor ne zna javlja se kao
// nepoznat, a razlika samo u napomeni se ne javlja.
func TestRazlikePostavkiIzvora(t *testing.T) {
	db := praznaArhiva(t)
	for _, s := range []*Sadrzaj{nil, {}} {
		if r, err := RazlikeIzvora(db, s); err != nil || r != nil {
			t.Errorf("bez postavki u paketu: %+v, %v", r, err)
		}
	}
	nasi := map[string]Izvor{}
	for _, i := range mustIzvori(t, db) {
		nasi[i.Naziv] = i
	}
	cop := nasi["cop"]
	s := &Sadrzaj{Izvori: []izvorUPaketu{
		{Naziv: "nova-postaja", Tocnost: 4, Red: 20, Ukljucen: true},
		{Naziv: "cop", Tocnost: cop.Tocnost, Red: cop.Red, Ukljucen: cop.Ukljucen, Napomena: "drukčija napomena"},
		{Naziv: "his2000", Tocnost: 2, Red: nasi["his2000"].Red, Ukljucen: nasi["his2000"].Ukljucen},
	}}
	r, err := RazlikeIzvora(db, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 {
		t.Fatalf("razlike: %+v", r)
	}
	if r[0].Naziv != "nova-postaja" || !r[0].Nepoznat || !r[0].Vazna() || r[0].Paket.Tocnost != 4 {
		t.Errorf("nepoznat izvor: %+v", r[0])
	}
	if r[1].Naziv != "his2000" || !r[1].Tocnost || r[1].Red || r[1].Ukljucen || r[1].Nas.Tocnost != 0 {
		t.Errorf("razlika u točnosti: %+v", r[1])
	}

	bez, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bez-izvora.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bez.Close()
	_, err = RazlikeIzvora(bez, s)
	greskaSadrzi(t, "bez popisa izvora", err, "čitanje izvora")
}

// Zaključava popis letvi u arhivi kad se niz ne da pročitati.
func TestLetveUArhiviIzPokvareneTablice(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE nizovi (letva TEXT); INSERT INTO nizovi VALUES (NULL);`); err != nil {
		t.Fatal(err)
	}
	if l, err := LetveUArhivi(db, ""); err == nil {
		t.Errorf("letva bez imena pročitana kao %v", l)
	}
}
