package ledger

import (
	"context"
	"testing"
	"time"
)

// Čišćenje briše samo verzije koje razmjena danas ne bi primila; ispravne,
// i one malo ispred sata, ostaju
func TestObrisiNeispravne(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "laptop-baranja")
	ctx := context.Background()
	sada := time.Now()
	ubaci := func(id, autor string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
			VALUES (?, 'stations', ?, ?, '', 0, '{}', ?, 1, '')`, id, id, autor, sada.UTC()); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := rec.ObrisiNeispravne(ctx, sada); err != nil || n != 0 {
		t.Fatalf("prazna knjiga: %d %v", n, err)
	}
	dobra := oznakaU(sada.Add(-time.Hour), "000000000001")
	ubaci(dobra, "cop-osijek")
	ubaci(oznakaU(sada.Add(NajviseUnaprijed-time.Second), "000000000002"), "cop-osijek")
	ubaci("~", "cop-osijek")
	ubaci("ffffffff-ffff-7fff-bfff-ffffffffffff", "cop-osijek")
	ubaci(oznakaU(sada.Add(-2*time.Hour), "000000000003"), "cop|osijek")
	n, err := rec.ObrisiNeispravne(ctx, sada)
	if err != nil || n != 3 {
		t.Fatalf("obrisano %d, %v; očekivano 3", n, err)
	}
	if n := brojVerzija(t, db); n != 2 {
		t.Errorf("u knjizi %d verzija, očekivano 2", n)
	}
	if ostalo, _, err := rec.NeispravneUKnjizi(ctx, sada, 0); err != nil || ostalo != 0 {
		t.Errorf("nakon čišćenja neispravnih %d, %v", ostalo, err)
	}
	if v, err := rec.Latest(ctx, "stations", dobra); err != nil || v == nil {
		t.Errorf("ispravna verzija je obrisana: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.ObrisiNeispravne(ctx, sada); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
}

// Kad brisanje ne prođe, knjiga ostaje kakva je bila
func TestObrisiNeispravneKadBrisanjeNeProde(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "laptop-baranja")
	if _, err := db.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
		VALUES ('~', 'stations', 's1', 'cop-osijek', '', 0, '{}', ?, 1, '')`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER bez_brisanja BEFORE DELETE ON record_versions BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`); err != nil {
		t.Fatal(err)
	}
	if n, err := rec.ObrisiNeispravne(context.Background(), time.Now()); err == nil || n != 0 {
		t.Fatalf("obrisano %d, %v; očekivana greška", n, err)
	}
	if n := brojVerzija(t, db); n != 1 {
		t.Errorf("u knjizi %d verzija, očekivano 1", n)
	}
}
