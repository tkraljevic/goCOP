package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/obracun"
)

// Prazan čvor dobiva hrvatski zakon i koeficijente obrasca; drugo punjenje
// ne prepisuje ono što je administrator u međuvremenu promijenio.
func TestObracunPostavkeSePuneJednom(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	r := NewObracunRepository(baza, ledger.New(baza, "test"))
	if err := r.Osiguraj(ctx); err != nil {
		t.Fatal(err)
	}
	ps, err := r.Blagdani(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != len(obracun.ZakonskiBlagdani()) {
		t.Fatalf("blagdana %d, zakon ih ima %d", len(ps), len(obracun.ZakonskiBlagdani()))
	}
	// Iz baze pročitan kalendar daje isto što i ugrađeni: 14 blagdana 2026.,
	// a 8.10. (Dan neovisnosti) više nije blagdan, dok 2019. jest.
	if n := len(ps.Blagdani(2026)); n != 14 {
		t.Errorf("2026: %d blagdana", n)
	}
	if ps.Blagdan(time.Date(2026, 10, 8, 0, 0, 0, 0, models.Zagreb)) {
		t.Error("8.10.2026. nije blagdan")
	}
	if !ps.Blagdan(time.Date(2019, 10, 8, 0, 0, 0, 0, models.Zagreb)) || !ps.Blagdan(time.Date(2019, 6, 25, 0, 0, 0, 0, models.Zagreb)) {
		t.Error("2019.: 8.10. i 25.6. bili su blagdani")
	}
	if ps.Blagdan(time.Date(2019, 5, 30, 0, 0, 0, 0, models.Zagreb)) {
		t.Error("30.5.2019. još nije bio blagdan")
	}

	// Administrator promijeni koeficijent i doda dan žalosti; ponovno
	// osiguravanje to ne dira.
	if err := r.SaveKoeficijent(ctx, Koeficijent{ID: KoeficijentID(obracun.Ured, obracun.BLD), Mjesto: "URED", Razred: "BLD", K: 2.5}); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveBlagdan(ctx, obracun.Pravilo{ID: "dan-zalosti-2026-03-03", Naziv: "Dan žalosti", Vrsta: obracun.Jednokratni, Datum: "2026-03-03"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Osiguraj(ctx); err != nil {
		t.Fatal(err)
	}
	k, err := r.Koeficijenti(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if k[obracun.Ured][obracun.BLD] != 2.5 || k[obracun.Teren][obracun.BLN] != 2.55 {
		t.Errorf("koeficijenti: %v", k)
	}
	ps, _ = r.Blagdani(ctx)
	if !ps.Blagdan(time.Date(2026, 3, 3, 12, 0, 0, 0, models.Zagreb)) || len(ps) != len(obracun.ZakonskiBlagdani())+1 {
		t.Errorf("dan žalosti nije ostao: %d pravila", len(ps))
	}

	// Maknuto nestaje s površine i ostaje u knjizi kao arhivirano.
	if err := r.MakniBlagdan(ctx, "dan-zalosti-2026-03-03"); err != nil {
		t.Fatal(err)
	}
	ps, _ = r.Blagdani(ctx)
	if len(ps) != len(obracun.ZakonskiBlagdani()) {
		t.Errorf("poslije micanja %d pravila", len(ps))
	}
	var arhivirano int
	if err := baza.QueryRow(`SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = ? AND archived = 1`, EntityBlagdani, "dan-zalosti-2026-03-03").Scan(&arhivirano); err != nil {
		t.Fatal(err)
	}
	if arhivirano != 1 {
		t.Errorf("u knjizi %d arhiviranih verzija, očekuje 1", arhivirano)
	}
}
