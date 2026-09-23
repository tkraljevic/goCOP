// provjeri-dnevnu provjerava dnevnu prognozu na razdoblju koje model nije
// vidio: uči se do zadanog dana, a mjeri poslije njega — na svakom danu i na
// vrhovima valova.
//
//	provjeri-dnevnu -uci-do 2012-01-01 -valovi vrhovi.csv
//
// vrhovi.csv: val;letva;vrh_utc (YYYY-MM-DD HH);vrh_cm, kao za provjeri-valove.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"gocop/internal/prognoza"

	_ "modernc.org/sqlite"
)

func dan(s string) int64 {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		log.Fatal(err)
	}
	return (t.Unix() + 43200) / 86400
}

func main() {
	arhivaPut := flag.String("arhiva", "data/vodostaji.db", "arhiva vodostaja")
	uciDo := flag.String("uci-do", "2012-01-01", "učenje vidi samo dane prije ovoga")
	kraj := flag.String("kraj", "2025-01-01", "provjera do ovog dana")
	valoviPut := flag.String("valovi", "", "CSV s vrhovima valova (neobavezno)")
	ciljeviS := flag.String("ciljevi", "", `isprobaj druge ciljeve, npr. "botovo=letenye,borl-i;belisce=botovo"`)
	flag.Parse()
	ciljevi := prognoza.DnevniCiljevi
	if *ciljeviS != "" {
		ciljevi = nil
		for _, c := range strings.Split(*ciljeviS, ";") {
			l, u, ok := strings.Cut(c, "=")
			if !ok {
				log.Fatalf("-ciljevi: %q nije oblika letva=ulaz,ulaz", c)
			}
			ciljevi = append(ciljevi, prognoza.DnevniCilj{Letva: strings.TrimSpace(l), Ulazi: strings.Split(u, ",")})
		}
	}

	arhiva, err := sql.Open("sqlite", *arhivaPut+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer arhiva.Close()
	nizovi := map[string]prognoza.DnevniNiz{}
	for _, c := range ciljevi {
		for _, l := range append([]string{c.Letva}, c.Ulazi...) {
			if nizovi[l] == nil {
				if nizovi[l], err = prognoza.DnevniIzArhive(arhiva, l); err != nil {
					log.Fatal(err)
				}
			}
		}
	}
	vrhovi := map[string][]int64{}
	if *valoviPut != "" {
		b, err := os.ReadFile(*valoviPut)
		if err != nil {
			log.Fatal(err)
		}
		for _, red := range strings.Split(string(b), "\n")[1:] {
			p := strings.Split(red, ";")
			if len(p) < 3 {
				continue
			}
			if t, err := time.Parse("2006-01-02 15", p[2]); err == nil {
				vrhovi[p[1]] = append(vrhovi[p[1]], (t.Unix()+3600)/86400)
			}
		}
	}

	od, do := dan(*uciDo), dan(*kraj)
	fmt.Printf("učeno do %s, provjereno %s – %s\n", *uciDo, *uciDo, *kraj)
	for _, c := range ciljevi {
		m, err := prognoza.NamjestiDnevni(nizovi, c, od)
		if err != nil {
			fmt.Println(err)
			continue
		}
		cilj := nizovi[c.Letva]
		var kvM, kvP [prognoza.DnevniDosezi + 1]float64
		var n, uRasponu [prognoza.DnevniDosezi + 1]int
		for t := od; t < do; t++ {
			x, ok := prognoza.DnevneZnacajke(c, nizovi, t)
			if !ok {
				continue
			}
			p, r := m.Prognoziraj(x)
			for k := 1; k <= prognoza.DnevniDosezi; k++ {
				v, ima := cilj[t+int64(k)]
				if !ima {
					continue
				}
				s := v - cilj[t]
				kvM[k] += (p[k] - s) * (p[k] - s)
				kvP[k] += s * s
				if math.Abs(p[k]-s) <= r[k] {
					uRasponu[k]++
				}
				n[k]++
			}
		}
		fmt.Printf("\n%s (ulazi: %s)\n  %-24s", c.Letva, strings.Join(c.Ulazi, ", "), "dan")
		for k := 1; k <= prognoza.DnevniDosezi; k++ {
			fmt.Printf("%8d", k)
		}
		fmt.Printf("\n  %-24s", "promašaj, svi dani (cm)")
		for k := 1; k <= prognoza.DnevniDosezi; k++ {
			fmt.Printf("%8.1f", math.Sqrt(kvM[k]/float64(n[k])))
		}
		fmt.Printf("\n  %-24s", "postojanost")
		for k := 1; k <= prognoza.DnevniDosezi; k++ {
			fmt.Printf("%8.1f", math.Sqrt(kvP[k]/float64(n[k])))
		}
		fmt.Printf("\n  %-24s", "u rasponu (%)")
		for k := 1; k <= prognoza.DnevniDosezi; k++ {
			fmt.Printf("%8.0f", 100*float64(uRasponu[k])/float64(n[k]))
		}
		if len(vrhovi[c.Letva]) > 0 {
			var ab, bi, abP [prognoza.DnevniDosezi + 1]float64
			var nv [prognoza.DnevniDosezi + 1]int
			for _, v := range vrhovi[c.Letva] {
				for k := 1; k <= prognoza.DnevniDosezi; k++ {
					t := v - int64(k)
					x, ok := prognoza.DnevneZnacajke(c, nizovi, t)
					stvarno, ima := cilj[v]
					if !ok || !ima || t < od {
						continue
					}
					p, _ := m.Prognoziraj(x)
					e := cilj[t] + p[k] - stvarno
					ab[k] += math.Abs(e)
					bi[k] += e
					abP[k] += math.Abs(cilj[t] - stvarno)
					nv[k]++
				}
			}
			fmt.Printf("\n  %-24s", fmt.Sprintf("vrh vala (%d), sr. pogr.", nv[1]))
			for k := 1; k <= prognoza.DnevniDosezi; k++ {
				fmt.Printf("%8.0f", ab[k]/float64(nv[k]))
			}
			fmt.Printf("\n  %-24s", "  pristranost")
			for k := 1; k <= prognoza.DnevniDosezi; k++ {
				fmt.Printf("%+8.0f", bi[k]/float64(nv[k]))
			}
			fmt.Printf("\n  %-24s", "  postojanost")
			for k := 1; k <= prognoza.DnevniDosezi; k++ {
				fmt.Printf("%8.0f", abP[k]/float64(nv[k]))
			}
		}
		fmt.Println()
	}
}
