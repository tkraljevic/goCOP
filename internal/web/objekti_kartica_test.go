package web

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Kartica objekta po crtežu 17d (5b, 5d): povratak na registar, Podaci s danom
// izmjene, kod crpne stanice ploča Pogon, „Dodaj opis” kad opisa nema, a
// „Upiši očitanje” i „Očitanja” ostaju uz objekt koji prima očitanja.
func TestKarticaObjektaPoCrtezu(t *testing.T) {
	var reg models.StructureRegister
	for _, r := range models.StructureRegisters {
		if r.Kind == models.StructureKindPumpingStation {
			reg = r
		}
	}
	cs := &models.Structure{ID: uuid.MustParse("9f1c0b2e-0000-7000-8000-000000000041"), Name: "CS Primjerovo", Code: "cs-primjerovo",
		Kind: models.StructureKindPumpingStation, SectorID: "P", AreaID: 1, CapacityText: "2x2,15", BrojAgregata: 2,
		StartText: "145", StopText: "80", ImaUstavu: true, UpdatedAt: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)}
	html := iscrtaj(t, "structure_detail.html", StructurePageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Structure: cs, CanEdit: true, Register: reg,
	})
	for _, want := range []string{
		`class="povratna-poveznica">← ` + reg.Title, "<h2>Podaci</h2>", "izmijenjen 4. 11. 2026.",
		"<h2>Pogon</h2>", "2x2,15 m³/s", "145 cm", "80 cm", "ugrađena, gravitacijsko upuštanje i ispuštanje",
		"Upiši očitanje", `href="/readings/structure/` + cs.ID.String() + `"`,
		"Dodaj opis", `href="` + cs.Path() + `/edit"`, "Objekt nije vezan ni na jednu dionicu.",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("kartica crpne stanice nema %q", want)
		}
	}
	nasip := &models.Structure{ID: uuid.New(), Name: "Nasip Primjerovo", Kind: models.StructureKindEmbankment, Notes: "Kruna 4 m."}
	html = iscrtaj(t, "structure_detail.html", StructurePageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{}, Structure: nasip,
	})
	for _, ne := range []string{"<h2>Pogon</h2>", "Dodaj opis", "Upiši očitanje", "/edit"} {
		if strings.Contains(html, ne) {
			t.Errorf("kartica nasipa bez prava nudi %q", ne)
		}
	}
}
