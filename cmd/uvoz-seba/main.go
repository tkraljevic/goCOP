// uvoz-seba pretvara izvoz Geolux SmartObserver / SEBA u dva kanonska
// arhivska niza: vodostaj u centimetrima i temperaturu vode u °C.
package main

import (
	"flag"
	"fmt"
	"log"

	"gocop/internal/arhiva"
	"gocop/internal/importer/seba"
)

func main() {
	koren := flag.String("izlaz", "vodostaji", "korijenska mapa arhivskih nizova")
	sliv := flag.String("sliv", "", "mapa sliva, npr. dunav")
	postaja := flag.String("postaja", "", "šifra postaje, npr. tikves")
	izvor := flag.String("izvor", "geolux-seba", "oznaka izvora")
	probno := flag.Bool("probno", false, "provjeri i ispiši rezultat bez zapisivanja")
	flag.Parse()
	if *sliv == "" || *postaja == "" || flag.NArg() == 0 {
		log.Fatal("trebaju -sliv, -postaja i barem jedna ulazna CSV datoteka")
	}

	r, err := seba.Citaj(flag.Args())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("izvornih redaka: %d; duplikata: %d; sukoba vrijednosti: %d\n",
		r.Izvjestaj.Redaka, r.Izvjestaj.Duplikata, r.Izvjestaj.Sukoba)
	fmt.Printf("vodostaja: %d; praznih: %d; neispravnih: %d\n",
		len(r.Vodostaji), r.Izvjestaj.BezVodostaja, r.Izvjestaj.NeispravnihVodostaja)
	fmt.Printf("temperatura: %d; praznih: %d; neispravnih: %d\n",
		len(r.Temperature), r.Izvjestaj.BezTemperature, r.Izvjestaj.NeispravnihTemperatura)
	if *probno {
		return
	}

	v, err := arhiva.Upisi(*koren, *sliv, *postaja, *izvor, "vodostaj", "satni", r.Vodostaji)
	if err != nil {
		log.Fatal(err)
	}
	t, err := arhiva.Upisi(*koren, *sliv, *postaja, *izvor, "temperatura", "satni", r.Temperature)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(v)
	fmt.Println(t)
}
