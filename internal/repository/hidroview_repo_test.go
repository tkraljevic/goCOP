package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"gocop/internal/db"

	_ "modernc.org/sqlite"
)

func bazaRacuna(t *testing.T) *sql.DB {
	t.Helper()
	baza, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	return baza
}

// Letva bez vlastitog računa čita se računom čvora; kad dobije svoj, vrijedi
// njezin. Tako sektor može dodati račun za svoje postaje, a ostale i dalje
// rade po računu čvora.
func TestRacunHidroViewPadaNaCvor(t *testing.T) {
	r := NewHidroViewRepository(bazaRacuna(t))
	ctx := context.Background()
	if err := r.Spremi(ctx, &RacunHidroView{Korisnik: "cvor.racun", Lozinka: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Racun(ctx, "tikves")
	if err != nil || got == nil {
		t.Fatalf("račun čvora se ne vidi: %v", err)
	}
	if got.Korisnik != "cvor.racun" || got.Letva != "" {
		t.Errorf("dobiven %+v, očekivan račun čvora", got)
	}

	if err := r.Spremi(ctx, &RacunHidroView{Letva: "tikves", Korisnik: "sektor.b", Lozinka: []byte("y")}); err != nil {
		t.Fatal(err)
	}
	got, _ = r.Racun(ctx, "tikves")
	if got == nil || got.Korisnik != "sektor.b" {
		t.Errorf("dobiven %+v, očekivan račun letve", got)
	}
	// druga letva i dalje ide na račun čvora
	got, _ = r.Racun(ctx, "batina")
	if got == nil || got.Korisnik != "cvor.racun" {
		t.Errorf("dobiven %+v, očekivan račun čvora za drugu letvu", got)
	}

	// brisanje računa letve vraća je na račun čvora
	if err := r.Obrisi(ctx, "tikves"); err != nil {
		t.Fatal(err)
	}
	got, _ = r.Racun(ctx, "tikves")
	if got == nil || got.Korisnik != "cvor.racun" {
		t.Errorf("dobiven %+v — nakon brisanja mora vrijediti račun čvora", got)
	}
}

// Bez ijednog upisanog računa čitanje ne smije biti greška: letva jednostavno
// nema odakle čitati telemetriju.
func TestRacunHidroViewBezIjednog(t *testing.T) {
	r := NewHidroViewRepository(bazaRacuna(t))
	got, err := r.Racun(context.Background(), "tikves")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("dobiven %+v, očekivano ništa", got)
	}
}
