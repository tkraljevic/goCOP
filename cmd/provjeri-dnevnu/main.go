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
	"strconv"
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
	udioRegr := flag.Float64("udio-regresije", prognoza.DnevniUdioRegresije, "udio regresije u srednjaku procjena (0 = samo analogije, 1 = samo regresija)")
	analogija := flag.Int("analogija", prognoza.DnevnihAnalogija, "koliko se analogija uzima")
	tezinske := flag.Bool("analogije-tezinske", false, "analogije s težinom obrnuto razmjernom udaljenosti")
	rezimKise := flag.Float64("rezim-kise", 0, "kvantil zbroja kiše iznad kojega vrijedi treći režim regresije, npr. 0.9; 0 isključuje")
	kvadrati := flag.Bool("kvadratna-kisa", false, "regresiji dodaj kvadrate značajki kiše")
	ciljeviS := flag.String("ciljevi", "", `isprobaj druge ciljeve, npr. "botovo=letenye,borl-i;belisce=botovo+A,B,C" (+ međuslivovi čija oborina ulazi)`)
	registarPut := flag.String("registar", "data/gocop.db", "registar s kišomjerima (za oborinu)")
	uciOd := flag.String("uci-od", "", "učenje vidi samo dane od ovoga (prazno = od početka niza)")
	korijen := flag.Bool("korijen", false, "oborina u značajke kao korijen zbroja")
	tezinaKNN := flag.Float64("oborina-knn", prognoza.OborinaTezinaKNN, "težina oborine u udaljenosti analogija (0 = samo regresija)")
	unaprijed := flag.Bool("prognoza-kise", true, "i prognozirana kiša (u provjeri: stvarna buduća kiša iz arhive, gornja granica)")
	prognozePut := flag.String("prognoze-kise", "", "CSV arhiviranih prognoza kiše (sliv;datum;dan;mm): provjera s onim što se tada doista prognoziralo")
	flag.Parse()
	prognoza.DnevniUdioRegresije = *udioRegr
	prognoza.DnevnihAnalogija = *analogija
	prognoza.DnevneAnalogijeTezinske = *tezinske
	prognoza.DnevniRezimKise = *rezimKise
	prognoza.DnevneKvadratneKise = *kvadrati
	prognoza.OborinaKorijen, prognoza.OborinaTezinaKNN, prognoza.OborinaUnaprijed = *korijen, *tezinaKNN, *unaprijed
	ciljevi := prognoza.DnevniCiljevi
	if *ciljeviS != "" {
		ciljevi = nil
		for _, c := range strings.Split(*ciljeviS, ";") {
			l, u, ok := strings.Cut(c, "=")
			if !ok {
				log.Fatalf("-ciljevi: %q nije oblika letva=ulaz,ulaz", c)
			}
			u, slivovi, _ := strings.Cut(u, "+")
			cilj := prognoza.DnevniCilj{Letva: strings.TrimSpace(l)}
			if u = strings.TrimSpace(u); u != "" { // vrh bez uzvodne letve: samo vlastita razina i kiša
				cilj.Ulazi = strings.Split(u, ",")
			}
			if slivovi != "" {
				cilj.Slivovi = strings.Split(slivovi, ",")
			}
			ciljevi = append(ciljevi, cilj)
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
	trebaOborina := false
	for _, c := range ciljevi {
		trebaOborina = trebaOborina || len(c.Slivovi) > 0
	}
	if trebaOborina {
		registar, err := sql.Open("sqlite", *registarPut+"?mode=ro")
		if err != nil {
			log.Fatal(err)
		}
		tocke, err := prognoza.OborinskeTocke(registar)
		registar.Close()
		if err != nil {
			log.Fatal(err)
		}
		oborine, err := prognoza.DnevneOborine(arhiva, tocke)
		if err != nil {
			log.Fatal(err)
		}
		for k, n := range oborine {
			nizovi[k] = n
		}
		fmt.Printf("oborina: %d kišomjera, međuslivovi", len(tocke))
		for k, n := range oborine {
			fmt.Printf(" %s (%d dana)", strings.TrimPrefix(k, "oborina:"), len(n))
		}
		fmt.Println()
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

	if *prognozePut != "" {
		p, err := citajPrognozeKise(*prognozePut)
		if err != nil {
			log.Fatal(err)
		}
		prognoza.PrognozaKise = func(sliv string, t int64, d int) (float64, bool) {
			v, ok := p[kljucPrognoze{sliv, t, d}]
			return v, ok
		}
		fmt.Printf("arhivirane prognoze kiše: %d vrijednosti\n", len(p))
	}
	od, do := dan(*uciDo), dan(*kraj)
	var odDana int64
	if *uciOd != "" {
		odDana = dan(*uciOd)
	}
	fmt.Printf("učeno %s– do %s, provjereno %s – %s\n", *uciOd, *uciDo, *uciDo, *kraj)
	for _, c := range ciljevi {
		// učenje uvijek na stvarnoj budućoj kiši; prognoze samo u provjeri
		hook := prognoza.PrognozaKise
		prognoza.PrognozaKise = nil
		m, err := prognoza.NamjestiDnevniOd(nizovi, c, odDana, od)
		prognoza.PrognozaKise = hook
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
		fmt.Printf("\n%s (ulazi: %s%s; %d dana učenja)\n  %-24s", c.Letva, strings.Join(c.Ulazi, ", "),
			map[bool]string{true: " + oborina " + strings.Join(c.Slivovi, ","), false: ""}[len(c.Slivovi) > 0], m.Uzoraka(), "dan")
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

type kljucPrognoze struct {
	sliv string
	t    int64 // dan izdavanja
	d    int   // koliko dana unaprijed
}

// citajPrognozeKise čita CSV sliv;datum;dan;mm — datum je dan izdavanja
// prognoze, dan koliko dana unaprijed vrijedi zbroj mm.
func citajPrognozeKise(put string) (map[kljucPrognoze]float64, error) {
	b, err := os.ReadFile(put)
	if err != nil {
		return nil, err
	}
	out := map[kljucPrognoze]float64{}
	for i, red := range strings.Split(strings.TrimPrefix(string(b), "\ufeff"), "\n") {
		p := strings.Split(strings.TrimSpace(red), ";")
		if i == 0 || len(p) < 4 {
			continue
		}
		t, err := time.Parse("2006-01-02", p[1])
		if err != nil {
			return nil, fmt.Errorf("%s redak %d: %w", put, i+1, err)
		}
		d, err := strconv.Atoi(p[2])
		if err != nil {
			return nil, fmt.Errorf("%s redak %d: %w", put, i+1, err)
		}
		v, err := strconv.ParseFloat(strings.ReplaceAll(p[3], ",", "."), 64)
		if err != nil {
			return nil, fmt.Errorf("%s redak %d: %w", put, i+1, err)
		}
		out[kljucPrognoze{p[0], (t.Unix() + 43200) / 86400, d}] = v
	}
	return out, nil
}
