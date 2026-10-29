package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// nasip iz registra objekata na koji se lokacija veže šifrom
const nasipProbniID = "0190a000-0000-7000-8000-000000000001"

var (
	podrucjeOdrzavanja = models.Area{ID: 1, SectorID: "P", Name: "Mali sliv Primjerica"}
	drugoPodrucje      = models.Area{ID: 2, SectorID: "P", Name: "Mali sliv Drugovo"}
	pravoOdrzavanja    = &models.UserPermissions{AllowedAreas: map[int]bool{1: true}}
)

// novaBazaOdrzavanja: sektor s dva područja, voda i nasip u registru
func novaBazaOdrzavanja(t *testing.T) (*sql.DB, *MaintenanceService) {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "odrzavanje.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo');
		INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', ''),
			(2, 'P', 'Mali sliv Drugovo', 'VGI Drugovo', '');
		INSERT INTO watercourses (code, official_name, name) VALUES ('V-PRIMJERICA', 'Primjerica', 'Primjerica');
		INSERT INTO structures (id, code, name, kind, sector_id, area_id, created_at, updated_at)
			VALUES ('` + nasipProbniID + `', 'N-PROBNI', 'Nasip Probni', 'NASIP', 'P', 1, '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	s := NewMaintenanceService(repository.NewMaintenanceRepository(baza, rec),
		repository.NewWatercourseRepository(baza, rec), repository.NewStructureRepository(baza, rec))
	return baza, s
}

// stanjeOdrzavanja: redovi popisa lokacija i njihove verzije u knjizi, bez
// oznaka verzija i vremena, koji su svaki put drukčiji
func stanjeOdrzavanja(t *testing.T, baza *sql.DB) []string {
	t.Helper()
	var out []string
	redovi, err := baza.Query(`SELECT id, area_id, program, watercourse_code, structure_id, name, seq, water_order, water_group, kind, source
		FROM maintained_waters ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer redovi.Close()
	for redovi.Next() {
		var id, program, voda, objekt, naziv, seq, red, skupina, vrsta, izvor string
		var podrucje int
		if err := redovi.Scan(&id, &podrucje, &program, &voda, &objekt, &naziv, &seq, &red, &skupina, &vrsta, &izvor); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("lokacija %s|%d|%s|%s|%s|%s|%s|%s|%s|%s|%s", id, podrucje, program, voda, objekt, naziv, seq, red, skupina, vrsta, izvor))
	}
	if err := redovi.Err(); err != nil {
		t.Fatal(err)
	}
	verzije, err := baza.Query(`SELECT entity, entity_id, node_id, supersedes <> '', archived, payload, schema_version, channel
		FROM record_versions WHERE entity = ? ORDER BY version_id`, repository.EntityMaintainedWaters)
	if err != nil {
		t.Fatal(err)
	}
	defer verzije.Close()
	for verzije.Next() {
		var entitet, id, cvor, sadrzaj, kanal string
		var zamjenjuje, arhivirano bool
		var shema int
		if err := verzije.Scan(&entitet, &id, &cvor, &zamjenjuje, &arhivirano, &sadrzaj, &shema, &kanal); err != nil {
			t.Fatal(err)
		}
		var polja map[string]any
		if err := json.Unmarshal([]byte(sadrzaj), &polja); err != nil {
			t.Fatal(err)
		}
		delete(polja, "created_at")
		delete(polja, "updated_at")
		bez, err := json.Marshal(polja)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("verzija %s|%s|%s|%t|%t|%s|%d|%s", entitet, id, cvor, zamjenjuje, arhivirano, bez, shema, kanal))
	}
	if err := verzije.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func tekstGreske(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// pokvari mijenja bazu tako da zadani korak repozitorija vrati grešku
func pokvari(t *testing.T, baza *sql.DB, sto string) {
	t.Helper()
	upiti := map[string]string{
		"popis": `ALTER TABLE maintained_waters RENAME TO nema_popisa`,
		// popis lokacija čita samo naziv i vrstu iz registara, pa ga stupci
		// koje čita samo registar ne kvare
		"vode":    `ALTER TABLE watercourses RENAME COLUMN wiki_slug TO nema_stupca`,
		"objekti": `ALTER TABLE structures RENAME COLUMN notes TO nema_stupca`,
		"upis":    `CREATE TRIGGER bez_upisa BEFORE INSERT ON maintained_waters BEGIN SELECT RAISE(ABORT, 'upis zabranjen'); END`,
	}
	if _, err := baza.Exec(upiti[sto]); err != nil {
		t.Fatal(err)
	}
}

// Ručni unos lokacije: provjere redom kojim ih obrazac javlja; ništa se ne
// upisuje dok neka provjera ne prođe
func TestAddWaterProvjere(t *testing.T) {
	ctx := context.Background()
	slucajevi := []struct {
		ime      string
		prava    *models.UserPermissions
		lokacija models.MaintainedWater
		greska   string
	}{
		{"bez prava", nil, models.MaintainedWater{Name: "Kanal Probni"}, "nemate pravo uređivati popis lokacija tog područja"},
		{"pravo u drugom području", &models.UserPermissions{AllowedAreas: map[int]bool{2: true}}, models.MaintainedWater{Name: "Kanal Probni"}, "nemate pravo uređivati popis lokacija tog područja"},
		{"prazan naziv", pravoOdrzavanja, models.MaintainedWater{Name: "  \t "}, "naziv lokacije je obvezan"},
		{"bez reda", pravoOdrzavanja, models.MaintainedWater{Name: "Kanal Probni"}, "odaberite red vode"},
		{"nepoznat red", pravoOdrzavanja, models.MaintainedWater{Name: "Kanal Probni", Order: "V"}, "odaberite red vode"},
		{"I. red bez skupine", pravoOdrzavanja, models.MaintainedWater{Name: "Rijeka Primjerica", Order: models.WaterOrderFirst}, "za vode I. reda odaberite skupinu"},
		{"I. red s nepoznatom skupinom", pravoOdrzavanja, models.MaintainedWater{Name: "Rijeka Primjerica", Order: models.WaterOrderFirst, Group: "OSTALE"}, "za vode I. reda odaberite skupinu"},
		{"nepoznat program", pravoOdrzavanja, models.MaintainedWater{Name: "Kanal Probni", Order: models.WaterOrderSecond, Program: "A.04"}, "program je A.02 ili A.03"},
		{"bez vrste", pravoOdrzavanja, models.MaintainedWater{Name: "Kanal Probni", Order: models.WaterOrderSecond}, "odaberite vrstu lokacije"},
		{"nepoznata vrsta", pravoOdrzavanja, models.MaintainedWater{Name: "Kanal Probni", Order: models.WaterOrderSecond, Kind: "POTOK"}, "odaberite vrstu lokacije"},
	}
	for _, sl := range slucajevi {
		t.Run(sl.ime, func(t *testing.T) {
			baza, s := novaBazaOdrzavanja(t)
			m := sl.lokacija
			if got := tekstGreske(s.AddWater(ctx, sl.prava, podrucjeOdrzavanja, &m, "")); got != sl.greska {
				t.Errorf("greška %q, a treba %q", got, sl.greska)
			}
			if st := stanjeOdrzavanja(t, baza); len(st) != 0 {
				t.Errorf("upisano unatoč grešci: %v", st)
			}
		})
	}
}

// Ručni unos dopunjava lokaciju: sređen naziv, program po redu vode, skupina
// samo za I. red, izvor "ručni unos" i stabilan identitet iz naziva
func TestAddWaterDopuna(t *testing.T) {
	ctx := context.Background()
	slucajevi := []struct {
		ime                            string
		prava                          *models.UserPermissions
		lokacija                       models.MaintainedWater
		program, skupina, izvor, naziv string
	}{
		{"I. red međudržavne", pravoOdrzavanja, models.MaintainedWater{Name: " Rijeka   Primjerica ", Order: models.WaterOrderFirst, Group: models.WaterGroupInterstate, Kind: models.MaintenanceKindWatercourse},
			models.ProgramA02, models.WaterGroupInterstate, "ručni unos", "Rijeka Primjerica"},
		{"I. red ostale državne", &models.UserPermissions{IsGlobalAdmin: true}, models.MaintainedWater{Name: "Rijeka Primjerica", Order: models.WaterOrderFirst, Group: models.WaterGroupOtherState, Kind: models.MaintenanceKindWatercourse},
			models.ProgramA02, models.WaterGroupOtherState, "ručni unos", "Rijeka Primjerica"},
		{"II. red gubi skupinu", &models.UserPermissions{AdminSectors: map[string]bool{"P": true}}, models.MaintainedWater{Name: "Kanal Probni", Order: models.WaterOrderSecond, Group: models.WaterGroupInterstate, Kind: models.MaintenanceKindDrainage},
			models.ProgramA02, "", "ručni unos", "Kanal Probni"},
		{"III. red je A.03", &models.UserPermissions{AdminAreas: map[int]bool{1: true}}, models.MaintainedWater{Name: "Kanal III-1", Order: models.WaterOrderThird, Kind: models.MaintenanceKindDrainage},
			models.ProgramA03, "", "ručni unos", "Kanal III-1"},
		{"IV. red je A.03", &models.UserPermissions{AllowedSectors: map[string]bool{"P": true}}, models.MaintainedWater{Name: "Kanal IV-1", Order: models.WaterOrderFourth, Kind: models.MaintenanceKindDrainage},
			models.ProgramA03, "", "ručni unos", "Kanal IV-1"},
		{"zadani program i izvor ostaju", pravoOdrzavanja, models.MaintainedWater{Name: "Kanal IV-2", Order: models.WaterOrderFourth, Kind: models.MaintenanceKindTorrent, Program: models.ProgramA02, Source: "ugovor 2026"},
			models.ProgramA02, "", "ugovor 2026", "Kanal IV-2"},
	}
	for _, sl := range slucajevi {
		t.Run(sl.ime, func(t *testing.T) {
			baza, s := novaBazaOdrzavanja(t)
			m := sl.lokacija
			m.AreaID, m.ID = 9, "zadano"
			if err := s.AddWater(ctx, sl.prava, podrucjeOdrzavanja, &m, ""); err != nil {
				t.Fatal(err)
			}
			if m.Name != sl.naziv || m.Program != sl.program || m.Group != sl.skupina || m.Source != sl.izvor || m.AreaID != 1 ||
				m.ID != repository.MaintainedWaterID(1, sl.naziv) || m.WatercourseCode != "" || m.StructureID != "" {
				t.Errorf("lokacija %+v", m)
			}
			got, err := s.repo.GetWater(ctx, m.ID)
			if err != nil || got == nil || got.Name != sl.naziv || got.Program != sl.program || got.Group != sl.skupina || got.Source != sl.izvor {
				t.Errorf("upisano %+v, %v", got, err)
			}
			if st := stanjeOdrzavanja(t, baza); len(st) != 2 {
				t.Errorf("treba jedan red i jedna verzija: %v", st)
			}
		})
	}
}

// Ručni unos veže lokaciju na vodu, a kad vode nema na objekt iste šifre;
// šifra se čita bez razmaka oko nje
func TestAddWaterVeza(t *testing.T) {
	ctx := context.Background()
	slucajevi := []struct {
		ime, zadanaVoda, zadanObjekt, cilj, voda, objekt, greska string
	}{
		{"bez cilja", "", "", "   ", "", "", ""},
		// za razliku od vezanja postojeće lokacije, unos bez šifre ne skida vezu
		{"bez cilja zadana voda ostaje", "V-PRIMJERICA", "", "", "V-PRIMJERICA", "", ""},
		{"bez cilja zadan objekt ostaje", "", nasipProbniID, " ", "", nasipProbniID, ""},
		{"voda", "", "", " V-PRIMJERICA ", "V-PRIMJERICA", "", ""},
		{"nasip", "", "", "N-PROBNI", "", nasipProbniID, ""},
		{"nepoznata šifra", "", "", "X-NEMA", "", "", "nema vode ni objekta s tom šifrom"},
	}
	for _, sl := range slucajevi {
		t.Run(sl.ime, func(t *testing.T) {
			baza, s := novaBazaOdrzavanja(t)
			m := models.MaintainedWater{Name: "Kanal Probni", Order: models.WaterOrderSecond, Kind: models.MaintenanceKindDrainage,
				WatercourseCode: sl.zadanaVoda, StructureID: sl.zadanObjekt}
			if got := tekstGreske(s.AddWater(ctx, pravoOdrzavanja, podrucjeOdrzavanja, &m, sl.cilj)); got != sl.greska {
				t.Fatalf("greška %q, a treba %q", got, sl.greska)
			}
			if m.WatercourseCode != sl.voda || m.StructureID != sl.objekt {
				t.Errorf("veza %q/%q, a treba %q/%q", m.WatercourseCode, m.StructureID, sl.voda, sl.objekt)
			}
			upisano := len(stanjeOdrzavanja(t, baza)) == 2
			if upisano != (sl.greska == "") {
				t.Errorf("upisano %t uz grešku %q", upisano, sl.greska)
			}
			if !upisano {
				return
			}
			got, err := s.repo.GetWater(ctx, m.ID)
			if err != nil || got == nil || got.WatercourseCode != sl.voda || got.StructureID != sl.objekt {
				t.Errorf("upisana veza %+v, %v", got, err)
			}
		})
	}
}

// Isti naziv u istom području je ista lokacija, pa se drugi put ne dodaje;
// u drugom području je druga lokacija
func TestAddWaterDvaput(t *testing.T) {
	ctx := context.Background()
	baza, s := novaBazaOdrzavanja(t)
	nova := func() *models.MaintainedWater {
		return &models.MaintainedWater{Name: "Kanal  Probni", Order: models.WaterOrderSecond, Kind: models.MaintenanceKindDrainage}
	}
	if err := s.AddWater(ctx, pravoOdrzavanja, podrucjeOdrzavanja, nova(), "V-PRIMJERICA"); err != nil {
		t.Fatal(err)
	}
	if got := tekstGreske(s.AddWater(ctx, pravoOdrzavanja, podrucjeOdrzavanja, nova(), "N-PROBNI")); got != "lokacija s tim nazivom već je u popisu" {
		t.Errorf("drugi put: %q", got)
	}
	if st := stanjeOdrzavanja(t, baza); len(st) != 2 || !strings.Contains(st[0], "V-PRIMJERICA") {
		t.Errorf("prvi upis mora ostati: %v", st)
	}
	if err := s.AddWater(ctx, &models.UserPermissions{IsGlobalAdmin: true}, drugoPodrucje, nova(), ""); err != nil {
		t.Errorf("drugo područje: %v", err)
	}
}

// Greška repozitorija vraća se kako jest, a lokacija se ne upisuje
func TestAddWaterGreskeRepozitorija(t *testing.T) {
	ctx := context.Background()
	slucajevi := []struct {
		ime, kvar, cilj, greska string
	}{
		{"čitanje popisa", "popis", "", "no such table: maintained_waters"},
		{"čitanje voda", "vode", "N-PROBNI", "greška pri dohvaćanju vodnog tijela"},
		{"čitanje objekata", "objekti", "N-PROBNI", "no such column: notes"},
		{"upis", "upis", "", "upis zabranjen"},
	}
	for _, sl := range slucajevi {
		t.Run(sl.ime, func(t *testing.T) {
			baza, s := novaBazaOdrzavanja(t)
			pokvari(t, baza, sl.kvar)
			m := models.MaintainedWater{Name: "Kanal Probni", Order: models.WaterOrderSecond, Kind: models.MaintenanceKindDrainage}
			err := s.AddWater(ctx, pravoOdrzavanja, podrucjeOdrzavanja, &m, sl.cilj)
			if err == nil || !strings.Contains(err.Error(), sl.greska) {
				t.Errorf("greška %v, a treba %q", err, sl.greska)
			}
			// bez tablice popisa nema se što pročitati
			if sl.kvar == "popis" {
				return
			}
			if st := stanjeOdrzavanja(t, baza); len(st) != 0 {
				t.Errorf("upisano unatoč grešci: %v", st)
			}
		})
	}
}

// dodajLokaciju upisuje lokaciju II. reda vezanu na vodu, za vezanje poslije
func dodajLokaciju(t *testing.T, s *MaintenanceService, podrucje models.Area) string {
	t.Helper()
	m := models.MaintainedWater{Name: "Kanal Probni", Order: models.WaterOrderSecond, Kind: models.MaintenanceKindDrainage}
	if err := s.AddWater(context.Background(), &models.UserPermissions{IsGlobalAdmin: true}, podrucje, &m, "V-PRIMJERICA"); err != nil {
		t.Fatal(err)
	}
	return m.ID
}

// Vezanje postojeće lokacije: nova veza briše staru, prazna šifra skida vezu,
// a lokacija drugog područja za ovo područje ne postoji
func TestLinkWater(t *testing.T) {
	ctx := context.Background()
	slucajevi := []struct {
		ime     string
		prava   *models.UserPermissions
		drugo   bool
		id      string
		cilj    string
		voda    string
		objekt  string
		greska  string
		verzija bool
	}{
		{"bez prava", &models.UserPermissions{}, false, "", "N-PROBNI", "V-PRIMJERICA", "", "nemate pravo uređivati popis lokacija tog područja", false},
		{"nepostojeća lokacija", pravoOdrzavanja, false, "nema-je", "N-PROBNI", "V-PRIMJERICA", "", "lokacija nije pronađena", false},
		{"lokacija drugog područja", pravoOdrzavanja, true, "", "N-PROBNI", "V-PRIMJERICA", "", "lokacija nije pronađena", false},
		{"na nasip", pravoOdrzavanja, false, "", " N-PROBNI ", "", nasipProbniID, "", true},
		{"na istu vodu", pravoOdrzavanja, false, "", "V-PRIMJERICA", "V-PRIMJERICA", "", "", true},
		{"skidanje veze", pravoOdrzavanja, false, "", "  ", "", "", "", true},
		{"nepoznata šifra", pravoOdrzavanja, false, "", "X-NEMA", "V-PRIMJERICA", "", "nema vode ni objekta s tom šifrom", false},
	}
	for _, sl := range slucajevi {
		t.Run(sl.ime, func(t *testing.T) {
			baza, s := novaBazaOdrzavanja(t)
			podrucje := podrucjeOdrzavanja
			if sl.drugo {
				podrucje = drugoPodrucje
			}
			id := dodajLokaciju(t, s, podrucje)
			if sl.id != "" {
				id = sl.id
			}
			if got := tekstGreske(s.LinkWater(ctx, sl.prava, podrucjeOdrzavanja, id, sl.cilj)); got != sl.greska {
				t.Fatalf("greška %q, a treba %q", got, sl.greska)
			}
			m, err := s.repo.GetWater(ctx, dodanaLokacija(sl.drugo))
			if err != nil || m == nil {
				t.Fatalf("lokacija %v, %v", m, err)
			}
			if m.WatercourseCode != sl.voda || m.StructureID != sl.objekt {
				t.Errorf("veza %q/%q, a treba %q/%q", m.WatercourseCode, m.StructureID, sl.voda, sl.objekt)
			}
			verzija := len(stanjeOdrzavanja(t, baza)) == 3
			if verzija != sl.verzija {
				t.Errorf("nova verzija %t, a treba %t", verzija, sl.verzija)
			}
		})
	}
}

// dodanaLokacija je identitet lokacije koju je upisao dodajLokaciju
func dodanaLokacija(drugo bool) string {
	if drugo {
		return repository.MaintainedWaterID(drugoPodrucje.ID, "Kanal Probni")
	}
	return repository.MaintainedWaterID(podrucjeOdrzavanja.ID, "Kanal Probni")
}

// Svako vezanje, i na istu vodu, upisuje novu verziju koja zamjenjuje prethodnu
func TestLinkWaterDvaput(t *testing.T) {
	ctx := context.Background()
	baza, s := novaBazaOdrzavanja(t)
	id := dodajLokaciju(t, s, podrucjeOdrzavanja)
	for range 2 {
		if err := s.LinkWater(ctx, pravoOdrzavanja, podrucjeOdrzavanja, id, "N-PROBNI"); err != nil {
			t.Fatal(err)
		}
	}
	st := stanjeOdrzavanja(t, baza)
	if len(st) != 4 || !strings.Contains(st[0], nasipProbniID) || !strings.Contains(st[3], "|true|false|") {
		t.Errorf("stanje %v", st)
	}
}

// Greška repozitorija vraća se kako jest, a veza ostaje stara
func TestLinkWaterGreskeRepozitorija(t *testing.T) {
	ctx := context.Background()
	slucajevi := []struct {
		ime, kvar, cilj, greska string
	}{
		{"čitanje popisa", "popis", "", "no such table: maintained_waters"},
		{"čitanje voda", "vode", "N-PROBNI", "greška pri dohvaćanju vodnog tijela"},
		{"čitanje objekata", "objekti", "N-PROBNI", "no such column: notes"},
		{"upis", "upis", "N-PROBNI", "upis zabranjen"},
	}
	for _, sl := range slucajevi {
		t.Run(sl.ime, func(t *testing.T) {
			baza, s := novaBazaOdrzavanja(t)
			id := dodajLokaciju(t, s, podrucjeOdrzavanja)
			pokvari(t, baza, sl.kvar)
			err := s.LinkWater(ctx, pravoOdrzavanja, podrucjeOdrzavanja, id, sl.cilj)
			if err == nil || !strings.Contains(err.Error(), sl.greska) {
				t.Errorf("greška %v, a treba %q", err, sl.greska)
			}
			// bez tablice popisa nema se što pročitati
			if sl.kvar == "popis" {
				return
			}
			m, err := s.repo.GetWater(ctx, id)
			if err != nil || m == nil || m.WatercourseCode != "V-PRIMJERICA" || m.StructureID != "" {
				t.Errorf("veza se promijenila: %+v, %v", m, err)
			}
			if st := stanjeOdrzavanja(t, baza); len(st) != 2 {
				t.Errorf("treba ostati jedan red i jedna verzija: %v", st)
			}
		})
	}
}
