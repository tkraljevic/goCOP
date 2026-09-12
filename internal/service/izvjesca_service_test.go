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
