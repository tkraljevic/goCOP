package models

import "testing"

func TestDefaultModulesAlwaysIncludeField(t *testing.T) {
	roles := []Role{
		RoleGlobalAdmin,
		RoleServiceLeaderForeman,
		RoleViewer,
		RoleWaterGuard,
		RoleCopLeader,
	}
	for _, role := range roles {
		found := false
		for _, module := range DefaultModules(role) {
			if module == ModuleField {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("uloga %s nema zadani modul Teren", role)
		}
	}
}

// Poslovođa izvođača vidi Očitanja: Teren je ukinut, a bez njih ne bi imao
// puta do letava (Odluke (15. 11. 2026.), 1).
func TestPoslovodjaVidiOcitanja(t *testing.T) {
	for _, m := range DefaultModules(RoleServiceLeaderForeman) {
		if m == ModuleReadings {
			return
		}
	}
	t.Error("poslovođa nema zadani modul Očitanja")
}
