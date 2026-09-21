package javnivodostaji

import (
	"testing"
	"time"
)

const probniARSO = `<?xml version="1.0" encoding="UTF-8"?>
<arsopodatki>
  <postaja sifra="2110"><datum>21.09.2026 12:00</datum><vodostaj></vodostaj><pretok></pretok><temp_vode></temp_vode></postaja>
  <postaja sifra="2150"><datum>21.09.2026 12:00</datum><vodostaj>58</vodostaj><pretok>123.4</pretok><temp_vode>18,7</temp_vode></postaja>
</arsopodatki>`

func TestCitajARSO(t *testing.T) {
	redci, err := CitajARSO([]byte(probniARSO), "2150")
	if err != nil {
		t.Fatal(err)
	}
	if len(redci) != 1 || redci[0].LevelCm == nil || *redci[0].LevelCm != 58 ||
		redci[0].TempC == nil || *redci[0].TempC != 18.7 ||
		redci[0].FlowM3s == nil || *redci[0].FlowM3s != 123.4 {
		t.Fatalf("ARSO redak: %+v", redci)
	}
	zeljeno := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	if !redci[0].Kad.Equal(zeljeno) {
		t.Errorf("vrijeme %v, očekivano %v", redci[0].Kad, zeljeno)
	}
	prazno, err := CitajARSO([]byte(probniARSO), "2110")
	if err != nil || len(prazno) != 0 {
		t.Fatalf("prazna postaja: redci=%+v err=%v", prazno, err)
	}
}

func TestCitajARSOPrihvacaISOdatum(t *testing.T) {
	b := []byte(`<arsopodatki><postaja sifra="2150"><datum>2026-09-21 12:00</datum><vodostaj>58</vodostaj></postaja></arsopodatki>`)
	r, err := CitajARSO(b, "2150")
	if err != nil || len(r) != 1 || r[0].LevelCm == nil || *r[0].LevelCm != 58 {
		t.Fatalf("redci=%+v err=%v", r, err)
	}
}

func TestAdresaARSO(t *testing.T) {
	a := AdresaARSO("2150")
	if got := PostajaARSOIzAdrese(a); got != "2150" {
		t.Fatalf("šifra %q iz %q", got, a)
	}
	if PostajaARSOIzAdrese("https://example.test/hidro_podatki_zadnji.xml#2150") != "" {
		t.Error("tuđa domena ne smije biti ARSO")
	}
}
