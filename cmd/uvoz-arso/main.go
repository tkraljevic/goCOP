// uvoz-arso pretvara ARSO-ov izvoz dnevnih vrijednosti u kanonske arhivske
// nizove vodostaja, protoka i temperature vode.
package main

import (
	"flag"
	"fmt"
	"log"

	"gocop/internal/arhiva"
	"gocop/internal/importer/arso"
)

func main() {
	koren := flag.String("izlaz", "vodostaji", "korijenska mapa arhivskih nizova")
	sliv := flag.String("sliv", "", "mapa sliva, npr. drava")
	postaja := flag.String("postaja", "", "šifra postaje, npr. ptuj")
	izvor := flag.String("izvor", "arso", "oznaka izvora")
	probno := flag.Bool("probno", true, "provjeri i ispiši rezultat bez zapisivanja")
	flag.Parse()
	if *sliv == "" || *postaja == "" || flag.NArg() == 0 {
		log.Fatal("trebaju -sliv, -postaja i barem jedna ulazna ARSO CSV datoteka")
	}

	r, err := arso.Citaj(flag.Args())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("redaka: %d; nečitljivih datuma: %d; duplikata: %d; sukoba: %d\n",
		r.Izvjestaj.Redaka, r.Izvjestaj.NeispravnihDatuma, r.Izvjestaj.Duplikata, r.Izvjestaj.Sukoba)
	fmt.Printf("vodostaja: %d; praznih: %d; neispravnih: %d\n",
		len(r.Vodostaji), r.Izvjestaj.BezVodostaja, r.Izvjestaj.NeispravnihVodostaja)
	fmt.Printf("protoka: %d; praznih: %d; neispravnih: %d\n",
		len(r.Protoci), r.Izvjestaj.BezProtoka, r.Izvjestaj.NeispravnihProtoka)
	fmt.Printf("temperatura: %d; praznih: %d; neispravnih: %d\n",
		len(r.Temperature), r.Izvjestaj.BezTemperature, r.Izvjestaj.NeispravnihTemperatura)
	if *probno {
		fmt.Println("probni prolaz — ništa nije zapisano")
		return
	}

	upisi := func(velicina string, redci []arhiva.Redak) {
		if len(redci) == 0 {
			return
		}
		put, err := arhiva.Dopuni(*koren, *sliv, *postaja, *izvor, velicina, "srednjak", redci)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(put)
	}
	upisi("vodostaj", r.Vodostaji)
	upisi("protok", r.Protoci)
	upisi("temperatura", r.Temperature)
}
