package main

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
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
		CREATE TABLE readings (station_id TEXT, measured_at DATETIME, level_cm INTEGER, flow_m3s REAL, source TEXT NOT NULL DEFAULT 'RUČNO');
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

// Krug koji izda prognozu upisuje u svoj dnevnik i upozorenja izdanja:
// dnevni cilj bez naučenog modela (čvor bez arhive) javlja se ondje gdje
// osoba koja prati krug gleda, a ne samo na stranici prognoza.
func TestKrugIzdanjaJavljaUpozorenja(t *testing.T) {
	k, objavljeno := izdavackiKrug(t)
	ciljevi := prognoza.DnevniCiljevi
	t.Cleanup(func() { prognoza.DnevniCiljevi = ciljevi })
	prognoza.DnevniCiljevi = []prognoza.DnevniCilj{{Letva: "donja"}}

	k.izracunajIIzdaj(context.Background())
	if len(*objavljeno) != 1 {
		t.Fatalf("objavljeno %d izdanja, redci %q", len(*objavljeno), k.uvoznik.Napredak().Redci)
	}
	if len((*objavljeno)[0].Upozorenja) == 0 {
		t.Fatal("izdanje bez upozorenja, a dnevnog modela nema")
	}
	redci := strings.Join(k.uvoznik.Napredak().Redci, "\n")
	if zelim := "upozorenje: dnevni; nema naučenog dnevnog modela; upitno: donja dnevna"; !strings.Contains(redci, zelim) {
		t.Errorf("redci:\n%s\nnema retka %q", redci, zelim)
	}
}

// zabraniZapisIzdanja: baza prognoza odbije svaki upis izračunatih
// vrijednosti, kao kad je disk pun
func zabraniZapisIzdanja(t *testing.T, pb *sql.DB) {
	t.Helper()
	if _, err := pb.Exec(`CREATE TRIGGER izdane_odbij BEFORE INSERT ON izdane
		BEGIN SELECT RAISE(ABORT, 'disk je pun'); END`); err != nil {
		t.Fatal(err)
	}
}

// Prognoza koja se ne da izračunati ili zapisati javi se u retku kruga i u
// dnevniku poslužitelja, a ne objavi se.
func TestIzdanjeKojeNijeUspjelo(t *testing.T) {
	dnevnik := dnevnikTesta(t)
	bezModela, _ := okolinaKruga(t, true) // prazna baza prognoza; objava ruši test
	bezModela.izracunajIIzdaj(context.Background())
	if r := redciS(bezModela.uvoznik.Napredak().Redci, "prognoza:"); len(r) != 1 || !strings.HasPrefix(r[0], "prognoza: baza prognoza je prazna") {
		t.Errorf("bez modela: %q", r)
	}

	k, objavljeno := izdavackiKrug(t)
	zabraniZapisIzdanja(t, k.prognoze)
	k.izracunajIIzdaj(context.Background())
	if len(*objavljeno) != 0 {
		t.Error("objavljeno izdanje koje se nije dalo zapisati")
	}
	if r := redciS(k.uvoznik.Napredak().Redci, "prognoza:"); len(r) != 1 || !strings.HasPrefix(r[0], "prognoza: zapis: ") || !strings.Contains(r[0], "disk je pun") {
		t.Errorf("zapis: %q", r)
	}
	for _, z := range []string{"prognoza: baza prognoza je prazna", "prognoza: zapis: "} {
		if !strings.Contains(dnevnik.String(), z) {
			t.Errorf("dnevnik nema %q:\n%s", z, dnevnik)
		}
	}
}

// sat6UTC je 14. 10. 2026. u 6 h UTC, u Zagrebu 8 h
var sat6UTC = time.Date(2026, 10, 14, 6, 0, 0, 0, time.UTC).Unix() / 3600

// Krug koji nije izdao novu prognozu kaže zašto: vrh lanca kasni (s letvom i
// zadnjim očitanjem) ili je ovaj sat već izdan.
func TestKrugKazeZastoNijeIzdao(t *testing.T) {
	_, uvoznik := okolinaKruga(t, true)
	dnevnik := dnevnikTesta(t)
	javiPreskoceno(uvoznik, &prognoza.Ishod{Sada: sat6UTC, Preskoceno: true, Ceka: map[string]int64{"gornja": sat6UTC - 5}})
	javiPreskoceno(uvoznik, &prognoza.Ishod{Sada: sat6UTC, Preskoceno: true})
	zelim := []string{"prognoza stoji na 14.10. u 08:00 jer kasni vrh lanca: gornja (zadnje 14.10. u 03:00)",
		"prognoza: za ovaj sat već je izdana, ništa novo"}
	if r := redciS(uvoznik.Napredak().Redci, "prognoza"); !reflect.DeepEqual(r, zelim) {
		t.Errorf("redci:\n%q\nželim\n%q", r, zelim)
	}
	if !strings.Contains(dnevnik.String(), "prognoza: prognoza stoji na 14.10. u 08:00 jer kasni vrh lanca") {
		t.Errorf("dnevnik: %s", dnevnik)
	}
}

// Izdana prognoza javi sat, broj letvi i vrijednosti s pravim oblikom riječi
// i svaku letvu koja nije dala nijedan sat, s razlogom.
func TestKrugJavljaIzdanje(t *testing.T) {
	_, uvoznik := okolinaKruga(t, true)
	dnevnik := dnevnikTesta(t)
	izdane := []prognoza.Izdana{{Letva: "gornja"}, {Letva: "gornja"}, {Letva: "donja"}}
	javiIzdanje(uvoznik, &prognoza.Ishod{Sada: sat6UTC, Izdane: izdane,
		BezPrognoze: map[string]error{"srednja": errors.New("nema svježeg ulaza")}})
	javiIzdanje(uvoznik, &prognoza.Ishod{Sada: sat6UTC, Izdane: izdane[:1]})
	zelim := []string{"prognoza izdana za 14.10. u 08:00, 2 letve, 3 vrijednosti", "bez prognoze srednja: nema svježeg ulaza",
		"prognoza izdana za 14.10. u 08:00, 1 letva, 1 vrijednost"}
	var r []string
	for _, x := range uvoznik.Napredak().Redci {
		if strings.HasPrefix(x, "prognoza izdana") || strings.HasPrefix(x, "bez prognoze") {
			r = append(r, x)
		}
	}
	if !reflect.DeepEqual(r, zelim) {
		t.Errorf("redci:\n%q\nželim\n%q", r, zelim)
	}
	if !strings.Contains(dnevnik.String(), "prognoza: izdana za 2026-10-14 06:00 UTC (") ||
		!strings.Contains(dnevnik.String(), "h unatrag), 2 letve, 3 vrijednosti") {
		t.Errorf("dnevnik: %s", dnevnik)
	}
}
