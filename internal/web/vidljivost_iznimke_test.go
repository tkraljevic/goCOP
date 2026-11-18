package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Vidljivost modula: prilagođena uloga ima „Vrati zadano” sa zadanim
// modulima, a kartica iznimaka nabraja račune s iznimkom imenima modula
// (Odluke (18. 11. 2026.), Administracija, novo ponašanje 3)
func TestVidljivostVratiZadanoIIznimke(t *testing.T) {
	o := novaOkolinaPrijave(t)
	ctx := context.Background()
	moduli := service.NewModuleService(repository.NewModuleRepository(o.baza, ledger.New(o.baza, "ured")))
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	ana := o.osoba("aprimjer", "anina-lozinka", "ana@voda.hr", false)
	if err := moduli.SetRoleRule(ctx, admin, models.RoleNationalLeader, []string{models.ModuleField}); err != nil {
		t.Fatal(err)
	}
	if err := moduli.SetUserOverride(ctx, admin, ana.ID.String(), []string{models.ModuleAdmin}, []string{models.ModuleRegisters}); err != nil {
		t.Fatal(err)
	}

	h := NewModulesHandler(moduli, predlozakStranice(t, "moduli.html"))
	r := httptest.NewRequest(http.MethodGet, "/moduli", nil)
	r = r.WithContext(context.WithValue(context.WithValue(r.Context(), contextKeyUser, &models.User{FullName: "Marko Primjer"}), contextKeyPerms, admin))
	w := httptest.NewRecorder()
	h.ShowMatrix(w, r)
	tijelo := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	zadano := models.JoinModules(models.DefaultModules(models.RoleNationalLeader))
	if strings.Count(tijelo, "Vrati zadano</button>") != 1 || !strings.Contains(tijelo, `data-zadano="`+zadano+`"`) {
		t.Errorf("„Vrati zadano” samo uz prilagođenu ulogu, sa zadanim %q", zadano)
	}
	for _, treba := range []string{"<th scope=\"row\">aprimjer</th>", "<td>" + models.ModuleLabel(models.ModuleAdmin) + "</td>",
		"<td>" + models.ModuleLabel(models.ModuleRegisters) + "</td>", `href="/users/` + ana.ID.String() + `#os-moduli"`} {
		if !strings.Contains(tijelo, treba) {
			t.Errorf("na kartici iznimaka nema %q", treba)
		}
	}

	// bez iznimki kartica to i kaže
	if err := moduli.SetUserOverride(ctx, admin, ana.ID.String(), nil, nil); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.ShowMatrix(w, r)
	if !strings.Contains(w.Body.String(), "Nijedan račun nema iznimku") {
		t.Error("prazna kartica iznimaka")
	}
}

func TestIznimkaRacunaKojegNemaJeNepoznata(t *testing.T) {
	got := iznimkePrikaz([]repository.IznimkaRacuna{{UserModules: models.UserModules{UserID: "x", Shown: []string{models.ModuleUsers}}}})
	if len(got) != 1 || got[0].Ime != "nepoznat račun" || got[0].Prikazano != models.ModuleLabel(models.ModuleUsers) || got[0].Skriveno != "" {
		t.Errorf("%+v", got)
	}
}
