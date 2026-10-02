package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"

	"gocop/internal/arhiva"
	"gocop/internal/ledger"
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

// Dva čvora s istom letvom ne smiju se nadglasavati: kad je drugi izdao
// novije izdanje, ovaj svoje starije više ne objavljuje.
func TestObjavaNeGaziNovijeIzdanje(t *testing.T) {
	ctx := context.Background()
	baza, rec, _ := cvorZaTest(t, "laptop")
	dir := t.TempDir()
	put := filepath.Join(dir, "vodostaji.db")
	arh, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if err := arhiva.PripremiPraznu(arh); err != nil {
		t.Fatal(err)
	}
	res, _ := arh.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa) VALUES ('drava','pljusak-x','pljusak','oborina','satni',1)`)
	niz, _ := res.LastInsertId()
	arh.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,1000,1.5)`, niz)
	paketi := filepath.Join(dir, "pakete")
	if _, err := arhiva.Izdaj(arh, paketi, rec.Cvor(), "", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	sp, _ := sadrzaj.Otvori("")
	a := &razmjenaArhive{baza: baza, rec: rec, sp: sp, arhivaPut: put, paketiDir: paketi, javljeno: map[string]bool{}}
	if n, _ := a.objavi(ctx); n != 1 {
		t.Fatalf("prva objava: %d", n)
	}
	// drugi čvor izdao je v2 iste letve; stiže razmjenom
	novije, _ := json.Marshal(paketUKnjizi{Letva: "pljusak-x", Izdanje: 2, Otisak: "drugi", Sadrzaj: "x", Izdao: "unraid"})
	if _, err := rec.Apply(ctx, []ledger.Version{{VersionID: "ffffffff-0000-7000-8000-000000000001", Entity: EntitetArhive, EntityID: "pljusak-x",
		NodeID: "unraid", Payload: novije, SchemaVersion: ledger.SchemaVersion}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if n, _ := a.objavi(ctx); n != 0 {
			t.Fatalf("krug %d: starije izdanje ponovno objavljeno (%d)", i, n)
		}
	}
}

// Letvu sagrađenu na ovom čvoru paket ne gazi dok čvor sam preuzima
// vodostaje. Kad uloga prijeđe na drugi čvor (laptop → Unraid), novije
// izdanje koje nosi sve što lokalna letva ima zamjenjuje je; paket koji
// seže kraće ne zamjenjuje.
func TestPaketZamjenjujeLetvuCvoraKojiNePreuzima(t *testing.T) {
	ctx := context.Background()
	bazaA, recA, _ := cvorZaTest(t, "unraid")
	bazaB, recB, _ := cvorZaTest(t, "laptop")
	niz := func(arh *sql.DB, letva, od, do string, zapisa int) {
		t.Helper()
		res, err := arh.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, od, do_, zapisa) VALUES ('drava', ?, 'kisomjer-pljusak', 'oborina', 'satni', ?, ?, ?)`, letva, od, do, zapisa)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		for i := 0; i < zapisa; i++ {
			if _, err := arh.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?, ?, 1)`, id, 1000+i); err != nil {
				t.Fatal(err)
			}
		}
	}
	otvori := func(put string) *sql.DB {
		t.Helper()
		arh, err := sql.Open("sqlite", put)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { arh.Close() })
		if err := arhiva.PripremiPraznu(arh); err != nil {
			t.Fatal(err)
		}
		return arh
	}

	// Unraid: kotoriba seže dan dalje od laptopove, tvrdja kraće
	putA := filepath.Join(t.TempDir(), "vodostaji.db")
	arhA := otvori(putA)
	niz(arhA, "pljusak-kotoriba", "2026-09-26", "2026-10-02", 3)
	niz(arhA, "pljusak-tvrdja", "2026-09-26", "2026-09-30", 2)
	paketi := filepath.Join(t.TempDir(), "pakete")
	if _, err := arhiva.Izdaj(arhA, paketi, recA.Cvor(), "", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	spA, _ := sadrzaj.Otvori("")
	a := &razmjenaArhive{baza: bazaA, rec: recA, sp: spA, arhivaPut: putA, paketiDir: paketi, javljeno: map[string]bool{}}
	if n, err := a.objavi(ctx); err != nil || n != 2 {
		t.Fatalf("objava: %d %v", n, err)
	}

	// laptop: obje letve sagradio sam dok je preuzimao
	putB := filepath.Join(t.TempDir(), "vodostaji.db")
	arhB := otvori(putB)
	niz(arhB, "pljusak-kotoriba", "2026-09-26", "2026-10-01", 2)
	niz(arhB, "pljusak-tvrdja", "2026-09-26", "2026-10-01", 2)
	verzije, _ := recA.Since(ctx, "", 0)
	if _, err := recB.Apply(ctx, verzije); err != nil {
		t.Fatal(err)
	}
	spB, _ := sadrzaj.Otvori("")
	for _, v := range verzije {
		var p paketUKnjizi
		if json.Unmarshal(v.Payload, &p) != nil || p.Sadrzaj == "" {
			continue
		}
		b, vrsta, err := spA.Citaj(ctx, p.Sadrzaj)
		if err != nil {
			t.Fatal(err)
		}
		if err := spB.UpisiProvjereno(ctx, p.Sadrzaj, vrsta, b, "unraid"); err != nil {
			t.Fatal(err)
		}
	}
	preuzima := true
	b := &razmjenaArhive{baza: bazaB, rec: recB, sp: spB, arhivaPut: putB, javljeno: map[string]bool{},
		preuzima: func() bool { return preuzima },
		zeli:     func(context.Context, string, string) bool { return true },
		ugradi: func(s *arhiva.Sadrzaj) error {
			db, err := sql.Open("sqlite", putB)
			if err != nil {
				return err
			}
			defer db.Close()
			return arhiva.Ugradi(db, putB, s)
		}}
	zapisa := func(letva string) int {
		var n int
		arhB.QueryRow(`SELECT count(*) FROM ocitanja WHERE niz IN (SELECT id FROM nizovi WHERE letva = ?)`, letva).Scan(&n)
		return n
	}

	b.primi(ctx)
	if zapisa("pljusak-kotoriba") != 2 || zapisa("pljusak-tvrdja") != 2 {
		t.Fatalf("čvor koji preuzima ne smije pustiti paket preko svoje letve: kotoriba %d, tvrdja %d", zapisa("pljusak-kotoriba"), zapisa("pljusak-tvrdja"))
	}
	if st := b.stanje(ctx); st.Ugradjeno != 2 || st.Ceka != 0 {
		t.Errorf("stanje dok preuzima: %+v", st)
	}

	preuzima = false
	if st := b.stanje(ctx); st.Ugradjeno != 1 || st.Ceka != 1 {
		t.Errorf("stanje kad ne preuzima, prije ugradnje: %+v", st)
	}
	b.primi(ctx)
	if n := zapisa("pljusak-kotoriba"); n != 3 {
		t.Errorf("kotoriba nakon paketa: %d zapisa, očekivano 3", n)
	}
	if n := zapisa("pljusak-tvrdja"); n != 2 {
		t.Errorf("tvrdja: paket seže kraće, a ugrađen je (%d zapisa)", n)
	}
	if p, _ := arhiva.Primljeno(arhB, "pljusak-kotoriba"); p == nil || p.Izdanje != 1 {
		t.Errorf("primljeno izdanje kotoribe: %+v", p)
	}
}
