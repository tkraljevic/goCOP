package ugovor

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// napisiXlsx gradi radnu knjigu s upisanim tekstom u ćelijama
func napisiXlsx(t *testing.T, sheets map[string][][]string, order []string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ugovor.xlsx")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	put := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	var wbSheets, rels strings.Builder
	for i, name := range order {
		id := i + 1
		fmt.Fprintf(&wbSheets, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, name, id, id)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="x" Target="worksheets/sheet%d.xml"/>`, id, id)
		var rows strings.Builder
		for r, row := range sheets[name] {
			fmt.Fprintf(&rows, `<row r="%d">`, r+1)
			for c, v := range row {
				if v == "" {
					continue
				}
				col := string(rune('A' + c))
				esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(v)
				fmt.Fprintf(&rows, `<c r="%s%d" t="inlineStr"><is><t>%s</t></is></c>`, col, r+1, esc)
			}
			rows.WriteString(`</row>`)
		}
		put(fmt.Sprintf("xl/worksheets/sheet%d.xml", id),
			`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`+rows.String()+`</sheetData></worksheet>`)
	}
	put("xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`+wbSheets.String()+`</sheets></workbook>`)
	put("xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+rels.String()+`</Relationships>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// blok gradi jednu ugovornu stavku troškovnika u obliku dodatka
func blok(pos, code, voda, objekt string, stavke ...[3]string) [][]string {
	col := func(i int, v string) []string {
		r := make([]string, 13)
		r[0] = "#" + string("PVOLZUNXHSGE"[i])
		r[8] = v
		return r
	}
	p := col(0, pos)
	p[10] = code
	rows := [][]string{p, col(1, voda), col(2, objekt), col(3, "0+000-5+000"), col(4, "XIV OSJEČKO-BARANJSKA"),
		col(5, "4.6. Uklanjanje vegetacije"), col(6, "1000"), {"#X"}, {"#H"}}
	for i, s := range stavke {
		r := make([]string, 13)
		r[0], r[6], r[7], r[8], r[9], r[10], r[11], r[12] = "#S", fmt.Sprint(i+1), s[1], s[0], s[2], "1", "1", "1"
		rows = append(rows, r)
	}
	return append(rows, []string{"#E"})
}

func uzorakUgovora(t *testing.T) string {
	t.Helper()
	tros := [][]string{{"#E"}}
	tros = append(tros, blok("A.02.01.16.01.01.01.", "1.1.", "Potok Karašica", "Vodotok",
		[3]string{"225", "Strojna košnja trave na ravnim površinama. Stavka obuhvaća sve.", "ha"},
		[3]string{"226", "Strojna košnja trave na kosim površinama", "ha"})...)
	tros = append(tros, blok("A.02.01.16.01.01.02.", "2.1.", "Nasip Puškaš", "Nasip",
		[3]string{"225", "Strojna košnja trave na ravnim površinama. Stavka obuhvaća sve.", "ha"})...)
	tros = append(tros, blok("A.02.01.16.02.04.", "3.1.", "Kanal K-8", "Kanal",
		[3]string{"229", "Strojno odstranjivanje šaša i trske", "m2"})...)
	lok := [][]string{
		{"POPIS LOKACIJA IZVRŠENJA USLUGA"}, {},
		{"RED VODE", "VRSTA VODE", "", "NAZIV VODE"},
		{"VODE I. REDA - Međudržavne vode"},
		{"", "1. Vodotoci"},
		{"", "", "1.1.", "Potok Karašica"},
		{"", "2. Akumulacije, retencije i jezera"},
		{"", "", "2.1.", "Nasip Puškaš"},
		{"VODE I. REDA - Ostale državne vode"},
		{"", "4. Osnovne meloiracijske građevine za odvodnju"},
		{"", "", "4.1.", "Kanal Bojana - GDK za CS Podunavlje"},
		{"VODE II. REDA"},
		{"", "3. Bujični tokovi"},
		{"", "", "3.1.", "Bujica Podolje"},
		{"", "4. Osnovne meloiracijske građevine za odvodnju"},
		{"", "", "4.1.", "Kanal K-8"},
	}
	return napisiXlsx(t, map[string][][]string{
		"PPI_POSTAVKE":   {{"BP:", "16"}, {"Naziv:", "Branjeno područje br. 16"}},
		"TROŠKOVNIK":     tros,
		"LOKACIJE_BP_16": lok,
		"PREVENTIVNA": {{"Redni br.", "Oznaka", "Opis", "Jed."}, {"1", "1", "Iskolčenje osi", "m"},
			{"2", "225", "Strojna košnja trave na ravnim površinama. Stavka obuhvaća sve.", "ha"}},
	}, []string{"PPI_POSTAVKE", "TROŠKOVNIK", "LOKACIJE_BP_16", "PREVENTIVNA"})
}

func pripremi(t *testing.T) Deps {
	t.Helper()
	if !db.UseRepoData() {
		t.Skip("data/ s registrima nije dostupan — registri stoje izvan repozitorija")
	}
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "ugovor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedInitialData(database); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(database, "test-node")
	users := repository.NewUserRepository(database, rec)
	areas, err := users.ListAreas("")
	if err != nil {
		t.Fatal(err)
	}
	return Deps{
		Waters:      repository.NewWatercourseRepository(database, rec),
		Structures:  repository.NewStructureRepository(database, rec),
		Maintenance: repository.NewMaintenanceRepository(database, rec),
		Areas:       areas,
	}
}

func TestUvozUgovora(t *testing.T) {
	deps := pripremi(t)
	ctx := context.Background()
	path := uzorakUgovora(t)

	rep, err := Run(ctx, Options{Path: path, DryRun: true, Deps: deps})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Area != 16 || len(rep.Locations) != 5 || rep.Blocks != 3 {
		t.Fatalf("područje %d, lokacija %d, blokova %d", rep.Area, len(rep.Locations), rep.Blocks)
	}
	status := map[string]Match{}
	for _, m := range rep.Locations {
		status[m.Location.Name] = m
	}
	if m := status["Potok Karašica"]; m.Status != StatusExisting || m.Code != "potok-karasica-baranja" {
		t.Errorf("Karašica: %+v", m)
	}
	if m := status["Potok Karašica"]; m.Location.Order != models.WaterOrderFirst || m.Location.Group != models.WaterGroupInterstate || m.Location.Kind != models.MaintenanceKindWatercourse {
		t.Errorf("razvrstavanje Karašice: %+v", m.Location)
	}
	if m := status["Nasip Puškaš"]; !m.Structure || m.Status != StatusNew {
		t.Errorf("nasip: %+v", m)
	}
	if m := status["Kanal Bojana - GDK za CS Podunavlje"]; m.Status != StatusSuggested || len(m.Options) == 0 || !strings.Contains(m.Options[0], "kanal-bojana") {
		t.Errorf("Bojana bi trebala biti prijedlog: %+v", m)
	}
	if m := status["Kanal K-8"]; m.Status != StatusNew || m.Location.Order != models.WaterOrderSecond || m.Location.Kind != models.MaintenanceKindDrainage {
		t.Errorf("K-8: %+v", m)
	}
	if m := status["Bujica Podolje"]; m.Location.Kind != models.MaintenanceKindTorrent {
		t.Errorf("Podolje: %+v", m)
	}
	if rep.ItemsTotal != 3 || rep.ItemsNew != 3 {
		t.Errorf("stavke: %+v", rep)
	}

	// probni prolaz ne piše
	if ws, _ := deps.Maintenance.ListWaters(ctx, 16); len(ws) != 0 {
		t.Fatalf("probni prolaz je upisao %d lokacija", len(ws))
	}

	// pravi uvoz, s ručnom vezom za Bojanu
	rep, err = Run(ctx, Options{Path: path, Deps: deps, Aliases: map[string]string{"Kanal Bojana - GDK za CS Podunavlje": "kanal-bojana"}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Suggested != 0 || rep.Existing != 2 {
		t.Errorf("s vezom: %+v", rep)
	}
	ws, err := deps.Maintenance.ListWaters(ctx, 16)
	if err != nil || len(ws) != 5 {
		t.Fatalf("lokacija u bazi %d (%v)", len(ws), err)
	}
	for _, w := range ws {
		switch w.Name {
		case "Nasip Puškaš":
			if w.StructureID == "" || w.StructureKind != models.StructureKindEmbankment {
				t.Errorf("nasip nije objekt: %+v", w)
			}
		case "Kanal K-8":
			if w.WatercourseCode == "" {
				t.Errorf("K-8 bez vode: %+v", w)
			}
			nw, _ := deps.Waters.GetWatercourse(ctx, w.WatercourseCode)
			if nw == nil || nw.Origin != models.WatercourseOriginContract || nw.Kind != "kanal" || nw.Name != "K-8" {
				t.Errorf("nova voda: %+v", nw)
			}
			if got := w.PlanPosition(); got != "A.02.01.16.02.04." {
				t.Errorf("pozicija plana %q", got)
			}
		case "Kanal Bojana - GDK za CS Podunavlje":
			if w.WatercourseCode != "kanal-bojana" {
				t.Errorf("Bojana: %+v", w)
			}
		case "Potok Karašica":
			if got := w.PlanPosition(); got != "A.02.01.16.01.01.01." {
				t.Errorf("pozicija plana %q", got)
			}
		}
	}
	items, _ := deps.Maintenance.ListItems(ctx, 16, false)
	if len(items) != 3 || items[0].Number != "225" || items[0].Unit != "ha" || items[0].Origin != models.WorkItemOriginContract {
		t.Errorf("stavke: %+v", items)
	}

	// ponovni prolaz: ništa novo, ništa dvostruko
	rep, err = Run(ctx, Options{Path: path, Deps: deps, Aliases: map[string]string{"Kanal Bojana - GDK za CS Podunavlje": "kanal-bojana"}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.ItemsExisting != 3 || rep.Created != 0 || rep.Existing != 5 {
		t.Errorf("ponovni prolaz: %+v", rep)
	}
	if ws, _ := deps.Maintenance.ListWaters(ctx, 16); len(ws) != 5 {
		t.Errorf("dvostruke lokacije: %d", len(ws))
	}

	// cijeli troškovnik na zahtjev
	rep, err = Run(ctx, Options{Path: path, Deps: deps, AllItems: true, Aliases: map[string]string{"Kanal Bojana - GDK za CS Podunavlje": "kanal-bojana"}})
	if err != nil || rep.ItemsTotal != 4 || rep.ItemsNew != 1 {
		t.Errorf("sve stavke: %+v %v", rep, err)
	}
}

func TestKljucevi(t *testing.T) {
	if normKey("G.D.K. za CS Puškaš") != normKey("Glavni dovodni kanal za CS Puškaš") {
		t.Error("kratica G.D.K. se ne raspisuje")
	}
	if bareKey("Potok Karašica") != "karasica" || normKey("potok Karašica (Baranja)") != "potok karasica" {
		t.Errorf("ključevi: %q %q", bareKey("Potok Karašica"), normKey("potok Karašica (Baranja)"))
	}
	if coreKey("Odvodni kanal Karašica") != "karasica" {
		t.Errorf("jezgra: %q", coreKey("Odvodni kanal Karašica"))
	}
}
