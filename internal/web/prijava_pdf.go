package web

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/pdfw"
	"gocop/internal/qr"
)

// kartaZaPDF prekodira PNG isječak karte u JPEG: pločice su fotografske, pa
// je PNG od 400–700 KB, a JPEG od 60–100 KB bez vidljive razlike
func kartaZaPDF(png []byte) []byte {
	img, _, err := image.Decode(bytes.NewReader(png))
	if err != nil {
		return nil
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 74}); err != nil {
		return nil
	}
	return b.Bytes()
}

// prilogPrijave je što uz prijavu ide u dokument: memorandum centra, sektor
// i područje za zaglavlje, slike po oznaci, isječak karte kao PNG (prazno
// kad ga nema), skenirani potpis, i adresa za QR kod
type prilogPrijave struct {
	Zaglavlje ZaglavljeIzvoza
	Sektor    *models.Sector
	Podrucje  *models.Area
	Slike     map[string][]byte
	Karta     []byte
	Zasluge   string
	Otisci    models.OtisciLista
	Adresa    string // javna adresa programa; prazno = QR nosi samo oznaku
}

// Mjesto prijemnog štambilja na prvoj stranici: gore desno, kao na
// dosadašnjim prijavama. Stalno je, pa se bilješka urudžbe poslije crta na
// isto mjesto preko potpisanog PDF-a.
const (
	stambiljW = 200.0
	stambiljH = 100.0
	stambiljY = 176.0 // ispod memoranduma, u desnom stupcu uz podatke o vodočuvaru
)

func stambiljX() float64 { return pdfw.A4W - 56 - stambiljW }

// sadrzajQR je što QR kod nosi: adresu prijave u programu kad je javna
// adresa postavljena, inače oznaku i kod dokumenta
func sadrzajQR(p *models.PrijavaSTerena, adresa string) string {
	if adresa != "" {
		return adresa + "/prijave/" + p.ID
	}
	return "gocop://prijave/" + p.ID + "?kod=" + p.Kod()
}

// crtajQR crta QR kod s lijevim gornjim kutom u (x, y), veličine w
func crtajQR(d *pdfw.Doc, tekst string, x, y, w float64) {
	k, err := qr.Kodiraj(tekst)
	if err != nil {
		return
	}
	m := w / float64(k.Velicina)
	for yy := 0; yy < k.Velicina; yy++ {
		for xx := 0; xx < k.Velicina; xx++ {
			if k.Moduli[yy][xx] {
				d.Ispuna(x+float64(xx)*m, y+float64(yy)*m, m+0.05, m+0.05, crna)
			}
		}
	}
}

// crtajStambilj crta prijemni štambilj kao na urudžbenom zapisniku
// Hrvatskih voda: naslov, redak „Primljeno”, pa klasifikacijska oznaka i
// urudžbeni broj lijevo, organizacijska jedinica i prilog desno. Vrijednosti
// su prazne dok pisarnica ne upiše ili ne zalijepi naljepnicu; upisane iz
// programa stoje na svom mjestu.
func crtajStambilj(d *pdfw.Doc, x, y float64, p *models.PrijavaSTerena, prilozi string) {
	d.Okvir(x, y, stambiljW, stambiljH, bijela, crna)
	d.TekstSredina(x+stambiljW/2, y+11, 7, true, "HRVATSKE VODE")
	// vodoravne pregrade: naslov, primljeno, klasa, urbroj
	for _, yy := range []float64{16, 34, 66} {
		d.CrtaBoja(x, y+yy, x+stambiljW, y+yy, 0.6, crna)
	}
	// okomita pregrada desnog stupca ispod retka „Primljeno”
	sx := x + stambiljW*0.62
	d.CrtaBoja(sx, y+34, sx, y+stambiljH, 0.6, crna)
	primljeno := ""
	if p.PrimljenoAt != nil {
		primljeno = p.PrimljenoAt.In(models.Zagreb).Format("02.01.2006.")
	}
	d.TekstBoja(x+5, y+28, 7, false, "Primljeno:", sivaTekst)
	if primljeno != "" {
		d.Tekst(x+60, y+28, 8.5, true, primljeno)
	}
	d.TekstBoja(x+5, y+45, 6.5, false, "Klasifikacijska oznaka", sivaTekst)
	if p.Klasa != "" {
		d.Tekst(x+5, y+59, 8.5, true, p.Klasa)
	}
	d.TekstBoja(sx+5, y+45, 6.5, false, "Org. jed.", sivaTekst)
	d.TekstBoja(x+5, y+77, 6.5, false, "Urudžbeni broj", sivaTekst)
	if p.Urbroj != "" {
		d.Tekst(x+5, y+91, 8.5, true, p.Urbroj)
	}
	d.TekstBoja(sx+5, y+77, 6.5, false, "Prilog", sivaTekst)
	if prilozi != "" {
		d.TekstBoja(sx+5, y+91, 6.5, false, prilozi, sivaTekst)
	}
}

// pdfPrijave crta prijavu s terena po uzoru na dosadašnju tiskanu prijavu
// vodočuvara: memorandum, mjesto za prijemni štambilj (samo prijava ide iz
// kuće), QR kod, podaci o vodočuvaru, naslov, tablica s danom, vodotokom,
// građevinom i opisom, karta kad stane, mjesto i potpis; zatim fotografije
// po dvije na stranici. Vraća PDF i mjesto bloka potpisa.
func pdfPrijave(p *models.PrijavaSTerena, pr prilogPrijave, t models.OrgTerms, crtajBlok bool) ([]byte, mjestaPotpisa) {
	d := pdfw.Novi(p.VrstaLabel()+" s terena "+p.Oznaka()+": "+p.Naslov, "goCOP")
	d.Predmet = "Prijava s terena: " + p.Ime
	d.SviZnakovi()
	org := t.OrgName
	if org == "" {
		org = "Hrvatske vode"
	}
	if pr.Zaglavlje.Organizacija != "" {
		org = pr.Zaglavlje.Organizacija
	}

	// memorandum kao na rješenjima i obavijestima: znak, organizacija, VGO,
	// centar s adresom; ispod njega lijevo podaci o vodočuvaru, desno
	// prijemni štambilj (samo prijava ide iz kuće)
	d.Y = d.Gore
	zaglavlje(d, t, pr.Sektor)
	sx := stambiljX()
	if p.Vrsta == models.PrijavaPrijava {
		crtajStambilj(d, sx, stambiljY, p, prilozi(p, pr))
	}
	d.Y = stambiljY - 10

	// tko i gdje, kao na obrascu
	polje := func(oznaka, vrijednost string, bold bool) {
		if vrijednost == "" {
			return
		}
		d.Osiguraj(16)
		d.Y += 10
		d.Tekst(d.Lijevo, d.Y, 8, true, oznaka)
		d.Tekst(d.Lijevo+150, d.Y, 9, bold, vrijednost)
		d.Y += 4
	}
	if pr.Podrucje != nil {
		polje("BRANJENO PODRUČJE:", fmt.Sprintf("%d, %s", pr.Podrucje.ID, pr.Podrucje.Name), false)
		vgiRed := pr.Podrucje.VgiName
		if pr.Podrucje.VgiPhone != "" {
			if vgiRed != "" {
				vgiRed += ", "
			}
			vgiRed += "tel. " + pr.Podrucje.VgiPhone
		}
		polje("VGI:", vgiRed, false)
	} else if p.AreaID > 0 {
		polje("BRANJENO PODRUČJE:", fmt.Sprint(p.AreaID), false)
	}
	polje("DIONICA:", p.DionicaCode, false)
	polje("IME I PREZIME (vodočuvara):", p.Ime, true)
	if p.Vrsta == models.PrijavaPrijava && d.Y < stambiljY+stambiljH {
		d.Y = stambiljY + stambiljH
	}

	// naslov: vrsta i naslov, sredina
	d.Y += 24
	d.TekstSredina(d.W/2, d.Y, 15, true, strings.ToUpper(p.VrstaLabel()))
	d.Y += 6
	d.OdlomakU(d.Lijevo, d.Sirina(), strings.ToUpper(p.Naslov), 10, false, pdfw.Sredina)
	d.Y += 8

	// tablica podataka, pa opis
	polje("Dan, mjesec i godina:", p.Datum.In(models.Zagreb).Format("02.01.2006."), true)
	polje("Naziv vodotoka:", p.Vodotok, true)
	gradjevina := p.Objekt
	if p.Element != "" {
		if gradjevina != "" {
			gradjevina += " / "
		}
		gradjevina += strings.ToUpper(p.Element)
	}
	polje("Građevina / konstrukcijski element:", gradjevina, true)
	polje("Važnost objekta:", p.Vaznost, true)
	polje("Stacionaža:", p.Stacionaza, true)
	if p.ImaKoordinate() {
		polje("Koordinate (WGS84):", fmt.Sprintf("%.6f, %.6f", *p.Latitude, *p.Longitude), false)
	}
	d.Y += 10
	d.Osiguraj(40)
	d.Y += 10
	d.Tekst(d.Lijevo, d.Y, 8, true, "Opis događanja:")
	d.Y -= 9
	d.OdlomakU(d.Lijevo+150, d.Sirina()-150, p.Opis, 9, false, pdfw.Lijevo)

	// karta: preko cijele širine kad stane s potpisom ispod, inače manja
	// lijevo uz potpis desno, a tek kad ni to ne stane na svojoj stranici
	const pw = 215.0
	kartaW := d.Sirina()
	kartaH := kartaW * float64(visinaKarte) / float64(sirinaKarte)
	kartaNacrtana := false
	kartaUzPotpis := false
	if len(pr.Karta) > 0 {
		if d.Y+16+kartaH+24+visinaPotpisa+40 <= d.H-d.Dolje {
			d.Y += 16
			d.Tekst(d.Lijevo, d.Y, 8, true, "Lokacija na karti:")
			d.Y += 6
			if err := d.SlikaJPEG(kartaZaPDF(pr.Karta), d.Lijevo, d.Y, kartaW, kartaH); err == nil {
				d.Y += kartaH + 9
				d.TekstBoja(d.Lijevo, d.Y, 6.5, false, kartaNapis(p, pr), sivaTekst)
				kartaNacrtana = true
			}
		} else if kw := d.Sirina() - pw - 24; d.Y+26+kw*float64(visinaKarte)/float64(sirinaKarte)+40 <= d.H-d.Dolje {
			kartaUzPotpis = true
		}
	}

	// mjesto i datum lijevo, potpis vodočuvara desno; ispod bloka ostaje
	// crta za vlastoručni potpis nakon ispisa
	d.Y += 26
	d.Osiguraj(visinaPotpisa + 30)
	yPot := d.Y
	m := mjestaPotpisa{stranica: d.Stranica(), y: yPot + 8, w: pw, h: visinaPotpisa, xVodocuvar: d.W - d.Desno - pw, xRuk: d.Lijevo}
	if kartaUzPotpis {
		kw := d.Sirina() - pw - 24
		kh := kw * float64(visinaKarte) / float64(sirinaKarte)
		d.Tekst(d.Lijevo, yPot, 8, true, "Lokacija na karti:")
		if err := d.SlikaJPEG(kartaZaPDF(pr.Karta), d.Lijevo, yPot+6, kw, kh); err == nil {
			d.TekstBoja(d.Lijevo, yPot+6+kh+9, 6, false, kartaNapis(p, pr), sivaTekst)
			kartaNacrtana = true
			if yPot+6+kh+20 > m.y+visinaPotpisa {
				d.Y = yPot + 6 + kh + 20 - visinaPotpisa - 6
			}
		}
	}
	mjesto := ""
	if pr.Podrucje != nil {
		mjesto = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(pr.Podrucje.Subcenter, "Podcentar "), "COP "))
	}
	if mjesto == "" {
		mjesto = pr.Zaglavlje.Mjesto
	}
	datum := p.Datum.In(models.Zagreb).Format("2.1.2006.")
	mjestoDatum := datum
	if mjesto != "" {
		mjestoDatum = "U " + uMjestu(mjesto) + ", " + datum
	}
	yMD := yPot + visinaPotpisa - 20
	if kartaUzPotpis {
		yMD = d.Y + visinaPotpisa + 20
	}
	d.Tekst(d.Lijevo, yMD, 8.5, false, mjestoDatum)
	d.TekstSredina(m.xVodocuvar+pw/2, yPot, 8.5, false, "potpis vodočuvara")
	switch {
	case p.Rekonstrukcija && p.ObjavljenoAt != nil:
		// prenesena iz ranije evidencije: bez elektroničkog potpisa, siv blok
		// koji to kaže, pa crta i ime kao na ispisu
		staro := d.Y
		d.Y = m.y
		sitno := "sken potpisanog ispisa: prilog na čvoru"
		if p.Sken == "" {
			sitno = "bez skena potpisanog ispisa"
		}
		blokOvjereBoja(d, m.xVodocuvar, pw, "PRENESENO IZ RANIJE EVIDENCIJE (app.bp16.xyz)", p.Ime,
			p.ObjavljenoAt.In(models.Zagreb).Format("02.01.2006. u 15:04")+" · prijava "+p.Oznaka()+" · bez e-potpisa", sitno, sivaTekst, sivaTekst)
		d.Y = staro
		crta := m.y + 46 + 42
		d.Crta(m.xVodocuvar+10, crta, m.xVodocuvar+pw-10, crta)
		d.TekstSredina(m.xVodocuvar+pw/2, crta+11, 9, false, p.Ime)
	default:
		crtajPotpisLista(d, m.xVodocuvar, m.y, pw, p.Ime, p.ObjavljenoAt, "prijava "+p.Oznaka(), p.Kod(), p.Cvor, pr.Otisci[p.UserID], crtajBlok, false)
		if p.ObjavljenoAt == nil {
			d.TekstSredina(m.xVodocuvar+pw/2, m.y+46+42+11, 8, false, "( "+p.Ime+" )")
		}
	}
	d.Y = m.y + visinaPotpisa + 6
	if kartaUzPotpis && yMD+10 > d.Y {
		d.Y = yMD + 10
	}
	// podnožje na svakoj stranici: oznaka i broj stranice; na prvoj još
	// napomena i QR kod za provjeru
	napomena := "NACRT: prijava još nije objavljena ni potpisana."
	switch {
	case p.Rekonstrukcija && p.ObjavljenoAt != nil:
		napomena = "Rekonstrukcija: prijava prenesena iz ranije evidencije VGI Baranja (app.bp16.xyz), složena iz podataka u goCOP-u; fotografije su smanjene i ugrađene."
	case p.ObjavljenoAt != nil:
		napomena = fmt.Sprintf("Upisano na dnevni list vodočuvara %03d/%d. Sastavljeno u goCOP-u; elektronički potpis je vremenska oznaka, a ispis se potpisuje i vlastoručno. Fotografije su smanjene i ugrađene.", p.ListBroj, p.Godina)
	}
	qrTekst := sadrzajQR(p, pr.Adresa)
	d.Podnozje = func(d *pdfw.Doc, stranica, ukupno int) {
		const qrW = 40.0
		y := d.H - d.Dolje
		d.CrtaBoja(d.Lijevo, y+2, d.W-d.Desno, y+2, 0.4, sivaRub)
		d.TekstBoja(d.Lijevo, y+12, 6.5, false, p.VrstaLabel()+" s terena "+p.Oznaka()+" · "+p.Ime+" · "+p.Datum.In(models.Zagreb).Format("02.01.2006."), sivaTekst)
		d.TekstSredina(d.W/2, y+12, 7, false, fmt.Sprintf("stranica %d/%d", stranica, ukupno))
		if stranica != 1 {
			return
		}
		// QR kod uz donji desni rub, natpis lijevo od njega; napomena u dva retka
		crtajQR(d, qrTekst, d.W-d.Desno-qrW, y+6, qrW)
		d.TekstBoja(d.W-d.Desno-qrW-5-pdfw.SirinaTeksta("provjera u goCOP-u", 5, false), y+44, 5, false, "provjera u goCOP-u", sivaSvijetla)
		yy := y + 22
		for _, redak := range pdfw.Prelomi(napomena, d.Sirina()-qrW-80, 6.5, false) {
			d.TekstBoja(d.Lijevo, yy, 6.5, false, redak, sivaTekst)
			yy += 8.5
		}
	}

	// karta na svojoj stranici kad na prvu nije stala
	if len(pr.Karta) > 0 && !kartaNacrtana {
		d.NovaStranica()
		d.Y = d.Gore + 10
		d.Tekst(d.Lijevo, d.Y, 9, true, "Lokacija na karti:")
		d.Y += 8
		if err := d.SlikaJPEG(kartaZaPDF(pr.Karta), d.Lijevo, d.Y, kartaW, kartaH); err == nil {
			d.Y += kartaH + 12
			d.TekstBoja(d.Lijevo, d.Y, 7.5, false, kartaNapis(p, pr), sivaTekst)
		}
	}

	// fotografije: po dvije na stranici, s natpisom
	for i, sl := range p.Slike {
		if i%2 == 0 {
			d.NovaStranica()
			d.Y = d.Gore + 10
		} else {
			d.Y += 18
		}
		napis := fmt.Sprintf("Fotografija %d od %d", i+1, len(p.Slike))
		if sl.Naziv != "" {
			napis += ": " + sl.Naziv
		}
		d.Tekst(d.Lijevo, d.Y, 9, true, napis)
		d.Y += 10
		b := pr.Slike[sl.ID]
		if len(b) == 0 || sl.Sirina == 0 || sl.Visina == 0 {
			d.TekstBoja(d.Lijevo, d.Y+10, 8, false, "Fotografija nije dostupna na ovom čvoru.", sivaTekst)
			d.Y += 24
			continue
		}
		w := d.Sirina()
		hMax := (d.H - d.Gore - d.Dolje - 70) / 2
		sw, sh := w, w*float64(sl.Visina)/float64(sl.Sirina)
		if sh > hMax {
			sh = hMax
			sw = sh * float64(sl.Sirina) / float64(sl.Visina)
		}
		_ = d.SlikaJPEG(b, d.Lijevo+(w-sw)/2, d.Y, sw, sh)
		d.Y += sh
	}
	return d.Bajtovi(), m
}

// dodatakUrudzbe je bilješka urudžbe preko štambilja na potpisanom PDF-u:
// ne mijenja potpisane bajtove, pa potpis vodočuvara vrijedi i dalje
func dodatakUrudzbe(p *models.PrijavaSTerena, upisao string, kad time.Time, prilozi string) pdfw.Dodatak {
	return pdfw.Dodatak{
		Stranica: 1, X: stambiljX(), Y: stambiljY, W: stambiljW, H: stambiljH, Pecat: "goCOPUrudzba",
		Ime: upisao, Razlog: "Urudžbeni zapisnik: KLASA " + p.Klasa + ", URBROJ " + p.Urbroj, Kad: kad,
		Crtaj: func(d *pdfw.Doc) { crtajStambilj(d, 0, 0, p, prilozi) },
	}
}

func vgi(pr prilogPrijave) string {
	if pr.Podrucje != nil {
		return pr.Podrucje.VgiName
	}
	return ""
}

func kartaNapis(p *models.PrijavaSTerena, pr prilogPrijave) string {
	s := "Oznaka: mjesto događaja"
	if p.ImaKoordinate() {
		s = fmt.Sprintf("Oznaka: %.6f, %.6f (WGS84)", *p.Latitude, *p.Longitude)
	}
	if pr.Zasluge != "" {
		s += ". " + pr.Zasluge
	}
	return s
}

// prilozi opisuje priloge za štambilj: karta i fotografije
func prilozi(p *models.PrijavaSTerena, pr prilogPrijave) string {
	var d []string
	if len(pr.Karta) > 0 {
		d = append(d, "karta")
	}
	if n := len(p.Slike); n == 1 {
		d = append(d, "1 fotografija")
	} else if n > 1 {
		d = append(d, fmt.Sprintf("%d fotografija", n))
	}
	return strings.Join(d, ", ")
}

// uMjestu daje lokativ za najčešća mjesta; ostala ostaju kako jesu
func uMjestu(m string) string {
	switch strings.ToLower(m) {
	case "darda":
		return "Dardi"
	case "osijek":
		return "Osijeku"
	case "draž":
		return "Dražu"
	case "virovitica":
		return "Virovitici"
	case "vinkovci":
		return "Vinkovcima"
	case "vukovar":
		return "Vukovaru"
	case "slavonski brod":
		return "Slavonskom Brodu"
	case "županja":
		return "Županji"
	case "našice":
		return "Našicama"
	case "đakovo":
		return "Đakovu"
	case "donji miholjac":
		return "Donjem Miholjcu"
	case "zagreb":
		return "Zagrebu"
	}
	return m
}

// PDFPrijaveRekonstrukcija crta prijavu prenesenu iz ranije evidencije kao
// izvornik bez potpisa, za uvoz; karta se slaže s pločica iz postavki kad
// prijava ima točku i pločice su dostupne
func PDFPrijaveRekonstrukcija(ctx context.Context, p *models.PrijavaSTerena, slike map[string][]byte, sek *models.Sector, area *models.Area, karta KartaPostavke) []byte {
	pr := prilogPrijave{Sektor: sek, Podrucje: area, Slike: slike, Otisci: models.OtisciLista{}}
	// memorandum kao na izvozima: znak organizacije (PNG), VGO sektora, centar
	t := models.Terms()
	pr.Zaglavlje = ZaglavljeIzvoza{Organizacija: t.OrgName}
	if t.HasLogo() && t.LogoMime == "image/png" {
		pr.Zaglavlje.LogoPNG = t.Logo
	}
	if sek != nil {
		pr.Zaglavlje.Odjel, pr.Zaglavlje.Centar = sek.VgoName, sek.CenterCop
	}
	if p.ImaKoordinate() {
		if k := slozKartu(ctx, karta, *p.Latitude, *p.Longitude, "http://localhost/"); k != nil {
			pr.Karta, pr.Zasluge = k.PNG, k.Zasluge
		}
	}
	pdf, _ := pdfPrijave(p, pr, models.Terms(), true)
	return pdf
}
