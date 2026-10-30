package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// odrzavanjeProbe diže bazu s područjima 34 i 16, vodom Dravom i nasipom
func odrzavanjeProbe(t *testing.T) (*sql.DB, *ledger.Recorder, *MaintenanceRepository) {
	t.Helper()
	baza, rec := bazaSPodrucjem(t)
	kad := "2026-03-01 08:00:00"
	for _, q := range []string{
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (16, 'B', 'Područje 16', 'VGI')`,
		`INSERT INTO watercourses (code, official_name, name, kind) VALUES ('rijeka-drava', 'rijeka Drava', 'Drava', 'rijeka')`,
		`INSERT INTO structures (id, code, name, kind, sector_id, area_id, created_at, updated_at)
			VALUES ('nasip-1', 'N1', 'Nasip Proba', 'NASIP', 'B', 34, '` + kad + `', '` + kad + `')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	return baza, rec, NewMaintenanceRepository(baza, rec)
}

func naziviLokacija(l []models.MaintainedWater) []string {
	var out []string
	for _, m := range l {
		out = append(out, m.Name)
	}
	return out
}

// Lokacije područja idu redom plana: A.02 prije A.03, I. red međudržavne
// prije ostalih, pa II. red; unutar toga po vrsti, rednom broju (brojčano)
// i nazivu
func TestLokacijeRedomPlana(t *testing.T) {
	_, _, repo := odrzavanjeProbe(t)
	ctx := context.Background()
	for _, m := range []models.MaintainedWater{
		{AreaID: 34, Program: models.ProgramA03, Name: "kanal III. reda", Order: models.WaterOrderThird, Kind: models.MaintenanceKindDrainage, Seq: "1."},
		{AreaID: 34, Name: "potok II. reda", Order: models.WaterOrderSecond, Kind: models.MaintenanceKindWatercourse, Seq: "1."},
		{AreaID: 34, Name: "jezero I. reda", Order: models.WaterOrderFirst, Group: models.WaterGroupOtherState, Kind: models.MaintenanceKindReservoir, Seq: "1."},
		{AreaID: 34, Name: "Drava 4.12.", Order: models.WaterOrderFirst, Group: models.WaterGroupInterstate, Kind: models.MaintenanceKindWatercourse, Seq: "4.12."},
		{AreaID: 34, Name: "Drava 4.2.", Order: models.WaterOrderFirst, Group: models.WaterGroupInterstate, Kind: models.MaintenanceKindWatercourse, Seq: "4.2."},
		{AreaID: 34, Name: "bez reda", Kind: "NEPOZNATA", Seq: "1."},
		{AreaID: 16, Name: "drugo područje", Order: models.WaterOrderFirst},
	} {
		m := m
		nuzno(t, repo.UpsertWater(ctx, &m))
	}
	l, err := repo.ListWaters(ctx, 34)
	if err != nil {
		t.Fatal(err)
	}
	htio := []string{"Drava 4.2.", "Drava 4.12.", "jezero I. reda", "potok II. reda", "bez reda", "kanal III. reda"}
	if got := naziviLokacija(l); !reflect.DeepEqual(got, htio) {
		t.Errorf("redoslijed %v\nhtio %v", got, htio)
	}
	if l, err := repo.ListWaters(ctx, 99); err != nil || len(l) != 0 {
		t.Errorf("područje bez lokacija: %v, %v", l, err)
	}
	if _, err := NewMaintenanceRepository(zatvorenaBaza(t), nil).ListWaters(ctx, 34); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Ključ plana: program, red, skupina i vrsta; nepoznat red i vrsta idu na kraj
func TestKljucPlanaLokacije(t *testing.T) {
	slucajevi := []struct {
		m    models.MaintainedWater
		htio string
	}{
		{models.MaintainedWater{Order: models.WaterOrderFirst, Group: models.WaterGroupInterstate, Kind: models.MaintenanceKindWatercourse}, "A.02111"},
		{models.MaintainedWater{Order: models.WaterOrderFirst, Group: models.WaterGroupOtherState, Kind: models.MaintenanceKindTorrent}, "A.02123"},
		{models.MaintainedWater{Program: models.ProgramA03, Order: models.WaterOrderFourth, Kind: models.MaintenanceKindDrainage}, "A.0344"},
		{models.MaintainedWater{Kind: "NEPOZNATA"}, "A.0299"},
	}
	for _, s := range slucajevi {
		if got := planRank(s.m); got != s.htio {
			t.Errorf("%+v: %q, htio %q", s.m, got, s.htio)
		}
	}
}

// Redni broj slaže se brojčano; što nije broj ostaje kako jest
func TestRedniBrojLokacije(t *testing.T) {
	for ulaz, htio := range map[string]string{
		"4.12.":  "0004.0012.",
		" 4.2. ": "0004.0002.",
		"7":      "0007.",
		"4.a.":   "4.a.",
		"":       "",
	} {
		if got := seqParts(ulaz); got != htio {
			t.Errorf("seqParts(%q) = %q, htio %q", ulaz, got, htio)
		}
	}
	if !(seqParts("4.2.") < seqParts("4.12.")) {
		t.Error("4.2. mora doći prije 4.12.")
	}
}

// Voda na granici vodi se u oba područja; objekt se nalazi po svom
// identitetu, a prazna šifra ne nalazi lokacije bez vode
func TestPopisiUKojimaSeVodaVodi(t *testing.T) {
	_, _, repo := odrzavanjeProbe(t)
	ctx := context.Background()
	for _, m := range []models.MaintainedWater{
		{AreaID: 34, WatercourseCode: "rijeka-drava", Name: "Drava (34)"},
		{AreaID: 16, WatercourseCode: "rijeka-drava", Name: "Drava (16)"},
		{AreaID: 34, StructureID: "nasip-1", Name: "nasip"},
		{AreaID: 34, Name: "lokacija bez veze"},
	} {
		m := m
		nuzno(t, repo.UpsertWater(ctx, &m))
	}
	l, err := repo.WatersFor(ctx, "rijeka-drava", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := naziviLokacija(l); !reflect.DeepEqual(got, []string{"Drava (16)", "Drava (34)"}) {
		t.Errorf("po vodi %v", got)
	}
	if l[0].WaterName != "rijeka Drava" {
		t.Errorf("naziv iz registra %q", l[0].WaterName)
	}
	l, _ = repo.WatersFor(ctx, "", "nasip-1")
	if len(l) != 1 || l[0].Name != "nasip" || l[0].WaterName != "Nasip Proba" || l[0].StructureKind != "NASIP" {
		t.Errorf("po objektu %+v", l)
	}
	if l, err := repo.WatersFor(ctx, "", ""); err != nil || len(l) != 0 {
		t.Errorf("bez šifre i objekta: %v, %v", naziviLokacija(l), err)
	}
	if _, err := NewMaintenanceRepository(zatvorenaBaza(t), nil).WatersFor(ctx, "rijeka-drava", ""); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Brisanje lokacije ostavlja spomenik u knjizi; nepostojeća javlja grešku
func TestBrisanjeLokacije(t *testing.T) {
	_, rec, repo := odrzavanjeProbe(t)
	ctx := context.Background()
	m := &models.MaintainedWater{AreaID: 34, WatercourseCode: "rijeka-drava", Name: "Drava"}
	nuzno(t, repo.UpsertWater(ctx, m))
	if m.ID != MaintainedWaterID(34, "Drava") {
		t.Fatalf("identitet %s", m.ID)
	}
	nuzno(t, repo.DeleteWater(ctx, m.ID))
	if g, err := repo.GetWater(ctx, m.ID); err != nil || g != nil {
		t.Fatalf("lokacija je ostala: %+v, %v", g, err)
	}
	v, err := rec.Latest(ctx, EntityMaintainedWaters, m.ID)
	if err != nil || v == nil || !v.Archived || !strings.Contains(string(v.Payload), `"name":"Drava"`) {
		t.Errorf("spomenik %+v, %v", v, err)
	}
	if err := repo.DeleteWater(ctx, m.ID); err == nil || !strings.Contains(err.Error(), "nije pronađena") {
		t.Errorf("drugo brisanje: %v", err)
	}
	if err := NewMaintenanceRepository(zatvorenaBaza(t), nil).DeleteWater(ctx, m.ID); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Stavke radova: bez all samo uključene, redom po rasporedu pa po broju
// kao broju (9 prije 10); nova stavka dobiva identitet, izmjena zadržava
// vrijeme upisa
func TestStavkeRadova(t *testing.T) {
	_, rec, repo := odrzavanjeProbe(t)
	ctx := context.Background()
	upisano := time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	stavke := []*models.WorkItem{
		{AreaID: 34, Number: "10", Description: "košnja", Unit: "ha", Active: true, CreatedAt: upisano},
		{AreaID: 34, Number: "9", Description: "čišćenje", Unit: "m3", Active: true},
		{AreaID: 34, Number: "1", Description: "isključena", Unit: "kom", Active: false},
		{AreaID: 34, Number: "50", Description: "prva po rasporedu", Unit: "sat", Active: true, SortOrder: -1},
		{AreaID: 16, Number: "1", Description: "drugo područje", Unit: "ha", Active: true},
	}
	for _, w := range stavke {
		nuzno(t, repo.SaveItem(ctx, w))
		if w.ID == "" {
			t.Fatal("stavka bez identiteta")
		}
	}
	opisi := func(l []models.WorkItem) []string {
		var out []string
		for _, w := range l {
			out = append(out, w.Description)
		}
		return out
	}
	l, err := repo.ListItems(ctx, 34, false)
	if err != nil || !reflect.DeepEqual(opisi(l), []string{"prva po rasporedu", "čišćenje", "košnja"}) {
		t.Errorf("uključene %v, %v", opisi(l), err)
	}
	l, _ = repo.ListItems(ctx, 34, true)
	if !reflect.DeepEqual(opisi(l), []string{"prva po rasporedu", "isključena", "čišćenje", "košnja"}) {
		t.Errorf("sve %v", opisi(l))
	}

	kosnja := stavke[0]
	kosnja.Description = "košnja nasipa"
	nuzno(t, repo.SaveItem(ctx, kosnja))
	g, err := repo.GetItem(ctx, kosnja.ID)
	if err != nil || g == nil || g.Description != "košnja nasipa" || !g.CreatedAt.Equal(upisano) || !g.Active {
		t.Errorf("izmijenjena stavka %+v, %v", g, err)
	}
	if g, err := repo.GetItem(ctx, "nema-je"); err != nil || g != nil {
		t.Errorf("nepostojeća stavka %+v, %v", g, err)
	}

	nuzno(t, repo.DeleteItem(ctx, kosnja.ID))
	if g, _ := repo.GetItem(ctx, kosnja.ID); g != nil {
		t.Error("stavka je ostala")
	}
	v, err := rec.Latest(ctx, EntityWorkItems, kosnja.ID)
	if err != nil || v == nil || !v.Archived {
		t.Fatalf("spomenik %+v, %v", v, err)
	}
	var zapis models.WorkItem
	if json.Unmarshal(v.Payload, &zapis) != nil || zapis.Description != "košnja nasipa" {
		t.Errorf("spomenik bez stanja: %s", v.Payload)
	}
	if err := repo.DeleteItem(ctx, kosnja.ID); err == nil || !strings.Contains(err.Error(), "nije pronađena") {
		t.Errorf("drugo brisanje: %v", err)
	}
	if _, err := NewMaintenanceRepository(zatvorenaBaza(t), nil).ListItems(ctx, 34, true); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Identitet uvezene stavke ovisi samo o području i oznaci
func TestIdentitetStavkeRadova(t *testing.T) {
	a := WorkItemID(34, "225|ha")
	if a != WorkItemID(34, "225|ha") {
		t.Error("isti ulaz, različit identitet")
	}
	if a == WorkItemID(16, "225|ha") || a == WorkItemID(34, "225|m2") {
		t.Error("različit ulaz, isti identitet")
	}
	if a == MaintainedWaterID(34, "225|ha") {
		t.Error("stavka i lokacija dijele identitet")
	}
}
