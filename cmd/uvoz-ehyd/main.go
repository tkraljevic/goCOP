// uvoz-ehyd pretvara austrijski eHYD izvoz dnevnih srednjaka u kanonski
// arhivski niz. Mjesečne srednjake zasad samo prepoznaje i odbija: ne smiju se
// predstavljati kao dnevni podaci.
package main

import (
	"flag"
	"fmt"
	"log"

	"gocop/internal/arhiva"
	"gocop/internal/importer/ehyd"
)

func main() {
	koren := flag.String("izlaz", "vodostaji", "korijenska mapa arhivskih nizova")
	sliv := flag.String("sliv", "", "mapa sliva, npr. dunav")
	postaja := flag.String("postaja", "", "šifra postaje")
	izvor := flag.String("izvor", "ehyd", "oznaka izvora")
	velicina := flag.String("velicina", "", "vodostaj, protok ili temperatura")
	probno := flag.Bool("probno", true, "provjeri i ispiši rezultat bez zapisivanja")
	flag.Parse()
	if *sliv == "" || *postaja == "" || *velicina == "" || flag.NArg() != 1 {
		log.Fatal("trebaju -sliv, -postaja, -velicina i jedna eHYD CSV datoteka")
	}

	r, err := ehyd.Citaj(flag.Arg(0), *velicina)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("postaja: %s (HZB %s); interval: %s; jedinica: %s\n",
		r.Izvjestaj.Postaja, r.Izvjestaj.HZB, r.Izvjestaj.Interval, r.Izvjestaj.Jedinica)
	fmt.Printf("redaka: %d; vrijednosti: %d; praznina: %d; neispravnih: %d\n",
		r.Izvjestaj.Redaka, len(r.Redci), r.Izvjestaj.Praznina, r.Izvjestaj.Neispravnih)
	if r.Izvjestaj.Interval != "T" {
		log.Fatal("niz nije dnevni; mjesečni niz ne smije se upisati kao dnevni srednjak")
	}
	if *probno {
		fmt.Println("probni prolaz — ništa nije zapisano")
		return
	}
	put, err := arhiva.Upisi(*koren, *sliv, *postaja, *izvor, *velicina, "srednjak", r.Redci)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(put)
}
