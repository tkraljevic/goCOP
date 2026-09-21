// uvoz-pegelonline svodi 15-minutni JSON na stvarna očitanja punog sata i
// zapisuje kanonski satni niz vodostaja.
package main

import (
	"flag"
	"fmt"
	"log"

	"gocop/internal/arhiva"
	"gocop/internal/importer/pegelonline"
)

func main() {
	koren := flag.String("izlaz", "vodostaji", "korijenska mapa arhivskih nizova")
	sliv := flag.String("sliv", "dunav", "mapa sliva")
	postaja := flag.String("postaja", "", "šifra postaje")
	izvor := flag.String("izvor", "pegelonline", "oznaka izvora")
	probno := flag.Bool("probno", true, "provjeri i ispiši rezultat bez zapisivanja")
	flag.Parse()
	if *postaja == "" || flag.NArg() != 1 {
		log.Fatal("trebaju -postaja i jedan PEGELONLINE JSON")
	}
	r, err := pegelonline.Citaj(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("izvornih redaka: %d; punih sati: %d; ostalih četvrt-sati: %d; neispravnih: %d; duplikata: %d; sukoba: %d\n",
		r.Izvjestaj.Redaka, len(r.Vodostaji), r.Izvjestaj.IzvanPunogSata,
		r.Izvjestaj.Neispravnih, r.Izvjestaj.Duplikata, r.Izvjestaj.Sukoba)
	if *probno {
		fmt.Println("probni prolaz — ništa nije zapisano")
		return
	}
	put, err := arhiva.Upisi(*koren, *sliv, *postaja, *izvor, "vodostaj", "satni", r.Vodostaji)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(put)
}
