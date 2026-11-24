package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Kad automatika u vrijeme akta ne pokazuje dobro, vodostaj i vrijeme
// upisuju se rukom i imaju prednost pred odabranim očitanjem. Tendencija je
// prema zadnjem očitanju letve prije upisanog vremena. Bez vremena, s
// vremenom u budućnosti ili s nemogućim vodostajem nacrta nema. Brojevi su
// izmišljeni.
func TestAktSRukomUpisanimVodostajem(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	sad := time.Now().Truncate(time.Minute)
	// okolina ima očitanja 310 cm prije tri sata i 320 cm prije dva sata
	rucno := func(v int, kad time.Time) service.ZahtjevAkta {
		return service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhasePrep,
			Vrijedi: sad.Add(-time.Hour), RucniCm: &v, RucnoKad: kad, OcitanjeID: uuid.NewString()}
	}

	a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, rucno(330, sad.Add(-90*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if a.VodostajCm == nil || *a.VodostajCm != 330 || !a.VodostajKad.Equal(sad.Add(-90*time.Minute)) || a.Tendencija != models.TendencijaPorast {
		t.Errorf("rukom upisan vodostaj: %v u %v, tendencija %s", a.VodostajCm, a.VodostajKad, a.Tendencija)
	}
	if a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, rucno(320, sad.Add(-90*time.Minute))); err != nil || a.Tendencija != models.TendencijaStagnacija {
		t.Errorf("isti vodostaj kao prije: %v %v", err, a)
	}
	if a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, rucno(250, sad.Add(-200*24*time.Hour))); err != nil || a.Tendencija != models.TendencijaStagnacija {
		t.Errorf("prije svih očitanja tendencija je stagnacija: %v %v", err, a)
	}

	for _, slucaj := range []struct {
		z      service.ZahtjevAkta
		poruka string
	}{
		{rucno(320, time.Time{}), "upišite i vrijeme"},
		{rucno(320, sad.Add(time.Hour)), "u budućnosti"},
		{rucno(5000, sad.Add(-time.Hour)), "nije moguć"},
	} {
		if _, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, slucaj.z); err == nil || !strings.Contains(err.Error(), slucaj.poruka) {
			t.Errorf("očekivana poruka %q, a dobio %v", slucaj.poruka, err)
		}
	}
}
