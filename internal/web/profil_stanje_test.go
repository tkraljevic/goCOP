package web

import (
	"testing"
	"time"

	"gocop/internal/models"
)

// Stanje uz odjeljke Profila (Odluke (15. 11. 2026.), Profil 8).
func TestStanjeOdjeljakaProfila(t *testing.T) {
	d := UserPageData{}
	if d.StanjeDezurstava() != "" || d.StanjePrijava() != "" {
		t.Error("bez planova i prijava stanje je prazno")
	}
	if g := d.StanjePotpisa(); g != "bez skena · bez ključa" {
		t.Errorf("bez potpisa: %q", g)
	}

	d.Planovi = []models.PlanOsobe{{Dezurstava: 3, CekaPotvrdu: 1}, {Dezurstava: 2}}
	if g := d.StanjeDezurstava(); g != "5 dežurstava · 1 čeka potvrdu" {
		t.Errorf("dežurstva: %q", g)
	}
	d.Planovi = []models.PlanOsobe{{Dezurstava: 2}}
	if g := d.StanjeDezurstava(); g != "2 dežurstva" {
		t.Errorf("dežurstva bez čekanja: %q", g)
	}

	d.MojePrijave = []models.PrijavaGreske{{}, {Odgovori: []models.OdgovorNaPrijavu{{}}}, {}}
	if g := d.StanjePrijava(); g != "3 prijave · 2 bez odgovora" {
		t.Errorf("prijave: %q", g)
	}
	d.MojePrijave = []models.PrijavaGreske{{Odgovori: []models.OdgovorNaPrijavu{{}}}}
	if g := d.StanjePrijava(); g != "1 prijava" {
		t.Errorf("odgovorena prijava: %q", g)
	}

	d.ImaPotpisSliku = true
	d.Kljuc = PodaciKljuca{Ima: true, Do: time.Date(2029, 11, 10, 9, 12, 0, 0, time.UTC)}
	if g := d.StanjePotpisa(); g != "sken spremljen · ključ do 2029." {
		t.Errorf("potpis s ključem: %q", g)
	}
	d.Kljuc.Istekao = true
	if g := d.StanjePotpisa(); g != "sken spremljen · ključ istekao" {
		t.Errorf("istekao ključ: %q", g)
	}
}
