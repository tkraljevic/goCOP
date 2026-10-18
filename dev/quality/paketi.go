package main

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// goPaketi su paketi glavnog modula koje git vidi (praćene ili nove, a ne
// ignorirane datoteke), s najduljim testovima na početku (poTrajanju).
// Lokalne mape izvan repozitorija, npr. tools/ iz .gitignore, tako ne ulaze
// ni u testove, ni u lint, ni u mjerenje: inače lokalna provjera mjeri drugi
// kod od CI-ja i javlja lažne regresije.
func goPaketi(root string) ([]string, error) {
	dirs, err := commandOutput(root, "go", "list", "-f", "{{.Dir}}", "./...")
	if err != nil {
		return nil, err
	}
	vidljive, err := commandOutput(root, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", "*.go")
	if err != nil {
		return nil, err
	}
	return poTrajanju(odaberiPakete(root, strings.Split(strings.TrimSpace(string(dirs)), "\n"), strings.Split(string(vidljive), "\x00"))), nil
}

// dugiPaketi su paketi s najduljim testovima, od najduljeg. go test pokreće
// testne binarke redom kojim su paketi navedeni (izmjereno 18. 10. 2026.,
// go test -json s -p 2 i popisom naprijed i natrag), pa ovi kreću prvi.
// Abecedno je internal/prognoza, najdulja, tek 37. od 53 paketa: lokalno uz
// četiri paketa usporedo (-p 4, kao na stroju CI-ja s malo jezgri) testovi
// su tako trajali 225 s, a s njom na početku 200 s, koliko traje sama.
// Trajanja su iz go test koji pokreće dev/quality (race uz iznimke, atomski
// coverage), 18. 10. 2026., macOS, 14 jezgri: prognoza 200 s, peers 72, web
// 59, service 43, repository 21, db 17, cmd/gocop 15; ostali do 14 s,
// većina 2–3 s. Ostali paketi idu abecedno. Popis ne mora biti točan: novi
// dugi paket samo kreće kasnije, a preimenovani javi TestDugiPaketiKrecuPrvi.
var dugiPaketi = []string{
	"./internal/prognoza",
	"./internal/peers",
	"./internal/web",
	"./internal/service",
	"./internal/repository",
	"./internal/db",
	"./cmd/gocop",
}

// poTrajanju stavlja pakete iz dugiPaketi na početak, redom s popisa, a
// ostale ostavlja redom kojim su došli
func poTrajanju(paketi []string) []string {
	out := make([]string, 0, len(paketi))
	for _, p := range dugiPaketi {
		if slices.Contains(paketi, p) {
			out = append(out, p)
		}
	}
	for _, p := range paketi {
		if !slices.Contains(dugiPaketi, p) {
			out = append(out, p)
		}
	}
	return out
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
