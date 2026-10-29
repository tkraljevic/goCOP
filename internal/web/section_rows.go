package web

import (
	"math"
	"regexp"
	"slices"
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
// bez pogađanja. Isti objekt smije stajati uz dva nasipa: most preko
// Bobotskog kanala stoji i uz nasip l.o. i uz nasip d.o. (B.15.11). Što ne
// stane nikamo, ide u zadnji red bez nasipa.
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
		nasipi := poStacionazi(p.Embankments, o)
		for _, i := range nasipi {
			rows[i].Objects = append(rows[i].Objects, o)
		}
		if len(nasipi) == 0 {
			loose = append(loose, o)
		}
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

// jednakRaspon: dva nasipa jednako su dugačka uz vodu kad im se rasponi
// razlikuju za najviše metar (stacionaža je u km)
const jednakRaspon = 0.001

// poStacionazi nalazi nasipe čiji raspon uz vodu pokriva stacionažu objekta.
// Uspoređuje se samo ista vrsta stacionaže: rkm objekta prema rkm nasipa.
// Rasponi se preklapaju — nasip za zaštitu Batine leži unutar nasipa Državna
// granica–Draž — pa uz svaku obalu pobjeđuje najuži nasip koji objekt
// pokriva: to je nasip na kojem objekt doista stoji, i tako ga slaže i
// Privitak.
//
// Na BP 15 nasip je upisan posebno za lijevu i posebno za desnu obalu:
// „Nasip uz l.o. Bobotskog kanala” i „Nasip uz d.o. Bobotskog kanala”, oba
// kkm 24+000–40+000 (B.15.11). Objekt bez obale — most, propust, ušće —
// prelazi vodu, pa ide na najuži nasip svake obale, i kad rasponi nisu
// jednaki: most Petrova Slatina na rkm 44+273 (B.15.2) stoji i uz nasip l.o.
// i uz nasip d.o. Vuke. Objekt s obalom ide samo na nasip svoje obale. Kad
// takvog nema, objekt ostaje bez nasipa: objekt l.o. na rkm 42+780 (B.15.2)
// ne stoji uz nasip d.o. samo zato što jedino taj pokriva rkm.
func poStacionazi(embankments []models.PartEmbankment, o models.PartObject) []int {
	km, ok := stacionazaKm(o)
	if !ok {
		return nil
	}
	var out []int
	for _, obala := range obaleObjekta(o.Bank) {
		for _, i := range najuziUzObalu(embankments, o.StationingKind, km, obala) {
			if !slices.Contains(out, i) {
				out = append(out, i)
			}
		}
	}
	slices.Sort(out)
	return out
}

// obaleObjekta: objekt lijeve ili desne obale traži nasip samo svoje obale,
// a objekt bez obale ili obiju obala nasip svake obale posebno.
func obaleObjekta(bank string) []string {
	switch o := strings.ToUpper(strings.TrimSpace(bank)); o {
	case hydro.BankLeft, hydro.BankRight:
		return []string{o}
	}
	return []string{hydro.BankLeft, hydro.BankRight}
}

// najuziUzObalu vraća najuže nasipe koji brane obalu i pokrivaju km; svi
// rasponi uži od metra razlike su jednako uski. Nasip obiju obala natječe
// se uz svaku obalu.
func najuziUzObalu(embankments []models.PartEmbankment, vrsta string, km float64, obala string) []int {
	type kandidat struct {
		i    int
		span float64
	}
	var kandidati []kandidat
	najuzi := math.Inf(1)
	for i, e := range embankments {
		span, ok := rasponPokriva(e, vrsta, km)
		if !ok || !obalaPrima(obalaNasipa(e.Name), obala) {
			continue
		}
		kandidati = append(kandidati, kandidat{i, span})
		najuzi = math.Min(najuzi, span)
	}
	var out []int
	for _, k := range kandidati {
		if k.span-najuzi <= jednakRaspon {
			out = append(out, k.i)
		}
	}
	return out
}

// rasponPokriva kaže pokriva li raspon nasipa uz vodu stacionažu km iste
// vrste, i koliko je taj raspon dug. Raspon smije biti upisan u bilo kojem
// smjeru: Privitak za B.34.1 piše rkm 1430+000–1423+770.
func rasponPokriva(e models.PartEmbankment, vrsta string, km float64) (float64, bool) {
	if e.WaterFrom == nil || e.WaterTo == nil {
		return 0, false
	}
	if e.WaterKind != "" && vrsta != "" && !strings.EqualFold(e.WaterKind, vrsta) {
		return 0, false
	}
	lo, hi := math.Min(*e.WaterFrom, *e.WaterTo), math.Max(*e.WaterFrom, *e.WaterTo)
	if km < lo || km > hi {
		return 0, false
	}
	return hi - lo, true
}

// Obala nasipa piše se samo u nazivu: „Nasip uz l.o. r. Vuke”, „Desni
// sekundarni nasip Kovačke livade”. Oznaka mora stajati kao zasebna riječ —
// „Lonja” i „Donji Miholjac” nisu l.o. ni d.o. — pa se točka između slova
// traži, a granica riječi gleda i hrvatska slova (regexp \b ih ne zna).
var (
	reNasipLijevo = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:l\s*\.\s*o(?:[^\p{L}\p{N}]|$)|lijev)`)
	reNasipDesno  = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:d\s*\.\s*o(?:[^\p{L}\p{N}]|$)|desn)`)
)

// obalaNasipa čita obalu iz naziva nasipa. Naziv s obje obale („Usporni
// nasip uz l.o. i d.o. K. Travnik”, „Lijevi i desni nasip spojnog kanala”)
// ili bez obale („Nasip Gomboš”) brani obje, pa prima objekte obiju obala.
func obalaNasipa(naziv string) string {
	l, d := reNasipLijevo.MatchString(naziv), reNasipDesno.MatchString(naziv)
	switch {
	case l && !d:
		return hydro.BankLeft
	case d && !l:
		return hydro.BankRight
	}
	return hydro.BankBoth
}

// obalaPrima: objekt lijeve obale ide samo na nasip lijeve ili obiju obala,
// desne isto tako; objekt bez obale ili obiju obala ide na svaki nasip.
func obalaPrima(nasip, objekt string) bool {
	switch o := strings.ToUpper(strings.TrimSpace(objekt)); o {
	case hydro.BankLeft, hydro.BankRight:
		return nasip == hydro.BankBoth || nasip == o
	}
	return true
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
