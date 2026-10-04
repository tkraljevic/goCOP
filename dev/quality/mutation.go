package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func mutation(o options) error {
	root, err := filepath.EvalSymlinks(o.root)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(o.mutationRoot)
	if err != nil {
		return err
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	if target == root || strings.HasPrefix(target, root+string(os.PathSeparator)) || strings.HasPrefix(root, target+string(os.PathSeparator)) {
		return fmt.Errorf("mutation-root mora biti zasebna kopija izvan radnog projekta")
	}
	p := filepath.ToSlash(filepath.Clean(o.mutationPackage))
	if !strings.HasPrefix(p, "internal/") || strings.Contains(p, "..") || strings.ContainsAny(p, "*\\") {
		return fmt.Errorf("navedite jedan paket, npr. MUTATION_PACKAGE=internal/models")
	}
	top, err := commandOutput(target, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(top)) != target {
		return fmt.Errorf("mutation-root nije korijen kopije")
	}
	status, err := commandOutput(target, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if len(status) > 0 {
		return fmt.Errorf("mutation kopija mora biti čista")
	}
	commit, err := commandOutput(target, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	var r report
	if err := readJSON(filepath.Join(o.out, "code-health.json"), &r); err != nil {
		return fmt.Errorf("prvo izmjerite istu čistu verziju s make quality: %w", err)
	}
	if r.Dirty || r.Commit != strings.TrimSpace(string(commit)) {
		return fmt.Errorf("mutation i code-health moraju biti ista čista verzija")
	}
	path := filepath.Join(o.out, "mutation.json")
	if err := logged(filepath.Join(target, p), filepath.Join(o.out, "mutation.log"), filepath.Join(o.policy, "bin/quality/gremlins"), "unleash", "--workers", "2", "--output", path); err != nil {
		return err
	}
	var result struct {
		TestEfficacy *float64 `json:"test_efficacy"`
		MutantsTotal int      `json:"mutants_total"`
	}
	if err := readJSON(path, &result); err != nil {
		return err
	}
	if result.TestEfficacy == nil || result.MutantsTotal == 0 {
		return fmt.Errorf("gremlins nije vratio valjano mjerenje")
	}
	r.MutationScore = result.TestEfficacy
	r.MutationStatus = fmt.Sprintf("%.2f%% · %s", *result.TestEfficacy, p)
	if *result.TestEfficacy < 80 {
		r.Warnings = append(r.Warnings, "mutation efficacy ispod 80% za "+p)
		if r.Status == "HEALTHY" {
			r.Status = "BASELINE_DEBT"
		}
	}
	return writeReport(o.out, r)
}
