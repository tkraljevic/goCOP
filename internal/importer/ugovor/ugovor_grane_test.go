package ugovor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/hydro"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Zaključava rubove uvoza ugovora: prazan redak u troškovniku, dva nasipa
// istog imena, nova voda čija je šifra već zauzeta, vrstu nove vode iz
// naziva ili razvrstavanja te greške radne knjige i baze.

// ugovorSRubovima je izmišljen ugovor za područje 1 s novim kanalom i
// nasipom kojeg registar ima dvaput
func ugovorSRubovima(t *testing.T) string {
	t.Helper()
	tros := [][]string{{"#E"}, {}}
	tros = append(tros, blok("A.02.01.01.01.04.01.", "4.1.", "Kanal Novi", "Kanal", stavka("201", "Košnja pokosa", "ha"))...)
	tros = append(tros, []string{})
	return napisiXlsx(t, map[string][][]string{
		"PPI_POSTAVKE": {{"BP:", "1"}, {"Naziv:", "Branjeno područje br. 1 (izmišljeno)"}},
		"TROŠKOVNIK":   tros,
		"LOKACIJE_BP_1": {
			{"POPIS LOKACIJA IZVRŠENJA USLUGA"},
			{"VODE I. REDA - Ostale državne vode"},
			{"", "4. Osnovne melioracijske građevine za odvodnju"},
			{"", "", "4.1.", "Kanal Novi"},
			{"VODE II. REDA"},
			{"", "2. Akumulacije, retencije i jezera"},
			{"", "", "2.1.", "Nasip Dvojni"},
		},
	}, []string{"PPI_POSTAVKE", "TROŠKOVNIK", "LOKACIJE_BP_1"})
}

func TestUvozUgovoraRubovi(t *testing.T) {
	d := registarUgovora(t)
	ctx := context.Background()
	// šifra novog kanala već pripada drugoj vodi
	zauzeta := models.Watercourse{Code: hydro.WatercourseCode("Kanal Novi"), OfficialName: "kanal Treći", Name: "Treći", Kind: "kanal"}
	if err := d.Waters.CreateWatercourse(ctx, &zauzeta); err != nil {
		t.Fatal(err)
	}
	for _, sifra := range []string{"nasip-dvojni-a", "nasip-dvojni-b"} {
		o := models.Structure{Code: sifra, Name: "Nasip Dvojni", Kind: models.StructureKindEmbankment, SectorID: "P", AreaID: 1}
		if err := d.Structures.CreateStructure(ctx, &o); err != nil {
			t.Fatal(err)
		}
	}
	put := ugovorSRubovima(t)

	rep, err := Run(ctx, Options{Path: put, DryRun: true, Deps: d})
	if err != nil {
		t.Fatal(err)
	}
	// prazni redci troškovnika ne prekidaju blok
	if rep.Blocks != 1 || rep.ItemsTotal != 1 || rep.Created != 1 || rep.Ambiguous != 1 || len(rep.Locations) != 2 {
		t.Fatalf("probni prolaz: %s", rep.Summary())
	}
	nasip := rep.Locations[1]
	if nasip.Location.Name != "Nasip Dvojni" || !nasip.Structure || nasip.Status != StatusAmbiguous || strings.Join(nasip.Options, ",") != "Nasip Dvojni,Nasip Dvojni" {
		t.Errorf("dvoznačan nasip: %+v", nasip)
	}

	if _, err := Run(ctx, Options{Path: put, Deps: d}); err != nil {
		t.Fatal(err)
	}
	sifra := hydro.WatercourseCode("Kanal Novi") + "-bp1"
	nova, _ := d.Waters.GetWatercourse(ctx, sifra)
	if nova == nil || nova.OfficialName != "Kanal Novi" || nova.Name != "Novi" || nova.Kind != "kanal" {
		t.Fatalf("nova voda %s: %+v", sifra, nova)
	}
	if stara, _ := d.Waters.GetWatercourse(ctx, zauzeta.Code); stara == nil || stara.OfficialName != "kanal Treći" {
		t.Errorf("zatečena voda: %+v", stara)
	}
	upisano := map[string]models.MaintainedWater{}
	ws, _ := d.Maintenance.ListWaters(ctx, 1)
	for _, w := range ws {
		upisano[w.Name] = w
	}
	if upisano["Kanal Novi"].WatercourseCode != sifra || upisano["Nasip Dvojni"].StructureID != "" || len(ws) != 2 {
		t.Errorf("lokacije: %+v", ws)
	}
}

// Vrsta nove vode čita se s početka naziva, a kad je ondje nema, iz naziva
// ili razvrstavanja lokacije.
func TestNovaVodaIzNaziva(t *testing.T) {
	for _, s := range []struct {
		l          Location
		ime, vrsta string
	}{
		{Location{Name: "Kanal Remetin"}, "Remetin", "kanal"},
		{Location{Name: "Bujica Mala"}, "Mala", "bujica"},
		{Location{Name: "Lateralni kanal Draž"}, "Lateralni kanal Draž", "kanal"},
		{Location{Name: "Mala Dolina", Kind: models.MaintenanceKindTorrent}, "Mala Dolina", "bujica"},
		{Location{Name: "Odvodnja Sjever", Kind: models.MaintenanceKindDrainage}, "Odvodnja Sjever", "kanal"},
		{Location{Name: "Jezero", Kind: models.MaintenanceKindReservoir}, "Jezero", ""},
	} {
		w := newWatercourse(s.l)
		if w.Name != s.ime || w.Kind != s.vrsta || w.OfficialName != s.l.Name || w.Code != hydro.WatercourseCode(s.l.Name) || w.Origin != models.WatercourseOriginContract {
			t.Errorf("%q → %+v", s.l.Name, w)
		}
	}
}

func TestUvozUgovoraGreskeBaze(t *testing.T) {
	ctx := context.Background()
	// radna knjiga bez troškovnika nije ugovor
	bez := napisiXlsx(t, map[string][][]string{"PPI_POSTAVKE": {{"BP:", "1"}}}, []string{"PPI_POSTAVKE"})
	if _, err := Run(ctx, Options{Path: bez, Deps: registarUgovora(t)}); err == nil || !strings.HasPrefix(err.Error(), "ugovor.xlsx: radna knjiga nema list TROŠKOVNIK") {
		t.Errorf("bez troškovnika: %v", err)
	}

	// zatvorena baza: registar voda se ne da pročitati
	zatvorena, err := db.OpenDB(filepath.Join(t.TempDir(), "zatvorena.db"))
	if err != nil {
		t.Fatal(err)
	}
	zatvorena.Close()
	rec := ledger.New(zatvorena, "test")
	d := Deps{
		Waters: repository.NewWatercourseRepository(zatvorena, rec), Structures: repository.NewStructureRepository(zatvorena, rec),
		Maintenance: repository.NewMaintenanceRepository(zatvorena, rec), Areas: []models.Area{podrucjePrimjerica},
	}
	if _, err := Run(ctx, Options{Path: ugovorPrimjerice(t), DryRun: true, Deps: d}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("zatvorena baza: %v", err)
	}

	// bez tablice stavki radova ni probni prolaz ne zna što je novo
	baza, d := registarUgovoraSBazom(t)
	if _, err := baza.Exec(`DROP TABLE work_items`); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, Options{Path: ugovorPrimjerice(t), DryRun: true, Deps: d}); err == nil || !strings.Contains(err.Error(), "no such table: work_items") {
		t.Errorf("bez stavki radova: %v", err)
	}

	// baza odbija upis: okidač glumi pun disk
	for ime, okidac := range map[string]string{
		"novi nasip":    `BEFORE INSERT ON structures`,
		"nova voda":     `BEFORE INSERT ON watercourses`,
		"lokacija":      `BEFORE INSERT ON maintained_waters`,
		"stavka radova": `BEFORE INSERT ON work_items`,
	} {
		baza, d := registarUgovoraSBazom(t)
		if _, err := baza.Exec(`CREATE TRIGGER pun_disk ` + okidac + ` BEGIN SELECT RAISE(ABORT, 'disk pun'); END`); err != nil {
			t.Fatal(err)
		}
		if _, err := Run(ctx, Options{Path: ugovorPrimjerice(t), Deps: d}); err == nil || !strings.Contains(err.Error(), "disk pun") {
			t.Errorf("%s: %v", ime, err)
		}
	}
}
