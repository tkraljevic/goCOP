package peers

// Prekidač pri padu WEB synca (pad_povezivosti.go): tri pokretanja s dva
// pada isključe WEB sync, uredno gašenje i rad bez pada vrate brojač, a
// pokvarena oznaka vrijedi kao zatečena.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPadPovezivosti(t *testing.T) {
	dir := t.TempDir()
	// prvo pokretanje: oznake nema, upiše se i program padne
	p := ProcitajPadPovezivosti(dir)
	if p.Zateceno != 0 || p.Iskljucen() {
		t.Fatalf("svježe: %+v", p)
	}
	if err := p.Upisi(); err != nil {
		t.Fatal(err)
	}
	// drugo: zatečena jednom, WEB sync još radi, i opet pad
	p = ProcitajPadPovezivosti(dir)
	if p.Zateceno != 1 || p.Iskljucen() {
		t.Fatalf("nakon jednog pada: %+v", p)
	}
	if err := p.Upisi(); err != nil {
		t.Fatal(err)
	}
	// treće: zatečena dvaput, kreće bez WEB synca
	if p = ProcitajPadPovezivosti(dir); p.Zateceno != 2 || !p.Iskljucen() {
		t.Fatalf("nakon dva pada: %+v", p)
	}
	// 10 minuta rada bez pada vrati brojač, a oznaka ostaje
	if err := p.Stabilno(); err != nil || p.Zateceno != 0 {
		t.Fatalf("stabilno: %+v %v", p, err)
	}
	if p = ProcitajPadPovezivosti(dir); p.Zateceno != 1 || p.Iskljucen() {
		t.Fatalf("pad nakon stabilnog rada: %+v", p)
	}
	// uredno gašenje obriše oznaku, i drugi put bez greške
	if err := p.Obrisi(); err != nil {
		t.Fatal(err)
	}
	if err := p.Obrisi(); err != nil {
		t.Fatalf("drugo brisanje: %v", err)
	}
	if p = ProcitajPadPovezivosti(dir); p.Zateceno != 0 {
		t.Fatalf("nakon brisanja: %+v", p)
	}
}

// Oznaka bez broja, s negativnim brojem ili koja se ne da pročitati vrijedi
// kao zatečena jednom; oznaka koja se ne da upisati ni obrisati je greška.
func TestPadPovezivostiPokvarenaOznaka(t *testing.T) {
	for ime, sadrzaj := range map[string]string{"prazna": "", "bez broja": "nešto", "negativna": "-3"} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, DatotekaPadaPovezivosti), []byte(sadrzaj), 0o600); err != nil {
			t.Fatal(err)
		}
		if p := ProcitajPadPovezivosti(dir); p.Zateceno != 1 {
			t.Errorf("%s: %+v", ime, p)
		}
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, DatotekaPadaPovezivosti), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, DatotekaPadaPovezivosti, "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	p := ProcitajPadPovezivosti(dir)
	if p.Zateceno != 1 {
		t.Errorf("mapa umjesto oznake: %+v", p)
	}
	if err := p.Upisi(); err == nil {
		t.Error("upis u mapu bez greške")
	}
	if err := p.Obrisi(); err == nil {
		t.Error("brisanje pune mape bez greške")
	}
}
