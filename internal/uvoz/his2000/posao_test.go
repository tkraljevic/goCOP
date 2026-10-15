package his2000

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Zaključava zapisivanje pročitanog izvoza u stablo (Posao): krivulje s
// izvornikom, snimke korita, vodomjerenja, nizove uz zatečene datoteke,
// sažetak posla i razvrstavanje datoteka izvoza po zaglavlju. Ulazi su mali
// i izmišljeni, zapisuje se u privremenu mapu.

func dan(g int, m time.Month, d int) time.Time { return time.Date(g, m, d, 0, 0, 0, 0, time.UTC) }

func procitajDatoteku(t *testing.T, put string) string {
	t.Helper()
	b, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ciljDatoteka je put koji je obična datoteka, pa se ispod njega ne može
// napraviti mapa.
func ciljDatoteka(t *testing.T) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "datoteka")
	if err := os.WriteFile(put, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return put
}

// mapaNaMjestu stavlja mapu na mjesto gdje posao želi zapisati datoteku.
func mapaNaMjestu(t *testing.T, cilj string, dijelovi ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(append([]string{cilj}, dijelovi...)...), 0o755); err != nil {
		t.Fatal(err)
	}
}

func krivuljeUzorak() *Sadrzaj {
	return &Sadrzaj{
		Ime: "izvoz/krivulje dalj.csv",
		Krivulje: []Krivulja{
			{Od: dan(2001, 1, 1), Do: dan(2009, 12, 31), Odsjecci: []Odsjecak{
				{OdCm: 100, DoCm: 500, Oblik: models.OblikPolinom, P1: "43,242", P2: "349,32", P3: "700"},
				{OdCm: 500, DoCm: 1000, Oblik: models.OblikPolinom, P1: "39,804", P2: "334,47", P3: "860,22"},
			}},
			{Od: dan(2010, 1, 1), Do: dan(2012, 12, 31), Odsjecci: []Odsjecak{
				{OdCm: 0, DoCm: 120, Oblik: models.OblikPotencija, P1: "71,088", P2: "2,463", P3: "0,4", P4: "131,3"},
			}},
		},
		Postaja: Postaja{KotaNule: "75,20", Sirina: "45 29 28", Duzina: "18 59 42"},
	}
}

// Zadnjoj krivulji kraj ostaje otvoren, jer vrijedi dok DHMZ ne objavi novu.
func TestKrivuljeCSVZadnjaOstajeOtvorena(t *testing.T) {
	s := krivuljeUzorak()
	ocekivano := ZaglavljeKrivulja + "\n" +
		"2001-01-01;2009-12-31;100;500;polinom;43,242;349,32;700;;DHMZ, HIS-2000;\n" +
		"2001-01-01;2009-12-31;500;1000;polinom;39,804;334,47;860,22;;DHMZ, HIS-2000;\n" +
		"2010-01-01;;0;120;potencija;71,088;2,463;0,4;131,3;DHMZ, HIS-2000;\n"
	if got := string(KrivuljeCSV(s)); got != ocekivano {
		t.Errorf("datoteka krivulja:\n%s", got)
	}
	if n := BrojOdsjecaka(s); n != 3 {
		t.Errorf("odsječaka %d, očekivana tri", n)
	}
	if n := BrojOdsjecaka(&Sadrzaj{}); n != 0 {
		t.Errorf("bez krivulja %d odsječaka", n)
	}
}

// Uz točke snimke ide datum, vodostaj pri mjerenju i kota nule sa sustavom
// visina kad je poznat.
func TestProfilCSV(t *testing.T) {
	p := &Profil{Datum: dan(2010, 3, 22), Vodostaj: 174, KotaNule: "80,45",
		Tocke: []Tocka{{"0,0", "90,113"}, {"6,74", "87,593"}}}
	ocekivano := "# poprečni profil korita, mjereno 2010-03-22, vodostaj pri mjerenju 174 cm, kota nule 80,45\n" +
		"stacionaza_m;visina_m\n0,0;90,113\n6,74;87,593\n"
	if got := string(ProfilCSV(p)); got != ocekivano {
		t.Errorf("bez sustava:\n%s", got)
	}
	p.Sustav = "HVRS71"
	if got := string(ProfilCSV(p)); !strings.HasPrefix(got, "# poprečni profil korita, mjereno 2010-03-22, vodostaj pri mjerenju 174 cm, kota nule 80,45 HVRS71\n") {
		t.Errorf("sa sustavom:\n%s", got)
	}
	if got := ImeProfila("batina", p); got != "batina_profil_2010-03-22.csv" {
		t.Errorf("ime snimke %q", got)
	}
}

// Krivulje se zapisuju u hq/, a izvornik izvoza čuva se uz njih pod svojim
// imenom; proba samo broji. Svaki neuspio zapis vraća grešku.
func TestPosaoKrivulje(t *testing.T) {
	cilj := t.TempDir()
	var dnevnik strings.Builder
	p := &Posao{Cilj: cilj, Letva: "dalj", Probno: true, Dnevnik: &dnevnik}
	izvornik := []byte("Krivulje protoka\r\n")
	if err := p.Krivulje(krivuljeUzorak(), izvornik); err != nil {
		t.Fatal(err)
	}
	if p.Zapisano != 1 {
		t.Errorf("proba je izbrojila %d", p.Zapisano)
	}
	if stavke, _ := os.ReadDir(cilj); len(stavke) != 0 {
		t.Errorf("proba je zapisala %d stavki", len(stavke))
	}
	for _, dio := range []string{"krivulje", "protok", "3  2001 .. 2012", "hq/dalj_hq_krivulje.csv",
		"uz krivulje stoji kota nule 75,20 m i koordinate 45 29 28 / 18 59 42"} {
		if !strings.Contains(dnevnik.String(), dio) {
			t.Errorf("u dnevniku nema %q:\n%s", dio, dnevnik.String())
		}
	}

	p = &Posao{Cilj: cilj, Letva: "dalj"}
	bezKote := krivuljeUzorak()
	bezKote.Postaja = Postaja{}
	if err := p.Krivulje(bezKote, izvornik); err != nil {
		t.Fatal(err)
	}
	if got := procitajDatoteku(t, filepath.Join(cilj, "hq", "dalj_hq_krivulje.csv")); got != string(KrivuljeCSV(bezKote)) {
		t.Errorf("zapisane krivulje:\n%s", got)
	}
	if got := procitajDatoteku(t, filepath.Join(cilj, "hq", "izvornik", "krivulje dalj.csv")); got != string(izvornik) {
		t.Errorf("izvornik %q", got)
	}
	if p.Zapisano != 1 {
		t.Errorf("zapisano %d", p.Zapisano)
	}

	for ime, pripremi := range map[string]func(t *testing.T) string{
		"cilj je datoteka": ciljDatoteka,
		"mapa na mjestu krivulja": func(t *testing.T) string {
			c := t.TempDir()
			mapaNaMjestu(t, c, "hq", "dalj_hq_krivulje.csv")
			return c
		},
		"mapa na mjestu izvornika": func(t *testing.T) string {
			c := t.TempDir()
			mapaNaMjestu(t, c, "hq", "izvornik", "krivulje dalj.csv")
			return c
		},
	} {
		p := &Posao{Cilj: pripremi(t), Letva: "dalj"}
		if err := p.Krivulje(krivuljeUzorak(), izvornik); err == nil {
			t.Errorf("%s: zapis je prošao", ime)
		}
		if p.Zapisano != 0 {
			t.Errorf("%s: neuspio zapis je izbrojen", ime)
		}
	}
}

// Snimka korita ide u profil/ pod datumom mjerenja.
func TestPosaoProfil(t *testing.T) {
	s := &Sadrzaj{Profil: &Profil{Datum: dan(2010, 3, 22), Vodostaj: 174, KotaNule: "80,45",
		Tocke: []Tocka{{"0,0", "90,113"}, {"6,74", "87,593"}, {"14,07", "85,763"}}}}
	cilj := t.TempDir()
	var dnevnik strings.Builder
	p := &Posao{Cilj: cilj, Letva: "batina", Probno: true, Dnevnik: &dnevnik}
	if err := p.Profil(s); err != nil {
		t.Fatal(err)
	}
	if stavke, _ := os.ReadDir(cilj); len(stavke) != 0 || p.Zapisano != 1 {
		t.Errorf("proba: %d stavki, izbrojeno %d", len(stavke), p.Zapisano)
	}
	if !strings.Contains(dnevnik.String(), "3  2010-03-22, vodostaj 174 cm") ||
		!strings.Contains(dnevnik.String(), "profil/batina_profil_2010-03-22.csv") {
		t.Errorf("dnevnik:\n%s", dnevnik.String())
	}

	p = &Posao{Cilj: cilj, Letva: "batina"}
	if err := p.Profil(s); err != nil {
		t.Fatal(err)
	}
	if got := procitajDatoteku(t, filepath.Join(cilj, "profil", "batina_profil_2010-03-22.csv")); got != string(ProfilCSV(s.Profil)) {
		t.Errorf("zapisana snimka:\n%s", got)
	}

	p = &Posao{Cilj: ciljDatoteka(t), Letva: "batina"}
	if err := p.Profil(s); err == nil {
		t.Error("mapa profil/ pod datotekom je prošla")
	}
	c := t.TempDir()
	mapaNaMjestu(t, c, "profil", "batina_profil_2010-03-22.csv")
	p = &Posao{Cilj: c, Letva: "batina"}
	if err := p.Profil(s); err == nil || p.Zapisano != 0 {
		t.Errorf("snimka preko mape: %v, izbrojeno %d", err, p.Zapisano)
	}
}

// Vodomjerenja se slažu po datumu i zapisuju uz krivulje, s rasponom godina
// u imenu.
func TestPosaoMjerenja(t *testing.T) {
	mjerenja := func() []Mjerenje {
		return []Mjerenje{
			{Datum: dan(2021, 6, 2), Vodostaj: "130", Brzina: "0,91", Protok: "301,5", Metoda: "krilo"},
			{Datum: dan(2019, 4, 10), Vodostaj: "120", Brzina: "0,84", Protok: "250,0", Metoda: "ADCP"},
		}
	}
	cilj := t.TempDir()
	var dnevnik strings.Builder
	p := &Posao{Cilj: cilj, Letva: "dalj", Probno: true, Dnevnik: &dnevnik}
	if err := p.Mjerenja(mjerenja()); err != nil {
		t.Fatal(err)
	}
	if stavke, _ := os.ReadDir(cilj); len(stavke) != 0 || p.Zapisano != 1 {
		t.Errorf("proba: %d stavki, izbrojeno %d", len(stavke), p.Zapisano)
	}
	if !strings.Contains(dnevnik.String(), "2  2019-04-10 .. 2021-06-02") {
		t.Errorf("dnevnik:\n%s", dnevnik.String())
	}

	p = &Posao{Cilj: cilj, Letva: "dalj"}
	if err := p.Mjerenja(mjerenja()); err != nil {
		t.Fatal(err)
	}
	ocekivano := "datum;vodostaj_cm;srednja_brzina_ms;protok_m3s;metoda\n" +
		"2019-04-10;120;0,84;250,0;ADCP\n2021-06-02;130;0,91;301,5;krilo\n"
	if got := procitajDatoteku(t, filepath.Join(cilj, "hq", "dalj_vodomjerenja_2019-2021.csv")); got != ocekivano {
		t.Errorf("vodomjerenja:\n%s", got)
	}

	p = &Posao{Cilj: ciljDatoteka(t), Letva: "dalj"}
	if err := p.Mjerenja(mjerenja()); err == nil {
		t.Error("mapa hq/ pod datotekom je prošla")
	}
	c := t.TempDir()
	mapaNaMjestu(t, c, "hq", "dalj_vodomjerenja_2019-2021.csv")
	p = &Posao{Cilj: c, Letva: "dalj"}
	if err := p.Mjerenja(mjerenja()); err == nil || p.Zapisano != 0 {
		t.Errorf("vodomjerenja preko mape: %v, izbrojeno %d", err, p.Zapisano)
	}
}

func TestSazetakPosla(t *testing.T) {
	slucajevi := []struct {
		p    Posao
		opis string
	}{
		{Posao{Zapisano: 3}, "zapisano 3 datoteka"},
		{Posao{Probno: true, Zapisano: 2}, "zapisalo bi se 2 datoteka"},
		{Posao{Zapisano: 1, Ostavljeno: 2}, "zapisano 1 datoteka, ostavljeno zatečeno 2"},
		{Posao{Zapisano: 1, Sporno: 1}, "zapisano 1 datoteka, sporno 1"},
		{Posao{Ostavljeno: 1, Sporno: 4}, "zapisano 0 datoteka, ostavljeno zatečeno 1, sporno 4"},
	}
	for _, s := range slucajevi {
		if got := s.p.Sazetak(); got != s.opis {
			t.Errorf("%+v → %q, očekivano %q", s.p, got, s.opis)
		}
	}
}

// Zatečeni niz se traži po letvi, izvoru, veličini i gustoći, bez obzira
// na godine u nazivu.
func TestZatecenNiz(t *testing.T) {
	cilj := t.TempDir()
	v := Vrsta{Velicina: "vodostaj", Gustoca: "srednjak"}
	for _, ime := range []string{"dalj_his2000_protok_srednjak_2001-2002.csv", "dalj_his2000_vodostaj_srednjak_1962-2001.csv"} {
		if err := os.WriteFile(filepath.Join(cilj, ime), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := zatecen(cilj, "dalj", "his2000", v); err != nil || got != "dalj_his2000_vodostaj_srednjak_1962-2001.csv" {
		t.Errorf("zatečen %q, %v", got, err)
	}
	if got, err := zatecen(cilj, "dalj", "his2000", Vrsta{Velicina: "temperatura", Gustoca: "srednjak"}); err != nil || got != "" {
		t.Errorf("nepostojeći niz: %q, %v", got, err)
	}
	if _, err := zatecen(filepath.Join(cilj, "nema"), "dalj", "his2000", v); err == nil {
		t.Error("nepostojeća mapa mora javiti grešku")
	}
}

func dnevniNiz(godina int, vrijednosti ...string) *Sadrzaj {
	s := &Sadrzaj{Vrsta: Vrsta{Velicina: "vodostaj", Gustoca: "srednjak", Stupac: "vodostaj_cm"}}
	for i, v := range vrijednosti {
		s.Niz = append(s.Niz, Vrijednost{Kad: dan(godina, 1, 1+i), Dan: true, V: v})
	}
	return s
}

// Zatečeni niz se ne gazi: drukčije vrijednosti ostaju dok nije zadano
// zamijeni, a niz koji ima ono čega u novom izvozu nema ostaje kakav jest.
// Kad novi izvoz pokriva više godina, stara datoteka se miče, osim u probi.
func TestPosaoNizUzZateceni(t *testing.T) {
	cilj := t.TempDir()
	staro := filepath.Join(cilj, "dalj_his2000_vodostaj_srednjak_1962-1962.csv")
	var dnevnik strings.Builder
	p := &Posao{Cilj: cilj, Letva: "dalj", Izvor: "his2000", Dnevnik: &dnevnik}
	if err := p.Niz(dnevniNiz(1962, "176", "186")); err != nil {
		t.Fatal(err)
	}
	if got := procitajDatoteku(t, staro); got != "datum;vodostaj_cm\n1962-01-01;176\n1962-01-02;186\n" {
		t.Errorf("prvi zapis:\n%s", got)
	}

	// drukčija vrijednost bez zamijeni ostaje sporna
	if err := p.Niz(dnevniNiz(1962, "176", "187")); err != nil {
		t.Fatal(err)
	}
	if p.Sporno != 1 || !strings.Contains(procitajDatoteku(t, staro), "1962-01-02;186") {
		t.Errorf("sporno %d, datoteka %s", p.Sporno, procitajDatoteku(t, staro))
	}
	// novi izvoz bez drugog dana ne briše ga
	if err := p.Niz(dnevniNiz(1962, "176")); err != nil {
		t.Fatal(err)
	}
	if p.Ostavljeno != 1 {
		t.Errorf("ostavljeno %d", p.Ostavljeno)
	}
	// sa zamijeni drukčija vrijednost prolazi
	p.Zamijeni = true
	if err := p.Niz(dnevniNiz(1962, "176", "187")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(procitajDatoteku(t, staro), "1962-01-02;187") {
		t.Errorf("zamijenjeno: %s", procitajDatoteku(t, staro))
	}

	// proširenje na novu godinu u probi ne briše staru datoteku
	prosireno := dnevniNiz(1962, "176", "187")
	prosireno.Niz = append(prosireno.Niz, Vrijednost{Kad: dan(1963, 1, 1), Dan: true, V: "190"})
	p.Probno = true
	if err := p.Niz(prosireno); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staro); err != nil {
		t.Errorf("proba je maknula zatečeni niz: %v", err)
	}
	p.Probno = false
	if err := p.Niz(prosireno); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staro); !os.IsNotExist(err) {
		t.Errorf("stari raspon nije maknut: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cilj, "dalj_his2000_vodostaj_srednjak_1962-1963.csv")); err != nil {
		t.Errorf("novi raspon: %v", err)
	}
	for _, dio := range []string{"1 vrijednosti se razlikuje", "ima 1 vrijednosti kojih u novom izvozu nema", "zamjenjujem novim rasponom"} {
		if !strings.Contains(dnevnik.String(), dio) {
			t.Errorf("u dnevniku nema %q:\n%s", dio, dnevnik.String())
		}
	}
	if p.Zapisano != 4 {
		t.Errorf("zapisano %d, očekivana četiri zapisa uz probu", p.Zapisano)
	}
}

// Satni niz nosi vrijeme u UTC-u, a sat koji lokalno ne postoji u zatečenom
// nizu nije gubitak.
func TestPosaoSatniNiz(t *testing.T) {
	cilj := t.TempDir()
	zateceno := "vrijeme_utc;vodostaj_cm\n2026-03-29 01:00:00;200\n2026-03-29 02:00:00;201\n2026-03-29 03:00:00;208\nnečitljivo\nx;1\n"
	if err := os.WriteFile(filepath.Join(cilj, "dalj_his2000_vodostaj_satni_2026-2026.csv"), []byte(zateceno), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Sadrzaj{Vrsta: Vrsta{Velicina: "vodostaj", Gustoca: "satni", Stupac: "vodostaj_cm"}, Preskoceno: 1,
		Niz: []Vrijednost{
			{Kad: time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC), V: "200"},
			{Kad: time.Date(2026, 3, 29, 3, 0, 0, 0, time.UTC), V: "208"},
		}}
	var dnevnik strings.Builder
	p := &Posao{Cilj: cilj, Letva: "dalj", Izvor: "his2000", Dnevnik: &dnevnik}
	if err := p.Niz(s); err != nil {
		t.Fatal(err)
	}
	ocekivano := "vrijeme_utc;vodostaj_cm\n2026-03-29 01:00:00;200\n2026-03-29 03:00:00;208\n"
	if got := procitajDatoteku(t, filepath.Join(cilj, "dalj_his2000_vodostaj_satni_2026-2026.csv")); got != ocekivano {
		t.Errorf("satni niz:\n%s", got)
	}
	for _, dio := range []string{"preskočeno 1 sati kojih u lokalnom vremenu nema", "ima 1 sati kojih u lokalnom vremenu nema"} {
		if !strings.Contains(dnevnik.String(), dio) {
			t.Errorf("u dnevniku nema %q:\n%s", dio, dnevnik.String())
		}
	}

	// zatečeni niz koji se ne da pročitati zaustavlja zapis
	c := t.TempDir()
	mapaNaMjestu(t, c, "dalj_his2000_vodostaj_satni_2026-2026.csv")
	p = &Posao{Cilj: c, Letva: "dalj", Izvor: "his2000"}
	if err := p.Niz(s); err == nil {
		t.Error("nečitljiv zatečeni niz je prošao")
	}
	p = &Posao{Cilj: ciljDatoteka(t), Letva: "dalj", Izvor: "his2000"}
	if err := p.Niz(s); err == nil {
		t.Error("niz pod datotekom je prošao")
	}
}

// SYLK tablica vodomjerenja: datum je redni broj dana radne tablice, a
// stupci se traže po naslovu.
const sylkMjerenja = "ID;PWXL;N;E\r\n" +
	"C;Y1;X1;K\"Datum mjerenja\"\r\nC;X2;K\"Vodostaj\"\r\nC;X3;K\"Srednja brzina\"\r\nC;X4;K\"Protok\"\r\nC;X5;K\"Komentar\"\r\n" +
	"C;Y2;X1;K44197\r\nC;X2;K123\r\nC;X3;K0.839999973773956\r\nC;X4;K250.04\r\nC;X5;K\"ADCP \"\r\n" +
	"F;Y3;X1\r\nC;K\"nije datum\"\r\nC;X4;K1\r\n" +
	"C;Y4;X1;K44200\r\nC;X4;K\r\n" +
	"E\r\n"

// Datoteke izvoza razvrstavaju se po zaglavlju, ne po imenu: nizovi,
// krivulje, snimke korita i vodomjerenja. Od dvije snimke istog dana ostaje
// bogatija, od krivulja ona s više razdoblja, a nečitljivo se samo javi.
func TestPosaoUveziRazvrstava(t *testing.T) {
	profil := func(tocaka int) []byte {
		s := "Mjerenje poprečnog profila korita;\r\n" +
			"Šifra;Podsifra;Stare šifre;Naziv;Vodotok;Kota \"0\";Širina;Dužina;Vodostaj;Datum mjerenja;\r\n" +
			"5170;1;;BATINA;DUNAV;80.45;45 50 45;18 51 17;174;3/22/2010;\r\n\r\nStacionaža;Visina\r\n"
		for i := 0; i < tocaka; i++ {
			s += "1,00;85,000;\r\n"
		}
		return cp(s)
	}
	krivulje := func(razdoblja int) []byte {
		s := "Krivulje protoka\r\nŠifra;Podsifra;Stare šifre;Naziv;Vodotok;Kota \"0\";Širina;Dužina;\r\n" +
			"5130;1;;DALJ;DUNAV;75,20;45 29 28;18 59 42;\r\n\r\n"
		for i := 0; i < razdoblja; i++ {
			s += "1/1/200" + string(rune('0'+i)) + " - 12/31/200" + string(rune('0'+i)) + "\r\nTip;H1;H2;A;B;C;D;\r\n" +
				"1;100,0000;500,0000;43,2420;349,3200;700,0000;0,0000;\r\n\r\n"
		}
		return cp(s)
	}
	dat := []Datoteka{
		{"vodostaj.csv", cp("Dnevni podaci postaje DALJ - DUNAV,  VODOSTAJ  (cm)\r\n01.01.2001;176;\r\n02.01.2001;186;\r\n")},
		{"prazno.csv", cp("Dnevni podaci postaje DALJ - DUNAV,  PROTOK  (m3/s)\r\n01.01.2001;;\r\n31.12.2002;;\r\n")},
		{"posve-prazno.csv", cp("Dnevni podaci postaje DALJ - DUNAV,  PROTOK  (m3/s)\r\n")},
		{"nepoznato.TXT", cp("Nešto sasvim drugo\r\n")},
		{"profil-a.csv", profil(3)},
		{"profil-b.csv", profil(21)},
		{"profil-c.csv", profil(22)},
		{"profil-d.csv", profil(3)},
		{"profil-e.csv", profil(5)},
		{"krivulje-1.csv", krivulje(1)},
		{"krivulje-2.csv", krivulje(2)},
		{"krivulje-3.csv", krivulje(1)},
		{"mjerenja.xls", []byte(sylkMjerenja)},
		{"bez-stupaca.xls", []byte("ID;P\r\nC;Y1;X1;K\"Datum\"\r\nE\r\n")},
		{"bez-mjerenja.xls", []byte("ID;P\r\nC;Y1;X1;K\"Datum mjerenja\"\r\nC;X2;K\"Protok\"\r\nE\r\n")},
		{"slika.png", []byte("nije izvoz")},
	}
	cilj := t.TempDir()
	var dnevnik strings.Builder
	p := &Posao{Cilj: cilj, Letva: "dalj", Izvor: "his2000", Dnevnik: &dnevnik}
	preskoceno, err := p.Uvezi(dat)
	if err != nil {
		t.Fatal(err)
	}
	ocekivano := []string{
		"bez-mjerenja.xls — u tablici nema nijednog mjerenja",
		"bez-stupaca.xls — tablica nema stupce „Datum mjerenja” i „Protok”",
		"nepoznato.TXT — zaglavlje ne kaže što je u datoteci: \"Nešto sasvim drugo\"",
		"posve-prazno.csv — izvoz je prošao, ali mjerenja nema nijednog",
		"prazno.csv — izvoz je prošao za 2001.-2002., ali mjerenja nema nijednog",
		"profil-a.csv (3 točke) — ista snimka korita kao profil-b.csv, koja ih ima 21",
		"profil-b.csv (21 točka) — ista snimka korita kao profil-c.csv, koja ih ima 22",
		"profil-d.csv (3 točke) — ista snimka korita kao profil-c.csv, koja ih ima 22",
		"profil-e.csv (5 točaka) — ista snimka korita kao profil-c.csv, koja ih ima 22",
	}
	if strings.Join(preskoceno, "\n") != strings.Join(ocekivano, "\n") {
		t.Errorf("preskočeno:\n%s", strings.Join(preskoceno, "\n"))
	}
	if p.Zapisano != 4 {
		t.Errorf("zapisano %d, očekivani niz, krivulje, snimka i vodomjerenja", p.Zapisano)
	}
	if got := procitajDatoteku(t, filepath.Join(cilj, "hq", "izvornik", "krivulje-2.csv")); got != string(krivulje(2)) {
		t.Errorf("izvornik krivulja nije onaj s dva razdoblja:\n%s", got)
	}
	if got := procitajDatoteku(t, filepath.Join(cilj, "profil", "dalj_profil_2010-03-22.csv")); strings.Count(got, "\n") != 2+22 {
		t.Errorf("snimka nije bogatija:\n%s", got)
	}
	if got := procitajDatoteku(t, filepath.Join(cilj, "hq", "dalj_vodomjerenja_2021-2021.csv")); got !=
		"datum;vodostaj_cm;srednja_brzina_ms;protok_m3s;metoda\n2021-01-01;123;0,84;250,0;ADCP\n" {
		t.Errorf("vodomjerenja:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(cilj, "dalj_his2000_vodostaj_srednjak_2001-2001.csv")); err != nil {
		t.Errorf("niz: %v", err)
	}
	if !strings.Contains(dnevnik.String(), "preskočeno: nepoznato.TXT") || !strings.HasSuffix(dnevnik.String(), "zapisano 4 datoteka\n") {
		t.Errorf("dnevnik:\n%s", dnevnik.String())
	}
}

// Neuspio zapis bilo koje vrste zaustavlja uvoz i vraća grešku.
func TestPosaoUveziGreskaZapisa(t *testing.T) {
	for ime, d := range map[string]Datoteka{
		"niz":          {"vodostaj.csv", cp("Dnevni podaci postaje DALJ - DUNAV,  VODOSTAJ  (cm)\r\n01.01.2001;176;\r\n")},
		"krivulje":     {"krivulje.csv", cp("Krivulje protoka\r\n1/1/2001 - 12/31/2001\r\nTip;H1;H2;A;B;C;D;\r\n1;100,0000;500,0000;43,2420;349,3200;700,0000;0,0000;\r\n")},
		"snimka":       {"profil.csv", cp("Mjerenje poprečnog profila korita;\r\nŠifra;Podsifra;Stare šifre;Naziv;Vodotok;Kota \"0\";Širina;Dužina;Vodostaj;Datum mjerenja;\r\n5170;1;;BATINA;DUNAV;80.45;45 50 45;18 51 17;174;3/22/2010;\r\n\r\nStacionaža;Visina\r\n0,00;90,113;\r\n6,74;87,593;\r\n14,07;85,763;\r\n")},
		"vodomjerenja": {"mjerenja.xls", []byte(sylkMjerenja)},
	} {
		p := &Posao{Cilj: ciljDatoteka(t), Letva: "dalj", Izvor: "his2000"}
		if _, err := p.Uvezi([]Datoteka{d}); err == nil {
			t.Errorf("%s: zapis pod datotekom je prošao", ime)
		}
	}
}

// Datum radne tablice je broj dana od 30.12.1899.; manji od 61 nije
// pouzdan. Broj se ispisuje zarezom, na zadani broj decimala.
func TestDatumIBrojRadneTablice(t *testing.T) {
	if d, err := izSerijskog(" 44197 "); err != nil || !d.Equal(dan(2021, 1, 1)) {
		t.Errorf("44197 → %s, %v", d, err)
	}
	for _, los := range []string{"60", "", "datum", "-3"} {
		if _, err := izSerijskog(los); err == nil {
			t.Errorf("%q je prošlo kao datum", los)
		}
	}
	for _, s := range []struct {
		ulaz     string
		decimala int
		izlaz    string
	}{
		{"0.839999973773956", 2, "0,84"},
		{"250,04", 1, "250,0"},
		{" 123 ", 0, "123"},
		{"", 1, ""},
		{"nije broj", 1, ""},
	} {
		if got := zaokruzi(s.ulaz, s.decimala); got != s.izlaz {
			t.Errorf("zaokruzi(%q, %d) = %q, očekivano %q", s.ulaz, s.decimala, got, s.izlaz)
		}
	}
	if !JeSylk([]byte("ID;PWXL;N;E\r\n")) || JeSylk([]byte("Datum;Protok\r\n")) {
		t.Error("SYLK se prepoznaje po početku ID;P")
	}
}
