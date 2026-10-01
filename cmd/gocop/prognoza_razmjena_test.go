package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/prognoza"

	_ "modernc.org/sqlite"
)

func cvorZaTest(t *testing.T, ime string) (*sql.DB, *ledger.Recorder, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	baza, err := sql.Open("sqlite", filepath.Join(dir, "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	pb, err := prognoza.Otvori(filepath.Join(dir, "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	return baza, ledger.New(baza, ime), pb
}

// Izdanje izdavača prođe knjigom do drugog čvora i ondje je u bazi prognoza;
// model ide samo kad se promijeni; čvor koji sam izdaje tuđe ne upisuje.
func TestIzdanjeKrozKnjigu(t *testing.T) {
	ctx := context.Background()
	bazaA, recA, pbA := cvorZaTest(t, "osijek")
	bazaB, recB, pbB := cvorZaTest(t, "vinkovci")
	if _, err := pbA.Exec(`INSERT INTO pojasi (letva, velicina, pojas_od, pojas_do, odsjecak, r, rasap, sati, namjesteno) VALUES ('batina','vodostaj',0,1000,1.5,0.9,3,5000,'2026-09-30')`); err != nil {
		t.Fatal(err)
	}
	o := &prognoza.Osvjezivac{Baza: pbA, Cvor: "cop-osijek"}
	izdaj := func(sada int64, v float64) {
		ishod := &prognoza.Ishod{Sada: sada, Izdane: []prognoza.Izdana{{Letva: "batina", Velicina: "vodostaj", Izdano: sada, Ciljni: sada + 1, Vrijednost: v, Dolje: v - 2, Gore: v + 2, Model: "lanac"}}}
		if err := o.Zapisi(ishod); err != nil {
			t.Fatal(err)
		}
		if err := objaviPrognozu(ctx, bazaA, recA, pbA, ishod, "cop-osijek"); err != nil {
			t.Fatal(err)
		}
	}
	izdaj(1000, 100)
	izdaj(1001, 101)
	verzije, err := recA.Since(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	modela := 0
	for _, v := range verzije {
		if v.Entity == prognoza.EntitetModela {
			modela++
		}
	}
	if modela != 1 {
		t.Errorf("model poslan %d puta, a nije se mijenjao", modela)
	}
	if _, err := recB.Apply(ctx, verzije); err != nil {
		t.Fatal(err)
	}
	_ = bazaB

	izdaje := true
	p := &primateljIzdanja{pb: pbB, rec: recB, izdaje: func() bool { return izdaje }}
	p.upisi(verzije)
	var n int
	pbB.QueryRow(`SELECT count(*) FROM izdane`).Scan(&n)
	if n != 0 {
		t.Fatalf("čvor koji izdaje upisao je tuđe izdanje (%d)", n)
	}
	izdaje = false
	p.nadoknadi()
	pbB.QueryRow(`SELECT count(*) FROM izdane`).Scan(&n)
	if n != 2 {
		t.Errorf("primljeno %d vrijednosti, očekivane 2", n)
	}
	pbB.QueryRow(`SELECT count(*) FROM pojasi`).Scan(&n)
	if n != 1 {
		t.Errorf("model nije stigao (%d pojasa)", n)
	}
	if cvor, primljeno, _ := prognoza.ZadnjiIzdavac(pbB); cvor != "cop-osijek" || !primljeno {
		t.Errorf("izdavač %q %v", cvor, primljeno)
	}
	// ponovljeno primanje ništa ne mijenja
	p.upisi(verzije)
	pbB.QueryRow(`SELECT count(*) FROM izdanja`).Scan(&n)
	if n != 2 {
		t.Errorf("izdanja nakon ponovljenog primanja: %d", n)
	}
}
