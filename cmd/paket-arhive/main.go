// paket-arhive izdaje arhivu kao mapu .cop paketa s katalogom.
//
// Izdanje se ne upisuje rukom nego raste samo kad se sadržaj promijenio.
// Otisak paketa računa se preko podataka, ne preko manifesta — ne ovisi ni o
// vremenu ni o izdavaču — pa dva čvora koja imaju isto stanje dođu do istog
// broja izdanja bez ikakvog dogovora.
//
// Mapa koja iz ovoga izađe je ono što ide na USB, disk ili GitHub.
package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gocop/internal/arhiva"
	_ "modernc.org/sqlite"
)

// UPaketu je jedan redak kataloga.
type UPaketu struct {
	Letva    string `json:"letva"`
	Izdanje  int    `json:"izdanje"`
	Otisak   string `json:"otisak"`
	Datoteka string `json:"datoteka"`
	Nizova   int    `json:"nizova"`
	Zapisa   int    `json:"zapisa"`
	Od       string `json:"od"`
	Do       string `json:"do"`
	Bajtova  int64  `json:"bajtova"`
}

// Katalog je popis izdanih paketa. Čvor koji dobije mapu po njemu vidi što
// ima i je li novije od onoga što drži.
type Katalog struct {
	Inacica int       `json:"inacica"`
	Nastalo time.Time `json:"nastalo"`
	Izdao   string    `json:"izdao"`
	Paketi  []UPaketu `json:"paketi"`
}

const imeKataloga = "katalog.json"

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

	letve, err := letveUArhivi(db, *samo)
	if err != nil {
		log.Fatal(err)
	}
	if len(letve) == 0 {
		log.Fatal("arhiva nema nijednu letvu")
	}

	prijasnji := ucitajKatalog(filepath.Join(*uMapu, imeKataloga))
	poLetvi := map[string]UPaketu{}
	for _, p := range prijasnji.Paketi {
		poLetvi[p.Letva] = p
	}

	if !*suho {
		if err := os.MkdirAll(*uMapu, 0o755); err != nil {
			log.Fatal(err)
		}
	}

	novi := Katalog{Inacica: arhiva.PaketInacica, Nastalo: time.Now().UTC(), Izdao: *izdao}
	var promijenjenih, istih int
	for _, letva := range letve {
		prije, imaPrije := poLetvi[letva]
		izdanje := prije.Izdanje
		if izdanje < 1 {
			izdanje = 1
		}

		var b bytes.Buffer
		m, err := arhiva.Izvezi(db, letva, izdanje, *izdao, &b)
		if err != nil {
			fmt.Printf("  %-18s preskačem: %v\n", letva, err)
			continue
		}
		if imaPrije && m.Otisak == prije.Otisak {
			fmt.Printf("  %-18s v%-3d nepromijenjeno\n", letva, prije.Izdanje)
			novi.Paketi = append(novi.Paketi, prije)
			istih++
			continue
		}
		// Sadržaj je drukčiji, pa i izdanje mora biti — a manifest nosi broj
		// izdanja, što znači da se paket mora složiti iznova.
		if imaPrije {
			izdanje = prije.Izdanje + 1
			b.Reset()
			if m, err = arhiva.Izvezi(db, letva, izdanje, *izdao, &b); err != nil {
				fmt.Printf("  %-18s preskačem: %v\n", letva, err)
				continue
			}
		}
		ime := fmt.Sprintf("%s_v%d.cop", letva, izdanje)
		red := UPaketu{Letva: letva, Izdanje: izdanje, Otisak: m.Otisak, Datoteka: ime,
			Nizova: m.Nizova, Zapisa: m.Zapisa, Od: m.Od, Do: m.Do, Bajtova: int64(b.Len())}

		stanje := "novo"
		if imaPrije {
			stanje = fmt.Sprintf("v%d → v%d", prije.Izdanje, izdanje)
		}
		fmt.Printf("  %-18s v%-3d %-10s %8d zapisa  %s .. %s  %5.0f kB\n",
			letva, izdanje, stanje, m.Zapisa, m.Od, m.Do, float64(b.Len())/1e3)

		// Starije izdanje se ne briše: čvor koji ga još nije preuzeo treba ga
		// moći naći, a katalog kaže koje je najnovije.
		if !*suho {
			if err := zapisiPaket(filepath.Join(*uMapu, ime), b.Bytes()); err != nil {
				log.Fatal(err)
			}
		}
		novi.Paketi = append(novi.Paketi, red)
		promijenjenih++
	}

	sort.Slice(novi.Paketi, func(a, b int) bool { return novi.Paketi[a].Letva < novi.Paketi[b].Letva })
	var zapisa, bajtova int64
	for _, p := range novi.Paketi {
		zapisa += int64(p.Zapisa)
		bajtova += p.Bajtova
	}
	fmt.Printf("\n%d %s, %d promijenjeno, %d nepromijenjeno\n",
		len(novi.Paketi), uzBrojPaket(len(novi.Paketi)), promijenjenih, istih)
	fmt.Printf("ukupno %d zapisa u %.1f MB\n", zapisa, float64(bajtova)/1e6)

	if *suho {
		fmt.Println("\nproba — ništa nije zapisano; ponovite bez -probno")
		return
	}
	if err := zapisiKatalog(filepath.Join(*uMapu, imeKataloga), novi); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("katalog: %s\n", filepath.Join(*uMapu, imeKataloga))
}

// letveUArhivi vraća letve koje imaju barem jedan niz, poredane.
func letveUArhivi(db *sql.DB, samo string) ([]string, error) {
	q := `SELECT DISTINCT letva FROM nizovi`
	var args []any
	if samo != "" {
		q += ` WHERE letva = ?`
		args = append(args, samo)
	}
	q += ` ORDER BY letva`
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ucitajKatalog čita prošlo izlaganje. Kataloga nema pri prvom izdavanju i to
// nije greška — tada su sve letve nove.
func ucitajKatalog(put string) Katalog {
	var k Katalog
	b, err := os.ReadFile(put)
	if err != nil {
		return k
	}
	if err := json.Unmarshal(b, &k); err != nil {
		fmt.Fprintf(os.Stderr, "katalog %s se ne čita (%v); nastavljam kao da ga nema\n", put, err)
		return Katalog{}
	}
	return k
}

// zapisiPaket piše sa strane pa preimenuje: prekid usred pisanja ne smije
// ostaviti pola paketa pod imenom koje izgleda cjelovito.
func zapisiPaket(put string, sadrzaj []byte) error {
	privremeno := put + ".novo"
	if err := os.WriteFile(privremeno, sadrzaj, 0o644); err != nil {
		return err
	}
	return os.Rename(privremeno, put)
}

func zapisiKatalog(put string, k Katalog) error {
	b, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}
	return zapisiPaket(put, append(b, '\n'))
}

func uzBrojPaket(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return "paket"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return "paketa"
	default:
		return "paketa"
	}
}
