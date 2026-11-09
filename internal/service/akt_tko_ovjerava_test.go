package service_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Rečenica „Tko ovjerava” na obrascu novog akta mora reći isto što pušta
// ovjera (SmijeOvjeriti): rukovoditelj područja spominje se kao onaj tko
// ovjerava samo za stupnjeve koje on smije ovjeriti, a rukovoditelj sektora
// ovjerava svaki stupanj.
func TestTkoOvjeravaPratiPraviloOvjere(t *testing.T) {
	sektor, podrucje := "B", 34
	osoba := func(uloga models.Role) *models.UserPermissions {
		return models.NewUserPermissions(models.User{ID: uuid.New(), IsActive: true, Duties: []models.Duty{
			{Role: uloga, SectorID: &sektor, AreaID: &podrucje, IsActive: true}}})
	}
	rukPodrucja, rukSektora := osoba(models.RoleAreaLeader), osoba(models.RoleSectorLeader)
	ovjera := &service.AktService{}
	for _, stupanj := range models.StupnjeviAkta {
		tekst := service.TkoOvjerava(stupanj, sektor)
		akt := &models.Akt{Stupanj: stupanj, Sektor: sektor, AreaID: podrucje}
		if strings.Contains(tekst, "za branjeno područje akta") != ovjera.SmijeOvjeriti(rukPodrucja, akt) {
			t.Errorf("%s: rečenica %q ne odgovara ovjeri rukovoditelja područja", stupanj, tekst)
		}
		if !ovjera.SmijeOvjeriti(rukSektora, akt) {
			t.Errorf("%s: rukovoditelj sektora mora smjeti ovjeriti", stupanj)
		}
		if strings.Contains(tekst, "34") {
			t.Errorf("%s: bez izbora područja rečenica ne smije imati broj područja: %q", stupanj, tekst)
		}
	}
	for stupanj, mora := range map[models.DefensePhase]string{
		models.PhasePrep:      "Rukovoditelj obrane od poplava za branjeno područje akta ili viši.",
		models.PhaseRegular:   "Rukovoditelj obrane od poplava za branjeno područje akta ili viši.",
		models.PhaseEmergency: "Rukovoditelj obrane od poplava Sektora B ili viši. Rukovoditelj branjenog područja izvanrednu obranu ne ovjerava.",
		models.PhaseState:     "Rukovoditelj obrane od poplava Sektora B ili viši. U hitnom slučaju i rukovoditelj obrane od poplava za branjeno područje akta.",
	} {
		if got := service.TkoOvjerava(stupanj, sektor); got != mora {
			t.Errorf("%s: %q, a treba %q", stupanj, got, mora)
		}
	}
	if got := service.TkoOvjerava(models.PhaseEmergency, ""); !strings.HasPrefix(got, "Rukovoditelj obrane od poplava sektora ili viši.") {
		t.Errorf("bez sektora: %q", got)
	}
}
