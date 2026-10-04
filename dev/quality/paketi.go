package main

import (
	"path/filepath"
	"sort"
	"strings"
)

// goPaketi su paketi glavnog modula koje git vidi (praćene ili nove, a ne
// ignorirane datoteke). Lokalne mape izvan repozitorija, npr. tools/ iz
// .gitignore, tako ne ulaze ni u testove, ni u lint, ni u mjerenje: inače
// lokalna provjera mjeri drugi kod od CI-ja i javlja lažne regresije.
func goPaketi(root string) ([]string, error) {
	dirs, err := commandOutput(root, "go", "list", "-f", "{{.Dir}}", "./...")
	if err != nil {
		return nil, err
	}
	vidljive, err := commandOutput(root, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", "*.go")
	if err != nil {
		return nil, err
	}
	return odaberiPakete(root, strings.Split(strings.TrimSpace(string(dirs)), "\n"), strings.Split(string(vidljive), "\x00")), nil
}

// odaberiPakete zadržava pakete (apsolutne mape iz go list) koji imaju bar
// jednu Go datoteku koju git vidi (putanje relativne prema root), kao
// uzorke za go alate (./internal/web)
func odaberiPakete(root string, dirs, vidljive []string) []string {
	imaVidljivu := map[string]bool{}
	for _, p := range vidljive {
		if p != "" {
			imaVidljivu[filepath.ToSlash(filepath.Dir(p))] = true
		}
	}
	var out []string
	for _, d := range dirs {
		if d == "" {
			continue
		}
		rel, err := filepath.Rel(root, d)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		rel = filepath.ToSlash(rel)
		if !imaVidljivu[rel] {
			continue
		}
		if rel == "." {
			out = append(out, ".")
		} else {
			out = append(out, "./"+rel)
		}
	}
	sort.Strings(out)
	return out
}
