package web

import (
	"fmt"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/pdfw"
	"gocop/internal/potpis"
)

type mjestaPotpisaCOP struct {
	stranica   int
	y, w, h    float64
	xZakljucio float64
}

// PDFDnevnikCOP renderira cjelovitu, čitljivu snimku dnevnika. Potpisi se
// dodaju inkrementalno na mjesta koja ova funkcija ostavlja na zadnjoj strani.
func PDFDnevnikCOP(j *models.Journal, zapisi []models.JournalEntry) ([]byte, mjestaPotpisaCOP) {
	d := pdfw.Novi(j.DisplayTitle(), "goCOP")
	d.Predmet = "Dnevnik COP-a: " + j.DisplayTitle()
	d.SviZnakovi()
	d.Podnozje = func(p *pdfw.Doc, stranica, ukupno int) {
		p.TekstBoja(p.Lijevo, p.H-p.Dolje+18, 6.5, false, j.DisplayTitle(), sivaTekst)
		p.TekstDesno(p.W-p.Desno, p.H-p.Dolje+18, 7, false, fmt.Sprintf("stranica %d/%d", stranica, ukupno))
	}
	d.TekstSredina(d.W/2, d.Y, 17, true, "DNEVNIK CENTRA OBRANE OD POPLAVA")
	d.Y += 28
	d.TekstSredina(d.W/2, d.Y, 13, true, j.DisplayTitle())
	d.Y += 24
	centar := j.CentarNaziv
	if centar == "" {
		centar = "COP sektora " + j.CentarSektor
	}
	d.Tekst(d.Lijevo, d.Y, 9, true, "Centar: "+centar)
	d.Y += 15
	razdoblje := ""
	if j.StartedAt != nil {
		razdoblje = j.StartedAt.In(models.Zagreb).Format("02.01.2006.")
	}
	if j.EndedAt != nil {
		razdoblje += " – " + j.EndedAt.In(models.Zagreb).Format("02.01.2006.")
	}
	d.Tekst(d.Lijevo, d.Y, 9, false, "Razdoblje: "+razdoblje+" · zapisa: "+fmt.Sprint(len(zapisi)))
	d.Y += 22
	if j.Notes != "" {
		d.OdlomakU(d.Lijevo, d.W-d.Lijevo-d.Desno, "Napomena: "+j.Notes, 8, false, pdfw.Lijevo)
		d.Y += 10
	}

	var dan string
	for _, e := range zapisi {
		if k := e.Date.In(models.Zagreb).Format("2006-01-02"); k != dan {
			d.Osiguraj(70)
			dan = k
			d.Y += 8
			d.Tekst(d.Lijevo, d.Y, 10, true, danTjednaHR(e.Date)+" "+e.Date.In(models.Zagreb).Format("02.01.2006."))
			d.Y += 15
		}
		d.Osiguraj(70)
		vrijeme := "—"
		if e.HappenedAt != nil {
			vrijeme = e.HappenedAt.In(models.Zagreb).Format("15:04")
		}
		meta := fmt.Sprintf("%03d · %s · %s", e.Number, vrijeme, e.KindLabel())
		if e.ReportedBy != "" {
			meta += " · javio " + e.ReportedBy
		}
		if e.UserName != "" {
			meta += " · upisao " + e.UserName
		}
		d.TekstBoja(d.Lijevo, d.Y, 7.5, true, meta, sivaTekst)
		d.Y += 12
		tekst := e.Text
		if e.Voided {
			tekst += " [STORNO: " + e.VoidReason + "]"
		}
		d.OdlomakU(d.Lijevo+8, d.W-d.Lijevo-d.Desno-8, tekst, 9, false, pdfw.Lijevo)
		d.Y += 8
	}
	if len(zapisi) == 0 {
		d.Tekst(d.Lijevo, d.Y, 9, false, "Dnevnik nema zapisa.")
		d.Y += 20
	}
	d.Osiguraj(145)
	d.Y += 14
	const w, h = 215.0, 100.0
	m := mjestaPotpisaCOP{stranica: d.Stranica(), y: d.Y, w: w, h: h, xZakljucio: d.Lijevo}
	d.TekstSredina(m.xZakljucio+w/2, d.Y, 8, false, "Zaključio i ovjerio dnevnik")
	d.TekstSredina(d.W-d.Desno-w/2, d.Y, 8, false, "Rukovoditelju sektora dostavljeno na znanje")
	d.Y += h
	d.TekstBoja(d.Lijevo, d.H-d.Dolje, 6.5, false, "Renderirani izvornik iz goCOP-a; naknadne potpise provjerava PDF čitač.", sivaTekst)
	return d.Bajtovi(), m
}

func dodatakPotpisaCOP(m mjestaPotpisaCOP, x float64, j *models.Journal, ime, uloga string, kad time.Time, p *potpis.Potpisnik) pdfw.Dodatak {
	sim := p != nil && p.Simulacija
	boja := plava
	naslov := "ELEKTRONIČKI POTPISANO U goCOP-u"
	if sim {
		boja, naslov = crvena, "SIMULIRANI POTPIS · BEZVRIJEDNO"
	}
	dod := pdfw.Dodatak{Stranica: m.stranica, X: x, Y: m.y + 12, W: m.w, H: m.h - 12, Ime: ime, Razlog: uloga + " dnevnika COP-a", Mjesto: j.CentarNaziv, Kad: kad,
		Crtaj: func(d *pdfw.Doc) {
			blokOvjereBoja(d, 0, m.w, naslov, ime+func() string {
				if sim {
					return " (SIMULACIJA)"
				}
				return ""
			}(),
				kad.In(models.Zagreb).Format("02.01.2006. u 15:04 MST")+" · "+uloga, "goCOP · "+strings.TrimSpace(j.CentarNaziv), boja, boja)
		},
	}
	if sim {
		dod.X, dod.Y, dod.W, dod.H = 0, 0, pdfw.A4W, pdfw.A4H
		dod.Crtaj = func(d *pdfw.Doc) {
			naslov := "SIMULACIJA · BEZVRIJEDNO"
			y := d.H/2 - 18
			d.Okvir(38, y-38, d.W-76, 82, bijela, crvena)
			d.TekstBoja((d.W-pdfw.SirinaTeksta(naslov, 27, true))/2, y, 27, true, naslov, crvena)
			d.TekstBoja((d.W-pdfw.SirinaTeksta("potpis simuliranim ključem · samo za testiranje", 14, false))/2, y+28, 14, false, "potpis simuliranim ključem · samo za testiranje", crvena)
			blokOvjereBoja(d, x, m.w, "SIMULIRANI POTPIS · BEZVRIJEDNO", ime+" (SIMULACIJA)", kad.In(models.Zagreb).Format("02.01.2006. u 15:04 MST")+" · "+uloga, "goCOP · "+strings.TrimSpace(j.CentarNaziv), crvena, crvena)
		}
	}
	return dod
}
