package web

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Popis dionica u Excelu ide po sektoru i području, dionice prirodnim redom
// šifre (B.34.2 prije B.34.10), sa zbrojem duljina po području i sektoru.
func TestKnjigaPopisaDionica(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	mk := func(code string, area int, opis string, km float64) DionicaUPopisu {
		return DionicaUPopisu{Section: models.Section{Code: code, AreaID: area, SectorID: "B", Description: opis, DescriptionCustom: true, LengthKm: f(km),
			Parts: []models.SectionPart{{Seq: 1, WatercourseName: "Dunav", Bank: "L"}}}, Rukovoditelj: "Ruk " + code}
	}
	redci := []DionicaUPopisu{mk("B.34.10", 34, "deseta", 3), mk("B.34.2", 34, "druga", 5), mk("B.16.1", 16, "prva u Baranji", 11.3)}
	redci[2].Vodomjeri = "Batina (P 500 · R 600)"
	sektori := []models.Sector{{ID: "B", Name: "Sektor B — Dunav i donja Drava"}}
	podrucja := []models.Area{{ID: 16, SectorID: "B", Name: "Mali sliv Baranja", Subcenter: "Podcentar Darda"}, {ID: 34, SectorID: "B", Name: "Međudržavne rijeke", Subcenter: "COP Osijek"}}
	var b bytes.Buffer
	if err := KnjigaPopisaDionica(redci, sektori, podrucja, ZaglavljeIzvoza{Organizacija: "Hrvatske vode", Datum: time.Now()}, "").Zapisi(&b); err != nil {
		t.Fatal(err)
	}
	r, err := procitajXLSX(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var sve []string
	for _, x := range r {
		sve = append(sve, strings.Join(x, "|"))
	}
	list := strings.Join(sve, "\n")
	for _, want := range []string{"POPIS ŠTIĆENIH DIONICA", "SEKTOR B — DUNAV I DONJA DRAVA", "BP 16 — Mali sliv Baranja · Podcentar Darda", "B.16.1|prva u Baranji|Dunav|lijeva obala||11.3||0 / 0|Batina (P 500 · R 600)||Ruk B.16.1|",
		"ukupno BP 16|1 dionica||||11.3|0", "ukupno BP 34|2 dionice||||8|0", "ukupno sektor B|3 dionice||||19.3|0"} {
		if !strings.Contains(list, want) {
			t.Errorf("popis nema %q\n%s", want, list)
		}
	}
	if strings.Index(list, "B.34.2|") > strings.Index(list, "B.34.10|") {
		t.Error("B.34.10 je ispred B.34.2")
	}
}
