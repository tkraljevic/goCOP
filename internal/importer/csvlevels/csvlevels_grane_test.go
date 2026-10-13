package csvlevels

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Zaključava rubove uvoza tablice: kratak redak, zatečeno očitanje bez
// vodostaja, stupac koji pogađa dva objekta ili objekt s tuđim vodomjerom te
// greške registra i očitanja.

func TestUvozTabliceKratakRedakIZatecenoBezVodostaja(t *testing.T) {
	o := novaOkolinaTablice(t)
	ctx := context.Background()
	// isti dan na letvi već stoji upis bez vodostaja (npr. samo stanje
	// objekta); on nije vodostaj, pa nije ni razlika ni preklapanje
	bez := models.Reading{ID: uuid.New(), StationID: o.primjerovo.ID.String(), Source: models.ReadingSourceManual,
		Note: "letva pod ledom", MeasuredAt: time.Date(2026, 9, 1, 6, 0, 0, 0, models.Zagreb).UTC()}
	if err := o.deps.Readings.Create(ctx, &bez); err != nil {
		t.Fatal(err)
	}
	put := napisi(t, []string{
		"Datum;Primjerovo;Probno",
		"01.09.2026.;120;80",
		"02.09.2026.;121",
	})
	rep, err := Run(ctx, Options{Path: put, DryRun: true, Deps: o.deps})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Rows != 2 || rep.Inserted != 3 || rep.Skipped != 0 || rep.Conflicts != 0 {
		t.Errorf("redaka %d, novih %d, zapisanih %d, razlika %d", rep.Rows, rep.Inserted, rep.Skipped, rep.Conflicts)
	}
	for _, c := range rep.Matched {
		if c.Name == "Probno" && c.Values != 1 {
			t.Errorf("kratak redak dao je vrijednost za Probno: %+v", c)
		}
	}
}

func TestUvozTabliceStupacNaViseObjekata(t *testing.T) {
	o := novaOkolinaTablice(t)
	ctx := context.Background()
	// dva objekta istog imena, i objekt čiji vodomjer nije letva istog imena
	tudja := &models.Station{ID: uuid.New(), Code: "cs-tudja-letva", Name: "CS Tuđa"}
	if err := o.deps.Stations.CreateStation(ctx, tudja); err != nil {
		t.Fatal(err)
	}
	for sifra, ime := range map[string]string{"cs-dvojna-a": "CS Dvojna", "cs-dvojna-b": "CS Dvojna", "cs-tudja": "CS Tuđa"} {
		s := &models.Structure{Code: sifra, Name: ime, Kind: models.StructureKindPumpingStation, SectorID: "P", AreaID: 1, Origin: "RUČNI_UNOS"}
		if sifra == "cs-tudja" {
			s.StationID = o.primjerovo.ID.String()
		}
		if err := o.deps.Structures.CreateStructure(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := Run(ctx, Options{Path: napisi(t, []string{"Datum;CS Dvojna;CS Tuđa;Probno", "01.09.2026.;1;2;3"}), DryRun: true, Deps: o.deps})
	if err != nil {
		t.Fatal(err)
	}
	// neka odluči čovjek: oba stupca su dvoznačna, uvozi se samo Probno
	if strings.Join(rep.Ambiguous, ",") != "CS Dvojna,CS Tuđa" || len(rep.Matched) != 1 || rep.Matched[0].Name != "Probno" || rep.Inserted != 1 {
		t.Errorf("dvoznačni %v, prepoznati %+v, novih %d", rep.Ambiguous, rep.Matched, rep.Inserted)
	}
}

func TestUvozTabliceGreskaBaze(t *testing.T) {
	ctx := context.Background()
	for tablica, poruka := range map[string]string{"stations": "no such table: stations", "readings": "no such table: readings"} {
		o := novaOkolinaTablice(t)
		if _, err := o.baza.Exec(`PRAGMA foreign_keys = OFF; DROP TABLE ` + tablica); err != nil {
			t.Fatal(err)
		}
		_, err := Run(ctx, Options{Path: napisi(t, []string{"Datum;CS Probni", "01.09.2026.;120"}), DryRun: true, Deps: o.deps})
		if err == nil || !strings.Contains(err.Error(), poruka) {
			t.Errorf("bez tablice %s: %v", tablica, err)
		}
	}
}
