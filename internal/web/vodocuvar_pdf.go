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
	pdf, _ := pdfLista(l, t, area, otisci, crtajOba, PrilogListaPDF{})
	return pdf
}

// PrilogListaPDF je ono što uz list ide u dokument: fotografije po oznaci
// priloga i karte ucrtanog obuhvata po oznaci zadatka
type PrilogListaPDF struct {
	Slike   map[string][]byte
	Karte   map[string][]byte
	Zasluge string
}

// PDFListaSPrilozima crta list, a iza njega karte obuhvata i fotografije.
// Prilozi su dio dokumenta, kao i kod prijave: kad se bajtovi jednom otpuste
// sa spremišta, dokument ih i dalje nosi.
func PDFListaSPrilozima(l *models.VodocuvarskiList, t models.OrgTerms, area *models.Area, otisci models.OtisciLista, pr PrilogListaPDF) []byte {
	pdf, _ := pdfLista(l, t, area, otisci, crtajOba, pr)
	return pdf
}

// nacrtajPriloge crta karte obuhvata i fotografije iza lista
func nacrtajPriloge(d *pdfw.Doc, l *models.VodocuvarskiList, pr PrilogListaPDF) {
	for _, z := range l.Zadaci {
		karta := pr.Karte[z.ID]
		slike := l.PriloziZadatka(z.ID)
		if len(karta) == 0 && len(slike) == 0 {
			continue
		}
		d.NovaStranica()
		d.Y = d.Gore + 10
		d.Tekst(d.Lijevo, d.Y, 10, true, "Prilozi uz zadatak")
		d.Y += 12
		for _, redak := range pdfw.Prelomi(z.Tekst, d.Sirina(), 9, false) {
			d.Tekst(d.Lijevo, d.Y, 9, false, redak)
			d.Y += 11
		}
		if uz := z.UzObilazak(); uz != "" {
			d.TekstBoja(d.Lijevo, d.Y, 7.5, false, uz, sivaTekst)
			d.Y += 12
		}
		if len(karta) > 0 {
			w := d.Sirina()
			h := w * float64(visinaKarte) / float64(sirinaKarte)
			d.Osiguraj(h + 28)
			d.Y += 4
			d.Tekst(d.Lijevo, d.Y, 8, true, "Obuhvat obilaska, ucrtan u ranijoj evidenciji")
			d.Y += 10
			if jpg := kartaZaPDF(karta); jpg != nil {
				_ = d.SlikaJPEG(jpg, d.Lijevo, d.Y, w, h)
				d.Y += h + 4
			}
			if pr.Zasluge != "" {
				d.TekstBoja(d.Lijevo, d.Y, 6, false, pr.Zasluge+". Ucrtano rukom u ranijoj evidenciji; nije zapis kretanja.", sivaTekst)
				d.Y += 12
			}
		}
		for i, sl := range slike {
			b := pr.Slike[sl.ID]
			if len(b) == 0 || sl.Sirina == 0 || sl.Visina == 0 {
				continue
			}
			// Slika i njezin opis idu zajedno: prvo se izračuna koliko im
			// treba, pa se prelomi stranica. Inače slika otkliže ispod ruba,
			// a opis ostane na idućoj stranici ili nestane.
			const naslov, ispod = 16.0, 26.0
			w := d.Sirina()
			sw, sh := w, w*float64(sl.Visina)/float64(sl.Sirina)
			hMax := (d.H - d.Gore - d.Dolje) - naslov - ispod
			if pola := (d.H - d.Gore - d.Dolje) / 2; pola < hMax {
				hMax = pola
			}
			if sh > hMax {
				sh = hMax
				sw = sh * float64(sl.Sirina) / float64(sl.Visina)
			}
			d.Osiguraj(naslov + sh + ispod)
			d.Y += 6
			d.Tekst(d.Lijevo, d.Y, 8, true, fmt.Sprintf("Fotografija %d od %d", i+1, len(slike)))
			d.Y += 10
			_ = d.SlikaJPEG(b, d.Lijevo+(w-sw)/2, d.Y, sw, sh)
			d.Y += sh + 9
			d.TekstBoja(d.Lijevo, d.Y, 7.5, false, sl.Podaci(), sivaTekst)
			if z := sl.Izvornik(); z != "" {
				d.Y += 8
				d.TekstBoja(d.Lijevo, d.Y, 6, false, z, sivaTekst)
			}
			d.Y += 6
		}
	}
}

// crtanjeBlokova kaže koji se blokovi potpisa crtaju u sadržaju stranice;
// blok koji se ne crta ostavlja mjesto za polje elektroničkog potpisa
type crtanjeBlokova struct{ vodocuvar, rukovoditelj bool }

var crtajOba = crtanjeBlokova{true, true}

// mjestaPotpisa su mjesta blokova potpisa na stranici, za polja potpisa
type mjestaPotpisa struct {
	stranica         int
	y, w, h          float64
	xVodocuvar, xRuk float64
}

// pdfLista crta list i vraća PDF pripremljen za naknadne potpise, s
// mjestima na kojima blokovi stoje
func pdfLista(l *models.VodocuvarskiList, t models.OrgTerms, area *models.Area, otisci models.OtisciLista, crtaj crtanjeBlokova, pr PrilogListaPDF) ([]byte, mjestaPotpisa) {
	d := pdfw.Novi("Vodočuvarski dnevnik, dnevni list "+l.Datum.In(models.Zagreb).Format("02.01.2006."), "goCOP")
	d.Predmet = "Vodočuvarski dnevnik: " + l.Ime
	d.SviZnakovi()
	m := nacrtajList(d, l, t, area, otisci, crtaj)
	// Prilozi idu u same bajtove lista, i u onaj koji se potpisuje: kad se
	// fotografije jednom otpuste sa spremišta, dokument ih i dalje nosi.
	nacrtajPriloge(d, l, pr)
	return d.Bajtovi(), m
}

// PDFVodocuvarskaKnjiga je cijela godišnja knjiga: naslovna stranica pa
// list po stranici, redom brojeva
func PDFVodocuvarskaKnjiga(listovi []models.VodocuvarskiList, ime string, godina int, t models.OrgTerms, area *models.Area, otisci models.OtisciLista, prilozi map[string][]byte) []byte {
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
	// knjiga nosi fotografije uz listove; karte obuhvata ne, jer se crtaju iz
	// mreže i uvijek se mogu nacrtati iznova iz zapisa zadatka
	pr := PrilogListaPDF{Slike: prilozi}
	for i := range poredani {
		d.NovaStranica()
		nacrtajList(d, &poredani[i], t, area, otisci, crtajOba)
		nacrtajPriloge(d, &poredani[i], pr)
	}
	return d.Bajtovi()
}

// nacrtajList crta jedan dnevni list na tekuću stranicu
func nacrtajList(d *pdfw.Doc, l *models.VodocuvarskiList, t models.OrgTerms, area *models.Area, otisci models.OtisciLista, crtaj crtanjeBlokova) mjestaPotpisa {
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
	if l.Rekonstrukcija {
		// list koji nije vođen u goCOP-u mora se i na prvi pogled razlikovati
		// od onoga koji jest: nosi napomenu, a dolje sivi blok umjesto potpisa
		d.Ispuna(d.Lijevo, d.Y-4, d.W-d.Lijevo-d.Desno, 30, pdfw.Boja{R: 0.95, G: 0.96, B: 0.97})
		y := d.Y + 8
		for _, redak := range pdfw.Prelomi(models.NapomenaPrenesenogLista, d.W-d.Lijevo-d.Desno-16, 7, false) {
			d.TekstBoja(d.Lijevo+8, y, 7, false, redak, sivaTekst)
			y += 9
		}
		d.Y = y + 6
	}

	sirina := d.W - d.Lijevo - d.Desno
	// redak radnog vremena i prilika
	okvirY := d.Y
	d.Okvir(d.Lijevo, okvirY, sirina, 44, bijela, sivaTekst)
	d.Tekst(d.Lijevo+8, okvirY+14, 9, true, "Početak i svršetak rada: od")
	d.Tekst(d.Lijevo+160, okvirY+14, 11, false, l.Od)
	d.Tekst(d.Lijevo+215, okvirY+14, 9, true, "do")
	d.Tekst(d.Lijevo+240, okvirY+14, 11, false, l.Do)
	d.Tekst(d.Lijevo+340, okvirY+14, 9, true, "UKUPNO")
	// prenesen list nema radnog vremena; "0,0 sati" bi izgledalo kao tvrdnja
	// da vodočuvar tog dana nije radio
	if !l.Rekonstrukcija {
		d.Tekst(d.Lijevo+395, okvirY+14, 11, false, l.SatiTekst())
	}
	d.Tekst(d.Lijevo+425, okvirY+14, 9, true, "sati")
	d.Crta(d.Lijevo, okvirY+22, d.Lijevo+sirina, okvirY+22)
	d.Tekst(d.Lijevo+8, okvirY+36, 9, true, "Vremenske prilike:")
	// opis prilika zna biti dug (temperatura, vjetar, tlak, oborine), a redak
	// je jedan: slovo se smanji dok ne stane, pa se tek onda krati
	if l.Prilike != "" {
		stane := sirina - 113
		vel := 10.0
		for vel > 6.5 && pdfw.SirinaTeksta(l.Prilike, vel, false) > stane {
			vel -= 0.5
		}
		tekst := l.Prilike
		for pdfw.SirinaTeksta(tekst, vel, false) > stane && len(tekst) > 4 {
			tekst = tekst[:len(tekst)-2]
		}
		if tekst != l.Prilike {
			tekst = strings.TrimSpace(tekst) + "…"
		}
		d.Tekst(d.Lijevo+105, okvirY+36, vel, false, tekst)
	}
	d.Y = okvirY + 44

	// Prenesen list nema vremena rada, zapažanja ni opisa koje je vodočuvar
	// tipkao, pa bi rubrike pune praznine potisnule potpis na sljedeću
	// stranicu. Zato se tada visina rubrike ravna po sadržaju.
	okvir := func(naslov, tekst string, visina float64) {
		if l.Rekonstrukcija {
			potrebno := 30.0
			for _, redak := range strings.Split(tekst, "\n") {
				potrebno += float64(len(pdfw.Prelomi(redak, sirina-20, 10, false))) * 13
			}
			if potrebno < visina {
				visina = potrebno
			}
		}
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
	// Zadatak je naredba rukovoditelja; ono što je vodočuvar na njega
	// odgovorio je njegov rad, pa ide u opis radnih aktivnosti. Brojevi u
	// objema rubrikama se poklapaju, kao na papirnatom obrascu.
	var naredbe, ucinjeno []string
	for _, z := range l.Zadaci {
		naredbe = append(naredbe, z.Tekst+" (zadao "+z.Zadao+", "+z.ZadanoAt.In(models.Zagreb).Format("02.01.")+")")
		red := z.Tekst + ": " + z.Oznaka()
		if uz := z.UzObilazak(); uz != "" {
			red += ", " + uz
		}
		if z.Obavljeno != "" {
			red += ". " + z.Obavljeno
		}
		ucinjeno = append(ucinjeno, red)
	}
	for _, up := range l.Upisi {
		naredbe = append(naredbe, up.Tekst+" (upisao "+up.Ime+ifNe(up.Funkcija)+", "+up.Kad.In(models.Zagreb).Format("02.01. 15:04")+")")
	}
	naredbe = append(naredbe, l.NaredbeStavke()...)
	ucinjeno = append(ucinjeno, l.OpisStavke()...)
	okvir("Naredbe rukovoditelja:", numerirano(naredbe), 120)
	okvir("Opis radnih aktivnosti:", numerirano(ucinjeno), 210)
	zap := l.ZapazanjaStavke()
	if l.Ocitanja != "" {
		zap = append(zap, models.Stavke(l.Ocitanja)...)
	}
	for _, p := range l.Prijave {
		zap = append(zap, p.Tekst()+" ("+p.Kad.In(models.Zagreb).Format("02.01. 15:04")+")")
	}
	okvir("Posebna zapažanja:", numerirano(zap), 160)

	// potpisi: lijevo vodočuvar, desno rukovoditelj branjenog područja; tko je
	// potpisao u programu nosi blok elektroničke ovjere kao na aktima, pa
	// skenirani potpis ako ga ima, pa crtu s imenom
	d.Y += 14
	d.Osiguraj(120)
	y := d.Y
	const pw = 215.0
	m := mjestaPotpisa{stranica: d.Stranica(), y: y + 8, w: pw, h: visinaPotpisa, xVodocuvar: d.Lijevo, xRuk: d.W - d.Desno - pw}
	list := fmt.Sprintf("list %03d/%d", l.Broj, dan.Year())
	potpis := func(x float64, naslov, ime string, kad *time.Time, kod, userID string, crtajBlok bool) {
		d.TekstSredina(x+pw/2, y, 9, false, naslov)
		crtajPotpisLista(d, x, m.y, pw, ime, kad, list, kod, l.Cvor, otisci[userID], crtajBlok, false)
	}
	if l.Rekonstrukcija {
		// prenesen list nema potpisa: ni vodočuvarev ni rukovoditeljev, jer
		// ga nitko nije vodio ni ovjeravao u programu
		blokOvjereBoja(d, d.Lijevo, d.Sirina(), "PRENESENO IZ RANIJE EVIDENCIJE",
			l.Ime, "zadaci obilaska iz VGI Baranja (app.bp16.xyz)", "bez potpisa vodočuvara i ovjere rukovoditelja",
			sivaTekst, sivaTekst)
		d.Y = m.y + visinaPotpisa + 10
		return m
	}
	potpis(m.xVodocuvar, "Vodočuvar", l.Ime, l.PredanoAt, l.KodPredaje(), l.UserID, crtaj.vodocuvar)
	potpis(m.xRuk, "Rukovoditelj branjenog područja", l.Potvrdio, l.PotvrdenoAt, l.KodOvjere(), l.PotvrdioID, crtaj.rukovoditelj)
	d.Y = m.y + visinaPotpisa + 10
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
	return m
}

// visinaPotpisa je visina mjesta za potpis: blok, prostor za skenirani
// potpis, crta i ime
const visinaPotpisa = 46 + 42 + 11 + 12

// crtajPotpisLista crta mjesto potpisa dnevnog lista: blok elektroničkog
// potpisa (kad je list potpisan i blok se crta u sadržaju), skenirani potpis
// ispod njega, crtu i ime. Isti crtež služi kao izgled polja potpisa koje se
// dodaje naknadno, s x i y od nule.
func crtajPotpisLista(d *pdfw.Doc, x, y, w float64, ime string, kad *time.Time, list, kod, cvor string, sken *models.PotpisSlika, crtajBlok, simulacija bool) {
	staro := d.Y
	if kad != nil && crtajBlok {
		d.Y = y
		k := kad.In(models.Zagreb)
		if simulacija {
			blokOvjereBoja(d, x, w, "SIMULIRANI POTPIS · BEZVRIJEDNO", ime+" (SIMULACIJA)",
				k.Format("02.01.2006. u 15:04")+" "+k.Format("MST")+" · "+list+" · samo za testiranje", "čvor "+cvor, crvena, crvena)
		} else {
			blokOvjere(d, x, w, "ELEKTRONIČKI POTPISANO U goCOP-u", ime,
				k.Format("02.01.2006. u 15:04")+" "+k.Format("MST")+" · "+list+" · kod "+kod, "čvor "+cvor)
		}
		if sken != nil && len(sken.Slika) > 0 {
			slika(d, sken.Mime, sken.Slika, x+w/2-60, y+50, 120, 36)
		}
	}
	crta := y + 46 + 42
	d.Crta(x+10, crta, x+w-10, crta)
	if kad != nil && crtajBlok {
		d.TekstSredina(x+w/2, crta+11, 9, false, ime)
	}
	d.Y = staro
}

func ifNe(f string) string {
	if f == "" {
		return ""
	}
	return ", " + f
}
