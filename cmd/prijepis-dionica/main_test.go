package main

import (
	"strings"
	"testing"
)

// Uzorak građen po B.34.14 iz wikija: dvije poddionice, u prvoj tri retka
// (Panjik I, Panjik II, Donji Miholjac–Sveti Đurađ), u drugoj tri; objekti
// po nasipu i po rijeci; vodomjer u proznom obliku i u tablici.
const uzorak = `
# Branjeno područje 34

## Dionica B.34.14.

**Ukupna duljina dionice:** 31,100 km

### Vodotok

r. Drava, d.o.; Sveti Đurađ – cestovni most Donji Miholjac; rkm 72+900 - 77+920; (5,020 km)

### Objekti na dionici

| Stacionaža | Objekt |
|---|---|
| km 0+195 | bet.cij. pr. Ø 100 cm sa AB ustavom / zapornicom |

### Nasipi

| Naziv nasipa | Podaci |
|---|---|
| Nasip Panjik I | rkm 72+900 - 72+630; km 0+000 - 0+270; (0,270 km) |

### Ugroženo područje

- **Osječko-baranjska**
  - Donji Miholjac: Sveti Đurađ, (D. Miholjac)

### Vodomjeri i kriteriji

Donji Miholjac , rkm 80,60 (88,570); P = +300; R = +400; I = +480; IS = +500; M = +538 (22.07.1972.)

### Vodotok

r. Drava, d.o.; Sveti Đurađ – cestovni most Donji Miholjac; rkm 72+900 - 77+920; (5,020 km)

### Nasipi

| Naziv nasipa | Podaci |
|---|---|
| Nasip Panjik II | rkm 73+350 - 73+000; km 0+000 - 0+532; (0,532 km) |

### Ugroženo područje

- **Osječko-baranjska**
  - Donji Miholjac: Sveti Đurađ, (D. Miholjac)

### Vodomjeri i kriteriji

Donji Miholjac , rkm 80,60 (88,570); P = +300; R = +400; I = +480; IS = +500; M = +538 (22.07.1972.)

### Vodotok

r. Drava, d.o.; Cestovni most Donji Miholjac – Dravica; rkm 77+920 - 104+000; (26,080 km)

### Objekti na dionici

| Stacionaža | Objekt |
|---|---|
| km 3+650 | ustava Hobođ II, b.c.p. Ø 100 |
| rkm 88+240 | ušće Spojni k. Karašica-Drava |

### Nasipi

| Naziv nasipa | Podaci |
|---|---|
| Nasip Zabara-Hobođ | rkm 86+450 - 80+550; km 0+000 - 4+750; (4,750 km) |

### Ugroženo područje

- **Osječko-baranjska**
  - Donji Miholjac: (D. Miholjac)
  - Viljevo: Viljevo, Ivanovo, Blanje

### Vodomjeri i kriteriji

| Vodomjer | P (Pripremno stanje) | R (Redovna obrana) | I (Izvanredna obrana) | IS (Izvanredno stanje) | M (Najviši zabilježeni vodostaj) | Napomena |
|---|---|---|---|---|---|---|
| Moslavina , rkm 98,20 (90,940) | +320 | +420 | +520 | +560 | +565 (20.07.1972.) |  |

## Dionica B.34.15.

### Vodotok

p. Karašica, l.o.; nešto; pkm 0+000 - 5+000; (5,000 km)

### Objekti na dionici

| Stacionaža | Objekt |
|---|---|
| pkm 1+200 | most |
|  | preljev: 149,50 m n.J.m. |

### Vodomjeri i kriteriji

Molve, cestovni most, km 0+720; R: 117,63 m.n.m; I: 117,88 m.n.m; Redovna obrana kada vodostaj dostiže 0,55 m ispod ploče
`

func TestPrijepisPoddionica(t *testing.T) {
	secs := parseFile(uzorak)
	if len(secs) != 2 {
		t.Fatalf("dionica %d, očekivane 2", len(secs))
	}
	s := secs[0]
	if s.Code != "B.34.14" || s.SectorID != "B" || s.AreaID != 34 {
		t.Errorf("šifra/sektor/područje: %s %s %d", s.Code, s.SectorID, s.AreaID)
	}
	if len(s.Parts) != 2 {
		t.Fatalf("poddionica %d, očekivane 2", len(s.Parts))
	}
	if n := len(s.Parts[0].Rows); n != 2 {
		t.Errorf("prva poddionica ima %d retka, očekivana 2", n)
	}
	if s.Parts[0].Bank != "D" || s.Parts[0].RkmFrom == nil || *s.Parts[0].RkmFrom != 72.9 {
		t.Errorf("obala/stacionaža prve poddionice: %s %v", s.Parts[0].Bank, s.Parts[0].RkmFrom)
	}

	// objekt zna na kojem je nasipu: prvi redak, km po nasipu Panjik I
	r0 := s.Parts[0].Rows[0]
	if len(r0.Embankments) != 1 || r0.Embankments[0].Name != "Nasip Panjik I" {
		t.Errorf("nasip prvog retka: %+v", r0.Embankments)
	}
	if len(r0.Objects) != 1 || r0.Objects[0].Kind != "km" || r0.Objects[0].Stationing != "km 0+195" {
		t.Errorf("objekt prvog retka: %+v", r0.Objects)
	}
	// drugi redak nema objekata, ali ima svoj nasip
	if r1 := s.Parts[0].Rows[1]; len(r1.Objects) != 0 || r1.Embankments[0].Name != "Nasip Panjik II" {
		t.Errorf("drugi redak: %+v", r1)
	}
	// druga poddionica: objekt po rijeci i po nasipu
	r2 := s.Parts[1].Rows[0]
	kinds := map[string]string{}
	for _, o := range r2.Objects {
		kinds[o.Stationing] = o.Kind
	}
	if kinds["km 3+650"] != "km" || kinds["rkm 88+240"] != "rkm" {
		t.Errorf("vrste stacionaže: %v", kinds)
	}

	// vodomjeri: prozni oblik i tablični, oba pročitana, po retku
	g0 := r0.Gauges
	if len(g0) != 1 || !strings.HasPrefix(g0[0].StationName, "Donji Miholjac") || g0[0].PrepCm != "+300" ||
		g0[0].CriticalCm != "+500" || g0[0].RecordCm != "+538 (22.07.1972.)" {
		t.Errorf("prozni vodomjer: %+v", g0)
	}
	if g := r2.Gauges; len(g) != 1 || !strings.HasPrefix(g[0].StationName, "Moslavina") || g[0].RegularCm != "+420" {
		t.Errorf("tablični vodomjer: %+v", g)
	}

	// ravna polja su unije: oba vodomjera, svi nasipi, svi objekti, ugroženo spojeno
	names := []string{}
	for _, g := range s.Gauges {
		names = append(names, strings.SplitN(g.StationName, " ", 2)[0])
	}
	if strings.Join(names, ",") != "Donji,Moslavina" {
		t.Errorf("unija vodomjera: %v", names)
	}
	if len(s.Embankments) != 3 || len(s.Structures) != 3 {
		t.Errorf("unije: nasipa %d, objekata %d", len(s.Embankments), len(s.Structures))
	}
	if !strings.Contains(s.Watercourse, "Sveti Đurađ") || !strings.Contains(s.Watercourse, "Dravica") {
		t.Errorf("opis mora nositi obje poddionice: %s", s.Watercourse)
	}
	want := "**Osječko-baranjska**; Donji Miholjac: Sveti Đurađ, (D. Miholjac); Viljevo: Viljevo, Ivanovo, Blanje"
	if s.ProtectedArea != want {
		t.Errorf("ugroženo područje:\n  dobiveno  %s\n  očekivano %s", s.ProtectedArea, want)
	}

	// dionica s potokom, objektom bez stacionaže i vodomjerom u metrima
	k := secs[1]
	if k.Parts[0].Rows[0].Objects[0].Kind != "pkm" || k.Parts[0].Rows[0].Objects[1].Kind != "" {
		t.Errorf("pkm i prazna stacionaža: %+v", k.Parts[0].Rows[0].Objects)
	}
	// mjerilo u metrima nadmorske visine ostaje u retku poddionice, ali ne ide
	// u ravnu uniju iz koje punjenje stvara postaje
	if g := k.Parts[0].Rows[0].Gauges; len(g) != 1 || g[0].StationName != "Molve, cestovni most, km 0+720" || g[0].RegularCm != "117,63 m.n.m" {
		t.Errorf("mjerilo u retku: %+v", g)
	}
	if len(k.Gauges) != 0 {
		t.Errorf("mjerilo u metrima ne smije u uniju vodomjera: %+v", k.Gauges)
	}
}

func TestPrirodniRedoslijedSifri(t *testing.T) {
	if !sectionLess("B.34.2", "B.34.14") || sectionLess("B.34.14", "B.34.2") || !sectionLess("A.1.1", "B.1.1") {
		t.Error("šifre se ne slažu prirodno")
	}
}
