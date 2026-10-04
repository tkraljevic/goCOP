// Command quality is development infrastructure, not part of the goCOP binary.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type options struct {
	root, policy, out, baseline, mutationRoot, mutationPackage string
	record, reportOnly, reuseTests                             bool
}

func main() {
	var o options
	flag.StringVar(&o.root, "root", "../..", "stablo koje se mjeri")
	flag.StringVar(&o.policy, "policy", "../..", "stablo s quality/config.json i .golangci.yml")
	flag.StringVar(&o.out, "out", "", "izlaz (zadano ROOT/.quality)")
	flag.StringVar(&o.baseline, "baseline", "", "prihvaćeni baseline JSON")
	flag.BoolVar(&o.record, "record-baseline", false, "snimi referentno mjerenje; postojeće ozbiljne statičke greške i dalje blokiraju quality gate")
	flag.BoolVar(&o.reportOnly, "report-only", false, "regresije prijavi, ali vrati nulu ako mjerenje uspije; nije CI gate")
	flag.BoolVar(&o.reuseTests, "reuse-tests", false, "ponovno analiziraj prethodno uspješne testove, samo uz identične izvore/Go/platformu")
	flag.StringVar(&o.mutationRoot, "mutation-root", "", "zasebna čista kopija za Gremlins")
	flag.StringVar(&o.mutationPackage, "mutation-package", "", "npr. internal/models")
	flag.Parse()
	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func commandOutput(dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", name, err, stderr.String())
	}
	return out, nil
}

func logged(dir, path, name string, args ...string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = f
	cmd.Stderr = f
	runErr := cmd.Run()
	closeErr := f.Close()
	if runErr != nil {
		return fmt.Errorf("%s: %w (log: %s)", name, runErr, path)
	}
	return closeErr
}

func normalize(o options) (options, error) {
	var err error
	o.root, err = filepath.Abs(o.root)
	if err != nil {
		return o, err
	}
	o.policy, err = filepath.Abs(o.policy)
	if err != nil {
		return o, err
	}
	if o.out == "" {
		o.out = filepath.Join(o.root, ".quality")
	}
	o.out, err = filepath.Abs(o.out)
	if err != nil {
		return o, err
	}
	if o.baseline == "" {
		o.baseline = filepath.Join(o.policy, "quality/baseline.json")
	}
	o.baseline, err = filepath.Abs(o.baseline)
	if err != nil {
		return o, err
	}
	if o.record && o.reportOnly {
		return o, fmt.Errorf("record-baseline i report-only ne mogu zajedno")
	}
	return o, os.MkdirAll(o.out, 0755)
}

func run(o options) error {
	o, err := normalize(o)
	if err != nil {
		return err
	}
	if o.mutationRoot != "" {
		return mutation(o)
	}
	c, err := loadConfig(filepath.Join(o.policy, "quality/config.json"))
	if err != nil {
		return err
	}
	r := report{Schema: 1, Analyzer: analyzerVersion, Timestamp: time.Now().UTC().Format(time.RFC3339), Platform: runtime.GOOS + "/" + runtime.GOARCH, Tests: "INCOMPLETE", Race: "INCOMPLETE", MutationStatus: "NOT_RUN", Static: lintSummary{Status: "INCOMPLETE"}}
	r.Measurements = "INCOMPLETE"
	commit, err := commandOutput(o.root, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	r.Commit = strings.TrimSpace(string(commit))
	status, err := commandOutput(o.root, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	r.Dirty = len(status) > 0
	if o.record && r.Dirty {
		return fmt.Errorf("baseline prihvatite iz čiste verzionirane kopije, ne iz nedovršenih izmjena")
	}
	version, err := commandOutput(o.root, "go", "env", "GOVERSION")
	if err != nil {
		return err
	}
	r.GoVersion = strings.TrimSpace(string(version))
	r.SourceHash, err = sourceHash(o.root)
	if err != nil {
		return err
	}
	r.ConfigHash, err = policyHash(o.policy)
	if err != nil {
		return err
	}
	var baseline *report
	if !o.record {
		var b report
		if err := readJSON(o.baseline, &b); err == nil {
			baseline = &b
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	fmt.Printf("goCOP CODE HEALTH · %s · %s\nTestovi + race + coverage… (log: %s)\n", r.Commit[:12], r.Platform, filepath.Join(o.out, "tests.log"))
	measurementFailed := false
	if err := runTests(o, &r); err != nil {
		measurementFailed = true
		r.Failures = append(r.Failures, err.Error())
	}
	fmt.Println("Statička analiza…")
	if err := runLint(o, c, &r); err != nil {
		measurementFailed = true
		r.Failures = append(r.Failures, err.Error())
	}
	if r.Tests == "PASS" {
		if err := runMeasurements(o, c, &r); err != nil {
			measurementFailed = true
			r.Failures = append(r.Failures, err.Error())
		} else {
			r.Measurements = "PASS"
		}
	}
	after, err := sourceHash(o.root)
	if err != nil || after != r.SourceHash {
		measurementFailed = true
		r.Failures = append(r.Failures, "izvor se promijenio tijekom mjerenja; rezultat nije konzistentan, ponovite u izoliranoj kopiji")
	}
	policyAfter, err := policyHash(o.policy)
	if err != nil || policyAfter != r.ConfigHash {
		measurementFailed = true
		r.Failures = append(r.Failures, "konfiguracija se promijenila tijekom mjerenja; ponoviti analizu")
	}
	executionFailed := len(r.Failures) > 0 || r.Tests != "PASS" || r.Race != "PASS" || r.Static.Status != "PASS" || r.Measurements != "PASS"
	evaluate(&r, baseline, c, o.record)
	if err := writeReport(o.out, r); err != nil {
		return err
	}
	printSummary(r)
	if o.record {
		if measurementFailed {
			return fmt.Errorf("baseline NIJE spremljen: mjerenje/testovi/race nisu pouzdano završili")
		}
		if err := writeJSON(o.baseline, r); err != nil {
			return err
		}
		fmt.Println("Baseline spremljen:", o.baseline)
		if r.Status == "FAIL" {
			fmt.Println("Referentno stanje sadrži statičke greške. Snimanje baselinea ih NE oslobađa quality gatea.")
		}
		return nil
	}
	if len(r.Failures) > 0 && (!o.reportOnly || executionFailed) {
		return fmt.Errorf("quality gate: %d nalaza; pogledajte %s", len(r.Failures), filepath.Join(o.out, "code-health.md"))
	}
	return nil
}

// policyHash je otisak pravila mjerenja. Iznimke i premještaji ne ulaze u
// njega: oni ne mijenjaju mjerenje nego samo propusnicu, pa nova ili istekla
// iznimka ne smije učiniti baseline neusporedivim (istekla i dalje pada u
// evaluate).
func policyHash(root string) (string, error) {
	cfg, err := os.ReadFile(filepath.Join(root, "quality/config.json"))
	if err != nil {
		return "", err
	}
	pravila, err := bezIznimki(cfg)
	if err != nil {
		return "", err
	}
	lint, err := os.ReadFile(filepath.Join(root, ".golangci.yml"))
	if err != nil {
		return "", err
	}
	data := append(pravila, lint...)
	return digest(append(data, []byte(analyzerVersion+":"+lintVersion)...)), nil
}

// bezIznimki je konfiguracija bez popisa iznimaka i premještaja, u stalnom
// obliku
func bezIznimki(cfg []byte) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(cfg, &m); err != nil {
		return nil, fmt.Errorf("quality/config.json: %w", err)
	}
	delete(m, "exceptions")
	delete(m, "premjestaji")
	return json.Marshal(m)
}

func runLint(o options, c config, r *report) error {
	tool := filepath.Join(o.policy, "bin/quality/golangci-lint")
	version, err := commandOutput(o.root, tool, "version")
	if err != nil {
		return fmt.Errorf("pokrenite make quality-tools: %w", err)
	}
	if !strings.Contains(string(version), "version "+lintVersion+" ") {
		return fmt.Errorf("potreban golangci-lint %s, dobiven %s", lintVersion, version)
	}
	path := filepath.Join(o.out, "lint.json")
	paketi, err := goPaketi(o.root)
	if err != nil {
		return err
	}
	if err := logged(o.root, filepath.Join(o.out, "lint.log"), tool, append([]string{"run", "--config", filepath.Join(o.policy, ".golangci.yml"), "--issues-exit-code", "0", "--output.json.path", path}, paketi...)...); err != nil {
		return err
	}
	return measureLint(path, o.root, c, r)
}

func runMeasurements(o options, c config, r *report) error {
	profile, err := readProfile(filepath.Join(o.out, "coverage.out"))
	if err != nil {
		return err
	}
	files, module, err := listSources(o.root)
	if err != nil {
		return err
	}
	bodies, err := measure(files, module, profile, c, r)
	if err != nil {
		return err
	}
	fmt.Printf("Kompleksnost, CRAP i duplikacije (%d funkcija)…\n", len(bodies))
	r.Exact = exactClones(bodies, c.ExactMinTokens)
	r.Fuzzy = fuzzyClones(bodies, c.FuzzyMinTokens, c.FuzzySimilarity)
	if err := logged(o.root, filepath.Join(o.out, "coverage-functions.txt"), "go", "tool", "cover", "-func="+filepath.Join(o.out, "coverage.out")); err != nil {
		return err
	}
	return logged(o.root, filepath.Join(o.out, "coverage-html.log"), "go", "tool", "cover", "-html="+filepath.Join(o.out, "coverage.out"), "-o", filepath.Join(o.out, "coverage.html"))
}
