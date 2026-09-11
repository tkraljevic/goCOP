package arhiva

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"testing"
)

// Ugradnja je jedna transakcija: ili cijelo izdanje ili ništa.
//
// Prije se transakcija potvrđivala nakon upisa nizova, pa su izvori i spoj išli
// izvan nje. Kvar u tom drugom dijelu ostavljao je obrisan stari spoj i upisane
// nove nizove — pola arhive, uz grešku korisniku.
//
// Kvar se izaziva okidačem koji puca na UPISU u spoj. Čišćenje spoj briše i to
// prolazi, nizovi se upišu, a pukne tek spajanje — dakle točno u dijelu koji je
// prije stajao izvan transakcije.
//
// Micanje tablice ne bi dokazalo ništa: spoj čišćenje dira pa bi se sve povuklo
// i u staroj izvedbi, a izvori se vrate sami jer dopuniShemu na početku
// ugradnje stvara tablice kojih nema.
func TestUgradnjaKojaPukneNeOstavljaPolaArhive(t *testing.T) {
	izvor, put := arhivaSNizom(t, "letva-hv")
	var b bytes.Buffer
	if _, err := Izvezi(izvor, "vukovar", 1, "cop-osijek", &b); err != nil {
		t.Fatal(err)
	}
	izvor.Close()
	s, err := Procitaj(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	_ = put

	cilj := filepath.Join(t.TempDir(), "prima.db")
	db, err := sql.Open("sqlite", cilj)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}
	// Zatečeno stanje: jedan niz druge letve, koji se ne smije izgubiti.
	if _, err := db.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa)
		VALUES ('dunav','batina','his2000','vodostaj','satni',1)`); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`CREATE TRIGGER pukni BEFORE INSERT ON spoj
		BEGIN SELECT RAISE(FAIL, 'namjerni kvar u spajanju'); END`); err != nil {
		t.Fatal(err)
	}
	if err := Ugradi(db, cilj, s); err == nil {
		t.Fatal("ugradnja je prošla iako je drugi dio puknuo")
	}

	// Ništa od paketa ne smije ostati: ni nizovi, ni očitanja.
	var nizova int
	if err := db.QueryRow(`SELECT count(*) FROM nizovi WHERE letva='vukovar'`).Scan(&nizova); err != nil {
		t.Fatal(err)
	}
	if nizova != 0 {
		t.Errorf("nakon puknute ugradnje ostalo %d nizova letve vukovar", nizova)
	}
	// Ono što je bilo prije ugradnje mora ostati.
	var batina int
	if err := db.QueryRow(`SELECT count(*) FROM nizovi WHERE letva='batina'`).Scan(&batina); err != nil {
		t.Fatal(err)
	}
	if batina != 1 {
		t.Errorf("zatečeni niz druge letve je nestao (%d)", batina)
	}
}

// Ugradnja koja prođe ostavlja i nizove i spoj — provjera da transakcija nije
// zatvorena prerano pa da se spoj gubi.
func TestUspjesnaUgradnjaOstavljaISpoj(t *testing.T) {
	izvor, _ := arhivaSNizom(t, "letva-hv")
	var b bytes.Buffer
	if _, err := Izvezi(izvor, "vukovar", 1, "cop-osijek", &b); err != nil {
		t.Fatal(err)
	}
	izvor.Close()
	s, _ := Procitaj(bytes.NewReader(b.Bytes()), int64(b.Len()))

	cilj := filepath.Join(t.TempDir(), "prima.db")
	db, _ := sql.Open("sqlite", cilj)
	defer db.Close()
	if err := PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}
	if err := Ugradi(db, cilj, s); err != nil {
		t.Fatal(err)
	}
	var nizova, uSpoju int
	db.QueryRow(`SELECT count(*) FROM nizovi WHERE letva='vukovar'`).Scan(&nizova)
	db.QueryRow(`SELECT count(*) FROM spoj WHERE letva='vukovar'`).Scan(&uSpoju)
	if nizova == 0 {
		t.Error("nizovi nisu upisani")
	}
	if uSpoju == 0 {
		t.Error("spoj nije složen — transakcija je zatvorena prije njega")
	}
}
