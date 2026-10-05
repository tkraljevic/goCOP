package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Krug akata koji stupaju na snagu radi odmah i zatim u razmaku, grešku
// zapiše u dnevnik i ide dalje, a staje kad stane čvor
func TestKrugAkataNaSnazi(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "krug-akata.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	epizode := service.NewEpisodeService(repository.NewEpisodeRepository(baza, rec), nil, nil)
	akti := service.NewAktService(repository.NewAktiRepository(baza, rec), nil, nil, nil, nil, nil, epizode, "cvor-probni")
	baza.Close() // svaki krug javlja grešku

	var zapis bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&zapis)
	t.Cleanup(func() { log.SetOutput(stari) })

	ctx, stop := context.WithCancel(context.Background())
	gotovo := make(chan struct{})
	go func() {
		pratiAkteNaSnazi(ctx, akti, time.Millisecond)
		close(gotovo)
	}()
	time.Sleep(20 * time.Millisecond)
	stop()
	select {
	case <-gotovo:
	case <-time.After(time.Second):
		t.Fatal("krug nije stao kad je stao čvor")
	}
	if n := strings.Count(zapis.String(), "povijest obrane:"); n < 2 {
		t.Errorf("očekivano više krugova s greškom, zapisano %d:\n%s", n, zapis.String())
	}
}

// Prolaz s greškom (npr. prolazni SQLITE_BUSY) ne pomiče granicu kruga: akt
// koji je stupio na snagu u tom razdoblju ulazi u povijest u idućem čistom
// prolazu, a ne tek s novom ovjerom ili stornom u sektoru
func TestKrugAkataNaSnaziGranicaSamoNakonCistogProlaza(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "granica-akata.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo');
		INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '');
		INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	repo := repository.NewAktiRepository(baza, rec)
	epizode := repository.NewEpisodeRepository(baza, rec)
	akti := service.NewAktService(repo, nil, nil, nil, nil, nil, service.NewEpisodeService(epizode, nil, nil), "cvor-probni")
	ctx := context.Background()
	stari := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(stari) })

	sad := time.Now().UTC().Truncate(time.Second)
	zadnji := sad.Add(-time.Hour)
	if err := repo.SaveAkt(ctx, &models.Akt{ID: "u1", Sektor: "P", AreaID: 1, Radnja: models.AktUspostava, Stupanj: models.PhasePrep,
		Status: models.AktOvjeren, Broj: 1, Godina: 2026, OvjerioID: "pperic", OvjeraKod: "KOD-u1", Vrijedi: sad.Add(-30 * time.Minute),
		Dionice: []models.AktDionica{{Code: "P.1.1"}}}); err != nil {
		t.Fatal(err)
	}

	// akti se privremeno ne daju pročitati: granica ostaje
	if _, err := baza.Exec(`ALTER TABLE akti RENAME TO akti_zauzeto`); err != nil {
		t.Fatal(err)
	}
	if g := prolazAkataNaSnazi(ctx, akti, zadnji, sad); !g.Equal(zadnji) {
		t.Fatalf("prolaz s greškom pomaknuo je granicu na %v", g)
	}
	if _, err := baza.Exec(`ALTER TABLE akti_zauzeto RENAME TO akti`); err != nil {
		t.Fatal(err)
	}

	// idući prolaz obuhvati i propušteno razdoblje, pa granicu pomakne
	poslije := sad.Add(10 * time.Minute)
	if g := prolazAkataNaSnazi(ctx, akti, zadnji, poslije); !g.Equal(poslije) {
		t.Fatalf("čist prolaz nije pomaknuo granicu: %v", g)
	}
	if e, err := epizode.OpenEpisode(ctx, "P.1.1"); err != nil || e == nil {
		t.Fatalf("akt koji je stupio na snagu u prolazu s greškom nije u povijesti: %+v (%v)", e, err)
	}
}
