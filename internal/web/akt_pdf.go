package web

import (
	"fmt"
	"math"
	"strings"

	"gocop/internal/models"
	"gocop/internal/pdfw"
)

// PDFAkta crta akt po uzoru na dosadašnje akte COP-a: znak i odjel gore
// lijevo, telefoni desno, pa blok centra obrane; pravna osnova s vodostajem,
// naslov, područje i dionice, dan i sat, završna rečenica; potpisnik desno;
// "O tome obavijest" sitnim slovima u dva stupca, naziv i e-pošta; poveznice
// na dnu. Nacrt nosi vidljivu oznaku; ovjeren nosi tko, kad i kod za provjeru.
func PDFAkta(a *models.Akt, t models.OrgTerms, sek *models.Sector, area *models.Area) []byte {
	return pdfAkta(a, t, sek, area, nacinProgram, models.OtisciAkta{})
}

// PDFAktaSaZigom je PDF ovjerenog akta sa skeniranim žigom centra i
// skeniranim potpisom ovjeritelja uz blok elektroničke ovjere
func PDFAktaSaZigom(a *models.Akt, t models.OrgTerms, sek *models.Sector, area *models.Area, o models.OtisciAkta) []byte {
	return pdfAkta(a, t, sek, area, nacinProgram, o)
}

// PDFAktaZaIspis je PDF nacrta za ispis, vlastoručni potpis i žig: crta za
// potpis, mjesto pečata, bez oznake nacrta
func PDFAktaZaIspis(a *models.Akt, t models.OrgTerms, sek *models.Sector, area *models.Area) []byte {
	return pdfAkta(a, t, sek, area, nacinIspis, models.OtisciAkta{})
}

// Načini PDF-a akta
const (
	nacinProgram = iota // nacrt ili akt ovjeren u goCOP-u
	nacinIspis          // za ispis, vlastoručni potpis i žig
)

func pdfAkta(a *models.Akt, t models.OrgTerms, sek *models.Sector, area *models.Area, nacin int, otisci models.OtisciAkta) []byte {
	zaPotpis := nacin == nacinIspis // ispis bez oznake nacrta, s mjestom za potpis i žig
	naslov := a.Naslov() + " " + a.Oznaka()
	if zaPotpis {
		naslov = a.Naslov()
	}
	d := pdfw.Novi(naslov, "goCOP")
	d.Predmet = "goCOP akt " + a.ID
	d.Gore, d.Lijevo, d.Desno = 36, 50, 50
	d.Y = d.Gore
	podnozje := a.Naslov() + " · " + a.Oznaka()
	if zaPotpis {
		podnozje = a.Naslov()
	}
	d.Podnozje = func(d *pdfw.Doc, stranica, ukupno int) {
		d.Tekst(d.Lijevo, d.H-d.Dolje+30, 6.5, false, podnozje)
		d.TekstDesno(d.W-d.Desno, d.H-d.Dolje+30, 6.5, false, fmt.Sprintf("stranica %d od %d", stranica, ukupno))
	}

	zaglavlje(d, t, sek)

	if !a.Ovjeren() && !zaPotpis {
		d.Odlomak("NACRT — nije ovjeren", 10, true, pdfw.Desno)
		d.Razmak(2)
	}

	// pravna osnova s vodostajem
	d.Odlomak(a.TekstUvoda(), 9.5, false, pdfw.Lijevo)
	d.Razmak(12)

	// naslov
	d.Odlomak(a.Vrsta(), 17, true, pdfw.Sredina)
	d.Razmak(2)
	d.Odlomak("o "+a.RadnjaLabel()+" "+a.Predmet(), 10.5, true, pdfw.Sredina)
	d.Razmak(8)

	// područje i dionice: oznaka lijevo, sadržaj u stupcu
	stupac := d.Lijevo + 120
	sirina := d.W - d.Desno - stupac
	podrucje := fmt.Sprintf("%d", a.AreaID)
	if area != nil && area.Name != "" {
		podrucje += ": " + area.Name
	}
	d.Osiguraj(14)
	d.Y += 10
	d.TekstDesno(stupac-6, d.Y, 9.5, false, "na branjenom području")
	d.Y -= 10
	d.OdlomakU(stupac, sirina, podrucje, 9.5, false, pdfw.Lijevo)
	d.Razmak(4)
	natpis := "na dionicama:"
	if len(a.Dionice) == 1 {
		natpis = "na dionici:"
	}
	for i, dn := range a.Dionice {
		d.Osiguraj(24)
		d.Y += 10
		if i == 0 {
			d.TekstDesno(stupac-6, d.Y, 9.5, false, natpis)
		}
		d.Tekst(stupac, d.Y, 9.5, true, dn.Code)
		d.Y -= 10
		d.OdlomakU(stupac+48, sirina-48, dn.Opis, 9.5, false, pdfw.Lijevo)
		d.Razmak(2)
	}
	d.Razmak(4)
	d.Osiguraj(16)
	d.Y += 11
	d.TekstDesno(stupac-6, d.Y, 9.5, false, "dana:")
	d.Tekst(stupac, d.Y, 10.5, true, a.Vrijedi.In(models.Zagreb).Format("02.01.2006.")+"   u   "+a.Vrijedi.In(models.Zagreb).Format("15:04")+"  sati")
	d.Razmak(12)

	if a.IzvanSnage != "" {
		d.Odlomak(a.IzvanSnage, 9.5, true, pdfw.Lijevo)
		d.Razmak(6)
	}
	d.Odlomak(a.TekstZavrsni(), 9.5, false, pdfw.Lijevo)
	if a.Napomena != "" {
		d.Razmak(4)
		d.Odlomak(a.Napomena, 9.5, false, pdfw.Lijevo)
	}
	d.Razmak(14)

	// potpisnik desno; ovjeren akt nosi blok elektroničkog potpisa, a ispod
	// ostaje crta za vlastoručni potpis i žig na ispisu
	d.Osiguraj(100)
	potpisX, potpisW := d.W-d.Desno-210, 210.0
	d.OdlomakU(potpisX, potpisW, a.Potpisnik, 9, false, pdfw.Sredina)
	switch {
	case nacin == nacinIspis:
		// crta za vlastoručni potpis i mjesto pečata
		d.Razmak(40)
		d.Tekst(potpisX-40, d.Y, 8, false, "M.P.")
		d.Crta(potpisX+15, d.Y, potpisX+potpisW-15, d.Y)
		d.Razmak(14)
	default:
		if a.Ovjeren() && a.OvjerenoAt != nil {
			d.Razmak(4)
			// skenirani žig centra lijevo od bloka potpisa, na mjestu pečata
			if zig := otisci.Zig; zig != nil && len(zig.Slika) > 0 {
				const zw = 96.0
				slika(d, zig.Mime, zig.Slika, potpisX-zw-18, d.Y-16, zw, zw)
			}
			blokPotpisa(d, a, potpisX+5, potpisW-10)
			// skenirani vlastoručni potpis ovjeritelja, iznad crte za potpis
			if p := otisci.Potpis; p != nil && len(p.Slika) > 0 {
				d.Razmak(4)
				slika(d, p.Mime, p.Slika, potpisX+potpisW/2-60, d.Y, 120, 36)
				d.Razmak(38)
			} else {
				d.Razmak(16)
			}
		} else {
			d.Razmak(24)
		}
		d.Crta(potpisX+15, d.Y, potpisX+potpisW-15, d.Y)
		if a.Ovjeren() {
			d.Razmak(2)
			d.OdlomakU(potpisX, potpisW, a.ImePotpisa(), 8.5, false, pdfw.Sredina)
		}
		d.Razmak(8)
	}

	// O tome obavijest: sitno, naziv lijevo, e-pošta u drugom stupcu
	const sitno = 6.3
	d.Osiguraj(12)
	d.Y += 7
	d.Tekst(d.Lijevo, d.Y, 7, false, "O tome obavijest:")
	d.Razmak(3)
	brojevi := models.RedniBrojevi(a.Primatelji)
	mailX := d.Lijevo + d.Sirina()*0.56
	for i, p := range a.Primatelji {
		d.Osiguraj(9)
		d.Y += sitno
		x := d.Lijevo + 12
		naziv := p.Naziv
		if p.Podstavka() {
			x += 10
			d.Tekst(x-7, d.Y, sitno, false, "–")
			naziv = p.NazivBezCrtice()
		} else {
			d.TekstDesno(d.Lijevo+9, d.Y, sitno, false, fmt.Sprintf("%d.", brojevi[i]))
		}
		if p.Email != "" {
			d.Tekst(mailX, d.Y, sitno, false, p.Email)
		}
		d.Y -= sitno
		d.OdlomakU(x, mailX-x-6, naziv, sitno, false, pdfw.Lijevo)
	}

	// poveznice
	if retci := a.Retci(); len(retci) > 0 {
		d.Razmak(8)
		for _, r := range retci {
			d.Osiguraj(9)
			d.Y += sitno
			d.Tekst(d.Lijevo, d.Y, sitno, false, r[0])
			d.Y -= sitno
			d.OdlomakU(d.Lijevo+d.Sirina()*0.36, d.Sirina()*0.64, r[1], sitno, false, pdfw.Lijevo)
		}
	}

	// ovjera i provjera, sitnim slovima
	d.Razmak(10)
	d.Osiguraj(34)
	d.Crta(d.Lijevo, d.Y, d.W-d.Desno, d.Y)
	d.Razmak(3)
	if nacin == nacinIspis {
		d.Odlomak(tekstZaIspis(a, t, sek), 6.3, false, pdfw.Lijevo)
	} else if a.Ovjeren() && a.OvjerenoAt != nil {
		d.Odlomak(tekstOvjere(a, t, sek), 6.3, false, pdfw.Lijevo)
	} else {
		d.Odlomak(fmt.Sprintf("Nacrt sastavio: %s, %s. Vrijedi tek nakon ovjere u goCOP-u.", a.Izradio, a.IzradenoAt.In(models.Zagreb).Format("02.01.2006. 15:04")), 6.5, false, pdfw.Lijevo)
	}
	return d.Bajtovi()
}

// zaglavlje crta znak organizacije s odjelom lijevo, telefone desno i ispod
// blok centra obrane od poplava, kao na dosadašnjim aktima
func zaglavlje(d *pdfw.Doc, t models.OrgTerms, sek *models.Sector) {
	top := d.Y
	x := d.Lijevo
	if t.HasLogo() && t.LogoMime == "image/png" {
		if err := d.SlikaPNG(t.Logo, d.Lijevo, top, 44, 51); err == nil {
			x += 52
		}
	}
	org := strings.ToUpper(t.OrgName)
	if org == "" {
		org = "HRVATSKE VODE"
	}
	d.Tekst(x, top+14, 15, true, org)
	y := top + 14
	odjel, mjesto := odjelIMjesto(sek)
	for _, redak := range odjel {
		y += 9.5
		d.Tekst(x, y, 8, true, redak)
	}
	if mjesto != "" {
		y += 9.5
		d.Tekst(x, y, 8, false, mjesto)
	}
	if sek != nil && sek.Phone != "" {
		d.TekstDesno(d.W-d.Desno, top+40, 8, false, "Telefon: "+sek.Phone)
	}
	d.Y = max(y, top+51) + 14

	if sek != nil {
		d.Tekst(d.Lijevo, d.Y, 8.5, true, "Centar obrane od poplava Sektora "+sek.ID)
		if a := adresaHR(sek.Address); a != "" {
			d.Y += 10
			d.Tekst(d.Lijevo, d.Y, 7.5, false, a)
		}
		if sek.Phone != "" {
			d.Y += 9
			d.Tekst(d.Lijevo, d.Y, 7.5, false, "Telefon:  "+sek.Phone)
		}
		if sek.Email != "" {
			d.Y += 9
			d.Tekst(d.Lijevo, d.Y, 7.5, false, "e-mail:  "+sek.Email)
		}
	}
	d.Y += 16
}

// odjelIMjesto slaže naziv odjela u dva retka velikim slovima i mjesto s
// adresom: "VGO za Dunav i donju Dravu, Osijek" postaje "VODNOGOSPODARSKI
// ODJEL" / "ZA DUNAV I DONJU DRAVU"
func odjelIMjesto(sek *models.Sector) ([]string, string) {
	if sek == nil || sek.VgoName == "" {
		return nil, ""
	}
	naziv := sek.VgoName
	if i := strings.LastIndex(naziv, ","); i > 0 {
		naziv = naziv[:i]
	}
	naziv = strings.TrimSpace(naziv)
	for _, pre := range []string{"VGO ", "Vgo "} {
		if strings.HasPrefix(naziv, pre) {
			naziv = "Vodnogospodarski odjel " + strings.TrimPrefix(naziv, pre)
		}
	}
	gore := strings.ToUpper(naziv)
	var retci []string
	if i := strings.Index(gore, " ZA "); i > 0 {
		retci = []string{gore[:i], gore[i+1:]}
	} else {
		retci = []string{gore}
	}
	adresa := adresaHR(sek.Address)
	mjesto := adresa
	if i := strings.Index(adresa, ","); i > 0 {
		// grad velikim slovima, ulica kako je upisana
		mjesto = strings.ToUpper(adresa[:i]) + adresa[i:]
	}
	return retci, mjesto
}

// adresaHR okreće "Splavarska 2a, 31000 Osijek" u "31000 Osijek, Splavarska 2a"
func adresaHR(a string) string {
	a = strings.TrimSpace(a)
	dijelovi := strings.SplitN(a, ",", 2)
	if len(dijelovi) == 2 {
		drugi := strings.TrimSpace(dijelovi[1])
		if len(drugi) > 5 && drugi[0] >= '0' && drugi[0] <= '9' {
			return drugi + ", " + strings.TrimSpace(dijelovi[0])
		}
	}
	return a
}

var (
	plava        = pdfw.Boja{R: 0.09, G: 0.24, B: 0.45}
	crvena       = pdfw.Boja{R: 0.75, G: 0.12, B: 0.12}
	crna         = pdfw.Boja{R: 0.05, G: 0.05, B: 0.05}
	zelena       = pdfw.Boja{R: 0.11, G: 0.50, B: 0.23}
	blijeda      = pdfw.Boja{R: 0.94, G: 0.97, B: 0.94}
	sivaTekst    = pdfw.Boja{R: 0.30, G: 0.33, B: 0.36}
	sivaSvijetla = pdfw.Boja{R: 0.50, G: 0.53, B: 0.56}
	sivaRub      = pdfw.Boja{R: 0.78, G: 0.80, B: 0.82}
	bijela       = pdfw.Boja{R: 1, G: 1, B: 1}
)

// blokPotpisa crta okvir elektroničkog potpisa, kakav čitači PDF-a
// prikazuju uz potpisan dokument: kvačica, tko je potpisao, kada, akt i
// otisak ključa čvora
func blokPotpisa(d *pdfw.Doc, a *models.Akt, x, w float64) {
	kad := a.OvjerenoAt.In(models.Zagreb)
	cvor := "čvor " + a.Cvor
	if a.KljucCvora != "" {
		cvor += " · ključ " + a.OtisakKljuca()
	}
	blokOvjere(d, x, w, "ELEKTRONIČKI OVJERENO U goCOP-u", a.ImePotpisa(),
		kad.Format("02.01.2006. u 15:04")+" "+kad.Format("MST")+" · akt "+a.Oznaka()+" · kod "+a.OvjeraKod, cvor)
}

// blokOvjere je blok elektroničkog potpisa kakav nose akti i dnevnici: tih i
// uredan, bez ispune, tanak sivi rub, mali lokot, tekst u sivim tonovima; ime
// je jedino istaknuto
func blokOvjere(d *pdfw.Doc, x, w float64, naslov, ime, redak, sitno string) {
	blokOvjereBoja(d, x, w, naslov, ime, redak, sitno, sivaTekst, plava)
}

// blokOvjereBoja je blokOvjere sa zadanim bojama naslova i imena (i lokota)
func blokOvjereBoja(d *pdfw.Doc, x, w float64, naslov, ime, redak, sitno string, naslovBoja, imeBoja pdfw.Boja) {
	const h = 46.0
	y := d.Y
	d.Okvir(x, y, w, h, bijela, sivaRub)
	lokot(d, x+9, y+12, 13, imeBoja)
	tx := x + 29
	d.TekstBoja(tx, y+10, 5.6, false, naslov, naslovBoja)
	d.TekstBoja(tx, y+21, 8.5, true, ime, imeBoja)
	d.TekstBoja(tx, y+30, 6, false, redak, sivaTekst)
	d.TekstBoja(tx, y+39, 5.6, false, sitno, sivaSvijetla)
	d.Y = y + h
}

// tekstOvjere je sitni tekst na dnu ovjerenog akta: kako je ovjeren i kako
// se ispravnost ispisa provjerava
func tekstOvjere(a *models.Akt, t models.OrgTerms, sek *models.Sector) string {
	org := t.OrgName
	if org == "" {
		org = "Hrvatske vode"
	}
	b := fmt.Sprintf("Akt je elektronički ovjeren u informacijskom sustavu obrane od poplava goCOP (%s): ovjerio %s, %s. ",
		org, a.ImePotpisa(), a.OvjerenoAt.In(models.Zagreb).Format("02.01.2006. u 15:04"))
	if a.Potpis != "" {
		b += fmt.Sprintf("Sadržaj akta potpisan je ključem čvora %s (Ed25519, otisak ključa %s); svaka izmjena teksta nakon ovjere poništava potpis. ", a.Cvor, a.OtisakKljuca())
	}
	b += fmt.Sprintf("Ispravnost ispisa provjerava se u goCOP-u (Dokumentacija › Rješenja i obavijesti) upisom koda %s", a.OvjeraKod)
	if sek != nil {
		b += " ili upitom Centru obrane od poplava Sektora " + sek.ID
		var k []string
		if sek.Phone != "" {
			k = append(k, "tel. "+sek.Phone)
		}
		if sek.Email != "" {
			k = append(k, "e-pošta "+sek.Email)
		}
		if len(k) > 0 {
			b += " (" + strings.Join(k, ", ") + ")"
		}
		b += ", uz navod oznake akta " + a.Oznaka()
	}
	return b + ". Na ispisu akt se ovjerava i žigom i vlastoručnim potpisom."
}

// tekstZaIspis je sitni tekst na dnu akta koji se potpisuje vlastoručno
func tekstZaIspis(a *models.Akt, t models.OrgTerms, sek *models.Sector) string {
	org := t.OrgName
	if org == "" {
		org = "Hrvatske vode"
	}
	b := fmt.Sprintf("Akt je sastavljen u informacijskom sustavu obrane od poplava goCOP (%s). Izvornik je ispis s vlastoručnim potpisom i žigom.", org)
	if sek != nil {
		b += " Vjerodostojnost se može provjeriti upitom Centru obrane od poplava Sektora " + sek.ID
		var k []string
		if sek.Phone != "" {
			k = append(k, "tel. "+sek.Phone)
		}
		if sek.Email != "" {
			k = append(k, "e-pošta "+sek.Email)
		}
		if len(k) > 0 {
			b += " (" + strings.Join(k, ", ") + ")"
		}
		b += "."
	}
	return b + " Evidencijski broj u goCOP-u: " + strings.ToUpper(strings.ReplaceAll(a.ID, "-", "")[:12]) + "."
}

// slika crta PNG ili JPEG na zadano mjesto; drugu vrstu preskače
func slika(d *pdfw.Doc, mime string, podaci []byte, x, y, w, h float64) {
	switch mime {
	case "image/png":
		_ = d.SlikaPNG(podaci, x, y, w, h)
	case "image/jpeg":
		_ = d.SlikaJPEG(podaci, x, y, w, h)
	}
}

// lokot crta mali lokot: tijelo kao ispunjen pravokutnik, luk kao niz kratkih
// crta, i ključanicu; w je širina tijela
func lokot(d *pdfw.Doc, x, y, w float64, c pdfw.Boja) {
	tijeloH := w * 0.78
	lukR := w * 0.3
	lukY := y + lukR + 1 // središte luka
	tijeloY := lukY + 1
	// luk: polukrug od lijeve do desne strane, u 10 koraka
	cx := x + w/2
	prev := [2]float64{cx - lukR, lukY}
	for i := 1; i <= 10; i++ {
		t := math.Pi - math.Pi*float64(i)/10
		p := [2]float64{cx + lukR*math.Cos(t), lukY - lukR*math.Sin(t)}
		d.CrtaBoja(prev[0], prev[1], p[0], p[1], 1.3, c)
		prev = p
	}
	d.CrtaBoja(cx-lukR, lukY, cx-lukR, tijeloY, 1.3, c)
	d.CrtaBoja(cx+lukR, lukY, cx+lukR, tijeloY, 1.3, c)
	d.Okvir(x, tijeloY, w, tijeloH, c, c)
	// ključanica: bijela točka i kratka crta
	d.CrtaBoja(cx, tijeloY+tijeloH*0.35, cx, tijeloY+tijeloH*0.7, 1.6, bijela)
}
