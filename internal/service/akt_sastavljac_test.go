package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Akt o stupnju obrane izdaje se isključivo u Centru obrane od poplava:
// nacrt sastavljaju samo voditelj centra, njegov zamjenik i Dežurni
// operater u COP-u sektora akta, s valjanom dužnošću. Nitko drugi, ni oni
// koji akt ovjeravaju; ovjera ostaje kakva je (Odluke (8. 10. 2026.),
// pitanje 18).

// osobaSDuznoscu je djelatnik s jednom dužnošću, bez upisa u bazu: pravo
// sastavljanja gleda samo dužnosti
func osobaSDuznoscu(ime string, d models.Duty) (*models.User, *models.UserPermissions) {
	d.IsActive = true
	u := &models.User{ID: uuid.New(), FullName: ime, IsActive: true, Duties: []models.Duty{d}}
	return u, models.NewUserPermissions(*u)
}

func TestNacrtAktaSastavljaSamoCentarObraneSektora(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	sektor, drugi, podrucje := "P", "R", 1
	proslo := time.Now().Add(-time.Hour)
	zahtjev := service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhasePrep,
		Vrijedi: time.Now().Add(-time.Hour).Truncate(time.Minute)}

	smiju := map[string]models.Duty{
		"voditelj centra":                {Role: models.RoleCopLeader, ScopeType: models.ScopeSector, SectorID: &sektor},
		"zamjenik voditelja centra":      {Role: models.RoleCopDeputy, ScopeType: models.ScopeSector, SectorID: &sektor},
		"voditelj ili zamjenik (ranije)": {Role: models.RoleAreaAdmin, ScopeType: models.ScopeSector, SectorID: &sektor},
		"dežurni operater":               {Role: models.RoleOperator, ScopeType: models.ScopeSector, SectorID: &sektor},
		"dežurni operater uz područje":   {Role: models.RoleOperator, ScopeType: models.ScopeSector, SectorID: &sektor, AreaID: &podrucje},
	}
	for ime, d := range smiju {
		u, perms := osobaSDuznoscu("Ivo Ivić", d)
		if !service.SmijeSastaviti(perms, "P") || !service.SastavljaNacrte(perms) || !o.akti.SmijeAktZaLetvu(ctx, perms, o.letva, models.PhasePrep) {
			t.Errorf("%s: mora smjeti sastaviti nacrt u sektoru P", ime)
		}
		a, err := o.akti.Pripremi(ctx, perms, u, zahtjev)
		if err != nil {
			t.Errorf("%s: priprema: %v", ime, err)
			continue
		}
		if err := o.akti.Spremi(ctx, perms, a); err != nil {
			t.Errorf("%s: spremanje: %v", ime, err)
		}
		if service.SmijeSastaviti(perms, "R") {
			t.Errorf("%s: sastavlja i u tuđem sektoru R", ime)
		}
	}

	ne := map[string]models.Duty{
		"vodočuvar dionice":                     {Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, SectorID: &sektor, AreaID: &podrucje, SectionCodes: "P.1.1"},
		"rukovoditelj dionice":                  {Role: models.RoleSectionLeader, ScopeType: models.ScopeSection, SectorID: &sektor, AreaID: &podrucje, SectionCodes: "P.1.1"},
		"zamjenik rukovoditelja dionice":        {Role: models.RoleSectionDeputy, ScopeType: models.ScopeSection, SectorID: &sektor, AreaID: &podrucje, SectionCodes: "P.1.1"},
		"rukovoditelj branjenog područja":       {Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje},
		"zamjenik rukovoditelja područja":       {Role: models.RoleAreaDeputy, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje},
		"rukovoditelj sektora":                  {Role: models.RoleSectorLeader, ScopeType: models.ScopeSector, SectorID: &sektor},
		"zamjenik rukovoditelja sektora":        {Role: models.RoleSectorDeputy, ScopeType: models.ScopeSector, SectorID: &sektor},
		"zamjenik glavnog za sektor":            {Role: models.RoleSectorMainDeputy, ScopeType: models.ScopeSector, SectorID: &sektor},
		"zamjenik sektora za područje":          {Role: models.RoleSectorAreaDeputy, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje},
		"ovlaštenik A2":                         {Role: models.RoleContractOfficerA2, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje},
		"ovlaštenik A3":                         {Role: models.RoleContractOfficerA3, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje},
		"skladištar":                            {Role: models.RoleWarehouseKeeper, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje},
		"voditelj usluga / poslovođa":           {Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje},
		"glavni rukovoditelj":                   {Role: models.RoleNationalLeader, ScopeType: models.ScopeAll},
		"voditelj Glavnog centra":               {Role: models.RoleMainCenterLeader, ScopeType: models.ScopeAll},
		"zamjenik voditelja Glavnog centra":     {Role: models.RoleMainCenterDeputy, ScopeType: models.ScopeAll},
		"gost":                                  {Role: models.RoleGuest, ScopeType: models.ScopeAll},
		"dežurni operater drugog sektora":       {Role: models.RoleOperator, ScopeType: models.ScopeSector, SectorID: &drugi},
		"dežurni operater, dužnost istekla":     {Role: models.RoleOperator, ScopeType: models.ScopeSector, SectorID: &sektor, ExpiresAt: &proslo},
		"voditelj centra drugog sektora":        {Role: models.RoleCopLeader, ScopeType: models.ScopeSector, SectorID: &drugi},
		"voditelj centra bez upisanog sektora":  {Role: models.RoleCopLeader, ScopeType: models.ScopeSector},
		"zamjenik voditelja centra bez sektora": {Role: models.RoleCopDeputy, ScopeType: models.ScopeSector, SectorID: new(string)},
	}
	_, dezurni := osobaSDuznoscu("Iva Ivić", smiju["dežurni operater"])
	nacrt, err := o.akti.Pripremi(ctx, dezurni, o.dezurni, zahtjev)
	if err != nil {
		t.Fatal(err)
	}
	provjeriOdbijen := func(ime string, u *models.User, perms *models.UserPermissions) {
		t.Helper()
		if service.SmijeSastaviti(perms, "P") || o.akti.SmijeAktZaLetvu(ctx, perms, o.letva, models.PhasePrep) {
			t.Errorf("%s: ne smije sastaviti nacrt", ime)
		}
		if _, err := o.akti.Pripremi(ctx, perms, u, zahtjev); !errors.Is(err, service.ErrUnauthorized) || !strings.Contains(err.Error(), "Centar obrane od poplava sektora P") {
			t.Errorf("%s: priprema mora biti odbijena s objašnjenjem: %v", ime, err)
		}
		novi := *nacrt
		if err := o.akti.Spremi(ctx, perms, &novi); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("%s: spremanje novog nacrta: %v", ime, err)
		}
	}
	for ime, d := range ne {
		u, perms := osobaSDuznoscu("Marko Markić", d)
		provjeriOdbijen(ime, u, perms)
		// centar drugog sektora sastavlja samo u svom sektoru
		if svoj := d.SectorID != nil && *d.SectorID == drugi; service.SastavljaNacrte(perms) != svoj || service.SmijeSastaviti(perms, drugi) != svoj {
			t.Errorf("%s: sastavlja nacrte %v, u sektoru R %v", ime, service.SastavljaNacrte(perms), service.SmijeSastaviti(perms, drugi))
		}
	}

	// dužnost centra koja nije aktivna ne daje ništa
	u, neaktivna := osobaSDuznoscu("Marko Markić", smiju["dežurni operater"])
	neaktivna.User.Duties[0].IsActive = false
	provjeriOdbijen("dežurni operater, dužnost neaktivna", u, neaktivna)

	// ni uprava organizacije ni globalni administrator bez dužnosti u centru
	admin := &models.User{ID: uuid.New(), FullName: "Pero Perić", IsGlobalAdmin: true, IsActive: true}
	provjeriOdbijen("globalni administrator", admin, models.NewUserPermissions(*admin))
	provjeriOdbijen("uprava organizacije", o.rukovod, &models.UserPermissions{IsGlobalAdmin: true, User: *o.rukovod})
	if service.SastavljaNacrte(nil) || service.SmijeSastaviti(nil, "P") || service.SmijeSastaviti(dezurni, "") || o.akti.SmijeAktZaLetvu(ctx, nil, o.letva, models.PhasePrep) {
		t.Error("bez ovlasti ili bez sektora nitko ne sastavlja")
	}

	// globalni administrator koji je i dežurni operater sastavlja po toj dužnosti
	admin.Duties = []models.Duty{{Role: models.RoleOperator, ScopeType: models.ScopeSector, SectorID: &sektor, IsActive: true}}
	if _, err := o.akti.Pripremi(ctx, models.NewUserPermissions(*admin), admin, zahtjev); err != nil {
		t.Errorf("administrator s dužnošću dežurnog: %v", err)
	}
}

// Postojeći nacrt mijenja samo njegov autor, i to dok smije sastavljati
// nacrte; inače se nacrt samo ovjerava, kao dosad (Odluke (8. 10. 2026.),
// pitanje 18). Brisanje nacrta ostaje kakvo je bilo: autor ili tko ga smije
// ovjeriti.
func TestNacrtMijenjaSamoAutorDokSastavlja(t *testing.T) {
	ctx := context.Background()
	nacrt := func(t *testing.T) (*okolinaAkta, *models.Akt) {
		t.Helper()
		o := novaOkolinaAkta(t)
		a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, service.ZahtjevAkta{StationID: o.letva.ID.String(),
			Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Vrijedi: time.Now().Add(-time.Hour).Truncate(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		if err := o.akti.Spremi(ctx, o.dezOvl, a); err != nil {
			t.Fatal(err)
		}
		return o, a
	}
	uredi := func(o *okolinaAkta, perms *models.UserPermissions, u *models.User, id string) error {
		_, err := o.akti.UrediTekst(ctx, perms, u, id, "Uvod.", "", "Završna rečenica.", "napomena")
		return err
	}
	slozi := func(o *okolinaAkta, perms *models.UserPermissions, u *models.User, id string) error {
		_, err := o.akti.PonovnoSloziPrimatelje(ctx, perms, u, id)
		return err
	}
	spremi := func(o *okolinaAkta, perms *models.UserPermissions, _ *models.User, id string) error {
		a, err := o.akti.Get(ctx, id)
		if err != nil {
			return err
		}
		return o.akti.Spremi(ctx, perms, a)
	}
	sektor, drugi := "P", "R"
	proslo := time.Now().Add(-time.Minute)

	for ime, izmjena := range map[string]func(o *okolinaAkta, perms *models.UserPermissions, u *models.User, id string) error{
		"ispravak teksta": uredi, "ponovno složeni primatelji": slozi, "ponovno spremljen nacrt": spremi,
	} {
		t.Run(ime, func(t *testing.T) {
			o, a := nacrt(t)
			if !service.SmijeUreditiNacrt(o.dezOvl, o.dezurni, a) {
				t.Error("autor dok sastavlja smije mijenjati nacrt")
			}
			if err := izmjena(o, o.dezOvl, o.dezurni, a.ID); err != nil {
				t.Fatalf("autor dok sastavlja: %v", err)
			}

			// drugi iz istog centra, rukovoditelj koji ovjerava i vodočuvar ne
			kolega, kolegaOvl := osobaSDuznoscu("Ivo Ivić", models.Duty{Role: models.RoleCopDeputy, ScopeType: models.ScopeSector, SectorID: &sektor})
			for tko, z := range map[string]struct {
				perms *models.UserPermissions
				u     *models.User
			}{
				"zamjenik voditelja centra, nije autor": {kolegaOvl, kolega},
				"rukovoditelj koji ovjerava":            {o.ovlasti, o.rukovod},
				"vodočuvar dionice akta":                {o.vodOvl, o.vodocuv},
			} {
				if service.SmijeUreditiNacrt(z.perms, z.u, a) {
					t.Errorf("%s: ne mijenja tuđi nacrt", tko)
				}
				if err := izmjena(o, z.perms, z.u, a.ID); !errors.Is(err, service.ErrUnauthorized) {
					t.Errorf("%s: %v", tko, err)
				}
			}

			// autor kojemu je dužnost istekla, ili je sada u drugom centru, ne
			istekla := *o.dezurni
			istekla.Duties = []models.Duty{{Role: models.RoleOperator, ScopeType: models.ScopeSector, SectorID: &sektor, IsActive: true, ExpiresAt: &proslo}}
			premjesten := *o.dezurni
			premjesten.Duties = []models.Duty{{Role: models.RoleOperator, ScopeType: models.ScopeSector, SectorID: &drugi, IsActive: true}}
			for tko, u := range map[string]*models.User{"istekla dužnost": &istekla, "dežurni drugog sektora": &premjesten} {
				if service.SmijeUreditiNacrt(models.NewUserPermissions(*u), u, a) {
					t.Errorf("autor, %s: ne mijenja nacrt", tko)
				}
				if err := izmjena(o, models.NewUserPermissions(*u), u, a.ID); !errors.Is(err, service.ErrUnauthorized) {
					t.Errorf("autor, %s: %v", tko, err)
				}
			}
		})
	}

	t.Run("ovjera i brisanje ostaju kakvi su bili", func(t *testing.T) {
		o, a := nacrt(t)
		if service.SmijeUreditiNacrt(o.dezOvl, nil, a) || service.SmijeUreditiNacrt(o.dezOvl, o.dezurni, nil) {
			t.Error("bez osobe ili bez nacrta nema izmjene")
		}
		istekla := *o.dezurni
		istekla.Duties = []models.Duty{{Role: models.RoleOperator, ScopeType: models.ScopeSector, SectorID: &sektor, IsActive: true, ExpiresAt: &proslo}}
		if err := o.akti.Obrisi(ctx, models.NewUserPermissions(istekla), &istekla, a.ID); err != nil {
			t.Errorf("autor briše svoj nacrt i kad više ne sastavlja: %v", err)
		}
		o, a = nacrt(t)
		if o.akti.SmijeOvjeriti(o.dezOvl, a) {
			t.Error("dežurni operater nacrt sastavlja, a ne ovjerava")
		}
		if _, _, err := o.akti.Ovjeri(ctx, o.dezOvl, o.dezurni, a.ID, potvrdaPrimatelja); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("ovjera dežurnog operatera: %v", err)
		}
		ovjeren, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, a.ID, potvrdaPrimatelja)
		if err != nil || !ovjeren.Ovjeren() || ovjeren.Izradio != "Iva Ivić" || ovjeren.Ovjerio != "Pero Perić" {
			t.Fatalf("rukovoditelj područja ovjerava nacrt centra: %+v %v", ovjeren, err)
		}
	})
}
