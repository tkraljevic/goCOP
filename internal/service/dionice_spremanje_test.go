package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Testovi SectionService.SaveSection i pravila dionice: šifra, područje i
// sektor, tko smije dodati i urediti dionicu, provjera poddionica i nove
// veze na tuđe objekte i vodomjere. Podaci su izmišljeni: sektor F
// (područja 41 i 42) i sektor E (područje 43); šifra dionice mora biti iz
// sektora A–F, pa izmišljeni sektor ne može biti drukčiji.

type okolinaDionica struct {
	baza   *sql.DB
	svc    *SectionService
	repo   *repository.SectionRepository
	dogadj chan string
}

func novaOkolinaDionica(t *testing.T) *okolinaDionica {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "dionice.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('F', 'Sektor F', 'VGO Primjerovo', 'COP Primjerovo'), ('E', 'Sektor E', 'VGO Drugdje', 'COP Drugdje')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (41, 'F', 'Mali sliv Primjerica', 'VGI Primjerica', ''),
			(42, 'F', 'Mali sliv Probni', 'VGI Probni', ''), (43, 'E', 'Mali sliv Drugdje', 'VGI Drugdje', '')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	sse := NewSSEBroker()
	repo := repository.NewSectionRepository(baza, ledger.New(baza, "test"))
	o := &okolinaDionica{baza: baza, svc: NewSectionService(repo, sse), repo: repo, dogadj: sse.Subscribe()}
	return o
}

// zadnjiDogadjaj vraća vrstu zadnjeg događaja koji je servis objavio
func (o *okolinaDionica) zadnjiDogadjaj(t *testing.T) string {
	t.Helper()
	var zadnji string
	for {
		select {
		case poruka := <-o.dogadj:
			var e SSEEvent
			if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(poruka, "data:")), "data: ")), &e); err == nil {
				zadnji = e.Type + ":" + e.Message
			} else {
				zadnji = poruka
			}
		default:
			return zadnji
		}
	}
}

func dionicaS(sifra string, podrucje int, dijelovi ...models.SectionPart) *models.Section {
	if len(dijelovi) == 0 {
		dijelovi = []models.SectionPart{{Description: "rijeka Primjerica, lijeva obala"}}
	}
	return &models.Section{Code: sifra, AreaID: podrucje, Parts: dijelovi}
}

var (
	upravaSektoraF = &models.UserPermissions{AdminSectors: map[string]bool{"F": true}}
	upravaPodr41   = &models.UserPermissions{AdminAreas: map[int]bool{41: true}}
	pisePodr42     = &models.UserPermissions{AllowedAreas: map[int]bool{42: true}}
	dionicar411    = &models.UserPermissions{AllowedSections: map[string]bool{"F.41.1": true}, PodrucjaDionica: map[int]bool{41: true}}
	globalni       = &models.UserPermissions{IsGlobalAdmin: true}
)

func TestNovaDionicaUlaz(t *testing.T) {
	o := novaOkolinaDionica(t)
	ctx := context.Background()
	for _, s := range []struct {
		ime    string
		sec    *models.Section
		greska string
	}{
		{"bez dionice", nil, ErrInvalidSection.Error()},
		{"bez šifre", dionicaS("  ", 41), ErrInvalidSection.Error()},
		{"bez područja", dionicaS("F.41.1", 0), ErrInvalidSection.Error()},
		{"sektor izvan A–F", dionicaS("P.41.1", 41), "SEKTOR.PODRUČJE.BROJ"},
		{"slovo u broju", dionicaS("F.41.1a", 41), "SEKTOR.PODRUČJE.BROJ"},
		{"troznamenkasto područje", dionicaS("F.410.1", 41), "SEKTOR.PODRUČJE.BROJ"},
		{"bez poddionica", &models.Section{Code: "F.41.1", AreaID: 41}, "bar jednu poddionicu"},
		{"poddionica bez vode i opisa", dionicaS("F.41.1", 41, models.SectionPart{Description: "  "}), "poddionica 1 nema vodotok"},
		{"kriva obala", dionicaS("F.41.1", 41, models.SectionPart{Description: "x"}, models.SectionPart{Description: "y", Bank: "S"}), "poddionica 2: obala je L, D ili LD"},
		{"objekt bez naziva", dionicaS("F.41.1", 41, models.SectionPart{Description: "x", Objects: []models.PartObject{{Name: " "}}}), "objekt bez naziva"},
		{"nasip bez naziva", dionicaS("F.41.1", 41, models.SectionPart{Description: "x", Embankments: []models.PartEmbankment{{}}}), "nasip bez naziva"},
		{"nova voda bez globalnog", dionicaS("F.41.1", 41, models.SectionPart{Description: "x", Objects: []models.PartObject{{Name: "ušće potoka Probni",
			NewRecord: &models.NewRegistryRecord{Registry: models.RegistryWatercourse, Kind: "potok", Name: "Probni"}}}}), "globalni administrator"},
	} {
		err := o.svc.SaveSection(ctx, upravaSektoraF, s.sec, true)
		if err == nil || !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: očekivano „%s”, dobiveno %v", s.ime, s.greska, err)
		}
	}
	if n, _ := o.svc.ListSections("", 0, ""); len(n) != 0 {
		t.Errorf("odbijene dionice su upisane: %d", len(n))
	}
	if d := o.zadnjiDogadjaj(t); d != "" {
		t.Errorf("odbijeno spremanje objavilo je događaj %q", d)
	}
}

func TestNovaDionicaUpis(t *testing.T) {
	o := novaOkolinaDionica(t)
	ctx := context.Background()
	od, do := 12.5, 3.0
	sec := dionicaS(" f.41.1 ", 41,
		models.SectionPart{Description: "rijeka Primjerica", KmFrom: &od, KmTo: &do, Bank: "LD", Seq: 7},
		models.SectionPart{Description: "kanal Probni", Seq: 7})
	if err := o.svc.SaveSection(ctx, upravaSektoraF, sec, true); err != nil {
		t.Fatal(err)
	}
	// šifra velikim slovima i bez razmaka, sektor iz šifre, poddionice
	// renumerirane, a stacionaža uređena od manje prema većoj
	if sec.Code != "F.41.1" || sec.SectorID != "F" || sec.Parts[0].Seq != 1 || sec.Parts[1].Seq != 2 || *sec.Parts[0].KmFrom != 3 || *sec.Parts[0].KmTo != 12.5 {
		t.Errorf("nakon spremanja: %+v", sec)
	}
	if d := o.zadnjiDogadjaj(t); !strings.Contains(d, "section_created") || !strings.Contains(d, "F.41.1") {
		t.Errorf("događaj: %q", d)
	}
	upisana, err := o.svc.GetSectionWithDetails("F.41.1")
	if err != nil || upisana.SectorID != "F" || upisana.AreaID != 41 || len(upisana.Parts) != 2 {
		t.Fatalf("upisana dionica: %+v %v", upisana, err)
	}
	// verzija u knjizi, za razmjenu
	var verzija int
	if err := o.baza.QueryRow(`SELECT count(*) FROM record_versions WHERE entity = 'sections' AND entity_id = 'F.41.1'`).Scan(&verzija); err != nil || verzija != 1 {
		t.Errorf("verzija u knjizi: %d %v", verzija, err)
	}
	// ista šifra drugi put ne
	if err := o.svc.SaveSection(ctx, upravaSektoraF, dionicaS("F.41.1", 41), true); err == nil || !strings.Contains(err.Error(), "već postoji") {
		t.Errorf("dvostruka šifra: %v", err)
	}
	// područje kojeg nema
	if err := o.svc.SaveSection(ctx, globalni, &models.Section{Code: "F.49.1", AreaID: 49, SectorID: "", Parts: []models.SectionPart{{Description: "x"}}}, true); err == nil {
		t.Error("dionica nepostojećeg područja")
	}
}

func TestPravoNaNovuDionicu(t *testing.T) {
	o := novaOkolinaDionica(t)
	ctx := context.Background()
	for _, s := range []struct {
		ime   string
		perms *models.UserPermissions
		sec   *models.Section
		smije bool
	}{
		{"uprava sektora", upravaSektoraF, dionicaS("F.42.1", 42), true},
		{"uprava područja u svom", upravaPodr41, dionicaS("F.41.2", 41), true},
		{"uprava područja u tuđem", upravaPodr41, dionicaS("F.42.2", 42), false},
		{"rukovoditelj dionice", dionicar411, dionicaS("F.41.3", 41), false},
		{"bez ovlasti", nil, dionicaS("F.41.4", 41), false},
		{"globalni administrator", globalni, dionicaS("E.43.1", 43), true},
	} {
		err := o.svc.SaveSection(ctx, s.perms, s.sec, true)
		if s.smije && err != nil {
			t.Errorf("%s: %v", s.ime, err)
		}
		if !s.smije && !errors.Is(err, ErrUnauthorized) {
			t.Errorf("%s: očekivano odbijanje, dobiveno %v", s.ime, err)
		}
	}
}

func TestSifraDioniceProvjeravaPodrucje(t *testing.T) {
	o := novaOkolinaDionica(t)
	ctx := context.Background()
	// Šifra nosi sektor i područje dionice: B.15.5 ide u područje 15
	// sektora B. Pravo se priznaje po području, pa bi šifra tuđeg sektora
	// ili područja upisala dionicu ondje gdje onaj tko piše nema ništa.
	for _, s := range []struct {
		ime    string
		perms  *models.UserPermissions
		sec    *models.Section
		sektor string
		greska string
	}{
		{"područje iz šifre nije područje dionice", upravaSektoraF, dionicaS("F.42.7", 41), "", "nosi branjeno područje 42"},
		{"područje s vodećom nulom", globalni, dionicaS("F.041.1", 41), "", "SEKTOR.PODRUČJE.BROJ"},
		{"nula ispred broja područja", globalni, dionicaS("F.05.1", 5), "", "nosi branjeno područje 05"},
		{"sektor iz šifre nije sektor područja", pisePodr42, dionicaS("E.42.1", 42), "", "pripada sektoru F"},
		{"zadani sektor nije sektor iz šifre", globalni, dionicaS("F.41.9", 41), "E", "nosi sektor F"},
		{"zadani sektor nije sektor područja", globalni, dionicaS("E.41.9", 41), "E", "pripada sektoru F"},
	} {
		s.sec.SectorID = s.sektor
		err := o.svc.SaveSection(ctx, s.perms, s.sec, true)
		if err == nil || !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: očekivano „%s”, dobiveno %v", s.ime, s.greska, err)
		}
	}
	if n, _ := o.svc.ListSections("", 0, ""); len(n) != 0 {
		t.Errorf("odbijene dionice su upisane: %d", len(n))
	}
	// zadani sektor koji se slaže sa šifrom i područjem prolazi, i malim slovom
	sec := dionicaS("E.43.1", 43)
	sec.SectorID = " e "
	if err := o.svc.SaveSection(ctx, globalni, sec, true); err != nil || sec.SectorID != "E" {
		t.Errorf("zadani sektor: %q %v", sec.SectorID, err)
	}
	if d, _ := o.svc.GetSectionWithDetails("E.43.1"); d == nil || d.SectorID != "E" || d.AreaID != 43 {
		t.Errorf("dionica E.43.1: %+v", d)
	}
}

func TestIzmjenaDionice(t *testing.T) {
	o := novaOkolinaDionica(t)
	ctx := context.Background()
	if err := o.svc.SaveSection(ctx, globalni, dionicaS("F.41.1", 41), true); err != nil {
		t.Fatal(err)
	}
	o.zadnjiDogadjaj(t)
	// uprava drugog područja ne uređuje
	if err := o.svc.SaveSection(ctx, pisePodr42, dionicaS("F.41.1", 41), false); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("tuđe područje: %v", err)
	}
	// rukovoditelj dionice uređuje svoju, ali ne može je premjestiti u
	// drugo područje ni sektor: područje i sektor ostaju zatečeni
	izmjena := dionicaS("F.41.1", 42, models.SectionPart{Description: "rijeka Primjerica, nova stacionaža"})
	izmjena.SectorID = "E"
	if err := o.svc.SaveSection(ctx, dionicar411, izmjena, false); err != nil {
		t.Fatal(err)
	}
	if izmjena.AreaID != 41 || izmjena.SectorID != "F" {
		t.Errorf("izmjena premjestila je dionicu: %d %s", izmjena.AreaID, izmjena.SectorID)
	}
	d, _ := o.svc.GetSectionWithDetails("F.41.1")
	if d == nil || d.Parts[0].Description != "rijeka Primjerica, nova stacionaža" {
		t.Errorf("izmjena nije upisana: %+v", d)
	}
	if e := o.zadnjiDogadjaj(t); !strings.Contains(e, "section_updated") {
		t.Errorf("događaj: %q", e)
	}
	// izmjena nepostojeće
	if err := o.svc.SaveSection(ctx, globalni, dionicaS("F.41.8", 41), false); !errors.Is(err, ErrSectionNotFound) {
		t.Errorf("izmjena nepostojeće: %v", err)
	}
	// izmjena s neispravnom poddionicom ne mijenja ništa
	if err := o.svc.SaveSection(ctx, globalni, dionicaS("F.41.1", 41, models.SectionPart{Bank: "X", Description: "x"}), false); err == nil {
		t.Error("neispravna izmjena")
	}
	if d, _ := o.svc.GetSectionWithDetails("F.41.1"); d.Parts[0].Description != "rijeka Primjerica, nova stacionaža" {
		t.Error("neispravna izmjena promijenila je dionicu")
	}
}

func TestNoveVezeDionice(t *testing.T) {
	sec := &models.Section{Code: "F.41.1", AreaID: 41, SectorID: "F"}
	objekt := func(podrucje int, sektor string) repository.ObjektUzDionice {
		return repository.ObjektUzDionice{ID: "o", Objekt: &models.Structure{Name: "CS Probni", AreaID: podrucje, SectorID: sektor}}
	}
	vodomjer := func(dionice ...models.Section) repository.VodomjerUzDionice {
		return repository.VodomjerUzDionice{ID: "v", Naziv: "Primjerovo", Dionice: dionice}
	}
	d42 := models.Section{Code: "F.42.1", AreaID: 42, SectorID: "F"}
	for _, s := range []struct {
		ime    string
		perms  *models.UserPermissions
		nove   repository.NoveVezeDionice
		greska string
	}{
		{"bez ovlasti", nil, repository.NoveVezeDionice{}, "nemate"},
		{"globalni sve", globalni, repository.NoveVezeDionice{Objekti: []repository.ObjektUzDionice{objekt(43, "E")}}, ""},
		{"objekt svog područja", dionicar411, repository.NoveVezeDionice{Objekti: []repository.ObjektUzDionice{objekt(41, "F")}}, ""},
		{"objekt tuđeg područja", dionicar411, repository.NoveVezeDionice{Objekti: []repository.ObjektUzDionice{objekt(42, "F")}}, "pripada branjenom području 42"},
		{"objekt tuđeg područja, uprava sektora", upravaSektoraF, repository.NoveVezeDionice{Objekti: []repository.ObjektUzDionice{objekt(42, "F")}}, ""},
		{"objekt kojeg nema", dionicar411, repository.NoveVezeDionice{Objekti: []repository.ObjektUzDionice{{ID: "nema"}}}, "nije u registru"},
		{"slobodan vodomjer", dionicar411, repository.NoveVezeDionice{Vodomjeri: []repository.VodomjerUzDionice{vodomjer()}}, ""},
		{"vodomjer tuđe dionice", dionicar411, repository.NoveVezeDionice{Vodomjeri: []repository.VodomjerUzDionice{vodomjer(d42)}}, "F.42.1 drugog područja"},
		{"vodomjer tuđe dionice, piše ondje", pisePodr42, repository.NoveVezeDionice{Vodomjeri: []repository.VodomjerUzDionice{vodomjer(d42)}}, ""},
		{"vodomjer i svoje i tuđe dionice", dionicar411, repository.NoveVezeDionice{Vodomjeri: []repository.VodomjerUzDionice{vodomjer(d42, models.Section{Code: "F.41.2", AreaID: 41})}}, ""},
		{"vodomjer bez naziva", dionicar411, repository.NoveVezeDionice{Vodomjeri: []repository.VodomjerUzDionice{{ID: "id-vodomjera", Dionice: []models.Section{d42}}}}, "„id-vodomjera”"},
	} {
		err := provjeriNoveVeze(s.perms, sec, s.nove)
		switch {
		case s.greska == "" && err != nil:
			t.Errorf("%s: %v", s.ime, err)
		case s.greska != "" && (err == nil || !strings.Contains(err.Error(), s.greska)):
			t.Errorf("%s: očekivano „%s”, dobiveno %v", s.ime, s.greska, err)
		}
	}
}

func TestPravaNaDionicu(t *testing.T) {
	o := novaOkolinaDionica(t)
	d := &models.Section{Code: "F.41.1", AreaID: 41, SectorID: "F"}
	for _, s := range []struct {
		ime   string
		perms *models.UserPermissions
		smije bool
	}{
		{"globalni", globalni, true},
		{"uprava sektora", upravaSektoraF, true},
		{"piše u sektoru", &models.UserPermissions{AllowedSectors: map[string]bool{"F": true}}, true},
		{"uprava područja", upravaPodr41, true},
		{"piše u drugom području", pisePodr42, false},
		{"rukovoditelj te dionice", dionicar411, true},
		{"rukovoditelj druge dionice", &models.UserPermissions{AllowedSections: map[string]bool{"F.41.2": true}}, false},
		{"bez ovlasti", nil, false},
	} {
		if got := o.svc.CanEditSection(s.perms, d); got != s.smije {
			t.Errorf("%s: %v", s.ime, got)
		}
	}
	if o.svc.CanEditSection(globalni, nil) {
		t.Error("nepostojeća dionica")
	}
	if !o.svc.CanCreateSectionInArea(&models.UserPermissions{AllowedSectors: map[string]bool{"F": true}}, "F", 0) ||
		o.svc.CanCreateSectionInArea(upravaPodr41, "", 0) || o.svc.CanCreateSectionInArea(nil, "F", 41) {
		t.Error("pravo na novu dionicu")
	}
	if _, err := o.svc.GetSectionWithDetails("F.41.1"); !errors.Is(err, ErrSectionNotFound) {
		t.Errorf("nepostojeća dionica: %v", err)
	}
}

func TestSljedecaSifraDionice(t *testing.T) {
	o := novaOkolinaDionica(t)
	ctx := context.Background()
	if got := o.svc.SljedecaSifra("F", 41); got != "F.41.1" {
		t.Errorf("prazno područje: %s", got)
	}
	for _, d := range []*models.Section{dionicaS("F.41.1", 41), dionicaS("F.41.4", 41), dionicaS("F.42.9", 42)} {
		if err := o.svc.SaveSection(ctx, globalni, d, true); err != nil {
			t.Fatal(err)
		}
	}
	// samo dionice područja 41 (F.42.9 je u području 42)
	if got := o.svc.SljedecaSifra("F", 41); got != "F.41.5" {
		t.Errorf("sljedeća u 41: %s", got)
	}
	if o.svc.SljedecaSifra("", 41) != "" || o.svc.SljedecaSifra("F", 0) != "" {
		t.Error("bez sektora ili područja nema prijedloga")
	}
	// bez tablice dionica prijedlog je prvi broj
	if _, err := o.baza.Exec(`ALTER TABLE sections RENAME TO nema_sections`); err != nil {
		t.Fatal(err)
	}
	if got := o.svc.SljedecaSifra("F", 41); got != "F.41.1" {
		t.Errorf("bez dionica: %s", got)
	}
	if err := o.svc.SaveSection(ctx, globalni, dionicaS("F.41.6", 41), true); err == nil {
		t.Error("spremanje bez tablice dionica")
	}
}
