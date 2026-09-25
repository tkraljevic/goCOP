// usporedi-rkm ne mijenja podatke. Postaje s upisanim rkm projicira na
// detaljnu liniju vodotoka i uspoređuje unesenu stacionažu s geometrijskom
// udaljenošću od ušća.
//
//	go run ./cmd/usporedi-rkm -vodotok rijeka-drava
package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"

	"gocop/internal/geometrija"

	_ "modernc.org/sqlite"
)

type geoJSON struct {
	Features []struct {
		Properties struct {
			Tip string `json:"tip"`
		} `json:"properties"`
		Geometry struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"geometry"`
	} `json:"features"`
}

type postaja struct {
	naziv, zapis string
	uneseni      float64
	lat, lon     sql.NullFloat64
	izracunati   float64
	odstupanje   float64
	odLinije     float64
}

func main() {
	bazaPut := flag.String("baza", "data/gocop.db", "goCOP baza podataka")
	geoDir := flag.String("geometrija", "internal/geometrija", "imenik GeoJSON geometrija")
	vodotok := flag.String("vodotok", "rijeka-drava", "šifra vodotoka")
	flag.Parse()

	linija, err := ucitajLiniju(*geoDir, *vodotok)
	if err != nil {
		log.Fatal(err)
	}
	db, err := sql.Open("sqlite", *bazaPut+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	postaje, err := ucitajPostaje(db, *vodotok, linija)
	if err != nil {
		log.Fatal(err)
	}
	ispisi(*vodotok, geometrija.DuljinaKM(linija), postaje)
}

func ucitajLiniju(dir, code string) ([][2]float64, error) {
	podaci, err := geometrija.Ucitaj(dir, code)
	if err != nil {
		return nil, err
	}
	if len(podaci) == 0 {
		return nil, fmt.Errorf("vodotok %q nema GeoJSON geometriju", code)
	}
	var geo geoJSON
	if err := json.Unmarshal(podaci, &geo); err != nil {
		return nil, err
	}
	for _, f := range geo.Features {
		if f.Properties.Tip == "vodotok" && f.Geometry.Type == "LineString" {
			var linija [][2]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &linija); err != nil {
				return nil, fmt.Errorf("koordinate toka: %w", err)
			}
			if len(linija) >= 2 {
				return linija, nil
			}
		}
	}
	return nil, errors.New("GeoJSON nema LineString značajku vodotoka")
}

func ucitajPostaje(db *sql.DB, vodotok string, linija [][2]float64) ([]postaja, error) {
	redovi, err := db.Query(`SELECT name, stationing, latitude, longitude
		FROM stations
		WHERE watercourse_code = ? AND lower(trim(stationing)) LIKE 'rkm%'
		ORDER BY name`, vodotok)
	if err != nil {
		return nil, err
	}
	defer redovi.Close()

	var rezultat []postaja
	for redovi.Next() {
		var p postaja
		if err := redovi.Scan(&p.naziv, &p.zapis, &p.lat, &p.lon); err != nil {
			return nil, err
		}
		p.uneseni, err = procitajRKM(p.zapis)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.naziv, err)
		}
		if p.lat.Valid && p.lon.Valid {
			projekcija, err := geometrija.Projektiraj(linija, [2]float64{p.lon.Float64, p.lat.Float64})
			if err != nil {
				return nil, err
			}
			// OSM relacije u repozitoriju teku od izvora prema ušću.
			p.izracunati = projekcija.UkupnoKM - projekcija.UzduzKM
			p.odstupanje = p.izracunati - p.uneseni
			p.odLinije = projekcija.UdaljenostKM
		}
		rezultat = append(rezultat, p)
	}
	if err := redovi.Err(); err != nil {
		return nil, err
	}
	sort.Slice(rezultat, func(i, j int) bool { return rezultat[i].uneseni < rezultat[j].uneseni })
	return rezultat, nil
}

func procitajRKM(zapis string) (float64, error) {
	s := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(zapis)), "rkm"))
	s = strings.ReplaceAll(s, " ", "")
	if dijelovi := strings.Split(s, "+"); len(dijelovi) == 2 {
		km, err1 := strconv.ParseFloat(strings.ReplaceAll(dijelovi[0], ".", ""), 64)
		m, err2 := strconv.ParseFloat(strings.ReplaceAll(dijelovi[1], ",", "."), 64)
		if err1 != nil || err2 != nil {
			return 0, fmt.Errorf("ne mogu protumačiti stacionažu %q", zapis)
		}
		return km + m/1000, nil
	}
	if strings.Contains(s, ",") {
		s = strings.ReplaceAll(s, ".", "")
		s = strings.ReplaceAll(s, ",", ".")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("ne mogu protumačiti stacionažu %q", zapis)
	}
	return v, nil
}

func ispisi(vodotok string, duljina float64, postaje []postaja) {
	var pomaci []float64
	for _, p := range postaje {
		if p.lat.Valid && p.lon.Valid {
			pomaci = append(pomaci, p.odstupanje)
		}
	}
	sort.Float64s(pomaci)
	pomak := medijan(pomaci)

	fmt.Printf("# Usporedba rkm — %s\n\n", vodotok)
	fmt.Printf("Duljina detaljne OSM središnjice: **%.2f km**. Izračunati rkm mjeri se po toj liniji od ušća uzvodno.\n\n", duljina)
	fmt.Printf("Medijan zajedničkog pomaka geometrije prema upisanim sidrima: **%+.3f km**.\n\n", pomak)
	fmt.Println("| Postaja | Upisani rkm | Geometrijski rkm | Sirova razlika | Nakon pomaka | Od linije |")
	fmt.Println("|---|---:|---:|---:|---:|---:|")
	var n int
	var zbrojAbs, zbrojKv, zbrojAbsPoravnat, zbrojKvPoravnat float64
	var najveca *postaja
	for i := range postaje {
		p := &postaje[i]
		if !p.lat.Valid || !p.lon.Valid {
			fmt.Printf("| %s | %.3f | — | — | — | nema koordinata |\n", p.naziv, p.uneseni)
			continue
		}
		poravnato := p.odstupanje - pomak
		fmt.Printf("| %s | %.3f | %.3f | %+.3f | %+.3f | %.3f km |\n",
			p.naziv, p.uneseni, p.izracunati, p.odstupanje, poravnato, p.odLinije)
		n++
		zbrojAbs += math.Abs(p.odstupanje)
		zbrojKv += p.odstupanje * p.odstupanje
		zbrojAbsPoravnat += math.Abs(poravnato)
		zbrojKvPoravnat += poravnato * poravnato
		if najveca == nil || math.Abs(p.odstupanje) > math.Abs(najveca.odstupanje) {
			najveca = p
		}
	}
	if n > 0 {
		fmt.Printf("\nPostaja s koordinatama: **%d**; sirovi MAE: **%.3f km**; sirovi RMSE: **%.3f km**",
			n, zbrojAbs/float64(n), math.Sqrt(zbrojKv/float64(n)))
		if najveca != nil {
			fmt.Printf("; najveće odstupanje: **%s %+.3f km**", najveca.naziv, najveca.odstupanje)
		}
		fmt.Printf(". Nakon uklanjanja zajedničkog pomaka MAE je **%.3f km**, a RMSE **%.3f km**.\n",
			zbrojAbsPoravnat/float64(n), math.Sqrt(zbrojKvPoravnat/float64(n)))
	}
	ispisiSusjedskuProvjeru(postaje)
}

// ispisiSusjedskuProvjeru privremeno izostavlja jednu postaju i njezin rkm
// predviđa iz najbližeg nizvodnog i uzvodnog sidra. Velika razlika otkriva
// sidro koje lomi lokalni slijed, bez pretpostavke da je pogrešan baš rkm:
// uzrok može biti i koordinata postaje ili pogrešan krak geometrije.
func ispisiSusjedskuProvjeru(postaje []postaja) {
	var poredane []postaja
	for _, p := range postaje {
		if p.lat.Valid && p.lon.Valid {
			poredane = append(poredane, p)
		}
	}
	sort.Slice(poredane, func(i, j int) bool { return poredane[i].izracunati < poredane[j].izracunati })
	if len(poredane) < 3 {
		return
	}

	fmt.Println("\n## Provjera sidara susjednim postajama")
	fmt.Println()
	fmt.Println("| Postaja | Upisani rkm | Procjena iz susjeda | Razlika |")
	fmt.Println("|---|---:|---:|---:|")
	for i := 1; i+1 < len(poredane); i++ {
		prije, sada, poslije := poredane[i-1], poredane[i], poredane[i+1]
		raspon := poslije.izracunati - prije.izracunati
		if raspon <= 0 {
			continue
		}
		udio := (sada.izracunati - prije.izracunati) / raspon
		procjena := prije.uneseni + udio*(poslije.uneseni-prije.uneseni)
		fmt.Printf("| %s | %.3f | %.3f | %+.3f |\n", sada.naziv, sada.uneseni, procjena, sada.uneseni-procjena)
	}
}

func medijan(vrijednosti []float64) float64 {
	if len(vrijednosti) == 0 {
		return 0
	}
	sredina := len(vrijednosti) / 2
	if len(vrijednosti)%2 == 1 {
		return vrijednosti[sredina]
	}
	return (vrijednosti[sredina-1] + vrijednosti[sredina]) / 2
}
