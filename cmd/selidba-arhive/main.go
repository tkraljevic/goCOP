// selidba-arhive miče povijesne nizove iz gocop.db u arhivsku bazu.
//
// gocop.db vodi ono što ovaj sustav proizvodi: očitanje koje vodočuvar upiše i
// odluke koje ljudi donesu. Povijesni niz nije ni jedno ni drugo — donesen je
// izvana, ne uređuje se i uvijek se može ponovno napraviti iz vodostaji/. Dok
// je stajao u glavnoj bazi, uz svaki je redak išla i verzija za sinkronizaciju,
// pa je 45.547 očitanja jedne letve zauzelo 61 % baze.
//
// Alat prvo provjeri da arhiva doista pokriva svaki zapis koji odlazi, i odbija
// posao ako ne pokriva. Brisanje bez te provjere značilo bi gubitak podatka.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

// veza jednog skupa očitanja u gocop.db s nizom u arhivi
type veza struct {
	origin                  string
	letva, izvor, vel, vrst string
}

var veze = []veza{
	{"DHMZ/HIS2000 — Batina, dnevni vodostaji 2001.–2024.", "batina", "his2000", "vodostaj", "srednjak"},
	{"PRERACUN-MOHACS", "batina", "preracun-mohacs", "vodostaj", "srednjak"},
	{"letva.voda.hr — Batina (DHMZ), satni niz, očitanje u 07 h", "batina", "letva-dhmz", "vodostaj", "jutarnji"},
}

func main() {
	glavna := flag.String("db", "data/gocop.db", "operativna baza")
	arhiva := flag.String("arhiva", "data/vodostaji.db", "arhivska baza")
	epizode := flag.Bool("epizode", false, "obriši i epizode obrane izvedene iz tih očitanja")
	izvedi := flag.Bool("izvedi", false, "stvarno obriši; bez toga se samo provjerava")
	flag.Parse()

	g, err := sql.Open("sqlite", *glavna)
	if err != nil {
		log.Fatal(err)
	}
	defer g.Close()
	a, err := sql.Open("sqlite", *arhiva+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer a.Close()

	fmt.Println("provjera pokrivenosti:")
	sve := true
	ukupno := 0
	for _, v := range veze {
		var n int
		var od, do sql.NullString
		if err := g.QueryRow(`SELECT count(*), min(substr(measured_at,1,10)), max(substr(measured_at,1,10))
			FROM readings WHERE origin = ?`, v.origin).Scan(&n, &od, &do); err != nil {
			log.Fatal(err)
		}
		if n == 0 {
			fmt.Printf("  %-18s %-10s nema zapisa u bazi\n", v.izvor, v.vrst)
			continue
		}
		var an int
		var aod, ado string
		err := a.QueryRow(`SELECT zapisa, od, do_ FROM nizovi
			WHERE letva=? AND izvor=? AND velicina=? AND vrsta=?`, v.letva, v.izvor, v.vel, v.vrst).
			Scan(&an, &aod, &ado)
		if err == sql.ErrNoRows {
			fmt.Printf("  %-18s %-10s NEMA u arhivi\n", v.izvor, v.vrst)
			sve = false
			continue
		} else if err != nil {
			log.Fatal(err)
		}
		pokriva := aod <= od.String && ado >= do.String
		stanje := "pokriva"
		if !pokriva {
			stanje = "NE POKRIVA"
			sve = false
		}
		fmt.Printf("  %-18s %-10s baza %6d (%s..%s)   arhiva %6d (%s..%s)  %s\n",
			v.izvor, v.vrst, n, od.String, do.String, an, aod, ado, stanje)
		ukupno += n
	}

	var ostatak int
	if err := g.QueryRow(`SELECT count(*) FROM readings WHERE origin NOT IN (?, ?, ?)`,
		veze[0].origin, veze[1].origin, veze[2].origin).Scan(&ostatak); err != nil {
		log.Fatal(err)
	}
	if ostatak > 0 {
		fmt.Printf("\n  %d očitanja nije ni u jednoj vezi — ta ostaju u gocop.db\n", ostatak)
	}
	if !sve {
		fmt.Fprintln(os.Stderr, "\narhiva ne pokriva sve — selidba se ne izvodi")
		os.Exit(1)
	}
	fmt.Printf("\nsve pokriveno; za selidbu %d očitanja\n", ukupno)

	if !*izvedi {
		fmt.Println("\nprobni prolaz. Za stvarnu selidbu dodaj -izvedi")
		return
	}

	tx, err := g.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()

	obrisano := 0
	for _, v := range veze {
		res, err := tx.Exec(`DELETE FROM record_versions WHERE entity='readings' AND entity_id IN
			(SELECT id FROM readings WHERE origin = ?)`, v.origin)
		if err != nil {
			log.Fatal(err)
		}
		nv, _ := res.RowsAffected()
		res, err = tx.Exec(`DELETE FROM readings WHERE origin = ?`, v.origin)
		if err != nil {
			log.Fatal(err)
		}
		no, _ := res.RowsAffected()
		fmt.Printf("  %-18s obrisano %d očitanja i %d verzija\n", v.izvor, no, nv)
		obrisano += int(no)
	}

	if *epizode {
		// Epizode su bile izvedene iz očitanja koja upravo odlaze, i to iz niza
		// koji je poslije ispravljen. Preračunat će se iznova iz arhive.
		res, err := tx.Exec(`DELETE FROM defense_episodes`)
		if err != nil {
			log.Fatal(err)
		}
		n, _ := res.RowsAffected()
		if _, err := tx.Exec(`DELETE FROM record_versions WHERE entity='defense_episodes'`); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  epizode obrane      obrisano %d\n", n)
	}
	if err := tx.Commit(); err != nil {
		log.Fatal(err)
	}

	if _, err := g.Exec(`VACUUM`); err != nil {
		log.Fatal(err)
	}
	if st, err := os.Stat(*glavna); err == nil {
		fmt.Printf("\n%s: %.1f MB  (%s)\n", *glavna, float64(st.Size())/1e6,
			time.Now().Format("2.1.2006. 15:04"))
	}
}
