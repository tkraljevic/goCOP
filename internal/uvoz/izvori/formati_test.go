package izvori

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// Zaključava uvoz svakog posebnog izvora kroz Format.Uvezi: što se zapisuje
// u stablo, što proba samo ispiše i da se greška čitača ili zapisa vrati
// pozivatelju. Ulazne datoteke su male i izmišljene.

// losSliv ima donju crtu, pa ga arhiva odbije tek pri zapisu, nakon što je
// datoteka već pročitana.
const losSliv = "do_nja"

func uvezi(t *testing.T, kod string, dat []Datoteka, z Zadatak) (Ishod, string, error) {
	t.Helper()
	f, ok := Nadji(kod)
	if !ok {
		t.Fatalf("nema formata %s", kod)
	}
	var dnevnik strings.Builder
	ishod, err := f.Uvezi(dat, z, &dnevnik)
	return ishod, dnevnik.String(), err
}

func zipSa(t *testing.T, datoteke map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for ime, sadrzaj := range datoteke {
		w, err := zw.Create(ime)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(sadrzaj)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const arsoGlava = "Datum;vodostaj (cm);pretok (m3/s);temp. vode (°C)\n"

// ARSO iz jednog izvoza daje do tri dnevna srednjaka; prazan niz se ne
// zapisuje.
func TestUvozARSO(t *testing.T) {
	koren := t.TempDir()
	dat := []Datoteka{{Ime: "ptuj.csv", Sadrzaj: []byte(arsoGlava + "01.01.2024;123;250;8,2\n02.01.2024;124;251;\n")}}
	z := Zadatak{Koren: koren, Sliv: "drava", Letva: "ptuj", Probno: true}
	ishod, dnevnik, err := uvezi(t, "arso", dat, z)
	if err != nil {
		t.Fatal(err)
	}
	if len(ishod.Zapisano) != 0 || len(datoteke(t, koren)) != 0 {
		t.Fatal("proba je zapisala")
	}
	for _, dio := range []string{"redaka: 2;", "vodostaja: 2;", "protoka: 2;", "temperatura: 1; praznih: 1", "probni prolaz"} {
		if !strings.Contains(dnevnik, dio) {
			t.Errorf("u dnevniku nema %q:\n%s", dio, dnevnik)
		}
	}

	z.Probno = false
	ishod, _, err = uvezi(t, "arso", dat, z)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(datoteke(t, koren), " ")
	for _, v := range []string{"ptuj_arso_vodostaj_srednjak_2024", "ptuj_arso_protok_srednjak_2024", "ptuj_arso_temperatura_srednjak_2024"} {
		if !strings.Contains(got, v) {
			t.Errorf("nema %s u %s", v, got)
		}
	}
	if len(ishod.Zapisano) != 3 || strings.Join(ishod.Letve, ",") != "ptuj" {
		t.Errorf("ishod: %+v", ishod)
	}

	// samo protok: ostale veličine nemaju što zapisati
	samoProtok := []Datoteka{{Ime: "q.csv", Sadrzaj: []byte("Datum;pretok (m3/s)\n01.01.2024;250\n")}}
	ishod, _, err = uvezi(t, "arso", samoProtok, Zadatak{Koren: t.TempDir(), Sliv: "drava", Letva: "ptuj"})
	if err != nil || len(ishod.Zapisano) != 1 || !strings.Contains(ishod.Zapisano[0], "ptuj_arso_protok_srednjak_") {
		t.Errorf("samo protok: %+v, %v", ishod, err)
	}

	if _, _, err := uvezi(t, "arso", []Datoteka{{Ime: "x.csv", Sadrzaj: []byte("Nešto;drugo\n")}},
		Zadatak{Koren: koren, Sliv: "drava", Letva: "ptuj"}); err == nil {
		t.Error("datoteka bez stupaca ARSO-a je prošla")
	}
	if _, _, err := uvezi(t, "arso", dat, Zadatak{Koren: koren, Sliv: losSliv, Letva: "ptuj"}); err == nil {
		t.Error("neuspio zapis je prošao")
	}
}

func ehydDatoteka(interval string) []byte {
	return []byte("Messstelle:                ;Lavamuend Ort\nHZB-Nummer:                ;213173\n" +
		"Exportzeitreihe:           ;(2001073,Abfluss,I,Mit," + interval + ",1,F,Z,50,,,)\n" +
		"Einheit:                   ;[m3/s]\nWerte:\n" +
		"01.01.2024 00:00:00; 123,4\n02.01.2024 00:00:00; Luecke\n03.01.2024 00:00:00; 124\n")
}

// eHYD prima samo dnevni niz i zapisuje ga kao srednjak zadane veličine;
// mjesečni niz se odbija prije zapisa.
func TestUvozEHYD(t *testing.T) {
	koren := t.TempDir()
	dat := []Datoteka{{Ime: "lavamuend.csv", Sadrzaj: ehydDatoteka("T")}}
	z := Zadatak{Koren: koren, Sliv: "drava", Letva: "lavamund", Velicina: "protok", Probno: true}
	ishod, dnevnik, err := uvezi(t, "ehyd", dat, z)
	if err != nil {
		t.Fatal(err)
	}
	if len(ishod.Zapisano) != 0 || len(datoteke(t, koren)) != 0 || !strings.Contains(dnevnik, "postaja: Lavamuend Ort (HZB 213173); interval: T") {
		t.Errorf("proba: %+v\n%s", ishod, dnevnik)
	}

	z.Probno = false
	ishod, _, err = uvezi(t, "ehyd", dat, z)
	if err != nil {
		t.Fatal(err)
	}
	if len(ishod.Zapisano) != 1 || !strings.HasPrefix(ishod.Zapisano[0], filepath.Join(koren, "drava", "lavamund", "lavamund_ehyd_protok_srednjak_2024")) {
		t.Errorf("zapisano %v", ishod.Zapisano)
	}

	_, _, err = uvezi(t, "ehyd", []Datoteka{{Ime: "m.csv", Sadrzaj: ehydDatoteka("M")}}, z)
	if err == nil || !strings.Contains(err.Error(), "niz nije dnevni") {
		t.Errorf("mjesečni niz: %v", err)
	}
	if _, _, err := uvezi(t, "ehyd", []Datoteka{{Ime: "x.csv", Sadrzaj: []byte("bez odjeljka\n")}}, z); err == nil {
		t.Error("datoteka bez vrijednosti je prošla")
	}
	z.Sliv = losSliv
	if _, _, err := uvezi(t, "ehyd", dat, z); err == nil {
		t.Error("neuspio zapis je prošao")
	}
}

// GKD daje ZIP s provjerenim dnevnim protocima; zapisuje se samo protok.
func TestUvozGKD(t *testing.T) {
	sadrzaj := zipSa(t, map[string]string{"fluesse-abfluss/niz.csv": "Messstellen-Name:;Hofkirchen\nMessstellen-Nr.:;10088003\n\n" +
		"Datum;Mittelwert;Maximum;Minimum;Prüfstatus\n2024-01-01;328;;;Geprueft\n2024-01-02;329,5;;;Vorlaeufig\n"})
	koren := t.TempDir()
	dat := []Datoteka{{Ime: "hofkirchen.zip", Sadrzaj: sadrzaj}}
	z := Zadatak{Koren: koren, Letva: "hofkirchen", Probno: true}
	ishod, dnevnik, err := uvezi(t, "gkd", dat, z)
	if err != nil {
		t.Fatal(err)
	}
	if len(ishod.Zapisano) != 0 || len(datoteke(t, koren)) != 0 ||
		!strings.Contains(dnevnik, "postaja: Hofkirchen (10088003)") || !strings.Contains(dnevnik, "neprovjerenih: 1") {
		t.Errorf("proba: %+v\n%s", ishod, dnevnik)
	}

	z.Probno = false
	ishod, _, err = uvezi(t, "gkd", dat, z)
	if err != nil {
		t.Fatal(err)
	}
	// sliv je zadani dunav
	if len(ishod.Zapisano) != 1 || !strings.HasPrefix(ishod.Zapisano[0], filepath.Join(koren, "dunav", "hofkirchen", "hofkirchen_gkd_protok_srednjak_2024")) {
		t.Errorf("zapisano %v", ishod.Zapisano)
	}

	if _, _, err := uvezi(t, "gkd", []Datoteka{{Ime: "x.zip", Sadrzaj: []byte("nije zip")}}, z); err == nil {
		t.Error("pokvaren ZIP je prošao")
	}
	z.Sliv = losSliv
	if _, _, err := uvezi(t, "gkd", dat, z); err == nil {
		t.Error("neuspio zapis je prošao")
	}
}

// PEGELONLINE: nečitljiv JSON i neuspio zapis vraćaju grešku, proba ne
// zapisuje.
func TestUvozPegelonlineGreskeIProba(t *testing.T) {
	koren := t.TempDir()
	json := `[{"timestamp":"2026-08-21T02:00:00+02:00","value":253}]`
	dat := []Datoteka{{Ime: "k.json", Sadrzaj: []byte(json)}}
	ishod, dnevnik, err := uvezi(t, "pegelonline", dat, Zadatak{Koren: koren, Letva: "kienstock", Probno: true})
	if err != nil || len(ishod.Zapisano) != 0 || len(datoteke(t, koren)) != 0 || !strings.Contains(dnevnik, "punih sati: 1") {
		t.Errorf("proba: %+v, %v\n%s", ishod, err, dnevnik)
	}
	if _, _, err := uvezi(t, "pegelonline", []Datoteka{{Ime: "k.json", Sadrzaj: []byte("{")}},
		Zadatak{Koren: koren, Letva: "kienstock"}); err == nil {
		t.Error("nečitljiv JSON je prošao")
	}
	if _, _, err := uvezi(t, "pegelonline", dat, Zadatak{Koren: koren, Sliv: losSliv, Letva: "kienstock"}); err == nil {
		t.Error("neuspio zapis je prošao")
	}
}

// SEBA mora zapisati oba niza; kad jedan nema vrijednosti, zapis staje.
func TestUvozSEBAGreske(t *testing.T) {
	koren := t.TempDir()
	z := Zadatak{Koren: koren, Sliv: "dunav", Letva: "tikves"}
	for ime, redak := range map[string]string{
		"bez vodostaja":   "2025-01-01 12:00,5,,8.2\n",
		"bez temperature": "2025-01-01 12:00,5,1.234,\n",
	} {
		if _, _, err := uvezi(t, "seba", []Datoteka{{Ime: "a.csv", Sadrzaj: []byte(sebaGlava + redak)}}, z); err == nil {
			t.Errorf("%s: zapis je prošao", ime)
		}
	}
	if _, _, err := uvezi(t, "seba", []Datoteka{{Ime: "a.csv", Sadrzaj: []byte("Time\n")}}, z); err == nil {
		t.Error("izvoz bez stupaca je prošao")
	}
}

// HIS-2000 prima datoteke izvoza i ZIP s njima; iz ZIP-a se uzimaju samo
// datoteke, bez mapa i skrivenih datoteka. Ime izvornika krivulja ostaje
// kakvo je stiglo.
func TestUvozHIS(t *testing.T) {
	dnevni := "Dnevni podaci postaje DALJ - DUNAV,  VODOSTAJ  (cm)\r\n01.01.2001;176;\r\n02.01.2001;186;\r\n"
	krivulje := "Krivulje protoka\r\n1/1/2001 - 12/31/2001\r\nTip;H1;H2;A;B;C;D;\r\n1;100,0000;500,0000;43,2420;349,3200;700,0000;0,0000;\r\n"
	arhiva := zipSa(t, map[string]string{
		"izvoz/krivulje dalj.csv": krivulje,
		"izvoz/.DS_Store":         "smeće",
		"izvoz/prazna-mapa/":      "",
	})
	dat := []Datoteka{{Ime: `C:\izvoz\vodostaj.csv`, Sadrzaj: []byte(dnevni)}, {Ime: "dalj.zip", Sadrzaj: arhiva}}
	koren := t.TempDir()
	z := Zadatak{Koren: koren, Sliv: "dunav", Letva: "dalj", Probno: true}
	ishod, dnevnik, err := uvezi(t, "his2000", dat, z)
	if err != nil {
		t.Fatal(err)
	}
	if len(datoteke(t, koren)) != 0 || !strings.Contains(dnevnik, "zapisalo bi se 2 datoteka") {
		t.Errorf("proba je zapisala ili krivo izbrojila:\n%s", dnevnik)
	}
	if strings.Join(ishod.Letve, ",") != "dalj" {
		t.Errorf("letve %v", ishod.Letve)
	}

	z.Probno = false
	if _, _, err := uvezi(t, "his2000", dat, z); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(datoteke(t, koren), " ")
	for _, v := range []string{
		filepath.Join("dunav", "dalj", "dalj_his2000_vodostaj_srednjak_2001-2001.csv"),
		filepath.Join("dunav", "dalj", "hq", "dalj_hq_krivulje.csv"),
		filepath.Join("dunav", "dalj", "hq", "izvornik", "krivulje dalj.csv"),
	} {
		if !strings.Contains(got, v) {
			t.Errorf("nema %s u %s", v, got)
		}
	}
	if strings.Contains(got, "DS_Store") {
		t.Errorf("skrivena datoteka iz ZIP-a je ušla: %s", got)
	}

	if _, _, err := uvezi(t, "his2000", []Datoteka{{Ime: "dalj.zip", Sadrzaj: []byte("nije zip")}}, z); err == nil ||
		!strings.Contains(err.Error(), "dalj.zip") {
		t.Errorf("pokvaren ZIP: %v", err)
	}
	// stablo pod datotekom: posao ne može zapisati niz
	z.Koren = filepath.Join(koren, "dunav", "dalj", "dalj_his2000_vodostaj_srednjak_2001-2001.csv")
	if _, _, err := uvezi(t, "his2000", dat[:1], z); err == nil {
		t.Error("zapis pod datotekom je prošao")
	}
}

// Godišnjak traži postaje u obliku šifra=letva; bez pdftotexta se ne čita.
func TestUvozGodisnjakaGreske(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dat := []Datoteka{{Ime: "1991.pdf", Sadrzaj: []byte("%PDF")}}
	if _, _, err := uvezi(t, "godisnjak", dat, Zadatak{Koren: t.TempDir(), Postaje: "42010"}); err == nil ||
		!strings.Contains(err.Error(), "šifra=letva") {
		t.Errorf("postaje bez letve: %v", err)
	}
	if _, _, err := uvezi(t, "godisnjak", dat, Zadatak{Koren: t.TempDir(), Postaje: "42010=bezdan"}); err == nil ||
		!strings.Contains(err.Error(), "pdftotext") {
		t.Errorf("bez pdftotexta: %v", err)
	}
}

// Ime datoteke iz preglednika ne određuje putanju: uzima se samo osnova,
// i kad stigne s kosim crtama Windowsa. Zadatak bez stabla se odbija.
func TestUvezenoImeINepoznatFormat(t *testing.T) {
	koren := t.TempDir()
	f, _ := Nadji("arso")
	_, err := f.Uvezi([]Datoteka{{Ime: `..\..\ptuj.csv`, Sadrzaj: []byte(arsoGlava + "01.01.2024;123;;\n")}},
		Zadatak{Koren: koren, Sliv: "drava", Letva: "ptuj"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d := datoteke(t, koren); len(d) != 1 || !strings.HasPrefix(d[0], filepath.Join("drava", "ptuj", "ptuj_arso_vodostaj_")) {
		t.Errorf("zapisano %v", d)
	}
	if _, err := f.Uvezi([]Datoteka{{Ime: "a.csv"}}, Zadatak{Sliv: "drava", Letva: "ptuj"}, nil); err == nil ||
		!strings.Contains(err.Error(), "nije zadano gdje stablo") {
		t.Errorf("bez stabla: %v", err)
	}
	if _, ok := Nadji("nepoznat"); ok {
		t.Error("nepoznat format je nađen")
	}
	letve, err := f.LetveZadatka(Zadatak{Letva: "ptuj"})
	if err != nil || strings.Join(letve, ",") != "ptuj" {
		t.Errorf("letve zadatka: %v, %v", letve, err)
	}
	g, _ := Nadji("godisnjak")
	if _, err := g.LetveZadatka(Zadatak{Postaje: "42010"}); err == nil {
		t.Error("postaje bez letve su prošle")
	}
}
