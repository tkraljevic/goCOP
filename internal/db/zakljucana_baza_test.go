package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Zaključava zašto upis koji počne čitanjem pada s „database is locked” iako
// baza čeka busy_timeout (5 s). Obična transakcija (BEGIN) pravo pisanja
// traži tek kod prvog upisa. Ako je prije toga čitala, SQLite je ne pušta
// čekati: kad drugi upravo piše, odmah vraća SQLITE_BUSY, a kad je drugi u
// međuvremenu upisao, SQLITE_BUSY_SNAPSHOT, jer bi upis išao preko stanja
// koje ova transakcija nije vidjela.
func TestUpisNakonCitanjaNeCeka(t *testing.T) {
	baza := bazaSTablicom(t)
	ctx := context.Background()

	t.Run("drugi upravo piše", func(t *testing.T) {
		citac, drugi := dvijeVeze(t, baza)
		procitajPaPocni(t, citac)
		defer citac.ExecContext(ctx, `ROLLBACK`) //nolint:errcheck // proba je gotova; veza se samo vraća u bazen

		if _, err := drugi.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
			t.Fatal(err)
		}
		defer drugi.ExecContext(ctx, `ROLLBACK`) //nolint:errcheck // proba je gotova; veza se samo vraća u bazen

		pocetak := time.Now()
		_, err := citac.ExecContext(ctx, `INSERT INTO t (x) VALUES (1)`)
		ocekujOdmah(t, err, time.Since(pocetak), sqlite3.SQLITE_BUSY)
	})

	t.Run("drugi je već upisao", func(t *testing.T) {
		citac, drugi := dvijeVeze(t, baza)
		procitajPaPocni(t, citac)
		defer citac.ExecContext(ctx, `ROLLBACK`) //nolint:errcheck // proba je gotova; veza se samo vraća u bazen

		if _, err := drugi.ExecContext(ctx, `INSERT INTO t (x) VALUES (2)`); err != nil {
			t.Fatal(err)
		}

		pocetak := time.Now()
		_, err := citac.ExecContext(ctx, `INSERT INTO t (x) VALUES (3)`)
		ocekujOdmah(t, err, time.Since(pocetak), sqlite3.SQLITE_BUSY_SNAPSHOT)
	})
}

// Zaključava da transakcija iz OpenDB pravo pisanja traži na početku
// (UpisOdPocetka): dok drugi piše, BeginTx pričeka, pa transakcija smije
// najprije čitati i onda pisati. Transakcija samo za čitanje ne čeka nikoga.
func TestTransakcijaPiseOdPocetka(t *testing.T) {
	baza := bazaSTablicom(t)
	ctx := context.Background()

	t.Run("čeka drugog", func(t *testing.T) {
		_, drugi := dvijeVeze(t, baza)
		if _, err := drugi.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
			t.Fatal(err)
		}
		defer drugi.ExecContext(ctx, `ROLLBACK`) //nolint:errcheck // nakon COMMIT nema što vratiti
		if _, err := drugi.ExecContext(ctx, `INSERT INTO t (x) VALUES (4)`); err != nil {
			t.Fatal(err)
		}

		gotovo := make(chan error, 1)
		go func() { gotovo <- procitajPaUpisi(ctx, baza) }()

		select {
		case err := <-gotovo:
			t.Fatalf("transakcija nije čekala drugog (greška %v)", err)
		case <-time.After(200 * time.Millisecond):
		}
		if _, err := drugi.ExecContext(ctx, `COMMIT`); err != nil {
			t.Fatal(err)
		}
		if err := <-gotovo; err != nil {
			t.Fatalf("upis nakon čekanja: %v", err)
		}
	})

	t.Run("samo čitanje ne čeka", func(t *testing.T) {
		_, drugi := dvijeVeze(t, baza)
		if _, err := drugi.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
			t.Fatal(err)
		}
		defer drugi.ExecContext(ctx, `ROLLBACK`) //nolint:errcheck // proba je gotova; veza se samo vraća u bazen

		pocetak := time.Now()
		tx, err := baza.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM t`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if trajalo := time.Since(pocetak); trajalo > time.Second {
			t.Errorf("čitanje je čekalo %v dok drugi piše", trajalo)
		}
	})
}

func bazaSTablicom(t *testing.T) *sql.DB {
	t.Helper()
	baza, err := OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if _, err := baza.Exec(`CREATE TABLE t (x INTEGER)`); err != nil {
		t.Fatal(err)
	}
	return baza
}

// dvijeVeze daje dvije zasebne veze iz bazena, kao dva istodobna posla čvora
func dvijeVeze(t *testing.T, baza *sql.DB) (*sql.Conn, *sql.Conn) {
	t.Helper()
	veze := [2]*sql.Conn{}
	for i := range veze {
		v, err := baza.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { v.Close() })
		veze[i] = v
	}
	return veze[0], veze[1]
}

// procitajPaPocni otvara običnu transakciju i u njoj čita, kao spremanje
// koje najprije provjeri postojeći zapis
func procitajPaPocni(t *testing.T, veza *sql.Conn) {
	t.Helper()
	ctx := context.Background()
	if _, err := veza.ExecContext(ctx, `BEGIN`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := veza.QueryRowContext(ctx, `SELECT count(*) FROM t`).Scan(&n); err != nil {
		t.Fatal(err)
	}
}

// procitajPaUpisi je spremanje kakvo program radi: BeginTx, čitanje, upis
func procitajPaUpisi(ctx context.Context, baza *sql.DB) error {
	tx, err := baza.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM t`).Scan(&n); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO t (x) VALUES (5)`); err != nil {
		return err
	}
	return tx.Commit()
}

// ocekujOdmah traži grešku sa zadanim kodom, vraćenu bez čekanja busy_timeouta
func ocekujOdmah(t *testing.T, err error, trajalo time.Duration, kod int) {
	t.Helper()
	var se *sqlite.Error
	if !errors.As(err, &se) || se.Code() != kod {
		t.Fatalf("greška %v, treba kod %d", err, kod)
	}
	if trajalo > time.Second {
		t.Errorf("upis je čekao %v; SQLite ga je trebao odbiti odmah", trajalo)
	}
}
