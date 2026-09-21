// Package ehyd čita izvoze austrijskog portala eHYD.
package ehyd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gocop/internal/arhiva"
	"golang.org/x/text/encoding/charmap"
)

// Izvjestaj nosi metapodatke i ishod čitanja jednog eHYD niza.
type Izvjestaj struct {
	Postaja, HZB, Jedinica        string
	Interval                      string // T=dnevni, M=mjesečni u eHYD izvozu
	Redaka, Praznina, Neispravnih int
}

type Rezultat struct {
	Redci     []arhiva.Redak
	Izvjestaj Izvjestaj
}

// Citaj čita jedan eHYD izvoz. Dnevni i mjesečni nizovi prepoznaju se iz
// metapodatka Exportzeitreihe; odluku kamo niz pripada donosi pozivatelj.
func Citaj(put, velicina string) (Rezultat, error) {
	var out Rezultat
	f, err := os.Open(put)
	if err != nil {
		return out, err
	}
	defer f.Close()

	skener := bufio.NewScanner(charmap.ISO8859_1.NewDecoder().Reader(f))
	uVrijednostima := false
	brojRetka := 0
	for skener.Scan() {
		brojRetka++
		linija := strings.TrimSpace(strings.TrimPrefix(skener.Text(), "\ufeff"))
		if !uVrijednostima {
			citajMetapodatak(linija, &out.Izvjestaj)
			if linija == "Werte:" {
				uVrijednostima = true
			}
			continue
		}
		if linija == "" {
			continue
		}
		dijelovi := strings.SplitN(linija, ";", 2)
		if len(dijelovi) != 2 {
			out.Izvjestaj.Neispravnih++
			continue
		}
		out.Izvjestaj.Redaka++
		vrijednost := strings.TrimSpace(dijelovi[1])
		if vrijednost == "" || strings.EqualFold(vrijednost, "Lücke") {
			out.Izvjestaj.Praznina++
			continue
		}
		kad, err := time.ParseInLocation("02.01.2006 15:04:05", strings.TrimSpace(dijelovi[0]), arhiva.Zagreb)
		if err != nil {
			return Rezultat{}, fmt.Errorf("%s redak %d: vrijeme %q nije čitljivo", put, brojRetka, dijelovi[0])
		}
		v, err := strconv.ParseFloat(strings.ReplaceAll(vrijednost, ",", "."), 64)
		if err != nil || !arhiva.MogucaVrijednost(velicina, v) {
			out.Izvjestaj.Neispravnih++
			continue
		}
		poDanu := out.Izvjestaj.Interval == "T"
		if poDanu {
			// Dnevni srednjak pripada kalendarskom danu, ne trenutku. Pretvorba
			// ponoći iz CET-a u UTC pomaknula bi 1. siječnja na 31. prosinca.
			kad = time.Date(kad.Year(), kad.Month(), kad.Day(), 0, 0, 0, 0, time.UTC)
		} else {
			kad = kad.UTC()
		}
		out.Redci = append(out.Redci, arhiva.Redak{Vrijeme: kad, PoDanu: poDanu, Vrijednost: v})
	}
	if err := skener.Err(); err != nil {
		return Rezultat{}, err
	}
	if !uVrijednostima {
		return Rezultat{}, fmt.Errorf("%s: nije pronađen odjeljak Werte", put)
	}
	if out.Izvjestaj.Interval == "" {
		return Rezultat{}, fmt.Errorf("%s: nije prepoznat interval eHYD niza", put)
	}
	if len(out.Redci) == 0 {
		return Rezultat{}, fmt.Errorf("%s: nema nijedne valjane vrijednosti", put)
	}
	return out, nil
}

func citajMetapodatak(linija string, iz *Izvjestaj) {
	kljuc, vrijednost, ima := strings.Cut(linija, ";")
	if !ima {
		return
	}
	vrijednost = strings.TrimSpace(vrijednost)
	kljuc = strings.TrimSuffix(strings.TrimSpace(kljuc), ":")
	switch kljuc {
	case "Messstelle":
		iz.Postaja = vrijednost
	case "HZB-Nummer":
		iz.HZB = vrijednost
	case "Einheit":
		iz.Jedinica = vrijednost
	case "Exportzeitreihe":
		v := strings.Trim(vrijednost, "()")
		d := strings.Split(v, ",")
		if len(d) > 4 {
			iz.Interval = strings.TrimSpace(d[4])
		}
	}
}
