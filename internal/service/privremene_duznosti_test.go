package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Privremeno imenovanje (privremena ispomoć): vrijedi dok na njegovim
// dionicama, odnosno u branjenom području, traje redovna ili izvanredna
// obrana, ili do zadanog datuma ako je raniji; što privremena uprava dodijeli
// na razini uprave ističe zajedno s njom. Ostale dužnosti su stalne.

// okolinaImenovanja je okolina akta s krajem obrane povezanim na dužnosti i
// područjem koje popis područja čita (okolina akta ne upisuje podcentar, a
// prazan stupac popis ne čita)
func okolinaImenovanja(t *testing.T) *okolinaAkta {
	t.Helper()
	o := novaOkolinaAkta(t)
	if _, err := o.baza.Exec(`UPDATE areas SET subcenter = '' WHERE subcenter IS NULL`); err != nil {
		t.Fatal(err)
	}
	o.users.SetPrestanakObrane(o.akti.PrestanakObraneDuznosti)
	return o
}

// upravaOrganizacije su ovlasti stalnog globalnog administratora
func (o *okolinaAkta) upravaOrganizacije() *models.UserPermissions {
	return &models.UserPermissions{IsGlobalAdmin: true, User: *o.rukovod}
}

// duznostOsobe je jedina dužnost osobe u ulozi (i opozvana)
func (o *okolinaAkta) duznostOsobe(t *testing.T, osoba uuid.UUID, uloga models.Role) *models.Duty {
	t.Helper()
	var id string
	if err := o.baza.QueryRow(`SELECT id FROM duties WHERE user_id = ? AND role = ?`, osoba.String(), string(uloga)).Scan(&id); err != nil {
		t.Fatalf("dužnost %s osobe %s: %v", uloga, osoba, err)
	}
	d, err := o.users.GetDuty(uuid.MustParse(id))
	if err != nil || d == nil {
		t.Fatalf("dužnost %s: %v", id, err)
	}
	return d
}

// verzijeDuznosti je broj verzija dužnosti u knjizi
func (o *okolinaAkta) verzijeDuznosti(t *testing.T, id uuid.UUID) int {
	t.Helper()
	v, err := ledger.New(o.baza, "test").History(context.Background(), repository.EntityDuties, id.String())
	if err != nil {
		t.Fatal(err)
	}
	return len(v)
}

func istiTrenutak(a *time.Time, b time.Time) bool { return a != nil && a.Equal(b) }

// Imenovanje rukovoditelja dionice za vrijeme redovne obrane: ovjereni prekid
// obrane zadaje istek, poništen prekid ga vraća, a zadani datum vrijedi kad
// je raniji
func TestPrivremenoImenovanjeIsticeSObranom(t *testing.T) {
	o := okolinaImenovanja(t)
	ctx := context.Background()
	o.ovjeri(t, models.AktUspostava, models.PhaseRegular, time.Now().Add(-time.Hour).Truncate(time.Minute))

	if err := o.users.AddDuty(o.upravaOrganizacije(), service.AddDutyRequest{UserID: o.vodocuv.ID, Role: models.RoleSectionLeader,
		SectionCodes: "P.1.1", IsTemporary: true, IsticeSObranom: true, Reason: "nedostaje osoblja"}); err != nil {
		t.Fatal(err)
	}
	d := o.duznostOsobe(t, o.vodocuv.ID, models.RoleSectionLeader)
	if !d.IsTemporary || !d.IsticeSObranom || d.ExpiresAt != nil || d.Rok != nil || d.OvisiO != nil || d.Reason != "nedostaje osoblja" {
		t.Fatalf("imenovanje dok obrana traje: %+v", d)
	}

	kraj := time.Now().Add(2 * time.Hour).Truncate(time.Minute)
	prekid, upozorenja := o.ovjeri(t, models.AktPrekid, models.PhaseRegular, kraj)
	if len(upozorenja) != 0 {
		t.Errorf("prekid: %v", upozorenja)
	}
	if d = o.duznostOsobe(t, o.vodocuv.ID, models.RoleSectionLeader); !istiTrenutak(d.ExpiresAt, kraj) {
		t.Fatalf("istek nakon ovjerenog prekida: %v, a prekid vrijedi %v", d.ExpiresAt, kraj)
	}
	// profil kaže odakle je kraj: iz akta o prestanku obrane, ne iz imenovanja
	if izvor := o.users.IzvoriIsteka([]models.Duty{*d})[d.ID]; izvor != "prestanak obrane, akt "+prekid.Oznaka() {
		t.Errorf("izvor isteka: %q", izvor)
	}

	if _, _, err := o.akti.Storniraj(ctx, o.ovlasti, o.rukovod, prekid.ID, "pogrešan sat"); err != nil {
		t.Fatal(err)
	}
	if d = o.duznostOsobe(t, o.vodocuv.ID, models.RoleSectionLeader); d.ExpiresAt != nil {
		t.Fatalf("poništen prekid mora vratiti imenovanje: %v", d.ExpiresAt)
	}

	// zadani datum prije kraja obrane: vrijedi datum
	rok := time.Now().Add(time.Hour).Truncate(time.Minute)
	if err := o.users.UpdateDuty(o.upravaOrganizacije(), d.ID, service.AddDutyRequest{Role: d.Role, SectionCodes: d.SectionCodes,
		IsTemporary: true, IsticeSObranom: true, ExpiresAt: &rok, Reason: d.Reason}); err != nil {
		t.Fatal(err)
	}
	o.ovjeri(t, models.AktPrekid, models.PhaseRegular, kraj)
	if d = o.duznostOsobe(t, o.vodocuv.ID, models.RoleSectionLeader); !istiTrenutak(d.ExpiresAt, rok) || !istiTrenutak(d.Rok, rok) {
		t.Errorf("raniji zadani datum: istek %v, rok %v", d.ExpiresAt, d.Rok)
	}
	// zadani dan sam kaže kad prestaje: izvor se ne dopisuje
	if izvori := o.users.IzvoriIsteka([]models.Duty{*d}); len(izvori) != 0 {
		t.Errorf("izvor uz zadani dan: %v", izvori)
	}
}

// Stalna dužnost nema ni datum ni istek s obranom, ni kad ih obrazac pošalje
func TestStalnaDuznostBezIsteka(t *testing.T) {
	o := okolinaImenovanja(t)
	o.users.SetPrestanakObrane(func(models.Duty) *models.PrestanakObrane {
		t.Error("stalnoj dužnosti ne traži se kraj obrane")
		return nil
	})
	sutra := time.Now().Add(24 * time.Hour)
	if err := o.users.AddDuty(o.upravaOrganizacije(), service.AddDutyRequest{UserID: o.vodocuv.ID, Role: models.RoleSectionDeputy,
		SectionCodes: "P.1.2", ExpiresAt: &sutra, IsticeSObranom: true, Reason: "zaostalo iz obrasca"}); err != nil {
		t.Fatal(err)
	}
	d := o.duznostOsobe(t, o.vodocuv.ID, models.RoleSectionDeputy)
	if d.IsTemporary || d.ExpiresAt != nil || d.Rok != nil || d.IsticeSObranom || d.Reason != "" {
		t.Errorf("stalna dužnost: %+v", d)
	}
}

// Privremeni rukovoditelj branjenog područja dodijeli zamjenika: zamjenik je
// privremen i ističe s imenovanjem (kraj obrane, opoziv), a ponovno
// usklađivanje ne mijenja već prošli istek
func TestDuznostPrivremeneUpraveIsticeSNjom(t *testing.T) {
	o := okolinaImenovanja(t)
	o.ovjeri(t, models.AktUspostava, models.PhaseRegular, time.Now().Add(-time.Hour).Truncate(time.Minute))
	uprava := o.upravaOrganizacije()

	// privremeni rukovoditelj područja (već ima dužnost vodočuvara)
	podrucje := 1
	if err := o.users.AddDuty(uprava, service.AddDutyRequest{UserID: o.vodocuv.ID, Role: models.RoleAreaLeader, AreaID: &podrucje,
		IsTemporary: true, IsticeSObranom: true, Reason: "nedostaje osoblja"}); err != nil {
		t.Fatal(err)
	}
	imenovanje := o.duznostOsobe(t, o.vodocuv.ID, models.RoleAreaLeader)
	privremeni, err := o.users.GetUserByID(o.vodocuv.ID)
	if err != nil {
		t.Fatal(err)
	}

	// on dodjeljuje zamjenika osobi koja već ima dužnost
	sektor := "P"
	zamjenik := &models.User{ID: uuid.New(), Username: "zamjenik", FullName: "Ivo Ivić", IsActive: true}
	if err := repository.NewUserRepository(o.baza, ledger.New(o.baza, "test")).CreateUser(zamjenik, &models.Duty{Title: "Vodočuvar P.1.2",
		Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, SectorID: &sektor, AreaID: &podrucje, SectionCodes: "P.1.2"}); err != nil {
		t.Fatal(err)
	}
	if err := o.users.AddDuty(models.NewUserPermissions(*privremeni), service.AddDutyRequest{UserID: zamjenik.ID, Role: models.RoleAreaDeputy,
		AreaID: &podrucje}); err != nil {
		t.Fatal(err)
	}
	dodijeljena := o.duznostOsobe(t, zamjenik.ID, models.RoleAreaDeputy)
	if !dodijeljena.IsTemporary || dodijeljena.OvisiO == nil || *dodijeljena.OvisiO != imenovanje.ID || dodijeljena.ExpiresAt != nil ||
		dodijeljena.Reason != "do isteka privremene uprave koja ju je dodijelila" {
		t.Fatalf("dodijeljeno od privremene uprave: %+v", dodijeljena)
	}

	// kraj obrane: oba ističu u isti trenutak
	kraj := time.Now().Add(2 * time.Hour).Truncate(time.Minute)
	o.ovjeri(t, models.AktPrekid, models.PhaseRegular, kraj)
	if d := o.duznostOsobe(t, o.vodocuv.ID, models.RoleAreaLeader); !istiTrenutak(d.ExpiresAt, kraj) {
		t.Errorf("imenovanje nakon prekida: %v", d.ExpiresAt)
	}
	if d := o.duznostOsobe(t, zamjenik.ID, models.RoleAreaDeputy); !istiTrenutak(d.ExpiresAt, kraj) {
		t.Errorf("dodijeljeno nakon prekida: %v", d.ExpiresAt)
	} else if izvor := o.users.IzvoriIsteka([]models.Duty{*d})[d.ID]; izvor != "istek privremene uprave koja ju je dodijelila" {
		t.Errorf("izvor isteka dodijeljenog: %q", izvor)
	}

	// opoziv imenovanja prekida i dodijeljeno, odmah
	prije := time.Now()
	if err := o.users.RevokeDuty(uprava, imenovanje.ID); err != nil {
		t.Fatal(err)
	}
	d := o.duznostOsobe(t, zamjenik.ID, models.RoleAreaDeputy)
	if d.ExpiresAt == nil || d.ExpiresAt.Before(prije.Add(-time.Second)) || d.ExpiresAt.After(time.Now()) {
		t.Fatalf("dodijeljeno nakon opoziva imenovanja: %v", d.ExpiresAt)
	}
	// ponovno usklađivanje ne upisuje novi „sad” (čvorovi bi ga prepisivali)
	verzije := o.verzijeDuznosti(t, d.ID)
	if err := o.users.UskladiPrivremene(); err != nil {
		t.Fatal(err)
	}
	if poslije := o.duznostOsobe(t, zamjenik.ID, models.RoleAreaDeputy); !istiTrenutak(poslije.ExpiresAt, *d.ExpiresAt) || o.verzijeDuznosti(t, d.ID) != verzije {
		t.Errorf("usklađivanje mijenja prošli istek: %v → %v, verzija %d → %d", d.ExpiresAt, poslije.ExpiresAt, verzije, o.verzijeDuznosti(t, d.ID))
	}
}

// Imenovanje za branjeno područje ili sektor (bez upisanih dionica) gleda
// obranu na svim dionicama dosega; dok traje, kraja nema; kraj je iz akta o
// prestanku obrane
func TestPrestanakObraneDosegaDuznosti(t *testing.T) {
	o := okolinaImenovanja(t)
	sektor, podrucje := "P", 1
	podrucjem := models.Duty{Role: models.RoleAreaLeader, SectorID: &sektor, AreaID: &podrucje, IsTemporary: true, IsticeSObranom: true}
	sektorom := models.Duty{Role: models.RoleSectorLeader, SectorID: &sektor, IsTemporary: true, IsticeSObranom: true}
	bezDionica := models.Duty{Role: models.RoleSectionLeader, SectionCodes: "X.9.9", IsTemporary: true, IsticeSObranom: true}

	o.ovjeri(t, models.AktUspostava, models.PhaseRegular, time.Now().Add(-time.Hour).Truncate(time.Minute))
	for _, d := range []models.Duty{podrucjem, sektorom, bezDionica} {
		if k := o.akti.PrestanakObraneDuznosti(d); k != nil {
			t.Errorf("%s: kraj %+v dok obrana traje", d.Role, k)
		}
	}
	kraj := time.Now().Add(3 * time.Hour).Truncate(time.Minute)
	prekid, _ := o.ovjeri(t, models.AktPrekid, models.PhaseRegular, kraj)
	for _, d := range []models.Duty{podrucjem, sektorom} {
		if k := o.akti.PrestanakObraneDuznosti(d); k == nil || !k.Kad.Equal(kraj) || k.Akt.ID != prekid.ID {
			t.Errorf("%s: kraj %+v, a prekid %s vrijedi %v", d.Role, k, prekid.Oznaka(), kraj)
		}
	}
	// dionica bez akata: obrana na njoj nije ni počela
	if k := o.akti.PrestanakObraneDuznosti(bezDionica); k != nil {
		t.Errorf("dionica bez akata: %v", k)
	}
}
