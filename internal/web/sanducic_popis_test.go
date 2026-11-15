package web

import (
	"testing"

	"gocop/internal/posta"
)

// Plan Sandučića, korak 9 i novo ponašanje 5, 6 i 11: mapa Skice se ne
// nudi, Arhiviraj samo kad mapa Arhiva postoji, a uz ukupan broj piše
// raspon prikazanih pisama.
func TestPopisPisamaMapeIRaspon(t *testing.T) {
	mape := bezSkica(posta.Mape)
	for _, m := range mape {
		if m.ID == "drafts" {
			t.Fatal("mapa Skice se ne nudi")
		}
	}
	if len(mape) != len(posta.Mape)-1 {
		t.Errorf("ostale mape moraju ostati: %v", mape)
	}
	d := SanducicData{Mape: mape}
	if !d.ImaArhivu() {
		t.Error("s mapom Arhiva Arhiviraj se nudi")
	}
	d.Mape = []posta.Mapa{{ID: "inbox"}}
	if d.ImaArhivu() {
		t.Error("bez mape Arhiva Arhiviraj se ne nudi")
	}
	if d.Raspon() != "" {
		t.Error("bez pisama nema raspona")
	}
	d.Stranica, d.Pisma = 2, make([]posta.Pismo, 7)
	if g := d.Raspon(); g != "51–57" {
		t.Errorf("raspon druge stranice: %q", g)
	}
}
