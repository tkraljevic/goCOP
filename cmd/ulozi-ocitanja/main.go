// ulozi-ocitanja pretvara operativna očitanja u arhivski niz.
//
// Sam posao živi u internal/ulaganje, jer ga radi i stranica (Administracija →
// Unos u arhivu). Ovdje je samo naredbeni redak oko njega: isti posao iz
// terminala, za čvor bez sučelja i za noćno pospremanje.
//
// Ulaganje i zaboravljanje su dva koraka. Uloženo se ne briše dok se ne
// provjeri da je doista u arhivi.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"gocop/internal/db"
	"gocop/internal/ulaganje"
	_ "modernc.org/sqlite"
)

func main() {
	dbPath := flag.String("db", "data/gocop.db", "putanja do baze programa")
	arhivaPut := flag.String("arhiva", "data/vodostaji.db", "arhivska baza")
	koren := flag.String("podaci", "vodostaji", "stablo s izvornim datotekama arhive")
	nodeID := flag.String("node", "cop-osijek-node", "oznaka čvora")
	sifra := flag.String("letva", "", "šifra postaje, npr. vukovar")
	odS := flag.String("od", "", "od datuma, YYYY-MM-DD")
	doS := flag.String("do", "", "do datuma, YYYY-MM-DD (uključivo)")
	izvor := flag.String("izvor", ulaganje.IzvorDojave, "pod kojim izvorom se ulaže ono što je stiglo dojavom")
	izvorRucno := flag.String("izvor-rucno", ulaganje.IzvorRucnog, "pod kojim izvorom se ulaže ono što je čovjek očitao na letvi")
	vrsta := flag.String("vrsta", "", "vrsta niza; prazno znači zatečena, pa pogodi iz gustoće očitanja")
	izdanje := flag.String("izdanje", "", "oznaka izdanja koja se upisuje uz uloženo očitanje")
	zaboravi := flag.Bool("zaboravi", false, "obriši uložena očitanja i njihove verzije")
	suho := flag.Bool("probno", false, "samo javi što bi se dogodilo")
	flag.Parse()

	if *sifra == "" {
		log.Fatal("zadajte -letva")
	}
	baza, err := db.OpenDB(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer baza.Close()
	ctx := context.Background()

	// Zaboravljanje ne treba razdoblje: posprema sve što je uloženo i
	// provjereno, pa je i zasebna naredba.
	if *zaboravi && *odS == "" && *doS == "" {
		pospremi(ctx, baza, *arhivaPut, *sifra)
		return
	}

	if *odS == "" || *doS == "" {
		log.Fatal("zadajte -od i -do")
	}
	od, err := time.ParseInLocation("2006-01-02", *odS, time.UTC)
	if err != nil {
		log.Fatalf("-od: %v", err)
	}
	do, err := time.ParseInLocation("2006-01-02", *doS, time.UTC)
	if err != nil {
		log.Fatalf("-do: %v", err)
	}
	do = do.AddDate(0, 0, 1).Add(-time.Second)

	z := ulaganje.Zahtjev{
		Baza: baza, ArhivaPut: *arhivaPut, Koren: *koren, Cvor: *nodeID,
		Letva: *sifra, Od: od, Do: do,
		Izvor: *izvor, IzvorRucno: *izvorRucno, Vrsta: *vrsta, Izdanje: *izdanje,
	}
	p, err := ulaganje.Pripremi(ctx, z)
	if err != nil {
		log.Fatal(err)
	}
	if p.Ukupno == 0 {
		fmt.Println("nema očitanja za ulaganje u tom razdoblju")
		return
	}

	fmt.Printf("%s (%s), %s – %s\n", p.Postaja.Name, p.Postaja.Code, *odS, *doS)
	fmt.Printf("  očitanja:      %d\n", p.Ukupno)
	fmt.Printf("  dojavljeno:    %d  → %s\n", p.Mjereno, p.Izvor)
	if p.Rucno > 0 {
		fmt.Printf("  ručno s letve: %d  → %s\n", p.Rucno, p.IzvorRucnog)
	}
	if p.Preracunato > 0 {
		fmt.Printf("  rekonstruirano:%d  → ulaže se odvojeno, kao preracun-%s\n", p.Preracunato, p.Izvor)
	}
	if p.Sumnjivo > 0 {
		fmt.Printf("  sumnjivo:      %d  → NE ulaže se; ostaje u operativi\n", p.Sumnjivo)
	}
	if p.BezVrijednosti > 0 {
		fmt.Printf("  bez vodostaja: %d  → NE ulaže se (građevine, prazna očitanja)\n", p.BezVrijednosti)
	}
	fmt.Printf("  s bilješkom:   %d  → prelazi u bilješke uz vrijednost\n", p.SBiljeskom)
	fmt.Printf("  vrsta niza:    %s\n", p.Vrsta)

	if *suho {
		fmt.Println("\nproba — ništa nije zapisano; ponovite bez -probno")
		return
	}

	iz, err := p.Ulozi(ctx, z, os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("nizova %d, očitanja %d, spojenih %d\n", iz.Nizova, iz.Ocitanja, iz.Spojenih)

	if !*zaboravi {
		fmt.Println("\nuloženo je, ali ništa nije obrisano. Za pospremanje ponovite s -zaboravi")
		return
	}
	pospremi(ctx, baza, *arhivaPut, *sifra)
}

func pospremi(ctx context.Context, baza *sql.DB, arhivaPut, sifra string) {
	postaja, err := ulaganje.PostajaPoSifri(ctx, baza, sifra)
	if err != nil {
		log.Fatal(err)
	}
	iz, err := ulaganje.Zaboravi(ctx, baza, arhivaPut, postaja.ID.String(), os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("pospremljeno: %d očitanja i %d verzija\n", iz.Obrisano, iz.Verzija)
}
