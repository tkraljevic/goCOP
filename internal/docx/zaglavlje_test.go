package docx

import (
	"bytes"
	"encoding/xml"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"
)

func probniZnak(t *testing.T, vrsta string, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{0, 128, 0, 255})
	var b bytes.Buffer
	var err error
	if vrsta == "jpeg" {
		err = jpeg.Encode(&b, img, nil)
	} else {
		err = png.Encode(&b, img)
	}
	if err != nil {
		t.Fatalf("probni znak: %v", err)
	}
	return b.Bytes()
}

func probnoZaglavlje(t *testing.T, vrsta string) Zaglavlje {
	t.Helper()
	z := Zaglavlje{
		Ustanova: "HRVATSKE VODE",
		Jedinica: []string{"VODNOGOSPODARSKI ODJEL", "ZA DUNAV I DONJU DRAVU",
			"Centar obrane od poplava Sektora B"},
		Adresa:  "Splavarska 2a, 31000 Osijek",
		Kontakt: []string{"Telefon:", "N/A"},
	}
	if vrsta != "" {
		z.Znak, z.ZnakVrsta = probniZnak(t, vrsta, 167, 193), vrsta
	}
	return z
}

// Zaglavlje je zaseban dio paketa i na njega upućuju tri mjesta. Ako se ijedno
// razmine, Word ga ne pokaže ili javi oštećen dokument — a to se vidi tek kad
// netko otvori gotovo izvješće.
func TestZaglavljeJePovezanoKrozCijeliPaket(t *testing.T) {
	d := noviProbni()
	d.PostaviZaglavlje(probnoZaglavlje(t, "jpeg"))
	d.Par("Nešto", "u tijelu")
	dijelovi := dijeloviPaketa(t, d)

	hdr, ima := dijelovi["word/header1.xml"]
	if !ima {
		t.Fatal("u paketu nema word/header1.xml")
	}
	dek := xml.NewDecoder(strings.NewReader(hdr))
	for {
		_, err := dek.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("header1.xml nije ispravan XML: %v", err)
		}
	}
	if !strings.Contains(dijelovi["[Content_Types].xml"], `PartName="/word/header1.xml"`) {
		t.Error("[Content_Types].xml ne prijavljuje zaglavlje")
	}
	if !strings.Contains(dijelovi["word/_rels/document.xml.rels"], `Id="rIdZaglavlje"`) {
		t.Error("dokument nije povezan sa zaglavljem")
	}
	doc := dijelovi["word/document.xml"]
	if !strings.Contains(doc, `<w:headerReference w:type="first" r:id="rIdZaglavlje"/>`) {
		t.Error("sekcija se ne poziva na zaglavlje")
	}
	if !strings.Contains(doc, "<w:titlePg/>") {
		t.Error("bez titlePg Word zaglavlje prve stranice uopće ne traži")
	}
	if !strings.Contains(doc, `xmlns:r=`) {
		t.Error("prostor imena r nije objavljen — dokument se ne otvara")
	}
}

// Znak u zaglavlju veže se iz veza zaglavlja, ne dokumenta; Word ga drugdje ne
// traži.
func TestZnakSeVezeIzZaglavljaINeIzDokumenta(t *testing.T) {
	d := noviProbni()
	d.PostaviZaglavlje(probnoZaglavlje(t, "jpeg"))
	dijelovi := dijeloviPaketa(t, d)

	if _, ima := dijelovi["word/media/znak.jpeg"]; !ima {
		t.Error("znak nije u paketu")
	}
	veze := dijelovi["word/_rels/header1.xml.rels"]
	if !strings.Contains(veze, `Id="rIdZnak"`) || !strings.Contains(veze, "media/znak.jpeg") {
		t.Errorf("veza na znak nedostaje:\n%s", veze)
	}
	if strings.Contains(dijelovi["word/_rels/document.xml.rels"], "rIdZnak") {
		t.Error("znak je vezan i iz dokumenta — Word to ne očekuje")
	}
	if !strings.Contains(dijelovi["[Content_Types].xml"], `Extension="jpeg"`) {
		t.Error("[Content_Types].xml ne prijavljuje JPEG")
	}
	if !strings.Contains(dijelovi["word/header1.xml"], `r:embed="rIdZnak"`) {
		t.Error("zaglavlje ne crta znak")
	}
}

// Znak zadržava omjer stranica: rastegnut grb ustanove je vidljiva pogreška na
// svakom dokumentu koji izađe iz centra.
func TestZnakZadrzavaOmjer(t *testing.T) {
	d := noviProbni()
	z := probnoZaglavlje(t, "png")
	z.Znak = probniZnak(t, "png", 167, 193)
	d.PostaviZaglavlje(z)

	if d.znak == nil {
		t.Fatal("znak nije prihvaćen")
	}
	if d.znak.visina != visinaZnakaEMU {
		t.Errorf("visina %d, očekivano %d", d.znak.visina, visinaZnakaEMU)
	}
	ocekivano := float64(visinaZnakaEMU) * 167 / 193
	if odstupanje := float64(d.znak.sirina) - ocekivano; odstupanje > 1 || odstupanje < -1 {
		t.Errorf("širina %d, očekivano oko %.0f", d.znak.sirina, ocekivano)
	}
}

// Vodoravan znak — grb s ispisanim nazivom uz njega — na zadanoj visini bio bi
// širi od cijelog memoranduma. Mora se sam smanjiti, i to bez izobličenja.
func TestVodoravanZnakStaneUStupac(t *testing.T) {
	d := noviProbni()
	z := probnoZaglavlje(t, "")
	z.Znak, z.ZnakVrsta = probniZnak(t, "png", 1200, 200), "png" // omjer 6:1
	d.PostaviZaglavlje(z)

	if d.znak == nil {
		t.Fatal("znak nije prihvaćen")
	}
	if d.znak.sirina > najveciZnakEMU {
		t.Errorf("širina %d prelazi granicu %d", d.znak.sirina, najveciZnakEMU)
	}
	if d.znak.visina >= visinaZnakaEMU {
		t.Errorf("visina %d nije smanjena", d.znak.visina)
	}
	if omjer := float64(d.znak.sirina) / float64(d.znak.visina); omjer < 5.9 || omjer > 6.1 {
		t.Errorf("omjer %.2f, očekivano 6", omjer)
	}
	// stupac mora ostati unutar sloga
	stupci := d.stupci()
	zbroj := 0
	for _, s := range stupci {
		zbroj += s
	}
	if zbroj != sirinaSloga {
		t.Errorf("zbroj stupaca %d, slog je %d", zbroj, sirinaSloga)
	}
	if stupci[2] < najmanjiRazmak {
		t.Errorf("razmak do kontakata je pao na %d", stupci[2])
	}
}

// Uspravan znak — grb bez teksta, kakav je na obrascu — ide u punoj visini, a
// stupac se pod njega proširi taman koliko treba, na račun praznog razmaka.
func TestUspravanZnakIdeUPunojVisini(t *testing.T) {
	d := noviProbni()
	z := probnoZaglavlje(t, "")
	z.Znak, z.ZnakVrsta = probniZnak(t, "png", 102, 118), "png"
	d.PostaviZaglavlje(z)

	if d.znak.visina != visinaZnakaEMU {
		t.Errorf("visina %d, očekivano %d", d.znak.visina, visinaZnakaEMU)
	}
	stupci := d.stupci()
	if trebaZnaku := int(d.znak.sirina / emuPoTwipu); stupci[0] <= trebaZnaku {
		t.Errorf("stupac %d ne prima znak širine %d", stupci[0], trebaZnaku)
	}
	if stupci[1] != stupciZaglavlja[1] || stupci[3] != stupciZaglavlja[3] {
		t.Errorf("naziv ili kontakti su se pomaknuli: %v", stupci)
	}
	zbroj := 0
	for _, s := range stupci {
		zbroj += s
	}
	if zbroj != sirinaSloga {
		t.Errorf("zbroj stupaca %d, slog je %d", zbroj, sirinaSloga)
	}
}

// Znak vrste koju Word ne prikazuje (SVG) preskače se, ali tekst zaglavlja
// ostaje: dokument bez grba je i dalje dokument tog centra, dokument bez
// zaglavlja nije.
func TestNepodrzanZnakNeRusiZaglavlje(t *testing.T) {
	d := noviProbni()
	z := probnoZaglavlje(t, "")
	z.Znak, z.ZnakVrsta = []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), VrstaZnaka("image/svg+xml")
	d.PostaviZaglavlje(z)
	dijelovi := dijeloviPaketa(t, d)

	if !strings.Contains(dijelovi["word/header1.xml"], "HRVATSKE VODE") {
		t.Error("tekst zaglavlja se izgubio zajedno sa znakom")
	}
	for ime := range dijelovi {
		if strings.HasPrefix(ime, "word/media/") {
			t.Errorf("nepodržan znak je ipak ušao u paket kao %s", ime)
		}
	}
	if strings.Contains(dijelovi["word/header1.xml"], "r:embed") {
		t.Error("zaglavlje crta znak kojeg u paketu nema")
	}
}

// Oštećena datoteka koja se predstavlja kao PNG ne smije srušiti sastavljanje.
func TestNeispravanZnakSePreskace(t *testing.T) {
	d := noviProbni()
	z := probnoZaglavlje(t, "")
	z.Znak, z.ZnakVrsta = []byte{0x89, 'P', 'N', 'G', 0, 1, 2}, "png"
	d.PostaviZaglavlje(z)
	if d.znak != nil {
		t.Error("neispravan PNG je prihvaćen kao znak")
	}
	if !strings.Contains(dijeloviPaketa(t, d)["word/header1.xml"], "Centar obrane") {
		t.Error("zaglavlje nije sastavljeno")
	}
}

// Sav sadržaj zaglavlja mora doći u dokument; izostavljen redak nitko ne
// primijeti dok ne usporedi s papirnatim obrascem.
func TestZaglavljeIspisujeSveRetke(t *testing.T) {
	d := noviProbni()
	z := probnoZaglavlje(t, "png")
	d.PostaviZaglavlje(z)
	hdr := dijeloviPaketa(t, d)["word/header1.xml"]

	for _, red := range append([]string{z.Ustanova, z.Adresa}, append(z.Jedinica, z.Kontakt...)...) {
		if !strings.Contains(hdr, red) {
			t.Errorf("zaglavlje ne sadrži %q", red)
		}
	}
}

// Prazno zaglavlje ne postoji: paket ostaje kakav je bio prije nego što je
// zaglavlja uopće bilo.
func TestPraznoZaglavljeSeNePostavlja(t *testing.T) {
	d := noviProbni()
	d.PostaviZaglavlje(Zaglavlje{})
	if d.ImaZaglavlje() {
		t.Fatal("prazno zaglavlje je postavljeno")
	}
	dijelovi := dijeloviPaketa(t, d)
	if _, ima := dijelovi["word/header1.xml"]; ima {
		t.Error("paket nosi prazno zaglavlje")
	}
	if strings.Contains(dijelovi["word/document.xml"], "titlePg") {
		t.Error("sekcija traži zaglavlje kojeg nema")
	}
}

// Znakovi koji u XML-u nešto znače moraju proći kroz zaglavlje neizmijenjeni.
func TestZaglavljePodnosiPosebneZnakove(t *testing.T) {
	d := noviProbni()
	d.PostaviZaglavlje(Zaglavlje{Ustanova: `Vode & "more"`, Jedinica: []string{"<Odjel>"}})
	hdr := dijeloviPaketa(t, d)["word/header1.xml"]

	dek := xml.NewDecoder(strings.NewReader(hdr))
	for {
		_, err := dek.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("zaglavlje s posebnim znakovima nije ispravan XML: %v", err)
		}
	}
	if !strings.Contains(hdr, "Vode &amp;") || !strings.Contains(hdr, "&lt;Odjel&gt;") {
		t.Errorf("znakovi nisu izbjegnuti:\n%s", hdr)
	}
}
