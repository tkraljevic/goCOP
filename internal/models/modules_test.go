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
