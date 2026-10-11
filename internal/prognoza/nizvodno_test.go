package prognoza

import (
	"reflect"
	"testing"
)

// brid je ulaz u letvu s pomakom u satima.
type brid struct {
	ulaz, letva string
	pomak       int
}

// pojasiLanca slaže pojase iz bridova ulaz→letva; letva s više bridova
// dobiva po jedan pojas za svaki, kao letva s pojasima vodnosti.
func pojasiLanca(bridovi ...brid) map[string][]Pojas {
	p := map[string][]Pojas{}
	for _, b := range bridovi {
		p[b.letva] = append(p[b.letva], Pojas{Letva: b.letva, Velicina: "vodostaj",
			Ulazi: []Ulaz{{Letva: b.ulaz, Velicina: "vodostaj", PomakH: b.pomak}}})
	}
	return p
}

// lanacKakavJeDunav: gornja → prva (pojasi 3 i 14) → druga 6 → {treca 0,
// cetvrta 3}, cetvrta → peta 2. Isti oblik kao Mohács → Batina → Aljmaš →
// {Osijek, Dalj} → Vukovar.
func lanacKakavJeDunav() map[string][]Pojas {
	return pojasiLanca(brid{"gornja", "prva", 3}, brid{"gornja", "prva", 14},
		brid{"prva", "druga", 6}, brid{"druga", "treca", 0},
		brid{"druga", "cetvrta", 3}, brid{"cetvrta", "peta", 2})
}

// Letva niže izmjerena je do izdanja, pa joj nevolja odozgo stiže tek
// poslije njega. Zbroj kašnjenja od zadnjeg očitanja gornje dao bi drugoj
// +1 h, a ona je upitna tek od +7 h.
func TestNizvodnoMjerenaLetvaNePomice(t *testing.T) {
	sada := int64(500000)
	a := Nizvodno(lanacKakavJeDunav(), map[string]int64{"gornja": sada - 10}, sada)
	got := doseziUpitnog(a, sada)
	want := map[string]int{"gornja": 0, "prva": 1, "druga": 7, "treca": 7, "cetvrta": 10, "peta": 12}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dosezi %v, želim %v", got, want)
	}
}

// Rezerva ili vrh bez budućnosti upitni su od sata poslije izdanja.
func TestNizvodnoRezervaOdSljedecegSata(t *testing.T) {
	sada := int64(500000)
	got := doseziUpitnog(Nizvodno(lanacKakavJeDunav(), map[string]int64{"gornja": sada + 1}, sada), sada)
	if got["prva"] != 4 || got["druga"] != 10 || got["gornja"] != 1 {
		t.Errorf("dosezi %v, želim gornja 1, prva 4, druga 10", got)
	}
}

// Redom slaže letve samo po prvom pojasu; put kroz drugi ulaz mora se naći
// i kad ga jedan prolaz ne bi stigao, a letva izvan lanca ne dobiva ništa.
func TestNizvodnoDoNepomicneTocke(t *testing.T) {
	sada := int64(500000)
	pojasi := pojasiLanca(brid{"c", "d", 1}, brid{"b", "c", 1}, brid{"a", "b", 1}, brid{"x", "y", 1})
	pojasi["d"][0].Ulazi = append([]Ulaz{{Letva: "y", PomakH: 1}}, pojasi["d"][0].Ulazi...)
	got := doseziUpitnog(Nizvodno(pojasi, map[string]int64{"a": sada + 1}, sada), sada)
	want := map[string]int{"a": 1, "b": 2, "c": 3, "d": 4}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dosezi %v, želim %v", got, want)
	}
	// Letva iz prvi zadržava svoj raniji sat i kad je niže od druge.
	got = doseziUpitnog(Nizvodno(pojasi, map[string]int64{"a": sada + 1, "c": sada - 2}, sada), sada)
	if got["c"] != 0 || got["d"] != 1 || got["b"] != 2 {
		t.Errorf("dosezi s dvije nevolje %v", got)
	}
}

// Prenesena letva upitna je od najranijeg dosega svojih izvora; bez upitnog
// izvora je nema.
func TestNizvodnoPrenesene(t *testing.T) {
	postavi(t, &PreneseneLetve, []struct {
		Letva  string
		Izvori []string
	}{
		{"preko-prve", []string{"prva"}},
		{"izmedju", []string{"prva", "druga"}},
		{"preko-druge", []string{"druga", "cetvrta"}},
	})
	d := map[string]int{"prva": 1, "druga": 7}
	preneseniDoseg(d)
	if d["preko-prve"] != 1 || d["izmedju"] != 1 || d["preko-druge"] != 7 {
		t.Errorf("preneseni %v", d)
	}
	d = map[string]int{"prva": 1}
	preneseniDoseg(d)
	if _, ima := d["preko-druge"]; ima {
		t.Errorf("bez upitnog izvora prenesena letva ne smije biti upitna: %v", d)
	}
}
