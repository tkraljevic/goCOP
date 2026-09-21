// Package pegelonline čita JSON izvoz vodostaja servisa PEGELONLINE.
package pegelonline

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"gocop/internal/arhiva"
)

type zapis struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
}

type Izvjestaj struct {
	Redaka, IzvanPunogSata, Neispravnih, Duplikata, Sukoba int
}

type Rezultat struct {
	Vodostaji []arhiva.Redak
	Izvjestaj Izvjestaj
}

// Citaj uzima samo očitanja na punom satu. Ne radi prosjek četiriju
// 15-minutnih vodostaja jer bi to stvorilo novu, zaglađenu vrijednost koju
// izvor nije objavio.
func Citaj(put string) (Rezultat, error) {
	var out Rezultat
	raw, err := os.ReadFile(put)
	if err != nil {
		return out, err
	}
	var ulaz []zapis
	if err := json.Unmarshal(raw, &ulaz); err != nil {
		return out, fmt.Errorf("%s: %w", put, err)
	}
	poVremenu := map[int64]float64{}
	for _, z := range ulaz {
		out.Izvjestaj.Redaka++
		kad, err := time.Parse(time.RFC3339, z.Timestamp)
		if err != nil || !arhiva.MogucaVrijednost("vodostaj", z.Value) {
			out.Izvjestaj.Neispravnih++
			continue
		}
		if kad.Minute() != 0 || kad.Second() != 0 || kad.Nanosecond() != 0 {
			out.Izvjestaj.IzvanPunogSata++
			continue
		}
		k := kad.UTC().Unix()
		if staro, postoji := poVremenu[k]; postoji {
			out.Izvjestaj.Duplikata++
			if staro != z.Value {
				out.Izvjestaj.Sukoba++
			}
		}
		poVremenu[k] = z.Value
	}
	kljucevi := make([]int64, 0, len(poVremenu))
	for k := range poVremenu {
		kljucevi = append(kljucevi, k)
	}
	sort.Slice(kljucevi, func(i, j int) bool { return kljucevi[i] < kljucevi[j] })
	for _, k := range kljucevi {
		out.Vodostaji = append(out.Vodostaji, arhiva.Redak{
			Vrijeme: time.Unix(k, 0).UTC(), Vrijednost: poVremenu[k],
		})
	}
	if len(out.Vodostaji) == 0 {
		return Rezultat{}, fmt.Errorf("%s: nema nijednog valjanog očitanja na punom satu", put)
	}
	return out, nil
}
