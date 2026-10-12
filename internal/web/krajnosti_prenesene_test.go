package web

import (
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/repository"
)

func kota(v float64) *float64 { return &v }

// Bačka Palanka nema vlastiti dugi niz u arhivi, pa najviši i najniži vodostaj
// preuzima s Iloka, letve nasuprot s istom kotom nule. Letva s više izvora
// prognoze (Apatin) i naša letva krajnosti ne preuzimaju.
func TestIzvorKrajnosti(t *testing.T) {
	for kod, hoce := range map[string]string{"backa-palanka": "ilok", "apatin": "", "osijek": ""} {
		if got := izvorKrajnosti(kod); got != hoce {
			t.Errorf("izvor krajnosti za %s: %q, očekivano %q", kod, got, hoce)
		}
	}
	if got := izvoriKrajnosti([]string{"osijek", "backa-palanka", "ilok"}); len(got) != 1 || got[0] != "ilok" {
		t.Errorf("izvori krajnosti: %v", got)
	}
}

// Krajnost se prenosi razlikom kota nule, a bez kote nule ne prenosi se.
func TestPrenesiKrajnosti(t *testing.T) {
	ilok := models.Station{Name: "Ilok", ZeroDatum: kota(75.00)}
	palanka := models.Station{Name: "Bačka Palanka (Srbija)", ZeroDatum: kota(74.50)}
	mn, mx := prenesiKrajnosti(&Krajnost{Cm: -150, Godina: 2022}, &Krajnost{Cm: 812, Godina: 1965, Najvisi: true}, ilok, palanka)
	if mn == nil || mx == nil || mn.Cm != -100 || mx.Cm != 862 || mn.Prema != "Ilok" || mx.Godina != 1965 {
		t.Fatalf("prenesene krajnosti: %+v %+v", mn, mx)
	}
	if mn, mx := prenesiKrajnosti(&Krajnost{Cm: -150}, nil, ilok, models.Station{Name: "Bez kote"}); mn != nil || mx != nil {
		t.Errorf("bez kote nule preneseno je %+v %+v", mn, mx)
	}
	if got := mx.Label(); got != "+862 cm · s letve Ilok" {
		t.Errorf("oznaka: %q", got)
	}
	if got := mn.Naslov(); !strings.Contains(got, "najniži izmjereni vodostaj na letvi Ilok (2022.)") {
		t.Errorf("opis: %q", got)
	}
}

// Letva bez vlastitih godina dobiva krajnosti izvora; letva sa svojima ih
// zadržava. Preračunata godina izvora ne nosi rekord ni na prenesenoj letvi.
func TestKrajnostiLetveSIzvora(t *testing.T) {
	popis := map[string]models.Station{
		"ilok":          {Name: "Ilok", ZeroDatum: kota(73.968)},
		"backa-palanka": {Name: "Bačka Palanka (Srbija)", ZeroDatum: kota(73.97)},
	}
	godine := map[string][]repository.GodinaVodostaja{
		"ilok": {
			{Godina: 1965, Min: 120, Max: 790},
			{Godina: 1950, Min: -200, Max: 900, Preracunata: true},
			{Godina: 2022, Min: -112, Max: 300},
		},
	}
	mn, mx := krajnostiLetve(godine, "backa-palanka", popis)
	if mn == nil || mx == nil || mn.Cm != -112 || mx.Cm != 790 || mn.Godina != 2022 || mx.Prema != "Ilok" {
		t.Fatalf("krajnosti Bačke Palanke s Iloka: %+v %+v", mn, mx)
	}
	godine["backa-palanka"] = []repository.GodinaVodostaja{{Godina: 2026, Min: -106, Max: -27}}
	if mn, mx := krajnostiLetve(godine, "backa-palanka", popis); mn.Cm != -106 || mx.Cm != -27 || mn.Prema != "" {
		t.Errorf("letva s vlastitim nizom: %+v %+v", mn, mx)
	}
	if mn, mx := krajnostiLetve(godine, "ilok", popis); mn.Cm != -112 || mx.Cm != 790 || mx.Prema != "" {
		t.Errorf("Ilok: %+v %+v", mn, mx)
	}
}
