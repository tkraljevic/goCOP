package arhiva

import (
	"database/sql"
	"path/filepath"
	"testing"

	gocopdb "gocop/internal/db"
)

// vezaBezUpisa otvara arhivu na put vezom koja transakciju otvara odmah za
// pisanje i ne čeka, dok druga veza drži upis. Čitanje tako prolazi, a
// transakcija za micanje se ne da otvoriti.
func vezaBezUpisa(t *testing.T, put string) *sql.DB {
	t.Helper()
	drugi, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := drugi.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`CREATE TABLE drugi_upis (x)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = tx.Rollback() // proba je gotova, upis druge veze ne treba
		drugi.Close()
	})
	db, err := sql.Open("sqlite", gocopdb.Adresa(put, "_pragma=busy_timeout(0)", gocopdb.UpisOdPocetka))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// Kad niz ili snimka prođu provjeru, a arhiva se ne da otvoriti za upis,
// micanje javlja grešku, ništa ne miče i pušta bravu.
func TestMicanjeKadSeArhivaNeDaPisati(t *testing.T) {
	koren := t.TempDir()
	zapisiDatoteku(t, filepath.Join(koren, "dunav", "vodograd", "vodograd_cop_vodostaj_satni_2026.csv"),
		"vrijeme;vodostaj_cm\n2026-01-01 01:00;119\n")
	put := filepath.Join(t.TempDir(), "arhiva.db")
	priprema, err := sql.Open("sqlite", AdresaZaPisanje(put))
	if err != nil {
		t.Fatal(err)
	}
	defer priprema.Close()
	if err := PripremiPraznu(priprema); err != nil {
		t.Fatal(err)
	}
	upisiNiz(t, priprema, "vodograd", "cop", "vodostaj", "jutarnji", zapis{t: 0, v: 120})
	upisiNiz(t, priprema, "vodograd", "cop", "vodostaj", "satni", zapis{t: 3600, v: 119})
	if _, err := priprema.Exec(`INSERT INTO profili (letva, datum) VALUES ('vodograd', '2010-03-22')`); err != nil {
		t.Fatal(err)
	}

	db := vezaBezUpisa(t, put)
	brava := filepath.Join(t.TempDir(), "arhiva.db")
	if _, err := MakniNiz(db, brava, koren, "vodograd", "cop", "vodostaj", "jutarnji"); err == nil {
		t.Error("niz je maknut iako se arhiva ne da otvoriti za upis")
	}
	if _, err := MakniSnimku(db, brava, koren, "vodograd", "2010-03-22"); err == nil {
		t.Error("snimka je maknuta iako se arhiva ne da otvoriti za upis")
	}
	if p := PosaoUTijeku(brava); p != "" {
		t.Errorf("brava je ostala iza neuspjelog micanja: %q", p)
	}
	if n := broj(t, priprema, `SELECT count(*) FROM nizovi`) + broj(t, priprema, `SELECT count(*) FROM profili`); n != 3 {
		t.Errorf("neuspjelo micanje promijenilo je arhivu (%d redaka)", n)
	}
}
