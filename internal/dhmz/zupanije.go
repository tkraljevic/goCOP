package dhmz

import (
	"math"
	"strings"
)

// Zupanija je županija s približnim središtem, imenom kakvo nose upozorenja
// i regijom dnevne prognoze.
type Zupanija struct {
	Oznaka, Naziv string
	Lat, Lon      float64 // približno središte područja, ne sjedište
	Regija        string  // ključ u regije_danas.xml
}

// Zupanije su sve 21. Točka se veže uz najbliže središte: to je približno i
// uz granicu zna promašiti, pa stranica uz upozorenja svoje županije uvijek
// pokazuje i ostala.
var Zupanije = []Zupanija{
	{"GZ", "Grad Zagreb", 45.81, 15.98, "sredisnja"},
	{"ZG", "Zagrebačka", 45.72, 16.12, "sredisnja"},
	{"KZ", "Krapinsko-zagorska", 46.08, 15.88, "sredisnja"},
	{"SM", "Sisačko-moslavačka", 45.32, 16.45, "sredisnja"},
	{"KA", "Karlovačka", 45.30, 15.45, "sredisnja"},
	{"VZ", "Varaždinska", 46.27, 16.20, "sredisnja"},
	{"KK", "Koprivničko-križevačka", 46.10, 16.85, "sredisnja"},
	{"BB", "Bjelovarsko-bilogorska", 45.78, 17.00, "sredisnja"},
	{"PG", "Primorsko-goranska", 45.30, 14.60, "sjjadran"},
	{"LS", "Ličko-senjska", 44.70, 15.30, "gorska"},
	{"VP", "Virovitičko-podravska", 45.75, 17.62, "istocna"},
	{"PS", "Požeško-slavonska", 45.37, 17.62, "istocna"},
	{"BP", "Brodsko-posavska", 45.18, 17.80, "istocna"},
	{"ZD", "Zadarska", 44.15, 15.60, "dalmacija"},
	{"OB", "Osječko-baranjska", 45.62, 18.45, "istocna"},
	{"SK", "Šibensko-kninska", 43.85, 16.00, "dalmacija"},
	{"VS", "Vukovarsko-srijemska", 45.20, 18.92, "istocna"},
	{"SD", "Splitsko-dalmatinska", 43.55, 16.70, "dalmacija"},
	{"IS", "Istarska", 45.20, 13.90, "istra"},
	{"DN", "Dubrovačko-neretvanska", 42.85, 17.80, "dalmacija"},
	{"ME", "Međimurska", 46.40, 16.55, "sredisnja"},
}

// ZupanijaZa vraća županiju najbližeg središta.
func ZupanijaZa(lat, lon float64) Zupanija {
	naj, d := Zupanije[0], math.Inf(1)
	for _, z := range Zupanije {
		if x := Udaljenost(lat, lon, z.Lat, z.Lon); x < d {
			naj, d = z, x
		}
	}
	return naj
}

// Vrijedi javlja odnosi li se upozorenje na županiju (upozorenja nose ime
// bez riječi "županija", a Zagreb kao "Grad Zagreb").
func (z Zupanija) Vrijedi(u Upozorenje) bool {
	p := strings.ToLower(strings.TrimSpace(u.Podrucje))
	n := strings.ToLower(z.Naziv)
	return p == n || strings.HasPrefix(p, n) || strings.HasPrefix(n, p) && p != ""
}

// NazivRegije je regija dnevne prognoze riječima
var NazivRegije = map[string]string{
	"istocna": "istočna Hrvatska", "sredisnja": "središnja Hrvatska", "gorska": "gorska Hrvatska",
	"sjjadran": "sjeverni Jadran", "istra": "Istra", "dalmacija": "Dalmacija",
}

// RijekeRegije su rijeke hidrološkog biltena po važnosti za regiju
var RijekeRegije = map[string][]string{
	"istocna":   {"drava", "dunav", "sava", "mura", "kupa"},
	"sredisnja": {"sava", "drava", "mura", "kupa", "dunav"},
	"gorska":    {"kupa", "sava", "drava", "mura", "dunav"},
}

// NazivRijeke je rijeka iz biltena s velikim slovom
var NazivRijeke = map[string]string{"sava": "Sava", "kupa": "Kupa", "dunav": "Dunav", "mura": "Mura", "drava": "Drava"}
