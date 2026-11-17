package web

import (
	"testing"

	"gocop/internal/models"
)

// Uz zahvat na kartici skladišta piše kamo je roba otišla ili odakle je
// došla: drugi redak poteza obrnutog predznaka, odredište puta, „u
// skladištu” za punjenje, inače crtica.
func TestKamoIliOdakle(t *testing.T) {
	ovdje := models.Promet{SkladisteID: "s1", SkladisteNaziv: "Bilje"}
	red := func(veza, vrsta string, kol float64, mjesto models.Promet) models.Promet {
		mjesto.VezaID, mjesto.Vrsta, mjesto.Kolicina = veza, vrsta, kol
		return mjesto
	}
	draz := models.Promet{SkladisteID: "s2", SkladisteNaziv: "CS Draž"}
	nasip := models.Promet{AreaID: 16, AreaName: "Branjeno", Mjesto: "nasip kod Draža"}
	drugi := []models.Promet{
		red("izd", models.PrometIzdano, 4, nasip),
		red("pov", models.PrometPovrat, -4, nasip),
		red("prij", models.PrometPrijenos, -6, draz),
		red("prij", models.PrometOtpis, -1, draz),
	}
	zahvat := func(redci ...models.Promet) RedakKnjige {
		return RedakKnjige{Promet: redci[0], Redci: redci}
	}
	put := red("put", models.PrometPrijenos, -2, ovdje)
	put.PremaSkladisteID, put.PremaSkladisteNaziv = "s3", "Lapovac"
	for ime, s := range map[string]struct {
		z    RedakKnjige
		ocek string
	}{
		"izdavanje":  {zahvat(red("izd", models.PrometIzdano, -4, ovdje)), "na teren · BP 16 Branjeno · nasip kod Draža"},
		"povrat":     {zahvat(red("pov", models.PrometPovrat, 4, ovdje)), "s terena · BP 16 Branjeno · nasip kod Draža"},
		"zaprimanje": {zahvat(red("prij", models.PrometPrijenos, 5, ovdje)), "iz CS Draž"},
		"manjak":     {zahvat(red("prij", models.PrometOtpis, -1, ovdje), red("prij", models.PrometPrijenos, 1, ovdje)), "iz CS Draž"},
		"na putu":    {zahvat(put), "u Lapovac"},
		"punjenje":   {zahvat(red("pun", models.PrometPunjenje, -4, ovdje), red("pun", models.PrometPunjenje, 4, ovdje)), "u skladištu"},
		"primka":     {zahvat(red("", models.PrometPrimka, 9, ovdje)), "—"},
	} {
		if g := kamoIliOdakle(s.z, "s1", drugi); g != s.ocek {
			t.Errorf("%s: %q, očekivano %q", ime, g, s.ocek)
		}
	}
	if v := vezeZahvata([]RedakKnjige{zahvat(put), zahvat(put), zahvat(red("", models.PrometPrimka, 1, ovdje))}); len(v) != 1 || v[0] != "put" {
		t.Errorf("potezi: %v", v)
	}
}

// Kartica potreba ide redom kataloga i preskače nule.
func TestPotrebeUKartici(t *testing.T) {
	vrste := []models.VrstaSredstva{{ID: "a", Naziv: "Agregat", Jedinica: "kom"}, {ID: "l", Naziv: "Lopata", Jedinica: "kom"}, {ID: "v", Naziv: "Vreće", Jedinica: "kom"}}
	p := potrebeUKartici(map[string]models.Potreba{
		"v": {Kolicina: 9000, Napomena: "prebačeno"}, "a": {Kolicina: 3}, "l": {Kolicina: 0},
	}, vrste)
	if len(p) != 2 || p[0].Naziv != "Agregat" || p[1].Napomena != "prebačeno" || p[1].Kolicina != 9000 {
		t.Errorf("potrebe: %+v", p)
	}
}
