package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Tvrde granice „Vrijedi od” (Odluke (8. 10. 2026.), pitanje 17) vrijede i
// pri ovjeri, na oba puta (u goCOP-u i skenom): nacrt koji ih ne poštuje ne
// postaje akt. Takav nacrt nastaje kad ga je sastavio čvor sa starijim
// programom, koji granice ne provodi, ili kad je Dnevnik COP-a u međuvremenu
// zatvoren i otvoren nov. Potvrda za više od 24 sata unatrag ili 3 dana
// unaprijed pri ovjeri se ne traži ponovno.
func TestTvrdeGraniceVrijediOdPriOvjeri(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	otvoren := time.Now().Add(-72 * time.Hour).Truncate(time.Hour)
	dnevnik := func(od time.Time) func(context.Context, string) *models.Journal {
		return func(context.Context, string) *models.Journal {
			return &models.Journal{ID: "dnevnik-cop", CentarSektor: "P", StartedAt: &od}
		}
	}
	o.akti.SetObrana(dnevnik(otvoren), nil)
	sken := []byte("%PDF-1.4 sken potpisanog akta")
	nijeOvjeren := func(opis, id string) {
		t.Helper()
		if a, _ := o.akti.Get(ctx, id); a == nil || a.Ovjeren() || a.Broj != 0 {
			t.Errorf("%s: nacrt ne smije postati akt: %+v", opis, a)
		}
		if pdf, _ := o.akti.Izvornik(ctx, id); len(pdf) != 0 {
			t.Errorf("%s: sken ne smije ostati kao izvornik", opis)
		}
	}

	// nacrt čvora sa starijim programom: godina 2062. umjesto 2026.
	stari := &models.Akt{Sektor: "P", AreaID: 1, Radnja: models.AktUspostava, Stupanj: models.PhasePrep,
		StationID: o.letva.ID.String(), StationName: o.letva.Name, Dionice: []models.AktDionica{{Code: "P.1.1"}},
		Vrijedi: time.Now().AddDate(36, 0, 0).Truncate(time.Minute), Status: models.AktNacrt, Godina: 2062,
		Potpisnik: "Rukovoditelj obrane od poplava za branjeno područje 1", Uvod: "uvod", Zavrsno: "završno",
		IzradioID: o.rukovod.ID.String(), Izradio: o.rukovod.FullName, IzradenoAt: time.Now()}
	if err := repository.NewAktiRepository(o.baza, ledger.New(o.baza, "stari-cvor")).SaveAkt(ctx, stari); err != nil {
		t.Fatal(err)
	}
	if _, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, stari.ID, potvrdaPrimatelja); err == nil || !strings.Contains(err.Error(), "više od 7 dana unaprijed") {
		t.Errorf("ovjera nacrta s „Vrijedi od” 2062.: %v", err)
	}
	if _, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, stari.ID, sken, o.rukovod.ID.String(), potvrdaPrimatelja); err == nil || !strings.Contains(err.Error(), "više od 7 dana unaprijed") {
		t.Errorf("sken nacrta s „Vrijedi od” 2062.: %v", err)
	}
	nijeOvjeren("2062.", stari.ID)

	// nacrt iz obrane čiji je dnevnik zatvoren; sada je otvoren nov
	dvaDana := time.Now().Add(-48 * time.Hour).Truncate(time.Minute)
	a, err := o.akti.Pripremi(ctx, o.ovlasti, o.rukovod, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava,
		Stupanj: models.PhasePrep, Vrijedi: dvaDana, PotvrdaVrijedi: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.akti.Spremi(ctx, o.ovlasti, a); err != nil {
		t.Fatal(err)
	}
	o.akti.SetObrana(dnevnik(time.Now().Add(-time.Hour)), nil)
	if _, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, a.ID, potvrdaPrimatelja); err == nil || !strings.Contains(err.Error(), "prije otvaranja Dnevnika COP-a") ||
		!strings.Contains(err.Error(), "novi nacrt") {
		t.Errorf("ovjera nacrta iz prošle obrane: %v", err)
	}
	if _, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, a.ID, sken, o.rukovod.ID.String(), potvrdaPrimatelja); err == nil || !strings.Contains(err.Error(), "prije otvaranja Dnevnika COP-a") {
		t.Errorf("sken nacrta iz prošle obrane: %v", err)
	}
	nijeOvjeren("prošla obrana", a.ID)

	// u istoj obrani isti nacrt ovjerava se bez nove potvrde, iako je
	// „Vrijedi od” sada dva dana unatrag
	o.akti.SetObrana(dnevnik(otvoren), nil)
	ovjeren, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, a.ID, potvrdaPrimatelja)
	if err != nil || !ovjeren.Ovjeren() || ovjeren.Godina != dvaDana.In(models.Zagreb).Year() {
		t.Fatalf("ovjera nacrta u istoj obrani: %+v %v", ovjeren, err)
	}
}
