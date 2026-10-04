package main

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

func allowed(key string, c config, r *report, now time.Time) bool {
	for _, e := range c.Exceptions {
		if e.Key != key {
			continue
		}
		expires, err := time.Parse("2006-01-02", e.Expires)
		if err != nil || !now.Before(expires.Add(24*time.Hour)) {
			return false
		}
		r.Exceptions = append(r.Exceptions, key+": "+e.Reason)
		return true
	}
	return false
}

func lintKey(i lintIssue, root string) string {
	path := i.Pos.Filename
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(root, path); err == nil {
			path = rel
		}
	}
	text := i.Text
	if i.FromLinter == "dupl" {
		text = cloneRange.ReplaceAllString(text, "<range>")
	}
	// Metrički linteri u poruku upišu izmjerenu vrijednost: bez ovoga bi i
	// smanjenje složenosti funkcije (66 → 64) bilo „novi nalaz”. Rast iste
	// funkcije hvata usporedba CC/CRAP po funkciji (compareFunctions).
	if metrickiLinteri[i.FromLinter] {
		text = metrickiBroj.ReplaceAllString(text, "#")
	}
	return "lint:" + i.FromLinter + ":" + filepath.ToSlash(path) + ":" + text
}

var cloneRange = regexp.MustCompile(`[0-9]+-[0-9]+`)

var (
	metrickiLinteri = map[string]bool{"gocyclo": true, "gocognit": true, "maintidx": true, "nestif": true, "cyclop": true, "funlen": true}
	metrickiBroj    = regexp.MustCompile(`[0-9]+(\.[0-9]+)?`)
)

func serious(i lintIssue) bool {
	return i.FromLinter == "govet" || i.FromLinter == "typecheck" || i.FromLinter == "staticcheck" && !strings.HasPrefix(i.Text, "SA1019:")
}

func measureLint(path, root string, c config, r *report) error {
	var output struct{ Issues []lintIssue }
	if err := readJSON(path, &output); err != nil {
		return err
	}
	r.Static = lintSummary{Status: "PASS", Counts: map[string]int{}, Findings: map[string]int{}}
	exempted := map[string]bool{}
	for _, i := range output.Issues {
		// Staticcheck emits these as locations attached to the primary diagnostic.
		// Count the problem once, not once per explanatory location.
		if strings.Contains(i.Text, "(related information)") {
			continue
		}
		key := lintKey(i, root)
		if !exempted[key] && allowed(key, c, r, time.Now()) {
			exempted[key] = true
			continue
		}
		r.Static.Counts[i.FromLinter]++
		r.Static.Findings[key]++
		if serious(i) {
			r.Static.Errors++
			r.Failures = append(r.Failures, key)
		} else {
			r.Static.Warnings++
		}
	}
	if r.Static.Errors > 0 {
		r.Static.Status = "FAIL"
	}
	return nil
}

func checkRegression(key, message string, c config, r *report) {
	if !allowed(key, c, r, time.Now()) {
		r.Failures = append(r.Failures, key+": "+message)
	}
}

func compareCoverage(key string, cur, base coverage, c config, r *report) {
	if cur.Percent == nil || base.Percent == nil {
		r.Failures = append(r.Failures, key+": nedostaje coverage")
		return
	}
	if *base.Percent-*cur.Percent > c.CoverageDrop+1e-9 {
		checkRegression(key, fmt.Sprintf("%.3f%% → %.3f%%", *base.Percent, *cur.Percent), c, r)
	}
}

func compareFunctions(r, base *report, c config) {
	old := map[string]function{}
	for _, f := range base.Functions {
		old[f.ID] = f
	}
	for _, f := range r.Functions {
		prev, exists := old[f.ID]
		// Nepromijenjena funkcija (isti broj tokena, naredbi i CC): razlika u
		// njezinom coverageu i CRAP-u dolazi od testova koji ovise o vremenu,
		// ne od promjene, pa se po njima ne uspoređuje. Ukupni i kritični
		// coverage i dalje se uspoređuju.
		nepromijenjena := exists && f.Tokens == prev.Tokens && f.Complexity == prev.Complexity &&
			f.Coverage.Statements == prev.Coverage.Statements
		ccLimit, crapLimit, coverageTarget := c.ComplexityLimit, c.CRAPLimit, c.CoverageTarget
		if f.Critical {
			ccLimit, crapLimit, coverageTarget = c.CriticalComplexityLimit, c.CriticalCRAPLimit, c.CriticalCoverageTarget
		}
		if exists {
			ccLimit = max(ccLimit, prev.Complexity)
			if prev.CRAP != nil {
				crapLimit = math.Max(crapLimit, *prev.CRAP+c.CRAPIncrease)
			}
		}
		if f.Complexity > ccLimit {
			checkRegression("complexity:"+f.ID, fmt.Sprintf("CC %d > %d", f.Complexity, ccLimit), c, r)
		}
		if f.CRAP != nil && *f.CRAP > crapLimit+1e-9 && !nepromijenjena {
			checkRegression("crap:"+f.ID, fmt.Sprintf("CRAP %.2f > %.2f", *f.CRAP, crapLimit), c, r)
		}
		if exists && !nepromijenjena && f.Coverage.Percent != nil && prev.Coverage.Percent != nil {
			compareCoverage("coverage:"+f.ID, f.Coverage, prev.Coverage, c, r)
		} else if !exists && f.Coverage.Percent != nil && *f.Coverage.Percent+c.CoverageDrop < coverageTarget {
			checkRegression("coverage:"+f.ID, fmt.Sprintf("nova funkcija %.2f%% < %.2f%%", *f.Coverage.Percent, coverageTarget), c, r)
		}
	}
}

func evaluate(r, base *report, c config, record bool) {
	if r.Measurements != "PASS" {
		r.Failures = append(r.Failures, "metrics: INCOMPLETE; nulte vrijednosti nisu valjano mjerenje")
	}
	for _, e := range c.Exceptions {
		expiry, _ := time.Parse("2006-01-02", e.Expires)
		if !time.Now().Before(expiry.Add(24 * time.Hour)) {
			r.Failures = append(r.Failures, "istekla iznimka: "+e.Key)
		}
	}
	if r.Tests != "PASS" {
		r.Failures = append(r.Failures, "tests: "+r.Tests)
	}
	if r.Race != "PASS" {
		r.Failures = append(r.Failures, "race: "+r.Race)
	}
	if r.Static.Status != "PASS" {
		r.Failures = append(r.Failures, "static analysis: "+r.Static.Status)
	}
	if r.Coverage.Percent != nil && *r.Coverage.Percent < c.CoverageTarget {
		r.Warnings = append(r.Warnings, fmt.Sprintf("coverage ispod cilja %.0f%%", c.CoverageTarget))
	}
	if r.CriticalCoverage.Percent != nil && *r.CriticalCoverage.Percent < c.CriticalCoverageTarget {
		r.Warnings = append(r.Warnings, fmt.Sprintf("kritični coverage ispod cilja %.0f%%", c.CriticalCoverageTarget))
	}
	debt := 0
	for _, f := range r.Functions {
		cc, cr := c.ComplexityLimit, c.CRAPLimit
		if f.Critical {
			cc, cr = c.CriticalComplexityLimit, c.CriticalCRAPLimit
		}
		if f.Complexity > cc || f.CRAP != nil && *f.CRAP > cr {
			debt++
		}
	}
	if debt > 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf("%d funkcija iznad CC/CRAP cilja", debt))
	}
	if r.Static.Warnings > 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf("%d lint upozorenja", r.Static.Warnings))
	}
	if r.Exact.Percent >= 2 {
		r.Warnings = append(r.Warnings, "exact duplication >= 2%")
	}
	if r.Fuzzy.Percent >= 2 {
		r.Warnings = append(r.Warnings, "fuzzy kandidati >= 2% (samo upozorenje)")
	}
	if base == nil {
		if !record {
			r.Failures = append(r.Failures, "baseline nedostaje; potreban je pregled i izričito prihvaćanje")
		}
	} else if base.Schema != r.Schema || base.Analyzer != r.Analyzer || base.Platform != r.Platform || base.GoVersion != r.GoVersion || base.ConfigHash != r.ConfigHash {
		r.Failures = append(r.Failures, "baseline nije usporediv (verzija alata, OS/arhitektura, Go ili konfiguracija); ponoviti isti baseline commit na ovoj platformi")
	} else {
		compareCoverage("coverage:overall", r.Coverage, base.Coverage, c, r)
		compareCoverage("coverage:critical", r.CriticalCoverage, base.CriticalCoverage, c, r)
		compareFunctions(r, base, c)
		if r.Exact.Percent-base.Exact.Percent > c.DuplicationIncrease+1e-9 {
			checkRegression("duplication:exact", fmt.Sprintf("%.3f%% → %.3f%%", base.Exact.Percent, r.Exact.Percent), c, r)
		}
		for key, n := range r.Static.Findings {
			if n > base.Static.Findings[key] {
				checkRegression(key, "novi/dodatni nalaz", c, r)
			}
		}
	}
	sort.Strings(r.Failures)
	sort.Strings(r.Exceptions)
	r.Status = "HEALTHY"
	if len(r.Warnings) > 0 {
		r.Status = "BASELINE_DEBT"
	}
	if len(r.Failures) > 0 {
		r.Status = "FAIL"
	}
}
