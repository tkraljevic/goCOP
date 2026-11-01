package models

import (
	"regexp"
	"strconv"
	"strings"
)

// Crpke su kapaciteti crpki crpne stanice u m³/s, kako slijede iz zapisa
// kapaciteta: „2,5 + 2x1,75” su tri crpke (2,5, 1,75 i 1,75). Prazan ili
// nečitljiv zapis daje nil, pa broj crpki tada nije poznat.
func (s Structure) Crpke() []float64 {
	zapis := strings.TrimSpace(strings.ReplaceAll(s.CapacityText, ",", "."))
	if zapis == "" {
		return nil
	}
	var out []float64
	for _, dio := range strings.Split(zapis, "+") {
		m := dioKapaciteta.FindStringSubmatch(strings.TrimSpace(dio))
		if m == nil {
			return nil
		}
		koliko := 1
		if m[1] != "" {
			koliko, _ = strconv.Atoi(m[1]) // regex jamči znamenke
		}
		v, err := strconv.ParseFloat(m[2], 64)
		if err != nil || koliko < 1 || v <= 0 {
			return nil
		}
		for i := 0; i < koliko; i++ {
			out = append(out, v)
		}
	}
	return out
}

// UkupniKapacitet je zbroj kapaciteta crpki u m³/s
func (s Structure) UkupniKapacitet() float64 {
	z := 0.0
	for _, c := range s.Crpke() {
		z += c
	}
	return z
}

var dioKapaciteta = regexp.MustCompile(`^(?:(\d+)\s*[xX×]\s*)?(\d+(?:\.\d+)?)$`)

// vodostajPogona: „76” ili „76 (90)”, gdje je broj u zagradi iznimka, a
// okvirni vodostaj prvi broj; „od -20 do 200” nema jednog broja
var vodostajPogona = regexp.MustCompile(`^([+-]?\d+)(?:\s*\([^)]*\))?$`)

// VodostajPogona je okvirni vodostaj uključenja ili zaustavljanja iz zapisa
// u evidenciji; zapis koji nije jedan broj (uz iznimku u zagradi) daje nil
func VodostajPogona(zapis string) *int {
	m := vodostajPogona.FindStringSubmatch(strings.TrimSpace(zapis))
	if m == nil {
		return nil
	}
	v, err := strconv.Atoi(strings.TrimPrefix(m[1], "+"))
	if err != nil {
		return nil
	}
	return &v
}
