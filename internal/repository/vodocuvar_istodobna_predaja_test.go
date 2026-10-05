package repository

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Dva lista s istim otvorenim zadatkom predaju se istodobno (dvije
// kartice): oba pročitaju zadatak kao otvoren, a evidenciju smije zaključiti
// samo prvi. Predaja drugog zadatak ne prepisuje nego se odbija s
// ErrZadatakZakljucen, bez upisa lista i bez verzije u knjizi; servis tada
// ponovno čita evidenciju. Podaci su izmišljeni: sektor P, područje 1,
// vodočuvar Pero Perić.
func TestPredajaNePrepisujeZadatakZakljucenUMeduvremenu(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "istodobna.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	repo := NewVodocuvarRepository(baza, ledger.New(baza, "cvor-probni"))
	pperic := uuid.NewString()
	dan := time.Date(2026, time.March, 10, 0, 0, 0, 0, models.Zagreb)
	predan := func(d time.Time) *models.VodocuvarskiList {
		kad := time.Now()
		return &models.VodocuvarskiList{UserID: pperic, Ime: "Pero Perić", Sektor: "P", AreaID: 1, Datum: d, Broj: 1, Od: "07:00", Do: "15:00",
			Opis: "obilazak nasipa", PredanoAt: &kad}
	}
	z := &models.Zadatak{UserID: pperic, Sektor: "P", AreaID: 1, Tekst: "pregledati ustavu", Zadao: "Pero Perić", Za: dan, Status: models.ZadatakOtvoren}
	if err := repo.SaveZadatak(ctx, z); err != nil {
		t.Fatal(err)
	}

	// obje kartice pročitale su zadatak kao otvoren
	prva, err := repo.GetZadatak(ctx, z.ID)
	if err != nil {
		t.Fatal(err)
	}
	druga, err := repo.GetZadatak(ctx, z.ID)
	if err != nil {
		t.Fatal(err)
	}
	kad := time.Now()
	prva.Status, prva.Obavljeno, prva.ObavljenoAt = models.ZadatakObavljen, "pregledana", &kad
	druga.Status, druga.Obavljeno, druga.ObavljenoAt = models.ZadatakOdbacen, "ustava srušena", &kad

	l1 := predan(dan)
	if err := repo.Predaj(ctx, l1, []*models.Zadatak{prva}); err != nil {
		t.Fatal(err)
	}
	l2 := predan(dan.AddDate(0, 0, 1))
	if err := repo.Predaj(ctx, l2, []*models.Zadatak{druga}); !errors.Is(err, ErrZadatakZakljucen) {
		t.Fatalf("predaja sa zadatkom zaključenim u međuvremenu: %v", err)
	}
	if ev, _ := repo.GetZadatak(ctx, z.ID); ev.Status != models.ZadatakObavljen || ev.Obavljeno != "pregledana" || ev.ListID != l1.ID {
		t.Errorf("evidencija nakon istodobne predaje: %+v", ev)
	}
	if b, _ := repo.ZaDan(ctx, pperic, l2.Datum); b != nil {
		t.Errorf("drugi list upisan iako je predaja odbijena: %+v", b)
	}
	var n int
	if err := baza.QueryRow(`SELECT count(*) FROM record_versions WHERE entity_id = ?`, z.ID).Scan(&n); err != nil || n != 2 {
		t.Errorf("verzije zadatka u knjizi: %d %v", n, err)
	}

	// zadatak kojeg u evidenciji nema ne smeta: upisuje se s listom, kao i dosad
	nov := &models.Zadatak{ID: uuid.NewString(), UserID: pperic, Sektor: "P", AreaID: 1, Tekst: "obići nasip", Za: dan,
		Status: models.ZadatakObavljen, Obavljeno: "obiđen", ObavljenoAt: &kad}
	l3 := predan(dan.AddDate(0, 0, 2))
	if err := repo.Predaj(ctx, l3, []*models.Zadatak{nov}); err != nil {
		t.Fatalf("predaja sa zadatkom kojeg nema u evidenciji: %v", err)
	}
	if ev, _ := repo.GetZadatak(ctx, nov.ID); ev == nil || ev.Status != models.ZadatakObavljen || ev.ListID != l3.ID {
		t.Errorf("zadatak kojeg nije bilo u evidenciji: %+v", ev)
	}

	// stanje zadatka ne može se pročitati: predaja se ne upisuje
	if _, err := baza.Exec(`ALTER TABLE vodocuvarski_zadaci RENAME TO vodocuvarski_zadaci_kvar`); err != nil {
		t.Fatal(err)
	}
	l4 := predan(dan.AddDate(0, 0, 3))
	if err := repo.Predaj(ctx, l4, []*models.Zadatak{nov}); err == nil || !strings.Contains(err.Error(), "stanje zadatka") {
		t.Errorf("predaja bez čitljive evidencije zadataka: %v", err)
	}
	if b, _ := repo.ZaDan(ctx, pperic, l4.Datum); b != nil {
		t.Errorf("list upisan iako se stanje zadatka nije moglo pročitati: %+v", b)
	}
}
