package arhiva

import (
	"bytes"
	"database/sql"
	"io"
	"path/filepath"
	"testing"
)

// Izvoz s letvine stranice dosad je broj izdanja uzimao iz URL-a, pa je čovjek
// mogao izdati vukovar_v1.cop s današnjim sadržajem iako drugi čvorovi pod tim
// imenom već drže nešto drugo. Broj sad odlučuje otisak.
func TestSljedeceIzdanjeCekaDaSeSadrzajPromijeni(t *testing.T) {
	mapa := t.TempDir()
	put := filepath.Join(t.TempDir(), "arhiva.db")
	db := napraviArhivu(t, put)
	defer db.Close()
	res, err := db.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa)
		VALUES ('dunav','batina','his2000','vodostaj','satni',1)`)
	if err != nil {
		t.Fatal(err)
	}
	nizID, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,1000,100)`, nizID); err != nil {
		t.Fatal(err)
	}

	// Prazna mapa: letva nikad nije izdana, pa je na redu prvo izdanje.
	var prvi bytes.Buffer
	m1, err := SljedeceIzdanje(db, mapa, "batina", "cvor", &prvi)
	if err != nil {
		t.Fatal(err)
	}
	if m1.Izdanje != 1 {
		t.Errorf("neizdana letva dobila izdanje %d", m1.Izdanje)
	}
	if prvi.Len() == 0 {
		t.Error("paket je prazan")
	}

	// Izdajemo ga stvarno, pa katalog zna za v1.
	if _, err := Izdaj(db, mapa, "cvor", "batina", false, io.Discard); err != nil {
		t.Fatal(err)
	}

	// Ništa se nije promijenilo — broj ostaje.
	m2, err := SljedeceIzdanje(db, mapa, "batina", "cvor", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Izdanje != 1 {
		t.Errorf("nepromijenjena letva skočila na izdanje %d", m2.Izdanje)
	}
	if m2.Otisak != m1.Otisak {
		t.Errorf("otisak se promijenio bez promjene sadržaja: %s → %s", m1.Otisak, m2.Otisak)
	}

	// Jedna nova vrijednost — i broj mora skočiti.
	if _, err := db.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,2000,105)`, nizID); err != nil {
		t.Fatal(err)
	}
	m3, err := SljedeceIzdanje(db, mapa, "batina", "cvor", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if m3.Izdanje != 2 {
		t.Errorf("promijenjena letva ostala na izdanju %d", m3.Izdanje)
	}
	if m3.Otisak == m1.Otisak {
		t.Error("otisak se nije promijenio iako je vrijednost dodana")
	}
}

// Izdavanje jedne letve ne smije izbaciti ostale iz kataloga: čvor koji čita
// katalog inače misli da ih više nema.
func TestIzdavanjeJedneLetveCuvaOstale(t *testing.T) {
	mapa := t.TempDir()
	db := napraviArhivu(t, filepath.Join(t.TempDir(), "arhiva.db"))
	defer db.Close()
	for _, letva := range []string{"batina", "vukovar"} {
		dodajNiz(t, db, letva)
	}
	if _, err := Izdaj(db, mapa, "cvor", "", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := Izdaj(db, mapa, "cvor", "vukovar", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	k, err := UcitajKatalog(mapa)
	if err != nil {
		t.Fatal(err)
	}
	if len(k.Paketi) != 2 {
		t.Fatalf("nakon izdavanja jedne letve katalog ima %d paketa", len(k.Paketi))
	}
	if k.IzdanjeZa("batina") != 1 {
		t.Errorf("batina je ispala iz kataloga (izdanje %d)", k.IzdanjeZa("batina"))
	}
}

// Probno izdavanje ne smije ništa zapisati.
func TestProbnoIzdavanjeNistaNePise(t *testing.T) {
	mapa := t.TempDir()
	db := napraviArhivu(t, filepath.Join(t.TempDir(), "arhiva.db"))
	defer db.Close()
	dodajNiz(t, db, "batina")

	iz, err := Izdaj(db, mapa, "cvor", "", true, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if iz.Promijenjenih != 1 {
		t.Errorf("proba javila %d promjena", iz.Promijenjenih)
	}
	k, err := UcitajKatalog(mapa)
	if err != nil {
		t.Fatal(err)
	}
	if len(k.Paketi) != 0 {
		t.Errorf("proba je zapisala katalog s %d paketa", len(k.Paketi))
	}
}

func dodajNiz(t *testing.T, db *sql.DB, letva string) {
	t.Helper()
	res, err := db.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa)
		VALUES ('dunav',?,'his2000','vodostaj','satni',1)`, letva)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,1000,100)`, id); err != nil {
		t.Fatal(err)
	}
}

// Letva koju izvoz ne uzme ne smije ispasti iz kataloga: sljedeće izdavanje bi
// je vidjelo kao novu i vratilo na v1, a pod tim imenom drugdje već stoji
// nešto drugo.
func TestNeuspjelaLetvaOstajeUKatalogu(t *testing.T) {
	mapa := t.TempDir()
	put := filepath.Join(t.TempDir(), "arhiva.db")
	db := napraviArhivu(t, put)
	dodajNiz(t, db, "batina")
	dodajNiz(t, db, "vukovar")
	if _, err := Izdaj(db, mapa, "cvor", "", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	// Batini nestanu sva očitanja — paket se više ne da sastaviti.
	if _, err := db.Exec(`DELETE FROM ocitanja WHERE niz IN (SELECT id FROM nizovi WHERE letva='batina')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM nizovi WHERE letva='batina'`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db2, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if _, err := Izdaj(db2, mapa, "cvor", "", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	k, err := UcitajKatalog(mapa)
	if err != nil {
		t.Fatal(err)
	}
	if k.IzdanjeZa("batina") != 1 {
		t.Errorf("batina je ispala iz kataloga (izdanje %d)", k.IzdanjeZa("batina"))
	}
}
