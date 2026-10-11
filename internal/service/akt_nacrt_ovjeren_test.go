package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Nacrt pročitan prije ovjere ne upisuje se preko akta koji je u
// međuvremenu ovjeren: ovjeren akt vratio bi se u nacrt bez broja i
// potpisa, a čvor bi njegov broj dao ponovno (Odluke (8. 10. 2026.),
// pitanje 4). Druga veza na bazu drži ovjeru upisanu, a nepotvrđenu, dok
// radnja s nacrtom čita nacrt; upis nacrta tada čeka bazu, a ovjera se
// potvrdi prije njega, kao kad druga osoba u tom trenutku klikne „Ovjeri”.
func TestNacrtSeNeUpisujePrekoAktaOvjerenogUMedjuvremenu(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		opis  string
		upisi func(o *okolinaAkta, a *models.Akt) error
	}{
		{"ponovno složeni primatelji", func(o *okolinaAkta, a *models.Akt) error {
			_, err := o.akti.PonovnoSloziPrimatelje(ctx, o.dezOvl, o.dezurni, a.ID)
			return err
		}},
		{"ispravljen tekst", func(o *okolinaAkta, a *models.Akt) error {
			_, err := o.akti.UrediTekst(ctx, o.dezOvl, o.dezurni, a.ID, "Uvod.", "", "Završna rečenica.", "napomena")
			return err
		}},
		{"ponovno spremljen nacrt", func(o *okolinaAkta, a *models.Akt) error {
			return o.akti.Spremi(ctx, o.dezOvl, a)
		}},
	} {
		t.Run(c.opis, func(t *testing.T) {
			o := novaOkolinaAkta(t)
			a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, service.ZahtjevAkta{StationID: o.letva.ID.String(),
				Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Vrijedi: time.Now().Add(-time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			if err := o.akti.Spremi(ctx, o.dezOvl, a); err != nil {
				t.Fatal(err)
			}
			verzija := func() int {
				var n int
				if err := o.baza.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = ? AND entity_id = ?`, repository.EntityAkti, a.ID).Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			}
			ovjera, err := o.baza.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ovjera.Rollback()
			if _, err := ovjera.Exec(`UPDATE akti SET status = 'OVJEREN', broj = 1, potpis = 'potpis čvora' WHERE id = ?`, a.ID); err != nil {
				t.Fatal(err)
			}
			prije := verzija()
			potvrdena := make(chan error, 1)
			go func() {
				time.Sleep(300 * time.Millisecond)
				potvrdena <- ovjera.Commit()
			}()
			if err := c.upisi(o, a); err == nil || !strings.Contains(err.Error(), "ovjeren") {
				t.Errorf("upis nacrta preko ovjerenog akta mora biti odbijen: %v", err)
			}
			if err := <-potvrdena; err != nil {
				t.Fatal(err)
			}
			procitan, err := o.akti.Get(ctx, a.ID)
			if err != nil || procitan == nil || !procitan.Ovjeren() || procitan.Broj != 1 || procitan.Potpis != "potpis čvora" {
				t.Fatalf("ovjeren akt vraćen je u nacrt: %+v %v", procitan, err)
			}
			if n := verzija(); n != prije {
				t.Errorf("odbijen upis nacrta ostavio je verziju u knjizi: %d prema %d", n, prije)
			}
		})
	}
}
