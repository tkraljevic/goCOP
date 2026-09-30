package prognoza

// Pričuvni izračun: kad naša prognoza ne radi (nema očitanja, pao je
// poslužitelj, zakazao je izvor), dežurni i dalje treba brojke za naše
// letve. Mađari i Srbi prognoziraju letve koje stoje nasuprot ili uz naše —
// Bezdan preko puta Batine, Bačka Palanka preko puta Iloka, Drávaszabolcs
// uz Donji Miholjac — a mađarska prognoza daje i Botovo, Terezino Polje,
// Donji Miholjac, Belišće i Osijek izravno. Pričuvni izračun prenosi
// njihove brojke na naše letve pravcima iz dnevnih vrijednosti (isti račun
// kao prenesena prognoza srpskih letvi, samo u drugom smjeru) i piše ih u
// Excel koji radi bez programa i bez mreže.

import (
	"context"
	"database/sql"
	"math"
	"sort"
	"time"
)

// Madjarska je oznaka izvora „mađarska prognoza iste letve”: pravac je
// jedinični, a brojka se uzima kako je objavljena.
const Madjarska = "hu:"

// PricuvniPutovi su za svaku našu letvu putovi redom prednosti: prvi čiji su
// svi ulazi upisani daje vrijednost. Ulaz s oznakom Madjarska je mađarska
// prognoza te letve; ulaz koji je i sam naša letva s popisa uzima njezinu
// već izračunatu vrijednost.
var PricuvniPutovi = []struct {
	Letva  string
	Putovi [][]string
}{
	{"batina", [][]string{{"bezdan"}, {"mohacs"}}},
	{"aljmas", [][]string{{"apatin", "bogojevo"}, {"bogojevo"}, {"batina"}}},
	{"dalj", [][]string{{"bogojevo", "backa-palanka"}, {"bogojevo"}, {"aljmas"}}},
	{"vukovar", [][]string{{"bogojevo", "backa-palanka"}, {"backa-palanka"}, {"dalj"}}},
	{"ilok", [][]string{{"backa-palanka"}, {"vukovar"}}},
	{"botovo", [][]string{{Madjarska + "botovo"}, {"ortilos"}}},
	{"terezino-polje", [][]string{{Madjarska + "terezino-polje"}, {"barcs"}}},
	{"donji-miholjac", [][]string{{"dravaszabolcs"}, {Madjarska + "donji-miholjac"}}},
	{"belisce", [][]string{{Madjarska + "belisce"}, {"donji-miholjac"}}},
	{"osijek", [][]string{{Madjarska + "osijek"}, {"belisce", "aljmas"}}},
}

// PricuvniUlazi su tuđe letve koje se upisuju, redom kako stoje u tablici:
// srpske pa mađarske, a iza njih mađarska prognoza naših letvi.
var PricuvniUlazi = []struct {
	Letva, Izvor string
}{
	{"bezdan", PodrijetloHidmet},
	{"apatin", PodrijetloHidmet},
	{"bogojevo", PodrijetloHidmet},
	{"backa-palanka", PodrijetloHidmet},
	{"mohacs", Podrijetlo},
	{"dravaszabolcs", Podrijetlo},
	{"ortilos", Podrijetlo},
	{"barcs", Podrijetlo},
	{Madjarska + "botovo", Podrijetlo},
	{Madjarska + "terezino-polje", Podrijetlo},
	{Madjarska + "donji-miholjac", Podrijetlo},
	{Madjarska + "belisce", Podrijetlo},
	{Madjarska + "osijek", Podrijetlo},
}

// PricuvniDana je koliko dana unaprijed tablica ima (uz današnji).
const PricuvniDana = 6

// PricuvnaVeza je jedan put: pravac, koliko drži i današnji pomak.
type PricuvnaVeza struct {
	Letva  string
	Izvori []string
	Koef   []float64 // odsječak, pa nagib po svakom izvoru
	Rasap  float64   // standardno odstupanje ostatka, cm
	Pomak  float64   // današnje odstupanje mjerenja od pravca, cm
	Dana   int
	Od     string
	Greska string // zašto puta nema
}

// PricuvniPodaci je sve što Excel treba: veze, današnja mjerenja naših letvi
// i unaprijed upisane tuđe prognoze (dan 0 = danas u 7 h).
type PricuvniPodaci struct {
	Sada   time.Time
	Dani   []time.Time                           // danas i sljedećih PricuvniDana dana, u 7 h po našem vremenu
	Veze   map[string][]PricuvnaVeza             // po našoj letvi, redom putova
	Ulazi  map[string][PricuvniDana + 1]*float64 // upisane vrijednosti tuđih letvi po danu
	Nase   map[string]float64                    // današnje mjerenje naših letvi
	Izvori map[string]string                     // odakle je upisana vrijednost ulaza (izdanje)
}

// PricuvniIzracun uči veze i skuplja što se zna. Veze se uče iz dnevnih
// vrijednosti (arhiva zadnjih PrijenosGodina godina i evidencija zadnjih
// dana); pomak je razlika današnjeg mjerenja i pravca, ograničena kao kod
// prenesene prognoze. Baza prognoza (može biti nil) daje tuđe prognoze za
// predupis.
func PricuvniIzracun(ctx context.Context, arhiva, ocitanja, prognoze *sql.DB, sada time.Time, zona *time.Location) PricuvniPodaci {
	out := PricuvniPodaci{Sada: sada, Veze: map[string][]PricuvnaVeza{},
		Ulazi: map[string][PricuvniDana + 1]*float64{}, Nase: map[string]float64{}, Izvori: map[string]string{}}
	lok := sada.In(zona)
	danas := time.Date(lok.Year(), lok.Month(), lok.Day(), 7, 0, 0, 0, zona)
	for d := 0; d <= PricuvniDana; d++ {
		out.Dani = append(out.Dani, danas.AddDate(0, 0, d))
	}
	dan := sada.UTC().Format("2006-01-02")
	danasnje := func(letva string) (float64, bool) {
		v, ima := dnevneVrijednosti(ctx, arhiva, ocitanja, letva, sada)[dan]
		return v, ima
	}
	for _, p := range PricuvniPutovi {
		if v, ima := danasnje(p.Letva); ima {
			out.Nase[p.Letva] = math.Round(v)
		}
		for _, put := range p.Putovi {
			veza := PricuvnaVeza{Letva: p.Letva, Izvori: put}
			if len(put) == 1 && len(put[0]) > len(Madjarska) && put[0][:len(Madjarska)] == Madjarska {
				veza.Koef = []float64{0, 1}
				out.Veze[p.Letva] = append(out.Veze[p.Letva], veza)
				continue
			}
			pr, err := NamjestiPrijenos(ctx, arhiva, ocitanja, p.Letva, put, sada)
			if err != nil {
				veza.Greska = err.Error()
				out.Veze[p.Letva] = append(out.Veze[p.Letva], veza)
				continue
			}
			veza.Koef, veza.Rasap, veza.Dana, veza.Od = pr.Koef, pr.Rasap, pr.Dana, pr.Od
			// Današnji pomak: koliko mjerenje odstupa od pravca, ne više od
			// PomakPrijenosaNajvise rasapa — veće je vjerojatnije kvar.
			cilj, ok := danasnje(p.Letva)
			vr := make([]float64, len(put))
			for i, l := range put {
				v, ima := danasnje(l)
				ok = ok && ima
				vr[i] = v
			}
			if ok {
				g := PomakPrijenosaNajvise * math.Max(pr.Rasap, 1)
				veza.Pomak = math.Round(math.Max(-g, math.Min(g, cilj-pr.U(vr))))
			}
			out.Veze[p.Letva] = append(out.Veze[p.Letva], veza)
		}
	}
	for _, u := range PricuvniUlazi {
		letva := u.Letva
		if len(letva) > len(Madjarska) && letva[:len(Madjarska)] == Madjarska {
			letva = letva[len(Madjarska):]
		}
		var red [PricuvniDana + 1]*float64
		if v, ima := danasnje(letva); ima {
			x := math.Round(v)
			red[0] = &x
		}
		if prognoze != nil {
			od, do := sada.Unix()/3600-72, sada.Unix()/3600+1
			if n, ima, err := TudaPrognoza(prognoze, u.Izvor, letva, od, do); err == nil && ima {
				for d := 1; d <= PricuvniDana; d++ {
					t := out.Dani[d].Unix() / 3600
					if v, ok := n.U(t); ok {
						x := math.Round(v)
						red[d] = &x
					}
				}
				out.Izvori[u.Letva] = u.Izvor
			}
		}
		out.Ulazi[u.Letva] = red
	}
	return out
}

// PricuvnoRacunaj računa vrijednosti naših letvi iz upisanih ulaza istim
// redom putova kakav stoji u Excelu; vraća vrijednost i indeks puta (−1 kad
// nijedan put nema sve ulaze). Služi i kao izračunata vrijednost ćelija.
//
// Kad letva prijeđe na sljedeći put (srpska prognoza ima četiri dana,
// mađarska šest), ne uzima se njegova vrijednost nego promjena od
// prethodnog dana, nastavljena na dotadašnju: dvije prognoze ne slažu se u
// razini, a razlika nije voda. Batina bi 30. 9. 2026. peti dan inače
// skočila s −143 (iz Bezdana) na −169 (iz Mohácsa).
func PricuvnoRacunaj(p PricuvniPodaci) (map[string][PricuvniDana + 1]*float64, map[string][PricuvniDana + 1]int) {
	vrijednosti := map[string][PricuvniDana + 1]*float64{}
	putovi := map[string][PricuvniDana + 1]int{}
	ulaz := func(l string, d int) *float64 {
		if r, ima := vrijednosti[l]; ima {
			return r[d]
		}
		if r, ima := p.Ulazi[l]; ima {
			return r[d]
		}
		return nil
	}
	izraz := func(v PricuvnaVeza, d int) (float64, bool) {
		y := v.Koef[0] + v.Pomak
		for j, l := range v.Izvori {
			x := ulaz(l, d)
			if x == nil {
				return 0, false
			}
			y += v.Koef[j+1] * *x
		}
		return y, true
	}
	for _, pp := range PricuvniPutovi {
		var red [PricuvniDana + 1]*float64
		var koji [PricuvniDana + 1]int
		for d := 0; d <= PricuvniDana; d++ {
			koji[d] = -1
			for i, v := range p.Veze[pp.Letva] {
				if v.Koef == nil {
					continue
				}
				y, ok := izraz(v, d)
				if !ok {
					continue
				}
				if i > 0 && d > 0 && red[d-1] != nil {
					if y0, ok0 := izraz(v, d-1); ok0 {
						y = *red[d-1] + (y - y0)
					}
				}
				y = math.Round(y)
				red[d], koji[d] = &y, i
				break
			}
		}
		vrijednosti[pp.Letva], putovi[pp.Letva] = red, koji
	}
	return vrijednosti, putovi
}

// imenaUlazaPricuvno vraća letve sa svih putova, bez naših — za provjeru.
func imenaUlazaPricuvno() []string {
	nase := map[string]bool{}
	for _, p := range PricuvniPutovi {
		nase[p.Letva] = true
	}
	skup := map[string]bool{}
	for _, p := range PricuvniPutovi {
		for _, put := range p.Putovi {
			for _, l := range put {
				if !nase[l] {
					skup[l] = true
				}
			}
		}
	}
	var out []string
	for l := range skup {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}
