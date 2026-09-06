package web

import (
	"testing"

	"gocop/internal/models"
)

func km(v float64) *float64 { return &v }

// Privitak za B.34.1 slaže objekte po nasipima; ušća i vodokaz nemaju upisan
// nasip nego samo rkm, i moraju pasti na nasip čiji raspon uz vodu ih pokriva.
func TestObjektiPadajuNaSvojNasip(t *testing.T) {
	p := models.SectionPart{
		Embankments: []models.PartEmbankment{
			{Name: "Nasip Državna granica -Draž", WaterKind: "rkm", WaterFrom: km(1430), WaterTo: km(1423.77)},
			{Name: "Nasip za zaštitu Batine", WaterKind: "rkm", WaterFrom: km(1425.77), WaterTo: km(1423.77)},
			{Name: "Nasip Gomboš", WaterKind: "rkm", WaterFrom: km(1423.77), WaterTo: km(1421.77)},
		},
		Objects: []models.PartObject{
			{Name: "CS Budžak", StationingKind: "nkm", StationingText: "km 2+700", OnEmbankment: "Nasip Državna granica -Draž"},
			{Name: "ušće Šarkanjskog Dun.", StationingKind: "rkm", StationingText: "rkm 1428+010"},
			{Name: "vodokaz Batina", StationingKind: "rkm", StationingText: "rkm 1424+850"},
			{Name: "cijevni propust", StationingKind: "nkm", StationingText: "km 0+304", OnEmbankment: "Nasip za zaštitu Batine"},
			{Name: "vik.nas.Zeleni otok", StationingKind: "rkm", StationingText: "rkm 1423+000"},
			{Name: "nešto bez stacionaže"},
		},
	}
	rows := embankmentRows(p)
	if len(rows) != 4 {
		t.Fatalf("redaka %d, očekivano 3 nasipa + 1 bez nasipa", len(rows))
	}
	imena := func(r EmbankmentRow) []string {
		var out []string
		for _, o := range r.Objects {
			out = append(out, o.Name)
		}
		return out
	}
	ocekivano := [][]string{
		{"CS Budžak", "ušće Šarkanjskog Dun."},
		{"vodokaz Batina", "cijevni propust"},
		{"vik.nas.Zeleni otok"},
		{"nešto bez stacionaže"},
	}
	for i, want := range ocekivano {
		got := imena(rows[i])
		if len(got) != len(want) {
			t.Errorf("red %d: %v, očekivano %v", i+1, got, want)
			continue
		}
		for j := range want {
			if got[j] != want[j] {
				t.Errorf("red %d, objekt %d: %q, očekivano %q", i+1, j+1, got[j], want[j])
			}
		}
	}
	if rows[3].Embankment != nil {
		t.Error("zadnji red mora biti bez nasipa")
	}
}

// Naziv nasipa u objektu i u popisu nasipa razlikuju se u crtici i razmaku —
// isti nasip je isti nasip.
func TestNazivNasipaPodnosiCrticuIRazmake(t *testing.T) {
	p := models.SectionPart{
		Embankments: []models.PartEmbankment{{Name: "Nasip Državna granica – Draž"}},
		Objects:     []models.PartObject{{Name: "CS", OnEmbankment: "nasip državna granica -draž"}},
	}
	rows := embankmentRows(p)
	if len(rows) != 1 || len(rows[0].Objects) != 1 {
		t.Errorf("objekt nije pao na svoj nasip: %d redaka", len(rows))
	}
}
