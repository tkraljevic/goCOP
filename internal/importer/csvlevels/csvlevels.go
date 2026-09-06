// Package csvlevels uvozi dnevne vodostaje iz tablice kakvu Centar obrane od
// poplava vodi u Excelu: stupci su postaje, redci datumi, a u ćeliji je
// jutarnje očitanje. Takva tablica nastaje godinama i vrijedi više od bilo
// koje baze, jer u njoj već stoji izbor postaja koje centar zaista prati.
//
// Uvoz ništa ne pretpostavlja o urednosti datoteke: razdjelnik, kodiranje i
// oblik datuma prepoznaje sam, prazne ćelije preskače, a stupce koje ne zna
// vezati na registar prijavljuje umjesto da pogađa.
//
// Identitet očitanja izvodi se iz letve i trenutka, ne iz datoteke. Isti
// jutarnji vodostaj iz drugog izvora dobiva isti identitet, pa se ne
// udvostručuje; ako se vrijednosti razlikuju, uvoz to prijavi i ostavi
// zatečeni zapis na miru.
package csvlevels

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gocop/internal/db"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Options su postavke jednog uvoza
type Options struct {
	Path     string            // datoteka
	Hour     int               // sat očitanja (zadano 7)
	Minute   int               // minuta očitanja
	Origin   string            // odakle tablica potječe, za trag u zapisu
	Source   string            // način: UVOZ kad se ne zna je li ručno ili automatski
	DryRun   bool              // samo pokaži što bi se dogodilo
	Skip     []string          // stupci koje namjerno preskačemo (npr. protoci)
	Aliases  map[string]string // stupac → šifra letve, kad naziv nije dovoljan
	Log      func(string, ...any)
	Deps     Deps
	MaxShown int // koliko primjera razlika ispisati
}

// Deps su spremišta koja uvoz treba
type Deps struct {
	Readings   *repository.ReadingRepository
	Stations   *repository.StationRepository
	Structures *repository.StructureRepository
}

// Column je jedan stupac tablice preslikan na letvu iz registra
type Column struct {
	Header string
	Name   string // naziv letve u registru
	Key    string // GaugeKey
	Values int
}

// Report je sažetak uvoza
type Report struct {
	Rows      int
	Matched   []Column
	Unmatched []string
	Ambiguous []string
	Skipped2  []string // namjerno preskočeni stupci
	Inserted  int
	Skipped   int // isti dan na istoj letvi već postoji
	Conflicts int // ... i vrijednost se razlikuje
	Differs   []Difference
	BadDates  int
	BadValues int
	From, To  time.Time
	DryRun    bool
}

// Difference je isto jutro na istoj letvi s drukčijom vrijednošću u drugom
// izvoru. Uspoređuje se dan, ne trenutak: tablica centra nosi fiksnih sedam
// sati, a očitanje s terena vrijeme kad je čovjek stvarno bio na letvi.
type Difference struct {
	Gauge  string
	Day    time.Time
	Have   int
	HaveAt time.Time
	From   string // podrijetlo zatečenog zapisa
	New    int
}

func (o *Options) logf(format string, args ...any) {
	if o.Log != nil {
		o.Log(format, args...)
	}
}

// Run pročita datoteku i upiše očitanja
func Run(ctx context.Context, o Options) (Report, error) {
	rep := Report{DryRun: o.DryRun}
	if o.Hour == 0 && o.Minute == 0 {
		o.Hour = 7
	}
	if o.Source == "" {
		o.Source = models.ReadingSourceImport
	}
	if o.MaxShown == 0 {
		o.MaxShown = 10
	}

	raw, err := os.ReadFile(o.Path)
	if err != nil {
		return rep, err
	}
	text := decode(raw)
	records, sep, err := readCSV(text)
	if err != nil {
		return rep, err
	}
	o.logf("Uvoz tablice: %s, razdjelnik %q, redaka %d", o.Path, sep, len(records))
	if len(records) < 2 {
		return rep, fmt.Errorf("%s: tablica nema ni zaglavlje ni jedan redak", o.Path)
	}

	header := records[0]
	if len(header) < 2 {
		return rep, fmt.Errorf("%s: očekivan je stupac s datumom i barem jedan stupac postaje", o.Path)
	}

	gauges, err := loadGauges(ctx, o.Deps)
	if err != nil {
		return rep, err
	}

	// Preslikavanje stupaca na letve; prvi stupac je datum
	cols := map[int]*Column{}
	for i := 1; i < len(header); i++ {
		name := strings.TrimSpace(header[i])
		if name == "" {
			continue
		}
		if skipped(o.Skip, name) {
			rep.Skipped2 = append(rep.Skipped2, name)
			continue
		}
		found, ambiguous := gauges.match(name)
		if found == nil {
			if code := alias(o.Aliases, name); code != "" {
				found = gauges.byCode[code]
				ambiguous = false
				if found == nil {
					rep.Unmatched = append(rep.Unmatched, name+" (šifra "+code+" nije u registru)")
					continue
				}
			}
		}
		switch {
		case ambiguous:
			rep.Ambiguous = append(rep.Ambiguous, name)
		case found == nil:
			rep.Unmatched = append(rep.Unmatched, name)
		default:
			cols[i] = &Column{Header: name, Name: found.name, Key: found.key}
		}
	}
	if len(cols) == 0 {
		return rep, fmt.Errorf("%s: nijedan stupac nije prepoznat kao letva iz registra", o.Path)
	}

	var batch []models.Reading
	for _, row := range records[1:] {
		if len(row) == 0 {
			continue
		}
		day, ok := parseDate(strings.TrimSpace(row[0]))
		if !ok {
			if strings.TrimSpace(row[0]) != "" {
				rep.BadDates++
			}
			continue
		}
		rep.Rows++
		at := time.Date(day.Year(), day.Month(), day.Day(), o.Hour, o.Minute, 0, 0, models.Zagreb)
		if rep.From.IsZero() || at.Before(rep.From) {
			rep.From = at
		}
		if at.After(rep.To) {
			rep.To = at
		}
		for i, col := range cols {
			if i >= len(row) {
				continue
			}
			cm, status := parseLevel(row[i])
			if status != levelOK {
				if status == levelBad {
					rep.BadValues++
				}
				continue
			}
			col.Values++
			rd := models.Reading{
				ID:         db.StableID("reading-dnevni", col.Key+"|"+at.UTC().Format(time.RFC3339)),
				MeasuredAt: at.UTC(),
				LevelCm:    &cm,
				Source:     o.Source,
				Origin:     o.Origin,
				SourceRef:  "csv:" + col.Header + ":" + day.Format("2006-01-02"),
				CreatedAt:  time.Now().UTC(),
			}
			rd.UpdatedAt = rd.CreatedAt
			if strings.HasPrefix(col.Key, "structure:") {
				rd.StructureID = strings.TrimPrefix(col.Key, "structure:")
			} else {
				rd.StationID = strings.TrimPrefix(col.Key, "station:")
			}
			batch = append(batch, rd)
		}
	}
	for _, c := range cols {
		rep.Matched = append(rep.Matched, *c)
	}
	sort.Slice(rep.Matched, func(i, j int) bool { return rep.Matched[i].Header < rep.Matched[j].Header })

	// Što od toga već stoji u bazi, i gdje se izvori razilaze. Usporedba ide
	// po danu i letvi, jer isto jutro iz dva izvora nosi različit sat.
	var stationIDs, structureIDs []string
	for _, c := range cols {
		if id := strings.TrimPrefix(c.Key, "structure:"); id != c.Key {
			structureIDs = append(structureIDs, id)
		} else {
			stationIDs = append(stationIDs, strings.TrimPrefix(c.Key, "station:"))
		}
	}
	known, err := o.Deps.Readings.ListForGauges(ctx, stationIDs, structureIDs,
		rep.From.AddDate(0, 0, -1), rep.To.AddDate(0, 0, 1))
	if err != nil {
		return rep, err
	}
	byDay := map[string]models.Reading{}
	for _, rd := range known {
		if rd.LevelCm == nil {
			continue
		}
		k := rd.GaugeKey() + "|" + rd.LocalTime().Format("2006-01-02")
		if have, ok := byDay[k]; !ok || rd.MeasuredAt.Before(have.MeasuredAt) {
			byDay[k] = rd // jutarnje očitanje je ono ranije u danu
		}
	}

	var fresh []models.Reading
	for _, rd := range batch {
		k := rd.GaugeKey() + "|" + rd.LocalTime().Format("2006-01-02")
		have, ok := byDay[k]
		if !ok {
			fresh = append(fresh, rd)
			continue
		}
		rep.Skipped++
		if *have.LevelCm != *rd.LevelCm {
			rep.Conflicts++
			if len(rep.Differs) < o.MaxShown {
				rep.Differs = append(rep.Differs, Difference{
					Gauge: gaugeName(cols, rd), Day: rd.LocalTime(), Have: *have.LevelCm,
					HaveAt: have.LocalTime(), From: have.OriginLabel(), New: *rd.LevelCm,
				})
			}
		}
	}

	if o.DryRun {
		rep.Inserted = len(fresh)
		return rep, nil
	}
	for start := 0; start < len(fresh); start += 2000 {
		end := start + 2000
		if end > len(fresh) {
			end = len(fresh)
		}
		n, err := o.Deps.Readings.ImportBatch(ctx, fresh[start:end])
		rep.Inserted += n
		if err != nil {
			return rep, err
		}
		o.logf("Uvoz tablice: upisano %d od %d", rep.Inserted, len(fresh))
	}
	return rep, nil
}

// skipped javlja je li stupac na popisu onih koje namjerno ne uvozimo.
// Tablica centra uz vodostaje nosi i srednje dnevne protoke u kubnim
// metrima u sekundi; oni nisu centimetri i ne smiju završiti kao vodostaj.
func skipped(list []string, header string) bool {
	k := normalize(header)
	for _, s := range list {
		if normalize(s) == k {
			return true
		}
	}
	return false
}

func gaugeName(cols map[int]*Column, rd models.Reading) string {
	key := rd.GaugeKey()
	for _, c := range cols {
		if c.Key == key {
			return c.Name
		}
	}
	return key
}

// --- letve iz registra ---

type gauge struct {
	name      string
	key       string
	stationID string // kod objekta: vodomjer koji mu pripada
}

type gaugeIndex struct {
	byName map[string][]gauge
	byCode map[string]*gauge
}

// columnAliases su kratice kojima tablica Centra obrane od poplava imenuje
// stupce. Nazivi su tamo skraćeni do neprepoznatljivosti ("dMiholjac"), pa
// se vežu popisom umjesto pogađanjem.
var columnAliases = map[string]string{
	"gradgona":   "gornja-radgona",
	"msredisce":  "mursko-sredisce",
	"nvirje":     "novo-virje",
	"tpolje":     "terezino-polje",
	"dmiholjac":  "donji-miholjac",
	"lavamund g": "lavamund",
	"ter polje":  "terezino-polje",
	"n virje":    "novo-virje",
	"d miholjac": "donji-miholjac",
	"m sredisce": "mursko-sredisce",
	"g radgona":  "gornja-radgona",
}

// alias traži šifru letve za stupac: prvo u popisu koji je zadao operater,
// pa u ugrađenom popisu kratica
func alias(zadane map[string]string, header string) string {
	k := normalize(header)
	for h, code := range zadane {
		if normalize(h) == k {
			return code
		}
	}
	return columnAliases[k]
}

func loadGauges(ctx context.Context, d Deps) (*gaugeIndex, error) {
	idx := &gaugeIndex{byName: map[string][]gauge{}, byCode: map[string]*gauge{}}
	stations, err := d.Stations.ListStations(ctx, "", "", false)
	if err != nil {
		return nil, err
	}
	for _, st := range stations {
		g := gauge{name: st.Name, key: "station:" + st.ID.String()}
		idx.byCode[st.Code] = &gauge{name: st.Name, key: g.key}
		idx.add(st.Name, g)
		idx.add(st.Code, g)
		if st.Watercourse != "" {
			idx.add(st.Watercourse+" "+st.Name, g)
			idx.add(st.Watercourse+" - "+st.Name, g)
		}
	}
	structures, err := d.Structures.ListStructures(ctx, "", 0, "", "")
	if err != nil {
		return nil, err
	}
	for _, s := range structures {
		g := gauge{name: s.Name, key: "structure:" + s.ID.String(), stationID: s.StationID}
		if _, ima := idx.byCode[s.Code]; !ima {
			idx.byCode[s.Code] = &gauge{name: s.Name, key: g.key}
		}
		idx.add(s.Name, g)
		idx.add(s.Code, g)
	}
	return idx, nil
}

func (i *gaugeIndex) add(name string, g gauge) {
	k := normalize(name)
	if k == "" {
		return
	}
	for _, have := range i.byName[k] {
		if have.key == g.key {
			return
		}
	}
	i.byName[k] = append(i.byName[k], g)
}

// match traži letvu po nazivu stupca. Tablica centra piše naziv na svoj
// način — "Dunav - Batina", "Batina (DHMZ)", "P. Karašica - Popovac" — pa se
// pokušava cijeli naziv, pa dio iza crte, pa dio ispred zagrade.
func (i *gaugeIndex) match(header string) (*gauge, bool) {
	for _, kandidat := range variants(header) {
		hits := i.byName[kandidat]
		switch len(hits) {
		case 1:
			return &hits[0], false
		case 0:
			continue
		}
		// Objekt i njegov vodomjer često nose isto ime ("CS Draž"). To je
		// ista letva na terenu, a očitanja stoje uz objekt, pa se bira on —
		// inače bi isto jutro završilo na dvije letve i nikad se ne bi
		// prepoznalo kao isti podatak.
		if g := preferStructure(hits); g != nil {
			return g, false
		}
		return nil, true
	}
	return nil, false
}

// variants su oblici naziva kojima se pokušava pogoditi letva
func variants(header string) []string {
	out := []string{normalize(header)}
	raw := strings.TrimSpace(header)
	if i := strings.Index(raw, "("); i > 0 {
		out = append(out, normalize(raw[:i]))
	}
	for _, sep := range []string{" - ", " – ", "-", "–", "/"} {
		if i := strings.LastIndex(raw, sep); i > 0 && i+len(sep) < len(raw) {
			out = append(out, normalize(raw[i+len(sep):]))
			break
		}
	}
	var uniq []string
	seen := map[string]bool{}
	for _, v := range out {
		if v != "" && !seen[v] {
			seen[v] = true
			uniq = append(uniq, v)
		}
	}
	return uniq
}

// preferStructure bira objekt kad su kandidati objekt i njegov vlastiti vodomjer
func preferStructure(hits []gauge) *gauge {
	var structure *gauge
	stations := map[string]bool{}
	for idx := range hits {
		if strings.HasPrefix(hits[idx].key, "structure:") {
			if structure != nil {
				return nil // dva objekta istog imena: neka odluči čovjek
			}
			structure = &hits[idx]
		} else {
			stations[strings.TrimPrefix(hits[idx].key, "station:")] = true
		}
	}
	if structure == nil || structure.stationID == "" || len(hits) != len(stations)+1 {
		return nil
	}
	if stations[structure.stationID] {
		return structure
	}
	return nil
}

var diacritics = strings.NewReplacer(
	"č", "c", "ć", "c", "ž", "z", "š", "s", "đ", "d", "dž", "dz",
	"Č", "c", "Ć", "c", "Ž", "z", "Š", "s", "Đ", "d",
)

func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = diacritics.Replace(s)
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r == ' ', r == '-', r == '.', r == '(', r == ')', r == '/':
			return ' '
		}
		return -1
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// --- čitanje datoteke ---

// decode vraća tekst kao UTF-8; tablice iz Excela na Windowsima obično su
// zapisane u windows-1250
func decode(raw []byte) string {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(raw) {
		return string(raw)
	}
	var sb strings.Builder
	for _, b := range raw {
		if b < 0x80 {
			sb.WriteByte(b)
			continue
		}
		sb.WriteRune(cp1250[b-0x80])
	}
	return sb.String()
}

// cp1250 je gornja polovica windows-1250
var cp1250 = [128]rune{
	'€', '�', '‚', '�', '„', '…', '†', '‡', '�', '‰', 'Š', '‹', 'Ś', 'Ť', 'Ž', 'Ź',
	'�', '‘', '’', '“', '”', '•', '–', '—', '�', '™', 'š', '›', 'ś', 'ť', 'ž', 'ź',
	' ', 'ˇ', '˘', 'Ł', '¤', 'Ą', '¦', '§', '¨', '©', 'Ş', '«', '¬', '­', '®', 'Ż',
	'°', '±', '˛', 'ł', '´', 'µ', '¶', '·', '¸', 'ą', 'ş', '»', 'Ľ', '˝', 'ľ', 'ż',
	'Ŕ', 'Á', 'Â', 'Ă', 'Ä', 'Ĺ', 'Ć', 'Ç', 'Č', 'É', 'Ę', 'Ë', 'Ě', 'Í', 'Î', 'Ď',
	'Đ', 'Ń', 'Ň', 'Ó', 'Ô', 'Ő', 'Ö', '×', 'Ř', 'Ů', 'Ú', 'Ű', 'Ü', 'Ý', 'Ţ', 'ß',
	'ŕ', 'á', 'â', 'ă', 'ä', 'ĺ', 'ć', 'ç', 'č', 'é', 'ę', 'ë', 'ě', 'í', 'î', 'ď',
	'đ', 'ń', 'ň', 'ó', 'ô', 'ő', 'ö', '÷', 'ř', 'ů', 'ú', 'ű', 'ü', 'ý', 'ţ', '˙',
}

// readCSV čita tablicu s razdjelnikom kakav zatekne
func readCSV(text string) ([][]string, string, error) {
	sep := detectSeparator(text)
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = sep
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil && !errIsPartial(err) {
		return nil, string(sep), err
	}
	return rows, string(sep), nil
}

func errIsPartial(err error) bool { return err == io.EOF }

func detectSeparator(text string) rune {
	line := text
	if i := strings.IndexAny(text, "\r\n"); i > 0 {
		line = text[:i]
	}
	best, bestN := ';', strings.Count(line, ";")
	for _, c := range []struct {
		r rune
		n int
	}{{',', strings.Count(line, ",")}, {'\t', strings.Count(line, "\t")}} {
		if c.n > bestN {
			best, bestN = c.r, c.n
		}
	}
	return best
}

// parseDate čita datum u oblicima kakve Excel izvozi, uključujući redni broj dana
func parseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		"2.1.2006.", "02.01.2006.", "2.1.2006", "02.01.2006",
		"2006-01-02", "02/01/2006", "2.1.06", "02.01.06",
		"2006-01-02 15:04:05", "02.01.2006 15:04",
	} {
		if t, err := time.ParseInLocation(layout, s, models.Zagreb); err == nil {
			return t, true
		}
	}
	// Excel zna izvesti datum kao broj dana od 30.12.1899.
	if n, err := strconv.Atoi(s); err == nil && n > 20000 && n < 80000 {
		return time.Date(1899, 12, 30, 0, 0, 0, 0, models.Zagreb).AddDate(0, 0, n), true
	}
	return time.Time{}, false
}

type levelStatus int

const (
	levelOK    levelStatus = iota
	levelBlank             // prazna ćelija ili oznaka da tog jutra nije očitano
	levelBad               // nešto piše, ali se ne da pročitati
)

// parseLevel čita vodostaj u centimetrima. Prazna ćelija i crtica nisu nula
// nego izostanak očitanja, i to nije greška u tablici.
func parseLevel(s string) (int, levelStatus) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(strings.TrimSpace(strings.TrimSuffix(s, "cm")), " ")
	switch strings.ToLower(s) {
	case "", "-", "--", "/", "x", "n/a", ".", "nema", "led", "suho":
		return 0, levelBlank
	}
	// Hrvatski izvoz piše decimalni zarez i točku za tisućice, ali DHMZ-ovi
	// standardizirani CSV-ovi koriste decimalnu točku (npr. 169.0). Jedina
	// točka s jednom ili dvije znamenke iza nje zato je decimalna; zapis 1.234
	// ostaje tisuću dvjesto trideset četiri centimetra.
	if strings.Contains(s, ",") {
		s = strings.ReplaceAll(s, ".", "")
		s = strings.ReplaceAll(s, ",", ".")
	} else if strings.Count(s, ".") == 1 {
		parts := strings.SplitN(s, ".", 2)
		if len(parts[1]) == 3 {
			s = parts[0] + parts[1]
		}
	} else {
		s = strings.ReplaceAll(s, ".", "")
	}
	s = strings.ReplaceAll(s, " ", "")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, levelBad
	}
	return int(math.Round(f)), levelOK
}

// Summary je izvješće za dnevnik
func (r Report) Summary() string {
	var sb strings.Builder
	if r.DryRun {
		sb.WriteString("PROBNI PROLAZ (ništa nije upisano) — ")
	}
	fmt.Fprintf(&sb, "redaka %d, letvi %d, novih očitanja %d, već zapisano %d (od toga %d s drukčijom vrijednošću)",
		r.Rows, len(r.Matched), r.Inserted, r.Skipped, r.Conflicts)
	if !r.From.IsZero() {
		fmt.Fprintf(&sb, ", razdoblje %s – %s", r.From.Format("02.01.2006."), r.To.Format("02.01.2006."))
	}
	if r.BadDates > 0 {
		fmt.Fprintf(&sb, ", nečitljivih datuma %d", r.BadDates)
	}
	if r.BadValues > 0 {
		fmt.Fprintf(&sb, ", nečitljivih vrijednosti %d", r.BadValues)
	}
	return sb.String()
}
