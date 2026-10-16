package ledger

import (
	"context"
	"encoding/json"
	"maps"
	"testing"
)

// samoPoznati zada popis poznatih entiteta i sheme za jedan test, a na kraju
// testa ih vrati: popis i sheme su zajednički cijelom procesu
func samoPoznati(t *testing.T, zadane map[string]int, entiteti ...string) {
	t.Helper()
	PoznatiEntiteti(entiteti...)
	for e, n := range zadane {
		PostaviShemu(e, n)
	}
	t.Cleanup(func() {
		shemeMu.Lock()
		defer shemeMu.Unlock()
		poznatiEnt = map[string]bool{}
		for e := range zadane {
			delete(sheme, e)
		}
	})
}

// Katalog je za svaki poznati entitet shema koju program zna; entitet sa
// shemom, a izvan popisa, program ne poznaje. Bajtovi su uvijek isti, jer
// encoding/json ključeve mape slaže abecedno.
func TestKatalog(t *testing.T) {
	if k := Katalog(); k != nil {
		t.Fatalf("bez popisa poznatih su poznati svi, a ne mogu se nabrojati: %v", k)
	}
	samoPoznati(t, map[string]int{"kat_b": 3, "kat_izvan": 2}, "kat_b", "kat_a", "kat_b")
	if k := Katalog(); !maps.Equal(k, map[string]int{"kat_a": 1, "kat_b": 3}) {
		t.Errorf("katalog: %v", k)
	}
	prvi, err := json.Marshal(Katalog())
	if err != nil {
		t.Fatal(err)
	}
	drugi, _ := json.Marshal(Katalog())
	if string(prvi) != `{"kat_a":1,"kat_b":3}` || string(drugi) != string(prvi) {
		t.Errorf("bajtovi kataloga: %s, pa %s", prvi, drugi)
	}
}

// Na čekanju su zapisi, ne verzije: zapisi entiteta koje program ne
// poznaje i zapisi s verzijom sheme novije od one koju program zna.
func TestCekaNadogradnju(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	rec := New(db, "stari")
	if n, err := rec.CekaNadogradnju(ctx); err != nil || n != 0 {
		t.Fatalf("prazna knjiga: %d %v", n, err)
	}
	samoPoznati(t, map[string]int{"kat_shema": 2}, "kat_poznat", "kat_shema")
	// nepoznat entitet: dva zapisa, jedan s dvije verzije
	novijaVerzija(t, rec, "kat_buduci", "b-1", `{}`, 1)
	novijaVerzija(t, rec, "kat_buduci", "b-1", `{}`, 2)
	novijaVerzija(t, rec, "kat_buduci", "b-2", `{}`, 1)
	// poznat entitet: čeka samo zapis s verzijom novije sheme
	novijaVerzija(t, rec, "kat_poznat", "p-1", `{}`, 1)
	novijaVerzija(t, rec, "kat_poznat", "p-1", `{}`, 3)
	novijaVerzija(t, rec, "kat_poznat", "p-1", `{}`, 3)
	novijaVerzija(t, rec, "kat_poznat", "p-2", `{}`, 1)
	// shemu 2 program zna, shemu 3 ne
	novijaVerzija(t, rec, "kat_shema", "s-1", `{}`, 2)
	novijaVerzija(t, rec, "kat_shema", "s-2", `{}`, 3)
	if n, err := rec.CekaNadogradnju(ctx); err != nil || n != 4 {
		t.Errorf("na čekanju %d %v, očekivano b-1, b-2, p-1 i s-2", n, err)
	}

	if _, err := db.Exec(`DROP TABLE record_versions`); err != nil {
		t.Fatal(err)
	}
	if n, err := rec.CekaNadogradnju(ctx); err == nil || n != 0 {
		t.Errorf("bez knjige: %d %v", n, err)
	}
}
