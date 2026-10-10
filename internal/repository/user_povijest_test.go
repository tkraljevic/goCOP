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
// opoziv tako ne brišu kako je zaduženje prije stajalo. Zaduženje stoji od
// nastanka, i kad nema verzija u knjizi; tuđa zaduženja ne ulaze.
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
		{osnovaDionica, true, d.CreatedAt},
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

// Zaduženje upisano mimo knjige verzija (imenik pri prvom pokretanju)
// dobiva prvu verziju tek pri prvoj izmjeni ili opozivu; do nje stoji od
// nastanka, aktivno, pa zapisi iz tog vremena ostaju vezani uz njega
// (Odluke (8. 10. 2026.), pitanje 1). Podaci su izmišljeni.
func TestPovijestZaduzenjaUpisanogMimoKnjige(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	nastanak := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	mimoKnjige := func(dionice string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if _, err := n.db.Exec(`INSERT INTO duties (id, user_id, title, role, scope_type, area_id, section_codes, created_at, is_active)
			VALUES (?, ?, 'Vodočuvar', 'WATER_GUARD', 'SECTION', 16, ?, ?, 1)`, id.String(), osnovaKorisnik.String(), dionice, nastanak); err != nil {
			t.Fatal(err)
		}
		return id
	}
	opozvano, izmijenjeno := mimoKnjige(osnovaDionica), mimoKnjige("B.16.2")
	nuzno(t, repo.RevokeDuty(opozvano))
	d, err := repo.GetDuty(izmijenjeno)
	nuzno(t, err)
	d.Title = "Vodočuvar dionice"
	nuzno(t, repo.UpdateDuty(d))

	povijest, err := repo.PovijestZaduzenja(osnovaKorisnik)
	nuzno(t, err)
	usred := time.Date(2026, 6, 1, 0, 0, 0, 0, models.Zagreb)
	if got := models.ZaduzenjaUDanu(povijest, usred); len(got) != 2 {
		t.Errorf("između nastanka i prve verzije: %+v", got)
	}
	if got := models.ZaduzenjaUDanu(povijest, time.Now().AddDate(0, 0, 1)); len(got) != 1 || got[0].ID != izmijenjeno || got[0].Title != "Vodočuvar dionice" {
		t.Errorf("nakon opoziva i izmjene: %+v", got)
	}
}

// Uloga koju je osoba ikad imala: na zapisu zaduženja i kad je opozvano, i
// u ranijoj verziji zaduženja koje je poslije izmijenjeno u drugu ulogu.
// Tuđa zaduženja ne ulaze, a knjiga koja se ne čita je greška (Odluke (8.
// 10. 2026.), pitanje 1: poslovođa ne dobiva dnevnike drugih firmi
// opozivom dužnosti). Podaci su izmišljeni.
func TestImaoUlogu(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	poslovodja := func(korisnik uuid.UUID) *models.Duty {
		sektor, podrucje := "B", 16
		return &models.Duty{UserID: korisnik, Title: "Poslovođa", Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje}
	}
	osoba := func(korisnik, ime string, d *models.Duty) (uuid.UUID, *models.Duty) {
		t.Helper()
		u := &models.User{Username: korisnik, PasswordHash: "x", FullName: ime, OrgType: models.OrgPravnaOsoba, IsActive: true}
		nuzno(t, repo.CreateUser(u, d))
		return u.ID, d
	}
	opozvan, d := osoba("mmarkic", "Marko Markić", poslovodja(uuid.Nil))
	nuzno(t, repo.RevokeDuty(d.ID))
	izmijenjen, d := osoba("iivic", "Ivo Ivić", poslovodja(uuid.Nil))
	d.Role, d.Title = models.RoleWaterGuard, "Vodočuvar"
	nuzno(t, repo.UpdateDuty(d))
	nikad, _ := osoba("pperic", "Pero Perić", vodocuvarDuznost(uuid.Nil, osnovaDionica))
	osoba("aanic", "Ana Anić", poslovodja(uuid.Nil))
	for _, s := range []struct {
		ime     string
		osoba   uuid.UUID
		uloga   models.Role
		ocekuje bool
	}{
		{"opozvana dužnost", opozvan, models.RoleServiceLeaderForeman, true},
		{"izmijenjena u drugu ulogu", izmijenjen, models.RoleServiceLeaderForeman, true},
		{"izmijenjena, nova uloga", izmijenjen, models.RoleWaterGuard, true},
		{"nikad nije imao ulogu", nikad, models.RoleServiceLeaderForeman, false},
		{"uloga drugoga", opozvan, models.RoleWaterGuard, false},
	} {
		got, err := repo.ImaoUlogu(s.osoba, s.uloga)
		nuzno(t, err)
		if got != s.ocekuje {
			t.Errorf("%s: %v, očekivano %v", s.ime, got, s.ocekuje)
		}
	}
	if _, err := n.db.Exec(`DROP TABLE record_versions`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ImaoUlogu(nikad, models.RoleServiceLeaderForeman); err == nil {
		t.Error("knjiga verzija se ne čita, a nema greške")
	}
}
