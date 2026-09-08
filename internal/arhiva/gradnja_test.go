package arhiva

import (
	"testing"
	"time"
)

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

// Premještanje nule pomiče cijeli niz prije tog datuma. Dunavskim letvama od
// Paksa do Mohácsa nula je 1.1.1943. spuštena za 2 m; bez svođenja bi se
// vrijednosti s dviju strana tog datuma tiho miješale.
func TestSvodenjeNaDanasnjuKotu(t *testing.T) {
	dan := func(s string) int64 {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return d.UTC().Unix()
	}
	z := []zapis{
		{t: dan("1909-01-07"), v: -165},
		{t: dan("1942-12-31"), v: 100},
		{t: dan("1943-01-01"), v: 300}, // od tog dana vrijedi nova nula
		{t: dan("2026-01-01"), v: 50},
	}
	p := []promjena{{do: dan("1943-01-01"), pomak: 200}}
	n, pomak := naKotu(z, p)
	if n != 2 || pomak != 200 {
		t.Errorf("svedeno %d zapisa uz pomak %v, očekivano 2 i 200", n, pomak)
	}
	if z[0].v != 35 {
		t.Errorf("7.1.1909.: %v cm, očekivano 35", z[0].v)
	}
	if z[1].v != 300 {
		t.Errorf("31.12.1942.: %v cm, očekivano 300", z[1].v)
	}
	// od datuma promjene nadalje niz se ne dira
	if z[2].v != 300 || z[3].v != 50 {
		t.Errorf("vrijednosti od 1.1.1943. su dirane: %v, %v", z[2].v, z[3].v)
	}
	// letva bez zabilježene promjene ostaje netaknuta
	c := []zapis{{t: dan("1909-01-07"), v: -165}}
	if n, _ := naKotu(c, nil); n != 0 || c[0].v != -165 {
		t.Error("niz bez zabilježene promjene je dirán")
	}
}
