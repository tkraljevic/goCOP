package web

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"gocop/internal/xlsxw"
)

// Izvoz nosi zaglavlje s izdavačem, letve u stupcima, našu vrijednost s
// rasponom i protokom, tuđe prognoze u svojim recima, te potpise voditelja
// centra i zamjenika. Brojevi su izmišljeni.
func TestIzvozPrognozeUExcel(t *testing.T) {
	v := func(x float64) *float64 { return &x }
	data := PrognozePageData{
		Izdano: "24.9.2026. u 08:00", Bliski: BliziDosezi, Dani: []string{"čet 24.9.", "pet 25.9."},
	}
	tab := TablicaPrognoza{Naslov: "Dunav", Letve: []LetvaPrognoze{
		{Kod: "bezdan", Naziv: "Bezdan (Srbija)", Voda: "Dunav", Stacionaza: "rkm 1.425,59", Pregledna: true,
			SadaCmV: v(120), Dani: []CelijaDana{{Tude: []TudaCelija{{Oznaka: "RS", CmV: 131}}}, {}}},
		{Kod: "batina", Naziv: "Batina", Voda: "Dunav", Racuna: "vodostaj", SadaCmV: v(150), SadaQV: v(2100),
			Vrijednosti: []VrijednostPrognoze{{DosegH: 6, Ima: true, CmV: v(152), CmRaspon: "±3"}, {DosegH: 12, Ima: true, CmV: v(155)}},
			Dani: []CelijaDana{
				{CmV: v(160.4), Raspon: "±5", QV: v(2249.7), Tude: []TudaCelija{{Oznaka: "HU", CmV: 158}}},
				{CmV: v(171), Raspon: "±9", QV: v(2400), Dnevna: true},
			}},
	}}
	z := ZaglavljeIzvoza{Organizacija: "Hrvatske vode", Odjel: "VGO za Dunav i donju Dravu, Osijek",
		Centar: "COP Osijek", Mjesto: "Osijek", Datum: time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC),
		Potpisnici: []PotpisnikIzvoza{{Funkcija: "voditelj Centra obrane od poplava", Ime: "Pero Perić"},
			{Funkcija: "zamjenik voditelja Centra obrane od poplava"}}}
	k := &xlsxw.Knjiga{}
	listPrognoze(k, z, data, tab)
	var b bytes.Buffer
	if err := k.Zapisi(&b); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var sve strings.Builder
	for _, f := range zr.File {
		r, _ := f.Open()
		x, _ := io.ReadAll(r)
		r.Close()
		sve.Write(x)
	}
	txt := sve.String()
	for _, want := range []string{"PROGNOZA VODOSTAJA — Dunav", "VGO za Dunav i donju Dravu, Osijek", "COP Osijek",
		">Bezdan<", "RS · 1.425,59", "Batina", "čet 24.9. 07 h", "±5", ">160<", "dnevni", "satni", ">2250<", ">158<", ">131<",
		"voditelj Centra obrane od poplava", "zamjenik voditelja Centra obrane od poplava", "Pero Perić",
		`s="15"`, `s="16"`} { // raspon sivo, protok ukošeno
		if !strings.Contains(txt, want) {
			t.Errorf("u izvozu nema %q", want)
		}
	}
}
