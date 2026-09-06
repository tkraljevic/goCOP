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

func naselje(id int, ime, nasip string) (models.PartTerritory, models.SectionTerritory) {
	t := models.PartTerritory{CountyID: 14, MunicipalityID: 332, SettlementID: &id, OnEmbankment: nasip}
	return t, models.SectionTerritory{CountyID: 14, MunicipalityID: 332, SettlementID: &id, SettlementName: ime}
}

// Privitak naselja navodi uz nasip; kartica ih mora tako i pokazati.
func TestUgrozenoIdeUzSvojNasip(t *testing.T) {
	t1, s1 := naselje(1, "Draž", "Nasip Državna granica -Draž")
	t2, s2 := naselje(2, "Batina", "Nasip za zaštitu Batine")
	t3, s3 := naselje(3, "Gajić", "")
	part := models.SectionPart{
		Embankments: []models.PartEmbankment{
			{Name: "Nasip Državna granica -Draž"},
			{Name: "Nasip za zaštitu Batine"},
		},
		Territories: []models.PartTerritory{t1, t2, t3},
	}
	poKljucu := map[string]models.SectionTerritory{t1.Key(): s1, t2.Key(): s2, t3.Key(): s3}

	rows := embankmentRows(part)
	ostalo := razvrstajUgrozeno(rows, part, poKljucu)

	if len(rows[0].Territories) != 1 || rows[0].Territories[0].SettlementName != "Draž" {
		t.Errorf("prvi nasip ima %v", rows[0].Territories)
	}
	if len(rows[1].Territories) != 1 || rows[1].Territories[0].SettlementName != "Batina" {
		t.Errorf("drugi nasip ima %v", rows[1].Territories)
	}
	if len(ostalo) != 1 || ostalo[0].SettlementName != "Gajić" {
		t.Errorf("nepripisano naselje nije ostalo na poddionici: %v", ostalo)
	}
}

// Dionica bez nasipa i dalje ima ugroženo područje — ono ostaje na poddionici.
func TestDionicaBezNasipaZadrzavaUgrozeno(t *testing.T) {
	t1, s1 := naselje(1, "Draž", "")
	part := models.SectionPart{Territories: []models.PartTerritory{t1}}
	rows := embankmentRows(part)
	ostalo := razvrstajUgrozeno(rows, part, map[string]models.SectionTerritory{t1.Key(): s1})
	if len(rows) != 0 {
		t.Errorf("bez nasipa i bez objekata nema redaka, a ima %d", len(rows))
	}
	if len(ostalo) != 1 || ostalo[0].SettlementName != "Draž" {
		t.Errorf("naselje se izgubilo: %v", ostalo)
	}
}

// Naselje pripisano nasipu kojeg u poddionici nema ne smije nestati.
func TestNaseljeNepoznatogNasipaOstajeNaPoddionici(t *testing.T) {
	t1, s1 := naselje(1, "Draž", "Nasip kojeg nema")
	part := models.SectionPart{
		Embankments: []models.PartEmbankment{{Name: "Nasip Gomboš"}},
		Territories: []models.PartTerritory{t1},
	}
	rows := embankmentRows(part)
	ostalo := razvrstajUgrozeno(rows, part, map[string]models.SectionTerritory{t1.Key(): s1})
	if len(rows[0].Territories) != 0 {
		t.Error("naselje je palo na krivi nasip")
	}
	if len(ostalo) != 1 {
		t.Errorf("naselje se izgubilo: %v", ostalo)
	}
}
