package main

import (
	"strings"
	"testing"

	"gocop/internal/hydro"
)

// Uzorak građen po B.34.14 iz wikija: ista voda u dva retka (Panjik I i II)
// pa druga voda; objekti po nasipu i po rijeci; vodomjer u proznom obliku i
// u tablici. Druga dionica: potok, objekt bez stacionaže, mjerilo u metrima.
const uzorak = `
# Branjeno područje 34

## Dionica B.34.14.

**Ukupna duljina dionice:** 31,100 km
**Ukupno nasipa:** 5,552 km nasipa

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
| km 3+650 | l.o., ustava Hobođ II, b.c.p. Ø 100 |
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

### Nasipi

Na ovoj dionici ne postoje nasipi!

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
	if s.LengthKm == nil || *s.LengthKm != 31.1 || s.EmbankmentKm == nil || *s.EmbankmentKm != 5.552 {
		t.Errorf("ukupne duljine: %v %v", s.LengthKm, s.EmbankmentKm)
	}
	// ista voda u dva retka = jedna poddionica s dva nasipa; druga voda = druga poddionica
	if len(s.Parts) != 2 {
		t.Fatalf("poddionica %d, očekivane 2", len(s.Parts))
	}
	p0 := s.Parts[0]
	if p0.Seq != 1 || p0.Bank != "D" || p0.StationingKind != hydro.StationingRiver || p0.KmFrom == nil || *p0.KmFrom != 72.9 || *p0.KmTo != 77.92 {
		t.Errorf("prva poddionica: %+v", p0)
	}
	if p0.Extent != "Sveti Đurađ – cestovni most Donji Miholjac" || p0.LengthKm == nil || *p0.LengthKm != 5.02 {
		t.Errorf("obuhvat i duljina: %q %v", p0.Extent, p0.LengthKm)
	}
	if len(p0.Embankments) != 2 || p0.Embankments[0].Name != "Nasip Panjik I" || p0.Embankments[1].Name != "Nasip Panjik II" {
		t.Errorf("nasipi prve poddionice: %+v", p0.Embankments)
	}
	e := p0.Embankments[0]
	if e.WaterKind != "rkm" || e.WaterFrom == nil || *e.WaterFrom != 72.9 || e.EmbFrom == nil || *e.EmbTo != 0.27 || e.LengthKm == nil || *e.LengthKm != 0.27 {
		t.Errorf("odsjek nasipa: %+v", e)
	}
	// objekt po nasipu zna na kojem je nasipu
	if len(p0.Objects) != 1 || p0.Objects[0].StationingKind != hydro.StationingEmbankment || p0.Objects[0].OnEmbankment != "Nasip Panjik I" || p0.Objects[0].Stationing == nil || *p0.Objects[0].Stationing != 0.195 {
		t.Errorf("objekt prve poddionice: %+v", p0.Objects)
	}
	// vodomjer isti u oba retka: jednom
	if len(p0.Gauges) != 1 || !strings.HasPrefix(p0.Gauges[0].StationName, "Donji Miholjac") || p0.Gauges[0].CriticalCm != "+500" {
		t.Errorf("vodomjeri prve poddionice: %+v", p0.Gauges)
	}
	if p0.ProtectedText != "**Osječko-baranjska**; Donji Miholjac: Sveti Đurađ, (D. Miholjac)" {
		t.Errorf("ugroženo: %q", p0.ProtectedText)
	}

	p1 := s.Parts[1]
	if p1.Seq != 2 || len(p1.Objects) != 2 || len(p1.Embankments) != 1 {
		t.Fatalf("druga poddionica: %+v", p1)
	}
	if o := p1.Objects[0]; o.Bank != "L" || o.Name != "ustava Hobođ II, b.c.p. Ø 100" || o.StationingKind != "nkm" || o.OnEmbankment != "Nasip Zabara-Hobođ" {
		t.Errorf("objekt s obalom: %+v", o)
	}
	if o := p1.Objects[1]; o.StationingKind != "rkm" || o.Stationing == nil || *o.Stationing != 88.24 || o.OnEmbankment != "" {
		t.Errorf("objekt po rijeci: %+v", o)
	}
	if len(p1.Gauges) != 1 || !strings.HasPrefix(p1.Gauges[0].StationName, "Moslavina") || p1.Gauges[0].RegularCm != "+420" {
		t.Errorf("tablični vodomjer: %+v", p1.Gauges)
	}
	if !strings.Contains(s.Description, "Sveti Đurađ") || !strings.Contains(s.Description, "Dravica") {
		t.Errorf("opis mora nositi obje poddionice: %s", s.Description)
	}

	// potok, objekt bez stacionaže, kota na mostu u metrima je mjerilo
	// (postaja s kotom), a ne uputa
	k := secs[1]
	if len(k.Parts) != 1 || k.Parts[0].StationingKind != hydro.StationingStream {
		t.Fatalf("potok: %+v", k.Parts)
	}
	if objs := k.Parts[0].Objects; len(objs) != 2 || objs[0].StationingKind != "pkm" || objs[1].StationingKind != "" || objs[1].Stationing != nil {
		t.Errorf("pkm i prazna stacionaža: %+v", objs)
	}
	if g := k.Parts[0].Gauges; len(g) != 1 || g[0].StationName != "Molve, cestovni most, km 0+720" || !g[0].IsGauge() {
		t.Errorf("kota na mostu je mjerilo vodostaja: %+v", g)
	}
	if len(k.Parts[0].Embankments) != 0 {
		t.Errorf("'ne postoje nasipi' nije nasip: %+v", k.Parts[0].Embankments)
	}
}
