package javnivodostaji

import (
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
	if !r[0].Kad.Equal(time.Date(2026, 9, 21, 4, 0, 0, 0, time.UTC)) || r[0].Cm != 268 {
		t.Errorf("prvi redak: %s %d cm — 06:00 po slovačkom je 04:00 UTC", r[0].Kad, r[0].Cm)
	}
	if !r[1].Kad.Equal(time.Date(2026, 9, 21, 5, 0, 0, 0, time.UTC)) || r[1].Cm != 266 {
		t.Errorf("drugi redak: %s %d cm", r[1].Kad, r[1].Cm)
	}
}

// Tablica bez ijednog punog sata mora to i reći, a ne šutjeti.
func TestSHMUJavljaKadNemaPunihSati(t *testing.T) {
	samoCetvrtine := `<td headers="h_datum_cas" >21.9.2026 07:15</td> <td headers="h_vodny_stav" >265</td>`
	if _, err := CitajSHMU(samoCetvrtine); err == nil {
		t.Error("tablica bez punih sati mora javiti grešku")
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
