package bp16

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Zaključava uvoz evidencije radova A.02 i A.03 u rekonstruirane
// građevinske dnevnike: dnevnik po programu i godini, listovi po danu i po
// šest izvođačevih upisa, napomena o rekonstrukciji na svakom listu, nadzor
// po adresi Hrvatskih voda, prilike najbliže sedam sati i lokacije koje se
// nalaze ili stvaraju.

// bazaDnevnika je baza s područjem 16 i onim što registar već ima: dvije
// lokacije održavanja, nasip Draž i vodu sa šifrom kanal-probni
func bazaDnevnika(t *testing.T) (*sql.DB, JournalDeps) {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "dnevnici.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO', 'COP')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (16, 'B', 'Područje 16', 'VGI')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	deps := depsDnevnika(baza)
	ctx := context.Background()
	for _, ime := range []string{"Retencija Stara Drava - Bilje", "Kanal Postojeći"} {
		if err := deps.Maintenance.UpsertWater(ctx, &models.MaintainedWater{AreaID: 16, Program: models.ProgramA02, Name: ime, Source: "ugovor"}); err != nil {
			t.Fatal(err)
		}
	}
	nasip := models.Structure{Code: "nasip-draz", Name: "Nasip Draž", Kind: models.StructureKindEmbankment, SectorID: "B", AreaID: 16}
	if err := deps.Structures.CreateStructure(ctx, &nasip); err != nil {
		t.Fatal(err)
	}
	if err := deps.Waters.CreateWatercourse(ctx, &models.Watercourse{Code: "kanal-probni", OfficialName: "Kanal Probni", Name: "Probni"}); err != nil {
		t.Fatal(err)
	}
	return baza, deps
}

// depsDnevnika su spremišta uvoza dnevnika nad zadanom bazom, za područje 16
func depsDnevnika(baza *sql.DB) JournalDeps {
	rec := ledger.New(baza, "cvor-b")
	return JournalDeps{
		Journals:    repository.NewJournalRepository(baza, rec),
		Maintenance: repository.NewMaintenanceRepository(baza, rec),
		Waters:      repository.NewWatercourseRepository(baza, rec),
		Structures:  repository.NewStructureRepository(baza, rec),
		Areas:       []models.Area{{ID: 3, SectorID: "A", Name: "Drugo područje"}, {ID: 16, SectorID: "B", Name: "Područje 16"}},
		AreaID:      16,
	}
}

// izvorRadova je evidencija radova s dvije godine A.02 i jednim upisom A.03
func izvorRadova() probniIzvor {
	return probniIzvor{zbirke: map[string][]json.RawMessage{
		"users": raw(
			`{"id":"u-nadzor","first_name":"Iva","last_name":"Ivić","email":"iva.ivic@VODA.hr"}`,
			`{"id":"u-izv","first_name":"Pero","last_name":"Perić","email":"pero@izvodjac.example"}`,
			`"nije korisnik"`,
		),
		"vrijeme_za_list": raw(
			`{"datum":"2024-03-04","vrijeme":"05:00:00","vrijeme_opisno":"oblačno, temperatura zraka: 4 °C"}`,
			`{"datum":"2024-03-04","vrijeme":"07:30:00","vrijeme_opisno":"Vedro, temperatura zraka: 6,5 °C, brzina vjetra: 1-3 m/s, tlak zraka: 1018 hPa, oborine: 0","vodostaji_opisno":" Batina 310 cm "}`,
			`{"datum":"2024-03-04","vrijeme":"12:00:00","vrijeme_opisno":"temperatura zraka: 15"}`,
			`{"datum":"2024-03-05","vrijeme":"07:00:00","vrijeme_opisno":null,"vodostaji_opisno":null}`,
		),
		"vode": raw(
			`{"id":1,"voda":"Upis"}`,
			`{"id":2,"voda":"naziv kanala"}`,
			`{"id":3,"voda":"Stara Drava Bilje","vrsta":"Retencija"}`,
			`{"id":4,"voda":"Kanal  Probni","vrsta":"kanal","red":"III. red","stavka":" 3.1. "}`,
			`{"id":5,"voda":"Nasip Draž","vrsta":"Nasip","red":"II. red"}`,
			`{"id":6,"voda":"Nasip Novi","vrsta":"nasip","red":"II. red"}`,
			`{"id":7,"voda":"Potok Mali","vrsta":"bujica","red":"I. red - ostale"}`,
			`{"id":8,"voda":"Dunavac Probni"}`,
			`{"id":9,"voda":"Kanal Postojeći"}`,
		),
		"a02_stavke": raw(
			`{"id_stavke":10,"voda":null}`, `{"id_stavke":11,"voda":1}`, `{"id_stavke":12,"voda":2}`, `{"id_stavke":13,"voda":3}`,
			`{"id_stavke":15,"voda":5}`, `{"id_stavke":16,"voda":6}`, `{"id_stavke":17,"voda":7}`, `{"id_stavke":18,"voda":8}`, `{"id_stavke":19,"voda":9}`,
		),
		"a03_stavke": raw(`{"id_stavke":24,"voda":4}`),
		"a02_evidencija_radova": raw(
			// 4. 3. 2024.: opći upis, sedam izvođačevih i jedan nadzora
			`{"id":9,"datum":"2024-03-04","naziv_vode":15,"upis":"Pregledan nasip.","user_created":"u-nadzor"}`,
			`{"id":1,"datum":"2024-03-04","naziv_vode":11,"upis":"Opći upis","user_created":"u-izv"}`,
			`{"id":2,"datum":"2024-03-04","naziv_vode":13,"upis":"  Košnja retencije ","lokacija":" 0+000 - 1+200 ","user_created":"u-izv","date_created":"2024-03-04T14:20:00Z"}`,
			`{"id":3,"datum":"2024-03-04","naziv_vode":15,"upis":"Košnja nasipa","user_created":"u-izv"}`,
			`{"id":4,"datum":"2024-03-04","naziv_vode":19,"upis":"Košnja kanala","user_created":"u-izv"}`,
			`{"id":5,"datum":"2024-03-04","naziv_vode":13,"upis":"Košnja 5","user_created":"u-izv"}`,
			`{"id":6,"datum":"2024-03-04","naziv_vode":13,"upis":"Košnja 6","user_created":"u-izv"}`,
			`{"id":7,"datum":"2024-03-04","naziv_vode":13,"upis":"Košnja 7","user_created":"u-izv"}`,
			`{"id":8,"datum":"2024-03-04","naziv_vode":13,"upis":"Košnja 8","user_created":"u-izv"}`,
			// 5. 3. 2024.: upisi bez lokacije
			`{"id":20,"datum":"2024-03-05","naziv_vode":12,"upis":"Bez naziva kanala","user_created":"u-izv"}`,
			`{"id":21,"datum":"2024-03-05","naziv_vode":10,"upis":"Stavka bez vode","user_created":"u-nepoznat"}`,
			// stavke 5 nema, a voda 5 (Nasip Draž) ima: upis ipak ostaje bez vode
			`{"id":22,"datum":"2024-03-05","naziv_vode":5,"upis":"Nepoznata stavka","user_created":"u-izv"}`,
			`{"id":23,"datum":"2024-03-05","naziv_vode":null,"upis":null,"user_created":null,"date_created":"jučer","lokacija":" Kanal 1+200 "}`,
			// 2025.: nove lokacije
			`{"id":30,"datum":"2025-01-02","naziv_vode":16,"upis":"Nasip novi","user_created":"u-izv","date_created":"2025-01-02T08:15:00Z"}`,
			`{"id":31,"datum":"2025-01-02","naziv_vode":17,"upis":"Potok","user_created":"u-izv"}`,
			`{"id":32,"datum":"2025-01-02","naziv_vode":18,"upis":"Dunavac","user_created":"u-izv"}`,
			`{"id":33,"datum":"2025-01-02","naziv_vode":19,"upis":"Opet postojeći","user_created":"u-izv"}`,
			`{"id":34,"datum":"","upis":"Bez datuma"}`,
		),
		"a03_evidencija_radova": raw(
			`{"id":1,"datum":"2024-06-01","naziv_vode":24,"upis":"Čišćenje kanala"}`,
		),
	}}
}

// upisiLista vraća upise lista po rednom broju
func upisiLista(t *testing.T, deps JournalDeps, listID string) []models.JournalEntry {
	t.Helper()
	upisi, err := deps.Journals.EntriesForSheet(context.Background(), listID)
	if err != nil {
		t.Fatal(err)
	}
	return upisi
}

func TestUvozDnevnikaRadova(t *testing.T) {
	baza, deps := bazaDnevnika(t)
	ctx := context.Background()

	ocekivaneLokacije := []string{
		fmt.Sprintf("%s Nasip Draž [%s %s]", models.ProgramA02, models.WaterOrderSecond, models.MaintenanceKindReservoir),
		fmt.Sprintf("%s Nasip Novi [%s %s]", models.ProgramA02, models.WaterOrderSecond, models.MaintenanceKindReservoir),
		fmt.Sprintf("%s Potok Mali [%s %s]", models.ProgramA02, models.WaterOrderFirst, models.MaintenanceKindTorrent),
		fmt.Sprintf("%s Dunavac Probni [ %s]", models.ProgramA02, models.MaintenanceKindDrainage),
		fmt.Sprintf("%s Kanal Probni [%s %s]", models.ProgramA03, models.WaterOrderThird, models.MaintenanceKindDrainage),
	}
	provjeri := func(rep JournalReport, suho bool) {
		t.Helper()
		if rep.DryRun != suho || rep.Records != 19 || rep.NoDate != 1 || rep.Journals != 3 || rep.Sheets != 5 || rep.Notes != 5 ||
			rep.Entries != 18 || rep.Supervisor != 2 || rep.Unmatched != 5 || rep.NoUser != 3 {
			t.Fatalf("izvješće (suho %v): %+v", suho, rep)
		}
		if rep.PerYear["A.02 2024"] != 13 || rep.PerYear["A.02 2025"] != 4 || rep.PerYear["A.03 2024"] != 1 || len(rep.PerYear) != 3 {
			t.Errorf("po godinama: %v", rep.PerYear)
		}
		if strings.Join(rep.NewLocations, "\n") != strings.Join(ocekivaneLokacije, "\n") {
			t.Errorf("nove lokacije:\n%s", strings.Join(rep.NewLocations, "\n"))
		}
	}

	// suhi prolaz broji, a ne upisuje ni dnevnike ni lokacije
	deps.DryRun = true
	rep, err := RunJournals(ctx, izvorRadova(), deps)
	if err != nil {
		t.Fatal(err)
	}
	provjeri(rep, true)
	var dnevnika, lokacija, voda int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM journals`).Scan(&dnevnika); err != nil {
		t.Fatal(err)
	}
	if err := baza.QueryRow(`SELECT COUNT(*) FROM maintained_waters`).Scan(&lokacija); err != nil {
		t.Fatal(err)
	}
	if err := baza.QueryRow(`SELECT COUNT(*) FROM watercourses`).Scan(&voda); err != nil {
		t.Fatal(err)
	}
	if dnevnika != 0 || lokacija != 2 || voda != 1 {
		t.Fatalf("suhi prolaz upisao: %d dnevnika, %d lokacija, %d voda", dnevnika, lokacija, voda)
	}

	deps.DryRun = false
	var zapisnik []string
	deps.Log = func(f string, a ...any) { zapisnik = append(zapisnik, fmt.Sprintf(f, a...)) }
	rep, err = RunJournals(ctx, izvorRadova(), deps)
	if err != nil {
		t.Fatal(err)
	}
	provjeri(rep, false)
	if len(zapisnik) != 0 {
		t.Errorf("zapisnik bez grešaka: %q", zapisnik)
	}

	dnevnici, err := deps.Journals.ListJournals(ctx, 16)
	if err != nil || len(dnevnici) != 3 {
		t.Fatalf("dnevnici: %d, %v", len(dnevnici), err)
	}
	j, err := deps.Journals.GetJournal(ctx, db.StableID("bp16-journal", "A.02-2024").String())
	if err != nil || j == nil {
		t.Fatalf("dnevnik A.02 2024: %v", err)
	}
	if j.Title != "A.02. Kanali I. i II. reda — rekonstrukcija 2024." || j.Year != 2024 || !j.Reconstruction || j.Kind != models.JournalKindMaintenanceA02 ||
		j.Gauges != "belisce, osijek, batina" || j.AreaID != 16 || j.StartedAt == nil || j.StartedAt.In(models.Zagreb).Format("2006-01-02") != "2024-03-04" ||
		j.EndedAt == nil || j.EndedAt.In(models.Zagreb).Format("2006-01-02") != "2024-03-05" {
		t.Errorf("dnevnik: %+v", j)
	}
	listovi, err := deps.Journals.ListSheets(ctx, j.ID)
	if err != nil || len(listovi) != 3 {
		t.Fatalf("listovi: %d, %v", len(listovi), err)
	}
	sort.Slice(listovi, func(a, b int) bool { return listovi[a].Number < listovi[b].Number })
	// prilike s lista: zapis u 07:30 najbliži je sedam sati
	l1 := listovi[0]
	if l1.Number != 1 || l1.Date.In(models.Zagreb).Format("2006-01-02") != "2024-03-04" || l1.WeatherSource != "UVOZ" || l1.WaterLevels != "Batina 310 cm" ||
		l1.Temperature == nil || *l1.Temperature != 6.5 || l1.WindFrom == nil || *l1.WindFrom != 1 || *l1.WindTo != 3 ||
		*l1.Pressure != 1018 || *l1.Precipitation != 0 {
		t.Errorf("prvi list: %+v", l1)
	}
	if l3 := listovi[2]; l3.Number != 3 || l3.Date.In(models.Zagreb).Format("2006-01-02") != "2024-03-05" || l3.Temperature != nil || l3.WaterLevels != "" {
		t.Errorf("treći list: %+v", l3)
	}

	// prvi list: napomena, opći upis i nadzor bez mjesta, šest izvođačevih
	upisi := upisiLista(t, deps, l1.ID)
	if len(upisi) != 8 {
		t.Fatalf("upisi prvog lista: %d", len(upisi))
	}
	napomena := upisi[0]
	if napomena.Number != 1 || napomena.Text != ReconstructionNote || napomena.Kind != models.EntryKindNote || napomena.Side != models.EntrySideSupervisor ||
		napomena.UserName != "goCOP — uvoz iz evidencije" || !napomena.CreatedAt.Equal(time.Date(2024, 3, 4, 6, 0, 0, 0, models.Zagreb)) {
		t.Errorf("napomena o rekonstrukciji: %+v", napomena)
	}
	if opci := upisi[1]; opci.Number != 2 || opci.Text != "Opći upis" || opci.Side != models.EntrySideSupervisor || opci.Kind != models.EntryKindNote || opci.MaintainedWaterID != "" {
		t.Errorf("opći upis: %+v", opci)
	}
	kosnja := upisi[2]
	if kosnja.Number != 3 || kosnja.Text != "Košnja retencije" || kosnja.Place != "0+000 - 1+200" || kosnja.Side != models.EntrySideContractor ||
		kosnja.Kind != models.EntryKindWork || kosnja.UserName != "Pero Perić" || kosnja.MaintainedWaterID != repository.MaintainedWaterID(16, "Retencija Stara Drava - Bilje") ||
		!kosnja.CreatedAt.Equal(time.Date(2024, 3, 4, 14, 20, 0, 0, time.UTC)) {
		t.Errorf("upis na lokaciju pod drugim imenom: %+v", kosnja)
	}
	if u := upisi[4]; u.MaintainedWaterID != repository.MaintainedWaterID(16, "Kanal Postojeći") {
		t.Errorf("upis na postojeću lokaciju: %+v", u)
	}
	// sedmi izvođačev upis otvara drugi list istog dana, nadzor ostaje na njemu
	drugi := upisiLista(t, deps, listovi[1].ID)
	if listovi[1].Date.In(models.Zagreb).Format("2006-01-02") != "2024-03-04" || len(drugi) != 3 || drugi[0].Text != ReconstructionNote || drugi[0].Number != 9 ||
		drugi[1].Text != "Košnja 8" || drugi[2].Text != "Pregledan nasip." || drugi[2].Side != models.EntrySideSupervisor || drugi[2].UserName != "Iva Ivić" ||
		drugi[2].MaintainedWaterID != repository.MaintainedWaterID(16, "Nasip Draž") {
		t.Errorf("drugi list: %+v", drugi)
	}
	treci := upisiLista(t, deps, listovi[2].ID)
	if len(treci) != 5 {
		t.Fatalf("treći list: %+v", treci)
	}
	for _, u := range treci[1:] {
		if u.MaintainedWaterID != "" || u.Side != models.EntrySideContractor {
			t.Errorf("upis bez lokacije: %+v", u)
		}
	}
	if u := treci[3]; u.Text != "Nepoznata stavka" {
		t.Errorf("upis s nepoznatom stavkom: %+v", u)
	}
	if u := treci[4]; u.Text != "" || u.Place != "Kanal 1+200" || u.UserName != "nepoznat upisivač" || !u.CreatedAt.Equal(time.Date(2024, 3, 5, 7, 0, 0, 0, models.Zagreb)) {
		t.Errorf("upis bez upisivača: %+v", u)
	}
	if u := treci[2]; u.UserName != "nepoznat upisivač" {
		t.Errorf("nepoznat korisnik: %+v", u)
	}

	// nove lokacije: nasip na postojećem i na novom objektu, vode u registru
	draz, _ := deps.Maintenance.GetWater(ctx, repository.MaintainedWaterID(16, "Nasip Draž"))
	postojeci, _ := deps.Structures.GetByCode(ctx, "nasip-draz")
	if draz == nil || postojeci == nil || draz.StructureID != postojeci.ID.String() || draz.WatercourseCode != "" || draz.Source != "evidencija app.bp16.xyz (vode)" {
		t.Errorf("nasip Draž: %+v", draz)
	}
	novi, _ := deps.Structures.GetByCode(ctx, "bp16-nasip-novi")
	if novi == nil || novi.Kind != models.StructureKindEmbankment || novi.SectorID != "B" || novi.AreaID != 16 || novi.Origin != "DIRECTUS_BP16" {
		t.Fatalf("novi nasip: %+v", novi)
	}
	if mw, _ := deps.Maintenance.GetWater(ctx, repository.MaintainedWaterID(16, "Nasip Novi")); mw == nil || mw.StructureID != novi.ID.String() {
		t.Errorf("lokacija novog nasipa: %+v", mw)
	}
	for sifra, ocek := range map[string]models.Watercourse{
		"kanal-probni-bp16": {OfficialName: "Kanal Probni", Name: "Probni", Kind: "kanal"},
		"potok-mali":        {OfficialName: "Potok Mali", Name: "Mali", Kind: "potok"},
		"dunavac-probni":    {OfficialName: "Dunavac Probni", Name: "Dunavac Probni", Kind: ""},
	} {
		w, err := deps.Waters.GetWatercourse(ctx, sifra)
		if err != nil || w == nil || w.OfficialName != ocek.OfficialName || w.Name != ocek.Name || w.Kind != ocek.Kind || w.Origin != "DIRECTUS_BP16" {
			t.Errorf("voda %s: %+v, %v", sifra, w, err)
		}
	}
	if mw, _ := deps.Maintenance.GetWater(ctx, repository.MaintainedWaterID(16, "Kanal Probni")); mw == nil || mw.Seq != "3.1." ||
		mw.WatercourseCode != "kanal-probni-bp16" || mw.Program != models.ProgramA03 || mw.Order != models.WaterOrderThird {
		t.Errorf("lokacija A.03: %+v", mw)
	}
	g25, _ := deps.Journals.GetJournal(ctx, db.StableID("bp16-journal", "A.02-2025").String())
	if g25 == nil {
		t.Fatal("nema dnevnika A.02 2025")
	}
	l25, _ := deps.Journals.ListSheets(ctx, g25.ID)
	if len(l25) != 1 {
		t.Fatalf("listovi 2025: %+v", l25)
	}
	// broj lista i upisa kreće ispočetka u svakoj godini
	if u := upisiLista(t, deps, l25[0].ID); len(u) != 5 || u[1].Number != 2 || !u[1].CreatedAt.Equal(time.Date(2025, 1, 2, 8, 15, 0, 0, time.UTC)) {
		t.Errorf("upisi 2025: %+v", u)
	}

	// ponovni uvoz ne stvara nove lokacije ni dvostruke upise
	rep, err = RunJournals(ctx, izvorRadova(), deps)
	if err != nil || len(rep.NewLocations) != 0 || rep.Entries != 18 {
		t.Fatalf("ponovni uvoz: %+v, %v", rep, err)
	}
	var upisa, nasipa int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM journal_entries`).Scan(&upisa); err != nil {
		t.Fatal(err)
	}
	if err := baza.QueryRow(`SELECT COUNT(*) FROM structures WHERE kind = ?`, models.StructureKindEmbankment).Scan(&nasipa); err != nil {
		t.Fatal(err)
	}
	if upisa != 23 || nasipa != 2 {
		t.Errorf("nakon ponovnog uvoza: %d upisa, %d nasipa", upisa, nasipa)
	}
}

// Nedostupni korisnici i vrijeme samo se zapišu; greška izvora ili baze
// prekida uvoz.
func TestUvozDnevnikaRadovaGreske(t *testing.T) {
	ctx := context.Background()

	predlozak, deps := bazaDnevnika(t)
	var zapisnik []string
	deps.Log = func(f string, a ...any) { zapisnik = append(zapisnik, fmt.Sprintf(f, a...)) }
	deps.DryRun = true
	rep, err := RunJournals(ctx, pokvareniIzvor{probniIzvor: izvorRadova(), korisnici: errors.New("401"),
		greske: map[string]error{"vrijeme_za_list": errors.New("403")}}, deps)
	if err != nil || rep.NoUser != rep.Entries || rep.Supervisor != 1 {
		t.Fatalf("bez korisnika i vremena: %+v, %v", rep, err)
	}
	if strings.Join(zapisnik, "\n") != "Korisnici Directusa nisu dostupni (401): upisi ostaju bez imena\nvrijeme_za_list nije dostupno (403): listovi bez prilika" {
		t.Errorf("zapisnik: %q", zapisnik)
	}

	for _, zbirka := range []string{"vode", "a02_stavke", "a02_evidencija_radova", "a03_stavke", "a03_evidencija_radova"} {
		_, err := RunJournals(ctx, pokvareniIzvor{probniIzvor: izvorRadova(), greske: map[string]error{zbirka: errors.New("Directus odgovorio 403")}}, deps)
		if err == nil || err.Error() != "Directus odgovorio 403" {
			t.Errorf("%s: %v", zbirka, err)
		}
	}

	// zatvorena baza: lokacije se ne mogu pročitati
	if _, err := RunJournals(ctx, izvorRadova(), depsDnevnika(zatvorenaBaza(t))); err == nil {
		t.Error("zatvorena baza je prošla")
	}

	// nasip ili voda iz registra koji se ne da pročitati: uvoz stane s
	// greškom čitanja, a ne upiše nasip drugi put ni padne na tuđoj šifri
	for ime, s := range map[string]struct{ kvar, poruka string }{
		"nasip": {`UPDATE structures SET start_cm = 'abc' WHERE code = 'nasip-draz'`, "čitanje nasipa područja: "},
		"voda":  {`UPDATE watercourses SET length_km = 'abc' WHERE code = 'kanal-probni'`, "čitanje voda: "},
	} {
		baza := kopijaBaze(t, predlozak)
		if _, err := baza.Exec(s.kvar); err != nil {
			t.Fatal(err)
		}
		_, err := RunJournals(ctx, izvorRadova(), depsDnevnika(baza))
		if err == nil || !strings.HasPrefix(err.Error(), s.poruka) {
			t.Errorf("%s koji se ne da pročitati: %v", ime, err)
		}
		var nasipa, voda int
		if err := baza.QueryRow(`SELECT (SELECT COUNT(*) FROM structures), (SELECT COUNT(*) FROM watercourses)`).Scan(&nasipa, &voda); err != nil {
			t.Fatal(err)
		}
		if nasipa != 1 || voda != 1 {
			t.Errorf("%s koji se ne da pročitati: upisano %d nasipa, %d voda", ime, nasipa, voda)
		}
	}

	// baza odbija upis na zadanom mjestu: okidač glumi pun disk
	for ime, okidac := range map[string]string{
		"dnevnik":  `BEFORE INSERT ON journals`,
		"list":     `BEFORE INSERT ON journal_sheets`,
		"napomena": `BEFORE INSERT ON journal_entries WHEN NEW.text LIKE 'REKONSTRUKCIJA%'`,
		"upis":     `BEFORE INSERT ON journal_entries WHEN NEW.text = 'Opći upis'`,
		"nasip":    `BEFORE INSERT ON structures`,
		"voda":     `BEFORE INSERT ON watercourses`,
		"lokacija": `BEFORE INSERT ON maintained_waters`,
	} {
		baza := kopijaBaze(t, predlozak)
		if _, err := baza.Exec(`CREATE TRIGGER pun_disk ` + okidac + ` BEGIN SELECT RAISE(ABORT, 'disk pun'); END`); err != nil {
			t.Fatal(err)
		}
		if _, err := RunJournals(ctx, izvorRadova(), depsDnevnika(baza)); err == nil || !strings.Contains(err.Error(), "disk pun") {
			t.Errorf("%s: %v", ime, err)
		}
	}
}
