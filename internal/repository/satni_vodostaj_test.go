package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	gocopdb "gocop/internal/db"
)

// Satni vodostaj broji se drukčije nego COUNT(DISTINCT vrijeme / 3600), a
// ishod je isti: sat s više zapisa, sat samo s zapisima izvan punog sata,
// vremena prije 1970. (dijeljenje odsijeca prema nuli, pa je sat nula širok
// dva sata) i zapisi druge letve, veličine i koraka koji se ne broje. Kad se
// arhiva promijeni, ne vraća se zapamćeni broj.
func TestSatniVodostajIstiKaoDistinctIPratiArhivu(t *testing.T) {
	ctx := context.Background()
	put := filepath.Join(t.TempDir(), "vodostaji.db")
	pis, err := sql.Open("sqlite", gocopdb.ZaPisanje(put, "_pragma=journal_mode(WAL)"))
	if err != nil {
		t.Fatal(err)
	}
	defer pis.Close()
	for _, q := range []string{
		`CREATE TABLE nizovi (id INTEGER PRIMARY KEY, letva TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE spoj (letva TEXT NOT NULL, velicina TEXT NOT NULL, korak TEXT NOT NULL,
			vrijeme INTEGER NOT NULL, vrijednost REAL NOT NULL, izvor TEXT NOT NULL DEFAULT 'proba',
			vrsta TEXT NOT NULL DEFAULT 'trenutna', tocnost REAL NOT NULL DEFAULT 0,
			PRIMARY KEY (letva, velicina, korak, vrijeme)) WITHOUT ROWID`,
	} {
		if _, err := pis.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	upisi := func(letva, velicina, korak string, vremena ...int64) {
		t.Helper()
		for _, v := range vremena {
			if _, err := pis.Exec(`INSERT INTO spoj (letva, velicina, korak, vrijeme, vrijednost) VALUES (?, ?, ?, ?, 1)`,
				letva, velicina, korak, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	h := int64(1_000_000 * 3600)
	upisi("a-letva", "vodostaj", "satni",
		h, h+600, h+1200, // puni sat i dva zapisa u istom satu
		h+3600+600, h+3600+1200, // sat bez zapisa na puni sat
		h+7200,                       // sam puni sat
		-5400, -3600, -1800, 0, 1800, // prije 1970. i oko nule
		-7200-600, // sat prije 1970. bez punog sata
	)
	upisi("a-letva", "protok", "satni", h+20*3600)
	upisi("a-letva", "vodostaj", "dnevni", h+30*3600)
	upisi("b-letva", "vodostaj", "satni", h+40*3600)

	r, err := OpenArhiva(put)
	if err != nil || r == nil {
		t.Fatalf("arhiva: %v", err)
	}
	defer r.Close()
	provjeri := func(letva string) {
		t.Helper()
		var n int
		var zadnje sql.NullInt64
		if err := pis.QueryRow(`SELECT COUNT(DISTINCT vrijeme / 3600), MAX(vrijeme) FROM spoj
			WHERE letva = ? AND velicina = 'vodostaj' AND korak = 'satni'`, letva).Scan(&n, &zadnje); err != nil {
			t.Fatal(err)
		}
		var kraj time.Time
		if zadnje.Valid {
			kraj = time.Unix(zadnje.Int64, 0).UTC()
		}
		for range 2 { // drugi put iz zapamćenog
			sati, k := r.SatniVodostaj(ctx, letva)
			if sati != n || !k.Equal(kraj) {
				t.Errorf("%s: %d sati do %v, COUNT(DISTINCT) %d do %v", letva, sati, k, n, kraj)
			}
		}
	}
	// prekinut zahtjev ne vraća i ne pamti krivi broj
	prekinut, otkazi := context.WithCancel(ctx)
	otkazi()
	if sati, k := r.SatniVodostaj(prekinut, "a-letva"); sati != 0 || !k.IsZero() {
		t.Errorf("prekinut zahtjev: %d sati do %v", sati, k)
	}
	bezPuta := &ArhivaRepository{db: r.db}
	if sati, _ := bezPuta.SatniVodostaj(prekinut, "a-letva"); sati != 0 {
		t.Errorf("prekinut zahtjev bez datoteke: %d sati", sati)
	}
	provjeri("a-letva")
	provjeri("b-letva")
	provjeri("nema-letve")

	// arhiva se promijenila: novi sat i nova letva se vide
	upisi("a-letva", "vodostaj", "satni", h+50*3600, h+50*3600+900)
	upisi("nema-letve", "vodostaj", "satni", h)
	provjeri("a-letva")
	provjeri("nema-letve")
}
