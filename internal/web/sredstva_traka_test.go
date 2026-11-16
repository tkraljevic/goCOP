package web

import (
	"testing"

	"gocop/internal/models"
)

// Traka modula: šest kartica nose sektor, odabrana nije poveznica, a čipovi
// sektora vode na istu karticu u drugom sektoru, bez „Svi” i bez Direkcije.
func TestTrakaSredstava(t *testing.T) {
	d := MtsPageData{
		Sektor:  "B",
		Kartica: "promet",
		Sektori: []models.Sector{{ID: "A", Name: "Sektor A"}, {ID: "B", Name: "Sektor B"}, {ID: "DIREKCIJA", Name: "Direkcija"}},
	}
	k := d.KarticeModula()
	if len(k) != 6 || k[0].Naziv != "Pregled" || k[5].Naziv != "Katalog vrsta" {
		t.Fatalf("kartice: %+v", k)
	}
	if !k[1].Odabrana || k[0].Odabrana {
		t.Errorf("odabrana bi trebala biti Knjiga prometa: %+v", k)
	}
	if k[3].URL != "/sredstva/popisi?sektor=B" {
		t.Errorf("kartica ne nosi sektor: %s", k[3].URL)
	}

	c := d.CipoviSektora()
	if len(c) != 2 || c[0].URL != "/sredstva/promet?sektor=A" || !c[1].Odabran {
		t.Errorf("čipovi sektora: %+v", c)
	}

	d.Kartica = "katalog"
	if c := d.CipoviSektora(); c != nil {
		t.Errorf("katalog je isti za sve sektore, čipova ne treba: %+v", c)
	}
	d.Sektor = ""
	if u := d.KarticeModula()[0].URL; u != "/sredstva" {
		t.Errorf("bez sektora adresa ostaje gola: %s", u)
	}
}

// Skladišta na pregledu idu po branjenim područjima; skladište bez
// područja stoji pod sektorom.
func TestSkladistaPoPodrucjima(t *testing.T) {
	d := MtsPageData{Skladista: []models.Skladiste{
		{Naziv: "Bez područja", Sektor: "B"},
		{Naziv: "CS Draž", Sektor: "B", AreaID: 16, AreaName: "Mali sliv Baranja"},
		{Naziv: "CS Bakanka", Sektor: "B", AreaID: 16, AreaName: "Mali sliv Baranja"},
		{Naziv: "Lapovac", Sektor: "B", AreaID: 17, AreaName: "Mali sliv Karašica-Vučica"},
	}}
	s := d.SkladistaPoPodrucjima()
	if len(s) != 3 || s[0].Naslov != "Sektor B" || s[1].Naslov != "BP 16 · Mali sliv Baranja" || len(s[1].Skladista) != 2 || len(s[2].Skladista) != 1 {
		t.Errorf("skupine: %+v", s)
	}
}
