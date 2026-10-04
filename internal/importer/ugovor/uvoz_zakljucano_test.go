package ugovor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/importer/xlsx"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Zaključava čitanje ugovora A.02 (Parse) i uvoz (Run) na izmišljenom
// registru koji test sam složi, bez data/: greške u radnoj knjizi, područje,
// stavke radova, uparivanje lokacija i što ponovni uvoz radi s vezama.

var podrucjePrimjerica = models.Area{ID: 1, SectorID: "P", Name: "Mali sliv Primjerica", VgiName: "VGI Primjerica"}

func registarUgovora(t *testing.T) Deps {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "ugovor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	d := Deps{
		Waters:      repository.NewWatercourseRepository(baza, rec),
		Structures:  repository.NewStructureRepository(baza, rec),
		Maintenance: repository.NewMaintenanceRepository(baza, rec),
		Areas:       []models.Area{podrucjePrimjerica},
	}
	ctx := context.Background()
	for _, w := range []models.Watercourse{
		// isto ime, pojašnjenje u zagradi odlučuje po području
		{Code: "potok-primjerica-primjerica", OfficialName: "potok Primjerica (Primjerica)", Name: "Primjerica", Kind: "potok"},
		{Code: "potok-primjerica-drugdje", OfficialName: "potok Primjerica (Drugdje)", Name: "Primjerica", Kind: "potok"},
		// isto ime, odlučuje voda iz Odluke
		{Code: "kanal-probni-odluka", OfficialName: "kanal Probni", Name: "Probni", Kind: "kanal", Category: "DRUGE VEĆE VODE I KANALI"},
		{Code: "kanal-probni-dokumentacija", OfficialName: "kanal Probni", Name: "Probni", Kind: "kanal"},
		// isto ime bez ičega što odlučuje
		{Code: "potok-dvojnik-a", OfficialName: "potok Dvojnik", Name: "Dvojnik", Kind: "potok"},
		{Code: "potok-dvojnik-b", OfficialName: "potok Dvojnik", Name: "Dvojnik", Kind: "potok"},
		{Code: "kanal-glavni", OfficialName: "kanal Glavni", Name: "Glavni", Kind: "kanal"},
	} {
		w := w
		if err := d.Waters.CreateWatercourse(ctx, &w); err != nil {
			t.Fatal(err)
		}
	}
	nasip := models.Structure{Code: "nasip-probni", Name: "Nasip Probni", Kind: models.StructureKindEmbankment, SectorID: "P", AreaID: 1, Origin: "RUČNI_UNOS"}
	if err := d.Structures.CreateStructure(ctx, &nasip); err != nil {
		t.Fatal(err)
	}
	return d
}

func stavka(broj, opis, jed string) [3]string { return [3]string{broj, opis, jed} }

// ugovorPrimjerice je izmišljen ugovor za područje 1
func ugovorPrimjerice(t *testing.T) string {
	t.Helper()
	tros := [][]string{{"#E"}}
	tros = append(tros, blok("A.02.01.01.01.01.01.", "1.1.", "Potok Primjerica", "Vodotok",
		stavka("101", "Ručna košnja trave", "ha"), stavka("102", "Strojna košnja trave", "ha"))...)
	tros = append(tros, blok("A.02.01.01.01.02.02.", "2.2.", "Obalni pojas", "Nasip",
		stavka("101", "Ručna   košnja trave", "ha"))...)
	kanal := blok("A.02.01.01.02.04.01.", "4.1.", "Kanal Probni", "Kanal", stavka("103.0", "Čišćenje kanala od mulja", "m3"))
	prazna := make([]string, 13)
	prazna[0] = "#S" // redak bez opisa broji se u bloku, ali nije stavka
	kanal = append(kanal[:len(kanal)-1], prazna, []string{"#E"})
	tros = append(tros, kanal...)
	lok := [][]string{
		{"POPIS LOKACIJA IZVRŠENJA USLUGA"},
		{"VODE I. REDA - Međudržavne vode"},
		{"", "1. Vodotoci"},
		{"", "", "1.1.", "Potok   Primjerica"},
		{"", "", "1.2.", "Potok Dvojnik"},
		{"", "", "1.3.", "Kanal Glavni - Spojni za CS Probnu"},
		{"", "", "1.4", "bez točke u rednom broju"},
		{"VODE I. REDA - Ostale državne vode"},
		{"", "4. Osnovne melioracijske građevine za odvodnju"},
		{"", "", "4.1.", "Kanal Probni"},
		{"VODE II. REDA"},
		{"", "3. Bujični tokovi"},
		{"", "", "3.1.", "Bujica Nova"},
		{"", "2. Akumulacije, retencije i jezera"},
		{"", "", "2.1.", "Nasip Probni"},
		{"", "", "2.2.", "Obalni pojas"},
	}
	return napisiXlsx(t, map[string][][]string{
		"PPI_POSTAVKE":  {{"BP:", "1"}, {"Naziv:", "Branjeno područje br. 1 (izmišljeno)"}},
		"TROŠKOVNIK":    tros,
		"LOKACIJE_BP_1": lok,
		"PREVENTIVNA": {{"Redni br.", "Oznaka", "Opis", "Jed."}, {"1", "101", "Ručna košnja trave", "ha"},
			{"2", "104.0", "Iskolčenje osi", "m"}, {"x", "105", "redak bez rednog broja", "m"}, {"3", "", "bez oznake", "m"}},
	}, []string{"PPI_POSTAVKE", "TROŠKOVNIK", "LOKACIJE_BP_1", "PREVENTIVNA"})
}

func procitaj(t *testing.T, listovi map[string][][]string, redom []string) (*Contract, error) {
	t.Helper()
	wb, err := xlsx.Open(napisiXlsx(t, listovi, redom))
	if err != nil {
		t.Fatal(err)
	}
	return Parse(wb)
}

func TestCitanjeUgovoraGreske(t *testing.T) {
	tros := blok("A.02.01.01.01.", "1.1.", "Potok Primjerica", "Vodotok", stavka("101", "Košnja", "ha"))
	lok := [][]string{{"", "", "1.1.", "Potok Primjerica"}}
	for ime, s := range map[string]struct {
		listovi map[string][][]string
		greska  string
	}{
		"bez troškovnika":   {map[string][][]string{"LOKACIJE_BP_1": lok}, "nema list TROŠKOVNIK"},
		"troškovnik bez #P": {map[string][][]string{"TROŠKOVNIK": {{"#S", "", "", "", "", "", "", "Košnja"}}, "LOKACIJE_BP_1": lok}, "nijedan blok #P"},
		"bez lokacija":      {map[string][][]string{"TROŠKOVNIK": tros}, "LOKACIJE_BP_NN"},
		"prazne lokacije":   {map[string][][]string{"TROŠKOVNIK": tros, "LOKACIJE_BP_1": {{"VODE II. REDA"}}}, "prazan"},
		"bez područja": {map[string][][]string{"TROŠKOVNIK": blok("B.01.01.", "1.1.", "Potok Primjerica", "Vodotok"),
			"LOKACIJE_BP_1": lok}, "koje je branjeno područje"},
		"pozicija drugog područja": {map[string][][]string{"PPI_POSTAVKE": {{"BP:", "1"}},
			"TROŠKOVNIK": blok("A.02.01.02.01.", "1.1.", "Potok Primjerica", "Vodotok"), "LOKACIJE_BP_1": lok}, "nije iz područja 1"},
	} {
		var redom []string
		for _, l := range []string{"PPI_POSTAVKE", "TROŠKOVNIK", "LOKACIJE_BP_1"} {
			if _, ima := s.listovi[l]; ima {
				redom = append(redom, l)
			}
		}
		c, err := procitaj(t, s.listovi, redom)
		if err == nil || c != nil || !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: %v", ime, err)
		}
	}

	// Bez postavki područje se čita iz prve pozicije plana; nečitljiv broj u
	// postavkama ne smeta.
	for ime, postavke := range map[string][][]string{"bez postavki": nil, "nečitljive postavke": {{"BP:", "jedan"}}} {
		listovi := map[string][][]string{"TROŠKOVNIK": tros, "LOKACIJE_BP_1": lok}
		redom := []string{"TROŠKOVNIK", "LOKACIJE_BP_1"}
		if postavke != nil {
			listovi["PPI_POSTAVKE"] = postavke
			redom = append([]string{"PPI_POSTAVKE"}, redom...)
		}
		c, err := procitaj(t, listovi, redom)
		if err != nil || c.AreaID != 1 {
			t.Errorf("%s: %+v, %v", ime, c, err)
		}
	}
}

func TestCitanjeUgovora(t *testing.T) {
	wb, err := xlsx.Open(ugovorPrimjerice(t))
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(wb)
	if err != nil {
		t.Fatal(err)
	}
	if c.AreaID != 1 || c.AreaName != "Branjeno područje br. 1 (izmišljeno)" || len(c.Blocks) != 3 {
		t.Fatalf("područje %d %q, blokova %d", c.AreaID, c.AreaName, len(c.Blocks))
	}
	// stavke: razmaci u opisu se sažmu, „.0” s oznake otpada, iste se broje
	if len(c.Items) != 3 || c.Items[0].Description != "Ručna košnja trave" || c.Items[0].Uses != 2 || c.Items[2].Number != "103" {
		t.Errorf("stavke: %+v", c.Items)
	}
	if b := c.Blocks[2]; b.Lines != 2 || b.Water != "Kanal Probni" || b.Object != "Kanal" || b.Value != 1000 {
		t.Errorf("blok kanala: %+v", b)
	}
	// ponudbeni troškovnik: samo redci s rednim brojem i oznakom
	if len(c.Catalogue) != 2 || c.Catalogue[1].Number != "104" {
		t.Errorf("ponudbeni troškovnik: %+v", c.Catalogue)
	}
	// lokacije: redni broj bez završne točke se ne čita
	if len(c.Locations) != 7 {
		t.Fatalf("lokacija %d: %+v", len(c.Locations), c.Locations)
	}
	po := map[string]Location{}
	for _, l := range c.Locations {
		po[l.Name] = l
	}
	if l := po["Potok Primjerica"]; l.Seq != "1.1." || l.Order != models.WaterOrderFirst || l.Group != models.WaterGroupInterstate || l.Kind != models.MaintenanceKindWatercourse {
		t.Errorf("Primjerica: %+v", l)
	}
	if l := po["Kanal Probni"]; l.Group != models.WaterGroupOtherState || l.Kind != models.MaintenanceKindDrainage {
		t.Errorf("Probni: %+v", l)
	}
	if l := po["Bujica Nova"]; l.Order != models.WaterOrderSecond || l.Kind != models.MaintenanceKindTorrent {
		t.Errorf("Nova: %+v", l)
	}
	// nasip po nazivu i po objektu u troškovniku; voda nije nasip
	if !c.IsEmbankment(po["Nasip Probni"]) || !c.IsEmbankment(po["Obalni pojas"]) || c.IsEmbankment(po["Kanal Probni"]) {
		t.Error("prepoznavanje nasipa")
	}
}

func TestUvozUgovoraUparivanje(t *testing.T) {
	d := registarUgovora(t)
	ctx := context.Background()
	put := ugovorPrimjerice(t)

	rep, err := Run(ctx, Options{Path: put, DryRun: true, Deps: d})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Existing != 3 || rep.Ambiguous != 1 || rep.Suggested != 1 || rep.Created != 2 || rep.ItemsTotal != 3 || rep.ItemsNew != 3 {
		t.Errorf("probni prolaz: %s", rep.Summary())
	}
	po := map[string]Match{}
	for _, m := range rep.Locations {
		po[m.Location.Name] = m
	}
	for ime, sifra := range map[string]string{
		"Potok Primjerica": "potok-primjerica-primjerica", // pojašnjenje odgovara području
		"Kanal Probni":     "kanal-probni-odluka",         // voda iz Odluke
	} {
		if m := po[ime]; m.Status != StatusExisting || m.Code != sifra {
			t.Errorf("%s: %+v", ime, m)
		}
	}
	if m := po["Potok Dvojnik"]; m.Status != StatusAmbiguous || len(m.Options) != 2 || !strings.HasPrefix(m.Options[0], "potok-dvojnik-") {
		t.Errorf("dvoznačno: %+v", m)
	}
	if m := po["Kanal Glavni - Spojni za CS Probnu"]; m.Status != StatusSuggested || strings.Join(m.Options, ",") != "kanal-glavni (kanal Glavni)" {
		t.Errorf("prijedlog: %+v", m)
	}
	if m := po["Nasip Probni"]; m.Status != StatusExisting || !m.Structure {
		t.Errorf("postojeći nasip: %+v", m)
	}
	if m := po["Obalni pojas"]; m.Status != StatusNew || !m.Structure {
		t.Errorf("novi nasip: %+v", m)
	}
	if ws, _ := d.Maintenance.ListWaters(ctx, 1); len(ws) != 0 {
		t.Fatalf("probni prolaz upisao je %d lokacija", len(ws))
	}

	// pravi uvoz: nove vode i nasipi nastaju, prijedlog i dvoznačno ostaju bez veze
	if _, err := Run(ctx, Options{Path: put, Deps: d}); err != nil {
		t.Fatal(err)
	}
	ws, err := d.Maintenance.ListWaters(ctx, 1)
	if err != nil || len(ws) != 7 {
		t.Fatalf("lokacija %d (%v)", len(ws), err)
	}
	upisano := map[string]models.MaintainedWater{}
	for _, w := range ws {
		upisano[w.Name] = w
	}
	if w := upisano["Potok Dvojnik"]; w.WatercourseCode != "" || w.StructureID != "" {
		t.Errorf("dvoznačna lokacija dobila je vezu: %+v", w)
	}
	if w := upisano["Kanal Glavni - Spojni za CS Probnu"]; w.WatercourseCode != "" {
		t.Errorf("prijedlog je vezan: %+v", w)
	}
	nova, _ := d.Waters.GetWatercourse(ctx, upisano["Bujica Nova"].WatercourseCode)
	if nova == nil || nova.Kind != "bujica" || nova.Name != "Nova" || nova.Origin != models.WatercourseOriginContract {
		t.Errorf("nova voda: %+v", nova)
	}
	if w := upisano["Obalni pojas"]; w.StructureID == "" || w.StructureKind != models.StructureKindEmbankment {
		t.Errorf("novi nasip: %+v", w)
	}
	stavke, _ := d.Maintenance.ListItems(ctx, 1, false)
	if len(stavke) != 3 {
		t.Errorf("stavki %d", len(stavke))
	}

	// Ručna veza dvoznačne lokacije ne preživi ponovni uvoz istog ugovora:
	// lokacija se upiše iznova, bez veze.
	dvojnik := upisano["Potok Dvojnik"]
	dvojnik.WatercourseCode = "potok-dvojnik-a"
	if err := d.Maintenance.UpsertWater(ctx, &dvojnik); err != nil {
		t.Fatal(err)
	}
	rep, err = Run(ctx, Options{Path: put, Deps: d, AllItems: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Existing != 5 || rep.Created != 0 || rep.ItemsTotal != 4 || rep.ItemsExisting != 3 || rep.ItemsNew != 1 {
		t.Errorf("ponovni uvoz: %s", rep.Summary())
	}
	ws, _ = d.Maintenance.ListWaters(ctx, 1)
	for _, w := range ws {
		if w.Name == "Potok Dvojnik" && w.WatercourseCode != "" {
			t.Errorf("ručna veza je danas izgubljena, a ostala je %q", w.WatercourseCode)
		}
	}
	if len(ws) != 7 {
		t.Errorf("ponovni uvoz udvostručio je lokacije: %d", len(ws))
	}

	// zadana veza ima prednost, a veza na nepostojeću šifru prekida uvoz
	rep, err = Run(ctx, Options{Path: put, DryRun: true, Deps: d, Aliases: map[string]string{"Potok Dvojnik": "potok-dvojnik-b"}})
	if err != nil || rep.Ambiguous != 0 {
		t.Errorf("s vezom: %s, %v", rep.Summary(), err)
	}
	if _, err := Run(ctx, Options{Path: put, DryRun: true, Deps: d, Aliases: map[string]string{"Potok Dvojnik": "nema-je"}}); err == nil || !strings.Contains(err.Error(), "nema takve vode") {
		t.Errorf("veza na nepostojeću šifru: %v", err)
	}
}

func TestUvozUgovoraNepoznatoPodrucje(t *testing.T) {
	d := registarUgovora(t)
	d.Areas = []models.Area{{ID: 2, SectorID: "P", Name: "Drugo područje"}}
	rep, err := Run(context.Background(), Options{Path: ugovorPrimjerice(t), Deps: d})
	if err == nil || !strings.Contains(err.Error(), "ne postoji u registru") || rep.Area != 1 {
		t.Fatalf("%+v, %v", rep, err)
	}
	if ws, _ := d.Maintenance.ListWaters(context.Background(), 1); len(ws) != 0 {
		t.Errorf("upisano %d lokacija", len(ws))
	}
	if _, err := Run(context.Background(), Options{Path: filepath.Join(t.TempDir(), "nema.xlsx"), Deps: d}); err == nil {
		t.Error("nepostojeća datoteka mora javiti grešku")
	}
}

func TestOdabirKandidataBezNazivaPodrucja(t *testing.T) {
	opcije := []candidate{{code: "a", qualifier: "Primjerica"}, {code: "b"}}
	ix := &index{area: podrucjePrimjerica}
	if c, ok := ix.pick(opcije); !ok || c.code != "a" {
		t.Errorf("s nazivom područja: %+v %v", c, ok)
	}
	// Područje bez naziva i bez naziva ispostave sruši odabir čim neki
	// kandidat ima pojašnjenje.
	defer func() {
		if recover() == nil {
			t.Error("odabir bez naziva područja danas pada; ako više ne pada, ažuriraj test")
		}
	}()
	(&index{area: models.Area{ID: 1}}).pick(opcije)
}
