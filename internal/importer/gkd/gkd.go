// Package gkd čita ZIP izvoze dnevnih protoka bavarskog GKD-a.
package gkd

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/raspakiraj"
)

type Izvjestaj struct {
	Postaja, Broj                                 string
	Datoteka, Redaka, BezPodataka                 int
	Neprovjerenih, Neispravnih, Duplikata, Sukoba int
}

type Rezultat struct {
	Protoci   []arhiva.Redak
	Izvjestaj Izvjestaj
}

// Citaj uzima samo provjerene dnevne srednje protoke. Maksimum i minimum
// ostaju u izvornom ZIP-u jer arhiva zasad vodi po jedan niz za jednu veličinu.
// ZIP s previše stavki ili s više raspakiranih bajtova nego što uvoz prima
// odbija se (raspakiraj.Zadana).
func Citaj(put string) (Rezultat, error) { return citaj(put, raspakiraj.Zadana()) }

func citaj(put string, g *raspakiraj.Granica) (Rezultat, error) {
	var out Rezultat
	z, err := zip.OpenReader(put)
	if err != nil {
		return out, err
	}
	defer z.Close()
	if err := g.Stavke(len(z.File)); err != nil {
		return out, err
	}
	poDanu := map[int64]float64{}
	if err := citajStavke(z.File, g, poDanu, &out.Izvjestaj); err != nil {
		return Rezultat{}, err
	}
	out.Protoci = protociPoDanu(poDanu)
	if len(out.Protoci) == 0 {
		return Rezultat{}, fmt.Errorf("%s: nema provjerenih dnevnih srednjih protoka", put)
	}
	return out, nil
}

// citajStavke čita CSV stavke ZIP-a; mape i ostale datoteke preskače.
func citajStavke(stavke []*zip.File, g *raspakiraj.Granica, poDanu map[int64]float64, iz *Izvjestaj) error {
	for _, f := range stavke {
		if f.FileInfo().IsDir() || !strings.HasSuffix(strings.ToLower(f.Name), ".csv") {
			continue
		}
		iz.Datoteka++
		if err := citajDatoteku(f, g, poDanu, iz); err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	return nil
}

// protociPoDanu slaže dnevne protoke od najstarijeg dana.
func protociPoDanu(poDanu map[int64]float64) []arhiva.Redak {
	var out []arhiva.Redak
	kljucevi := make([]int64, 0, len(poDanu))
	for k := range poDanu {
		kljucevi = append(kljucevi, k)
	}
	sort.Slice(kljucevi, func(i, j int) bool { return kljucevi[i] < kljucevi[j] })
	for _, k := range kljucevi {
		out = append(out, arhiva.Redak{
			Vrijeme: time.Unix(k, 0).UTC(), PoDanu: true, Vrijednost: poDanu[k],
		})
	}
	return out
}

func citajDatoteku(f *zip.File, g *raspakiraj.Granica, po map[int64]float64, iz *Izvjestaj) error {
	raw, err := g.Otvori(f)
	if err != nil {
		return err
	}
	defer raw.Close()
	r := csv.NewReader(raw)
	r.Comma = ';'
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	uTablici := false
	imaVrijednost := false
	for brojRetka := 1; ; brojRetka++ {
		red, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("redak %d: %w", brojRetka, err)
		}
		if len(red) == 0 {
			continue
		}
		prvo := strings.TrimSpace(strings.TrimPrefix(red[0], "\ufeff"))
		if !uTablici {
			if len(red) > 1 {
				vrijednost := strings.TrimSpace(red[1])
				switch strings.TrimSuffix(prvo, ":") {
				case "Messstellen-Name":
					iz.Postaja = vrijednost
				case "Messstellen-Nr.":
					iz.Broj = vrijednost
				}
			}
			if prvo == "Datum" && len(red) >= 5 && strings.TrimSpace(red[1]) == "Mittelwert" {
				uTablici = true
			}
			continue
		}
		if len(red) < 5 || len(prvo) != 10 {
			continue
		}
		iz.Redaka++
		if strings.TrimSpace(red[4]) != "Geprueft" {
			iz.Neprovjerenih++
			continue
		}
		kad, err := time.Parse("2006-01-02", prvo)
		if err != nil {
			iz.Neispravnih++
			continue
		}
		v, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(red[1]), ",", "."), 64)
		if err != nil || !arhiva.MogucaVrijednost("protok", v) {
			iz.Neispravnih++
			continue
		}
		k := kad.UTC().Unix()
		if staro, postoji := po[k]; postoji {
			iz.Duplikata++
			if staro != v {
				iz.Sukoba++
			}
		}
		po[k] = v
		imaVrijednost = true
	}
	if !imaVrijednost {
		iz.BezPodataka++
	}
	return nil
}
