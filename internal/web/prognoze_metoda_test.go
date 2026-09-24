package web

import (
	"bytes"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/xlsxw"
)

// Opis mora nositi brojke iz računa, a ne prepisane: promijeni li se
// poluvrijeme ispravka ili broj analogija, opis se mijenja s njima.
func TestOpisMetodeCitaBrojkeIzRacuna(t *testing.T) {
	var sve strings.Builder
	for _, o := range OpisMetode(68, "COP Osijek") {
		sve.WriteString(o.Naslov + "\n")
		for _, od := range o.Odlomci {
			sve.WriteString(od.Tekst + "\n")
		}
	}
	tekst := sve.String()
	for _, want := range []string{
		"Prognoza COP Osijek",
		"2^(−τ / " + brojHRf(prognoza.PoluvijekIspravka, 0) + ")",
		"K = " + tekstBroja(prognoza.DnevnihAnalogija),
		"[0, " + tekstBroja(prognoza.NajveciPomak) + "] h",
		"68. percentil", "u 32 % stvarna vrijednost", "70 %",
		"linearni spline", "najbližih susjeda", "Q–H",
	} {
		if !strings.Contains(tekst, want) {
			t.Errorf("opis metode nema %q", want)
		}
	}
}

// Tablica postaja: ulazi lanca s kašnjenjem po pojasima i prozorom, raspon iz
// izmjerenih promašaja, ulazi dnevnog modela imenima postaja.
func TestLetveMetodeOpisujuUlaze(t *testing.T) {
	postaje := map[string]models.Station{
		"gornja": {Name: "Gornja"}, "pritok": {Name: "Pritok"},
	}
	pojasi := map[string][]prognoza.Pojas{"donja": {
		{Letva: "donja", Velicina: "vodostaj", R: 0.981, Ulazi: []prognoza.Ulaz{
			{Letva: "gornja", Velicina: "protok", PomakH: 12, Sirina: 5},
			{Letva: "pritok", Velicina: "vodostaj", PomakH: 6, Sirina: 1}}},
		{Letva: "donja", Velicina: "vodostaj", R: 0.993, Ulazi: []prognoza.Ulaz{
			{Letva: "gornja", Velicina: "protok", PomakH: 20, Sirina: 5},
			{Letva: "pritok", Velicina: "vodostaj", PomakH: 6, Sirina: 1}}},
	}}
	promasaji := map[string]map[int]prognoza.Promasaj{"donja": {
		24: {Rasap: 6.4}, 48: {Rasap: 11.2}}}
	tablice := []TablicaPrognoza{{Naslov: "Rijeka", Letve: []LetvaPrognoze{
		{Kod: "gornja", Naziv: "Gornja"}, {Kod: "donja", Naziv: "Donja", Voda: "Rijeka"}}}}

	l := letveMetode(tablice, postaje, pojasi, promasaji)
	if len(l) != 1 {
		t.Fatalf("u tablici %d postaja, a računa se samo jedna", len(l))
	}
	d := l[0]
	if len(d.Satni) != 2 || d.Satni[0] != "Gornja · protok · 12–20 h · prozor 5 h" || d.Satni[1] != "Pritok · vodostaj · 6 h" {
		t.Errorf("ulazi lanca: %q", d.Satni)
	}
	if d.Raspon != "±6 / ±11 / — cm" {
		t.Errorf("raspon: %q", d.Raspon)
	}
	if d.Slaganje != "0,981–0,993" {
		t.Errorf("R: %q", d.Slaganje)
	}
}

func TestStranicaOPrognozi(t *testing.T) {
	html := iscrtaj(t, "prognoze_metoda.html", PrognozeMetodaData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		ActiveNav:   "prognoze", Izdaje: "COP Osijek", Izdano: "24.9.2026. u 07:00",
		Odjeljci: OpisMetode(68, "COP Osijek"), RasponDo: DoseziRaspona,
		Letve: []LetvaMetode{{Naziv: "Donja", Voda: "Rijeka", Racuna: "vodostaj",
			Satni: []string{"Gornja · protok · 12–20 h · prozor 5 h"}, Raspon: "±6 / ±11 / ±17 cm",
			Dnevni: []string{"Gornja", "Pritok"}, DnevniOd: "3. dana"}},
	})
	for _, want := range []string{"O prognozi", "Satni lanac — građa", "prog-formula",
		"Gornja · protok · 12–20 h · prozor 5 h", "±6 / ±11 / ±17 cm", "Gornja, Pritok", "3. dana",
		"/prognoze.xlsx"} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica O prognozi nema %q", want)
		}
	}
}

// List „O prognozi” u izvozu: opis i tablica postaja, a knjiga se da zapisati.
func TestListMetodeUIzvozu(t *testing.T) {
	k := &xlsxw.Knjiga{}
	listMetode(k, ZaglavljeIzvoza{Organizacija: "Org", Centar: "COP Osijek"}, PrognozeMetodaData{
		Odjeljci: OpisMetode(68, "COP Osijek"), RasponDo: DoseziRaspona,
		Letve: []LetvaMetode{{Naziv: "Donja", Satni: []string{"a", "b"}, Dnevni: []string{"c"}}},
	})
	if len(k.Listovi) != 1 || k.Listovi[0].Naziv != "O prognozi" {
		t.Fatalf("listovi: %+v", k.Listovi)
	}
	var sve []string
	for _, red := range k.Listovi[0].Redci {
		for _, c := range red {
			sve = append(sve, c.Tekst)
		}
	}
	tekst := strings.Join(sve, "\n")
	for _, want := range []string{"O PROGNOZI", "Dnevni model — procjena", "Satni lanac — ulazi", "a\nb"} {
		if !strings.Contains(tekst, want) {
			t.Errorf("list nema %q", want)
		}
	}
	var buf bytes.Buffer
	if err := k.Zapisi(&buf); err != nil {
		t.Fatal(err)
	}
}
