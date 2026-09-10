package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// Čvor koji je preuzeo starije izdanje ima arhivu bez napomene uz niz.
// Čitanje mora raditi i s njom — inače bi mu historijat nestao zbog stupca
// koji nema.
func TestArhivaBezNapomeneSeIDaljeCita(t *testing.T) {
	for _, sluc := range []struct {
		ime      string
		napomena bool
	}{{"stara arhiva", false}, {"nova arhiva", true}} {
		t.Run(sluc.ime, func(t *testing.T) {
			put := filepath.Join(t.TempDir(), "arhiva.db")
			db, err := sql.Open("sqlite", put)
			if err != nil {
				t.Fatal(err)
			}
			stupci := `id INTEGER PRIMARY KEY, zona TEXT NOT NULL DEFAULT '', sliv TEXT NOT NULL,
				letva TEXT NOT NULL, izvor TEXT NOT NULL, velicina TEXT NOT NULL,
				vrsta TEXT NOT NULL, po_danu INTEGER NOT NULL DEFAULT 0,
				od TEXT NOT NULL DEFAULT '', do_ TEXT NOT NULL DEFAULT '',
				zapisa INTEGER NOT NULL DEFAULT 0, otisak TEXT NOT NULL DEFAULT ''`
			if sluc.napomena {
				stupci += `, napomena TEXT NOT NULL DEFAULT ''`
			}
			if _, err := db.Exec(`CREATE TABLE nizovi (` + stupci + `)`); err != nil {
				t.Fatal(err)
			}
			upis := `INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa) VALUES ('dunav','batina','his2000','vodostaj','satni',222598)`
			if sluc.napomena {
				upis = `INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa, napomena)
					VALUES ('dunav','batina','his2000','vodostaj','satni',222598,'zaleđen mjerač 2016./2017.')`
			}
			if _, err := db.Exec(upis); err != nil {
				t.Fatal(err)
			}
			db.Close()

			repo, err := OpenArhiva(put)
			if err != nil || repo == nil {
				t.Fatalf("arhiva se ne otvara: %v", err)
			}
			defer repo.Close()

			nizovi, err := repo.Nizovi(context.Background(), "batina")
			if err != nil {
				t.Fatalf("čitanje nizova: %v", err)
			}
			if len(nizovi) != 1 {
				t.Fatalf("nizova %d, očekivan 1", len(nizovi))
			}
			if nizovi[0].Zapisa != 222598 {
				t.Errorf("zapisa %d", nizovi[0].Zapisa)
			}
			want := ""
			if sluc.napomena {
				want = "zaleđen mjerač 2016./2017."
			}
			if nizovi[0].Napomena != want {
				t.Errorf("napomena %q, očekivano %q", nizovi[0].Napomena, want)
			}
		})
	}
}
