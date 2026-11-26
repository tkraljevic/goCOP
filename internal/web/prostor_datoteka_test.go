package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Što zauzima prostor: datoteke koje postoje, s WAL-om, od najveće, traka
// prema najvećoj; arhiva bez postavke stoji uz bazu (Odluke (18. 11. 2026.),
// Administracija, novo ponašanje 9)
func TestProstorDatoteka(t *testing.T) {
	dir := t.TempDir()
	pisi := func(ime string, n int) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, ime), []byte(strings.Repeat("x", n)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pisi("gocop.db", 300)
	pisi("gocop.db-wal", 100)
	pisi("vodostaji.db", 1000)
	pisi("sadrzaj.db", 5)
	db := filepath.Join(dir, "gocop.db")

	got := prostorDatoteka(db, "")
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	if got[0].Naziv != "Arhiva vodostaja" || got[0].Postotak != 100 || got[1].Naziv != "Baza" || got[1].Bajtova != 400 || got[1].Postotak != 40 {
		t.Errorf("redoslijed i veličine: %+v", got)
	}
	if got[2].Naziv != "Sadržaj, PDF-ovi i slike" || got[2].Postotak != 1 || got[2].Poveznica != "#pohrana" {
		t.Errorf("najmanja traka ne smije nestati: %+v", got[2])
	}

	// arhiva na drugom disku, a uz bazu je nema
	drugi := filepath.Join(t.TempDir(), "arhiva.db")
	if err := os.WriteFile(drugi, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got = prostorDatoteka(db, drugi)
	if got[len(got)-1].Put != drugi || got[len(got)-1].Bajtova != 0 || got[len(got)-1].Postotak != 1 {
		t.Errorf("prazna arhiva na drugom disku: %+v", got)
	}
	if prostorDatoteka(filepath.Join(t.TempDir(), "gocop.db"), "") != nil {
		t.Error("bez ijedne datoteke kartice nema")
	}
}

func TestArhivaRukovateljaBaze(t *testing.T) {
	h := &DBMaintHandler{}
	if h.arhiva() != "" {
		t.Error("bez arhive mora biti prazno")
	}
	h.SetArhivaPut(func() string { return "/arhiva/vodostaji.db" })
	if h.arhiva() != "/arhiva/vodostaji.db" {
		t.Error("putanja arhive")
	}
}

// Sadržaj je popis u sadrzaj.db i datoteke u mapi uz nju: kartica broji oboje
func TestProstorSadrzajaSMapom(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sadrzaj.db"), []byte("popis"), 0o600); err != nil {
		t.Fatal(err)
	}
	podmapa := filepath.Join(dir, "sadrzaj", "ab")
	if err := os.MkdirAll(podmapa, 0o755); err != nil {
		t.Fatal(err)
	}
	for ime, n := range map[string]int{"ab01": 1000, "ab02": 2000} {
		if err := os.WriteFile(filepath.Join(podmapa, ime), make([]byte, n), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := prostorDatoteka(filepath.Join(dir, "gocop.db"), "")
	if len(got) != 1 || got[0].Bajtova != 3005 {
		t.Errorf("sadržaj s mapom: %+v", got)
	}
	if n := velicinaMape(filepath.Join(dir, "nema")); n != 0 {
		t.Errorf("mapa koje nema: %d", n)
	}
}
