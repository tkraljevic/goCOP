package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/arhiva"
	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/sadrzaj"
)

// Paket izdan na jednom čvoru stigne drugome: kazalo knjigom, paket
// spremištem sadržaja, i ondje se ugradi. Čvor kojemu pretplata paket ne
// pokriva ga ne traži.
func TestArhivaKrozRazmjenu(t *testing.T) {
	ctx := context.Background()
	bazaA, recA, _ := cvorZaTest(t, "osijek")
	bazaB, recB, _ := cvorZaTest(t, "server")
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
	if _, err := arhiva.Izdaj(arhA, paketi, recA.Cvor(), "", false, nil, io.Discard); err != nil {
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
	arhB, _ := sql.Open("sqlite", db.SamoZaCitanje(putB))
	defer arhB.Close()
	var n int
	arhB.QueryRow(`SELECT count(*) FROM ocitanja`).Scan(&n)
	if n != 2 {
		t.Errorf("ugrađeno očitanja: %d", n)
	}
	if p, _ := arhiva.Primljeno(arhB, "batina"); p == nil || p.Izdanje != 1 {
		t.Errorf("primljeno izdanje: %+v", p)
	}
	// Rok držanja otpusti primljeni paket sa spremišta (Odluke
	// (13. 10. 2026.)): arhiva ostaje ugrađena, a paket se ne traži ponovno.
	otpusten := z[0].Otisak
	if err := spB.Otpusti(ctx, otpusten); err != nil || spB.Ima(ctx, otpusten) {
		t.Fatalf("otpuštanje paketa: %v", err)
	}
	b.primi(ctx)
	if z, _ := spB.Zeljeni(ctx, 10); len(z) != 0 {
		t.Errorf("otpušteni paket ponovno se traži: %+v", z)
	}
	if err := arhB.QueryRow(`SELECT count(*) FROM ocitanja`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if st := b.stanje(ctx); n != 2 || st.Ugradjeno != 1 || st.Ceka != 0 {
		t.Errorf("nakon otpuštanja paketa: %d očitanja, stanje %+v", n, st)
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
	if _, err := arhiva.Izdaj(arh, paketi, rec.Cvor(), "", false, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	sp, _ := sadrzaj.Otvori("")
	a := &razmjenaArhive{baza: baza, rec: rec, sp: sp, arhivaPut: put, paketiDir: paketi, javljeno: map[string]bool{}}
	if n, _ := a.objavi(ctx); n != 1 {
		t.Fatalf("prva objava: %d", n)
	}
	// drugi čvor izdao je v2 iste letve; stiže razmjenom
	novije, err := json.Marshal(paketUKnjizi{Letva: "pljusak-x", Izdanje: 2, Otisak: "drugi", Sadrzaj: "x", Izdao: "server"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Apply(ctx, []ledger.Version{{VersionID: "ffffffff-0000-7000-8000-000000000001", Entity: EntitetArhive, EntityID: "pljusak-x",
		NodeID: "server", Payload: novije, SchemaVersion: ledger.SchemaVersion}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if n, _ := a.objavi(ctx); n != 0 {
			t.Fatalf("krug %d: starije izdanje ponovno objavljeno (%d)", i, n)
		}
	}
}

// Letvu sagrađenu na ovom čvoru paket ne gazi dok čvor sam preuzima
// vodostaje. Kad uloga prijeđe na drugi čvor (laptop → server), novije
// izdanje koje nosi sve što lokalna letva ima zamjenjuje je; paket koji
// seže kraće ne zamjenjuje.
func TestPaketZamjenjujeLetvuCvoraKojiNePreuzima(t *testing.T) {
	ctx := context.Background()
	bazaA, recA, _ := cvorZaTest(t, "server")
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

	// Server: kotoriba seže dan dalje od laptopove, tvrdja kraće
	putA := filepath.Join(t.TempDir(), "vodostaji.db")
	arhA := otvori(putA)
	niz(arhA, "pljusak-kotoriba", "2026-09-26", "2026-10-02", 3)
	niz(arhA, "pljusak-tvrdja", "2026-09-26", "2026-09-30", 2)
	paketi := filepath.Join(t.TempDir(), "pakete")
	if _, err := arhiva.Izdaj(arhA, paketi, recA.Cvor(), "", false, nil, io.Discard); err != nil {
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
		if err := spB.UpisiProvjereno(ctx, p.Sadrzaj, vrsta, b, "server"); err != nil {
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

// izdanaMapa izda arhivu s jednom kišnom letvom u novu mapu paketa i vraća
// tu mapu.
func izdanaMapa(t *testing.T, izdao string) string {
	t.Helper()
	dir := t.TempDir()
	arh, err := sql.Open("sqlite", filepath.Join(dir, "vodostaji.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer arh.Close()
	if err := arhiva.PripremiPraznu(arh); err != nil {
		t.Fatal(err)
	}
	res, err := arh.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa) VALUES ('drava','pljusak-x','pljusak','oborina','satni',1)`)
	if err != nil {
		t.Fatal(err)
	}
	niz, _ := res.LastInsertId()
	if _, err := arh.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,1000,1.5)`, niz); err != nil {
		t.Fatal(err)
	}
	paketi := filepath.Join(dir, "pakete")
	if _, err := arhiva.Izdaj(arh, paketi, izdao, "", false, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	return paketi
}

// zapisDnevnika hvata dnevnik do kraja testa.
func zapisDnevnika(t *testing.T) *bytes.Buffer {
	t.Helper()
	var zapis bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&zapis)
	t.Cleanup(func() { log.SetOutput(stari) })
	return &zapis
}

// Prazan redak koji je u katalogu ostavilo izdanje 0.0.37 objava preskače:
// ne čita mapu paketa kao paket i ne javlja grešku u dnevnik svaki krug. Isto
// vrijedi za redak s letvom, a bez datoteke. Pravi paket iz istog kataloga
// objavi se kao i prije.
func TestObjavaPreskaceRedakBezLetveIliDatoteke(t *testing.T) {
	ctx := context.Background()
	baza, rec, _ := cvorZaTest(t, "laptop")
	paketi := izdanaMapa(t, rec.Cvor())
	k, err := arhiva.UcitajKatalog(paketi)
	if err != nil {
		t.Fatal(err)
	}
	k.Paketi = append(k.Paketi, arhiva.UPaketu{}, arhiva.UPaketu{Letva: "pljusak-y", Izdanje: 1})
	b, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paketi, arhiva.ImeKataloga), b, 0o644); err != nil {
		t.Fatal(err)
	}

	dnevnik := zapisDnevnika(t)
	sp, _ := sadrzaj.Otvori("")
	a := &razmjenaArhive{baza: baza, rec: rec, sp: sp, paketiDir: paketi, javljeno: map[string]bool{}}
	for krug, ocekivano := range []int{1, 0} {
		if n, err := a.objavi(ctx); err != nil || n != ocekivano {
			t.Fatalf("krug %d: objavljeno %d (%v), očekivano %d", krug, n, err, ocekivano)
		}
	}
	if dnevnik.Len() != 0 {
		t.Errorf("objava je pisala u dnevnik:\n%s", dnevnik.String())
	}
	if _, err := rec.Latest(ctx, EntitetArhive, "pljusak-y"); !errors.Is(err, ledger.ErrNoVersion) {
		t.Errorf("redak bez datoteke ušao je u kazalo: %v", err)
	}
}

// Objava stane s greškom kad ne može pročitati katalog ili knjigu ili upisati
// paket, a paket kojeg u mapi nema preskoči i javi u dnevnik. Bez mape
// paketa, bez spremišta ili s katalogom drugog čvora nema što objaviti.
func TestObjavaKojaNeUspije(t *testing.T) {
	ctx := context.Background()
	cvor := func(t *testing.T) (*razmjenaArhive, string) {
		t.Helper()
		baza, rec, _ := cvorZaTest(t, "laptop")
		sp, err := sadrzaj.Otvori("")
		if err != nil {
			t.Fatal(err)
		}
		paketi := izdanaMapa(t, rec.Cvor())
		return &razmjenaArhive{baza: baza, rec: rec, sp: sp, paketiDir: paketi, javljeno: map[string]bool{}}, paketi
	}
	nistaBezGreske := func(t *testing.T, a *razmjenaArhive) {
		t.Helper()
		if n, err := a.objavi(ctx); err != nil || n != 0 {
			t.Errorf("objavljeno %d (%v), očekivano ništa bez greške", n, err)
		}
	}
	greska := func(t *testing.T, a *razmjenaArhive, dio string) {
		t.Helper()
		n, err := a.objavi(ctx)
		if err == nil || !strings.Contains(err.Error(), dio) || n != 0 {
			t.Errorf("objavljeno %d, greška %v; očekivano 0 i greška s %q", n, err, dio)
		}
	}

	t.Run("bez mape paketa", func(t *testing.T) {
		a, _ := cvor(t)
		a.paketiDir = ""
		nistaBezGreske(t, a)
	})
	t.Run("bez spremišta", func(t *testing.T) {
		a, _ := cvor(t)
		a.sp = nil
		nistaBezGreske(t, a)
	})
	t.Run("katalog drugog čvora", func(t *testing.T) {
		a, _ := cvor(t)
		a.rec = ledger.New(a.baza, "server")
		nistaBezGreske(t, a)
	})
	t.Run("pokvaren katalog", func(t *testing.T) {
		a, paketi := cvor(t)
		if err := os.WriteFile(filepath.Join(paketi, arhiva.ImeKataloga), []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		greska(t, a, "katalog se ne čita")
	})
	t.Run("knjiga se ne čita", func(t *testing.T) {
		a, _ := cvor(t)
		a.baza.Close()
		greska(t, a, "database is closed")
	})
	t.Run("paketa nema u mapi", func(t *testing.T) {
		a, paketi := cvor(t)
		if err := os.Remove(filepath.Join(paketi, "pljusak-x_v1.cop")); err != nil {
			t.Fatal(err)
		}
		dnevnik := zapisDnevnika(t)
		nistaBezGreske(t, a)
		if !strings.Contains(dnevnik.String(), "arhiva: paket pljusak-x_v1.cop nije pročitan") {
			t.Errorf("dnevnik ne kaže da paket nije pročitan:\n%s", dnevnik.String())
		}
	})
	t.Run("prazan paket", func(t *testing.T) {
		a, paketi := cvor(t)
		if err := os.WriteFile(filepath.Join(paketi, "pljusak-x_v1.cop"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		greska(t, a, "prazan sadržaj se ne sprema")
	})
	t.Run("kazalo se ne da upisati", func(t *testing.T) {
		a, _ := cvor(t)
		zatvorena, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "zatvorena.db"))
		if err != nil {
			t.Fatal(err)
		}
		zatvorena.Close()
		a.baza = zatvorena
		greska(t, a, "database is closed")
		if _, err := a.rec.Latest(ctx, EntitetArhive, "pljusak-x"); !errors.Is(err, ledger.ErrNoVersion) {
			t.Errorf("kazalo je upisano iako upis nije uspio: %v", err)
		}
	})
}

// Katalog vraćen na stariji ne smije objaviti drugi sadržaj pod brojem koji
// je već u kazalu: primatelji koji taj broj imaju preskočili bi ga bez
// traga. Izdavanje broj uzima i iz kazala (uKazalu), pa novi sadržaj dobiva
// broj iza zadnjeg objavljenog.
func TestVraceniKatalogObjavljujeNoviBroj(t *testing.T) {
	ctx := context.Background()
	baza, rec, _ := cvorZaTest(t, "laptop")
	dir := t.TempDir()
	put := filepath.Join(dir, "vodostaji.db")
	arh, err := sql.Open("sqlite", arhiva.AdresaZaPisanje(put))
	if err != nil {
		t.Fatal(err)
	}
	defer arh.Close()
	if err := arhiva.PripremiPraznu(arh); err != nil {
		t.Fatal(err)
	}
	res, err := arh.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa) VALUES ('drava','pljusak-x','pljusak','oborina','satni',1)`)
	if err != nil {
		t.Fatal(err)
	}
	niz, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	paketi := filepath.Join(dir, "pakete")
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	a := &razmjenaArhive{baza: baza, rec: rec, sp: sp, arhivaPut: put, paketiDir: paketi, javljeno: map[string]bool{}}
	izdajIObjavi := func(vrijeme int) arhiva.RedIzdanja {
		t.Helper()
		if _, err := arh.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,?,1.5)`, niz, vrijeme); err != nil {
			t.Fatal(err)
		}
		iz, err := arhiva.Izdaj(arh, paketi, rec.Cvor(), "", false, a.uKazalu, io.Discard)
		if err != nil || len(iz.Redci) != 1 {
			t.Fatalf("izdavanje: %+v, %v", iz.Redci, err)
		}
		if _, err := a.objavi(ctx); err != nil {
			t.Fatal(err)
		}
		return iz.Redci[0]
	}
	izdajIObjavi(1000)
	katalogV1, err := os.ReadFile(filepath.Join(paketi, arhiva.ImeKataloga))
	if err != nil {
		t.Fatal(err)
	}
	if r := izdajIObjavi(4600); r.Izdanje != 2 {
		t.Fatalf("drugo izdanje: %+v", r)
	}
	if err := os.WriteFile(filepath.Join(paketi, arhiva.ImeKataloga), katalogV1, 0o644); err != nil {
		t.Fatal(err)
	}
	r := izdajIObjavi(8200)
	if izdanje, otisak := a.uKazalu("pljusak-x"); r.Izdanje != 3 || izdanje != 3 || otisak != r.Otisak {
		t.Errorf("nakon vraćenog kataloga izdano v%d, u kazalu v%d (%s), a izdani otisak je %s", r.Izdanje, izdanje, otisak, r.Otisak)
	}
}
