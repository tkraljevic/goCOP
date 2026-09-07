// uvoz-vodostaja upisuje očitanja iz CSV datoteke uz bazu (vodostaji/) u
// registar, godinu po godinu, kroz knjigu verzija.
//
// Uvozi se i ono što nije mjereno na toj letvi — preračun iz druge postaje —
// pa svaki zapis nosi kvalitetu, izvor i način. Rekonstruirano očitanje ne
// smije se poslije čitati kao mjerenje: program ga zato posvuda i označava.
//
// Ide se po godinama namjerno. Uvezeš li sto godina odjednom, grešku nećeš
// primijetiti; ovako se poslije svake godine vidi koliko je ušlo i kakav je
// raspon.
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

type redak struct {
	kad  time.Time
	cm   int
	izvo string // polazna vrijednost preračuna, ako je datoteka nosi
}

func main() {
	dbPath := flag.String("db", "data/gocop.db", "putanja do baze")
	nodeID := flag.String("node", "cop-osijek-node", "oznaka čvora")
	csvPut := flag.String("csv", "", "datoteka s očitanjima (vrijeme_utc;vodostaj_cm[;polazna])")
	sifra := flag.String("postaja", "", "šifra postaje u registru, npr. batina")
	kvaliteta := flag.String("kvaliteta", models.QualityMeasured, "IZMJERENO, REKONSTRUIRANO ili SUMNJIVO")
	izvedeno := flag.String("izvedeno-iz", "", "odakle je preračunato, npr. „postaja Mohács”")
	metoda := flag.String("metoda", "", "kako je preračunato — formula ili opis postupka")
	oznaka := flag.String("oznaka", "", "oznaka uvoza (Origin), po kojoj se skup poslije prepoznaje")
	biljeska := flag.String("biljeska", "", "napomena uz svako očitanje; %s zamjenjuje polaznu vrijednost")
	odG := flag.Int("od", 0, "prva godina koja se uvozi")
	doG := flag.Int("do", 0, "zadnja godina koja se uvozi")
	suho := flag.Bool("probno", false, "samo ispiši što bi se upisalo")
	flag.Parse()

	if *csvPut == "" || *sifra == "" {
		log.Fatal("trebaju -csv i -postaja")
	}
	if *kvaliteta != models.QualityMeasured && *izvedeno == "" {
		log.Fatalf("očitanje koje nije izmjereno mora reći odakle je: dodaj -izvedeno-iz")
	}

	redci, err := citaj(*csvPut)
	if err != nil {
		log.Fatal(err)
	}
	if *odG > 0 || *doG > 0 {
		var f []redak
		for _, r := range redci {
			if (*odG == 0 || r.kad.Year() >= *odG) && (*doG == 0 || r.kad.Year() <= *doG) {
				f = append(f, r)
			}
		}
		redci = f
	}
	if len(redci) == 0 {
		log.Fatal("nema redaka za uvoz")
	}

	database, err := db.OpenDB(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	rec := ledger.New(database, *nodeID)
	repo := repository.NewReadingRepository(database, rec)

	var stationID string
	if err := database.QueryRow(`SELECT id FROM stations WHERE code = ?`, *sifra).Scan(&stationID); err != nil {
		log.Fatalf("postaja %q nije nađena: %v", *sifra, err)
	}

	ctx := context.Background()
	po := map[int][]redak{}
	for _, r := range redci {
		po[r.kad.Year()] = append(po[r.kad.Year()], r)
	}
	var godine []int
	for g := range po {
		godine = append(godine, g)
	}
	sort.Ints(godine)

	fmt.Printf("postaja %s (%s), %d redaka, %d.-%d.\n", *sifra, stationID, len(redci), godine[0], godine[len(godine)-1])
	if *kvaliteta != models.QualityMeasured {
		fmt.Printf("kvaliteta %s, izvedeno iz: %s\n", *kvaliteta, *izvedeno)
	}
	fmt.Println()

	ukupno := 0
	for _, g := range godine {
		ocitanja := make([]models.Reading, 0, len(po[g]))
		najnizi, najvisi := po[g][0].cm, po[g][0].cm
		for _, r := range po[g] {
			if r.cm < najnizi {
				najnizi = r.cm
			}
			if r.cm > najvisi {
				najvisi = r.cm
			}
			cm := r.cm
			ref := *oznaka + ":" + r.kad.Format("2006-01-02T15:04:05")
			nap := *biljeska
			if strings.Contains(nap, "%s") {
				nap = strings.ReplaceAll(nap, "%s", r.izvo)
			}
			ocitanja = append(ocitanja, models.Reading{
				ID:          db.StableID("reading", stationID+"|"+ref),
				StationID:   stationID,
				MeasuredAt:  r.kad,
				LevelCm:     &cm,
				Quality:     *kvaliteta,
				DerivedFrom: *izvedeno,
				Method:      *metoda,
				Source:      models.ReadingSourceImport,
				Origin:      *oznaka,
				SourceRef:   ref,
				Note:        nap,
			})
		}
		if *suho {
			fmt.Printf("%d.  %4d očitanja  %+5d .. %+5d cm  (probno)\n", g, len(ocitanja), najnizi, najvisi)
			continue
		}
		n, err := repo.ImportBatch(ctx, ocitanja)
		if err != nil {
			log.Fatalf("%d.: %v", g, err)
		}
		presko := len(ocitanja) - n
		poruka := ""
		if presko > 0 {
			poruka = fmt.Sprintf("  (%d već postoji)", presko)
		}
		fmt.Printf("%d.  %4d upisano  %+5d .. %+5d cm%s\n", g, n, najnizi, najvisi, poruka)
		ukupno += n
	}
	fmt.Printf("\nukupno upisano: %d\n", ukupno)
}

// citaj čita datoteku iz vodostaji/: točka-zarez, prvi redak nazivi stupaca,
// vrijeme u UTC-u.
func citaj(put string) ([]redak, error) {
	f, err := os.Open(put)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cr := csv.NewReader(f)
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	svi, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	var out []redak
	for i, r := range svi {
		if i == 0 && strings.HasPrefix(strings.ToLower(r[0]), "vrijeme") {
			continue
		}
		if len(r) < 2 || strings.TrimSpace(r[1]) == "" {
			continue
		}
		t, err := time.Parse("2006-01-02 15:04:05", strings.TrimSpace(r[0]))
		if err != nil {
			return nil, fmt.Errorf("redak %d: vrijeme %q nije čitljivo: %w", i+1, r[0], err)
		}
		cm, err := strconv.Atoi(strings.TrimSpace(r[1]))
		if err != nil {
			return nil, fmt.Errorf("redak %d: vodostaj %q nije cijeli broj: %w", i+1, r[1], err)
		}
		red := redak{kad: t.UTC(), cm: cm}
		if len(r) > 2 {
			red.izvo = strings.TrimSpace(r[2])
		}
		out = append(out, red)
	}
	return out, nil
}
