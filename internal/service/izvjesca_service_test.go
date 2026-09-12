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

// Dnevno izvješće: piše ga tko vodi dionicu, jedno po dionici i danu, ne
// unaprijed; predlozak nosi vodotok iz dionice i otvorenu obranu sektora;
// predaja traži da bar nešto piše.
func TestDnevnoIzvjesceTok(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (16, 'B', 'Baranja', 'VGI Baranja', 'Osijek')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	journals := repository.NewJournalRepository(baza, rec)
	s := NewIzvjescaService(repository.NewIzvjescaRepository(baza, rec), nil, nil, nil, nil, journals)
	ctx := context.Background()

	sec := &models.Section{Code: "B.16.3", AreaID: 16, SectorID: "B", Parts: []models.SectionPart{{WatercourseName: "Dunav"}, {WatercourseName: "Karašica"}}}
	pocetak := time.Date(2026, 9, 10, 0, 0, 0, 0, models.Zagreb)
	obrana := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "B", Title: "Dnevnik COP-a, 2026.", Year: 2026, StartedAt: &pocetak}
	if err := journals.SaveJournal(ctx, obrana); err != nil {
		t.Fatal(err)
	}

	rukovoditelj := &models.User{ID: uuid.New(), FullName: "Rukovoditelj Dionice"}
	dionica := &models.UserPermissions{AllowedSections: map[string]bool{"B.16.3": true}}
	tudja := &models.UserPermissions{AllowedSections: map[string]bool{"B.15.1": true}}
	uprava := &models.UserPermissions{AdminAreas: map[int]bool{16: true}, AllowedAreas: map[int]bool{16: true}}

	dan := time.Date(2026, 9, 12, 9, 30, 0, 0, models.Zagreb)
	iz, err := s.Predlozak(ctx, sec, dan)
	if err != nil {
		t.Fatal(err)
	}
	if iz.Sadrzaj.Vodotok != "Dunav, Karašica" || iz.JournalID != obrana.ID || iz.Stadij != models.PhaseNormal || iz.Dan.Hour() != 0 {
		t.Fatalf("predložak: %+v", iz)
	}

	// Tuđa dionica ne prolazi; sutra ne prolazi.
	if err := s.Spremi(ctx, rukovoditelj, tudja, sec, iz); err == nil || !strings.Contains(err.Error(), "rukovoditelj dionice") {
		t.Errorf("tuđa dionica: %v", err)
	}
	sutra := *iz
	sutra.Dan = pocetakDana(time.Now().In(models.Zagreb)).AddDate(0, 0, 1)
	if err := s.Spremi(ctx, rukovoditelj, dionica, sec, &sutra); err == nil || !strings.Contains(err.Error(), "unaprijed") {
		t.Errorf("unaprijed: %v", err)
	}

	// Prazno se sprema kao nacrt, ali se ne predaje.
	if err := s.Spremi(ctx, rukovoditelj, dionica, sec, iz); err != nil {
		t.Fatal(err)
	}
	if iz.ID == "" || iz.Izradio != "Rukovoditelj Dionice" || iz.Predano() {
		t.Fatalf("spremljeno: %+v", iz)
	}
	if err := s.Predaj(ctx, rukovoditelj, dionica, sec, iz.ID); err == nil || !strings.Contains(err.Error(), "prazno") {
		t.Errorf("prazno predano: %v", err)
	}

	// Drugo izvješće za isti dan ne prolazi; isto izvješće se mijenja.
	drugo := &models.DnevnoIzvjesce{SectionCode: sec.Code, Dan: dan}
	if err := s.Spremi(ctx, rukovoditelj, dionica, sec, drugo); err == nil || !strings.Contains(err.Error(), "već postoji") {
		t.Errorf("dvostruko: %v", err)
	}
	iz.Stadij = models.PhaseRegular
	iz.Sadrzaj.Tendencija = models.TendencijaPorast
	iz.Sadrzaj.Vodostaji = []models.VodostajUIzvjescu{{Postaja: "Dunav – Batina", Sat: "07:00", Vrijednost: "+551", Jedinica: "cm"}}
	iz.Sadrzaj.Pregled = "Nasip pregledan, bez oštećenja."
	iz.Sadrzaj.Pravne.Ljudi = 6
	if err := s.Spremi(ctx, rukovoditelj, dionica, sec, iz); err != nil {
		t.Fatal(err)
	}
	if err := s.Predaj(ctx, rukovoditelj, dionica, sec, iz.ID); err != nil {
		t.Fatal(err)
	}
	opet, _ := s.Get(ctx, iz.ID)
	if !opet.Predano() || opet.Stadij != models.PhaseRegular || opet.Sadrzaj.Pravne.Ljudi != 6 || opet.Sadrzaj.Vodostaji[0].Vrijednost != "+551" || opet.Podrucje != "" {
		t.Errorf("poslije predaje: %+v", opet)
	}
	if opet.StadijKratica() != "R" {
		t.Errorf("kratica stadija: %s", opet.StadijKratica())
	}

	// Popis po dnevniku i po sektoru
	if popis, _ := s.List(ctx, obrana.ID, "", "", nil); len(popis) != 1 {
		t.Errorf("popis po dnevniku: %d", len(popis))
	}
	if n, _ := s.Broj(ctx); n != 1 {
		t.Errorf("broj: %d", n)
	}

	// Predano briše samo uprava.
	if err := s.Obrisi(ctx, rukovoditelj, dionica, sec, iz.ID); err == nil || !strings.Contains(err.Error(), "uprava") {
		t.Errorf("brisanje predanog: %v", err)
	}
	if err := s.Obrisi(ctx, rukovoditelj, uprava, sec, iz.ID); err != nil {
		t.Fatal(err)
	}
	if ostalo, _ := s.Get(ctx, iz.ID); ostalo != nil {
		t.Error("obrisano još postoji")
	}
}

// Sektorsko izvješće: zbroji predana izvješća dionica po područjima i
// vodotocima, predloži tekst, preuzme zapise dnevnika toga dana; snimka
// ostaje kakva je bila kad se izvješće dionice poslije promijeni.
func TestSektorskoIzvjesceTok(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (16, 'B', 'Baranja', 'VGI Baranja', 'Darda')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (34, 'B', 'Dunav i Drava', 'COP', 'Osijek')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.16.1', 16, 'B', 'Dunav l.o.', '2026-01-01', '2026-01-01')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.34.1', 34, 'B', 'Dunav d.o.', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	journals := repository.NewJournalRepository(baza, rec)
	sections := repository.NewSectionRepository(baza, rec)
	s := NewIzvjescaService(repository.NewIzvjescaRepository(baza, rec), sections, nil, nil, nil, journals)
	s.SetSektorska(repository.NewSektorskaIzvjescaRepository(baza, rec))
	ctx := context.Background()

	pocetak := time.Date(2026, 9, 10, 0, 0, 0, 0, models.Zagreb)
	obrana := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "B", Title: "Dnevnik COP-a", Year: 2026, StartedAt: &pocetak}
	if err := journals.SaveJournal(ctx, obrana); err != nil {
		t.Fatal(err)
	}
	dan := pocetakDana(time.Now().In(models.Zagreb))
	voditelj := &models.User{ID: uuid.New(), FullName: "Voditelj Centra"}
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}}
	rukovoditelj := &models.UserPermissions{AllowedSections: map[string]bool{"B.16.1": true}}

	// zapisi dnevnika: jedan danas, jedan storniran, jedno dežurstvo
	js := NewJournalService(journals, nil, nil)
	pod := 16
	for _, z := range []models.JournalEntry{
		{Kind: models.EntryKindReport, Text: "Procjeđivanje kod Zmajevca.", ReportedBy: "vodočuvar", Podrucje: &pod},
		{Kind: models.EntryKindDuty, Text: "Dežurstvo preuzeo."},
	} {
		z.JournalID, z.Date = obrana.ID, dan.Add(8*time.Hour)
		h := dan.Add(7*time.Hour + 15*time.Minute)
		z.HappenedAt = &h
		if err := js.DodajZapisCOP(ctx, voditelj, uprava, models.Opseg{Sektor: "B", Podrucja: []int{16, 34}}, obrana, &z); err != nil {
			t.Fatal(err)
		}
	}

	// izvješća dionica: B.16.1 predano, B.34.1 nacrt
	sec16, _ := sections.GetSectionByCode("B.16.1")
	sec34, _ := sections.GetSectionByCode("B.34.1")
	iz16 := &models.DnevnoIzvjesce{SectionCode: "B.16.1", Dan: dan, Stadij: models.PhaseEmergency, Sadrzaj: models.IzvjesceSadrzaj{Vodotok: "Dunav", Tendencija: models.TendencijaPorast,
		Vodostaji: []models.VodostajUIzvjescu{{Postaja: "Batina", Sat: "07:00", Vrijednost: "+571", Jedinica: "cm"}},
		Pregled:   "Procjeđivanje kod Zmajevca.", Radnje: "Kontranasip 45 m.", Vrece: "1 800",
		Pravne: models.SudioniciPravne{Ljudi: 14, Kamioni: 3}, Ostali: models.SudioniciOstali{Vatrogasci: 12},
		Poplavljeno: models.Poplavljeno{Naselja: "Zmajevac", Stambeni: 4, PoljoprivredneHa: 35}}}
	if err := s.Spremi(ctx, voditelj, uprava, sec16, iz16); err != nil {
		t.Fatal(err)
	}
	if err := s.Predaj(ctx, voditelj, uprava, sec16, iz16.ID); err != nil {
		t.Fatal(err)
	}
	iz34 := &models.DnevnoIzvjesce{SectionCode: "B.34.1", Dan: dan, Stadij: models.PhaseRegular, Sadrzaj: models.IzvjesceSadrzaj{Vodotok: "Dunav", Pregled: "Bez oštećenja.", Pravne: models.SudioniciPravne{Ljudi: 6}}}
	if err := s.Spremi(ctx, voditelj, uprava, sec34, iz34); err != nil {
		t.Fatal(err)
	}

	// predložak: samo predano ulazi, nacrt se broji; zapis dnevnika bez dežurstva
	pr, err := s.PredlozakSektora(ctx, "B", dan)
	if err != nil {
		t.Fatal(err)
	}
	p := pr.Sadrzaj.Pregled
	if pr.JournalID != obrana.ID || p.Izvjesca != 1 || p.Nacrta != 1 || len(p.Podrucja) != 1 || p.Podrucja[0].Naziv != "Baranja" || p.Ukupno.Pravne.Ljudi != 14 ||
		len(p.Vodotoci) != 1 || p.Vodotoci[0].Stadij != models.PhaseEmergency || p.Vodotoci[0].Tendencija != models.TendencijaPorast {
		t.Fatalf("predložak: %+v", p)
	}
	if !strings.Contains(pr.Sadrzaj.Ostecenja, "Baranja — B.16.1 (Dunav): Procjeđivanje") || !strings.Contains(pr.Sadrzaj.Mjere, "[vreće 1 800]") ||
		!strings.Contains(pr.Sadrzaj.Poplavljeno, "Zmajevac, 4 stambenih, 35 ha poljoprivrednih") {
		t.Errorf("prijedlog teksta: %+v", pr.Sadrzaj)
	}
	if len(pr.Sadrzaj.Zapisi) != 1 || pr.Sadrzaj.Zapisi[0].Podrucje != "Baranja" || pr.Sadrzaj.Zapisi[0].Vrijeme != "07:15" || pr.Sadrzaj.Zapisi[0].Javio != "vodočuvar" {
		t.Errorf("zapisi: %+v", pr.Sadrzaj.Zapisi)
	}

	// rukovoditelj dionice ne slaže sektorsko; uprava da, s oba izvješća i zapisom
	pr.Sadrzaj.Hidrometeo = "Kiša na cijelom sektoru."
	if err := s.SpremiSektorsko(ctx, voditelj, rukovoditelj, pr, []string{iz16.ID}, nil); err == nil || !strings.Contains(err.Error(), "voditelj centra") {
		t.Errorf("rukovoditelj dionice: %v", err)
	}
	if err := s.SpremiSektorsko(ctx, voditelj, uprava, pr, []string{iz16.ID, iz34.ID}, []string{pr.Sadrzaj.Zapisi[0].ID}); err != nil {
		t.Fatal(err)
	}
	if pr.ID == "" || pr.Sadrzaj.Pregled.Izvjesca != 2 || pr.Sadrzaj.Pregled.Nacrta != 0 || pr.Sadrzaj.Pregled.Ukupno.Pravne.Ljudi != 20 || len(pr.Sadrzaj.Pregled.Podrucja) != 2 || len(pr.Sadrzaj.Zapisi) != 1 {
		t.Fatalf("spremljeno: %+v", pr.Sadrzaj.Pregled)
	}
	if pr.NajvisiStadij() != models.PhaseEmergency {
		t.Errorf("najviši stadij: %s", pr.NajvisiStadij())
	}
	if err := s.PredajSektorsko(ctx, voditelj, uprava, pr.ID); err != nil {
		t.Fatal(err)
	}

	// izvješće dionice se poslije mijenja, snimka u sektorskom ostaje
	iz16.Sadrzaj.Pravne.Ljudi = 99
	if err := s.Spremi(ctx, voditelj, uprava, sec16, iz16); err != nil {
		t.Fatal(err)
	}
	opet, _ := s.GetSektorsko(ctx, pr.ID)
	if !opet.Predano() || opet.Sadrzaj.Pregled.Ukupno.Pravne.Ljudi != 20 || opet.Sadrzaj.Hidrometeo != "Kiša na cijelom sektoru." {
		t.Errorf("poslije predaje: %+v", opet)
	}
	// isti dan drugi put vraća postojeće
	if p2, _ := s.PredlozakSektora(ctx, "B", dan); p2 == nil || p2.ID != pr.ID {
		t.Error("predložak ne vraća postojeće")
	}
	if popis, _ := s.ListSektorska(ctx, "B"); len(popis) != 1 {
		t.Errorf("popis: %d", len(popis))
	}
	if s.SmijeVidjetiSektor(rukovoditelj, "B") != true || s.SmijeVidjetiSektor(&models.UserPermissions{}, "B") {
		t.Error("vidljivost")
	}
	if err := s.ObrisiSektorsko(ctx, voditelj, uprava, pr.ID); err != nil {
		t.Fatal(err)
	}
}
