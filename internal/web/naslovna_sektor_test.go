package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// dozvoleUpraveSektora su prava osobe u upravi sektora B
func dozvoleUpraveSektora(uloga models.Role) *models.UserPermissions {
	p := dozvoleNaslovne(false, uloga)
	p.User.Duties[0].SectorID = nizNaslovne("B")
	return p
}

// Uprava sektora (rukovoditelj sektora, voditelj COP-a i zamjenici) ima
// Naslovnu sektora, i kad je usto dežurni; bez sektora nema
func TestDuznostSektora(t *testing.T) {
	for _, uloga := range []models.Role{models.RoleSectorLeader, models.RoleSectorDeputy, models.RoleCopLeader, models.RoleCopDeputy} {
		p := dozvoleUpraveSektora(uloga)
		if ulogaNaslovne(p, sadNaslovne) != uSektor || sektorOsobe(p, sadNaslovne) != "B" || karticeUloge(p)[0] != kStanjeSektora {
			t.Errorf("%s: %s %v", uloga, ulogaNaslovne(p, sadNaslovne), karticeUloge(p))
		}
	}
	p := dozvoleUpraveSektora(models.RoleCopLeader)
	p.User.Duties = append(p.User.Duties, models.Duty{Role: models.RoleOperator, IsActive: true, SectorID: nizNaslovne("C")})
	if ulogaNaslovne(p, sadNaslovne) != uSektor || sektorOsobe(p, sadNaslovne) != "B" {
		t.Error("voditelj COP-a koji je i dežurni")
	}
	if d := duznostSektora(&dozvoleNaslovne(false, models.RoleSectorLeader).User, sadNaslovne); d != nil {
		t.Error("dužnost bez sektora")
	}
	if s := sektorOsobe(dozvoleNaslovne(false, models.RoleWaterGuard), sadNaslovne); s != "" {
		t.Errorf("vodočuvar ima sektor %q", s)
	}
}

// Akt na potpis: stupanj, područje i tko je sastavio nacrt
func TestRedakAkta(t *testing.T) {
	r := redakAkta(models.Akt{ID: "a1", Stupanj: models.PhasePrep, AreaID: 34, Izradio: "Iva Proba"})
	if r.Glavno != "Akt čeka potpis" || r.Sporedno != "pripremno stanje na BP 34 · nacrt: Iva Proba" || r.URL != "/akti/a1" || !r.Hitno {
		t.Errorf("uspostava: %+v", r)
	}
	if r := redakAkta(models.Akt{Radnja: models.AktPrekid, Stupanj: models.PhaseRegular}); r.Sporedno != "prekid: redovna obrana" {
		t.Errorf("prekid: %+v", r)
	}
}

// Naslovna uprave sektora kroz rukovatelja: područja s rukovoditeljem i
// ljudima na terenu, Za riješiti sektora, sve letve sektora
func TestNaslovnaUpraveSektora(t *testing.T) {
	o := novaOkolinaNaslovne(t)
	ruk := dozvoleUpraveSektora(models.RoleSectorLeader)
	sad := time.Now()
	ivo := models.User{ID: uuid.New(), FullName: "Ivo Proba", Duties: []models.Duty{{Role: models.RoleAreaLeader, IsActive: true, AreaID: brojNaslovne(34)}}}
	objavljeno := sad.Add(-time.Hour)
	o.h.SetIzvori(IzvoriNaslovne{
		StanjaSektora: func(context.Context, string) (map[string]models.StanjeObrane, []models.Section, error) {
			return nil, nil, nil
		},
		Podrucja: func(string) ([]models.Area, error) {
			return []models.Area{{ID: 16, Name: "Mali sliv Baranja"}, {ID: 34}}, nil
		},
		Osobe: func(string, int, models.Role) ([]models.User, error) { return []models.User{ivo}, nil },
		Listovi: func(context.Context, *models.UserPermissions, repository.FiltarListova) ([]models.VodocuvarskiList, error) {
			return []models.VodocuvarskiList{{UserID: "a", AreaID: 34, Datum: sad}, {UserID: "a", AreaID: 34, Datum: sad}, {UserID: "b", AreaID: 34, Datum: sad}, {UserID: "c", AreaID: 16, Datum: sad.AddDate(0, 0, -1)}}, nil
		},
		AktiNaPotpis: func(context.Context, *models.UserPermissions, string) ([]models.Akt, error) {
			return []models.Akt{{ID: "a1", Stupanj: models.PhasePrep, AreaID: 34}}, nil
		},
		Nepredani: func(context.Context, *models.UserPermissions, string) ([]models.VodocuvarskiList, error) {
			return []models.VodocuvarskiList{{Datum: sad.AddDate(0, 0, -2)}}, nil
		},
		Prijave: func(context.Context, *models.UserPermissions, repository.FiltarPrijava) ([]models.PrijavaSTerena, error) {
			return []models.PrijavaSTerena{{ID: "p1", Vrsta: models.PrijavaPrijava, Naslov: "procjeđivanje", ObjavljenoAt: &objavljeno}}, nil
		},
		Letve: func(context.Context) ([]models.GaugeSummary, error) {
			return []models.GaugeSummary{
				{Name: "Osijek", StationID: "o", SectorIDs: []string{"B"}, Phase: models.PhaseNormal, Latest: &models.Reading{LevelCm: cmNaslovne(105), MeasuredAt: sad}},
				{Name: "Sisak", StationID: "s", SectorIDs: []string{"C"}, Phase: models.PhasePrep, Latest: &models.Reading{LevelCm: cmNaslovne(600), MeasuredAt: sad}},
				objekt("CS Draž", models.StructureKindPumpingStation, 16, &models.Reading{LevelCm: cmNaslovne(186), MeasuredAt: sad}, nil),
			}, nil
		},
	})
	s := o.zovi("/", ruk).Body.String()
	for _, zeli := range []string{"Stanje obrane · Sektor B", "Ivo Proba · 2 na terenu", "Dnevnik COP-a nije otvoren"} {
		if !strings.Contains(s, zeli) {
			t.Errorf("Naslovna uprave sektora nema %q", zeli)
		}
	}
	if strings.Contains(s, "Moje dežurstvo") {
		t.Error("uprava sektora ima karticu dežurstva")
	}
	w := o.zovi("/naslovna/kartica/za-rijesiti", ruk).Body.String()
	for _, zeli := range []string{"Akt čeka potpis", "1 dnevni list nije predan", "Prijava: procjeđivanje"} {
		if !strings.Contains(w, zeli) {
			t.Errorf("Za riješiti nema %q: %s", zeli, w)
		}
	}
	if w := o.zovi("/naslovna/kartica/vodostaji", ruk).Body.String(); !strings.Contains(w, "Osijek") || strings.Contains(w, "Sisak") || strings.Contains(w, "CS Draž") {
		t.Errorf("vodostaji sektora: %s", w)
	}
	if k := vodostajiSektora(nil, nil, "B", sad); k.Prazno == nil || k.Prazno.Tekst != "U zadnja tri dana nema očitanja letava sektora B." {
		t.Errorf("bez letava: %+v", k)
	}
	bez := dozvoleNaslovne(false)
	if k := o.h.karticaDezurnog(zahtjevSDozvolama(bez), bez, &DashboardViewData{}, kDnevnikCOP); k.Prazno == nil {
		t.Errorf("bez dužnosti u sektoru: %+v", k)
	}
	if n := (&DashboardHandler{}).naTerenuPoPodrucju(context.Background(), ruk, "B", sad); len(n) != 0 {
		t.Errorf("na terenu bez dnevnika: %v", n)
	}
}
