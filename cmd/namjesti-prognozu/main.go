// namjesti-prognozu mjeri kako se letve slažu sa svojim uzvodnim ulazima i
// sprema izmjereno u bazu prognoza. Sam račun prognoze ne radi — ovo je korak
// koji mu daje brojke.
//
//	namjesti-prognozu -probno          samo ispiši što bi se namjestilo
//	namjesti-prognozu                  namjesti i spremi
//
// Veze se mjere po pojasima vodnosti jer se kašnjenje mijenja s razinom: na
// Batini → Aljmaš ide od 4 sata pri maloj vodi do 38 pri velikoj, jer se
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

// Racun je jedna letva i ono iz čega se računa. Prvi ulaz je glavni tok: po
// njemu se dijele pojasi vodnosti.
type Racun struct {
	Letva string
	Ulazi []string
}

// Batina, Donja Dubrava i Letenye nemaju svoj račun: ništa uzvodno od njih
// nemamo u arhivi. Oni su ulaz, i oni određuju dokle prognoza seže.
var Racuni = []struct {
	Tok    string
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
		{"ilok", []string{"vukovar"}},
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
	flag.Parse()

	arhiva, err := sql.Open("sqlite", *arhivaPut+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer arhiva.Close()

	var sve []prognoza.Pojas
	for _, tok := range Racuni {
		fmt.Printf("\n— %s —\n", tok.Tok)
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

func namjesti(arhiva *sql.DB, r Racun) []prognoza.Pojas {
	zaglavlje := r.Letva + " ← " + strings.Join(r.Ulazi, " + ")
	pojasi, err := prognoza.NamjestiLetvu(arhiva, r.Letva, r.Ulazi)
	if err != nil {
		fmt.Printf("%-40s %v\n", zaglavlje, err)
		return nil
	}
	fmt.Printf("%-40s %s\n", zaglavlje, pojasi[0].Velicina)
	for _, p := range pojasi {
		fmt.Printf("   %8.0f – %8.0f %-4s  r %.3f   rasap %7.1f %-4s  (%d sati)\n",
			p.Od, p.Do, jedinica(p.Velicina), p.R, p.Rasap, jedinica(p.Velicina), p.Sati)
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
