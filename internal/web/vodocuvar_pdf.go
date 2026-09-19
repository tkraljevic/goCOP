package web

import (
	"fmt"
	"strings"

	"gocop/internal/models"
	"gocop/internal/pdfw"
)

// PDFVodocuvarskiList crta dnevni list kao papirnatu stranicu knjige:
// datum, radno vrijeme, prilike, tri okvira, potpisi i broj stranice
func PDFVodocuvarskiList(l *models.VodocuvarskiList, t models.OrgTerms, area *models.Area) []byte {
	d := pdfw.Novi("Vodočuvarski dnevnik, dnevni list "+l.Datum.In(models.Zagreb).Format("02.01.2006."), "goCOP")
	d.Predmet = "Vodočuvarski dnevnik: " + l.Ime
	dan := l.Datum.In(models.Zagreb)
	// zaglavlje: organizacija i područje, sitno
	org := t.OrgName
	if org == "" {
		org = "Hrvatske vode"
	}
	glava := org
	if area != nil {
		glava += " · " + area.VgiName + " · BP " + fmt.Sprint(area.ID)
	}
	glava += " · vodočuvar: " + l.Ime
	d.TekstBoja(d.Lijevo, d.Y, 7, false, glava, sivaTekst)
	d.Y += 16
	// Datum 11 . svibnja . utorak
	d.Tekst(d.Lijevo, d.Y, 10, true, "Datum")
	x := d.Lijevo + 40
	d.Tekst(x, d.Y, 11, false, fmt.Sprintf("%d.", dan.Day()))
	d.Tekst(x+30, d.Y, 11, false, l.MjesecGenitiv())
	d.Tekst(x+130, d.Y, 11, false, l.DanUTjednu())
	d.Crta(x, d.Y+3, x+220, d.Y+3)
	d.TekstBoja(x+30, d.Y+11, 6, false, "(dan i mjesec)", sivaTekst)
	d.TekstBoja(x+130, d.Y+11, 6, false, "(dan u tjednu)", sivaTekst)
	d.Y += 30
	d.TekstSredina(d.W/2, d.Y, 13, true, "DNEVNI LIST")
	d.Y += 18

	sirina := d.W - d.Lijevo - d.Desno
	// redak radnog vremena i prilika
	okvirY := d.Y
	d.Okvir(d.Lijevo, okvirY, sirina, 44, bijela, sivaTekst)
	d.Tekst(d.Lijevo+8, okvirY+14, 9, true, "Početak i svršetak rada: od")
	d.Tekst(d.Lijevo+160, okvirY+14, 11, false, l.Od)
	d.Tekst(d.Lijevo+215, okvirY+14, 9, true, "do")
	d.Tekst(d.Lijevo+240, okvirY+14, 11, false, l.Do)
	d.Tekst(d.Lijevo+340, okvirY+14, 9, true, "UKUPNO")
	d.Tekst(d.Lijevo+395, okvirY+14, 11, false, l.SatiTekst())
	d.Tekst(d.Lijevo+425, okvirY+14, 9, true, "sati")
	d.Crta(d.Lijevo, okvirY+22, d.Lijevo+sirina, okvirY+22)
	d.Tekst(d.Lijevo+8, okvirY+36, 9, true, "Vremenske prilike:")
	d.Tekst(d.Lijevo+105, okvirY+36, 10, false, l.Prilike)
	d.Y = okvirY + 44

	okvir := func(naslov, tekst string, visina float64) {
		y := d.Y
		d.Okvir(d.Lijevo, y, sirina, visina, bijela, sivaTekst)
		d.Tekst(d.Lijevo+8, y+14, 9, true, naslov)
		d.Y = y + 22
		if tekst != "" {
			d.OdlomakU(d.Lijevo+10, sirina-20, tekst, 10, false, pdfw.Lijevo)
		}
		if d.Y < y+visina {
			d.Y = y + visina
		}
	}
	numerirano := func(stavke []string) string {
		var b strings.Builder
		for i, st := range stavke {
			fmt.Fprintf(&b, "%d. %s\n", i+1, st)
		}
		return strings.TrimSpace(b.String())
	}
	var naredbe []string
	for _, z := range l.Zadaci {
		red := z.Tekst + " (zadao " + z.Zadao + ", " + z.ZadanoAt.In(models.Zagreb).Format("02.01.") + "): " + z.Oznaka()
		if z.Obavljeno != "" {
			red += "; " + z.Obavljeno
		}
		naredbe = append(naredbe, red)
	}
	naredbe = append(naredbe, l.NaredbeStavke()...)
	okvir("Naredbe rukovoditelja:", numerirano(naredbe), 120)
	okvir("Opis radnih aktivnosti:", numerirano(l.OpisStavke()), 210)
	zap := l.ZapazanjaStavke()
	if l.Ocitanja != "" {
		zap = append(zap, models.Stavke(l.Ocitanja)...)
	}
	okvir("Posebna zapažanja:", numerirano(zap), 160)

	// potpisi
	d.Y += 14
	d.Osiguraj(60)
	y := d.Y
	d.Tekst(d.Lijevo, y, 9, false, "Potpis vodočuvara:")
	d.Crta(d.Lijevo, y+30, d.Lijevo+170, y+30)
	if l.Predan() {
		d.Tekst(d.Lijevo+4, y+26, 9, true, l.Ime)
		d.TekstBoja(d.Lijevo, y+40, 6.5, false, "predano u goCOP-u "+l.PredanoAt.In(models.Zagreb).Format("02.01.2006. 15:04"), sivaTekst)
	}
	dx := d.W - d.Desno - 200
	d.Tekst(dx, y, 9, false, "Potpis rukovoditelja VGI:")
	d.Crta(dx, y+30, dx+200, y+30)
	if l.Potvrden() {
		d.Tekst(dx+4, y+26, 9, true, l.Potvrdio)
		d.TekstBoja(dx, y+40, 6.5, false, "ovjereno u goCOP-u "+l.PotvrdenoAt.In(models.Zagreb).Format("02.01.2006. 15:04"), sivaTekst)
	}
	d.Y = y + 50
	if len(l.Parafe) > 0 {
		var p []string
		for _, x := range l.Parafe {
			p = append(p, x.Ime+" ("+x.Kad.In(models.Zagreb).Format("02.01.")+")")
		}
		d.TekstBoja(d.Lijevo, d.Y, 6.5, false, "Parafirali: "+strings.Join(p, ", "), sivaTekst)
		d.Y += 12
	}
	if l.Broj > 0 {
		d.TekstDesno(d.W-d.Desno, d.H-d.Dolje+20, 9, true, fmt.Sprintf("%03d", l.Broj))
	}
	return d.Bajtovi()
}
