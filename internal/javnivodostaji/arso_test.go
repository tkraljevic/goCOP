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

const probnaARSOTablica = `<h1>Postaja Gornja Radgona I - Mura</h1><table class="podatki">
<thead><tr>
<th>Datum</th>
<th>Vodostaj [cm]</th>
<th>Pretok [m&sup3;/s]</th>
<th>Temperatura vode [&deg;C]</th>
</tr></thead>
<tbody>
<tr>
<td>27.09.2026 21:20</td>
<td>-</td>
<td>-</td>
<td>-</td>
</tr>
<tr>
<td>27.09.2026 21:10</td>
<td>54.0</td>
<td>43.4</td>
<td>16.5</td>
</tr>
<tr>
<td>27.09.2026 21:00</td>
<td>54.0</td>
<td>-</td>
<td>16.6</td>
</tr>
</tbody></table>`

// Tablica postaje daje zadnji dan svakih 10 minuta; „-” je rupa, a redak bez
// ijedne vrijednosti ne ulazi.
func TestCitajARSOTablicu(t *testing.T) {
	r, err := CitajARSOTablicu([]byte(probnaARSOTablica))
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 {
		t.Fatalf("redaka %d, očekivano 2: %+v", len(r), r)
	}
	// od najstarijeg prema najnovijem, kako uvoz očekuje
	if !r[1].Kad.Equal(time.Date(2026, 9, 27, 19, 10, 0, 0, time.UTC)) || r[1].LevelCm == nil || *r[1].LevelCm != 54 ||
		r[1].FlowM3s == nil || *r[1].FlowM3s != 43.4 || r[1].TempC == nil || *r[1].TempC != 16.5 {
		t.Errorf("zadnji redak %+v", r[1])
	}
	if r[0].FlowM3s != nil || r[0].LevelCm == nil || !r[0].Kad.Before(r[1].Kad) {
		t.Errorf("rupa u protoku nije rupa ili redoslijed nije od najstarijeg: %+v", r[0])
	}
}
