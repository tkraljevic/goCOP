package web

import (
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strings"

	"gocop/internal/hydro"
)

// Ušća na uzdužnom profilu: gdje pritoka ulazi u tok. Na profilu glavnog toka
// ušće stoji kao oznaka s vodostajem zadnje letve pritoke (Dunav: „ušće Drave
// · Osijek −143 cm”), a na profilu pritoke kao oznaka na njezinu kraju s
// vodostajem najbliže letve glavnog toka („ušće u Dunav · Aljmaš −45 cm”).
// Tako se dva crteža vežu na mjestu gdje se vode doista sastaju.
//
// Kilometar ušća računa se iz geometrije: kraj linije pritoke koji leži bliže
// glavnom toku projicira se na njegovu liniju, a kilometar se očita
// interpolacijom između oznaka rkm koje geometrija nosi. Gdje pritoka nema
// geometriju, uzima se kilometar iz opisa ušća u registru („ušće u Dravu kod
// Petrijevaca, rkm 22+400”).

// UsceUlaz je ušće kako ga crtež prima: na kojem kilometru ovog toka stoji i
// što uz njega piše.
type UsceUlaz struct {
	Naziv  string  // npr. „ušće Drave” ili „ušće u Dunav”
	Rkm    float64 // kilometar na toku koji se crta
	Tekst  string  // vodostaj s druge strane ušća, npr. „Osijek −143 cm”; prazno kad ga nema
	Vezano bool    // s druge strane ušća ima letvi na pregledu, pa oznaka nekamo vodi
}

// UsceProfila je ušće na crtežu.
type UsceProfila struct {
	Naziv, Tekst string
	X            float64
	Sidro        string
	Dolje        bool // natpis u drugom redu, da se susjedna ušća ne preklapaju
}

// KolikoIzvanZaUsce kaže koliko se crtež smije produžiti izvan krajnjih letvi
// da bi ušće ušlo u sliku, u udjelu duljine toka među letvama. Drava završava
// 19 km ispod Osijeka na 208 km crteža — to stane; ušće Mure 10 km iznad
// Botova također. Dalje od toga ušće ne pripada ovom crtežu.
const KolikoIzvanZaUsce = 0.15

// geoTok je ono što iz GeoJSON-a vodotoka treba: linija toka i oznake rkm.
type geoTok struct {
	linija [][2]float64
	oznake []geoOznaka
}

type geoOznaka struct {
	rkm float64
	xy  [2]float64
}

// citajGeoTok čita zbirku značajki vodotoka: prvu liniju kao tok, točke s
// „rkm” kao oznake kilometara.
func citajGeoTok(b []byte) (geoTok, bool) {
	var fc struct {
		Features []struct {
			Properties struct {
				Tip string          `json:"tip"`
				Rkm json.RawMessage `json:"rkm"`
			} `json:"properties"`
			Geometry struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(b, &fc); err != nil {
		return geoTok{}, false
	}
	var t geoTok
	for _, f := range fc.Features {
		switch f.Geometry.Type {
		case "LineString":
			if len(t.linija) > 0 {
				continue
			}
			var c [][2]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &c); err == nil && len(c) >= 2 {
				t.linija = c
			}
		case "Point":
			var xy [2]float64
			var rkm float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &xy); err != nil {
				continue
			}
			if len(f.Properties.Rkm) == 0 || json.Unmarshal(f.Properties.Rkm, &rkm) != nil {
				continue
			}
			t.oznake = append(t.oznake, geoOznaka{rkm: rkm, xy: xy})
		}
	}
	return t, len(t.linija) >= 2
}

// ravnina preslikava zemljopisne koordinate u približne metre oko zadane
// širine, da se udaljenosti i projekcije računaju ravno. Na 46° stupanj
// dužine je kraći od stupnja širine za trećinu.
func ravnina(sirina float64) func([2]float64) [2]float64 {
	k := math.Cos(sirina * math.Pi / 180)
	return func(p [2]float64) [2]float64 { return [2]float64{p[0] * k * 111320, p[1] * 111320} }
}

// projekcija vraća položaj točke uzduž linije (duljina od početka) i njezinu
// udaljenost od linije, oboje u jedinicama ravnine.
func projekcija(linija [][2]float64, p [2]float64) (uzduz, udaljenost float64) {
	najbolje := math.Inf(1)
	var duljina, mjesto float64
	for i := 0; i+1 < len(linija); i++ {
		a, b := linija[i], linija[i+1]
		dx, dy := b[0]-a[0], b[1]-a[1]
		seg := math.Hypot(dx, dy)
		t := 0.0
		if seg > 0 {
			t = ((p[0]-a[0])*dx + (p[1]-a[1])*dy) / (seg * seg)
			t = math.Max(0, math.Min(1, t))
		}
		q := [2]float64{a[0] + t*dx, a[1] + t*dy}
		d := math.Hypot(p[0]-q[0], p[1]-q[1])
		if d < najbolje {
			najbolje, mjesto = d, duljina+t*seg
		}
		duljina += seg
	}
	return mjesto, najbolje
}

// usceNaToku računa kilometar glavnog toka na kojem pritoka u njega ulazi.
// Kraj pritoke je onaj njezin kraj koji je bliže glavnom toku; kilometar se
// interpolira između dviju oznaka rkm između kojih projekcija padne. Bez
// barem dviju oznaka ili s krajem pritoke dalje od dva kilometra od toka
// ušća nema — takva geometrija ne kaže gdje se vode sastaju.
func usceNaToku(pritoka, glavni geoTok) (float64, bool) {
	if len(glavni.oznake) < 2 || len(pritoka.linija) == 0 {
		return 0, false
	}
	u := ravnina(glavni.linija[0][1])
	linija := make([][2]float64, len(glavni.linija))
	for i, p := range glavni.linija {
		linija[i] = u(p)
	}
	kraj, najD := 0.0, math.Inf(1)
	for _, k := range [][2]float64{pritoka.linija[0], pritoka.linija[len(pritoka.linija)-1]} {
		s, d := projekcija(linija, u(k))
		if d < najD {
			kraj, najD = s, d
		}
	}
	if najD > 2000 {
		return 0, false
	}
	type oznaka struct{ s, rkm float64 }
	var o []oznaka
	for _, z := range glavni.oznake {
		s, d := projekcija(linija, u(z.xy))
		if d > 2000 {
			continue
		}
		o = append(o, oznaka{s, z.rkm})
	}
	if len(o) < 2 {
		return 0, false
	}
	sort.Slice(o, func(i, j int) bool { return o[i].s < o[j].s })
	i := sort.Search(len(o), func(i int) bool { return o[i].s >= kraj })
	switch {
	case i == 0:
		i = 1
	case i == len(o):
		i = len(o) - 1
	}
	a, b := o[i-1], o[i]
	if b.s == a.s {
		return a.rkm, true
	}
	return a.rkm + (kraj-a.s)/(b.s-a.s)*(b.rkm-a.rkm), true
}

var reRkmUOpisu = regexp.MustCompile(`(?i)rkm\s*[0-9]+(?:[+.,][0-9]+)?`)

// kilometarIzOpisa vadi kilometar iz opisa ušća u registru vodotoka, npr.
// „ušće u Dravu kod Petrijevaca, rkm 22+400”. Bez kilometra ušća nema.
func kilometarIzOpisa(opis string) (float64, bool) {
	m := reRkmUOpisu.FindString(opis)
	if m == "" {
		return 0, false
	}
	return hydro.ParseStationingKm(m)
}

// genitiv i akuzativ imena rijeke, koliko treba za natpis „ušće Drave” i
// „ušće u Dravu”: imena na -a mijenjaju nastavak, ostala (Dunav) ne.
func genitiv(ime string) string {
	if strings.HasSuffix(ime, "a") {
		return strings.TrimSuffix(ime, "a") + "e"
	}
	return ime
}

func akuzativ(ime string) string {
	if strings.HasSuffix(ime, "a") {
		return strings.TrimSuffix(ime, "a") + "u"
	}
	return ime
}
