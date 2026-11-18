package web

import (
	"strconv"
	"testing"

	"gocop/internal/ledger"
)

// Knjiga verzija: hrvatska imena, najveće vrste prve, ostale u jednom retku.
func TestVrsteUKnjizi(t *testing.T) {
	v := vrsteUKnjizi(map[string]int{"readings": 300, "users": 20, "nepoznata_vrsta": 5}, 9)
	if len(v) != 3 || v[0] != (VrstaUKnjizi{"Očitanja", 300}) || v[1] != (VrstaUKnjizi{"Djelatnici", 20}) || v[2].Naziv != "nepoznata_vrsta" {
		t.Errorf("vrste: %+v", v)
	}
	mnogo := map[string]int{}
	for i := 0; i < 12; i++ {
		mnogo["vrsta_"+strconv.Itoa(i)] = 100 - i
	}
	v = DBMaintPageData{Stats: ledger.Stats{ByEntity: mnogo}}.VrsteUKnjizi()
	if len(v) != 9 || v[8].Naziv != "Ostale vrste" || v[8].Broj != 92+91+90+89 {
		t.Errorf("ostale vrste: %+v", v)
	}
}
