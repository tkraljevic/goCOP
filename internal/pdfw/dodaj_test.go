package pdfw

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

var kadDodatka = time.Date(2026, 3, 10, 7, 30, 0, 0, time.FixedZone("CET", 3600))

// dokumentZaDodatak je dnevni list od dvije stranice, pripremljen za dodatke
func dokumentZaDodatak(t *testing.T) []byte {
	t.Helper()
	d := Novi("Dnevni list", "goCOP")
	d.SviZnakovi()
	d.Odlomak("Dnevni list vodočuvara, nasip Draž", 12, true, Lijevo)
	d.NovaStranica()
	d.Odlomak("Druga stranica", 10, false, Lijevo)
	return d.Bajtovi()
}

// izgled crta okvir, tekst te PNG i JPEG sliku, pa dodatak dobije sve vrste
// objekata koje Dodaj zna upisati
func izgled(t *testing.T) func(d *Doc) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := 0; x < 4; x++ {
		img.Set(x, x, color.RGBA{0, 0, 255, 255})
	}
	var p, j bytes.Buffer
	if err := png.Encode(&p, img); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&j, img, nil); err != nil {
		t.Fatal(err)
	}
	siva := image.NewGray(image.Rect(0, 0, 4, 4))
	var g bytes.Buffer
	if err := jpeg.Encode(&g, siva, nil); err != nil {
		t.Fatal(err)
	}
	return func(d *Doc) {
		d.Okvir(0, 0, d.W, d.H, Boja{1, 1, 1}, Boja{0, 0, 0})
		d.Tekst(4, 14, 9, true, "Marko Markić")
		if err := d.SlikaPNG(p.Bytes(), 4, 20, 8, 8); err != nil {
			t.Fatal(err)
		}
		if err := d.SlikaJPEG(j.Bytes(), 14, 20, 8, 8); err != nil {
			t.Fatal(err)
		}
		if err := d.SlikaJPEG(g.Bytes(), 24, 20, 8, 8); err != nil {
			t.Fatal(err)
		}
	}
}

// lazniPotpisnik pamti što je potpisao i vraća ono što mu se zada
type lazniPotpisnik struct {
	potpisano []byte
	cms       []byte
	greska    error
}

func (l *lazniPotpisnik) potpisi(podaci []byte) ([]byte, error) {
	l.potpisano = append([]byte{}, podaci...)
	return l.cms, l.greska
}

// cmsZapis je DER zapis zadane duljine sadržaja, kakav vraća pravi potpisnik
func cmsZapis(n int) []byte {
	b := []byte{0x30, 0x82, byte(n >> 8), byte(n)}
	for i := 0; i < n; i++ {
		b = append(b, byte(i%251+1))
	}
	return b
}

// provjeriXref: svaki pomak u zadnjoj tablici objekata pokazuje na svoj
// objekt, a startxref na samu tablicu
func provjeriXref(t *testing.T, pdf []byte) {
	t.Helper()
	m := regexp.MustCompile(`startxref\n(\d+)\n%%EOF\n$`).FindSubmatch(pdf)
	if m == nil {
		t.Fatal("nema startxref na kraju")
	}
	xref, _ := strconv.Atoi(string(m[1])) // regex je uzeo samo znamenke
	if !bytes.HasPrefix(pdf[xref:], []byte("xref\n")) {
		t.Fatalf("startxref %d ne pokazuje na tablicu", xref)
	}
	redci := strings.Split(string(pdf[xref:]), "\n")[1:]
	for i := 0; i < len(redci) && redci[i] != "trailer"; {
		var prvi, broj int
		if _, err := fmtSscanfOdsjecak(redci[i], &prvi, &broj); err != nil {
			t.Fatalf("zaglavlje odsječka %q", redci[i])
		}
		for k := 0; k < broj; k++ {
			off, err := strconv.Atoi(redci[i+1+k][:10])
			if err != nil {
				t.Fatalf("redak tablice %q", redci[i+1+k])
			}
			if !bytes.HasPrefix(pdf[off:], []byte(strconv.Itoa(prvi+k)+" 0 obj\n")) {
				t.Errorf("objekt %d: pomak %d pokazuje na %q", prvi+k, off, pdf[off:min(len(pdf), off+12)])
			}
		}
		i += 1 + broj
	}
}

func fmtSscanfOdsjecak(s string, a, b *int) (int, error) {
	dijelovi := strings.Fields(s)
	if len(dijelovi) != 2 {
		return 0, errors.New("nije odsječak")
	}
	var err error
	if *a, err = strconv.Atoi(dijelovi[0]); err != nil {
		return 0, err
	}
	*b, err = strconv.Atoi(dijelovi[1])
	return 2, err
}

// Pečat se dopisuje kao inkrementalna izmjena: stari dokument ostaje bajt za
// bajt, stranica dobiva bilješku, a nova tablica objekata pokazuje na stare
// preko /Prev.
func TestDodajPecat(t *testing.T) {
	pdf := dokumentZaDodatak(t)
	out, err := Dodaj(pdf, Dodatak{Stranica: 2, X: 56, Y: 600, W: 200, H: 46, Crtaj: izgled(t), Ime: "Marko Markić", Razlog: "Urudžba", Kad: kadDodatka})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, pdf) {
		t.Fatal("stari sadržaj se promijenio")
	}
	dodano := string(out[len(pdf):])
	for _, s := range []string{"/Subtype /Stamp /Name /goCOP", "/Annots [ ", "/Subtype /Form", "/Filter /DCTDecode", "/ColorSpace /DeviceGray", "/Filter /FlateDecode", "/M (D:20260310073000+01'00')", tekstUTF16("Urudžba")} {
		if !strings.Contains(dodano, s) {
			t.Errorf("u dodatku nema %q", s)
		}
	}
	if strings.Contains(dodano, "/AcroForm") || strings.Contains(dodano, "/ByteRange") {
		t.Error("pečat ne smije dodati polje potpisa")
	}
	stariXref := reXref.FindSubmatch(pdf)[1]
	if !strings.Contains(dodano, "/Prev "+string(stariXref)+" >>") {
		t.Error("trailer ne pokazuje na staru tablicu")
	}
	provjeriXref(t, out)
	if len(Potpisi(out)) != 0 {
		t.Error("pečat se čita kao potpis")
	}

	// vlastiti naziv pečata, bez izgleda: bilješka bez površine; dokument
	// koji ne završava novim retkom dobije ga prije dodatka
	bezRetka := bytes.TrimSuffix(pdf, []byte("\n"))
	out, err = Dodaj(bezRetka, Dodatak{Stranica: 1, Pecat: "Primljeno", Kad: kadDodatka})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, append(append([]byte{}, bezRetka...), '\n')) {
		t.Error("dokument bez novog retka na kraju nije dobio redak prije dodatka")
	}
	provjeriXref(t, out)
	if d := string(out[len(bezRetka):]); !strings.Contains(d, "/Name /Primljeno /Rect [0 0 0 0]") || strings.Contains(d, "/AP") {
		t.Errorf("pečat bez izgleda:\n%s", d)
	}
}

// Dva potpisa jedan za drugim: oba se čitaju natrag, prvi pokriva dokument
// do svoje izmjene, drugi cijeli, a polja obrasca su oba u katalogu.
func TestDodajDvaPotpisa(t *testing.T) {
	pdf := dokumentZaDodatak(t)
	prvi := &lazniPotpisnik{cms: cmsZapis(300)}
	v1, err := Dodaj(pdf, Dodatak{Stranica: 1, X: 56, Y: 600, W: 200, H: 46, Crtaj: izgled(t), Potpisnik: prvi.potpisi, Ime: "Marko Markić", Razlog: "Predaja", Mjesto: "Osijek", Kad: kadDodatka})
	if err != nil {
		t.Fatal(err)
	}
	provjeriXref(t, v1)
	drugi := &lazniPotpisnik{cms: append(cmsZapis(200), 0)} // zapis koji završava nulom
	v2, err := Dodaj(v1, Dodatak{Stranica: 1, X: 340, Y: 600, W: 200, H: 46, Potpisnik: drugi.potpisi, Ime: "Ana Anić", Razlog: "Ovjera", Kad: kadDodatka.Add(3 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(v2, v1) {
		t.Fatal("drugi potpis je promijenio prvi")
	}
	provjeriXref(t, v2)
	p := Potpisi(v2)
	if len(p) != 2 {
		t.Fatalf("potpisa %d, očekivano 2", len(p))
	}
	if !bytes.Equal(p[0].CMS, prvi.cms) || !bytes.Equal(p[0].Podaci, prvi.potpisano) || p[0].Cijeli {
		t.Error("prvi potpis se ne čita kako je upisan")
	}
	if !bytes.Equal(p[1].CMS, drugi.cms[:len(drugi.cms)-1]) || !bytes.Equal(p[1].Podaci, drugi.potpisano) || !p[1].Cijeli {
		t.Error("drugi potpis se ne čita kako je upisan")
	}
	if p[0].Ime != "Marko Markić" || p[0].Razlog != "Predaja" || p[1].Ime != "Ana Anić" || p[0].SubFilter != "ETSI.CAdES.detached" {
		t.Errorf("podaci potpisa: %+v / %+v", p[0], p[1])
	}
	if !p[1].Kad.Equal(kadDodatka.Add(3 * time.Hour)) {
		t.Errorf("vrijeme drugog potpisa %v", p[1].Kad)
	}
	dodano := string(v2[len(v1):])
	if !strings.Contains(dodano, "/T (Potpis2)") || strings.Count(reAcro.FindString(dodano), " 0 R") != 2 {
		t.Errorf("polja obrasca nakon drugog potpisa:\n%s", reAcro.FindString(dodano))
	}
	if strings.Contains(dodano, "/AP") || !strings.Contains(dodano, "/Rect [0 0 0 0]") {
		t.Error("potpis bez izgleda ne smije imati površinu")
	}
}

// Svaka greška zaustavlja dodatak prije nego što išta vrati
func TestDodajGreske(t *testing.T) {
	pdf := dokumentZaDodatak(t)
	predugi := &lazniPotpisnik{cms: make([]byte, rezerva+1)}
	neuspio := &lazniPotpisnik{greska: errors.New("kartica izvađena")}
	bezKataloga := bytes.Replace(pdf, []byte("/Type /Catalog"), []byte("/Type /Katalog"), 1)
	bezKataloga = regexp.MustCompile(`/Pages \d+ 0 R`).ReplaceAll(bezKataloga, []byte("/Stranice"))
	for _, c := range []struct {
		opis string
		pdf  []byte
		dod  Dodatak
		ima  string
	}{
		{"nije PDF", []byte("ovo nije dokument nikako"), Dodatak{Stranica: 1}, "nije PDF"},
		{"bez traillera", append([]byte("%PDF-1.4\n"), make([]byte, 40)...), Dodatak{Stranica: 1}, "zaglavlja"},
		{"bez korijena", bytes.Replace(pdf, []byte("/Root 1 0 R"), []byte("/Root 99 0 R"), -1), Dodatak{Stranica: 1}, "nema objekta 99"},
		{"bez stranica", bezKataloga, Dodatak{Stranica: 1}, "bez stranica"},
		{"stranica nula", pdf, Dodatak{Stranica: 0}, "nema stranice 0"},
		{"stranica previše", pdf, Dodatak{Stranica: 3}, "nema stranice 3"},
		{"potpisnik pao", pdf, Dodatak{Stranica: 1, Potpisnik: neuspio.potpisi}, "kartica izvađena"},
		{"potpis prevelik", pdf, Dodatak{Stranica: 1, Potpisnik: predugi.potpisi}, "ne stane"},
	} {
		out, err := Dodaj(c.pdf, c.dod)
		if err == nil || !strings.Contains(err.Error(), c.ima) || out != nil {
			t.Errorf("%s: greška %v, očekivano %q", c.opis, err, c.ima)
		}
	}
}
