package models

import (
	"strings"
	"testing"
	"time"
)

// Izvornik akta sa skenom je zadnji zamijenjeni sken, a bez zamjene prvi;
// akt bez skena nema sažetak skena. Potpis zamjene pokriva akt, oba skena,
// osobu, vrijeme i razlog.
func TestSazetakSkenaIPorukaZamjene(t *testing.T) {
	a := Akt{ID: "akt-1", OvjeraKod: "KOD"}
	if a.SazetakSkena() != "" {
		t.Error("akt bez skena")
	}
	a.Rucno = &RucniPotpis{Sazetak: "prvi"}
	if a.SazetakSkena() != "prvi" {
		t.Error("bez zamjene vrijedi prvi sken")
	}
	z := ZamjenaSkena{StariSazetak: "prvi", NoviSazetak: "drugi", ZamijenioID: "u1", Zamijenio: "Iva Ivić",
		ZamijenjenoAt: time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC), Razlog: "nečitko"}
	a.ZamjeneSkena = []ZamjenaSkena{z}
	if a.SazetakSkena() != "drugi" {
		t.Error("nakon zamjene vrijedi novi sken")
	}
	if p := string(a.PorukaZamjene(z)); p != "goCOP-zamjena-skena-v1|akt-1|KOD|prvi|drugi|u1|Iva Ivić|2026-11-23T09:00:00Z|nečitko" {
		t.Errorf("poruka zamjene %q", p)
	}
	if strings.Contains(a.Sazetak(), "drugi") {
		t.Error("zamjena ne smije ući u potpisani sadržaj akta")
	}
}
