package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// izvodjacIDezurni je izvor zaduženja testa: izvođač ima dužnost poslovođe
// otprije, a svi ostali dužnost dežurnog u sektoru P
func izvodjacIDezurni(izvodjac uuid.UUID) func(uuid.UUID) ([]models.VerzijaZaduzenja, error) {
	ostali := duznostUSektoruOdUvijek("P")
	return func(id uuid.UUID) ([]models.VerzijaZaduzenja, error) {
		if id != izvodjac {
			return ostali(id)
		}
		p, prvo := "P", 1
		d := models.Duty{ID: id, UserID: id, Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, SectorID: &p, AreaID: &prvo, IsActive: true}
		return []models.VerzijaZaduzenja{{Duty: d, Od: time.Date(2020, 1, 1, 0, 0, 0, 0, models.Zagreb)}}, nil
	}
}

// izvodjacDezura postavlja izvođača kao dežurnog od prije dva sata, kao da
// je ostao upisan iz ranijeg izdanja programa
func (o *okolinaDezurstava) izvodjacDezura(t *testing.T, izvodjac *models.User) {
	t.Helper()
	od := time.Now().In(models.Zagreb).Add(-2 * time.Hour)
	o.dnevnik.DezurniID, o.dnevnik.DezurniIme, o.dnevnik.DezurniOd = izvodjac.ID.String(), izvodjac.FullName, &od
	if err := o.svc.repo.SaveJournal(context.Background(), o.dnevnik); err != nil {
		t.Fatal(err)
	}
}

// Kad uprava centra preda smjenu izvođača, u dnevniku ostaje zapis, ali
// izvođač ne ulazi u plan ni u obračun sati. Smjenu djelatnika uprava
// predaje kao i dosad, s razmakom u planu (Odluke (30. 10. 2026.)). Podaci
// su izmišljeni.
func TestPredanaSmjenaIzvodjacaNeUlaziUPlan(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	izvodjac := &models.User{ID: uuid.New(), FullName: "Ivo Izvođač"}
	o.svc.SetPovijestZaduzenja(izvodjacIDezurni(izvodjac.ID))
	o.izvodjacDezura(t, izvodjac)
	if err := o.svc.PredajDezurstvo(ctx, o.uprava, o.ovlUprav, o.dnevnik); err != nil {
		t.Fatalf("uprava centra ne predaje smjenu izvođača: %v", err)
	}
	if plan, err := o.svc.Dezurstva(ctx, o.dnevnik.ID); err != nil || len(plan) != 0 {
		t.Errorf("smjena izvođača ušla je u plan: %+v, %v", plan, err)
	}
	if zapisi, err := o.svc.EntriesForJournal(ctx, o.dnevnik.ID); err != nil || len(zapisi) != 1 {
		t.Errorf("zapis predaje u dnevniku: %d, %v", len(zapisi), err)
	}

	// kontrola: smjena djelatnika ulazi u plan, potvrđena jer je predala uprava
	od := time.Now().In(models.Zagreb).Add(-2 * time.Hour)
	o.dnevnik.DezurniID, o.dnevnik.DezurniIme, o.dnevnik.DezurniOd = o.pperic.ID.String(), o.pperic.FullName, &od
	if err := o.svc.PredajDezurstvo(ctx, o.uprava, o.ovlUprav, o.dnevnik); err != nil {
		t.Fatal(err)
	}
	plan, err := o.svc.Dezurstva(ctx, o.dnevnik.ID)
	if err != nil || len(plan) != 1 || plan[0].UserID != o.pperic.ID.String() || !plan[0].Potvrdeno() {
		t.Errorf("smjena djelatnika u planu: %+v, %v", plan, err)
	}
}

// Dok dežura izvođač, dežurstvo preuzima samo uprava centra: ona zaključi
// njegovu smjenu, bez razmaka u planu. Operater sektora dobiva poruku da
// smjenu najprije preda uprava (Odluke (30. 10. 2026.)). Podaci su
// izmišljeni.
func TestDezurstvoOdIzvodjacaPreuzimaUpravaCentra(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	izvodjac := &models.User{ID: uuid.New(), FullName: "Ivo Izvođač"}
	o.svc.SetPovijestZaduzenja(izvodjacIDezurni(izvodjac.ID))
	o.izvodjacDezura(t, izvodjac)
	if err := o.svc.PreuzmiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik); err == nil || !strings.Contains(err.Error(), "predaje voditelj ili zamjenik centra") {
		t.Errorf("operater preuzima dežurstvo od izvođača: %v", err)
	}
	if o.dnevnik.DezurniID != izvodjac.ID.String() {
		t.Fatal("odbijeno preuzimanje promijenilo je dežurnog")
	}
	if zapisi, err := o.svc.EntriesForJournal(ctx, o.dnevnik.ID); err != nil || len(zapisi) != 0 {
		t.Errorf("odbijeno preuzimanje upisalo je zapis: %d, %v", len(zapisi), err)
	}

	// kontrola: uprava centra preuzima, smjena izvođača ne ulazi u plan
	if err := o.svc.PreuzmiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, o.dnevnik); err != nil {
		t.Fatalf("uprava centra ne preuzima dežurstvo od izvođača: %v", err)
	}
	if o.dnevnik.DezurniID != o.uprava.ID.String() {
		t.Error("dežura netko drugi, a ne uprava")
	}
	if plan, err := o.svc.Dezurstva(ctx, o.dnevnik.ID); err != nil || len(plan) != 0 {
		t.Errorf("smjena izvođača ušla je u plan: %+v, %v", plan, err)
	}
	// kontrola: od djelatnika operater preuzima kao i dosad
	if err := o.svc.PreuzmiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik); err != nil {
		t.Errorf("operater ne preuzima dežurstvo od uprave: %v", err)
	}
}

// Upis poslovođe izvođača koji je ostao u planu od prije uprava ne
// potvrđuje i ne ispravlja mu opis; upis djelatnika potvrđuje kao i dosad.
// Greška čitanja dužnosti odbija potvrdu (Odluke (30. 10. 2026.)). Podaci su
// izmišljeni.
func TestUpravaNePotvrdujeUpisIzvodjaca(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	izvodjac := uuid.New()
	stari := &models.Dezurstvo{ID: uuid.NewString(), JournalID: o.dnevnik.ID, UserID: izvodjac.String(), UserName: "Ivo Izvođač",
		Od: dzSat(2, 8, 0), Do: dzSat(2, 16, 0), Opis: models.OpisiRada[0].Opis}
	if err := o.svc.repo.SaveDezurstvo(ctx, stari); err != nil {
		t.Fatal(err)
	}
	o.svc.SetPovijestZaduzenja(izvodjacIDezurni(izvodjac))
	if err := o.svc.PotvrdiDezurstvo(ctx, o.uprava, o.ovlUprav, o.dnevnik, stari.ID, ""); err == nil || !strings.Contains(err.Error(), "maknite ga iz plana") {
		t.Errorf("uprava potvrđuje upis izvođača: %v", err)
	}
	drugiOpis := models.OpisiRada[len(models.OpisiRada)-1].Opis
	if _, err := o.svc.IspraviOpisDezurstva(ctx, o.uprava, o.ovlUprav, o.dnevnik, stari.ID, drugiOpis, ""); err == nil {
		t.Error("uprava ispravlja opis upisa izvođača")
	}
	if d, err := o.svc.repo.GetDezurstvo(ctx, stari.ID); err != nil || d.Potvrdeno() || d.Opis != stari.Opis {
		t.Errorf("upis izvođača promijenjen: %+v, %v", d, err)
	}

	// kontrola: upis djelatnika uprava potvrđuje
	pero := o.peroDezura(dzSat(3, 8, 0), dzSat(3, 16, 0))
	pero.ID, pero.JournalID = uuid.NewString(), o.dnevnik.ID
	if err := o.svc.repo.SaveDezurstvo(ctx, pero); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.PotvrdiDezurstvo(ctx, o.uprava, o.ovlUprav, o.dnevnik, pero.ID, ""); err != nil {
		t.Errorf("uprava ne potvrđuje upis djelatnika: %v", err)
	}

	// dužnosti se ne daju pročitati: potvrda se odbija
	o.svc.SetPovijestZaduzenja(func(uuid.UUID) ([]models.VerzijaZaduzenja, error) { return nil, errors.New("baza zaključana") })
	if err := o.svc.PotvrdiDezurstvo(ctx, o.uprava, o.ovlUprav, o.dnevnik, stari.ID, ""); err == nil {
		t.Error("potvrda prošla uz grešku čitanja dužnosti")
	}
}

// Bez valjanog identiteta ili bez izvora dužnosti osoba se ne gleda kao
// izvođač, pa plan radi kao i dosad; greška čitanja se javlja.
func TestOsobaJeIzvodjacBezIzvora(t *testing.T) {
	s := &JournalService{}
	if izv, err := s.osobaJeIzvodjac(uuid.NewString()); izv || err != nil {
		t.Errorf("bez izvora: %v, %v", izv, err)
	}
	s.SetPovijestZaduzenja(func(uuid.UUID) ([]models.VerzijaZaduzenja, error) { return nil, errors.New("baza zaključana") })
	if izv, err := s.osobaJeIzvodjac("nije-identitet"); izv || err != nil {
		t.Errorf("neispravan identitet: %v, %v", izv, err)
	}
	if _, err := s.osobaJeIzvodjac(uuid.NewString()); err == nil {
		t.Error("greška čitanja dužnosti nije javljena")
	}
	// kontrola: poslovođa iz izvora jest izvođač
	id := uuid.New()
	s.SetPovijestZaduzenja(izvodjacIDezurni(id))
	if izv, err := s.osobaJeIzvodjac(id.String()); !izv || err != nil {
		t.Errorf("poslovođa nije izvođač: %v, %v", izv, err)
	}
}

// Bez pročitanih ovlasti dežurstvo se ne predaje, a poruka ne kaže da je
// osoba izvođač.
func TestPredajaBezOvlastiImaSvojuPoruku(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	od := time.Now().In(models.Zagreb).Add(-time.Hour)
	o.dnevnik.DezurniID, o.dnevnik.DezurniIme, o.dnevnik.DezurniOd = o.pperic.ID.String(), o.pperic.FullName, &od
	err := o.svc.PredajDezurstvo(context.Background(), o.pperic, nil, o.dnevnik)
	if err == nil || strings.Contains(err.Error(), "izvođač") || !strings.Contains(err.Error(), "ovlasti") {
		t.Errorf("predaja bez ovlasti: %v", err)
	}
	// kontrola: s ovlastima dežurni predaje svoju smjenu
	if err := o.svc.PredajDezurstvo(context.Background(), o.pperic, o.ovlPero, o.dnevnik); err != nil {
		t.Errorf("dežurni ne predaje svoju smjenu: %v", err)
	}
}

// U dnevniku COP-a nadzor (storno tuđeg zapisa) ima svatko tko u njega
// piše, i osoba kojoj je dužnost poslovođe istekla, a ima drugu dužnost u
// sektoru. Tko danas ima važeću dužnost poslovođe nije nadzor (Odluke
// (30. 10. 2026.)). Podaci su izmišljeni.
func TestNadzorUCOPuPoVazecojDuznostiPoslovodje(t *testing.T) {
	s := &JournalService{}
	p, prvo := "P", 1
	cop := &models.Journal{ID: "cop", Kind: models.JournalKindDefense, CentarSektor: "P"}
	o := models.OpsegDnevnika(*cop, nil, []models.Area{{ID: 1, SectorID: "P"}})
	istekla := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	rukovoditelj := models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, SectorID: &p, AreaID: &prvo, IsActive: true}
	poslovodja := models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, SectorID: &p, AreaID: &prvo, IsActive: true}
	bivsa := poslovodja
	bivsa.ExpiresAt = &istekla
	bivsi := models.NewUserPermissions(models.User{ID: uuid.New(), Duties: []models.Duty{bivsa, rukovoditelj}})
	if !s.CanSupervise(&bivsi.User, bivsi, o, cop) {
		t.Error("osoba s isteklom dužnošću poslovođe nije nadzor u dnevniku COP-a")
	}
	sadasnji := models.NewUserPermissions(models.User{ID: uuid.New(), Duties: []models.Duty{poslovodja, rukovoditelj}})
	if s.CanSupervise(&sadasnji.User, sadasnji, o, cop) {
		t.Error("poslovođa izvođača je nadzor u dnevniku COP-a")
	}
	if s.CanSupervise(nil, nil, o, cop) {
		t.Error("osoba bez ovlasti je nadzor u dnevniku COP-a")
	}
}

// Dnevnik usluge bez svog područja, ili s područjem drugačijim od onoga po
// kojem se gleda, ne daje upravi nadzor ni kad ovlaštenika nema; dnevnik
// svog područja daje (Odluke (30. 10. 2026.)). Podaci su izmišljeni.
func TestNadzorUslugeSamoNaPodrucjuDnevnika(t *testing.T) {
	s := &JournalService{}
	pitano := -1
	s.SetImaOvlastenika(func(podrucje int, program string) (bool, error) {
		pitano = podrucje
		return false, nil
	})
	podrucje := 15
	ruk := models.NewUserPermissions(models.User{ID: uuid.New(), Duties: []models.Duty{{Role: models.RoleAreaLeader, AreaID: &podrucje, IsActive: true}}})
	o := models.Opseg{Sektor: "B", Podrucje: 15, Podrucja: []int{15}}
	for _, j := range []*models.Journal{
		{ID: "bez", Kind: models.JournalKindMaintenanceA02},
		{ID: "drugo", Kind: models.JournalKindMaintenanceA02, AreaID: 16},
	} {
		if s.CanSupervise(&ruk.User, ruk, o, j) {
			t.Errorf("uprava područja 15 je nadzor u dnevniku %s (područje %d)", j.ID, j.AreaID)
		}
	}
	if pitano != -1 {
		t.Errorf("za dnevnik bez svog područja pitano je područje %d", pitano)
	}
	// kontrola: dnevnik područja 15 bez ovlaštenika
	if !s.CanSupervise(&ruk.User, ruk, o, &models.Journal{ID: "svoj", Kind: models.JournalKindMaintenanceA02, AreaID: 15}) || pitano != 15 {
		t.Errorf("uprava područja 15 nije nadzor u svom dnevniku (pitano %d)", pitano)
	}
}

// Kad se dužnosti dežurnog ne daju pročitati, predaja i preuzimanje se
// odbijaju i ništa se ne upisuje; s izvorom dužnosti rade (Odluke
// (30. 10. 2026.)). Podaci su izmišljeni.
func TestPredajaIPreuzimanjeUzGreskuDuznosti(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	od := time.Now().In(models.Zagreb).Add(-2 * time.Hour)
	o.dnevnik.DezurniID, o.dnevnik.DezurniIme, o.dnevnik.DezurniOd = o.pperic.ID.String(), o.pperic.FullName, &od
	o.svc.SetPovijestZaduzenja(func(uuid.UUID) ([]models.VerzijaZaduzenja, error) { return nil, errors.New("baza zaključana") })
	if err := o.svc.PredajDezurstvo(ctx, o.uprava, o.ovlUprav, o.dnevnik); err == nil {
		t.Error("predaja prošla uz grešku čitanja dužnosti")
	}
	if err := o.svc.PreuzmiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, o.dnevnik); err == nil {
		t.Error("preuzimanje prošlo uz grešku čitanja dužnosti")
	}
	if zapisi, err := o.svc.EntriesForJournal(ctx, o.dnevnik.ID); err != nil || len(zapisi) != 0 || o.dnevnik.DezurniID != o.pperic.ID.String() {
		t.Errorf("odbijena predaja upisala je %d zapisa ili promijenila dežurnog: %v", len(zapisi), err)
	}
	// kontrola: s izvorom dužnosti uprava preuzima dežurstvo
	o.svc.SetPovijestZaduzenja(duznostUSektoruOdUvijek("P"))
	if err := o.svc.PreuzmiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, o.dnevnik); err != nil {
		t.Errorf("uprava ne preuzima dežurstvo: %v", err)
	}
}

// Stariji dnevnik obrane po području zadržava svoje pravilo nadzora: nadzor
// je tko u njega piše, a ne izvođač. Podaci su izmišljeni.
func TestNadzorUStarijemDnevnikuObrane(t *testing.T) {
	s := &JournalService{}
	b, podrucje := "B", 15
	stari := &models.Journal{ID: "stari", Kind: models.JournalKindDefense, AreaID: 15}
	o := models.Opseg{Sektor: "B", Podrucje: 15, Podrucja: []int{15}}
	ruk := models.NewUserPermissions(models.User{ID: uuid.New(), Duties: []models.Duty{{Role: models.RoleAreaLeader, SectorID: &b, AreaID: &podrucje, IsActive: true}}})
	if !s.CanSupervise(&ruk.User, ruk, o, stari) {
		t.Error("rukovoditelj područja nije nadzor u starijem dnevniku obrane")
	}
	izv := models.NewUserPermissions(models.User{ID: uuid.New(), Duties: []models.Duty{{Role: models.RoleServiceLeaderForeman, SectorID: &b, AreaID: &podrucje, IsActive: true}}})
	if s.CanSupervise(&izv.User, izv, o, stari) {
		t.Error("izvođač je nadzor u starijem dnevniku obrane")
	}
}

// Kad se dnevnik ne da spremiti, preuzimanje javlja grešku. Podaci su
// izmišljeni.
func TestPreuzimanjeJavljaGreskuSpremanja(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	if err := o.baza.Close(); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.PreuzmiDezurstvo(context.Background(), o.uprava, o.ovlUprav, o.opseg, o.dnevnik); err == nil {
		t.Error("preuzimanje bez baze prošlo")
	}
}
