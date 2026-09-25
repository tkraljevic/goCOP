// usporedi-dnevnu pušta naš dnevni model u trenucima kad su izdane mađarske
// prognoze (hydroinfo.hu) i uspoređuje oboje s izmjerenim, po danu unaprijed.
//
//	usporedi-dnevnu -do 2024-09-01 -prognoze-kise prognoze_kise.csv
//
// Model se uči samo do zadanog dana (prije prve tuđe prognoze), s kišom iz
// arhive; u trenutku izdanja dobiva ono što bi imao i uživo: srednjake 24 h
// koji završavaju u satu izdanja, palu kišu iz arhive i prognozu kiše kakva je
// tada bila izdana (CSV iz Previous Runs). Njihova vrijednost za 07 h
// uspoređuje se s našim mjerenjem u 07 h, naš dnevni srednjak s izmjerenim
// srednjakom istih 24 sata — svaki prema svojoj definiciji dana.
package main

import (
	"database/sql"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"

	_ "modernc.org/sqlite"
)

var sifre = map[string]string{
	"Botovo": "botovo", "Terezino Polje": "terezino-polje", "Donji Miholjac": "donji-miholjac",
	"Belišæe": "belisce", "Belišće": "belisce", "Osijek": "osijek", "Aljmaš": "aljmas",
}

type tuda struct {
	letva          string
	izdano, ciljni int64 // sat od epohe, UTC
	cm             float64
	danas          bool // njihovo jutarnje mjerenje, po njemu se vidi razlika nule
}

type zbroj struct {
	n                   int
	oni, mi, post       float64
	oniPomak, miPomak   float64
	miBoljih, oniBoljih int
}

func main() {
	tudePut := flag.String("tude", "data/tude/hydroinfo_iz_tablica.csv", "tuđe prognoze, CSV")
	arhivaPut := flag.String("arhiva", "data/vodostaji.db", "arhiva vodostaja i oborina")
	registarPut := flag.String("registar", "data/gocop.db", "registar s kišomjerima")
	prognozePut := flag.String("prognoze-kise", "", "CSV arhiviranih prognoza kiše (sliv;datum;dan;mm); prazno = bez prognozirane kiše")
	doS := flag.String("do", "2024-09-01", "model se uči samo na danima prije ovoga")
	odS := flag.String("od", "1990-01-01", "i od ovoga")
	kasnjenje := flag.Int("kasnjenje", 1, "koliko sati prije njihova izdanja završavaju naša mjerenja")
	bezKise := flag.Bool("bez-kise", false, "model bez oborine, za usporedbu")
	flag.Parse()

	tude, err := citajTude(*tudePut)
	if err != nil {
		log.Fatal(err)
	}
	arhiva, err := sql.Open("sqlite", *arhivaPut+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer arhiva.Close()

	// ciljevi: samo letve koje i oni prognoziraju
	imaTude := map[string]bool{}
	for _, t := range tude {
		imaTude[t.letva] = true
	}
	var ciljevi []prognoza.DnevniCilj
	for _, c := range prognoza.DnevniCiljevi {
		if imaTude[c.Letva] {
			if *bezKise {
				c.Slivovi = nil
			}
			ciljevi = append(ciljevi, c)
		}
	}

	// dnevni nizovi za učenje, satni za izdanje
	dnevni := map[string]prognoza.DnevniNiz{}
	satni := map[string]prognoza.Niz{}
	for _, c := range ciljevi {
		for _, l := range append([]string{c.Letva}, c.Ulazi...) {
			if dnevni[l] == nil {
				if dnevni[l], err = prognoza.DnevniIzArhive(arhiva, l); err != nil {
					log.Fatal(err)
				}
				v, err := prognoza.NizIzArhive(arhiva, l, "vodostaj")
				if err != nil {
					log.Fatal(err)
				}
				satni[l] = prognoza.NoviNiz(v)
			}
		}
	}
	var izvor *prognoza.OborinskiIzvor
	if !*bezKise {
		registar, err := sql.Open("sqlite", *registarPut+"?mode=ro")
		if err != nil {
			log.Fatal(err)
		}
		tocke, err := prognoza.OborinskeTocke(registar)
		registar.Close()
		if err != nil {
			log.Fatal(err)
		}
		ob, err := prognoza.DnevneOborine(arhiva, tocke)
		if err != nil {
			log.Fatal(err)
		}
		for k, n := range ob {
			dnevni[k] = n
		}
		izvor = &prognoza.OborinskiIzvor{Tocke: tocke, Satne: satneIzArhive(arhiva)}
	}
	prognozeKise := map[kljucPrognoze]float64{}
	if *prognozePut != "" && !*bezKise {
		if prognozeKise, err = citajPrognozeKise(*prognozePut); err != nil {
			log.Fatal(err)
		}
	}

	od, do := dan(*odS), dan(*doS)
	// kao uživo: glavni ulazi pa rezerve (Borl I nema satnog niza, pa Botovo
	// uživo ide preko Varaždina), s kišom pa bez nje
	modeli := map[string][]*prognoza.DnevniModel{}
	for _, c := range ciljevi {
		for _, in := range c.Inacice() {
			for _, l := range in.Ulazi {
				if dnevni[l] == nil {
					if dnevni[l], err = prognoza.DnevniIzArhive(arhiva, l); err != nil {
						log.Fatal(err)
					}
					v, err := prognoza.NizIzArhive(arhiva, l, "vodostaj")
					if err != nil {
						log.Fatal(err)
					}
					satni[l] = prognoza.NoviNiz(v)
				}
			}
			m, err := prognoza.NamjestiDnevniOd(dnevni, in, od, do)
			if err != nil {
				fmt.Println(err)
				m = nil
			}
			modeli[c.Letva] = append(modeli[c.Letva], m)
		}
	}

	// Nula letve: njihovo jutarnje mjerenje prema našem u isti sat; razlika se
	// oduzima od njihove prognoze, da se ne mjeri razlika nula nego prognoze.
	nula := map[string]float64{}
	{
		razlike := map[string][]float64{}
		for _, t := range tude {
			if t.danas {
				if v, ima := satni[t.letva].U(t.ciljni); ima {
					razlike[t.letva] = append(razlike[t.letva], t.cm-v)
				}
			}
		}
		for l, r := range razlike {
			sort.Float64s(r)
			nula[l] = r[len(r)/2]
		}
	}

	poIzdanju := map[int64][]tuda{}
	for _, t := range tude {
		if !t.danas {
			poIzdanju[t.izdano] = append(poIzdanju[t.izdano], t)
		}
	}
	izdanja := make([]int64, 0, len(poIzdanju))
	for iz := range poIzdanju {
		izdanja = append(izdanja, iz)
	}
	sort.Slice(izdanja, func(i, j int) bool { return izdanja[i] < izdanja[j] })

	po := map[string]map[int]*zbroj{}
	bezNase := map[string]int{}
	for _, izd := range izdanja {
		sada := izd - int64(*kasnjenje)
		var oborine map[string]prognoza.DnevniNiz
		if izvor != nil {
			if oborine, err = prognoza.OborineOkoSada(izvor, sada, 7, 0); err != nil {
				log.Fatal(err)
			}
			// prognozirana kiša: ona koja je na dan izdanja bila izdana
			danIzd := (sada*3600 + 43200) / 86400
			for _, c := range ciljevi {
				for _, s := range c.Slivovi {
					n := oborine[prognoza.OborinaKljuc(s)]
					if n == nil {
						n = prognoza.DnevniNiz{}
						oborine[prognoza.OborinaKljuc(s)] = n
					}
					for d := 1; d <= 6; d++ {
						if v, ok := prognozeKise[kljucPrognoze{s, danIzd, d}]; ok {
							n[int64(d)] = v
						} else if len(prognozeKise) == 0 {
							// bez arhiviranih prognoza: kiša koja je doista pala (savršena prognoza)
							if v, ok := dnevni[prognoza.OborinaKljuc(s)][danIzd+int64(d)]; ok {
								n[int64(d)] = v
							}
						}
					}
				}
			}
		}
		nase := map[string][]prognoza.DnevnaIzdana{}
		for l, in := range modeli {
			for _, m := range in {
				if m == nil {
					continue
				}
				if d, err := prognoza.PrognozirajDnevno(m, satni, oborine, sada); err == nil {
					nase[l] = d
					break
				}
			}
			if nase[l] == nil {
				bezNase[l]++
			}
		}
		for _, t := range poIzdanju[izd] {
			k := int((t.ciljni - sada + 12) / 24)
			if k < 1 || k > prognoza.DnevniDosezi {
				continue
			}
			d := nase[t.letva]
			if d == nil {
				continue
			}
			niz := satni[t.letva]
			izmj07, ok1 := niz.U(t.ciljni)
			srednjak := prognoza.DnevniIzSatnog(niz, sada+int64(24*k), 1)
			izmjDan, ok2 := srednjak[0]
			sad := prognoza.DnevniIzSatnog(niz, sada, 1)
			izmjSad, ok3 := sad[0]
			if !ok1 || !ok2 || !ok3 {
				continue
			}
			var nasa float64
			for _, x := range d {
				if x.Dan == k {
					nasa = x.Vrijednost
				}
			}
			if po[t.letva] == nil {
				po[t.letva] = map[int]*zbroj{}
			}
			z := po[t.letva][k]
			if z == nil {
				z = &zbroj{}
				po[t.letva][k] = z
			}
			oni, mi, post := t.cm-nula[t.letva]-izmj07, nasa-izmjDan, izmjSad-izmjDan
			z.n++
			z.oni += math.Abs(oni)
			z.mi += math.Abs(mi)
			z.post += math.Abs(post)
			z.oniPomak += oni
			z.miPomak += mi
			switch {
			case math.Abs(mi) < math.Abs(oni):
				z.miBoljih++
			case math.Abs(oni) < math.Abs(mi):
				z.oniBoljih++
			}
		}
	}

	fmt.Printf("%d izdanja hydroinfo, od %s do %s; učeno %s – %s%s\n\n", len(izdanja),
		vrijeme(izdanja[0]), vrijeme(izdanja[len(izdanja)-1]), *odS, *doS,
		map[bool]string{true: ", bez kiše", false: ", s kišom" + map[bool]string{true: " i arhiviranim prognozama kiše", false: ""}[len(prognozeKise) > 0]}[*bezKise])
	for _, c := range ciljevi {
		if po[c.Letva] == nil {
			continue
		}
		fmt.Printf("%s  (njihova nula %+.0f cm prema našoj)%s\n  %-22s", c.Letva, nula[c.Letva],
			map[bool]string{true: fmt.Sprintf("  (naša prognoza nije izdana %d puta)", bezNase[c.Letva]), false: ""}[bezNase[c.Letva] > 0], "dan")
		for k := 1; k <= prognoza.DnevniDosezi; k++ {
			fmt.Printf("%8d", k)
		}
		for _, red := range []struct {
			naziv string
			f     func(z *zbroj) string
		}{
			{"broj izdanja", func(z *zbroj) string { return fmt.Sprintf("%8d", z.n) }},
			{"oni, sr. pogr. (cm)", func(z *zbroj) string { return fmt.Sprintf("%8.0f", z.oni/float64(z.n)) }},
			{"mi", func(z *zbroj) string { return fmt.Sprintf("%8.0f", z.mi/float64(z.n)) }},
			{"postojanost", func(z *zbroj) string { return fmt.Sprintf("%8.0f", z.post/float64(z.n)) }},
			{"oni, pristranost", func(z *zbroj) string { return fmt.Sprintf("%+8.0f", z.oniPomak/float64(z.n)) }},
			{"mi, pristranost", func(z *zbroj) string { return fmt.Sprintf("%+8.0f", z.miPomak/float64(z.n)) }},
			{"mi bolji / oni bolji", func(z *zbroj) string { return fmt.Sprintf("%5d/%-2d", z.miBoljih, z.oniBoljih) }},
		} {
			fmt.Printf("\n  %-22s", red.naziv)
			for k := 1; k <= prognoza.DnevniDosezi; k++ {
				if z := po[c.Letva][k]; z != nil && z.n > 0 {
					fmt.Print(red.f(z))
				} else {
					fmt.Printf("%8s", "—")
				}
			}
		}
		fmt.Println()
		fmt.Println()
	}
}

// satneIzArhive daje satnu oborinu kišomjera iz spojenog niza arhive, kako
// bi izdanje dobilo palu kišu kakvu bi uživo imalo iz oborine.db.
func satneIzArhive(arhiva *sql.DB) func(od, do int64) (map[string]map[int64]float64, error) {
	return func(od, do int64) (map[string]map[int64]float64, error) {
		r, err := arhiva.Query(`SELECT letva, vrijeme, vrijednost FROM spoj
			WHERE velicina = 'oborina' AND korak = 'satni' AND vrijeme BETWEEN ? AND ?`, od*3600, do*3600)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		out := map[string]map[int64]float64{}
		for r.Next() {
			var l string
			var t int64
			var v float64
			if err := r.Scan(&l, &t, &v); err != nil {
				return nil, err
			}
			if out[l] == nil {
				out[l] = map[int64]float64{}
			}
			out[l][t/3600] = v
		}
		return out, r.Err()
	}
}

func dan(s string) int64 {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		log.Fatal(err)
	}
	return (t.Unix() + 43200) / 86400
}

func vrijeme(sat int64) string {
	return time.Unix(sat*3600, 0).In(models.Zagreb).Format("02.01.2006.")
}

func citajTude(put string) ([]tuda, error) {
	f, err := os.Open(put)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.Comma = ';'
	if _, err := r.Read(); err != nil {
		return nil, err
	}
	var out []tuda
	for {
		red, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		letva, ima := sifre[strings.TrimSpace(red[1])]
		if !ima {
			continue
		}
		izd, err1 := time.ParseInLocation("2006-01-02 15:04", red[2], models.Zagreb)
		cilj, err2 := time.ParseInLocation("2006-01-02 15:04", red[3], models.Zagreb)
		cm, err3 := strconv.ParseFloat(red[4], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		out = append(out, tuda{letva: letva, izdano: izd.Unix() / 3600, ciljni: cilj.Unix() / 3600, cm: cm, danas: red[5] == "danas"})
	}
	return out, nil
}

type kljucPrognoze struct {
	sliv string
	t    int64
	d    int
}

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
			return nil, err
		}
		d, err := strconv.Atoi(p[2])
		if err != nil {
			return nil, err
		}
		v, err := strconv.ParseFloat(strings.ReplaceAll(p[3], ",", "."), 64)
		if err != nil {
			return nil, err
		}
		out[kljucPrognoze{p[0], (t.Unix() + 43200) / 86400, d}] = v
	}
	return out, nil
}
