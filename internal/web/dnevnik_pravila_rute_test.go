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

// Dnevnik usluge kroz rute (Odluke (30. 10. 2026.), 4 i 6): list za nadzor
// potvrđuje ovlaštenik za praćenje ugovora, a ne dežurni operater;
// naslovnicu mijenja ovlaštenik, a dežurni ne; dnevnik briše uprava
// sektora, a ovlaštenik ne, i gumb za brisanje vidi samo ona. Na listu koji
// su potvrdili izvođač i nadzor upis se ne stornira i stranica ne nudi
// storno. Podaci su izmišljeni.
func TestDnevnikUslugePravaKrozRute(t *testing.T) {
	p := novaCOPProba(t)
	h := p.h
	p.mux.HandleFunc("POST /dnevnici/{id}", h.HandleSaveJournal)
	p.mux.HandleFunc("POST /dnevnici/{id}/obrisi", h.HandleObrisiDnevnik)
	p.mux.HandleFunc("GET /dnevnici/{id}/listovi/{sheet}", h.ShowSheet)
	p.mux.HandleFunc("POST /dnevnici/{id}/listovi/{sheet}/potvrdi", h.HandleConfirmSheet)
	p.mux.HandleFunc("POST /dnevnici/{id}/upisi/{entry}/storno", h.HandleVoidEntry)
	rec := ledger.New(p.baza, "test")
	h.maintenance = service.NewMaintenanceService(repository.NewMaintenanceRepository(p.baza, rec),
		repository.NewWatercourseRepository(p.baza, rec), repository.NewStructureRepository(p.baza, rec))
	h.sections = service.NewSectionService(repository.NewSectionRepository(p.baza, rec), nil)
	ctx := context.Background()
	prvo := 1
	operater := p.osoba(t, "ddezurni", "Dora Dežurna", "", models.Duty{Title: "Dežurni operater", Role: models.RoleOperator, ScopeType: models.ScopeSector})
	ovlastenik := p.osoba(t, "oovlastenik", "Olga Ovlaštenica", "", models.Duty{Title: "Ovlaštenik A2", Role: models.RoleContractOfficerA2, ScopeType: models.ScopeArea, AreaID: &prvo})
	uprava := p.osoba(t, "ssektor", "Saša Sektorski", "", models.Duty{Title: "Rukovoditelj sektora", Role: models.RoleSectorLeader, ScopeType: models.ScopeSector})
	poslovodja := p.poslovodja(t, "mmarkic", "Marko Markić", "Primjer d.o.o.")
	j := p.dnevnik(t, 1, models.JournalKindMaintenanceA02, "A.02", "Primjer d.o.o.", "", nil)
	list := &models.JournalSheet{JournalID: j.ID, Date: time.Date(2026, 10, 2, 0, 0, 0, 0, models.Zagreb)}
	if err := p.journalRepo.SaveSheet(ctx, list); err != nil {
		t.Fatal(err)
	}
	upis := &models.JournalEntry{JournalID: j.ID, SheetID: list.ID, Date: list.Date, Kind: models.EntryKindWork, Side: models.EntrySideContractor, Text: "košnja nasipa", UserID: poslovodja.ID.String()}
	if err := p.journalRepo.SaveEntry(ctx, upis); err != nil {
		t.Fatal(err)
	}
	listPut := "/dnevnici/" + j.ID + "/listovi/" + list.ID

	naslovnica := url.Values{"kind": {models.JournalKindMaintenanceA02}, "title": {"A.02"}, "contractor": {"Primjer d.o.o."}, "year": {"2026"}}
	if got := ishod(p.salji(t, operater, http.MethodPost, "/dnevnici/"+j.ID, naslovnica)); got != "error" {
		t.Errorf("dežurni mijenja naslovnicu: %s", got)
	}
	if got := ishod(p.salji(t, ovlastenik, http.MethodPost, "/dnevnici/"+j.ID, naslovnica)); got != "success" {
		t.Errorf("ovlaštenik ne mijenja naslovnicu: %s", got)
	}
	// vrsta dnevnika ne mijenja se: ovlaštenik A3 dnevnik A.02 ne pretvara u A.03
	ovlA3 := p.osoba(t, "aatri", "Ana Trojka", "", models.Duty{Title: "Ovlaštenik A3", Role: models.RoleContractOfficerA3, ScopeType: models.ScopeArea, AreaID: &prvo})
	druga := url.Values{"kind": {models.JournalKindMaintenanceA03}, "title": {"A.02"}, "contractor": {"Primjer d.o.o."}, "year": {"2026"}}
	if got := ishod(p.salji(t, ovlA3, http.MethodPost, "/dnevnici/"+j.ID, druga)); got != "error" {
		t.Errorf("ovlaštenik A3 mijenja vrstu dnevnika: %s", got)
	}
	if cur, err := p.journalRepo.GetJournal(ctx, j.ID); err != nil || cur == nil || cur.Kind != models.JournalKindMaintenanceA02 {
		t.Fatalf("vrsta dnevnika je promijenjena: %+v", cur)
	}
	if got := ishod(p.salji(t, ovlA3, http.MethodPost, "/dnevnici/"+j.ID, naslovnica)); got != "success" {
		t.Errorf("ovlaštenik A3 ne sprema naslovnicu iste vrste: %s", got)
	}

	if got := ishod(p.salji(t, operater, http.MethodPost, listPut+"/potvrdi", url.Values{})); got != "error" {
		t.Errorf("dežurni potvrđuje list za nadzor: %s", got)
	}
	if strings.Contains(p.salji(t, operater, http.MethodGet, listPut, nil).Body.String(), listPut+`/potvrdi"`) {
		t.Error("dežurnom list nudi potvrdu")
	}
	for _, tko := range []*models.User{ovlastenik, poslovodja} {
		if got := ishod(p.salji(t, tko, http.MethodPost, listPut+"/potvrdi", url.Values{})); got != "success" {
			t.Errorf("%s ne potvrđuje list: %s", tko.FullName, got)
		}
	}
	zamjenik := p.osoba(t, "zzamjenik", "Zora Zamjenica", "", models.Duty{Title: "Zamjenik ovlaštenika A2", Role: models.RoleContractDeputyA2, ScopeType: models.ScopeArea, AreaID: &prvo})
	if got := ishod(p.salji(t, zamjenik, http.MethodPost, listPut+"/potvrdi", url.Values{})); got != "error" {
		t.Errorf("drugi ovlaštenik ponovno potvrđuje list: %s", got)
	}
	if sh, err := p.journalRepo.GetSheet(ctx, list.ID); err != nil || sh == nil || !sh.IsConfirmed() || sh.SupervisorConfirmedBy != ovlastenik.FullName {
		t.Fatalf("list nije potvrđen s obje strane: %+v", sh)
	}

	storno := url.Values{"sheet": {list.ID}, "reason": {"krivi sat"}}
	w := p.salji(t, ovlastenik, http.MethodPost, "/dnevnici/"+j.ID+"/upisi/"+upis.ID+"/storno", storno)
	if got := ishod(w); got != "error" || !strings.Contains(w.Header().Get("Location"), url.QueryEscape("novi upis s napomenom")) {
		t.Errorf("storno na potvrđenom listu: %s, %s", got, w.Header().Get("Location"))
	}
	if e, err := p.journalRepo.GetEntry(ctx, upis.ID); err != nil || e == nil || e.Voided {
		t.Errorf("upis na potvrđenom listu je storniran: %+v", e)
	}
	stranica := p.salji(t, ovlastenik, http.MethodGet, listPut, nil).Body.String()
	if strings.Contains(stranica, "/storno") || !strings.Contains(stranica, "novi upis s napomenom") {
		t.Error("potvrđen list nudi storno ili ne kaže kako se ispravlja")
	}

	obrisi := "/dnevnici/" + j.ID + "/obrisi"
	if got := ishod(p.salji(t, ovlastenik, http.MethodPost, obrisi, url.Values{})); got != "error" {
		t.Errorf("ovlaštenik briše dnevnik usluge: %s", got)
	}
	if strings.Contains(p.salji(t, ovlastenik, http.MethodGet, "/dnevnici/"+j.ID, nil).Body.String(), obrisi) {
		t.Error("ovlašteniku dnevnik nudi brisanje")
	}
	if !strings.Contains(p.salji(t, uprava, http.MethodGet, "/dnevnici/"+j.ID, nil).Body.String(), obrisi) {
		t.Error("upravi sektora dnevnik ne nudi brisanje")
	}
	if got := ishod(p.salji(t, uprava, http.MethodPost, obrisi, url.Values{})); got != "success" {
		t.Errorf("uprava sektora ne briše dnevnik usluge: %s", got)
	}
}

// Plan dežurstava ovjerenog dnevnika COP-a kroz rute (Odluke (30. 10. 2026.),
// 5): dežurni ne upisuje ni ne miče svoje dežurstvo i stranica mu ne nudi
// obrazac; voditelj centra bez razloga ne potvrđuje, a uz razlog potvrđuje,
// i razlog se vidi u planu. Ovjera ne prolazi dok plan ima nepotvrđeno
// dežurstvo. Podaci su izmišljeni.
func TestPlanOvjerenogDnevnikaKrozRute(t *testing.T) {
	p := novaCOPProba(t)
	h := p.h
	p.mux.HandleFunc("POST /dnevnici/{id}/dezurstva/{dez}/makni", h.HandleMakniDezurstvo)
	p.mux.HandleFunc("POST /dnevnici/{id}/dezurstva/{dez}/potvrdi", h.HandlePotvrdiDezurstvo)
	p.mux.HandleFunc("POST /dnevnici/{id}/dezurstva/{dez}/opis", h.HandleIspraviOpisDezurstva)
	ctx := context.Background()
	operater := p.osoba(t, "ddezurni", "Dora Dežurna", "", models.Duty{Title: "Dežurni operater", Role: models.RoleOperator, ScopeType: models.ScopeSector})
	voditelj := p.osoba(t, "vvoditelj", "Vid Voditelj", "", models.Duty{Title: "Voditelj COP-a", Role: models.RoleCopLeader, ScopeType: models.ScopeSector})
	plan := "/dnevnici/" + p.cop.ID + "/dezurstva"
	svoje := func(dan string) url.Values {
		return url.Values{"user_id": {operater.ID.String()}, "opis": {"Dežurstvo u COP-u"}, "od_date": {dan}, "od_time": {"07:00"}, "do_time": {"19:00"}}
	}
	if got := ishod(p.salji(t, operater, http.MethodPost, plan, svoje("2026-10-02"))); got != "success" {
		t.Fatalf("dežurni ne upisuje svoje dežurstvo: %s", got)
	}
	dez, err := p.journalRepo.ListDezurstva(ctx, p.cop.ID)
	if err != nil || len(dez) != 1 {
		t.Fatalf("u planu je %d dežurstava: %v", len(dez), err)
	}
	cijeli, err := p.users.GetUserByID(voditelj.ID)
	if err != nil || cijeli == nil {
		t.Fatalf("voditelj: %v", err)
	}
	// priprema mijenja samo kopiju dnevnika koju dobije
	zavrsen := func() *models.Journal {
		k := *p.cop
		kraj := time.Date(2026, 10, 5, 0, 0, 0, 0, models.Zagreb)
		k.EndedAt = &kraj
		return &k
	}
	if err := h.pripremiOvjeruCOP(ctx, cijeli, models.NewUserPermissions(*cijeli), zavrsen(), time.Now()); err == nil || !strings.Contains(err.Error(), "nepotvrđenih") {
		t.Errorf("ovjera s nepotvrđenim dežurstvom: %v", err)
	}
	// tko nije voditelj COP-a čuje samo da ne ovjerava, a ne stanje plana
	dezurni, err := p.users.GetUserByID(operater.ID)
	if err != nil || dezurni == nil {
		t.Fatalf("dežurni: %v", err)
	}
	if err := h.pripremiOvjeruCOP(ctx, dezurni, models.NewUserPermissions(*dezurni), zavrsen(), time.Now()); err == nil || !strings.Contains(err.Error(), "ovjerava voditelj COP-a") {
		t.Errorf("ovjera dežurnog: %v", err)
	}
	// kontrola: dnevnik s praznim planom i upisanim krajem priprema se za ovjeru
	pocetak, kraj := time.Date(2026, 9, 1, 0, 0, 0, 0, models.Zagreb), time.Date(2026, 9, 5, 0, 0, 0, 0, models.Zagreb)
	prazan := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "P", Title: "Dnevnik COP-a, rujan", Year: 2026, StartedAt: &pocetak, EndedAt: &kraj}
	if err := p.journalRepo.SaveJournal(ctx, prazan); err != nil {
		t.Fatal(err)
	}
	if err := h.pripremiOvjeruCOP(ctx, cijeli, models.NewUserPermissions(*cijeli), prazan, time.Now()); err != nil || !prazan.Ovjeren() {
		t.Errorf("ovjera dnevnika s potvrđenim planom: %v", err)
	}

	if _, err := p.baza.Exec(`UPDATE journals SET ended_at = ?, zakljucio = 'Vid Voditelj', zakljuceno_at = ? WHERE id = ?`,
		time.Date(2026, 10, 5, 0, 0, 0, 0, models.Zagreb).UTC(), time.Now().UTC(), p.cop.ID); err != nil {
		t.Fatal(err)
	}
	if got := ishod(p.salji(t, operater, http.MethodPost, plan, svoje("2026-10-03"))); got != "error" {
		t.Errorf("dežurni upisuje u zaključan plan: %s", got)
	}
	if got := ishod(p.salji(t, operater, http.MethodPost, plan+"/"+dez[0].ID+"/makni", url.Values{"razlog": {"krivo"}})); got != "error" {
		t.Errorf("dežurni miče iz zaključanog plana: %s", got)
	}
	if strings.Contains(p.salji(t, operater, http.MethodGet, plan, nil).Body.String(), `id="novo-dezurstvo"`) {
		t.Error("zaključan plan dežurnom nudi obrazac")
	}
	if got := ishod(p.salji(t, voditelj, http.MethodPost, plan+"/"+dez[0].ID+"/potvrdi", url.Values{})); got != "error" {
		t.Errorf("voditelj potvrđuje bez razloga: %s", got)
	}

	// kontrola: voditelj uz razlog
	if got := ishod(p.salji(t, voditelj, http.MethodPost, plan+"/"+dez[0].ID+"/potvrdi", url.Values{"razlog": {"provjereno s putnim nalogom"}})); got != "success" {
		t.Errorf("voditelj ne potvrđuje uz razlog: %s", got)
	}
	stranica := p.salji(t, voditelj, http.MethodGet, plan, nil).Body.String()
	if !strings.Contains(stranica, "provjereno s putnim nalogom") || !strings.Contains(stranica, `name="razlog"`) || !strings.Contains(stranica, "plan dežurstava zaključan") {
		t.Error("plan ne pokazuje razlog ispravka ili ne traži razlog")
	}
	w := p.salji(t, voditelj, http.MethodPost, plan+"/"+dez[0].ID+"/opis", url.Values{"opis": {"Obilazak i pregled obrambenih objekata"}, "razlog": {"bila na terenu"}})
	if got := ishod(w); got != "success" || !strings.Contains(w.Header().Get("Location"), url.QueryEscape("razlog je zapisan")) {
		t.Errorf("voditelj ne ispravlja opis uz razlog: %s, %s", got, w.Header().Get("Location"))
	}
	if got := ishod(p.salji(t, voditelj, http.MethodPost, plan+"/"+dez[0].ID+"/makni", url.Values{"razlog": {"upisano dvaput"}})); got != "success" {
		t.Errorf("voditelj ne miče uz razlog: %s", got)
	}
}
