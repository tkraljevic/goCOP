// Package arso čita dnevne hidrološke nizove slovenskog ARSO-a.
package arso

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

// Izvjestaj opisuje što je pronađeno u izvornim datotekama. Prazna ćelija
// nije greška: ARSO-ovi stariji nizovi često počinju samo protokom, a ostale
// se veličine pojavljuju kasnije.
type Izvjestaj struct {
	Redaka, NeispravnihDatuma                                        int
	Duplikata, Sukoba                                                int
	BezVodostaja, BezProtoka, BezTemperature                         int
	NeispravnihVodostaja, NeispravnihProtoka, NeispravnihTemperatura int
}

// Rezultat nosi tri neovisna dnevna niza. Svaki redak ima samo datum jer
// dnevni srednjak nije mjerenje u određenom satu.
type Rezultat struct {
	Vodostaji   []arhiva.Redak
	Protoci     []arhiva.Redak
	Temperature []arhiva.Redak
	Izvjestaj   Izvjestaj
}

type stupci struct {
	datum, vodostaj, protok, temperatura int
}

type dan struct {
	vodostaj, protok, temperatura *float64
}

// Citaj spaja jednu ili više ARSO datoteka. Kad se isti datum ponovi, zadnja
// valjana vrijednost pobjeđuje; prazna ili nevaljana ćelija ne briše dobru.
func Citaj(putanje []string) (Rezultat, error) {
	var out Rezultat
	poDanu := map[int64]dan{}
	videno := map[int64]bool{}
	for _, put := range putanje {
		if err := citajJednu(put, poDanu, videno, &out.Izvjestaj); err != nil {
			return Rezultat{}, err
		}
	}

	kljucevi := make([]int64, 0, len(poDanu))
	for k := range poDanu {
		kljucevi = append(kljucevi, k)
	}
	sort.Slice(kljucevi, func(i, j int) bool { return kljucevi[i] < kljucevi[j] })
	for _, k := range kljucevi {
		v := poDanu[k]
		kad := time.Unix(k, 0).UTC()
		if v.vodostaj != nil {
			out.Vodostaji = append(out.Vodostaji, arhiva.Redak{Vrijeme: kad, PoDanu: true, Vrijednost: *v.vodostaj})
		}
		if v.protok != nil {
			out.Protoci = append(out.Protoci, arhiva.Redak{Vrijeme: kad, PoDanu: true, Vrijednost: *v.protok})
		}
		if v.temperatura != nil {
			out.Temperature = append(out.Temperature, arhiva.Redak{Vrijeme: kad, PoDanu: true, Vrijednost: *v.temperatura})
		}
	}
	return out, nil
}

func citajJednu(put string, po map[int64]dan, videno map[int64]bool, iz *Izvjestaj) error {
	f, err := os.Open(put)
	if err != nil {
		return err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.Comma = ';'
	r.FieldsPerRecord = -1
	glava, err := r.Read()
	if err != nil {
		return fmt.Errorf("%s: zaglavlje: %w", put, err)
	}
	s := nadjiStupce(glava)
	if s.datum < 0 || (s.vodostaj < 0 && s.protok < 0 && s.temperatura < 0) {
		return fmt.Errorf("%s: nisu pronađeni datum ni hidrološke vrijednosti", put)
	}

	for brojRetka := 2; ; brojRetka++ {
		red, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("%s redak %d: %w", put, brojRetka, err)
		}
		iz.Redaka++
		kad, err := datum(polje(red, s.datum))
		if err != nil {
			iz.NeispravnihDatuma++
			continue
		}
		kljuc := kad.Unix()
		if videno[kljuc] {
			iz.Duplikata++
		}
		videno[kljuc] = true
		staro := po[kljuc]
		novo := staro
		obradi(polje(red, s.vodostaj), "vodostaj", staro.vodostaj, &novo.vodostaj,
			&iz.BezVodostaja, &iz.NeispravnihVodostaja, &iz.Sukoba)
		obradi(polje(red, s.protok), "protok", staro.protok, &novo.protok,
			&iz.BezProtoka, &iz.NeispravnihProtoka, &iz.Sukoba)
		obradi(polje(red, s.temperatura), "temperatura", staro.temperatura, &novo.temperatura,
			&iz.BezTemperature, &iz.NeispravnihTemperatura, &iz.Sukoba)
		po[kljuc] = novo
	}
	return nil
}

func nadjiStupce(glava []string) stupci {
	s := stupci{datum: -1, vodostaj: -1, protok: -1, temperatura: -1}
	for i, v := range glava {
		v = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(v, "\ufeff")))
		switch {
		case v == "datum":
			s.datum = i
		case strings.Contains(v, "vodostaj") && strings.Contains(v, "cm"):
			s.vodostaj = i
		case strings.Contains(v, "pretok") && strings.Contains(v, "m3/s"):
			s.protok = i
		case strings.Contains(v, "temp") && strings.Contains(v, "°c"):
			s.temperatura = i
		}
	}
	return s
}

func obradi(s, velicina string, staro *float64, odrediste **float64, praznih, neispravnih, sukoba *int) {
	if strings.TrimSpace(s) == "" || strings.TrimSpace(s) == "-" {
		*praznih++
		return
	}
	v, err := broj(s)
	if err != nil || !arhiva.MogucaVrijednost(velicina, v) {
		*neispravnih++
		return
	}
	if staro != nil && *staro != v {
		*sukoba++
	}
	*odrediste = &v
}

func datum(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, oblik := range []string{"02.01.2006", "2.1.2006", "02.01.2006.", "2.1.2006."} {
		if t, err := time.Parse(oblik, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("datum %q nije čitljiv", s)
}

func broj(s string) (float64, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	s = strings.ReplaceAll(s, ",", ".")
	return strconv.ParseFloat(s, 64)
}

func polje(red []string, i int) string {
	if i >= 0 && i < len(red) {
		return red[i]
	}
	return ""
}
