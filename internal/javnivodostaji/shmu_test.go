package javnivodostaji

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Isječak tablice sa slovačke stranice: vrijednosti svakih petnaest minuta,
// vrijeme lokalno.
const ispisSHMU = `<table><caption>Merané hodnoty vodného stavu pre stanicu<br>Bratislava - Dunaj</caption>
<tbody><tr> <td headers="h_datum_cas" >21.9.2026 07:15</td> <td headers="h_vodny_stav" >265</td><td headers="h_teplota_vody" >18.8</td></tr>
<tr> <td headers="h_datum_cas" >21.9.2026 07:00</td> <td headers="h_vodny_stav" >266</td><td headers="h_teplota_vody" >18.8</td></tr>
<tr> <td headers="h_datum_cas" >21.9.2026 06:45</td> <td headers="h_vodny_stav" >266</td><td headers="h_teplota_vody" >18.7</td></tr>
<tr> <td headers="h_datum_cas" >21.9.2026 06:00</td> <td headers="h_vodny_stav" >268</td><td headers="h_teplota_vody" >18.7</td></tr></tbody></table>`

// Uzimaju se samo puni sati, a vrijeme je slovačko lokalno: ljeti dva sata
// ispred UTC-a.
func TestSHMUUzimaPuneSate(t *testing.T) {
	r, err := CitajSHMU(ispisSHMU)
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 {
		t.Fatalf("redaka %d, očekivano 2: %+v", len(r), r)
	}
	if !r[0].Kad.Equal(time.Date(2026, 9, 21, 4, 0, 0, 0, time.UTC)) || r[0].LevelCm == nil || *r[0].LevelCm != 268 {
		t.Errorf("prvi redak: %+v — 06:00 po slovačkom je 04:00 UTC", r[0])
	}
	if !r[1].Kad.Equal(time.Date(2026, 9, 21, 5, 0, 0, 0, time.UTC)) || r[1].LevelCm == nil || *r[1].LevelCm != 266 {
		t.Errorf("drugi redak: %+v", r[1])
	}
}

// Tablica bez ijednog punog sata mora to i reći, a ne šutjeti.
func TestSHMUJavljaKadNemaPunihSati(t *testing.T) {
	samoCetvrtine := `<td headers="h_datum_cas" >21.9.2026 07:15</td> <td headers="h_vodny_stav" >265</td>`
	if _, err := CitajSHMU(samoCetvrtine); err == nil {
		t.Error("tablica bez punih sati mora javiti grešku")
	}
	if _, err := CitajSHMU("<html><body>nešto drugo</body></html>"); err == nil || !strings.Contains(err.Error(), "nema tablice") {
		t.Errorf("stranica bez tablice: %v", err)
	}
	// Greška se vidi u ispisu kruga: kaže što nedostaje i što učiniti, a broj
	// redaka stoji iza dvotočke, pa mu riječ ne treba slagati.
	for _, n := range []int{1, 2, 5, 11, 21, 22} {
		zeli := fmt.Sprintf("slovačka stranica nema nijednog očitanja na punom satu (redaka u tablici: %d); "+
			"ako se to ponavlja, stranica se vjerojatno promijenila, javite administratoru", n)
		if _, err := CitajSHMU(strings.Repeat(samoCetvrtine+"\n", n)); err == nil || err.Error() != zeli {
			t.Errorf("%d redaka: %v, želim %q", n, err, zeli)
		}
	}
}

// Svaki čitač prepoznaje samo svoje adrese.
func TestSHMUPrepoznajeSvojeAdrese(t *testing.T) {
	sk := AdresaSHMU(5140)
	if PostajaSHMUIzAdrese(sk) != 5140 {
		t.Errorf("iz %q ne čita se broj postaje", sk)
	}
	if !(SHMU{}).Prepoznaje(sk) {
		t.Error("slovački čitač ne prepoznaje svoju adresu")
	}
	if (Vizugy{}).Prepoznaje(sk) || (Hidmet{}).Prepoznaje(sk) || (&Client{}).Prepoznaje(sk) {
		t.Error("tuđi čitač prepoznaje slovačku adresu")
	}
	if (SHMU{}).Prepoznaje(AdresaHidmet(42010)) {
		t.Error("slovački čitač prepoznaje srpsku adresu")
	}
}
