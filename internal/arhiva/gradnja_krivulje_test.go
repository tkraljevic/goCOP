package arhiva

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// putKrivulja je mjesto datoteke HQ krivulja letve u stablu.
func putKrivulja(koren, sliv, letva string) string {
	return filepath.Join(koren, sliv, letva, "hq", letva+"_hq_krivulje.csv")
}

type odsjecakIzBaze struct {
	vrijediOd, vrijediDo, izvor, napomena, oblik string
	odCm, doCm                                   int
	p1, p2, p3, p4                               float64
}

func odsjecciIzBaze(t *testing.T, db *sql.DB, letva string) []odsjecakIzBaze {
	t.Helper()
	rows, err := db.Query(`SELECT k.vrijedi_od, k.vrijedi_do, k.izvor, k.napomena, o.oblik,
		o.od_cm, o.do_cm, o.p1, o.p2, o.p3, o.p4
		FROM hq_odsjecci o JOIN hq_krivulje k ON k.id = o.krivulja
		WHERE k.letva = ? ORDER BY k.vrijedi_od, o.od_cm`, letva)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []odsjecakIzBaze
	for rows.Next() {
		var o odsjecakIzBaze
		if err := rows.Scan(&o.vrijediOd, &o.vrijediDo, &o.izvor, &o.napomena, &o.oblik,
			&o.odCm, &o.doCm, &o.p1, &o.p2, &o.p3, &o.p4); err != nil {
			t.Fatal(err)
		}
		out = append(out, o)
	}
	return out
}

// Zaključava čitanje krivulja iz stabla: stupci se traže po zaglavlju, bez
// obzira na velika slova, razmake i oznaku BOM koju na početak stavlja
// Excel; odsječci iste krivulje dolaze u svojim retcima; prazan oblik čita
// se kao polinom, decimalni zarez kao točka, a datum se čita i s razmacima
// oko njega. Prazan redak, i onaj sa samo razmacima i razdjelnicima, kratak
// ili pun, se preskače. Druga letva se ne dira.
func TestKrivuljeSeCitajuPoZaglavlju(t *testing.T) {
	db := praznaArhiva(t)
	koren := t.TempDir()
	zapisiDatoteku(t, putKrivulja(koren, "dunav", "vodograd"),
		"\ufeffvrijedi_od;vrijedi_do;od_cm;do_cm;Oblik;p1;p2;p3; P4 ;izvor;napomena\n"+
			"2001-01-01;2010-12-31;-100;150;polinom;0,1;2,5;30;;DHMZ;stara krivulja\n"+
			"2001-01-01;2010-12-31;150;600;potencija;12,5;1,6;-50;3;DHMZ;stara krivulja\n"+
			"\n"+
			" 2011-01-01 ;;-120;500;;1;2;3;;mjerenje 2011.;\n"+
			"   \n"+
			" ; ;\n"+
			";;;;;;;;;;\n")
	zapisiDatoteku(t, putKrivulja(koren, "dunav", "rijecica"),
		"vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n"+
			"2001-01-01;;0;100;polinom;1;1;1;;DHMZ;\n")

	ispis := uhvatiIspis(t, &os.Stdout, func() {
		if _, err := krivulje(db, koren, "vodograd"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(ispis, "vodograd") || !strings.Contains(ispis, "HQ krivulje: 2") {
		t.Errorf("ispis gradnje: %q", ispis)
	}
	if strings.Contains(ispis, "rijecica") {
		t.Errorf("gradnja jedne letve dirala je drugu: %q", ispis)
	}

	got := odsjecciIzBaze(t, db, "vodograd")
	want := []odsjecakIzBaze{
		{"2001-01-01", "2010-12-31", "DHMZ", "stara krivulja", "polinom", -100, 150, 0.1, 2.5, 30, 0},
		{"2001-01-01", "2010-12-31", "DHMZ", "stara krivulja", "potencija", 150, 600, 12.5, 1.6, -50, 3},
		{"2011-01-01", "", "mjerenje 2011.", "", "polinom", -120, 500, 1, 2, 3, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("odsječci: %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("odsječak %d: %+v, očekivano %+v", i, got[i], want[i])
		}
	}
	if n := broj(t, db, `SELECT count(*) FROM hq_krivulje WHERE letva='vodograd'`); n != 2 {
		t.Errorf("krivulja %d, očekivane dvije", n)
	}
	if n := broj(t, db, `SELECT count(*) FROM hq_krivulje WHERE letva='rijecica'`); n != 0 {
		t.Errorf("krivulje druge letve upisane su iako se gradila samo jedna: %d", n)
	}
}

// Zaključava da odsječak iste donje granice zamijeni raniji redak iste
// krivulje, a odsječci stoje poredani po donjoj granici, kao u arhivi, gdje je
// donja granica ključ odsječka.
func TestOdsjecakIsteDonjeGraniceZamijeniRaniji(t *testing.T) {
	kriv := slozeneKrivulje([][]string{
		{"vrijedi_od", "vrijedi_do", "od_cm", "do_cm", "oblik", "p1", "p2", "p3"},
		{"2001-01-01", "", "150", "600", "polinom", "4", "5", "6"},
		{"2001-01-01", "", "-100", "150", "polinom", "1", "2", "3"},
		{"2001-01-01", "", "150", "650", "polinom", "7", "8", "9"},
	})
	zeli := []odsjecakUPaketu{
		{OdCm: -100, DoCm: 150, Oblik: "polinom", P1: 1, P2: 2, P3: 3},
		{OdCm: 150, DoCm: 650, Oblik: "polinom", P1: 7, P2: 8, P3: 9},
	}
	if len(kriv) != 1 || !slices.Equal(kriv[0].Odsjecci, zeli) {
		t.Errorf("krivulje %+v, očekivani odsječci %+v", kriv, zeli)
	}
}

// Zaključava čitanje zatečene datoteke bez stupca p4: zbrojni član je tada
// nula, a stupac koji redak nema čita se kao prazan.
func TestKrivuljaBezStupcaP4(t *testing.T) {
	db := praznaArhiva(t)
	koren := t.TempDir()
	zapisiDatoteku(t, putKrivulja(koren, "drava", "vodograd"),
		"vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;izvor;napomena\n"+
			"2001-01-01;;0;300;potencija;10;1,5;0,2;DHMZ\n")
	uhvatiIspis(t, &os.Stdout, func() {
		if _, err := krivulje(db, koren, ""); err != nil {
			t.Fatal(err)
		}
	})
	got := odsjecciIzBaze(t, db, "vodograd")
	if len(got) != 1 {
		t.Fatalf("odsječci: %+v", got)
	}
	o := got[0]
	if o.oblik != "potencija" || o.p1 != 10 || o.p2 != 1.5 || o.p3 != 0.2 || o.p4 != 0 {
		t.Errorf("odsječak bez stupca p4: %+v", o)
	}
	if o.izvor != "DHMZ" || o.napomena != "" {
		t.Errorf("izvor %q i napomena %q, očekivano DHMZ i prazno", o.izvor, o.napomena)
	}
}

// Krivulja se pri svakoj gradnji briše i slaže iznova, da odsječak koji je
// maknut iz datoteke ne ostane visjeti u arhivi.
func TestUklonjeniOdsjecakNeOstajePriPonovnojGradnji(t *testing.T) {
	db := praznaArhiva(t)
	koren := t.TempDir()
	put := putKrivulja(koren, "dunav", "vodograd")
	zaglavlje := "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n"
	zapisiDatoteku(t, put, zaglavlje+
		"2001-01-01;;-100;150;polinom;1;2;3;;DHMZ;\n"+
		"2001-01-01;;150;600;polinom;4;5;6;;DHMZ;\n")
	gradi := func() {
		uhvatiIspis(t, &os.Stdout, func() {
			if _, err := krivulje(db, koren, ""); err != nil {
				t.Fatal(err)
			}
		})
	}
	gradi()
	zapisiDatoteku(t, put, zaglavlje+"2001-01-01;;-100;600;polinom;7;8;9;;DHMZ;\n")
	gradi()
	got := odsjecciIzBaze(t, db, "vodograd")
	if len(got) != 1 || got[0].odCm != -100 || got[0].doCm != 600 || got[0].p1 != 7 {
		t.Errorf("nakon ponovne gradnje: %+v", got)
	}
	if n := broj(t, db, `SELECT count(*) FROM hq_krivulje`); n != 1 {
		t.Errorf("krivulja %d, očekivana jedna", n)
	}
}

// Zaključava pravilo iz Odluka (13. 10. 2026.): krivulje letve prate njezinu
// datoteku. Krivulja maknuta iz datoteke nestaje iz arhive pri ponovnoj
// gradnji, s odsječcima, i vraća se s razdobljem valjanosti, da je izvještaj
// gradnje pokaže. Krivulje letve koja datoteke krivulja nema, npr. primljene
// paketom, ostaju. Prije je maknuta krivulja ostajala, pa je ista mapa davala
// drukčiju arhivu ovisno o tome gradi li se iznova ili preko stare.
func TestMaknutaKrivuljaNestajePriPonovnojGradnji(t *testing.T) {
	db := praznaArhiva(t)
	koren := t.TempDir()
	put := putKrivulja(koren, "dunav", "vodograd")
	zaglavlje := "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n"
	zapisiDatoteku(t, put, zaglavlje+
		"2001-01-01;2010-12-31;-100;600;polinom;1;2;3;;DHMZ;\n"+
		"2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;\n")
	res, err := db.Exec(`INSERT INTO hq_krivulje (letva, vrijedi_od) VALUES ('rijecica', '2001-01-01')`)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO hq_odsjecci (krivulja, od_cm, do_cm, p1, p2, p3) VALUES (?, 0, 100, 7, 7, 7)`, id); err != nil {
		t.Fatal(err)
	}
	gradi := func() []MaknutaKrivulja {
		t.Helper()
		var m []MaknutaKrivulja
		uhvatiIspis(t, &os.Stdout, func() {
			var err error
			var kg krivuljeGradnje
			if kg, err = krivulje(db, koren, ""); err != nil {
				t.Fatal(err)
			}
			m = kg.maknute
		})
		return m
	}
	if m := gradi(); len(m) != 0 {
		t.Errorf("prva gradnja maknula je %+v", m)
	}
	zapisiDatoteku(t, put, zaglavlje+"2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;\n")
	m := gradi()
	want := []MaknutaKrivulja{{Letva: "vodograd", VrijediOd: "2001-01-01", VrijediDo: "2010-12-31"}}
	if len(m) != 1 || m[0] != want[0] {
		t.Errorf("maknuto %+v, očekivano %+v", m, want)
	}
	if n := broj(t, db, `SELECT count(*) FROM hq_krivulje WHERE letva='vodograd'`); n != 1 {
		t.Errorf("u arhivi je %d krivulja, a datoteka ima jednu", n)
	}
	if n := broj(t, db, `SELECT count(*) FROM hq_odsjecci WHERE p1 = 1`); n != 0 {
		t.Errorf("odsječak maknute krivulje ostao je u arhivi")
	}
	if n := broj(t, db, `SELECT count(*) FROM hq_krivulje k JOIN hq_odsjecci o ON o.krivulja = k.id
		WHERE k.letva='rijecica' AND o.p1 = 7`); n != 1 {
		t.Errorf("krivulja letve bez datoteke krivulja je dirana (%d)", n)
	}
	if m := gradi(); len(m) != 0 {
		t.Errorf("gradnja iste datoteke opet je maknula %+v", m)
	}

	// Datoteka bez ijedne krivulje znači da letva krivulja nema.
	zapisiDatoteku(t, put, zaglavlje)
	if m := gradi(); len(m) != 1 || m[0].VrijediOd != "2011-01-01" || m[0].VrijediDo != "" {
		t.Errorf("prazna datoteka maknula je %+v", m)
	}
	if n := broj(t, db, `SELECT count(*) FROM hq_krivulje WHERE letva='vodograd'`); n != 0 {
		t.Errorf("u arhivi je %d krivulja, a datoteka nema nijednu", n)
	}
}

// Zaključava rečenicu kojom izvještaj gradnje javlja maknutu krivulju: s
// razdobljem valjanosti, datumom napisanim hrvatski, a datum koji se ne čita
// ostaje kako je zapisan u datoteci.
func TestOpisMaknuteKrivulje(t *testing.T) {
	for _, s := range []struct {
		k    MaknutaKrivulja
		zeli string
	}{
		{MaknutaKrivulja{Letva: "vodograd", VrijediOd: "2001-01-01", VrijediDo: "2010-12-31"},
			"Krivulja protoka koja je vrijedila od 1. 1. 2001. do 31. 12. 2010. maknuta je iz arhive, jer je u datoteci krivulja više nema."},
		{MaknutaKrivulja{Letva: "vodograd", VrijediOd: "2011-03-05"},
			"Krivulja protoka koja vrijedi od 5. 3. 2011. maknuta je iz arhive, jer je u datoteci krivulja više nema."},
		{MaknutaKrivulja{Letva: "vodograd", VrijediOd: "1.1.2011."},
			"Krivulja protoka koja vrijedi od 1.1.2011. maknuta je iz arhive, jer je u datoteci krivulja više nema."},
	} {
		if got := s.k.Opis(); got != s.zeli {
			t.Errorf("%+v: %q, očekivano %q", s.k, got, s.zeli)
		}
	}
}

// Zaključava da gradnja maknutu krivulju javi i u izvještaju i u ispisu koji
// čovjek čita na stranici.
func TestGradnjaJavljaMaknutuKrivulju(t *testing.T) {
	koren := t.TempDir()
	baza := filepath.Join(t.TempDir(), "arhiva.db")
	zapisiDatoteku(t, filepath.Join(koren, "dunav", "vodograd", "vodograd_his2000_vodostaj_dnevni_2024.csv"),
		"datum;vodostaj_cm\n2024-03-01;120\n")
	put := putKrivulja(koren, "dunav", "vodograd")
	zaglavlje := "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n"
	zapisiDatoteku(t, put, zaglavlje+
		"2001-01-01;2010-12-31;-100;600;polinom;1;2;3;;DHMZ;\n"+
		"2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;\n")
	uhvatiIspis(t, &os.Stdout, func() {
		if _, err := Izgradi(koren, baza, "vodograd", nil); err != nil {
			t.Fatal(err)
		}
	})
	zapisiDatoteku(t, put, zaglavlje+"2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;\n")
	var ispis strings.Builder
	var iz Izvjestaj
	uhvatiIspis(t, &os.Stdout, func() {
		var err error
		if iz, err = Izgradi(koren, baza, "vodograd", &ispis); err != nil {
			t.Fatal(err)
		}
	})
	if len(iz.MaknuteKrivulje) != 1 || iz.MaknuteKrivulje[0].VrijediOd != "2001-01-01" {
		t.Errorf("izvještaj: %+v", iz.MaknuteKrivulje)
	}
	if !strings.Contains(ispis.String(), "vodograd         Krivulja protoka koja je vrijedila od 1. 1. 2001. do 31. 12. 2010. maknuta je iz arhive") {
		t.Errorf("ispis gradnje:\n%s", ispis.String())
	}
}

// Zaključava da se krivulja koja se ne da maknuti javlja greškom, a ne
// preskače: zatečena arhiva s praznim krajem valjanosti, upit koji pukne i
// brisanje odsječka ili krivulje koje ne prođe.
func TestKrivuljeKojeSeNeDajuMaknuti(t *testing.T) {
	uDatoteci := map[string]bool{"2011-01-01": true}
	napuni := func(t *testing.T, db *sql.DB) {
		t.Helper()
		for _, od := range []string{"2001-01-01", "2011-01-01"} {
			res, err := db.Exec(`INSERT INTO hq_krivulje (letva, vrijedi_od) VALUES ('vodograd', ?)`, od)
			if err != nil {
				t.Fatal(err)
			}
			id, _ := res.LastInsertId()
			if _, err := db.Exec(`INSERT INTO hq_odsjecci (krivulja, od_cm, do_cm, p1, p2, p3) VALUES (?, 0, 100, 1, 1, 1)`, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, s := range []struct{ ime, kada, tablica string }{
		{"brisanje odsječka", "DELETE", "hq_odsjecci"},
		{"brisanje krivulje", "DELETE", "hq_krivulje"},
	} {
		db := praznaArhiva(t)
		napuni(t, db)
		kvar(t, db, s.kada, s.tablica, "")
		_, err := makniKrivuljeIzvanDatoteke(db, "vodograd", uDatoteci)
		greskaSadrzi(t, s.ime, err, "namjerni kvar")
	}

	// Gradnja koja ne može maknuti krivulju pada, a ne preskače je.
	db := praznaArhiva(t)
	koren := t.TempDir()
	zapisiDatoteku(t, putKrivulja(koren, "dunav", "vodograd"),
		"vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n"+
			"2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;\n")
	res, err := db.Exec(`INSERT INTO hq_krivulje (letva, vrijedi_od) VALUES ('vodograd', '2001-01-01')`)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO hq_odsjecci (krivulja, od_cm, do_cm, p1, p2, p3) VALUES (?, 0, 100, 9, 9, 9)`, id); err != nil {
		t.Fatal(err)
	}
	kvar(t, db, "DELETE", "hq_odsjecci", "OLD.p1 = 9")
	uhvatiIspis(t, &os.Stdout, func() {
		_, err = krivulje(db, koren, "")
	})
	greskaSadrzi(t, "gradnja koja miče krivulju", err, "namjerni kvar")

	put := filepath.Join(t.TempDir(), "arhiva.db")
	napuni(t, napraviArhivu(t, put))
	_, err = makniKrivuljeIzvanDatoteke(kvarUpita(t, put, "SELECT vrijedi_od, vrijedi_do FROM hq_krivulje", 1), "vodograd", uDatoteci)
	greskaSadrzi(t, "čitanje krivulja", err, "namjerni kvar")

	zateceno, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "zatecena.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer zateceno.Close()
	if _, err := zateceno.Exec(`CREATE TABLE hq_krivulje (id INTEGER PRIMARY KEY, letva TEXT, vrijedi_od TEXT, vrijedi_do TEXT);
		INSERT INTO hq_krivulje (letva, vrijedi_od, vrijedi_do) VALUES ('vodograd', '2001-01-01', NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := krivuljeLetve(zateceno, "vodograd"); err == nil {
		t.Error("krivulja bez kraja valjanosti (NULL) pročitana je bez greške")
	}
}

// Zaključava da se datoteka krivulja koja se ne da pročitati ili upisati
// javlja greškom, a ne preskače: protok bez krivulje tiho bi nestao.
func TestKrivuljeKojeSeNeDajuProcitati(t *testing.T) {
	ispravna := "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n" +
		"2001-01-01;;-100;600;polinom;1;2;3;;DHMZ;\n"

	t.Run("krivi uzorak putanje", func(t *testing.T) {
		db := praznaArhiva(t)
		_, err := krivulje(db, filepath.Join(t.TempDir(), "mapa["), "")
		if !errors.Is(err, filepath.ErrBadPattern) {
			t.Errorf("greška %v, očekivan krivi uzorak", err)
		}
	})
	t.Run("veza na datoteku koje nema", func(t *testing.T) {
		db := praznaArhiva(t)
		koren := t.TempDir()
		put := putKrivulja(koren, "dunav", "vodograd")
		if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(koren, "nema.csv"), put); err != nil {
			t.Fatal(err)
		}
		if _, err := krivulje(db, koren, ""); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("greška %v, očekivano da datoteke nema", err)
		}
	})
	t.Run("navodnik usred polja", func(t *testing.T) {
		db := praznaArhiva(t)
		koren := t.TempDir()
		zapisiDatoteku(t, putKrivulja(koren, "dunav", "vodograd"), ispravna+"2011-01-01;;-100;600;\"polinom\"x;1;2;3;;DHMZ;\n")
		if _, err := krivulje(db, koren, ""); !errors.Is(err, csv.ErrQuote) {
			t.Errorf("greška %v, očekivan navodnik usred polja", err)
		}
		if n := broj(t, db, `SELECT count(*) FROM hq_krivulje`); n != 0 {
			t.Errorf("iz datoteke koja se ne čita do kraja upisano je %d krivulja", n)
		}
	})
	t.Run("mapa umjesto datoteke", func(t *testing.T) {
		db := praznaArhiva(t)
		koren := t.TempDir()
		if err := os.MkdirAll(putKrivulja(koren, "dunav", "vodograd"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := krivulje(db, koren, ""); err == nil {
			t.Error("mapa je pročitana kao datoteka krivulja")
		}
	})
	for _, s := range []struct {
		ime, kada, tablica string
		dvaput             bool
	}{
		{"brisanje stare krivulje", "DELETE", "hq_krivulje", true},
		{"upis krivulje", "INSERT", "hq_krivulje", false},
		{"upis odsječka", "INSERT", "hq_odsjecci", false},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db := praznaArhiva(t)
			koren := t.TempDir()
			zapisiDatoteku(t, putKrivulja(koren, "dunav", "vodograd"), ispravna)
			uhvatiIspis(t, &os.Stdout, func() {
				if s.dvaput {
					if _, err := krivulje(db, koren, ""); err != nil {
						t.Fatal(err)
					}
				}
				kvar(t, db, s.kada, s.tablica, "")
				_, err := krivulje(db, koren, "")
				greskaSadrzi(t, s.ime, err, "namjerni kvar")
			})
		})
	}
}

// Zaključava da se o krivuljama letve sudi po svim njezinim datotekama
// krivulja zajedno. Kad je letva u hq/ imala dvije datoteke, svaka gradnja
// maknula je krivulje prve i javila da ih u datoteci više nema, iako je
// datoteka bila ondje.
func TestDvijeDatotekeKrivuljaIsteLetve(t *testing.T) {
	db := praznaArhiva(t)
	koren := t.TempDir()
	zaglavlje := "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n"
	hq := filepath.Dir(putKrivulja(koren, "dunav", "vodograd"))
	zapisiDatoteku(t, filepath.Join(hq, "vodograd-stara_hq_krivulje.csv"),
		zaglavlje+"2001-01-01;2010-12-31;-100;600;polinom;1;2;3;;DHMZ;\n")
	zapisiDatoteku(t, filepath.Join(hq, "vodograd_hq_krivulje.csv"),
		zaglavlje+"2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;\n")
	for i := 1; i <= 2; i++ {
		var m []MaknutaKrivulja
		var err error
		uhvatiIspis(t, &os.Stdout, func() {
			var kg krivuljeGradnje
			kg, err = krivulje(db, koren, "")
			m = kg.maknute
		})
		if err != nil || len(m) != 0 {
			t.Errorf("%d. gradnja: maknuto %+v (%v)", i, m, err)
		}
		if n := broj(t, db, `SELECT count(*) FROM hq_krivulje WHERE letva='vodograd'`); n != 2 {
			t.Errorf("%d. gradnja: u arhivi je %d krivulja, a dvije datoteke imaju dvije", i, n)
		}
	}
}

// Zaključava da datoteka krivulja iz koje se ništa ne da pročitati ne odnese
// krivulje letve: prazna datoteka, npr. nakon prekinutog prijenosa, datoteka
// sa samo praznim retcima ispod zaglavlja i datoteka sa zarezom umjesto
// točke-zareza zaustave gradnju, a krivulje ostanu. Sa zarezom svuda poruka
// kaže zaglavlje, a kad su samo svi redci kratki, prvi od njih. Samo
// zaglavlje i dalje znači da letva krivulja nema.
func TestDatotekaKrivuljaBezIjednogRetkaNeOdneseKrivulje(t *testing.T) {
	zaglavlje := "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n"
	ispravna := zaglavlje +
		"2001-01-01;2010-12-31;-100;600;polinom;1;2;3;;DHMZ;\n" +
		"2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;\n"
	const (
		stala     = "gradnja je stala i nije maknula nijednu krivulju protoka, jer "
		ispravite = " Upišite krivulje u datoteku, ili samo zaglavlje ako letva krivulja nema, pa pokrenite gradnju ponovno."
	)
	for _, s := range []struct{ ime, sadrzaj, greska string }{
		{"prazna datoteka", "", stala + "je datoteka krivulja vodograd_hq_krivulje.csv prazna, bez ijednog retka." + ispravite},
		{"samo prazni redci", zaglavlje + "   \n;;;;;;;;;;\n",
			stala + "datoteka krivulja vodograd_hq_krivulje.csv ispod zaglavlja ima samo prazne retke." + ispravite},
		{"zarez umjesto točke-zareza", strings.ReplaceAll(ispravna, ";", ","),
			stala + "prvi redak datoteke krivulja vodograd_hq_krivulje.csv nije zaglavlje kakvo treba " +
				"(„vrijedi_od,vrijedi_do,od_cm,do_cm,oblik,p1,p2,p3,p4,izvor,napomena”). " + morajuStupci},
		{"zarez umjesto točke-zareza ispod zaglavlja", zaglavlje + strings.ReplaceAll(strings.TrimPrefix(ispravna, zaglavlje), ";", ","),
			stala + "datoteka krivulja vodograd_hq_krivulje.csv ima redak koji se ne da pročitati (redak 2). " +
				"„2001-01-01,2010-12-31,-100,600,polinom,1,2,3,,DHMZ,” ima 1 stupac, a redak krivulje ih ima barem 8, " +
				"odvojenih točkom-zarezom. Ispravite taj redak, pa pokrenite gradnju ponovno."},
	} {
		db := praznaArhiva(t)
		koren := t.TempDir()
		put := putKrivulja(koren, "dunav", "vodograd")
		zapisiDatoteku(t, put, ispravna)
		uhvatiIspis(t, &os.Stdout, func() {
			if _, err := krivulje(db, koren, ""); err != nil {
				t.Fatal(err)
			}
		})
		zapisiDatoteku(t, put, s.sadrzaj)
		var m []MaknutaKrivulja
		var err error
		uhvatiIspis(t, &os.Stdout, func() {
			var kg krivuljeGradnje
			kg, err = krivulje(db, koren, "")
			m = kg.maknute
		})
		greskaSadrzi(t, s.ime, err, s.greska)
		if len(m) != 0 {
			t.Errorf("%s: maknuto %+v", s.ime, m)
		}
		if n := broj(t, db, `SELECT count(*) FROM hq_odsjecci o JOIN hq_krivulje k ON k.id = o.krivulja
			WHERE k.letva='vodograd'`); n != 2 {
			t.Errorf("%s: broj odsječaka letve u arhivi je %d, a trebala su ostati dva", s.ime, n)
		}
	}
}

// Zaključava da jedan oštećen redak datoteke krivulja ne odnese krivulju iz
// arhive. Redak sa zarezima umjesto točke-zareza gradnja je tiho preskakala,
// a redak sa zarezom iza datuma čitala kao krivulju drugog početka. Zadnju
// krivulju letve onda je maknula kao da je u datoteci više nema, a letva je
// ostala bez protoka. Jedan zarez usred retka od jedanaest stupaca dao je
// nule ili pomaknute brojeve, a gradnja je njima zamijenila ispravnu
// krivulju istog početka. Sad gradnja stane, kaže koji je redak, a obje
// krivulje ostanu kakve su bile, bez ijedne nove. Broj retka je redak u
// datoteci, s praznim retkom koji čitač preskače.
func TestOstecenRedakKrivuljeNeOdneseKrivulju(t *testing.T) {
	const (
		zaglavlje = "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n\n"
		stara     = "2001-01-01;2010-12-31;-100;600;polinom;1;2;3;;DHMZ;\n"
		stala     = "gradnja je stala i nije maknula nijednu krivulju protoka, " +
			"jer datoteka krivulja vodograd_hq_krivulje.csv ima redak koji se ne da pročitati (redak 4). "
		ispravite = ". Ispravite taj redak, pa pokrenite gradnju ponovno."
	)
	for _, s := range []struct{ ime, redak, razlog string }{
		{"zarezi umjesto točke-zareza", "2011-01-01;;-100,600,polinom,4,5,6,,DHMZ,",
			"ima 3 stupca, a redak krivulje ih ima barem 8, odvojenih točkom-zarezom"},
		{"zarez iza datuma", "2011-01-01,;-100;600;polinom;4;5;6;;DHMZ;", "ne počinje datumom oblika 2001-01-01"},
		{"dva zareza iza datuma", "2011-01-01,,-100;600;polinom;4;5;6;;DHMZ;", "ne počinje datumom oblika 2001-01-01"},
		{"zarez među granicama", "2011-01-01;;-100,600;polinom;4;5;6;;DHMZ;",
			"u stupcu od_cm nema cijelog broja, nego „-100,600”, npr. zbog zareza umjesto točke-zareza"},
		{"zarez među p-stupcima", "2011-01-01;;-100;600;polinom;4,5;6;;DHMZ;",
			"u stupcu p4 nema broja, nego „DHMZ”, npr. zbog zareza umjesto točke-zareza"},
		{"prazna gornja granica", "2011-01-01;;-100;;polinom;4;5;6;;DHMZ;",
			"u stupcu do_cm nema cijelog broja, nego je stupac prazan, npr. zbog zareza umjesto točke-zareza"},
		{"točka tisućica", "2011-01-01;;-100;600;polinom;1.234,5;5;6;;DHMZ;",
			"u stupcu p1 nema broja, nego „1.234,5”, npr. zbog zareza umjesto točke-zareza"},
		{"oblik velikim slovom", "2011-01-01;;-100;600;Polinom;4;5;6;;DHMZ;",
			"u stupcu oblik ima „Polinom”, a smije imati samo polinom ili potencija, malim slovima"},
	} {
		t.Run(s.ime, func(t *testing.T) {
			err := gradnjaSOstecenomDatotekom(t, zaglavlje+stara+"2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;\n",
				zaglavlje+stara+s.redak+"\n")
			greskaSadrzi(t, s.ime, err, stala+"„"+s.redak+"” "+s.razlog+ispravite)
		})
	}
}

// Zaključava da se zarez među p-stupcima hvata i u datoteci bez stupca p4,
// kakve imaju letve na Dunavu: izvor tada dođe u stupac p3.
func TestZarezUDatoteciBezStupcaP4(t *testing.T) {
	const zaglavlje = "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;izvor;napomena\n"
	stara := "2001-01-01;2010-12-31;-100;600;polinom;1;2;3;DHMZ;\n"
	err := gradnjaSOstecenomDatotekom(t, zaglavlje+stara+"2011-01-01;;-100;600;polinom;4;5;6;DHMZ;\n",
		zaglavlje+stara+"2011-01-01;;-100;600;polinom;4,5;6;DHMZ;\n")
	greskaSadrzi(t, "zarez među p-stupcima", err, "(redak 3). „2011-01-01;;-100;600;polinom;4,5;6;DHMZ;” "+
		"u stupcu p3 nema broja, nego „DHMZ”, npr. zbog zareza umjesto točke-zareza. Ispravite taj redak")
}

// Zaključava da redak koji završi samim znakom CR, bez LF-a, ne odnese
// krivulju. Čitač takav redak spoji sa sljedećim, pa je krivulja od 2001.
// dobila napomenu „2011-01-01”, a krivulju od 2011. gradnja je maknula iz
// arhive kao da je u datoteci više nema. Sad gradnja stane na tom retku, a
// poruka CR pokaže kao razmak.
func TestRedakSamoSCRNeOdneseKrivulju(t *testing.T) {
	const (
		zaglavlje = "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n"
		stara     = "2001-01-01;2010-12-31;-100;600;polinom;1;2;3;;DHMZ;"
		nova      = "2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;"
	)
	err := gradnjaSOstecenomDatotekom(t, zaglavlje+stara+"\n"+nova+"\n", zaglavlje+stara+"\r"+nova+"\n")
	greskaSadrzi(t, "samo CR na kraju retka", err, "(redak 2). „"+stara+" "+nova+"” u sebi ima znak CR, "+
		"pa je u jednom retku datoteke više redaka krivulje. Svaki redak mora biti u svom retku. Ispravite taj redak")
}

// Zaključava da točka-zarez na kraju zaglavlja i svakog retka, kakav Excel i
// LibreOffice zapišu kad je tablica šira od podataka, ne mijenja krivulje.
// Prazno ime stupca nije nepoznat stupac.
func TestTockaZarezNaKrajuZaglavljaKrivulja(t *testing.T) {
	const (
		zaglavlje = "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena"
		redci     = "2001-01-01;2010-12-31;-100;600;polinom;1;2;3;;DHMZ;\n2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;\n"
	)
	if err := gradnjaSOstecenomDatotekom(t, zaglavlje+"\n"+redci,
		zaglavlje+";\n"+strings.ReplaceAll(redci, ";\n", ";;\n")); err != nil {
		t.Errorf("zaglavlje sa ; na kraju: %v", err)
	}
}

// morajuStupci je kraj poruke kad prvi redak datoteke krivulja nije
// zaglavlje.
const morajuStupci = "Prvi redak mora imati imena stupaca, npr. " +
	"vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena, " +
	"a svaki redak mora biti u svom retku datoteke. Ispravite datoteku, pa pokrenite gradnju ponovno."

// Zaključava da datoteka krivulja kojoj prvi redak nije zaglavlje ne odnese
// krivulje letve. Datoteka koja retke dijeli samo znakom CR čitala se kao
// samo zaglavlje, pa je gradnja maknula sve krivulje letve. Datoteci bez
// zaglavlja gubio se prvi odsječak, a ostali su se čitali po krivim
// stupcima. Sad gradnja stane, poruka navede prvi redak, skraćen kad je
// dug, a krivulje ostanu. I datoteka sa samo CR-om kojoj su ispod zaglavlja
// samo prazni redci stane: imena su ispravna, ali uz njih je znak CR.
func TestOstecenoZaglavljeKrivuljeNeOdneseKrivulje(t *testing.T) {
	const zaglavlje = "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena"
	redci := "2001-01-01;2010-12-31;-100;600;polinom;1;2;3;;DHMZ;\n" +
		"2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;\n"
	bezZaglavlja := "2001-01-01;2010-12-31;-100;600;polinom;1;2;3;;DHMZ;"
	for _, s := range []struct{ ime, sadrzaj, prviRedak string }{
		{"samo CR između redaka", strings.ReplaceAll(zaglavlje+"\n"+redci, "\n", "\r"), zaglavlje + " 2001-01-01;20…"},
		{"samo CR, ispod samo prazni redci", strings.ReplaceAll(zaglavlje+"\n;;;;;;;;;;\n;;;;;;;;;;\n", "\n", "\r"),
			(zaglavlje + " ;;;;;;;;;; ;;;;;;;;;;")[:80] + "…"},
		{"bez zaglavlja", redci, bezZaglavlja},
		{"nepoznat stupac", strings.Replace(zaglavlje, "napomena", "opis", 1) + "\n" + redci,
			"vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;opis"},
		{"bez stupca p3", strings.Replace(zaglavlje, ";p3", "", 1) + "\n" + redci,
			"vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p4;izvor;napomena"},
		{"razdoblje nije prvo", "od_cm;do_cm;vrijedi_od;vrijedi_do;oblik;p1;p2;p3;p4;izvor;napomena\n" + redci,
			"od_cm;do_cm;vrijedi_od;vrijedi_do;oblik;p1;p2;p3;p4;izvor;napomena"},
	} {
		t.Run(s.ime, func(t *testing.T) {
			err := gradnjaSOstecenomDatotekom(t, zaglavlje+"\n"+redci, s.sadrzaj)
			greskaSadrzi(t, s.ime, err, "gradnja je stala i nije maknula nijednu krivulju protoka, jer prvi redak "+
				"datoteke krivulja vodograd_hq_krivulje.csv nije zaglavlje kakvo treba („"+s.prviRedak+"”). "+morajuStupci)
		})
	}
}

// gradnjaSOstecenomDatotekom gradi letvu vodograd s datotekom krivulja
// prije, pa s datotekom poslije, i vraća grešku druge gradnje. Druga gradnja
// ne smije maknuti nijednu krivulju, a u arhivi moraju ostati obje krivulje
// iz prve gradnje kakve su bile (objeKrivuljeUArhivi).
func gradnjaSOstecenomDatotekom(t *testing.T, prije, poslije string) error {
	t.Helper()
	koren := t.TempDir()
	baza := filepath.Join(t.TempDir(), "arhiva.db")
	zapisiDatoteku(t, filepath.Join(koren, "dunav", "vodograd", "vodograd_his2000_vodostaj_dnevni_2024.csv"),
		"datum;vodostaj_cm\n2024-03-01;120\n")
	put := putKrivulja(koren, "dunav", "vodograd")
	zapisiDatoteku(t, put, prije)
	uhvatiIspis(t, &os.Stdout, func() {
		if _, err := Izgradi(koren, baza, "vodograd", nil); err != nil {
			t.Fatal(err)
		}
	})
	db, err := sql.Open("sqlite", baza)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	objeKrivuljeUArhivi(t, db, "prva gradnja")

	zapisiDatoteku(t, put, poslije)
	var iz Izvjestaj
	uhvatiIspis(t, &os.Stdout, func() {
		iz, err = Izgradi(koren, baza, "vodograd", nil)
	})
	if len(iz.MaknuteKrivulje) != 0 {
		t.Errorf("maknuto %+v", iz.MaknuteKrivulje)
	}
	objeKrivuljeUArhivi(t, db, "gradnja s oštećenom datotekom")
	return err
}

// objeKrivuljeUArhivi javlja grešku testa kad letva vodograd u arhivi nema
// točno dvije krivulje, od 1. 1. 2001. s p1 1 i od 1. 1. 2011. s p1 4, svaku
// s jednim odsječkom od -100 do 600 cm.
func objeKrivuljeUArhivi(t *testing.T, db *sql.DB, kada string) {
	t.Helper()
	if n := broj(t, db, `SELECT count(*) FROM hq_krivulje WHERE letva='vodograd'`); n != 2 {
		t.Errorf("%s: u arhivi je %d krivulja letve, a trebaju biti dvije", kada, n)
	}
	for _, k := range []struct {
		od string
		p1 float64
	}{{"2001-01-01", 1}, {"2011-01-01", 4}} {
		if n := broj(t, db, `SELECT count(*) FROM hq_odsjecci o JOIN hq_krivulje k ON k.id = o.krivulja
			WHERE k.letva='vodograd' AND k.vrijedi_od=?`, k.od); n != 1 {
			t.Errorf("%s: krivulja od %s ima u arhivi %d odsječaka, a treba jedan", kada, k.od, n)
		}
		if n := broj(t, db, `SELECT count(*) FROM hq_odsjecci o JOIN hq_krivulje k ON k.id = o.krivulja
			WHERE k.letva='vodograd' AND k.vrijedi_od=? AND o.od_cm=-100 AND o.do_cm=600 AND o.p1=?`, k.od, k.p1); n != 1 {
			t.Errorf("%s: krivulja od %s u arhivi nije ona iz prve gradnje", kada, k.od)
		}
	}
}
