// paket-arhive izdaje arhivu kao mapu .cop paketa s katalogom.
//
// Sam posao živi u internal/arhiva, jer ga radi i stranica (Administracija →
// Unos u arhivu). Ovdje je samo naredbeni redak oko njega: isti posao iz
// terminala, za noćno izdavanje i za čvor bez sučelja.
//
// Mapa koja iz ovoga izađe je ono što ide na USB, disk ili GitHub.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gocop/internal/arhiva"
	_ "modernc.org/sqlite"
)

func main() {
	baza := flag.String("baza", "data/vodostaji.db", "arhivska baza")
	uMapu := flag.String("u", "pakete", "mapa u koju se izdaje")
	izdao := flag.String("izdao", "cop-osijek-node", "čvor koji izdaje")
	samo := flag.String("letva", "", "izdaj samo zadanu letvu")
	suho := flag.Bool("probno", false, "samo javi što bi se izdalo")
	flag.Parse()

	db, err := sql.Open("sqlite", *baza+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	iz, err := arhiva.Izdaj(db, *uMapu, *izdao, *samo, *suho, os.Stdout)
	if err != nil {
		log.Fatal(err)
	}

	n := len(iz.Katalog.Paketi)
	fmt.Printf("\n%d %s, %d promijenjeno, %d nepromijenjeno\n",
		n, uzBrojPaket(n), iz.Promijenjenih, iz.Istih)
	fmt.Printf("ukupno %d zapisa u %.1f MB\n", iz.Zapisa, float64(iz.Bajtova)/1e6)

	if *suho {
		fmt.Println("\nproba — ništa nije zapisano; ponovite bez -probno")
		return
	}
	fmt.Printf("katalog: %s\n", filepath.Join(*uMapu, arhiva.ImeKataloga))
}

func uzBrojPaket(n int) string {
	if n%10 == 1 && n%100 != 11 {
		return "paket"
	}
	return "paketa"
}
