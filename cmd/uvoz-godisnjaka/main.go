// uvoz-godisnjaka čita hidrološke godišnjake Republičkog hidrometeorološkog
// zavoda Srbije i iz njih slaže dnevne nizove vodostaja za naše stablo.
//
//	uvoz-godisnjaka -iz "vodostaji/PRISTUP RHMZ-SRBIJA/godisnjaci" \
//	                -postaje 42010=bezdan,42015=apatin,42020=bogojevo -probno
//
// Godišnjaci su PDF-ovi, pa tekst vadi pdftotext (paket poppler). Bez njega
// program javi što nedostaje i stane.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	iz := flag.String("iz", "", "godišnjak (PDF) ili mapa s godišnjacima")
	u := flag.String("u", "vodostaji", "korijen stabla s datotekama")
	sliv := flag.String("sliv", "dunav", "sliv u koji letve idu")
	postaje := flag.String("postaje", "", "koje postaje uzeti: šifra=letva, odvojeno zarezom")
	probno := flag.Bool("probno", false, "samo ispiši što bi se zapisalo")
	popis := flag.Bool("popis", false, "ispiši sve postaje koje godišnjak sadrži i stani")
	flag.Parse()

	if *iz == "" || (*postaje == "" && !*popis) {
		log.Fatal("trebaju -iz i -postaje")
	}
	zeljene := map[string]string{}
	for _, p := range strings.Split(*postaje, ",") {
		if strings.TrimSpace(p) == "" {
			continue
		}
		d := strings.SplitN(strings.TrimSpace(p), "=", 2)
		if len(d) != 2 {
			log.Fatalf("postaja %q nije u obliku šifra=letva", p)
		}
		zeljene[strings.TrimSpace(d[0])] = strings.TrimSpace(d[1])
	}

	puts, err := godisnjaci(*iz)
	if err != nil {
		log.Fatal(err)
	}
	if len(puts) == 0 {
		log.Fatalf("u %q nema nijednog godišnjaka", *iz)
	}

	// letva → dan → vrijednost
	nizovi := map[string]map[Dan2]int{}
	oznaceno := map[string]int{}
	var upozorenja []string

	for _, put := range puts {
		tekst, err := tekstPDF(put)
		if err != nil {
			log.Fatalf("%s: %v", filepath.Base(put), err)
		}
		if *popis {
			for _, t := range Citaj(tekst) {
				fmt.Printf("%-8s %-22s %d.  dana %3d\n", t.Sifra, t.Naziv, t.Godina, len(t.Dani))
			}
			continue
		}
		nadjeno := 0
		for _, t := range Citaj(tekst) {
			letva, hocemo := zeljene[t.Sifra]
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
			oznaceno[letva] += t.Oznacenih
			nadjeno++
			fmt.Printf("%-10s %d.  dana %3d, provjerenih mjeseci %2d, s oznakom leda ili uspora %3d%s\n",
				letva, t.Godina, len(t.Dani), provjereno, t.Oznacenih, akoGreske(greske))
		}
		if nadjeno == 0 {
			upozorenja = append(upozorenja, filepath.Base(put)+": nijedna tražena postaja nije nađena")
		}
	}

	if *popis {
		return
	}
	for _, r := range upozorenja {
		fmt.Println("  upozorenje: " + r)
	}

	zapisano := 0
	for letva, dani := range nizovi {
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
		put := filepath.Join(*u, *sliv, letva, ime)
		fmt.Printf("\n%-10s %6d dana, %d.-%d.\n              → %s\n", letva, len(dani), od, do, ime)
		if *probno {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
			log.Fatal(err)
		}
		f, err := os.Create(put)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Fprintln(f, "datum;vodostaj_cm")
		for _, d := range popis {
			fmt.Fprintf(f, "%04d-%02d-%02d;%d\n", d.Godina, d.Mjesec, d.Dan, dani[d])
		}
		f.Close()
		zapisano++
	}
	if *probno {
		fmt.Println("\nproba — ništa nije zapisano")
		return
	}
	fmt.Printf("\nzapisano %d nizova\n", zapisano)
}

func akoGreske(g []string) string {
	if len(g) == 0 {
		return ""
	}
	return fmt.Sprintf("  — %d mjeseci se ne slaže sa sažetkom", len(g))
}

// Dan2 je dan s godinom, jer niz ide kroz više godišnjaka.
type Dan2 struct{ Godina, Mjesec, Dan int }

func (a Dan2) Prije(b Dan2) bool {
	if a.Godina != b.Godina {
		return a.Godina < b.Godina
	}
	if a.Mjesec != b.Mjesec {
		return a.Mjesec < b.Mjesec
	}
	return a.Dan < b.Dan
}

func godisnjaci(put string) ([]string, error) {
	st, err := os.Stat(put)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return []string{put}, nil
	}
	stavke, err := os.ReadDir(put)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range stavke {
		if !s.IsDir() && strings.EqualFold(filepath.Ext(s.Name()), ".pdf") {
			out = append(out, filepath.Join(put, s.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// tekstPDF vadi tekst iz godišnjaka, s poravnanjem stupaca kakvo tablica i
// ima; bez -layout se stupci sliju i tablica se ne da pročitati.
func tekstPDF(put string) (string, error) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return "", fmt.Errorf("treba pdftotext (brew install poppler)")
	}
	cmd := exec.Command("pdftotext", "-layout", put, "-")
	cmd.WaitDelay = 5 * time.Second
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pdftotext: %w", err)
	}
	return string(b), nil
}
