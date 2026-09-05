package service

import (
	"regexp"
	"strings"
	"testing"

	"gocop/internal/models"

	"github.com/google/uuid"
)

// Privremena lozinka čita se preko telefona: tri riječi i tri znamenke, bez
// slova koja se u izgovoru miješaju, i svaki put drukčija.
func TestPrivremenaLozinkaSeMozeProcitatiPrekoTelefona(t *testing.T) {
	oblik := regexp.MustCompile(`^[a-z0-9]+-[a-z0-9]+-[a-z0-9]+-\d{3}$`)
	vidjene := map[string]bool{}
	for i := 0; i < 50; i++ {
		lozinka, err := GenerateTempPassword()
		if err != nil {
			t.Fatal(err)
		}
		if !oblik.MatchString(lozinka) {
			t.Fatalf("lozinka %q nije oblika rijec-rijec-rijec-123", lozinka)
		}
		if len(lozinka) < 6 {
			t.Errorf("lozinka %q je kraća od najmanje dopuštene", lozinka)
		}
		if strings.ContainsAny(lozinka, "čćđšžCĆĐŠŽ ") {
			t.Errorf("lozinka %q nosi znak koji se preko telefona krivo čuje", lozinka)
		}
		vidjene[lozinka] = true
	}
	if len(vidjene) < 45 {
		t.Errorf("od 50 lozinki samo %d različitih — izvor slučajnosti je preslab", len(vidjene))
	}
}

// Tko smije poništiti tuđu lozinku: globalni administrator svakome, a
// administrator sektora ili područja samo onima koji tamo imaju zaduženje.
func TestPravoPonistavanjaTudjeLozinke(t *testing.T) {
	sektorB, podrucje16 := "B", 16
	uSektoru := &models.User{ID: uuid.New(), Duties: []models.Duty{{SectorID: &sektorB}}}
	uPodrucju := &models.User{ID: uuid.New(), Duties: []models.Duty{{AreaID: &podrucje16}}}
	tudji := &models.User{ID: uuid.New(), Duties: []models.Duty{{SectorID: strPtr("D")}}}
	globalni := &models.User{ID: uuid.New(), IsGlobalAdmin: true}

	admin := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AdminAreas: map[int]bool{16: true}}
	globalniAdmin := &models.UserPermissions{IsGlobalAdmin: true}

	cases := []struct {
		naziv  string
		actor  *models.UserPermissions
		target *models.User
		want   bool
	}{
		{"administrator sektora nad svojim čovjekom", admin, uSektoru, true},
		{"administrator područja nad svojim čovjekom", admin, uPodrucju, true},
		{"administrator nad tuđim sektorom", admin, tudji, false},
		{"administrator nad globalnim administratorom", admin, globalni, false},
		{"globalni administrator nad svakim", globalniAdmin, tudji, true},
		{"bez ovlasti", nil, uSektoru, false},
	}
	for _, c := range cases {
		if got := canManageTarget(c.actor, c.target); got != c.want {
			t.Errorf("%s: dobiveno %v, očekivano %v", c.naziv, got, c.want)
		}
	}
}

func strPtr(s string) *string { return &s }
