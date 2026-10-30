package xlsxw

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// dijeloviKnjige zapiše knjigu i vrati sadržaj svakog dijela zipa.
func dijeloviKnjige(t *testing.T, k *Knjiga) map[string]string {
	t.Helper()
	var buf bytes.Buffer
	if err := k.Zapisi(&buf); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	d := map[string]string{}
	for _, f := range z.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if _, err := b.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		rc.Close()
		d[f.Name] = b.String()
	}
	return d
}

// probniPNG je samo zaglavlje PNG-a sa zadanom širinom i visinom — toliko
// sidro logotipa čita.
func probniPNG(w, h uint32) []byte {
	b := make([]byte, 24)
	copy(b, "\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")
	binary.BigEndian.PutUint32(b[16:20], w)
	binary.BigEndian.PutUint32(b[20:24], h)
	return b
}

// Pojas mijenja samo stilove koji imaju svoju plavu inačicu.
func TestUPojasu(t *testing.T) {
	for stil, want := range map[int]int{
		Tablica: TablicaPojas, TablicaSredina: TablicaSredinaPojas, TablicaSivo: TablicaSivoPojas,
		TablicaKurziv: TablicaKurzivPojas, Zaglavlje: Zaglavlje, Obican: Obican, TablicaPojas: TablicaPojas,
	} {
		if got := UPojasu(stil); got != want {
			t.Errorf("UPojasu(%d) = %d, želim %d", stil, got, want)
		}
	}
}

// Naziv lista dulji od 31 znaka kraće se po znakovima, ne po bajtovima, pa
// hrvatska slova ostaju cijela.
func TestNoviListKratiNaziv(t *testing.T) {
	k := &Knjiga{}
	l := k.NoviList(strings.Repeat("č", 40))
	if n := len([]rune(l.Naziv)); n != 31 || l.Naziv != strings.Repeat("č", 31) {
		t.Errorf("naziv %q (%d znakova)", l.Naziv, n)
	}
	if l2 := k.NoviList("a*b?c[d]"); l2.Naziv != "abc(d)" {
		t.Errorf("očišćen naziv %q", l2.Naziv)
	}
	if len(k.Listovi) != 2 {
		t.Errorf("listova %d", len(k.Listovi))
	}
}

// Formula s tekstom nosi t="str", a prazna ćelija sa stilom ostaje (obrub),
// dok se prazna ćelija bez stila izostavlja.
func TestCelijeFormulaTekstIPrazne(t *testing.T) {
	l := &List{Naziv: "L"}
	l.Dodaj(FT(`IF(A1>0,"da","ne")`, "da & ne", Tablica), T(""), T("", Tablica), N(-0.25))
	x := l.xml(false)
	for _, s := range []string{
		`<c r="A1" s="6" t="str"><f>IF(A1&gt;0,&quot;da&quot;,&quot;ne&quot;)</f><v>da &amp; ne</v></c>`,
		`<c r="C1" s="6"/>`,
		`<c r="D1" s="0"><v>-0.25</v></c>`,
	} {
		if !strings.Contains(x, s) {
			t.Errorf("nema %s", s)
		}
	}
	if strings.Contains(x, `r="B1"`) {
		t.Error("prazna ćelija bez stila ne smije biti upisana")
	}
	if strings.Contains(x, `<drawing`) {
		t.Error("list bez crteža ne smije upućivati na crtež")
	}
}

// Spojene ćelije, visine redaka, podnožje i ispis vodoravno na jednu širinu.
func TestListSpajanjeVisinaIspis(t *testing.T) {
	l := &List{Naziv: "L", Vodoravno: true, Podnozje: "Stranica &P od &N", Sirine: []float64{0, 12.5}}
	l.Dodaj(T("naslov", Naslov))
	l.Dodaj(T("x"))
	l.Spoji(0, 0, 3, 0)
	l.Spoji(1, 1, 2, 2)
	l.Visina(0, 28)
	x := l.xml(false)
	for _, s := range []string{
		`<sheetPr><pageSetUpPr fitToPage="1"/></sheetPr>`,
		`<mergeCells count="2"><mergeCell ref="A1:D1"/><mergeCell ref="B2:C3"/></mergeCells>`,
		`<row r="1" ht="28.0" customHeight="1">`,
		`<row r="2">`,
		`orientation="landscape" fitToWidth="1" fitToHeight="0"`,
		`<oddFooter>Stranica &amp;P od &amp;N</oddFooter>`,
		`<col min="2" max="2" width="12.50" customWidth="1"/>`,
	} {
		if !strings.Contains(x, s) {
			t.Errorf("nema %s", s)
		}
	}
	if strings.Contains(x, `<col min="1"`) {
		t.Error("stupac bez širine ne smije dobiti <col>")
	}

	u := &List{Naziv: "U", Uspravno: true}
	if x := u.xml(false); !strings.Contains(x, `orientation="portrait" fitToWidth="1"`) || !strings.Contains(x, `fitToPage="1"`) {
		t.Errorf("uspravno na jednu širinu: %s", x)
	}
	o := &List{Naziv: "O"}
	if x := o.xml(false); !strings.Contains(x, `<pageSetup paperSize="9" orientation="portrait"/>`) || strings.Contains(x, "fitToPage") || strings.Contains(x, "<headerFooter>") {
		t.Errorf("običan ispis: %s", x)
	}
}

// Zamrzavanje: samo stupci → okno desno gore, redci i stupci → desno dolje.
func TestZamrzniStupceIRetke(t *testing.T) {
	for _, c := range []struct {
		z    [2]int
		want string
	}{
		{[2]int{0, 2}, `<pane xSplit="2" topLeftCell="C1" activePane="topRight" state="frozen"/><selection pane="topRight"/>`},
		{[2]int{1, 1}, `<pane xSplit="1" ySplit="1" topLeftCell="B2" activePane="bottomRight" state="frozen"/><selection pane="bottomRight"/>`},
	} {
		l := &List{Naziv: "Z", Zamrzni: c.z}
		if x := l.xml(false); !strings.Contains(x, c.want) {
			t.Errorf("zamrzni %v: %s", c.z, x)
		}
	}
}

// Redci koji se ponavljaju na svakoj stranici postaju definirano ime
// _xlnm.Print_Titles za taj list (1-based), s apostrofom u nazivu udvojenim.
func TestPonoviRetkeUKnjizi(t *testing.T) {
	k := &Knjiga{}
	k.NoviList("Prvi").Dodaj(T("a"))
	l := k.NoviList("Ivanov list")
	l.Naziv = "Ivan's & Co"
	l.Dodaj(T("zaglavlje"))
	l.PonoviRetke(2, 3)
	d := dijeloviKnjige(t, k)
	want := `<definedNames><definedName name="_xlnm.Print_Titles" localSheetId="1">&#39;Ivan&#39;&#39;s &amp; Co&#39;!$3:$4</definedName></definedNames>`
	wb := d["xl/workbook.xml"]
	if !strings.Contains(wb, want) && !strings.Contains(wb, strings.ReplaceAll(want, "&#39;", "'")) {
		t.Errorf("Print_Titles: %s", wb)
	}
	if !strings.Contains(wb, `<calcPr fullCalcOnLoad="1"/>`) {
		t.Error("knjiga mora preračunati formule pri otvaranju")
	}

	bez := dijeloviKnjige(t, &Knjiga{Listovi: []*List{{Naziv: "x"}}})
	if strings.Contains(bez["xl/workbook.xml"], "definedNames") {
		t.Error("bez ponavljanja redaka nema definiranih imena")
	}
}

// Logotip: slika se upiše jednom, list s Logo dobije crtež, a list bez
// njega ne; širina slike ide po omjeru iz PNG zaglavlja.
func TestLogotipUKnjizi(t *testing.T) {
	k := &Knjiga{LogoPNG: probniPNG(200, 100)}
	s := k.NoviList("S logotipom")
	s.Logo = true
	s.Sirine = []float64{60}
	s.Dodaj(T("x"))
	k.NoviList("Bez").Dodaj(T("y"))
	d := dijeloviKnjige(t, k)
	if d["xl/media/image1.png"] != string(k.LogoPNG) {
		t.Error("slika logotipa nije upisana kakva jest")
	}
	cr := d["xl/drawings/drawing1.xml"]
	if !strings.Contains(cr, `<xdr:ext cx="1580000" cy="790000"/>`) || !strings.Contains(cr, `r:embed="rIdL"`) {
		t.Errorf("sidro logotipa: %s", cr)
	}
	if !strings.Contains(d["xl/drawings/_rels/drawing1.xml.rels"], "../media/image1.png") {
		t.Error("crtež ne upućuje na sliku")
	}
	if _, ok := d["xl/drawings/drawing2.xml"]; ok {
		t.Error("list bez logotipa ne smije imati crtež")
	}
	if strings.Contains(d["xl/worksheets/sheet2.xml"], "<drawing") || !strings.Contains(d["xl/worksheets/sheet1.xml"], `<drawing r:id="rIdD"/>`) {
		t.Error("upute na crtež u listovima")
	}
	if !strings.Contains(d["[Content_Types].xml"], `/xl/drawings/drawing1.xml`) || strings.Contains(d["[Content_Types].xml"], `/xl/drawings/drawing2.xml`) {
		t.Error("vrste dijelova crteža")
	}

	// Logo traži list, a knjiga bez slike nema ni crteža ni medija.
	b := &Knjiga{}
	l := b.NoviList("L")
	l.Logo = true
	db := dijeloviKnjige(t, b)
	if _, ok := db["xl/media/image1.png"]; ok {
		t.Error("bez slike nema medija")
	}
	if _, ok := db["xl/drawings/drawing1.xml"]; ok {
		t.Error("bez slike nema crteža")
	}
}

// Logotip se smanji kad su stupci rezervirani za njega uži od 2,2 cm, i
// zadrži omjer; bez valjanog zaglavlja slika je kvadrat.
func TestSidroLogotipa(t *testing.T) {
	// Široki stupac: puna visina, širina po omjeru 2:1.
	if s := sidroLogotipa(probniPNG(2, 1), 10_000_000); !strings.Contains(s, `cx="1580000" cy="790000"`) {
		t.Errorf("široki stupac: %s", s)
	}
	// Uski stupac: mjesta 400000 EMU, omjer ostaje 2:1.
	if s := sidroLogotipa(probniPNG(2, 1), 520000); !strings.Contains(s, `cx="400000" cy="200000"`) {
		t.Errorf("uski stupac: %s", s)
	}
	// Prekratko zaglavlje i nulta visina: kvadrat 2,2 cm.
	for _, png := range [][]byte{[]byte("kratko"), probniPNG(100, 0)} {
		if s := sidroLogotipa(png, 10_000_000); !strings.Contains(s, `cx="790000" cy="790000"`) {
			t.Errorf("kvadrat: %s", s)
		}
	}
	// Stupac uži od dva ruba: ne smanjuje se u nulu.
	if s := sidroLogotipa(probniPNG(1, 1), 100000); !strings.Contains(s, `cx="790000" cy="790000"`) {
		t.Errorf("preuzak stupac: %s", s)
	}
}

// Širina stupaca za logotip: zadana 8,43 znaka kad širine nema, zbroj
// više stupaca kad ih logotip smije zauzeti više.
func TestSirinaStupcaA(t *testing.T) {
	emu := func(znakova float64) int64 { return int64((znakova*7 + 5) * 9525) }
	if got := (&List{}).sirinaStupcaA(); got != emu(8.43) {
		t.Errorf("zadana širina %d, želim %d", got, emu(8.43))
	}
	l := &List{Sirine: []float64{10, 0}, LogoStupaca: 3}
	ukupno := (10*7 + 5) + (8.43*7 + 5) + (8.43*7 + 5)
	want := int64(ukupno * 9525)
	if got := l.sirinaStupcaA(); got != want {
		t.Errorf("tri stupca %d, želim %d", got, want)
	}
}

// Logotip i dva grafa na istom listu dijele crtež; grafovi se broje kroz
// cijelu knjigu.
func TestLogotipIGrafoviKrozKnjigu(t *testing.T) {
	k := &Knjiga{LogoPNG: probniPNG(1, 1)}
	a := k.NoviList("A")
	a.Logo = true
	a.Grafovi = []Graf{{Naslov: "prvi"}, {Naslov: "drugi"}}
	b := k.NoviList("B")
	b.Grafovi = []Graf{{Naslov: "treći"}}
	d := dijeloviKnjige(t, k)
	for _, ime := range []string{"xl/charts/chart1.xml", "xl/charts/chart2.xml", "xl/charts/chart3.xml", "xl/drawings/drawing2.xml"} {
		if d[ime] == "" {
			t.Errorf("nema %s", ime)
		}
	}
	if !strings.Contains(d["xl/charts/chart3.xml"], "<a:t>treći</a:t>") {
		t.Error("treći graf nije na drugom listu")
	}
	veze := d["xl/drawings/_rels/drawing1.xml.rels"]
	for _, s := range []string{`Id="rIdL"`, `Id="rIdG1"`, `../charts/chart1.xml`, `Id="rIdG2"`, `../charts/chart2.xml`} {
		if !strings.Contains(veze, s) {
			t.Errorf("veze crteža 1 nemaju %s", s)
		}
	}
	if !strings.Contains(d["xl/drawings/_rels/drawing2.xml.rels"], `Id="rIdG1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart" Target="../charts/chart3.xml"`) {
		t.Errorf("veze crteža 2: %s", d["xl/drawings/_rels/drawing2.xml.rels"])
	}
	if !strings.Contains(d["xl/drawings/drawing1.xml"], `<xdr:cNvPr id="11" name="Graf 11"/>`) {
		t.Error("drugi graf na listu nema svoj broj")
	}
}

// Graf bez naslova, s oblikom osi x, korakom, naslovom osi y i bez granica
// osi y: Excel bira mjerilo, a korak y se ne upisuje.
func TestGrafBezNaslovaIMjerila(t *testing.T) {
	g := Graf{XMin: 10, XMax: 20, XKorak: 2.5, XFormat: "d.m.", YNaslov: "cm <AT>", YMin: 5, YMax: 5, YKorak: 10,
		Nizovi: []NizGrafa{{Naziv: "n", X: []float64{1, 2, 3}, Y: []float64{math.Inf(1), 4, math.Inf(-1)}, Boja: " 00ff00 ", Debljina: 2}, {Naziv: "z", Boja: "nije boja"}}}
	c := g.chartXML()
	for _, s := range []string{
		`<c:autoTitleDeleted val="1"/>`,
		`<c:numFmt formatCode="d.m." sourceLinked="0"/>`,
		`<c:majorUnit val="2.5"/>`,
		`<a:t>cm &lt;AT&gt;</a:t>`,
		`<c:max val="20"/><c:min val="10"/>`,
		`<a:ln w="25400" cap="rnd"><a:solidFill><a:srgbClr val="00FF00"/>`,
		`<a:srgbClr val="1F4E9A"/>`,
		`<c:ptCount val="3"/><c:pt idx="1"><c:v>4</c:v></c:pt></c:numLit>`,
	} {
		if !strings.Contains(c, s) {
			t.Errorf("graf nema %s", s)
		}
	}
	if strings.Count(c, "<c:max ") != 1 || strings.Contains(c, `<c:majorUnit val="10"/>`) {
		t.Error("os y bez granica ne smije imati ni granice ni korak")
	}
	if strings.Contains(c, "<c:legendEntry>") {
		t.Error("svi nizovi su u legendi")
	}
	if !strings.Contains(c, `<a:ln w="19050" cap="rnd">`) {
		t.Error("zadana debljina mora biti 1,5 točke")
	}
	if strings.Contains(c, `formatCode="General" sourceLinked="0"`) {
		t.Error("zadani oblik osi x ne smije biti upisan kad je zadan drugi")
	}
	if c2 := (Graf{}).chartXML(); !strings.Contains(c2, `<c:numFmt formatCode="General" sourceLinked="0"/>`) || strings.Contains(c2, "<c:majorUnit") {
		t.Errorf("prazni graf: zadani oblik osi x, bez koraka")
	}
}

// Excelov datum slaže se s Go kalendarom za bilo koji dan, i prije nove ere
// (prijestupne godine, prijelaz veljače, granice 400-godišnjih era).
func TestExcelDaniKaoKalendar(t *testing.T) {
	nula := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	for _, d := range [][3]int{
		{1899, 12, 30}, {1899, 12, 31}, {1900, 2, 28}, {2000, 2, 29}, {2000, 3, 1}, {2100, 3, 1},
		{2026, 10, 30}, {1, 1, 1}, {0, 2, 29}, {0, 3, 1}, {-1, 1, 1}, {-1, 12, 31}, {-399, 3, 1}, {-400, 1, 1}, {-401, 6, 15}, {-800, 2, 29},
	} {
		want := float64((time.Date(d[0], time.Month(d[1]), d[2], 0, 0, 0, 0, time.UTC).Unix() - nula.Unix()) / 86400)
		if got := excelDani(d[0], d[1], d[2]); got != want {
			t.Errorf("%v: %v, želim %v", d, got, want)
		}
	}
	if got := ExcelDatum(2026, 10, 30, 18, 36); got != excelDani(2026, 10, 30)+0.775 {
		t.Errorf("sat kao dio dana: %v", got)
	}
}

// pokvaren pisač javlja grešku nakon prvih n bajtova.
type pokvaren struct{ n int }

func (p *pokvaren) Write(b []byte) (int, error) {
	if len(b) > p.n {
		w := p.n
		p.n = 0
		return w, errors.New("disk pun")
	}
	p.n -= len(b)
	return len(b), nil
}

// Greška pri pisanju (pun disk) mora doći do pozivatelja, gdje god u
// knjizi nastala, a ne tiho dati krnju datoteku.
func TestZapisiJavljaGreskuPisanja(t *testing.T) {
	k := &Knjiga{LogoPNG: probniPNG(1, 1)}
	for i := 0; i < 3; i++ {
		l := k.NoviList("L")
		l.Logo = true
		l.Grafovi = []Graf{{Naslov: "g"}}
		for r := 0; r < 300; r++ {
			l.Dodaj(T(strings.Repeat("tekst ", 20)), N(float64(r)))
		}
		l.PonoviRetke(0, 0)
	}
	var cijela bytes.Buffer
	if err := k.Zapisi(&cijela); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{0, 100, 5000, cijela.Len() / 3, cijela.Len() / 2, cijela.Len() - 100} {
		if err := k.Zapisi(&pokvaren{n: n}); err == nil {
			t.Errorf("nakon %d bajtova: greška nije javljena", n)
		}
	}
}
