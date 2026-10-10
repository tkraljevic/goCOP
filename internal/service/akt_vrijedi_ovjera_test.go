package service_test

import (
	"context"
	"errors"
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
// unaprijed dana pri sastavljanju nacrta pri ovjeri se ne traži ponovno.
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
	a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava,
		Stupanj: models.PhasePrep, Vrijedi: dvaDana, PotvrdaVrijedi: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.akti.Spremi(ctx, o.dezOvl, a); err != nil {
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

// Nacrt sastavljen bez potvrde „Vrijedi od” (tada u uobičajenom rasponu, ili
// s čvora sa starijim programom) ovjerava se kad je „Vrijedi od” više od 24
// sata unatrag ili više od 3 dana unaprijed: ovjera tada traži izričitu
// potvrdu, na oba puta, i bilježi je uz akt (Odluke (8. 10. 2026.),
// pitanje 17)
func TestPotvrdaVrijediOdPriOvjeri(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	sken := []byte("%PDF-1.4 sken potpisanog akta")
	nacrt := func(o *okolinaAkta, stupanj models.DefensePhase, vrijedi time.Time) *models.Akt {
		t.Helper()
		a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava,
			Stupanj: stupanj, Vrijedi: time.Now().Add(-time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if err := o.akti.Spremi(ctx, o.dezOvl, a); err != nil {
			t.Fatal(err)
		}
		// nacrt je sastavljen davno: „Vrijedi od” je tada bio sada
		if _, err := o.baza.Exec(`UPDATE akti SET vrijedi = ? WHERE id = ?`, vrijedi.UTC(), a.ID); err != nil {
			t.Fatal(err)
		}
		return a
	}
	dvaDana := time.Now().Add(-48 * time.Hour).Truncate(time.Minute)
	a := nacrt(o, models.PhasePrep, dvaDana)
	for opis, ovjeri := range map[string]func(service.PotvrdaOvjere) error{
		"u goCOP-u": func(p service.PotvrdaOvjere) error {
			_, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, a.ID, p)
			return err
		},
		"skenom": func(p service.PotvrdaOvjere) error {
			_, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, a.ID, sken, o.rukovod.ID.String(), p)
			return err
		},
	} {
		var p *service.TrebaPotvrduError
		if err := ovjeri(potvrdaPrimatelja); !errors.As(err, &p) || !strings.Contains(p.Pitanje, "jeste li sigurni") {
			t.Errorf("%s: ovjera bez potvrde „Vrijedi od” dva dana unatrag: %v", opis, err)
		}
		if x, _ := o.akti.Get(ctx, a.ID); x.Ovjeren() || x.Broj != 0 {
			t.Errorf("%s: nacrt bez potvrde ne smije postati akt: %+v", opis, x)
		}
	}
	if pdf, _ := o.akti.Izvornik(ctx, a.ID); len(pdf) != 0 {
		t.Error("sken bez potvrde ne smije ostati kao izvornik")
	}
	ovjeren, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, a.ID, service.PotvrdaOvjere{Primatelji: true, Vrijedi: true})
	if err != nil || !ovjeren.Ovjeren() || !ovjeren.ImaPotvrdu(models.PotvrdaVrijedi) {
		t.Fatalf("ovjera uz potvrdu: %+v %v", ovjeren, err)
	}
	if x, _ := o.akti.Get(ctx, a.ID); !x.ImaPotvrdu(models.PotvrdaVrijedi) || x.Potvrde[0].Tko != "Pero Perić" || !strings.Contains(x.Potvrde[0].Tekst, "jeste li sigurni") {
		t.Errorf("potvrda pri ovjeri nije zabilježena uz akt: %+v", x.Potvrde)
	}

	// potvrda pri ovjeri skenom bilježi se kao i u goCOP-u; „Vrijedi od” u
	// uobičajenom rasponu potvrdu ne traži
	b := nacrt(o, models.PhaseRegular, time.Now().Add(5*24*time.Hour).Truncate(time.Minute))
	if x, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, b.ID, sken, o.rukovod.ID.String(), service.PotvrdaOvjere{Primatelji: true, Vrijedi: true}); err != nil || !x.ImaPotvrdu(models.PotvrdaVrijedi) {
		t.Errorf("sken uz potvrdu: %+v %v", x, err)
	}
	druga := novaOkolinaAkta(t)
	c := nacrt(druga, models.PhasePrep, time.Now().Add(-2*time.Hour).Truncate(time.Minute))
	if x, _, err := druga.akti.Ovjeri(ctx, druga.ovlasti, druga.rukovod, c.ID, potvrdaPrimatelja); err != nil || x.ImaPotvrdu(models.PotvrdaVrijedi) {
		t.Errorf("ovjera u uobičajenom rasponu: %+v %v", x, err)
	}
	// pitanje za stranicu nacrta
	d := nacrt(o, models.PhaseEmergency, dvaDana)
	for _, c := range []struct {
		opis string
		o    *okolinaAkta
		id   string
		pita bool
	}{{"nacrt dva dana unatrag", o, d.ID, true}, {"ovjeren uz potvrdu", o, a.ID, false}, {"ovjeren u rasponu", druga, c.ID, false}} {
		x, _ := c.o.akti.Get(ctx, c.id)
		if q := service.PitanjeVrijediZaOvjeru(*x, time.Now()); strings.Contains(q, "jeste li sigurni") != c.pita {
			t.Errorf("%s: pitanje %q", c.opis, q)
		}
	}
	x, _ := o.akti.Get(ctx, d.ID)
	x.Potvrde = []models.PotvrdaAkta{{Vrsta: models.PotvrdaVrijedi}}
	if q := service.PitanjeVrijediZaOvjeru(*x, time.Now()); q != "" {
		t.Errorf("nacrt s potvrdom pri sastavljanju ne pita ponovno: %q", q)
	}
}

// Nacrt sastavljen u obrani koja traje od otvaranja jednog Dnevnika COP-a
// ovjerava se i kad je u sektoru u međuvremenu otvoren i noviji dnevnik
// (npr. godišnji, dok stari još nije zaključen): donja granica „Vrijedi
// od” je otvaranje najranije otvorenog dnevnika, i pri sastavljanju i pri
// ovjeri (Odluke (8. 10. 2026.), pitanje 17)
func TestVrijediOdUzDvaOtvorenaDnevnikaCOPa(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	repo := repository.NewJournalRepository(o.baza, ledger.New(o.baza, "test"))
	js := service.NewJournalService(repo, nil, nil)
	o.akti.SetObrana(js.AktivnaObrana, nil)
	o.akti.SetPocetakObrane(js.PocetakAktivneObrane)
	otvori := func(od time.Time) {
		t.Helper()
		if err := repo.SaveJournal(ctx, &models.Journal{CentarSektor: "P", Title: "Dnevnik COP-a", Year: od.Year(), StartedAt: &od}); err != nil {
			t.Fatal(err)
		}
	}
	otvori(time.Now().Add(-10 * 24 * time.Hour).Truncate(time.Hour))
	nacrt := func(vrijedi time.Time) (*models.Akt, error) {
		a, err := o.akti.Pripremi(ctx, o.ovlasti, o.rukovod, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava,
			Stupanj: models.PhasePrep, Vrijedi: vrijedi, PotvrdaVrijedi: true})
		if err != nil {
			t.Fatal(err)
		}
		return a, o.akti.Spremi(ctx, o.ovlasti, a)
	}
	a, err := nacrt(time.Now().Add(-5 * 24 * time.Hour).Truncate(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	otvori(time.Now().Add(-2 * time.Hour).Truncate(time.Minute))
	if ovjeren, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, a.ID, potvrdaPrimatelja); err != nil || !ovjeren.Ovjeren() {
		t.Fatalf("ovjera nacrta iz obrane koja traje: %+v %v", ovjeren, err)
	}
	if _, err := nacrt(time.Now().Add(-6 * 24 * time.Hour).Truncate(time.Minute)); err != nil {
		t.Errorf("nov nacrt u obrani koja traje: %v", err)
	}
	if _, err := nacrt(time.Now().Add(-11 * 24 * time.Hour).Truncate(time.Minute)); err == nil || !strings.Contains(err.Error(), "prije otvaranja Dnevnika COP-a") {
		t.Errorf("nacrt prije otvaranja obrane: %v", err)
	}
}
