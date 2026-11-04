package web

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"gocop/internal/models"
)

// Popis Očitanja po dizajnu (goCOP-m3, „12 · Očitanja”): letve grupirane po
// vodi, u retku trend, naziv, kad i kako je očitano, faza obrane, poruka o
// kvaru i vodostaj desno. Filtri su čipovi; njihove poveznice zadržavaju
// ostale filtre.

// RedakLetve je jedan redak popisa Očitanja
type RedakLetve struct {
	G       models.GaugeSummary
	Trend   string // gore, dolje, ravno ili prazno kad nema prethodnog očitanja
	Kad     string // „07:00”, „jučer 19:05” ili „3. 11. 2026. 07:00”
	Opis    string // „ručno · Marko Primjer · nizvodno 105 cm · 1.250 m³/s”
	Vrijed  string // „312 cm”, „—” ili „nije očitan”
	Kvar    string // rečenica za korisnika kad mjerni uređaj vjerojatno ne radi
	KvarZa  string // dulji opis kvara (title)
	Skokova int
	Odabran bool
}

// SkupinaLetava su letve iste vode; objekti s letvom imaju svoju skupinu
type SkupinaLetava struct {
	Voda  string
	Letve []RedakLetve
}

// IzborFiltra je stavka izbornika čipa (sektor ili područje)
type IzborFiltra struct {
	Naziv   string
	URL     string
	Odabran bool
}

const skupinaObjekata = "Objekti s letvom"

// kadOcitano piše vrijeme očitanja kako ga vodočuvar govori: danas samo sat,
// jučer s riječju, starije s datumom
func kadOcitano(t, sad time.Time) string {
	t, sad = t.In(models.Zagreb), sad.In(models.Zagreb)
	dan := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, models.Zagreb) }
	switch {
	case dan(t).Equal(dan(sad)):
		return t.Format("15:04")
	case dan(t).AddDate(0, 0, 1).Equal(dan(sad)):
		return "jučer " + t.Format("15:04")
	}
	return fmt.Sprintf("%d. %d. %d. %s", t.Day(), int(t.Month()), t.Year(), t.Format("15:04"))
}

func smjerTrenda(g models.GaugeSummary) string {
	d := g.Trend()
	switch {
	case d == nil:
		return ""
	case *d > 0:
		return "gore"
	case *d < 0:
		return "dolje"
	}
	return "ravno"
}

// redakOcitanja slaže redak za letvu iz zadnjeg očitanja, protoka i znakova
// mjernog uređaja
func redakOcitanja(g models.GaugeSummary, protok ProtokLetve, znak ZnakLetve, sad time.Time) RedakLetve {
	r := RedakLetve{G: g, Trend: smjerTrenda(g), Skokova: znak.Skokova, KvarZa: znak.Kvar}
	if znak.Kvar != "" {
		r.Kvar = "Mjerni uređaj je vjerojatno u kvaru."
		if znak.NaDnu {
			r.Kvar = "Voda je vjerojatno ispod dosega mjernog uređaja."
		}
	}
	l := g.Latest
	if l == nil {
		return r
	}
	r.Kad = kadOcitano(l.MeasuredAt, sad)
	r.Opis = opisOcitanja(*l, protok)
	switch {
	case l.LevelCm != nil:
		r.Vrijed = fmt.Sprintf("%d cm", *l.LevelCm)
	case l.Level2Cm != nil:
		r.Vrijed = "—"
	default:
		r.Vrijed = "nije očitan"
	}
	return r
}

// opisOcitanja: kako je očitano i tko, uz nizvodnu stranu, protok i
// temperaturu kad ih ima
func opisOcitanja(l models.Reading, protok ProtokLetve) string {
	dijelovi := []string{l.SourceLabel()}
	if l.Observer != "" {
		dijelovi = append(dijelovi, l.Observer)
	}
	if l.Level2Cm != nil {
		dijelovi = append(dijelovi, fmt.Sprintf("nizvodno %d cm", *l.Level2Cm))
	}
	if protok.Q != "" {
		q := protok.Q + " m³/s"
		if !protok.Izmjeren {
			q = "≈ " + q
		}
		dijelovi = append(dijelovi, q)
	}
	if l.TempC != nil {
		dijelovi = append(dijelovi, brojHRf(*l.TempC, 1)+" °C")
	}
	return strings.Join(dijelovi, " · ")
}

// skupineLetava grupira retke po vodi redom kojim se vode prvi put pojave;
// objekti s letvom idu na kraj
func skupineLetava(redovi []RedakLetve) []SkupinaLetava {
	var out []SkupinaLetava
	mjesto := map[string]int{}
	var objekti []RedakLetve
	for _, r := range redovi {
		if r.G.StructureID != "" || r.G.Voda == "" {
			objekti = append(objekti, r)
			continue
		}
		i, ok := mjesto[r.G.Voda]
		if !ok {
			i = len(out)
			mjesto[r.G.Voda] = i
			out = append(out, SkupinaLetava{Voda: r.G.Voda})
		}
		out[i].Letve = append(out[i].Letve, r)
	}
	if len(objekti) > 0 {
		out = append(out, SkupinaLetava{Voda: skupinaObjekata, Letve: objekti})
	}
	return out
}

// adresaPopisa vraća /readings s trenutnim filtrima uz zadanu promjenu;
// prazna vrijednost miče filtar, a stranica popisa uvijek kreće od prve
func adresaPopisa(q url.Values, promjene map[string]string) string {
	n := url.Values{}
	for _, k := range []string{"q", "sector", "area", "all", "moje"} {
		if v := q.Get(k); v != "" {
			n.Set(k, v)
		}
	}
	for k, v := range promjene {
		if v == "" {
			n.Del(k)
		} else {
			n.Set(k, v)
		}
	}
	if len(n) == 0 {
		return "/readings"
	}
	return "/readings?" + n.Encode()
}

// izborSektora i izborPodrucja su stavke izbornika čipova
func izborSektora(q url.Values, sektori []models.Sector, odabran string) []IzborFiltra {
	out := []IzborFiltra{{Naziv: "Svi sektori", URL: adresaPopisa(q, map[string]string{"sector": "", "area": ""}), Odabran: odabran == ""}}
	for _, s := range sektori {
		out = append(out, IzborFiltra{Naziv: s.Name, URL: adresaPopisa(q, map[string]string{"sector": s.ID, "area": ""}), Odabran: s.ID == odabran})
	}
	return out
}

func izborPodrucja(q url.Values, podrucja []models.Area, odabrano int, imaSektor bool) []IzborFiltra {
	sva := "Sva područja"
	if imaSektor {
		sva = "Sva područja sektora"
	}
	out := []IzborFiltra{{Naziv: sva, URL: adresaPopisa(q, map[string]string{"area": ""}), Odabran: odabrano == 0}}
	for _, a := range podrucja {
		out = append(out, IzborFiltra{Naziv: fmt.Sprintf("%d: %s", a.ID, a.Name), URL: adresaPopisa(q, map[string]string{"area": fmt.Sprint(a.ID)}), Odabran: a.ID == odabrano})
	}
	return out
}

// adresaKarteLetava otvara glavnu Kartu zumiranu na letve s popisa
// (Odluke (4. 11. 2026.): svaka karta u programu je glavna Karta)
func adresaKarteLetava(letve []models.GaugeSummary) string {
	var id []string
	for _, g := range letve {
		if g.StationID != "" {
			id = append(id, g.StationID)
		}
	}
	if len(id) == 0 {
		return "/karta"
	}
	return "/karta#letve=" + strings.Join(id, ",")
}

// nazivCipaSektora je kratko ime sektora za čip („Sektor B”)
func nazivCipaSektora(s models.Sector) string {
	if len(s.ID) <= 2 {
		return "Sektor " + s.ID
	}
	return s.Name
}

// naziviCipova su natpisi čipova sektora i područja kad su odabrani
func naziviCipova(sektori []models.Sector, podrucja []models.Area, sektor string, podrucje int) (string, string) {
	var ns, np string
	for _, s := range sektori {
		if s.ID == sektor {
			ns = nazivCipaSektora(s)
		}
	}
	for _, a := range podrucja {
		if a.ID == podrucje {
			np = fmt.Sprintf("BP %d", a.ID)
		}
	}
	return ns, np
}

// prekidac je vrijednost filtra nakon klika: uključen se gasi, ugašen pali
func prekidac(ukljucen bool) string {
	if ukljucen {
		return ""
	}
	return "1"
}

// poStraniciPopisa: popis uz otvorenu letvu nema stranica, jer bi listanje
// odvelo s letve
func poStraniciPopisa(okno bool) int {
	if okno {
		return 2000
	}
	return registryPerPage
}
