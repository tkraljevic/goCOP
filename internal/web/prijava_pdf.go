package web

import (
	"fmt"
	"strings"

	"gocop/internal/models"
	"gocop/internal/pdfw"
)

// pdfPrijave crta prijavu s terena kao dokument: zaglavlje, vrsta i naslov,
// podaci o mjestu i vremenu, opis, fotografije, i mjesto potpisa vodočuvara.
// Slike su smanjene JPEG-ovi po oznaci; koje nedostaju, preskaču se uz
// napomenu. Vraća PDF i mjesto bloka potpisa za polje potpisa.
func pdfPrijave(p *models.PrijavaSTerena, slike map[string][]byte, t models.OrgTerms, area *models.Area, otisci models.OtisciLista, crtajBlok bool) ([]byte, mjestaPotpisa) {
	d := pdfw.Novi(p.VrstaLabel()+" s terena "+p.Oznaka()+": "+p.Naslov, "goCOP")
	d.Predmet = "Prijava s terena: " + p.Ime
	d.SviZnakovi()
	org := t.OrgName
	if org == "" {
		org = "Hrvatske vode"
	}
	glava := org
	if area != nil {
		glava += " · " + area.VgiName + " · BP " + fmt.Sprint(area.ID)
	}
	glava += " · vodočuvar: " + p.Ime
	d.TekstBoja(d.Lijevo, d.Y, 7, false, glava, sivaTekst)
	d.Y += 22
	d.TekstBoja(d.Lijevo, d.Y, 9, false, strings.ToUpper(p.VrstaLabel())+" S TERENA "+p.Oznaka(), sivaTekst)
	d.Y += 8
	d.OdlomakU(d.Lijevo, d.Sirina(), p.Naslov, 15, true, pdfw.Lijevo)
	d.Y += 6
	d.Crta(d.Lijevo, d.Y, d.W-d.Desno, d.Y)
	d.Y += 12

	redak := func(oznaka, vrijednost string) {
		if vrijednost == "" {
			return
		}
		d.Osiguraj(14)
		d.Y += 9
		d.TekstBoja(d.Lijevo, d.Y, 8, false, oznaka, sivaTekst)
		d.Tekst(d.Lijevo+110, d.Y, 9, false, vrijednost)
		d.Y += 5
	}
	redak("Dan događaja:", p.Datum.In(models.Zagreb).Format("02.01.2006."))
	redak("Mjesto:", p.Mjesto())
	if p.ImaKoordinate() {
		redak("Koordinate:", fmt.Sprintf("%.6f, %.6f (WGS84)", *p.Latitude, *p.Longitude))
	}
	if area != nil {
		redak("Branjeno područje:", fmt.Sprintf("%d, %s", area.ID, area.Name))
	}
	if p.ObjavljenoAt != nil {
		redak("Objavljeno:", p.ObjavljenoAt.In(models.Zagreb).Format("02.01.2006. u 15:04"))
	}
	d.Y += 12
	d.Tekst(d.Lijevo, d.Y, 9, true, "Opis")
	d.Y += 6
	d.Odlomak(p.Opis, 10, false, pdfw.Lijevo)

	if len(p.Slike) > 0 {
		d.Y += 14
		d.Osiguraj(40)
		d.Tekst(d.Lijevo, d.Y, 9, true, fmt.Sprintf("Fotografije (%d)", len(p.Slike)))
		d.Y += 8
		w := d.Sirina()
		for i, sl := range p.Slike {
			b := slike[sl.ID]
			if len(b) == 0 || sl.Sirina == 0 || sl.Visina == 0 {
				d.Osiguraj(14)
				d.Y += 9
				d.TekstBoja(d.Lijevo, d.Y, 8, false, fmt.Sprintf("%d. fotografija nije dostupna na ovom čvoru", i+1), sivaTekst)
				d.Y += 5
				continue
			}
			sw, sh := w, w*float64(sl.Visina)/float64(sl.Sirina)
			if sh > 360 {
				sh = 360
				sw = sh * float64(sl.Sirina) / float64(sl.Visina)
			}
			d.Osiguraj(sh + 22)
			_ = d.SlikaJPEG(b, d.Lijevo+(w-sw)/2, d.Y, sw, sh)
			d.Y += sh + 3
			napis := fmt.Sprintf("%d.", i+1)
			if sl.Naziv != "" {
				napis += " " + sl.Naziv
			}
			d.Y += 7
			d.TekstBoja(d.Lijevo, d.Y, 7, false, napis, sivaTekst)
			d.Y += 10
		}
	}

	// potpis vodočuvara, kao na dnevnom listu
	d.Y += 16
	d.Osiguraj(visinaPotpisa + 20)
	y := d.Y
	const pw = 215.0
	m := mjestaPotpisa{stranica: d.Stranica(), y: y + 8, w: pw, h: visinaPotpisa, xVodocuvar: d.Lijevo, xRuk: d.W - d.Desno - pw}
	d.TekstSredina(m.xVodocuvar+pw/2, y, 9, false, "Vodočuvar")
	crtajPotpisLista(d, m.xVodocuvar, m.y, pw, p.Ime, p.ObjavljenoAt, "prijava "+p.Oznaka(), p.Kod(), p.Cvor, otisci[p.UserID], crtajBlok, false)
	if p.ListBroj > 0 {
		d.TekstBoja(m.xRuk, m.y+20, 7.5, false, fmt.Sprintf("Upisano na dnevni list %03d/%d", p.ListBroj, p.Godina), sivaTekst)
	}
	d.Y = m.y + visinaPotpisa + 10
	d.TekstBoja(d.Lijevo, d.H-d.Dolje, 6.5, false, "Dokument je sastavljen u goCOP-u; fotografije su smanjene i ugrađene, pa ostaju s dokumentom trajno.", sivaTekst)
	return d.Bajtovi(), m
}
