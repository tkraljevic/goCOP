// arhiva-vodostaja gradi i osvježava arhivsku bazu iz datoteka u vodostaji/.
// Sam posao radi paket internal/arhiva, jer ga zove i program kad netko upiše
// nova očitanja.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"gocop/internal/arhiva"
)

func main() {
	izvorDir := flag.String("iz", "vodostaji", "mapa s datotekama, složena po slivu i letvi")
	baza := flag.String("baza", "data/vodostaji.db", "arhivska baza")
	samo := flag.String("letva", "", "gradi samo zadanu letvu")
	flag.Parse()

	iz, err := arhiva.Izgradi(*izvorDir, *baza, *samo, os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nnizova %d, očitanja %d, spojenih vrijednosti %d\n", iz.Nizova, iz.Ocitanja, iz.Spojenih)
	if st, err := os.Stat(*baza); err == nil {
		fmt.Printf("%s: %.1f MB\n", *baza, float64(st.Size())/1e6)
	}
}
