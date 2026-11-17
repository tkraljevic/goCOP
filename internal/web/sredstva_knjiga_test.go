package web

import (
	"testing"

	"gocop/internal/models"
)

// Čip filtra knjige mijenja samo svoj parametar, a ostali izbor ostaje;
// Excel ide po istim filtrima.
func TestFiltriKnjige(t *testing.T) {
	d := MtsPageData{Sektor: "B", Skladiste: &models.Skladiste{ID: "s1"}, VrstaID: "lopata", Od: "2025-12-01",
		Vrste: []models.VrstaSredstva{{ID: "lopata", Naziv: "Lopata"}}}
	if u := d.URLKnjige("vrsta", ""); u != "/sredstva/promet?od=2025-12-01&sektor=B&skladiste=s1" {
		t.Errorf("brisanje sredstva: %s", u)
	}
	if u := d.URLKnjige("skladiste", "s2"); u != "/sredstva/promet?od=2025-12-01&sektor=B&skladiste=s2&vrsta=lopata" {
		t.Errorf("drugo skladište: %s", u)
	}
	if u := d.ExcelKnjige(); u != "/sredstva/promet.xlsx?od=2025-12-01&sektor=B&skladiste=s1&vrsta=lopata" {
		t.Errorf("Excel: %s", u)
	}
	if n := d.NazivVrsteKnjige(); n != "Lopata" {
		t.Errorf("naziv sredstva: %s", n)
	}
	d.VrstaID = ""
	if n := d.NazivVrsteKnjige(); n != "sva" {
		t.Errorf("bez sredstva: %s", n)
	}
	if dd := d.DatumiKnjige(); len(dd) != 2 || dd[0].Vrijednost != "2025-12-01" || dd[1].Ime != "do" {
		t.Errorf("datumi: %+v", dd)
	}
}
