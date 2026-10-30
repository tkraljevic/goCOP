package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// autoriProba su računi autora izvješća za test: obrisani su samo u knjizi,
// a za neCita i neProvjerava čitanje vraća grešku
type autoriProba struct {
	racuni               map[uuid.UUID]*models.User
	obrisani             map[uuid.UUID]bool
	neCita, neProvjerava uuid.UUID
}

func (a *autoriProba) GetUserByID(id uuid.UUID) (*models.User, error) {
	if id == a.neCita {
		return nil, errors.New("baza zaključana")
	}
	return a.racuni[id], nil
}

func (a *autoriProba) RacunObrisan(id uuid.UUID) (bool, error) {
	if id == a.neProvjerava {
		return true, errors.New("baza zaključana")
	}
	return a.obrisani[id], nil
}

// Odluke (30. 10. 2026.), 10: predano izvješće dionice kojemu autor više
// ne vodi dionicu mijenja svaki rukovoditelj ili zamjenik te dionice. Dok
// autor vodi dionicu, predano mijenja samo on. Račun koji još nije na ovom
// računalu ili se ne može pročitati izmjenu ne otvara. Osobe su izmišljene.
func TestPredanoIzvjesceKadAutorViseNeVodiDionicu(t *testing.T) {
	sektor, podrucje := "P", 1
	sec := &models.Section{Code: "P.1.1", SectorID: sektor, AreaID: podrucje}
	isteklo := time.Date(2020, 1, 1, 0, 0, 0, 0, models.Zagreb)
	vodi := duznost(models.RoleSectionLeader, models.ScopeSection, sektor, podrucje, "P.1.1")
	zamjenjuje := duznost(models.RoleSectionDeputy, models.ScopeSection, sektor, podrucje, "P.1.1")
	istekla := duznost(models.RoleSectionLeader, models.ScopeSection, sektor, podrucje, "P.1.1")
	istekla.ExpiresAt = &isteklo
	isteklaZamjena := duznost(models.RoleSectionDeputy, models.ScopeSection, sektor, podrucje, "P.1.1")
	isteklaZamjena.ExpiresAt = &isteklo
	ugasena := duznost(models.RoleSectionDeputy, models.ScopeSection, sektor, podrucje, "P.1.1")
	ugasena.IsActive = false
	druga := duznost(models.RoleSectionLeader, models.ScopeSection, sektor, podrucje, "P.1.2")
	vodocuvar := duznost(models.RoleWaterGuard, models.ScopeSection, sektor, podrucje, "P.1.1")

	proba := &autoriProba{racuni: map[uuid.UUID]*models.User{}, obrisani: map[uuid.UUID]bool{}, neCita: uuid.New(), neProvjerava: uuid.New()}
	autor := func(ime string, aktivan bool, d ...models.Duty) string {
		a := &models.User{ID: uuid.New(), FullName: ime, IsActive: aktivan, Duties: d}
		proba.racuni[a.ID] = a
		return a.ID.String()
	}
	obrisan := uuid.New()
	proba.obrisani[obrisan] = true
	s := &IzvjescaService{autori: proba}

	mijenja := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	zOvl := ovlastiOsobe(zamjenjuje)
	rOvl := ovlastiOsobe(vodi)
	vOvl := ovlastiOsobe(vodocuvar)
	uOvl := ovlastiOsobe(duznost(models.RoleAreaLeader, models.ScopeArea, sektor, podrucje, ""))
	sad := time.Now()
	predano := func(izradio string) *models.DnevnoIzvjesce {
		return &models.DnevnoIzvjesce{SectionCode: "P.1.1", IzradioID: izradio, PredanoAt: &sad}
	}
	for _, k := range []struct {
		naziv string
		p     *models.UserPermissions
		iz    *models.DnevnoIzvjesce
		smije bool
	}{
		{"autor i dalje vodi dionicu", zOvl, predano(autor("Ivo Ivić", true, vodi)), false},
		{"autoru je dužnost istekla", zOvl, predano(autor("Ivo Ivić", true, istekla)), true},
		{"autoru je dužnost isključena", zOvl, predano(autor("Ivo Ivić", true, ugasena)), true},
		{"autor sad vodi drugu dionicu", zOvl, predano(autor("Ivo Ivić", true, druga)), true},
		{"autor je sad vodočuvar dionice", zOvl, predano(autor("Ivo Ivić", true, vodocuvar)), true},
		{"autorov račun je isključen", zOvl, predano(autor("Ivo Ivić", false, vodi)), true},
		{"autorov račun je obrisan", zOvl, predano(obrisan.String()), true},
		{"autorov račun još nije stigao", zOvl, predano(uuid.NewString()), false},
		{"brisanje autorova računa se ne može provjeriti", zOvl, predano(proba.neProvjerava.String()), false},
		{"autor nije upisan", zOvl, predano(""), true},
		{"autorov račun se ne može pročitati", zOvl, predano(proba.neCita.String()), false},
		{"rukovoditelj, autor je zamjenik koji vodi dionicu", rOvl, predano(autor("Ivo Ivić", true, zamjenjuje)), false},
		{"rukovoditelj, zamjeniku je dužnost istekla", rOvl, predano(autor("Ivo Ivić", true, isteklaZamjena)), true},
		{"vodočuvar, autor ne vodi dionicu", vOvl, predano(autor("Ivo Ivić", true, istekla)), false},
		{"uprava područja, autor ne vodi dionicu", uOvl, predano(autor("Ivo Ivić", true, istekla)), false},
	} {
		if got := s.SmijeMijenjati(mijenja, k.p, sec, k.iz); got != k.smije {
			t.Errorf("%s: mijenja %v, očekivano %v", k.naziv, got, k.smije)
		}
	}
}

// Isto pravilo kroz spremanje, s računima u bazi: zamjenik ne mijenja
// predano izvješće dok ga rukovoditelj dionice vodi, a mijenja ga kad
// rukovoditelju dužnost prestane. Autor ostaje upisan. Izvješće autora
// čijeg računa na ovom računalu još nema ostaje zatvoreno.
func TestSpremanjePredanogKadAutorViseNeVodiDionicu(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	ctx := context.Background()
	pero := &models.User{ID: o.pero.ID, Username: "pperic", FullName: o.pero.FullName, IsActive: true}
	perina := duznost(models.RoleSectionLeader, models.ScopeSection, "P", 1, "P.1.1")
	if err := o.korisnici.CreateUser(pero, &perina); err != nil {
		t.Fatal(err)
	}
	zamjenik := ovlastiOsobe(duznost(models.RoleSectionDeputy, models.ScopeSection, "P", 1, "P.1.1"))
	drugi := &models.User{ID: uuid.New(), FullName: "Ivo Ivić"}

	iz := punoIzvjesce("P.1.1", danas())
	if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, iz); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.Predaj(ctx, o.pero, o.pisePero, o.d111, iz.ID); err != nil {
		t.Fatalf("rukovoditelj predaje svoje izvješće: %v", err)
	}
	predano, err := o.svc.Get(ctx, iz.ID)
	if err != nil {
		t.Fatal(err)
	}
	izmjena := *predano
	izmjena.Sadrzaj.Pregled = "Zamjenik dopunjuje pregled."
	if err := o.svc.Spremi(ctx, o.urednik, zamjenik, o.d111, &izmjena); !errors.Is(err, ErrPredanoMijenjaAutor) {
		t.Fatalf("zamjenik mijenja predano dok ga rukovoditelj vodi: %v", err)
	}

	if err := o.korisnici.RevokeDuty(perina.ID); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.Spremi(ctx, o.urednik, zamjenik, o.d111, &izmjena); err != nil {
		t.Fatalf("zamjenik mijenja predano kad rukovoditelj više ne vodi dionicu: %v", err)
	}
	g, err := o.svc.Get(ctx, iz.ID)
	if err != nil {
		t.Fatal(err)
	}
	if g.Predano() || g.Sadrzaj.Pregled != "Zamjenik dopunjuje pregled." || g.IzradioID != pero.ID.String() {
		t.Errorf("izmjena zamjenika: predano %v, pregled %q, izradio %s", g.Predano(), g.Sadrzaj.Pregled, g.Izradio)
	}
	if err := o.svc.Predaj(ctx, o.urednik, zamjenik, o.d111, iz.ID); err != nil {
		t.Errorf("zamjenik ponovo predaje: %v", err)
	}

	// zamjenik (urednik) nema račun na ovom računalu: njegovo predano
	// izvješće drugi ne mijenja
	jucer := punoIzvjesce("P.1.1", danas().AddDate(0, 0, -1))
	if err := o.svc.Spremi(ctx, o.urednik, zamjenik, o.d111, jucer); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.Predaj(ctx, o.urednik, zamjenik, o.d111, jucer.ID); err != nil {
		t.Fatal(err)
	}
	tude, err := o.svc.Get(ctx, jucer.ID)
	if err != nil {
		t.Fatal(err)
	}
	tude.Sadrzaj.Pregled = "Drugi zamjenik mijenja."
	if err := o.svc.Spremi(ctx, drugi, zamjenik, o.d111, tude); !errors.Is(err, ErrPredanoMijenjaAutor) {
		t.Errorf("drugi mijenja predano izvješće autora čijeg računa još nema: %v", err)
	}
}

// Predano izvješće bez upisanog autora ne preuzima onaj tko ga prvi
// promijeni: autor ostaje prazan, onaj tko mijenja pamti se među onima
// koji su ga mijenjali, a drugi zamjenik ga i dalje smije mijenjati.
func TestPredanoBezAutoraOstajeBezAutora(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	ctx := context.Background()
	zamjenik := ovlastiOsobe(duznost(models.RoleSectionDeputy, models.ScopeSection, "P", 1, "P.1.1"))
	drugi := &models.User{ID: uuid.New(), FullName: "Ivo Ivić"}

	iz := punoIzvjesce("P.1.1", danas())
	if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, iz); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.Predaj(ctx, o.pero, o.pisePero, o.d111, iz.ID); err != nil {
		t.Fatal(err)
	}
	predano, err := o.svc.Get(ctx, iz.ID)
	if err != nil {
		t.Fatal(err)
	}
	predano.IzradioID, predano.Izradio = "", ""
	if err := o.svc.repo.Save(ctx, predano); err != nil {
		t.Fatal(err)
	}

	for i, tko := range []*models.User{o.urednik, drugi} {
		cur, err := o.svc.Get(ctx, iz.ID)
		if err != nil {
			t.Fatal(err)
		}
		cur.Sadrzaj.Pregled = tko.FullName + " dopunjuje pregled."
		if err := o.svc.Spremi(ctx, tko, zamjenik, o.d111, cur); err != nil {
			t.Fatalf("%s mijenja predano bez autora: %v", tko.FullName, err)
		}
		g, err := o.svc.Get(ctx, iz.ID)
		if err != nil {
			t.Fatal(err)
		}
		if g.IzradioID != "" || g.Izradio != "" || len(g.TragPredaje.Izmijenili) != i+1 || g.TragPredaje.Izmijenili[i].ID != tko.ID.String() {
			t.Errorf("nakon izmjene %s: izradio %q %q, mijenjali %v", tko.FullName, g.IzradioID, g.Izradio, g.TragPredaje.Izmijenili)
		}
	}

	// nacrt bez autora koji nikad nije predan preuzima onaj tko ga mijenja
	nacrt := punoIzvjesce("P.1.1", danas().AddDate(0, 0, -1))
	if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, nacrt); err != nil {
		t.Fatal(err)
	}
	nacrt.IzradioID, nacrt.Izradio = "", ""
	if err := o.svc.repo.Save(ctx, nacrt); err != nil {
		t.Fatal(err)
	}
	nacrt.Sadrzaj.Pregled = "Zamjenik dovršava nacrt."
	if err := o.svc.Spremi(ctx, drugi, zamjenik, o.d111, nacrt); err != nil {
		t.Fatal(err)
	}
	if g, err := o.svc.Get(ctx, nacrt.ID); err != nil || g.IzradioID != drugi.ID.String() {
		t.Errorf("nacrt bez autora: izradio %v (%v)", g, err)
	}
}
