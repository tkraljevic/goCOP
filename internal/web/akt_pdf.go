package web

import (
	"fmt"
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
	d := pdfw.Novi(a.Naslov()+" "+a.Oznaka(), "goCOP")
	d.Gore, d.Lijevo, d.Desno = 36, 50, 50
	d.Y = d.Gore
	d.Podnozje = func(d *pdfw.Doc, stranica, ukupno int) {
		d.Tekst(d.Lijevo, d.H-d.Dolje+30, 6.5, false, a.Naslov()+" · "+a.Oznaka())
		d.TekstDesno(d.W-d.Desno, d.H-d.Dolje+30, 6.5, false, fmt.Sprintf("stranica %d od %d", stranica, ukupno))
	}

	zaglavlje(d, t, sek)

	if !a.Ovjeren() {
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
	if a.Ovjeren() && a.OvjerenoAt != nil {
		d.Razmak(4)
		blokPotpisa(d, a, potpisX+5, potpisW-10)
		d.Razmak(16)
	} else {
		d.Razmak(24)
	}
	d.Crta(potpisX+15, d.Y, potpisX+potpisW-15, d.Y)
	if a.Ovjeren() {
		d.Razmak(2)
		d.OdlomakU(potpisX, potpisW, a.ImePotpisa(), 8.5, false, pdfw.Sredina)
	}
	d.Razmak(8)

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
	if a.Ovjeren() && a.OvjerenoAt != nil {
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
	plava     = pdfw.Boja{R: 0.09, G: 0.24, B: 0.45}
	zelena    = pdfw.Boja{R: 0.11, G: 0.50, B: 0.23}
	blijeda   = pdfw.Boja{R: 0.94, G: 0.97, B: 0.94}
	sivaTekst = pdfw.Boja{R: 0.30, G: 0.33, B: 0.36}
)

// blokPotpisa crta okvir elektroničkog potpisa, kakav čitači PDF-a
// prikazuju uz potpisan dokument: kvačica, tko je potpisao, kada, akt i
// otisak ključa čvora
func blokPotpisa(d *pdfw.Doc, a *models.Akt, x, w float64) {
	const h = 58.0
	y := d.Y
	d.Okvir(x, y, w, h, blijeda, zelena)
	// kvačica
	d.CrtaBoja(x+9, y+24, x+15, y+31, 2.2, zelena)
	d.CrtaBoja(x+15, y+31, x+27, y+15, 2.2, zelena)
	tx := x + 36
	d.TekstBoja(tx, y+11, 7, true, "ELEKTRONIČKI POTPISANO", zelena)
	d.TekstBoja(tx, y+22, 8.5, true, a.ImePotpisa(), plava)
	kad := a.OvjerenoAt.In(models.Zagreb)
	d.TekstBoja(tx, y+32, 6.5, false, "Datum: "+kad.Format("02.01.2006. u 15:04:05")+" "+kad.Format("MST"), sivaTekst)
	d.TekstBoja(tx, y+41, 6.5, false, "Akt "+a.Oznaka()+" · kod "+a.OvjeraKod, sivaTekst)
	if a.KljucCvora != "" {
		d.TekstBoja(tx, y+50, 6, false, "goCOP · čvor "+a.Cvor+" · ključ "+a.OtisakKljuca(), sivaTekst)
	} else {
		d.TekstBoja(tx, y+50, 6, false, "goCOP · čvor "+a.Cvor, sivaTekst)
	}
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
