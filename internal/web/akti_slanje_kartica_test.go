package web

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// karticaSlanja provjerava karticu Slanje po crtežu 4a: broj „N od M”, popis
// sklopljen dok akt nije ni jednom slan, adresa na koju slanje nije prošlo
// na vrhu, nijedna kućica označena unaprijed i svi primatelji, i oni ispod
// „Još N primatelja”, u istom obrascu.
func karticaSlanja(t *testing.T, s string, slano bool) {
	t.Helper()
	i := strings.Index(s, `id="slanje"`)
	if i < 0 {
		t.Fatal("nema kartice Slanje")
	}
	k := s[i:]
	if !regexp.MustCompile(`<span class="akt-slanje-broj">\d+ od \d+</span>`).MatchString(k) {
		t.Error("kartica Slanje nema broj poslanih")
	}
	if slano != strings.Contains(k, `<details class="akt-slanje-popis" open>`) {
		t.Errorf("popis primatelja otvoren=%v, a akt je slan=%v", !slano, slano)
	}
	if !slano && !strings.Contains(k, "Primatelji još nisu označeni.") {
		t.Error("prije slanja nema rečenice da primatelji nisu označeni")
	}
	od, do := strings.Index(k, `id="akt-slanje-obrazac"`), strings.Index(k, "</form>")
	if od < 0 || do < od {
		t.Fatal("nema obrasca slanja")
	}
	obrazac := k[od:do]
	const kucica = `<input type="checkbox" name="adresa"`
	if n, sve := strings.Count(obrazac, kucica), strings.Count(k, kucica); n != sve || n == 0 {
		t.Errorf("u obrascu je %d od %d kućica primatelja", n, sve)
	}
	if regexp.MustCompile(kucica + `[^>]*checked`).MatchString(obrazac) {
		t.Error("primatelj je označen unaprijed")
	}
	if slano {
		prvi := strings.Index(obrazac, kucica)
		if neuspjeh := strings.Index(obrazac, "nije prošlo"); neuspjeh < 0 || strings.Index(obrazac[prvi:], "</tr>") < neuspjeh-prvi {
			t.Error("adresa na koju slanje nije prošlo nije na vrhu popisa")
		}
	}
}

// Akt koji je ovjeren, a još nije stupio na snagu (crtež 4d), ima traku i
// smije se poslati; poništen akt i nacrt ne
func TestJosNeVrijedi(t *testing.T) {
	sutra, jucer := time.Now().Add(24*time.Hour), time.Now().Add(-24*time.Hour)
	for _, x := range []struct {
		opis string
		akt  *models.Akt
		zeli bool
	}{
		{"bez akta", nil, false},
		{"nacrt", &models.Akt{Status: models.AktNacrt, Vrijedi: sutra}, false},
		{"ovjeren, stupa sutra", &models.Akt{Status: models.AktOvjeren, Vrijedi: sutra}, true},
		{"ovjeren, vrijedi od jučer", &models.Akt{Status: models.AktOvjeren, Vrijedi: jucer}, false},
		{"poništen", &models.Akt{Status: models.AktOvjeren, Vrijedi: sutra, Storno: &models.StornoAkta{}}, false},
	} {
		if got := (AktiPageData{Akt: x.akt}).JosNeVrijedi(); got != x.zeli {
			t.Errorf("%s: %v, želi %v", x.opis, got, x.zeli)
		}
	}
}

// Upozorenje u kartici Slanje stoji dok akt ima istu oznaku kao drugi akt i
// sukob nije riješen; isti nacrt na dva čvora ga ne daje (crtež 4c)
func TestUpozoriNaOznaku(t *testing.T) {
	drugi := []models.Akt{{ID: "drugi"}}
	for _, x := range []struct {
		opis  string
		sukob models.SukobOznake
		zeli  bool
	}{
		{"bez sukoba", models.SukobOznake{}, false},
		{"ista oznaka", models.SukobOznake{Drugi: drugi}, true},
		{"riješen", models.SukobOznake{Drugi: drugi, Rjesenje: &models.RjesenjeOznake{}}, false},
		{"isti nacrt na dva čvora", models.SukobOznake{Ovjere: drugi}, false},
	} {
		if got := (AktiPageData{Sukob: x.sukob}).UpozoriNaOznaku(); got != x.zeli {
			t.Errorf("%s: %v, želi %v", x.opis, got, x.zeli)
		}
	}
}
