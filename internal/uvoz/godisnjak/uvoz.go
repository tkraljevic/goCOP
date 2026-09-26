package godisnjak

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// Zadatak je jedan uvoz: koji godišnjaci, koje postaje i kamo.
type Zadatak struct {
	Godisnjaci []string          // putanje do PDF-ova
	Postaje    map[string]string // šifra postaje u godišnjaku → naša letva
	Koren      string            // korijen stabla vodostaji/
	Sliv       string
	Probno     bool
	Popis      bool // samo ispiši postaje koje godišnjaci sadrže
	Dnevnik    io.Writer
}

// Ishod kaže što je zapisano.
type Ishod struct {
	Letve    []string
	Zapisano []string
}

// Dan2 je dan s godinom, jer niz ide kroz više godišnjaka.
type Dan2 struct{ Godina, Mjesec, Dan int }

// Prije kaže je li dan a prije dana b.
func (a Dan2) Prije(b Dan2) bool {
	if a.Godina != b.Godina {
		return a.Godina < b.Godina
	}
	if a.Mjesec != b.Mjesec {
		return a.Mjesec < b.Mjesec
	}
	return a.Dan < b.Dan
}

// Uvezi čita godišnjake i zapiše dnevni niz vodostaja svake tražene postaje.
func Uvezi(z Zadatak) (Ishod, error) {
	pisi := func(format string, a ...any) {
		if z.Dnevnik != nil {
			fmt.Fprintf(z.Dnevnik, format, a...)
		}
	}
	var ishod Ishod
	if len(z.Godisnjaci) == 0 {
		return ishod, fmt.Errorf("nema nijednog godišnjaka")
	}
	// letva → dan → vrijednost
	nizovi := map[string]map[Dan2]int{}
	var upozorenja []string

	for _, put := range z.Godisnjaci {
		tekst, err := TekstPDF(put)
		if err != nil {
			return ishod, fmt.Errorf("%s: %w", filepath.Base(put), err)
		}
		if z.Popis {
			for _, t := range Citaj(tekst) {
				pisi("%-8s %-22s %d.  dana %3d\n", t.Sifra, t.Naziv, t.Godina, len(t.Dani))
			}
			continue
		}
		nadjeno := 0
		for _, t := range Citaj(tekst) {
			letva, hocemo := z.Postaje[t.Sifra]
			if !hocemo {
				continue
			}
			provjereno, greske := t.Provjeri()
			for _, g := range greske {
				upozorenja = append(upozorenja, fmt.Sprintf("%s, %s %d.: %s", letva, filepath.Base(put), t.Godina, g))
			}
			if nizovi[letva] == nil {
				nizovi[letva] = map[Dan2]int{}
			}
			for d, v := range t.Dani {
				nizovi[letva][Dan2{t.Godina, d.Mjesec, d.Dan}] = v
			}
			nadjeno++
			pisi("%-10s %d.  dana %3d, provjerenih mjeseci %2d, s oznakom leda ili uspora %3d%s\n",
				letva, t.Godina, len(t.Dani), provjereno, t.Oznacenih, akoGreske(greske))
		}
		if nadjeno == 0 {
			upozorenja = append(upozorenja, filepath.Base(put)+": nijedna tražena postaja nije nađena")
		}
	}
	if z.Popis {
		return ishod, nil
	}
	for _, r := range upozorenja {
		pisi("  upozorenje: %s\n", r)
	}

	letve := make([]string, 0, len(nizovi))
	for l := range nizovi {
		letve = append(letve, l)
	}
	sort.Strings(letve)
	for _, letva := range letve {
		dani := nizovi[letva]
		if len(dani) == 0 {
			continue
		}
		popis := make([]Dan2, 0, len(dani))
		for d := range dani {
			popis = append(popis, d)
		}
		sort.Slice(popis, func(i, j int) bool { return popis[i].Prije(popis[j]) })
		od, do := popis[0].Godina, popis[len(popis)-1].Godina
		ime := fmt.Sprintf("%s_hidmet_vodostaj_srednjak_%d-%d.csv", letva, od, do)
		put := filepath.Join(z.Koren, z.Sliv, letva, ime)
		pisi("\n%-10s %6d dana, %d.-%d.\n              → %s\n", letva, len(dani), od, do, ime)
		ishod.Letve = append(ishod.Letve, letva)
		if z.Probno {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
			return ishod, err
		}
		f, err := os.Create(put)
		if err != nil {
			return ishod, err
		}
		fmt.Fprintln(f, "datum;vodostaj_cm")
		for _, d := range popis {
			fmt.Fprintf(f, "%04d-%02d-%02d;%d\n", d.Godina, d.Mjesec, d.Dan, dani[d])
		}
		if err := f.Close(); err != nil {
			return ishod, err
		}
		ishod.Zapisano = append(ishod.Zapisano, put)
	}
	if z.Probno {
		pisi("\nproba — ništa nije zapisano\n")
		return ishod, nil
	}
	pisi("\nzapisano %d nizova\n", len(ishod.Zapisano))
	return ishod, nil
}

func akoGreske(g []string) string {
	if len(g) == 0 {
		return ""
	}
	return fmt.Sprintf("  — %d mjeseci se ne slaže sa sažetkom", len(g))
}

// ImaPDFAlat kaže može li ovo računalo čitati godišnjake.
func ImaPDFAlat() bool {
	_, err := exec.LookPath("pdftotext")
	return err == nil
}

// TekstPDF vadi tekst iz godišnjaka, s poravnanjem stupaca kakvo tablica i
// ima; bez -layout se stupci sliju i tablica se ne da pročitati. Tekst vadi
// pdftotext (paket poppler); skenirani godišnjak bez sloja teksta daje prazno.
func TekstPDF(put string) (string, error) {
	if !ImaPDFAlat() {
		return "", fmt.Errorf("na ovom računalu nema programa pdftotext (paket poppler), a bez njega se godišnjak ne može pročitati")
	}
	cmd := exec.Command("pdftotext", "-layout", put, "-")
	cmd.WaitDelay = 5 * time.Second
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pdftotext: %w", err)
	}
	return string(b), nil
}
