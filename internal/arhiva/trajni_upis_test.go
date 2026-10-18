package arhiva

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"modernc.org/sqlite"
)

// pracenje bilježi synchronous svake veze koja se otvori na putu koji test
// prati. Driver popis hookova čita bez brave, pa se hook prijavi jednom,
// prije svih testova.
var pracenje struct {
	sync.Mutex
	put         string
	synchronous []driver.Value
}

func init() {
	sqlite.RegisterConnectionHook(func(veza sqlite.ExecQuerierContext, dsn string) error {
		pracenje.Lock()
		defer pracenje.Unlock()
		if pracenje.put == "" || !strings.HasPrefix(dsn, pracenje.put) {
			return nil
		}
		redovi, err := veza.QueryContext(context.Background(), "PRAGMA synchronous", nil)
		if err != nil {
			return err
		}
		defer redovi.Close()
		v := make([]driver.Value, 1)
		if err := redovi.Next(v); err != nil {
			return err
		}
		pracenje.synchronous = append(pracenje.synchronous, v[0])
		return nil
	})
}

// Zaključava da gradnja piše u arhivu tako da potvrđen upis preživi nestanak
// struje. Gradnja piše ravno u arhivu koju čvor koristi, a uz
// synchronous(OFF) nestanak struje usred gradnje mogao je pokvariti cijelu
// arhivu.
func TestGradnjaPiseTrajno(t *testing.T) {
	koren, baza := stabloSKvarom(t, "")
	pracenje.Lock()
	pracenje.put, pracenje.synchronous = baza, nil
	pracenje.Unlock()
	t.Cleanup(func() {
		pracenje.Lock()
		pracenje.put = ""
		pracenje.Unlock()
	})

	if _, err := Izgradi(koren, baza, "", io.Discard); err != nil {
		t.Fatal(err)
	}
	pracenje.Lock()
	defer pracenje.Unlock()
	if len(pracenje.synchronous) == 0 {
		t.Fatal("gradnja nije otvorila nijednu vezu na arhivu")
	}
	for i, v := range pracenje.synchronous {
		if v != int64(2) {
			t.Errorf("%d. veza gradnje: synchronous %v, treba 2 (FULL)", i+1, v)
		}
	}
}

// brojPotvrda vraća koliko je puta baza potvrdila transakciju dok je radio f.
// Baza se svede na jednu vezu, da svaki upis prođe kroz brojač.
func brojPotvrda(t *testing.T, db *sql.DB, f func()) int {
	t.Helper()
	db.SetMaxOpenConns(1)
	veza, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	potvrda := 0
	if err := veza.Raw(func(dc any) error {
		dc.(sqlite.HookRegisterer).RegisterCommitHook(func() int32 { potvrda++; return 0 })
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := veza.Close(); err != nil {
		t.Fatal(err)
	}
	f()
	return potvrda
}

// Zaključava da gradnja snimku korita, krivulju protoka i promjene kote nule
// upisuje jednom potvrdom, a ne redak po redak. Uz trajni upis svaka je
// potvrda jedno spuštanje na disk, a snimke cijelog stabla imaju oko 19.000
// točaka: na Linuxu i Windowsu samo one trajale bi i po minutu.
func TestGradnjaUpisujeJednomPotvrdom(t *testing.T) {
	t.Run("snimka korita", func(t *testing.T) {
		db := praznaArhiva(t)
		koren := t.TempDir()
		var b strings.Builder
		b.WriteString("# poprečni profil korita, mjereno 2010-03-22, vodostaj pri mjerenju 174 cm, kota nule 80.45\n" +
			"stacionaza_m;visina_m\n")
		for i := range 60 {
			fmt.Fprintf(&b, "%d;%d\n", i, 90-i%7)
		}
		zapisiDatoteku(t, putProfila(koren, "dunav", "vodograd", "2010-03-22"), b.String())
		potvrda := brojPotvrda(t, db, func() {
			uhvatiIspis(t, &os.Stdout, func() {
				if err := profili(db, koren, "", nil); err != nil {
					t.Fatal(err)
				}
			})
		})
		if potvrda != 1 {
			t.Errorf("snimka sa 60 točaka: %d potvrda, treba 1", potvrda)
		}
		if n := broj(t, db, `SELECT count(*) FROM profil_tocke`); n != 60 {
			t.Errorf("upisano %d točaka, treba 60", n)
		}
	})
	t.Run("krivulja protoka", func(t *testing.T) {
		db := praznaArhiva(t)
		k := krivuljaUPaketu{VrijediOd: "2020-01-01", Izvor: "his2000", Odsjecci: []odsjecakUPaketu{
			{OdCm: -50, DoCm: 100, Oblik: "potencija", P1: 1, P2: 2, P3: 1.5},
			{OdCm: 100, DoCm: 300, Oblik: "potencija", P1: 1, P2: 2, P3: 1.6},
			{OdCm: 300, DoCm: 900, Oblik: "potencija", P1: 1, P2: 2, P3: 1.7},
		}}
		potvrda := brojPotvrda(t, db, func() {
			if err := postaviKrivulju(db, "vodograd", k); err != nil {
				t.Fatal(err)
			}
		})
		if potvrda != 1 {
			t.Errorf("krivulja s tri odsječka: %d potvrda, treba 1", potvrda)
		}
		if n := broj(t, db, `SELECT count(*) FROM hq_odsjecci`); n != 3 {
			t.Errorf("upisano %d odsječaka, treba 3", n)
		}
	})
	t.Run("promjene kote nule", func(t *testing.T) {
		db := praznaArhiva(t)
		koren := t.TempDir()
		zapisiDatoteku(t, filepath.Join(koren, "promjene-kote.csv"), "letva;datum;pomak_cm\n"+
			"vodograd;1943-01-01;200\nvodograd;1950-06-01;-12\nrijecno;1961-03-15;7\n")
		potvrda := brojPotvrda(t, db, func() {
			if _, err := promjeneKote(db, koren, io.Discard); err != nil {
				t.Fatal(err)
			}
		})
		if potvrda != 1 {
			t.Errorf("tri promjene kote: %d potvrda, treba 1", potvrda)
		}
		if n := broj(t, db, `SELECT count(*) FROM promjene_kote`); n != 3 {
			t.Errorf("upisano %d promjena, treba 3", n)
		}
	})
}

// Zaključava da transakcija na zatvorenoj arhivi javlja grešku, a zadani
// upis se ne pokrene.
func TestTransakcijaNaZatvorenojArhivi(t *testing.T) {
	db := praznaArhiva(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	pokrenut := false
	err := uTransakciji(db, func(Izvrsitelj) error { pokrenut = true; return nil })
	if err == nil || pokrenut {
		t.Errorf("zatvorena arhiva: greška %v, upis pokrenut %v", err, pokrenut)
	}
}
