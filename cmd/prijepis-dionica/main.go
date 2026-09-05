// prijepis-dionica iz wikija (VodaAI-LLM-WIKI) stvara internal/db/sections.json
// vjerno građi Privitka 1: dionica je tablica čiji je redak jedna cjelina —
// vodotok, nasipi tog retka, objekti tog retka, ugroženo područje tog retka i
// mjerodavni vodomjeri tog retka. Uzastopni redci s istim vodotokom čine
// poddionicu.
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

// sectionOut je zapis u sections.json: stara ravna polja ostaju kao izvedene
// unije (čitaju ih punjenje postaja i registar objekata), a parts nosi građu.
type sectionOut struct {
	Code          string                  `json:"code"`
	AreaID        int                     `json:"area_id"`
	SectorID      string                  `json:"sector_id"`
	Watercourse   string                  `json:"watercourse"`
	ProtectedArea string                  `json:"protected_area"`
	Embankments   []models.EmbankmentItem `json:"embankments"`
	Structures    []models.StructureItem  `json:"structures"`
	Gauges        []models.GaugeItem      `json:"gauges"`
	Notes         string                  `json:"notes"`
	Parts         []models.SectionPart    `json:"parts"`
}

func main() {
	wiki := flag.String("wiki", "", "mapa s podrucje-*.md datotekama iz wikija")
	out := flag.String("out", "internal/db/sections.json", "gdje zapisati prijepis")
	flag.Parse()
	if *wiki == "" {
		log.Fatal("zadajte -wiki mapu")
	}
	files, err := filepath.Glob(filepath.Join(*wiki, "podrucje-*.md"))
	if err != nil || len(files) == 0 {
		log.Fatalf("nema podrucje-*.md u %s", *wiki)
	}
	sort.Strings(files)

	var all []sectionOut
	stats := map[string]int{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			log.Fatal(err)
		}
		secs := parseFile(string(raw))
		for _, s := range secs {
			all = append(all, s)
			stats["dionica"]++
			for _, p := range s.Parts {
				stats["poddionica"]++
				stats["redaka"] += len(p.Rows)
				for _, r := range p.Rows {
					if r.Unaligned {
						stats["neporavnano"]++
					}
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
	log.Printf("zapisano %s: %d dionica, %d poddionica, %d redaka, %d redaka s upozorenjem o poravnanju",
		*out, stats["dionica"], stats["poddionica"], stats["redaka"], stats["neporavnano"])
}

// parseFile čita jednu datoteku područja i vraća njezine dionice
func parseFile(text string) []sectionOut {
	var out []sectionOut
	blocks := strings.Split(text, "\n## Dionica ")
	for _, b := range blocks[1:] {
		nl := strings.IndexByte(b, '\n')
		if nl < 0 {
			continue
		}
		code := strings.TrimSuffix(strings.TrimSpace(b[:nl]), ".")
		body := b[nl:]
		sec := parseSection(code, body)
		out = append(out, sec)
	}
	return out
}

// row je jedan redak tablice Privitka dok se skuplja
type row struct {
	vodotok string
	rowData models.SectionRow
}

func parseSection(code, body string) sectionOut {
	sec := sectionOut{Code: code, Notes: ""}
	sec.SectorID, sec.AreaID = sectorAndArea(code)
	unaligned := strings.Contains(body, "colspan")

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
			cur.rowData.Unaligned = unaligned
		case "Objekti na dionici":
			if cur == nil {
				continue
			}
			for _, cells := range tableRows(content, "stacion") {
				st := cells[0]
				name := ""
				if len(cells) > 1 {
					name = cells[1]
				}
				cur.rowData.Objects = append(cur.rowData.Objects, models.DocObject{
					Kind: stationingKind(st), Stationing: st, Name: name,
				})
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
				cur.rowData.Embankments = append(cur.rowData.Embankments, models.EmbankmentItem{Name: cells[0], Data: data})
			}
		case "Ugroženo područje":
			if cur == nil {
				continue
			}
			cur.rowData.ProtectedArea = protectedText(content)
		case "Vodomjeri i kriteriji":
			if cur == nil {
				continue
			}
			cur.rowData.Gauges = append(cur.rowData.Gauges, parseGauges(content)...)
		}
	}

	// poddionice: uzastopni redci s istim vodotokom
	for _, r := range rows {
		if n := len(sec.Parts); n > 0 && sec.Parts[n-1].Description == r.vodotok {
			sec.Parts[n-1].Rows = append(sec.Parts[n-1].Rows, r.rowData)
			continue
		}
		p := models.SectionPart{Description: r.vodotok, Rows: []models.SectionRow{r.rowData}}
		d := hydro.ParseSectionDescription(r.vodotok)
		p.Bank = d.Bank
		if d.HasRange {
			from, to := d.RkmFrom, d.RkmTo
			p.RkmFrom, p.RkmTo = &from, &to
		}
		sec.Parts = append(sec.Parts, p)
	}

	// ravna polja kao unije, redom pojavljivanja
	var descs []string
	seenEmb, seenObj, seenGauge := map[string]bool{}, map[string]bool{}, map[string]bool{}
	prot := newProtectedMerge()
	for _, p := range sec.Parts {
		descs = append(descs, p.Description)
		for _, r := range p.Rows {
			for _, e := range r.Embankments {
				k := e.Name + "|" + e.Data
				if !seenEmb[k] {
					seenEmb[k] = true
					sec.Embankments = append(sec.Embankments, e)
				}
			}
			for _, o := range r.Objects {
				k := o.Stationing + "|" + o.Name
				if !seenObj[k] {
					seenObj[k] = true
					sec.Structures = append(sec.Structures, models.StructureItem{Station: o.Stationing, Name: o.Name})
				}
			}
			for _, g := range r.Gauges {
				// U ravnu uniju ide što je i dosad išlo — svaki redak tablice
				// vodomjera — te prozni zapisi s pragovima P/R/I/IS, koje je
				// stari prijepis gubio. Prozna mjerila druge vrste (kota na
				// mostu u metrima, pravilnik retencije) ostaju samo u retku
				// poddionice, da punjenje iz njih ne stvara postaje.
				if g.FromText && !g.IsGauge() {
					continue
				}
				k := g.StationName + "|" + g.PrepCm + "|" + g.RegularCm + "|" + g.EmergCm + "|" + g.CriticalCm
				if !seenGauge[k] {
					seenGauge[k] = true
					sec.Gauges = append(sec.Gauges, g)
				}
			}
			prot.add(r.ProtectedArea)
		}
	}
	sec.Watercourse = strings.Join(descs, " | ")
	sec.ProtectedArea = prot.String()
	if sec.Embankments == nil {
		sec.Embankments = []models.EmbankmentItem{}
	}
	if sec.Structures == nil {
		sec.Structures = []models.StructureItem{}
	}
	if sec.Gauges == nil {
		sec.Gauges = []models.GaugeItem{}
	}
	if sec.Parts == nil {
		sec.Parts = []models.SectionPart{}
	}
	return sec
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

// stationingKind čita po čemu se objekt stacionira: rkm rijeke, km nasipa,
// pkm potoka, kkm kanala; prazno kad stacionaže nema (dijelovi brane)
func stationingKind(st string) string {
	m := kindRe.FindStringSubmatch(strings.TrimSpace(st))
	if m == nil {
		return ""
	}
	return strings.ToLower(m[1])
}

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
// "**Županija**; Općina: naselja", kako je prijepis i dosad zapisivao
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

// sectionLess slaže šifre prirodno: B.34.2 prije B.34.14
func sectionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] == pb[i] {
			continue
		}
		var na, nb int
		_, ea := fmt.Sscanf(pa[i], "%d", &na)
		_, eb := fmt.Sscanf(pb[i], "%d", &nb)
		if ea == nil && eb == nil {
			return na < nb
		}
		return pa[i] < pb[i]
	}
	return len(pa) < len(pb)
}
