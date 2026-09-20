package service

import (
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Dnevnik je vodočuvarev: kolega s istog područja ga ne vidi, iako na tom
// području smije pisati. Vide ga vodočuvar, oni koji ga ovjeravaju i
// parafiraju, i administracija.
func TestTudjiDnevnikNeVidiKolegaSIstogPodrucja(t *testing.T) {
	bp := 16
	sektor := "B"
	vodocuvarID := uuid.New()
	list := &models.VodocuvarskiList{UserID: vodocuvarID.String(), Sektor: sektor, AreaID: bp}
	s := &VodocuvarService{}

	osoba := func(id uuid.UUID, uloga models.Role) *models.UserPermissions {
		u := models.User{ID: id, FullName: "proba", Duties: []models.Duty{{
			IsActive: true, Role: uloga, ScopeType: models.ScopeArea,
			SectorID: &sektor, AreaID: &bp,
		}}}
		p := models.NewUserPermissions(u)
		return p
	}
	sam := osoba(vodocuvarID, models.RoleWaterGuard)
	if !s.SmijeVidjeti(sam, list) {
		t.Error("vodočuvar ne vidi vlastiti list")
	}
	kolega := osoba(uuid.New(), models.RoleWaterGuard)
	if s.SmijeVidjeti(kolega, list) {
		t.Error("kolega s istog područja vidi tuđi dnevnik")
	}
	ruk := osoba(uuid.New(), models.RoleAreaLeader)
	if !s.SmijeVidjeti(ruk, list) {
		t.Error("rukovoditelj branjenog područja ne vidi list")
	}
}
