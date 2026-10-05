package service

import (
	"context"
	"database/sql"
	"math"
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

// Testovi ReadingService: provjera očitanja, upis, izmjena i brisanje s
// pravima, pregled svih letvi i terenski pogled. Podaci su izmišljeni:
// sektor P (područja 1 i 2) i sektor Q (područje 3), letve Primjerovo,
// Probno i Uzvodna, a jedina osoba je Pero Perić (pperic).

type okolinaCitanja struct {
	baza                        *sql.DB
	rs                          *ReadingService
	readings                    *repository.ReadingRepository
	users                       *repository.UserRepository
	primjerovo, probno, uzvodna *models.Station
	dvije                       *models.Station // na dionicama područja 1 i 2
	csProbni, nasip, ustava     *models.Structure
	adminOvl                    *models.UserPermissions
}

func novaOkolinaCitanja(t *testing.T) *okolinaCitanja {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "citanja.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo'), ('Q', 'Sektor Q', 'VGO Drugdje', 'COP Drugdje')`,
		// bez podcentra: stupac ostaje NULL, a popis područja ga čita kao prazan
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica'),
			(2, 'P', 'Mali sliv Probni', 'VGI Probni'), (3, 'Q', 'Mali sliv Drugdje', 'VGI Drugdje')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES
			('P.1.1', 1, 'P', 'rijeka Primjerica', '2026-01-01', '2026-01-01'),
			('P.1.2', 1, 'P', 'kanal Probni', '2026-01-01', '2026-01-01'),
			('P.2.1', 2, 'P', 'potok Probni', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	o := &okolinaCitanja{baza: baza, readings: repository.NewReadingRepository(baza, rec), users: repository.NewUserRepository(baza, rec)}
	stations := repository.NewStationRepository(baza, rec)
	structures := repository.NewStructureRepository(baza, rec)
	sections := NewSectionService(repository.NewSectionRepository(baza, rec), NewSSEBroker())
	o.rs = NewReadingService(o.readings, stations, structures, sections, NewUserService(o.users, nil, NewSSEBroker()))

	ctx := context.Background()
	letva := func(sifra, naziv, voda, stacionaza string, pragovi bool, dionice ...string) *models.Station {
		st := &models.Station{ID: uuid.New(), Code: sifra, Name: naziv, Watercourse: voda, Stationing: stacionaza}
		if pragovi {
			st.Prep, st.Regular, st.Emergency, st.State = pragCm(300), pragCm(500), pragCm(650), pragCm(800)
		}
		if err := stations.CreateStation(ctx, st); err != nil {
			t.Fatal(err)
		}
		for _, d := range dionice {
			if _, err := baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, ?, ?, ?)`,
				uuid.NewString(), d, st.ID.String(), time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		st.SectionCodes = dionice
		return st
	}
	o.primjerovo = letva("primjerovo", "Primjerovo", "Primjerica", "rkm 12+300", true, "P.1.1")
	o.probno = letva("probno", "Probno", "Probni", "", false, "P.2.1")
	o.uzvodna = letva("uzvodna", "Uzvodna", "", "", false)
	o.dvije = letva("dvije", "Granica", "", "", false, "P.1.2", "P.2.1")
	objekt := func(sifra, naziv, vrsta string, podrucje int, letva *models.Station) *models.Structure {
		s := &models.Structure{Code: sifra, Name: naziv, Kind: vrsta, SectorID: "P", AreaID: podrucje, Origin: "RUČNI_UNOS"}
		if letva != nil {
			s.StationID = letva.ID.String()
		}
		if err := structures.CreateStructure(ctx, s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	o.csProbni = objekt("cs-probni", "CS Probni", models.StructureKindPumpingStation, 1, o.primjerovo)
	o.nasip = objekt("nasip-probni", "Nasip Probni", models.StructureKindEmbankment, 1, nil)
	o.ustava = objekt("ustava-probna", "Ustava Probna", models.StructureKindSluice, 2, nil)
	o.adminOvl = &models.UserPermissions{IsGlobalAdmin: true, User: models.User{ID: uuid.New(), FullName: "Uprava"}}
	return o
}

// pperic otvara račun Pere Perića s jednom dužnošću (nil: bez dužnosti)
func (o *okolinaCitanja) pperic(t *testing.T, korisnicko string, d *models.Duty) (*models.User, *models.UserPermissions) {
	t.Helper()
	u := &models.User{ID: uuid.New(), Username: korisnicko, FullName: "Pero Perić", IsActive: true}
	if err := o.users.CreateUser(u, d); err != nil {
		t.Fatal(err)
	}
	u, err := o.users.GetUserByID(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return u, models.NewUserPermissions(*u)
}

func dionicaDuznost(dionice string, podrucje int) *models.Duty {
	sektor := "P"
	return &models.Duty{Title: "Rukovoditelj dionice", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection,
		SectorID: &sektor, AreaID: &podrucje, SectionCodes: dionice, IsPrimary: true}
}

func (o *okolinaCitanja) upisi(t *testing.T, rd models.Reading) models.Reading {
	t.Helper()
	if rd.ID == uuid.Nil {
		rd.ID = uuid.New()
	}
	if rd.Source == "" {
		rd.Source = models.ReadingSourceManual
	}
	if err := o.readings.Create(context.Background(), &rd); err != nil {
		t.Fatal(err)
	}
	return rd
}

func cmP(v int) *int           { return &v }
func brojP(v float64) *float64 { return &v }

func TestProvjeraOcitanja(t *testing.T) {
	o := novaOkolinaCitanja(t)
	sad := time.Now()
	letva := o.primjerovo.ID.String()
	osnovno := func(izmjena func(*models.Reading)) *models.Reading {
		rd := &models.Reading{StationID: letva, MeasuredAt: sad.Add(-time.Hour), LevelCm: cmP(120)}
		if izmjena != nil {
			izmjena(rd)
		}
		return rd
	}
	slucajevi := []struct {
		ime    string
		izmj   func(*models.Reading)
		greska string // prazno: prolazi
	}{
		{"ispravno", nil, ""},
		{"i letva i objekt", func(r *models.Reading) { r.StructureID = o.csProbni.ID.String() }, "ili postaji ili objektu"},
		{"ni letva ni objekt", func(r *models.Reading) { r.StationID = "" }, "ili postaji ili objektu"},
		{"bez vremena", func(r *models.Reading) { r.MeasuredAt = time.Time{} }, "obavezno"},
		// do sat unaprijed je dopušteno (razlika satova), a poruka kaže „budućnost”
		{"59 min unaprijed", func(r *models.Reading) { r.MeasuredAt = sad.Add(59 * time.Minute) }, ""},
		{"61 min unaprijed", func(r *models.Reading) { r.MeasuredAt = sad.Add(61 * time.Minute) }, "budućnosti"},
		{"1899.", func(r *models.Reading) { r.MeasuredAt = time.Date(1899, 12, 31, 7, 0, 0, 0, models.Zagreb) }, "vjerojatno"},
		{"1900.", func(r *models.Reading) { r.MeasuredAt = time.Date(1900, 1, 1, 7, 0, 0, 0, time.UTC) }, ""},
		{"bez ičega", func(r *models.Reading) { r.LevelCm = nil }, "bar napomenu"},
		{"napomena od razmaka", func(r *models.Reading) { r.LevelCm, r.Note = nil, "   " }, "bar napomenu"},
		{"samo napomena", func(r *models.Reading) { r.LevelCm, r.Note = nil, "letva pod ledom" }, ""},
		{"samo stanje objekta", func(r *models.Reading) {
			r.StationID, r.StructureID, r.LevelCm, r.StructureState = "", o.csProbni.ID.String(), nil, models.StructureStateIdle
		}, ""},
		{"samo zapornica", func(r *models.Reading) {
			r.StationID, r.StructureID, r.LevelCm, r.Gate = "", o.ustava.ID.String(), nil, models.GateClosed
		}, ""},
		{"samo drugi vodostaj", func(r *models.Reading) { r.LevelCm, r.Level2Cm = nil, cmP(40) }, ""},
		{"vodostaj -500", func(r *models.Reading) { r.LevelCm = cmP(-500) }, ""},
		{"vodostaj -501", func(r *models.Reading) { r.LevelCm = cmP(-501) }, "izvan razumnog raspona"},
		{"vodostaj 3000", func(r *models.Reading) { r.LevelCm = cmP(3000) }, ""},
		{"vodostaj 3001", func(r *models.Reading) { r.LevelCm = cmP(3001) }, "3001 cm"},
		{"drugi vodostaj 3001", func(r *models.Reading) { r.Level2Cm = cmP(3001) }, "3001 cm"},
		{"temperatura -5", func(r *models.Reading) { r.TempC = brojP(-5) }, ""},
		{"temperatura -5,1", func(r *models.Reading) { r.TempC = brojP(-5.1) }, "temperatura"},
		{"temperatura 45", func(r *models.Reading) { r.TempC = brojP(45) }, ""},
		{"temperatura 45,1", func(r *models.Reading) { r.TempC = brojP(45.1) }, "temperatura"},
		{"protok 0", func(r *models.Reading) { r.FlowM3s = brojP(0) }, ""},
		{"protok negativan", func(r *models.Reading) { r.FlowM3s = brojP(-0.1) }, "protok"},
		{"protok 100000", func(r *models.Reading) { r.FlowM3s = brojP(100000) }, ""},
		{"protok iznad", func(r *models.Reading) { r.FlowM3s = brojP(100000.1) }, "protok"},
		{"protok beskonačan", func(r *models.Reading) { r.FlowM3s = brojP(math.Inf(1)) }, "protok"},
		// NaN nije broj: ne prolazi ni kao temperatura ni kao protok
		{"temperatura NaN", func(r *models.Reading) { r.TempC = brojP(math.NaN()) }, "temperatura"},
		{"protok NaN", func(r *models.Reading) { r.FlowM3s = brojP(math.NaN()) }, "protok"},
		{"automatski", func(r *models.Reading) { r.Source = models.ReadingSourceAutomatic }, ""},
		{"uvoz", func(r *models.Reading) { r.Source = models.ReadingSourceImport }, ""},
		{"nepoznat način", func(r *models.Reading) { r.Source = "TELEPATIJA" }, "nepoznat način"},
		{"nepoznato stanje", func(r *models.Reading) {
			r.StationID, r.StructureID, r.StructureState = "", o.csProbni.ID.String(), "PLESE"
		}, "stanje crpne stanice"},
		{"nepoznata zapornica", func(r *models.Reading) {
			r.StationID, r.StructureID, r.Gate = "", o.ustava.ID.String(), "NAPOLA"
		}, "zapornice"},
		// stanje objekta i zapornica primaju se samo uz objekt
		{"stanje na postaji", func(r *models.Reading) { r.StructureState = models.StructureStateIdle }, "samo na objektu"},
		{"zapornica na postaji", func(r *models.Reading) { r.Gate = models.GateOpen }, "samo na objektu"},
		{"samo zapornica na postaji", func(r *models.Reading) { r.LevelCm, r.Gate = nil, models.GateClosed }, "samo na objektu"},
	}
	for _, s := range slucajevi {
		err := o.rs.validate(osnovno(s.izmj))
		switch {
		case s.greska == "" && err != nil:
			t.Errorf("%s: %v", s.ime, err)
		case s.greska != "" && (err == nil || !strings.Contains(err.Error(), s.greska)):
			t.Errorf("%s: očekivana greška s „%s”, dobiveno %v", s.ime, s.greska, err)
		}
	}

	// provjera i mijenja očitanje: prazan način postaje ručni, a napomena i
	// očitao se obrežu
	rd := osnovno(func(r *models.Reading) { r.Note, r.Observer = "  mutna voda \n", "  Pero Perić " })
	if err := o.rs.validate(rd); err != nil {
		t.Fatal(err)
	}
	if rd.Source != models.ReadingSourceManual || rd.Note != "mutna voda" || rd.Observer != "Pero Perić" {
		t.Errorf("nakon provjere: način %q, napomena %q, očitao %q", rd.Source, rd.Note, rd.Observer)
	}
	// upis na postaju sa stanjem objekta odbija se i kroz Create
	rd = osnovno(func(r *models.Reading) { r.StructureState, r.Gate = models.StructureStateIdle, models.GateOpen })
	if err := o.rs.Create(context.Background(), o.adminOvl, rd); err == nil || !strings.Contains(err.Error(), "samo na objektu") {
		t.Errorf("stanje i zapornica na postaji: %v", err)
	}
}

func TestUpisOcitanjaIPrava(t *testing.T) {
	o := novaOkolinaCitanja(t)
	ctx := context.Background()
	pp, ovl := o.pperic(t, "pperic", dionicaDuznost("P.1.1", 1))
	_, gost := o.pperic(t, "pperic-gost", &models.Duty{Title: "Gost", Role: models.RoleGuest, ScopeType: models.ScopeAll})
	sad := time.Now().Add(-time.Hour)
	novo := func(letva *models.Station) *models.Reading {
		return &models.Reading{StationID: letva.ID.String(), MeasuredAt: sad, LevelCm: cmP(150)}
	}

	// na svojoj dionici upisuje, s tragom tko je upisao
	rd := novo(o.primjerovo)
	if err := o.rs.Create(ctx, ovl, rd); err != nil {
		t.Fatal(err)
	}
	if rd.UserID != pp.ID.String() || rd.Observer != "Pero Perić" || rd.Origin != models.ReadingOriginGoCOP || rd.Source != models.ReadingSourceManual {
		t.Errorf("trag upisa: korisnik %q, očitao %q, podrijetlo %q, način %q", rd.UserID, rd.Observer, rd.Origin, rd.Source)
	}
	// zadani očitao i podrijetlo ostaju
	rd = novo(o.primjerovo)
	rd.MeasuredAt, rd.Observer, rd.Origin = sad.Add(-time.Hour), "Pero Perić (vodočuvar)", "TELEFON"
	if err := o.rs.Create(ctx, ovl, rd); err != nil || rd.Observer != "Pero Perić (vodočuvar)" || rd.Origin != "TELEFON" {
		t.Errorf("zadani očitao i podrijetlo: %q %q %v", rd.Observer, rd.Origin, err)
	}

	odbijeno := func(ime string, perms *models.UserPermissions, rd *models.Reading, poruka string) {
		t.Helper()
		if err := o.rs.Create(ctx, perms, rd); err == nil || !strings.Contains(err.Error(), poruka) {
			t.Errorf("%s: očekivano „%s”, dobiveno %v", ime, poruka, err)
		}
	}
	odbijeno("tuđa dionica", ovl, novo(o.probno), "nemate pravo upisivati očitanja na Probno")
	odbijeno("gost", gost, novo(o.primjerovo), "nemate pravo")
	odbijeno("bez ovlasti", nil, novo(o.primjerovo), "nemate pravo")
	odbijeno("neispravna letva", ovl, &models.Reading{StationID: "nije-uuid", MeasuredAt: sad, LevelCm: cmP(1)}, "neispravna postaja")
	odbijeno("nepoznata letva", ovl, &models.Reading{StationID: uuid.NewString(), MeasuredAt: sad, LevelCm: cmP(1)}, "postaja ne postoji")
	odbijeno("neispravan objekt", ovl, &models.Reading{StructureID: "nije-uuid", MeasuredAt: sad, LevelCm: cmP(1)}, "neispravan objekt")
	odbijeno("nepoznat objekt", ovl, &models.Reading{StructureID: uuid.NewString(), MeasuredAt: sad, LevelCm: cmP(1)}, "objekt ne postoji")
	// provjera prava ide prije provjere unosa, kao kod izmjene: gost ne
	// dozna pravila unosa
	odbijeno("gost s neispravnim unosom", gost, &models.Reading{StationID: o.probno.ID.String(), MeasuredAt: sad}, "nemate pravo")
	odbijeno("gost s neispravnim unosom na objektu", gost, &models.Reading{StructureID: o.csProbni.ID.String(), MeasuredAt: sad}, "nemate pravo")
	// tko ima pravo, dobije grešku unosa
	odbijeno("neispravan unos", ovl, &models.Reading{StationID: o.primjerovo.ID.String(), MeasuredAt: sad}, "bar napomenu")
	odbijeno("i letva i objekt", ovl, &models.Reading{StationID: o.primjerovo.ID.String(), StructureID: o.csProbni.ID.String(),
		MeasuredAt: sad, LevelCm: cmP(1)}, "ili postaji ili objektu")

	// Letva bez dionica prima očitanje od svakoga tko igdje piše, i s
	// dužnošću u drugom sektoru.
	if err := o.rs.Create(ctx, ovl, novo(o.uzvodna)); err != nil {
		t.Errorf("letva bez dionica: %v", err)
	}
	if err := o.rs.Create(ctx, gost, novo(o.uzvodna)); err == nil {
		t.Error("gost ne piše ni na letvi bez dionica")
	}
	// letva na dvije dionice: dovoljna je jedna
	_, ovl2 := o.pperic(t, "pperic-p21", dionicaDuznost("P.2.1", 2))
	if err := o.rs.Create(ctx, ovl2, novo(o.dvije)); err != nil {
		t.Errorf("letva na dvije dionice, pravo na jednoj: %v", err)
	}

	// objekt: na dionici P.1.1 radi se u području 1, pa objekt područja 1
	// bez dionica prima očitanje; objekt područja 2 ne
	if err := o.rs.Create(ctx, ovl, &models.Reading{StructureID: o.csProbni.ID.String(), MeasuredAt: sad, LevelCm: cmP(90)}); err != nil {
		t.Errorf("objekt svog područja bez dionica: %v", err)
	}
	odbijeno("objekt drugog područja", ovl, &models.Reading{StructureID: o.ustava.ID.String(), MeasuredAt: sad, Gate: models.GateOpen}, "nemate pravo upisivati očitanja na Ustava Probna")
}

func TestPravoUpisaNaObjekt(t *testing.T) {
	o := novaOkolinaCitanja(t)
	_, dionicar := o.pperic(t, "pperic", dionicaDuznost("P.1.1", 1))
	sektor, podrucje := "P", 1
	_, uprava := o.pperic(t, "pperic-uprava", &models.Duty{Title: "Rukovoditelj područja", Role: models.RoleAreaLeader,
		ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje, IsPrimary: true})
	objekt := func(podrucje int, dionice ...string) *models.Structure {
		return &models.Structure{SectorID: "P", AreaID: podrucje, SectionCodes: dionice}
	}
	for _, s := range []struct {
		ime    string
		perms  *models.UserPermissions
		objekt *models.Structure
		smije  bool
	}{
		{"uprava područja, objekt područja", uprava, objekt(1, "P.1.2"), true},
		{"uprava područja, objekt drugog područja", uprava, objekt(2), false},
		{"dionica objekta", dionicar, objekt(1, "P.1.1"), true},
		// objekt na tuđoj dionici istog područja
		{"druga dionica istog područja", dionicar, objekt(1, "P.1.2"), false},
		{"objekt područja bez dionica", dionicar, objekt(1), true},
		// objekt drugog područja koji stoji na mojoj dionici vodi njegovo područje
		{"objekt drugog područja na mojoj dionici", dionicar, objekt(2, "P.1.1"), false},
		{"globalni administrator", o.adminOvl, objekt(3), true},
		{"bez ovlasti", nil, objekt(1), false},
	} {
		if got := o.rs.CanRecordStructure(s.perms, s.objekt); got != s.smije {
			t.Errorf("%s: %v, očekivano %v", s.ime, got, s.smije)
		}
	}
	if o.rs.CanRecordStructure(o.adminOvl, nil) || o.rs.CanRecordStation(o.adminOvl, nil) {
		t.Error("nepostojeći objekt ili letva")
	}
}

func TestIzmjenaIBrisanjeOcitanja(t *testing.T) {
	o := novaOkolinaCitanja(t)
	ctx := context.Background()
	pp, ovl := o.pperic(t, "pperic", dionicaDuznost("P.1.1", 1))
	_, drugi := o.pperic(t, "pperic-p21", dionicaDuznost("P.2.1", 2))
	sad := time.Now().Add(-2 * time.Hour)
	rd := o.upisi(t, models.Reading{StationID: o.primjerovo.ID.String(), MeasuredAt: sad, LevelCm: cmP(150),
		UserID: pp.ID.String(), Origin: "TELEFON", SourceRef: "poziv-1"})

	// Izmjena ne mijenja letvu, podrijetlo, oznaku izvora ni autora, iako ih
	// zahtjev nosi drukčije.
	izmjena := models.Reading{ID: rd.ID, StationID: o.probno.ID.String(), MeasuredAt: sad, LevelCm: cmP(155),
		Origin: "LAŽNO", SourceRef: "drugo", UserID: uuid.NewString()}
	if err := o.rs.Update(ctx, ovl, &izmjena); err != nil {
		t.Fatal(err)
	}
	sada, _ := o.readings.Get(ctx, rd.ID)
	if sada.StationID != o.primjerovo.ID.String() || *sada.LevelCm != 155 || sada.Origin != "TELEFON" || sada.SourceRef != "poziv-1" || sada.UserID != pp.ID.String() {
		t.Errorf("nakon izmjene: %+v", sada)
	}

	// autor smije mijenjati svoje očitanje i kad više nema prava na letvi
	if _, err := o.baza.Exec(`UPDATE duties SET is_active = 0 WHERE user_id = ?`, pp.ID.String()); err != nil {
		t.Fatal(err)
	}
	bezDuznosti, _ := o.users.GetUserByID(pp.ID)
	autorOvl := models.NewUserPermissions(*bezDuznosti)
	if err := o.rs.Update(ctx, autorOvl, &models.Reading{ID: rd.ID, MeasuredAt: sad, LevelCm: cmP(156)}); err != nil {
		t.Errorf("autor bez dužnosti: %v", err)
	}

	// tuđe očitanje na tuđoj letvi ne
	if err := o.rs.Update(ctx, drugi, &models.Reading{ID: rd.ID, MeasuredAt: sad, LevelCm: cmP(1)}); err == nil || !strings.Contains(err.Error(), "nemate pravo mijenjati") {
		t.Errorf("tuđa izmjena: %v", err)
	}
	// provjera unosa ide nakon provjere prava, kao kod upisa
	if err := o.rs.Update(ctx, autorOvl, &models.Reading{ID: rd.ID, MeasuredAt: sad}); err == nil || !strings.Contains(err.Error(), "bar napomenu") {
		t.Errorf("neispravna izmjena: %v", err)
	}
	if err := o.rs.Update(ctx, drugi, &models.Reading{ID: rd.ID, MeasuredAt: sad}); err == nil || !strings.Contains(err.Error(), "nemate pravo mijenjati") {
		t.Errorf("tuđa neispravna izmjena: %v", err)
	}
	if err := o.rs.Update(ctx, ovl, &models.Reading{ID: uuid.New(), MeasuredAt: sad, LevelCm: cmP(1)}); err == nil || !strings.Contains(err.Error(), "ne postoji") {
		t.Errorf("izmjena nepostojećeg: %v", err)
	}

	// brisanje: tuđe ne, svoje da, i vrati obrisano
	if _, err := o.rs.Delete(ctx, drugi, rd.ID); err == nil || !strings.Contains(err.Error(), "nemate pravo brisati") {
		t.Errorf("tuđe brisanje: %v", err)
	}
	obrisano, err := o.rs.Delete(ctx, autorOvl, rd.ID)
	if err != nil || obrisano == nil || obrisano.ID != rd.ID {
		t.Fatalf("brisanje: %v %v", obrisano, err)
	}
	if ostalo, _ := o.readings.Get(ctx, rd.ID); ostalo != nil {
		t.Error("očitanje je i dalje tu")
	}
	if _, err := o.rs.Delete(ctx, autorOvl, rd.ID); err == nil || !strings.Contains(err.Error(), "ne postoji") {
		t.Errorf("drugo brisanje: %v", err)
	}

	// CanEdit: očitanje s neispravnom letvom ili objektom nitko osim autora
	// i administratora ne dira
	if o.rs.CanEdit(ctx, drugi, &models.Reading{StationID: "nije-uuid"}) || o.rs.CanEdit(ctx, drugi, &models.Reading{StructureID: "nije-uuid"}) {
		t.Error("neispravna letva ili objekt")
	}
	if !o.rs.CanEdit(ctx, o.adminOvl, &models.Reading{StationID: "nije-uuid"}) || o.rs.CanEdit(ctx, nil, &models.Reading{}) {
		t.Error("administrator smije, bez ovlasti ne")
	}
}

func TestZalijepljenaOcitanja(t *testing.T) {
	o := novaOkolinaCitanja(t)
	ctx := context.Background()
	_, ovl := o.pperic(t, "pperic", dionicaDuznost("P.1.1", 1))
	pocetak := time.Date(2026, 3, 1, 7, 0, 0, 0, models.Zagreb)
	niz := func() []models.Reading {
		var out []models.Reading
		for i := 0; i < 3; i++ {
			kad := pocetak.Add(time.Duration(i) * 24 * time.Hour)
			out = append(out, models.Reading{ID: db.StableID("zalijepljeno", kad.String()), StationID: o.primjerovo.ID.String(),
				MeasuredAt: kad, LevelCm: cmP(200 + i)})
		}
		return out
	}
	if n, err := o.rs.UveziZalijepljena(ctx, ovl, o.primjerovo, niz()); err != nil || n != 3 {
		t.Fatalf("prvi uvoz: %d %v", n, err)
	}
	// ponovni uvoz istog niza ništa ne udvostručuje
	if n, err := o.rs.UveziZalijepljena(ctx, ovl, o.primjerovo, niz()); err != nil || n != 0 {
		t.Errorf("ponovni uvoz: %d %v", n, err)
	}
	// jedno neispravno očitanje odbija cijeli niz, s trenutkom u poruci
	los := niz()
	los[1].LevelCm = cmP(5000)
	if _, err := o.rs.UveziZalijepljena(ctx, ovl, o.primjerovo, los); err == nil || !strings.HasPrefix(err.Error(), "2.3.2026. 07:00:") {
		t.Errorf("neispravan niz: %v", err)
	}
	if _, err := o.rs.UveziZalijepljena(ctx, ovl, o.probno, niz()); err == nil || !strings.Contains(err.Error(), "nemate pravo") {
		t.Errorf("tuđa letva: %v", err)
	}
	if _, err := o.rs.UveziZalijepljena(ctx, ovl, nil, niz()); err == nil {
		t.Error("bez letve")
	}
}

func TestPregledSvihLetvi(t *testing.T) {
	o := novaOkolinaCitanja(t)
	ctx := context.Background()
	sad := time.Now().Add(-time.Hour)
	// Primjerovo: dva očitanja, zadnje u redovnoj obrani
	o.upisi(t, models.Reading{StationID: o.primjerovo.ID.String(), MeasuredAt: sad.Add(-3 * time.Hour), LevelCm: cmP(480)})
	o.upisi(t, models.Reading{StationID: o.primjerovo.ID.String(), MeasuredAt: sad, LevelCm: cmP(510)})
	// Probno: novije očitanje, ali bez pragova
	o.upisi(t, models.Reading{StationID: o.probno.ID.String(), MeasuredAt: sad.Add(30 * time.Minute), LevelCm: cmP(90)})
	// Uzvodna: samo napomena, bez vodostaja
	o.upisi(t, models.Reading{StationID: o.uzvodna.ID.String(), MeasuredAt: sad.Add(-time.Hour), Note: "letva pod ledom"})
	// CS Probni: pragovi vodomjera Primjerovo, 320 je pripremno stanje
	o.upisi(t, models.Reading{StructureID: o.csProbni.ID.String(), MeasuredAt: sad.Add(-2 * time.Hour), LevelCm: cmP(320)})

	pregled, err := o.rs.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var redom []string
	po := map[string]models.GaugeSummary{}
	for _, g := range pregled {
		redom = append(redom, g.Name)
		po[g.Name] = g
	}
	// nasip ne prima očitanja; poredak: s očitanjem prije bez, viši stupanj
	// prije nižeg, novije prije starijeg, pa po imenu
	ocekivano := "Primjerovo,CS Probni,Probno,Uzvodna,Granica,Ustava Probna"
	if strings.Join(redom, ",") != ocekivano {
		t.Errorf("poredak %s, očekivano %s", strings.Join(redom, ","), ocekivano)
	}
	p := po["Primjerovo"]
	if p.Count != 2 || p.Latest == nil || *p.Latest.LevelCm != 510 || p.Previous == nil || *p.Previous.LevelCm != 480 || p.Phase != models.PhaseRegular {
		t.Errorf("Primjerovo: broj %d, zadnje %v, prethodno %v, stupanj %s", p.Count, p.Latest, p.Previous, p.Phase)
	}
	if p.Sub != "Primjerica · rkm 12+300" || p.Kind != "POSTAJA" || p.URL != "/readings/station/"+o.primjerovo.ID.String() ||
		p.NewURL != "/readings/new?station="+o.primjerovo.ID.String() || strings.Join(p.SectorIDs, ",") != "P" || len(p.AreaIDs) != 1 || p.AreaIDs[0] != 1 {
		t.Errorf("Primjerovo: %+v", p)
	}
	if g := po["Probno"]; g.Phase != models.PhaseUnknown || g.Sub != "Probni" || g.Previous != nil {
		t.Errorf("Probno: %+v", g)
	}
	// očitanje bez vodostaja: stupanj se ne zna
	if g := po["Uzvodna"]; g.Phase != models.PhaseUnknown || g.Sub != "" || g.Count != 1 || len(g.AreaIDs) != 0 {
		t.Errorf("Uzvodna: %+v", g)
	}
	if g := po["Granica"]; g.Count != 0 || g.Latest != nil || len(g.AreaIDs) != 2 {
		t.Errorf("Granica: %+v", g)
	}
	cs := po["CS Probni"]
	if cs.Phase != models.PhasePrep || cs.Kind != models.StructureKindPumpingStation || cs.Sub != "crpna stanica · BP 1" ||
		cs.AreaID != 1 || cs.SectorID != "P" || cs.NewURL != "/readings/new?structure="+o.csProbni.ID.String() {
		t.Errorf("CS Probni: %+v", cs)
	}
	if g := po["Ustava Probna"]; g.Phase != models.PhaseUnknown || g.Sub != "ustava · BP 2" {
		t.Errorf("ustava bez vodomjera: %+v", g)
	}
	if _, ima := po["Nasip Probni"]; ima {
		t.Error("nasip ne smije biti među letvama")
	}
}

func TestTerenskiPogled(t *testing.T) {
	o := novaOkolinaCitanja(t)
	ctx := context.Background()
	pp, ovl := o.pperic(t, "pperic", dionicaDuznost("P.1.1", 1))
	imenjak, _ := o.pperic(t, "pperic-imenjak", dionicaDuznost("P.2.1", 2))
	danas := time.Now()
	prije := func(dana int, sat, minuta int) time.Time {
		d := danas.In(models.Zagreb).AddDate(0, 0, -dana)
		return time.Date(d.Year(), d.Month(), d.Day(), sat, minuta, 0, 0, models.Zagreb)
	}
	// Pero Perić obično očitava Primjerovo oko 7:00, a CS Probni oko 6:30
	for i := 1; i <= 3; i++ {
		o.upisi(t, models.Reading{StationID: o.primjerovo.ID.String(), MeasuredAt: prije(i, 7, 0), LevelCm: cmP(200), UserID: pp.ID.String()})
		o.upisi(t, models.Reading{StructureID: o.csProbni.ID.String(), MeasuredAt: prije(i, 6, 30), LevelCm: cmP(100), UserID: pp.ID.String()})
	}
	// Imenjak (drugi račun, isto ime) očitava Probno u području 2; po imenu
	// to ulazi u navike prvoga.
	o.upisi(t, models.Reading{StationID: o.probno.ID.String(), MeasuredAt: prije(2, 8, 0), LevelCm: cmP(90),
		UserID: imenjak.ID.String(), Observer: "Pero Perić"})
	// danas je Primjerovo očitao netko drugi
	o.upisi(t, models.Reading{StationID: o.primjerovo.ID.String(), MeasuredAt: danas, LevelCm: cmP(205), Observer: "dežurni"})
	// starije od 90 dana se ne broji
	o.upisi(t, models.Reading{StationID: o.uzvodna.ID.String(), MeasuredAt: danas.AddDate(0, 0, -91), LevelCm: cmP(10), UserID: pp.ID.String()})

	fo, err := o.rs.FieldOverview(ctx, ovl, pp, 0)
	if err != nil {
		t.Fatal(err)
	}
	// područje je iz primarne dužnosti, a izbor samo područje dužnosti
	if fo.Area == nil || fo.Area.ID != 1 || len(fo.Areas) != 1 || fo.Areas[0].ID != 1 {
		t.Fatalf("područje %v, izbor %v", fo.Area, fo.Areas)
	}
	imena := func(gs []models.GaugeSummary) string {
		var s []string
		for _, g := range gs {
			s = append(s, g.Name)
		}
		return strings.Join(s, ",")
	}
	// moje: po uobičajenom vremenu; i letva drugog područja koju je očitao imenjak
	if got := imena(fo.Mine); got != "CS Probni,Primjerovo,Probno" {
		t.Errorf("moje letve: %s", got)
	}
	// ostale letve područja 1: letve dionica i objekti područja koji primaju očitanja
	if got := imena(fo.Others); got != "Granica" {
		t.Errorf("ostale letve: %s", got)
	}
	var primjerovo models.GaugeSummary
	for _, g := range fo.Mine {
		if g.Name == "Primjerovo" {
			primjerovo = g
		}
	}
	// „danas obavljeno” vrijedi i kad je očitao netko drugi
	if !primjerovo.DoneToday || primjerovo.Habit != 3 || primjerovo.UsualTime() != "07:00" {
		t.Errorf("Primjerovo: danas %v, navika %d, vrijeme %s", primjerovo.DoneToday, primjerovo.Habit, primjerovo.UsualTime())
	}
	if fo.Total != 3 || fo.Done != 1 {
		t.Errorf("obavljeno %d od %d", fo.Done, fo.Total)
	}

	// Zadano područje mora biti u izboru: Pero Perić traži područje 2, kojeg
	// nema u izboru, pa dobije prvo dopušteno (područje 1).
	fo2, err := o.rs.FieldOverview(ctx, ovl, pp, 2)
	if err != nil || fo2.Area == nil || fo2.Area.ID != 1 || len(fo2.Areas) != 1 {
		t.Fatalf("tuđe područje: %+v %v", fo2, err)
	}
	if got := imena(fo2.Others); got != "Granica" {
		t.Errorf("ostale letve umjesto područja 2: %s", got)
	}
	// uprava sektora P smije birati područje 2, pa ga i dobije
	sektor := "P"
	upravaSektora, upravaOvl := o.pperic(t, "pperic-sektor", &models.Duty{Title: "Rukovoditelj sektora", Role: models.RoleSectorLeader,
		ScopeType: models.ScopeSector, SectorID: &sektor, IsPrimary: true})
	if fu, err := o.rs.FieldOverview(ctx, upravaOvl, upravaSektora, 2); err != nil || fu.Area == nil || fu.Area.ID != 2 ||
		imena(fu.Others) != "Granica,Ustava Probna" {
		t.Errorf("uprava sektora, područje 2: %+v %v", fu, err)
	}

	// administrator bira među svim područjima, a bez dužnosti dobije prvo
	fa, err := o.rs.FieldOverview(ctx, o.adminOvl, &o.adminOvl.User, 0)
	if err != nil || len(fa.Areas) != 3 || fa.Area == nil || fa.Area.ID != 1 || len(fa.Mine) != 0 {
		t.Errorf("administrator: %+v %v", fa, err)
	}
	// uprava sektora P bira područja svog sektora
	fs, err := o.rs.FieldOverview(ctx, upravaOvl, upravaSektora, 0)
	if err != nil || len(fs.Areas) != 2 || fs.Area == nil || fs.Area.ID != 1 {
		t.Errorf("uprava sektora: izbor %v, područje %v, %v", fs.Areas, fs.Area, err)
	}
	// bez ovlasti i bez korisnika: nema izbora ni područja
	fn, err := o.rs.FieldOverview(ctx, nil, nil, 0)
	if err != nil || fn.Area != nil || len(fn.Areas) != 0 || len(fn.Mine)+len(fn.Others) != 0 {
		t.Errorf("bez ovlasti: %+v %v", fn, err)
	}
	// bez ovlasti ni dužnost ne daje područje
	if fd, err := o.rs.FieldOverview(ctx, nil, pp, 1); err != nil || fd.Area != nil || len(fd.Others) != 0 {
		t.Errorf("bez ovlasti, s dužnošću: %+v %v", fd, err)
	}
}

func TestTerenskiPogledVrijemeOkoPonoci(t *testing.T) {
	o := novaOkolinaCitanja(t)
	pp, ovl := o.pperic(t, "pperic", dionicaDuznost("P.1.1", 1))
	d := time.Now().In(models.Zagreb).AddDate(0, 0, -2)
	u := func(sat, minuta int) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), sat, minuta, 0, 0, models.Zagreb)
	}
	// Primjerovo uvijek oko ponoći (23:50 i 0:10), CS Probni u 6:00. Prosjek
	// minuta stavlja ponoćnu letvu na podne, pa ona ide iza jutarnje.
	o.upisi(t, models.Reading{StationID: o.primjerovo.ID.String(), MeasuredAt: u(23, 50), LevelCm: cmP(1), UserID: pp.ID.String()})
	o.upisi(t, models.Reading{StationID: o.primjerovo.ID.String(), MeasuredAt: u(0, 10), LevelCm: cmP(1), UserID: pp.ID.String()})
	o.upisi(t, models.Reading{StructureID: o.csProbni.ID.String(), MeasuredAt: u(6, 0), LevelCm: cmP(1), UserID: pp.ID.String()})
	fo, err := o.rs.FieldOverview(context.Background(), ovl, pp, 0)
	if err != nil || len(fo.Mine) != 2 {
		t.Fatalf("%+v %v", fo, err)
	}
	if fo.Mine[0].Name != "CS Probni" || fo.Mine[1].UsualTime() != "12:00" {
		t.Errorf("poredak %s (%s), %s (%s)", fo.Mine[0].Name, fo.Mine[0].UsualTime(), fo.Mine[1].Name, fo.Mine[1].UsualTime())
	}
}

func TestFazaZaOcitanje(t *testing.T) {
	o := novaOkolinaCitanja(t)
	if o.rs.PhaseFor(nil, cmP(600)) != models.PhaseUnknown || o.rs.PhaseFor(o.primjerovo, nil) != models.PhaseUnknown {
		t.Error("bez letve ili vodostaja stupanj se ne zna")
	}
	if o.rs.PhaseFor(o.primjerovo, cmP(650)) != models.PhaseEmergency {
		t.Error("650 cm na Primjerovu je izvanredna obrana")
	}
	var prazan *ReadingService
	if _, ima := prazan.PrvoOcitanje(context.Background(), o.primjerovo.ID.String()); ima || prazan.Krajnosti(context.Background(), "") != nil {
		t.Error("servis bez spremišta ne zna ni prvo očitanje ni krajnosti")
	}
}

func TestPravoUpisaPrekoDionice(t *testing.T) {
	o := novaOkolinaCitanja(t)
	ctx := context.Background()
	sektor, podrucje := "P", 1
	_, uprava := o.pperic(t, "pperic", &models.Duty{Title: "Rukovoditelj područja", Role: models.RoleAreaLeader,
		ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje, IsPrimary: true})
	// uprava područja 1 upisuje na letvu dionice svog područja, ne i
	// drugog, a letva na dionicama oba područja je njezina
	if !o.rs.CanRecordStation(uprava, o.primjerovo) || o.rs.CanRecordStation(uprava, o.probno) || !o.rs.CanRecordStation(uprava, o.dvije) {
		t.Error("uprava područja i letve dionica")
	}
	// dionica letve koje nema u registru ne daje pravo
	nepostojeca := &models.Station{Name: "Bez dionice", SectionCodes: []string{"P.9.9"}}
	if o.rs.CanRecordStation(uprava, nepostojeca) {
		t.Error("nepostojeća dionica")
	}
	if !o.rs.CanRecordStation(o.adminOvl, o.probno) || o.rs.CanRecordStation(nil, o.uzvodna) {
		t.Error("administrator i bez ovlasti")
	}
	if hasAnyWriteRight(nil) || hasAnyWriteRight(&models.UserPermissions{}) {
		t.Error("prazne ovlasti ne pišu nigdje")
	}
	// tuđe očitanje na objektu svog područja uprava mijenja
	rd := o.upisi(t, models.Reading{StructureID: o.csProbni.ID.String(), MeasuredAt: time.Now().Add(-time.Hour), LevelCm: cmP(100), UserID: uuid.NewString()})
	if !o.rs.CanEdit(ctx, uprava, &rd) {
		t.Error("uprava područja mijenja očitanje objekta svog područja")
	}
	if o.rs.CanEdit(ctx, uprava, &models.Reading{StructureID: uuid.NewString()}) {
		t.Error("objekt kojeg nema")
	}
}

func TestCitanjeOcitanja(t *testing.T) {
	o := novaOkolinaCitanja(t)
	ctx := context.Background()
	prvo := time.Date(2026, 2, 1, 7, 0, 0, 0, models.Zagreb)
	a := o.upisi(t, models.Reading{StationID: o.primjerovo.ID.String(), MeasuredAt: prvo, LevelCm: cmP(150)})
	o.upisi(t, models.Reading{StationID: o.primjerovo.ID.String(), MeasuredAt: prvo.AddDate(0, 0, 1), LevelCm: cmP(610)})
	o.upisi(t, models.Reading{StationID: o.probno.ID.String(), MeasuredAt: prvo.AddDate(0, 0, 2), LevelCm: cmP(5)})

	if got, err := o.rs.Get(ctx, a.ID); err != nil || got == nil || *got.LevelCm != 150 {
		t.Errorf("Get: %v %v", got, err)
	}
	lista, err := o.rs.List(ctx, repository.ReadingFilter{StationID: o.primjerovo.ID.String()})
	if err != nil || len(lista) != 2 || *lista[0].LevelCm != 610 {
		t.Errorf("List (najnovije prvo): %v %v", lista, err)
	}
	if n, od, do, err := o.rs.Stats(ctx); err != nil || n != 3 || !od.Equal(prvo) || !do.Equal(prvo.AddDate(0, 0, 2)) {
		t.Errorf("Stats: %d %v %v %v", n, od, do, err)
	}
	if kad, ima := o.rs.PrvoOcitanje(ctx, o.primjerovo.ID.String()); !ima || !kad.Equal(prvo) {
		t.Errorf("PrvoOcitanje: %v %v", kad, ima)
	}
	if k := o.rs.Krajnosti(ctx, o.primjerovo.ID.String()); len(k) == 0 {
		t.Error("Krajnosti: prazno")
	}
}

func TestPregledBezDijelaBaze(t *testing.T) {
	ctx := context.Background()
	// Pregled ne vraća djelomičan popis: greška bilo kojeg čitanja vraća
	// samo grešku. Tablica se preimenuje, pa njezino čitanje ne uspije.
	for _, tablica := range []string{"readings", "stations", "section_stations", "structures"} {
		o := novaOkolinaCitanja(t)
		if _, err := o.baza.Exec(`ALTER TABLE ` + tablica + ` RENAME TO nema_` + tablica); err != nil {
			t.Fatal(err)
		}
		if g, err := o.rs.Overview(ctx); err == nil || g != nil {
			t.Errorf("bez tablice %s: %d letvi, %v", tablica, len(g), err)
		}
		// terenski pogled javlja istu grešku
		if fo, err := o.rs.FieldOverview(ctx, o.adminOvl, nil, 1); err == nil || fo != nil {
			t.Errorf("terenski pogled bez tablice %s: %v", tablica, err)
		}
	}
	o := novaOkolinaCitanja(t)
	if _, err := o.baza.Exec(`ALTER TABLE areas RENAME TO nema_areas`); err != nil {
		t.Fatal(err)
	}
	if _, err := o.rs.FieldOverview(ctx, o.adminOvl, nil, 1); err == nil {
		t.Error("terenski pogled bez područja")
	}
	// izmjena i brisanje bez tablice očitanja javljaju grešku spremišta
	o = novaOkolinaCitanja(t)
	if _, err := o.baza.Exec(`ALTER TABLE readings RENAME TO nema_readings`); err != nil {
		t.Fatal(err)
	}
	if err := o.rs.Update(ctx, o.adminOvl, &models.Reading{ID: uuid.New()}); err == nil {
		t.Error("izmjena bez tablice")
	}
	if _, err := o.rs.Delete(ctx, o.adminOvl, uuid.New()); err == nil {
		t.Error("brisanje bez tablice")
	}
}
