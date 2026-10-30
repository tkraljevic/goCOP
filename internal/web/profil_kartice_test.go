package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Profil pokazuje račun e-pošte, sken potpisa i dežurstva osobe; izvođač
// kartice dežurstava nema, jer ga plaća njegova firma (Odluke
// (30. 10. 2026.)). Bez prijave profil vodi na prijavu.
func TestProfilKarticeIIzvodjac(t *testing.T) {
	o := novaOkolinaIzvana(t)
	ana := o.racun("ana", "lozinka-ane-1", "ana@primjer.hr", false)
	o.usersH.postaRacun = func(context.Context, string) (string, time.Time) {
		return "ana@primjer.hr", time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	}
	o.usersH.potpisSlika = func(context.Context, string) bool { return true }
	o.usersH.potpisniKljuc = func(context.Context, string) PodaciKljuca { return PodaciKljuca{} }
	o.usersH.planovi = func(context.Context, string) ([]models.PlanOsobe, error) {
		return []models.PlanOsobe{{JournalID: "cop-1", Naslov: "Dnevnik COP-a Primjerovo", Prvo: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Zadnje: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)}}, nil
	}
	w := o.posalji(zahtjevIzvana{stvarni: ana}, http.MethodGet, "/profile", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Dnevnik COP-a Primjerovo") {
		t.Fatalf("profil djelatnika bez kartice dežurstava: %d", w.Code)
	}
	izvodjac := o.racun("ivo", "lozinka-ive-1", "ivo@primjer.hr", false)
	izvodjac.Duties = []models.Duty{{Role: models.RoleServiceLeaderForeman, IsActive: true}}
	if planovi := o.usersH.planoviProfila(context.Background(), izvodjac); planovi != nil {
		t.Errorf("izvođač ima kartice dežurstava: %+v", planovi)
	}
	r := httptest.NewRequest(http.MethodGet, "/profile", nil)
	rec := httptest.NewRecorder()
	o.usersH.ShowProfile(rec, r)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("profil bez prijave: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}
