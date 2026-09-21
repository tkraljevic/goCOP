package arso

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCitajRazdvajaDnevneNizove(t *testing.T) {
	put := filepath.Join(t.TempDir(), "arso.csv")
	sadrzaj := "Datum;vodostaj (cm);pretok (m3/s);temp. vode (°C);transport suspendiranega materiala (kg/s)\n" +
		"01.01.2024;;254;;1,2\n" +
		"02.01.2024;123,5;250;8,2;1,3\n" +
		"03.01.2024;124;251;-99;1,4\n" +
		"nešto;125;252;8,4;1,5\n"
	if err := os.WriteFile(put, []byte(sadrzaj), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := Citaj([]string{put})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Vodostaji) != 2 || r.Vodostaji[0].Vrijednost != 123.5 {
		t.Fatalf("vodostaji: %+v", r.Vodostaji)
	}
	if len(r.Protoci) != 3 || r.Protoci[0].Vrijednost != 254 {
		t.Fatalf("protoci: %+v", r.Protoci)
	}
	if len(r.Temperature) != 1 || r.Temperature[0].Vrijednost != 8.2 {
		t.Fatalf("temperature: %+v", r.Temperature)
	}
	if !r.Protoci[0].PoDanu || r.Protoci[0].Vrijeme.Format("2006-01-02") != "2024-01-01" {
		t.Errorf("dnevni zapis: %+v", r.Protoci[0])
	}
	if r.Izvjestaj.NeispravnihDatuma != 1 || r.Izvjestaj.NeispravnihTemperatura != 1 {
		t.Fatalf("izvještaj: %+v", r.Izvjestaj)
	}
}

func TestCitajPreklapanjeZadnjaVrijednostPobjedjuje(t *testing.T) {
	d := t.TempDir()
	prvi := filepath.Join(d, "prvi.csv")
	drugi := filepath.Join(d, "drugi.csv")
	glava := "Datum;vodostaj (cm);pretok (m3/s);temp. vode (°C)\n"
	if err := os.WriteFile(prvi, []byte(glava+"01.01.2024;100;200;8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(drugi, []byte(glava+"01.01.2024;101;;8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Citaj([]string{prvi, drugi})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Vodostaji) != 1 || r.Vodostaji[0].Vrijednost != 101 || len(r.Protoci) != 1 {
		t.Fatalf("rezultat: %+v", r)
	}
	if r.Izvjestaj.Duplikata != 1 || r.Izvjestaj.Sukoba != 1 {
		t.Fatalf("izvještaj: %+v", r.Izvjestaj)
	}
}
