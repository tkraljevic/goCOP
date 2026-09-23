// namjesti-prognozu mjeri kako se letve slažu sa svojim uzvodnim ulazima i
// sprema izmjereno u bazu prognoza. Sam račun prognoze ne radi — ovo je korak
// koji mu daje brojke.
//
//	namjesti-prognozu -probno          samo ispiši što bi se namjestilo
//	namjesti-prognozu                  namjesti i spremi
//	namjesti-prognozu -proba "aljmas = batina + belisce + siga"
//	                                   isprobaj jednu postavku, ništa ne spremaj
//
// Veze se mjere po pojasima vodnosti jer se kašnjenje mijenja s razinom: na
// Batini → Aljmaš ide od 11 sati pri maloj vodi do 39 pri velikoj, jer se
// Kopački rit puni i val uspori.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"gocop/internal/prognoza"

	_ "modernc.org/sqlite"
)

// Velicine kaže u čemu se koja letva vodi — i kao cilj, i kad ulazi drugamo.
// Ne bira se sama: na gornjoj Dravi korito se ispod lanca hidroelektrana
// produbljuje, pa vodostaj kroz desetljeća mijenja značenje i Novo Virje iz
// vodostaja drži r 0,49–0,61 umjesto 0,87–0,97 iz protoka. Na Dunavu je
// obrnuto, ondje je vodostaj bolji. Vrbovka, Moslavina, Donji Miholjac,
// Belišće, Ilok i Osijek protok u arhivi nemaju, pa im izbora ni nema.
var Velicine = map[string]string{
	"donja-dubrava":  "protok",
	"letenye":        "vodostaj",
	"botovo":         "protok",
	"novo-virje":     "protok",
	"terezino-polje": "protok",
	"vrbovka":        "vodostaj",
	"moslavina":      "vodostaj",
	"donji-miholjac": "vodostaj",
	"belisce":        "vodostaj",
	"batina":         "vodostaj",
	"aljmas":         "vodostaj",
	"dalj":           "vodostaj",
	"vukovar":        "vodostaj",
	"sotin":          "vodostaj",
	"mohovo":         "vodostaj",
	"ilok":           "vodostaj",
	"osijek":         "vodostaj",
	"siga":           "vodostaj",
	"petres":         "vodostaj",
}

// Racun je jedna letva i ono iz čega se računa. Prvi ulaz je glavni tok: po
// njemu se dijele pojasi vodnosti.
type Racun struct {
	Letva string
	Ulazi []string
}

// Batina, Donja Dubrava i Letenye nemaju svoj račun: ništa uzvodno od njih
// nemamo u arhivi. Oni su ulaz, i oni određuju dokle prognoza seže.
var Tokovi = []struct {
	Ime    string
	Racuni []Racun
}{
	{"Drava", []Racun{
		// Botovo je nizvodno od ušća Mure kod Legrada. Bez Mure mu veza s
		// Donjom Dubravom drži svega R² 0,24–0,45 po pojasu, a Dubravi ispadne
		// nagib 2,07 — protok koji se na dvadeset kilometara udvostruči. To
		// nije bio val nego Mura koja se u njemu skrivala.
		{"botovo", []string{"donja-dubrava", "letenye"}},
		{"novo-virje", []string{"botovo"}},
		{"terezino-polje", []string{"novo-virje"}},
		{"vrbovka", []string{"terezino-polje"}},
		{"moslavina", []string{"vrbovka"}},
		{"donji-miholjac", []string{"moslavina"}},
		{"belisce", []string{"donji-miholjac"}},
	}},
	{"Dunav", []Racun{
		// Drava se ulijeva u Dunav kod Aljmaša, pa Aljmaš nije samo dunavska
		// letva. Ovdje se dva kraka sastaju.
		{"aljmas", []string{"batina", "belisce"}},
		{"dalj", []string{"aljmas"}},
		{"vukovar", []string{"dalj"}},
		{"sotin", []string{"vukovar"}},
		{"mohovo", []string{"sotin"}},
		{"ilok", []string{"mohovo"}},
	}},
	{"ušće", []Racun{
		// Osijek nema krivulje protoka i nikad je neće imati: blizu ušća veza
		// vodostaja i protoka nije jednoznačna. Zato ide na vodostaj, i zato mu
		// treba i Dunav — razinu mu jednako drži uspor odozdo koliko dotok
		// odozgo.
		{"osijek", []string{"belisce", "aljmas"}},
	}},
}

func main() {
	arhivaPut := flag.String("arhiva", "data/vodostaji.db", "arhiva vodostaja")
	bazaPut := flag.String("baza", "data/prognoze.db", "baza prognoza")
	probno := flag.Bool("probno", false, "samo ispiši što bi se namjestilo")
	proba := flag.String("proba", "", `isprobaj jednu postavku, npr. "aljmas = batina + belisce"`)
	flag.Parse()

	arhiva, err := sql.Open("sqlite", *arhivaPut+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer arhiva.Close()

	if *proba != "" {
		r, err := razaberi(*proba)
		if err != nil {
			log.Fatal(err)
		}
		namjesti(arhiva, r)
		return
	}

	var sve []prognoza.Pojas
	for _, tok := range Tokovi {
		fmt.Printf("\n— %s —\n", tok.Ime)
		for _, r := range tok.Racuni {
			sve = append(sve, namjesti(arhiva, r)...)
		}
	}

	if *probno {
		fmt.Printf("\nproba — ništa nije spremljeno; %d pojasa bi ušlo\n", len(sve))
		return
	}
	baza, err := prognoza.Otvori(*bazaPut)
	if err != nil {
		log.Fatal(err)
	}
	defer baza.Close()
	if err := prognoza.Spremi(baza, sve, time.Now().Format(time.RFC3339)); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nspremljeno %d pojasa u %s\n", len(sve), *bazaPut)
}

// razaberi čita "letva = ulaz + ulaz".
func razaberi(zapis string) (Racun, error) {
	strane := strings.SplitN(zapis, "=", 2)
	if len(strane) != 2 {
		return Racun{}, fmt.Errorf(`proba mora izgledati kao "letva = ulaz + ulaz"`)
	}
	r := Racun{Letva: strings.TrimSpace(strane[0])}
	for _, u := range strings.Split(strane[1], "+") {
		if u = strings.TrimSpace(u); u != "" {
			r.Ulazi = append(r.Ulazi, u)
		}
	}
	if len(r.Ulazi) == 0 {
		return Racun{}, fmt.Errorf("%s nema nijedan ulaz", r.Letva)
	}
	return r, nil
}

// rastavi vadi letvu i veličinu iz zapisa "letva" ili "letva:velicina". Druga
// se navodi kad se postavka isprobava bez diranja koda:
// "vrbovka:vodostaj = terezino-polje:protok".
func rastavi(zapis string) (string, string, error) {
	letva, velicina, ima := strings.Cut(zapis, ":")
	if ima {
		return letva, velicina, nil
	}
	v, zna := Velicine[letva]
	if !zna {
		return "", "", fmt.Errorf("za %s se ne zna u čemu se vodi; navedi %s:vodostaj ili %s:protok",
			letva, letva, letva)
	}
	return letva, v, nil
}

func namjesti(arhiva *sql.DB, r Racun) []prognoza.Pojas {
	letva, vel, err := rastavi(r.Letva)
	if err != nil {
		fmt.Println(err)
		return nil
	}
	izvori := make([]prognoza.Izvor, 0, len(r.Ulazi))
	imena := make([]string, 0, len(r.Ulazi))
	for _, u := range r.Ulazi {
		l, v, err := rastavi(u)
		if err != nil {
			fmt.Println(err)
			return nil
		}
		izvori = append(izvori, prognoza.Izvor{Letva: l, Velicina: v})
		imena = append(imena, l+" ("+jedinica(v)+")")
	}
	zaglavlje := letva + " ← " + strings.Join(imena, " + ")

	pojasi, err := prognoza.NamjestiLetvu(arhiva, letva, vel, izvori)
	if err != nil {
		fmt.Printf("%-56s %v\n", zaglavlje, err)
		return nil
	}
	fmt.Printf("%-56s %s\n", zaglavlje, vel)
	for _, p := range pojasi {
		// Granice pojasa mjere se u glavnom ulazu, pa nose njegovu jedinicu,
		// a rasap nosi jedinicu cilja.
		fmt.Printf("   %8.0f – %8.0f %-4s  r %.3f   rasap %7.1f %-4s  (%d sati)\n",
			p.Od, p.Do, jedinica(p.Ulazi[0].Velicina), p.R, p.Rasap, jedinica(p.Velicina), p.Sati)
		for _, u := range p.Ulazi {
			fmt.Printf("        %-16s -%2d h   nagib %6.2f %s\n",
				u.Letva, u.PomakH, u.Nagib, poJedinici(p.Velicina, u.Velicina))
		}
	}
	return pojasi
}

func jedinica(velicina string) string {
	if velicina == "protok" {
		return "m³/s"
	}
	return "cm"
}

func poJedinici(cilj, ulaz string) string {
	if cilj == ulaz {
		return ""
	}
	return jedinica(cilj) + "/" + jedinica(ulaz)
}
