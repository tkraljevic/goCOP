package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"time"
)

const analyzerVersion = "2"
const lintVersion = "2.14.0"

type exception struct {
	Key     string `json:"key"`
	Reason  string `json:"reason"`
	Owner   string `json:"owner"`
	Expires string `json:"expires"`
}

// premjestaj: funkcije izdvojene iz funkcije koja postoji u baselineu, bez
// izmjene ponašanja (npr. rastavljanje main). Takva funkcija nije nov kod:
// nasljeđuje stanje izvora iz baselinea (CC, CRAP, coverage) i ne smije biti
// gora od njega, a zbroj složenosti premještenih funkcija ne smije biti veći
// od onoga što je izvor izgubio. Kad se baseline ponovno snimi, premještene
// funkcije u njemu postoje i mjere se kao svaka druga.
type premjestaj struct {
	Iz         string   `json:"iz"`
	Funkcije   []string `json:"funkcije"`
	Razlog     string   `json:"razlog"`
	Pregledano string   `json:"pregledano"`
}

type config struct {
	Schema                  int          `json:"schema"`
	CoverageTarget          float64      `json:"coverage_target"`
	CriticalCoverageTarget  float64      `json:"critical_coverage_target"`
	ComplexityLimit         int          `json:"complexity_limit"`
	CriticalComplexityLimit int          `json:"critical_complexity_limit"`
	CRAPLimit               float64      `json:"crap_limit"`
	CriticalCRAPLimit       float64      `json:"critical_crap_limit"`
	CoverageDrop            float64      `json:"coverage_drop_pp"`
	CRAPIncrease            float64      `json:"crap_increase"`
	DuplicationIncrease     float64      `json:"duplication_increase_pp"`
	ExactMinTokens          int          `json:"exact_min_tokens"`
	FuzzyMinTokens          int          `json:"fuzzy_min_tokens"`
	FuzzySimilarity         float64      `json:"fuzzy_similarity"`
	Critical                []string     `json:"critical"`
	Exceptions              []exception  `json:"exceptions"`
	Premjestaji             []premjestaj `json:"premjestaji"`
	patterns                []*regexp.Regexp
}

func loadConfig(path string) (config, error) {
	var c config
	if err := readJSON(path, &c); err != nil {
		return c, err
	}
	if c.Schema != 1 || c.CoverageTarget <= 0 || c.CoverageTarget > 100 || c.CriticalCoverageTarget <= 0 || c.CriticalCoverageTarget > 100 || c.ComplexityLimit < 1 || c.CriticalComplexityLimit < 1 || c.CRAPLimit < 1 || c.CriticalCRAPLimit < 1 || c.CoverageDrop < 0 || c.CRAPIncrease < 0 || c.DuplicationIncrease < 0 || c.ExactMinTokens < 20 || c.FuzzyMinTokens < 20 || c.FuzzySimilarity <= 0 || c.FuzzySimilarity > 1 || len(c.Critical) == 0 {
		return c, fmt.Errorf("neispravni pragovi konfiguracije")
	}
	for _, pattern := range c.Critical {
		r, err := regexp.Compile(pattern)
		if err != nil {
			return c, err
		}
		c.patterns = append(c.patterns, r)
	}
	seen := map[string]bool{}
	for _, e := range c.Exceptions {
		_, err := time.Parse("2006-01-02", e.Expires)
		if e.Key == "" || e.Reason == "" || e.Owner == "" || err != nil || seen[e.Key] {
			return c, fmt.Errorf("neispravna ili ponovljena iznimka %q", e.Key)
		}
		seen[e.Key] = true
	}
	premjestene := map[string]bool{}
	for _, p := range c.Premjestaji {
		_, err := time.Parse("2006-01-02", p.Pregledano)
		if p.Iz == "" || p.Razlog == "" || err != nil || len(p.Funkcije) == 0 {
			return c, fmt.Errorf("neispravan premještaj iz %q", p.Iz)
		}
		for _, id := range p.Funkcije {
			if id == "" || id == p.Iz || premjestene[id] {
				return c, fmt.Errorf("premještaj iz %q: neispravna ili ponovljena funkcija %q", p.Iz, id)
			}
			premjestene[id] = true
		}
	}
	return c, nil
}

func (c config) critical(id string) bool {
	for _, r := range c.patterns {
		if r.MatchString(id) {
			return true
		}
	}
	return false
}

type coverage struct {
	Statements int      `json:"statements"`
	Covered    int      `json:"covered"`
	Percent    *float64 `json:"percent"`
}

func (c *coverage) add(statements, covered int) {
	c.Statements += statements
	c.Covered += covered
	if c.Statements > 0 {
		p := 100 * float64(c.Covered) / float64(c.Statements)
		c.Percent = &p
	}
}

type function struct {
	ID         string   `json:"id"`
	File       string   `json:"file"`
	Name       string   `json:"name"`
	Line       int      `json:"line"`
	Kraj       int      `json:"end_line,omitempty"`
	Complexity int      `json:"complexity"`
	Coverage   coverage `json:"coverage"`
	CRAP       *float64 `json:"crap"`
	Critical   bool     `json:"critical"`
	Tokens     int      `json:"tokens"`
}

func crap(cc int, percent float64) float64 {
	uncovered := 1 - percent/100
	return float64(cc*cc)*uncovered*uncovered*uncovered + float64(cc)
}

type clone struct {
	A          string  `json:"a"`
	B          string  `json:"b"`
	Similarity float64 `json:"similarity"`
	Tokens     int     `json:"tokens"`
}

type duplication struct {
	Percent          float64 `json:"percent"`
	DuplicatedTokens int     `json:"duplicated_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Pairs            []clone `json:"pairs"`
}

type lintIssue struct {
	FromLinter string
	Text       string
	Pos        struct {
		Filename string
		Line     int
	}
}

type lintSummary struct {
	Status   string         `json:"status"`
	Errors   int            `json:"errors"`
	Warnings int            `json:"warnings"`
	Counts   map[string]int `json:"counts"`
	Findings map[string]int `json:"findings"`
}

type report struct {
	Schema            int                 `json:"schema"`
	Analyzer          string              `json:"analyzer"`
	Timestamp         string              `json:"timestamp"`
	Commit            string              `json:"commit"`
	Dirty             bool                `json:"dirty"`
	Platform          string              `json:"platform"`
	GoVersion         string              `json:"go_version"`
	ConfigHash        string              `json:"config_hash"`
	SourceHash        string              `json:"source_hash"`
	Tests             string              `json:"tests"`
	Race              string              `json:"race"`
	Measurements      string              `json:"measurements_status"`
	Static            lintSummary         `json:"static_analysis"`
	Coverage          coverage            `json:"coverage"`
	CriticalCoverage  coverage            `json:"critical_coverage"`
	Packages          map[string]coverage `json:"packages"`
	AverageCRAP       float64             `json:"average_crap"`
	CRAPRaspodjela    raspodjelaCRAP      `json:"crap_distribution"`
	MaxCRAP           float64             `json:"max_crap"`
	WorstCRAP         string              `json:"worst_crap_function"`
	AverageComplexity float64             `json:"average_complexity"`
	MaxComplexity     int                 `json:"max_complexity"`
	Functions         []function          `json:"functions"`
	Exact             duplication         `json:"exact_duplication"`
	Fuzzy             duplication         `json:"fuzzy_duplication"`
	MutationScore     *float64            `json:"mutation_score"`
	MutationStatus    string              `json:"mutation_status"`
	Failures          []string            `json:"failures"`
	Warnings          []string            `json:"warnings"`
	Exceptions        []string            `json:"applied_exceptions"`
	Premjestaji       []string            `json:"applied_moves,omitempty"`
	Status            string              `json:"status"`

	// lintMjesta: gdje je koji lint nalaz, da se metrički nalaz u premještenoj
	// funkciji ne broji kao nov; premjestene su funkcije kojima je priznat
	// premještaj (puni ih compareFunctions)
	lintMjesta  map[string][]lintMjesto
	premjestene []function
}

// lintMjesto je datoteka (relativno, kosim crtama) i redak jednog lint nalaza
type lintMjesto struct {
	linter, datoteka string
	redak            int
}

// raspodjelaCRAP: prosjek skriva nekoliko čudovišta (jedna funkcija s CRAP-om
// 48 000 podigne prosjek cijelog projekta), pa medijan, percentili i razredi
// kažu je li loš cijeli kod ili tek nekoliko funkcija. Samo izvještaj, ne prag.
type raspodjelaCRAP struct {
	Medijan    float64 `json:"median"`
	P90        float64 `json:"p90"`
	P95        float64 `json:"p95"`
	DoDeset    int     `json:"do_10"`
	DoTrideset int     `json:"od_10_do_30"`
	DoSto      int     `json:"od_30_do_100"`
	PrekoSto   int     `json:"preko_100"`
}

// raspodjela računa medijan, P90, P95 (najbliži rang) i razrede CRAP-a
func raspodjela(vrijednosti []float64) raspodjelaCRAP {
	var r raspodjelaCRAP
	n := len(vrijednosti)
	if n == 0 {
		return r
	}
	v := append([]float64(nil), vrijednosti...)
	sort.Float64s(v)
	if n%2 == 1 {
		r.Medijan = v[n/2]
	} else {
		r.Medijan = (v[n/2-1] + v[n/2]) / 2
	}
	rang := func(p float64) float64 { return v[int(math.Ceil(p*float64(n)))-1] }
	r.P90, r.P95 = rang(0.90), rang(0.95)
	for _, x := range v {
		switch {
		case x <= 10:
			r.DoDeset++
		case x <= 30:
			r.DoTrideset++
		case x <= 100:
			r.DoSto++
		default:
			r.PrekoSto++
		}
	}
	return r
}

func readJSON(path string, dst any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

func writeJSON(path string, src any) error {
	b, err := json.MarshalIndent(src, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
}

func digest(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
