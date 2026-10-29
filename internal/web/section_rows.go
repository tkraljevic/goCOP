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

// UgrozenoPoZupanijama daje ugroženo područje složeno kao u Privitku: županija,
// pa općina, pa naselja.
func (r EmbankmentRow) UgrozenoPoZupanijama() []UgrozenaZupanija {
	return grupirajUgrozeno(r.Territories)
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

// ugrozenoBezNasipa: naselje koje nije uz nijedan nasip stoji u retku „Bez
// nasipa”, uz objekte izvan nasipa, i na poddionici s nasipima i bez njih.
// Prije ga je poddionica s nasipima pokazivala iznad tablice, pa je redak
// „Bez nasipa” imao nula ugroženih (B.34.12, poddionica 2). Kad takvog retka
// nema, dodaje se samo radi ugroženog područja.
func ugrozenoBezNasipa(rows []EmbankmentRow, ostalo []models.SectionTerritory) ([]EmbankmentRow, []models.SectionTerritory) {
	if len(ostalo) == 0 {
		return rows, ostalo
	}
	if len(rows) == 0 || rows[len(rows)-1].Embankment != nil {
		rows = append(rows, EmbankmentRow{})
	}
	rows[len(rows)-1].Territories = ostalo
	return rows, nil
}

// rasporediUgrozeno razvrstava ugroženo područje poddionice po retcima
// kartice. Nasip s kvačicom „vrijedi i ugroženo područje cijele
// poddionice” pokazuje uz svoja i naselja upisana bez nasipa (B.34.14,
// poddionica 2: tri nasipa, pet naselja za cijelu poddionicu). Redak „Bez
// nasipa” koji postoji zbog objekata izvan nasipa i dalje pokazuje sva
// naselja bez svog nasipa. Samo radi naselja dodaje se tek kad ima naselja
// čiji je nasip preimenovan ili uklonjen: ona ne pripadaju cijeloj
// poddionici, pa ih nasip s kvačicom ne preuzima. Kad nijedan nasip nema
// kvačicu, sve je kao u ugrozenoBezNasipa.
func rasporediUgrozeno(rows []EmbankmentRow, part models.SectionPart, poKljucu map[string]models.SectionTerritory) ([]EmbankmentRow, []models.SectionTerritory) {
	ostalo := razvrstajUgrozeno(rows, part, poKljucu)
	if len(ostalo) == 0 || !nekiNasipNosiPoddionicu(rows) {
		return ugrozenoBezNasipa(rows, ostalo)
	}
	zaPoddionicu, bezSvogNasipa := podijeliBezNasipa(ostalo, part)
	for i := range rows {
		if e := rows[i].Embankment; e != nil && e.UgrozenoPoddionice {
			rows[i].Territories = dodajNaselja(rows[i].Territories, zaPoddionicu)
		}
	}
	if n := len(rows); rows[n-1].Embankment == nil {
		rows[n-1].Territories = ostalo
	} else if len(bezSvogNasipa) > 0 {
		rows = append(rows, EmbankmentRow{Territories: bezSvogNasipa})
	}
	return rows, nil
}

// podijeliBezNasipa dijeli naselja bez svog nasipa na ona upisana za cijelu
// poddionicu (on_embankment prazno) i ona čiji nasip više ne postoji
func podijeliBezNasipa(ostalo []models.SectionTerritory, part models.SectionPart) (zaPoddionicu, bezSvogNasipa []models.SectionTerritory) {
	bezOznake := map[string]bool{}
	for _, t := range part.Territories {
		if kljucNaziva(t.OnEmbankment) == "" {
			bezOznake[t.Key()] = true
		}
	}
	for _, t := range ostalo {
		if bezOznake[kljucUgrozenog(t)] {
			zaPoddionicu = append(zaPoddionicu, t)
		} else {
			bezSvogNasipa = append(bezSvogNasipa, t)
		}
	}
	return zaPoddionicu, bezSvogNasipa
}

func nekiNasipNosiPoddionicu(rows []EmbankmentRow) bool {
	for _, r := range rows {
		if r.Embankment != nil && r.Embankment.UgrozenoPoddionice {
			return true
		}
	}
	return false
}

// dodajNaselja vraća nov popis: naselja nasipa, pa naselja poddionice kojih
// uz nasip još nema. Isto naselje uz nasip i uz poddionicu ne piše se dvaput;
// tako otpada i naselje upisano dvaput uz isti nasip, što obrazac ionako ne
// dopušta.
func dodajNaselja(svoja, poddionice []models.SectionTerritory) []models.SectionTerritory {
	out := make([]models.SectionTerritory, 0, len(svoja)+len(poddionice))
	ima := map[string]bool{}
	for _, popis := range [][]models.SectionTerritory{svoja, poddionice} {
		for _, t := range popis {
			k := kljucUgrozenog(t)
			if ima[k] {
				continue
			}
			ima[k] = true
			out = append(out, t)
		}
	}
	return out
}

func kljucUgrozenog(t models.SectionTerritory) string {
	return models.PartTerritory{CountyID: t.CountyID, MunicipalityID: t.MunicipalityID, SettlementID: t.SettlementID}.Key()
}
