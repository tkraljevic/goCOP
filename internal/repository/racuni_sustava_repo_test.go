package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
)

// Račun koji šalje PIN pamti adresu pošiljatelja i trenutak kad je
// poslužitelj odbio lozinku; nova lozinka (Spremi) briše oznaku. Stara baza
// bez tih stupaca dobije ih pri pokretanju.
func TestRacunSustavaAdresaINeispravan(t *testing.T) {
	baza, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "stara.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if _, err := baza.Exec(`CREATE TABLE racuni_sustava (sustav TEXT PRIMARY KEY, korisnik TEXT NOT NULL,
		lozinka BLOB NOT NULL, updated_at DATETIME NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO racuni_sustava VALUES ('mletva.voda.hr', 'pero', x'00', '2026-01-01 00:00:00')`); err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	r := NewRacuniSustavaRepository(baza)
	ctx := context.Background()
	if got, err := r.Racun(ctx, "mletva.voda.hr"); err != nil || got == nil || got.Adresa != "" || got.NeispravanOd != nil {
		t.Fatalf("stari račun: %+v (%v)", got, err)
	}
	if err := r.Spremi(ctx, &RacunSustava{Sustav: "posta-pin", Korisnik: "VODA\\pin", Lozinka: []byte("x"), Adresa: "pin@voda.hr"}); err != nil {
		t.Fatal(err)
	}
	kad := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	if err := r.OznaciNeispravan(ctx, "posta-pin", kad); err != nil {
		t.Fatal(err)
	}
	_ = r.OznaciNeispravan(ctx, "posta-pin", kad.Add(time.Hour)) // prvi trenutak ostaje
	got, _ := r.Racun(ctx, "posta-pin")
	if got.Adresa != "pin@voda.hr" || got.NeispravanOd == nil || !got.NeispravanOd.Equal(kad) {
		t.Fatalf("označen: %+v", got)
	}
	if err := r.Spremi(ctx, &RacunSustava{Sustav: "posta-pin", Korisnik: "VODA\\pin", Lozinka: []byte("y"), Adresa: "pin@voda.hr"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Racun(ctx, "posta-pin"); got.NeispravanOd != nil {
		t.Fatalf("nova lozinka nije obrisala oznaku: %+v", got)
	}
}

// Račun sustava vrijedi samo za svoj sustav: nema pada na neki drugi račun,
// jer bi se tuđa lozinka slala na krivo mjesto.
func TestRacunSustavaSamoSvoj(t *testing.T) {
	r := NewRacuniSustavaRepository(bazaRacuna(t))
	ctx := context.Background()
	if err := r.Spremi(ctx, &RacunSustava{Sustav: "mletva.voda.hr", Korisnik: "pero", Lozinka: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Racun(ctx, "hdv.voda.hr"); err != nil || got != nil {
		t.Fatalf("za drugi sustav vraćen račun %+v (%v)", got, err)
	}
	got, err := r.Racun(ctx, "mletva.voda.hr")
	if err != nil || got == nil || got.Korisnik != "pero" {
		t.Fatalf("račun se ne vidi: %+v (%v)", got, err)
	}
	if err := r.Obrisi(ctx, "mletva.voda.hr"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Racun(ctx, "mletva.voda.hr"); got != nil {
		t.Fatalf("račun nije obrisan")
	}
}
