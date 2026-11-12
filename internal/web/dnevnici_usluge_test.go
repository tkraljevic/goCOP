package web

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Dan na popisu dnevnika usluga piše se kao na crtežu, s razmacima; ono što
// nije dan ostaje kako je upisano.
func TestDanNaZaslonu(t *testing.T) {
	for ulaz, izlaz := range map[string]string{"2026-10-26": "26. 10. 2026.", "2026-01-03": "3. 1. 2026.", "krivo": "krivo", "": ""} {
		if got := danNaZaslonu(ulaz); got != izlaz {
			t.Errorf("danNaZaslonu(%q) = %q, a mora %q", ulaz, got, izlaz)
		}
	}
}

// Popis dnevnika usluga (crtež 9 i 9a): povratak na Dokumente, kartica s
// naslovnicom, oznakama i brojem listova, područje kao oznaka ili izbor.
func TestPopisDnevnikaUslugaPoCrtezu(t *testing.T) {
	baranja := models.Area{ID: 16, Name: "Mali sliv Baranja"}
	d := JournalPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Vrsta:       models.JournalKindMaintenanceA02,
		Area:        &baranja,
		Areas:       []models.Area{baranja},
		CanManage:   true,
		Journals: []models.Journal{
			{ID: "j1", Kind: models.JournalKindMaintenanceA02, Title: "A.02. Kanali I. i II. reda", Year: 2026, Contractor: "Vodogradnja Primjer d.o.o.",
				ContractorLead: "Pero Primjer", Supervisor: "Zoran Primjer", SheetCount: 148, LastSheetOn: "2026-10-26", OpenTasks: 2},
			{ID: "j2", Kind: models.JournalKindMaintenanceA02, Title: "A.02. Kanali I. i II. reda", Year: 2024, SheetCount: 1, Reconstruction: true},
		},
	}
	html := iscrtaj(t, "dnevnici.html", d)
	for _, want := range []string{
		`<a href="/dnevnici" class="povratna-poveznica">← Dokumenti</a>`, "Dnevnik usluga A.02</h1>",
		`href="/dnevnici/new?area=16&kind=ODRZAVANJE_A02"`, "BP 16: Mali sliv Baranja", `href="/dnevnici/j1"`,
		"Vodogradnja Primjer d.o.o.", "Voditelj usluga", "Zoran Primjer", "148 listova", "zadnji list 26. 10. 2026.",
		`<span class="oznaka-stanja obrub">2 otvorena naloga</span>`, `oznaka-stanja pozor`, ">rekonstrukcija</span>", "1 list</span>",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("popis nema %q", want)
		}
	}
	if strings.Contains(html, `id="area"`) || strings.Contains(html, "breadcrumb") {
		t.Error("uz jedno područje nema izbora, a povratak nije breadcrumb")
	}

	// Više područja: izbor; bez prava upravljanja nema gumba.
	d.Areas = append(d.Areas, models.Area{ID: 17, Name: "Karašica"})
	d.CanManage = false
	html = iscrtaj(t, "dnevnici.html", d)
	if !strings.Contains(html, `id="area"`) || !strings.Contains(html, ">BP 17: Karašica</option>") {
		t.Error("uz više područja nema izbora područja")
	}
	if strings.Contains(html, "Novi dnevnik") {
		t.Error("bez prava upravljanja popis nudi Novi dnevnik")
	}

	// Prazan popis (9a): rečenica, a uputa samo uz pravo otvaranja.
	d.Journals, d.Vrsta = nil, models.JournalKindMaintenanceA03
	html = iscrtaj(t, "dnevnici.html", d)
	if !strings.Contains(html, "Dnevnik usluga A.03</h1>") || !strings.Contains(html, "U području Mali sliv Baranja još nema dnevnika te vrste.") {
		t.Error("prazan popis A.03 nema naslov ili rečenicu")
	}
	if strings.Contains(html, "Otvorite ga gumbom") {
		t.Error("uputa za otvaranje bez prava")
	}
	d.CanManage = true
	if html = iscrtaj(t, "dnevnici.html", d); !strings.Contains(html, "Otvorite ga gumbom „Novi dnevnik“ i popunite naslovnicu.") {
		t.Error("uz pravo otvaranja nema upute")
	}
	d.Area = nil
	if html = iscrtaj(t, "dnevnici.html", d); !strings.Contains(html, "<p>Nema područja.</p>") {
		t.Error("bez područja nema rečenice")
	}
}

// Stranica dnevnika usluge (crtež 9b, 9g, 9h): povratak na popis vrste,
// naslovnica, Novi list, nalozi i listovi kao tablica i kartice, rekonstrukcija
// u boji upozorenja. Rupe u brojevima ostaju crvene.
func TestStranicaDnevnikaUslugePoCrtezu(t *testing.T) {
	dan := func(m, d int) time.Time { return time.Date(2026, time.Month(m), d, 0, 0, 0, 0, models.Zagreb) }
	pocetak, prosli, potvrdio := dan(1, 15), dan(10, 22), dan(10, 23)
	rok := time.Now().In(models.Zagreb).AddDate(0, 1, 0)
	temp := 12.0
	d := JournalPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Journal: &models.Journal{ID: "j1", Kind: models.JournalKindMaintenanceA02, Title: "A.02. Kanali I. i II. reda", Year: 2026, AreaID: 16,
			Contractor: "Vodogradnja Primjer d.o.o.", ContractorLead: "Pero Primjer", Supervisor: "Zoran Primjer", SupervisorDeputy: "Dario Primjer",
			Contract: "KLASA 325-01/26-09/000", StartedAt: &pocetak},
		Area:     &models.Area{ID: 16, Name: "Mali sliv Baranja"},
		CanWrite: true, CanManage: true, BriseDnevnik: true, Today: "2026-10-26",
		OpenTasks: []models.JournalEntry{
			{ID: "e1", SheetID: "s1", Number: 412, Date: dan(10, 23), Kind: models.EntryKindTask, Status: models.TaskOpen, Place: "Kanal Travnik 2+100", Text: "Ponoviti košnju pokosa", DueDate: &rok, UserName: "Zoran Primjer"},
			{ID: "e2", SheetID: "s2", Number: 405, Date: dan(10, 15), Kind: models.EntryKindTask, Status: models.TaskOpen, Text: "Popraviti rampu", DueDate: &prosli},
		},
		Sheets: []models.JournalSheet{
			{ID: "s1", Number: 148, Date: dan(10, 26), Label: "košnja", Conditions: "oblačno", Temperature: &temp, Rating: 3, EntryCount: 5},
			{ID: "s2", Number: 147, Date: dan(10, 23), Rating: 4, EntryCount: 1, ContractorConfirmedAt: &potvrdio, ContractorConfirmedBy: "Pero Primjer"},
			{ID: "s3", Number: 146, Date: dan(10, 22)},
		},
		Gaps: []int{7},
	}
	html := iscrtaj(t, "dnevnik.html", d)
	for _, want := range []string{
		`<a href="/dnevnici/popis?vrsta=ODRZAVANJE_A02&amp;area=16" class="povratna-poveznica">← Dnevnik usluga A.02</a>`,
		`data-naziv-stranice="Dnevnik"`, "BP 16: Mali sliv Baranja", `href="/dnevnici/j1/ispis"`, `href="/dnevnici/j1/edit"`,
		"<dt>Ovlaštenik</dt><dd>Zoran Primjer</dd>", "<dt>Početak</dt><dd>15. 1. 2026.</dd>", `action="/dnevnici/j1/obrisi"`,
		`action="/dnevnici/j1/listovi"`, `name="date"`, `value="2026-10-26"`, `name="label"`,
		"Otvoreni nalozi (2)", "Ponoviti košnju pokosa", rok.Format("2. 1. 2006."), "22. 10. 2026.", "zadao Zoran Primjer", `<span class="oznaka-stanja pozor">rok prošao</span>`,
		"Listovi dnevnika (3)", `href="/dnevnici/j1/listovi/s1"`, "oblačno, 12 °C · 3. dobri", "4. vrlo dobri</td>", "—</td>",
		`<span class="oznaka-stanja obrub" title="Pero Primjer">✓ izvođač</span>`, `<span class="oznaka-stanja">nadzor</span>`,
		`alert alert-danger`, "nedostaju: 7",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("dnevnik nema %q", want)
		}
	}
	if strings.Contains(html, "breadcrumb") || strings.Contains(html, "table-responsive") || strings.Contains(html, "Rekonstrukcija.") {
		t.Error("dnevnik ima stari put, tablicu s klizačem ili traku rekonstrukcije bez razloga")
	}
	// Prvi nalog nema rok prošao, drugi ima; nalog bez mjesta piše crticu.
	if strings.Count(html, "rok prošao") != 1 {
		t.Error("rok prošao mora stajati samo uz nalog kojem je rok prošao")
	}

	// Rekonstrukcija (9h) u boji upozorenja, A.03 se vraća na svoj popis,
	// izvođač bez prava nema Naslovnicu, brisanje ni Novi list.
	d.Journal.Reconstruction, d.Journal.Notes, d.Journal.Kind = true, "Dnevnik je složen iz starije evidencije.", models.JournalKindMaintenanceA03
	d.CanWrite, d.CanManage, d.BriseDnevnik, d.Sheets, d.OpenTasks, d.Gaps = false, false, false, nil, nil, nil
	html = iscrtaj(t, "dnevnik.html", d)
	for _, want := range []string{"alert alert-warning", "<strong>Rekonstrukcija.</strong> Dnevnik je složen iz starije evidencije.",
		"← Dnevnik usluga A.03", "Još nema nijednog lista."} {
		if !strings.Contains(html, want) {
			t.Errorf("rekonstruirani dnevnik nema %q", want)
		}
	}
	for _, nesmije := range []string{"/edit", "/obrisi", `action="/dnevnici/j1/listovi"`, "Otvoreni nalozi", "nedostaju"} {
		if strings.Contains(html, nesmije) {
			t.Errorf("dnevnik bez prava ima %q", nesmije)
		}
	}

	// Stariji dnevnik obrane po području vraća se na Dokumente i zove nadzor po svom.
	d.Journal.Kind, d.Journal.Reconstruction = models.JournalKindDefense, false
	html = iscrtaj(t, "dnevnik.html", d)
	if !strings.Contains(html, `<a href="/dnevnici" class="povratna-poveznica">← Dokumenti</a>`) || !strings.Contains(html, "<dt>Nadzorni inženjer</dt>") {
		t.Error("dnevnik obrane po području nema povratak na Dokumente ili nadzornog inženjera")
	}
}

// List dnevnika usluge (crtež 9c, 9d): povratak na dnevnik, potvrde ispod
// naslova, upisi kao tablica s radnjama riječima, storno precrtan s razlogom.
func TestListDnevnikaUslugePoCrtezu(t *testing.T) {
	dan := time.Date(2026, 10, 26, 0, 0, 0, 0, models.Zagreb)
	upisano := time.Date(2026, 10, 26, 8, 5, 0, 0, models.Zagreb)
	rok := time.Now().In(models.Zagreb).AddDate(0, 1, 0)
	d := JournalPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Journal:     &models.Journal{ID: "j1", Kind: models.JournalKindMaintenanceA02, Title: "A.02. Kanali", Year: 2026, AreaID: 16, Contractor: "Vodogradnja Primjer d.o.o."},
		Area:        &models.Area{ID: 16, Name: "Mali sliv Baranja"},
		Sheet:       &models.JournalSheet{ID: "s1", JournalID: "j1", Number: 148, Date: dan, Label: "košnja"},
		CanWrite:    true, CanSupervise: true, Capacity: 6, Used: 2,
		Entries: []models.JournalEntry{
			{ID: "e1", Number: 610, Kind: models.EntryKindWork, Place: "Kanal Travnik 1+000", Text: "Strojna košnja trave.", WorkItemNo: "1.2", WorkItemText: "Strojna košnja trave",
				UserName: "Pero Primjer", CreatedAt: upisano, Voided: true, VoidedBy: "Pero Primjer", VoidReason: "krivo upisana stacionaža"},
			{ID: "e2", Number: 613, Kind: models.EntryKindTask, Status: models.TaskOpen, Text: "Popraviti rampu.", DueDate: &rok, UserName: "Zoran Primjer", CreatedAt: upisano},
		},
	}
	html := iscrtaj(t, "dnevnik_list.html", d)
	for _, want := range []string{
		`<a href="/dnevnici/j1" class="povratna-poveznica">← A.02. Kanali`, `data-naziv-stranice="List dnevnika"`, "List 148 · 26. 10. 2026. · košnja",
		"izvođač: nije potvrdio", "nadzor: nije potvrdio", `action="/dnevnici/j1/listovi/s1/potvrdi"`, "Potvrdi list</button>",
		`class="uu-storno"`, "STORNO (Pero Primjer): krivo upisana stacionaža", "1.2 · Strojna košnja trave", "26. 10. 26. 08:05",
		">izveden</button>", ">otkaži</button>", ">storniraj</button>", "—</td>", "Novi list za ovaj dan",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("list nema %q", want)
		}
	}
	if strings.Contains(html, "breadcrumb") || strings.Contains(html, "table-responsive") || strings.Contains(html, `title="Storniraj upis"`) {
		t.Error("list ima stari put, tablicu s klizačem ili radnje kao ikone")
	}
	// stornirani upis nema radnji; otvoreni nalog ima tri
	if n := strings.Count(html, ">storniraj</button>"); n != 1 {
		t.Errorf("storniraj se nudi %d puta, želim jednom", n)
	}
}
