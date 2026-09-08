package arhiva

import "testing"

// Telemetrija zna javiti jedan sat besmislice. Prepoznaje se po susjedima:
// 65, −640, 63 nije pad vodostaja nego kvar mjerila.
func TestIspadiTelemetrijeSeIzbacuju(t *testing.T) {
	sat := int64(3600)
	niz := func(v ...float64) []zapis {
		out := make([]zapis, len(v))
		for i, x := range v {
			out[i] = zapis{t: int64(i) * sat, v: x}
		}
		return out
	}
	imaVrijednost := func(z []zapis, v float64) bool {
		for _, x := range z {
			if x.v == v {
				return true
			}
		}
		return false
	}

	// pojedinačni ispad među mirnim susjedima
	got := bezSiljaka("vodostaj", niz(65, 65, -640, 63, 63))
	if len(got) != 4 || imaVrijednost(got, -640) {
		t.Errorf("ispad nije izbačen: %v", got)
	}
	// stvarni vodni val se ne dira, ma koliko strm bio
	val := niz(100, 130, 165, 205, 250)
	if len(bezSiljaka("vodostaj", val)) != len(val) {
		t.Error("porast vodostaja proglašen ispadom")
	}
	// pad koji traje ostaje: nije ispad ako se susjedi ne slažu
	pad := niz(300, 200, 100, 0, -100)
	if len(bezSiljaka("vodostaj", pad)) != len(pad) {
		t.Error("trajni pad proglašen ispadom")
	}
	// ispad uz prazninu u nizu, gdje drugog susjeda nema
	uzPrazninu := []zapis{{t: 0, v: -430}, {t: sat, v: 43}, {t: 2 * sat, v: 43}}
	if got := bezSiljaka("vodostaj", uzPrazninu); imaVrijednost(got, -430) {
		t.Errorf("ispad uz prazninu nije izbačen: %v", got)
	}
	// druge veličine se ne diraju: temperatura vode zna skočiti preko noći
	temp := []zapis{{t: 0, v: 4}, {t: sat, v: 25}, {t: 2 * sat, v: 4}}
	if len(bezSiljaka("temperatura", temp)) != 3 {
		t.Error("filtar šiljaka ne smije dirati temperaturu")
	}

	// fizički nemoguće vrijednosti staju i prije toga
	if mogucaVrijednost("vodostaj", -2270) {
		t.Error("-2270 cm je prošlo kao vodostaj")
	}
	if !mogucaVrijednost("vodostaj", -151) {
		t.Error("-151 cm je stvarno izmjeren vodostaj Batine")
	}
	if mogucaVrijednost("temperatura", 60) || !mogucaVrijednost("temperatura", 0) {
		t.Error("granice temperature vode")
	}
}
