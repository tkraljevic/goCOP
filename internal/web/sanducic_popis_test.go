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

// Plan Sandučića, novo ponašanje 10: Odustani u novom pismu vraća na
// pismo na koje se odgovaralo, inače u mapu ili u Sandučić.
func TestOdustaniOdNovogPisma(t *testing.T) {
	for _, c := range []struct {
		id      string
		izvorno *posta.Pismo
		mapa    string
		want    string
	}{
		{"AAMk/1+=", &posta.Pismo{}, "inbox", "/posta/pismo?id=AAMk%2F1%2B%3D&mapa=inbox"},
		{"", nil, "sentitems", "/posta?mapa=sentitems"},
		{"", nil, "", "/posta"},
		{"AAMk/1+=", nil, "", "/posta"},
	} {
		if g := odustaniOd(c.id, c.izvorno, c.mapa); g != c.want {
			t.Errorf("odustaniOd(%q, %v, %q) = %q, a treba %q", c.id, c.izvorno != nil, c.mapa, g, c.want)
		}
	}
}
