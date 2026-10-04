package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
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

type config struct {
	Schema                  int         `json:"schema"`
	CoverageTarget          float64     `json:"coverage_target"`
	CriticalCoverageTarget  float64     `json:"critical_coverage_target"`
	ComplexityLimit         int         `json:"complexity_limit"`
	CriticalComplexityLimit int         `json:"critical_complexity_limit"`
	CRAPLimit               float64     `json:"crap_limit"`
	CriticalCRAPLimit       float64     `json:"critical_crap_limit"`
	CoverageDrop            float64     `json:"coverage_drop_pp"`
	CRAPIncrease            float64     `json:"crap_increase"`
	DuplicationIncrease     float64     `json:"duplication_increase_pp"`
	ExactMinTokens          int         `json:"exact_min_tokens"`
	FuzzyMinTokens          int         `json:"fuzzy_min_tokens"`
	FuzzySimilarity         float64     `json:"fuzzy_similarity"`
	Critical                []string    `json:"critical"`
	Exceptions              []exception `json:"exceptions"`
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
	Status            string              `json:"status"`
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
