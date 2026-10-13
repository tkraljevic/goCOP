package arhiva

import (
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
// obzira na velika slova i razmake; odsječci iste krivulje dolaze u svojim
// retcima; oblik koji nije potencija čita se kao polinom, decimalni zarez kao
// točka, a prekratak redak se preskače. Druga letva se ne dira.
func TestKrivuljeSeCitajuPoZaglavlju(t *testing.T) {
	db := praznaArhiva(t)
	koren := t.TempDir()
	zapisiDatoteku(t, putKrivulja(koren, "dunav", "vodograd"),
		"vrijedi_od;vrijedi_do;od_cm;do_cm;Oblik;p1;p2;p3; P4 ;izvor;napomena\n"+
			"2001-01-01;2010-12-31;-100;150;polinom;0,1;2,5;30;;DHMZ;stara krivulja\n"+
			"2001-01-01;2010-12-31;150;600;potencija;12,5;1,6;-50;3;DHMZ;stara krivulja\n"+
			"2011-01-01;;-120;500;;1;2;3;;mjerenje 2011.;\n"+
			"2012-01-01;;1\n")
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
			if m, err = krivulje(db, koren, ""); err != nil {
				t.Fatal(err)
			}
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
	uDatoteci := map[string]int64{"2011-01-01": 1}
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
