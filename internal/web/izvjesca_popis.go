package web

import (
	"cmp"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"time"

	"gocop/internal/models"
)

// RedakIzvjesca je jedan redak zajedničkog popisa dnevnih izvješća: izvješće
// dionice ili izvješće sektora, nikad oboje.
type RedakIzvjesca struct {
	Dan       time.Time
	Dionice   *models.DnevnoIzvjesce
	Sektorsko *models.SektorskoIzvjesce
}

// KartaSektora je izvješće sektora za jedan dan: postoji ili ne, i smije li
// ga osoba sastaviti.
type KartaSektora struct {
	Sektor    string
	Izvjesce  *models.SektorskoIzvjesce
	Sastavlja bool
}

// Vrste izvješća na popisu (?vrsta=).
const (
	vrstaIzvjescaDionice = "dionice"
	vrstaIzvjescaSektor  = "sektor"
)

// slozenRedoslijed spaja izvješća dionica i sektora u jedan popis: noviji dan
// prvi, u danu izvješće sektora prije dionica, a dionice redom kojim ih daje
// repozitorij.
func slozenRedoslijed(dionice []models.DnevnoIzvjesce, sektorska []models.SektorskoIzvjesce) []RedakIzvjesca {
	out := make([]RedakIzvjesca, 0, len(dionice)+len(sektorska))
	for i := range sektorska {
		out = append(out, RedakIzvjesca{Dan: sektorska[i].Dan, Sektorsko: &sektorska[i]})
	}
	for i := range dionice {
		out = append(out, RedakIzvjesca{Dan: dionice[i].Dan, Dionice: &dionice[i]})
	}
	slices.SortStableFunc(out, func(a, b RedakIzvjesca) int {
		if c := cmp.Compare(danKljuc(b.Dan), danKljuc(a.Dan)); c != 0 {
			return c
		}
		return cmp.Compare(prvoSektor(a), prvoSektor(b))
	})
	return out
}

func danKljuc(t time.Time) string { return t.In(models.Zagreb).Format("2006-01-02") }

func prvoSektor(r RedakIzvjesca) int {
	if r.Sektorsko != nil {
		return 0
	}
	return 1
}

// cipoviVrsteIzvjesca su Sva, Dionice i Sektor s brojem redaka; mijenjaju
// samo parametar vrsta, ostali izbor ostaje.
func cipoviVrsteIzvjesca(osnova url.Values, izabrana string, dionica, sektor int) []CipPopisa {
	out := make([]CipPopisa, 0, 3)
	for _, v := range []struct {
		naziv, vrsta string
		broj         int
	}{{"Sva", "", dionica + sektor}, {"Dionice", vrstaIzvjescaDionice, dionica}, {"Sektor", vrstaIzvjescaSektor, sektor}} {
		izbor := maps.Clone(osnova)
		izbor.Del("vrsta")
		if v.vrsta != "" {
			izbor.Set("vrsta", v.vrsta)
		}
		out = append(out, CipPopisa{Naziv: v.naziv + " (" + strconv.Itoa(v.broj) + ")", URL: adresaSIzborom("/izvjesca", izbor), Odabran: v.vrsta == izabrana})
	}
	return out
}

// karteSektora su izvješća sektora za jedan dan: za izabrani sektor, inače
// za svaki sektor koji osoba slaže ili za koji izvješće toga dana postoji.
func karteSektora(dan string, izabran string, slaze []string, sektorska []models.SektorskoIzvjesce) []KartaSektora {
	var out []KartaSektora
	dodaj := func(s string) *KartaSektora {
		for i := range out {
			if out[i].Sektor == s {
				return &out[i]
			}
		}
		out = append(out, KartaSektora{Sektor: s})
		return &out[len(out)-1]
	}
	for _, s := range slaze {
		if izabran == "" || s == izabran {
			dodaj(s).Sastavlja = true
		}
	}
	for i := range sektorska {
		si := &sektorska[i]
		if danKljuc(si.Dan) != dan || (izabran != "" && si.Sektor != izabran) {
			continue
		}
		dodaj(si.Sektor).Izvjesce = si
	}
	slices.SortFunc(out, func(a, b KartaSektora) int { return cmp.Compare(a.Sektor, b.Sektor) })
	return out
}
