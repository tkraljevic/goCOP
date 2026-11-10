package web

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Plan dežurstava po crtežu: „Dodaj dežurstvo” otvara obrazac iznad popisa,
// otvoren je samo dok je plan prazan; redak nosi kratki dan i „Potvrdi” za
// upravu centra, a mobilni dio od–do i mjesto rada.
func TestPlanDezurstavaIzgled(t *testing.T) {
	od := time.Date(2026, 10, 26, 7, 0, 0, 0, models.Zagreb)
	d := JournalPageData{
		CurrentUser: &models.User{FullName: "Marko Marić"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Journal:      &models.Journal{ID: "dn-7", Kind: models.JournalKindDefense, CentarSektor: "B", Title: "Dnevnik COP-a, 2026."},
		UpravaCentra: true, VidiPlan: true, KarticaCOP: "plan", Today: "2026-10-26",
	}
	html := iscrtaj(t, "dnevnik_dezurstva.html", d)
	if !strings.Contains(html, `<details class="plan-dodaj" open>`) || !strings.Contains(html, "U planu još nema dežurstava.") {
		t.Error("prazan plan nema otvoren obrazac")
	}
	d.Dezurstva = []models.Dezurstvo{{ID: "d1", UserName: "Ivan Ivić", Od: od, Do: od.Add(8 * time.Hour), Opis: "Obilazak i pregled obrambenih objekata"}}
	html = iscrtaj(t, "dnevnik_dezurstva.html", d)
	for _, want := range []string{
		`<details class="plan-dodaj">`, `id="novo-dezurstvo"`, "pon 26. 10. 07:00", `class="plan-od-mob">pon 26. 10. 07:00 – </span>15:00`,
		`class="btn btn-secondary plan-potvrdi"`, ">makni</button>", "Što uprava upiše potvrđeno je odmah.", "U obračun ulazi samo potvrđeno.",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("plan nema %q", want)
		}
	}
}
