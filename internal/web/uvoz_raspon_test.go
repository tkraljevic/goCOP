package web

import (
	"testing"
	"time"
)

// Letva akumulacije vodi kotu u cm n. m., pa joj je raspon drugi, a poruka
// govori o koti.
func TestRasponUnosaZaKotu(t *testing.T) {
	sada := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	redci := []ZalijepljenoOcitanje{{Kad: sada.Add(-time.Hour), Vrijedi: 19089}, {Kad: sada.Add(-time.Hour), Vrijedi: 100}}
	oznaciIzvanRasponaUnosa(redci, true, sada)
	if redci[0].Greska != "" || redci[1].Greska != "kota je izvan razumnog raspona, od 5000 do 40000 cm" {
		t.Errorf("kota: %+v", redci)
	}
}
