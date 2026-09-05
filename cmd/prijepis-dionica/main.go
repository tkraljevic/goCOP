// prijepis-dionica iz wikija (VodaAI-LLM-WIKI) stvara data/sections.json, prijepis dionica uz bazu
// u građi kakvu program vodi: dionica → poddionice, a u poddionici objekti,
// nasipi, ugroženo područje i vodomjeri. Redak Privitka je jedan nasip;
// uzastopni redci iste vode (lijevi pa desni nasip) čine jednu poddionicu.
//
// Alat je u repozitoriju da se prijepis može ponoviti kad se Privitak
// promijeni, a ne da ga netko opet radi rukom i opet nešto izgubi.
//
//	go run ./cmd/prijepis-dionica -wiki ~/projekti/VodaAI-LLM-WIKI/WIKI/hrvatske-vode/teritorijalne-jedinice
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gocop/internal/hydro"
	"gocop/internal/models"
)

func main() {
	wiki := flag.String("wiki", "", "mapa s podrucje-*.md datotekama iz wikija")
	out := flag.String("out", "data/sections.json", "gdje zapisati prijepis")
	flag.Parse()
	if *wiki == "" {
		log.Fatal("zadajte -wiki mapu")
	}
	files, err := filepath.Glob(filepath.Join(*wiki, "podrucje-*.md"))
	if err != nil || len(files) == 0 {
		log.Fatalf("nema podrucje-*.md u %s", *wiki)
	}
	sort.Strings(files)

	var all []models.Section
	stats := map[string]int{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			log.Fatal(err)
		}
		for _, s := range parseFile(string(raw)) {
			all = append(all, s)
			stats["dionica"]++
			for _, p := range s.Parts {
				stats["poddionica"]++
				stats["objekata"] += len(p.Objects)
				stats["nasipa"] += len(p.Embankments)
				stats["vodomjera"] += len(p.Gauges)
				if p.Unaligned {
					stats["neporavnano"]++
				}
			}
		}
	}
	// Redoslijed ostaje kakav je bio: datoteke abecedno (područje 1, 10, 11…),
	// dionice redom iz datoteke. Punjenje postaja šifru izvodi iz prvog
	// viđenog naziva vodomjera, pa bi drukčiji redoslijed promijenio šifre, a
	// s njima i identitete postaja koje drugi čvorovi već imaju.

	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("zapisano %s: %d dionica, %d poddionica, %d objekata, %d nasipa, %d vodomjera, %d poddionica s upozorenjem o poravnanju",
		*out, stats["dionica"], stats["poddionica"], stats["objekata"], stats["nasipa"], stats["vodomjera"], stats["neporavnano"])
}

// parseFile čita jednu datoteku područja i vraća njezine dionice
func parseFile(text string) []models.Section {
	var out []models.Section
	blocks := strings.Split(text, "\n## Dionica ")
	for _, b := range blocks[1:] {
		nl := strings.IndexByte(b, '\n')
		if nl < 0 {
			continue
		}
		code := strings.TrimSuffix(strings.TrimSpace(b[:nl]), ".")
		out = append(out, parseSection(code, b[nl:]))
	}
	return out
}

// row je jedan redak tablice Privitka dok se skuplja: voda i što uz nju stoji
type row struct {
	vodotok     string
	embankments []models.PartEmbankment
	objects     []models.PartObject
	protected   string
	gauges      []models.GaugeItem
}

var (
	reTotalLength = regexp.MustCompile(`\*\*Ukupna duljina dionice:\*\*\s*([\d.,]+)\s*km`)
	reTotalEmb    = regexp.MustCompile(`\*\*Ukupno nasipa:\*\*\s*([\d.,]+)\s*km`)
)

func parseSection(code, body string) models.Section {
	sec := models.Section{Code: code}
	sec.SectorID, sec.AreaID = sectorAndArea(code)
	unaligned := strings.Contains(body, "colspan")
	if m := reTotalLength.FindStringSubmatch(body); m != nil {
		if v, ok := hydro.ParseKm(m[1]); ok {
			sec.LengthKm = &v
		}
	}
	if m := reTotalEmb.FindStringSubmatch(body); m != nil {
		if v, ok := hydro.ParseKm(m[1]); ok {
			sec.EmbankmentKm = &v
		}
	}

	var rows []row
	var cur *row
	chunks := strings.Split(body, "\n### ")
	for _, ch := range chunks[1:] {
		nl := strings.IndexByte(ch, '\n')
		heading, content := ch, ""
		if nl >= 0 {
			heading, content = ch[:nl], ch[nl+1:]
		}
		heading = strings.TrimSpace(heading)
		content = strings.TrimSpace(content)
		switch heading {
		case "Vodotok":
			rows = append(rows, row{vodotok: strings.Join(strings.Fields(content), " ")})
			cur = &rows[len(rows)-1]
		case "Objekti na dionici":
			if cur == nil {
				continue
			}
			for _, cells := range tableRows(content, "stacion") {
				name := ""
				if len(cells) > 1 {
					name = cells[1]
				}
				cur.objects = append(cur.objects, parseObject(cells[0], name))
			}
		case "Nasipi":
			if cur == nil {
				continue
			}
			for _, cells := range tableRows(content, "naziv") {
				data := ""
				if len(cells) > 1 {
					data = cells[1]
				}
				cur.embankments = append(cur.embankments, parseEmbankment(cells[0], data))
			}
		case "Ugroženo područje":
			if cur == nil {
				continue
			}
			cur.protected = protectedText(content)
		case "Vodomjeri i kriteriji":
			if cur == nil {
				continue
			}
			cur.gauges = append(cur.gauges, parseGauges(content)...)
		}
	}

	// poddionice: uzastopni redci iste vode se spajaju; objekt po nasipu
	// pamti nasip svog retka kad je redak imao točno jedan
	for _, r := range rows {
		for i := range r.embankments {
			distinctDamName(&r.embankments[i], r.vodotok)
		}
		for i := range r.objects {
			if r.objects[i].StationingKind == hydro.StationingEmbankment && len(r.embankments) == 1 {
				r.objects[i].OnEmbankment = r.embankments[0].Name
			}
		}
		if n := len(sec.Parts); n > 0 && sec.Parts[n-1].Description == r.vodotok {
			p := &sec.Parts[n-1]
			p.Embankments = append(p.Embankments, r.embankments...)
			p.Objects = append(p.Objects, r.objects...)
			p.Gauges = mergeGauges(p.Gauges, r.gauges)
			p.ProtectedText = mergeProtected(p.ProtectedText, r.protected)
			continue
		}
		d := hydro.ParseSectionDescription(r.vodotok)
		p := models.SectionPart{
			Seq: len(sec.Parts) + 1, Description: r.vodotok, Bank: d.Bank, StationingKind: d.Kind, Extent: d.Extent,
			ProtectedText: r.protected, Unaligned: unaligned,
			Embankments: r.embankments, Objects: r.objects, Gauges: mergeGauges(nil, r.gauges),
		}
		if d.HasRange {
			from, to := d.RkmFrom, d.RkmTo
			p.KmFrom, p.KmTo = &from, &to
		}
		if d.LengthKm > 0 {
			l := d.LengthKm
			p.LengthKm = &l
		}
		sec.Parts = append(sec.Parts, p)
	}
	if sec.Parts == nil {
		sec.Parts = []models.SectionPart{}
	}
	var descs []string
	for _, p := range sec.Parts {
		descs = append(descs, p.Description)
	}
	sec.Description = strings.Join(descs, " · ")
	return sec
}

var reRetention = regexp.MustCompile(`(?i)\b(retencij[ae]|akumulacij[ae]|jezer[oa])\s+([^;,()]+)`)

// distinctDamName daje generičkoj brani ime njezine retencije ili
// akumulacije: šest retencija jednog potoka ima šest "nasutih homogenih
// zemljanih brana", a u registru su to različite građevine. Vrsta brane iz
// dokumentacije seli u podatke nasipa.
func distinctDamName(e *models.PartEmbankment, vodotok string) {
	low := strings.ToLower(e.Name)
	if !strings.Contains(low, "brana") {
		return
	}
	m := reRetention.FindStringSubmatch(vodotok)
	if m == nil {
		return
	}
	what := strings.TrimSpace(m[2])
	if what == "" || strings.Contains(low, strings.ToLower(what)) {
		return
	}
	kind := strings.ToLower(m[1])
	switch {
	case strings.HasPrefix(kind, "akumul"):
		kind = "akumulacije"
	case strings.HasPrefix(kind, "jezer"):
		kind = "jezera"
	default:
		kind = "retencije"
	}
	if e.Data != "" {
		e.Data = strings.TrimSpace(e.Name) + "; " + e.Data
	} else {
		e.Data = strings.TrimSpace(e.Name)
	}
	e.Name = fmt.Sprintf("Brana %s %s", kind, what)
}

// parseObject čita objekt iz stacionaže i naziva: vrstu i kilometražu iz
// stacionaže, obalu s početka naziva
func parseObject(stationing, name string) models.PartObject {
	o := models.PartObject{StationingText: strings.TrimSpace(stationing)}
	o.Bank, o.Name = hydro.ParseObjectBank(name)
	if m := kindRe.FindStringSubmatch(o.StationingText); m != nil {
		o.StationingKind = hydro.NormalizeStationingKind(m[1])
	}
	if km, ok := hydro.ParseStationingKm(o.StationingText); ok {
		o.Stationing = &km
	} else if km, ok := hydro.ParseKm(o.StationingText); ok && strings.Contains(o.StationingText, "+") {
		o.Stationing = &km
	}
	return o
}

// parseEmbankment čita nasip: naziv i odsjek uz vodu i po nasipu
func parseEmbankment(name, data string) models.PartEmbankment {
	e := models.PartEmbankment{Name: strings.TrimSpace(name), Data: strings.TrimSpace(data)}
	d := hydro.ParseEmbankmentData(data)
	if d.HasWater {
		from, to := d.WaterFrom, d.WaterTo
		e.WaterKind, e.WaterFrom, e.WaterTo = d.WaterKind, &from, &to
	}
	if d.HasEmb {
		from, to := d.EmbFrom, d.EmbTo
		e.EmbFrom, e.EmbTo = &from, &to
	}
	if d.LengthKm > 0 {
		l := d.LengthKm
		e.LengthKm = &l
	}
	return e
}

// mergeGauges dodaje vodomjere bez ponavljanja
func mergeGauges(have, add []models.GaugeItem) []models.GaugeItem {
	seen := map[string]bool{}
	for _, g := range have {
		seen[g.StationName+"|"+g.PrepCm+"|"+g.RegularCm+"|"+g.EmergCm+"|"+g.CriticalCm] = true
	}
	for _, g := range add {
		k := g.StationName + "|" + g.PrepCm + "|" + g.RegularCm + "|" + g.EmergCm + "|" + g.CriticalCm
		if !seen[k] {
			seen[k] = true
			have = append(have, g)
		}
	}
	return have
}

// mergeProtected spaja ugrožena područja dvaju redaka iste vode
func mergeProtected(a, b string) string {
	p := newProtectedMerge()
	p.add(a)
	p.add(b)
	return p.String()
}

// sectorAndArea iz šifre "B.34.14" čita sektor B i područje 34
func sectorAndArea(code string) (string, int) {
	parts := strings.Split(code, ".")
	if len(parts) < 2 {
		return "", 0
	}
	var area int
	fmt.Sscanf(parts[1], "%d", &area)
	return parts[0], area
}

// tableRows vraća retke markdown tablice bez zaglavlja i crte ispod njega
func tableRows(content, headerWord string) [][]string {
	var out [][]string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.HasPrefix(line, "|---") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if len(cells) == 0 || strings.HasPrefix(strings.ToLower(cells[0]), headerWord) {
			continue
		}
		if cells[0] == "" && (len(cells) < 2 || cells[1] == "") {
			continue
		}
		out = append(out, cells)
	}
	return out
}

var kindRe = regexp.MustCompile(`^([a-zA-Z]+)\s*[\d]`)

var (
	gaugeLineRe = regexp.MustCompile(`^(.+?);\s*P\s*=\s*([^;]+);\s*R\s*=\s*([^;]+);\s*I\s*=\s*([^;]+);\s*IS\s*=\s*([^;]+?)(?:;\s*M\s*=\s*(.+))?$`)
	mnmRe       = regexp.MustCompile(`^(.+?);\s*R:\s*([^;]+);\s*I:\s*([^;]+);?\s*(.*)$`)
)

// parseGauges čita vodomjere iz tablice ili iz proznog retka.
// Prozni oblik: "Batina , rkm 1.424,85 (80,450); P = +300; R = +500; I = +650; IS = +800; M = +775 (14.06.2013.)"
func parseGauges(content string) []models.GaugeItem {
	var out []models.GaugeItem
	for _, cells := range tableRows(content, "vodomjer") {
		g := models.GaugeItem{StationName: cells[0]}
		get := func(i int) string {
			if i < len(cells) {
				return cells[i]
			}
			return ""
		}
		g.PrepCm, g.RegularCm, g.EmergCm, g.CriticalCm, g.RecordCm, g.Notes = get(1), get(2), get(3), get(4), get(5), get(6)
		if g.StationName != "" || g.Notes != "" {
			out = append(out, g)
		}
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "|") || strings.HasPrefix(line, ">") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if m := gaugeLineRe.FindStringSubmatch(line); m != nil {
			out = append(out, models.GaugeItem{
				StationName: strings.TrimSpace(m[1]), PrepCm: strings.TrimSpace(m[2]), RegularCm: strings.TrimSpace(m[3]),
				EmergCm: strings.TrimSpace(m[4]), CriticalCm: strings.TrimSpace(m[5]), RecordCm: strings.TrimSpace(m[6]),
				FromText: true,
			})
			continue
		}
		if m := mnmRe.FindStringSubmatch(line); m != nil {
			out = append(out, models.GaugeItem{
				StationName: strings.TrimSpace(m[1]), RegularCm: strings.TrimSpace(m[2]),
				EmergCm: strings.TrimSpace(m[3]), Notes: strings.TrimSpace(m[4]), FromText: true,
			})
			continue
		}
		out = append(out, models.GaugeItem{StationName: line, FromText: true})
	}
	return out
}

// protectedText pretvara popis "- **Županija**\n  - Općina: naselja" u
// "**Županija**; Općina: naselja"
func protectedText(content string) string {
	var parts []string
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "-"))
		t = strings.TrimSpace(t)
		if t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "; ")
}

// protectedMerge spaja ugrožena područja više redaka: ista općina se ne
// ponavlja, njezina naselja se dopunjuju
type protectedMerge struct {
	order []string
	items map[string][]string
}

func newProtectedMerge() *protectedMerge { return &protectedMerge{items: map[string][]string{}} }

func (p *protectedMerge) add(text string) {
	for _, part := range strings.Split(text, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, vals := part, []string{}
		if i := strings.Index(part, ":"); i > 0 && !strings.HasPrefix(part, "**") {
			key = strings.TrimSpace(part[:i])
			for _, v := range strings.Split(part[i+1:], ",") {
				if v = strings.TrimSpace(v); v != "" {
					vals = append(vals, v)
				}
			}
		}
		if _, ok := p.items[key]; !ok {
			p.order = append(p.order, key)
			p.items[key] = nil // ključ je viđen i kad nema naselja (županija)
		}
		for _, v := range vals {
			if !contains(p.items[key], v) {
				p.items[key] = append(p.items[key], v)
			}
		}
	}
}

func (p *protectedMerge) String() string {
	var out []string
	for _, k := range p.order {
		if vals := p.items[k]; len(vals) > 0 {
			out = append(out, k+": "+strings.Join(vals, ", "))
		} else {
			out = append(out, k)
		}
	}
	return strings.Join(out, "; ")
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
