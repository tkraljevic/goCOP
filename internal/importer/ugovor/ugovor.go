// Package ugovor uvozi iz ugovora o održavanju (program A.02) ono što je
// trajno: popis lokacija izvršenja usluga s kategorijom pod kojom se voda ili
// nasip vodi, i stavke radova bez cijena. Pozicije plana i cjenici se ne
// uvoze — mijenjaju se sa svakim okvirnim sporazumom.
//
// Ugovore od 2023. generira Excel dodatak Hrvatskih voda, pa je list
// TROŠKOVNIK označen po retku (#P pozicija, #V voda, #O objekt, #S stavka...),
// a list LOKACIJE_BP_NN nosi razvrstavanje voda područja. Isti oblik vrijedi
// za sva branjena područja.
package ugovor

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gocop/internal/hydro"
	"gocop/internal/importer/xlsx"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Location je redak popisa lokacija izvršenja usluga
type Location struct {
	Seq   string // "4.12."
	Name  string // "Kanal Gornji Zmajevački"
	Order string // models.WaterOrder*
	Group string // models.WaterGroup*
	Kind  string // models.MaintenanceKind*
}

// Item je stavka radova kako se pojavljuje u troškovniku, bez cijene
type Item struct {
	Number      string // oznaka iz okvirnog sporazuma, npr. "225"
	Description string
	Unit        string
	Uses        int // u koliko se redaka troškovnika pojavljuje
}

// Block je jedna ugovorna stavka troškovnika (blok #P … #E); služi za
// provjeru i za prepoznavanje nasipa, ne pohranjuje se
type Block struct {
	Position string // pozicija plana
	Code     string // "1.1."
	Water    string
	Object   string // Vodotok, Kanal, Retencija, Nasip, Bujica
	Site     string // lokacija usluga, stacionaža
	County   string
	Value    float64
	Lines    int
}

// Contract je pročitani ugovor
type Contract struct {
	Path      string
	AreaID    int
	AreaName  string
	Locations []Location
	Items     []Item // stavke koje ugovor stvarno koristi
	Catalogue []Item // sve stavke ponudbenog troškovnika (bez cijena)
	Blocks    []Block
}

var (
	reSeq     = regexp.MustCompile(`^\d+\.\d+\.\s*$`)
	reKindRow = regexp.MustCompile(`^(\d+)\.\s`)
	reAreaPos = regexp.MustCompile(`^A\.02\.01\.(\d+)\.`)
)

// Parse čita ugovor iz radne knjige
func Parse(wb *xlsx.Workbook) (*Contract, error) {
	c := &Contract{}

	// Područje: iz postavki dodatka, a potvrda iz pozicija plana
	if ps := wb.Sheet("PPI_POSTAVKE"); ps != nil {
		if n, err := strconv.Atoi(strings.TrimSpace(ps.Cell(0, 1))); err == nil {
			c.AreaID = n
		}
		c.AreaName = strings.TrimSpace(ps.Cell(1, 1))
	}

	if err := c.parseTroskovnik(wb.Sheet("TROŠKOVNIK")); err != nil {
		return nil, err
	}
	if err := c.parseLocations(wb.SheetPrefix("LOKACIJE")); err != nil {
		return nil, err
	}
	c.parseCatalogue(wb.Sheet("PREVENTIVNA"))
	if c.AreaID == 0 {
		return nil, fmt.Errorf("iz radne knjige se ne vidi koje je branjeno područje")
	}
	return c, nil
}

func cellOf(row []string) func(int) string {
	return func(i int) string {
		if i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
}

// parseTroskovnik čita blokove ugovornih stavki i skuplja stavke radova
func (c *Contract) parseTroskovnik(s *xlsx.Sheet) error {
	if s == nil {
		return fmt.Errorf("radna knjiga nema list TROŠKOVNIK — nije ugovor iz dodatka Hrvatskih voda")
	}
	items := map[string]*Item{}
	var order []string
	var cur *Block
	for _, row := range s.Rows {
		if len(row) == 0 {
			continue
		}
		cell := cellOf(row)
		switch cell(0) {
		case "#P":
			c.Blocks = append(c.Blocks, Block{Position: cell(8), Code: cell(10)})
			cur = &c.Blocks[len(c.Blocks)-1]
			if m := reAreaPos.FindStringSubmatch(cur.Position); m != nil {
				if n, err := strconv.Atoi(m[1]); err == nil {
					if c.AreaID == 0 {
						c.AreaID = n
					} else if c.AreaID != n {
						return fmt.Errorf("pozicija %s nije iz područja %d", cur.Position, c.AreaID)
					}
				}
			}
		case "#V":
			if cur != nil {
				cur.Water = cell(8)
			}
		case "#O":
			if cur != nil {
				cur.Object = cell(8)
			}
		case "#L":
			if cur != nil {
				cur.Site = cell(8)
			}
		case "#Z":
			if cur != nil {
				cur.County = cell(8)
			}
		case "#N":
			if cur != nil {
				cur.Value, _ = xlsx.Number(cell(8))
			}
		case "#S":
			if cur != nil {
				cur.Lines++
			}
			desc := strings.Join(strings.Fields(cell(7)), " ")
			number := strings.TrimSuffix(cell(8), ".0")
			unit := cell(9)
			if desc == "" {
				continue
			}
			key := models.WorkItemKey(number, desc, unit)
			it := items[key]
			if it == nil {
				it = &Item{Number: number, Description: desc, Unit: unit}
				items[key] = it
				order = append(order, key)
			}
			it.Uses++
		case "#E":
			cur = nil
		}
	}
	for _, k := range order {
		c.Items = append(c.Items, *items[k])
	}
	if len(c.Blocks) == 0 {
		return fmt.Errorf("list TROŠKOVNIK nema nijedan blok #P")
	}
	return nil
}

// parseLocations čita popis lokacija: zaglavlja reda vode u prvom stupcu,
// vrste u drugom, redni broj i naziv u trećem i četvrtom
func (c *Contract) parseLocations(s *xlsx.Sheet) error {
	if s == nil {
		return fmt.Errorf("radna knjiga nema list LOKACIJE_BP_NN")
	}
	var order, group, kind string
	for _, row := range s.Rows {
		cell := cellOf(row)
		if h := cell(0); strings.HasPrefix(strings.ToUpper(h), "VODE ") {
			order, group = classifyHeader(h)
			kind = ""
			continue
		}
		if m := reKindRow.FindStringSubmatch(cell(1)); m != nil {
			kind = kindByNumber(m[1])
			continue
		}
		if reSeq.MatchString(cell(2)) && cell(3) != "" {
			c.Locations = append(c.Locations, Location{
				Seq: cell(2), Name: strings.Join(strings.Fields(cell(3)), " "),
				Order: order, Group: group, Kind: kind,
			})
		}
	}
	if len(c.Locations) == 0 {
		return fmt.Errorf("popis lokacija je prazan")
	}
	return nil
}

// parseCatalogue čita ponudbeni troškovnik: redni broj, oznaka, opis, jedinica.
// Cijene se ne čitaju.
func (c *Contract) parseCatalogue(s *xlsx.Sheet) {
	if s == nil {
		return
	}
	for _, row := range s.Rows {
		cell := cellOf(row)
		if _, err := strconv.Atoi(cell(0)); err != nil {
			continue
		}
		number := strings.TrimSuffix(cell(1), ".0")
		desc := strings.Join(strings.Fields(cell(2)), " ")
		if number == "" || desc == "" {
			continue
		}
		c.Catalogue = append(c.Catalogue, Item{Number: number, Description: desc, Unit: cell(3)})
	}
}

func classifyHeader(h string) (order, group string) {
	u := strings.ToUpper(hydro.FoldDiacritics(h))
	switch {
	case strings.Contains(u, "II. REDA") || strings.Contains(u, "II REDA"):
		return models.WaterOrderSecond, ""
	case strings.Contains(u, "MEDUDRZAVNE"):
		return models.WaterOrderFirst, models.WaterGroupInterstate
	case strings.Contains(u, "OSTALE") || strings.Contains(u, "DRUGE"):
		return models.WaterOrderFirst, models.WaterGroupOtherState
	case strings.Contains(u, "I. REDA") || strings.Contains(u, "I REDA"):
		return models.WaterOrderFirst, ""
	}
	return "", ""
}

func kindByNumber(n string) string {
	switch n {
	case "1":
		return models.MaintenanceKindWatercourse
	case "2":
		return models.MaintenanceKindReservoir
	case "3":
		return models.MaintenanceKindTorrent
	case "4":
		return models.MaintenanceKindDrainage
	}
	return ""
}

// IsEmbankment govori je li lokacija nasip, a ne voda: po nazivu, ili zato
// što je troškovnik za tu vodu označio objekt "Nasip"
func (c *Contract) IsEmbankment(l Location) bool {
	if strings.Contains(strings.ToLower(l.Name), "nasip") {
		return true
	}
	key := normKey(l.Name)
	for _, b := range c.Blocks {
		if strings.EqualFold(b.Object, "Nasip") && normKey(b.Water) == key {
			return true
		}
	}
	return false
}

// --- ključevi za uparivanje ---

var (
	abbreviations = strings.NewReplacer(
		"g.d.k.", "glavni dovodni kanal", "gdk", "glavni dovodni kanal",
		"o.k.", "odvodni kanal", "c.s.", "cs",
	)
	reLeadKind = regexp.MustCompile(`^(?:kanal|potok|bujica|retencija|akumulacija|nasip|rijeka|jezero)\s+`)
	reNonWord  = regexp.MustCompile(`[^a-z0-9]+`)
	reParen    = regexp.MustCompile(`\s*\([^)]*\)`)
)

// MatchKey je ključ za usporedbu naziva lokacija, za druge uvoznike
func MatchKey(name string) string { return normKey(name) }

// normKey svodi naziv na oblik za usporedbu, s vrstom vode: mala slova bez
// dijakritike, bez zagrade, kratice raspisane, razmaci i crte izjednačeni.
// "G.D.K. za CS Puškaš" i "Glavni dovodni kanal za CS Puškaš" daju isti ključ.
func normKey(name string) string {
	k := strings.ToLower(hydro.FoldDiacritics(strings.TrimSpace(name)))
	k = reParen.ReplaceAllString(k, "")
	k = abbreviations.Replace(k)
	return strings.TrimSpace(reNonWord.ReplaceAllString(k, " "))
}

// bareKey je ključ bez vrste vode s početka: "potok karasica" → "karasica"
func bareKey(name string) string {
	return strings.TrimSpace(reLeadKind.ReplaceAllString(normKey(name), ""))
}

// coreKey je jezgra imena bez vrste gdje god stajala: "odvodni kanal karasica"
// → "karasica"; služi za prijedloge kad točan ključ ne pogodi
func coreKey(name string) string {
	k := normKey(name)
	for _, w := range []string{"glavni dovodni kanal", "odvodni kanal", "spojni kanal", "lateralni kanal", "kanal", "potok", "bujica", "retencija", "akumulacija", "za cs", "cs"} {
		k = strings.ReplaceAll(k, w, " ")
	}
	return strings.Join(strings.Fields(k), " ")
}

// --- registar ---

type candidate struct {
	code, name string
	qualifier  string // pojašnjenje u zagradi službenog naziva
	decree     bool   // voda iz Odluke (ima kategoriju)
	structure  bool
}

func (c candidate) label() string { return c.code + " (" + c.name + ")" }

// index su vode i nasipi registra po ključevima
type index struct {
	full   map[string][]candidate // s vrstom: "potok karasica"
	bare   map[string][]candidate // bez vrste: "karasica"
	core   map[string][]candidate // jezgra
	emb    map[string][]candidate // nasipi područja
	byCode map[string]candidate
	area   models.Area
}

func (ix *index) add(m map[string][]candidate, k string, cnd candidate) {
	if k == "" {
		return
	}
	for _, e := range m[k] {
		if e.code == cnd.code {
			return
		}
	}
	m[k] = append(m[k], cnd)
}

func (ix *index) addWater(w models.Watercourse) {
	cnd := candidate{code: w.Code, name: w.OfficialName, qualifier: hydro.Qualifier(w.OfficialName), decree: w.Category != ""}
	ix.byCode[w.Code] = cnd
	ix.add(ix.full, normKey(w.OfficialName), cnd)
	ix.add(ix.full, normKey(w.Name), cnd)
	ix.add(ix.bare, bareKey(w.OfficialName), cnd)
	ix.add(ix.bare, bareKey(w.Name), cnd)
	ix.add(ix.core, coreKey(w.OfficialName), cnd)
}

func (ix *index) addEmbankment(s models.Structure) {
	cnd := candidate{code: s.ID.String(), name: s.Name, structure: true}
	ix.byCode[s.Code] = cnd
	ix.byCode[s.ID.String()] = cnd
	ix.add(ix.emb, normKey(s.Name), cnd)
	ix.add(ix.emb, bareKey(s.Name), cnd)
}

// pick bira među kandidatima istog ključa: jedan je jasan; među više njih
// pobjeđuje onaj čije pojašnjenje odgovara području ("potok Karašica (Baranja)"
// u Baranji), pa onaj iz Odluke. Inače je dvoznačno.
func (ix *index) pick(opts []candidate) (candidate, bool) {
	switch len(opts) {
	case 0:
		return candidate{}, false
	case 1:
		return opts[0], true
	}
	areaText := strings.ToLower(hydro.FoldDiacritics(ix.area.Name + " " + ix.area.VgiName))
	if one, ok := onlyOne(opts, func(c candidate) bool {
		q := strings.ToLower(hydro.FoldDiacritics(c.qualifier))
		return q != "" && (strings.Contains(areaText, q) || strings.Contains(q, strings.Fields(areaText)[len(strings.Fields(areaText))-1]))
	}); ok {
		return one, true
	}
	if one, ok := onlyOne(opts, func(c candidate) bool { return c.decree }); ok {
		return one, true
	}
	return candidate{}, false
}

func onlyOne(opts []candidate, match func(candidate) bool) (candidate, bool) {
	var found candidate
	n := 0
	for _, c := range opts {
		if match(c) {
			found = c
			n++
		}
	}
	return found, n == 1
}

// resolve odlučuje što je lokacija u registru
func (ix *index) resolve(l Location, embankment bool) Match {
	m := Match{Location: l, Structure: embankment}
	if embankment {
		opts := ix.emb[normKey(l.Name)]
		if len(opts) == 0 {
			opts = ix.emb[bareKey(l.Name)]
		}
		if cnd, ok := ix.pick(opts); ok {
			m.Code, m.Display, m.Status = cnd.code, cnd.name, StatusExisting
		} else if len(opts) > 1 {
			m.Status = StatusAmbiguous
			for _, x := range opts {
				m.Options = append(m.Options, x.name)
			}
		} else {
			m.Status = StatusNew
		}
		return m
	}

	// Razine: točan ključ s vrstom, pa bez vrste; tek onda prijedlozi
	for _, opts := range [][]candidate{ix.full[normKey(l.Name)], ix.bare[bareKey(l.Name)]} {
		if len(opts) == 0 {
			continue
		}
		if cnd, ok := ix.pick(opts); ok {
			m.Code, m.Display, m.Status = cnd.code, cnd.name, StatusExisting
			return m
		}
		m.Status = StatusAmbiguous
		for _, x := range opts {
			m.Options = append(m.Options, x.label())
		}
		return m
	}

	// Prijedlozi: jezgra imena, ili prvi dio složenog naziva
	// ("Kanal Bojana - GDK za CS Podunavlje" → "Kanal Bojana")
	seen := map[string]bool{}
	suggest := func(opts []candidate) {
		for _, x := range opts {
			if !seen[x.code] {
				seen[x.code] = true
				m.Options = append(m.Options, x.label())
			}
		}
	}
	if k := coreKey(l.Name); k != "" {
		suggest(ix.core[k])
	}
	for _, sep := range []string{" - ", " – ", " i "} {
		if first, _, ok := strings.Cut(l.Name, sep); ok {
			suggest(ix.full[normKey(first)])
			suggest(ix.bare[bareKey(first)])
			if k := coreKey(first); k != "" {
				suggest(ix.core[k])
			}
		}
	}
	if len(m.Options) > 0 {
		m.Status = StatusSuggested
	} else {
		m.Status = StatusNew
	}
	return m
}

// Ishodi uparivanja lokacije
const (
	StatusExisting  = "postoji"
	StatusNew       = "novo"
	StatusSuggested = "prijedlog"
	StatusAmbiguous = "dvoznačno"
)

// Deps su repozitoriji koje uvoz koristi
type Deps struct {
	Waters      *repository.WatercourseRepository
	Structures  *repository.StructureRepository
	Maintenance *repository.MaintenanceRepository
	Areas       []models.Area
}

// Options upravlja uvozom
type Options struct {
	Path     string
	DryRun   bool
	Aliases  map[string]string // naziv iz popisa → šifra vode ili objekta u registru
	AllItems bool              // uz korištene stavke upisati i cijeli ponudbeni troškovnik (bez cijena)
	Log      func(string, ...any)
	Deps     Deps
}

// Match je jedna lokacija i što je uvoz s njom odlučio
type Match struct {
	Location  Location
	Code      string // šifra vode ili identitet objekta u registru
	Display   string // naziv u registru
	Structure bool   // objekt (nasip), ne voda
	Status    string // Status*
	Options   []string
}

// Report je izvješće uvoza
type Report struct {
	Area          int
	AreaName      string
	Path          string
	Locations     []Match
	Existing      int // lokacija vezanih na postojeći zapis
	Created       int // novih voda i nasipa koje uvoz stvara
	Suggested     int // lokacija za koje ima kandidat, ali ne pouzdan
	Ambiguous     int
	ItemsTotal    int
	ItemsNew      int
	ItemsExisting int
	Blocks        int
	DryRun        bool
}

// Summary sažima izvješće u jedan redak
func (r Report) Summary() string {
	return fmt.Sprintf("BP %d, %d lokacija (%d postojećih, %d novih, %d prijedloga, %d dvoznačnih), %d stavki radova (%d novih, %d već upisanih), %d ugovornih stavki",
		r.Area, len(r.Locations), r.Existing, r.Created, r.Suggested, r.Ambiguous, r.ItemsTotal, r.ItemsNew, r.ItemsExisting, r.Blocks)
}

func (o *Options) logf(format string, args ...any) {
	if o.Log != nil {
		o.Log(format, args...)
	}
}

// Run pročita ugovor, upari lokacije s registrom i — bez probnog načina —
// upiše popis lokacija i stavke radova
func Run(ctx context.Context, o Options) (Report, error) {
	wb, err := xlsx.Open(o.Path)
	if err != nil {
		return Report{}, err
	}
	c, err := Parse(wb)
	if err != nil {
		return Report{}, fmt.Errorf("%s: %w", filepath.Base(o.Path), err)
	}
	c.Path = o.Path
	items := c.Items
	if o.AllItems {
		items = mergeItems(c.Items, c.Catalogue)
	}
	rep := Report{Area: c.AreaID, AreaName: c.AreaName, Path: o.Path, DryRun: o.DryRun, Blocks: len(c.Blocks), ItemsTotal: len(items)}
	o.logf("Ugovor %s: BP %d %s, %d lokacija, %d ugovornih stavki, %d različitih stavki radova",
		filepath.Base(o.Path), c.AreaID, c.AreaName, len(c.Locations), len(c.Blocks), len(items))

	var area *models.Area
	for i := range o.Deps.Areas {
		if o.Deps.Areas[i].ID == c.AreaID {
			area = &o.Deps.Areas[i]
		}
	}
	if area == nil {
		return rep, fmt.Errorf("branjeno područje %d ne postoji u registru", c.AreaID)
	}

	ix, err := buildIndex(ctx, o.Deps, *area)
	if err != nil {
		return rep, err
	}

	for _, l := range c.Locations {
		var m Match
		if alias, ok := o.Aliases[l.Name]; ok {
			cnd, ok := ix.byCode[alias]
			if !ok {
				return rep, fmt.Errorf("veza %q=%q: nema takve vode ni nasipa u registru", l.Name, alias)
			}
			m = Match{Location: l, Code: cnd.code, Display: cnd.name, Structure: cnd.structure, Status: StatusExisting}
		} else {
			m = ix.resolve(l, c.IsEmbankment(l))
		}
		switch m.Status {
		case StatusExisting:
			rep.Existing++
		case StatusNew:
			rep.Created++
		case StatusSuggested:
			rep.Suggested++
		case StatusAmbiguous:
			rep.Ambiguous++
		}
		rep.Locations = append(rep.Locations, m)
	}

	// Stavke radova: što već postoji, ostaje kako jest
	for _, it := range items {
		id := repository.WorkItemID(c.AreaID, models.WorkItemKey(it.Number, it.Description, it.Unit))
		if ex, err := o.Deps.Maintenance.GetItem(ctx, id); err != nil {
			return rep, err
		} else if ex != nil {
			rep.ItemsExisting++
		} else {
			rep.ItemsNew++
		}
	}

	if o.DryRun {
		return rep, nil
	}

	source := filepath.Base(o.Path)
	for i := range rep.Locations {
		m := &rep.Locations[i]
		l := m.Location
		if m.Status == StatusNew {
			if m.Structure {
				st := models.Structure{
					Code: fmt.Sprintf("bp%d-%s", c.AreaID, hydro.Slug(l.Name)), Name: l.Name,
					Kind: models.StructureKindEmbankment, SectorID: area.SectorID, AreaID: c.AreaID,
					Origin: models.StructureOriginContract,
				}
				if err := o.Deps.Structures.CreateStructure(ctx, &st); err != nil {
					return rep, err
				}
				m.Code, m.Display = st.ID.String(), st.Name
			} else {
				w := newWatercourse(l)
				if _, taken := ix.byCode[w.Code]; taken {
					w.Code = fmt.Sprintf("%s-bp%d", w.Code, c.AreaID)
				}
				if err := o.Deps.Waters.CreateWatercourse(ctx, &w); err != nil {
					return rep, err
				}
				ix.addWater(w)
				m.Code, m.Display = w.Code, w.OfficialName
			}
		}
		// Prijedlog i dvoznačno ostaju bez veze dok ih netko ne veže
		mw := models.MaintainedWater{
			AreaID: c.AreaID, Program: models.ProgramA02, Name: l.Name, Seq: l.Seq, Order: l.Order, Group: l.Group, Kind: l.Kind, Source: source,
		}
		if m.Structure {
			mw.StructureID = m.Code
		} else {
			mw.WatercourseCode = m.Code
		}
		if err := o.Deps.Maintenance.UpsertWater(ctx, &mw); err != nil {
			return rep, err
		}
	}

	for i, it := range items {
		id := repository.WorkItemID(c.AreaID, models.WorkItemKey(it.Number, it.Description, it.Unit))
		if ex, err := o.Deps.Maintenance.GetItem(ctx, id); err != nil {
			return rep, err
		} else if ex != nil {
			continue
		}
		wi := models.WorkItem{
			ID: id, AreaID: c.AreaID, Number: it.Number, Description: it.Description, Unit: it.Unit,
			Active: true, SortOrder: (i + 1) * 10, Origin: models.WorkItemOriginContract, Source: source,
		}
		if err := o.Deps.Maintenance.SaveItem(ctx, &wi); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

func buildIndex(ctx context.Context, d Deps, area models.Area) (*index, error) {
	ix := &index{
		full: map[string][]candidate{}, bare: map[string][]candidate{}, core: map[string][]candidate{},
		emb: map[string][]candidate{}, byCode: map[string]candidate{}, area: area,
	}
	waters, err := d.Waters.ListWatercourses(ctx, "", "", false)
	if err != nil {
		return nil, err
	}
	for _, w := range waters {
		ix.addWater(w)
	}
	embankments, err := d.Structures.ListStructures(ctx, "", area.ID, models.StructureKindEmbankment, "")
	if err != nil {
		return nil, err
	}
	for _, s := range embankments {
		ix.addEmbankment(s)
	}
	return ix, nil
}

// mergeItems spaja korištene stavke s cijelim troškovnikom; korištene idu prve
func mergeItems(used, all []Item) []Item {
	out := append([]Item(nil), used...)
	seen := map[string]bool{}
	for _, it := range used {
		seen[models.WorkItemKey(it.Number, it.Description, it.Unit)] = true
	}
	for _, it := range all {
		k := models.WorkItemKey(it.Number, it.Description, it.Unit)
		if !seen[k] {
			seen[k] = true
			out = append(out, it)
		}
	}
	return out
}

// newWatercourse gradi zapis vode koje registar nema: naziv iz popisa je
// službeni naziv, vrsta se čita s početka naziva ("Kanal Remetin" → kanal
// Remetin) ili iz razvrstavanja. Crtica u nazivu je dio imena ("Kanal Trokut
// - Jasenovac II"), ne opis prostiranja kao u opisu dionice.
func newWatercourse(l Location) models.Watercourse {
	name, kind := l.Name, ""
	if first, rest, ok := strings.Cut(l.Name, " "); ok && rest != "" {
		if k := hydro.NormalizeWaterKind(first); k != "" {
			name, kind = strings.TrimSpace(rest), k
		} else if strings.EqualFold(first, "bujica") {
			name, kind = strings.TrimSpace(rest), "bujica"
		}
	}
	if kind == "" {
		switch {
		case strings.Contains(strings.ToLower(l.Name), "kanal"):
			kind = "kanal"
		case l.Kind == models.MaintenanceKindTorrent:
			kind = "bujica"
		case l.Kind == models.MaintenanceKindDrainage:
			kind = "kanal"
		}
	}
	return models.Watercourse{
		Code: hydro.WatercourseCode(l.Name), OfficialName: l.Name, Name: name, Kind: kind,
		Origin: models.WatercourseOriginContract,
	}
}
