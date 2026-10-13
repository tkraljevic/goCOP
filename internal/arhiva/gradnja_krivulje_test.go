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
		if err := krivulje(db, koren, "vodograd"); err != nil {
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
		if err := krivulje(db, koren, ""); err != nil {
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
			if err := krivulje(db, koren, ""); err != nil {
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

// PITANJE za odluku: krivulja koja je cijela maknuta iz datoteke ostaje u
// arhivi i nakon ponovne gradnje. Briše se samo krivulja čiji se početak
// valjanosti pojavi u datoteci, pa ista mapa daje drukčiju arhivu ovisno o
// tome gradi li se iznova ili preko stare. Gradnja ipak namjerno ne briše
// ono čemu u stablu nema datoteke, a krivulje letve stižu i paketom.
// Popravak ne smije brisati krivulje primljene paketom.
func TestMaknutaKrivuljaNestajePriPonovnojGradnji(t *testing.T) {
	t.Skip("PITANJE: internal/arhiva/gradnja.go:1094 — briše li gradnja krivulju koje više nema u datoteci; krivulje stižu i paketom")
	db := praznaArhiva(t)
	koren := t.TempDir()
	put := putKrivulja(koren, "dunav", "vodograd")
	zaglavlje := "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n"
	zapisiDatoteku(t, put, zaglavlje+
		"2001-01-01;2010-12-31;-100;600;polinom;1;2;3;;DHMZ;\n"+
		"2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;\n")
	if err := krivulje(db, koren, ""); err != nil {
		t.Fatal(err)
	}
	zapisiDatoteku(t, put, zaglavlje+"2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;\n")
	if err := krivulje(db, koren, ""); err != nil {
		t.Fatal(err)
	}
	if n := broj(t, db, `SELECT count(*) FROM hq_krivulje WHERE letva='vodograd'`); n != 1 {
		t.Errorf("u arhivi je %d krivulja, a datoteka ima jednu", n)
	}
}

// Zaključava da se datoteka krivulja koja se ne da pročitati ili upisati
// javlja greškom, a ne preskače: protok bez krivulje tiho bi nestao.
func TestKrivuljeKojeSeNeDajuProcitati(t *testing.T) {
	ispravna := "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n" +
		"2001-01-01;;-100;600;polinom;1;2;3;;DHMZ;\n"

	t.Run("krivi uzorak putanje", func(t *testing.T) {
		db := praznaArhiva(t)
		err := krivulje(db, filepath.Join(t.TempDir(), "mapa["), "")
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
		if err := krivulje(db, koren, ""); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("greška %v, očekivano da datoteke nema", err)
		}
	})
	t.Run("mapa umjesto datoteke", func(t *testing.T) {
		db := praznaArhiva(t)
		koren := t.TempDir()
		if err := os.MkdirAll(putKrivulja(koren, "dunav", "vodograd"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := krivulje(db, koren, ""); err == nil {
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
					if err := krivulje(db, koren, ""); err != nil {
						t.Fatal(err)
					}
				}
				kvar(t, db, s.kada, s.tablica, "")
				greskaSadrzi(t, s.ime, krivulje(db, koren, ""), "namjerni kvar")
			})
		})
	}
}
