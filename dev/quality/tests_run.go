package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
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
	if err := logged(o.root, filepath.Join(o.out, "tests.log"), "go", append([]string{"test", "-race", "-covermode=atomic", "-coverprofile=" + profile, "-count=1", "-timeout=10m"}, paketi...)...); err != nil {
		r.Tests = "FAIL"
		log, readErr := os.ReadFile(filepath.Join(o.out, "tests.log"))
		if readErr == nil && bytes.Contains(log, []byte("DATA RACE")) {
			r.Race = "FAIL"
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
