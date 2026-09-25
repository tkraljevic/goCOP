// provjeri-prognozu pušta prognozu unatrag, po arhivi, i mjeri koliko je
// promašila. Bez toga se ne zna vrijedi li išta.
//
//	provjeri-prognozu -od 2024-01-01 -do 2025-12-31
//
// Uz svaki promašaj stoji i promašaj postojanosti — prognoze da se ništa neće
// promijeniti. Prognoza koja ne pobjeđuje postojanost nije prognoza nego
// trošak.
package main

import (
	"database/sql"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/prognoza"

	_ "modernc.org/sqlite"
)

// Dosezi su vremena za koja se mjeri promašaj.
var Dosezi = []int{6, 12, 24, 48, 72}

func main() {
	arhivaPut := flag.String("arhiva", "data/vodostaji.db", "arhiva vodostaja")
	bazaPut := flag.String("baza", "data/prognoze.db", "baza prognoza")
	odS := flag.String("od", "2023-01-01", "od kojeg datuma")
	doS := flag.String("do", "2025-12-31", "do kojeg datuma")
	korak := flag.Int("korak", 12, "koliko sati između dva izdanja")
	najdalje := flag.Int("najdalje", 96, "dokle se mjeri, u satima")
	zapisi := flag.Bool("zapisi", false, "zapiši izmjerene promašaje u bazu prognoza")
	glacenje := flag.Int("glacenje", 6, "koliko sati na svaku stranu pri glačanju ispravka; 0 isključuje")
	ispravi := flag.Bool("ispravi", false, "primijeni zapisane pomake i raspone, kao živa prognoza — da se vidi vrijede li izvan razdoblja na kojem su mjereni")
	udjeliS := flag.String("udjeli", "", "uz tablicu ispiši polovinu raspona za zadane udjele, npr. 0.68,0.70,0.80,0.90")
	csvPut := flag.String("csv", "", "zapiši svaki par prognoza–mjerenje (na dosezima iz tablice) u CSV, za ponavljanje računa izvan aplikacije")
	poluvijek := flag.Float64("poluvijek", prognoza.PoluvijekIspravka,
		"za koliko sati ispravak prema mjerenju oslabi na pola; 0 isključuje")
	flag.Parse()
	prognoza.PoluvijekIspravka = *poluvijek

	od, err := time.Parse("2006-01-02", *odS)
	if err != nil {
		log.Fatal(err)
	}
	do, err := time.Parse("2006-01-02", *doS)
	if err != nil {
		log.Fatal(err)
	}

	baza, err := prognoza.Otvori(*bazaPut)
	if err != nil {
		log.Fatal(err)
	}
	defer baza.Close()
	pojasi, err := prognoza.SviPojasi(baza)
	if err != nil {
		log.Fatal(err)
	}
	// Zapisani promašaji smiju se primijeniti samo na razdoblju na kojem nisu
	// mjereni; inače se provjerava sam sebe i svaka brojka izlazi bolja.
	zapisani := map[string]map[int]prognoza.Promasaj{}
	if *ispravi {
		if zapisani, err = prognoza.Promasaji(baza); err != nil {
			log.Fatal(err)
		}
	}
	arhiva, err := sql.Open("sqlite", *arhivaPut+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer arhiva.Close()

	nizovi := map[prognoza.Izvor]prognoza.Niz{}
	for iz := range trebani(pojasi) {
		v, err := prognoza.NizIzArhive(arhiva, iz.Letva, iz.Velicina)
		if err != nil {
			log.Fatal(err)
		}
		nizovi[iz] = prognoza.NoviNiz(v)
	}

	var csvW *csv.Writer
	if *csvPut != "" {
		f, err := os.Create(*csvPut)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		f.WriteString("\ufeff")
		csvW = csv.NewWriter(f)
		csvW.Comma = ';'
		csvW.Write([]string{"izdano_utc", "letva", "velicina", "ciljni_utc", "doseg_h", "prognoza", "dolje", "gore", "izmjereno", "postojanost"})
		defer csvW.Flush()
	}
	uDosezima := map[int]bool{}
	for _, d := range Dosezi {
		uDosezima[d] = true
	}
	promasaji := map[string]map[int]*zbroj{}
	izdanja := 0
	for t := od.Unix() / 3600; t <= do.Unix()/3600; t += int64(*korak) {
		// Izdanje se ne smije osloniti na ono što tek dolazi: račun vidi samo
		// ono do svojeg sata. NovoRacunalo to poštuje jer izmjereno uzima
		// isključivo do sada.
		r := prognoza.NovoRacunalo(pojasi, nizovi, t)
		imalo := false
		for letva, ps := range pojasi {
			izdane, err := r.Prognoziraj(letva, *najdalje, "provjera")
			if err != nil || len(izdane) == 0 {
				continue
			}
			imalo = true
			iz := prognoza.Izvor{Letva: letva, Velicina: ps[0].Velicina}
			sada, imaSad := nizovi[iz].U(t)
			for _, i := range izdane {
				if p, ima := zapisani[letva][int(i.Ciljni-t)]; ima {
					// Isto što radi živa prognoza: pomak se oduzme, a raspon
					// je zapisani — pa „u rasponu” mjeri baš njega.
					i.Vrijednost -= p.Pomak
					i.Dolje, i.Gore = i.Vrijednost-p.Rasap, i.Vrijednost+p.Rasap
				}
				stvarno, ima := nizovi[iz].U(i.Ciljni)
				if !ima {
					continue
				}
				d := int(i.Ciljni - t)
				if promasaji[letva] == nil {
					promasaji[letva] = map[int]*zbroj{}
				}
				if promasaji[letva][d] == nil {
					promasaji[letva][d] = &zbroj{}
				}
				z := promasaji[letva][d]
				z.dodaj(i.Vrijednost-stvarno, i.Raspon())
				if imaSad {
					z.dodajPostojanost(sada - stvarno)
				}
				if csvW != nil && uDosezima[d] {
					post := ""
					if imaSad {
						post = strconv.FormatFloat(math.Round(sada*10)/10, 'f', -1, 64)
					}
					csvW.Write([]string{
						time.Unix(t*3600, 0).UTC().Format("2006-01-02 15:04"), letva, iz.Velicina,
						time.Unix(i.Ciljni*3600, 0).UTC().Format("2006-01-02 15:04"), strconv.Itoa(d),
						strconv.FormatFloat(math.Round(i.Vrijednost*10)/10, 'f', -1, 64),
						strconv.FormatFloat(math.Round(i.Dolje*10)/10, 'f', -1, 64),
						strconv.FormatFloat(math.Round(i.Gore*10)/10, 'f', -1, 64),
						strconv.FormatFloat(math.Round(stvarno*10)/10, 'f', -1, 64), post,
					})
				}
			}
		}
		if imalo {
			izdanja++
		}
	}

	fmt.Printf("provjera od %s do %s, izdanja svakih %d h — %d izdanja\n\n",
		*odS, *doS, *korak, izdanja)
	fmt.Printf("%-16s %5s %8s %9s %9s %9s %7s\n",
		"letva", "doseg", "slučaja", "pomak", "promašaj", "postojanost", "u rasponu")
	for _, letva := range poredane(promasaji) {
		vel := pojasi[letva][0].Velicina
		for _, d := range Dosezi {
			z := promasaji[letva][d]
			if z == nil || z.n == 0 {
				continue
			}
			bolje := ""
			if z.np > 0 {
				if z.rms() < z.rmsP() {
					bolje = fmt.Sprintf("  %.0f%% bolje", 100*(1-z.rms()/z.rmsP()))
				} else {
					bolje = "  LOŠIJE"
				}
			}
			fmt.Printf("%-16s %4d h %8d %8.1f %8.1f %10.1f %6.0f %%%s\n",
				letva, d, z.n, z.pomak(), z.rms(), z.rmsP(), 100*z.uRasponu(), bolje)
		}
		fmt.Printf("%-16s %s\n", "", jedinica(vel))
	}

	if *udjeliS != "" {
		var udjeli []float64
		for _, u := range strings.Split(*udjeliS, ",") {
			v, err := strconv.ParseFloat(strings.TrimSpace(u), 64)
			if err != nil {
				log.Fatalf("udio %q: %v", u, err)
			}
			udjeli = append(udjeli, v)
		}
		fmt.Printf("\npolovina raspona po udjelu (brojanjem), i koliko je šira od one za %.0f %%\n", 100*prognoza.UdioURasponu)
		fmt.Printf("%-16s %5s", "letva", "doseg")
		for _, u := range udjeli {
			fmt.Printf(" %13s", fmt.Sprintf("%.0f %%", 100*u))
		}
		fmt.Println()
		for _, letva := range poredane(promasaji) {
			for _, d := range Dosezi {
				z := promasaji[letva][d]
				if z == nil || z.n < 100 {
					continue
				}
				osnova := z.odstupanjeUdio(prognoza.UdioURasponu)
				fmt.Printf("%-16s %4d h", letva, d)
				for _, u := range udjeli {
					r := z.odstupanjeUdio(u)
					fmt.Printf(" %7.1f ×%4.2f", r, r/osnova)
				}
				fmt.Println()
			}
		}
	}

	if !*zapisi {
		fmt.Printf("\nništa nije zapisano; -zapisi upisuje promašaje u bazu prognoza\n")
		return
	}
	var upis []prognoza.Promasaj
	for letva, po := range promasaji {
		for d, z := range po {
			if z.n < 100 {
				continue // premalo slučaja da bi brojka išta značila
			}
			upis = append(upis, prognoza.Promasaj{
				Letva: letva, Velicina: pojasi[letva][0].Velicina, DosegH: d,
				Pomak: z.pomak(), Rasap: z.odstupanje(),
				Postojanost: z.rmsP(), Slucaja: z.n,
			})
		}
	}
	upis = zagladi(upis, *glacenje)
	kad := fmt.Sprintf("%s..%s, svakih %d h, poluvijek %.0f h",
		*odS, *doS, *korak, *poluvijek)
	if err := prognoza.SpremiPromasaje(baza, upis, kad); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nzapisano %d izmjerenih promašaja\n", len(upis))
}

// zagladi izravnava ispravak po dosegu. Izmjereni pomak zna skočiti između
// dva susjedna sata — na Belišću s 25,7 na 48,8 cm — jer lanac na tom dosegu
// prestaje imati izmjeren ulaz za jednu kariku i prelazi na prognoziran, pa
// pogreška naraste stubom. Stuba vrijedi za prosjek mnogih izdanja, ali jedan
// niz ne smije po njoj skakati: rijeka ne zna da je nama ponestalo mjerenja.
func zagladi(p []prognoza.Promasaj, sirina int) []prognoza.Promasaj {
	if sirina <= 0 {
		return p
	}
	po := map[string]map[int]prognoza.Promasaj{}
	for _, x := range p {
		if po[x.Letva] == nil {
			po[x.Letva] = map[int]prognoza.Promasaj{}
		}
		po[x.Letva][x.DosegH] = x
	}
	out := make([]prognoza.Promasaj, 0, len(p))
	for _, x := range p {
		var zbirP, zbirR float64
		n := 0
		for d := x.DosegH - sirina; d <= x.DosegH+sirina; d++ {
			s, ima := po[x.Letva][d]
			if !ima {
				continue
			}
			zbirP += s.Pomak
			zbirR += s.Rasap
			n++
		}
		x.Pomak = zbirP / float64(n)
		x.Rasap = zbirR / float64(n)
		out = append(out, x)
	}
	return out
}

type zbroj struct {
	n, np, uRasp int
	zbir, kvad   float64
	kvadP        float64
	promasaji    []float64 // svi, da se raspon dobije brojanjem a ne pretpostavkom
}

func (z *zbroj) dodaj(promasaj, raspon float64) {
	z.n++
	z.zbir += promasaj
	z.kvad += promasaj * promasaj
	z.promasaji = append(z.promasaji, promasaj)
	if raspon > 0 && math.Abs(promasaj) <= raspon {
		z.uRasp++
	}
}

func (z *zbroj) dodajPostojanost(p float64) { z.np++; z.kvadP += p * p }

func (z *zbroj) pomak() float64 { return z.zbir / float64(z.n) }

// odstupanje je raspon unutar kojeg promašaj ostane u UdioURasponu slučajeva,
// mjeren brojanjem. Standardno odstupanje bi tu vrijedilo samo kad bi promašaji
// bili normalno raspoređeni, a nisu — velike vode ostavljaju dug rep. Ovako
// tvrdnja "ostaje unutar raspona u 68 % slučajeva" vrijedi po izgradnji, kao
// izmjerena činjenica, a ne kao pretpostavka.
func (z *zbroj) odstupanje() float64 { return z.odstupanjeUdio(prognoza.UdioURasponu) }

// odstupanjeUdio je polovina raspona u kojoj ostane zadani udio promašaja.
func (z *zbroj) odstupanjeUdio(udio float64) float64 {
	if len(z.promasaji) == 0 {
		return 0
	}
	m := z.pomak()
	odmaci := make([]float64, len(z.promasaji))
	for i, p := range z.promasaji {
		odmaci[i] = math.Abs(p - m)
	}
	sort.Float64s(odmaci)
	i := int(float64(len(odmaci)) * udio)
	if i >= len(odmaci) {
		i = len(odmaci) - 1
	}
	return odmaci[i]
}
func (z *zbroj) rms() float64 { return math.Sqrt(z.kvad / float64(z.n)) }
func (z *zbroj) rmsP() float64 {
	if z.np == 0 {
		return 0
	}
	return math.Sqrt(z.kvadP / float64(z.np))
}
func (z *zbroj) uRasponu() float64 { return float64(z.uRasp) / float64(z.n) }

func trebani(pojasi map[string][]prognoza.Pojas) map[prognoza.Izvor]bool {
	svi := map[prognoza.Izvor]bool{}
	for letva, ps := range pojasi {
		for _, p := range ps {
			svi[prognoza.Izvor{Letva: letva, Velicina: p.Velicina}] = true
			for _, u := range p.Ulazi {
				svi[prognoza.Izvor{Letva: u.Letva, Velicina: u.Velicina}] = true
			}
		}
	}
	return svi
}

func poredane(m map[string]map[int]*zbroj) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func jedinica(velicina string) string {
	if velicina == "protok" {
		return "m³/s"
	}
	return "cm"
}
