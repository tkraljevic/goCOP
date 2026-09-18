package web

import (
	"fmt"
	"strings"

	"gocop/internal/models"
	"gocop/internal/pdfw"
)

// PDFAkta crta akt onako kako COP-ovi akti izgledaju: zaglavlje odjela i
// centra, pravna osnova s vodostajem, naslov, područje i dionice, dan i sat,
// standardna rečenica, potpisnik i primatelji. Nacrt nosi vidljivu oznaku da
// nije ovjeren; ovjeren nosi tko ga je ovjerio, kad i kod za provjeru.
func PDFAkta(a *models.Akt, t models.OrgTerms, sek *models.Sector, area *models.Area) []byte {
	d := pdfw.Novi(a.Naslov()+" "+a.Oznaka(), "goCOP")
	d.Podnozje = func(d *pdfw.Doc, stranica, ukupno int) {
		d.Crta(d.Lijevo, d.H-d.Dolje+10, d.W-d.Desno, d.H-d.Dolje+10)
		d.Tekst(d.Lijevo, d.H-d.Dolje+22, 8, false, a.Naslov()+" · "+a.Oznaka())
		d.TekstDesno(d.W-d.Desno, d.H-d.Dolje+22, 8, false, fmt.Sprintf("stranica %d od %d", stranica, ukupno))
	}

	// zaglavlje: znak i organizacija lijevo, centar desno
	x := d.Lijevo
	if t.HasLogo() && t.LogoMime == "image/png" {
		if err := d.SlikaPNG(t.Logo, d.Lijevo, d.Y, 40, 40); err == nil {
			x += 48
		}
	}
	y := d.Y + 11
	org := strings.ToUpper(t.OrgName)
	if org == "" {
		org = "HRVATSKE VODE"
	}
	d.Tekst(x, y, 11, true, org)
	if sek != nil {
		y += 13
		d.Tekst(x, y, 9, true, strings.ToUpper(sek.VgoName))
		if sek.Address != "" {
			y += 11
			d.Tekst(x, y, 8, false, sek.Address)
		}
		if sek.Phone != "" {
			y += 11
			d.Tekst(x, y, 8, false, "Telefon: "+sek.Phone)
		}
		// centar desno
		dy := d.Y + 11
		d.TekstDesno(d.W-d.Desno, dy, 9, true, "Centar obrane od poplava Sektora "+sek.ID)
		if sek.CenterCop != "" {
			dy += 11
			d.TekstDesno(d.W-d.Desno, dy, 8, false, sek.CenterCop)
		}
		if sek.Email != "" {
			dy += 11
			d.TekstDesno(d.W-d.Desno, dy, 8, false, sek.Email)
		}
	}
	d.Y = max(y, d.Y+48) + 10
	d.Crta(d.Lijevo, d.Y, d.W-d.Desno, d.Y)
	d.Razmak(18)

	if !a.Ovjeren() {
		d.Odlomak("NACRT — nije ovjeren", 11, true, pdfw.Desno)
		d.Razmak(4)
	}

	// pravna osnova
	d.Odlomak("Na temelju Zakona o vodama, članak 130. (N.N. br. 66/19, 84/21 i 47/23) te odredbi članka "+a.Clanak()+
		" Državnog plana obrane od poplava (N.N. br. 84/10) i Glavnog provedbenog plana obrane od poplava (Hrvatske vode), "+
		a.Osnova()+", donosim", 10, false, pdfw.Lijevo)
	d.Razmak(16)

	// naslov
	d.Odlomak(a.Vrsta(), 16, true, pdfw.Sredina)
	d.Razmak(2)
	d.Odlomak("o "+a.RadnjaLabel(), 11, false, pdfw.Sredina)
	d.Odlomak(a.Predmet(), 12, true, pdfw.Sredina)
	d.Razmak(8)
	podrucje := fmt.Sprintf("na branjenom području %d", a.AreaID)
	if area != nil && area.Name != "" {
		podrucje += ": " + area.Name
	}
	d.Odlomak(podrucje, 11, false, pdfw.Sredina)
	d.Razmak(8)
	if len(a.Dionice) == 1 {
		d.Odlomak("na dionici:", 10, false, pdfw.Sredina)
	} else {
		d.Odlomak("na dionicama:", 10, false, pdfw.Sredina)
	}
	d.Razmak(4)
	for _, dn := range a.Dionice {
		d.Osiguraj(26)
		d.Y += 10
		d.Tekst(d.Lijevo+40, d.Y, 10, true, dn.Code)
		d.Y -= 10
		d.OdlomakU(d.Lijevo+110, d.Sirina()-110, dn.Opis, 10, false, pdfw.Lijevo)
		d.Razmak(3)
	}
	d.Razmak(8)
	d.Odlomak("dana:", 10, false, pdfw.Sredina)
	d.Odlomak(a.Vrijedi.In(models.Zagreb).Format("02.01.2006.")+"   u   "+a.Vrijedi.In(models.Zagreb).Format("15:04")+"  sati", 12, true, pdfw.Sredina)
	d.Razmak(12)
	d.Odlomak("Za vrijeme provođenja mjera obrane od poplava treba postupiti prema odredbama Državnog plana obrane od poplava (N.N. br. 84/10) i Glavnog provedbenog plana obrane od poplava (Hrvatske vode)!", 10, false, pdfw.Lijevo)
	if a.Napomena != "" {
		d.Razmak(6)
		d.Odlomak(a.Napomena, 10, false, pdfw.Lijevo)
	}
	d.Razmak(18)

	// potpisnik desno, "O tome obavijest" lijevo
	d.Osiguraj(60)
	y0 := d.Y
	d.Tekst(d.Lijevo, y0+10, 10, false, "O tome obavijest:")
	d.OdlomakU(d.W-d.Desno-220, 220, a.Potpisnik, 10, true, pdfw.Sredina)
	d.Razmak(28)
	d.Crta(d.W-d.Desno-200, d.Y, d.W-d.Desno-20, d.Y)
	if a.Ovjeren() {
		d.Razmak(3)
		d.OdlomakU(d.W-d.Desno-220, 220, a.Ovjerio, 9, false, pdfw.Sredina)
	}
	d.Razmak(10)

	for i, p := range a.Primatelji {
		d.Osiguraj(14)
		d.Y += 10
		d.Tekst(d.Lijevo, d.Y, 9, false, fmt.Sprintf("%d.", i+1))
		d.Y -= 10
		tekst := p.Naziv
		if p.Email != "" {
			tekst += "   " + p.Email
		}
		d.OdlomakU(d.Lijevo+24, d.Sirina()-24, tekst, 9, false, pdfw.Lijevo)
	}

	d.Razmak(16)
	d.Osiguraj(40)
	d.Crta(d.Lijevo, d.Y, d.W-d.Desno, d.Y)
	d.Razmak(4)
	if a.Ovjeren() && a.OvjerenoAt != nil {
		d.Odlomak(fmt.Sprintf("Ovjereno u goCOP-u: %s, %s · akt %s · kod %s", a.Ovjerio, a.OvjerenoAt.In(models.Zagreb).Format("02.01.2006. 15:04"), a.Oznaka(), a.OvjeraKod), 8, false, pdfw.Lijevo)
		d.Odlomak("Kod je sažetak sadržaja i ovjere; isti kod stoji uz akt u programu, pa se ispis može provjeriti.", 8, false, pdfw.Lijevo)
	} else {
		d.Odlomak(fmt.Sprintf("Nacrt sastavio: %s, %s. Vrijedi tek nakon ovjere u goCOP-u.", a.Izradio, a.IzradenoAt.In(models.Zagreb).Format("02.01.2006. 15:04")), 8, false, pdfw.Lijevo)
	}
	return d.Bajtovi()
}
