package service_test

import (
	"context"
	"crypto/ed25519"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Nacrt s drugog čvora ne vraća ovjeren akt u nacrt. Laptop ovjeri nacrt
// kao P-1/2026, a poslužitelj, koji ovjeru još nije primio, isti nacrt
// ispravi, ponovno mu složi primatelje ili ga obriše. Razmjena u bilo kojem
// redu ostavlja akt ovjeren i potpisan na svim čvorovima: objavljena oznaka
// ne nestaje i laptop njegov broj ne daje ponovno (Odluke (8. 10. 2026.),
// pitanje 4)
func TestNacrtSDrugogCvoraNeVracaOvjerenAktUNacrt(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		opis     string
		izmijeni func(o *okolinaAkta, id string) error
	}{
		{"ispravljen tekst", func(o *okolinaAkta, id string) error {
			_, err := o.akti.UrediTekst(ctx, o.dezOvl, o.dezurni, id, "Uvod s poslužitelja.", "", "Završna rečenica.", "napomena s poslužitelja")
			return err
		}},
		{"ponovno složeni primatelji", func(o *okolinaAkta, id string) error {
			_, err := o.akti.PonovnoSloziPrimatelje(ctx, o.dezOvl, o.dezurni, id)
			return err
		}},
		{"obrisan nacrt", func(o *okolinaAkta, id string) error {
			return o.akti.Obrisi(ctx, o.ovlasti, o.rukovod, id)
		}},
	} {
		t.Run(c.opis, func(t *testing.T) {
			laptop, posluzitelj, treci := novaOkolinaAkta(t), novaOkolinaAkta(t), novaOkolinaAkta(t)
			_, k, _ := ed25519.GenerateKey(nil)
			laptop.akti.SetKljuc(k)
			a, err := laptop.akti.Pripremi(ctx, laptop.dezOvl, laptop.dezurni, service.ZahtjevAkta{StationID: laptop.letva.ID.String(),
				Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Vrijedi: time.Now().Add(-time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			if err := laptop.akti.Spremi(ctx, laptop.dezOvl, a); err != nil {
				t.Fatal(err)
			}
			prenesiAkte(t, laptop, posluzitelj)
			ovjeren, _, err := laptop.akti.Ovjeri(ctx, laptop.ovlasti, laptop.rukovod, a.ID, potvrdaPrimatelja)
			if err != nil || ovjeren.Broj != 1 {
				t.Fatalf("ovjera na laptopu: %v %+v", err, ovjeren)
			}
			if err := c.izmijeni(posluzitelj, a.ID); err != nil {
				t.Fatalf("izmjena nacrta na poslužitelju: %v", err)
			}
			ostajeOvjeren := func(cvor string, o *okolinaAkta) {
				t.Helper()
				x, err := o.akti.Get(ctx, a.ID)
				if err != nil || x == nil || !x.Ovjeren() || x.Oznaka() != ovjeren.Oznaka() || x.Potpis != ovjeren.Potpis ||
					service.ProvjeriPotpis(x) != service.PotpisVrijedi || x.Napomena != ovjeren.Napomena {
					t.Fatalf("%s: ovjeren akt nakon razmjene: %v %+v", cvor, err, x)
				}
			}

			prenesiAkte(t, posluzitelj, laptop)
			ostajeOvjeren("laptop", laptop)
			prenesiAkte(t, laptop, posluzitelj)
			ostajeOvjeren("poslužitelj", posluzitelj)
			prenesiAkte(t, laptop, treci) // sve verzije u jednoj razmjeni
			ostajeOvjeren("treći čvor", treci)

			if _, _, err := laptop.akti.Ovjeri(ctx, laptop.ovlasti, laptop.rukovod, a.ID, potvrdaPrimatelja); err == nil {
				t.Error("ovjeren akt ovjeren je ponovno")
			}
			if sljedeci, _ := laptop.ovjeri(t, models.AktPrekid, models.PhasePrep, time.Now().Add(-30*time.Minute)); sljedeci.Broj != 2 {
				t.Errorf("sljedeći broj na laptopu: %s", sljedeci.Oznaka())
			}
		})
	}
}
