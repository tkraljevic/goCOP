package prognoza

// Provjera unatrag: prognoza se pušta po arhivi i mjeri koliko je
// promašila. Uz svaki promašaj stoji i promašaj postojanosti — prognoze da se
// ništa neće promijeniti. Zapisani promašaji daju živoj prognozi ispravak i
// raspon, pa je ovo dio pripreme modela, ne samo dijagnostika. Stajalo je u
// naredbi provjeri-prognozu; ovdje je da isti postupak radi i aplikacija.

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"time"
)

// DoseziProvjere su vremena za koja se promašaj ispisuje u tablici.
var DoseziProvjere = []int{6, 12, 24, 48, 72}

// OpcijeProvjere su postavke jedne provjere unatrag.
type OpcijeProvjere struct {
	Od, Do   time.Time
	Korak    int       // sati između dva izdanja
	Najdalje int       // dokle se mjeri, u satima
	Zapisi   bool      // zapiši izmjerene promašaje u bazu prognoza
	Glacenje int       // sati na svaku stranu pri glačanju ispravka; 0 isključuje
	Ispravi  bool      // primijeni zapisane pomake i raspone, kao živa prognoza
	Udjeli   []float64 // uz tablicu ispiši polovinu raspona za zadane udjele
	CSV      io.Writer // svaki par prognoza–mjerenje na dosezima iz tablice
	Dnevnik  io.Writer
}

// ZadaneOpcijeProvjere su postavke s kojima se promašaji zapisuju za živu
// prognozu.
func ZadaneOpcijeProvjere() OpcijeProvjere {
	return OpcijeProvjere{Od: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC), Do: time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC),
		Korak: 12, Najdalje: 96, Glacenje: 6}
}

// ProvjeriUnatrag pušta prognozu po arhivi od Od do Do i ispiše promašaje; s
// Zapisi ih upiše u bazu prognoza i vrati koliko ih je zapisano.
func ProvjeriUnatrag(arhiva, baza *sql.DB, o OpcijeProvjere) (int, error) {
	w := o.Dnevnik
	if w == nil {
		w = io.Discard
	}
	if o.Korak <= 0 {
		return 0, fmt.Errorf("korak mora biti barem 1 sat")
	}
	inacice, err := SveInacice(baza)
	if err != nil {
		return 0, err
	}
	// pojasi su glavne inačice (za veličine i popis letvi); ulazi se čitaju
	// za sve inačice, a za svako izdanje bira se kao uživo.
	pojasi := map[string][]Pojas{}
	svePojase := map[string][]Pojas{}
	for letva, in := range inacice {
		if len(in) > 0 {
			pojasi[letva] = in[0]
		}
		for _, ps := range in {
			svePojase[letva] = append(svePojase[letva], ps...)
		}
	}
	// Zapisani promašaji smiju se primijeniti samo na razdoblju na kojem nisu
	// mjereni; inače se provjerava sam sebe i svaka brojka izlazi bolja.
	zapisani := map[string]map[int]Promasaj{}
	if o.Ispravi {
		if zapisani, err = Promasaji(baza); err != nil {
			return 0, err
		}
	}

	// Modeli ispuštanja elektrana: vrh lanca u provjeri dobiva istu budućnost
	// kao uživo, iz nizova koji u sam lanac ne ulaze.
	operateri, err := UcitajOperatere(baza)
	if err != nil {
		return 0, err
	}
	potrebni := trebaniProvjere(svePojase)
	for _, iz := range OperaterIzvori(operateri) {
		potrebni[iz] = true
	}
	nizovi := map[Izvor]Niz{}
	for iz := range potrebni {
		v, err := NizIzArhive(arhiva, iz.Letva, iz.Velicina)
		if err != nil {
			return 0, err
		}
		nizovi[iz] = NoviNiz(v)
	}

	var csvW *csv.Writer
	if o.CSV != nil {
		io.WriteString(o.CSV, "\ufeff")
		csvW = csv.NewWriter(o.CSV)
		csvW.Comma = ';'
		csvW.Write([]string{"izdano_utc", "letva", "velicina", "ciljni_utc", "doseg_h", "prognoza", "dolje", "gore", "izmjereno", "postojanost"})
		defer csvW.Flush()
	}
	uDosezima := map[int]bool{}
	for _, d := range DoseziProvjere {
		uDosezima[d] = true
	}
	promasaji := map[string]map[int]*zbrojProvjere{}
	izdanja := 0
	for t := o.Od.Unix() / 3600; t <= o.Do.Unix()/3600; t += int64(o.Korak) {
		// Izdanje se ne smije osloniti na ono što tek dolazi: račun vidi samo
		// ono do svojeg sata. NovoRacunalo to poštuje jer izmjereno uzima
		// isključivo do sada.
		odabrani, _ := OdaberiInacice(inacice, nizovi, t)
		r := NovoRacunalo(odabrani, nizovi, t)
		_, vrhovi := TrebaniIzvori(odabrani)
		for iz, n := range BuducnostOperatera(operateri, nizovi, t) {
			if vrhovi[iz] {
				r.PostaviBuducnostVrha(iz, n)
			}
		}
		imalo := false
		for letva, ps := range pojasi {
			izdane, err := r.Prognoziraj(letva, o.Najdalje, "provjera")
			if err != nil || len(izdane) == 0 {
				continue
			}
			imalo = true
			iz := Izvor{Letva: letva, Velicina: ps[0].Velicina}
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
					promasaji[letva] = map[int]*zbrojProvjere{}
				}
				if promasaji[letva][d] == nil {
					promasaji[letva][d] = &zbrojProvjere{}
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

	fmt.Fprintf(w, "provjera od %s do %s, izdanja svakih %d h — %d izdanja\n\n",
		o.Od.Format("2006-01-02"), o.Do.Format("2006-01-02"), o.Korak, izdanja)
	fmt.Fprintf(w, "%-16s %5s %8s %9s %9s %9s %7s\n",
		"letva", "doseg", "slučaja", "pomak", "promašaj", "postojanost", "u rasponu")
	for _, letva := range poredaneProvjere(promasaji) {
		vel := pojasi[letva][0].Velicina
		for _, d := range DoseziProvjere {
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
			fmt.Fprintf(w, "%-16s %4d h %8d %8.1f %8.1f %10.1f %6.0f %%%s\n",
				letva, d, z.n, z.pomak(), z.rms(), z.rmsP(), 100*z.uRasponu(), bolje)
		}
		fmt.Fprintf(w, "%-16s %s\n", "", jedinicaVelicine(vel))
	}

	if len(o.Udjeli) > 0 {
		udjeli := o.Udjeli
		fmt.Fprintf(w, "\npolovina raspona po udjelu (brojanjem), i koliko je šira od one za %.0f %%\n", 100*UdioURasponu)
		fmt.Fprintf(w, "%-16s %5s", "letva", "doseg")
		for _, u := range udjeli {
			fmt.Fprintf(w, " %13s", fmt.Sprintf("%.0f %%", 100*u))
		}
		fmt.Fprintln(w)
		for _, letva := range poredaneProvjere(promasaji) {
			for _, d := range DoseziProvjere {
				z := promasaji[letva][d]
				if z == nil || z.n < 100 {
					continue
				}
				osnova := z.odstupanjeUdio(UdioURasponu)
				fmt.Fprintf(w, "%-16s %4d h", letva, d)
				for _, u := range udjeli {
					r := z.odstupanjeUdio(u)
					fmt.Fprintf(w, " %7.1f ×%4.2f", r, r/osnova)
				}
				fmt.Fprintln(w)
			}
		}
	}

	if !o.Zapisi {
		fmt.Fprintf(w, "\nništa nije zapisano; -zapisi upisuje promašaje u bazu prognoza\n")
		return 0, nil
	}
	var upis []Promasaj
	for letva, po := range promasaji {
		for d, z := range po {
			if z.n < 100 {
				continue // premalo slučaja da bi brojka išta značila
			}
			upis = append(upis, Promasaj{
				Letva: letva, Velicina: pojasi[letva][0].Velicina, DosegH: d,
				Pomak: z.pomak(), Rasap: z.odstupanje(),
				Postojanost: z.rmsP(), Slucaja: z.n,
			})
		}
	}
	upis = zagladiPromasaje(upis, o.Glacenje)
	kad := fmt.Sprintf("%s..%s, svakih %d h, poluvijek %.0f h",
		o.Od.Format("2006-01-02"), o.Do.Format("2006-01-02"), o.Korak, PoluvijekIspravka)
	if err := SpremiPromasaje(baza, upis, kad); err != nil {
		return 0, err
	}
	fmt.Fprintf(w, "\nzapisano %d izmjerenih promašaja\n", len(upis))
	return len(upis), nil
}

// zagladiPromasaje izravnava ispravak po dosegu. Izmjereni pomak zna skočiti između
// dva susjedna sata — na Belišću s 25,7 na 48,8 cm — jer lanac na tom dosegu
// prestaje imati izmjeren ulaz za jednu kariku i prelazi na prognoziran, pa
// pogreška naraste stubom. Stuba vrijedi za prosjek mnogih izdanja, ali jedan
// niz ne smije po njoj skakati: rijeka ne zna da je nama ponestalo mjerenja.
func zagladiPromasaje(p []Promasaj, sirina int) []Promasaj {
	if sirina <= 0 {
		return p
	}
	po := map[string]map[int]Promasaj{}
	for _, x := range p {
		if po[x.Letva] == nil {
			po[x.Letva] = map[int]Promasaj{}
		}
		po[x.Letva][x.DosegH] = x
	}
	out := make([]Promasaj, 0, len(p))
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

type zbrojProvjere struct {
	n, np, uRasp int
	zbir, kvad   float64
	kvadP        float64
	promasaji    []float64 // svi, da se raspon dobije brojanjem a ne pretpostavkom
}

func (z *zbrojProvjere) dodaj(promasaj, raspon float64) {
	z.n++
	z.zbir += promasaj
	z.kvad += promasaj * promasaj
	z.promasaji = append(z.promasaji, promasaj)
	if raspon > 0 && math.Abs(promasaj) <= raspon {
		z.uRasp++
	}
}

func (z *zbrojProvjere) dodajPostojanost(p float64) { z.np++; z.kvadP += p * p }

func (z *zbrojProvjere) pomak() float64 { return z.zbir / float64(z.n) }

// odstupanje je raspon unutar kojeg promašaj ostane u UdioURasponu slučajeva,
// mjeren brojanjem. Standardno odstupanje bi tu vrijedilo samo kad bi promašaji
// bili normalno raspoređeni, a nisu — velike vode ostavljaju dug rep. Ovako
// tvrdnja "ostaje unutar raspona u 68 % slučajeva" vrijedi po izgradnji, kao
// izmjerena činjenica, a ne kao pretpostavka.
func (z *zbrojProvjere) odstupanje() float64 { return z.odstupanjeUdio(UdioURasponu) }

// odstupanjeUdio je polovina raspona u kojoj ostane zadani udio promašaja.
func (z *zbrojProvjere) odstupanjeUdio(udio float64) float64 {
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
func (z *zbrojProvjere) rms() float64 { return math.Sqrt(z.kvad / float64(z.n)) }
func (z *zbrojProvjere) rmsP() float64 {
	if z.np == 0 {
		return 0
	}
	return math.Sqrt(z.kvadP / float64(z.np))
}
func (z *zbrojProvjere) uRasponu() float64 { return float64(z.uRasp) / float64(z.n) }

func trebaniProvjere(pojasi map[string][]Pojas) map[Izvor]bool {
	svi := map[Izvor]bool{}
	for letva, ps := range pojasi {
		for _, p := range ps {
			svi[Izvor{Letva: letva, Velicina: p.Velicina}] = true
			for _, u := range p.Ulazi {
				svi[Izvor{Letva: u.Letva, Velicina: u.Velicina}] = true
			}
		}
	}
	return svi
}

func poredaneProvjere(m map[string]map[int]*zbrojProvjere) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
