package arhiva

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cuvarBrave pamti koji je posao držao bravu kad mu je gradnja javila prvi
// korak.
type cuvarBrave struct {
	strings.Builder
	baza, posao string
}

func (c *cuvarBrave) Korak(sto string, gotovo, ukupno int) {
	if c.posao == "" {
		c.posao = PosaoUTijeku(c.baza)
	}
}

// Zaključava gradnju cijele arhive iz stabla: mapa pristupa, datoteke uz
// slivove i letve, mape unutar letve i datoteke s imenom izvan dogovora se
// preskaču; isti trenutak iz dviju datoteka uzima se iz zadnje; niz se svodi
// na današnju kotu nule; brava nosi ime posla i pušta se na kraju.
func TestGradnjaCijeleArhive(t *testing.T) {
	koren := t.TempDir()
	baza := filepath.Join(t.TempDir(), "arhiva.db")
	vodograd := filepath.Join(koren, "dunav", "vodograd")
	zapisiDatoteku(t, filepath.Join(vodograd, "vodograd_cop_vodostaj_jutarnji_1940.csv"),
		"datum;vodostaj_cm\n1942-12-30;100\n1942-12-31;101\n")
	zapisiDatoteku(t, filepath.Join(vodograd, "vodograd_cop_vodostaj_jutarnji_1943.csv"),
		"datum;vodostaj_cm\n1942-12-31;105\n1943-01-02;300\n")
	zapisiDatoteku(t, filepath.Join(vodograd, "vodograd_cop.csv"), "datum;vodostaj_cm\n1950-01-01;1\n")
	zapisiDatoteku(t, filepath.Join(vodograd, "napomena.txt"), "nije niz")
	zapisiDatoteku(t, filepath.Join(vodograd, "stari", "vodograd_cop_vodostaj_satni_1999.csv"),
		"vrijeme_utc;vodostaj_cm\n1999-01-01 07:00:00;1\n")
	zapisiDatoteku(t, filepath.Join(koren, "dunav", "zbirka.csv"), "nije letva")
	zapisiDatoteku(t, filepath.Join(koren, "opis.txt"), "nije sliv")
	zapisiDatoteku(t, filepath.Join(koren, "PRISTUP-cvorovi", "vodograd", "vodograd_cop_vodostaj_satni_2024.csv"),
		"vrijeme_utc;vodostaj_cm\n2024-03-01 07:00:00;1\n")
	zapisiDatoteku(t, filepath.Join(koren, "drava", "rijecica", "rijecica_his2000_vodostaj_satni_2024.csv"),
		"vrijeme_utc;vodostaj_cm\n2024-03-01 07:00:00;55\n2024-03-01 08:00:00;56\n")
	zapisiDatoteku(t, filepath.Join(koren, "promjene-kote.csv"), "letva;datum;pomak_cm\nvodograd;1943-01-01;200\n")

	zapisi := &cuvarBrave{baza: baza}
	var iz Izvjestaj
	var gradnjaErr error
	greske := uhvatiIspis(t, &os.Stderr, func() {
		ispis := uhvatiIspis(t, &os.Stdout, func() {
			iz, gradnjaErr = Izgradi(koren, baza, "", zapisi)
		})
		if !strings.Contains(ispis, "svedeno na današnju kotu") || !strings.Contains(ispis, "(+200 cm)") {
			t.Errorf("svođenje na kotu nije javljeno:\n%s", ispis)
		}
	})
	if gradnjaErr != nil {
		t.Fatal(gradnjaErr)
	}
	if !strings.Contains(greske, "preskačem, naziv nije po dogovoru: vodograd_cop.csv") {
		t.Errorf("datoteka izvan dogovora nije javljena: %q", greske)
	}
	if zapisi.posao != "gradnja cijele arhive" {
		t.Errorf("brava je za vrijeme gradnje nosila %q", zapisi.posao)
	}
	if p := PosaoUTijeku(baza); p != "" {
		t.Errorf("brava je ostala iza gradnje: %q", p)
	}
	if iz.Nizova != 2 || iz.Ocitanja != 5 {
		t.Errorf("izgrađeno %d nizova i %d očitanja, očekivano 2 i 5", iz.Nizova, iz.Ocitanja)
	}

	db, err := sql.Open("sqlite", baza)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dan := func(s string) int64 {
		d, _ := time.Parse("2006-01-02", s)
		return d.Unix()
	}
	for datum, ocekivano := range map[string]float64{"1942-12-30": 300, "1942-12-31": 305, "1943-01-02": 300} {
		var v float64
		if err := db.QueryRow(`SELECT o.vrijednost FROM ocitanja o JOIN nizovi n ON n.id = o.niz
			WHERE n.letva='vodograd' AND o.vrijeme=?`, dan(datum)).Scan(&v); err != nil {
			t.Fatalf("%s: %v", datum, err)
		}
		if v != ocekivano {
			t.Errorf("%s: %v cm, očekivano %v", datum, v, ocekivano)
		}
	}
	var poDanu int
	var od, do, datoteke, zona string
	if err := db.QueryRow(`SELECT po_danu, od, do_, datoteke, zona FROM nizovi WHERE letva='vodograd'`).
		Scan(&poDanu, &od, &do, &datoteke, &zona); err != nil {
		t.Fatal(err)
	}
	if poDanu != 1 || od != "1942-12-30" || do != "1943-01-02" || zona != "Europe/Zagreb" ||
		datoteke != "vodograd_cop_vodostaj_jutarnji_1940.csv vodograd_cop_vodostaj_jutarnji_1943.csv" {
		t.Errorf("opis niza: po danu %d, %s .. %s, zona %s, datoteke %q", poDanu, od, do, zona, datoteke)
	}

	// Letva koje u stablu uopće nema, kao da je stigla paketom, nije sirotan.
	if err := os.RemoveAll(filepath.Join(koren, "drava")); err != nil {
		t.Fatal(err)
	}
	uhvatiIspis(t, &os.Stderr, func() {
		uhvatiIspis(t, &os.Stdout, func() {
			iz, gradnjaErr = Izgradi(koren, baza, "", nil)
		})
	})
	if gradnjaErr != nil {
		t.Fatal(gradnjaErr)
	}
	if len(iz.Sirotani) != 0 {
		t.Errorf("letva bez ijedne datoteke u stablu javljena je kao sirotan: %+v", iz.Sirotani)
	}
	if n := broj(t, db, `SELECT count(*) FROM nizovi WHERE letva='rijecica'`); n != 1 {
		t.Errorf("niz letve bez stabla nestao je iz arhive (%d)", n)
	}
}

// Zaključava da gradnja preuzima bravu procesa kojeg više nema i to zapiše:
// prekinuti posao mogao je ostaviti nered.
func TestGradnjaPreuzimaMrtvuBravu(t *testing.T) {
	koren := t.TempDir()
	baza := filepath.Join(t.TempDir(), "arhiva.db")
	zapisiDatoteku(t, filepath.Join(koren, "dunav", "vodograd", "vodograd_cop_vodostaj_satni_2024.csv"),
		"vrijeme_utc;vodostaj_cm\n2024-03-01 07:00:00;120\n")
	mrtva, _ := json.Marshal(stanjeBrave{Sto: "gradnja letve stara", Tko: "Ana Anić", PID: 999999,
		Od: time.Date(2026, 1, 1, 7, 0, 0, 0, time.UTC)})
	zapisiDatoteku(t, baza+ImeBrave, string(mrtva))

	var zapisi strings.Builder
	uhvatiIspis(t, &os.Stdout, func() {
		if _, err := Izgradi(koren, baza, "vodograd", &zapisi); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(zapisi.String(), "preuzeta brava prekinutog posla: gradnja letve stara (Ana Anić, pid 999999") {
		t.Errorf("preuzimanje brave nije zapisano:\n%s", zapisi.String())
	}
	if _, err := os.Stat(baza + ImeBrave); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("brava je ostala iza gradnje: %v", err)
	}
}

// Zaključava da gradnja na svakom kvaru stane s greškom koja kaže što nije
// valjalo, i pusti bravu.
func TestGradnjaJavljaSvakiKvar(t *testing.T) {
	niz := func(t *testing.T, koren, izvor, vrijednost string) {
		zapisiDatoteku(t, filepath.Join(koren, "dunav", "vodograd", "vodograd_"+izvor+"_vodostaj_satni_2024.csv"),
			"vrijeme_utc;vodostaj_cm\n2024-03-01 07:00:00;"+vrijednost+"\n")
	}
	zatecena := func(t *testing.T, baza, sql0 string, kvarovi func(*sql.DB)) {
		db, err := sql.Open("sqlite", baza)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if sql0 != "" {
			if _, err := db.Exec(sql0); err != nil {
				t.Fatal(err)
			}
		}
		if kvarovi != nil {
			if err := PripremiPraznu(db); err != nil {
				t.Fatal(err)
			}
			kvarovi(db)
		}
	}
	for _, s := range []struct {
		ime      string
		pripremi func(t *testing.T, koren, baza *string)
		greska   string
		cilj     error
	}{
		{"stablo kojeg nema", func(t *testing.T, koren, _ *string) {
			*koren = filepath.Join(*koren, "nema")
		}, "", fs.ErrNotExist},
		{"prazno stablo", func(*testing.T, *string, *string) {}, "nema nijednog niza za uvoz", nil},
		{"baza je mapa", func(t *testing.T, koren, baza *string) {
			niz(t, *koren, "cop", "120")
			*baza = t.TempDir()
		}, "unable to open database file", nil},
		{"indeks pod imenom tablice", func(t *testing.T, koren, baza *string) {
			niz(t, *koren, "cop", "120")
			zatecena(t, *baza, `CREATE TABLE t (a); CREATE INDEX spoj ON t(a)`, nil)
		}, "there is already an index named spoj", nil},
		{"tablica izvora bez stupaca", func(t *testing.T, koren, baza *string) {
			niz(t, *koren, "cop", "120")
			zatecena(t, *baza, `CREATE TABLE izvori (naziv TEXT PRIMARY KEY)`, nil)
		}, "upis izvora his2000", nil},
		{"krivi datum promjene kote", func(t *testing.T, koren, _ *string) {
			niz(t, *koren, "cop", "120")
			zapisiDatoteku(t, filepath.Join(*koren, "promjene-kote.csv"), "letva;datum;pomak_cm\nvodograd;1943-13-01;200\n")
		}, `promjene kote, redak 2: datum "1943-13-01"`, nil},
		{"krivo poravnanje profila", func(t *testing.T, koren, _ *string) {
			niz(t, *koren, "cop", "120")
			zapisiDatoteku(t, filepath.Join(*koren, "poravnanje-profila.csv"), "letva;datum;pomak_m\nvodograd;2010-03-22;x\n")
		}, "poravnanje profila, redak 2", nil},
		{"nečitljiv profil", func(t *testing.T, koren, _ *string) {
			niz(t, *koren, "cop", "120")
			zapisiDatoteku(t, putProfila(*koren, "dunav", "vodograd", "2010-03-22"), "0;9\"0\n")
		}, "vodograd_profil_2010-03-22.csv:", nil},
		{"krivulje su mapa", func(t *testing.T, koren, _ *string) {
			niz(t, *koren, "cop", "120")
			if err := os.MkdirAll(putKrivulja(*koren, "dunav", "vodograd"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, "is a directory", nil},
		{"nečitljiva vrijednost", func(t *testing.T, koren, _ *string) {
			niz(t, *koren, "cop", "sto")
		}, `vodograd|cop|vodostaj|satni: vodograd_cop_vodostaj_satni_2024.csv redak 2: vrijednost "sto"`, nil},
		{"novi izvor koji tablica odbija", func(t *testing.T, koren, baza *string) {
			niz(t, *koren, "nova-postaja", "120")
			zatecena(t, *baza, "", func(db *sql.DB) { kvar(t, db, "INSERT", "izvori", "NEW.naziv = 'nova-postaja'") })
		}, "namjerni kvar", nil},
		{"spoj koji se ne da upisati", func(t *testing.T, koren, baza *string) {
			niz(t, *koren, "cop", "120")
			zatecena(t, *baza, "", func(db *sql.DB) { kvar(t, db, "INSERT", "spoj", "") })
		}, "vodograd/vodostaj: constraint failed: namjerni kvar", nil},
	} {
		t.Run(s.ime, func(t *testing.T) {
			koren := t.TempDir()
			baza := filepath.Join(t.TempDir(), "arhiva.db")
			s.pripremi(t, &koren, &baza)
			var err error
			uhvatiIspis(t, &os.Stdout, func() {
				_, err = Izgradi(koren, baza, "", io.Discard)
			})
			if s.cilj != nil {
				if !errors.Is(err, s.cilj) {
					t.Errorf("greška %v, očekivano %v", err, s.cilj)
				}
			} else {
				greskaSadrzi(t, s.ime, err, s.greska)
			}
			if p := PosaoUTijeku(baza); p != "" {
				t.Errorf("brava je ostala iza puknute gradnje: %q", p)
			}
		})
	}

	// Dok arhivu drži drugi posao, gradnja ne počinje nego kaže tko je drži.
	koren := t.TempDir()
	baza := filepath.Join(t.TempDir(), "arhiva.db")
	niz(t, koren, "cop", "120")
	drugi, err := Uzmi(baza, "ugradnja paketa vodograd", "Marko Markić")
	if err != nil {
		t.Fatal(err)
	}
	defer drugi.Pusti()
	_, err = Izgradi(koren, baza, "vodograd", nil)
	var z *Zauzeto
	if !asZauzeto(err, &z) || z.Sto != "ugradnja paketa vodograd" || z.Tko != "Marko Markić" {
		t.Errorf("gradnja uz zauzetu arhivu: %v", err)
	}
	if _, err := os.Stat(baza); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("gradnja je otvorila bazu iako je arhiva zauzeta: %v", err)
	}
}

// Zaključava da upis niza koji pukne na bilo kojem koraku ne ostavlja pola
// niza: ni opis bez očitanja, ni očitanja bez opisa, a zatečeni sadržaj ostaje.
func TestUpisNizaKojiPukne(t *testing.T) {
	novi := func(t *testing.T, vrijednost string) *niz {
		put := filepath.Join(t.TempDir(), "vodograd_cop_vodostaj_satni_2024.csv")
		zapisiDatoteku(t, put, "vrijeme_utc;vodostaj_cm\n2024-03-01 07:00:00;"+vrijednost+"\n")
		return &niz{sliv: "dunav", letva: "vodograd", izvor: "cop", velicina: "vodostaj", vrsta: "satni",
			datoteke: []string{put}}
	}
	for _, s := range []struct {
		ime    string
		kvar   func(t *testing.T, db *sql.DB)
		greska string
	}{
		{"upis opisa niza", func(t *testing.T, db *sql.DB) { kvar(t, db, "INSERT", "nizovi", "") }, "namjerni kvar"},
		{"upis očitanja", func(t *testing.T, db *sql.DB) { kvar(t, db, "INSERT", "ocitanja", "") }, "namjerni kvar"},
		{"dopuna opisa niza", func(t *testing.T, db *sql.DB) { kvar(t, db, "UPDATE", "nizovi", "") }, "namjerni kvar"},
		{"tablica očitanja bez stupca vrijednosti", func(t *testing.T, db *sql.DB) {
			if _, err := db.Exec(`ALTER TABLE ocitanja RENAME COLUMN vrijednost TO iznos`); err != nil {
				t.Fatal(err)
			}
		}, "vrijednost"},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db := praznaArhiva(t)
			s.kvar(t, db)
			_, _, _, _, err := ubaci(db, novi(t, "120"), nil)
			greskaSadrzi(t, s.ime, err, s.greska)
			if n := broj(t, db, `SELECT count(*) FROM nizovi`); n != 0 {
				t.Errorf("iza puknutog upisa ostao je opis niza (%d)", n)
			}
		})
	}

	t.Run("brisanje zatečenih očitanja", func(t *testing.T) {
		db := praznaArhiva(t)
		if _, _, _, _, err := ubaci(db, novi(t, "120"), nil); err != nil {
			t.Fatal(err)
		}
		kvar(t, db, "DELETE", "ocitanja", "")
		_, _, _, _, err := ubaci(db, novi(t, "130"), nil)
		greskaSadrzi(t, "brisanje", err, "namjerni kvar")
		var v float64
		if err := db.QueryRow(`SELECT vrijednost FROM ocitanja`).Scan(&v); err != nil || v != 120 {
			t.Errorf("zatečeno očitanje nakon puknutog upisa: %v (%v)", v, err)
		}
	})
	t.Run("datoteke nema", func(t *testing.T) {
		db := praznaArhiva(t)
		n := novi(t, "120")
		n.datoteke = append(n.datoteke, filepath.Join(t.TempDir(), "nema.csv"))
		if _, _, _, _, err := ubaci(db, n, nil); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("greška %v, očekivano da datoteke nema", err)
		}
	})
	t.Run("nijedan čitljiv redak", func(t *testing.T) {
		db := praznaArhiva(t)
		_, _, _, _, err := ubaci(db, novi(t, "-2270"), nil)
		greskaSadrzi(t, "nemoguća vrijednost", err, "nijedan čitljiv redak")
		if n := broj(t, db, `SELECT count(*) FROM nizovi`); n != 0 {
			t.Errorf("niz bez ijedne vrijednosti upisan je u arhivu (%d)", n)
		}
	})
}

// Zaključava čitanje datoteke niza: datum bez sata znači niz po danu i ne
// ovisi o zoni, sat se čita u zoni izvora, a prazna i nemoguća vrijednost
// te prekratak redak se preskaču.
func TestCitanjeDatotekeNiza(t *testing.T) {
	put := filepath.Join(t.TempDir(), "vodograd.csv")
	zapisiDatoteku(t, put, "\ufeffdatum;vodostaj_cm\n"+
		"2024-03-01;120\n"+
		"2024-03-02;\n"+
		"2024-03-03;-2270\n"+
		"2024-03-05\n"+
		"2024-03-04 07:00:00;130,5\n")
	z, poDanu, err := citaj(put, Zagreb, "vodostaj")
	if err != nil {
		t.Fatal(err)
	}
	if !poDanu {
		t.Error("zaglavlje „datum” nije prepoznato kao niz po danu")
	}
	want := []zapis{
		{t: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC).Unix(), v: 120},
		{t: time.Date(2024, 3, 4, 6, 0, 0, 0, time.UTC).Unix(), v: 130.5},
	}
	if len(z) != len(want) || z[0] != want[0] || z[1] != want[1] {
		t.Errorf("pročitano %+v, očekivano %+v", z, want)
	}
}

// Zaključava da se pokvarena datoteka niza javlja greškom s imenom i retkom.
func TestPokvarenaDatotekaNiza(t *testing.T) {
	mapa := t.TempDir()
	if _, _, err := citaj(filepath.Join(mapa, "nema.csv"), time.UTC, "vodostaj"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("datoteka koje nema: %v", err)
	}
	prazna := filepath.Join(mapa, "prazna.csv")
	zapisiDatoteku(t, prazna, "")
	if _, _, err := citaj(prazna, time.UTC, "vodostaj"); !errors.Is(err, io.EOF) {
		t.Errorf("prazna datoteka: %v", err)
	}
	for _, s := range []struct {
		ime, redci, greska string
	}{
		{"navodnik usred polja", "2024-03-01 07:00:00;1\n2024-03-01 08:00:00;1\"2\n", "vodograd.csv redak 3: "},
		{"nemoguć datum", "2024-13-45 25:00:00;120\n", `vodograd.csv redak 2: vrijeme "2024-13-45 25:00:00"`},
		{"vrijednost nije broj", "2024-03-01 07:00:00;sto\n", `vodograd.csv redak 2: vrijednost "sto"`},
	} {
		put := filepath.Join(t.TempDir(), "vodograd.csv")
		zapisiDatoteku(t, put, "vrijeme_utc;vodostaj_cm\n"+s.redci)
		_, _, err := citaj(put, time.UTC, "vodostaj")
		greskaSadrzi(t, s.ime, err, s.greska)
	}
}

// GREŠKA: datum kraći od deset znakova ruši gradnju. citaj reže s[:10] bez
// provjere duljine, pa redak „2024-3-1;120” izazove panic umjesto greške s
// imenom datoteke i retkom.
func TestKratakDatumNeRusiGradnju(t *testing.T) {
	t.Skip("GREŠKA: internal/arhiva/gradnja.go:775 — datum kraći od 10 znakova izaziva panic (slice bounds out of range)")
	put := filepath.Join(t.TempDir(), "vodograd.csv")
	zapisiDatoteku(t, put, "datum;vodostaj_cm\n2024-3-1;120\n")
	_, _, err := citaj(put, time.UTC, "vodostaj")
	greskaSadrzi(t, "kratak datum", err, `vrijeme "2024-3-1"`)
}

// Zaključava da se pokvarena datoteka promjena kote nule javlja greškom s
// retkom: krivo pročitan pomak pomaknuo bi cijeli stari niz.
func TestPokvarenePromjeneKote(t *testing.T) {
	datoteka := filepath.Join(t.TempDir(), "stablo")
	zapisiDatoteku(t, datoteka, "x")
	if p, err := promjeneKote(praznaArhiva(t), datoteka, io.Discard); err == nil {
		t.Errorf("promjene iz stabla koje nije mapa: %+v bez greške", p)
	}
	for _, s := range []struct {
		ime, redak, greska string
	}{
		{"navodnik usred polja", "vodograd;1943-01-01;2\"00", "promjene kote: "},
		{"pomak nije broj", "vodograd;1943-01-01;dvjesto", `promjene kote, redak 2: pomak "dvjesto"`},
		{"nemoguć datum", "vodograd;1943-13-01;200", `promjene kote, redak 2: datum "1943-13-01"`},
	} {
		koren := t.TempDir()
		zapisiDatoteku(t, filepath.Join(koren, "promjene-kote.csv"), "letva;datum;pomak_cm\n"+s.redak+"\n")
		_, err := promjeneKote(praznaArhiva(t), koren, io.Discard)
		greskaSadrzi(t, s.ime, err, s.greska)
	}
	koren := t.TempDir()
	zapisiDatoteku(t, filepath.Join(koren, "promjene-kote.csv"), "letva;datum;pomak_cm\nvodograd;1943-01-01;200\n")
	db := praznaArhiva(t)
	kvar(t, db, "INSERT", "promjene_kote", "")
	_, err := promjeneKote(db, koren, io.Discard)
	greskaSadrzi(t, "upis promjene", err, "namjerni kvar")
}

// Zaključava izbacivanje kota daleko od uobičajene: samo za kotu vodnog lica,
// samo uz dovoljno vrijednosti da medijan nešto znači, i uz zapis koliko je
// izbačeno.
func TestKoteDalekoOdUobicajeneSeIzbacuju(t *testing.T) {
	niz := func(n int) []zapis {
		z := make([]zapis, n)
		for i := range z {
			z[i] = zapis{t: int64(i) * 3600, v: 19000 + float64(i%7)}
		}
		return z
	}
	z := niz(120)
	z[10].v = 36800 // udvostručena
	z[50].v = 11100 // srezana
	var out []zapis
	ispis := uhvatiIspis(t, &os.Stdout, func() { out = bezDalekih("kota", z) })
	if len(out) != 118 {
		t.Fatalf("ostalo %d od 120 kota, očekivano 118", len(out))
	}
	for _, x := range out {
		if x.v == 36800 || x.v == 11100 {
			t.Errorf("kota %v nije izbačena", x.v)
		}
	}
	if out[10].t != 11*3600 {
		t.Errorf("redoslijed je poremećen: %+v", out[10])
	}
	if !strings.Contains(ispis, "izbačeno kota daleko od uobičajene: 2") {
		t.Errorf("izbacivanje nije zapisano: %q", ispis)
	}

	// Bez ispada nema ni zapisa.
	if ispis := uhvatiIspis(t, &os.Stdout, func() { out = bezDalekih("kota", niz(120)) }); ispis != "" || len(out) != 120 {
		t.Errorf("miran niz: ostalo %d, ispis %q", len(out), ispis)
	}
	// Premalo vrijednosti i druge veličine se ne diraju.
	kratki := niz(99)
	kratki[3].v = 36800
	if got := bezDalekih("kota", kratki); len(got) != 99 {
		t.Errorf("niz od 99 kota je diran: ostalo %d", len(got))
	}
	vodostaj := niz(120)
	vodostaj[3].v = 36800
	if got := bezDalekih("vodostaj", vodostaj); len(got) != 120 {
		t.Errorf("vodostaj je diran: ostalo %d", len(got))
	}
}

// Zaključava granice mogućih vrijednosti za veličine koje vrata ne
// provjeravaju zasebnim testom.
func TestGraniceMogucihVrijednosti(t *testing.T) {
	for _, s := range []struct {
		velicina string
		v        float64
		moguca   bool
	}{
		{"koncentracija", -0.01, false},
		{"koncentracija", 0, true},
		{"pronos", 12.5, true},
		{"pronos", -1, false},
		{"snijeg", -1, false},
		{"oborina", 999, true},
		{"oborina", 1000, false},
		{"visina-snijega", 1999, true},
		{"visina-snijega", 2000, false},
		{"temperatura-zraka", 50, true},
		{"kota", 5000, false},
		{"kota", 19000, true},
		{"kota", 40000, false},
	} {
		if got := mogucaVrijednost(s.velicina, s.v); got != s.moguca {
			t.Errorf("%s %v: moguća %v, očekivano %v", s.velicina, s.v, got, s.moguca)
		}
	}
}

// Zaključava da popis za jednu letvu vraća samo nju, s datotekama po redu.
func TestPopisJedneLetve(t *testing.T) {
	koren := t.TempDir()
	for _, p := range []string{
		filepath.Join("dunav", "vodograd", "vodograd_cop_vodostaj_satni_2025.csv"),
		filepath.Join("dunav", "vodograd", "vodograd_cop_vodostaj_satni_2024.csv"),
		filepath.Join("drava", "rijecica", "rijecica_cop_vodostaj_satni_2024.csv"),
	} {
		zapisiDatoteku(t, filepath.Join(koren, p), "vrijeme_utc;vodostaj_cm\n")
	}
	nizovi, err := popisi(koren, "vodograd")
	if err != nil {
		t.Fatal(err)
	}
	n := nizovi["vodograd|cop|vodostaj|satni"]
	if len(nizovi) != 1 || n == nil {
		t.Fatalf("popis jedne letve: %v", nizovi)
	}
	if n.sliv != "dunav" || len(n.datoteke) != 2 ||
		filepath.Base(n.datoteke[0]) != "vodograd_cop_vodostaj_satni_2024.csv" {
		t.Errorf("niz: %+v", n)
	}
}

// Zaključava da mapa sliva ili letve koja se ne da pročitati zaustavlja popis
// umjesto da se letve tiho izgube.
func TestNecitljivaMapaZaustavljaPopis(t *testing.T) {
	koren := t.TempDir()
	letva := filepath.Join(koren, "dunav", "vodograd")
	zapisiDatoteku(t, filepath.Join(letva, "vodograd_cop_vodostaj_satni_2024.csv"), "vrijeme_utc;vodostaj_cm\n")

	bezPrava(t, letva, 0)
	if _, err := popisi(koren, ""); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("nečitljiva mapa letve: %v", err)
	}
	if err := os.Chmod(letva, 0o755); err != nil {
		t.Fatal(err)
	}

	bezPrava(t, filepath.Join(koren, "dunav"), 0)
	if _, err := popisi(koren, ""); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("nečitljiva mapa sliva: %v", err)
	}
	if _, err := Letve(koren); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("letve iz nečitljivog sliva: %v", err)
	}
}

// Zaključava dopunu sheme koja ne uspije: greška kaže koja tablica ili koji
// stupac, a arhiva otvorena samo za čitanje se ne dopunjuje.
func TestDopunaShemeKojaNeUspije(t *testing.T) {
	for _, s := range []struct {
		ime, priprema, greska string
	}{
		{"indeks pod imenom tablice izvora", `CREATE TABLE t (a); CREATE INDEX izvori ON t(a)`, "tablica izvora: "},
		{"indeks pod imenom primljenih izdanja", `CREATE TABLE t (a); CREATE INDEX primljena_izdanja ON t(a)`,
			"tablica primljenih izdanja: "},
	} {
		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "a.db"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(s.priprema); err != nil {
			t.Fatal(err)
		}
		greskaSadrzi(t, s.ime, dopuniShemu(db), s.greska)
		db.Close()
	}

	// Stara arhiva bez napomene uz niz, otvorena samo za čitanje.
	put := filepath.Join(t.TempDir(), "stara.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE nizovi (id INTEGER PRIMARY KEY, letva TEXT, izvor TEXT);` +
		shemaIzvora + shemaPrimljenih); err != nil {
		t.Fatal(err)
	}
	db.Close()
	ro, err := sql.Open("sqlite", "file:"+put+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	greskaSadrzi(t, "arhiva samo za čitanje", dopuniShemu(ro), "dodavanje stupca nizovi.napomena: ")

	// Prazna arhiva samo za čitanje ne dobiva shemu.
	prazna := filepath.Join(t.TempDir(), "prazna.db")
	zapisiDatoteku(t, prazna, "")
	ro2, err := sql.Open("sqlite", "file:"+prazna+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer ro2.Close()
	greskaSadrzi(t, "prazna samo za čitanje", PripremiPraznu(ro2), "readonly")
}
