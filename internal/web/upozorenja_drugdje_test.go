package web

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/dhmz"
	"gocop/internal/models"
)

// Jedno područje je jedan redak: najviša razina prva, pa više upozorenja;
// isto upozorenje dvaput za isto područje broji se jednom, a pomorsko
// područje nema mjesto, pa nosi svoja upozorenja.
func TestUpozorenjaDrugdje(t *testing.T) {
	sada := time.Date(2026, 11, 11, 7, 30, 0, 0, models.Zagreb)
	od := sada.Add(3 * time.Hour)
	u := func(podrucje, dogadjaj string, razina int, sati int) dhmz.Upozorenje {
		return dhmz.Upozorenje{Dogadjaj: dogadjaj, Razina: razina, Podrucje: podrucje, Od: od, Do: od.Add(time.Duration(sati) * time.Hour)}
	}
	d := upozorenjaDrugdje([]dhmz.Upozorenje{
		u("Istarska", "Žuto upozorenje za grmljavinu", 2, 6),
		u("Ličko-senjska", "Žuto upozorenje za kišu", 2, 6),
		u("Ličko-senjska", "Narančasto upozorenje za vjetar", 3, 6),
		u("Ličko-senjska", "Narančasto upozorenje za vjetar", 3, 6), // isto, ponovljeno
		u("Ličko-senjska", "Žuto upozorenje za vjetar", 2, 12),
		u("Velebitski kanal", "Narančasto upozorenje za vjetar", 3, 6),
		u("Zadarska", "Zeleno upozorenje", 1, 6), // zeleno ne ulazi
		u("", "Žuto upozorenje za kišu", 2, 6),   // bez područja ne ulazi
	}, sada)
	if d == nil || d.Ukupno != 5 || d.Razina != 3 || len(d.Podrucja) != 3 {
		t.Fatalf("drugdje: %+v", d)
	}
	if d.Naslov() != "Drugdje u Hrvatskoj · 5 upozorenja u 3 područja" || d.NaslovUsko() != "Drugdje u Hrvatskoj · 5 upozorenja" ||
		d.Najvise != "najviše narančasto: vjetar" {
		t.Errorf("naslov: %q, %q, %q", d.Naslov(), d.NaslovUsko(), d.Najvise)
	}
	ls, vk, is := d.Podrucja[0], d.Podrucja[1], d.Podrucja[2]
	if ls.Naziv != "Ličko-senjska" || ls.Broj != 3 || ls.BrojRijecima() != "3 upozorenja" || ls.Razina != 3 ||
		ls.Sazetak != "narančasto: vjetar · žuto: kiša, vjetar" || ls.URL != "/prognoze/vrijeme?zupanija=LS" || ls.Upozorenja != nil {
		t.Errorf("Ličko-senjska: %+v", ls)
	}
	if vk.Naziv != "Velebitski kanal" || vk.URL != "" || len(vk.Upozorenja) != 1 ||
		vk.Upozorenja[0].Naslov != "DHMZ · Narančasto upozorenje za vjetar" || !strings.HasPrefix(vk.Upozorenja[0].Opis, "Danas od 10:30 do 16:30 h") {
		t.Errorf("pomorsko područje: %+v", vk)
	}
	if is.Naziv != "Istarska" || is.BrojRijecima() != "1 upozorenje" || is.Sazetak != "žuto: grmljavina" || is.URL != "/prognoze/vrijeme?zupanija=IS" {
		t.Errorf("Istarska: %+v", is)
	}
	if upozorenjaDrugdje(nil, sada) != nil || upozorenjaDrugdje([]dhmz.Upozorenje{u("Zadarska", "Zeleno", 1, 1)}, sada) != nil {
		t.Error("bez upozorenja drugdje nema ni retka")
	}
}

// Pojava iz naziva upozorenja ide u nominativ; nepoznata ostaje kakva jest.
func TestPojavaUpozorenja(t *testing.T) {
	for ulaz, treba := range map[string]string{
		"Žuto upozorenje za kišu": "kiša", "Narančasto upozorenje za vjetar": "vjetar", "Crveno upozorenje za visoku temperaturu": "visoka temperatura",
		"Žuto upozorenje za snijeg": "snijeg", "kiša": "kiša", "Žuto upozorenje za obalne događaje": "obalne događaje",
	} {
		if p := pojavaUpozorenja(ulaz); p != treba {
			t.Errorf("%q: %q, treba %q", ulaz, p, treba)
		}
	}
}

// Županija iz adrese je samo poznata oznaka.
func TestZupanijaPoOznaci(t *testing.T) {
	if z, ok := zupanijaPoOznaci("LS"); !ok || z.Naziv != "Ličko-senjska" {
		t.Errorf("LS: %+v", z)
	}
	for _, o := range []string{"", "XX", "ls"} {
		if _, ok := zupanijaPoOznaci(o); ok {
			t.Errorf("%q je prihvaćena", o)
		}
	}
}
