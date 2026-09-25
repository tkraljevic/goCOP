package web

import (
	"encoding/json"
	"regexp"
	"strings"

	geom "gocop/internal/geometrija"
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
	Letva  string  // šifra letve s druge strane; na kraju pritoke njezine vrijednosti produžuju krivulju do ušća
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

// usceNaToku računa kilometar glavnog toka na kojem pritoka u njega ulazi.
// Kraj pritoke je onaj njezin kraj koji je bliže glavnom toku; kilometar se
// interpolira između dviju susjednih rkm sidara između kojih projekcija padne. Bez
// barem dviju oznaka ili s krajem pritoke dalje od dva kilometra od toka
// ušća nema — takva geometrija ne kaže gdje se vode sastaju.
func usceNaToku(pritoka, glavni geoTok) (float64, bool) {
	if len(glavni.oznake) < 2 || len(pritoka.linija) == 0 {
		return 0, false
	}
	sidra := make([]geom.Sidro, 0, len(glavni.oznake))
	for _, z := range glavni.oznake {
		sidra = append(sidra, geom.Sidro{Rkm: z.rkm, Tocka: z.xy, Izvor: "GeoJSON rkm sidro"})
	}
	kalibracija, err := geom.NovaKalibracija(glavni.linija, sidra)
	if err != nil {
		return 0, false
	}
	kraj, najD := 0.0, 1e9
	for _, k := range [][2]float64{pritoka.linija[0], pritoka.linija[len(pritoka.linija)-1]} {
		p, err := geom.Projektiraj(glavni.linija, k)
		if err != nil {
			return 0, false
		}
		if p.UdaljenostKM < najD {
			kraj, najD = p.UzduzKM, p.UdaljenostKM
		}
	}
	if najD > 2 {
		return 0, false
	}
	return kalibracija.RKM(kraj)
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
