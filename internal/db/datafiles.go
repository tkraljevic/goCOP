package db

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Registri nisu ugrađeni u program ni u repozitorij: dionice, vodomjeri s
// pragovima, vode, teritorijalne jedinice i objekti stoje kao JSON uz bazu
// (mapa data/) i čitaju se samo pri prvom punjenju prvog čvora. Svaki
// sljedeći čvor registre dobiva sinkronizacijom, pa mu datoteke ne trebaju.
// Repozitorij nosi program i shemu, ništa drugo.

// DataDir je mapa uz bazu iz koje se čitaju registri i imenik
var DataDir = "data"

// ErrNoDataFile javlja da datoteka registra uz bazu ne postoji
var ErrNoDataFile = errors.New("datoteka registra uz bazu ne postoji")

// DataFile vraća putanju datoteke u mapi uz bazu
func DataFile(name string) string {
	return filepath.Join(DataDir, name)
}

// readDataFile čita datoteku registra; nepostojeću javlja kao ErrNoDataFile
func readDataFile(name string) ([]byte, error) {
	raw, err := os.ReadFile(DataFile(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", DataFile(name), ErrNoDataFile)
	}
	if err != nil {
		return nil, fmt.Errorf("greška pri čitanju %s: %w", DataFile(name), err)
	}
	return raw, nil
}

// UseRepoData traži mapu data/ repozitorija s registrima (za testove koji
// rade na stvarnim podacima) i javlja je li nađena
func UseRepoData() bool {
	for _, up := range []string{"", "..", "../..", "../../.."} {
		candidate := filepath.Join(up, "data")
		if _, err := os.Stat(filepath.Join(candidate, "sections.json")); err == nil {
			DataDir = candidate
			ImenikPath = filepath.Join(candidate, "imenik.json")
			return true
		}
	}
	return false
}
