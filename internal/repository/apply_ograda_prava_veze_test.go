package repository

// Potvrda primljenih promjena prava (apply_ograda_prava.go): dijete koje se
// vraća s roditeljem, promjena u dva koraka, izmjena na ovom računalu dok
// promjena čeka, dva računala, redoslijed primanja i ime računa na popisu.
// Imena su izmišljena.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// duznostUPovrsini javlja ima li dužnosti na površini
func duznostUPovrsini(t *testing.T, c *cvor, id uuid.UUID) bool {
	t.Helper()
	return brojRedaka(t, c.db, `SELECT COUNT(*) FROM duties WHERE id = ?`, id.String()) == 1
}

// noviRacun je račun koji ovo računalo još nema, bez zastavice
func noviRacun(ime string) models.User {
	return models.User{ID: uuid.New(), Username: ime, FullName: "Ana Anić", PasswordHash: "h", IsActive: true,
		OrgType: models.OrgHrvatskeVode, CreatedAt: time.Now(), UpdatedAt: time.Now()}
}

// upravaZa je dužnost uprave organizacije za račun
func upravaZa(korisnik uuid.UUID) models.Duty {
	return models.Duty{ID: uuid.New(), UserID: korisnik, Role: models.RoleGlobalAdmin, ScopeType: models.ScopeAll,
		IsActive: true, CreatedAt: time.Now()}
}

// Dijete koje se vraća s roditeljem (račun ili sektor koji je bio obrisan
// pa se vratio) prolazi istu potvrdu kao primljena verzija: dužnost uprave
// organizacije koja čeka ne dolazi ni u istoj razmjeni, ni s kasnijom
// izmjenom roditelja, ni s obnovom površine, ni kad se sektor vrati na ovom
// računalu. Dužnost razine 2 vraća se s roditeljem kao i prije.
func TestPravaDijeteSRoditeljem(t *testing.T) {
	provjeri := func(t *testing.T, c *cvor, d models.Duty, obicna *models.Duty) {
		t.Helper()
		if duznostUPovrsini(t, c, d.ID) {
			t.Fatal("dužnost uprave organizacije stigla je s roditeljem bez potvrde")
		}
		_, err := ReplaySurface(ctxRaz, c.db, c.rec)
		nuzno(t, err)
		if duznostUPovrsini(t, c, d.ID) {
			t.Fatal("dužnost uprave organizacije stigla je obnovom površine bez potvrde")
		}
		if p := cekaju(t, c); len(p) != 1 || p[0].Entitet != EntityDuties {
			t.Errorf("čeka %+v", p)
		}
		if obicna != nil && !duznostUPovrsini(t, c, obicna.ID) {
			t.Error("dužnost razine 2 nije se vratila s roditeljem")
		}
	}
	t.Run("racun_u_istoj_razmjeni", func(t *testing.T) {
		c, _ := uredSRacunom(t, true)
		n := noviRacun("aanic")
		v1, v2 := tudjiRacun(t, n), tudjiRacun(t, n)
		v2.Archived = true
		d := upravaZa(n.ID)
		sektor := "B"
		obicna := models.Duty{ID: uuid.New(), UserID: n.ID, Role: models.RoleSectorLeader, ScopeType: models.ScopeSector,
			SectorID: &sektor, IsActive: true, CreatedAt: time.Now()}
		vd, vo := tudja(t, EntityDuties, d.ID.String(), d), tudja(t, EntityDuties, obicna.ID.String(), obicna)
		primi(t, c, []ledger.Version{v1, v2, vd, vo, tudjiRacun(t, n)})
		provjeri(t, c, d, &obicna)
	})
	t.Run("izmjena_racuna", func(t *testing.T) {
		c, _ := uredSRacunom(t, true)
		n := noviRacun("bbabic")
		v2 := tudjiRacun(t, n)
		v2.Archived = true
		for _, v := range []ledger.Version{tudjiRacun(t, n), v2, tudjiRacun(t, n)} {
			primi(t, c, []ledger.Version{v})
		}
		d := upravaZa(n.ID)
		primi(t, c, []ledger.Version{tudja(t, EntityDuties, d.ID.String(), d)})
		n.Phone = "091 000"
		primi(t, c, []ledger.Version{tudjiRacun(t, n)})
		provjeri(t, c, d, nil)
	})
	sektorC := models.Sector{ID: "C", Name: "Sektor C", VgoName: "VGO", CenterCop: "COP", Level: 2}
	t.Run("sektor_u_istoj_razmjeni", func(t *testing.T) {
		c, u := uredSRacunom(t, false)
		s1, s2 := tudja(t, EntitySectors, "C", sektorC), tudja(t, EntitySectors, "C", sektorC)
		s2.Archived = true
		d := upravaZa(u.ID)
		d.ScopeType, d.SectorID = models.ScopeSector, &sektorC.ID
		primi(t, c, []ledger.Version{s1, s2, tudja(t, EntityDuties, d.ID.String(), d), tudja(t, EntitySectors, "C", sektorC)})
		provjeri(t, c, d, nil)
	})
	t.Run("sektor_na_ovom_racunalu", func(t *testing.T) {
		c, u := uredSRacunom(t, false)
		org := NewOrgRepository(c.db, c.rec)
		s := sektorC
		nuzno(t, org.SaveSector(ctxRaz, &s))
		spomenik := tudja(t, EntitySectors, "C", sektorC)
		spomenik.Archived = true
		d := upravaZa(u.ID)
		d.ScopeType, d.SectorID = models.ScopeSector, &sektorC.ID
		primi(t, c, []ledger.Version{spomenik, tudja(t, EntityDuties, d.ID.String(), d)})
		nuzno(t, org.SaveSector(ctxRaz, &s))
		provjeri(t, c, d, nil)
	})
}

// Globalni administrator ne ostaje bez prava ni u dva koraka: oduzeta
// zastavica čeka, pa ni isključenje koje dolazi poslije ne prolazi. Isto za
// dužnost uprave organizacije pa račun.
func TestPravaDvaKoraka(t *testing.T) {
	t.Run("zastavica_pa_racun", func(t *testing.T) {
		c, u := uredSRacunom(t, true)
		bez := u
		bez.IsGlobalAdmin = false
		primi(t, c, []ledger.Version{tudjiRacun(t, bez)})
		bez.IsActive = false
		v2 := tudjiRacun(t, bez)
		primi(t, c, []ledger.Version{v2})
		if stupacRacuna(t, c, u.ID, "is_active") != "1" || stupacRacuna(t, c, u.ID, "is_global_admin") != "1" {
			t.Fatal("globalni administrator je ostao bez prava bez potvrde")
		}
		if p := jedinaCeka(t, c, v2); p.Promjene != "oduzima zastavicu globalnog administratora; isključuje račun globalnog administratora" {
			t.Errorf("popis: %+v", p)
		}
	})
	t.Run("duznost_pa_racun", func(t *testing.T) {
		c, u := uredSRacunom(t, false)
		d := upravaZa(u.ID)
		nuzno(t, NewUserRepository(c.db, c.rec).AddDuty(&d))
		d.IsActive = false
		u.IsActive = false
		primi(t, c, []ledger.Version{tudja(t, EntityDuties, d.ID.String(), d), tudjiRacun(t, u)})
		if !aktivnaDuznost(t, c, d.ID, u.ID) || stupacRacuna(t, c, u.ID, "is_active") != "1" {
			t.Fatal("globalni administrator je ostao bez prava bez potvrde")
		}
		if p := cekaju(t, c); len(p) != 2 {
			t.Errorf("čeka %+v", p)
		}
	})
	t.Run("obrisana_duznost", func(t *testing.T) {
		c, u := uredSRacunom(t, false)
		d := upravaZa(u.ID)
		nuzno(t, NewUserRepository(c.db, c.rec).AddDuty(&d))
		v := tudja(t, EntityDuties, d.ID.String(), d)
		v.Archived = true
		primi(t, c, []ledger.Version{v})
		if !aktivnaDuznost(t, c, d.ID, u.ID) {
			t.Fatal("dužnost uprave organizacije obrisana je bez potvrde")
		}
		if p := jedinaCeka(t, c, v); p.Promjene != "oduzima dužnost uprave organizacije: "+d.Role.Label() {
			t.Errorf("popis: %+v", p)
		}
		nuzno(t, PotvrdiPromjenuPrava(ctxRaz, c.db, c.rec, v.VersionID, tkoOdlucuje))
		if duznostUPovrsini(t, c, d.ID) {
			t.Error("potvrđeno brisanje nije maknulo dužnost")
		}
	})
}

// Dok promjena prava čeka, zapis se na ovom računalu ne mijenja: izmjena,
// lozinka, brisanje i dužnost javljaju zašto, prijava se ne bilježi, a
// preračun isteka dužnost preskače. Nakon odluke izmjena prolazi.
func TestPravaLokalnaIzmjenaCeka(t *testing.T) {
	c, u := uredSRacunom(t, true)
	repo := NewUserRepository(c.db, c.rec)
	sutra := time.Now().Add(24 * time.Hour)
	d := upravaZa(u.ID)
	d.IsTemporary, d.IsticeSObranom, d.ExpiresAt, d.IsPrimary = true, true, &sutra, true
	nuzno(t, repo.AddDuty(&d))
	lozinka := u
	lozinka.PasswordHash = "drugi-sazetak"
	ugasena := d
	ugasena.IsActive = false
	vr, vd := tudjiRacun(t, lozinka), tudja(t, EntityDuties, d.ID.String(), ugasena)
	primi(t, c, []ledger.Version{vr, vd})
	if len(cekaju(t, c)) != 2 {
		t.Fatalf("čeka %+v", cekaju(t, c))
	}
	prije := brojVerzijaUKnjizi(t, c)
	x, err := repo.GetUserByID(u.ID)
	nuzno(t, err)
	x.Phone = "099 7"
	for _, s := range []struct {
		naziv          string
		err, ocekivano error
	}{
		{"izmjena", repo.UpdateUser(x), ErrRacunCekaPotvrdu},
		{"lozinka", repo.ChangePassword(u.ID, "lokalno"), ErrRacunCekaPotvrdu},
		{"poništenje", repo.ResetPassword(u.ID, "lokalno"), ErrRacunCekaPotvrdu},
		{"brisanje", repo.DeleteUser(u.ID), ErrRacunCekaPotvrdu},
		{"dužnost", repo.UpdateDuty(&d), ErrDuznostCekaPotvrdu},
		{"opoziv", repo.RevokeDuty(d.ID), ErrDuznostCekaPotvrdu},
		{"istek", repo.PostaviIstek(d.ID, nil), ErrDuznostCekaPotvrdu},
		{"nova_primarna", repo.AddDuty(&models.Duty{UserID: u.ID, Role: models.RoleNationalLeader, ScopeType: models.ScopeAll, IsPrimary: true}),
			ErrDuznostCekaPotvrdu},
		{"prijava", repo.MarkLogin(u.ID, time.Now()), nil},
	} {
		if !errors.Is(s.err, s.ocekivano) || (s.ocekivano == nil) != (s.err == nil) {
			t.Errorf("%s: %v", s.naziv, s.err)
		}
	}
	if n := brojVerzijaUKnjizi(t, c); n != prije {
		t.Errorf("izmjene dok promjena čeka upisale su verzije: %d → %d", prije, n)
	}
	if stupacRacuna(t, c, u.ID, "phone") != "" || stupacRacuna(t, c, u.ID, "COALESCE(last_login_at, '')") != "" {
		t.Error("izmjena ili prijava stigla je na površinu")
	}
	priv, err := repo.PrivremeneSIstekom()
	nuzno(t, err)
	for _, p := range priv {
		if p.ID == d.ID {
			t.Error("preračun isteka uzima dužnost koja čeka potvrdu")
		}
	}
	nuzno(t, OdbijPromjenuPrava(ctxRaz, c.db, c.rec, vr.VersionID, tkoOdlucuje))
	nuzno(t, OdbijPromjenuPrava(ctxRaz, c.db, c.rec, vd.VersionID, tkoOdlucuje))
	nuzno(t, repo.UpdateUser(x))
	nuzno(t, repo.RevokeDuty(d.ID))
	if stupacRacuna(t, c, u.ID, "phone") != "099 7" || aktivnaDuznost(t, c, d.ID, u.ID) {
		t.Error("nakon odluke izmjena nije prošla")
	}
}

// Dva računala: na A račun dobije zastavicu, na B čeka. Izmjena na B ne
// prolazi dok se ne odluči, pa ne vraća zastavicu na A. Kad B odbije i
// zatim promijeni telefon, A dobije telefon, a oduzimanje zastavice ondje
// čeka potvrdu.
func TestPravaDvaRacunala(t *testing.T) {
	sNedovrsenima(t)
	a, b := noviCvor(t, "ured-a"), noviCvor(t, "laptop-b")
	repoA, repoB := NewUserRepository(a.db, a.rec), NewUserRepository(b.db, b.rec)
	u := models.User{ID: uuid.New(), Username: "ppetric", FullName: "Petar Petrić", PasswordHash: "h0", IsActive: true,
		OrgType: models.OrgHrvatskeVode, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	nuzno(t, repoA.CreateUser(&u, nil))
	posalji(t, a, b)
	naA, err := repoA.GetUserByID(u.ID)
	nuzno(t, err)
	naA.IsGlobalAdmin = true
	nuzno(t, repoA.UpdateUser(naA))
	posalji(t, a, b)
	ceka := cekaju(t, b)
	if stupacRacuna(t, b, u.ID, "is_global_admin") != "0" || len(ceka) != 1 {
		t.Fatalf("na B: zastavica %s, čeka %+v", stupacRacuna(t, b, u.ID, "is_global_admin"), ceka)
	}
	naB, err := repoB.GetUserByID(u.ID)
	nuzno(t, err)
	naB.Phone = "031 123 456"
	if err := repoB.UpdateUser(naB); !errors.Is(err, ErrRacunCekaPotvrdu) {
		t.Fatalf("izmjena na B dok promjena čeka: %v", err)
	}
	posalji(t, b, a)
	if stupacRacuna(t, a, u.ID, "is_global_admin") != "1" || len(cekaju(t, a)) != 0 {
		t.Fatal("A je izgubio zastavicu")
	}

	nuzno(t, OdbijPromjenuPrava(ctxRaz, b.db, b.rec, ceka[0].VersionID, tkoOdlucuje))
	nuzno(t, repoB.UpdateUser(naB))
	posalji(t, b, a)
	if stupacRacuna(t, a, u.ID, "is_global_admin") != "1" || stupacRacuna(t, a, u.ID, "phone") != "031 123 456" {
		t.Errorf("na A: zastavica %s, telefon %s", stupacRacuna(t, a, u.ID, "is_global_admin"), stupacRacuna(t, a, u.ID, "phone"))
	}
	if p := cekaju(t, a); len(p) != 1 || p[0].Promjene != "oduzima zastavicu globalnog administratora" {
		t.Errorf("na A čeka %+v", p)
	}
}

// Isti skup verzija daje istu površinu bez obzira na to stižu li u jednoj
// razmjeni ili u dvije: telefon stigne, a lozinka i obavezna promjena
// lozinke globalnog administratora ostaju kakve su ovdje dok se ne potvrde.
func TestPravaRedoslijedPrimanja(t *testing.T) {
	stanje := func(t *testing.T, jednom bool) [4]string {
		c, u := uredSRacunom(t, true)
		u.MustChangePassword = true
		nuzno(t, NewUserRepository(c.db, c.rec).UpdateUser(&u))
		u.PasswordHash, u.MustChangePassword = "nova", false
		v1 := tudjiRacun(t, u)
		u.Phone = "098 1"
		v2 := tudjiRacun(t, u)
		if jednom {
			primi(t, c, []ledger.Version{v1, v2})
		} else {
			primi(t, c, []ledger.Version{v1})
			primi(t, c, []ledger.Version{v2})
		}
		jedinaCeka(t, c, v2)
		var out [4]string
		for i, s := range []string{"phone", "password_hash", "must_change_password", "is_global_admin"} {
			out[i] = stupacRacuna(t, c, u.ID, s)
		}
		return out
	}
	jednom, dvaput := stanje(t, true), stanje(t, false)
	if jednom != dvaput || jednom != [4]string{"098 1", "sazetak-ureda", "1", "1"} {
		t.Errorf("u jednoj razmjeni %v, u dvije %v", jednom, dvaput)
	}
}

// Popis pokazuje ime računa kakvo je na ovom računalu; ime iz primljene
// verzije samo uz napomenu
func TestPravaImeRacunaNaPopisu(t *testing.T) {
	c, u := uredSRacunom(t, true)
	u.FullName, u.Username, u.PasswordHash = "Ivo Ivić", "iivic", "drugi-sazetak"
	v := tudjiRacun(t, u)
	primi(t, c, []ledger.Version{v})
	if p := jedinaCeka(t, c, v); p.Racun != "Marko Markić (mmarkic); u primljenoj verziji: Ivo Ivić (iivic)" {
		t.Errorf("račun na popisu: %q", p.Racun)
	}
}

// Kad se račun ili dužnost s površine ne da pročitati, primjena javi grešku
// i ništa ne stavlja na površinu; potvrda s prekinutim zahtjevom ne zapisuje
// odluku
func TestPravaRedakSeNeDaProcitati(t *testing.T) {
	c, u := uredSRacunom(t, true)
	d := upravaZa(u.ID)
	nuzno(t, NewUserRepository(c.db, c.rec).AddDuty(&d))
	_, err := c.db.Exec(`UPDATE duties SET created_at = 'nije vrijeme' WHERE id = ?`, d.ID.String())
	nuzno(t, err)
	d.IsActive = false
	vd := tudja(t, EntityDuties, d.ID.String(), d)
	_, err = c.rec.Apply(ctxRaz, []ledger.Version{vd})
	nuzno(t, err)
	if err := ApplyVersions(ctxRaz, c.db, c.rec, []ledger.Version{vd}); err == nil {
		t.Error("dužnost koja se ne da pročitati: primjena bez greške")
	}
	_, err = c.db.Exec(`UPDATE users SET created_at = 'nije vrijeme' WHERE id = ?`, u.ID.String())
	nuzno(t, err)
	u.IsGlobalAdmin = false
	vr := tudjiRacun(t, u)
	_, err = c.rec.Apply(ctxRaz, []ledger.Version{vr})
	nuzno(t, err)
	if err := ApplyVersions(ctxRaz, c.db, c.rec, []ledger.Version{vr}); err == nil {
		t.Error("račun koji se ne da pročitati: primjena bez greške")
	}
	if len(cekaju(t, c)) != 0 {
		t.Error("promjena čeka iako primjena nije prošla")
	}

	n, _ := uredSRacunom(t, false)
	podignut := noviRacun("ccvitic")
	podignut.IsGlobalAdmin = true
	v := tudjiRacun(t, podignut)
	primi(t, n, []ledger.Version{v})
	prekinut, odustani := context.WithCancel(ctxRaz)
	odustani()
	if err := PotvrdiPromjenuPrava(prekinut, n.db, n.rec, v.VersionID, tkoOdlucuje); err == nil {
		t.Error("potvrda s prekinutim zahtjevom bez greške")
	}
	jedinaCeka(t, n, v)
}
