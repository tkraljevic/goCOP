package web

import (
	"io/fs"
	"strings"
	"testing"

	"gocop/internal/models"
	webassets "gocop/web"
)

// Odluke (15. 11. 2026.): izvođač je vanjski suradnik, pa na Profilu nema dežurstava ni sati, a
// Profil mu to tako i kaže, a ne obećava plan u koji se „upiše sam”.
func TestProfilIzvodjacuKazeDaDezurstavaNema(t *testing.T) {
	izvodjac := UserPageData{User: &models.User{Duties: []models.Duty{{Role: models.RoleServiceLeaderForeman, IsActive: true}}}}
	if !izvodjac.IzvodjacZaCOP() {
		t.Error("poslovođa izvođača mora biti izvođač za COP")
	}
	if (UserPageData{User: &models.User{Duties: []models.Duty{{Role: models.RoleWaterGuard, IsActive: true}}}}).IzvodjacZaCOP() || (UserPageData{}).IzvodjacZaCOP() {
		t.Error("vodočuvar i prazan profil nisu izvođači")
	}
	b, err := fs.ReadFile(webassets.Files, "templates/profile.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "{{if .IzvodjacZaCOP}}Izvođač je vanjski suradnik: njegova dežurstva i sate vodi njegova tvrtka, pa ih na Profilu nema.{{else}}") {
		t.Error("Profil izvođaču mora reći da dežurstava i sati ondje nema")
	}
	if strings.Contains(porukaIzvodjacuCOP, "Profil") {
		t.Error("poruka izvođaču ne smije ga slati na Profil po sate")
	}
}
