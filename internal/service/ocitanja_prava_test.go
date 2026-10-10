package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Prava na očitanja letve postaje po sektoru koji je prati (Odluke (8. 10.
// 2026.), pitanja 8 i 9). Okolina je ona iz ocitanja_servis_test.go
// (sektor P s područjima 1 i 2, sektor Q s područjem 3), uz dionicu Q.3.1 s
// letvom Drugdje i tri letve bez dionica: Mohács probni (prati ga sektor P),
// Barcs probni (prati ga područje 3, dakle sektor Q) i Letenye probna, koju
// prati sektor Q, a stoji na dionici P.1.1.

type letvePracenja struct {
	*okolinaCitanja
	drugdje, mohacs, barcs, letenye *models.Station
}

func novaOkolinaPracenja(t *testing.T) *letvePracenja {
	t.Helper()
	o := &letvePracenja{okolinaCitanja: novaOkolinaCitanja(t)}
	if _, err := o.baza.Exec(`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at)
		VALUES ('Q.3.1', 3, 'Q', 'rijeka Drugdje', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	stations := repository.NewStationRepository(o.baza, ledger.New(o.baza, "test"))
	letva := func(naziv string, sektori []string, podrucja []int, dionice ...string) *models.Station {
		st := &models.Station{ID: uuid.New(), Code: strings.ToLower(strings.ReplaceAll(naziv, " ", "-")), Name: naziv,
			PrateSektori: sektori, PratePodrucja: podrucja}
		if err := stations.CreateStation(context.Background(), st); err != nil {
			t.Fatal(err)
		}
		for _, d := range dionice {
			if _, err := o.baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, ?, ?, ?)`,
				uuid.NewString(), d, st.ID.String(), time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		got, err := stations.GetStationByID(context.Background(), st.ID)
		if err != nil || got == nil {
			t.Fatalf("letva %s: %v", naziv, err)
		}
		return got
	}
	o.drugdje = letva("Drugdje", nil, nil, "Q.3.1")
	o.mohacs = letva("Mohács probni", []string{"P"}, nil)
	o.barcs = letva("Barcs probni", nil, []int{3})
	o.letenye = letva("Letenye probna", []string{"Q"}, nil, "P.1.1")
	return o
}

// duznost je valjana dužnost uloge s dosegom; područje 0 i prazne dionice
// znače da ih dužnost nema
func duznost(uloga models.Role, doseg models.ScopeType, sektor string, podrucje int, dionice string) models.Duty {
	d := models.Duty{ID: uuid.New(), Role: uloga, ScopeType: doseg, SectionCodes: dionice, IsActive: true}
	if sektor != "" {
		d.SectorID = &sektor
	}
	if podrucje > 0 {
		d.AreaID = &podrucje
	}
	return d
}

// ovlastiOsobe su ovlasti Pere Perića s tim dužnostima
func ovlastiOsobe(duznosti ...models.Duty) *models.UserPermissions {
	return models.NewUserPermissions(models.User{ID: uuid.New(), FullName: "Pero Perić", IsActive: true, Duties: duznosti})
}

func TestUpisOcitanjaPoSektoruLetve(t *testing.T) {
	o := novaOkolinaPracenja(t)
	prosla := time.Now().Add(-time.Hour)
	istekla := duznost(models.RoleOperator, models.ScopeSector, "P", 0, "")
	istekla.ExpiresAt = &prosla
	neaktivna := duznost(models.RoleOperator, models.ScopeSector, "P", 0, "")
	neaktivna.IsActive = false

	letve := []*models.Station{o.primjerovo, o.probno, o.dvije, o.drugdje, o.mohacs, o.barcs, o.uzvodna, o.letenye}
	for _, s := range []struct {
		ime   string
		perms *models.UserPermissions
		smije string // letve na koje upisuje, redom iz popisa letve
	}{
		// dužnost na dionici P.1.1 je dužnost u sektoru P: upisuje na svaku
		// letvu sektora P, i na stranu letvu koju sektor P prati
		{"vodočuvar dionice P.1.1", ovlastiOsobe(duznost(models.RoleWaterGuard, models.ScopeSection, "P", 1, "P.1.1")),
			"Primjerovo,Probno,Granica,Mohács probni,Uzvodna,Letenye probna"},
		{"Dežurni operater u COP-u sektora P", ovlastiOsobe(duznost(models.RoleOperator, models.ScopeSector, "P", 0, "")),
			"Primjerovo,Probno,Granica,Mohács probni,Uzvodna,Letenye probna"},
		// područje 3 donosi sektor Q; Letenye prati Q, a stoji na dionici sektora P
		{"Dežurni operater u COP-u sektora Q", ovlastiOsobe(duznost(models.RoleOperator, models.ScopeSector, "Q", 0, "")),
			"Drugdje,Barcs probni,Uzvodna,Letenye probna"},
		{"uprava sektora Q", ovlastiOsobe(duznost(models.RoleSectorLeader, models.ScopeSector, "Q", 0, "")),
			"Drugdje,Barcs probni,Uzvodna,Letenye probna"},
		{"uprava područja 2", ovlastiOsobe(duznost(models.RoleAreaLeader, models.ScopeArea, "P", 2, "")),
			"Primjerovo,Probno,Granica,Mohács probni,Uzvodna,Letenye probna"},
		// Gost i Preglednik ne upisuju ni u svom sektoru, ni na letvu bez sektora
		{"Gost", ovlastiOsobe(duznost(models.RoleGuest, models.ScopeSector, "P", 0, "")), ""},
		{"Preglednik", ovlastiOsobe(duznost(models.RoleViewer, models.ScopeSector, "P", 0, "")), ""},
		// dužnost mora vrijediti
		{"istekla dužnost", ovlastiOsobe(istekla), ""},
		{"isključena dužnost", ovlastiOsobe(neaktivna), ""},
		{"razina 1", o.adminOvl, "Primjerovo,Probno,Granica,Drugdje,Mohács probni,Barcs probni,Uzvodna,Letenye probna"},
		{"bez ovlasti", nil, ""},
	} {
		var smije []string
		for _, st := range letve {
			if o.rs.CanRecordStation(s.perms, st) {
				smije = append(smije, st.Name)
			}
		}
		// poredak iz popisa letve: Primjerovo, Probno, Granica, Drugdje, …
		if got := strings.Join(smije, ","); got != s.smije {
			t.Errorf("%s: upisuje na %q, očekivano %q", s.ime, got, s.smije)
		}
	}
	if o.rs.CanRecordStation(o.adminOvl, nil) {
		t.Error("letve nema")
	}
	// dosadašnje pravilo ostaje uz novo: dionica dužnosti koje nema u
	// registru i pravo pisanja u području bez upisanog sektora dužnosti
	nepostojeca := &models.Station{Name: "Bez dionice", SectionCodes: []string{"P.9.9"}}
	if !o.rs.CanRecordStation(&models.UserPermissions{AllowedSections: map[string]bool{"P.9.9": true}}, nepostojeca) ||
		!o.rs.CanRecordStation(&models.UserPermissions{AllowedAreas: map[int]bool{2: true}}, o.probno) ||
		o.rs.CanRecordStation(&models.UserPermissions{AllowedAreas: map[int]bool{2: true}}, o.primjerovo) {
		t.Error("dosadašnje pravilo upisa po dionicama")
	}

	// isto vrijedi za upis kroz servis: operater sektora P upisuje na
	// Mohács, Gost ne
	ctx := context.Background()
	rd := &models.Reading{StationID: o.mohacs.ID.String(), MeasuredAt: prosla, LevelCm: cmP(420)}
	if err := o.rs.Create(ctx, ovlastiOsobe(duznost(models.RoleOperator, models.ScopeSector, "P", 0, "")), rd); err != nil {
		t.Errorf("operater na strani letvi svog sektora: %v", err)
	}
	gost := ovlastiOsobe(duznost(models.RoleGuest, models.ScopeAll, "", 0, ""))
	if err := o.rs.Create(ctx, gost, &models.Reading{StationID: o.uzvodna.ID.String(), MeasuredAt: prosla, LevelCm: cmP(1)}); err == nil {
		t.Error("Gost ne upisuje ni na letvu bez sektora")
	}
}

// Područje iz praćenja daje sektor tek iz registra područja; bez njega
// (servis bez popisa područja) letvu prati samo upisani sektor
func TestPodrucjePracenjaBezRegistra(t *testing.T) {
	o := novaOkolinaPracenja(t)
	rs := NewReadingService(o.readings, o.rs.stationRepo, o.rs.structureRepo, o.rs.sectionService, nil)
	operaterQ := ovlastiOsobe(duznost(models.RoleOperator, models.ScopeSector, "Q", 0, ""))
	if rs.CanRecordStation(operaterQ, o.barcs) || !o.rs.CanRecordStation(operaterQ, o.barcs) {
		t.Error("sektor područja iz praćenja dolazi iz registra područja")
	}
	// letva s praćenjem nije letva bez sektora ni kad se sektor ne nađe
	if rs.CanRecordStation(ovlastiOsobe(duznost(models.RoleOperator, models.ScopeSector, "P", 0, "")), o.barcs) {
		t.Error("letva s upisanim praćenjem ne prima očitanje od svakoga")
	}
}

func TestOdgovornostZaLetvu(t *testing.T) {
	o := novaOkolinaPracenja(t)
	letve := []*models.Station{o.primjerovo, o.probno, o.dvije, o.drugdje, o.mohacs, o.barcs, o.uzvodna, o.letenye}
	for _, s := range []struct {
		ime      string
		perms    *models.UserPermissions
		odgovara string
	}{
		{"rukovoditelj dionice P.1.1", ovlastiOsobe(duznost(models.RoleSectionLeader, models.ScopeSection, "P", 1, "P.1.1")), "Primjerovo,Letenye probna"},
		// vodočuvar bez popisa dionica čuva cijelo područje 1
		{"vodočuvar cijelog područja 1", ovlastiOsobe(duznost(models.RoleWaterGuard, models.ScopeSection, "P", 1, "")), "Primjerovo,Granica,Letenye probna"},
		{"vodočuvar dionice P.1.2", ovlastiOsobe(duznost(models.RoleWaterGuard, models.ScopeSection, "P", 1, "P.1.2")), "Granica"},
		{"uprava područja 2", ovlastiOsobe(duznost(models.RoleAreaLeader, models.ScopeArea, "P", 2, "")), "Probno,Granica"},
		// letvu bez dionica: uprava sektora koji je prati
		{"uprava sektora P", ovlastiOsobe(duznost(models.RoleSectorLeader, models.ScopeSector, "P", 0, "")),
			"Primjerovo,Probno,Granica,Mohács probni,Letenye probna"},
		// sektor Q prati Letenye, ali Letenye ima dionicu sektora P: za nju
		// odgovara tko i za dionicu
		{"uprava sektora Q", ovlastiOsobe(duznost(models.RoleSectorLeader, models.ScopeSector, "Q", 0, "")), "Drugdje,Barcs probni"},
		// uprava područja iz praćenja ne odgovara za letvu bez dionica
		{"uprava područja 3", ovlastiOsobe(duznost(models.RoleAreaLeader, models.ScopeArea, "Q", 3, "")), "Drugdje"},
		// operater upisuje, ali ne odgovara
		{"Dežurni operater u COP-u sektora P", ovlastiOsobe(duznost(models.RoleOperator, models.ScopeSector, "P", 0, "")), ""},
		// letvu bez sektora (Uzvodna) samo razina 1
		{"razina 1", o.adminOvl, "Primjerovo,Probno,Granica,Drugdje,Mohács probni,Barcs probni,Uzvodna,Letenye probna"},
		{"bez ovlasti", nil, ""},
	} {
		var odgovara []string
		for _, st := range letve {
			if o.rs.PravaNaLetvu(s.perms, st).MozeObrisati() {
				odgovara = append(odgovara, st.Name)
			}
		}
		if got := strings.Join(odgovara, ","); got != s.odgovara {
			t.Errorf("%s: odgovara za %q, očekivano %q", s.ime, got, s.odgovara)
		}
	}
	// dionica letve koje nema u registru ne daje odgovornost ni upravi sektora
	nepostojeca := &models.Station{Name: "Bez dionice", SectionCodes: []string{"P.9.9"}}
	if o.rs.PravaNaLetvu(ovlastiOsobe(duznost(models.RoleSectorLeader, models.ScopeSector, "P", 0, "")), nepostojeca).Odgovara {
		t.Error("nepostojeća dionica")
	}
}

func TestIspravakVlastitogOcitanja(t *testing.T) {
	sad := time.Date(2026, 10, 9, 12, 0, 0, 0, models.Zagreb)
	ja := uuid.New().String()
	upisano := func(prije time.Duration) *models.Reading {
		return &models.Reading{UserID: ja, CreatedAt: sad.Add(-prije)}
	}
	for _, s := range []struct {
		ime       string
		prava     PravaLetve
		rd        *models.Reading
		ispravlja bool
	}{
		{"autor s dužnošću u sektoru, staro očitanje", PravaLetve{Upisuje: true, osoba: ja}, upisano(30 * 24 * time.Hour), true},
		{"autor bez dužnosti, unutar 72 h", PravaLetve{osoba: ja}, upisano(71 * time.Hour), true},
		{"autor bez dužnosti, točno 72 h", PravaLetve{osoba: ja}, upisano(72 * time.Hour), false},
		{"autor bez dužnosti, nakon 72 h", PravaLetve{osoba: ja}, upisano(100 * time.Hour), false},
		{"tuđe očitanje, osoba upisuje na letvu", PravaLetve{Upisuje: true, osoba: uuid.NewString()}, upisano(time.Hour), false},
		{"tuđe očitanje, osoba odgovara za letvu", PravaLetve{Odgovara: true, osoba: uuid.NewString()}, upisano(100 * time.Hour), true},
		{"očitanje bez autora", PravaLetve{Upisuje: true}, &models.Reading{CreatedAt: sad}, false},
		{"nema očitanja", PravaLetve{osoba: ja}, nil, false},
	} {
		if got := s.prava.MozeIspraviti(s.rd, sad); got != s.ispravlja {
			t.Errorf("%s: ispravlja %v, očekivano %v", s.ime, got, s.ispravlja)
		}
	}
	// autor ne briše svoje očitanje, osim kad odgovara za letvu
	if (PravaLetve{Upisuje: true, osoba: ja}).MozeObrisati() || !(PravaLetve{Odgovara: true}).MozeObrisati() {
		t.Error("brisanje ide samo po odgovornosti za letvu")
	}
}

// Izmjena i brisanje kroz servis: autor ispravlja svoje očitanje i kad
// izgubi dužnost, 72 h od upisa; brisati ga ne smije. Briše onaj tko
// odgovara za letvu.
func TestIspravakIBrisanjeKrozServis(t *testing.T) {
	o := novaOkolinaPracenja(t)
	ctx := context.Background()
	operater := duznost(models.RoleOperator, models.ScopeSector, "P", 0, "")
	autor := ovlastiOsobe(operater)
	sad := time.Now()
	staro := o.upisi(t, models.Reading{StationID: o.mohacs.ID.String(), MeasuredAt: sad.Add(-100 * time.Hour), LevelCm: cmP(300),
		UserID: autor.User.ID.String()})
	// upisano prije 100 h (spremište pri upisu stavlja sadašnje vrijeme)
	if _, err := o.baza.Exec(`UPDATE readings SET created_at = ? WHERE id = ?`, sad.Add(-100*time.Hour).UTC(), staro.ID.String()); err != nil {
		t.Fatal(err)
	}
	novo := o.upisi(t, models.Reading{StationID: o.mohacs.ID.String(), MeasuredAt: sad.Add(-2 * time.Hour), LevelCm: cmP(310),
		UserID: autor.User.ID.String()})

	// dok ima dužnost u sektoru P, autor ispravlja i staro očitanje
	if err := o.rs.Update(ctx, autor, &models.Reading{ID: staro.ID, MeasuredAt: staro.MeasuredAt, LevelCm: cmP(301)}); err != nil {
		t.Errorf("autor s dužnošću, staro očitanje: %v", err)
	}
	if _, err := o.rs.Delete(ctx, autor, novo.ID); err == nil || !strings.Contains(err.Error(), "nemate pravo brisati") {
		t.Errorf("autor ne briše svoje očitanje: %v", err)
	}

	// operateru istekne dužnost: novo očitanje ispravlja još 72 h, staro ne
	prosla := sad.Add(-time.Minute)
	operater.ExpiresAt = &prosla
	bez := models.NewUserPermissions(models.User{ID: autor.User.ID, FullName: "Pero Perić", IsActive: true, Duties: []models.Duty{operater}})
	if err := o.rs.Update(ctx, bez, &models.Reading{ID: novo.ID, MeasuredAt: novo.MeasuredAt, LevelCm: cmP(311)}); err != nil {
		t.Errorf("autor bez dužnosti, unutar 72 h: %v", err)
	}
	if err := o.rs.Update(ctx, bez, &models.Reading{ID: staro.ID, MeasuredAt: staro.MeasuredAt, LevelCm: cmP(302)}); err == nil || !strings.Contains(err.Error(), "nemate pravo mijenjati") {
		t.Errorf("autor bez dužnosti, nakon 72 h: %v", err)
	}

	// drugi operater sektora P upisuje na Mohács, ali tuđe ne ispravlja ni briše
	drugi := ovlastiOsobe(duznost(models.RoleOperator, models.ScopeSector, "P", 0, ""))
	if o.rs.CanEdit(ctx, drugi, &novo) || o.rs.CanDelete(ctx, drugi, &novo) {
		t.Error("operater ne dira tuđe očitanje")
	}
	// uprava sektora koji letvu prati ispravlja i briše
	uprava := ovlastiOsobe(duznost(models.RoleSectorLeader, models.ScopeSector, "P", 0, ""))
	if !o.rs.CanEdit(ctx, uprava, &novo) {
		t.Error("uprava sektora ispravlja tuđe očitanje")
	}
	if obrisano, err := o.rs.Delete(ctx, uprava, novo.ID); err != nil || obrisano == nil || obrisano.ID != novo.ID {
		t.Errorf("uprava sektora briše: %v %v", obrisano, err)
	}

	// neispravna ili nepostojeća letva: samo razina 1
	for _, rd := range []*models.Reading{{StationID: "nije-uuid"}, {StationID: uuid.NewString()}} {
		if o.rs.CanDelete(ctx, uprava, rd) || o.rs.CanEdit(ctx, uprava, rd) || !o.rs.CanDelete(ctx, o.adminOvl, rd) {
			t.Errorf("letva %q", rd.StationID)
		}
	}
	if o.rs.CanDelete(ctx, nil, &novo) || o.rs.CanDelete(ctx, uprava, nil) {
		t.Error("bez ovlasti ili očitanja")
	}
	// letva koja se ne može pročitati ne daje pravo
	o.baza.Close()
	if o.rs.CanEdit(ctx, uprava, &staro) {
		t.Error("letva koja se ne može pročitati")
	}
}

// Očitanje objekta ostaje na dosadašnjem pravilu: autor ga mijenja i briše,
// a i tko smije upisivati na objekt
func TestOcitanjeObjektaPoStaromPravilu(t *testing.T) {
	o := novaOkolinaPracenja(t)
	ctx := context.Background()
	autor := ovlastiOsobe()
	rd := o.upisi(t, models.Reading{StructureID: o.ustava.ID.String(), MeasuredAt: time.Now().Add(-time.Hour), Gate: models.GateOpen,
		UserID: autor.User.ID.String()})
	rd.CreatedAt = time.Now().Add(-500 * time.Hour) // rok od 72 h objekt ne poznaje
	if !o.rs.CanEdit(ctx, autor, &rd) || !o.rs.CanDelete(ctx, autor, &rd) {
		t.Error("autor očitanja objekta")
	}
	uprava2 := ovlastiOsobe(duznost(models.RoleAreaLeader, models.ScopeArea, "P", 2, ""))
	uprava1 := ovlastiOsobe(duznost(models.RoleAreaLeader, models.ScopeArea, "P", 1, ""))
	if !o.rs.CanDelete(ctx, uprava2, &rd) || o.rs.CanDelete(ctx, uprava1, &rd) {
		t.Error("uprava područja objekta")
	}
	if o.rs.CanDelete(ctx, uprava2, &models.Reading{StructureID: "nije-uuid"}) {
		t.Error("neispravan objekt")
	}
}
