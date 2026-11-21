package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// zaduzenjeOsobe doda osobi zaduženje uloge r koje vrijedi do istek (nil:
// stalno)
func zaduzenjeOsobe(t *testing.T, repo *UserRepository, u *models.User, r models.Role, istek *time.Time) *models.Duty {
	t.Helper()
	d := &models.Duty{UserID: u.ID, Title: r.Label(), Role: r, ScopeType: models.ScopeAll, IsActive: true, ExpiresAt: istek, CreatedAt: time.Now().UTC()}
	nuzno(t, repo.AddDuty(d))
	return d
}

// Odlazak iz službe u jednom upisu isključi prijavu, upiše razlog i dan i
// opozove zaduženja koja vrijede; isteklo ostaje isteklo. Verzije nose tko
// je zabilježio odlazak. Filtar „Izvan službe” daje tu osobu, a ne
// isključen račun bez odlaska.
func TestOdlazakIzSluzbe(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	uprava := novaOsoba(t, repo, "uprava", "Ana Uprava", nil)
	pero := novaOsoba(t, repo, "pperic", "Pero Perić", nil)
	novaOsoba(t, repo, "iivic", "Ivo Ivić", func(u *models.User) { u.IsActive = false })
	vazece := zaduzenjeOsobe(t, repo, pero, models.RoleViewer, nil)
	jucer := time.Now().Add(-24 * time.Hour)
	isteklo := zaduzenjeOsobe(t, repo, pero, models.RoleGuest, &jucer)

	dan := time.Date(2026, 11, 1, 0, 0, 0, 0, models.Zagreb)
	ids, err := repo.Kao(uprava.ID).OdlazakIzSluzbe(pero.ID, models.OdlazakMirovina, dan)
	nuzno(t, err)
	if len(ids) != 1 || ids[0] != vazece.ID.String() {
		t.Fatalf("opozvana: %v, a vrijedilo je %s", ids, vazece.ID)
	}
	sad, err := repo.GetUserByID(pero.ID)
	nuzno(t, err)
	if sad.IsActive || sad.Odlazak != models.OdlazakMirovina || sad.OdlazakDan == nil || !sad.OdlazakDan.Equal(dan) || len(sad.Duties) != 0 {
		t.Errorf("nakon odlaska: %+v", sad)
	}
	if d, err := repo.GetDuty(isteklo.ID); err != nil || d == nil || !d.IsActive {
		t.Errorf("isteklo zaduženje je opozvano: %+v %v", d, err)
	}
	povijest, err := n.rec.History(ctxRaz, EntityUsers, pero.ID.String())
	nuzno(t, err)
	var uv userVersion
	nuzno(t, json.Unmarshal(povijest[0].Payload, &uv))
	if uv.Izmjena == nil || uv.Izmjena.Tko != uprava.ID || !uv.Izmjena.VrijediZa(povijest[0].CreatedAt) || uv.Odlazak != models.OdlazakMirovina {
		t.Errorf("verzija odlaska: %+v", uv.Izmjena)
	}
	popis, err := repo.ListUsers("", 0, "", "", string(StanjeIzvanSluzbe))
	nuzno(t, err)
	if len(popis) != 1 || popis[0].ID != pero.ID {
		t.Errorf("izvan službe: %v", popis)
	}

	if _, err := repo.OdlazakIzSluzbe(uuid.New(), models.OdlazakOdlazak, dan); err == nil {
		t.Error("odlazak nepostojećeg računa je prošao")
	}
}

// Odlazak koji ne uspije ni u jednom koraku ne mijenja ništa: ni prijavu ni
// zaduženja
func TestOdlazakNeuspjeoNistaNeMijenja(t *testing.T) {
	for _, kvar := range []kvarBaze{
		{"zatvorena baza", ""},
		{"zaduženja", `CREATE TRIGGER kvar_duznosti BEFORE UPDATE ON duties BEGIN SELECT RAISE(ABORT, 'kvar'); END`},
		{"knjiga", zabranaKnjigeZa(EntityUsers)},
	} {
		t.Run(kvar.naziv, func(t *testing.T) {
			n := noviCvor(t, "ured")
			repo := NewUserRepository(n.db, n.rec)
			pero := novaOsoba(t, repo, "pperic", "Pero Perić", nil)
			zaduzenjeOsobe(t, repo, pero, models.RoleViewer, nil)
			pokvari(t, n, kvar)
			if _, err := repo.OdlazakIzSluzbe(pero.ID, models.OdlazakMirovina, time.Now()); err == nil {
				t.Fatal("odlazak je prošao")
			}
			if kvar.q == "" {
				return
			}
			sad, err := repo.GetUserByID(pero.ID)
			nuzno(t, err)
			if !sad.IsActive || sad.IzvanSluzbe() || len(sad.Duties) != 1 {
				t.Errorf("nakon neuspjeha: aktivan %v, odlazak %q, zaduženja %d", sad.IsActive, sad.Odlazak, len(sad.Duties))
			}
		})
	}
}

// Verzija računa i zaduženja nosi tko ju je napisao samo kad se zna; bez
// osobe polje je prazno, a ostali zapisi ostaju kakvi jesu
func TestVerzijaNosiTkoJeMijenjao(t *testing.T) {
	r := &UserRepository{}
	tko := uuid.New()
	d := models.Duty{ID: uuid.New()}
	if v, ok := r.sIzmjenom(d).(dutyVersion); !ok || v.Izmjena != nil {
		t.Errorf("zaduženje bez osobe: %#v", r.sIzmjenom(d))
	}
	k := r.Kao(tko)
	if r.tko != uuid.Nil {
		t.Error("Kao je promijenio izvorni repozitorij")
	}
	if v, ok := k.sIzmjenom(&d).(dutyVersion); !ok || v.Izmjena == nil || v.Izmjena.Tko != tko || v.ID != d.ID {
		t.Errorf("zaduženje s osobom: %#v", k.sIzmjenom(&d))
	}
	if v, ok := k.sIzmjenom(userVersion{}).(userVersion); !ok || v.Izmjena == nil || v.Izmjena.Tko != tko {
		t.Errorf("račun s osobom: %#v", k.sIzmjenom(userVersion{}))
	}
	if v := k.sIzmjenom("ostalo"); v != "ostalo" {
		t.Errorf("drugi zapis: %#v", v)
	}
}
