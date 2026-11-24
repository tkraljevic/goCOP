package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Nalozi u obrani kroz rute (Odluke (22. 11. 2026.)): dok na području
// traje obrana, list dnevnika A.02 rukovoditelju dionice nudi nalog s
// dionicom područja, a upisan nalog pokazuje njegovu ulogu. Bez obrane
// nalog se ne nudi. Okolina je sektor P s područjima 1 i 2; podaci su
// izmišljeni.

// obranaNaDionici je izvor stanja obrane za probu: redovna obrana na
// dionicama s popisa
func obranaNaDionici(dionice ...string) func(context.Context, string, time.Time) (map[string]models.StanjeObrane, error) {
	return func(context.Context, string, time.Time) (map[string]models.StanjeObrane, error) {
		out := map[string]models.StanjeObrane{}
		for _, d := range dionice {
			out[d] = models.StanjeObrane{Aktivni: []models.AktivniStadij{{Stupanj: models.PhaseRegular}}}
		}
		return out, nil
	}
}

// dioniceSektoraP su dionice probe: P.1.1 i P.1.2 na području 1, P.2.1 na 2
func dioniceSektoraP(_ string, podrucje int, _ string) ([]models.Section, error) {
	var out []models.Section
	for _, d := range []models.Section{{Code: "P.1.1", AreaID: 1, SectorID: "P", Description: "Lijevi nasip"},
		{Code: "P.1.2", AreaID: 1, SectorID: "P"}, {Code: "P.2.1", AreaID: 2, SectorID: "P"}} {
		if podrucje == 0 || d.AreaID == podrucje {
			out = append(out, d)
		}
	}
	return out, nil
}

// listUObrani je proba s rutama lista i dnevnikom A.02 firme na području 1
func listUObrani(t *testing.T) (*copProba, *models.Journal, *models.JournalSheet) {
	t.Helper()
	p := novaCOPProba(t)
	h := p.h
	p.mux.HandleFunc("GET /dnevnici/{id}/listovi/{sheet}", h.ShowSheet)
	p.mux.HandleFunc("POST /dnevnici/{id}/listovi/{sheet}/upisi", h.HandleAddEntry)
	p.mux.HandleFunc("POST /dnevnici/{id}/upisi/{entry}/stanje", h.HandleTaskStatus)
	rec := ledger.New(p.baza, "test")
	h.maintenance = service.NewMaintenanceService(repository.NewMaintenanceRepository(p.baza, rec),
		repository.NewWatercourseRepository(p.baza, rec), repository.NewStructureRepository(p.baza, rec))
	h.sections = service.NewSectionService(repository.NewSectionRepository(p.baza, rec), nil)
	j := p.dnevnik(t, 1, models.JournalKindMaintenanceA02, "A.02", "Primjer d.o.o.", "", nil)
	list := &models.JournalSheet{JournalID: j.ID, Date: time.Date(2026, 11, 22, 0, 0, 0, 0, models.Zagreb)}
	if err := p.journalRepo.SaveSheet(context.Background(), list); err != nil {
		t.Fatal(err)
	}
	return p, j, list
}

func TestNalogUObraniKrozRute(t *testing.T) {
	p, j, list := listUObrani(t)
	prvo := 1
	dionicar := p.osoba(t, "pperic", "Pero Perić", "", models.Duty{Title: "Rukovoditelj dionice P.1.1", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection, AreaID: &prvo})
	listPut := "/dnevnici/" + j.ID + "/listovi/" + list.ID

	// bez obrane nalog se ne nudi i ne prolazi
	stranica := p.salji(t, dionicar, http.MethodGet, listPut, nil).Body.String()
	if strings.Contains(stranica, `<option value="NALOG">`) || strings.Contains(stranica, `name="section_code"`) {
		t.Error("bez obrane list nudi nalog ili dionicu")
	}
	nalog := url.Values{"kind": {models.EntryKindTask}, "section_code": {"P.1.1"}, "text": {"dovesti bager na nasip"}, "due_date": {"2026-11-23"}}
	if got := ishod(p.salji(t, dionicar, http.MethodPost, listPut+"/upisi", nalog)); got != "error" {
		t.Errorf("bez obrane rukovoditelj dionice piše nalog: %s", got)
	}

	p.h.journals.SetObrana(obranaNaDionici("P.1.2"), dioniceSektoraP)
	stranica = p.salji(t, dionicar, http.MethodGet, listPut, nil).Body.String()
	for _, treba := range []string{`<option value="NALOG">`, `name="section_code"`, `<option value="P.1.1">P.1.1 · Lijevi nasip</option>`, "Na području traje obrana"} {
		if !strings.Contains(stranica, treba) {
			t.Errorf("list u obrani nema %q", treba)
		}
	}
	if strings.Contains(stranica, `value="P.2.1"`) {
		t.Error("list nudi dionicu drugog područja")
	}
	if got := ishod(p.salji(t, dionicar, http.MethodPost, listPut+"/upisi", nalog)); got != "success" {
		t.Fatalf("rukovoditelj dionice ne piše nalog u obrani: %s", got)
	}
	upisi, err := p.journalRepo.EntriesForSheet(context.Background(), list.ID)
	if err != nil || len(upisi) != 1 || upisi[0].UlogaDavatelja != "Rukovoditelj dionice" || upisi[0].SectionCode != "P.1.1" || upisi[0].DueDate == nil {
		t.Fatalf("upisan nalog: %+v, %v", upisi, err)
	}
	if stranica = p.salji(t, dionicar, http.MethodGet, listPut, nil).Body.String(); !strings.Contains(stranica, `<span class="uu-sitno">Rukovoditelj dionice</span>`) {
		t.Error("list ne pokazuje ulogu davatelja")
	}

	// poslovođa označava nalog izvedenim
	poslovodja := p.poslovodja(t, "mmarkic", "Marko Markić", "Primjer d.o.o.")
	if got := ishod(p.salji(t, poslovodja, http.MethodPost, "/dnevnici/"+j.ID+"/upisi/"+upisi[0].ID+"/stanje",
		url.Values{"sheet": {list.ID}, "status": {models.TaskDone}})); got != "success" {
		t.Errorf("poslovođa ne označava nalog izvedenim: %s", got)
	}
}

// Kartica naloga izvođača uz ime pokazuje i ulogu onoga tko je nalog dao
func TestRedakNalogaSUlogomDavatelja(t *testing.T) {
	sad := time.Date(2026, 11, 22, 10, 0, 0, 0, models.Zagreb)
	d := dnevnikRadova{Dnevnik: &models.Journal{ID: "j1"}}
	e := models.JournalEntry{Kind: models.EntryKindTask, Status: models.TaskOpen, Text: "dovesti bager", SheetID: "l1", Date: sad,
		UserName: "Pero Perić", UlogaDavatelja: "Rukovoditelj dionice"}
	if r := redakOtvorenogNaloga(e, d, sad); !strings.HasPrefix(r.Sporedno, "Pero Perić, rukovoditelj dionice · zadan") {
		t.Errorf("redak naloga: %q", r.Sporedno)
	}
	e.UlogaDavatelja = ""
	if r := redakOtvorenogNaloga(e, d, sad); !strings.HasPrefix(r.Sporedno, "Pero Perić · zadan") {
		t.Errorf("redak naloga bez uloge: %q", r.Sporedno)
	}
}
