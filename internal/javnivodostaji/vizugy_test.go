package javnivodostaji

import (
	"testing"
	"time"
)

// Stranica ne daje tablicu nego polja koja crta graf. Na kraju polja stoje
// nule za sate koje postaja još nije javila.
const ispisVizugy = `<script>
 var Vizallas = new Array(28, 30, 31, 0, 33, 0, 0, 0);
 Idopont = new Array( '2026.09.06. 01:00', '2026.09.06. 02:00', '2026.09.06. 03:00',
 '2026.09.06. 04:00', '2026.09.06. 05:00', '2026.09.06. 06:00', '2026.09.06. 07:00',
 '2026.09.06. 08:00');
 Vizhozam = new Array( '832,00 m<sup>3</sup>/s', '-', '840,50 m<sup>3</sup>/s', '-',
 '846,70 m<sup>3</sup>/s', '-', '-', '-');
</script>`

// Vrijeme je mađarsko lokalno; ljeti je to dva sata ispred UTC-a. Nula usred
// niza je pravi vodostaj i ostaje, a završne nule su sati koji još nisu javljeni.
func TestVizugyCitaPoljaIZonu(t *testing.T) {
	r, err := CitajVizugy(ispisVizugy)
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 5 {
		t.Fatalf("redaka %d, očekivano 5 (tri završne nule otpadaju): %+v", len(r), r)
	}
	if !r[0].Kad.Equal(time.Date(2026, 9, 5, 23, 0, 0, 0, time.UTC)) || r[0].LevelCm == nil || *r[0].LevelCm != 28 {
		t.Errorf("prvi redak: %+v — 01:00 po mađarskom je 23:00 UTC prethodnog dana", r[0])
	}
	if r[3].LevelCm == nil || *r[3].LevelCm != 0 {
		t.Errorf("nula usred niza je vodostaj i mora ostati: %+v", r[3])
	}
	if !r[4].Kad.Equal(time.Date(2026, 9, 6, 3, 0, 0, 0, time.UTC)) || r[4].LevelCm == nil || *r[4].LevelCm != 33 {
		t.Errorf("zadnji redak: %+v", r[4])
	}
}

// Protok stoji uz vodostaj istog sata, s decimalnim zarezom; crtica znači da
// ga nema, a vodostaj toga sata ostaje.
func TestVizugyCitaProtok(t *testing.T) {
	r, err := CitajVizugy(ispisVizugy)
	if err != nil {
		t.Fatal(err)
	}
	if r[0].FlowM3s == nil || *r[0].FlowM3s != 832 || r[0].FlowMetoda != "KRIVULJA" {
		t.Errorf("prvi sat: protok %v, metoda %q", r[0].FlowM3s, r[0].FlowMetoda)
	}
	if r[1].FlowM3s != nil || r[1].LevelCm == nil {
		t.Errorf("sat s crticom nema protok, a vodostaj ostaje: %+v", r[1])
	}
	if r[2].FlowM3s == nil || *r[2].FlowM3s != 840.5 {
		t.Errorf("decimalni zarez: %v", r[2].FlowM3s)
	}
	if r[4].FlowM3s == nil || *r[4].FlowM3s != 846.7 {
		t.Errorf("zadnji sat: %v", r[4].FlowM3s)
	}
}

// Svaki čitač prepoznaje samo svoje adrese.
func TestVizugyPrepoznajeSvojeAdrese(t *testing.T) {
	hu := AdresaVizugy("16496059-97ab-11d4-bb62-00508ba24287")
	if PostajaVizugyIzAdrese(hu) != "16496059-97AB-11D4-BB62-00508BA24287" {
		t.Errorf("iz %q ne čita se oznaka postaje", hu)
	}
	v := Vizugy{}
	if !v.Prepoznaje(hu) {
		t.Errorf("mađarski čitač ne prepoznaje svoju adresu")
	}
	if v.Prepoznaje(AdresaHidmet(42010)) || v.Prepoznaje(AdresaPostaje(Postaja{ID: 425, Sektor: 2})) {
		t.Errorf("mađarski čitač prepoznaje tuđu adresu")
	}
	if (Hidmet{}).Prepoznaje(hu) || (&Client{}).Prepoznaje(hu) {
		t.Errorf("tuđi čitač prepoznaje mađarsku adresu")
	}
}
