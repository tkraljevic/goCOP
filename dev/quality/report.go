package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func percent(c coverage) string {
	if c.Percent == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.2f %%", *c.Percent)
}

func printSummary(r report) {
	if r.Measurements != "PASS" {
		fmt.Println("UPOZORENJE: mjerenje nije dovršeno; brojčane metrike nisu valjane.")
	}
	fmt.Printf("\ngoCOP CODE HEALTH\n────────────────────────────────\nStatic analysis       %s (%d errors, %d warnings)\nTests                 %s\nRace detector         %s\nCoverage              %s\nCritical coverage     %s\nAverage CRAP          %.2f\nWorst CRAP            %.2f\nAverage complexity    %.2f\nMax complexity        %d\nExact duplication     %.2f %%\nFuzzy candidates      %.2f %%\nMutation              %s\n────────────────────────────────\nSTATUS                %s\n", r.Static.Status, r.Static.Errors, r.Static.Warnings, r.Tests, r.Race, percent(r.Coverage), percent(r.CriticalCoverage), r.AverageCRAP, r.MaxCRAP, r.AverageComplexity, r.MaxComplexity, r.Exact.Percent, r.Fuzzy.Percent, r.MutationStatus, r.Status)
}

func sortedFunctions(r report, byCRAP bool) []function {
	fs := append([]function(nil), r.Functions...)
	sort.Slice(fs, func(i, j int) bool {
		a, b := float64(fs[i].Complexity), float64(fs[j].Complexity)
		if byCRAP {
			a, b = -1, -1
			if fs[i].CRAP != nil {
				a = *fs[i].CRAP
			}
			if fs[j].CRAP != nil {
				b = *fs[j].CRAP
			}
		}
		if a == b {
			return fs[i].ID < fs[j].ID
		}
		return a > b
	})
	return fs
}

func functionTable(b *bytes.Buffer, fs []function, limit int) {
	fmt.Fprintln(b, "\n| Funkcija | CC | Coverage | CRAP | Kritična |\n|---|---:|---:|---:|:---:|")
	for _, f := range fs[:min(limit, len(fs))] {
		score := "N/A"
		if f.CRAP != nil {
			score = fmt.Sprintf("%.2f", *f.CRAP)
		}
		fmt.Fprintf(b, "| `%s:%d` · `%s` | %d | %s | %s | %t |\n", f.File, f.Line, f.Name, f.Complexity, percent(f.Coverage), score, f.Critical)
	}
}

func markdown(r report) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "# goCOP Code Health\n\n- Commit: `%s` (dirty: %t)\n- Vrijeme: %s\n- Platforma: %s, %s\n- Konfiguracija: `%s`\n- Status: **%s**\n\n| Metrika | Rezultat |\n|---|---:|\n| Testovi | %s |\n| Race | %s |\n| Statičke greške / upozorenja | %d / %d |\n| Coverage | %s |\n| Kritični coverage | %s |\n| Prosječni / najveći CRAP | %.2f / %.2f |\n| Prosječna / najveća kompleksnost | %.2f / %d |\n| Exact duplikacija (tokeni tijela funkcija) | %.2f %% |\n| Fuzzy kandidati (uključuju exact) | %.2f %% |\n| Mutation | %s |\n", r.Commit, r.Dirty, r.Timestamp, r.Platform, r.GoVersion, r.ConfigHash, r.Status, r.Tests, r.Race, r.Static.Errors, r.Static.Warnings, percent(r.Coverage), percent(r.CriticalCoverage), r.AverageCRAP, r.MaxCRAP, r.AverageComplexity, r.MaxComplexity, r.Exact.Percent, r.Fuzzy.Percent, r.MutationStatus)
	fmt.Fprintln(&b, "\n## Top 10 CRAP")
	functionTable(&b, sortedFunctions(r, true), 10)
	fmt.Fprintln(&b, "\n## Top 10 kompleksnost")
	functionTable(&b, sortedFunctions(r, false), 10)
	fmt.Fprintln(&b, "\n## Coverage po paketu\n\n| Paket | Coverage | Pokrivene / ukupne naredbe |\n|---|---:|---:|")
	packages := make([]string, 0, len(r.Packages))
	for p := range r.Packages {
		packages = append(packages, p)
	}
	sort.Strings(packages)
	for _, p := range packages {
		c := r.Packages[p]
		fmt.Fprintf(&b, "| `%s` | %s | %d / %d |\n", p, percent(c), c.Covered, c.Statements)
	}
	fmt.Fprintln(&b, "\n## Statička analiza\n\n| Linter | Nalazi |\n|---|---:|")
	names := make([]string, 0, len(r.Static.Counts))
	for n := range r.Static.Counts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&b, "| %s | %d |\n", n, r.Static.Counts[n])
	}
	for _, entry := range []struct {
		name string
		d    duplication
	}{{"Exact blokovi — primjeri", r.Exact}, {"Fuzzy funkcije — kandidati za pregled", r.Fuzzy}} {
		fmt.Fprintf(&b, "\n## %s\n\n", entry.name)
		for _, p := range entry.d.Pairs[:min(20, len(entry.d.Pairs))] {
			fmt.Fprintf(&b, "- `%s` ↔ `%s` (%.1f%%, najmanje %d tokena).\n", p.A, p.B, 100*p.Similarity, p.Tokens)
		}
	}
	for _, entry := range []struct {
		name  string
		items []string
	}{{"Blokade i regresije", r.Failures}, {"Upozorenja / zatečeni dug", r.Warnings}, {"Primijenjene obrazložene iznimke", r.Exceptions}} {
		fmt.Fprintf(&b, "\n## %s\n\n", entry.name)
		if len(entry.items) == 0 {
			fmt.Fprintln(&b, "Nema.")
		}
		for _, item := range entry.items {
			fmt.Fprintf(&b, "- %s\n", item)
		}
	}
	fmt.Fprintln(&b, "\nSve funkcije i njihova pokrivenost nalaze se u `code-health.json`; izvorni Go izvještaj u `coverage-functions.txt`, a nepokrivene naredbe u `coverage.html`. Fuzzy nije dokaz iste odgovornosti. Pogledati `docs/CODE_QUALITY.md` za metodologiju i ograničenja.")
	return b.Bytes()
}

func writeReport(out string, r report) error {
	if err := writeJSON(filepath.Join(out, "code-health.json"), r); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "code-health.md"), markdown(r), 0644)
}
