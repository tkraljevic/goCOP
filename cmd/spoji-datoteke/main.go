// spoji-datoteke lijepi godišnje datoteke jednog niza u jednu.
//
// Arhiva je narasla na 626 datoteka za 128 nizova, jer telemetrija stiže po
// godinama. Gradnja ih ionako grupira u niz, pa razlomljenost ne nosi ništa —
// samo otežava pogled u mapu i traženje one prave.
//
// Ništa se ne briše dok se spojena datoteka ne pročita natrag i ne usporedi s
// onim što je u nju ušlo. Ovo su tuđa mjerenja koja se ne mogu ponovno
// izmjeriti, pa provjera ide prije pospremanja.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type zapis struct {
	vrijeme string
	redak   string
}

type niz struct {
	kljuc    string
	mapa     string
	datoteke []string
}

func main() {
	koren := flag.String("iz", "vodostaji", "mapa s datotekama, složena po slivu i letvi")
	stvarno := flag.Bool("stvarno", false, "doista zapiši i pospremi; bez toga se samo javlja što bi se dogodilo")
	flag.Parse()

	nizovi, err := popisi(*koren)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sort.Slice(nizovi, func(a, b int) bool { return nizovi[a].kljuc < nizovi[b].kljuc })

	var spojenih, maknutih, preskocenih int
	for _, n := range nizovi {
		if len(n.datoteke) < 2 {
			continue
		}
		if err := spoji(n, *stvarno, &spojenih, &maknutih); err != nil {
			fmt.Printf("  %-46s preskačem: %v\n", n.kljuc, err)
			preskocenih++
		}
	}
	fmt.Printf("\nspojeno nizova %d, maknuto datoteka %d, preskočeno %d\n", spojenih, maknutih, preskocenih)
	if !*stvarno {
		fmt.Println("proba — ništa nije zapisano ni maknuto; ponovite s -stvarno")
	}
}

// popisi grupira datoteke po nizu. Naziv je ugovor:
// letva_izvor_velicina_vrsta_razdoblje.csv.
func popisi(koren string) ([]niz, error) {
	puts, err := filepath.Glob(filepath.Join(koren, "*", "*", "*.csv"))
	if err != nil {
		return nil, err
	}
	po := map[string]*niz{}
	for _, p := range puts {
		ime := strings.TrimSuffix(filepath.Base(p), ".csv")
		dj := strings.Split(ime, "_")
		if len(dj) != 5 {
			continue
		}
		k := strings.Join(dj[:4], "_")
		if po[k] == nil {
			po[k] = &niz{kljuc: k, mapa: filepath.Dir(p)}
		}
		po[k].datoteke = append(po[k].datoteke, p)
	}
	var out []niz
	for _, n := range po {
		sort.Strings(n.datoteke)
		out = append(out, *n)
	}
	return out, nil
}

func spoji(n niz, stvarno bool, spojenih, maknutih *int) error {
	glava := ""
	vidjeno := map[string]string{}
	var zapisi []zapis
	for _, p := range n.datoteke {
		g, redci, err := citaj(p)
		if err != nil {
			return err
		}
		if glava == "" {
			glava = g
		} else if g != glava {
			return fmt.Errorf("zaglavlja se razlikuju (%q vs %q)", glava, g)
		}
		for _, z := range redci {
			if prije, ima := vidjeno[z.vrijeme]; ima {
				if prije != z.redak {
					// Dvije datoteke tvrde različito za isti trenutak. To nije
					// pospremanje nego odluka o podatku, i ne donosi se ovdje.
					return fmt.Errorf("%s: dvije vrijednosti za %s (%q i %q)",
						filepath.Base(p), z.vrijeme, prije, z.redak)
				}
				continue
			}
			vidjeno[z.vrijeme] = z.redak
			zapisi = append(zapisi, z)
		}
	}
	sort.Slice(zapisi, func(a, b int) bool { return zapisi[a].vrijeme < zapisi[b].vrijeme })
	if len(zapisi) == 0 {
		return fmt.Errorf("nema nijednog retka")
	}

	od, do := godina(zapisi[0].vrijeme), godina(zapisi[len(zapisi)-1].vrijeme)
	razdoblje := od
	if do != od {
		razdoblje = od + "-" + do
	}
	cilj := filepath.Join(n.mapa, n.kljuc+"_"+razdoblje+".csv")

	fmt.Printf("  %-46s %2d → 1  %7d redaka  %s\n", n.kljuc, len(n.datoteke), len(zapisi), filepath.Base(cilj))
	if !stvarno {
		return nil
	}

	// Piše se sa strane pa se preimenuje: prekid usred pisanja ne smije
	// ostaviti pola niza pod imenom koje izgleda cjelovito.
	privremeno := cilj + ".nova"
	if err := zapisi_u(privremeno, glava, zapisi); err != nil {
		return err
	}
	// Provjera prije pospremanja: pročitaj natrag i usporedi s onim što je ušlo.
	_, natrag, err := citaj(privremeno)
	if err != nil {
		os.Remove(privremeno)
		return err
	}
	if len(natrag) != len(zapisi) {
		os.Remove(privremeno)
		return fmt.Errorf("provjera: zapisano %d, pročitano %d", len(zapisi), len(natrag))
	}
	for i := range natrag {
		if natrag[i] != zapisi[i] {
			os.Remove(privremeno)
			return fmt.Errorf("provjera: redak %d se razlikuje", i+1)
		}
	}
	if err := os.Rename(privremeno, cilj); err != nil {
		return err
	}
	*spojenih++
	for _, p := range n.datoteke {
		if p == cilj {
			continue
		}
		if err := os.Remove(p); err != nil {
			return err
		}
		*maknutih++
	}
	return nil
}

func citaj(put string) (glava string, out []zapis, err error) {
	f, err := os.Open(put)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1<<20), 1<<20)
	prvi := true
	for s.Scan() {
		redak := strings.TrimRight(s.Text(), "\r")
		if prvi {
			glava = strings.TrimPrefix(redak, "\ufeff")
			prvi = false
			continue
		}
		if strings.TrimSpace(redak) == "" {
			continue
		}
		i := strings.IndexByte(redak, ';')
		if i < 0 {
			return "", nil, fmt.Errorf("%s: redak bez razdjelnika: %q", filepath.Base(put), redak)
		}
		out = append(out, zapis{vrijeme: redak[:i], redak: redak})
	}
	return glava, out, s.Err()
}

func zapisi_u(put, glava string, zapisi []zapis) error {
	f, err := os.Create(put)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	if _, err := fmt.Fprintln(w, glava); err != nil {
		f.Close()
		return err
	}
	for _, z := range zapisi {
		if _, err := fmt.Fprintln(w, z.redak); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func godina(vrijeme string) string {
	if len(vrijeme) >= 4 {
		return vrijeme[:4]
	}
	return vrijeme
}
