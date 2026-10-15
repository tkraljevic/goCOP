package ledger

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// msVerzije je vrijeme nastanka verzije u milisekundama, iz UUIDv7
func msVerzije(t *testing.T, id string) int64 {
	t.Helper()
	u, err := uuid.Parse(id)
	if err != nil || u.Version() != 7 || u.Variant() != uuid.RFC4122 {
		t.Fatalf("oznaka %s nije UUIDv7: %v", id, err)
	}
	var ms int64
	for _, b := range u[:6] {
		ms = ms<<8 | int64(b)
	}
	return ms
}

// Brisanje koje ponavlja tuđe brisanje stoji u poretku milisekundu iza
// njega, a ne s današnjim vremenom: starije je od svega što je drugi čvor
// napisao poslije, pa razmjenom ne pregazi to. Dva takva brisanja dobiju
// različite oznake. Bez zadane verzije ili s verzijom bez vremena nastanka
// ne upisuje se ništa.
func TestBrisanjeIzaZadaneVerzije(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	ured := New(db, "ured")
	laptop := New(db, "laptop")
	kanal := "vodocuvarski/16/2026"
	uredovo, err := ured.ArchiveIn(ctx, db, kanal, "vodocuvarski_listovi", "l-ivo", probni{"Ivin list", 1})
	if err != nil {
		t.Fatal(err)
	}

	vlastito, err := laptop.ArchiveNakon(ctx, db, kanal, "vodocuvarski_listovi", "l-ivo", probni{"Ivin list", 1}, uredovo)
	if err != nil {
		t.Fatal(err)
	}
	if vlastito <= uredovo || msVerzije(t, vlastito)-msVerzije(t, uredovo) != 1 {
		t.Errorf("vlastito brisanje %s nije milisekundu iza uredova %s", vlastito, uredovo)
	}
	if v, err := laptop.Latest(ctx, "vodocuvarski_listovi", "l-ivo"); err != nil || v.Supersedes != uredovo || v.NodeID != "laptop" || !v.Archived {
		t.Errorf("vlastito brisanje: %+v %v", v, err)
	}
	drugo, err := laptop.ArchiveNakon(ctx, db, kanal, "vodocuvarski_listovi", "l-ivo", probni{"Ivin list", 1}, uredovo)
	if err != nil || drugo == vlastito || msVerzije(t, drugo) != msVerzije(t, vlastito) {
		t.Errorf("drugo brisanje iza iste verzije: %s (prvo %s), %v", drugo, vlastito, err)
	}

	for _, nakon := range []string{"", "nije-oznaka", uuid.NewString()} {
		if id, err := laptop.ArchiveNakon(ctx, db, kanal, "vodocuvarski_listovi", "l-pero", probni{"Perin list", 1}, nakon); err == nil {
			t.Errorf("brisanje iza %q upisano kao %s", nakon, id)
		}
	}
	if n, err := laptop.History(ctx, "vodocuvarski_listovi", "l-pero"); err != nil || len(n) != 0 {
		t.Errorf("neuspjela brisanja ostavila su verzije: %v %v", n, err)
	}
}
