package main

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"

	_ "modernc.org/sqlite"
)

// knjigaZaProvjeru je prazna knjiga verzija u memoriji
func knjigaZaProvjeru(t *testing.T) (*sql.DB, *ledger.Recorder) {
	t.Helper()
	baza, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if _, err := baza.Exec(ledger.Schema); err != nil {
		t.Fatal(err)
	}
	return baza, ledger.New(baza, "laptop-baranja")
}

// Pokretanje u dnevnik javi koliko je u knjizi verzija koje razmjena ne bi
// primila i navede prve; ispravna knjiga ne daje redak, a knjiga koja se ne
// da pročitati daje razlog.
func TestJaviNeispravneUKnjizi(t *testing.T) {
	ctx := context.Background()
	baza, rec := knjigaZaProvjeru(t)
	sada := time.Now()
	if _, err := rec.Record(ctx, baza, "stations", "s1", map[string]string{"id": "s1"}); err != nil {
		t.Fatal(err)
	}
	if poruka := neispravneUKnjizi(ctx, rec, sada); poruka != "" {
		t.Errorf("ispravna knjiga: %q", poruka)
	}
	javiNeispravneUKnjizi(ctx, rec, sada)
	for i, id := range []string{"~", "ffffffff-ffff-7fff-bfff-ffffffffffff", "a\nb", "1", "2", "3", "4"} {
		if _, err := baza.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
			VALUES (?, 'stations', ?, 'cop-osijek', '', 0, '{}', ?, 1, '')`, id, "z"+string(rune('a'+i)), sada.UTC()); err != nil {
			t.Fatal(err)
		}
	}
	poruka := neispravneUKnjizi(ctx, rec, sada)
	if !strings.HasPrefix(poruka, "Knjiga verzija: 7 verzija ima oznaku koju razmjena ne bi primila") ||
		strings.Count(poruka, "autor") != primjeraNeispravnih || strings.Contains(poruka, "\n") ||
		!strings.Contains(poruka, ledger.ErrOblikOznake.Error()) {
		t.Errorf("poruka: %q", poruka)
	}
	javiNeispravneUKnjizi(ctx, rec, sada)
	baza.Close()
	if poruka := neispravneUKnjizi(ctx, rec, sada); !strings.Contains(poruka, "provjera oznaka nije prošla") {
		t.Errorf("zatvorena baza: %q", poruka)
	}
}

// Kad program stane dok provjera još čita knjigu, u dnevnik ne ide ništa,
// pa ni greška zatvorene baze
func TestJaviNeispravneUKnjiziNakonGasenja(t *testing.T) {
	baza, rec := knjigaZaProvjeru(t)
	var dnevnik strings.Builder
	stari := log.Writer()
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(stari) })
	ctx, otkazi := context.WithCancel(context.Background())
	otkazi()
	baza.Close()
	javiNeispravneUKnjizi(ctx, rec, time.Now())
	if dnevnik.Len() != 0 {
		t.Errorf("dnevnik nakon gašenja: %q", dnevnik.String())
	}
}
