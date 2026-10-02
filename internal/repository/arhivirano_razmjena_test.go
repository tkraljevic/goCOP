package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// cvorZaRazmjenu je baza s knjigom jednog čvora, s ustrojem koji dionice
// trebaju (sektor B, područje 16) upisanim mimo knjige, kao iz početnih podataka
type cvorZaRazmjenu struct {
	db  *sql.DB
	rec *ledger.Recorder
	do  string // zadnja verzija s ovog čvora koju je drugi primio
}

func noviCvorZaRazmjenu(t *testing.T, ime string) *cvorZaRazmjenu {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), ime+".db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO', 'COP')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (16, 'B', 'BP 16', 'VGI')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return &cvorZaRazmjenu{db: baza, rec: ledger.New(baza, ime)}
}

// posalji prenosi drugom čvoru sve nove verzije (ili samo zadanog entiteta)
// onako kako to radi razmjena: u knjigu, pa na površinu
func (a *cvorZaRazmjenu) posalji(t *testing.T, b *cvorZaRazmjenu, samo string) {
	t.Helper()
	ctx := context.Background()
	vs, err := a.rec.Since(ctx, a.do, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) == 0 {
		t.Fatal("nema novih verzija za poslati")
	}
	a.do = vs[len(vs)-1].VersionID
	var saljem []ledger.Version
	for _, v := range vs {
		if samo == "" || v.Entity == samo {
			saljem = append(saljem, v)
		}
	}
	if _, err := b.rec.Apply(ctx, saljem); err != nil {
		t.Fatal(err)
	}
	if err := ApplyVersions(ctx, b.db, b.rec, saljem); err != nil {
		t.Fatal(err)
	}
}

func brojRedaka(t *testing.T, baza *sql.DB, upit string, args ...any) int {
	t.Helper()
	var n int
	if err := baza.QueryRow(upit, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Arhivirana epizoda obrane nestaje i s površine čvora koji ju je primio
func TestArhiviranaEpizodaNestajeNaDrugomCvoru(t *testing.T) {
	ctx := context.Background()
	a, b := noviCvorZaRazmjenu(t, "a"), noviCvorZaRazmjenu(t, "b")
	sec := &models.Section{Code: "B.16.1", AreaID: 16, SectorID: "B", Description: "probna"}
	if err := NewSectionRepository(a.db, a.rec).SaveSection(ctx, sec); err != nil {
		t.Fatal(err)
	}
	e := &models.DefenseEpisode{SectionCode: "B.16.1", StartedAt: time.Date(2026, 3, 1, 6, 0, 0, 0, time.UTC),
		Phase: models.PhaseRegular, Origin: models.EpisodeFromReadings}
	if err := NewEpisodeRepository(a.db, a.rec).SaveEpisode(ctx, e); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, "")
	if n := brojRedaka(t, b.db, `SELECT COUNT(*) FROM defense_episodes WHERE id = ?`, e.ID.String()); n != 1 {
		t.Fatalf("epizoda nije stigla: %d", n)
	}

	// lokalnog arhiviranja epizode (još) nema; verziju piše kao noviji program
	if _, err := a.rec.Archive(ctx, a.db, EntityEpisodes, e.ID.String(), e); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, "")
	if n := brojRedaka(t, b.db, `SELECT COUNT(*) FROM defense_episodes WHERE id = ?`, e.ID.String()); n != 0 {
		t.Errorf("arhivirana epizoda ostala na drugom čvoru")
	}
}

// Arhivirani ispravak arhive nestaje i s površine čvora koji ga je primio
func TestArhiviraniIspravakNestajeNaDrugomCvoru(t *testing.T) {
	ctx := context.Background()
	a, b := noviCvorZaRazmjenu(t, "a"), noviCvorZaRazmjenu(t, "b")
	staro := 120.0
	isp := []models.ArhivaIspravak{{Letva: "vukovar", Velicina: "vodostaj", Korak: "satni",
		Vrijeme: time.Date(2026, 6, 12, 11, 0, 0, 0, time.UTC), Vrijednost: 123, Staro: &staro, Razlog: "proba"}}
	if _, err := NewIspravakRepository(a.db, a.rec).Spremi(ctx, isp); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, "")
	if n := brojRedaka(t, b.db, `SELECT COUNT(*) FROM arhiva_ispravci`); n != 1 {
		t.Fatalf("ispravak nije stigao: %d", n)
	}

	if _, err := a.rec.Archive(ctx, a.db, EntityIspravci, isp[0].ID.String(), isp[0]); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, "")
	if n := brojRedaka(t, b.db, `SELECT COUNT(*) FROM arhiva_ispravci`); n != 0 {
		t.Errorf("arhivirani ispravak ostao na drugom čvoru")
	}
}

// Obrisana bilješka (prazan tekst) nestaje i s površine čvora koji ju je primio
func TestObrisanaBiljeskaNestajeNaDrugomCvoru(t *testing.T) {
	ctx := context.Background()
	a, b := noviCvorZaRazmjenu(t, "a"), noviCvorZaRazmjenu(t, "b")
	kad := time.Date(2026, 6, 12, 11, 11, 0, 0, time.UTC)
	repo := NewBiljeskaRepository(a.db, a.rec)
	if _, err := repo.Spremi(ctx, []models.ArhivaBiljeska{{Letva: "vukovar", Velicina: "vodostaj", Korak: "satni",
		Vrijeme: kad, Vrsta: models.BiljeskaVrh, Tekst: "očitan maksimum", Tko: "Ivan"}}); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, "")
	if n := brojRedaka(t, b.db, `SELECT COUNT(*) FROM arhiva_biljeske`); n != 1 {
		t.Fatalf("bilješka nije stigla: %d", n)
	}

	if _, err := repo.Spremi(ctx, []models.ArhivaBiljeska{{Letva: "vukovar", Velicina: "vodostaj", Korak: "satni",
		Vrijeme: kad}}); err != nil {
		t.Fatal(err)
	}
	if n := brojRedaka(t, a.db, `SELECT COUNT(*) FROM arhiva_biljeske`); n != 0 {
		t.Fatalf("bilješka nije obrisana na izvoru: %d", n)
	}
	a.posalji(t, b, "")
	if n := brojRedaka(t, b.db, `SELECT COUNT(*) FROM arhiva_biljeske`); n != 0 {
		t.Errorf("obrisana bilješka ostala na drugom čvoru")
	}
}

// Arhivirani nazivi razina na drugom čvoru vraćaju zadane, kao da ih nitko
// nije upisao
func TestArhiviraniNaziviRazinaNaDrugomCvoru(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() { models.SetTerms(models.DefaultTerms()) })
	a, b := noviCvorZaRazmjenu(t, "a"), noviCvorZaRazmjenu(t, "b")
	org := NewOrgRepository(a.db, a.rec)
	terms := models.DefaultTerms()
	terms.OrgName = "Probna organizacija"
	if err := org.SaveTerms(ctx, terms); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, "")
	if n := brojRedaka(t, b.db, `SELECT COUNT(*) FROM org_terms`); n != 1 {
		t.Fatalf("nazivi nisu stigli: %d", n)
	}

	if _, err := a.rec.Archive(ctx, a.db, EntityOrgTerms, models.TermsID, terms); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, "")
	if n := brojRedaka(t, b.db, `SELECT COUNT(*) FROM org_terms`); n != 0 {
		t.Errorf("arhivirani nazivi ostali na drugom čvoru")
	}
	got, err := NewOrgRepository(b.db, b.rec).GetTerms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.OrgName != models.DefaultTerms().OrgName {
		t.Errorf("drugi čvor nema zadane nazive: %q", got.OrgName)
	}
}

// Veza dionice i letve, arhivirana pri brisanju letve, nestaje i na čvoru
// koji je primio samo nju (letva tamo još stoji)
func TestArhiviranaVezaDioniceILetveNaDrugomCvoru(t *testing.T) {
	ctx := context.Background()
	a, b := noviCvorZaRazmjenu(t, "a"), noviCvorZaRazmjenu(t, "b")
	st := NewStationRepository(a.db, a.rec)
	letva := &models.Station{Code: "batina", Name: "Batina", Watercourse: "Dunav"}
	if err := st.CreateStation(ctx, letva); err != nil {
		t.Fatal(err)
	}
	sec := &models.Section{Code: "B.16.1", AreaID: 16, SectorID: "B", Description: "probna",
		Parts: []models.SectionPart{{Seq: 1, Description: "probna", StationIDs: []string{letva.ID.String()}}}}
	if err := NewSectionRepository(a.db, a.rec).SaveSection(ctx, sec); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, "")
	if n := brojRedaka(t, b.db, `SELECT COUNT(*) FROM section_stations WHERE station_id = ?`, letva.ID.String()); n != 1 {
		t.Fatalf("veza nije stigla: %d", n)
	}

	if err := st.DeleteStation(ctx, letva.ID); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, EntitySectionStations)
	if n := brojRedaka(t, b.db, `SELECT COUNT(*) FROM section_stations WHERE station_id = ?`, letva.ID.String()); n != 0 {
		t.Errorf("arhivirana veza ostala na drugom čvoru")
	}
	if n := brojRedaka(t, b.db, `SELECT COUNT(*) FROM stations WHERE id = ?`, letva.ID.String()); n != 1 {
		t.Errorf("letva je nestala, a njezina verzija nije poslana: %d", n)
	}
}
