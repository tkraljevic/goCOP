package web

import (
	"fmt"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/pdfw"
)

// PDFVodocuvarskiList crta dnevni list kao papirnatu stranicu knjige:
// datum, radno vrijeme, prilike, tri okvira, potpisi i broj stranice
func PDFVodocuvarskiList(l *models.VodocuvarskiList, t models.OrgTerms, area *models.Area, otisci models.OtisciLista) []byte {
	d := pdfw.Novi("Vodočuvarski dnevnik, dnevni list "+l.Datum.In(models.Zagreb).Format("02.01.2006."), "goCOP")
	d.Predmet = "Vodočuvarski dnevnik: " + l.Ime
	nacrtajList(d, l, t, area, otisci)
	return d.Bajtovi()
}

// PDFVodocuvarskaKnjiga je cijela godišnja knjiga: naslovna stranica pa
// list po stranici, redom brojeva
func PDFVodocuvarskaKnjiga(listovi []models.VodocuvarskiList, ime string, godina int, t models.OrgTerms, area *models.Area, otisci models.OtisciLista) []byte {
	d := pdfw.Novi(fmt.Sprintf("Vodočuvarski dnevnik %d, %s", godina, ime), "goCOP")
	d.Predmet = "Vodočuvarski dnevnik: " + ime
	org := t.OrgName
	if org == "" {
		org = "Hrvatske vode"
	}
	d.Y += 120
	d.TekstSredina(d.W/2, d.Y, 11, false, org)
	d.Y += 40
	d.TekstSredina(d.W/2, d.Y, 22, true, "VODOČUVARSKI DNEVNIK")
	d.Y += 34
	d.TekstSredina(d.W/2, d.Y, 16, false, fmt.Sprintf("%d.", godina))
	d.Y += 60
	d.TekstSredina(d.W/2, d.Y, 13, true, ime)
	if area != nil {
		d.Y += 20
		d.TekstSredina(d.W/2, d.Y, 10, false, area.VgiName+" · branjeno područje "+fmt.Sprint(area.ID)+": "+area.Name)
	}
	d.Y += 60
	d.TekstSredina(d.W/2, d.Y, 9, false, fmt.Sprintf("Listova: %d", len(listovi)))
	d.Y += 14
	if len(listovi) > 0 {
		prvi, zadnji := listovi[len(listovi)-1], listovi[0]
		if prvi.Datum.After(zadnji.Datum) {
			prvi, zadnji = zadnji, prvi
		}
		d.TekstSredina(d.W/2, d.Y, 9, false, prvi.Datum.In(models.Zagreb).Format("02.01.2006.")+" – "+zadnji.Datum.In(models.Zagreb).Format("02.01.2006."))
	}
	d.TekstBoja(d.Lijevo, d.H-d.Dolje, 6.5, false, "Knjiga je zaključena istekom godine i čuva se u goCOP-u; ispis iz programa, listovi nose potpise kako su dani u programu.", sivaTekst)
	// listovi od najstarijeg, po broju
	poredani := append([]models.VodocuvarskiList{}, listovi...)
	for i := 0; i < len(poredani); i++ {
		for j := i + 1; j < len(poredani); j++ {
			if poredani[j].Broj < poredani[i].Broj || (poredani[j].Broj == poredani[i].Broj && poredani[j].Datum.Before(poredani[i].Datum)) {
				poredani[i], poredani[j] = poredani[j], poredani[i]
			}
		}
	}
	for i := range poredani {
		d.NovaStranica()
		nacrtajList(d, &poredani[i], t, area, otisci)
	}
	return d.Bajtovi()
}

// nacrtajList crta jedan dnevni list na tekuću stranicu
func nacrtajList(d *pdfw.Doc, l *models.VodocuvarskiList, t models.OrgTerms, area *models.Area, otisci models.OtisciLista) {
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
	for _, up := range l.Upisi {
		naredbe = append(naredbe, up.Tekst+" (upisao "+up.Ime+ifNe(up.Funkcija)+", "+up.Kad.In(models.Zagreb).Format("02.01. 15:04")+")")
	}
	naredbe = append(naredbe, l.NaredbeStavke()...)
	okvir("Naredbe rukovoditelja:", numerirano(naredbe), 120)
	okvir("Opis radnih aktivnosti:", numerirano(l.OpisStavke()), 210)
	zap := l.ZapazanjaStavke()
	if l.Ocitanja != "" {
		zap = append(zap, models.Stavke(l.Ocitanja)...)
	}
	okvir("Posebna zapažanja:", numerirano(zap), 160)

	// potpisi: lijevo vodočuvar, desno rukovoditelj branjenog područja; tko je
	// potpisao u programu nosi blok elektroničke ovjere kao na aktima, pa
	// skenirani potpis ako ga ima, pa crtu s imenom
	d.Y += 14
	d.Osiguraj(120)
	y := d.Y
	const pw = 215.0
	list := fmt.Sprintf("list %03d/%d", l.Broj, dan.Year())
	potpis := func(x float64, naslov, ime string, kad *time.Time, kod, userID string) {
		d.Y = y
		d.TekstSredina(x+pw/2, d.Y, 9, false, naslov)
		d.Y += 8
		if kad != nil {
			k := kad.In(models.Zagreb)
			blokOvjere(d, x, pw, "ELEKTRONIČKI POTPISANO U goCOP-u", ime,
				k.Format("02.01.2006. u 15:04")+" "+k.Format("MST")+" · "+list+" · kod "+kod, "čvor "+l.Cvor)
			if p := otisci[userID]; p != nil && len(p.Slika) > 0 {
				d.Y += 4
				slika(d, p.Mime, p.Slika, x+pw/2-60, d.Y, 120, 36)
				d.Y += 38
			} else {
				d.Y += 16
			}
		} else {
			d.Y += 50
		}
		d.Crta(x+10, d.Y, x+pw-10, d.Y)
		d.Y += 11
		if kad != nil {
			d.TekstSredina(x+pw/2, d.Y, 9, false, ime)
		}
	}
	potpis(d.Lijevo, "Vodočuvar", l.Ime, l.PredanoAt, l.KodPredaje(), l.UserID)
	kraj := d.Y
	potpis(d.W-d.Desno-pw, "Rukovoditelj branjenog područja", l.Potvrdio, l.PotvrdenoAt, l.KodOvjere(), l.PotvrdioID)
	if d.Y < kraj {
		d.Y = kraj
	}
	d.Y += 10
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
}

func ifNe(f string) string {
	if f == "" {
		return ""
	}
	return ", " + f
}
