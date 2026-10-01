package main

import (
	"context"
	"database/sql"
	"io"
	"path/filepath"
	"testing"

	"gocop/internal/arhiva"
	"gocop/internal/sadrzaj"
)

// Paket izdan na jednom čvoru stigne drugome: kazalo knjigom, paket
// spremištem sadržaja, i ondje se ugradi. Čvor kojemu pretplata paket ne
// pokriva ga ne traži.
func TestArhivaKrozRazmjenu(t *testing.T) {
	ctx := context.Background()
	bazaA, recA, _ := cvorZaTest(t, "osijek")
	bazaB, recB, _ := cvorZaTest(t, "unraid")
	dirA, dirB := t.TempDir(), t.TempDir()

	putA := filepath.Join(dirA, "vodostaji.db")
	arhA, err := sql.Open("sqlite", putA)
	if err != nil {
		t.Fatal(err)
	}
	if err := arhiva.PripremiPraznu(arhA); err != nil {
		t.Fatal(err)
	}
	res, err := arhA.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa) VALUES ('dunav','batina','his2000','vodostaj','satni',2)`)
	if err != nil {
		t.Fatal(err)
	}
	niz, _ := res.LastInsertId()
	if _, err := arhA.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,1000,100), (?,1001,101)`, niz, niz); err != nil {
		t.Fatal(err)
	}
	paketi := filepath.Join(dirA, "pakete")
	if _, err := arhiva.Izdaj(arhA, paketi, recA.Cvor(), "", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	spA, _ := sadrzaj.Otvori("")
	spB, _ := sadrzaj.Otvori("")

	a := &razmjenaArhive{baza: bazaA, rec: recA, sp: spA, arhivaPut: putA, paketiDir: paketi, javljeno: map[string]bool{}}
	if n, err := a.objavi(ctx); err != nil || n != 1 {
		t.Fatalf("objava: %d %v", n, err)
	}
	if n, _ := a.objavi(ctx); n != 0 {
		t.Errorf("isto izdanje objavljeno dvaput")
	}
	verzije, _ := recA.Since(ctx, "", 0)
	if _, err := recB.Apply(ctx, verzije); err != nil {
		t.Fatal(err)
	}

	putB := filepath.Join(dirB, "vodostaji.db")
	pratiArhivu := false
	b := &razmjenaArhive{baza: bazaB, rec: recB, sp: spB, arhivaPut: putB, javljeno: map[string]bool{},
		zeli: func(context.Context, string, string) bool { return pratiArhivu },
		ugradi: func(s *arhiva.Sadrzaj) error {
			db, err := sql.Open("sqlite", putB)
			if err != nil {
				return err
			}
			defer db.Close()
			if err := arhiva.PripremiPraznu(db); err != nil {
				return err
			}
			return arhiva.Ugradi(db, putB, s)
		}}
	b.primi(ctx)
	if z, _ := spB.Zeljeni(ctx, 10); len(z) != 0 {
		t.Fatalf("čvor bez pretplate traži paket: %+v", z)
	}
	pratiArhivu = true
	b.primi(ctx)
	z, _ := spB.Zeljeni(ctx, 10)
	if len(z) != 1 {
		t.Fatalf("želje: %+v", z)
	}
	// razmjena sadržaja: A daje bajtove po otisku
	bajtovi, vrsta, err := spA.Citaj(ctx, z[0].Otisak)
	if err != nil {
		t.Fatal(err)
	}
	if err := spB.UpisiProvjereno(ctx, z[0].Otisak, vrsta, bajtovi, "osijek"); err != nil {
		t.Fatal(err)
	}
	b.primi(ctx)
	arhB, _ := sql.Open("sqlite", putB+"?mode=ro")
	defer arhB.Close()
	var n int
	arhB.QueryRow(`SELECT count(*) FROM ocitanja`).Scan(&n)
	if n != 2 {
		t.Errorf("ugrađeno očitanja: %d", n)
	}
	if p, _ := arhiva.Primljeno(arhB, "batina"); p == nil || p.Izdanje != 1 {
		t.Errorf("primljeno izdanje: %+v", p)
	}
	// izvorni čvor svoj paket ne ugrađuje
	a.zeli = func(context.Context, string, string) bool { return true }
	a.primi(ctx)
	if z, _ := spA.Zeljeni(ctx, 10); len(z) != 0 {
		t.Errorf("izdavač traži vlastiti paket")
	}
}
