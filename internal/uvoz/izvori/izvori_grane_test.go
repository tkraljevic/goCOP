package izvori

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// Zaključava greške pri spuštanju datoteka u privremenu mapu (mape nema,
// ime predugo za sustav datoteka) i HIS ZIP čija se datoteka ne da otvoriti
// ili pročitati.

func TestUvozBezPrivremeneMape(t *testing.T) {
	koren := t.TempDir()
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "nema-privremene"))
	f, _ := Nadji("arso")
	_, err := f.Uvezi([]Datoteka{{Ime: "ptuj.csv", Sadrzaj: []byte(arsoGlava + "01.01.2024;123;;\n")}},
		Zadatak{Koren: koren, Sliv: "drava", Letva: "ptuj"}, nil)
	if err == nil || !strings.Contains(err.Error(), "nema-privremene") {
		t.Errorf("bez privremene mape: %v", err)
	}
	if d := datoteke(t, koren); len(d) != 0 {
		t.Errorf("zapisano %v", d)
	}
}

func TestUvozPredugoImeDatoteke(t *testing.T) {
	koren := t.TempDir()
	f, _ := Nadji("arso")
	_, err := f.Uvezi([]Datoteka{{Ime: strings.Repeat("ptuj", 70) + ".csv", Sadrzaj: []byte(arsoGlava + "01.01.2024;123;;\n")}},
		Zadatak{Koren: koren, Sliv: "drava", Letva: "ptuj"}, nil)
	if err == nil {
		t.Error("predugo ime je prošlo")
	}
	if d := datoteke(t, koren); len(d) != 0 {
		t.Errorf("zapisano %v", d)
	}
}

func TestUvozHISNecitljivZIP(t *testing.T) {
	sadrzaj := []byte("Dnevni podaci postaje DALJ - DUNAV,  VODOSTAJ  (cm)\r\n01.01.2001;176;\r\n")
	for greska, zaglavlje := range map[string]zip.FileHeader{
		"unsupported compression algorithm": {Name: "izvoz/dnevni.csv", Method: 99},
		"checksum error":                    {Name: "izvoz/dnevni.csv", Method: zip.Store, CRC32: 1},
	} {
		zaglavlje.CompressedSize64 = uint64(len(sadrzaj))
		zaglavlje.UncompressedSize64 = uint64(len(sadrzaj))
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, err := zw.CreateRaw(&zaglavlje)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(sadrzaj); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		koren := t.TempDir()
		_, _, err = uvezi(t, "his2000", []Datoteka{{Ime: "dalj.zip", Sadrzaj: buf.Bytes()}}, Zadatak{Koren: koren, Sliv: "dunav", Letva: "dalj"})
		if err == nil || !strings.HasPrefix(err.Error(), "dalj.zip: ") || !strings.Contains(err.Error(), greska) {
			t.Errorf("%s: %v", greska, err)
		}
		if d := datoteke(t, koren); len(d) != 0 {
			t.Errorf("%s: zapisano %v", greska, d)
		}
	}
}
