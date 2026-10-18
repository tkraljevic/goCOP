package godisnjak

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Zaključava uvoz godišnjaka (Uvezi) i vađenje teksta iz PDF-a (TekstPDF) bez
// pravog PDF-a i bez popplera: pdftotext u testu je skripta koja provjeri da
// je zadan -layout i ispiše „PDF” kakav jest, jer je on ovdje već tekst.

// lazniPdftotext stavlja lažni pdftotext na početak PATH-a. Datoteka čije
// ime sadrži „pokvaren” ne da se pročitati.
func lazniPdftotext(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("lažni pdftotext je skripta ljuske")
	}
	mapa := t.TempDir()
	skripta := "#!/bin/sh\n" +
		"[ \"$1\" = \"-layout\" ] || { echo \"bez -layout\" >&2; exit 2; }\n" +
		"case \"$2\" in *pokvaren*) echo \"nije PDF\" >&2; exit 1;; esac\n" +
		"exec /bin/cat \"$2\"\n"
	if err := os.WriteFile(filepath.Join(mapa, "pdftotext"), []byte(skripta), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", mapa)
}

// godisnjakNaDisku zapisuje tekst godišnjaka pod imenom PDF-a.
func godisnjakNaDisku(t *testing.T, ime, tekst string) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), ime)
	if err := os.WriteFile(put, []byte(tekst), 0o644); err != nil {
		t.Fatal(err)
	}
	return put
}

// Bez pdftotexta godišnjak se ne može pročitati i to se kaže; neuspio
// pdftotext javlja grešku, a uspio daje tekst kakav ispiše.
func TestTekstPDF(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("lažni pdftotext je skripta ljuske")
	}
	t.Setenv("PATH", t.TempDir())
	if ImaPDFAlat() {
		t.Fatal("u praznom PATH-u nema pdftotexta")
	}
	if _, err := TekstPDF("bilo.pdf"); err == nil || !strings.Contains(err.Error(), "nema programa pdftotext") {
		t.Errorf("bez alata: %v", err)
	}

	lazniPdftotext(t)
	if !ImaPDFAlat() {
		t.Fatal("lažni pdftotext nije nađen")
	}
	put := godisnjakNaDisku(t, "1991.pdf", "В О Д О С Т А Ј И\n")
	tekst, err := TekstPDF(put)
	if err != nil || tekst != "В О Д О С Т А Ј И\n" {
		t.Errorf("tekst %q, greška %v", tekst, err)
	}
	if _, err := TekstPDF(godisnjakNaDisku(t, "pokvaren.pdf", "x")); err == nil || !strings.HasPrefix(err.Error(), "pdftotext:") {
		t.Errorf("neuspio pdftotext: %v", err)
	}
}

func dvijePostaje(godina int) string {
	lijeva := punaGodina(godina)
	desna := ulaznaTablica{
		dani:    map[Dan]int{{1, 1}: 300, {1, 2}: 310},
		sazetak: map[int]Mjesec{1: {Min: 305, ImaMin: true}}, // 300 probija ispisani najmanji
	}
	return stranica(godina, [2]string{"42010", "42015"}, [2]string{"БЕЗДАН", "АПАТИН"}, lijeva, desna)
}

// Popis samo ispiše postaje iz godišnjaka i ne zapisuje ništa.
func TestUveziPopisPostaja(t *testing.T) {
	lazniPdftotext(t)
	koren := t.TempDir()
	var dnevnik strings.Builder
	ishod, err := Uvezi(Zadatak{
		Godisnjaci: []string{godisnjakNaDisku(t, "2020.pdf", dvijePostaje(2020))},
		Koren:      koren, Sliv: "dunav", Popis: true, Dnevnik: &dnevnik,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ishod.Letve) != 0 || len(ishod.Zapisano) != 0 {
		t.Errorf("popis je vratio %+v", ishod)
	}
	for _, dio := range []string{"42010", "БЕЗДАН", "2020.", "dana 366", "42015", "АПАТИН"} {
		if !strings.Contains(dnevnik.String(), dio) {
			t.Errorf("u popisu nema %q:\n%s", dio, dnevnik.String())
		}
	}
	if stavke, _ := os.ReadDir(koren); len(stavke) != 0 {
		t.Errorf("popis je zapisao %d stavki", len(stavke))
	}
}

// Uvoz traženih postaja zapiše po letvi jedan dnevni niz kroz sve
// godišnjake, poredan po danu, pod imenom s rasponom godina. Neslaganje sa
// sažetkom i godišnjak bez tražene postaje samo se upozore.
func TestUveziZapisujeNizPoLetvi(t *testing.T) {
	lazniPdftotext(t)
	koren := t.TempDir()
	g2020 := godisnjakNaDisku(t, "2020.pdf", dvijePostaje(2020))
	g2019 := godisnjakNaDisku(t, "2019.pdf", dvijePostaje(2019))
	tudji := godisnjakNaDisku(t, "tudji.pdf", stranica(2018, [2]string{"45001", "45002"},
		[2]string{"НОВИ САД", "СЛАНКАМЕН"}, punaGodina(2018), punaGodina(2018)))
	var dnevnik strings.Builder
	ishod, err := Uvezi(Zadatak{
		Godisnjaci: []string{g2020, g2019, tudji},
		Postaje:    map[string]string{"42015": "apatin", "42010": "bezdan"},
		Koren:      koren, Sliv: "dunav", Dnevnik: &dnevnik,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ishod.Letve, ",") != "apatin,bezdan" {
		t.Errorf("letve %v, očekivane po abecedi", ishod.Letve)
	}
	bezdan := filepath.Join(koren, "dunav", "bezdan", "bezdan_hidmet_vodostaj_srednjak_2019-2020.csv")
	apatin := filepath.Join(koren, "dunav", "apatin", "apatin_hidmet_vodostaj_srednjak_2019-2020.csv")
	if strings.Join(ishod.Zapisano, ",") != apatin+","+bezdan {
		t.Errorf("zapisano %v", ishod.Zapisano)
	}
	b, err := os.ReadFile(apatin)
	if err != nil {
		t.Fatal(err)
	}
	ocekivano := "datum;vodostaj_cm\n2019-01-01;300\n2019-01-02;310\n2020-01-01;300\n2020-01-02;310\n"
	if string(b) != ocekivano {
		t.Errorf("niz apatin:\n%s", b)
	}
	b, err = os.ReadFile(bezdan)
	if err != nil {
		t.Fatal(err)
	}
	redci := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(redci) != 1+365+366 || redci[1] != "2019-01-01;111" || redci[len(redci)-1] != "2020-12-31;223" {
		t.Errorf("niz bezdan: %d redaka, prvi %q, zadnji %q", len(redci), redci[1], redci[len(redci)-1])
	}
	for _, dio := range []string{
		"apatin, 2020.pdf 2020.: 1. mjesec: dnevna vrijednost 300 ispod ispisanog najmanjeg 305",
		"1 mjeseci se ne slaže sa sažetkom",
		"tudji.pdf: nijedna tražena postaja nije nađena",
		"zapisano 2 nizova",
	} {
		if !strings.Contains(dnevnik.String(), dio) {
			t.Errorf("u dnevniku nema %q:\n%s", dio, dnevnik.String())
		}
	}
}

// Probni uvoz kaže koje bi letve dobile niz, a ne zapisuje ništa.
func TestUveziProbno(t *testing.T) {
	lazniPdftotext(t)
	koren := t.TempDir()
	var dnevnik strings.Builder
	ishod, err := Uvezi(Zadatak{
		Godisnjaci: []string{godisnjakNaDisku(t, "2020.pdf", dvijePostaje(2020))},
		Postaje:    map[string]string{"42010": "bezdan"},
		Koren:      koren, Sliv: "dunav", Probno: true, Dnevnik: &dnevnik,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ishod.Letve, ",") != "bezdan" || len(ishod.Zapisano) != 0 {
		t.Errorf("ishod probe: %+v", ishod)
	}
	if !strings.Contains(dnevnik.String(), "proba — ništa nije zapisano") {
		t.Errorf("dnevnik:\n%s", dnevnik.String())
	}
	if stavke, _ := os.ReadDir(koren); len(stavke) != 0 {
		t.Errorf("proba je zapisala %d stavki", len(stavke))
	}
	// bez dnevnika se ništa ne ispisuje, a uvoz i dalje radi
	if _, err := Uvezi(Zadatak{Godisnjaci: []string{godisnjakNaDisku(t, "2020.pdf", dvijePostaje(2020))},
		Postaje: map[string]string{"42010": "bezdan"}, Koren: koren, Sliv: "dunav", Probno: true}); err != nil {
		t.Error(err)
	}
}

// Greške uvoza: bez godišnjaka, nečitljiv godišnjak i stablo u koje se ne
// da pisati.
func TestUveziGreske(t *testing.T) {
	lazniPdftotext(t)
	if _, err := Uvezi(Zadatak{}); err == nil || !strings.Contains(err.Error(), "nema nijednog godišnjaka") {
		t.Errorf("bez godišnjaka: %v", err)
	}
	pokvaren := godisnjakNaDisku(t, "pokvaren.pdf", "x")
	if _, err := Uvezi(Zadatak{Godisnjaci: []string{pokvaren}}); err == nil || !strings.HasPrefix(err.Error(), "pokvaren.pdf: pdftotext:") {
		t.Errorf("nečitljiv godišnjak: %v", err)
	}

	godisnjak := godisnjakNaDisku(t, "2020.pdf", dvijePostaje(2020))
	postaje := map[string]string{"42010": "bezdan"}
	// korijen je datoteka, pa se mapa letve ne može napraviti
	koren := filepath.Join(t.TempDir(), "datoteka")
	if err := os.WriteFile(koren, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Uvezi(Zadatak{Godisnjaci: []string{godisnjak}, Postaje: postaje, Koren: koren, Sliv: "dunav"}); err == nil {
		t.Error("mapa letve pod datotekom je prošla")
	}
	// na mjestu niza stoji mapa, pa se niz ne može zapisati
	koren = t.TempDir()
	if err := os.MkdirAll(filepath.Join(koren, "dunav", "bezdan", "bezdan_hidmet_vodostaj_srednjak_2020-2020.csv"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Uvezi(Zadatak{Godisnjaci: []string{godisnjak}, Postaje: postaje, Koren: koren, Sliv: "dunav"}); err == nil {
		t.Error("niz preko mape je prošao")
	}
}

// Niz koji se nije dao zapisati do kraja javlja se i ne ostaje u stablu.
// Prije je uvoz javio datoteku kao zapisanu, a gradnja bi je pročitala bez
// dana koji nedostaju.
func TestNezapisanNizNeOstaje(t *testing.T) {
	put := filepath.Join(t.TempDir(), "bezdan_hidmet_vodostaj_srednjak_2020-2020.csv")
	f, err := os.Create(put)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	dan := Dan2{2020, 1, 1}
	err = upisiNiz(f, []Dan2{dan}, map[Dan2]int{dan: 111})
	if err == nil || !strings.Contains(err.Error(), "niz bezdan_hidmet_vodostaj_srednjak_2020-2020.csv nije zapisan do kraja") {
		t.Errorf("greška %v, očekivano da niz nije zapisan", err)
	}
	if _, err := os.Stat(put); !os.IsNotExist(err) {
		t.Errorf("nepotpun niz ostao je u stablu: %v", err)
	}
}

// Dan prije drugoga ide po godini, pa mjesecu, pa danu.
func TestDanPrije(t *testing.T) {
	slucajevi := []struct {
		a, b  Dan2
		prije bool
	}{
		{Dan2{2019, 12, 31}, Dan2{2020, 1, 1}, true},
		{Dan2{2020, 1, 1}, Dan2{2019, 12, 31}, false},
		{Dan2{2020, 1, 31}, Dan2{2020, 2, 1}, true},
		{Dan2{2020, 3, 1}, Dan2{2020, 2, 28}, false},
		{Dan2{2020, 2, 1}, Dan2{2020, 2, 2}, true},
		{Dan2{2020, 2, 2}, Dan2{2020, 2, 2}, false},
	}
	for _, s := range slucajevi {
		if got := s.a.Prije(s.b); got != s.prije {
			t.Errorf("%v prije %v = %v", s.a, s.b, got)
		}
	}
	if akoGreske(nil) != "" || akoGreske([]string{"a", "b"}) != "  — 2 mjeseci se ne slaže sa sažetkom" {
		t.Errorf("akoGreske: %q", akoGreske([]string{"a", "b"}))
	}
}
