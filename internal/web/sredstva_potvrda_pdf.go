package web

import (
	"strconv"
	"strings"

	"gocop/internal/models"
	"gocop/internal/pdfw"
)

// sivaZaglavlja je pozadina zaglavlja tablice stavki
var sivaZaglavlja = pdfw.Boja{R: 0.94, G: 0.95, B: 0.96}

// pdfPotvrde crta potvrdu zahvata za ispis i vlastoručne potpise, istog
// sadržaja kao Excel: memorandum kao na aktima, naslov s brojem, polja,
// tablica stavki, tko je upisao, pa mjesto, dan i crte za potpis
func pdfPotvrde(p *potvrdaZahvata, t models.OrgTerms) []byte {
	prvi := p.redci[0]
	naslov := nazivPotvrde(prvi.Vrsta)
	d := pdfw.Novi(naslov+" br. "+brojPotvrde(prvi), "goCOP")
	d.Predmet = models.PrometNaziv(prvi.Vrsta) + " · " + prvi.Datum.Format("02.01.2006.")
	zaglavlje(d, t, p.sek, false)
	d.Y += 8
	d.TekstSredina(d.W/2, d.Y, 15, true, strings.ToUpper(naslov)+" br. "+brojPotvrde(prvi))
	d.Y += 16
	d.TekstSredina(d.W/2, d.Y, 9, false, d.Predmet)
	d.Y += 22
	poljaPDF(d, poljaPotvrde(p.redci, p.sk, p.obrana))
	d.Y += 16
	stavkePDF(d, stavkePotvrde(p.redci))
	d.Y += 14
	d.TekstBoja(d.Lijevo, d.Y, 7, false, upisaoPotvrde(prvi), sivaTekst)
	d.Y += 28
	mjesto := prvi.Datum.Format("02.01.2006.")
	if p.z.Mjesto != "" {
		mjesto = p.z.Mjesto + ", " + mjesto
	}
	potpisiPDF(d, mjesto, potpisniciPotvrde(prvi))
	d.TekstBoja(d.Lijevo, d.H-d.Dolje, 6.5, false, "Potvrda je složena u goCOP-u i vrijedi s vlastoručnim potpisima. Oznaka zahvata u programu: "+strings.ToUpper(prvi.VezaID[:8])+".", sivaTekst)
	return d.Bajtovi()
}

// poljaPDF crta polja potvrde: oznaka lijevo, tekst desno, tanka crta ispod
func poljaPDF(d *pdfw.Doc, polja [][2]string) {
	const oznakaW = 120.0
	for _, p := range polja {
		d.Osiguraj(24)
		d.TekstBoja(d.Lijevo, d.Y+9, 8, true, p[0], sivaTekst)
		d.OdlomakU(d.Lijevo+oznakaW, d.Sirina()-oznakaW, p[1], 9, false, pdfw.Lijevo)
		d.Y += 4
		d.CrtaBoja(d.Lijevo, d.Y, d.W-d.Desno, d.Y, 0.5, sivaRub)
		d.Y += 5
	}
}

// stavkePDF crta tablicu stavki: redni broj, vrsta, oblik, jedinica i
// količina desno
func stavkePDF(d *pdfw.Doc, stavke []models.Promet) {
	const visina = 18.0
	x := d.Lijevo
	desno := d.W - d.Desno
	oblikX, jedX := desno-210, desno-110
	redak := func(rb, vrsta, oblik, jed, kolicina string, bold bool) {
		d.TekstSredina(x+14, d.Y+12, 8.5, bold, rb)
		d.Tekst(x+34, d.Y+12, 9, bold, vrsta)
		d.Tekst(oblikX, d.Y+12, 8.5, bold, oblik)
		d.Tekst(jedX, d.Y+12, 8.5, bold, jed)
		d.TekstDesno(desno-6, d.Y+12, 9, bold, kolicina)
		d.Y += visina
		d.CrtaBoja(x, d.Y, desno, d.Y, 0.5, sivaRub)
	}
	d.Osiguraj(visina * 2)
	d.Ispuna(x, d.Y, desno-x, visina, sivaZaglavlja)
	redak("R. br.", "Vrsta sredstva", "Oblik", "Jed.", "Količina", true)
	for i, s := range stavke {
		d.Osiguraj(visina)
		redak(strconv.Itoa(i+1)+".", s.VrstaNaziv, oblikStavke(s), s.Jedinica, kolicinaHR(absKolicina(s.Kolicina)), false)
	}
}

// potpisiPDF crta mjesto i dan, pa crte za potpis u stupcima: funkcija ispod
// crte, ime ispod funkcije
func potpisiPDF(d *pdfw.Doc, mjesto string, potpisnici []PotpisnikIzvoza) {
	d.Osiguraj(90)
	d.Tekst(d.Lijevo, d.Y, 9, false, mjesto)
	d.Y += 44
	stupac := d.Sirina() / float64(len(potpisnici))
	for i, p := range potpisnici {
		x := d.Lijevo + float64(i)*stupac
		d.Crta(x, d.Y, x+stupac-18, d.Y)
		d.TekstBoja(x, d.Y+10, 7, false, p.Funkcija, sivaTekst)
		d.Tekst(x, d.Y+21, 9, true, p.Ime)
	}
	d.Y += 28
}
