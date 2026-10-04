package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/prognoza"
)

// izdavackiKrug je krug čvora koji izdaje, s malim lancem od dvije letve
// (gornja → donja) nad zadnja 48 sata očitanja, pa izračun uspije
func izdavackiKrug(t *testing.T) (*satniKrug, *[]*prognoza.Ishod) {
	t.Helper()
	stari := prognoza.PoluvijekIspravka
	prognoza.PoluvijekIspravka = 0
	t.Cleanup(func() { prognoza.PoluvijekIspravka = stari })
	k, _ := okolinaKruga(t, true)

	ocitanja, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "ocitanja.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ocitanja.Close() })
	if _, err := ocitanja.Exec(`
		CREATE TABLE stations (id TEXT PRIMARY KEY, code TEXT);
		CREATE TABLE readings (station_id TEXT, measured_at DATETIME, level_cm INTEGER, flow_m3s REAL);
		INSERT INTO stations VALUES ('1','gornja'),('2','donja');`); err != nil {
		t.Fatal(err)
	}
	zadnji := time.Now().UTC().Truncate(time.Hour)
	for i := 0; i < 48; i++ {
		kad := zadnji.Add(-time.Duration(i) * time.Hour)
		if _, err := ocitanja.Exec(`INSERT INTO readings (station_id, measured_at, level_cm) VALUES ('1',?,?),('2',?,?)`,
			kad, 100+i, kad, 90+i); err != nil {
			t.Fatal(err)
		}
	}
	pojasi := []prognoza.Pojas{{Letva: "donja", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
		Ulazi: []prognoza.Ulaz{{Letva: "gornja", Velicina: "vodostaj", PomakH: 3, Sirina: 1, Nagib: 1}}}}
	if err := prognoza.Spremi(k.prognoze, pojasi, "proba"); err != nil {
		t.Fatal(err)
	}
	k.osvjezivac = &prognoza.Osvjezivac{Baza: k.prognoze, Ocitanja: ocitanja, Najdalje: 12, Model: prognoza.ModelLanac, Cvor: "Probni"}
	var objavljeno []*prognoza.Ishod
	k.objavi = func(_ context.Context, ishod *prognoza.Ishod) { objavljeno = append(objavljeno, ishod) }
	return k, &objavljeno
}

// Krug izda prognozu jednom po satu; „Generiraj” (SIznova) računa isti sat
// iznova i gazi staro izdanje cijelo, pa letva koja se više ne računa ne
// zadrži staru prognozu uz novo izdanje.
func TestGenerirajGaziStaroIzdanjeIstogSata(t *testing.T) {
	k, objavljeno := izdavackiKrug(t)
	ctx := context.Background()

	k.izracunajIIzdaj(ctx)
	if len(*objavljeno) != 1 {
		t.Fatalf("prvi krug: objavljeno %d izdanja, redci %q", len(*objavljeno), k.uvoznik.Napredak().Redci)
	}
	sat := (*objavljeno)[0].Sada
	// letva koju novi račun više ne daje, iz ranijeg izdanja istog sata
	if _, err := k.prognoze.Exec(`INSERT INTO izdane (letva, velicina, izdano, ciljni, vrijednost, dolje, gore, model)
		VALUES ('stara', 'vodostaj', ?, ?, 1, 0, 2, 'proba')`, sat, sat+1); err != nil {
		t.Fatal(err)
	}

	k.izracunajIIzdaj(ctx)
	if len(*objavljeno) != 1 {
		t.Fatal("isti sat izdan dvaput bez Generiraj")
	}
	if r := k.uvoznik.Napredak().Redci; !strings.Contains(strings.Join(r, "\n"), "za ovaj sat već je izdana") {
		t.Errorf("redci: %q", r)
	}

	k.izracunajIIzdaj(prognoza.SIznova(ctx))
	if len(*objavljeno) != 2 || (*objavljeno)[1].Sada != sat {
		t.Fatalf("Generiraj: objavljeno %d izdanja", len(*objavljeno))
	}
	var stare int
	if err := k.prognoze.QueryRow(`SELECT count(*) FROM izdane WHERE letva = 'stara' AND izdano = ?`, sat).Scan(&stare); err != nil {
		t.Fatal(err)
	}
	if stare != 0 {
		t.Error("Generiraj nije pogazio staro izdanje istog sata: letva koja se više ne računa zadržala je staru prognozu")
	}
}
