package db

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shemaBaze su svi objekti baze (tablice, indeksi, okidači) s tekstom
// naredbe kojom su nastali; tablica koja nije prazna je greška testa
func shemaBaze(t *testing.T, baza *sql.DB) string {
	t.Helper()
	redovi, err := baza.Query(`SELECT type, name, tbl_name, coalesce(sql, '') FROM sqlite_master ORDER BY type, name`)
	if err != nil {
		t.Fatal(err)
	}
	defer redovi.Close()
	var b strings.Builder
	var tablice []string
	for redovi.Next() {
		var vrsta, ime, tablica, naredba string
		if err := redovi.Scan(&vrsta, &ime, &tablica, &naredba); err != nil {
			t.Fatal(err)
		}
		b.WriteString(vrsta + " " + ime + " " + tablica + "\n" + naredba + "\n")
		if vrsta == "table" {
			tablice = append(tablice, ime)
		}
	}
	if err := redovi.Err(); err != nil {
		t.Fatal(err)
	}
	for _, tablica := range tablice {
		var n int
		if err := baza.QueryRow(`SELECT count(*) FROM "` + tablica + `"`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		// Redak u novoj bazi (slučajni id, vrijeme, ključ) predložak bi
		// smrznuo i dao istim svim bazama jednog testnog procesa
		if n != 0 {
			t.Errorf("tablica %s nove baze ima %d redaka; predložak smije nositi samo shemu", tablica, n)
		}
	}
	return b.String()
}

// Baza iz predloška ista je kao nova baza sa InitSchema: isti objekti istim
// naredbama, bez ijednog retka, i nakon OpenDB isti način rada (WAL, strani
// ključevi, trajni upis). Ponovni InitSchema, kao pri svakom pokretanju
// čvora, na njoj ništa ne mijenja.
func TestBazaIzPredloskaJeKaoNovaShema(t *testing.T) {
	svjeza := praznaBaza(t)
	izPredloska, err := BazaIzPredloska(filepath.Join(t.TempDir(), "nova", "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer izPredloska.Close()

	ocekivano := shemaBaze(t, svjeza)
	if !strings.Contains(ocekivano, "table record_versions") {
		t.Fatalf("nova shema nema knjigu verzija:\n%s", ocekivano)
	}
	if dobiveno := shemaBaze(t, izPredloska); dobiveno != ocekivano {
		t.Errorf("baza iz predloška nema istu shemu kao nova baza:\n%s\nnova:\n%s", dobiveno, ocekivano)
	}
	for _, pragma := range []string{"journal_mode", "foreign_keys", "synchronous", "user_version", "page_size", "auto_vacuum", "encoding"} {
		var nova, kopija string
		if err := svjeza.QueryRow(`PRAGMA ` + pragma).Scan(&nova); err != nil {
			t.Fatal(err)
		}
		if err := izPredloska.QueryRow(`PRAGMA ` + pragma).Scan(&kopija); err != nil {
			t.Fatal(err)
		}
		if nova != kopija {
			t.Errorf("PRAGMA %s: iz predloška %q, nova baza %q", pragma, kopija, nova)
		}
	}

	if err := InitSchema(izPredloska); err != nil {
		t.Fatal(err)
	}
	if dobiveno := shemaBaze(t, izPredloska); dobiveno != ocekivano {
		t.Errorf("InitSchema je promijenio bazu iz predloška:\n%s", dobiveno)
	}
}

// Predložak se upisuje samo u novu datoteku: bazu koju test ponovno otvara
// ne prepiše, nego javlja grešku
func TestPredlozakNePrepisujePostojecuBazu(t *testing.T) {
	put := filepath.Join(t.TempDir(), "gocop.db")
	if err := os.WriteFile(put, []byte("zatečena baza"), 0o644); err != nil {
		t.Fatal(err)
	}
	if baza, err := BazaIzPredloska(put); err == nil {
		baza.Close()
		t.Fatal("predložak je upisan preko postojeće baze")
	}
	if sadrzaj, err := os.ReadFile(put); err != nil || string(sadrzaj) != "zatečena baza" {
		t.Errorf("postojeća baza je promijenjena: %q, %v", sadrzaj, err)
	}
}

// Greške pri slaganju i upisu predloška vraćaju se pozivatelju
func TestPredlozakJavljaGreske(t *testing.T) {
	mapa := t.TempDir()
	datoteka := filepath.Join(mapa, "datoteka")
	if err := os.WriteFile(datoteka, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ispod := filepath.Join(datoteka, "gocop.db")

	if err := UpisiPredlozak(ispod); err == nil {
		t.Error("upis predloška ispod obične datoteke nije javio grešku")
	}
	for _, adresa := range []string{":memory:", "file:" + filepath.Join(mapa, "b.db") + "?mode=rwc"} {
		if err := UpisiPredlozak(adresa); err == nil {
			t.Errorf("predložak je upisan u %s", adresa)
		}
	}
	if _, err := os.Stat(":memory:"); err == nil {
		t.Error("predložak je u radnoj mapi napravio datoteku :memory:")
	}
	ocekivana := errors.New("predložak se ne da složiti")
	if err := upisiNovu(filepath.Join(mapa, "a.db"), func() ([]byte, error) { return nil, ocekivana }); !errors.Is(err, ocekivana) {
		t.Errorf("greška izvora predloška: %v", err)
	}
	if _, err := slozPredlozak(ispod); err == nil {
		t.Error("slaganje predloška ispod obične datoteke nije javilo grešku")
	}

	// tablica letvi bez stupca code: indeks sheme se ne da složiti
	krivaShema := filepath.Join(mapa, "kriva.db")
	baza, err := OpenDB(krivaShema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`CREATE TABLE stations (x TEXT)`); err != nil {
		t.Fatal(err)
	}
	baza.Close()
	if _, err := slozPredlozak(krivaShema); err == nil {
		t.Error("slaganje predloška na krivoj shemi nije javilo grešku")
	}

	// kopija već postoji: VACUUM INTO ne piše preko druge baze
	zauzeto := filepath.Join(mapa, "zauzeto.db")
	druga, err := OpenDB(zauzeto + ".kopija")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := druga.Exec(`CREATE TABLE druga (x TEXT)`); err != nil {
		t.Fatal(err)
	}
	druga.Close()
	if _, err := slozPredlozak(zauzeto); err == nil {
		t.Error("slaganje predloška preko postojeće kopije nije javilo grešku")
	}
}
