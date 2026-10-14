package main

// Stvarni kišomjeri u satnom krugu: mjerenja svaki krug, a jednom dnevno
// ulaganje u arhivu i izdavanje paketa. Što ne uspije javi se, a ostatak
// kruga ide dalje.

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/kisomjeri"
)

// laznaArhiva gradi i izdaje letve kišomjera bez prave arhive: letva
// neGradi se ne da izgraditi, letva neIzdaje se ne da izdati, a svaka druga
// izda dva paketa
type laznaArhiva struct {
	neGradi, neIzdaje string
	izdane            []string
}

func (a *laznaArhiva) IzgradiLetvu(letva string, _ io.Writer) (arhiva.Izvjestaj, error) {
	if letva == a.neGradi {
		return arhiva.Izvjestaj{}, errors.New("u stablu nema datoteka te letve")
	}
	return arhiva.Izvjestaj{}, nil
}

func (a *laznaArhiva) IzdajArhivu(letva string, _ bool, _ io.Writer) (arhiva.IzvjestajIzdanja, error) {
	if letva == a.neIzdaje {
		return arhiva.IzvjestajIzdanja{}, errors.New("mapa paketa je zaključana")
	}
	a.izdane = append(a.izdane, letva)
	return arhiva.IzvjestajIzdanja{Promijenjenih: 2}, nil
}

// citacKisomjera daje jedno mjerenje za svaku postaju
type citacKisomjera struct{}

func (citacKisomjera) Preuzmi(_ context.Context, postaje []kisomjeri.Postaja) ([]kisomjeri.Mjerenje, error) {
	var m []kisomjeri.Mjerenje
	for _, p := range postaje {
		m = append(m, kisomjeri.Mjerenje{Kisomjer: p.Code, Kraj: time.Now().UTC().Truncate(time.Hour), Sati: 1,
			Oborina: 0.4, Izvor: "kisomjer-proba"})
	}
	return m, nil
}

// krugSKisomjerom je krug čvora koji izdaje, s jednim stvarnim kišomjerom,
// lažnom arhivom i stablom izvornih datoteka s jednim slivom i letvom
func krugSKisomjerom(t *testing.T) (*satniKrug, *laznaArhiva) {
	t.Helper()
	k, _ := okolinaKruga(t, true)
	ob, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ob.Close() })
	sp := &kisomjeri.Spremiste{DB: ob}
	if err := sp.Pripremi(); err != nil {
		t.Fatal(err)
	}
	k.kisomjeri = &kisomjeri.Uvoznik{Spremiste: sp,
		Postaje: func() ([]kisomjeri.Postaja, error) {
			return []kisomjeri.Postaja{{Code: "kisomjer-probni", Naziv: "Probni kišomjer", Izvor: "proba"}}, nil
		},
		Citaci: map[string]kisomjeri.Citac{"proba": citacKisomjera{}}}
	a := &laznaArhiva{}
	k.posluzitelj = a
	k.podaci = t.TempDir()
	if err := os.MkdirAll(filepath.Join(k.podaci, "sliv-proba", "letva-proba"), 0o755); err != nil {
		t.Fatal(err)
	}
	return k, a
}

// lazanUlog zamijeni ulaganje u stablo: vraća zadane letve i grešku, a
// broji pozive
func lazanUlog(t *testing.T, letve []string, greska error) *int {
	t.Helper()
	stari := uloziKisomjere
	t.Cleanup(func() { uloziKisomjere = stari })
	pozvano := 0
	uloziKisomjere = func(koren string, s *kisomjeri.Spremiste, postaje []kisomjeri.Postaja, od, sada time.Time) ([]string, error) {
		pozvano++
		if sada.Sub(od) != 96*time.Hour || len(postaje) != 1 || s == nil || koren == "" {
			t.Errorf("ulaganje: koren %q, %d postaja, od %v do %v", koren, len(postaje), od, sada)
		}
		return letve, greska
	}
	return &pozvano
}

// Krug uloži mjerenja jednom dnevno: letva koja se ne da izgraditi ili izdati
// javi se u dnevniku poslužitelja, ostale izađu i razmjena arhive se potakne.
// Redak kruga broji postaje i pakete s pravim oblikom riječi.
func TestKisomjeriSeUlazuJednomDnevno(t *testing.T) {
	k, a := krugSKisomjerom(t)
	k.razmjenaArh = &razmjenaArhive{potakni: make(chan struct{}, 1)}
	a.neGradi, a.neIzdaje = "kisomjer-a", "kisomjer-b"
	pozvano := lazanUlog(t, []string{"kisomjer-a", "kisomjer-b", "kisomjer-c"}, errors.New("jedan dan nije cijel"))
	dnevnik := dnevnikTesta(t)

	k.preuzmiKisomjere(context.Background())
	k.preuzmiKisomjere(context.Background())

	zelim := []string{"stvarni kišomjeri", "stvarni kišomjeri: 1 mjerenje",
		"stvarni kišomjeri: uloženo u arhivu za 3 postaje, izdana 2 paketa",
		"stvarni kišomjeri", "stvarni kišomjeri: 1 mjerenje"}
	if redci := redciS(k.uvoznik.Napredak().Redci, "stvarni kišomjeri"); !reflect.DeepEqual(redci, zelim) {
		t.Errorf("redci:\n%q\nželim\n%q", redci, zelim)
	}
	if *pozvano != 1 {
		t.Errorf("isti dan uloženo %d puta, želim jednom", *pozvano)
	}
	if !reflect.DeepEqual(a.izdane, []string{"kisomjer-c"}) {
		t.Errorf("izdane letve %q", a.izdane)
	}
	if len(k.razmjenaArh.potakni) != 1 {
		t.Error("razmjena arhive nije potaknuta, a paketi su izdani")
	}
	for _, z := range []string{"ulaganje: jedan dan nije cijel", "gradnja kisomjer-a: u stablu nema datoteka",
		"izdavanje kisomjer-b: mapa paketa je zaključana"} {
		if !strings.Contains(dnevnik.String(), z) {
			t.Errorf("dnevnik nema %q:\n%s", z, dnevnik)
		}
	}
	if danas := time.Now().In(kisomjeri.Zagreb).Format("2006-01-02"); k.kisUlozeno != danas {
		t.Errorf("dan ulaganja %q, želim %q", k.kisUlozeno, danas)
	}
}

// Popis postaja koji se ne da pročitati: mjerenja se ne preuzmu, ništa se ne
// uloži, a ulaganje se pokuša već idući krug, ne tek sutra. Paketi izdani
// bez razmjene arhive ne trebaju poticaj.
func TestKisomjeriBezPopisaPostaja(t *testing.T) {
	k, _ := krugSKisomjerom(t)
	postaje := k.kisomjeri.Postaje
	k.kisomjeri.Postaje = func() ([]kisomjeri.Postaja, error) { return nil, errors.New("registar ne odgovara") }
	pozvano := lazanUlog(t, []string{"kisomjer-a"}, nil)
	dnevnik := dnevnikTesta(t)

	k.preuzmiKisomjere(context.Background())
	if *pozvano != 0 || k.kisUlozeno != "" {
		t.Fatalf("bez popisa postaja: uloženo %d puta, dan %q", *pozvano, k.kisUlozeno)
	}
	zelim := []string{"stvarni kišomjeri", "stvarni kišomjeri: registar ne odgovara", "stvarni kišomjeri: 0 mjerenja"}
	if redci := redciS(k.uvoznik.Napredak().Redci, "stvarni kišomjeri"); !reflect.DeepEqual(redci, zelim) {
		t.Errorf("redci:\n%q\nželim\n%q", redci, zelim)
	}
	if !strings.Contains(dnevnik.String(), "stvarni kišomjeri: registar ne odgovara") {
		t.Errorf("dnevnik: %s", dnevnik)
	}

	k.kisomjeri.Postaje = postaje
	k.ulaziKisomjereJednomDnevno(time.Now())
	if *pozvano != 1 || k.kisUlozeno == "" {
		t.Errorf("idući krug: uloženo %d puta, dan %q", *pozvano, k.kisUlozeno)
	}
	if r := k.uvoznik.Napredak().Redci; r[len(r)-1] != "stvarni kišomjeri: uloženo u arhivu za 1 postaju, izdana 2 paketa" {
		t.Errorf("zadnji redak %q", r[len(r)-1])
	}
}

// Čvor bez stabla izvornih datoteka kišomjere ne ulaže i to kaže jednom na
// dan; čvor bez zadanog stabla ne kaže ništa.
func TestKisomjeriBezStabla(t *testing.T) {
	k, _ := krugSKisomjerom(t)
	k.podaci = t.TempDir()
	pozvano := lazanUlog(t, nil, nil)
	sada := time.Date(2026, 10, 14, 6, 30, 0, 0, time.UTC)

	k.ulaziKisomjereJednomDnevno(sada)
	k.ulaziKisomjereJednomDnevno(sada.Add(time.Hour))
	bez := redciS(k.uvoznik.Napredak().Redci, "stvarni kišomjeri: na ovom čvoru nema stabla")
	if len(bez) != 1 || k.kisUlozeno != "2026-10-14" || *pozvano != 0 {
		t.Errorf("bez stabla: redci %q, dan %q, uloženo %d puta", bez, k.kisUlozeno, *pozvano)
	}

	k.podaci, k.kisUlozeno = "", ""
	k.ulaziKisomjereJednomDnevno(sada)
	if k.kisUlozeno != "" || *pozvano != 0 {
		t.Errorf("bez zadanog stabla: dan %q, uloženo %d puta", k.kisUlozeno, *pozvano)
	}
}
