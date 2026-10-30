package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// vodeProbe diže bazu s tri vode, dionicama i letvama: Drava ima dvije
// dionice, Karašica letvu, Vučica ništa; jedna dionica i jedna letva nisu
// vezane na registar
func vodeProbe(t *testing.T) (*sql.DB, *ledger.Recorder, *WatercourseRepository) {
	t.Helper()
	baza, rec := bazaSPodrucjem(t)
	repo := NewWatercourseRepository(baza, rec)
	ctx := context.Background()
	for _, w := range []*models.Watercourse{
		{Code: "rijeka-drava", OfficialName: "rijeka Drava", Name: "Drava", Kind: "rijeka", Category: "I_RED"},
		{Code: "potok-karasica", OfficialName: "potok Karašica", Name: "karašica", Kind: "potok", Category: "II_RED"},
		{Code: "rijeka-vucica", OfficialName: "rijeka Vučica", Name: "Vučica", Kind: "rijeka"},
	} {
		nuzno(t, repo.CreateWatercourse(ctx, w))
	}
	kad := "2026-03-01 08:00:00"
	for _, q := range []string{
		`INSERT INTO sections (code, area_id, sector_id, description, watercourse_code, created_at, updated_at) VALUES
			('B.34.10', 34, 'B', 'deseta', 'rijeka-drava', '` + kad + `', '` + kad + `'),
			('B.34.2', 34, 'B', 'druga', 'rijeka-drava', '` + kad + `', '` + kad + `'),
			('B.34.3', 34, 'B', 'treća', '', '` + kad + `', '` + kad + `')`,
		`INSERT INTO stations (id, code, name, watercourse_code, created_at, updated_at) VALUES
			('0f1e2d3c-0000-4000-8000-000000000001', 'L1', 'Petlovac', 'potok-karasica', '` + kad + `', '` + kad + `'),
			('` + letvaBelisce + `', 'L2', 'Belišće', '', '` + kad + `', '` + kad + `')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return baza, rec, repo
}

// letvaBelisce je letva koja još nije vezana na vodu
const letvaBelisce = "0f1e2d3c-0000-4000-8000-000000000002"

func sifreVoda(l []models.Watercourse) []string {
	var out []string
	for _, w := range l {
		out = append(out, w.Code)
	}
	return out
}

// Popis voda sužava se pretragom, kategorijom (i vodama bez kategorije) i
// samo onima koje imaju dionicu ili letvu; slaže se po nazivu bez obzira na
// veličinu slova
func TestPopisVodaSFiltrima(t *testing.T) {
	_, _, repo := vodeProbe(t)
	ctx := context.Background()
	slucajevi := []struct {
		ime               string
		trazi, kategorija string
		samoUUpotrebi     bool
		htio              []string
	}{
		{"sve po nazivu", "", "", false, []string{"rijeka-drava", "potok-karasica", "rijeka-vucica"}},
		{"pretraga po službenom nazivu", " potok ", "", false, []string{"potok-karasica"}},
		{"kategorija", "", "I_RED", false, []string{"rijeka-drava"}},
		{"bez kategorije", "", "NIJE_I_REDA", false, []string{"rijeka-vucica"}},
		{"u upotrebi", "", "", true, []string{"rijeka-drava", "potok-karasica"}},
		{"pretraga bez pogotka", "Sava", "", false, nil},
	}
	for _, s := range slucajevi {
		l, err := repo.ListWatercourses(ctx, s.trazi, s.kategorija, s.samoUUpotrebi)
		if err != nil {
			t.Fatalf("%s: %v", s.ime, err)
		}
		if got := sifreVoda(l); !reflect.DeepEqual(got, s.htio) {
			t.Errorf("%s: %v, htio %v", s.ime, got, s.htio)
		}
	}
	l, _ := repo.ListWatercourses(ctx, "Drava", "", false) // probni podaci su ispravni; kvar bi pokazala provjera niže
	if len(l) != 1 || l[0].SectionCount != 2 || l[0].StationCount != 0 {
		t.Errorf("Drava: %+v", l)
	}
	if _, err := NewWatercourseRepository(zatvorenaBaza(t), nil).ListWatercourses(ctx, "", "", false); err == nil || !strings.Contains(err.Error(), "vodnih tijela") {
		t.Errorf("greška %v", err)
	}
}

// Kategorije su samo one zastupljene, bez prazne, abecedno
func TestKategorijeVoda(t *testing.T) {
	_, _, repo := vodeProbe(t)
	ctx := context.Background()
	l, err := repo.ListCategories(ctx)
	if err != nil || !reflect.DeepEqual(l, []string{"II_RED", "I_RED"}) {
		t.Errorf("kategorije %v, %v", l, err)
	}
	if _, err := NewWatercourseRepository(zatvorenaBaza(t), nil).ListCategories(ctx); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Brojanje voda: ukupno, u upotrebi, te dionice i letve bez vode
func TestBrojVodaIOnihBezVode(t *testing.T) {
	_, _, repo := vodeProbe(t)
	ctx := context.Background()
	ukupno, uUpotrebi, dionica, letava, err := repo.CountWatercourses(ctx)
	if err != nil || ukupno != 3 || uUpotrebi != 2 || dionica != 1 || letava != 1 {
		t.Errorf("brojevi %d %d %d %d, %v; htio 3 2 1 1", ukupno, uUpotrebi, dionica, letava, err)
	}
	if _, _, _, _, err := NewWatercourseRepository(zatvorenaBaza(t), nil).CountWatercourses(ctx); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Upotreba vode broji njene dionice i letve
func TestUpotrebaVode(t *testing.T) {
	_, _, repo := vodeProbe(t)
	ctx := context.Background()
	for kod, htio := range map[string][2]int{"rijeka-drava": {2, 0}, "potok-karasica": {0, 1}, "rijeka-vucica": {0, 0}, "nema-je": {0, 0}} {
		d, l, err := repo.CountUsage(ctx, kod)
		if err != nil || d != htio[0] || l != htio[1] {
			t.Errorf("%s: %d dionica, %d letava, %v; htio %v", kod, d, l, err, htio)
		}
	}
	if _, _, err := NewWatercourseRepository(zatvorenaBaza(t), nil).CountUsage(ctx, "rijeka-drava"); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Brisanje vode miče je s površine, a u knjizi ostaje arhivirana sa zadnjim stanjem
func TestBrisanjeVodeOstajeUKnjizi(t *testing.T) {
	_, rec, repo := vodeProbe(t)
	ctx := context.Background()
	nuzno(t, repo.DeleteWatercourse(ctx, "rijeka-vucica"))
	if w, err := repo.GetWatercourse(ctx, "rijeka-vucica"); err != nil || w != nil {
		t.Fatalf("voda je ostala: %+v, %v", w, err)
	}
	v, err := rec.Latest(ctx, EntityWatercourses, "rijeka-vucica")
	if err != nil || v == nil || !v.Archived {
		t.Fatalf("zadnja verzija %+v, %v", v, err)
	}
	var zapis models.Watercourse
	if err := json.Unmarshal(v.Payload, &zapis); err != nil || zapis.OfficialName != "rijeka Vučica" {
		t.Errorf("spomenik bez stanja vode: %s", v.Payload)
	}
	if l, err := repo.ListWatercourses(ctx, "", "", false); err != nil || len(l) != 2 {
		t.Errorf("ostalo %d voda, htio 2", len(l))
	}
}

// Pridruživanje vode letvi upisuje šifru, naziv i izvor i bilježi letvu u
// knjigu; nepostojeća letva javlja grešku bez verzije
func TestPridruzivanjeVodeLetvi(t *testing.T) {
	baza, rec, repo := vodeProbe(t)
	ctx := context.Background()
	nuzno(t, repo.SetStationWatercourse(ctx, letvaBelisce, "rijeka-drava", "Drava", "RUČNO"))
	var kod, naziv, izvor string
	if err := baza.QueryRow(`SELECT watercourse_code, watercourse, watercourse_source FROM stations WHERE id = ?`, letvaBelisce).Scan(&kod, &naziv, &izvor); err != nil {
		t.Fatal(err)
	}
	if kod != "rijeka-drava" || naziv != "Drava" || izvor != "RUČNO" {
		t.Errorf("letva: %q %q %q", kod, naziv, izvor)
	}
	v, err := rec.Latest(ctx, EntityStations, letvaBelisce)
	if err != nil || v == nil || !strings.Contains(string(v.Payload), "rijeka-drava") {
		t.Errorf("verzija letve %+v, %v", v, err)
	}
	if d, l, err := repo.CountUsage(ctx, "rijeka-drava"); err != nil || d != 2 || l != 1 {
		t.Errorf("Drava sad ima %d dionica i %d letava", d, l)
	}

	err = repo.SetStationWatercourse(ctx, "0f1e2d3c-0000-4000-8000-0000000000ff", "rijeka-drava", "Drava", "RUČNO")
	if err == nil || !strings.Contains(err.Error(), "nije pronađena") {
		t.Errorf("nepostojeća letva: %v", err)
	}
	if v, err := rec.Latest(ctx, EntityStations, "0f1e2d3c-0000-4000-8000-0000000000ff"); err == nil && v != nil { // ne postoji: greška ili prazno
		t.Errorf("verzija za nepostojeću letvu: %+v", v)
	}
}

// Dionice vode slažu se po broju, ne slovno: B.34.2 prije B.34.10
func TestDioniceVodePoBroju(t *testing.T) {
	_, _, repo := vodeProbe(t)
	ctx := context.Background()
	l, err := repo.ListSectionsForWatercourse(ctx, "rijeka-drava")
	if err != nil || !reflect.DeepEqual(l, []string{"B.34.2", "B.34.10"}) {
		t.Errorf("dionice %v, %v", l, err)
	}
	if l, err := repo.ListSectionsForWatercourse(ctx, "rijeka-vucica"); err != nil || len(l) != 0 {
		t.Errorf("voda bez dionica: %v, %v", l, err)
	}
	if _, err := NewWatercourseRepository(zatvorenaBaza(t), nil).ListSectionsForWatercourse(ctx, "rijeka-drava"); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}
