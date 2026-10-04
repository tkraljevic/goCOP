package main

import (
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fzipp/gocyclo"
)

func testConfig() config {
	return config{Schema: 1, CoverageTarget: 85, CriticalCoverageTarget: 95, ComplexityLimit: 15, CriticalComplexityLimit: 10, CRAPLimit: 30, CriticalCRAPLimit: 10, CoverageDrop: 0.1, CRAPIncrease: 0.5, DuplicationIncrease: 0.1, ExactMinTokens: 20, FuzzyMinTokens: 20, FuzzySimilarity: 0.85}
}

func cov(total, covered int) coverage { var c coverage; c.add(total, covered); return c }
func number(n float64) *float64       { return &n }
func cleanReport() report {
	return report{Schema: 1, Analyzer: "1", Platform: "test", GoVersion: "test", ConfigHash: "same", Tests: "PASS", Race: "PASS", Measurements: "PASS", Static: lintSummary{Status: "PASS", Findings: map[string]int{}}, Coverage: cov(100, 70), CriticalCoverage: cov(100, 70)}
}

func TestCRAPFormula(t *testing.T) {
	for _, tc := range []struct {
		cc             int
		coverage, want float64
	}{{1, 0, 2}, {6, 100, 6}, {10, 50, 22.5}, {15, 0, 240}, {10, 100, 10}} {
		if got := crap(tc.cc, tc.coverage); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%+v: %f", tc, got)
		}
	}
}

func TestComplexityIncludesConditionsAndClosures(t *testing.T) {
	src := `package p; func f(a,b bool){if a && b {} ; for a {} ; _=func(){if b{}}}`
	f, err := parser.ParseFile(token.NewFileSet(), "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := gocyclo.Complexity(f.Decls[0]); got != 5 {
		t.Fatalf("CC=%d", got)
	}
}

func TestWeightedCoverageAndFunctionBoundaries(t *testing.T) {
	blocks := []block{{Start: token.Position{Line: 2, Column: 2}, Statements: 9, Count: 1}, {Start: token.Position{Line: 3, Column: 2}, Statements: 1, Count: 0}, {Start: token.Position{Line: 4, Column: 2}, Statements: 500, Count: 1}}
	c := functionCoverage(blocks, token.Position{Line: 2, Column: 1}, token.Position{Line: 4, Column: 1})
	if c.Percent == nil || *c.Percent != 90 || c.Statements != 10 {
		t.Fatalf("%+v", c)
	}
	if functionCoverage(nil, token.Position{}, token.Position{}).Percent != nil {
		t.Fatal("prazna funkcija ne smije dobiti izmišljeni coverage")
	}
}

func TestProfileParsing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cover.out")
	if err := os.WriteFile(path, []byte("mode: atomic\np/f.go:2.1,4.2 9 1\np/f.go:5.1,6.2 1 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := readProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(p["p/f.go"]) != 2 || p["p/f.go"][0].Statements != 9 {
		t.Fatal(p)
	}
	if err := os.WriteFile(path, []byte("mode: atomic\ninvalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readProfile(path); err == nil {
		t.Fatal("prihvaćen neispravan profil")
	}
}

func TestMethodIdentitiesAndCriticalCoverage(t *testing.T) {
	c := testConfig()
	c.patterns = []*regexp.Regexp{regexp.MustCompile(`::\(\*A\)\.Read$`)}
	src := []byte("package p\ntype A struct{}; type B struct{}\nfunc (a *A) Read(){ println(1) }\nfunc (b *B) Read(){ println(2) }\n")
	r := report{}
	p := map[string][]block{"p/x.go": {{Start: token.Position{Line: 3, Column: 19}, Statements: 2, Count: 1}, {Start: token.Position{Line: 4, Column: 19}, Statements: 8, Count: 0}}}
	_, err := measure([]sourceFile{{"x.go", "p", src}}, "p", p, c, &r)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Functions) != 2 || r.Functions[0].ID == r.Functions[1].ID {
		t.Fatal(r.Functions)
	}
	if *r.Coverage.Percent != 20 || *r.CriticalCoverage.Percent != 100 {
		t.Fatalf("%+v %+v", r.Coverage, r.CriticalCoverage)
	}
	p["p/stale.go"] = nil
	if _, err := measure([]sourceFile{{"x.go", "p", src}}, "p", p, c, &report{}); err == nil {
		t.Fatal("zastarjeli profil mora pasti")
	}
}

func body(t *testing.T, id, src string) functionTokens {
	t.Helper()
	v, err := scanTokens([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return functionTokens{ID: id, Tokens: v}
}

func TestExactClonesUseUnionAndRespectIdentifierChanges(t *testing.T) {
	a := body(t, "a", `{ x:=1; if x>0 { println(x) }; for x<10 { x++ }; println(x) }`)
	b := body(t, "b", `{ /*same*/ x := 1; if x > 0 { println(x) }; for x < 10 { x++ }; println(x) }`)
	d := exactClones([]functionTokens{a, b}, 10)
	if d.Percent != 100 || d.DuplicatedTokens != len(a.Tokens)+len(b.Tokens) {
		t.Fatalf("%+v", d)
	}
	c := body(t, "c", strings.ReplaceAll(`{ x:=1; if x>0 { println(x) }; for x<10 { x++ }; println(x) }`, "x", "renamed"))
	if got := exactClones([]functionTokens{a, c}, len(a.Tokens)); got.Percent != 0 {
		t.Fatal(got)
	}
	if got := fuzzyClones([]functionTokens{a, c}, 20, 0.99); got.Percent != 100 {
		t.Fatal(got)
	}
}

func TestSingleFunctionIsNotItsOwnDuplicate(t *testing.T) {
	a := body(t, "a", `{ println(1); println(2); println(3); println(4) }`)
	if d := exactClones([]functionTokens{a}, 10); d.Percent != 0 {
		t.Fatal(d)
	}
}

func TestLegacyDebtPassesButRegressionFails(t *testing.T) {
	c := testConfig()
	base := cleanReport()
	base.Functions = []function{{ID: "legacy", Complexity: 40, Coverage: cov(100, 70), CRAP: number(83.2)}}
	r := cleanReport()
	r.Functions = append([]function(nil), base.Functions...)
	evaluate(&r, &base, c, false)
	if len(r.Failures) != 0 || r.Status != "BASELINE_DEBT" {
		t.Fatalf("%+v", r)
	}
	r = cleanReport()
	r.Coverage = cov(100, 65)
	r.Functions = []function{{ID: "legacy", Complexity: 41, Coverage: cov(100, 70), CRAP: number(86.4)}}
	evaluate(&r, &base, c, false)
	if len(r.Failures) < 3 || r.Status != "FAIL" {
		t.Fatal(r.Failures)
	}
}

func TestNewCriticalCodeHasStricterTargets(t *testing.T) {
	c := testConfig()
	base := cleanReport()
	r := cleanReport()
	r.Functions = []function{{ID: "new", Critical: true, Complexity: 11, Coverage: cov(100, 90), CRAP: number(11.121)}}
	evaluate(&r, &base, c, false)
	if len(r.Failures) != 3 {
		t.Fatal(r.Failures)
	}
}

func TestChangedFunctionCoverageCannotHideBehindAggregate(t *testing.T) {
	c := testConfig()
	base := cleanReport()
	r := cleanReport()
	base.Functions = []function{{ID: "f", Coverage: cov(10, 10), Complexity: 1, CRAP: number(1), Tokens: 40}}
	r.Functions = []function{{ID: "f", Coverage: cov(10, 9), Complexity: 1, CRAP: number(1.001), Tokens: 44}}
	evaluate(&r, &base, c, false)
	if len(r.Failures) != 1 || !strings.Contains(r.Failures[0], "coverage:f") {
		t.Fatal(r.Failures)
	}
}

// Nepromijenjena funkcija čiji coverage pleše zbog testova ovisnih o
// vremenu nije regresija; ukupni coverage se i dalje uspoređuje
func TestUnchangedFunctionCoverageNoiseIsNotRegression(t *testing.T) {
	c := testConfig()
	base := cleanReport()
	base.Functions = []function{{ID: "f", Coverage: cov(17, 14), Complexity: 5, CRAP: number(5.2), Tokens: 120}}
	r := cleanReport()
	r.Functions = []function{{ID: "f", Coverage: cov(17, 13), Complexity: 5, CRAP: number(5.5), Tokens: 120}}
	evaluate(&r, &base, c, false)
	if len(r.Failures) != 0 {
		t.Fatalf("šum coveragea nepromijenjene funkcije: %v", r.Failures)
	}
	r.Coverage = cov(100, 60)
	evaluate(&r, &base, c, false)
	if len(r.Failures) == 0 {
		t.Fatal("pad ukupnog coveragea mora i dalje biti regresija")
	}
}

func TestGateFailureCases(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*report)
	}{
		{"race", func(r *report) { r.Race = "FAIL" }},
		{"test", func(r *report) { r.Tests = "FAIL" }},
		{"missing lint", func(r *report) { r.Static.Status = "INCOMPLETE" }},
		{"platform", func(r *report) { r.Platform = "other" }},
		{"policy", func(r *report) { r.ConfigHash = "changed" }},
		{"duplicate", func(r *report) { r.Exact.Percent = 0.2 }},
		{"new lint", func(r *report) { r.Static.Findings["lint:errcheck:file:unchecked"] = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := cleanReport()
			r := cleanReport()
			tc.change(&r)
			evaluate(&r, &base, testConfig(), false)
			if len(r.Failures) == 0 {
				t.Fatal("false green")
			}
		})
	}
}

func TestExceptionsAreScopedAndExpire(t *testing.T) {
	c := testConfig()
	c.Exceptions = []exception{{Key: "crap:f", Reason: "review", Owner: "owner", Expires: "2026-10-05"}}
	r := report{}
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	if !allowed("crap:f", c, &r, now) || allowed("crap:other", c, &r, now) || allowed("crap:f", c, &r, now.Add(48*time.Hour)) {
		t.Fatal("iznimka nije ograničena")
	}
}

func TestStaticSeverity(t *testing.T) {
	for _, tc := range []struct {
		linter, text string
		want         bool
	}{{"govet", "error", true}, {"typecheck", "error", true}, {"staticcheck", "SA5000: assignment", true}, {"staticcheck", "SA1019: deprecated", false}, {"errcheck", "ignored", false}} {
		if serious(lintIssue{FromLinter: tc.linter, Text: tc.text}) != tc.want {
			t.Fatal(tc)
		}
	}
}

func TestEvidenceRejectsChangedReport(t *testing.T) {
	o := options{out: t.TempDir(), reuseTests: true}
	r := cleanReport()
	r.SourceHash = "source"
	for _, name := range []string{"coverage.out", "tests.log"} {
		if err := os.WriteFile(filepath.Join(o.out, name), []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	e, err := evidence(o, &r)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(o.out, "test-evidence.json"), e); err != nil {
		t.Fatal(err)
	}
	if err := runTests(o, &r); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(o.out, "coverage.out"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runTests(o, &r); err == nil {
		t.Fatal("pozmenjen profil prihvaćen")
	}
}

func TestCheckedInConfig(t *testing.T) {
	c, err := loadConfig("../../quality/config.json")
	if err != nil {
		t.Fatal(err)
	}
	if !c.critical("internal/repository/apply.go::applyOne") || !c.critical("internal/prognoza/osvjezavanje.go::(*Osvjezivac).Osvjezi") || c.critical("internal/web/ikone.go::ikona") {
		t.Fatal("kritični odabir")
	}
}

func TestDuplLineMovesDoNotCreateNewFinding(t *testing.T) {
	a := lintIssue{FromLinter: "dupl", Text: "76-110 lines are duplicate of `repo/other.go:71-105`"}
	a.Pos.Filename = "repo/current.go"
	b := a
	b.Text = "80-114 lines are duplicate of `repo/other.go:100-134`"
	if lintKey(a, "/root") != lintKey(b, "/root") {
		t.Fatal("pomak retka promijenio identitet klona")
	}
}

func TestIntentionalExceptionDoesNotHideSecondProblem(t *testing.T) {
	i := lintIssue{FromLinter: "staticcheck", Text: "SA5000: assignment to nil map"}
	i.Pos.Filename = "p_test.go"
	c := testConfig()
	c.Exceptions = []exception{{Key: lintKey(i, "/root"), Reason: "one deliberate panic", Owner: "test", Expires: time.Now().AddDate(1, 0, 0).Format("2006-01-02")}}
	path := filepath.Join(t.TempDir(), "lint.json")
	if err := writeJSON(path, struct{ Issues []lintIssue }{[]lintIssue{i, i}}); err != nil {
		t.Fatal(err)
	}
	r := report{}
	if err := measureLint(path, "/root", c, &r); err != nil {
		t.Fatal(err)
	}
	if r.Static.Errors != 1 || len(r.Exceptions) != 1 {
		t.Fatalf("%+v", r.Static)
	}
}

func TestIncompleteMetricsAndMissingBaselineFailClosed(t *testing.T) {
	r := cleanReport()
	r.Measurements = "INCOMPLETE"
	evaluate(&r, nil, testConfig(), false)
	if len(r.Failures) != 2 || r.Status != "FAIL" {
		t.Fatal(r.Failures)
	}
}

func TestMutationRejectsLiveProject(t *testing.T) {
	root := t.TempDir()
	if err := mutation(options{root: root, mutationRoot: root, mutationPackage: "internal/models"}); err == nil {
		t.Fatal("mutation dopušten u živom projektu")
	}
}

func TestMarkdownAndJSONExposeFailureAndNullMutation(t *testing.T) {
	r := cleanReport()
	r.Failures = []string{"test failure"}
	r.Status = "FAIL"
	r.MutationStatus = "NOT_RUN"
	out := t.TempDir()
	if err := writeReport(out, r); err != nil {
		t.Fatal(err)
	}
	md, err := os.ReadFile(filepath.Join(out, "code-health.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "test failure") || !strings.Contains(string(md), "FAIL") {
		t.Fatal(string(md))
	}
	var read report
	if err := readJSON(filepath.Join(out, "code-health.json"), &read); err != nil {
		t.Fatal(err)
	}
	if read.MutationScore != nil || read.Status != "FAIL" {
		t.Fatal(read)
	}
}

func TestMetricLintersCompareByFunctionNotValue(t *testing.T) {
	a := lintIssue{FromLinter: "gocognit", Text: "cognitive complexity 66 of func `pdfPrijave` is high (> 30)"}
	a.Pos.Filename = "internal/web/prijava_pdf.go"
	b := a
	b.Text = "cognitive complexity 64 of func `pdfPrijave` is high (> 30)"
	if lintKey(a, "/root") != lintKey(b, "/root") {
		t.Fatal("smanjenje složenosti iste funkcije postalo je novi nalaz")
	}
	c := a
	c.Text = "cognitive complexity 66 of func `drugaFunkcija` is high (> 30)"
	if lintKey(a, "/root") == lintKey(c, "/root") {
		t.Fatal("dvije funkcije dobile su isti ključ")
	}
	m := lintIssue{FromLinter: "maintidx", Text: "Function name: applyOne, Cyclomatic Complexity: 134, Halstead Volume: 28997.31, Maintainability Index: 0"}
	n := m
	n.Text = "Function name: applyOne, Cyclomatic Complexity: 140, Halstead Volume: 29101.80, Maintainability Index: 0"
	if lintKey(m, "/root") != lintKey(n, "/root") {
		t.Fatal("maintidx s promijenjenim vrijednostima nije isti nalaz")
	}
	// ostali linteri čuvaju brojeve: SA kodovi i poruke nisu mjerenja
	s := lintIssue{FromLinter: "staticcheck", Text: "SA4006: this value of org is never used"}
	s2 := s
	s2.Text = "SA4009: this value of org is never used"
	if lintKey(s, "/root") == lintKey(s2, "/root") {
		t.Fatal("staticcheck kodovi se ne smiju izjednačiti")
	}
}

func TestOnlyPackagesGitSeesAreMeasured(t *testing.T) {
	root := filepath.FromSlash("/repo")
	dirs := []string{
		root,
		filepath.Join(root, "internal", "web"),
		filepath.Join(root, "tools", "admin", "alat"), // u .gitignore: git je ne vidi
		filepath.Join(root, "internal", "novi"),       // nova, još nepraćena datoteka
		"",
	}
	vidljive := []string{"main.go", "internal/web/server.go", "internal/novi/novi.go", ""}
	got := odaberiPakete(root, dirs, vidljive)
	want := []string{".", "./internal/novi", "./internal/web"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("paketi: %v, želim %v", got, want)
	}
}

func TestExceptionsDoNotChangePolicyHash(t *testing.T) {
	osnova := []byte(`{"schema":1,"complexity_limit":15,"exceptions":[]}`)
	sIznimkom := []byte(`{"schema":1,"complexity_limit":15,"exceptions":[{"key":"complexity:x::y","reason":"r","owner":"o","expires":"2026-12-01"}]}`)
	strozi := []byte(`{"schema":1,"complexity_limit":12,"exceptions":[]}`)
	a, err := bezIznimki(osnova)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := bezIznimki(sIznimkom)
	c, _ := bezIznimki(strozi)
	if string(a) != string(b) {
		t.Fatal("iznimka je promijenila pravila mjerenja")
	}
	if string(a) == string(c) {
		t.Fatal("promjena praga mora promijeniti pravila mjerenja")
	}
}

func TestSazetakPadaPokazujeStoJePalo(t *testing.T) {
	log := []byte("ok  \tgocop/a\t1s\n--- FAIL: TestNesto (0.01s)\n    x_test.go:12: krivo\nFAIL\tgocop/b\t2s\npanic: test timed out after 30m0s\nok  \tgocop/c\t1s\n")
	got := sazetakPada(log)
	for _, ima := range []string{"--- FAIL: TestNesto", "FAIL\tgocop/b", "test timed out"} {
		if !strings.Contains(got, ima) {
			t.Errorf("sažetak nema %q:\n%s", ima, got)
		}
	}
	if strings.Contains(got, "gocop/a") || sazetakPada([]byte("ok  \tgocop/a\t1s\n")) != "" {
		t.Errorf("sažetak sadrži uspješne pakete:\n%s", got)
	}
}

func TestRaspodjelaCRAPPokazujeCudovista(t *testing.T) {
	// 18 urednih funkcija i dva čudovišta: prosjek je velik, medijan nije
	v := []float64{1, 1, 2, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 8, 9, 12, 25, 72, 48180}
	r := raspodjela(v)
	if r.Medijan != 5 || r.P90 != 25 || r.P95 != 72 {
		t.Errorf("medijan/P90/P95: %+v", r)
	}
	if r.DoDeset != 16 || r.DoTrideset != 2 || r.DoSto != 1 || r.PrekoSto != 1 {
		t.Errorf("razredi: %+v", r)
	}
	if (raspodjela(nil) != raspodjelaCRAP{}) || raspodjela([]float64{3, 7, 1}).Medijan != 3 {
		t.Error("prazan ili neparan skup")
	}
}
