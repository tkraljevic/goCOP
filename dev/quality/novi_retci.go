package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// noviRetci su retci Go datoteka dodani ili promijenjeni od commita od do
// radne kopije, s nespremljenim izmjenama (kao i otisak izvora). Datoteka
// koju git još ne prati nova je cijela (nil). Nisu novi redak koji se samo
// pomaknuo jer je iznad njega nešto dodano ili maknuto, redak kojem se
// promijenio samo razmak (-w) ni preimenovana datoteka (-M). Kod izrezan i
// zalijepljen na drugo mjesto jest nov, kao i u brojanju po ključu kad ode
// u drugu datoteku. Prefiksi a/ i b/ zadani su izričito, da diff.noprefix ili
// diff.mnemonicPrefix u postavkama gita ne promijene zapis koji se čita.
func noviRetci(root, od string) (map[string]map[int]bool, error) {
	if od == "" {
		return nil, fmt.Errorf("referentna točka nema commita")
	}
	diff, err := commandOutput(root, "git", "-c", "core.quotePath=false", "diff", "--no-ext-diff", "--no-color", "--relative", "--src-prefix=a/", "--dst-prefix=b/", "-M", "-w", "--unified=0", od, "--", "*.go")
	if err != nil {
		return nil, err
	}
	retci := retciIzRazlike(string(diff))
	nove, err := commandOutput(root, "git", "ls-files", "-z", "--others", "--exclude-standard", "--", "*.go")
	if err != nil {
		return nil, err
	}
	for _, p := range strings.Split(string(nove), "\x00") {
		if p != "" {
			retci[p] = nil
		}
	}
	return retci, nil
}

// retciIzRazlike čita git diff --unified=0. Ime datoteke čita se samo u
// zaglavlju datoteke, prije prvog odsječka, da dodani redak koji počinje s
// „++ ” ne postane ime datoteke. Obrisana datoteka (+++ /dev/null) nema
// novih redaka.
func retciIzRazlike(diff string) map[string]map[int]bool {
	retci := map[string]map[int]bool{}
	datoteka, zaglavlje := "", false
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "diff --git "):
			datoteka, zaglavlje = "", true
		case zaglavlje && strings.HasPrefix(l, "+++ b/"):
			datoteka = strings.TrimPrefix(l, "+++ b/")
		case strings.HasPrefix(l, "@@"):
			zaglavlje = false
			dodajOdsjecak(retci, datoteka, l)
		}
	}
	return retci
}

var odsjecak = regexp.MustCompile(`^@@ -[0-9,]+ \+([0-9]+)(?:,([0-9]+))? @@`)

// dodajOdsjecak upisuje retke nove strane odsječka „@@ -a,b +c,d @@”, retke
// c … c+d-1. Bez d odsječak je jedan redak, a d = 0 znači samo brisanje.
func dodajOdsjecak(retci map[string]map[int]bool, datoteka, zaglavlje string) {
	m := odsjecak.FindStringSubmatch(zaglavlje)
	if m == nil || datoteka == "" {
		return
	}
	prvi, _ := strconv.Atoi(m[1]) // regex propušta samo znamenke
	n := 1
	if m[2] != "" {
		n, _ = strconv.Atoi(m[2]) // regex propušta samo znamenke
	}
	if retci[datoteka] == nil {
		retci[datoteka] = map[int]bool{}
	}
	for i := prvi; i < prvi+n; i++ {
		retci[datoteka][i] = true
	}
}

// novRedak: je li redak nov ili promijenjen (nil je cijela nova datoteka)
func novRedak(retci map[string]map[int]bool, datoteka string, redak int) bool {
	r, ok := retci[datoteka]
	return ok && (r == nil || r[redak])
}

// neprovjerenaGreska je jedan nalaz errchecka: gdje je i koji poziv vraća
// grešku
type neprovjerenaGreska struct {
	datoteka string
	redak    int
	poziv    string
}

var imePoziva = regexp.MustCompile("`([^`]+)`")

// nalaziErrchecka su mjesta svih nalaza errchecka, svako jednom
func nalaziErrchecka(r *report) map[neprovjerenaGreska]bool {
	nalazi := map[neprovjerenaGreska]bool{}
	for key, mjesta := range r.lintMjesta {
		poziv := "Poziv"
		if m := imePoziva.FindStringSubmatch(key); m != nil {
			poziv = "`" + m[1] + "`"
		}
		for _, m := range mjesta {
			if m.linter == "errcheck" {
				nalazi[neprovjerenaGreska{m.datoteka, m.redak, poziv}] = true
			}
		}
	}
	return nalazi
}

// provjeriNoveGreske: neprovjerena greška (nalaz errchecka) u novom ili
// promijenjenom retku ruši vrata, koliko god ih je već bilo u datoteci
// (Odluke (17. 10. 2026.)). Brojanje nalaza po ključu to ne vidi kad se
// jedan stari nalaz makne, a drugi doda. Stari retci su dug i ne ruše.
// Bezopasni pozivi su u exclude-functions u .golangci.yml, a izričito
// zanemarivanje s razlogom u istom retku errcheck ne javlja. Ako se ne zna
// koji su retci novi, to je blokada, ne propusnica.
func provjeriNoveGreske(r *report, od string) {
	nalazi := nalaziErrchecka(r)
	if len(nalazi) == 0 {
		return
	}
	if r.noviRetciOd == nil {
		r.Failures = append(r.Failures, "neprovjerene greške: mjerenje ne zna koji su retci novi, pa ih ne može provjeriti")
		return
	}
	retci, err := r.noviRetciOd(od)
	if err != nil {
		r.Failures = append(r.Failures, fmt.Sprintf("neprovjerene greške: ne zna se koji su retci novi od %s, pa ih se ne može provjeriti: %v", od, err))
		return
	}
	for g := range nalazi {
		if novRedak(retci, g.datoteka, g.redak) {
			r.Failures = append(r.Failures, fmt.Sprintf("%s:%d: %s vraća grešku koju nitko ne gleda, a redak je nov ili promijenjen. Obradite grešku ili je zanemarite s razlogom u istom retku: _ = … // razlog", g.datoteka, g.redak, g.poziv))
		}
	}
}
