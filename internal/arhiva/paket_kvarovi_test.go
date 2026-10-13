package arhiva

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// izvornaLetva slaže arhivu s jednom letvom koja ima sve što paket nosi:
// niz, krivulju, snimku korita koja se ne crta i promjenu kote nule.
func izvornaLetva(t *testing.T) *sql.DB {
	t.Helper()
	db := napraviArhivu(t, filepath.Join(t.TempDir(), "izvor.db"))
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{
		`INSERT INTO nizovi (id, zona, sliv, letva, izvor, velicina, vrsta, od, do_, zapisa)
			VALUES (1, 'UTC', 'dunav', 'vodograd', 'cop', 'vodostaj', 'satni', '2024-03-01', '2024-03-01', 2)`,
		`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (1, 1709276400, 120), (1, 1709280000, 121)`,
		`INSERT INTO hq_krivulje (id, letva, vrijedi_od, izvor) VALUES (1, 'vodograd', '2001-01-01', 'DHMZ')`,
		`INSERT INTO hq_odsjecci (krivulja, od_cm, do_cm, oblik, p1, p2, p3) VALUES (1, -100, 600, 'polinom', 1, 2, 3)`,
		`INSERT INTO profili (id, letva, datum, vodostaj, kota_nule, pomak_m, crtaj)
			VALUES (1, 'vodograd', '2010-03-22', 174, 80.45, 0, 0)`,
		`INSERT INTO profil_tocke (profil, stacionaza, visina) VALUES (1, 0, 90), (1, 6.5, 87)`,
		`INSERT INTO promjene_kote (letva, datum, pomak_cm, izvor) VALUES ('vodograd', '1943-01-01', 200, 'elaborat')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return db
}

// Zaključava da izvoz iz arhive koja se ne da cijela pročitati javlja grešku
// i ne piše ništa: pola paketa gore je od nikakvog.
func TestIzvozIzPokvareneArhive(t *testing.T) {
	for _, s := range []struct {
		ime, kvar, greska string
	}{
		{"tablica nizova nedostaje", `DROP TABLE nizovi`, "no such table: nizovi"},
		{"nečitljiv opis niza", `UPDATE nizovi SET po_danu = 'x'`, `name "po_danu"`},
		{"tablica očitanja nedostaje", `DROP TABLE ocitanja`, "no such table: ocitanja"},
		{"nečitljivo očitanje", `UPDATE ocitanja SET vrijednost = 'x'`, `name "vrijednost"`},
		{"tablica krivulja nedostaje", `DROP TABLE hq_krivulje`, "no such table: hq_krivulje"},
		{"tablica odsječaka nedostaje", `DROP TABLE hq_odsjecci`, "no such table: hq_odsjecci"},
		{"nečitljiv odsječak", `UPDATE hq_odsjecci SET p1 = 'x'`, `name "p1"`},
		{"tablica snimki nedostaje", `DROP TABLE profili`, "no such table: profili"},
		{"nečitljiva snimka", `UPDATE profili SET vodostaj = 'x'`, `name "vodostaj"`},
		{"tablica točaka nedostaje", `DROP TABLE profil_tocke`, "no such table: profil_tocke"},
		{"nečitljiva točka", `UPDATE profil_tocke SET visina = 'x'`, `name "visina"`},
		{"tablica promjena kote nedostaje", `DROP TABLE promjene_kote`, "no such table: promjene_kote"},
		{"nečitljiva promjena kote", `UPDATE promjene_kote SET pomak_cm = 'x'`, `name "pomak_cm"`},
		{"tablica izvora nedostaje", `DROP TABLE izvori`, "čitanje izvora"},
		{"beskonačan član krivulje", `UPDATE hq_odsjecci SET p1 = 1e999`, "unsupported value: +Inf"},
		{"beskonačna kota nule", `UPDATE profili SET kota_nule = 1e999`, "unsupported value: +Inf"},
		{"beskonačna točnost izvora", `UPDATE izvori SET tocnost = 1e999 WHERE naziv = 'cop'`, "unsupported value: +Inf"},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db := izvornaLetva(t)
			if _, err := db.Exec(s.kvar); err != nil {
				t.Fatal(err)
			}
			var b bytes.Buffer
			_, err := Izvezi(db, "vodograd", 1, "cvor-proba", &b)
			greskaSadrzi(t, s.ime, err, s.greska)
			if b.Len() != 0 {
				t.Errorf("uz grešku je zapisano %d bajta paketa", b.Len())
			}
		})
	}

	db := izvornaLetva(t)
	var b bytes.Buffer
	_, err := Izvezi(db, "kamenjak", 1, "cvor-proba", &b)
	greskaSadrzi(t, "letva bez nizova", err, `letva "kamenjak" nema nizova u arhivi`)
}

// Zaključava da izvoz u odredište koje usred pisanja odbije javlja grešku
// pisanja. Niz je dovoljno dug da paket ne stane u međuspremnik ZIP-a.
func TestIzvozUPokvarenoOdrediste(t *testing.T) {
	db := napraviArhivu(t, filepath.Join(t.TempDir(), "izvor.db"))
	defer db.Close()
	id := upisiNiz(t, db, "vodograd", "cop", "vodostaj", "satni")
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	st, err := tx.Prepare(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewPCG(1, 2))
	for i := range 20000 {
		if _, err := st.Exec(id, int64(i)*3600, float64(r.IntN(1500)-500)); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := Izvezi(db, "vodograd", 1, "cvor-proba", &pokvarenPisac{}); !errors.Is(err, errPisanja) {
		t.Errorf("greška %v, očekivana greška pisanja", err)
	}
}

// sloziPaket slaže paket od zadanih dijelova, s otiskom koji im odgovara;
// manifest se može izmijeniti prije zapisa, ili zadati gotov.
func sloziPaket(t *testing.T, inacica int, dijelovi map[string][]byte, izmijeni func(*Manifest)) *bytes.Reader {
	t.Helper()
	sadrzaj := map[string][]byte{"nizovi.json": []byte("[]"), "ocitanja.bin": {},
		"krivulje.json": []byte("[]"), "profili.json": []byte("[]"), "promjene.json": []byte("[]")}
	if inacica >= 2 {
		sadrzaj["izvori.json"] = []byte("[]")
	}
	for k, v := range dijelovi {
		sadrzaj[k] = v
	}
	if _, zadan := dijelovi["manifest.json"]; !zadan {
		h := sha256.New()
		for _, ime := range dijeloviZa(inacica) {
			h.Write(sadrzaj[ime])
		}
		m := Manifest{Inacica: inacica, Letva: "vodograd", Izdanje: 1, Otisak: hex.EncodeToString(h.Sum(nil))}
		if izmijeni != nil {
			izmijeni(&m)
		}
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		sadrzaj["manifest.json"] = b
	}
	return napraviZip(t, sadrzaj)
}

// Zaključava čitanje paketa koji ne valja: svaka nedosljednost odbija paket
// s porukom koja kaže što ne valja, prije nego išta dođe do ugradnje.
func TestPaketKojiNeValjaSeOdbija(t *testing.T) {
	var bin bytes.Buffer
	if err := zapisiNiz(&bin, []int64{100, 200}, []float64{120, 121}, 1); err != nil {
		t.Fatal(err)
	}
	niz := func(zapisa int) []byte {
		return fmt.Appendf(nil, `[{"izvor":"cop","velicina":"vodostaj","vrsta":"satni","zapisa":%d,"mjerilo":1}]`, zapisa)
	}
	for _, s := range []struct {
		ime      string
		inacica  int
		dijelovi map[string][]byte
		izmijeni func(*Manifest)
		greska   string
	}{
		{"manifest nije JSON", 2, map[string][]byte{"manifest.json": []byte("{")}, nil, "manifest se ne čita"},
		{"novija inačica", 2, nil, func(m *Manifest) { m.Inacica = 3 },
			"paket je inačice 3, a ovaj program poznaje 2 — nadogradi program"},
		{"manifest navodi dio kojeg nema", 1, nil, func(m *Manifest) {
			m.Dijelovi = []DioPaketa{{Ime: "izvori.json", Otisak: "00", Bajtova: 2}}
		}, `manifest navodi dio "izvori.json" kojeg u paketu nema`},
		{"dio drukčiji od manifesta", 2, nil, func(m *Manifest) {
			m.Dijelovi = []DioPaketa{{Ime: "nizovi.json", Otisak: "00", Bajtova: 2}}
		}, `dio "nizovi.json" ne odgovara onome što manifest o njemu kaže`},
		{"neupotrebljiv potpis", 2, nil, func(m *Manifest) {
			m.Potpis = &Potpis{Kljuc: "%%%", Vaznost: "x"}
		}, "potpis paketa: ključ izdavača nije upotrebljiv"},
		{"nizovi bez kraja", 2, map[string][]byte{"ocitanja.bin": bin.Bytes()[:bin.Len()-1], "nizovi.json": niz(2)}, nil,
			"niz 1 (cop, vodostaj): "},
		{"niz obećava više zapisa", 2, map[string][]byte{"ocitanja.bin": bin.Bytes(), "nizovi.json": niz(3)}, nil,
			"niz 1 obećava 3 zapisa, a nosi 2"},
	} {
		r := sloziPaket(t, s.inacica, s.dijelovi, s.izmijeni)
		_, err := Procitaj(r, r.Size())
		greskaSadrzi(t, s.ime, err, s.greska)
	}

	for _, dio := range []string{"nizovi.json", "krivulje.json", "profili.json", "promjene.json", "izvori.json"} {
		r := sloziPaket(t, 2, map[string][]byte{dio: []byte("{")}, nil)
		_, err := Procitaj(r, r.Size())
		var sintaksa *json.SyntaxError
		if !errors.As(err, &sintaksa) {
			t.Errorf("%s nije JSON: greška %v", dio, err)
		}
	}

	// Ispravno složen paket prolazi, da gornji kvarovi ne padaju iz drugog razloga.
	r := sloziPaket(t, 2, map[string][]byte{"ocitanja.bin": bin.Bytes(), "nizovi.json": niz(2)}, nil)
	s, err := Procitaj(r, r.Size())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.vrijeme) != 1 || len(s.vrijednosti[0]) != 2 || s.vrijednosti[0][1] != 121 {
		t.Errorf("pročitano %+v %+v", s.vrijeme, s.vrijednosti)
	}
}

// Zaključava da se niz bez broja zapisa ili bez vremena ne raspakirava:
// ništa od toga se ne smije izmisliti.
func TestNizBezKrajaSeNeRaspakirava(t *testing.T) {
	if _, _, err := citajNiz(&citac{}, 1); !errors.Is(err, io.EOF) {
		t.Errorf("niz bez broja zapisa: %v", err)
	}
	if _, _, err := citajNiz(&citac{b: []byte{1}}, 1); !errors.Is(err, io.EOF) {
		t.Errorf("jedan zapis bez vremena: %v", err)
	}
}

type nopZatvarac struct{ io.Writer }

func (nopZatvarac) Close() error { return nil }

// Zaključava da se dio paketa koji se ne da raspakirati odbija: nepoznat način
// sažimanja i sadržaj kojemu kontrolni zbroj ne odgovara.
func TestDioKojiSeNeDaRaspakirati(t *testing.T) {
	manifest := []byte(`{"inacica":2}`)

	var b bytes.Buffer
	z := zip.NewWriter(&b)
	z.RegisterCompressor(99, func(w io.Writer) (io.WriteCloser, error) { return nopZatvarac{w}, nil })
	w, err := z.CreateHeader(&zip.FileHeader{Name: "manifest.json", Method: 99})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(manifest); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Procitaj(bytes.NewReader(b.Bytes()), int64(b.Len())); !errors.Is(err, zip.ErrAlgorithm) {
		t.Errorf("nepoznato sažimanje: %v", err)
	}

	b.Reset()
	z = zip.NewWriter(&b)
	w, err = z.CreateRaw(&zip.FileHeader{Name: "manifest.json", Method: zip.Store, CRC32: 1,
		CompressedSize64: uint64(len(manifest)), UncompressedSize64: uint64(len(manifest))})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(manifest); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Procitaj(bytes.NewReader(b.Bytes()), int64(b.Len())); !errors.Is(err, zip.ErrChecksum) {
		t.Errorf("krivi kontrolni zbroj: %v", err)
	}
}

// paketVodograda slaže i čita paket izvorne letve, po želji izmijenjene.
func paketVodograda(t *testing.T, izdanje int, izmijeni func(*sql.DB)) *Sadrzaj {
	t.Helper()
	db := izvornaLetva(t)
	if izmijeni != nil {
		izmijeni(db)
	}
	var b bytes.Buffer
	if _, err := Izvezi(db, "vodograd", izdanje, "cvor-proba", &b); err != nil {
		t.Fatal(err)
	}
	s, err := Procitaj(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// stanjeArhive opisuje sadržaj arhive brojem redaka po tablici, zbrojem
// spojenog niza i primljenim izdanjem.
func stanjeArhive(t *testing.T, db *sql.DB) string {
	t.Helper()
	var b strings.Builder
	for _, tablica := range []string{"nizovi", "ocitanja", "hq_krivulje", "hq_odsjecci", "profili",
		"profil_tocke", "promjene_kote", "spoj", "izvori"} {
		fmt.Fprintf(&b, "%s=%d ", tablica, broj(t, db, `SELECT count(*) FROM `+tablica))
	}
	var zbroj float64
	if err := db.QueryRow(`SELECT total(vrijednost) FROM spoj`).Scan(&zbroj); err != nil {
		t.Fatal(err)
	}
	p, err := Primljeno(db, "vodograd")
	if err != nil || p == nil {
		t.Fatalf("primljeno izdanje: %+v, %v", p, err)
	}
	fmt.Fprintf(&b, "spoj=%v izdanje=%d", zbroj, p.Izdanje)
	return b.String()
}

// Zaključava da ugradnja koja pukne na bilo kojem koraku ostavlja arhivu
// točno kakva je bila: stari sadržaj, stari spoj i staru evidenciju.
func TestUgradnjaKojaPukneNaBiloKojemKoraku(t *testing.T) {
	v1 := paketVodograda(t, 1, nil)
	// Drugo izdanje ima novu vrijednost i novu promjenu kote; po toj promjeni
	// okidači znaju da su unutar ugradnje drugog izdanja.
	v2 := paketVodograda(t, 2, func(db *sql.DB) {
		if _, err := db.Exec(`UPDATE ocitanja SET vrijednost = vrijednost + 1`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO promjene_kote (letva, datum, pomak_cm, izvor)
			VALUES ('vodograd', '1960-01-01', 5, 'novi elaborat')`); err != nil {
			t.Fatal(err)
		}
	})
	const uDrugom = `EXISTS (SELECT 1 FROM promjene_kote WHERE izvor = 'novi elaborat')`

	for _, s := range []struct {
		ime    string
		kvar   func(t *testing.T, db *sql.DB)
		greska string
	}{
		{"čišćenje prethodnog izdanja", func(t *testing.T, db *sql.DB) { kvar(t, db, "DELETE", "ocitanja", "") },
			"čišćenje prethodnog izdanja: "},
		{"pospremanje ostataka", func(t *testing.T, db *sql.DB) {
			for _, q := range []string{`PRAGMA foreign_keys = OFF`,
				`INSERT INTO hq_odsjecci (krivulja, od_cm, do_cm, oblik, p1, p2, p3) VALUES (999, 0, 1, 'polinom', 1, 1, 1)`} {
				if _, err := db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			kvar(t, db, "DELETE", "hq_odsjecci", "OLD.krivulja = 999")
		}, "pospremanje ostataka: "},
		{"stupac koji fali u nizovima", func(t *testing.T, db *sql.DB) {
			if _, err := db.Exec(`ALTER TABLE nizovi DROP COLUMN osvjezeno`); err != nil {
				t.Fatal(err)
			}
		}, "osvjezeno"},
		{"stupac koji fali u očitanjima", func(t *testing.T, db *sql.DB) {
			if _, err := db.Exec(`ALTER TABLE ocitanja RENAME COLUMN vrijednost TO iznos`); err != nil {
				t.Fatal(err)
			}
		}, "vrijednost"},
		{"upis niza", func(t *testing.T, db *sql.DB) { kvar(t, db, "INSERT", "nizovi", "") }, "upis niza cop/vodostaj: "},
		{"upis vrijednosti", func(t *testing.T, db *sql.DB) { kvar(t, db, "INSERT", "ocitanja", "") },
			"upis vrijednosti niza cop/vodostaj: "},
		{"upis krivulje", func(t *testing.T, db *sql.DB) { kvar(t, db, "INSERT", "hq_krivulje", "") }, "namjerni kvar"},
		{"upis odsječka", func(t *testing.T, db *sql.DB) { kvar(t, db, "INSERT", "hq_odsjecci", "") }, "namjerni kvar"},
		{"upis snimke korita", func(t *testing.T, db *sql.DB) { kvar(t, db, "INSERT", "profili", "") }, "namjerni kvar"},
		{"upis točke korita", func(t *testing.T, db *sql.DB) { kvar(t, db, "INSERT", "profil_tocke", "") }, "namjerni kvar"},
		{"upis promjene kote", func(t *testing.T, db *sql.DB) { kvar(t, db, "INSERT", "promjene_kote", "") }, "namjerni kvar"},
		{"upis izvora iz paketa", func(t *testing.T, db *sql.DB) { kvar(t, db, "INSERT", "izvori", uDrugom) },
			"upis izvora cop iz paketa: "},
		{"upis zadanih izvora", func(t *testing.T, db *sql.DB) {
			kvar(t, db, "INSERT", "izvori", "NEW.naziv = 'his2000' AND "+uDrugom)
		}, "upis izvora his2000: "},
		{"evidencija primljenog izdanja", func(t *testing.T, db *sql.DB) { kvar(t, db, "INSERT", "primljena_izdanja", "") },
			"evidencija primljenog izdanja: "},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db, put := prazniCvor(t)
			defer db.Close()
			// Jedna veza, da postavka stranih ključeva vrijedi za sve upite.
			db.SetMaxOpenConns(1)
			uhvatiIspis(t, &os.Stdout, func() {
				if err := Ugradi(db, put, v1); err != nil {
					t.Fatal(err)
				}
			})
			if n := broj(t, db, `SELECT count(*) FROM profili WHERE crtaj = 0`); n != 1 {
				t.Fatalf("snimka koja se ne crta stigla je kao da se crta (%d)", n)
			}
			s.kvar(t, db)
			prije := stanjeArhive(t, db)
			var err error
			uhvatiIspis(t, &os.Stdout, func() { err = Ugradi(db, put, v2) })
			greskaSadrzi(t, s.ime, err, s.greska)
			if poslije := stanjeArhive(t, db); poslije != prije {
				t.Errorf("puknuta ugradnja promijenila je arhivu:\nprije   %s\nposlije %s", prije, poslije)
			}
			if p := PosaoUTijeku(put); p != "" {
				t.Errorf("brava je ostala iza puknute ugradnje: %q", p)
			}
		})
	}

	// Nad zatvorenom bazom ugradnja ne počinje.
	db, put := prazniCvor(t)
	db.Close()
	greskaSadrzi(t, "zatvorena baza", Ugradi(db, put, v1), "database is closed")
	if p := PosaoUTijeku(put); p != "" {
		t.Errorf("brava je ostala iza neuspjele ugradnje: %q", p)
	}
}
