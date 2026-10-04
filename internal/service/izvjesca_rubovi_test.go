package service

import (
	"context"
	"database/sql"
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

// Rubni slučajevi dnevnih i sektorskih izvješća (IzvjescaService.Spremi,
// Predaj, Obrisi, Predlozak, SpremiSektorsko i pregled sektora). Sektor P
// s područjima 1 i 2 je izmišljen; jedina osoba je Pero Perić (pperic).

type okolinaIzvjesca struct {
	baza       *sql.DB
	svc        *IzvjescaService
	dnevnici   *repository.JournalRepository
	epizode    *repository.EpisodeRepository
	citanja    *repository.ReadingRepository
	d111, d112 *models.Section
	d211       *models.Section
	letva      *models.Station
	pero       *models.User
	urednik    *models.User // drugi Pero Perić, s pravom na istoj dionici
	pisePero   *models.UserPermissions
	upravaP    *models.UserPermissions
}

func novaOkolinaIzvjesca(t *testing.T) *okolinaIzvjesca {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "izvjesca.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo'), ('Q', 'Sektor Q', 'VGO Drugdje', 'COP Drugdje')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', ''), (2, 'P', 'Mali sliv Probni', 'VGI Probni', '')`,
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
	sekcije := repository.NewSectionRepository(baza, rec)
	stanice := repository.NewStationRepository(baza, rec)
	o := &okolinaIzvjesca{baza: baza, dnevnici: repository.NewJournalRepository(baza, rec), epizode: repository.NewEpisodeRepository(baza, rec),
		citanja: repository.NewReadingRepository(baza, rec)}
	o.svc = NewIzvjescaService(repository.NewIzvjescaRepository(baza, rec), sekcije, stanice, o.citanja, o.epizode, o.dnevnici)
	o.svc.SetSektorska(repository.NewSektorskaIzvjescaRepository(baza, rec))
	o.letva = &models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo", Watercourse: "Primjerica"}
	if err := stanice.CreateStation(context.Background(), o.letva); err != nil {
		t.Fatal(err)
	}
	dionica := func(code string) *models.Section {
		d, err := sekcije.GetSectionByCode(code)
		if err != nil || d == nil {
			t.Fatalf("dionica %s: %v", code, err)
		}
		return d
	}
	o.d111, o.d112, o.d211 = dionica("P.1.1"), dionica("P.1.2"), dionica("P.2.1")
	o.d111.Parts = []models.SectionPart{{WatercourseName: "Primjerica", StationIDs: []string{o.letva.ID.String(), "nije-uuid", uuid.NewString()}}}
	o.pero = &models.User{ID: uuid.New(), FullName: "Pero Perić"}
	o.urednik = &models.User{ID: uuid.New(), FullName: "Pero Perić (zamjenik)"}
	o.pisePero = &models.UserPermissions{AllowedSections: map[string]bool{"P.1.1": true}}
	o.upravaP = &models.UserPermissions{AdminSectors: map[string]bool{"P": true}}
	return o
}

func danas() time.Time { return pocetakDana(time.Now().In(models.Zagreb)) }

func punoIzvjesce(code string, dan time.Time) *models.DnevnoIzvjesce {
	return &models.DnevnoIzvjesce{SectionCode: code, Dan: dan, Sadrzaj: models.IzvjesceSadrzaj{Pregled: "Nasip pregledan, bez oštećenja."}}
}

func TestDnevnoIzvjesceOdbijanja(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	ctx := context.Background()
	for _, s := range []struct {
		ime    string
		u      *models.User
		perms  *models.UserPermissions
		sec    *models.Section
		iz     *models.DnevnoIzvjesce
		greska string
	}{
		{"bez prijave", nil, o.pisePero, o.d111, punoIzvjesce("P.1.1", danas()), "prijavu"},
		{"bez dionice", o.pero, o.pisePero, nil, punoIzvjesce("P.1.1", danas()), "nema dionicu"},
		{"druga dionica", o.pero, o.pisePero, o.d111, punoIzvjesce("P.1.2", danas()), "nema dionicu"},
		{"bez prava", o.pero, &models.UserPermissions{AllowedSections: map[string]bool{"P.1.2": true}}, o.d111, punoIzvjesce("P.1.1", danas()), "rukovoditelj dionice"},
		{"bez dana", o.pero, o.pisePero, o.d111, punoIzvjesce("P.1.1", time.Time{}), "mora imati dan"},
		{"sutra", o.pero, o.pisePero, o.d111, punoIzvjesce("P.1.1", danas().AddDate(0, 0, 1)), "unaprijed"},
		{"kriva tendencija", o.pero, o.pisePero, o.d111, func() *models.DnevnoIzvjesce {
			iz := punoIzvjesce("P.1.1", danas())
			iz.Sadrzaj.Tendencija = "RASTE"
			return iz
		}(), "tendencija"},
		{"nepostojeći ID", o.pero, o.pisePero, o.d111, func() *models.DnevnoIzvjesce {
			iz := punoIzvjesce("P.1.1", danas())
			iz.ID = uuid.NewString()
			return iz
		}(), "nije pronađeno"},
	} {
		if err := o.svc.Spremi(ctx, s.u, s.perms, s.sec, s.iz); err == nil || !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: očekivano „%s”, dobiveno %v", s.ime, s.greska, err)
		}
	}
	if n, _ := o.svc.Broj(ctx); n != 0 {
		t.Errorf("odbijeno je upisano: %d", n)
	}

	// danas u 23:59 je danas; dan se svede na ponoć, stadij na „normalno”,
	// a vodotok dođe iz dionice
	iz := punoIzvjesce("P.1.1", danas().Add(23*time.Hour+59*time.Minute))
	if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, iz); err != nil {
		t.Fatal(err)
	}
	if !iz.Dan.Equal(danas()) || iz.Stadij != models.PhaseNormal || iz.Sadrzaj.Vodotok != "Primjerica" || iz.Izradio != "Pero Perić" || iz.IzradenoAt.IsZero() {
		t.Errorf("spremljeno: %+v", iz)
	}
}

func TestDnevnoIzvjescePredanoIzmjena(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	ctx := context.Background()
	iz := punoIzvjesce("P.1.1", danas())
	if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, iz); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.Predaj(ctx, o.pero, o.pisePero, o.d111, iz.ID); err != nil {
		t.Fatal(err)
	}
	predano, _ := o.svc.Get(ctx, iz.ID)
	// druga predaja ne mijenja vrijeme predaje
	if err := o.svc.Predaj(ctx, o.pero, o.pisePero, o.d111, iz.ID); err != nil {
		t.Fatal(err)
	}
	if opet, _ := o.svc.Get(ctx, iz.ID); !opet.PredanoAt.Equal(*predano.PredanoAt) {
		t.Error("druga predaja promijenila je vrijeme")
	}

	// zamjenik s pravom na dionici predano ne mijenja
	izmjena := *predano
	izmjena.Sadrzaj.Pregled = "Procjeđivanje kod Primjerova."
	if err := o.svc.Spremi(ctx, o.urednik, o.pisePero, o.d111, &izmjena); err == nil || !strings.Contains(err.Error(), "tko ga je predao") {
		t.Errorf("zamjenik mijenja predano: %v", err)
	}
	// Autor mijenja i predano: sadržaj se mijenja, a izvješće ostaje
	// predano s istim vremenom predaje.
	izmjena.IzradioID, izmjena.Izradio = "netko", "netko drugi"
	if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, &izmjena); err != nil {
		t.Fatal(err)
	}
	poslije, _ := o.svc.Get(ctx, iz.ID)
	if poslije.Sadrzaj.Pregled != "Procjeđivanje kod Primjerova." || !poslije.Predano() || !poslije.PredanoAt.Equal(*predano.PredanoAt) ||
		poslije.Izradio != "Pero Perić" || poslije.IzradioID != o.pero.ID.String() {
		t.Errorf("izmjena predanog: %+v", poslije)
	}
	// uprava sektora mijenja predano
	izmjena.Sadrzaj.Pregled = "Ispravak uprave."
	if err := o.svc.Spremi(ctx, o.urednik, o.upravaP, o.d111, &izmjena); err != nil {
		t.Errorf("uprava mijenja predano: %v", err)
	}

	// brisanje predanog: zamjenik ne, uprava da
	if err := o.svc.Obrisi(ctx, o.urednik, o.pisePero, o.d111, iz.ID); err == nil {
		t.Error("zamjenik briše predano")
	}
	if err := o.svc.Obrisi(ctx, o.urednik, o.upravaP, o.d112, iz.ID); err == nil || !strings.Contains(err.Error(), "nije pronađeno") {
		t.Errorf("brisanje kroz drugu dionicu: %v", err)
	}
	if err := o.svc.Obrisi(ctx, o.urednik, o.upravaP, o.d111, iz.ID); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.Obrisi(ctx, nil, o.upravaP, o.d111, iz.ID); err == nil {
		t.Error("brisanje bez prijave")
	}
	if err := o.svc.Predaj(ctx, o.pero, o.pisePero, o.d111, uuid.NewString()); err == nil || !strings.Contains(err.Error(), "nije pronađeno") {
		t.Errorf("predaja nepostojećeg: %v", err)
	}
	if err := o.svc.Predaj(ctx, o.pero, &models.UserPermissions{}, o.d111, iz.ID); err == nil {
		t.Error("predaja bez prava")
	}
}

func TestDnevnoIzvjesceTudjimIdentitetom(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	ctx := context.Background()
	// nacrt dionice P.1.2, koji piše uprava
	tudje := punoIzvjesce("P.1.2", danas())
	if err := o.svc.Spremi(ctx, o.urednik, o.upravaP, o.d112, tudje); err != nil {
		t.Fatal(err)
	}
	// Zadani ID mora biti izvješće iste dionice: rukovoditelj P.1.1 svojim
	// izvješćem ne prepiše nacrt dionice P.1.2.
	moje := punoIzvjesce("P.1.1", danas())
	moje.ID = tudje.ID
	if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, moje); err == nil || !strings.Contains(err.Error(), "nije pronađeno") {
		t.Fatalf("prepisivanje izvješća druge dionice: %v", err)
	}
	if zapis, _ := o.svc.Get(ctx, tudje.ID); zapis == nil || zapis.SectionCode != "P.1.2" {
		t.Errorf("izvješće dionice P.1.2: %+v", zapis)
	}
	// isto kao predaja i brisanje kroz drugu dionicu
	if err := o.svc.Predaj(ctx, o.urednik, o.upravaP, o.d111, tudje.ID); err == nil {
		t.Error("predaja kroz drugu dionicu")
	}
}

func TestPredlozakDnevnogIzvjesca(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	ctx := context.Background()
	dan := danas().AddDate(0, 0, -1)
	upisi := func(sat, minuta int, cm *int) {
		t.Helper()
		if err := o.citanja.Create(ctx, &models.Reading{ID: uuid.New(), StationID: o.letva.ID.String(), Source: models.ReadingSourceManual,
			MeasuredAt: dan.Add(time.Duration(sat)*time.Hour + time.Duration(minuta)*time.Minute), LevelCm: cm, Note: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	cm := func(v int) *int { return &v }
	upisi(4, 59, cm(1))  // izvan 5–9
	upisi(6, 40, cm(-5)) // 20 min od sedam: najbliže
	upisi(7, 30, cm(120))
	upisi(7, 5, nil) // bez vodostaja
	upisi(9, 30, cm(2))
	// proglašena obrana na dionici i otvoren dnevnik COP-a
	if err := o.epizode.SaveEpisode(ctx, &models.DefenseEpisode{ID: uuid.New(), SectionCode: "P.1.1", StationID: o.letva.ID.String(),
		StartedAt: dan, Phase: models.PhaseRegular, Basis: models.BasisOrder, Origin: "OPERATER"}); err != nil {
		t.Fatal(err)
	}
	pocetak := dan.AddDate(0, 0, -2)
	for _, j := range []*models.Journal{
		{Kind: models.JournalKindDefense, CentarSektor: "P", Title: "Prijepis", Year: 2026, StartedAt: &pocetak, Reconstruction: true},
		{Kind: models.JournalKindDefense, CentarSektor: "P", Title: "Dnevnik COP-a", Year: 2026, StartedAt: &pocetak},
	} {
		if err := o.dnevnici.SaveJournal(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	iz, err := o.svc.Predlozak(ctx, o.d111, dan.Add(15*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if iz.Stadij != models.PhaseRegular || iz.JournalID == "" || iz.Sadrzaj.Vodotok != "Primjerica" || iz.ID != "" {
		t.Errorf("predložak: %+v", iz)
	}
	// neispravna i nepostojeća letva se preskaču; ostaje Primjerovo s -5 u 06:40
	if len(iz.Sadrzaj.Vodostaji) != 1 {
		t.Fatalf("vodostaji: %+v", iz.Sadrzaj.Vodostaji)
	}
	v := iz.Sadrzaj.Vodostaji[0]
	if v.Postaja != "Primjerica – Primjerovo" || v.Vrijednost != "-5" || v.Sat != "06:40" || v.Izvor != "očitanja" || v.Jedinica != "cm" {
		t.Errorf("vodostaj: %+v", v)
	}
	// dan bez očitanja: redak s imenom letve i praznom vrijednosti
	prazno, _ := o.svc.Predlozak(ctx, o.d111, dan.AddDate(0, 0, -5))
	if len(prazno.Sadrzaj.Vodostaji) != 1 || prazno.Sadrzaj.Vodostaji[0].Vrijednost != "" || prazno.Sadrzaj.Vodostaji[0].Sat != "07:00" {
		t.Errorf("bez očitanja: %+v", prazno.Sadrzaj.Vodostaji)
	}
	// postojeće izvješće za dan vraća se kakvo jest
	postojece := punoIzvjesce("P.1.1", dan)
	if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, postojece); err != nil {
		t.Fatal(err)
	}
	if opet, _ := o.svc.Predlozak(ctx, o.d111, dan); opet.ID != postojece.ID {
		t.Error("predložak ne vraća postojeće")
	}
	// servis bez letava daje prazan popis vodostaja
	bez := &IzvjescaService{repo: o.svc.repo}
	if n, err := bez.Predlozak(ctx, o.d112, dan); err != nil || len(n.Sadrzaj.Vodostaji) != 0 || n.JournalID != "" {
		t.Errorf("bez spremišta: %+v %v", n, err)
	}
}

func TestPravaNaIzvjesca(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	podr1 := &models.UserPermissions{AllowedAreas: map[int]bool{1: true}}
	drugiSektor := &models.UserPermissions{AllowedSections: map[string]bool{"Q.3.1": true}}
	for _, s := range []struct {
		ime           string
		perms         *models.UserPermissions
		pise, vidi    bool
		dionicaZaPisi int
	}{
		{"rukovoditelj dionice", o.pisePero, true, true, 1},
		{"područje 1", podr1, true, true, 2},
		{"uprava sektora", o.upravaP, true, true, 3},
		// dionica u sektoru daje uvid i u druge dionice sektora
		{"dionica P.2.1", &models.UserPermissions{AllowedSections: map[string]bool{"P.2.1": true}}, false, true, 1},
		{"drugi sektor", drugiSektor, false, false, 0},
		{"bez ovlasti", nil, false, false, 0},
	} {
		if got := o.svc.SmijePisati(s.perms, o.d111); got != s.pise {
			t.Errorf("%s piše: %v", s.ime, got)
		}
		if got := o.svc.SmijeVidjeti(s.perms, o.d111); got != s.vidi {
			t.Errorf("%s vidi: %v", s.ime, got)
		}
		if d, err := o.svc.DioniceZaPisanje(s.perms); err != nil || len(d) != s.dionicaZaPisi {
			t.Errorf("%s dionice za pisanje: %d %v", s.ime, len(d), err)
		}
	}
	if o.svc.SmijeVidjeti(o.upravaP, nil) {
		t.Error("bez dionice")
	}
	if d, err := o.svc.Dionica("P.2.1"); err != nil || d == nil || d.AreaID != 2 {
		t.Errorf("Dionica: %+v %v", d, err)
	}
	if _, err := (&IzvjescaService{}).Dionica("P.2.1"); err == nil {
		t.Error("bez registra dionica")
	}
	if sektori := o.svc.SektoriZaSastavljanje(o.upravaP); strings.Join(sektori, ",") != "P" {
		t.Errorf("sektori za sastavljanje: %v", sektori)
	}
	if o.svc.SektoriZaSastavljanje(o.pisePero) != nil || o.svc.SektoriZaSastavljanje(nil) != nil {
		t.Error("rukovoditelj dionice ne sastavlja sektorsko")
	}
	// tko radi u području sektora vidi izvješće sektora
	if !o.svc.SmijeVidjetiSektor(podr1, "P") || o.svc.SmijeVidjetiSektor(drugiSektor, "P") || o.svc.SmijeVidjetiSektor(o.upravaP, "") {
		t.Error("vidljivost sektora")
	}
}

func TestSektorskoIzvjesceOdbijanja(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	ctx := context.Background()
	novo := func(sektor string, dan time.Time) *models.SektorskoIzvjesce {
		return &models.SektorskoIzvjesce{Sektor: sektor, Dan: dan, Sadrzaj: models.SektorskiSadrzaj{Hidrometeo: "Kiša u cijelom sektoru."}}
	}
	for _, s := range []struct {
		ime    string
		u      *models.User
		perms  *models.UserPermissions
		iz     *models.SektorskoIzvjesce
		greska string
	}{
		{"bez prijave", nil, o.upravaP, novo("P", danas()), "prijavu"},
		{"bez sektora", o.pero, o.upravaP, novo("", danas()), "nema sektor"},
		{"rukovoditelj dionice", o.pero, o.pisePero, novo("P", danas()), "voditelj centra"},
		{"uprava drugog sektora", o.pero, o.upravaP, novo("Q", danas()), "voditelj centra"},
		{"bez dana", o.pero, o.upravaP, novo("P", time.Time{}), "mora imati dan"},
		{"sutra", o.pero, o.upravaP, novo("P", danas().AddDate(0, 0, 1)), "unaprijed"},
		{"nepostojeći ID", o.pero, o.upravaP, func() *models.SektorskoIzvjesce {
			iz := novo("P", danas())
			iz.ID = uuid.NewString()
			return iz
		}(), "nije pronađeno"},
	} {
		if err := o.svc.SpremiSektorsko(ctx, s.u, s.perms, s.iz, nil, nil); err == nil || !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: očekivano „%s”, dobiveno %v", s.ime, s.greska, err)
		}
	}
	if err := (&IzvjescaService{}).SpremiSektorsko(ctx, o.pero, o.upravaP, novo("P", danas()), nil, nil); err == nil || !strings.Contains(err.Error(), "nisu uključena") {
		t.Errorf("bez spremišta: %v", err)
	}
	if _, err := (&IzvjescaService{}).PredlozakSektora(ctx, "P", danas()); err == nil {
		t.Error("predložak bez spremišta")
	}
	if g, _ := (&IzvjescaService{}).GetSektorsko(ctx, "x"); g != nil {
		t.Error("Get bez spremišta")
	}
	if l, _ := (&IzvjescaService{}).ListSektorska(ctx, "P"); l != nil {
		t.Error("List bez spremišta")
	}

	// prvo izvješće dana prolazi, drugo za isti dan ne
	prvo := novo("P", danas())
	if err := o.svc.SpremiSektorsko(ctx, o.pero, o.upravaP, prvo, nil, nil); err != nil {
		t.Fatal(err)
	}
	if prvo.Izradio != "Pero Perić" || prvo.JournalID != "" {
		t.Errorf("prvo: %+v", prvo)
	}
	if err := o.svc.SpremiSektorsko(ctx, o.pero, o.upravaP, novo("P", danas()), nil, nil); err == nil || !strings.Contains(err.Error(), "već postoji") {
		t.Errorf("drugo za isti dan: %v", err)
	}
}

func TestSektorskoIzvjesceIzmjenaIPredaja(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	ctx := context.Background()
	pocetak := danas().AddDate(0, 0, -3)
	obrana := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "P", Title: "Dnevnik COP-a", Year: 2026, StartedAt: &pocetak}
	if err := o.dnevnici.SaveJournal(ctx, obrana); err != nil {
		t.Fatal(err)
	}
	dnevno := punoIzvjesce("P.1.1", danas())
	if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, dnevno); err != nil {
		t.Fatal(err)
	}
	iz := &models.SektorskoIzvjesce{Sektor: "P", Dan: danas()}
	if err := o.svc.SpremiSektorsko(ctx, o.pero, o.upravaP, iz, nil, nil); err != nil {
		t.Fatal(err)
	}
	// novo izvješće dobije otvoreni dnevnik; bez izbora nijedno dnevno ne ulazi
	if iz.JournalID != obrana.ID || iz.Sadrzaj.Pregled.Izvjesca != 0 || iz.Sadrzaj.Pregled.Nacrta != 1 {
		t.Errorf("novo: %+v", iz.Sadrzaj.Pregled)
	}
	// prazno se ne predaje
	if err := o.svc.PredajSektorsko(ctx, o.pero, o.upravaP, iz.ID); err == nil || !strings.Contains(err.Error(), "prazno") {
		t.Errorf("predaja praznog: %v", err)
	}
	// izmjena s izabranim nacrtom dnevnog: ulazi i nacrt kad je izabran;
	// izmjena zadrži autora i dnevnik, i kad zahtjev nosi drukčije
	izmjena := &models.SektorskoIzvjesce{ID: iz.ID, Sektor: "P", Dan: danas(), JournalID: "drugi", IzradioID: "netko",
		Sadrzaj: models.SektorskiSadrzaj{Hidrometeo: "Vodostaji rastu."}}
	if err := o.svc.SpremiSektorsko(ctx, o.urednik, o.upravaP, izmjena, []string{dnevno.ID}, []string{"nepostojeci-zapis"}); err != nil {
		t.Fatal(err)
	}
	if izmjena.Sadrzaj.Pregled.Izvjesca != 1 || izmjena.JournalID != obrana.ID || izmjena.Izradio != "Pero Perić" || len(izmjena.Sadrzaj.Zapisi) != 0 {
		t.Errorf("izmjena: %+v", izmjena)
	}
	if err := o.svc.PredajSektorsko(ctx, o.pero, o.pisePero, iz.ID); err == nil {
		t.Error("predaja bez uprave")
	}
	if err := o.svc.PredajSektorsko(ctx, o.pero, o.upravaP, iz.ID); err != nil {
		t.Fatal(err)
	}
	predano, _ := o.svc.GetSektorsko(ctx, iz.ID)
	if err := o.svc.PredajSektorsko(ctx, o.pero, o.upravaP, iz.ID); err != nil {
		t.Errorf("druga predaja: %v", err)
	}
	// Predano izvješće sektora i dalje se mijenja: tekst se promijeni, a
	// vrijeme predaje ostane.
	poslije := &models.SektorskoIzvjesce{ID: iz.ID, Sektor: "P", Dan: danas(), Sadrzaj: models.SektorskiSadrzaj{Hidrometeo: "Ispravljeno poslije predaje."}}
	if err := o.svc.SpremiSektorsko(ctx, o.pero, o.upravaP, poslije, nil, nil); err != nil {
		t.Fatalf("izmjena predanog danas prolazi: %v", err)
	}
	if g, _ := o.svc.GetSektorsko(ctx, iz.ID); g.Sadrzaj.Hidrometeo != "Ispravljeno poslije predaje." || !g.PredanoAt.Equal(*predano.PredanoAt) {
		t.Errorf("predano poslije izmjene: %+v", g)
	}
	// Zadani ID mora biti izvješće istog sektora: uprava sektora Q ne
	// prepiše izvješće sektora P.
	upravaQ := &models.UserPermissions{AdminSectors: map[string]bool{"Q": true}}
	tudje := &models.SektorskoIzvjesce{ID: iz.ID, Sektor: "Q", Dan: danas(), Sadrzaj: models.SektorskiSadrzaj{Hidrometeo: "Sektor Q."}}
	if err := o.svc.SpremiSektorsko(ctx, o.urednik, upravaQ, tudje, nil, nil); err == nil || !strings.Contains(err.Error(), "nije pronađeno") {
		t.Fatalf("prepisivanje izvješća drugog sektora: %v", err)
	}
	if g, _ := o.svc.GetSektorsko(ctx, iz.ID); g.Sektor != "P" {
		t.Errorf("izvješće sektora P: %+v", g)
	}
	// brisanje: tuđi sektor ne, nepostojeće ne, uprava da
	if err := o.svc.ObrisiSektorsko(ctx, o.pero, upravaQ, iz.ID); err == nil {
		t.Error("uprava Q briše izvješće sektora P")
	}
	if err := o.svc.ObrisiSektorsko(ctx, o.pero, upravaQ, uuid.NewString()); err == nil || !strings.Contains(err.Error(), "nije pronađeno") {
		t.Errorf("brisanje nepostojećeg: %v", err)
	}
	if err := o.svc.PredajSektorsko(ctx, o.pero, upravaQ, uuid.NewString()); err == nil || !strings.Contains(err.Error(), "nije pronađeno") {
		t.Errorf("predaja nepostojećeg: %v", err)
	}
	if err := o.svc.ObrisiSektorsko(ctx, o.pero, o.upravaP, iz.ID); err != nil {
		t.Fatal(err)
	}
}

func TestPregledSektoraStadijVodotoka(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	izvjesce := func(id, code string, stadij models.DefensePhase, tendencija string) models.DnevnoIzvjesce {
		sad := time.Now()
		return models.DnevnoIzvjesce{ID: id, SectionCode: code, Stadij: stadij, PredanoAt: &sad,
			Sadrzaj: models.IzvjesceSadrzaj{Vodotok: "Primjerica, Probni", Tendencija: tendencija}}
	}
	// Prvo izvješće ima redovnu obranu bez tendencije, drugo pripremno s
	// porastom: vodotok preuzme niži stadij, jer prazna tendencija
	// dopušta prepisivanje.
	p := o.svc.PregledSektora([]models.DnevnoIzvjesce{
		izvjesce("a", "P.1.1", models.PhaseRegular, ""),
		izvjesce("b", "P.2.1", models.PhasePrep, models.TendencijaPorast),
	}, nil)
	if len(p.Vodotoci) != 2 || p.Vodotoci[0].Vodotok != "Primjerica" || len(p.Vodotoci[0].Dionice) != 2 {
		t.Fatalf("vodotoci: %+v", p.Vodotoci)
	}
	if p.Vodotoci[0].Stadij != models.PhasePrep || p.Vodotoci[0].Tendencija != models.TendencijaPorast {
		t.Errorf("stadij vodotoka: %s %s (danas niži stadij)", p.Vodotoci[0].Stadij, p.Vodotoci[0].Tendencija)
	}
	// obrnutim redom ostane viši
	p = o.svc.PregledSektora([]models.DnevnoIzvjesce{
		izvjesce("b", "P.2.1", models.PhasePrep, models.TendencijaPorast),
		izvjesce("a", "P.1.1", models.PhaseRegular, ""),
	}, nil)
	if p.Vodotoci[0].Stadij != models.PhaseRegular {
		t.Errorf("obrnuti red: %s", p.Vodotoci[0].Stadij)
	}
	// područja po broju iz šifre dionice, redom
	if len(p.Podrucja) != 2 || p.Podrucja[0].AreaID != 1 || p.Podrucja[1].AreaID != 2 {
		t.Errorf("područja: %+v", p.Podrucja)
	}
	if areaIzSifre("P") != 0 || areaIzSifre("P.x1.1") != 0 || areaIzSifre("P.12.3") != 12 {
		t.Error("broj područja iz šifre")
	}
	if spojiTekst("", "b", "; ") != "b" || spojiTekst("a", "", "; ") != "a" || spojiTekst("a", "b", "; ") != "a; b" {
		t.Error("spojiTekst")
	}
}
