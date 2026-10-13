package prognoza

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// bazaSaShemom je baza sa shemom prognoza, otvorena bez usklađivanja, da se
// zatečeno stanje može složiti prije nego uskladi dođe na red.
func bazaSaShemom(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(shema); err != nil {
		t.Fatal(err)
	}
	return db
}

// izvrsi izvršava upite redom i ruši test na prvom koji ne prođe.
func izvrsi(t *testing.T, db *sql.DB, upiti ...string) {
	t.Helper()
	for _, q := range upiti {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// Baza nastala prije novih stupaca dograđuje se pri otvaranju: zapis o
// izdanjima dobiva stupce i čuva retke, a izračunate tablice bez novog
// stupca odbacuju se i grade iznova — pojasi i ulazi zajedno.
func TestOtvoriDogradujeStaruBazu(t *testing.T) {
	put := filepath.Join(t.TempDir(), "stara.db")
	stara, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	izvrsi(t, stara,
		`CREATE TABLE izdanja (izdano INTEGER NOT NULL, verzija INTEGER NOT NULL, nastalo INTEGER NOT NULL,
			model TEXT NOT NULL DEFAULT '', operateri TEXT NOT NULL DEFAULT '', tude TEXT NOT NULL DEFAULT '{}',
			izbor TEXT NOT NULL DEFAULT '{}', PRIMARY KEY (izdano, verzija))`,
		`INSERT INTO izdanja (izdano, verzija, nastalo) VALUES (100, 1, 360000)`,
		`CREATE TABLE pojasi (letva TEXT, velicina TEXT, pojas_od REAL, pojas_do REAL, odsjecak REAL,
			r REAL, rasap REAL, sati INTEGER)`,
		`INSERT INTO pojasi VALUES ('batina', 'vodostaj', 0, 900, 1, 0.9, 5, 3000)`,
		`CREATE TABLE ulazi (letva TEXT, velicina TEXT, pojas_od REAL, redni INTEGER, uzvodna TEXT,
			uz_velicina TEXT, pomak_h INTEGER, sirina_h INTEGER, nagib REAL, inacica INTEGER)`,
		`INSERT INTO ulazi VALUES ('batina', 'vodostaj', 0, 0, 'mohacs', 'vodostaj', 5, 1, 1, 0)`,
		`CREATE TABLE izdane (letva TEXT, velicina TEXT, izdano INTEGER, ciljni INTEGER, vrijednost REAL, model TEXT)`,
		`INSERT INTO izdane VALUES ('batina', 'vodostaj', 100, 101, 200, 'lanac-1')`)
	stara.Close()

	db, err := Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var cvor, knjiga, upozorenja string
	if err := db.QueryRow(`SELECT cvor, knjiga, upozorenja FROM izdanja WHERE izdano = 100`).Scan(&cvor, &knjiga, &upozorenja); err != nil ||
		cvor != "" || knjiga != "" || upozorenja != "" {
		t.Errorf("zapis o izdanju nakon dogradnje: %q %q %q, %v", cvor, knjiga, upozorenja, err)
	}
	for _, s := range []struct{ tablica, stupac string }{{"pojasi", "inacica"}, {"ulazi", "inacica"}, {"izdane", "gore"}} {
		if ima, err := imaStupac(db, s.tablica, s.stupac); err != nil || !ima {
			t.Errorf("%s nakon dogradnje nema %s (%v)", s.tablica, s.stupac, err)
		}
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM ` + s.tablica).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s: %d starih redaka (%v) — stara tablica se odbacuje", s.tablica, n, err)
		}
	}
	if err := Spremi(db, dvaPojasa(), "novo"); err != nil {
		t.Errorf("spremanje u dograđenu bazu: %v", err)
	}
	if err := SpremiIzdane(db, []Izdana{izdanaBatine(100, 101, 200)}); err != nil {
		t.Errorf("izdavanje u dograđenu bazu: %v", err)
	}
}

// Ulazi bez inačice odlaze zajedno s pojasima, iako pojasi inačicu imaju:
// bez ulaza pojas ništa ne znači.
func TestOtvoriOdbacujeUlazeIPojaseZajedno(t *testing.T) {
	put := filepath.Join(t.TempDir(), "stara.db")
	stara, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	izvrsi(t, stara, shema, `DROP TABLE ulazi`,
		`CREATE TABLE ulazi (letva TEXT, velicina TEXT, pojas_od REAL, redni INTEGER, uzvodna TEXT,
			uz_velicina TEXT, pomak_h INTEGER, sirina_h INTEGER, nagib REAL)`,
		`INSERT INTO ulazi VALUES ('batina', 'vodostaj', 0, 0, 'mohacs', 'vodostaj', 5, 1, 1)`,
		`INSERT INTO pojasi (letva, velicina, pojas_od, pojas_do, odsjecak, r, rasap, sati) VALUES ('batina', 'vodostaj', 0, 900, 1, 0.9, 5, 3000)`)
	stara.Close()
	db, err := Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if ima, err := imaStupac(db, "ulazi", "inacica"); err != nil || !ima {
		t.Errorf("ulazi nakon dogradnje bez inačice (%v)", err)
	}
	if p, err := SviPojasi(db); err != nil || len(p) != 0 {
		t.Errorf("pojasi nakon odbacivanja ulaza: %+v, %v", p, err)
	}
}

// Otvaranje javlja grešku kad se baza ne da napraviti ili kad se zatečena
// ne da dograditi, i tada ne vraća bazu.
func TestOtvoriJavljaGresku(t *testing.T) {
	if db, err := Otvori(filepath.Join(t.TempDir(), "nema-mape", "p.db")); err == nil || db != nil || !strings.Contains(err.Error(), "shema baze prognoza") {
		t.Errorf("baza u mapi koje nema: %v, %v", db, err)
	}

	put := filepath.Join(t.TempDir(), "pogled.db")
	stara, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	// Zapis o izdanjima kao pogled: stupac mu se ne da dodati.
	izvrsi(t, stara, `CREATE VIEW izdanja AS SELECT 1 AS izdano, 1 AS verzija`)
	stara.Close()
	if db, err := Otvori(put); err == nil || db != nil || !strings.Contains(err.Error(), "izdanja.cvor") {
		t.Errorf("zapis o izdanjima koji se ne da dograditi: %v, %v", db, err)
	}
}

// Usklađivanje javlja koji korak nije prošao: čitanje stupaca, uklanjanje
// stare tablice ili ponovna gradnja.
func TestUskladiJavljaGresku(t *testing.T) {
	// Greška dolazi već iz čitanja stupaca, a ne tek iz dodavanja stupca.
	t.Run("zatvorena baza", func(t *testing.T) {
		db := bazaSaShemom(t)
		db.Close()
		if err := uskladi(db); err == nil || strings.HasPrefix(err.Error(), "izdanja.") || !strings.Contains(err.Error(), "database is closed") {
			t.Errorf("greška %v, želim grešku čitanja stupaca zatvorene baze", err)
		}
	})
	t.Run("stara tablica je pogled", func(t *testing.T) {
		db := bazaSaShemom(t)
		izvrsi(t, db, `DROP TABLE dnevne`, `CREATE VIEW dnevne AS SELECT 1 AS letva`)
		if err := uskladi(db); err == nil || !strings.Contains(err.Error(), "uklanjanje stare tablice dnevne") {
			t.Errorf("greška %v", err)
		}
	})
	t.Run("shema se ne da složiti", func(t *testing.T) {
		db := bazaSaShemom(t)
		// Tuđe prognoze kao pogled imaju sve stupce, ali indeks na njima
		// ne prolazi, pa ponovna gradnja izdanih pukne.
		izvrsi(t, db, `DROP TABLE tude`,
			`CREATE VIEW tude AS SELECT 'a' AS izvor, 'b' AS letva, 1 AS izdano, 2 AS ciljni, 3.0 AS vrijednost`,
			`DROP TABLE izdane`, `CREATE TABLE izdane (letva TEXT, ciljni INTEGER)`)
		if err := uskladi(db); err == nil || !strings.Contains(err.Error(), "ponovna gradnja izdane") {
			t.Errorf("greška %v", err)
		}
	})
}

// Zatvorena baza: svako čitanje i upis vraća grešku, a ne prazan rezultat.
func TestBazaPrognozaZatvorenaJavljaGresku(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	db.Close()
	izd := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	for ime, f := range map[string]func() error{
		"Spremi":       func() error { return Spremi(db, dvaPojasa(), "x") },
		"InaciceLetve": func() error { _, err := InaciceLetve(db, "aljmas"); return err },
		"SveInacice":   func() error { _, err := SveInacice(db); return err },
		"SpremiIzbor":  func() error { return SpremiIzbor(db, 1, map[string]Izbor{"aljmas": {1, "rezerva"}}) },
		"IzborIzdanja": func() error { _, err := IzborIzdanja(db, 1); return err },
		"SpremiIzdane": func() error { return SpremiIzdane(db, []Izdana{izdanaBatine(1, 2, 3)}) },
		"SpremiZapisIzdanja": func() error {
			_, err := SpremiZapisIzdanja(db, ZapisIzdanja{Izdano: 1})
			return err
		},
		"SpremiDnevne": func() error { return SpremiDnevne(db, []DnevnaIzdana{{Letva: "batina", Dan: 1}}) },
		"SpremiTude": func() error {
			_, err := SpremiTude(db, Podrijetlo, []Letva{{Naziv: "Mohács", Izdano: izd}}, Sifra)
			return err
		},
		"ZadnjeIzdanje": func() error {
			_, _, err := ZadnjeIzdanje(db)
			return err
		},
		"SpremiPromasaje": func() error { return SpremiPromasaje(db, nil, "x") },
		"Promasaji":       func() error { _, err := Promasaji(db); return err },
	} {
		if err := f(); err == nil {
			t.Errorf("%s: zatvorena baza bez greške", ime)
		}
	}
}

// Spremanje pojasa ide sve ili ništa: kad bilo koji upis ne prođe, javlja
// grešku s letvom, a zatečeni pojasi ostaju.
func TestSpremiNeSpremaNapola(t *testing.T) {
	novi := []Pojas{
		{Letva: "dalj", Velicina: "vodostaj", Od: 0, Do: 500, Rasap: 3,
			Ulazi: []Ulaz{{Letva: "aljmas", Velicina: "vodostaj", PomakH: 3, Sirina: 1, Nagib: 1}}},
	}
	for _, s := range []struct {
		ime    string
		pojasi []Pojas
		upiti  []string
		greska string
	}{
		{ime: "isti pojas dvaput", pojasi: append(novi, novi[0]), greska: "pojas dalj 0: "},
		{ime: "ulazi bez prozora", pojasi: novi, greska: "ulaz dalj ← aljmas: ",
			upiti: []string{`DROP TABLE ulazi`, `CREATE TABLE ulazi (letva TEXT, velicina TEXT, pojas_od REAL, redni INTEGER,
				uzvodna TEXT, uz_velicina TEXT, pomak_h INTEGER, nagib REAL, inacica INTEGER)`,
				`INSERT INTO ulazi VALUES ('batina', 'vodostaj', 0, 0, 'bezdan', 'vodostaj', 1, 1, 0)`}},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db := otvoriProbnu(t, "p.db")
			if err := Spremi(db, dvaPojasa(), "staro"); err != nil {
				t.Fatal(err)
			}
			izvrsi(t, db, s.upiti...)
			if err := Spremi(db, s.pojasi, "novo"); err == nil || !strings.HasPrefix(err.Error(), s.greska) {
				t.Errorf("greška %v, želim %q", err, s.greska)
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM pojasi WHERE namjesteno = 'staro'`).Scan(&n); err != nil || n != 3 {
				t.Errorf("zatečenih pojasa nakon neuspjeha %d (%v), želim 3", n, err)
			}
		})
	}
	db := otvoriProbnu(t, "p.db")
	izvrsi(t, db, `DROP TABLE pojasi`)
	if err := Spremi(db, novi, "novo"); err == nil || !strings.HasPrefix(err.Error(), "čišćenje pojasi") {
		t.Errorf("bez tablice pojasa: %v", err)
	}
}

// Inačice letve čitaju se cijele ili nikako: redni broj inačice koji nije
// broj, upit koji pukne usred čitanja i rezerva koja se ne da pročitati
// javljaju grešku — i SveInacice, iako se glavne dadu pročitati.
func TestInaciceLetveJavljajuGresku(t *testing.T) {
	sRezervom := func(t *testing.T) *sql.DB {
		t.Helper()
		db := otvoriProbnu(t, "p.db")
		p := dvaPojasa()
		rezerva := p[2]
		rezerva.Inacica = 1
		if err := Spremi(db, append(p, rezerva), "proba"); err != nil {
			t.Fatal(err)
		}
		return db
	}
	db := sRezervom(t)
	if in, err := InaciceLetve(db, "batina"); err != nil || len(in) != 2 {
		t.Fatalf("inačice: %+v, %v", in, err)
	}
	if sve, err := SveInacice(db); err != nil || len(sve["batina"]) != 2 || len(sve["aljmas"]) != 1 {
		t.Fatalf("sve inačice: %+v, %v", sve, err)
	}

	for _, s := range []struct {
		ime     string
		pokvari func(t *testing.T, db *sql.DB)
		sve     bool // SveInacice mora pasti
	}{
		{ime: "inačica nije broj", pokvari: func(t *testing.T, db *sql.DB) {
			izvrsi(t, db, `UPDATE pojasi SET inacica = 'x' WHERE letva = 'batina' AND inacica = 1`)
		}},
		{ime: "upit pukne na rezervi", pokvari: func(t *testing.T, db *sql.DB) {
			zamijeniStupac(t, db, "pojasi", "inacica", pucaNa("inacica = 1", "inacica"), "letva", "inacica", "pojas_od")
		}},
		// Pogled koji drugi pojas rezerve propusti tek pri čitanju: glavna se
		// pročita, a čitanje pukne iza nje, ne već pri slaganju upita.
		{ime: "čitanje pukne iza glavne", pokvari: func(t *testing.T, db *sql.DB) {
			izvrsi(t, db, `INSERT INTO pojasi (letva, velicina, pojas_od, pojas_do, odsjecak, r, rasap, sati, inacica)
					VALUES ('batina', 'vodostaj', 900, 1200, 0, 0.9, 6, 100, 1)`,
				`ALTER TABLE pojasi RENAME TO pojasi_izvorna`,
				`CREATE INDEX pojasi_izvorna_redom ON pojasi_izvorna(letva, inacica, pojas_od)`,
				`CREATE VIEW pojasi AS SELECT * FROM pojasi_izvorna WHERE `+pucaNa("inacica = 1 AND pojas_od = 900", "1"))
		}},
		{ime: "rezerva se ne da pročitati", sve: true, pokvari: func(t *testing.T, db *sql.DB) {
			izvrsi(t, db, `UPDATE pojasi SET rasap = 'x' WHERE letva = 'batina' AND inacica = 1`)
		}},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db := sRezervom(t)
			s.pokvari(t, db)
			if in, err := InaciceLetve(db, "batina"); err == nil {
				t.Errorf("bez greške: %+v", in)
			}
			if s.sve {
				if sve, err := SveInacice(db); err == nil {
					t.Errorf("SveInacice bez greške: %+v", sve)
				}
			}
		})
	}
}

// Kad se ulazi pojasa ne daju pročitati — nema tablice ili kašnjenje nije
// broj — pojas se ne vraća bez njih.
func TestUlaziPojasaJavljajuGresku(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	if err := Spremi(db, dvaPojasa(), "proba"); err != nil {
		t.Fatal(err)
	}
	izvrsi(t, db, `UPDATE ulazi SET pomak_h = 'x' WHERE letva = 'batina'`)
	if p, err := ZaLetvu(db, "batina"); err == nil {
		t.Errorf("kašnjenje koje nije broj: %+v", p)
	}
	if p, err := ZaLetvu(db, "aljmas"); err != nil || len(p) != 2 {
		t.Errorf("druga letva: %+v, %v", p, err)
	}
	izvrsi(t, db, `ALTER TABLE ulazi RENAME TO ulazi_stari`)
	if u, err := ulaziPojasa(db, dvaPojasa()[0]); err == nil {
		t.Errorf("bez tablice ulaza: %+v", u)
	}
}

// Izbor izdanja: upis bez tablice i redak koji se ne da pročitati su greška.
func TestIzborIzdanjaJavljaGresku(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	if err := SpremiIzbor(db, 7, map[string]Izbor{"batina": {1, "rezerva"}}); err != nil {
		t.Fatal(err)
	}
	izvrsi(t, db, `UPDATE izbor SET inacica = 'x'`)
	if i, err := IzborIzdanja(db, 7); err == nil {
		t.Errorf("inačica koja nije broj: %+v", i)
	}
	izvrsi(t, db, `DROP TABLE izbor`)
	if err := SpremiIzbor(db, 8, map[string]Izbor{"batina": {1, "rezerva"}}); err == nil {
		t.Error("upis izbora bez tablice bez greške")
	}
}

// Izdane i dnevne prognoze: upis bez tablice je greška koja kaže koju
// letvu nije upisao.
func TestUpisIzdanihJavljaGresku(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	izvrsi(t, db, `DROP TABLE izdane`, `DROP TABLE dnevne`)
	if err := SpremiIzdane(db, []Izdana{izdanaBatine(100, 101, 200)}); err == nil || !strings.HasPrefix(err.Error(), "izdana batina za 101: ") {
		t.Errorf("izdane bez tablice: %v", err)
	}
	if err := SpremiDnevne(db, []DnevnaIzdana{{Letva: "batina", Izdano: 100, Dan: 2, Model: ModelDnevni}}); err == nil ||
		!strings.HasPrefix(err.Error(), "dnevna batina, 2. dan: ") {
		t.Errorf("dnevne bez tablice: %v", err)
	}
}

// Zapis izdanja ne nastaje napola: kad ne prođe čitanje verzije, tuđih
// prognoza, upis zapisa ili kiše, javlja grešku i ništa ne ostaje.
func TestSpremiZapisIzdanjaJavljaGresku(t *testing.T) {
	kisa := map[string]DnevniNiz{"A": {0: 3, 1: 5}}
	for _, s := range []struct {
		ime   string
		upiti []string
	}{
		{ime: "nema zapisa o izdanjima", upiti: []string{`DROP TABLE izdanja`}},
		{ime: "nema tuđih prognoza", upiti: []string{`DROP TABLE tude`}},
		{ime: "sat tuđeg izdanja nije cijeli broj", upiti: []string{
			`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost) VALUES ('hydroinfo.hu', 'batina', 1.5, 30, 200)`}},
		{ime: "zapis bez stupaca", upiti: []string{`DROP TABLE izdanja`, `CREATE TABLE izdanja (izdano INTEGER, verzija INTEGER)`}},
		{ime: "nema mjesta za kišu", upiti: []string{`DROP TABLE kisa_izdanja`}},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db := otvoriProbnu(t, "p.db")
			izvrsi(t, db, s.upiti...)
			if v, err := SpremiZapisIzdanja(db, ZapisIzdanja{Izdano: 100, Kisa: kisa}); err == nil {
				t.Fatalf("bez greške, verzija %d", v)
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM izdanja`).Scan(&n); err == nil && n != 0 {
				t.Errorf("nakon neuspjeha ostalo %d zapisa", n)
			}
		})
	}
}

// Tuđa prognoza: postaja bez izdanja i postaja bez imena se preskaču, a
// jutarnje mjerenje iz njihove tablice zapisuje se kao vrijednost u 7 h
// dana izdanja po našem vremenu. Upis bez tablice je greška.
func TestSpremiTudeJutarnjeMjerenje(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	izd := time.Date(2026, 9, 23, 9, 8, 0, 0, time.UTC) // 11:08 po ljetnom
	sutra := izd.Add(22 * time.Hour)
	letve := []Letva{
		{Naziv: "Mohács", Izdano: izd, Danas: 412, ImaDanas: true, Dani: []Dan{{Kad: sutra, Cm: 420, PlusMin: 8}}},
		{Naziv: "Baja"}, // bez izdanja
		{Naziv: "***", Izdano: izd, Dani: []Dan{{Kad: sutra, Cm: 1}}}, // bez imena od kojeg bi bila šifra
	}
	n, err := SpremiTude(db, Podrijetlo, letve, Sifra)
	if err != nil || n != 2 {
		t.Fatalf("upisano %d, %v; želim jutarnje mjerenje i jedan dan Mohácsa", n, err)
	}
	jutro := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC).Unix() / 3600
	r, err := db.Query(`SELECT letva, izdano, ciljni, vrijednost, raspon FROM tude ORDER BY ciljni`)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var got [][5]any
	for r.Next() {
		var letva string
		var izdano, ciljni, v, raspon int64
		if err := r.Scan(&letva, &izdano, &ciljni, &v, &raspon); err != nil {
			t.Fatal(err)
		}
		got = append(got, [5]any{letva, izdano, ciljni, v, raspon})
	}
	sat := izd.Unix() / 3600
	want := [][5]any{{"mohacs", sat, jutro, int64(412), int64(0)}, {"mohacs", sat, sutra.Unix() / 3600, int64(420), int64(8)}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tuđe vrijednosti %v, želim %v", got, want)
	}

	izvrsi(t, db, `DROP TABLE tude`)
	if _, err := SpremiTude(db, Podrijetlo, letve[:1], Sifra); err == nil || !strings.HasPrefix(err.Error(), "hydroinfo.hu mohacs: ") {
		t.Errorf("upis bez tablice: %v", err)
	}
}

// Jedna točka daje niz od jednog sata, a ne prazan niz.
func TestNizIzJedneTocke(t *testing.T) {
	n := NizIzTocaka(map[int64]float64{50: 7})
	if v, ok := n.U(50); !ok || v != 7 || len(n.Sati) != 1 {
		t.Errorf("niz %+v", n)
	}
	if n := NizIzTocaka(nil); len(n.Sati) != 0 {
		t.Errorf("bez točaka: %+v", n)
	}
}

// Promašaj koji se ne da pročitati je greška, a ne prazan popis.
func TestPromasajiJavljajuGresku(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	if err := SpremiPromasaje(db, []Promasaj{{Letva: "batina", Velicina: "vodostaj", DosegH: 6, Rasap: 3, Postojanost: 5, Slucaja: 100}}, "x"); err != nil {
		t.Fatal(err)
	}
	izvrsi(t, db, `UPDATE promasaji SET pomak = 'x'`)
	if p, err := Promasaji(db); err == nil {
		t.Errorf("bez greške: %+v", p)
	}
}
