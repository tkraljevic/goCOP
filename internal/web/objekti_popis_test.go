package web

import (
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Popis objekata po crtežu 17d, 5a: čipovi vode na sve postojeće registre s
// brojem zapisa, tablica bez .table-responsive, stupci crpne stanice i
// „Bez dionice” kad objekt nije ni na jednoj dionici.
func TestPopisObjekataPoCrtezu(t *testing.T) {
	var cs models.StructureRegister
	for _, r := range models.StructureRegisters {
		if r.Kind == models.StructureKindPumpingStation {
			cs = r
		}
	}
	puskas := models.Structure{ID: uuid.MustParse("9f1c0b2e-0000-7000-8000-000000000031"), Name: "CS Primjerovo",
		Kind: models.StructureKindPumpingStation, AreaID: 16, CapacityText: "2x2,15", BrojAgregata: 2,
		StartText: "145", StopText: "80", ImaUstavu: true}
	d := StructuresPageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Register: cs, Kinds: cs.Kinds, Structures: []models.Structure{puskas}, CanCreate: true,
	}
	poVrsti := map[string]int{models.StructureKindPumpingStation: 12, models.StructureKindEmbankment: 78}
	d.CipoviRegistara = cipoviRegistaraObjekata(cs, poVrsti, url.Values{"area": {"16"}, "stranica": {"2"}})
	html := iscrtaj(t, "structures.html", d)
	for _, reg := range models.StructureRegisters {
		if !strings.Contains(html, `href="`+reg.Path+`?area=16"`) {
			t.Errorf("nema čipa za registar %s", reg.Path)
		}
	}
	for _, want := range []string{
		`class="povratna-poveznica">← Registri`, `aria-pressed="true">`, "Crpne stanice (12)", "Nasipi (78)",
		`href="` + puskas.Path() + `">CS Primjerovo</a>`, "2x2,15", "2 agregata", "145 · 80", "ugrađena",
		`<span class="oznaka-stanja obrub">Bez dionice</span>`, "Povezanost ustava i CS", `data-listanje="popis"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("popis crpnih stanica nema %q", want)
		}
	}
	if strings.Contains(html, "table-responsive") {
		t.Error("popis je u table-responsive")
	}
}
