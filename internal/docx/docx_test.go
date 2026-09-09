package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func sastavi(t *testing.T) *zip.Reader {
	t.Helper()
	d := Novi("Izvješće", "COP Osijek", time.Date(2026, 9, 9, 8, 30, 0, 0, time.UTC))
	d.Naslov("Vodomjerna postaja Batina")
	d.Podnaslov("izvješće za 1901.–2026.")
	d.Poglavlje("Kota nule")
	d.Par("Trst", "80,450 m")
	d.Odlomak("Profil je snimljen u starom sustavu.")
	d.Stavka("prva natuknica")
	d.Stavka("druga s „navodnicima\" i znakom & te <oštrim> zagradama")
	d.Tablica([]string{"Stanje", "Prag"}, [][]string{{"pripremno", "+300 cm"}, {"redovna", "+500 cm"}})
	d.Napomena("Procjena, ne mjerenje.")
	d.PrijelomStranice()
	d.Odlomak("Druga stranica.\nDrugi redak istog odlomka.")

	var b bytes.Buffer
	if err := d.Zapisi(&b); err != nil {
		t.Fatalf("zapisivanje: %v", err)
	}
	z, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatalf("nije valjan ZIP: %v", err)
	}
	return z
}

func procitaj(t *testing.T, z *zip.Reader, ime string) string {
	t.Helper()
	for _, f := range z.File {
		if f.Name == ime {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			b, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
	}
	t.Fatalf("u paketu nema dijela %q", ime)
	return ""
}

// Word odbija otvoriti paket kojemu ijedan propisani dio nedostaje. Kvar se
// ne vidi ni u jednom drugom testu — datoteka nastane, samo se ne da otvoriti.
func TestPaketImaSvePropisaneDijelove(t *testing.T) {
	z := sastavi(t)
	ima := map[string]bool{}
	for _, f := range z.File {
		ima[f.Name] = true
	}
	for _, dio := range []string{
		"[Content_Types].xml", "_rels/.rels", "docProps/core.xml",
		"word/document.xml", "word/_rels/document.xml.rels",
		"word/styles.xml", "word/numbering.xml",
	} {
		if !ima[dio] {
			t.Errorf("u paketu nema %q", dio)
		}
	}
}

// Svaki dio mora biti ispravan XML. Jedan nezatvoren element i Word javlja
// da je datoteka oštećena, bez ijedne naznake gdje.
func TestSvakiDioJeIspravanXML(t *testing.T) {
	z := sastavi(t)
	for _, f := range z.File {
		if !strings.HasSuffix(f.Name, ".xml") && !strings.HasSuffix(f.Name, ".rels") {
			continue
		}
		sadrzaj := procitaj(t, z, f.Name)
		dek := xml.NewDecoder(strings.NewReader(sadrzaj))
		for {
			_, err := dek.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("%s nije ispravan XML: %v", f.Name, err)
				break
			}
		}
	}
}

// Ono što je upisano mora biti u dokumentu, a znakovi koji u XML-u nešto
// znače moraju biti izbjegnuti — inače tekst s ampersandom razbije datoteku.
func TestSadrzajIZnakoviPrezivePakiranje(t *testing.T) {
	z := sastavi(t)
	doc := procitaj(t, z, "word/document.xml")
	for _, want := range []string{
		"Vodomjerna postaja Batina", "izvješće za 1901.–2026.", "Kota nule",
		"80,450 m", "prva natuknica", "pripremno", "+300 cm",
		"Procjena, ne mjerenje.", "Druga stranica.",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("u dokumentu nema %q", want)
		}
	}
	if strings.Contains(doc, "znakom & te") {
		t.Error("ampersand nije izbjegnut — Word bi javio oštećenu datoteku")
	}
	if !strings.Contains(doc, "znakom &amp; te &lt;oštrim&gt;") {
		t.Error("posebni znakovi nisu ispravno izbjegnuti")
	}
	// prijelom retka unutar odlomka
	if !strings.Contains(doc, "<w:br/>") {
		t.Error("prijelom retka se izgubio")
	}
	// naslov i vrijeme u svojstvima datoteke
	core := procitaj(t, z, "docProps/core.xml")
	if !strings.Contains(core, "<dc:title>Izvješće</dc:title>") ||
		!strings.Contains(core, "2026-09-09T08:30:00Z") {
		t.Errorf("svojstva datoteke: %s", core)
	}
}

// Prazna tablica se preskače: prazan okvir u izvješću tvrdi da podataka ima.
// Prazna vrijednost u paru isto — redak „Kota nule:" bez brojke izgleda kao
// podatak koji je izmjeren i ispao nula.
func TestPraznoSeNePisu(t *testing.T) {
	d := Novi("t", "a", time.Now())
	d.Tablica([]string{"a", "b"}, nil)
	d.Tablica(nil, [][]string{{"x"}})
	d.Par("Kota nule", "")
	d.Par("Kota nule", "   ")
	var b bytes.Buffer
	if err := d.Zapisi(&b); err != nil {
		t.Fatal(err)
	}
	z, _ := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	doc := procitaj(t, z, "word/document.xml")
	if strings.Contains(doc, "<w:tbl>") {
		t.Error("prazna tablica je ispisana")
	}
	if strings.Contains(doc, "Kota nule") {
		t.Error("par bez vrijednosti je ispisan")
	}
}

// probniPNG je najmanja ispravna PNG datoteka; sadržaj nije bitan, bitno je da
// prođe kroz paket nedirnut.
func probniPNG() []byte {
	return []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3, 4}
}

func dijeloviPaketa(t *testing.T, d *Dokument) map[string]string {
	t.Helper()
	var b bytes.Buffer
	if err := d.Zapisi(&b); err != nil {
		t.Fatalf("zapisivanje: %v", err)
	}
	z, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatalf("paket nije ZIP: %v", err)
	}
	dijelovi := map[string]string{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatalf("dio %s: %v", f.Name, err)
		}
		s, _ := io.ReadAll(r)
		r.Close()
		dijelovi[f.Name] = string(s)
	}
	return dijelovi
}

// Slika u dokumentu nije samo bajtovi u paketu: Word je pokaže tek kad se
// poklope tri mjesta — sadržaj, vrsta datoteke i veza s crtežom. Ako se
// razidu, dokument se otvori bez slike ili se javi kao oštećen.
func TestSlikaPovezujeSvaTriDijelaPaketa(t *testing.T) {
	d := noviProbni()
	d.Slika(probniPNG(), 768, 480, "Položaj letve & okolina")
	dijelovi := dijeloviPaketa(t, d)

	if got := dijelovi["word/media/slika1.png"]; got != string(probniPNG()) {
		t.Errorf("slika u paketu se ne podudara s izvornikom (%d bajtova)", len(got))
	}
	if !strings.Contains(dijelovi["[Content_Types].xml"], `Extension="png"`) {
		t.Error("[Content_Types].xml ne prijavljuje PNG — Word javlja oštećen dokument")
	}
	rels := dijelovi["word/_rels/document.xml.rels"]
	if !strings.Contains(rels, `Id="rIdSlika1"`) || !strings.Contains(rels, "media/slika1.png") {
		t.Errorf("veza prema slici nedostaje:\n%s", rels)
	}
	doc := dijelovi["word/document.xml"]
	if !strings.Contains(doc, `r:embed="rIdSlika1"`) {
		t.Error("crtež se ne poziva na vezu rIdSlika1")
	}
	// opis ide u atribut, pa se & mora izbjeći
	if strings.Contains(doc, "letve & okolina") {
		t.Error("opis slike nije izbjegnut — XML puca na &")
	}
	if !strings.Contains(doc, "letve &amp; okolina") {
		t.Error("opis slike se izgubio")
	}
}

// Široka slika mora stati u širinu stranice, a da se ne izobliči.
func TestSirokaSlikaStaneUStranicuBezIzoblicenja(t *testing.T) {
	d := noviProbni()
	d.Slika(probniPNG(), 4000, 2000, "široka")
	doc := dijeloviPaketa(t, d)["word/document.xml"]

	m := regexp.MustCompile(`<wp:extent cx="(\d+)" cy="(\d+)"/>`).FindStringSubmatch(doc)
	if m == nil {
		t.Fatalf("nema veličine crteža:\n%s", doc)
	}
	cx, _ := strconv.ParseInt(m[1], 10, 64)
	cy, _ := strconv.ParseInt(m[2], 10, 64)
	if cx != najvecaSirinaEMU {
		t.Errorf("širina %d, očekivano ograničenje %d", cx, najvecaSirinaEMU)
	}
	// omjer 2:1 mora ostati
	if odstupanje := float64(cx)/float64(cy) - 2.0; odstupanje > 0.01 || odstupanje < -0.01 {
		t.Errorf("omjer stranica se izgubio: %d×%d", cx, cy)
	}
}

// Dokument bez slike ne smije nositi ni prazan medijski dio ni PNG u
// [Content_Types] — takav paket neki čitači odbijaju.
func TestBezSlikePaketOstajeKakavJeBio(t *testing.T) {
	d := noviProbni()
	d.Par("Nešto", "bez slike")
	d.Slika(nil, 100, 100, "prazna")        // ništa se ne događa
	d.Slika(probniPNG(), 0, 0, "bez mjere") // ni ovdje
	dijelovi := dijeloviPaketa(t, d)

	for ime := range dijelovi {
		if strings.HasPrefix(ime, "word/media/") {
			t.Errorf("paket bez slike ipak nosi %s", ime)
		}
	}
	if strings.Contains(dijelovi["[Content_Types].xml"], `Extension="png"`) {
		t.Error("[Content_Types].xml prijavljuje PNG kojeg nema")
	}
	if strings.Contains(dijelovi["word/document.xml"], "w:drawing") {
		t.Error("u dokumentu je ostao crtež bez slike")
	}
}

func noviProbni() *Dokument {
	return Novi("Pokus", "COP Osijek", time.Date(2026, 9, 9, 8, 30, 0, 0, time.UTC))
}
