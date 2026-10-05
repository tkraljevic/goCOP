package repository

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
)

// Predaja lista je jedna transakcija: list i zadaci koje zaključuje upisuju
// se zajedno, s verzijama u knjizi, ili se ne upisuje ništa. Podaci su
// izmišljeni: sektor P, područje 1, vodočuvar Pero Perić.
func TestPredajaListaIZadatakaUJednojTransakciji(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "predaja.db"))
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
	zadatak := func(tekst string) *models.Zadatak {
		t.Helper()
		z := &models.Zadatak{UserID: pperic, Sektor: "P", AreaID: 1, Tekst: tekst, Zadao: "Pero Perić", Za: dan, Status: models.ZadatakOtvoren}
		if err := repo.SaveZadatak(ctx, z); err != nil {
			t.Fatal(err)
		}
		return z
	}
	verzija := func(id string) int {
		t.Helper()
		var n int
		if err := baza.QueryRow(`SELECT count(*) FROM record_versions WHERE entity_id = ?`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	predan := func(d time.Time) *models.VodocuvarskiList {
		kad := time.Now()
		return &models.VodocuvarskiList{UserID: pperic, Ime: "Pero Perić", Sektor: "P", AreaID: 1, Datum: d, Broj: 1, Od: "07:00", Do: "15:00",
			Opis: "obilazak nasipa", PredanoAt: &kad}
	}

	ustava := zadatak("pregledati ustavu")
	l := predan(dan)
	kad := time.Now()
	ustava.Status, ustava.Obavljeno, ustava.ObavljenoAt = models.ZadatakObavljen, "pregledana", &kad
	if err := repo.Predaj(ctx, l, []*models.Zadatak{ustava}); err != nil {
		t.Fatal(err)
	}
	if b, err := repo.Get(ctx, l.ID); err != nil || b == nil || !b.Predan() {
		t.Fatalf("predan list: %+v %v", b, err)
	}
	if z, _ := repo.GetZadatak(ctx, ustava.ID); z.Status != models.ZadatakObavljen || z.ListID != l.ID || z.ObavljenoAt == nil {
		t.Errorf("zaključen zadatak: %+v", z)
	}
	if verzija(l.ID) != 1 || verzija(ustava.ID) != 2 {
		t.Errorf("verzije u knjizi: list %d, zadatak %d", verzija(l.ID), verzija(ustava.ID))
	}

	// zaključivanje ne uspije: ni list nije upisan, ni u knjizi
	nasip := zadatak("obići nasip")
	if _, err := baza.Exec(`CREATE TRIGGER vd_bez_zakljucivanja BEFORE UPDATE ON vodocuvarski_zadaci BEGIN SELECT RAISE(ABORT, 'zadatak zaključan'); END`); err != nil {
		t.Fatal(err)
	}
	drugi := predan(dan.AddDate(0, 0, 1))
	nasip.Status, nasip.Obavljeno = models.ZadatakOdbacen, "nasip pod vodom"
	if err := repo.Predaj(ctx, drugi, []*models.Zadatak{nasip}); err == nil || !strings.Contains(err.Error(), "zadatak zaključan") {
		t.Fatalf("predaja sa zaključanim zadatkom: %v", err)
	}
	if b, _ := repo.ZaDan(ctx, pperic, drugi.Datum); b != nil {
		t.Errorf("list upisan iako zadatak nije zaključen: %+v", b)
	}
	if z, _ := repo.GetZadatak(ctx, nasip.ID); !z.Otvoren() || z.ListID != "" {
		t.Errorf("zadatak nakon neuspjele predaje: %+v", z)
	}
	if verzija(drugi.ID) != 0 || verzija(nasip.ID) != 1 {
		t.Errorf("neuspjela predaja u knjizi: list %d, zadatak %d", verzija(drugi.ID), verzija(nasip.ID))
	}

	// neuspio upis lista ne upisuje ni zadatke
	if _, err := baza.Exec(`DROP TRIGGER vd_bez_zakljucivanja`); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`CREATE TRIGGER vd_bez_lista BEFORE INSERT ON vodocuvarski_listovi BEGIN SELECT RAISE(ABORT, 'upis zabranjen'); END`); err != nil {
		t.Fatal(err)
	}
	if err := repo.Predaj(ctx, predan(dan.AddDate(0, 0, 2)), []*models.Zadatak{nasip}); err == nil || !strings.Contains(err.Error(), "upis zabranjen") {
		t.Fatalf("predaja s neuspjelim upisom lista: %v", err)
	}
	if z, _ := repo.GetZadatak(ctx, nasip.ID); !z.Otvoren() {
		t.Errorf("zadatak zaključen bez lista: %+v", z)
	}
	// ni samostalan upis lista ni zadatka ne prolazi kad baza odbije upis
	if err := repo.Save(ctx, predan(dan.AddDate(0, 0, 4))); err == nil || !strings.Contains(err.Error(), "upis zabranjen") {
		t.Errorf("upis lista: %v", err)
	}
	if _, err := baza.Exec(`CREATE TRIGGER vd_bez_zadatka BEFORE INSERT ON vodocuvarski_zadaci BEGIN SELECT RAISE(ABORT, 'zadatak zabranjen'); END`); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveZadatak(ctx, &models.Zadatak{UserID: pperic, Sektor: "P", AreaID: 1, Tekst: "očistiti propust"}); err == nil || !strings.Contains(err.Error(), "zadatak zabranjen") {
		t.Errorf("upis zadatka: %v", err)
	}
	// prekinut kontekst: transakcija ne počinje
	prekinut, odustani := context.WithCancel(ctx)
	odustani()
	if err := repo.Predaj(prekinut, predan(dan.AddDate(0, 0, 3)), nil); err == nil {
		t.Error("predaja s prekinutim kontekstom")
	}
	if err := repo.Save(prekinut, predan(dan.AddDate(0, 0, 3))); err == nil {
		t.Error("upis lista s prekinutim kontekstom")
	}
	if err := repo.SaveZadatak(prekinut, nasip); err == nil {
		t.Error("upis zadatka s prekinutim kontekstom")
	}
}
