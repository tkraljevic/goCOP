package his2000

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Zaključava spajanje krivulja iz izvoza s datotekom koju letva već ima:
// razdoblja iz izvoza dodaju se ili zamijene isto razdoblje, a ostala
// ostaju. Brojke su izmišljene; razdoblja prate Donji Miholjac, gdje izvoz
// spojene postaje ima krivulje 1926.–2016., a izvoz nove 1993.–2026.

// krivuljaTesta je jedno razdoblje s jednim polinomskim odsječkom.
type krivuljaTesta struct {
	od, do time.Time
	p1     string
}

func izvozKrivulja(sifra string, krivulje ...krivuljaTesta) *Sadrzaj {
	s := &Sadrzaj{Ime: "izvoz/krivulje.csv", Postaja: Postaja{Sifra: sifra}}
	for _, k := range krivulje {
		s.Krivulje = append(s.Krivulje, Krivulja{Od: k.od, Do: k.do, Odsjecci: []Odsjecak{
			{OdCm: -100, DoCm: 600, Oblik: models.OblikPolinom, P1: k.p1, P2: "200", P3: "300"},
		}})
	}
	return s
}

func godina(od, do int, p1 string) krivuljaTesta {
	return krivuljaTesta{od: dan(od, 1, 1), do: dan(do, 12, 31), p1: p1}
}

// redakKrivulje je redak datoteke za krivulju iz izvozKrivulja.
func redakKrivulje(od, do, p1 string) string {
	return od + ";" + do + ";-100;600;polinom;" + p1 + ";200;300;;DHMZ, HIS-2000;\n"
}

// Stari izvoz 1926.–2016. pa novi 1993.–2026. daju krivulje 1926.–2026.:
// starije koje novi nema ostaju, zajednička razdoblja uzimaju se iz novog,
// a zadnja ostaje otvorena. Dnevnik kaže što se razlikuje i što ostaje.
// Izvornik novog izvoza ne gazi stari, a isti izvoz drugi put ne mijenja ni
// datoteku ni izvornike.
func TestPosaoKrivuljeSpajaSaStarijima(t *testing.T) {
	cilj := t.TempDir()
	stari := izvozKrivulja("5555", godina(1926, 1965, "10"), godina(1993, 1993, "20"), godina(2016, 2016, "30"))
	novi := izvozKrivulja("5150", godina(1993, 1993, "21"), godina(2016, 2016, "30"), godina(2025, 2026, "40"))
	put := filepath.Join(cilj, "hq", "miholjac_hq_krivulje.csv")

	p := &Posao{Cilj: cilj, Letva: "miholjac"}
	if err := p.Krivulje(stari, []byte("izvoz 5555")); err != nil {
		t.Fatal(err)
	}
	if got := procitajDatoteku(t, put); got != string(KrivuljeCSV(stari)) {
		t.Fatalf("prvi uvoz:\n%s", got)
	}

	var dnevnik strings.Builder
	p = &Posao{Cilj: cilj, Letva: "miholjac", Dnevnik: &dnevnik}
	if err := p.Krivulje(novi, []byte("izvoz 5150")); err != nil {
		t.Fatal(err)
	}
	spojeno := ZaglavljeKrivulja + "\n" +
		redakKrivulje("1926-01-01", "1965-12-31", "10") +
		redakKrivulje("1993-01-01", "1993-12-31", "21") +
		redakKrivulje("2016-01-01", "2016-12-31", "30") +
		redakKrivulje("2025-01-01", "", "40")
	if got := procitajDatoteku(t, put); got != spojeno {
		t.Errorf("spojene krivulje:\n%s", got)
	}
	for _, dio := range []string{
		"razlikuje se: 1993-01-01, upisuje se krivulja iz izvoza\n",
		"ostaje iz zatečene datoteke: 1 krivulja koje u izvozu nema\n",
		"nakon spajanja: 4 krivulje\n",
	} {
		if !strings.Contains(dnevnik.String(), dio) {
			t.Errorf("u dnevniku nema %q:\n%s", dio, dnevnik.String())
		}
	}
	if strings.Contains(dnevnik.String(), "razlikuje se: 2016-01-01") {
		t.Errorf("ista krivulja otvorenog kraja javljena je kao razlika:\n%s", dnevnik.String())
	}
	izvornik := filepath.Join(cilj, "hq", "izvornik")
	if procitajDatoteku(t, filepath.Join(izvornik, "krivulje.csv")) != "izvoz 5555" ||
		procitajDatoteku(t, filepath.Join(izvornik, "krivulje-5150.csv")) != "izvoz 5150" {
		t.Error("izvornik novog izvoza nije spremljen uz stari")
	}

	if err := p.Krivulje(novi, []byte("izvoz 5150")); err != nil {
		t.Fatal(err)
	}
	if got := procitajDatoteku(t, put); got != spojeno {
		t.Errorf("isti izvoz promijenio je datoteku:\n%s", got)
	}
	if stavke, _ := os.ReadDir(izvornik); len(stavke) != 2 {
		t.Errorf("isti izvoz spremljen je ponovno: %d izvornika", len(stavke))
	}

	// Proba starog izvoza kaže da bi vratio 1993. kakva je bila, a ne
	// zapisuje ništa.
	dnevnik.Reset()
	p = &Posao{Cilj: cilj, Letva: "miholjac", Probno: true, Dnevnik: &dnevnik}
	if err := p.Krivulje(stari, []byte("izvoz 5555")); err != nil {
		t.Fatal(err)
	}
	if got := procitajDatoteku(t, put); got != spojeno || p.Zapisano != 1 {
		t.Errorf("proba je zapisala, ili nije izbrojila (%d):\n%s", p.Zapisano, got)
	}
	if !strings.Contains(dnevnik.String(), "razlikuje se: 1993-01-01") ||
		!strings.Contains(dnevnik.String(), "ostaje iz zatečene datoteke: 1 krivulja koje u izvozu nema") {
		t.Errorf("dnevnik probe:\n%s", dnevnik.String())
	}
	// Krivulja 2016. u starom izvozu je zadnja, pa joj kraj nije stvaran:
	// ostaje kraj koji je donio novi izvoz.
	if strings.Contains(dnevnik.String(), "razlikuje se: 2016-01-01") {
		t.Errorf("kraj razdoblja starog izvoza javljen je kao razlika:\n%s", dnevnik.String())
	}
}

// Datoteka koju je zapisao uvoz istog izvoza ostaje bajt za bajt ista, i
// ništa se ne javlja kao razlika.
func TestSpojiKrivuljeIstiIzvozNeMijenjaDatoteku(t *testing.T) {
	s := krivuljeUzorak()
	sp, err := SpojiKrivulje(KrivuljeCSV(s), s)
	if err != nil {
		t.Fatal(err)
	}
	if string(sp.CSV) != string(KrivuljeCSV(s)) || sp.Razlikuju != nil || sp.Ostaju != nil ||
		sp.Krivulja != 2 || sp.Zatecenih != 2 || sp.ZatecenihOdsjecaka != 3 {
		t.Errorf("isti izvoz: %+v\n%s", sp, sp.CSV)
	}
	prazno, err := SpojiKrivulje(nil, s)
	if err != nil || string(prazno.CSV) != string(KrivuljeCSV(s)) || prazno.Zatecenih != 0 {
		t.Errorf("bez zatečenih: %+v, %v", prazno, err)
	}
}

// Zatečena krivulja otvorenog kraja koja više nije zadnja dobiva kraj dan
// prije sljedeće. Kad izvoz donosi starije krivulje, zadnja zatečena ostaje
// otvorena, a zadnja iz izvoza vrijedi do dana prije prve zatečene, kao da
// je uvezena prva. Promijenjen zatvoren kraj je razlika.
func TestSpojiKrivuljeKrajevi(t *testing.T) {
	zateceno := ZaglavljeKrivulja + "\n" +
		redakKrivulje("2005-01-01", "2008-12-31", "5") +
		redakKrivulje("2010-03-01", "", "10")
	sp, err := SpojiKrivulje([]byte(zateceno), izvozKrivulja("1", godina(2005, 2006, "5"), godina(2020, 2021, "20")))
	if err != nil {
		t.Fatal(err)
	}
	zeli := ZaglavljeKrivulja + "\n" +
		redakKrivulje("2005-01-01", "2006-12-31", "5") +
		redakKrivulje("2010-03-01", "2019-12-31", "10") +
		redakKrivulje("2020-01-01", "", "20")
	if string(sp.CSV) != zeli || strings.Join(sp.Razlikuju, ",") != "2005-01-01" || strings.Join(sp.Ostaju, ",") != "2010-03-01" {
		t.Errorf("novija krivulja iz izvoza: %v %v\n%s", sp.Razlikuju, sp.Ostaju, sp.CSV)
	}

	sp, err = SpojiKrivulje([]byte(zateceno), izvozKrivulja("1", godina(1990, 2000, "1")))
	if err != nil {
		t.Fatal(err)
	}
	zeli = ZaglavljeKrivulja + "\n" +
		redakKrivulje("1990-01-01", "2004-12-31", "1") +
		redakKrivulje("2005-01-01", "2008-12-31", "5") +
		redakKrivulje("2010-03-01", "", "10")
	if string(sp.CSV) != zeli || sp.Razlikuju != nil || strings.Join(sp.Ostaju, ",") != "2005-01-01,2010-03-01" || sp.Krivulja != 3 {
		t.Errorf("starija krivulja iz izvoza: %+v\n%s", sp, sp.CSV)
	}
}

// Redoslijed uvoza nije važan. Zadnja krivulja izvoza nosi kraj razdoblja
// izvoza, a ne kraj valjanosti, pa vrijedi do sljedeće krivulje u datoteci,
// a zatečena krivulja istog početka zadrži svoj kraj. Stariji izvoz uvezen
// poslije novijeg zato ne ostavlja rupu, a izvoz koji je već uvezen ne
// mijenja datoteku i ne javlja razliku.
func TestSpojiKrivuljeRedoslijedUvoza(t *testing.T) {
	stari := izvozKrivulja("1", godina(2005, 2009, "5"), godina(2010, 2012, "10")) // izvoz iz 2012.
	novi := izvozKrivulja("2", godina(2015, 2026, "15"))
	uvezi := func(izvozi ...*Sadrzaj) []byte {
		t.Helper()
		var csv []byte
		for _, s := range izvozi {
			sp, err := SpojiKrivulje(csv, s)
			if err != nil {
				t.Fatal(err)
			}
			csv = sp.CSV
		}
		return csv
	}
	zeli := ZaglavljeKrivulja + "\n" +
		redakKrivulje("2005-01-01", "2009-12-31", "5") +
		redakKrivulje("2010-01-01", "2014-12-31", "10") +
		redakKrivulje("2015-01-01", "", "15")
	if got := string(uvezi(stari, novi)); got != zeli {
		t.Errorf("stari pa novi:\n%s", got)
	}
	if got := string(uvezi(novi, stari)); got != zeli {
		t.Errorf("novi pa stari:\n%s", got)
	}
	for _, s := range []*Sadrzaj{stari, novi} {
		sp, err := SpojiKrivulje([]byte(zeli), s)
		if err != nil || string(sp.CSV) != zeli || sp.Razlikuju != nil {
			t.Errorf("ponovni uvoz %s: %v, razlikuju %v\n%s", s.Postaja.Sifra, err, sp.Razlikuju, sp.CSV)
		}
	}
}

// Zatečena datoteka čita se kao u gradnji arhive: razdoblje iz prva dva
// stupca, ostalo po zaglavlju, odsječci istog početka zajedno. Starija
// datoteka bez stupca p4 prepiše se pod današnjim zaglavljem, a napomena s
// točkom-zarezom ostaje cijela.
func TestSpojiKrivuljeCitaKaoGradnja(t *testing.T) {
	zateceno := "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;izvor;napomena\r\n" +
		"1976-01-01;1978-12-31;344;600;polinom;0;645,83;-892;DHMZ, HIS-2000;\r\n" +
		"1983-01-01;1983-12-31; -130 ;146;polinom;5,71;214,14;414,32;DHMZ;\"ručno; provjereno\"\r\n" +
		"1976-01-01;1978-12-31;-50;344;polinom;1;2;3;DHMZ;\r\n"
	sp, err := SpojiKrivulje([]byte(zateceno), izvozKrivulja("1", godina(2001, 2001, "7")))
	if err != nil {
		t.Fatal(err)
	}
	zeli := ZaglavljeKrivulja + "\n" +
		"1976-01-01;1978-12-31;344;600;polinom;0;645,83;-892;;DHMZ, HIS-2000;\n" +
		"1976-01-01;1978-12-31;-50;344;polinom;1;2;3;;DHMZ;\n" +
		"1983-01-01;1983-12-31;-130;146;polinom;5,71;214,14;414,32;;DHMZ;\"ručno; provjereno\"\n" +
		redakKrivulje("2001-01-01", "", "7")
	if string(sp.CSV) != zeli || sp.Zatecenih != 2 || sp.ZatecenihOdsjecaka != 3 {
		t.Errorf("starija datoteka:\n%s", sp.CSV)
	}
}

// Izvoz se čita kao i zatečena datoteka: dvije krivulje istog početka su
// jedna, sa svim odsječcima i krajem prve, a razdoblje bez odsječaka nije
// krivulja, pa zatečena krivulja istog početka ostaje. Otvorena ostaje
// krivulja najkasnijeg početka, i kad izvoz nije poredan.
func TestSpojiKrivuljeIzvozKaoGradnja(t *testing.T) {
	odsjecak := func(od, do int, p1 string) []Odsjecak {
		return []Odsjecak{{OdCm: od, DoCm: do, Oblik: models.OblikPolinom, P1: p1, P2: "200", P3: "300"}}
	}
	s := &Sadrzaj{Krivulje: []Krivulja{
		{Od: dan(2004, 1, 1), Do: dan(2004, 12, 31), Odsjecci: odsjecak(100, 500, "7")},
		{Od: dan(2001, 1, 1), Do: dan(2001, 12, 31), Odsjecci: odsjecak(-100, 100, "1")},
		{Od: dan(2001, 1, 1), Do: dan(2003, 12, 31), Odsjecci: odsjecak(100, 500, "4")},
		{Od: dan(1990, 1, 1), Do: dan(1990, 12, 31)},
	}}
	sp, err := SpojiKrivulje([]byte(ZaglavljeKrivulja+"\n"+redakKrivulje("1990-01-01", "2000-12-31", "9")), s)
	if err != nil {
		t.Fatal(err)
	}
	zeli := ZaglavljeKrivulja + "\n" +
		redakKrivulje("1990-01-01", "2000-12-31", "9") +
		"2001-01-01;2001-12-31;-100;100;polinom;1;200;300;;DHMZ, HIS-2000;\n" +
		"2001-01-01;2001-12-31;100;500;polinom;4;200;300;;DHMZ, HIS-2000;\n" +
		"2004-01-01;;100;500;polinom;7;200;300;;DHMZ, HIS-2000;\n"
	if string(sp.CSV) != zeli || sp.Krivulja != 3 || sp.Razlikuju != nil || strings.Join(sp.Ostaju, ",") != "1990-01-01" {
		t.Errorf("izvoz s dvostrukim i praznim razdobljem: %v %v\n%s", sp.Razlikuju, sp.Ostaju, sp.CSV)
	}
	if got := string(KrivuljeCSV(s)); got != strings.Replace(zeli, redakKrivulje("1990-01-01", "2000-12-31", "9"), "", 1) {
		t.Errorf("KrivuljeCSV:\n%s", got)
	}
}

// Redak koji gradnja ne bi pročitala, datum koji nije datum i datoteka koja
// nije tablica zaustavljaju spajanje, a poruka kaže koji je redak. Kad se
// zatečena datoteka ne da pročitati, greška nosi njezin put.
func TestSpojiKrivuljeOdbija(t *testing.T) {
	s := izvozKrivulja("1", godina(2001, 2001, "7"))
	for _, sl := range []struct{ opis, zateceno, greska string }{
		{"kratak redak", ZaglavljeKrivulja + "\nkratak-redak\n",
			"redak „kratak-redak” ima 1 stupac, a redak krivulje ih ima barem 8"},
		{"tri stupca", ZaglavljeKrivulja + "\na;b;c\n", "redak „a;b;c” ima 3 stupca, a redak krivulje ih ima barem 8"},
		{"datum", ZaglavljeKrivulja + "\n2001-1-1;;0;100;polinom;1;2;3;;DHMZ;\n",
			"redak „2001-1-1;;0;100;polinom;1;2;3;;DHMZ;” ne počinje datumom oblika 2001-01-01"},
		{"nije tablica", ZaglavljeKrivulja + "\n2001-01-01;\"x\"y;0\n", "ne čita se kao tablica: "},
	} {
		if _, err := SpojiKrivulje([]byte(sl.zateceno), s); err == nil || !strings.HasPrefix(err.Error(), sl.greska) {
			t.Errorf("%s: %v", sl.opis, err)
		}
	}

	mapa := t.TempDir()
	put := filepath.Join(mapa, "letva_hq_krivulje.csv")
	if sp, err := SpojiSDatotekom(put, s); err != nil || string(sp.CSV) != string(KrivuljeCSV(s)) {
		t.Errorf("bez datoteke: %v\n%s", err, sp.CSV)
	}
	if err := os.WriteFile(put, []byte(ZaglavljeKrivulja+"\nkratak-redak\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SpojiSDatotekom(put, s); err == nil || !strings.HasPrefix(err.Error(), put+": redak „kratak-redak”") {
		t.Errorf("nečitljiv redak: %v", err)
	}
	if _, err := SpojiSDatotekom(mapa, s); err == nil || !strings.Contains(err.Error(), mapa) {
		t.Errorf("mapa umjesto datoteke: %v", err)
	}
}

// Izvornik ide pod svojim imenom. Kad pod njim stoji drugi izvoz, dobiva
// šifru postaje (bez znakova koji bi izašli iz mape), pa redni broj; isti
// izvoz ostaje pod imenom pod kojim već stoji.
func TestImeIzvornika(t *testing.T) {
	mapa := t.TempDir()
	zapisi := func(ime, sadrzaj string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(mapa, ime), []byte(sadrzaj), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, sl := range []struct {
		opis, ime, sifra, sadrzaj, zeli string
		prije                           func()
	}{
		{"prazna mapa", "izvoz/krivulje.csv", "5150", "A", "krivulje.csv", func() {}},
		{"isti izvoz", "krivulje.csv", "5150", "A", "krivulje.csv", func() { zapisi("krivulje.csv", "A") }},
		{"drugi izvoz", "krivulje.csv", " ../51/50 ", "B", "krivulje-5150.csv", func() {}},
		{"drugi izvoz iste postaje", "krivulje.csv", "5150", "C", "krivulje-5150-2.csv", func() { zapisi("krivulje-5150.csv", "B") }},
		{"bez šifre", "krivulje.csv", "", "D", "krivulje-2.csv", func() {}},
	} {
		sl.prije()
		if got, err := imeIzvornika(mapa, sl.ime, sl.sifra, []byte(sl.sadrzaj)); err != nil || got != sl.zeli {
			t.Errorf("%s: %q, %v", sl.opis, got, err)
		}
	}

	mapaNaMjestu(t, mapa, "profil.csv")
	if _, err := imeIzvornika(mapa, "profil.csv", "", nil); err == nil {
		t.Error("mapa na mjestu izvornika je prošla")
	}
	puna := t.TempDir()
	for _, ime := range kandidatiImena("k.csv", "") {
		if err := os.WriteFile(filepath.Join(puna, ime), []byte("drugi"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := imeIzvornika(puna, "k.csv", "", []byte("novi")); err == nil ||
		err.Error() != "u mapi "+puna+" već je 99 drugih izvoza pod imenima od k.csv do k-99.csv" {
		t.Errorf("puna mapa: %v", err)
	}
}

// Zapis krivulja: izvornik se sprema prvi, pa datoteka; svaki neuspio korak
// kaže što nije uspjelo.
func TestZapisiKrivulje(t *testing.T) {
	s := izvozKrivulja("5150", godina(2001, 2001, "7"))
	s.Ime = "krivulje.csv"
	hq := filepath.Join(t.TempDir(), "hq")
	put := filepath.Join(hq, "letva_hq_krivulje.csv")
	if ime, err := ZapisiKrivulje(put, []byte("csv"), s, []byte("izvoz")); err != nil || ime != "krivulje.csv" ||
		procitajDatoteku(t, put) != "csv" || procitajDatoteku(t, filepath.Join(hq, "izvornik", "krivulje.csv")) != "izvoz" {
		t.Errorf("zapis: %q, %v", ime, err)
	}

	if _, err := ZapisiKrivulje(filepath.Join(ciljDatoteka(t), "x.csv"), nil, s, nil); err == nil {
		t.Error("mapa izvornika pod datotekom je prošla")
	}
	c := t.TempDir()
	mapaNaMjestu(t, c, "izvornik", "krivulje.csv")
	if _, err := ZapisiKrivulje(filepath.Join(c, "x.csv"), nil, s, nil); err == nil ||
		!strings.HasPrefix(err.Error(), "izvornik se nije spremio: ") {
		t.Errorf("mapa na mjestu izvornika: %v", err)
	}
	c = t.TempDir()
	mapaNaMjestu(t, c, "x.csv")
	if _, err := ZapisiKrivulje(filepath.Join(c, "x.csv"), nil, s, []byte("izvoz")); err == nil ||
		!strings.HasPrefix(err.Error(), "upis nije uspio: ") {
		t.Errorf("mapa na mjestu krivulja: %v", err)
	}
	if procitajDatoteku(t, filepath.Join(c, "izvornik", "krivulje.csv")) != "izvoz" {
		t.Error("izvornik nije spremljen prije datoteke krivulja")
	}

	if os.Geteuid() == 0 {
		t.Skip("pod rootom prava nad mapom ne vrijede")
	}
	c = t.TempDir()
	mapaNaMjestu(t, c, "izvornik")
	if err := os.Chmod(filepath.Join(c, "izvornik"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(c, "izvornik"), 0o755) })
	if _, err := ZapisiKrivulje(filepath.Join(c, "x.csv"), nil, s, []byte("izvoz")); err == nil ||
		!strings.HasPrefix(err.Error(), "izvornik se nije spremio: ") {
		t.Errorf("izvornik u mapu samo za čitanje: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c, "x.csv")); !os.IsNotExist(err) {
		t.Errorf("krivulje su zapisane iako izvornik nije: %v", err)
	}
}
