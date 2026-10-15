package gkd

import (
	"archive/zip"
	"bytes"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Zaključava čitanje GKD izvoza s više datoteka: mape i ostale datoteke se
// preskaču, kriv datum i vrijednost broje se kao neispravni, isti dan u dvije
// datoteke kao duplikat ili sukob, a datoteka bez vrijednosti kao prazna.

const zaglavlje = "Messstellen-Name:;Hofkirchen\nDatum;Mittelwert;Maximum;Minimum;Prüfstatus\n"

// zipSDatotekama slaže ZIP s datotekama zadanim po imenu, redom
func zipSDatotekama(t *testing.T, datoteke ...[2]string) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "gkd.zip")
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, d := range datoteke {
		w, err := z.Create(d[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(d[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(put, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return put
}

func TestCitajViseDatotekaIRubneRetke(t *testing.T) {
	put := zipSDatotekama(t,
		[2]string{"fluesse-abfluss/", ""},
		[2]string{"fluesse-abfluss/opis.txt", "nije tablica"},
		[2]string{"fluesse-abfluss/prva.CSV", zaglavlje +
			"2024-01-01;328;;;Geprueft\n" +
			"2024-01-02;329;;;Geprueft\n" +
			"kratak;red\n" +
			"2024-1-3;330;;;Geprueft\n" +
			"2024-13-01;331;;;Geprueft\n" +
			"2024-01-04;nema;;;Geprueft\n" +
			"2024-01-05;-5;;;Geprueft\n"},
		[2]string{"fluesse-abfluss/druga.csv", zaglavlje +
			"2024-01-01;328;;;Geprueft\n" +
			"2024-01-02;400;;;Geprueft\n"},
		[2]string{"fluesse-abfluss/prazna.csv", zaglavlje + "2024-02-01;12;;;Vorlaeufig\n"},
	)
	r, err := Citaj(put)
	if err != nil {
		t.Fatal(err)
	}
	iz := r.Izvjestaj
	// kratak red i datum kraći od deset znakova ne broje se kao redak tablice
	if iz.Datoteka != 3 || iz.Redaka != 8 || iz.Neispravnih != 3 || iz.Duplikata != 2 || iz.Sukoba != 1 || iz.BezPodataka != 1 || iz.Neprovjerenih != 1 {
		t.Fatalf("izvještaj: %+v", iz)
	}
	// kod sukoba vrijedi zadnja pročitana vrijednost
	if len(r.Protoci) != 2 || r.Protoci[0].Vrijednost != 328 || r.Protoci[1].Vrijednost != 400 || r.Protoci[1].Vrijeme.Format("2006-01-02") != "2024-01-02" {
		t.Errorf("protoci: %+v", r.Protoci)
	}
}

func TestCitajGreske(t *testing.T) {
	if _, err := Citaj(filepath.Join(t.TempDir(), "nema.zip")); err == nil {
		t.Error("nepostojeći ZIP je prošao")
	}
	// bez ijednog provjerenog srednjaka nema ni niza
	put := zipSDatotekama(t, [2]string{"niz.csv", zaglavlje + "2024-01-01;328;;;Vorlaeufig\n"})
	if _, err := Citaj(put); err == nil || !strings.HasSuffix(err.Error(), "nema provjerenih dnevnih srednjih protoka") {
		t.Errorf("bez provjerenih: %v", err)
	}

	// datoteka koja se ne da otvoriti (nepoznato sažimanje) ili pročitati
	// (kriv zbroj) ruši čitanje s imenom datoteke
	sadrzaj := []byte(zaglavlje + "2024-01-01;328;;;Geprueft\n")
	for ime, zaglavljeZipa := range map[string]zip.FileHeader{
		"sazimanje": {Name: "niz.csv", Method: 99, CRC32: crc32.ChecksumIEEE(sadrzaj)},
		"zbroj":     {Name: "niz.csv", Method: zip.Store, CRC32: 1},
	} {
		zaglavljeZipa.CompressedSize64 = uint64(len(sadrzaj))
		zaglavljeZipa.UncompressedSize64 = uint64(len(sadrzaj))
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		w, err := z.CreateRaw(&zaglavljeZipa)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(sadrzaj); err != nil {
			t.Fatal(err)
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		put := filepath.Join(t.TempDir(), ime+".zip")
		if err := os.WriteFile(put, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Citaj(put); err == nil || !strings.HasPrefix(err.Error(), "niz.csv: ") {
			t.Errorf("%s: %v", ime, err)
		}
	}
}
