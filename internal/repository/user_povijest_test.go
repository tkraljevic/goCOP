package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// vodocuvarDuznost je zaduženje vodočuvara na dionici osnovaDionica
func vodocuvarDuznost(korisnik uuid.UUID, dionice string) *models.Duty {
	sektor, podrucje := "B", 16
	return &models.Duty{UserID: korisnik, Title: "Vodočuvar", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection,
		SectorID: &sektor, AreaID: &podrucje, SectionCodes: dionice}
}

// Povijest zaduženja: svaka verzija iz knjige redom nastanka, a zadnje stanje
// sa zapisa na površini, od trenutka zadnje verzije. Izmjena u mjestu i
// opoziv tako ne brišu kako je zaduženje prije stajalo. Zaduženje bez
// verzija u knjizi stoji od nastanka; tuđa zaduženja ne ulaze.
func TestPovijestZaduzenja(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	d := vodocuvarDuznost(osnovaKorisnik, osnovaDionica)
	nuzno(t, repo.AddDuty(d))
	izmjena := *d
	izmjena.SectionCodes = "B.16.2"
	nuzno(t, repo.UpdateDuty(&izmjena))
	nuzno(t, repo.RevokeDuty(d.ID))

	tudji := &models.User{Username: "pperic", PasswordHash: "x", FullName: "Pero Perić", OrgType: models.OrgHrvatskeVode, IsActive: true}
	nuzno(t, repo.CreateUser(tudji, vodocuvarDuznost(uuid.Nil, osnovaDionica)))

	// zaduženje upisano mimo knjige (starija baza) nema verzija
	bezKnjige := uuid.New()
	nastanak := time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC)
	if _, err := n.db.Exec(`INSERT INTO duties (id, user_id, title, role, scope_type, area_id, section_codes, created_at, is_active)
		VALUES (?, ?, 'Vodočuvar', 'WATER_GUARD', 'AREA', 16, '', ?, 1)`, bezKnjige.String(), osnovaKorisnik.String(), nastanak); err != nil {
		t.Fatal(err)
	}

	povijest, err := repo.PovijestZaduzenja(osnovaKorisnik)
	nuzno(t, err)
	if len(povijest) != 4 {
		t.Fatalf("povijest: %d verzija, očekivano 4: %+v", len(povijest), povijest)
	}
	if !povijest[0].Od.Equal(nastanak) || povijest[0].ID != bezKnjige {
		t.Errorf("zaduženje bez knjige: %+v", povijest[0])
	}
	verzije, err := n.rec.History(ctxRaz, EntityDuties, d.ID.String())
	nuzno(t, err)
	ocekivano := []struct {
		dionice string
		aktivno bool
		od      time.Time
	}{
		{osnovaDionica, true, verzije[2].CreatedAt},
		{"B.16.2", true, verzije[1].CreatedAt},
		{"B.16.2", false, verzije[0].CreatedAt},
	}
	for i, o := range ocekivano {
		v := povijest[i+1]
		if v.ID != d.ID || v.SectionCodes != o.dionice || v.IsActive != o.aktivno || !v.Od.Equal(o.od) {
			t.Errorf("%d. verzija: %s aktivno %v od %v, očekivano %s %v od %v", i+1, v.SectionCodes, v.IsActive, v.Od, o.dionice, o.aktivno, o.od)
		}
	}
}

// Povijest koja se ne da pročitati je greška, a ne kraća povijest: baza
// bez zaduženja, knjiga koja se ne čita, verzija s nečitljivim vremenom i
// verzija s nečitljivim sadržajem
func TestPovijestZaduzenjaGreske(t *testing.T) {
	pokvari := func(t *testing.T, q string) *UserRepository {
		t.Helper()
		n := noviCvor(t, "ured")
		repo := NewUserRepository(n.db, n.rec)
		nuzno(t, repo.AddDuty(vodocuvarDuznost(osnovaKorisnik, osnovaDionica)))
		if _, err := n.db.Exec(q); err != nil {
			t.Fatal(err)
		}
		return repo
	}
	for _, s := range []struct{ ime, q string }{
		{"bez tablice zaduženja", `DROP TABLE duties`},
		{"bez knjige verzija", `DROP TABLE record_versions`},
		{"nečitljivo vrijeme verzije", `UPDATE record_versions SET created_at = 'nije vrijeme' WHERE entity = 'duties'`},
		{"nečitljiv sadržaj verzije", `UPDATE record_versions SET payload = '{' WHERE entity = 'duties'`},
	} {
		t.Run(s.ime, func(t *testing.T) {
			if p, err := pokvari(t, s.q).PovijestZaduzenja(osnovaKorisnik); err == nil {
				t.Errorf("bez greške: %+v", p)
			}
		})
	}
}
