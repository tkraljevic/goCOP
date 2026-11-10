package web

import (
	"strings"
	"testing"

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
