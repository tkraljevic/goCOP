package service_test

import (
	"testing"

	"gocop/internal/service"
)

// Isključen račun gleda se tuđim očima samo za čitanje (Odluke (8. 10.
// 2026.), pitanje 2): pregled se smije pokrenuti i traje, a čim se račun
// isključi, i usred pregleda, sesija to javlja već pri idućem zahtjevu.
func TestIskljucenRacunTudjimOcimaSamoCita(t *testing.T) {
	o := novaOkolinaOporavka(t)
	sesija, _, err := o.auth.Login("uprava", "lozinka1", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	ana := o.racun(t, "aanic", "lozinka-aanic", "aanic@voda.hr", false, true)
	pogled := func() *service.SessionView {
		t.Helper()
		v, err := o.auth.AuthenticateSessionView(sesija.ID)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	ukljuci := func(da bool) {
		t.Helper()
		if _, err := o.db.Exec(`UPDATE users SET is_active = ? WHERE id = ?`, da, ana.ID.String()); err != nil {
			t.Fatal(err)
		}
	}

	if err := o.auth.StartViewingAs(sesija.ID, o.admin, ana.ID); err != nil {
		t.Fatal(err)
	}
	if v := pogled(); !v.Viewing || v.RacunIskljucen() {
		t.Fatalf("aktivan račun tuđim očima: gleda %v, isključen %v", v.Viewing, v.RacunIskljucen())
	}

	// isključenje usred pregleda: pregled traje, ali samo čita
	ukljuci(false)
	if v := pogled(); !v.Viewing || v.User.ID != ana.ID || !v.RacunIskljucen() {
		t.Errorf("račun isključen usred pregleda: gleda %v, isključen %v", v.Viewing, v.RacunIskljucen())
	}

	// vlastiti pogled administratora nije pogled na isključen račun
	if err := o.auth.StopViewingAs(sesija.ID); err != nil {
		t.Fatal(err)
	}
	if v := pogled(); v.Viewing || v.RacunIskljucen() {
		t.Errorf("vlastiti pogled: gleda %v, isključen %v", v.Viewing, v.RacunIskljucen())
	}

	// pregled isključenog računa smije se i pokrenuti
	if err := o.auth.StartViewingAs(sesija.ID, o.admin, ana.ID); err != nil {
		t.Fatalf("pregled isključenog računa: %v", err)
	}
	if v := pogled(); !v.Viewing || !v.RacunIskljucen() {
		t.Errorf("pokrenut pregled isključenog računa: gleda %v, isključen %v", v.Viewing, v.RacunIskljucen())
	}

	// ponovno uključen račun više nije samo za čitanje
	ukljuci(true)
	if v := pogled(); !v.Viewing || v.RacunIskljucen() {
		t.Errorf("ponovno uključen račun: gleda %v, isključen %v", v.Viewing, v.RacunIskljucen())
	}

	// pogled bez gledane osobe nije pogled na isključen račun
	if (&service.SessionView{Viewing: true}).RacunIskljucen() {
		t.Error("pogled bez gledane osobe javlja isključen račun")
	}
}
