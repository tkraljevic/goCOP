package prognoza

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// probneOcitanja slaže malu bazu očitanja s dvije letve.
func probneOcitanja(t *testing.T, sati int, zadnji time.Time) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE stations (id TEXT PRIMARY KEY, code TEXT);
		CREATE TABLE readings (station_id TEXT, measured_at DATETIME, level_cm INTEGER, flow_m3s REAL);
		INSERT INTO stations VALUES ('1','gornja'),('2','donja');`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < sati; i++ {
		kad := zadnji.Add(-time.Duration(i) * time.Hour)
		for _, x := range []struct {
			id string
			cm int
		}{{"1", 100 + i}, {"2", 90 + i}} {
			if _, err := db.Exec(`INSERT INTO readings (station_id, measured_at, level_cm)
				VALUES (?,?,?)`, x.id, kad, x.cm); err != nil {
				t.Fatal(err)
			}
		}
	}
	return db
}

func probniOsvjezivac(t *testing.T, ocitanja *sql.DB) *Osvjezivac {
	t.Helper()
	baza, err := Otvori(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	pojasi := []Pojas{{Letva: "donja", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
		Ulazi: []Ulaz{{Letva: "gornja", Velicina: "vodostaj", PomakH: 3, Sirina: 1, Nagib: 1}}}}
	if err := Spremi(baza, pojasi, "proba"); err != nil {
		t.Fatal(err)
	}
	return &Osvjezivac{Baza: baza, Ocitanja: ocitanja, Najdalje: 12, Model: ModelLanac}
}

// Prvo osvježavanje računa i zapisuje.
func TestOsvjezavanjeIzdajePrognozu(t *testing.T) {
	PoluvijekIspravka = 0
	zadnji := time.Now().UTC().Truncate(time.Hour)
	o := probniOsvjezivac(t, probneOcitanja(t, 48, zadnji))
	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ishod.Preskoceno {
		t.Fatal("prvo osvježavanje je preskočeno")
	}
	if ishod.Sada != zadnji.Unix()/3600 {
		t.Errorf("izdano za %d, a zadnje očitanje je %d", ishod.Sada, zadnji.Unix()/3600)
	}
	if len(ishod.Izdane) == 0 {
		t.Fatal("ništa nije izračunato")
	}
	if err := o.Zapisi(ishod); err != nil {
		t.Fatal(err)
	}
	if kad, ima, _ := ZadnjeIzdanje(o.Baza); !ima || kad != ishod.Sada {
		t.Errorf("zapisano izdanje %d, očekivano %d", kad, ishod.Sada)
	}
}

// Ista očitanja daju istu prognozu, pa se drugi put ne računa ništa: ponovni
// upis bio bi samo trošak, a poslužitelj to zove svaki sat.
func TestOsvjezavanjeNePonavljaIstiSat(t *testing.T) {
	PoluvijekIspravka = 0
	zadnji := time.Now().UTC().Truncate(time.Hour)
	o := probniOsvjezivac(t, probneOcitanja(t, 48, zadnji))
	prvi, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Zapisi(prvi); err != nil {
		t.Fatal(err)
	}
	drugi, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !drugi.Preskoceno {
		t.Error("isti sat je preračunat iznova")
	}
	if len(drugi.Izdane) != 0 {
		t.Errorf("preskočeno osvježavanje ipak je dalo %d vrijednosti", len(drugi.Izdane))
	}
	// Zapisi na preskočenom ishodu ne smije ništa dirati.
	if err := o.Zapisi(drugi); err != nil {
		t.Fatal(err)
	}
}

// Novo očitanje pomiče sat izdavanja i prognoza se računa iznova.
func TestNovoOcitanjePomicePrognozu(t *testing.T) {
	PoluvijekIspravka = 0
	zadnji := time.Now().UTC().Truncate(time.Hour)
	ocitanja := probneOcitanja(t, 48, zadnji)
	o := probniOsvjezivac(t, ocitanja)
	prvi, _ := o.Osvjezi(context.Background())
	if err := o.Zapisi(prvi); err != nil {
		t.Fatal(err)
	}
	novi := zadnji.Add(time.Hour)
	for _, x := range []struct {
		id string
		cm int
	}{{"1", 99}, {"2", 89}} {
		if _, err := ocitanja.Exec(`INSERT INTO readings (station_id, measured_at, level_cm)
			VALUES (?,?,?)`, x.id, novi, x.cm); err != nil {
			t.Fatal(err)
		}
	}
	drugi, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if drugi.Preskoceno {
		t.Fatal("novo očitanje nije pomaknulo prognozu")
	}
	if drugi.Sada != prvi.Sada+1 {
		t.Errorf("izdano za %d, očekivano %d", drugi.Sada, prvi.Sada+1)
	}
}

// Bez svježih očitanja račun mora reći zašto ne ide, a ne izdati prognozu iz
// ničega.
func TestBezOcitanjaNemaPrognoze(t *testing.T) {
	PoluvijekIspravka = 0
	o := probniOsvjezivac(t, probneOcitanja(t, 0, time.Now().UTC()))
	if _, err := o.Osvjezi(context.Background()); err == nil {
		t.Error("prognoza izdana bez ijednog očitanja")
	}
}

// Dnevnik mora javljati koliko je letvi prognoza dotaknula i koliko je izdanje
// staro. Izvori objavljuju sa zakašnjenjem, pa se izdaje za zadnji sat koji
// imaju sve ulazne letve, a dežurni mora vidjeti koliko je to star podatak.
func TestIshodBrojiLetveIZaostatak(t *testing.T) {
	PoluvijekIspravka = 0
	zadnji := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	o := probniOsvjezivac(t, probneOcitanja(t, 48, zadnji))
	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Dvije: donja se prognozira, gornja dobiva sidro u satu izdavanja jer je
	// vrh lanca i treba stajati na uzdužnom profilu.
	if n := ishod.Letvi(); n != 2 {
		t.Errorf("%d letvi, očekivane dvije (prognozirana i usidrena)", n)
	}
	if z := ishod.Zaostatak(time.Now()); z != 3*time.Hour {
		t.Errorf("zaostatak %v, očekivano 3h", z)
	}
}

// Nakon namještanja isti sat treba preračunati, inače se učinak novih pojasa
// ne vidi do idućeg očitanja. Zastavica je jednom već bila mrtva: naredba ju
// je imala, Osvjezivac nije, pa je -iznova samo ispisivala praznu tablicu.
func TestIznovaPreracunavaIstiSat(t *testing.T) {
	PoluvijekIspravka = 0
	zadnji := time.Now().UTC().Truncate(time.Hour)
	o := probniOsvjezivac(t, probneOcitanja(t, 48, zadnji))
	prvi, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Zapisi(prvi); err != nil {
		t.Fatal(err)
	}
	o.Iznova = true
	drugi, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if drugi.Preskoceno {
		t.Fatal("-iznova je ipak preskočilo isti sat")
	}
	if len(drugi.Izdane) != len(prvi.Izdane) {
		t.Errorf("preračunato %d vrijednosti, prvi put %d",
			len(drugi.Izdane), len(prvi.Izdane))
	}
}

// Izbor inačice: kad glavni ulaz zakaže, karika prelazi na rezervu; ulaz koji
// sam ima račun vrijedi kao dostupan i kad mu je mjerenje staro, jer dolazi
// izračunat; kad je sve svježe, ostaje glavna.
func TestOdaberiInacicePrelaziNaRezervu(t *testing.T) {
	pojas := func(letva string, inacica int, ulazi ...string) []Pojas {
		var u []Ulaz
		for _, x := range ulazi {
			u = append(u, Ulaz{Letva: x, Velicina: "vodostaj", Nagib: 1})
		}
		return []Pojas{{Letva: letva, Velicina: "vodostaj", Od: -1000, Do: 1000, Ulazi: u, Inacica: inacica, R: 0.9, Rasap: 5}}
	}
	inacice := map[string][][]Pojas{
		"batina": {pojas("batina", 0, "mohacs"), pojas("batina", 1, "dunaszekcso")},
		"aljmas": {pojas("aljmas", 0, "batina"), pojas("aljmas", 1, "bezdan")},
	}
	niz := func(do int64) Niz { return ravanNiz(do-200, do, 100, 0) }
	iz := func(l string) Izvor { return Izvor{l, "vodostaj"} }
	// Sve svježe: glavne inačice.
	svjeze := map[Izvor]Niz{iz("mohacs"): niz(1000), iz("dunaszekcso"): niz(1000), iz("batina"): niz(1000), iz("bezdan"): niz(1000), iz("aljmas"): niz(1000)}
	odabrano, izbor := OdaberiInacice(inacice, svjeze, 0)
	if len(izbor) != 0 || odabrano["batina"][0].Inacica != 0 {
		t.Errorf("sa svježim ulazima uzeta je rezerva: %+v", izbor)
	}
	// Mohács stao prije 12 h: Batina ide iz Dunaszekcsőa; Aljmaš ostaje na
	// Batini, jer Batina ima svoj račun pa dolazi izračunata.
	mohacsStao := map[Izvor]Niz{iz("mohacs"): niz(988), iz("dunaszekcso"): niz(1000), iz("batina"): niz(990), iz("bezdan"): niz(1000), iz("aljmas"): niz(1000)}
	odabrano, izbor = OdaberiInacice(inacice, mohacsStao, 0)
	if odabrano["batina"][0].Inacica != 1 || izbor["batina"].Inacica != 1 {
		t.Errorf("Batina nije prešla na rezervu: %+v", izbor)
	}
	if odabrano["aljmas"][0].Inacica != 0 {
		t.Errorf("Aljmaš prešao na rezervu iako Batina ima račun: %+v", izbor)
	}
	if izbor["batina"].Opis != "rezerva: dunaszekcso umjesto mohacs" {
		t.Errorf("opis izbora: %q", izbor["batina"].Opis)
	}
	// Ništa svježe za glavnu ni rezervu, a Batina sama svježa: Batina je za
	// ovo izdanje vrh (bez računa), Aljmaš ide iz nje kao iz vrha.
	sveStalo := map[Izvor]Niz{iz("mohacs"): niz(900), iz("dunaszekcso"): niz(900), iz("batina"): niz(1000), iz("bezdan"): niz(900), iz("aljmas"): niz(1000)}
	odabrano, izbor = OdaberiInacice(inacice, sveStalo, 0)
	if _, ima := odabrano["batina"]; ima || izbor["batina"].Inacica != -1 {
		t.Errorf("bez ijednog svježeg ulaza Batina je trebala postati vrh: %+v", izbor)
	}
	if odabrano["aljmas"][0].Inacica != 0 {
		t.Errorf("Aljmaš iz vrha Batine: %+v", izbor)
	}
	// Isto, ali ni Batina nije svježa: ostaje glavna, bez zapisa izbora.
	sveStaro := map[Izvor]Niz{iz("mohacs"): niz(900), iz("dunaszekcso"): niz(900), iz("batina"): niz(900), iz("bezdan"): niz(900), iz("aljmas"): niz(1000)}
	odabrano, izbor = OdaberiInacice(inacice, sveStaro, 0)
	if odabrano["batina"][0].Inacica != 0 || len(izbor) != 0 {
		t.Errorf("bez ičega svježeg trebala je ostati glavna: %+v", izbor)
	}
	// U prošlom satu (provjera unatrag): svježina se mjeri prema tom satu.
	odabrano, izbor = OdaberiInacice(inacice, mohacsStao, 985)
	if odabrano["batina"][0].Inacica != 0 || len(izbor) != 0 {
		t.Errorf("u satu 985 Mohács je bio svjež, a uzeta je rezerva: %+v", izbor)
	}
}

// Tuđa prognoza ispred računa: srednja letva ima svoj račun iz gornje, ali
// dok je njezina tuđa prognoza svježa, postaje vrh i slijedi nju; donja se
// računa iz nje. Bez tuđe prognoze srednja se računa kao i dosad.
func TestTudaIspredRacunaSkidaRacunDokJeSvjeza(t *testing.T) {
	PoluvijekIspravka = 0
	zadnji := time.Now().UTC().Truncate(time.Hour)
	ocitanja := probneOcitanja(t, 48, zadnji)
	if _, err := ocitanja.Exec(`INSERT INTO stations VALUES ('3','srednja')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 48; i++ {
		if _, err := ocitanja.Exec(`INSERT INTO readings (station_id, measured_at, level_cm) VALUES ('3',?,?)`,
			zadnji.Add(-time.Duration(i)*time.Hour), 95); err != nil {
			t.Fatal(err)
		}
	}
	baza, err := Otvori(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	pojasi := []Pojas{
		{Letva: "srednja", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
			Ulazi: []Ulaz{{Letva: "gornja", Velicina: "vodostaj", PomakH: 3, Sirina: 1, Nagib: 1}}},
		{Letva: "donja", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
			Ulazi: []Ulaz{{Letva: "srednja", Velicina: "vodostaj", PomakH: 2, Sirina: 1, Nagib: 1}}},
	}
	if err := Spremi(baza, pojasi, "proba"); err != nil {
		t.Fatal(err)
	}
	o := &Osvjezivac{Baza: baza, Ocitanja: ocitanja, Najdalje: 12, Model: ModelLanac}
	staro := TudaIspredRacuna
	TudaIspredRacuna = map[string]string{"srednja": "probni-izvor"}
	defer func() { TudaIspredRacuna = staro }()

	// bez tuđe prognoze: srednja se računa
	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	izdanih := func(ishod *Ishod, letva string) (n int, kraj *Izdana) {
		for i := range ishod.Izdane {
			if ishod.Izdane[i].Letva == letva {
				n++
				if ishod.Izdane[i].Ciljni == ishod.Sada+12 {
					kraj = &ishod.Izdane[i]
				}
			}
		}
		return n, kraj
	}
	if n, _ := izdanih(ishod, "srednja"); len(ishod.TudiVrhovi) != 0 || n < 2 {
		t.Fatalf("bez tuđe prognoze srednja mora imati račun: vrhovi %v, izdanih %d", ishod.TudiVrhovi, n)
	}

	// svježa tuđa prognoza: srednja raste 5 cm na sat
	sat := zadnji.Unix() / 3600
	for h := int64(0); h <= 12; h++ {
		if _, err := baza.Exec(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost) VALUES ('probni-izvor','srednja',?,?,?)`,
			sat, sat+h, 95+5*h); err != nil {
			t.Fatal(err)
		}
	}
	ishod, err = o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ishod.TudiVrhovi) != 1 || ishod.TudiVrhovi[0] != "srednja" {
		t.Fatalf("srednja mora biti tuđi vrh: %v", ishod.TudiVrhovi)
	}
	if iz := ishod.Izbor["vrh:srednja"]; iz.Inacica != 2 {
		t.Errorf("izbor vrh:srednja %+v, želim inačicu 2", iz)
	}
	if n, _ := izdanih(ishod, "srednja"); n > 1 {
		t.Errorf("srednja se ne smije računati dok je vrh, a ima %d izdanih", n)
	}
	_, kraj := izdanih(ishod, "donja")
	if kraj == nil || kraj.Vrijednost < 95+5*8 {
		t.Errorf("donja za 12 h mora slijediti porast tuđe prognoze srednje: %+v", kraj)
	}
}
