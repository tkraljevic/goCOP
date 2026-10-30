package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// upisiProba je baza sa sektorima B i D, dnevnikom COP-a svakog od njih i
// dnevnikom usluge na po jednom području
type upisiProba struct {
	s                *JournalService
	repo             *repository.JournalRepository
	copB, copD       *models.Journal
	uslugaB, uslugaD *models.Journal
	oB, oD           models.Opseg
	oUB, oUD         models.Opseg
	pocetak          time.Time
}

func novaUpisiProba(t *testing.T) *upisiProba {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "upisi.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO B', 'COP B'), ('D', 'Sektor D', 'VGO D', 'COP D')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (15, 'B', 'Prvo', 'VGI', 'Prvo'), (30, 'D', 'Drugo', 'VGI', 'Drugo')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	p := &upisiProba{repo: repository.NewJournalRepository(baza, ledger.New(baza, "test"))}
	p.s = NewJournalService(p.repo, nil, nil)
	p.pocetak = time.Date(2026, 10, 1, 0, 0, 0, 0, models.Zagreb)
	podrucja := []models.Area{{ID: 15, SectorID: "B"}, {ID: 30, SectorID: "D"}}
	spremi := func(j *models.Journal) *models.Journal {
		if err := p.repo.SaveJournal(context.Background(), j); err != nil {
			t.Fatal(err)
		}
		return j
	}
	p.copB = spremi(&models.Journal{Kind: models.JournalKindDefense, CentarSektor: "B", Title: "COP B", Year: 2026, StartedAt: &p.pocetak})
	p.copD = spremi(&models.Journal{Kind: models.JournalKindDefense, CentarSektor: "D", Title: "COP D", Year: 2026, StartedAt: &p.pocetak})
	p.uslugaB = spremi(&models.Journal{Kind: models.JournalKindMaintenanceA02, AreaID: 15, Title: "A.02 B", Year: 2026})
	p.uslugaD = spremi(&models.Journal{Kind: models.JournalKindMaintenanceA02, AreaID: 30, Title: "A.02 D", Year: 2026})
	p.oB, p.oD = models.OpsegDnevnika(*p.copB, nil, podrucja), models.OpsegDnevnika(*p.copD, nil, podrucja)
	p.oUB, p.oUD = models.OpsegPodrucja(podrucja[0]), models.OpsegPodrucja(podrucja[1])
	return p
}

// sektorski je osoba s pravom pisanja po cijelom sektoru
func sektorski(sektor string) (*models.User, *models.UserPermissions) {
	return &models.User{ID: uuid.New(), FullName: "Dežurni " + sektor}, &models.UserPermissions{AllowedSectors: map[string]bool{sektor: true}}
}

// zapisCOP upisuje zapis u dnevnik COP-a kao osoba sa sektorskim pravom
func (p *upisiProba) zapisCOP(t *testing.T, j *models.Journal, o models.Opseg, tekst string) *models.JournalEntry {
	t.Helper()
	u, perms := sektorski(j.CentarSektor)
	e := &models.JournalEntry{Kind: models.EntryKindReport, Date: p.pocetak.AddDate(0, 0, 1), Text: tekst}
	if err := p.s.DodajZapisCOP(context.Background(), u, perms, o, j, e); err != nil {
		t.Fatal(err)
	}
	return e
}

// ovlastenik je ovlaštenik za praćenje ugovora s ulogom uloga na području:
// nadzor u dnevniku usluge tog programa
func ovlastenik(podrucje int, uloga models.Role) (*models.User, *models.UserPermissions) {
	sektor := map[int]string{15: "B", 30: "D"}[podrucje]
	p := models.NewUserPermissions(models.User{ID: uuid.New(), FullName: "Ovlaštenik " + sektor, Duties: []models.Duty{
		{Role: uloga, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje, IsActive: true}}})
	return &p.User, p
}

// nalog otvara list u dnevniku usluge i upisuje nalog kao nadzor
func (p *upisiProba) nalog(t *testing.T, j *models.Journal, o models.Opseg) (*models.JournalSheet, *models.JournalEntry) {
	t.Helper()
	ctx := context.Background()
	u, perms := ovlastenik(j.AreaID, models.RoleContractOfficerA2)
	sh, err := p.s.NewSheet(ctx, u, perms, o, j, p.pocetak, "")
	if err != nil {
		t.Fatal(err)
	}
	e := &models.JournalEntry{Kind: models.EntryKindTask, Text: "pokositi nasip"}
	if _, err := p.s.AddEntry(ctx, u, perms, o, j, sh, e); err != nil {
		t.Fatal(err)
	}
	return sh, e
}

func (p *upisiProba) upis(t *testing.T, id string) *models.JournalEntry {
	t.Helper()
	e, err := p.s.GetEntry(context.Background(), id)
	if err != nil || e == nil {
		t.Fatalf("upis %s: %v", id, err)
	}
	return e
}

// Storno i stanje naloga mijenjaju samo upis iz dnevnika u čijem se dosegu
// računaju prava. Upis iz dnevnika drugog sektora s istim brojem upisa nije
// pronađen i ostaje kakav je bio; u svom dnevniku storno i nalog rade.
func TestStornoINalogSamoUSvomDnevniku(t *testing.T) {
	p := novaUpisiProba(t)
	ctx := context.Background()
	tudji := p.zapisCOP(t, p.copD, p.oD, "zapis sektora D")
	svoj := p.zapisCOP(t, p.copB, p.oB, "zapis sektora B")
	u, perms := sektorski("B")

	if err := p.s.VoidEntry(ctx, u, perms, p.oB, p.copB, tudji.ID, "razlog"); err == nil || !strings.Contains(err.Error(), "nije pronađen u ovom dnevniku") {
		t.Errorf("storno upisa iz drugog dnevnika: %v", err)
	}
	if p.upis(t, tudji.ID).Voided {
		t.Error("upis iz drugog dnevnika je storniran")
	}
	if err := p.s.VoidEntry(ctx, u, perms, p.oB, p.copB, svoj.ID, "krivo upisano"); err != nil || !p.upis(t, svoj.ID).Voided {
		t.Errorf("storno u svom dnevniku: %v", err)
	}

	ou, op := ovlastenik(15, models.RoleContractOfficerA2)
	_, nalogD := p.nalog(t, p.uslugaD, p.oUD)
	_, nalogB := p.nalog(t, p.uslugaB, p.oUB)
	if err := p.s.SetTaskStatus(ctx, ou, op, p.oUB, p.uslugaB, nalogD.ID, models.TaskCancelled); err == nil || !strings.Contains(err.Error(), "nije pronađen u ovom dnevniku") {
		t.Errorf("otkazan nalog iz drugog dnevnika: %v", err)
	}
	if p.upis(t, nalogD.ID).Status != models.TaskOpen {
		t.Error("nalog iz drugog dnevnika je promijenjen")
	}
	if err := p.s.VoidEntry(ctx, ou, op, p.oUB, p.uslugaB, nalogD.ID, "razlog"); err == nil {
		t.Error("storniran nalog iz drugog dnevnika")
	}
	if err := p.s.SetTaskStatus(ctx, ou, op, p.oUB, p.uslugaB, nalogB.ID, models.TaskCancelled); err != nil || p.upis(t, nalogB.ID).Status != models.TaskCancelled {
		t.Errorf("otkazan nalog u svom dnevniku: %v", err)
	}
	if err := p.s.SetTaskStatus(ctx, ou, op, p.oUB, p.uslugaB, svoj.ID, models.TaskDone); err == nil {
		t.Error("stanje naloga na zapisu iz drugog dnevnika")
	}
}

// Ovjeren dnevnik COP-a ne prima nove zapise, storno ni ispravke prijepisa.
// Isti dnevnik prije ovjere prima sve to.
func TestOvjerenDnevnikCOPaNePrimaZapise(t *testing.T) {
	p := novaUpisiProba(t)
	ctx := context.Background()
	zapis := p.zapisCOP(t, p.copB, p.oB, "zapis prije ovjere")
	u, perms := sektorski("B")
	kraj := p.pocetak.AddDate(0, 0, 5)
	sad := time.Now()
	ovjeren := *p.copB
	ovjeren.EndedAt, ovjeren.ZakljucenoAt = &kraj, &sad

	novi := models.JournalEntry{Kind: models.EntryKindReport, Date: p.pocetak.AddDate(0, 0, 2), Text: "naknadno"}
	if err := p.s.DodajZapisCOP(ctx, u, perms, p.oB, &ovjeren, &novi); err == nil || !strings.Contains(err.Error(), "ovjereni dnevnik") {
		t.Errorf("ovjeren dnevnik primio zapis: %v", err)
	}
	if err := p.s.VoidEntry(ctx, u, perms, p.oB, &ovjeren, zapis.ID, "razlog"); err == nil || !strings.Contains(err.Error(), "ovjereni dnevnik") {
		t.Errorf("storno u ovjerenom dnevniku: %v", err)
	}
	if p.upis(t, zapis.ID).Voided {
		t.Error("zapis ovjerenog dnevnika je storniran")
	}
	prijepis := ovjeren
	prijepis.Reconstruction = true
	ispravak := models.JournalEntry{Kind: models.EntryKindReport, Text: "drukčije"}
	if err := p.s.IspraviPrijepis(ctx, u, perms, p.oB, &prijepis, zapis.ID, ispravak); err == nil || !strings.Contains(err.Error(), "ovjereni dnevnik") {
		t.Errorf("ispravak u ovjerenom prijepisu: %v", err)
	}

	// kontrola: neovjeren prijepis se ispravlja, a neovjeren dnevnik prima zapis
	prijepis.ZakljucenoAt = nil
	if err := p.s.IspraviPrijepis(ctx, u, perms, p.oB, &prijepis, zapis.ID, ispravak); err != nil || p.upis(t, zapis.ID).Text != "drukčije" {
		t.Errorf("ispravak u neovjerenom prijepisu: %v", err)
	}
	if err := p.s.DodajZapisCOP(ctx, u, perms, p.oB, p.copB, &novi); err != nil {
		t.Errorf("neovjeren dnevnik ne prima zapis: %v", err)
	}
	tudjePodrucje := 30
	izvan := models.JournalEntry{Kind: models.EntryKindReport, Date: p.pocetak.AddDate(0, 0, 2), Text: "dojava", Podrucje: &tudjePodrucje}
	if err := p.s.DodajZapisCOP(ctx, u, perms, p.oB, p.copB, &izvan); err == nil {
		t.Error("zapis za područje drugog sektora")
	}
}

// Dnevnik COP-a nema listova: ne otvara se list, na list se ne upisuje, a
// uvjeti, vremenske prilike i potvrda lista se ne mijenjaju. Dnevnik usluge
// listove ima kao i dosad.
func TestDnevnikCOPaNemaListova(t *testing.T) {
	p := novaUpisiProba(t)
	ctx := context.Background()
	u, perms := sektorski("B")
	nema := "nema listova"

	if _, err := p.s.NewSheet(ctx, u, perms, p.oB, p.copB, p.pocetak.AddDate(-1, 0, 0), ""); err == nil || !strings.Contains(err.Error(), nema) {
		t.Errorf("list u dnevniku COP-a: %v", err)
	}
	// list koji bi već stajao uz dnevnik COP-a
	sh := &models.JournalSheet{ID: "list", JournalID: p.copB.ID, Date: p.pocetak}
	e := models.JournalEntry{Kind: models.EntryKindReport, Text: "dojava"}
	if _, err := p.s.AddEntry(ctx, u, perms, p.oB, p.copB, sh, &e); err == nil || !strings.Contains(err.Error(), nema) {
		t.Errorf("upis na list dnevnika COP-a: %v", err)
	}
	if err := p.s.UpdateSheet(ctx, perms, p.oB, p.copB, sh); err == nil || !strings.Contains(err.Error(), nema) {
		t.Errorf("uvjeti na listu dnevnika COP-a: %v", err)
	}
	if err := p.s.RefreshWeather(ctx, perms, p.oB, p.copB, sh); err == nil || !strings.Contains(err.Error(), nema) {
		t.Errorf("vrijeme na listu dnevnika COP-a: %v", err)
	}
	if err := p.s.ConfirmSheet(ctx, u, perms, p.oB, p.copB, sh.ID); err == nil || !strings.Contains(err.Error(), nema) {
		t.Errorf("potvrda lista dnevnika COP-a: %v", err)
	}
	zapisi, err := p.s.EntriesForJournal(ctx, p.copB.ID)
	if err != nil || len(zapisi) != 0 {
		t.Errorf("u dnevniku COP-a ima zapisa: %d, %v", len(zapisi), err)
	}

	// kontrola: dnevnik usluge
	list, nalog := p.nalog(t, p.uslugaB, p.oUB)
	if nalog.SheetID != list.ID {
		t.Errorf("nalog nije na listu: %+v", nalog)
	}
	upd := *list
	upd.Conditions = "suho"
	if err := p.s.UpdateSheet(ctx, perms, p.oUB, p.uslugaB, &upd); err != nil {
		t.Errorf("uvjeti na listu usluge: %v", err)
	}
	if err := p.s.RefreshWeather(ctx, perms, p.oUB, p.uslugaB, list); err == nil || !strings.Contains(err.Error(), "koordinata") {
		t.Errorf("vrijeme na listu usluge bez koordinata: %v", err)
	}
	ou, op := ovlastenik(15, models.RoleContractOfficerA2)
	if err := p.s.ConfirmSheet(ctx, ou, op, p.oUB, p.uslugaB, list.ID); err != nil {
		t.Errorf("potvrda lista usluge: %v", err)
	}
}

// Ovjeren dnevnik ne mijenja stanje naloga i ne prima obavijest o aktu.
// Neovjeren dnevnik i jedno i drugo prima kao i dosad.
func TestOvjerenDnevnikNeMijenjaNalogNiObavijest(t *testing.T) {
	p := novaUpisiProba(t)
	ctx := context.Background()
	u, perms := ovlastenik(15, models.RoleContractOfficerA2)
	_, nalog := p.nalog(t, p.uslugaB, p.oUB)
	sad := time.Now()
	ovjerenaUsluga := *p.uslugaB
	ovjerenaUsluga.ZakljucenoAt = &sad
	if err := p.s.SetTaskStatus(ctx, u, perms, p.oUB, &ovjerenaUsluga, nalog.ID, models.TaskDone); err == nil || !strings.Contains(err.Error(), "ovjereni dnevnik") {
		t.Errorf("stanje naloga u ovjerenom dnevniku: %v", err)
	}
	if p.upis(t, nalog.ID).Status != models.TaskOpen {
		t.Error("nalog ovjerenog dnevnika je promijenjen")
	}

	ovjerenCOP := *p.copB
	ovjerenCOP.ZakljucenoAt = &sad
	obavijest := func() *models.JournalEntry {
		return &models.JournalEntry{Kind: models.EntryKindNotice, Date: p.pocetak.AddDate(0, 0, 1), Text: "akt o stupnju obrane"}
	}
	if err := p.s.ZapisIzAkta(ctx, &ovjerenCOP, obavijest()); err == nil || !strings.Contains(err.Error(), "ovjereni dnevnik") {
		t.Errorf("obavijest o aktu u ovjerenom dnevniku: %v", err)
	}
	if zapisi, err := p.s.EntriesForJournal(ctx, p.copB.ID); err != nil || len(zapisi) != 0 {
		t.Errorf("ovjeren dnevnik ima %d zapisa", len(zapisi))
	}

	// kontrola: neovjeren dnevnik
	if err := p.s.SetTaskStatus(ctx, u, perms, p.oUB, p.uslugaB, nalog.ID, models.TaskDone); err != nil || p.upis(t, nalog.ID).Status != models.TaskDone {
		t.Errorf("stanje naloga u neovjerenom dnevniku: %v", err)
	}
	if err := p.s.ZapisIzAkta(ctx, p.copB, obavijest()); err != nil {
		t.Errorf("obavijest o aktu u neovjerenom dnevniku: %v", err)
	}
}
