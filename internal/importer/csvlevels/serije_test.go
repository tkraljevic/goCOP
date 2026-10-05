package csvlevels

import (
	"context"
	"strings"
	"testing"
)

// Upis ide u serijama, svaka u svojoj transakciji. Kad serija padne, poništi
// se cijela, pa se njezini redci ne smiju brojati kao upisani; ono što su
// upisale ranije serije ostaje i greška kaže koliko ga je.
func TestUvozTabliceGreskaUDrugojSeriji(t *testing.T) {
	stara := velicinaSerije
	velicinaSerije = 2
	t.Cleanup(func() { velicinaSerije = stara })

	o := novaOkolinaTablice(t)
	// vodostaj 666 baza odbija, pa druga serija pada na svom drugom retku,
	// nakon što je prvi već prošao unutar njezine transakcije
	if _, err := o.baza.Exec(`CREATE TRIGGER probna_greska BEFORE INSERT ON readings
		WHEN NEW.level_cm = 666 BEGIN SELECT RAISE(ABORT, 'probna greška upisa'); END`); err != nil {
		t.Fatal(err)
	}
	put := napisi(t, []string{
		"Datum;Primjerovo",
		"01.09.2026.;120",
		"02.09.2026.;121",
		"03.09.2026.;122",
		"04.09.2026.;666",
		"05.09.2026.;123",
	})
	rep, err := Run(context.Background(), Options{Path: put, Deps: o.deps})
	if err == nil || !strings.Contains(err.Error(), "probna greška upisa") {
		t.Fatalf("greška upisa nije javljena: %v", err)
	}
	if !strings.Contains(err.Error(), "upisano 2 od 5") {
		t.Errorf("greška ne kaže koliko je upisano prije nje: %v", err)
	}
	if rep.Inserted != 2 {
		t.Errorf("izvješće broji %d upisanih, a upisane su samo dvije iz prve serije", rep.Inserted)
	}
	if n := len(o.ocitanja(t, o.primjerovo.ID.String())); n != 2 {
		t.Errorf("u bazi %d očitanja, očekivana 2", n)
	}
}
