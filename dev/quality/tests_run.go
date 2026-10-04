package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type testEvidence struct{ Source, Platform, GoVersion, ProfileHash, LogHash string }

func evidence(o options, r *report) (testEvidence, error) {
	p, err := os.ReadFile(filepath.Join(o.out, "coverage.out"))
	if err != nil {
		return testEvidence{}, err
	}
	l, err := os.ReadFile(filepath.Join(o.out, "tests.log"))
	if err != nil {
		return testEvidence{}, err
	}
	return testEvidence{r.SourceHash, r.Platform, r.GoVersion, digest(p), digest(l)}, nil
}

func runTests(o options, r *report) error {
	receipt := filepath.Join(o.out, "test-evidence.json")
	if o.reuseTests {
		var saved testEvidence
		if err := readJSON(receipt, &saved); err != nil {
			return err
		}
		current, err := evidence(o, r)
		if err != nil {
			return err
		}
		if saved != current {
			return fmt.Errorf("test evidence nije za iste izvore/Go/platformu ili su izvještaji promijenjeni")
		}
		fmt.Println("Ponovna analiza provjerenog testnog izvještaja (testovi nisu ponovno pokrenuti).")
		r.Tests, r.Race = "PASS", "PASS"
		return nil
	}
	profile := filepath.Join(o.out, "coverage.out")
	paketi, err := goPaketi(o.root)
	if err != nil {
		return err
	}
	if err := logged(o.root, filepath.Join(o.out, "tests.log"), "go", append([]string{"test", "-race", "-covermode=atomic", "-coverprofile=" + profile, "-count=1", "-timeout=" + rokPaketa}, paketi...)...); err != nil {
		r.Tests = "FAIL"
		log, readErr := os.ReadFile(filepath.Join(o.out, "tests.log"))
		if readErr == nil && bytes.Contains(log, []byte("DATA RACE")) {
			r.Race = "FAIL"
		}
		if readErr == nil {
			fmt.Print(sazetakPada(log))
		}
		return err
	}
	after, err := sourceHash(o.root)
	if err != nil {
		return err
	}
	if after != r.SourceHash {
		return fmt.Errorf("izvor se promijenio tijekom testiranja")
	}
	r.Tests, r.Race = "PASS", "PASS"
	e, err := evidence(o, r)
	if err != nil {
		return err
	}
	return writeJSON(receipt, e)
}

// rokPaketa je rok jednog testnog paketa. Race i atomski coverage zajedno
// usporavaju testove nekoliko puta; najveći paket (internal/web) lokalno
// traje oko 3,5 minute, a na sporijem stroju CI-ja i uz ostale pakete
// usporedo prelazio je 10 minuta.
const rokPaketa = "30m"

// sazetakPada su retci zapisa testova koji kažu što je palo (test, panika,
// istek roka, utrka), da se vide u ispisu i kad zapis nije priložen
func sazetakPada(log []byte) string {
	var b strings.Builder
	n := 0
	for _, redak := range strings.Split(string(log), "\n") {
		t := strings.TrimSpace(redak)
		if strings.HasPrefix(t, "--- FAIL") || strings.HasPrefix(t, "FAIL") || strings.HasPrefix(t, "panic:") ||
			strings.Contains(t, "test timed out") || strings.Contains(t, "WARNING: DATA RACE") || strings.HasPrefix(t, "Error Trace") {
			if n == 0 {
				b.WriteString("Testovi su pali:\n")
			}
			b.WriteString("  " + t + "\n")
			if n++; n == 40 {
				b.WriteString("  …\n")
				break
			}
		}
	}
	return b.String()
}
