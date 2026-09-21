// uvoz-gkd pretvara ZIP s provjerenim dnevnim protocima bavarskog GKD-a u
// kanonski arhivski niz.
package main

import (
	"flag"
	"fmt"
	"log"

	"gocop/internal/arhiva"
	"gocop/internal/importer/gkd"
)

func main() {
	koren := flag.String("izlaz", "vodostaji", "korijenska mapa arhivskih nizova")
	sliv := flag.String("sliv", "dunav", "mapa sliva")
	postaja := flag.String("postaja", "", "šifra postaje")
	izvor := flag.String("izvor", "gkd", "oznaka izvora")
	probno := flag.Bool("probno", true, "provjeri i ispiši rezultat bez zapisivanja")
	flag.Parse()
	if *postaja == "" || flag.NArg() != 1 {
		log.Fatal("trebaju -postaja i jedan GKD ZIP")
	}
	r, err := gkd.Citaj(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("postaja: %s (%s); ZIP datoteka: %d\n", r.Izvjestaj.Postaja, r.Izvjestaj.Broj, r.Izvjestaj.Datoteka)
	fmt.Printf("redaka: %d; provjerenih protoka: %d; praznih datoteka: %d; neprovjerenih: %d; neispravnih: %d; duplikata: %d; sukoba: %d\n",
		r.Izvjestaj.Redaka, len(r.Protoci), r.Izvjestaj.BezPodataka, r.Izvjestaj.Neprovjerenih,
		r.Izvjestaj.Neispravnih, r.Izvjestaj.Duplikata, r.Izvjestaj.Sukoba)
	if *probno {
		fmt.Println("probni prolaz — ništa nije zapisano")
		return
	}
	put, err := arhiva.Upisi(*koren, *sliv, *postaja, *izvor, "protok", "srednjak", r.Protoci)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(put)
}
