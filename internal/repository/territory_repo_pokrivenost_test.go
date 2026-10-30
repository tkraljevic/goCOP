package repository

import (
	"context"
	"database/sql"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// teritorijProbe diže bazu s područjem 34, dvjema županijama, jednom
// općinom, gradom i trima naseljima; vraća repozitorij i bazu
func teritorijProbe(t *testing.T) (*sql.DB, *TerritoryRepository) {
	t.Helper()
	baza, rec := bazaSPodrucjem(t)
	repo := NewTerritoryRepository(baza, rec)
	ctx := context.Background()
	for _, c := range []*models.County{
		{Code: "OBŽ", Name: "Osječko-baranjska", Seat: "Osijek", Prefect: "Pero Perić", AreaSqKm: 4155, Population: 259481, Email: "zupan@primjer.hr", Phone: "031 000 000", Website: "primjer.hr"},
		{Name: "Vukovarsko-srijemska", Seat: "Vukovar"},
	} {
		nuzno(t, repo.CreateCounty(ctx, c))
	}
	for _, m := range []*models.Municipality{
		{CountyID: 1, Name: "Bilje", Type: "OPĆINA"},
		{CountyID: 1, Name: "Belišće", Type: "GRAD"},
	} {
		nuzno(t, repo.CreateMunicipality(ctx, m))
	}
	for _, s := range []*models.Settlement{
		{MunicipalityID: 1, CountyID: 1, Name: "Kopačevo"},
		{MunicipalityID: 1, CountyID: 1, Name: "Lug"},
		{MunicipalityID: 2, CountyID: 1, Name: "Gat"},
	} {
		nuzno(t, repo.CreateSettlement(ctx, s))
	}
	kad := "2026-03-01 08:00:00"
	if _, err := baza.Exec(`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES
		('B.34.1', 34, 'B', 'prva', ?, ?), ('B.34.2', 34, 'B', 'druga', ?, ?)`, kad, kad, kad, kad); err != nil {
		t.Fatal(err)
	}
	return baza, repo
}

// vezaDionice upisuje vezu dionice s jedinicom; naselje 0 je veza s cijelom općinom
func vezaDionice(t *testing.T, baza *sql.DB, id, dionica string, zupanija, opcina, naselje int) {
	t.Helper()
	var n any
	if naselje > 0 {
		n = naselje
	}
	if _, err := baza.Exec(`INSERT INTO section_territories (id, section_code, county_id, municipality_id, settlement_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, id, dionica, zupanija, opcina, n, time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
}

// zatvorenaBaza je baza koja više ne odgovara: svaki upit javlja grešku
func zatvorenaBaza(t *testing.T) *sql.DB {
	t.Helper()
	baza, _ := bazaSPodrucjem(t)
	baza.Close()
	return baza
}

// Popis županija ide po identitetu i prazna polja čita kao prazna, bez greške.
func TestPopisZupanijaPoIdentitetu(t *testing.T) {
	baza, rec := bazaSPodrucjem(t)
	repo := NewTerritoryRepository(baza, rec)
	ctx := context.Background()
	if l, err := repo.ListCounties(ctx); err != nil || len(l) != 0 {
		t.Fatalf("prazan popis: %v, %v", l, err)
	}
	_, repo = teritorijProbe(t)
	l, err := repo.ListCounties(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(l) != 2 {
		t.Fatalf("dvije županije, dobio %d", len(l))
	}
	prva := models.County{ID: 1, Code: "OBŽ", Name: "Osječko-baranjska", Seat: "Osijek", Prefect: "Pero Perić", AreaSqKm: 4155, Population: 259481,
		Email: "zupan@primjer.hr", Phone: "031 000 000", Website: "primjer.hr"}
	if !reflect.DeepEqual(l[0], prva) {
		t.Errorf("prva županija %+v, htio %+v", l[0], prva)
	}
	if l[1].ID != 2 || l[1].Name != "Vukovarsko-srijemska" || l[1].Code != "" || l[1].Prefect != "" || l[1].Population != 0 {
		t.Errorf("druga županija %+v", l[1])
	}
}

// Kad baza ne odgovara, popis županija javlja grešku s opisom
func TestPopisZupanijaKadBazaNeOdgovara(t *testing.T) {
	repo := NewTerritoryRepository(zatvorenaBaza(t), nil)
	if _, err := repo.ListCounties(context.Background()); err == nil || !strings.Contains(err.Error(), "dohvatu županija") {
		t.Errorf("greška %v", err)
	}
}

// Brojevi jedinica prate upisano: dvije županije, dvije općine, tri naselja
func TestBrojTeritorijalnihJedinica(t *testing.T) {
	baza, rec := bazaSPodrucjem(t)
	ctx := context.Background()
	if z, o, n, err := NewTerritoryRepository(baza, rec).GetTerritoryCounts(ctx); err != nil || z != 0 || o != 0 || n != 0 {
		t.Fatalf("prazna baza: %d %d %d %v", z, o, n, err)
	}
	_, repo := teritorijProbe(t)
	z, o, n, err := repo.GetTerritoryCounts(ctx)
	if err != nil || z != 2 || o != 2 || n != 3 {
		t.Errorf("brojevi %d %d %d, %v; htio 2 2 3", z, o, n, err)
	}
	if _, _, _, err := NewTerritoryRepository(zatvorenaBaza(t), nil).GetTerritoryCounts(ctx); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Teritorij dionice nosi imena jedinica; veza s cijelom općinom nema naselja
func TestTeritorijDionice(t *testing.T) {
	baza, repo := teritorijProbe(t)
	ctx := context.Background()
	vezaDionice(t, baza, "v1", "B.34.1", 1, 1, 2) // Lug
	vezaDionice(t, baza, "v2", "B.34.1", 1, 2, 0) // cijeli Belišće
	vezaDionice(t, baza, "v3", "B.34.1", 1, 1, 1) // Kopačevo
	vezaDionice(t, baza, "v4", "B.34.2", 1, 2, 3)

	l, err := repo.GetSectionTerritories(ctx, "B.34.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(l) != 3 {
		t.Fatalf("tri veze, dobio %d", len(l))
	}
	// županija, pa općina, pa naselje po nazivu: Belišće prije Bilja
	var redom []string
	for _, v := range l {
		redom = append(redom, v.MunicipalityName+"/"+v.SettlementName)
	}
	if want := []string{"Belišće/", "Bilje/Kopačevo", "Bilje/Lug"}; !reflect.DeepEqual(redom, want) {
		t.Errorf("redoslijed %v, htio %v", redom, want)
	}
	if l[0].SettlementID != nil || l[0].MunicipalityType != "GRAD" || l[0].CountyName != "Osječko-baranjska" {
		t.Errorf("veza s cijelom općinom: %+v", l[0])
	}
	if l[1].SettlementID == nil || *l[1].SettlementID != 1 || l[1].ID != "v3" || l[1].CreatedAt.IsZero() {
		t.Errorf("veza s naseljem: %+v", l[1])
	}
	if l, err := repo.GetSectionTerritories(ctx, "B.99.9"); err != nil || len(l) != 0 {
		t.Errorf("dionica bez veza: %v, %v", l, err)
	}
	if _, err := NewTerritoryRepository(zatvorenaBaza(t), nil).GetSectionTerritories(ctx, "B.34.1"); err == nil || !strings.Contains(err.Error(), "B.34.1") {
		t.Errorf("greška bez šifre dionice: %v", err)
	}
}

// Zahvaćene dionice traže se po najužoj zadanoj jedinici: naselje ima
// prednost pred općinom, općina pred županijom; bez ijedne nema ničega
func TestDioniceZahvaceneJedinicom(t *testing.T) {
	baza, repo := teritorijProbe(t)
	ctx := context.Background()
	vezaDionice(t, baza, "v1", "B.34.1", 1, 1, 1)
	vezaDionice(t, baza, "v2", "B.34.1", 1, 1, 2)
	vezaDionice(t, baza, "v3", "B.34.2", 1, 2, 3)

	slucajevi := []struct {
		ime                       string
		zupanija, opcina, naselje int
		htio                      []string
	}{
		{"naselje Gat", 1, 1, 3, []string{"B.34.2"}},
		{"općina Bilje bez dvostrukih", 1, 1, 0, []string{"B.34.1"}},
		{"cijela županija", 1, 0, 0, []string{"B.34.1", "B.34.2"}},
		{"druga županija", 2, 0, 0, nil},
		{"ništa zadano", 0, 0, 0, nil},
	}
	for _, s := range slucajevi {
		got, err := repo.GetSectionsAffectedByTerritory(ctx, s.zupanija, s.opcina, s.naselje)
		if err != nil {
			t.Fatalf("%s: %v", s.ime, err)
		}
		slices.Sort(got)
		if !reflect.DeepEqual(got, s.htio) {
			t.Errorf("%s: %v, htio %v", s.ime, got, s.htio)
		}
	}
	if _, err := NewTerritoryRepository(zatvorenaBaza(t), nil).GetSectionsAffectedByTerritory(ctx, 1, 0, 0); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}
