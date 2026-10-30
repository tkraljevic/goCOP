package trajno

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Zapis stavlja cijeli sadržaj pod pravo ime, sa zadanim pravima, bez
// privremene datoteke uz njega; i datoteka i mapa spuštene su na disk
func TestZapisiSpustaDatotekuIMapu(t *testing.T) {
	mapa := t.TempDir()
	put := filepath.Join(mapa, "gocop.toml")
	if err := os.WriteFile(put, []byte("staro"), 0o644); err != nil {
		t.Fatal(err)
	}
	popis, prestani := Prati()
	defer prestani()
	if err := Zapisi(put, []byte("novo"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(put); err != nil || string(b) != "novo" {
		t.Errorf("sadržaj %q, %v", b, err)
	}
	if st, err := os.Stat(put); err != nil || (runtime.GOOS != "windows" && st.Mode().Perm() != 0o600) {
		t.Errorf("prava %v, %v", st.Mode().Perm(), err)
	}
	if ima, err := os.ReadDir(mapa); err != nil || len(ima) != 1 {
		t.Errorf("u mapi je ostalo još nešto: %v, %v", ima, err)
	}
	if !popis.ImaUMapi(mapa) || !popis.Ima(mapa) {
		t.Error("datoteka ili mapa nije spuštena na disk")
	}
}

// Zapis u mapu koje nema javlja grešku i pod pravim imenom ne nastaje ništa
func TestZapisiUMapuKojeNema(t *testing.T) {
	put := filepath.Join(t.TempDir(), "nema", "gocop.toml")
	if err := Zapisi(put, []byte("x"), 0o644); err == nil {
		t.Error("zapis u nepostojeću mapu javio je uspjeh")
	}
	if _, err := os.Stat(put); err == nil {
		t.Error("pod pravim imenom je nešto nastalo")
	}
}

// Kad praćenje prestane, popis više ne raste
func TestPracenjePrestaje(t *testing.T) {
	popis, prestani := Prati()
	prestani()
	SpustiMapu(t.TempDir())
	if len(popis.putevi) != 0 {
		t.Errorf("popis nakon prestanka: %v", popis.putevi)
	}
}

// Zatvorena datoteka ne da se spustiti na disk: greška se javlja i ništa se
// ne bilježi kao spušteno
func TestSpustanjeZatvoreneDatoteke(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "kopija.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	popis, prestani := Prati()
	defer prestani()
	if err := SpustiDatoteku(f); err == nil {
		t.Error("spuštanje zatvorene datoteke javilo je uspjeh")
	}
	if err := SpustiIZatvori(f); err == nil {
		t.Error("spuštanje i zatvaranje zatvorene datoteke javilo je uspjeh")
	}
	if popis.Ima(f.Name()) {
		t.Error("zatvorena datoteka zabilježena je kao spuštena")
	}
}

// Preimenovanje spušta mapu odredišta; datoteka koje nema javlja grešku
func TestPreimenuj(t *testing.T) {
	mapa := t.TempDir()
	iz, u := filepath.Join(mapa, "gocop.dio"), filepath.Join(mapa, "gocop")
	if err := os.WriteFile(iz, []byte("program"), 0o644); err != nil {
		t.Fatal(err)
	}
	popis, prestani := Prati()
	defer prestani()
	if err := Preimenuj(iz, u); err != nil || !popis.Ima(mapa) {
		t.Errorf("preimenovanje: %v, mapa spuštena %v", err, popis.Ima(mapa))
	}
	if err := Preimenuj(iz, u); err == nil {
		t.Error("preimenovanje datoteke koje nema javilo je uspjeh")
	}
}
