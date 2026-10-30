package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gocop/internal/models"
)

// linkerKazala je vezač nad bazom kazala (sektor P, područje 61), u
// transakciji koja se na kraju testa vraća
func linkerKazala(t *testing.T, database *sql.DB) (*Linker, *sql.Tx) {
	t.Helper()
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Rollback() })
	l, err := NewLinker(context.Background(), tx)
	if err != nil {
		t.Fatal(err)
	}
	return l, tx
}

func dionicaPodrucja61(dijelovi ...models.SectionPart) *models.Section {
	return &models.Section{Code: "P.61.9", AreaID: 61, Parts: dijelovi}
}

// Voda iz opisa koje registar nema upisuje se jednom, s podrijetlom
// dokumentacije; druga poddionica na istoj vodi veže se na nju. Poznata voda
// se samo veže, a poddionica s vodom ili bez opisa ostaje kakva jest.
func TestLinkerVodaIzOpisa(t *testing.T) {
	database, _ := bazaKazala(t)
	if _, err := database.Exec(`INSERT INTO watercourses (code, official_name, name, kind) VALUES ('rijeka-proba', 'rijeka Proba', 'Proba', 'rijeka')`); err != nil {
		t.Fatal(err)
	}
	l, tx := linkerKazala(t, database)
	sec := dionicaPodrucja61(
		models.SectionPart{Description: "potok Probnica, lijeva obala od ušća"},
		models.SectionPart{Description: "Probnica - od mosta do brane"},
		models.SectionPart{Description: "rijeka Proba, desna obala"},
		models.SectionPart{Description: "potok Probnica", WatercourseCode: "vec-vezano"},
		models.SectionPart{},
		models.SectionPart{Description: "(bez naziva)"},
	)
	if err := l.LinkRegistries(context.Background(), sec); err != nil {
		t.Fatal(err)
	}
	var kodovi []string
	for _, p := range sec.Parts {
		kodovi = append(kodovi, p.WatercourseCode)
	}
	if want := []string{"potok-probnica", "potok-probnica", "rijeka-proba", "vec-vezano", "", ""}; !slices.Equal(kodovi, want) {
		t.Errorf("vode poddionica %v, želim %v", kodovi, want)
	}
	if l.Added.Waters != 1 {
		t.Errorf("upisano voda %d, želim 1", l.Added.Waters)
	}
	var naziv, ime, vrsta, podrijetlo string
	if err := tx.QueryRow(`SELECT official_name, name, kind, origin FROM watercourses WHERE code = 'potok-probnica'`).
		Scan(&naziv, &ime, &vrsta, &podrijetlo); err != nil {
		t.Fatal(err)
	}
	if naziv != "potok Probnica" || ime != "Probnica" || vrsta != "potok" || podrijetlo != models.WatercourseOriginDocumentation {
		t.Errorf("upisana voda %q %q %q %q", naziv, ime, vrsta, podrijetlo)
	}
	if sec.SectorID != "P" {
		t.Errorf("sektor iz područja %q", sec.SectorID)
	}
}

// Dionica nepostojećeg područja ne veže se.
func TestLinkerNepoznatoPodrucje(t *testing.T) {
	database, _ := bazaKazala(t)
	l, _ := linkerKazala(t, database)
	err := l.LinkRegistries(context.Background(), &models.Section{Code: "X.1", AreaID: 99})
	if err == nil || !strings.Contains(err.Error(), "područje 99 ne postoji") {
		t.Fatalf("greška %v", err)
	}
}

// Objekt koji redak dionice traži upisuje se u registar područja, redak se
// veže na njega i zahtjev se briše; idući redak istog naziva veže se na isti
// objekt. Podrijetlo je zadano, inače dokumentacija.
func TestLinkerNoviObjekt(t *testing.T) {
	database, _ := bazaKazala(t)
	l, tx := linkerKazala(t, database)
	l.Origin = models.WatercourseOriginContract
	sec := dionicaPodrucja61(models.SectionPart{Objects: []models.PartObject{
		{Name: "CS Nova", NewRecord: &models.NewRegistryRecord{Registry: models.RegistryStructure, Kind: models.StructureKindPumpingStation, Name: "  CS   Nova "}},
		{Name: "cs Nova, lijevi ulaz"},
	}})
	if err := l.LinkRegistries(context.Background(), sec); err != nil {
		t.Fatal(err)
	}
	o := sec.Parts[0].Objects
	id := StableID("structure", "bp61-cs-nova").String()
	if o[0].StructureID != id || o[0].NewRecord != nil || o[1].StructureID != id {
		t.Errorf("objekti %+v", o)
	}
	if !slices.Equal(l.Created, []string{id}) {
		t.Errorf("upisani %v", l.Created)
	}
	var naziv, vrsta, sektor, podrijetlo string
	var podrucje int
	if err := tx.QueryRow(`SELECT name, kind, sector_id, area_id, origin FROM structures WHERE id = ?`, id).
		Scan(&naziv, &vrsta, &sektor, &podrucje, &podrijetlo); err != nil {
		t.Fatal(err)
	}
	if naziv != "CS Nova" || vrsta != models.StructureKindPumpingStation || sektor != "P" || podrucje != 61 || podrijetlo != models.WatercourseOriginContract {
		t.Errorf("upisani objekt %q %q %q %d %q", naziv, vrsta, sektor, podrucje, podrijetlo)
	}

	// isti zahtjev drugi put: objekt već postoji
	sec = dionicaPodrucja61(models.SectionPart{Objects: []models.PartObject{
		{Name: "CS Nova", NewRecord: &models.NewRegistryRecord{Registry: models.RegistryStructure, Kind: models.StructureKindPumpingStation, Name: "CS Nova"}},
	}})
	if err := l.LinkRegistries(context.Background(), sec); err == nil || !strings.Contains(err.Error(), "već postoji u registru") {
		t.Errorf("dvaput isti objekt: %v", err)
	}
}

// Zahtjev bez naziva, s vrstom koja nije objekt ili s nepoznatim registrom
// ne upisuje ništa.
func TestLinkerNoviZapisOdbijen(t *testing.T) {
	// svaki podtest radi u svojoj transakciji koja se vraća, pa baza može
	// biti zajednička
	database, _ := bazaKazala(t)
	for _, s := range []struct {
		ime   string
		zapis models.NewRegistryRecord
		zelim string
	}{
		{"bez naziva", models.NewRegistryRecord{Registry: models.RegistryStructure, Kind: models.StructureKindSluice, Name: "  "}, "nema naziva"},
		{"nasip", models.NewRegistryRecord{Registry: models.RegistryStructure, Kind: models.StructureKindEmbankment, Name: "Nasip X"}, `nepoznata vrsta "NASIP"`},
		{"brana", models.NewRegistryRecord{Registry: models.RegistryStructure, Kind: models.StructureKindDam, Name: "Brana X"}, `nepoznata vrsta "BRANA"`},
		{"izmišljena vrsta", models.NewRegistryRecord{Registry: models.RegistryStructure, Kind: "TORANJ", Name: "Toranj"}, `nepoznata vrsta "TORANJ"`},
		{"registar", models.NewRegistryRecord{Registry: "osobe", Kind: "x", Name: "Ana"}, `nepoznat registar "osobe"`},
	} {
		t.Run(s.ime, func(t *testing.T) {
			l, tx := linkerKazala(t, database)
			zapis := s.zapis
			sec := dionicaPodrucja61(models.SectionPart{Objects: []models.PartObject{{Name: "redak", NewRecord: &zapis}}})
			err := l.LinkRegistries(context.Background(), sec)
			if err == nil || !strings.Contains(err.Error(), s.zelim) {
				t.Fatalf("greška %v, želim %q", err, s.zelim)
			}
			if sec.Parts[0].Objects[0].NewRecord == nil || len(l.Created) != 0 {
				t.Error("odbijeni zahtjev je obrisan ili upisan")
			}
			var n int
			tx.QueryRow(`SELECT COUNT(*) FROM structures`).Scan(&n)
			if n != 1 {
				t.Errorf("objekata u registru %d", n)
			}
		})
	}
}

// Nova voda iz retka: naziv bez rubnih znakova, ručni unos; ista voda drugi
// put se ne upisuje, ali se redak veže na nju.
func TestLinkerNovaVoda(t *testing.T) {
	database, _ := bazaKazala(t)
	l, tx := linkerKazala(t, database)
	nova := func() *models.NewRegistryRecord {
		return &models.NewRegistryRecord{Registry: models.RegistryWatercourse, Kind: "kanal", Name: " Novi  Odvod -"}
	}
	sec := dionicaPodrucja61(models.SectionPart{Objects: []models.PartObject{
		{Name: "odvodni kanal", NewRecord: nova()},
		{Name: "isti kanal niže", NewRecord: nova()},
	}})
	if err := l.LinkRegistries(context.Background(), sec); err != nil {
		t.Fatal(err)
	}
	o := sec.Parts[0].Objects
	if o[0].WatercourseCode != "kanal-novi-odvod" || o[1].WatercourseCode != "kanal-novi-odvod" || o[0].NewRecord != nil || o[1].NewRecord != nil {
		t.Errorf("redci %+v", o)
	}
	if !slices.Equal(l.NoveVode, []string{"kanal-novi-odvod"}) || l.Added.Waters != 1 {
		t.Errorf("nove vode %v, upisano %d", l.NoveVode, l.Added.Waters)
	}
	var naziv, ime, podrijetlo string
	if err := tx.QueryRow(`SELECT official_name, name, origin FROM watercourses WHERE code = 'kanal-novi-odvod'`).Scan(&naziv, &ime, &podrijetlo); err != nil {
		t.Fatal(err)
	}
	if naziv != "kanal Novi Odvod" || ime != "Novi Odvod" || podrijetlo != models.WatercourseOriginManual {
		t.Errorf("voda %q %q %q", naziv, ime, podrijetlo)
	}
	// poddionica s opisom te vode sad se veže na nju bez novog upisa
	sec = dionicaPodrucja61(models.SectionPart{Description: "kanal Novi Odvod, cijelom dužinom"})
	if err := l.LinkRegistries(context.Background(), sec); err != nil {
		t.Fatal(err)
	}
	if sec.Parts[0].WatercourseCode != "kanal-novi-odvod" || l.Added.Waters != 1 {
		t.Errorf("poddionica %q, upisano %d", sec.Parts[0].WatercourseCode, l.Added.Waters)
	}
}

// Naslijeđeni teritorij: jedna poddionica dobiva sve; kod više njih ide
// onamo gdje ugroženo područje spominje naselje, ili općinu kad naselja
// nema, a ostalo na prvu. Isto se ne dodaje dvaput.
func TestLinkerRazdiobaTeritorija(t *testing.T) {
	selo := 5
	l := &Linker{territory: map[string][]territoryRow{
		"D.1": {
			{county: 1, municipality: 10, settlement: &selo, muniName: "Općina Gornja", settName: "Donje Selo"},
			{county: 1, municipality: 11, muniName: "Grad Ravni"},
			{county: 1, municipality: 12, muniName: "Daleka"},
		},
	}}
	ključ := func(p models.SectionPart) []string {
		var out []string
		for _, t := range p.Territories {
			out = append(out, t.Key())
		}
		slices.Sort(out)
		return out
	}

	jedna := &models.Section{Code: "D.1", Parts: []models.SectionPart{{}}}
	l.linkTerritories(jedna)
	if got := ključ(jedna.Parts[0]); len(got) != 3 {
		t.Errorf("jedna poddionica: %v", got)
	}

	vise := &models.Section{Code: "D.1", Parts: []models.SectionPart{
		{ProtectedText: "naselja Gornji Kraj"},
		{ProtectedText: "naselje DONJE SELO i okolica"},
		{ProtectedText: "grad Ravni, istočni dio"},
		{},
	}}
	vise.Parts[1].Territories = []models.PartTerritory{{CountyID: 1, MunicipalityID: 10, SettlementID: &selo}}
	l.linkTerritories(vise)
	zelim := [][]string{
		{(models.PartTerritory{CountyID: 1, MunicipalityID: 12}).Key()},
		{(models.PartTerritory{CountyID: 1, MunicipalityID: 10, SettlementID: &selo}).Key()},
		{(models.PartTerritory{CountyID: 1, MunicipalityID: 11}).Key()},
		nil,
	}
	for i, p := range vise.Parts {
		if got := ključ(p); !slices.Equal(got, zelim[i]) {
			t.Errorf("poddionica %d: %v, želim %v", i, got, zelim[i])
		}
	}

	bez := &models.Section{Code: "D.2", Parts: []models.SectionPart{{}}}
	l.linkTerritories(bez)
	if len(bez.Parts[0].Territories) != 0 {
		t.Error("dionica bez naslijeđenih veza dobila teritorij")
	}
	l.linkTerritories(&models.Section{Code: "D.1"}) // bez poddionica ne pada

	// redak s naseljem ne ide po općini: spomen općine bez naselja ne
	// privlači ga, pa ide na prvu poddionicu
	samoNaselje := &Linker{territory: map[string][]territoryRow{
		"D.3": {{county: 1, municipality: 10, settlement: &selo, muniName: "Općina Gornja", settName: "Donje Selo"}},
	}}
	poOpcini := &models.Section{Code: "D.3", Parts: []models.SectionPart{
		{ProtectedText: "uz nasip"},
		{ProtectedText: "Općina Gornja, istočni dio"},
	}}
	samoNaselje.linkTerritories(poOpcini)
	if len(poOpcini.Parts[0].Territories) != 1 || len(poOpcini.Parts[1].Territories) != 0 {
		t.Errorf("naselje po općini: %+v", poOpcini.Parts)
	}
}

// Vezač se ne gradi kad nedostaje neki od registara koje čita.
func TestNewLinkerBezRegistara(t *testing.T) {
	database, err := OpenDB(filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	tablice := []string{
		`CREATE TABLE watercourses (code TEXT, name TEXT, official_name TEXT, kind TEXT)`,
		`CREATE TABLE stations (id TEXT, name TEXT, stationing TEXT, watercourse_code TEXT)`,
		`CREATE TABLE areas (id INTEGER, sector_id TEXT, name TEXT, vgi_name TEXT, subcenter TEXT)`,
		`CREATE TABLE municipalities (id INTEGER, name TEXT)`,
		`CREATE TABLE settlements (id INTEGER, name TEXT)`,
		`CREATE TABLE section_territories (section_code TEXT, county_id INTEGER, municipality_id INTEGER, settlement_id INTEGER)`,
	}
	ctx := context.Background()
	for _, tablica := range tablice {
		if _, err := NewLinker(ctx, database); err == nil {
			t.Fatalf("vezač sagrađen prije %s", tablica)
		}
		if _, err := database.Exec(tablica); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`INSERT INTO stations VALUES ('a', 'Gornji Grad (uzvodno)', 'km 2,5', 'rijeka-x'), ('b', 'Bez Stacionaže', '', '')`); err != nil {
		t.Fatal(err)
	}
	l, err := NewLinker(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	if l.vodaPostaje["a"] != "rijeka-x" || l.vodaPostaje["b"] != "" || len(l.osnova) != 1 || len(l.stations) != 2 {
		t.Errorf("vezač %+v", l)
	}
}

// Vezanje svih dionica pada cijelo kad jedna dionica ne može biti vezana,
// i ne ostavlja ništa upisano.
func TestLinkAllSectionsPadaCijelo(t *testing.T) {
	database, _ := bazaKazala(t)
	if err := upisiDionicu(database, &models.Section{Code: "P.61.2", AreaID: 61, SectorID: "P",
		Parts: []models.SectionPart{{Description: "potok Novica, lijeva obala"}}}); err != nil {
		t.Fatal(err)
	}
	// dionica područja kojeg nema: upisuje se na vezi bez stranih ključeva
	veza, err := database.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, upit := range []string{`PRAGMA foreign_keys = OFF`,
		`INSERT INTO sections (code, area_id, sector_id, description, parts, created_at, updated_at)
		VALUES ('X.99.1', 99, 'P', '', '[]', '2026-01-01', '2026-01-01')`, `PRAGMA foreign_keys = ON`} {
		if _, err := veza.ExecContext(context.Background(), upit); err != nil {
			t.Fatal(err)
		}
	}
	veza.Close()
	err = LinkAllSections(context.Background(), database)
	if err == nil || !strings.Contains(err.Error(), "područje 99 ne postoji") {
		t.Fatalf("greška %v", err)
	}
	if n := count(t, database, `SELECT COUNT(*) FROM watercourses WHERE code = 'potok-novica'`); n != 0 {
		t.Error("voda je upisana iako vezanje nije prošlo")
	}

	database.Close()
	if err := LinkAllSections(context.Background(), database); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Vezanje svih dionica upiše vodu iz opisa i dionicu s njom.
func TestLinkAllSectionsUpisujeVezano(t *testing.T) {
	database, _ := bazaKazala(t)
	if err := upisiDionicu(database, &models.Section{Code: "P.61.3", AreaID: 61, SectorID: "P", Notes: "napomena",
		Parts: []models.SectionPart{{Description: "potok Novica, lijeva obala"}}}); err != nil {
		t.Fatal(err)
	}
	if err := LinkAllSections(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	var voda, napomena string
	if err := database.QueryRow(`SELECT watercourse_code, notes FROM sections WHERE code = 'P.61.3'`).Scan(&voda, &napomena); err != nil {
		t.Fatal(err)
	}
	if voda != "potok-novica" || napomena != "napomena" {
		t.Errorf("dionica nakon vezanja: %q %q", voda, napomena)
	}
}
