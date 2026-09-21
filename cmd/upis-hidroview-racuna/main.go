// upis-hidroview-racuna upisuje na ovaj čvor račun za telemetriju na Geolux
// HydroViewu (hdv.voda.hr), da goCOP može sam preuzimati vodostaje letava
// koje su ondje.
//
// Lozinka se ne upisuje u naredbeni redak nego se čita iz okoline, i sprema
// se šifrirana ključem izvedenim iz ključa čvora — vrijedi samo na ovom
// računalu i ne putuje razmjenom:
//
//	read "?Korisnik: " HDV_KORISNIK
//	read -s "?Lozinka: " HDV_LOZINKA
//	export HDV_KORISNIK HDV_LOZINKA
//	go run ./cmd/upis-hidroview-racuna
//
// Bez -letva upisuje se račun čvora, koji vrijedi za svaku letvu bez
// vlastitoga. S -letva upisuje se račun samo za tu letvu, pa sektor može
// čitati svoje postaje svojim računom.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"gocop/internal/db"
	"gocop/internal/hidroview"
	"gocop/internal/peers"
	"gocop/internal/posta"
	"gocop/internal/repository"

	_ "modernc.org/sqlite"
)

func main() {
	baza := flag.String("db", "data/gocop.db", "putanja do baze čvora")
	cvor := flag.String("cvor", "cop-osijek-node", "oznaka čvora, kao pri pokretanju programa")
	letva := flag.String("letva", "", "šifra letve; prazno znači račun čvora")
	adresa := flag.String("adresa", "", "adresa sustava; prazno znači "+hidroview.ZadanaAdresa)
	popis := flag.Bool("popis", false, "ispiši upisane račune i izađi")
	obrisi := flag.Bool("obrisi", false, "obriši račun zadane letve")
	bezProvjere := flag.Bool("bez-provjere", false, "ne pokušavaj prijavu prije spremanja")
	flag.Parse()

	sqlDB, err := db.OpenDB(*baza)
	if err != nil {
		log.Fatal(err)
	}
	defer sqlDB.Close()
	// Tablica računa može nedostajati u bazi nastaloj prije nje.
	if err := db.InitSchema(sqlDB); err != nil {
		log.Fatal("shema: ", err)
	}
	repo := repository.NewHidroViewRepository(sqlDB)
	ctx := context.Background()

	if *popis {
		racuni, err := repo.Racuni(ctx)
		if err != nil {
			log.Fatal(err)
		}
		if len(racuni) == 0 {
			fmt.Println("nema upisanih računa")
			return
		}
		for _, r := range racuni {
			gdje := r.Letva
			if gdje == "" {
				gdje = "(čvor)"
			}
			adr := r.Adresa
			if adr == "" {
				adr = hidroview.ZadanaAdresa
			}
			fmt.Printf("%-16s %-24s %s   upisano %s\n", gdje, r.Korisnik, adr,
				r.UpdatedAt.Local().Format("2.1.2006. 15:04"))
		}
		return
	}
	if *obrisi {
		if err := repo.Obrisi(ctx, *letva); err != nil {
			log.Fatal(err)
		}
		fmt.Println("obrisano")
		return
	}

	korisnik, lozinka := os.Getenv("HDV_KORISNIK"), os.Getenv("HDV_LOZINKA")
	if korisnik == "" || lozinka == "" {
		log.Fatal("nedostaju HDV_KORISNIK i HDV_LOZINKA u okolini")
	}

	// Prijava se prvo provjeri: bolje javiti odmah nego da letva svaki sat
	// javlja grešku koju nitko ne gleda.
	if !*bezProvjere {
		k := &hidroview.Klijent{Adresa: *adresa}
		ctxP, otkazi := context.WithTimeout(ctx, time.Minute)
		err := k.Prijava(ctxP, korisnik, lozinka)
		otkazi()
		if err != nil {
			log.Fatalf("prijava nije prošla, ništa nije spremljeno: %v", err)
		}
		fmt.Println("prijava je provjerena")
	}

	// Ključ čvora je isti onaj kojim program radi; bez njega se lozinka ne
	// može zaključati tako da je poslije umije otključati.
	node, err := peers.LoadNode(*baza, *cvor, "", "upis-racuna")
	if err != nil {
		log.Fatal("ključ čvora: ", err)
	}
	z, err := posta.Zakljucaj(hidroview.Kljuc(node.PrivateKey().Seed()), lozinka)
	if err != nil {
		log.Fatal(err)
	}
	if err := repo.Spremi(ctx, &repository.RacunHidroView{
		Letva: *letva, Adresa: *adresa, Korisnik: korisnik, Lozinka: z,
	}); err != nil {
		log.Fatal(err)
	}
	gdje := "čvor"
	if *letva != "" {
		gdje = "letva " + *letva
	}
	fmt.Printf("račun %s spremljen za %s\n", korisnik, gdje)
}
