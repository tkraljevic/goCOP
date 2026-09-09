package docx

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // za čitanje veličine znaka
	_ "image/png"
	"strconv"
	"strings"
)

// Zaglavlje je memorandum ustanove na prvoj stranici dokumenta: znak, naziv
// ustanove i ustrojstvene jedinice, adresa i brojevi telefona. Preslikan je s
// papirnatog obrasca centra obrane, da dokument koji program sastavi izgleda
// kao dokument koji iz tog centra i inače izlazi.
//
// Ništa se ovdje ne upisuje u kod: sve što zaglavlje ispisuje program već
// vodi u nazivlju organizacije i u zapisu sektora, pa svaki centar dobiva
// svoje zaglavlje bez ijedne izmjene programa.
type Zaglavlje struct {
	Znak      []byte   // znak ustanove; podržani su PNG i JPEG
	ZnakVrsta string   // "png" ili "jpeg"; prazno kad znaka nema
	Ustanova  string   // "HRVATSKE VODE"
	Jedinica  []string // "VODNOGOSPODARSKI ODJEL", "ZA DUNAV I DONJU DRAVU", "Centar obrane…"
	Adresa    string
	Kontakt   []string // "Telefon:", "+385 31 252 800", …
}

// Prazno javlja da nema što ispisati; takav se dokument sastavlja bez
// zaglavlja, kao i dosad.
func (z Zaglavlje) Prazno() bool {
	return strings.TrimSpace(z.Ustanova) == "" && len(z.Jedinica) == 0 &&
		strings.TrimSpace(z.Adresa) == "" && len(z.Kontakt) == 0 && len(z.Znak) == 0
}

// Boje i pisma preuzeti s obrasca centra; ondje su dio vizualnog identiteta
// ustanove, pa se ne biraju iznova.
const (
	pismoJedinice = "Lucida Sans Unicode"
	pismoKontakta = "Arial"
	bojaJedinice  = "2F5496"
	bojaKontakta  = "0C54B4"
	// Visina znaka u memorandumu. Na papirnatom obrascu znak je visok 0,87
	// palca, ali ondje je uz njega i tekst u dva stupca; u izvješću stoji sam,
	// pa ispadne premalen. Malo je povećan da nosi zaglavlje.
	visinaZnakaEMU = 1051560 // 1,15 palca
	emuPoTwipu     = 635
)

// Širine stupaca u twipima, kao na obrascu; zbroj je širina sloga A4 s
// rubovima od 1417. Širina se zadaje izričito, a ne prepušta prikazivaču:
// tablica koja se skupi na sadržaj gurne memorandum u lijevu polovicu
// stranice, umjesto da stoji preko cijelog sloga.
var stupciZaglavlja = [4]int{1310, 3488, 1520, 2754}

const sirinaSloga = 1310 + 3488 + 1520 + 2754

// najmanjiRazmak je stupac koji odvaja naziv od kontakata; ispod toga se
// tekst i brojevi počnu doticati.
const najmanjiRazmak = 400

// najveciZnakEMU je najšire što znak smije biti: prvi stupac proširen do
// granice, bez ruba ćelije.
const najveciZnakEMU = (1310 + 1520 - najmanjiRazmak - 140) * emuPoTwipu

// stupci vraća širine prilagođene znaku koji je doista unesen. Znak širi od
// prvog stupca inače razvuče tablicu preko sloga, pa mu se stupac proširi, a
// razlika uzme od praznog razmaka u sredini — naziv ustanove i kontakti
// ostaju ondje gdje su na obrascu.
func (d *Dokument) stupci() [4]int {
	s := stupciZaglavlja
	if d.znak == nil {
		return s
	}
	treba := int(d.znak.sirina/emuPoTwipu) + 140 // znak plus rub ćelije
	if treba <= s[0] {
		return s
	}
	visak := treba - s[0]
	if moze := s[2] - najmanjiRazmak; visak > moze {
		visak = moze
	}
	if visak <= 0 {
		return s
	}
	s[0] += visak
	s[2] -= visak
	return s
}

// PostaviZaglavlje daje dokumentu memorandum. Znak nepodržane vrste (npr.
// SVG, koji Word ne prikazuje bez rasterske zamjene) preskače se, a tekst
// zaglavlja ostaje — bolje zaglavlje bez znaka nego dokument bez zaglavlja.
func (d *Dokument) PostaviZaglavlje(z Zaglavlje) {
	if z.Prazno() {
		return
	}
	if len(z.Znak) > 0 && (z.ZnakVrsta == "png" || z.ZnakVrsta == "jpeg") {
		if k, _, err := image.DecodeConfig(bytes.NewReader(z.Znak)); err == nil && k.Height > 0 {
			visina := int64(visinaZnakaEMU)
			sirina := int64(float64(visina) * float64(k.Width) / float64(k.Height))
			// Vodoravan znak (npr. onaj s ispisanim nazivom uz grb) na zadanoj
			// visini bio bi širi od stupca i razgurao bi memorandum. Takav se
			// ravna po širini, pa ispadne niži — bolje niži znak nego razvaljeno
			// zaglavlje.
			if sirina > najveciZnakEMU {
				visina = visina * najveciZnakEMU / sirina
				sirina = najveciZnakEMU
			}
			d.znak = &znakSlike{
				slika:  slika{ime: "znak." + z.ZnakVrsta, sadrzaj: z.Znak},
				visina: visina,
				sirina: sirina,
			}
		}
	}
	d.zaglavlje = &z
}

type znakSlike struct {
	slika
	sirina, visina int64
}

// VrstaZnaka pretvara MIME u oznaku vrste koju zaglavlje razumije. Prazan
// odgovor znači da se znak ne može ugraditi.
func VrstaZnaka(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg":
		return "jpeg"
	}
	return ""
}

// zaglavljeDio je dio paketa word/header1.xml. Građen je kao tablica bez
// obruba: znak lijevo, naziv ustanove u sredini, brojevi telefona desno —
// isti raspored kao na obrascu.
func (d *Dokument) zaglavljeDio() string {
	z := d.zaglavlje
	var b strings.Builder
	b.WriteString(zaglavljeXML +
		`<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		`<w:tbl><w:tblPr><w:tblW w:w="` + strconv.Itoa(sirinaSloga) + `" w:type="dxa"/>` +
		`<w:tblLayout w:type="fixed"/></w:tblPr><w:tblGrid>`)
	stupci := d.stupci()
	for _, s := range stupci {
		fmt.Fprintf(&b, `<w:gridCol w:w="%d"/>`, s)
	}
	b.WriteString(`</w:tblGrid><w:tr>`)

	// znak
	b.WriteString(celijaPoravnata(stupci[0], d.znakXML(), "center"))

	// ustanova i jedinica
	var sredina strings.Builder
	if z.Ustanova != "" {
		sredina.WriteString(redakZaglavlja(z.Ustanova, pismoJedinice, 28, bojaJedinice, "center", true))
	}
	for _, red := range z.Jedinica {
		sredina.WriteString(redakZaglavlja(red, pismoJedinice, 16, bojaJedinice, "center", false))
	}
	if z.Adresa != "" {
		sredina.WriteString(redakZaglavlja(z.Adresa, pismoJedinice, 16, bojaJedinice, "center", false))
	}
	b.WriteString(celijaPoravnata(stupci[1], sredina.String(), "center"))

	// razmak
	b.WriteString(celija(stupci[2], prazanOdlomak))

	// kontakti
	var desno strings.Builder
	for _, red := range z.Kontakt {
		desno.WriteString(redakZaglavlja(red, pismoKontakta, 16, bojaKontakta, "right", false))
	}
	b.WriteString(celija(stupci[3], desno.String()))

	b.WriteString(`</w:tr></w:tbl>` + prazanOdlomak + `</w:hdr>`)
	return b.String()
}

const prazanOdlomak = `<w:p><w:pPr><w:spacing w:before="0" w:after="0"/></w:pPr></w:p>`

func celija(sirina int, sadrzaj string) string {
	return celijaPoravnata(sirina, sadrzaj, "")
}

// celijaPoravnata: naziv ustanove stoji okomito po sredini uz znak. Znak je
// viši od tri-četiri retka teksta, pa uz vrh ćelije tekst izgleda kao da visi.
func celijaPoravnata(sirina int, sadrzaj, okomito string) string {
	if sadrzaj == "" {
		sadrzaj = prazanOdlomak
	}
	poravnanje := ""
	if okomito != "" {
		poravnanje = fmt.Sprintf(`<w:vAlign w:val="%s"/>`, okomito)
	}
	return fmt.Sprintf(`<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>%s</w:tcPr>%s</w:tc>`,
		sirina, poravnanje, sadrzaj)
}

func redakZaglavlja(tekst, pismo string, velicina int, boja, poravnanje string, prvi bool) string {
	razmak := `<w:spacing w:before="0" w:after="0"/>`
	prored := ""
	if prvi {
		razmak = `<w:spacing w:before="120" w:after="0"/>`
		// Naziv ustanove na obrascu ima razmaknuta slova; bez toga redak
		// izgleda zbijeno u odnosu na papirnati memorandum.
		prored = `<w:spacing w:val="20"/>`
	}
	oblik := fmt.Sprintf(`<w:rFonts w:ascii="%s" w:hAnsi="%s" w:cs="%s"/>`+
		`<w:color w:val="%s"/>%s<w:sz w:val="%d"/><w:szCs w:val="%d"/>`,
		pismo, pismo, pismo, boja, prored, velicina, velicina)
	return fmt.Sprintf(`<w:p><w:pPr>%s<w:jc w:val="%s"/><w:rPr>%s</w:rPr></w:pPr>`+
		`<w:r><w:rPr>%s</w:rPr><w:t xml:space="preserve">%s</w:t></w:r></w:p>`,
		razmak, poravnanje, oblik, oblik, escape(tekst))
}

// znakXML je crtež znaka u zaglavlju. Veza rIdZnak živi u vezama zaglavlja, a
// ne dokumenta: Word sliku u zaglavlju traži ondje.
func (d *Dokument) znakXML() string {
	if d.znak == nil {
		return ""
	}
	return fmt.Sprintf(`<w:p><w:pPr><w:spacing w:before="60" w:after="0"/></w:pPr><w:r><w:drawing>`+
		`<wp:inline distT="0" distB="0" distL="0" distR="0" `+
		`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">`+
		`<wp:extent cx="%d" cy="%d"/><wp:docPr id="900" name="znak" descr="%s"/>`+
		`<a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">`+
		`<a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:nvPicPr><pic:cNvPr id="900" name="znak"/><pic:cNvPicPr/></pic:nvPicPr>`+
		`<pic:blipFill><a:blip r:embed="rIdZnak"/>`+
		`<a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm>`+
		`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr>`+
		`</pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`,
		d.znak.sirina, d.znak.visina, escape("Znak ustanove"), d.znak.sirina, d.znak.visina)
}

// zaglavljeVeze su veze samog zaglavlja; nosi ih samo kad u njemu ima znaka.
func (d *Dokument) zaglavljeVeze() string {
	if d.znak == nil {
		return zaglavljeXML + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`
	}
	return zaglavljeXML + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rIdZnak" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" ` +
		`Target="media/` + d.znak.ime + `"/></Relationships>`
}
