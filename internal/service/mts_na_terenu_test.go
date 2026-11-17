package service

import (
	"testing"
	"time"

	"gocop/internal/models"
)

// Na terenu se vidi skladište koje je izdalo i dan dolaska (crtež 19a, 6);
// vreće ispražnjene na terenu nose skladište svoje vrste.
func TestNaTerenuPodrijetlo(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	danas := time.Now().In(models.Zagreb)
	for _, z := range []Zahvat{
		{Vrsta: models.PrometPocetno, Oblik: models.OblikPunjeno, Kolicina: 50},
		{Vrsta: models.PrometIzdano, Oblik: models.OblikPunjeno, Kolicina: 20, AreaID: 34, SectionCode: "B.34.1", Mjesto: "kod ustave"},
	} {
		z.SkladisteID, z.VrstaID = sk.ID, mpVrece
		if _, err := s.Provedi(ctx, u, uprava, z); err != nil {
			t.Fatal(err)
		}
	}
	zaprimiSve(t, s, ctx, u)
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPraznjenje, SkladisteID: sk.ID, VrstaID: mpVrece, Kolicina: 5,
		AreaID: 34, SectionCode: "B.34.1", Mjesto: "kod ustave"}); err != nil {
		t.Fatal(err)
	}
	teren, err := s.NaTerenu(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(teren) != 2 {
		t.Fatalf("na terenu: %+v", teren)
	}
	for _, st := range teren {
		if st.IzdaloID != sk.ID || st.IzdaloNaziv != sk.Naziv || st.Od.Format("2006-01-02") != danas.Format("2006-01-02") {
			t.Errorf("%s: izdalo %q %q, od %v", st.Oblik, st.IzdaloID, st.IzdaloNaziv, st.Od)
		}
		if st.NaslovMjesta() == st.MjestoNaziv() || st.Mjesto != "kod ustave" {
			t.Errorf("naslov mjesta nosi opis: %q", st.NaslovMjesta())
		}
	}
	if !danIz("nije dan").IsZero() || s.nazivSkladista(ctx, map[string]string{}, "nema-ga") != "" {
		t.Error("nečitljiv dan ili nepostojeće skladište")
	}
}
