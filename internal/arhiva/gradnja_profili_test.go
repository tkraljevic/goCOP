package arhiva

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func putProfila(koren, sliv, letva, datum string) string {
	return filepath.Join(koren, sliv, letva, "profil", letva+"_profil_"+datum+".csv")
}

// Zaključava čitanje poravnanja snimki: pomak s decimalnim zarezom, stupac
// „crtaj” neobavezan i bez obzira na velika slova, letva bez okolnih
// razmaka, a prekratak redak i zaglavlje se preskaču.
func TestPoravnanjaProfilaIzDatoteke(t *testing.T) {
	koren := t.TempDir()
	zapisiDatoteku(t, filepath.Join(koren, "poravnanje-profila.csv"),
		"letva;datum;pomak_m;crtaj\n"+
			"vodograd;2010-03-22;104,5\n"+
			"vodograd;2015-05-05;0;ne\n"+
			"vodograd;2016-06-06;-2.5;DA\n"+
			"vodograd;2017-07-07;1;0\n"+
			"vodograd;2018-08-08;1;false\n"+
			"vodograd;2019-09-09;3;\n"+
			"kratko;2010\n"+
			" rijecica ; 2020-01-01 ;7;true\n")
	got, err := poravnanjaProfila(koren)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Poravnanje{
		"vodograd|2010-03-22": {104.5, true},
		"vodograd|2015-05-05": {0, false},
		"vodograd|2016-06-06": {-2.5, true},
		"vodograd|2017-07-07": {1, false},
		"vodograd|2018-08-08": {1, false},
		"vodograd|2019-09-09": {3, true},
		"rijecica|2020-01-01": {7, true},
	}
	if len(got) != len(want) {
		t.Errorf("poravnanja: %+v", got)
	}
	for k, p := range want {
		if got[k] != p {
			t.Errorf("%s: %+v, očekivano %+v", k, got[k], p)
		}
	}
}

// Zaključava da se neispravno poravnanje javlja greškom s brojem retka, a ne
// preskače: snimka bez poravnanja crta se kao lažan oblik korita.
func TestNeispravnoPoravnanjeProfila(t *testing.T) {
	for _, s := range []struct {
		ime, sadrzaj, greska string
	}{
		{"pomak nije broj", "letva;datum;pomak_m\nvodograd;2010-03-22;desno\n", `redak 2: pomak "desno"`},
		{"crtaj nije da ni ne", "letva;datum;pomak_m;crtaj\nvodograd;2010-03-22;1;mozda\n",
			`redak 2: crtaj "mozda" (očekivano da ili ne)`},
		{"navodnik usred polja", "letva;datum;pomak_m\nvodograd;2010-03-22;1\"5\n", "poravnanje profila:"},
	} {
		koren := t.TempDir()
		zapisiDatoteku(t, filepath.Join(koren, "poravnanje-profila.csv"), s.sadrzaj)
		_, err := poravnanjaProfila(koren)
		greskaSadrzi(t, s.ime, err, s.greska)
	}
	// Stablo koje nije mapa ne znači da poravnanja nema.
	datoteka := filepath.Join(t.TempDir(), "stablo")
	zapisiDatoteku(t, datoteka, "x")
	if p, err := poravnanjaProfila(datoteka); err == nil {
		t.Errorf("poravnanja iz stabla koje nije mapa: %+v bez greške", p)
	}
}

// Zaključava upis snimki korita s poravnanjem: pomak i oznaka „ne crta se”
// ulaze u arhivu i u ispis, sustav kote čita se iza broja, snimka bez točaka
// i druga letva se ne upisuju, a nečitljiva točka se preskače.
func TestProfiliSPoravnanjem(t *testing.T) {
	db := praznaArhiva(t)
	koren := t.TempDir()
	zapisiDatoteku(t, putProfila(koren, "dunav", "vodograd", "2010-03-22"),
		"# poprečni profil korita, mjereno 2010-03-22, vodostaj pri mjerenju 174 cm, kota nule 121.55 TRST\n"+
			"stacionaza_m;visina_m\n0,0;90,113\n6,74;87,593\nx;y\n1\n14,07;85,763\n")
	zapisiDatoteku(t, putProfila(koren, "dunav", "vodograd", "2015-05-05"),
		"# poprečni profil korita, mjereno 2015-05-05, vodostaj pri mjerenju 90 cm, kota nule 80.45\n"+
			"stacionaza_m;visina_m\n0;82\n10;79\n")
	zapisiDatoteku(t, putProfila(koren, "dunav", "vodograd", "2016-06-06"),
		"# poprečni profil korita, mjereno 2016-06-06, vodostaj pri mjerenju 50 cm, kota nule 80.45\n"+
			"stacionaza_m;visina_m\n")
	zapisiDatoteku(t, putProfila(koren, "dunav", "rijecica", "2010-03-22"),
		"# poprečni profil korita, mjereno 2010-03-22, vodostaj pri mjerenju 10 cm, kota nule 70\n0;71\n")
	poravnanja := map[string]Poravnanje{
		"vodograd|2010-03-22": {PomakM: 104.5, Crtaj: true},
		"vodograd|2015-05-05": {Crtaj: false},
	}

	ispis := uhvatiIspis(t, &os.Stdout, func() {
		if err := profili(db, koren, "vodograd", poravnanja); err != nil {
			t.Fatal(err)
		}
	})
	for _, dio := range []string{
		"profil korita 2010-03-22   3 točaka, vodostaj 174 cm  — pomak +104.5 m",
		"profil korita 2015-05-05   2 točaka, vodostaj 90 cm  — ne crta se",
	} {
		if !strings.Contains(ispis, dio) {
			t.Errorf("ispis ne sadrži %q:\n%s", dio, ispis)
		}
	}
	if strings.Contains(ispis, "2016-06-06") || strings.Contains(ispis, "rijecica") {
		t.Errorf("upisana je snimka bez točaka ili druga letva:\n%s", ispis)
	}

	type snimka struct {
		vodostaj    int
		kota, pomak float64
		sustav      string
		crtaj       int
	}
	procitaj := func(datum string) snimka {
		var s snimka
		if err := db.QueryRow(`SELECT vodostaj, kota_nule, pomak_m, sustav_kote, crtaj FROM profili
			WHERE letva='vodograd' AND datum=?`, datum).Scan(&s.vodostaj, &s.kota, &s.pomak, &s.sustav, &s.crtaj); err != nil {
			t.Fatalf("snimka %s: %v", datum, err)
		}
		return s
	}
	if s := procitaj("2010-03-22"); s != (snimka{174, 121.55, 104.5, "TRST", 1}) {
		t.Errorf("snimka 2010-03-22: %+v", s)
	}
	if s := procitaj("2015-05-05"); s != (snimka{90, 80.45, 0, "", 0}) {
		t.Errorf("snimka 2015-05-05: %+v", s)
	}
	if n := broj(t, db, `SELECT count(*) FROM profili`); n != 2 {
		t.Errorf("upisano %d snimki, očekivane dvije", n)
	}
	var visina float64
	if err := db.QueryRow(`SELECT t.visina FROM profil_tocke t JOIN profili p ON p.id = t.profil
		WHERE p.datum='2010-03-22' AND t.stacionaza=6.74`).Scan(&visina); err != nil || visina != 87.593 {
		t.Errorf("točka 6,74 m: %v (%v)", visina, err)
	}
}

// Zaključava da se snimka korita koja se ne da pročitati ili upisati javlja
// greškom s imenom datoteke ili datumom snimke.
func TestProfiliKojiSeNeDajuUpisati(t *testing.T) {
	ispravna := "# poprečni profil korita, mjereno 2010-03-22, vodostaj pri mjerenju 174 cm, kota nule 80.45\n" +
		"stacionaza_m;visina_m\n0;90\n6;87\n"

	t.Run("krivi uzorak putanje", func(t *testing.T) {
		db := praznaArhiva(t)
		if err := profili(db, filepath.Join(t.TempDir(), "mapa["), "", nil); !errors.Is(err, filepath.ErrBadPattern) {
			t.Errorf("greška %v, očekivan krivi uzorak", err)
		}
	})
	t.Run("veza na datoteku koje nema", func(t *testing.T) {
		db := praznaArhiva(t)
		koren := t.TempDir()
		put := putProfila(koren, "dunav", "vodograd", "2010-03-22")
		if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(koren, "nema.csv"), put); err != nil {
			t.Fatal(err)
		}
		if err := profili(db, koren, "", nil); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("greška %v, očekivano da datoteke nema", err)
		}
	})
	t.Run("navodnik usred polja", func(t *testing.T) {
		db := praznaArhiva(t)
		koren := t.TempDir()
		zapisiDatoteku(t, putProfila(koren, "dunav", "vodograd", "2010-03-22"), "stacionaza_m;visina_m\n0;9\"0\n")
		greskaSadrzi(t, "navodnik", profili(db, koren, "", nil), "vodograd_profil_2010-03-22.csv:")
	})
	for _, s := range []struct {
		ime, kada, tablica, greska string
		dvaput                     bool
	}{
		{"upis snimke", "INSERT", "profili", "profil vodograd 2010-03-22: ", false},
		{"brisanje starih točaka", "DELETE", "profil_tocke", "namjerni kvar", true},
		{"upis točke", "INSERT", "profil_tocke", "namjerni kvar", false},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db := praznaArhiva(t)
			koren := t.TempDir()
			zapisiDatoteku(t, putProfila(koren, "dunav", "vodograd", "2010-03-22"), ispravna)
			uhvatiIspis(t, &os.Stdout, func() {
				if s.dvaput {
					if err := profili(db, koren, "", nil); err != nil {
						t.Fatal(err)
					}
				}
				kvar(t, db, s.kada, s.tablica, "")
				err := profili(db, koren, "", nil)
				greskaSadrzi(t, s.ime, err, s.greska)
				greskaSadrzi(t, s.ime, err, "namjerni kvar")
			})
		})
	}
}

// GREŠKA: snimka korita čija je datoteka maknuta iz stabla ostaje u arhivi i
// nakon ponovne gradnje, isto kao krivulja. Ispravak datuma u imenu datoteke
// tako ostavlja dvije snimke istog korita.
func TestMaknutaSnimkaNestajePriPonovnojGradnji(t *testing.T) {
	t.Skip("GREŠKA: internal/arhiva/gradnja.go:929 — snimka maknuta iz stabla ostaje u profili nakon ponovne gradnje")
	db := praznaArhiva(t)
	koren := t.TempDir()
	staro := putProfila(koren, "dunav", "vodograd", "2010-03-22")
	zapisiDatoteku(t, staro, "# poprečni profil korita, mjereno 2010-03-22, vodostaj pri mjerenju 174 cm, kota nule 80.45\n0;90\n")
	if err := profili(db, koren, "", nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(staro); err != nil {
		t.Fatal(err)
	}
	zapisiDatoteku(t, putProfila(koren, "dunav", "vodograd", "2010-03-23"),
		"# poprečni profil korita, mjereno 2010-03-23, vodostaj pri mjerenju 174 cm, kota nule 80.45\n0;90\n")
	if err := profili(db, koren, "", nil); err != nil {
		t.Fatal(err)
	}
	if n := broj(t, db, `SELECT count(*) FROM profili WHERE letva='vodograd'`); n != 1 {
		t.Errorf("u arhivi je %d snimki, a stablo ima jednu", n)
	}
}
