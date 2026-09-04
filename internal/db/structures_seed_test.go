package db

import (
	"path/filepath"
	"testing"
)

// Registar objekata iz evidencije Baranje: crpne stanice i ustave, vezane
// na vodomjere istog naziva i na dionice koje ih spominju
func TestObjektiBaranjeVezaniNaVodomjereIDionice(t *testing.T) {
	database, err := OpenDB(filepath.Join(t.TempDir(), "objekti.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := InitSchema(database); err != nil {
		t.Fatal(err)
	}
	if err := SeedInitialData(database); err != nil {
		t.Fatal(err)
	}

	if n := count(t, database, `SELECT COUNT(*) FROM structures`); n != 26 {
		t.Errorf("objekata = %d, očekivano 26 (12 crpnih stanica + 14 ustava)", n)
	}
	if n := count(t, database, `SELECT COUNT(*) FROM structures WHERE kind = 'CRPNA_STANICA'`); n != 12 {
		t.Errorf("crpnih stanica = %d, očekivano 12", n)
	}
	if n := count(t, database, `SELECT COUNT(*) FROM structures WHERE station_id <> ''`); n < 7 {
		t.Errorf("objekata s vodomjerom = %d, očekivano barem 7 (CS Draž, ustava Bilje, Zmajevac…)", n)
	}
	if n := count(t, database, `SELECT COUNT(*) FROM section_structures`); n < 4 {
		t.Errorf("veza objekt–dionica = %d, očekivano barem 4 (CS Draž, CS Zmajevac, CS Velika…)", n)
	}

	// "CS i ustava Zmajevac" je jedan vodomjer za dva objekta
	var stationName string
	if err := database.QueryRow(`SELECT st.name FROM structures s JOIN stations st ON st.id = s.station_id WHERE s.code = 'bp16-ustava-zmajevac'`).Scan(&stationName); err != nil {
		t.Errorf("ustava Zmajevac nema vodomjer: %v", err)
	} else if stationName != "CS i ustava Zmajevac" {
		t.Errorf("ustava Zmajevac vezana na %q", stationName)
	}

	// CS Draž stoji na dionici B.16.4 (spojni kanal CS Draž)
	if n := count(t, database, `SELECT COUNT(*) FROM section_structures ss JOIN structures s ON s.id = ss.structure_id WHERE s.code = 'bp16-cs-draz' AND ss.section_code = 'B.16.4'`); n != 1 {
		t.Errorf("CS Draž nije vezana na B.16.4")
	}

	// drugo punjenje ne umnožava
	if err := SeedInitialData(database); err != nil {
		t.Fatal(err)
	}
	if n := count(t, database, `SELECT COUNT(*) FROM structures`); n != 26 {
		t.Errorf("nakon drugog punjenja objekata = %d", n)
	}
}
