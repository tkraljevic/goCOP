package web

import (
	"gocop/internal/models"
	"html"
	"regexp"
	"testing"
)

// vrijednostPolja čita vrijednost polja iz iscrtanog obrasca, kakvu bi
// preglednik poslao da je nitko ne dira.
func vrijednostPolja(t *testing.T, stranica, id string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)id="` + id + `"[^>]*?value="([^"]*)"`).FindStringSubmatch(stranica)
	if m == nil {
		t.Fatalf("obrazac nema polje %s", id)
	}
	return html.UnescapeString(m[1])
}

func decimalni(v float64) *float64 { return &v }

// Letva se sprema cijelim obrascem, pa i kad se mijenja samo adresa. Ono što
// nitko nije dirao mora ostati kakvo je bilo, i kad obrazac prikazuje kraće.
func TestObrazacLetveNeMijenjaNedirnuto(t *testing.T) {
	stara := models.Station{
		Name:      "Vodograd",
		Record:    models.Threshold{Cm: cm(320), Raw: "+320 (23.06.2010.) — Plan obrane od poplava"},
		Prep:      models.Threshold{Cm: cm(300), Raw: "+300"},
		Regular:   models.Threshold{Raw: "uputa iz pravilnika"},
		Latitude:  decimalni(45.6158333),
		Longitude: decimalni(18.9041667),
		ZeroDatum: decimalni(80.12345),
	}
	stranica := iscrtaj(t, "station_form.html", StationPageData{
		CurrentUser: &models.User{FullName: "Test"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     stara,
	})
	f := stationForm{
		Obrazac:   obrazacKartica,
		Name:      "Vodograd",
		Record:    vrijednostPolja(t, stranica, "record"),
		Prep:      vrijednostPolja(t, stranica, "prep"),
		Regular:   vrijednostPolja(t, stranica, "regular"),
		Latitude:  vrijednostPolja(t, stranica, "latitude"),
		Longitude: vrijednostPolja(t, stranica, "longitude"),
		ZeroDatum: vrijednostPolja(t, stranica, "zero_datum"),
	}
	nova := stara
	f.primijeni(&nova)

	if nova.Record.Raw != stara.Record.Raw || nova.Record.Cm == nil || *nova.Record.Cm != 320 {
		t.Errorf("rekord: %+v", nova.Record)
	}
	if nova.Prep.Raw != "+300" || nova.Prep.Cm == nil || *nova.Prep.Cm != 300 {
		t.Errorf("pripremno stanje: %+v", nova.Prep)
	}
	if nova.Regular.Raw != "uputa iz pravilnika" || nova.Regular.Cm != nil {
		t.Errorf("redovne mjere: %+v", nova.Regular)
	}
	if *nova.Latitude != 45.6158333 || *nova.Longitude != 18.9041667 {
		t.Errorf("koordinate: %v, %v", *nova.Latitude, *nova.Longitude)
	}
	if *nova.ZeroDatum != 80.12345 {
		t.Errorf("kota nule: %v", *nova.ZeroDatum)
	}
}

func TestZadrziPrag(t *testing.T) {
	stari := models.Threshold{Cm: cm(320), Raw: "+320 (23.06.2010.) — Plan"}
	slucajevi := []struct {
		naziv, unos string
		cm          *int
		raw         string
	}{
		{"isti broj sa znakom", "+320", cm(320), stari.Raw},
		{"isti broj bez znaka", "320", cm(320), stari.Raw},
		{"novi broj", "+330", cm(330), "+330"},
		{"obrisano", "", nil, ""},
		{"vraćen opis", "+320 (1.1.2000.)", cm(320), "+320 (1.1.2000.)"},
		{"opis bez znaka", "320 cm, 1.1.2000.", cm(320), "320 cm, 1.1.2000."},
		{"drugi broj koji počinje istim znamenkama", "3205 (1.1.2000.)", nil, "3205 (1.1.2000.)"},
		{"sam tekst", "uputa iz pravilnika", nil, "uputa iz pravilnika"},
	}
	for _, s := range slucajevi {
		t.Run(s.naziv, func(t *testing.T) {
			got := zadrziPrag(stari, s.unos)
			if got.Raw != s.raw {
				t.Errorf("tekst: %q, očekivano %q", got.Raw, s.raw)
			}
			if (got.Cm == nil) != (s.cm == nil) || (got.Cm != nil && *got.Cm != *s.cm) {
				t.Errorf("broj: %v, očekivano %v", got.Cm, s.cm)
			}
		})
	}
}

func TestZadrziBroj(t *testing.T) {
	stari := decimalni(45.6158333)
	if got := zadrziBroj(stari, "45,615833", 6); got == nil || *got != 45.6158333 {
		t.Errorf("nedirnuto: %v", got)
	}
	if got := zadrziBroj(stari, "45,7", 6); got == nil || *got != 45.7 {
		t.Errorf("promijenjeno: %v", got)
	}
	if got := zadrziBroj(stari, "", 6); got != nil {
		t.Errorf("obrisano: %v", *got)
	}
	if got := zadrziBroj(nil, "45,7", 6); got == nil || *got != 45.7 {
		t.Errorf("novo: %v", got)
	}
}
