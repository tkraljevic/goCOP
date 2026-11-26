package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/sadrzaj"
)

func TestPorukaSeljenja(t *testing.T) {
	if p := porukaSeljenja(0, nil); p != "" {
		t.Errorf("ništa preseljeno: %q", p)
	}
	if p := porukaSeljenja(12, nil); !strings.Contains(p, "12") {
		t.Errorf("preseljeno 12: %q", p)
	}
	if p := porukaSeljenja(3, errors.New("disk pun")); !strings.Contains(p, "disk pun") || !strings.Contains(p, "nastavlja se") {
		t.Errorf("greška: %q", p)
	}
	preseliSadrzaj(otvoriSpremiste(t)) // novo spremište nema što seliti
}

func otvoriSpremiste(t *testing.T) *sadrzaj.Spremiste {
	t.Helper()
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sp.Zatvori() }) //nolint:errcheck // proba, zatvaranje nije predmet
	return sp
}

// Pospremanje javlja otpušten sadržaj i maknut višak datoteka, a greške
// obaju koraka idu u dnevnik i ne zaustavljaju drugi korak
func TestPospremiSadrzaj(t *testing.T) {
	ctx := context.Background()
	sp := otvoriSpremiste(t)
	visak := filepath.Join(sadrzaj.MapaUz(sp.Put()), "ab", "ab"+sadrzaj.Otisak([]byte("siroce"))[2:])
	if err := os.MkdirAll(filepath.Dir(visak), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(visak, []byte("siroce"), 0o644); err != nil {
		t.Fatal(err)
	}
	staro := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(visak, staro, staro); err != nil {
		t.Fatal(err)
	}
	otpusti := func(context.Context) (int, int64, error) { return 2, 3_000_000, nil }
	poruke := strings.Join(pospremiSadrzaj(ctx, otpusti, sp), "\n")
	if !strings.Contains(poruke, "otpušteno 2") || !strings.Contains(poruke, "maknuto 1") {
		t.Errorf("poruke: %q", poruke)
	}
	if p := pospremiSadrzaj(ctx, func(context.Context) (int, int64, error) { return 0, 0, nil }, sp); len(p) != 0 {
		t.Errorf("ništa za pospremiti: %q", p)
	}

	if err := os.Chmod(filepath.Dir(visak), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Dir(visak), 0o755) }) //nolint:errcheck // samo da TempDir može obrisati mapu
	pokvaren := func(context.Context) (int, int64, error) { return 0, 0, errors.New("baza zaključana") }
	poruke = strings.Join(pospremiSadrzaj(ctx, pokvaren, sp), "\n")
	if !strings.Contains(poruke, "baza zaključana") || !strings.Contains(poruke, "čišćenje datoteka sadržaja") {
		t.Errorf("poruke grešaka: %q", poruke)
	}

	krugova := 0
	pospremajSadrzaj(func() bool { krugova++; return krugova < 2 }, pokvaren, sp)
	if krugova != 2 {
		t.Errorf("krugova %d, treba 2", krugova)
	}
}
