package gkd

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestCitajSamoProvjereneSrednjake(t *testing.T) {
	put := filepath.Join(t.TempDir(), "gkd.zip")
	f, err := os.Create(put)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create("fluesse-abfluss/niz.csv")
	if err != nil {
		t.Fatal(err)
	}
	sadrzaj := "\ufeffQuelle:;GKD\nMessstellen-Name:;Hofkirchen\nMessstellen-Nr.:;10088003\n\n" +
		"Datum;Mittelwert;Maximum;Minimum;Prüfstatus\n" +
		"2024-01-01;328;;;Geprueft\n2024-01-02;329,5;;;Vorlaeufig\n2024-01-03;330;;;Geprueft\n"
	if _, err := w.Write([]byte(sadrzaj)); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := Citaj(put)
	if err != nil {
		t.Fatal(err)
	}
	if r.Izvjestaj.Postaja != "Hofkirchen" || r.Izvjestaj.Broj != "10088003" {
		t.Fatalf("metapodaci: %+v", r.Izvjestaj)
	}
	if len(r.Protoci) != 2 || r.Protoci[0].Vrijednost != 328 || !r.Protoci[0].PoDanu {
		t.Fatalf("protoci: %+v", r.Protoci)
	}
	if r.Izvjestaj.Neprovjerenih != 1 || r.Izvjestaj.Neispravnih != 0 {
		t.Fatalf("izvještaj: %+v", r.Izvjestaj)
	}
}
