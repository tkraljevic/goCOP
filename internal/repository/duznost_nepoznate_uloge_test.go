package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Nova uloga je samo nova vrijednost u polju uloge dužnosti. Čvor koji
// ulogu zna (rukovoditelj obrane izvođača, od izdanja s njom) primljenu
// dužnost stavlja na površinu i daje joj prava. Stariji čvor, koji je ne
// zna, dužnost isto prima i čuva bez čekanja na potvrdu, ali joj ne daje
// pravo upisa (models.Role.Writes); ovdje to glumi uloga koju ni ovo izdanje
// ne zna. Zato se te dužnosti dodjeljuju tek kad su sva računala na novom
// izdanju.
func TestPrimljenaDuznostNoveUloge(t *testing.T) {
	for _, s := range []struct {
		uloga models.Role
		pise  bool
	}{
		{models.RoleContractorAreaLeader, true},
		{models.RoleContractorAreaDeputy, true},
		{models.Role("ULOGA_IZ_NOVIJEG_IZDANJA"), false},
	} {
		t.Run(string(s.uloga), func(t *testing.T) {
			c, u := uredSRacunom(t, false)
			if _, err := c.db.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO P', 'COP P')`); err != nil {
				t.Fatal(err)
			}
			if _, err := c.db.Exec(`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (7, 'P', 'Područje 7', 'VGI 7')`); err != nil {
				t.Fatal(err)
			}
			sektor, podrucje := "P", 7
			d := models.Duty{ID: uuid.New(), UserID: u.ID, Title: "Dužnost s drugog računala", Role: s.uloga, ScopeType: models.ScopeArea,
				SectorID: &sektor, AreaID: &podrucje, IsActive: true, CreatedAt: time.Now()}
			primi(t, c, []ledger.Version{tudja(t, EntityDuties, d.ID.String(), d)})
			if p := cekaju(t, c); len(p) != 0 {
				t.Fatalf("dužnost čeka potvrdu: %+v", p)
			}
			repo := NewUserRepository(c.db, c.rec)
			x, err := repo.GetDuty(d.ID)
			if err != nil || x == nil || x.Role != s.uloga || !x.IsActive || x.AreaID == nil || *x.AreaID != 7 {
				t.Fatalf("dužnost na površini: %+v, %v", x, err)
			}
			racun, err := repo.GetUserByID(u.ID)
			if err != nil || racun == nil {
				t.Fatal(err)
			}
			p := models.NewUserPermissions(*racun)
			if p.HasWriteAccess("", 7, "") != s.pise || len(p.AdminAreas) != 0 || p.IsGlobalAdmin {
				t.Errorf("pisanje na području 7: %v, uprava %v", p.HasWriteAccess("", 7, ""), p.AdminAreas)
			}
		})
	}
}
