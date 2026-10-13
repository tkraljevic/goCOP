package arhiva

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// upisiKrivulju upisuje krivulju letve s jednim odsječkom; p1 je oznaka po
// kojoj test poslije prepozna krivulju.
func upisiKrivulju(t *testing.T, db *sql.DB, letva, od, do string, p1 float64) {
	t.Helper()
	res, err := db.Exec(`INSERT INTO hq_krivulje (letva, vrijedi_od, vrijedi_do) VALUES (?, ?, ?)`, letva, od, do)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO hq_odsjecci (krivulja, od_cm, do_cm, p1, p2, p3) VALUES (?, -150, 600, ?, 1, 1)`, id, p1); err != nil {
		t.Fatal(err)
	}
}

// oznaciPrimljenu bilježi da je letva stigla paketom s drugog čvora.
func oznaciPrimljenu(t *testing.T, db *sql.DB, letva string) {
	t.Helper()
	if err := zapisiPrimljeno(db, Manifest{Letva: letva, Izdanje: 6, Otisak: "otisak", Izdao: "cop-osijek-unraid"}, true, ""); err != nil {
		t.Fatal(err)
	}
}

// Zaključava pravilo iz Odluka (13. 10. 2026.): primljeno paketom se ne dira.
// Letva koja je stigla paketom s drugog čvora zadrži krivulje kojih lokalna
// datoteka nema, a izvještaj ih vrati kao zadržane. Krivulje iz datoteke se i
// dalje upišu. Laptop i Unraid znali su imati različite krivulje Donjeg
// Miholjca, pa bi starija lokalna datoteka odnijela krivulje iz paketa. Letva
// koja nije stigla paketom i dalje prati svoju datoteku.
func TestKrivuljeLetvePrimljenePaketomOstaju(t *testing.T) {
	db := praznaArhiva(t)
	koren := t.TempDir()
	zaglavlje := "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n"
	zapisiDatoteku(t, putKrivulja(koren, "drava", "donji-miholjac"), zaglavlje+
		"2015-01-01;2015-12-31;-150;600;polinom;15;1;1;;DHMZ;\n"+
		"2016-01-01;;-150;600;polinom;16;1;1;;DHMZ;\n")
	oznaciPrimljenu(t, db, "donji-miholjac")
	upisiKrivulju(t, db, "donji-miholjac", "2016-01-01", "2016-12-31", 116)
	upisiKrivulju(t, db, "donji-miholjac", "2017-01-01", "2018-12-31", 117)
	upisiKrivulju(t, db, "donji-miholjac", "2025-01-01", "", 125)

	zapisiDatoteku(t, putKrivulja(koren, "dunav", "vodograd"), zaglavlje+
		"2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;\n")
	upisiKrivulju(t, db, "vodograd", "2001-01-01", "2010-12-31", 1)

	var maknute, zadrzane []MaknutaKrivulja
	uhvatiIspis(t, &os.Stdout, func() {
		var err error
		if maknute, zadrzane, err = krivulje(db, koren, ""); err != nil {
			t.Fatal(err)
		}
	})
	zeliZadrzane := []MaknutaKrivulja{
		{Letva: "donji-miholjac", VrijediOd: "2017-01-01", VrijediDo: "2018-12-31"},
		{Letva: "donji-miholjac", VrijediOd: "2025-01-01"},
	}
	if len(zadrzane) != 2 || zadrzane[0] != zeliZadrzane[0] || zadrzane[1] != zeliZadrzane[1] {
		t.Errorf("zadržano %+v, očekivano %+v", zadrzane, zeliZadrzane)
	}
	if len(maknute) != 1 || maknute[0] != (MaknutaKrivulja{Letva: "vodograd", VrijediOd: "2001-01-01", VrijediDo: "2010-12-31"}) {
		t.Errorf("maknuto %+v, a treba samo krivulja letve koja nije stigla paketom", maknute)
	}
	for _, s := range []struct {
		od string
		p1 float64
	}{{"2015-01-01", 15}, {"2017-01-01", 117}, {"2025-01-01", 125}} {
		if n := broj(t, db, `SELECT count(*) FROM hq_krivulje k JOIN hq_odsjecci o ON o.krivulja = k.id
			WHERE k.letva = 'donji-miholjac' AND k.vrijedi_od = ? AND o.p1 = ?`, s.od, s.p1); n != 1 {
			t.Errorf("krivulja od %s s odsječkom %g nije u arhivi", s.od, s.p1)
		}
	}
	if n := broj(t, db, `SELECT count(*) FROM hq_krivulje WHERE letva = 'donji-miholjac'`); n != 4 {
		t.Errorf("letva primljena paketom ima %d krivulja, a trebaju četiri: dvije iz datoteke i dvije iz paketa", n)
	}
	if n := broj(t, db, `SELECT count(*) FROM hq_krivulje WHERE letva = 'vodograd'`); n != 1 {
		t.Errorf("letva koja nije stigla paketom ima %d krivulja, a datoteka jednu", n)
	}
}

// Zaključava da se greška pri čitanju evidencije primljenih paketa javi, a ne
// preskoči: bez nje gradnja ne zna smije li micati krivulje.
func TestKrivuljeKadSeNeZnaJeLiLetvaPrimljena(t *testing.T) {
	put := filepath.Join(t.TempDir(), "arhiva.db")
	db := napraviArhivu(t, put)
	upisiKrivulju(t, db, "vodograd", "2001-01-01", "", 1)
	db.Close()
	_, _, err := izvanDatotekaLetve(kvarUpita(t, put, "FROM primljena_izdanja", 1), "vodograd", map[string]int64{})
	greskaSadrzi(t, "evidencija primljenih", err, "je li letva vodograd stigla paketom: ")

	db = napraviArhivu(t, filepath.Join(t.TempDir(), "druga.db"))
	defer db.Close()
	oznaciPrimljenu(t, db, "vodograd")
	upisiKrivulju(t, db, "vodograd", "2001-01-01", "", 1)
	if _, err := db.Exec(`DROP TABLE hq_krivulje`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := izvanDatotekaLetve(db, "vodograd", map[string]int64{}); err == nil || !strings.Contains(err.Error(), "hq_krivulje") {
		t.Errorf("krivulje primljene letve koje se ne daju pročitati: %v", err)
	}
}

// Zaključava rečenicu kojom izvještaj gradnje javlja krivulju koja nije
// maknuta jer je letva stigla paketom, s razdobljem valjanosti.
func TestOpisZadrzaneKrivulje(t *testing.T) {
	for _, s := range []struct {
		k    MaknutaKrivulja
		zeli string
	}{
		{MaknutaKrivulja{Letva: "donji-miholjac", VrijediOd: "2017-01-01", VrijediDo: "2018-12-31"},
			"Krivulja protoka koja je vrijedila od 1. 1. 2017. do 31. 12. 2018. nije maknuta iz arhive, iako je u datoteci krivulja nema, jer je letva stigla paketom s drugog čvora."},
		{MaknutaKrivulja{Letva: "donji-miholjac", VrijediOd: "2025-01-01"},
			"Krivulja protoka koja vrijedi od 1. 1. 2025. nije maknuta iz arhive, iako je u datoteci krivulja nema, jer je letva stigla paketom s drugog čvora."},
	} {
		if got := s.k.OpisZadrzane(); got != s.zeli {
			t.Errorf("%+v: %q, očekivano %q", s.k, got, s.zeli)
		}
	}
}

// Zaključava da gradnja zadržanu krivulju javi i u izvještaju i u ispisu koji
// čovjek čita na stranici.
func TestGradnjaJavljaZadrzanuKrivulju(t *testing.T) {
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
	db, err := sql.Open("sqlite", baza)
	if err != nil {
		t.Fatal(err)
	}
	oznaciPrimljenu(t, db, "vodograd")
	db.Close()

	zapisiDatoteku(t, put, zaglavlje+"2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;\n")
	var ispis strings.Builder
	var iz Izvjestaj
	uhvatiIspis(t, &os.Stdout, func() {
		if iz, err = Izgradi(koren, baza, "vodograd", &ispis); err != nil {
			t.Fatal(err)
		}
	})
	if len(iz.MaknuteKrivulje) != 0 || len(iz.ZadrzaneKrivulje) != 1 || iz.ZadrzaneKrivulje[0].VrijediOd != "2001-01-01" {
		t.Errorf("izvještaj: maknute %+v, zadržane %+v", iz.MaknuteKrivulje, iz.ZadrzaneKrivulje)
	}
	if !strings.Contains(ispis.String(), "vodograd         Krivulja protoka koja je vrijedila od 1. 1. 2001. do 31. 12. 2010. nije maknuta iz arhive, iako je u datoteci krivulja nema, jer je letva stigla paketom s drugog čvora.\n") {
		t.Errorf("ispis gradnje:\n%s", ispis.String())
	}
}
