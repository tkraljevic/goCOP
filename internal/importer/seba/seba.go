// Package seba čita izvoze telemetrije Geolux SmartObserver / SEBA.
package seba

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/arhiva"
)

// Izvjestaj opisuje što je pronađeno u izvornim datotekama. Izvorni redci se
// ne mijenjaju; nečitljive i fizički nemoguće vrijednosti samo ne ulaze u niz.
type Izvjestaj struct {
	Redaka, Duplikata, Sukoba                    int
	BezVodostaja, BezTemperature                 int
	NeispravnihVodostaja, NeispravnihTemperatura int
}

// Rezultat nosi dva neovisna niza. Prazna temperatura ne smije izbaciti
// valjani vodostaj istog trenutka, niti obratno.
type Rezultat struct {
	Vodostaji   []arhiva.Redak
	Temperature []arhiva.Redak
	Izvjestaj   Izvjestaj
}

type vrijednosti struct {
	vodostaj    *float64
	temperatura *float64
}

// Citaj spaja više uzastopnih izvoza. Datoteke se čitaju zadanim redom; kad
// se isti trenutak ponovi, zadnja valjana vrijednost pobjeđuje jer noviji
// izvoz može ispraviti raniji. Prazna ili nevaljana vrijednost ne briše dobru.
func Citaj(putanje []string) (Rezultat, error) {
	var out Rezultat
	poVremenu := map[int64]vrijednosti{}
	videno := map[int64]bool{}
	for _, put := range putanje {
		if err := citajJednu(put, poVremenu, videno, &out.Izvjestaj); err != nil {
			return Rezultat{}, err
		}
	}

	kljucevi := make([]int64, 0, len(poVremenu))
	for k := range poVremenu {
		kljucevi = append(kljucevi, k)
	}
	sort.Slice(kljucevi, func(i, j int) bool { return kljucevi[i] < kljucevi[j] })
	for _, k := range kljucevi {
		v := poVremenu[k]
		kad := time.Unix(k, 0).UTC()
		if v.vodostaj != nil {
			out.Vodostaji = append(out.Vodostaji, arhiva.Redak{Vrijeme: kad, Vrijednost: *v.vodostaj})
		}
		if v.temperatura != nil {
			out.Temperature = append(out.Temperature, arhiva.Redak{Vrijeme: kad, Vrijednost: *v.temperatura})
		}
	}
	return out, nil
}

func citajJednu(put string, po map[int64]vrijednosti, videno map[int64]bool, iz *Izvjestaj) error {
	f, err := os.Open(put)
	if err != nil {
		return err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	glava, err := r.Read()
	if err != nil {
		return fmt.Errorf("%s: zaglavlje: %w", put, err)
	}
	iVrijeme, iVodostaj, iTemperatura := -1, -1, -1
	for i, s := range glava {
		s = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(s, "\ufeff")))
		switch {
		case s == "time":
			iVrijeme = i
		case strings.Contains(s, "average water level"):
			iVodostaj = i
		case strings.Contains(s, "water temperature"):
			iTemperatura = i
		}
	}
	if iVrijeme < 0 || iVodostaj < 0 || iTemperatura < 0 {
		return fmt.Errorf("%s: nedostaje stupac Time, Average Water Level ili Water Temperature", put)
	}

	for redak := 2; ; redak++ {
		z, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("%s redak %d: %w", put, redak, err)
		}
		iz.Redaka++
		kad, err := vrijeme(polje(z, iVrijeme))
		if err != nil {
			return fmt.Errorf("%s redak %d: %w", put, redak, err)
		}
		kljuc := kad.Unix()
		if videno[kljuc] {
			iz.Duplikata++
		}
		videno[kljuc] = true
		staro := po[kljuc]
		novo := staro

		if s := strings.TrimSpace(polje(z, iVodostaj)); s == "" {
			iz.BezVodostaja++
		} else if m, err := strconv.ParseFloat(s, 64); err != nil {
			iz.NeispravnihVodostaja++
		} else {
			cm := m * 100
			if !arhiva.MogucaVrijednost("vodostaj", cm) {
				iz.NeispravnihVodostaja++
			} else {
				if staro.vodostaj != nil && *staro.vodostaj != cm {
					iz.Sukoba++
				}
				novo.vodostaj = broj(cm)
			}
		}

		if s := strings.TrimSpace(polje(z, iTemperatura)); s == "" {
			iz.BezTemperature++
		} else if c, err := strconv.ParseFloat(s, 64); err != nil {
			iz.NeispravnihTemperatura++
		} else if !arhiva.MogucaVrijednost("temperatura", c) {
			iz.NeispravnihTemperatura++
		} else {
			if staro.temperatura != nil && *staro.temperatura != c {
				iz.Sukoba++
			}
			novo.temperatura = broj(c)
		}
		po[kljuc] = novo
	}
	return nil
}

func broj(v float64) *float64 { return &v }

func polje(r []string, i int) string {
	if i >= 0 && i < len(r) {
		return r[i]
	}
	return ""
}

func vrijeme(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, oblik := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(oblik, s, arhiva.Zagreb); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("vrijeme %q nije čitljivo", s)
}
