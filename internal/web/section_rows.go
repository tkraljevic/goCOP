package web

import (
	"strings"

	"gocop/internal/hydro"
	"gocop/internal/models"
)

// Kartica dionice slaže se kao Privitak: nosivi redak je nasip, a uz njega
// stoje objekti koji na njemu leže. Aplikacija je to razbijala u odvojene
// popise, pa je oko samo sastavljalo što ide s čime.
type EmbankmentRow struct {
	Embankment  *models.PartEmbankment // nil za red objekata koji nisu ni na jednom nasipu
	Objects     []models.PartObject
	Territories []models.SectionTerritory // ugroženo područje koje Privitak navodi uz taj nasip
}

// embankmentRows razvrstava objekte po nasipima. Objekt s upisanim nasipom ide
// na njega; objekt bez nasipa, ali sa stacionažom uz vodu, ide na nasip čiji
// raspon uz vodu tu stacionažu pokriva — ušće ili vodokaz na rkm 1424+850 leži
// uz nasip koji štiti 1425+770–1423+770. To pravilo vraća raspored Privitka
// bez pogađanja. Što ne stane nikamo, ide u zadnji red bez nasipa.
func embankmentRows(p models.SectionPart) []EmbankmentRow {
	rows := make([]EmbankmentRow, len(p.Embankments))
	for i := range p.Embankments {
		rows[i].Embankment = &p.Embankments[i]
	}
	var loose []models.PartObject
	for _, o := range p.Objects {
		if i := poNazivu(p.Embankments, o.OnEmbankment); i >= 0 {
			rows[i].Objects = append(rows[i].Objects, o)
			continue
		}
		if i := poStacionazi(p.Embankments, o); i >= 0 {
			rows[i].Objects = append(rows[i].Objects, o)
			continue
		}
		loose = append(loose, o)
	}
	if len(loose) > 0 {
		rows = append(rows, EmbankmentRow{Objects: loose})
	}
	return rows
}

func poNazivu(embankments []models.PartEmbankment, name string) int {
	k := kljucNaziva(name)
	if k == "" {
		return -1
	}
	for i, e := range embankments {
		if kljucNaziva(e.Name) == k {
			return i
		}
	}
	return -1
}

// poStacionazi nalazi nasip čiji raspon uz vodu pokriva stacionažu objekta.
// Uspoređuje se samo ista vrsta stacionaže: rkm objekta prema rkm nasipa.
// Rasponi se preklapaju — nasip za zaštitu Batine leži unutar nasipa Državna
// granica–Draž — pa pobjeđuje najuži koji objekt pokriva: to je nasip na
// kojem objekt doista stoji, i tako ga slaže i Privitak.
func poStacionazi(embankments []models.PartEmbankment, o models.PartObject) int {
	km, ok := stacionazaKm(o)
	if !ok {
		return -1
	}
	best, bestSpan := -1, 0.0
	for i, e := range embankments {
		if e.WaterFrom == nil || e.WaterTo == nil {
			continue
		}
		if e.WaterKind != "" && o.StationingKind != "" && !strings.EqualFold(e.WaterKind, o.StationingKind) {
			continue
		}
		lo, hi := *e.WaterFrom, *e.WaterTo
		if lo > hi {
			lo, hi = hi, lo
		}
		if km < lo || km > hi {
			continue
		}
		if span := hi - lo; best < 0 || span < bestSpan {
			best, bestSpan = i, span
		}
	}
	return best
}

// stacionazaKm uzima broj kad ga ima, a inače ga čita iz zapisa "rkm 1428+010"
func stacionazaKm(o models.PartObject) (float64, bool) {
	if o.Stationing != nil {
		return *o.Stationing, true
	}
	return hydro.ParseStationingKm(o.StationingText)
}

func kljucNaziva(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.NewReplacer("-", " ", "–", " ").Replace(s))), " ")
}

// razvrstajUgrozeno stavlja naselja uz nasip uz koji ih Privitak navodi.
// Naselje bez te oznake — a takva su sva na dionici bez nasipa — ostaje na
// poddionici i ispisuje se iznad tablice, kako je i bilo prije podjele.
func razvrstajUgrozeno(rows []EmbankmentRow, part models.SectionPart, poKljucu map[string]models.SectionTerritory) []models.SectionTerritory {
	postoji := map[string]bool{}
	for _, r := range rows {
		if r.Embankment != nil {
			postoji[kljucNaziva(r.Embankment.Name)] = true
		}
	}
	uNasipu := map[string][]models.SectionTerritory{}
	var ostalo []models.SectionTerritory
	for _, t := range part.Territories {
		puni, ok := poKljucu[t.Key()]
		if !ok {
			continue
		}
		// Nasip pod kojim naselje stoji mogao je u međuvremenu biti
		// preimenovan ili uklonjen; naselje tada pada natrag na poddionicu, a
		// ne nestaje s kartice.
		if k := kljucNaziva(t.OnEmbankment); k != "" && postoji[k] {
			uNasipu[k] = append(uNasipu[k], puni)
			continue
		}
		ostalo = append(ostalo, puni)
	}
	for i := range rows {
		if rows[i].Embankment == nil {
			continue
		}
		rows[i].Territories = uNasipu[kljucNaziva(rows[i].Embankment.Name)]
	}
	return ostalo
}
