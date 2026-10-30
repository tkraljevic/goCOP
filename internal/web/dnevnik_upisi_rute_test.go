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

// Stanje naloga kroz rutu jednog dnevnika usluge ne mijenja nalog iz drugog
// dnevnika. U svom dnevniku nalog otkazuje ovlaštenik za praćenje ugovora,
// a rukovoditelj područja ne, jer nije nadzor. Podaci su izmišljeni.
func TestStanjeNalogaSamoUSvomDnevnikuKrozRutu(t *testing.T) {
	p := novaCOPProba(t)
	p.mux.HandleFunc("POST /dnevnici/{id}/upisi/{entry}/stanje", p.h.HandleTaskStatus)
	ctx := context.Background()
	prvo := 1
	rina := p.osoba(t, "rrukovoditelj", "Rina Rukovoditeljica", "", models.Duty{Title: "Rukovoditelj područja", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &prvo})
	rukovoditelj := p.osoba(t, "oovlastenik", "Olga Ovlaštenica", "", models.Duty{Title: "Ovlaštenik A2", Role: models.RoleContractOfficerA2, ScopeType: models.ScopeArea, AreaID: &prvo})
	prvi := p.dnevnik(t, 1, models.JournalKindMaintenanceA02, "A.02 prvi", "Primjer d.o.o.", "", nil)
	drugi := p.dnevnik(t, 1, models.JournalKindMaintenanceA02, "A.02 drugi", "Primjer d.o.o.", "", nil)
	nalog := &models.JournalEntry{JournalID: drugi.ID, Date: time.Date(2026, 10, 2, 0, 0, 0, 0, models.Zagreb), Kind: models.EntryKindTask, Text: "pokositi nasip", Status: models.TaskOpen}
	if err := p.journalRepo.SaveEntry(ctx, nalog); err != nil {
		t.Fatal(err)
	}
	otkazi := url.Values{"status": {models.TaskCancelled}}

	if got := ishod(p.salji(t, rukovoditelj, http.MethodPost, "/dnevnici/"+prvi.ID+"/upisi/"+nalog.ID+"/stanje", otkazi)); got != "error" {
		t.Errorf("stanje naloga iz drugog dnevnika: %s", got)
	}
	if e, err := p.journalRepo.GetEntry(ctx, nalog.ID); err != nil || e == nil || e.Status != models.TaskOpen {
		t.Errorf("nalog iz drugog dnevnika je promijenjen: %+v", e)
	}

	if got := ishod(p.salji(t, rina, http.MethodPost, "/dnevnici/"+drugi.ID+"/upisi/"+nalog.ID+"/stanje", otkazi)); got != "error" {
		t.Errorf("rukovoditelj područja otkazuje nalog: %s", got)
	}

	// kontrola: u svom dnevniku nalog se otkazuje
	if got := ishod(p.salji(t, rukovoditelj, http.MethodPost, "/dnevnici/"+drugi.ID+"/upisi/"+nalog.ID+"/stanje", otkazi)); got != "success" {
		t.Errorf("stanje naloga u svom dnevniku: %s", got)
	}
	if e, err := p.journalRepo.GetEntry(ctx, nalog.ID); err != nil || e == nil || e.Status != models.TaskCancelled {
		t.Errorf("nalog u svom dnevniku nije otkazan: %+v", e)
	}
}

// Ispravak prijepisa kroz rutu: zapis iz drugog dnevnika se ne ispravlja i
// njegov broj ne ulazi u povratnu adresu, a ovjeren prijepis ne prima
// ispravak i ne nudi ga na stranici. Neovjeren prijepis dežurni operater
// ispravlja kao i dosad. Podaci su izmišljeni.
func TestIspravakPrijepisaKrozRutu(t *testing.T) {
	p := novaCOPProba(t)
	p.mux.HandleFunc("POST /dnevnici/{id}/upisi/{entry}/ispravak", p.h.HandleIspraviPrijepis)
	ctx := context.Background()
	operater := p.osoba(t, "ddezurni", "Dora Dežurna", "", models.Duty{Title: "Dežurni operater", Role: models.RoleOperator, ScopeType: models.ScopeSector})
	pocetak := time.Date(2009, 6, 1, 0, 0, 0, 0, models.Zagreb)
	dan := pocetak.AddDate(0, 0, 3)
	prijepis := func(naslov string) (*models.Journal, *models.JournalEntry) {
		j := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "P", Title: naslov, Year: 2009, Reconstruction: true, StartedAt: &pocetak}
		if err := p.journalRepo.SaveJournal(ctx, j); err != nil {
			t.Fatal(err)
		}
		e := &models.JournalEntry{JournalID: j.ID, Date: dan, Kind: models.EntryKindNote, Text: "vodostaj 55l cm"}
		if err := p.journalRepo.SaveEntry(ctx, e); err != nil {
			t.Fatal(err)
		}
		return j, e
	}
	prvi, _ := prijepis("Prijepis prvi")
	drugi, tudji := prijepis("Prijepis drugi")
	ispravak := url.Values{"time": {"07:00"}, "kind": {models.EntryKindReport}, "text": {"vodostaj 551 cm"}}

	w := p.salji(t, operater, http.MethodPost, "/dnevnici/"+prvi.ID+"/upisi/"+tudji.ID+"/ispravak", ispravak)
	if got := ishod(w); got != "error" || strings.Contains(w.Header().Get("Location"), "#zapis-") {
		t.Errorf("ispravak zapisa drugog dnevnika: %s, %s", got, w.Header().Get("Location"))
	}
	if e, err := p.journalRepo.GetEntry(ctx, tudji.ID); err != nil || e == nil || e.Text != "vodostaj 55l cm" {
		t.Errorf("zapis drugog dnevnika je ispravljen: %+v", e)
	}

	// ovjera drugog prijepisa
	if _, err := p.baza.Exec(`UPDATE journals SET ended_at = ?, zakljucio = 'Vid Voditelj', zakljuceno_at = ? WHERE id = ?`,
		pocetak.AddDate(0, 0, 9).UTC(), time.Now().UTC(), drugi.ID); err != nil {
		t.Fatal(err)
	}
	if got := ishod(p.salji(t, operater, http.MethodPost, "/dnevnici/"+drugi.ID+"/upisi/"+tudji.ID+"/ispravak", ispravak)); got != "error" {
		t.Errorf("ispravak u ovjerenom prijepisu: %s", got)
	}
	if e, err := p.journalRepo.GetEntry(ctx, tudji.ID); err != nil || e == nil || e.Text != "vodostaj 55l cm" {
		t.Errorf("zapis ovjerenog prijepisa je ispravljen: %+v", e)
	}
	if strings.Contains(p.salji(t, operater, http.MethodGet, "/dnevnici/"+drugi.ID, nil).Body.String(), "/ispravak") {
		t.Error("ovjeren prijepis nudi ispravak")
	}

	// kontrola: neovjeren prijepis, svoj zapis
	treci, svoj := prijepis("Prijepis treći")
	w = p.salji(t, operater, http.MethodPost, "/dnevnici/"+treci.ID+"/upisi/"+svoj.ID+"/ispravak", ispravak)
	if got := ishod(w); got != "success" || !strings.HasSuffix(w.Header().Get("Location"), "#zapis-1") {
		t.Errorf("ispravak u neovjerenom prijepisu: %s, %s", got, w.Header().Get("Location"))
	}
}

// List koji već stoji uz dnevnik COP-a otvara se samo za čitanje: stranica
// ne nudi obrasce za uvjete, upis ni potvrdu. List dnevnika usluge i
// dnevnika obrane područja nudi ih kao i dosad. Podaci su izmišljeni.
func TestStariListCOPaSamoZaCitanje(t *testing.T) {
	p := novaCOPProba(t)
	p.mux.HandleFunc("GET /dnevnici/{id}/listovi/{sheet}", p.h.ShowSheet)
	rec := ledger.New(p.baza, "test")
	odrzavanje := repository.NewMaintenanceRepository(p.baza, rec)
	p.h.maintenance = service.NewMaintenanceService(odrzavanje,
		repository.NewWatercourseRepository(p.baza, rec), repository.NewStructureRepository(p.baza, rec))
	p.h.sections = service.NewSectionService(repository.NewSectionRepository(p.baza, rec), nil)
	ctx := context.Background()
	prvo := 1
	rukovoditelj := p.osoba(t, "rrukovoditelj", "Rina Rukovoditeljica", "", models.Duty{Title: "Rukovoditelj područja", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &prvo})
	usluga := p.dnevnik(t, 1, models.JournalKindMaintenanceA02, "A.02", "Primjer d.o.o.", "", nil)
	for _, mw := range []models.MaintainedWater{{AreaID: 1, Program: models.ProgramA02, Name: "Kanal Prvi red"}, {AreaID: 1, Program: models.ProgramA03, Name: "Kanal Treći red"}} {
		if err := odrzavanje.UpsertWater(ctx, &mw); err != nil {
			t.Fatal(err)
		}
	}
	// obrasci javlja nudi li stranica lista potvrdu, upis i uvjete za izmjenu
	var obrasciZa func(*models.User, *models.Journal) (bool, bool, bool)
	obrasci := func(j *models.Journal) (potvrda, upis, uvjeti bool) {
		return obrasciZa(rukovoditelj, j)
	}
	ovlastenik := p.osoba(t, "oovlastenik", "Olga Ovlaštenica", "", models.Duty{Title: "Ovlaštenik A2", Role: models.RoleContractOfficerA2, ScopeType: models.ScopeArea, AreaID: &prvo})
	obrasciZa = func(osoba *models.User, j *models.Journal) (potvrda, upis, uvjeti bool) {
		sh := &models.JournalSheet{JournalID: j.ID, Date: time.Date(2026, 10, 2, 0, 0, 0, 0, models.Zagreb)}
		if err := p.journalRepo.SaveSheet(ctx, sh); err != nil {
			t.Fatal(err)
		}
		w := p.salji(t, osoba, http.MethodGet, "/dnevnici/"+j.ID+"/listovi/"+sh.ID, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("list %s: %d", j.Title, w.Code)
		}
		html := w.Body.String()
		if j == usluga && (!strings.Contains(html, "Kanal Prvi red") || strings.Contains(html, "Kanal Treći red")) {
			t.Error("list usluge A.02 ne nudi samo lokacije svog programa")
		}
		put := "/dnevnici/" + j.ID + "/listovi/" + sh.ID
		uvjeti = !strings.Contains(html, `<fieldset disabled`)
		return strings.Contains(html, put+`/potvrdi"`), strings.Contains(html, put+`/upisi"`), uvjeti
	}
	if potvrda, upis, uvjeti := obrasci(p.cop); potvrda || upis || uvjeti {
		t.Errorf("list dnevnika COP-a nudi potvrdu %v, upis %v, uvjete %v", potvrda, upis, uvjeti)
	}
	// kontrola: list dnevnika usluge i dnevnika obrane područja
	obrana := &models.Journal{AreaID: 1, Kind: models.JournalKindDefense, Title: "Obrana područja", Year: 2026}
	if err := p.journalRepo.SaveJournal(ctx, obrana); err != nil {
		t.Fatal(err)
	}
	for _, j := range []*models.Journal{usluga, obrana} {
		if potvrda, upis, uvjeti := obrasciZa(ovlastenik, j); !potvrda || !upis || !uvjeti {
			t.Errorf("list %s nudi potvrdu %v, upis %v, uvjete %v", j.Title, potvrda, upis, uvjeti)
		}
	}
	// list usluge potvrđuje za nadzor ovlaštenik za praćenje ugovora, a ne
	// rukovoditelj područja; upis i uvjeti mu ostaju (Odluke (30. 10. 2026.), 4)
	if potvrda, upis, uvjeti := obrasci(usluga); potvrda || !upis || !uvjeti {
		t.Errorf("rukovoditelju područja list usluge nudi potvrdu %v, upis %v, uvjete %v", potvrda, upis, uvjeti)
	}
}

// Popis dnevnika COP-a javlja grešku kad se dnevnici ne daju pročitati, a ne
// prazan popis
func TestPopisCOPaUzKvarBaze(t *testing.T) {
	p := novaCOPProba(t)
	operater := p.osoba(t, "ddezurni", "Dora Dežurna", "", models.Duty{Title: "Dežurni operater", Role: models.RoleOperator, ScopeType: models.ScopeSector})
	if w := p.salji(t, operater, http.MethodGet, "/dnevnici/popis?vrsta="+models.JournalKindDefense, nil); w.Code != http.StatusOK {
		t.Fatalf("popis dnevnika COP-a: %d", w.Code)
	}
	if _, err := p.baza.Exec(`ALTER TABLE journals RENAME TO journals_drugdje`); err != nil {
		t.Fatal(err)
	}
	if w := p.salji(t, operater, http.MethodGet, "/dnevnici/popis?vrsta="+models.JournalKindDefense, nil); w.Code != http.StatusInternalServerError {
		t.Errorf("popis dnevnika COP-a uz kvar baze: %d", w.Code)
	}
}
