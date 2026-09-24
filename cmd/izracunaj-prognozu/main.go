// izracunaj-prognozu uzima zadnja očitanja, prenosi ih lancem nizvodno i
// zapisuje prognozu po satu.
//
//	izracunaj-prognozu -probno    samo ispiši
//	izracunaj-prognozu            izračunaj i zapiši
//
// Sam račun stoji u paketu prognoza, jer ga zove i poslužitelj čim preuzme
// nove vodostaje. Ova naredba služi za ručno pokretanje i za pogled u niz.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"time"

	"gocop/internal/prognoza"

	_ "modernc.org/sqlite"
)

func main() {
	ocitanjaPut := flag.String("ocitanja", "data/gocop.db", "baza očitanja")
	arhivaPut := flag.String("arhiva", "data/vodostaji.db", "arhiva, zbog krivulja")
	bazaPut := flag.String("baza", "data/prognoze.db", "baza prognoza")
	najdalje := flag.Int("najdalje", 96, "dokle se računa, u satima")
	probno := flag.Bool("probno", false, "samo ispiši, ne zapisuj")
	poluvijek := flag.Float64("poluvijek", prognoza.PoluvijekIspravka,
		"za koliko sati ispravak prema mjerenju oslabi na pola; 0 isključuje")
	ispisi := flag.String("ispisi", "", "ispiši niz po satu za jednu letvu")
	iznova := flag.Bool("iznova", false, "izračunaj i kad je za taj sat prognoza već izdana")
	flag.Parse()
	prognoza.PoluvijekIspravka = *poluvijek

	baza, err := prognoza.Otvori(*bazaPut)
	if err != nil {
		log.Fatal(err)
	}
	defer baza.Close()
	ocitanja, err := sql.Open("sqlite", *ocitanjaPut+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer ocitanja.Close()
	arhiva, err := sql.Open("sqlite", *arhivaPut+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer arhiva.Close()

	o := &prognoza.Osvjezivac{Baza: baza, Ocitanja: ocitanja, Arhiva: arhiva,
		Najdalje: *najdalje, Model: prognoza.ModelLanac, Iznova: *iznova}
	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	if ishod.Preskoceno && !*iznova {
		fmt.Printf("za %s UTC prognoza je već izdana; -iznova računa unatoč tome\n",
			time.Unix(ishod.Sada*3600, 0).UTC().Format("2006-01-02 15:04"))
		return
	}

	fmt.Printf("izdano za %s UTC\n", time.Unix(ishod.Sada*3600, 0).UTC().Format("2006-01-02 15:04"))
	for iz, z := range ishod.Vrhovi {
		fmt.Printf("   %-16s %-9s zadnje %s\n", iz.Letva, iz.Velicina,
			time.Unix(z*3600, 0).UTC().Format("02.01. 15:04"))
	}
	if len(ishod.TudiVrhovi) > 0 {
		fmt.Printf("   budućnost vrha iz tuđe prognoze: %v\n", ishod.TudiVrhovi)
	}
	for letva, i := range ishod.Izbor {
		fmt.Printf("   %-16s %s (inačica %d)\n", letva, i.Opis, i.Inacica)
	}
	ispisi_ := *ispisi
	fmt.Printf("\n%-16s %-9s %8s %10s %12s\n", "letva", "veličina", "doseg", "za 6 h", "na kraju")
	for _, letva := range redom(ishod) {
		niz := zaLetvu(ishod, letva)
		if len(niz) == 0 {
			continue
		}
		if ispisi_ == letva {
			for _, i := range niz {
				fmt.Printf("   %s  %+3d h  %8.1f ± %-6.1f %s\n",
					time.Unix(i.Ciljni*3600, 0).UTC().Format("02.01. 15:04"),
					i.Ciljni-ishod.Sada, i.Vrijednost, i.Raspon(), jedinica(i.Velicina))
			}
		}
		zad, sest := niz[len(niz)-1], niz[min(6, len(niz)-1)]
		fmt.Printf("%-16s %-9s %6d h %7.0f±%-3.0f %7.0f±%-3.0f %s%s\n",
			letva, niz[0].Velicina, len(niz),
			sest.Vrijednost, sest.Raspon(), zad.Vrijednost, zad.Raspon(),
			jedinica(niz[0].Velicina), slabija(ishod.Promasaji[letva], len(niz)))
	}
	for letva, err := range ishod.BezPrognoze {
		fmt.Printf("%-16s %v\n", letva, err)
	}
	if len(ishod.Dnevne) > 0 {
		fmt.Printf("\ndnevno (cm)      zadnja 24 h")
		for k := 1; k <= prognoza.DnevniDosezi; k++ {
			fmt.Printf("  %7d. dan", k)
		}
		for _, c := range prognoza.DnevniCiljevi {
			for _, d := range ishod.Dnevne {
				if d.Letva != c.Letva {
					continue
				}
				if d.Dan == 0 {
					fmt.Printf("\n%-16s %11.0f", c.Letva, d.Vrijednost)
					continue
				}
				fmt.Printf("  %5.0f ±%-4.0f", d.Vrijednost, d.Raspon())
			}
		}
		fmt.Println()
	}
	for letva, err := range ishod.BezDnevne {
		fmt.Printf("dnevno %-16s %v\n", letva, err)
	}

	if *probno {
		fmt.Printf("\nproba — ništa nije zapisano; %d vrijednosti bi ušlo\n", len(ishod.Izdane))
		return
	}
	if err := o.Zapisi(ishod); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nzapisano %d satnih i %d dnevnih vrijednosti\n", len(ishod.Izdane), len(ishod.Dnevne))
}

// zaLetvu vadi niz jedne letve u veličini u kojoj se računa, poredan po satu.
func zaLetvu(ishod *prognoza.Ishod, letva string) []prognoza.Izdana {
	var out []prognoza.Izdana
	var vel string
	for _, i := range ishod.Izdane {
		if i.Letva != letva || i.Racunata {
			continue
		}
		if vel == "" {
			vel = i.Velicina
		}
		if i.Velicina == vel {
			out = append(out, i)
		}
	}
	return out
}

// redom slaže letve onako kako se pojavljuju u ishodu, dakle kako voda teče.
func redom(ishod *prognoza.Ishod) []string {
	vidjeno := map[string]bool{}
	var out []string
	for _, i := range ishod.Izdane {
		if !vidjeno[i.Letva] {
			vidjeno[i.Letva] = true
			out = append(out, i.Letva)
		}
	}
	return out
}

// slabija javlja na kojim dosezima prognoza ne pobjeđuje postojanost. Ondje
// se ne isplati izdavati je: bolje je reći da se ništa neće promijeniti.
func slabija(po map[int]prognoza.Promasaj, doseg int) string {
	var od, do int
	for d := 1; d <= doseg; d++ {
		p, ima := po[d]
		if !ima || p.BoljaOdPostojanosti() {
			continue
		}
		if od == 0 {
			od = d
		}
		do = d
	}
	if od == 0 {
		return ""
	}
	if od == do {
		return fmt.Sprintf("   slabija od postojanosti na %d h", od)
	}
	return fmt.Sprintf("   slabija od postojanosti od %d do %d h", od, do)
}

func jedinica(velicina string) string {
	if velicina == "protok" {
		return "m³/s"
	}
	return "cm"
}
