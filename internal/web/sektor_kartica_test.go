package web

import (
	"strings"
	"testing"

	"gocop/internal/models"
)

// Kartica sektora po crtežu (17a, 2b): povratak na organizaciju, Dionice sektora i Excel,
// Podaci kao retci s razinom i praznim „nije upisana”. Bez prava na žig nema odjeljka žiga,
// a krovna jedinica nema dionica.
func TestKarticaSektoraPoCrtezu(t *testing.T) {
	sektor := models.Sector{ID: "P", Name: "Sektor P — Primjerica", Level: 2, VgoName: "VGO Primjerovo", CenterCop: "COP Primjerovo", Phone: "031/000-002"}
	html := iscrtaj(t, "sektor.html", SektorData{CurrentUser: &models.User{FullName: "Ana Anić"}, Permissions: &models.UserPermissions{}, Sektor: sektor})
	for _, mora := range []string{
		`<a href="/organizacija" class="povratna-poveznica">← Administrativna organizacija</a>`,
		`href="/sections?sector=P"`, `href="/sections/dionice.xlsx?sector=P"`,
		`P · prvi dio šifre dionice`, `2 · sektor`, `COP Primjerovo`, `tel:031/000-002`, `nije upisana`,
	} {
		if !strings.Contains(html, mora) {
			t.Errorf("kartica sektora nema %q", mora)
		}
	}
	for _, ne := range []string{`id="zig"`, `/organizacija/sektori/P/edit`} {
		if strings.Contains(html, ne) {
			t.Errorf("osoba bez prava vidi %q", ne)
		}
	}

	krovna := models.Sector{ID: "DIREKCIJA", Name: "Direkcija, Zagreb", Level: 1}
	html = iscrtaj(t, "sektor.html", SektorData{CurrentUser: &models.User{FullName: "Ana Anić"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true}, Sektor: krovna})
	if strings.Contains(html, "Dionice sektora") || !strings.Contains(html, "1 · krovna jedinica") || !strings.Contains(html, `/organizacija/sektori/DIREKCIJA/edit`) {
		t.Error("krovna jedinica: krivi natpisi ili gumbi")
	}
}
