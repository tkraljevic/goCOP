package service_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// okolinaOvlasti: baza okolinaOporavka (globalni administratori o.admin i
// o.sef, PIN uključen) sa sektorima A i B, područjima 5 (A), 16 i 17 (B) i
// dionicama B.16.1 i B.17.1. Servis korisnika ima kuke kao na poslužitelju:
// uklanjanje potpisnog ključa i brisanje spremljene lozinke sandučića.
type okolinaOvlasti struct {
	*okolinaOporavka
	users *service.UserService
	ps    *service.PotpisService
	akti  *service.AktService
}

func novaOkolinaOvlasti(t *testing.T) *okolinaOvlasti {
	t.Helper()
	o := novaOkolinaOporavka(t)
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop, level, vgo_phone) VALUES ('A', 'Sektor A', 'VGO A', 'COP A', 2, ''), ('B', 'Sektor B', 'VGO B', 'COP B', 2, '')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter, direct_to_sector, latitude, longitude, vgi_phone) VALUES
			(5, 'A', 'Područje 5', 'VGI 5', '', 0, 45.5, 18.6, ''), (16, 'B', 'Područje 16', 'VGI 16', '', 0, 45.6, 18.7, ''),
			(17, 'B', 'Područje 17', 'VGI 17', '', 0, 45.7, 18.8, '')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES
			('B.16.1', 16, 'B', 'Dionica 16', datetime('now'), datetime('now')), ('B.17.1', 17, 'B', 'Dionica 17', datetime('now'), datetime('now'))`,
	} {
		if _, err := o.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	users := service.NewUserService(o.repo, o.auth, service.NewSSEBroker())
	_, kljucCvora, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ps := service.NewPotpisService(repository.NewPotpisRepository(o.db, o.rec), users, "cvor-web", kljucCvora, o.auth.CheckPassword)
	if err := ps.Pokreni(context.Background()); err != nil {
		t.Fatal(err)
	}
	akti := service.NewAktService(repository.NewAktiRepository(o.db, o.rec), nil, repository.NewSectionRepository(o.db, o.rec), nil, nil, users, nil, "cvor-web")
	akti.SetKljuc(kljucCvora)
	users.SetUklanjanjeKljuca(ps.UkloniKljuc)
	users.SetBrisanjeSanducica(akti.ZaboraviSanducic)
	return &okolinaOvlasti{okolinaOporavka: o, users: users, ps: ps, akti: akti}
}

// osoba otvara aktivan račun (lozinka "lozinka-<ime>", adresa <ime>@voda.hr)
// i daje mu dužnost kao globalni administrator; d.Role prazan: bez dužnosti
func (o *okolinaOvlasti) osoba(t *testing.T, ime string, d service.AddDutyRequest) *models.User {
	t.Helper()
	u := o.racun(t, ime, "lozinka-"+ime, ime+"@voda.hr", false, true)
	if d.Role != "" {
		d.UserID = u.ID
		if err := o.users.AddDuty(o.admin, d); err != nil {
			t.Fatal(err)
		}
	}
	svjez, err := o.repo.GetUserByID(u.ID)
	if err != nil || svjez == nil {
		t.Fatalf("račun %s: %v", ime, err)
	}
	return svjez
}

func (o *okolinaOvlasti) ovlasti(t *testing.T, id uuid.UUID) *models.UserPermissions {
	t.Helper()
	p, err := o.auth.PermissionsFor(id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func sektorP(s string) *string { return &s }
func podrucjeP(i int) *int     { return &i }
