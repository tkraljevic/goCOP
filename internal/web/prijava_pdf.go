package web

import (
	"fmt"
	"strings"

	"gocop/internal/models"
	"gocop/internal/pdfw"
)

// prilogPrijave je što uz prijavu ide u dokument: sektor i područje za
// zaglavlje, slike po oznaci, isječak karte kao PNG (prazno kad ga nema)
type prilogPrijave struct {
	Sektor   *models.Sector
	Podrucje *models.Area
	Slike    map[string][]byte
	Karta    []byte
	Zasluge  string
	Otisci   models.OtisciLista
}

// pdfPrijave crta prijavu s terena po uzoru na dosadašnju tiskanu prijavu
// vodočuvara koja ide u urudžbeni zapisnik kao dolazni akt: zaglavlje
// organizacije i mjesto za prijemni štambilj, podaci o vodočuvaru, naslov,
// tablica s danom, vodotokom, građevinom i opisom, mjesto i potpis; zatim
// stranica s kartom i po jedna stranica za svaku fotografiju. Vraća PDF i
// mjesto bloka potpisa za polje elektroničkog potpisa.
func pdfPrijave(p *models.PrijavaSTerena, pr prilogPrijave, t models.OrgTerms, crtajBlok bool) ([]byte, mjestaPotpisa) {
	d := pdfw.Novi(p.VrstaLabel()+" s terena "+p.Oznaka()+": "+p.Naslov, "goCOP")
	d.Predmet = "Prijava s terena: " + p.Ime
	d.SviZnakovi()
	org := t.OrgName
	if org == "" {
		org = "Hrvatske vode"
	}

	// zaglavlje lijevo: organizacija, VGO, VGI; desno prijemni štambilj
	y := d.Gore + 8
	d.Tekst(d.Lijevo, y+12, 11, true, strings.ToUpper(org))
	if pr.Sektor != nil && pr.Sektor.VgoName != "" {
		d.TekstBoja(d.Lijevo, y+26, 7.5, false, strings.ToUpper(pr.Sektor.VgoName), sivaTekst)
	}
	if pr.Podrucje != nil && pr.Podrucje.VgiName != "" {
		d.TekstBoja(d.Lijevo, y+37, 7.5, false, strings.ToUpper(pr.Podrucje.VgiName), sivaTekst)
	}
	const sw, sh = 190.0, 84.0
	sx := d.W - d.Desno - sw
	d.Okvir(sx, y, sw, sh, bijela, sivaRub)
	d.TekstBoja(sx+8, y+11, 6, false, "PRIJEMNI ŠTAMBILJ (URUDŽBENI ZAPISNIK)", sivaSvijetla)
	stambilj := func(i int, oznaka, vrijednost string) {
		yy := y + 26 + float64(i)*15
		d.TekstBoja(sx+8, yy, 6.5, false, oznaka, sivaTekst)
		if vrijednost != "" {
			d.Tekst(sx+62, yy, 8, false, vrijednost)
		} else {
			d.CrtaBoja(sx+62, yy+1, sx+sw-8, yy+1, 0.4, sivaRub)
		}
	}
	primljeno := ""
	if p.PrimljenoAt != nil {
		primljeno = p.PrimljenoAt.In(models.Zagreb).Format("02.01.2006.")
	}
	stambilj(0, "Primljeno:", primljeno)
	stambilj(1, "Klasa:", p.Klasa)
	stambilj(2, "Urbroj:", p.Urbroj)
	stambilj(3, "Prilozi:", prilozi(p, pr))
	d.Y = y + sh + 22

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
	} else if p.AreaID > 0 {
		polje("BRANJENO PODRUČJE:", fmt.Sprint(p.AreaID), false)
	}
	polje("DIONICA:", p.DionicaCode, false)
	polje("IME I PREZIME (vodočuvara):", p.Ime, true)

	// naslov: vrsta i naslov, sredina
	d.Y += 26
	d.TekstSredina(d.W/2, d.Y, 15, true, strings.ToUpper(p.VrstaLabel()))
	d.Y += 6
	d.OdlomakU(d.Lijevo, d.Sirina(), strings.ToUpper(p.Naslov), 10, false, pdfw.Sredina)
	d.Y += 10

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
	d.Y += 12
	d.Osiguraj(40)
	d.Y += 10
	d.Tekst(d.Lijevo, d.Y, 8, true, "Opis događanja:")
	d.Y -= 9
	d.OdlomakU(d.Lijevo+150, d.Sirina()-150, p.Opis, 9, false, pdfw.Lijevo)

	// mjesto i datum lijevo, potpis vodočuvara desno
	d.Y += 30
	d.Osiguraj(visinaPotpisa + 30)
	const pw = 215.0
	yPot := d.Y
	m := mjestaPotpisa{stranica: d.Stranica(), y: yPot + 8, w: pw, h: visinaPotpisa, xVodocuvar: d.W - d.Desno - pw, xRuk: d.Lijevo}
	mjesto := ""
	if pr.Podrucje != nil {
		mjesto = strings.TrimSpace(strings.TrimPrefix(pr.Podrucje.Subcenter, "Podcentar "))
	}
	datum := p.Datum.In(models.Zagreb).Format("2.1.2006.")
	if mjesto != "" {
		d.Tekst(d.Lijevo, yPot+visinaPotpisa-20, 8.5, false, "U "+uMjestu(mjesto)+", "+datum)
	} else {
		d.Tekst(d.Lijevo, yPot+visinaPotpisa-20, 8.5, false, datum)
	}
	d.TekstSredina(m.xVodocuvar+pw/2, yPot, 8.5, false, "potpis vodočuvara")
	crtajPotpisLista(d, m.xVodocuvar, m.y, pw, p.Ime, p.ObjavljenoAt, "prijava "+p.Oznaka(), p.Kod(), p.Cvor, pr.Otisci[p.UserID], crtajBlok, false)
	if p.ObjavljenoAt == nil {
		d.TekstSredina(m.xVodocuvar+pw/2, m.y+46+42+11, 8, false, "( "+p.Ime+" )")
	}
	d.Y = m.y + visinaPotpisa + 6
	if p.ListBroj > 0 {
		d.TekstBoja(d.Lijevo, d.H-d.Dolje, 6.5, false, fmt.Sprintf("Upisano na dnevni list vodočuvara %03d/%d. Dokument je sastavljen u goCOP-u; fotografije su smanjene i ugrađene, pa ostaju s dokumentom trajno.", p.ListBroj, p.Godina), sivaTekst)
	} else {
		d.TekstBoja(d.Lijevo, d.H-d.Dolje, 6.5, false, "NACRT: prijava još nije objavljena ni potpisana.", sivaTekst)
	}

	// karta na svojoj stranici
	if len(pr.Karta) > 0 {
		d.NovaStranica()
		d.Y = d.Gore + 10
		d.Tekst(d.Lijevo, d.Y, 9, true, "Lokacija na karti:")
		d.Y += 8
		w := d.Sirina()
		h := w * float64(visinaKarte) / float64(sirinaKarte)
		if err := d.SlikaPNG(pr.Karta, d.Lijevo, d.Y, w, h); err == nil {
			d.Y += h + 12
			if p.ImaKoordinate() {
				d.TekstBoja(d.Lijevo, d.Y, 7.5, false, fmt.Sprintf("Oznaka: %.6f, %.6f (WGS84). %s", *p.Latitude, *p.Longitude, pr.Zasluge), sivaTekst)
			}
		}
	}

	// svaka fotografija na svojoj stranici
	for i, sl := range p.Slike {
		b := pr.Slike[sl.ID]
		d.NovaStranica()
		d.Y = d.Gore + 10
		napis := fmt.Sprintf("Fotografija %d od %d", i+1, len(p.Slike))
		if sl.Naziv != "" {
			napis += ": " + sl.Naziv
		}
		d.Tekst(d.Lijevo, d.Y, 9, true, napis)
		d.Y += 10
		if len(b) == 0 || sl.Sirina == 0 || sl.Visina == 0 {
			d.TekstBoja(d.Lijevo, d.Y+10, 8, false, "Fotografija nije dostupna na ovom čvoru.", sivaTekst)
			continue
		}
		w, hMax := d.Sirina(), d.H-d.Dolje-d.Y-20
		sw, sh := w, w*float64(sl.Visina)/float64(sl.Sirina)
		if sh > hMax {
			sh = hMax
			sw = sh * float64(sl.Sirina) / float64(sl.Visina)
		}
		_ = d.SlikaJPEG(b, d.Lijevo+(w-sw)/2, d.Y, sw, sh)
	}
	return d.Bajtovi(), m
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
