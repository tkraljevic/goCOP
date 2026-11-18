package web

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Na stranici Potpisi tablica ključeva kaže je li računu osobe prijava
// uključena, a kartica nabraja tko po sektoru smije ovjeriti akte (Odluke
// (18. 11. 2026.), Administracija, novo ponašanje 13)
func TestPotpisiStanjeRacunaIOvjeritelji(t *testing.T) {
	ctx := context.Background()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "potpisi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop, address, phone, email) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek', '', '', '')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (34, 'B', 'Drava i Dunav', 'COP', 'Osijek')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "cvor")
	repo := repository.NewUserRepository(baza, rec)
	auth := service.NewAuthService(repo, repository.NewSessionRepository(baza))
	users := service.NewUserService(repo, auth, service.NewSSEBroker())
	sektor, podrucje := "B", 34
	osoba := func(ime string, d *models.Duty) *models.User {
		t.Helper()
		hash, err := auth.HashPassword("lozinka-" + ime)
		if err != nil {
			t.Fatal(err)
		}
		u := &models.User{Username: ime, FullName: ime, PasswordHash: hash, IsActive: true, OrgType: models.OrgHrvatskeVode}
		if err := repo.CreateUser(u, nil); err != nil {
			t.Fatal(err)
		}
		if d != nil {
			d.UserID, d.IsActive = u.ID, true
			if err := repo.AddDuty(d); err != nil {
				t.Fatal(err)
			}
		}
		return u
	}
	ana := osoba("Ana Primjer", &models.Duty{Role: models.RoleSectorLeader, SectorID: &sektor, Title: "Rukovoditeljica sektora", ScopeType: models.ScopeSector})
	osoba("Ivo Primjer", &models.Duty{Role: models.RoleAreaLeader, SectorID: &sektor, AreaID: &podrucje, ScopeType: models.ScopeArea})
	osoba("Pero Primjer", &models.Duty{Role: models.RoleWaterGuard, SectorID: &sektor, AreaID: &podrucje, ScopeType: models.ScopeArea})
	luka := osoba("Luka Primjer", nil)

	_, kc, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ps := service.NewPotpisService(repository.NewPotpisRepository(baza, rec), users, "cvor", kc, auth.CheckPassword)
	if err := ps.Pokreni(ctx); err != nil {
		t.Fatal(err)
	}
	for _, u := range []*models.User{ana, luka} {
		if _, err := ps.Novi(ctx, u, "lozinka-"+u.FullName); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := baza.Exec(`UPDATE users SET is_active = 0 WHERE id = ?`, luka.ID.String()); err != nil {
		t.Fatal(err)
	}

	h := NewPotpisHandler(func() *service.PotpisService { return ps }, users, predlozakStranice(t, "administracija_potpisi.html"))
	w := httptest.NewRecorder()
	h.ShowAdministracija(w, zahtjevPostavki(http.MethodGet, "/administracija/potpisi", nil, kaoAdmin))
	html := w.Body.String()
	for _, mora := range []string{
		`<a href="/users/` + ana.ID.String() + `">Ana Primjer</a>`, `<span class="rac-cip dobro">aktivan</span>`, `<span class="rac-cip pozor">prijava isključena</span>`,
		`id="ovjeritelji"`, "<td>Rukovoditeljica sektora</td><td>cijeli sektor, sva stanja</td>",
		"<td>BP 34, osim izvanredne obrane</td>",
	} {
		if !strings.Contains(html, mora) {
			t.Errorf("stranica potpisa nema %q", mora)
		}
	}
	if strings.Contains(html, ">Pero Primjer<") {
		t.Error("vodočuvar ne ovjerava akte")
	}
}
