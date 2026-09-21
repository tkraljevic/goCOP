package ehyd

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

func TestCitajDnevniNizIPrazninu(t *testing.T) {
	put := filepath.Join(t.TempDir(), "ehyd.csv")
	sadrzaj := "Messstelle:                ;Lavamünd Ort\nHZB-Nummer:                ;213173\n" +
		"Exportzeitreihe:           ;(2001073,Abfluss,I,Mit,T,1,F,Z,50,,,)\n" +
		"Einheit:                   ;[m³/s]\nWerte:\n" +
		"01.01.2024 00:00:00; 123,4\n02.01.2024 00:00:00; Lücke\n03.01.2024 00:00:00; 124\n"
	kodirano, err := charmap.ISO8859_1.NewEncoder().Bytes([]byte(sadrzaj))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(put, kodirano, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := Citaj(put, "protok")
	if err != nil {
		t.Fatal(err)
	}
	if r.Izvjestaj.Postaja != "Lavamünd Ort" || r.Izvjestaj.HZB != "213173" || r.Izvjestaj.Interval != "T" {
		t.Fatalf("metapodaci: %+v", r.Izvjestaj)
	}
	if len(r.Redci) != 2 || r.Redci[0].Vrijednost != 123.4 || !r.Redci[0].PoDanu {
		t.Fatalf("vrijednosti: %+v", r.Redci)
	}
	if r.Redci[0].Vrijeme.Format("2006-01-02") != "2024-01-01" {
		t.Fatalf("dnevni datum se pomaknuo u vremenskoj zoni: %s", r.Redci[0].Vrijeme)
	}
	if r.Izvjestaj.Praznina != 1 || r.Izvjestaj.Neispravnih != 0 {
		t.Fatalf("izvještaj: %+v", r.Izvjestaj)
	}
}

func TestCitajPrepoznajeMjesecniNiz(t *testing.T) {
	put := filepath.Join(t.TempDir(), "mjesecni.csv")
	sadrzaj := "Exportzeitreihe:;(1,WTemperatur,I,Mit,M,1,F,Z,0,,,)\nWerte:\n01.01.2024 00:00:00; 1,9\n"
	if err := os.WriteFile(put, []byte(sadrzaj), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Citaj(put, "temperatura")
	if err != nil {
		t.Fatal(err)
	}
	if r.Izvjestaj.Interval != "M" || r.Redci[0].PoDanu {
		t.Fatalf("mjesečni niz nije prepoznat: %+v", r)
	}
}
