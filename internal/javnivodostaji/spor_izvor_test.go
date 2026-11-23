package javnivodostaji

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// izvorKojiIstice broji pozive i vraća istek, osim za adrese u radi
type izvorKojiIstice struct {
	mu     sync.Mutex
	pozvan int
	radi   map[string]bool
	greska error
}

func (i *izvorKojiIstice) Naziv() string                 { return "spori.example" }
func (i *izvorKojiIstice) Prepoznaje(adresa string) bool { return strings.HasPrefix(adresa, "spori://") }
func (i *izvorKojiIstice) Ocitanja(_ context.Context, adresa string) ([]Redak, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.pozvan++
	if i.radi[adresa] {
		return []Redak{{Kad: time.Now().Add(-time.Hour).Truncate(time.Hour), LevelCm: intPtr(100)}}, nil
	}
	return nil, i.greska
}

func letveSporogIzvora(n int) []models.Station {
	var out []models.Station
	for k := 1; k <= n; k++ {
		out = append(out, models.Station{ID: uuid.New(), Name: fmt.Sprintf("Spora %d", k), Code: fmt.Sprintf("spora-%d", k),
			JavniUvoz: true, JavniURL: fmt.Sprintf("spori://%d", k)})
	}
	return out
}

func uvoznikSporogIzvora(iz *izvorKojiIstice, letve []models.Station, zapisnik *[]string) *Uvoznik {
	u := NoviUvoznik(&probnoSpremiste{letve: letve, postojeca: map[int64]bool{}}, func(f string, a ...any) {
		*zapisnik = append(*zapisnik, fmt.Sprintf(f, a...))
	})
	u.Izvori = append([]Izvor{iz}, u.Izvori...)
	podmetni(u, stranice(nil))
	return u
}

// Izvor kojem dvije letve zaredom isteknu ostatak kruga se preskače, a
// sljedeći krug ga opet pita.
func TestSporIzvorSePreskaceDoKrajaKruga(t *testing.T) {
	iz := &izvorKojiIstice{greska: context.DeadlineExceeded}
	var zapisnik []string
	letve := letveSporogIzvora(5)
	u := uvoznikSporogIzvora(iz, letve, &zapisnik)

	u.PreuzmiSve(context.Background())
	if iz.pozvan != 2*SporIzvorNakon { // svaka letva dvaput (ponovljeni upit), pa ništa
		t.Errorf("izvor pozvan %d puta, želim %d", iz.pozvan, 2*SporIzvorNakon)
	}
	for k, st := range letve {
		s, _ := u.Stanje(st.ID.String())
		preskocena := s.Greska == errSporIzvor.Error()
		if preskocena != (k >= SporIzvorNakon) {
			t.Errorf("letva %d: %q", k+1, s.Greska)
		}
	}
	javljeno := 0
	for _, z := range zapisnik {
		if strings.Contains(z, "spori.example ne odgovara (2 letve zaredom)") {
			javljeno++
		}
	}
	if javljeno != 1 {
		t.Errorf("dnevnik je spori izvor javio %d puta:\n%s", javljeno, strings.Join(zapisnik, "\n"))
	}

	u.PreuzmiSve(context.Background())
	if iz.pozvan != 4*SporIzvorNakon {
		t.Errorf("drugi krug: izvor pozvan ukupno %d puta, želim %d", iz.pozvan, 4*SporIzvorNakon)
	}
}

// Letva koja odgovori vraća brojanje na nulu, a greška koja nije istek ne
// broji se: tako izvor s jednom pokvarenom letvom ne ispada iz kruga.
func TestSporIzvorBrojiSamoIstekeZaredom(t *testing.T) {
	iz := &izvorKojiIstice{greska: context.DeadlineExceeded, radi: map[string]bool{"spori://2": true, "spori://4": true}}
	var zapisnik []string
	u := uvoznikSporogIzvora(iz, letveSporogIzvora(5), &zapisnik)
	u.PreuzmiSve(context.Background())
	if want := 3*2 + 2; iz.pozvan != want { // 1, 3 i 5 isteknu dvaput, 2 i 4 odgovore
		t.Errorf("izvor pozvan %d puta, želim %d", iz.pozvan, want)
	}

	drugi := &izvorKojiIstice{greska: errors.New("stranica je promijenila oblik")}
	u = uvoznikSporogIzvora(drugi, letveSporogIzvora(4), &zapisnik)
	u.PreuzmiSve(context.Background())
	if drugi.pozvan != 4 {
		t.Errorf("greška koja nije istek: izvor pozvan %d puta, želim 4", drugi.pozvan)
	}
}

// Uvoznik bez kruga (pojedinačno preuzimanje letve) broji isto, bez
// pripremljene tablice.
func TestSporIzvorIzvanKruga(t *testing.T) {
	iz := &izvorKojiIstice{greska: context.DeadlineExceeded}
	var zapisnik []string
	letve := letveSporogIzvora(3)
	u := uvoznikSporogIzvora(iz, letve, &zapisnik)
	for k := range letve {
		u.Preuzmi(context.Background(), &letve[k])
	}
	if iz.pozvan != 2*SporIzvorNakon {
		t.Errorf("izvor pozvan %d puta, želim %d", iz.pozvan, 2*SporIzvorNakon)
	}
}
