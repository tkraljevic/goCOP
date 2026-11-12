package web

import (
	"strings"
	"testing"

	"gocop/internal/models"
)

// Obrazac dionice po crtežu 3c: povratak na karticu, ploča Osnovno u kojoj
// se u izmjeni područje i šifra samo pokazuju, a šalju se skrivenim poljima
// kao i prije; dolje Odustani i Spremi, bez brisanja.
func TestObrazacDionicePoCrtezu(t *testing.T) {
	d := SectionPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Section:     models.Section{Code: "B.16.1", AreaID: 16, AreaName: "Mali sliv Baranja", SectorID: "B"},
		IsEdit:      true,
	}
	html := iscrtaj(t, "section_form.html", d)
	for _, want := range []string{
		`href="/sections/B.16.1" class="povratna-poveznica">← B.16.1`,
		"Uredi dionicu B.16.1", "Polja sa zvjezdicom su obavezna.",
		`<input type="hidden" name="code" value="B.16.1">`,
		`<input type="hidden" name="area_id" value="16">`,
		`value="16: Mali sliv Baranja"`,
		`name="length_km"`, `name="embankment_km"`, `name="description_custom"`, `name="description"`, `name="notes"`,
		`placeholder="npr. prilaz, ključ ustave, posebnosti"`,
		`>Spremi</button>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("obrazac nema %q", want)
		}
	}
	// Prikaz područja ne smije dobiti id area_id: skripta po njemu bira nasipe
	// samo kod nove dionice.
	if strings.Count(html, `id="area_id"`) != 0 {
		t.Error("izmjena dionice ima polje area_id za skriptu")
	}
	for _, ne := range []string{"Obriši dionicu", `name="dalje"`} {
		if strings.Contains(html, ne) {
			t.Errorf("izmjena dionice nudi %q", ne)
		}
	}

	d.IsEdit = false
	d.Section = models.Section{}
	d.PredlozenaSifra = "B.16.11"
	html = iscrtaj(t, "section_form.html", d)
	for _, want := range []string{`class="povratna-poveznica">← Dionice`, ">Nova dionica<", `id="area_id" name="area_id"`,
		`name="code" class="form-control font-mono" required`, `name="dalje" value="1"`} {
		if !strings.Contains(html, want) {
			t.Errorf("nova dionica nema %q", want)
		}
	}
}
