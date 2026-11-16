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
