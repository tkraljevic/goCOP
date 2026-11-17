package web

import (
	"context"
	"testing"

	"gocop/internal/models"
	"gocop/internal/service"
)

// „Svi sektori” na pregledu zbraja skladišta svih sektora, a pojedini sektor
// samo svoja (Odluke 16. 11. 2026., 4). Prije se prazan izbor vraćao na
// sektor korisnika, pa „svi” nikad nije bilo moguće vidjeti.
func TestPregledSvihSektora(t *testing.T) {
	o := novaOkolinaSredstava(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('C', 'Sektor C', 'VGO Zagreb', 'COP Zagreb')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (40, 'C', 'Sava', 'COP', 'Zagreb')`,
	} {
		if _, err := o.baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	upravaC := &models.UserPermissions{AdminSectors: map[string]bool{"C": true}, AllowedSectors: map[string]bool{"C": true}}
	skC := &models.Skladiste{Sektor: "C", AreaID: 40, Naziv: "Skladište Sava", Aktivno: true}
	if err := o.svc.SpremiSkladiste(ctx, upravaC, skC); err != nil {
		t.Fatal(err)
	}
	if _, err := o.svc.Provedi(ctx, o.osoba, upravaC, service.Zahvat{Vrsta: models.PrometPocetno, SkladisteID: skC.ID, VrstaID: "lopata", Kolicina: 5}); err != nil {
		t.Fatal(err)
	}

	mora(t, o.ispis(t, upravaSredstava(), "/sredstva?sektor=svi"), "svi sektori", "Sektor=;", "Skladista=2;", "Svi=true;", "SmijeUrediti=false;")
	mora(t, o.ispis(t, upravaSredstava(), "/sredstva?sektor=B"), "sektor B", "Sektor=B;", "Skladista=1;", "Svi=false;", "SmijeUrediti=true;")
	mora(t, o.ispis(t, upravaSredstava(), "/sredstva"), "bez izbora", "Sektor=B;", "Svi=false;")

	sve, err := o.svc.StanjeSektora(ctx, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range sve {
		if r.Vrsta.ID == "lopata" && r.Ukupno != 17 {
			t.Errorf("lopate u svim sektorima: %v, očekivano 17", r.Ukupno)
		}
	}
}

// Čip „Svi sektori” stoji samo na pregledu, prvi po redu; ostale kartice
// rade sa skladištima jednog sektora i ne nose ga dalje.
func TestCipSvihSektora(t *testing.T) {
	d := MtsPageData{Kartica: "pregled", SviSektori: true,
		Sektori: []models.Sector{{ID: "A", Name: "Sektor A"}, {ID: "B", Name: "Sektor B"}}}
	c := d.CipoviSektora()
	if len(c) != 3 || c[0].Naziv != "Svi sektori" || c[0].URL != "/sredstva?sektor=svi" || !c[0].Odabran || c[1].Odabran {
		t.Errorf("čipovi pregleda: %+v", c)
	}
	if u := d.KarticeModula()[1].URL; u != "/sredstva/promet" {
		t.Errorf("knjiga prometa ne smije dobiti „svi”: %s", u)
	}
	d.Kartica, d.SviSektori, d.Sektor = "promet", false, "B"
	if c := d.CipoviSektora(); len(c) != 2 || c[0].Naziv == "Svi sektori" {
		t.Errorf("čipovi knjige prometa: %+v", c)
	}
}
