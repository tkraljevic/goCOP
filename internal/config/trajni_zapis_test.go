package config

import (
	"os"
	"path/filepath"
	"testing"

	"gocop/internal/trajno"
)

// Zaključava da upis imena čvora i prvi zapis postavki idu kroz privremenu
// datoteku spuštenu na disk, a mapa nakon preimenovanja. Uz izravan upis
// nestanak struje mogao je ostaviti praznu ili napola zapisanu gocop.toml.
func TestPostavkeSeZapisujuTrajno(t *testing.T) {
	dir := t.TempDir()
	put := filepath.Join(dir, FileName)

	popis, prestani := trajno.Prati()
	defer prestani()
	if _, err := WriteExample(put, Default()); err != nil {
		t.Fatal(err)
	}
	if !popis.ImaUMapi(dir) || !popis.Ima(dir) {
		t.Error("zapis postavki nije spušten na disk")
	}

	if err := os.Chmod(put, 0o640); err != nil {
		t.Fatal(err)
	}
	popis, prestani2 := trajno.Prati()
	defer prestani2()
	if err := UpisiIme(put, "pperic-thinkpad"); err != nil {
		t.Fatal(err)
	}
	if !popis.ImaUMapi(dir) || !popis.Ima(dir) {
		t.Error("upis imena nije spušten na disk")
	}
	if st, err := os.Stat(put); err != nil || st.Mode().Perm() != 0o640 {
		t.Errorf("upis imena promijenio je prava datoteke: %v, %v", st.Mode().Perm(), err)
	}
}
