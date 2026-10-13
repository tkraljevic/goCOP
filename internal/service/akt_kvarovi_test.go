package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// izvrsi izvršava upit nad bazom okoline akata
func (o *okolinaAkta) izvrsi(t *testing.T, upit string, args ...any) {
	t.Helper()
	if _, err := o.baza.Exec(upit, args...); err != nil {
		t.Fatalf("%v\n%s", err, upit)
	}
}

// nacrtDezurnog priprema i sprema nacrt dežurnog operatera za letvu okoline
func (o *okolinaAkta) nacrtDezurnog(t *testing.T, radnja string, vrijedi time.Time) *models.Akt {
	t.Helper()
	ctx := context.Background()
	a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: radnja,
		Stupanj: models.PhasePrep, Vrijedi: vrijedi})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.akti.Spremi(ctx, o.dezOvl, a); err != nil {
		t.Fatal(err)
	}
	return a
}

// bezOvlasti je osoba bez ijedne dužnosti
func bezOvlasti() (*models.User, *models.UserPermissions) {
	u := models.User{ID: uuid.New(), Username: "nnovak", FullName: "Nikola Novak", IsActive: true}
	return &u, models.NewUserPermissions(u)
}

// Nacrt akta: bez osobe nema nacrta; letva koje nema ili koja se ne da
// pročitati, i očitanja koja se ne daju pročitati, javljaju se. Odabrano
// očitanje mora biti očitanje letve. Tendencija se računa prema očitanju
// prije odabranoga, a izričito zadana ima prednost.
func TestPripremaAktaRubovi(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	zahtjev := service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhasePrep,
		Vrijedi: time.Now().Add(-time.Hour).Truncate(time.Minute)}
	if _, err := o.akti.Pripremi(ctx, nil, o.dezurni, zahtjev); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("nacrt bez ovlasti: %v", err)
	}
	if _, err := o.akti.Pripremi(ctx, o.dezOvl, nil, zahtjev); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("nacrt bez osobe: %v", err)
	}
	nema := zahtjev
	nema.StationID = uuid.NewString()
	if _, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, nema); err == nil || !strings.Contains(err.Error(), "vodomjer ne postoji") {
		t.Errorf("letva koje nema: %v", err)
	}
	tudje := zahtjev
	tudje.OcitanjeID = uuid.NewString()
	if _, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, tudje); err == nil || !strings.Contains(err.Error(), "nije među očitanjima vodomjera Primjerovo") {
		t.Errorf("tuđe očitanje: %v", err)
	}

	cm := 305
	pad := models.Reading{ID: uuid.New(), StationID: o.letva.ID.String(), MeasuredAt: time.Now().Add(-30 * time.Minute), LevelCm: &cm,
		Source: models.ReadingSourceManual}
	if err := repository.NewReadingRepository(o.baza, ledger.New(o.baza, "test")).Create(ctx, &pad); err != nil {
		t.Fatal(err)
	}
	a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, zahtjev)
	if err != nil || a.VodostajCm == nil || *a.VodostajCm != 305 || a.Tendencija != models.TendencijaOpadanje {
		t.Errorf("nacrt uz pad vodostaja: %+v %v", a, err)
	}
	zadana := zahtjev
	zadana.Tendencija = models.TendencijaPorast
	if a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, zadana); err != nil || a.Tendencija != models.TendencijaPorast {
		t.Errorf("zadana tendencija: %+v %v", a, err)
	}

	o.izvrsi(t, `UPDATE readings SET measured_at = 'nije vrijeme' WHERE id = ?`, pad.ID.String())
	if _, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, zahtjev); err == nil {
		t.Error("nacrt s očitanjem koje se ne da pročitati")
	}
	o.izvrsi(t, `UPDATE stations SET created_at = 'nije vrijeme' WHERE id = ?`, o.letva.ID.String())
	if _, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, zahtjev); err == nil || strings.Contains(err.Error(), "ne postoji") {
		t.Errorf("letva koja se ne da pročitati: %v", err)
	}
}

// Ovjera skenom: akt mora postojati, osoba ga smije pripremiti, sken je
// PDF ili slika, a potpisnik je osoba koja postoji i smije ovjeriti akt.
// Izvornik koji baza ne primi ne ovjerava akt.
func TestOvjeraSkenomKadNestoNeValja(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	sken := []byte("%PDF-1.4 potpisani akt")
	a := o.nacrtDezurnog(t, models.AktUspostava, time.Now().Add(-time.Hour).Truncate(time.Minute))
	ruk := o.rukovod.ID.String()
	nitko, nitkoOvl := bezOvlasti()
	for _, slucaj := range []struct {
		naziv     string
		perms     *models.UserPermissions
		u         *models.User
		id        string
		datoteka  []byte
		potpisnik string
		poruka    string
	}{
		{"akt kojeg nema", o.ovlasti, o.rukovod, uuid.NewString(), sken, ruk, "akt ne postoji"},
		{"bez prava na akt", nitkoOvl, nitko, a.ID, sken, ruk, service.ErrUnauthorized.Error()},
		{"sken nije PDF", o.ovlasti, o.rukovod, a.ID, []byte("nije ni PDF ni slika"), ruk, "sken mora biti PDF"},
		{"potpisnik nije odabran", o.ovlasti, o.rukovod, a.ID, sken, "", "odaberite tko je akt potpisao"},
		{"potpisnika nema", o.ovlasti, o.rukovod, a.ID, sken, uuid.NewString(), "potpisnik ne postoji"},
		{"potpisnik ne smije ovjeriti", o.ovlasti, o.rukovod, a.ID, sken, o.vodocuv.ID.String(), "po zaduženjima ne smije ovjeriti"},
	} {
		if _, _, err := o.akti.UcitajSkenirani(ctx, slucaj.perms, slucaj.u, slucaj.id, slucaj.datoteka, slucaj.potpisnik, potvrdaPrimatelja); err == nil ||
			!strings.Contains(err.Error(), slucaj.poruka) {
			t.Errorf("%s: %v", slucaj.naziv, err)
		}
	}

	o.izvrsi(t, `CREATE TRIGGER zabrana BEFORE INSERT ON akti_izvornici BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`)
	if _, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, a.ID, sken, ruk, potvrdaPrimatelja); err == nil || !strings.Contains(err.Error(), "zabranjeno") {
		t.Errorf("izvornik koji baza ne primi: %v", err)
	}
	if x, err := o.akti.Get(ctx, a.ID); err != nil || x.Ovjeren() {
		t.Errorf("akt nakon neuspjelog skena: %+v %v", x, err)
	}

	o.izvrsi(t, `UPDATE akti SET izradeno_at = 'nije vrijeme' WHERE id = ?`, a.ID)
	if _, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, a.ID, sken, ruk, potvrdaPrimatelja); err == nil || strings.Contains(err.Error(), "ne postoji") {
		t.Errorf("akt koji se ne da pročitati: %v", err)
	}
}

// Nacrt, tekst i ovjera akta kojeg nema, koji je ovjeren ili se ne da
// pročitati: ništa se ne mijenja, a poruka kaže zašto. Uvod i završna
// rečenica ne smiju biti prazni; tekst izvan snage ima samo akt o prekidu.
func TestNacrtITekstAktaRubovi(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	sat := time.Now().Add(-3 * time.Hour).Truncate(time.Minute)
	ovjeren, _ := o.ovjeri(t, models.AktUspostava, models.PhasePrep, sat)
	prekid := o.nacrtDezurnog(t, models.AktPrekid, sat.Add(time.Hour))

	if err := o.akti.Spremi(ctx, nil, prekid); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("spremanje bez ovlasti: %v", err)
	}
	if err := o.akti.Spremi(ctx, o.dezOvl, ovjeren); err == nil || !strings.Contains(err.Error(), "je ovjeren i ne mijenja se") {
		t.Errorf("spremanje ovjerenog akta: %v", err)
	}
	bezDionica := *prekid
	bezDionica.ID, bezDionica.Dionice = "", nil
	if err := o.akti.Spremi(ctx, o.dezOvl, &bezDionica); err == nil || !strings.Contains(err.Error(), "akt bez dionica") {
		t.Errorf("nacrt bez dionica: %v", err)
	}

	if _, err := o.akti.UrediTekst(ctx, o.dezOvl, o.dezurni, uuid.NewString(), "Uvod", "", "Završno", ""); err == nil || !strings.Contains(err.Error(), "akt ne postoji") {
		t.Errorf("tekst akta kojeg nema: %v", err)
	}
	if _, err := o.akti.UrediTekst(ctx, o.dezOvl, o.dezurni, ovjeren.ID, "Uvod", "", "Završno", ""); err == nil || !strings.Contains(err.Error(), "je ovjeren i ne mijenja se") {
		t.Errorf("tekst ovjerenog akta: %v", err)
	}
	if _, err := o.akti.UrediTekst(ctx, o.dezOvl, o.dezurni, prekid.ID, "  ", "", "Završno", ""); err == nil || !strings.Contains(err.Error(), "ne smiju biti prazni") {
		t.Errorf("prazan uvod: %v", err)
	}
	a, err := o.akti.UrediTekst(ctx, o.dezOvl, o.dezurni, prekid.ID, " Novi uvod ", "  Akt od jutros prestaje važiti.  ", " Završna rečenica. ", " napomena ")
	if err != nil || a.Uvod != "Novi uvod" || a.Zavrsno != "Završna rečenica." || a.IzvanSnage != "Akt od jutros prestaje važiti." || a.Napomena != "napomena" {
		t.Errorf("tekst prekida: %+v %v", a, err)
	}
	if x, err := o.akti.Get(ctx, prekid.ID); err != nil || x.IzvanSnage != "Akt od jutros prestaje važiti." {
		t.Errorf("spremljen tekst prekida: %+v %v", x, err)
	}

	if _, _, err := o.akti.Ovjeri(ctx, nil, o.rukovod, prekid.ID, potvrdaPrimatelja); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("ovjera bez ovlasti: %v", err)
	}
	if _, _, err := o.akti.Ovjeri(ctx, o.ovlasti, nil, prekid.ID, potvrdaPrimatelja); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("ovjera bez osobe: %v", err)
	}
	if _, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, uuid.NewString(), potvrdaPrimatelja); err == nil || !strings.Contains(err.Error(), "akt ne postoji") {
		t.Errorf("ovjera akta kojeg nema: %v", err)
	}

	o.izvrsi(t, `UPDATE akti SET izradeno_at = 'nije vrijeme' WHERE id = ?`, prekid.ID)
	for naziv, err := range map[string]error{
		"spremanje": o.akti.Spremi(ctx, o.dezOvl, prekid),
		"tekst": func() error {
			_, err := o.akti.UrediTekst(ctx, o.dezOvl, o.dezurni, prekid.ID, "Uvod", "", "Završno", "")
			return err
		}(),
		"ovjera": func() error {
			_, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, prekid.ID, potvrdaPrimatelja)
			return err
		}(),
	} {
		if err == nil || strings.Contains(err.Error(), "ne postoji") {
			t.Errorf("%s akta koji se ne da pročitati: %v", naziv, err)
		}
	}
}

// Primatelja upisuje uprava, s nazivom
func TestPrimateljTraziUpravuINaziv(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	if err := o.akti.SpremiPrimatelja(ctx, o.vodOvl, &models.Primatelj{Sektor: "P", AreaID: 1, Naziv: "Općina Probna"}); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("primatelj bez uprave: %v", err)
	}
	if err := o.akti.SpremiPrimatelja(ctx, nil, &models.Primatelj{Sektor: "P", AreaID: 1, Naziv: "Općina Probna"}); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("primatelj bez ovlasti: %v", err)
	}
	if err := o.akti.SpremiPrimatelja(ctx, o.ovlasti, &models.Primatelj{Sektor: "P", AreaID: 1, Naziv: "   "}); err == nil || !strings.Contains(err.Error(), "upiši naziv primatelja") {
		t.Errorf("primatelj bez naziva: %v", err)
	}
}

// Epizode obrane svih dionica letve vide se na stranici letve, svaka s
// opisom svoje dionice. Do 13. 10. 2026. upit je čitao naziv dionice iz
// stupca kojeg nema, pa nije vraćao ništa.
func TestEpizodeLetve(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	if e, err := o.epizode.ByStation(ctx, o.letva.ID.String(), 10); err != nil || len(e) != 0 {
		t.Errorf("epizode prije obrane: %+v %v", e, err)
	}
	o.ovjeri(t, models.AktUspostava, models.PhasePrep, time.Now().Add(-time.Hour).Truncate(time.Minute))
	e, err := o.epizode.ByStation(ctx, o.letva.ID.String(), 10)
	if err != nil || len(e) != 2 {
		t.Fatalf("epizode letve: %+v %v", e, err)
	}
	opisi := map[string]string{"P.1.1": "rijeka Primjerica", "P.1.2": "kanal Probni"}
	for _, x := range e {
		if x.Phase != models.PhasePrep || x.SectionName == "" || x.SectionName != opisi[x.SectionCode] {
			t.Errorf("epizoda letve: dionica %q (%q), stupanj %s", x.SectionCode, x.SectionName, x.Phase)
		}
	}
}
