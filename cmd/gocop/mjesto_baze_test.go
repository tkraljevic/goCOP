package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// instalacijaPostave napravi mape instalacije s bazom čvora i vrati putanju
// programa i baze
func instalacijaPostave(t *testing.T) (exe, baza string) {
	t.Helper()
	korijen := t.TempDir()
	for _, d := range []string{"postava", "program", "data"} {
		if err := os.MkdirAll(filepath.Join(korijen, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	baza = filepath.Join(korijen, "data", "gocop.db")
	if err := os.WriteFile(baza, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(korijen, "program", "gocop.exe"), baza
}

// Program iz instalacije Postave pokrenut bez -db iz svoje mape (kućni
// Windows, 26. 10. 2026.) ne kreće, a poruka kaže gdje su podaci čvora.
func TestBezDbIzInstalacijePostave(t *testing.T) {
	exe, baza := instalacijaPostave(t)
	t.Chdir(filepath.Dir(exe))
	err := provjeriMjestoBaze(map[string]bool{}, filepath.Join("data", "gocop.db"), exe)
	if err == nil || !strings.Contains(err.Error(), filepath.Dir(baza)) || !strings.Contains(err.Error(), "-db") {
		t.Fatalf("pokretanje bez -db: %v", err)
	}
}

// Kreće kad je baza ona iz instalacije, kad je -db ili -config upisan i kad
// program nije iz instalacije Postave.
func TestMjestoBazeDopusteno(t *testing.T) {
	exe, baza := instalacijaPostave(t)
	t.Chdir(filepath.Dir(filepath.Dir(exe)))
	slucajevi := []struct {
		ime    string
		zadane map[string]bool
		db     string
		exe    string
	}{
		{"baza instalacije, relativno", nil, filepath.Join("data", "gocop.db"), exe},
		{"baza instalacije, puna putanja", nil, baza, exe},
		{"upisan -db", map[string]bool{"db": true}, filepath.Join(t.TempDir(), "gocop.db"), exe},
		{"upisan -config", map[string]bool{"config": true}, filepath.Join(t.TempDir(), "gocop.db"), exe},
		{"program izvan instalacije", nil, filepath.Join(t.TempDir(), "gocop.db"), filepath.Join(t.TempDir(), "bin", "gocop")},
		{"putanja programa nepoznata", nil, filepath.Join(t.TempDir(), "gocop.db"), ""},
	}
	for _, s := range slucajevi {
		if err := provjeriMjestoBaze(s.zadane, s.db, s.exe); err != nil {
			t.Errorf("%s: %v", s.ime, err)
		}
	}
	// Testni program nije iz instalacije Postave.
	if err := mjestoBazePrograma(nil, filepath.Join(t.TempDir(), "gocop.db")); err != nil {
		t.Errorf("testni program: %v", err)
	}
}

// Mapa program bez mape postava ili bez baze u data nije instalacija Postave.
func TestBazaInstalacije(t *testing.T) {
	exe, baza := instalacijaPostave(t)
	if b, ok := bazaInstalacije(exe); !ok || b != baza {
		t.Fatalf("instalacija: %q %v", b, ok)
	}
	if err := os.Remove(baza); err != nil {
		t.Fatal(err)
	}
	if _, ok := bazaInstalacije(exe); ok {
		t.Error("instalacija bez baze čvora")
	}
	korijen := t.TempDir()
	if err := os.MkdirAll(filepath.Join(korijen, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(korijen, "data", "gocop.db"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := bazaInstalacije(filepath.Join(korijen, "program", "gocop")); ok {
		t.Error("bez mape postava nije instalacija Postave")
	}
}
