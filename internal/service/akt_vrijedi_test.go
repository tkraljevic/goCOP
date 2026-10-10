package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Nacrt s „Vrijedi od” prije otvaranja Dnevnika COP-a ili dalje od 7 dana
// unaprijed ne sastavlja se; više od 24 sata unatrag sastavlja se tek uz
// potvrdu, koja ostaje zabilježena uz akt (Odluke (8. 10. 2026.), pitanje 17)
func TestVrijediOdNovogNacrta(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	otvoren := time.Now().Add(-72 * time.Hour).Truncate(time.Hour)
	o.akti.SetObrana(func(context.Context, string) *models.Journal {
		return &models.Journal{ID: "dnevnik-cop", CentarSektor: "P", StartedAt: &otvoren}
	}, nil)
	nacrt := func(vrijedi time.Time, potvrda bool) (*models.Akt, error) {
		t.Helper()
		a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava,
			Stupanj: models.PhasePrep, Vrijedi: vrijedi, PotvrdaVrijedi: potvrda})
		if err != nil {
			t.Fatal(err)
		}
		return a, o.akti.Spremi(ctx, o.dezOvl, a)
	}

	if _, err := nacrt(otvoren.Add(-time.Minute), true); err == nil || !strings.Contains(err.Error(), "prije otvaranja Dnevnika COP-a") {
		t.Errorf("prije otvaranja dnevnika, i uz potvrdu: %v", err)
	}
	if _, err := nacrt(time.Now().AddDate(36, 0, 0), true); err == nil || !strings.Contains(err.Error(), "7 dana unaprijed") {
		t.Errorf("tipfeler u godini, i uz potvrdu: %v", err)
	}
	dvaDana := time.Now().Add(-48 * time.Hour).Truncate(time.Minute)
	var pitanje *service.TrebaPotvrduError
	if _, err := nacrt(dvaDana, false); !errors.As(err, &pitanje) || !strings.Contains(err.Error(), "jeste li sigurni?") {
		t.Fatalf("dva dana unatrag bez potvrde: %v", err)
	}
	a, err := nacrt(dvaDana, true)
	if err != nil {
		t.Fatalf("dva dana unatrag uz potvrdu: %v", err)
	}
	// potvrdu daje tko nacrt sastavlja: dežurni operater (Odluke
	// (8. 10. 2026.), pitanje 18)
	spremljen, _ := o.akti.Get(ctx, a.ID)
	if !spremljen.ImaPotvrdu(models.PotvrdaVrijedi) || spremljen.Potvrde[0].Tko != "Iva Ivić" {
		t.Fatalf("potvrda nije zabilježena uz akt: %+v", spremljen.Potvrde)
	}
	// doradu postojećeg nacrta granice ne priječe, a potvrda ostaje
	if _, err := o.akti.UrediTekst(ctx, o.dezOvl, o.dezurni, a.ID, spremljen.Uvod, "", spremljen.Zavrsno, "dopuna"); err != nil {
		t.Fatal(err)
	}
	if err := o.akti.Spremi(ctx, o.dezOvl, spremljen); err != nil {
		t.Errorf("spremanje postojećeg nacrta: %v", err)
	}
	if poslije, _ := o.akti.Get(ctx, a.ID); !poslije.ImaPotvrdu(models.PotvrdaVrijedi) {
		t.Error("potvrda je nestala nakon dorade nacrta")
	}
	// uobičajeno vrijeme ne traži potvrdu ni je ne bilježi
	obican, err := nacrt(time.Now().Add(-time.Hour).Truncate(time.Minute), true)
	if err != nil || len(obican.Potvrde) != 0 {
		t.Errorf("uobičajeno vrijeme: %+v %v", obican.Potvrde, err)
	}
	// novi nacrt bez „Vrijedi od” javlja što nedostaje, a u preventivnoj
	// obrani ne nastaje ni s ispravnim vremenom
	bez := *obican
	bez.ID, bez.Vrijedi = "", time.Time{}
	if err := o.akti.Spremi(ctx, o.dezOvl, &bez); err == nil || !strings.Contains(err.Error(), "upiši dan i sat") {
		t.Errorf("nacrt bez „Vrijedi od”: %v", err)
	}
	o.akti.SetObrana(func(context.Context, string) *models.Journal { return nil }, nil)
	bez.Vrijedi = time.Now()
	var preventivna service.ErrPreventivnaObrana
	if err := o.akti.Spremi(ctx, o.dezOvl, &bez); !errors.As(err, &preventivna) {
		t.Errorf("nacrt u preventivnoj obrani: %v", err)
	}
}
