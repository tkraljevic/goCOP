package repository

import (
	"context"
	"strings"
	"testing"
)

// Otisak poslužitelja sprema se uz lozinku e-pošte i uz račun sustava.
// Vezanje upisuje otisak samo zapisu koji ga nema; upisani otisak ne mijenja.
// Bez podešene pošte zapis bez otiska se briše.
func TestOtisakPosluziteljaUzRacun(t *testing.T) {
	ctx := context.Background()
	_, akti := bazaAkata(t)
	if err := akti.SaveRacunPoste(ctx, &RacunPoste{UserID: "u1", Korisnik: "pero", Lozinka: []byte("z")}); err != nil {
		t.Fatal(err)
	}
	if err := akti.SaveRacunPoste(ctx, &RacunPoste{UserID: "u2", Korisnik: "ana", Lozinka: []byte("z"), Posluzitelj: "smtp|smtp.primjer.hr|587|starttls|"}); err != nil {
		t.Fatal(err)
	}
	for i, otisak := range []string{"ews|https://owa.primjer.hr/ews/exchange.asmx|voda", "ews|https://drugi.test/ews/exchange.asmx|voda"} {
		vezano, obrisano, err := akti.VeziRacunePostePosluzitelju(ctx, otisak)
		if err != nil || obrisano != 0 || vezano != int64(1-i) {
			t.Fatalf("vezanje %d: vezano %d, obrisano %d, %v", i, vezano, obrisano, err)
		}
	}
	if r, err := akti.GetRacunPoste(ctx, "u1"); err != nil || r == nil || r.Posluzitelj != "ews|https://owa.primjer.hr/ews/exchange.asmx|voda" {
		t.Errorf("lozinka e-pošte: %+v %v", r, err)
	}
	if r, err := akti.GetRacunPoste(ctx, "u2"); err != nil || r == nil || r.Posluzitelj != "smtp|smtp.primjer.hr|587|starttls|" {
		t.Errorf("lozinka e-pošte s otiskom: %+v %v", r, err)
	}
	// bez podešene pošte zapis bez otiska se briše, a zapis s otiskom ostaje
	if err := akti.SaveRacunPoste(ctx, &RacunPoste{UserID: "u3", Korisnik: "iva", Lozinka: []byte("z")}); err != nil {
		t.Fatal(err)
	}
	if vezano, obrisano, err := akti.VeziRacunePostePosluzitelju(ctx, ""); err != nil || vezano != 0 || obrisano != 1 {
		t.Fatalf("bez pošte: vezano %d, obrisano %d, %v", vezano, obrisano, err)
	}
	if r, err := akti.GetRacunPoste(ctx, "u3"); err != nil || r != nil {
		t.Errorf("zapis bez otiska ostao je: %+v %v", r, err)
	}
	if r, err := akti.GetRacunPoste(ctx, "u2"); err != nil || r == nil {
		t.Errorf("zapis s otiskom je obrisan: %v", err)
	}
	// greška baze javlja se, i pri vezanju i pri brisanju
	if _, err := akti.db.ExecContext(ctx, `CREATE TRIGGER zabrana_vezanja BEFORE UPDATE OF posluzitelj ON posta_racuni BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := akti.db.ExecContext(ctx, `CREATE TRIGGER zabrana_brisanja BEFORE DELETE ON posta_racuni BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`); err != nil {
		t.Fatal(err)
	}
	if err := akti.SaveRacunPoste(ctx, &RacunPoste{UserID: "u4", Korisnik: "eva", Lozinka: []byte("z")}); err != nil {
		t.Fatal(err)
	}
	for _, otisak := range []string{"", "smtp|smtp.primjer.hr|587|starttls|"} {
		if _, _, err := akti.VeziRacunePostePosluzitelju(ctx, otisak); err == nil || !strings.Contains(err.Error(), "zabranjeno") {
			t.Errorf("greška baze uz otisak %q: %v", otisak, err)
		}
	}

	sustavi := NewRacuniSustavaRepository(bazaRacuna(t))
	if err := sustavi.Spremi(ctx, &RacunSustava{Sustav: "posta-pin", Korisnik: "pin", Lozinka: []byte("z"), Posluzitelj: "smtp|smtp.primjer.hr|587|"}); err != nil {
		t.Fatal(err)
	}
	if err := sustavi.VeziPosluzitelju(ctx, "posta-pin", "smtp|drugi.test|587|"); err != nil {
		t.Fatal(err)
	}
	if r, err := sustavi.Racun(ctx, "posta-pin"); err != nil || r == nil || r.Posluzitelj != "smtp|smtp.primjer.hr|587|" {
		t.Errorf("račun sustava s otiskom: %+v %v", r, err)
	}
	if err := sustavi.Spremi(ctx, &RacunSustava{Sustav: "posta-pin", Korisnik: "pin", Lozinka: []byte("z")}); err != nil {
		t.Fatal(err)
	}
	if err := sustavi.VeziPosluzitelju(ctx, " posta-pin ", "smtp|drugi.test|587|"); err != nil {
		t.Fatal(err)
	}
	if r, err := sustavi.Racun(ctx, "posta-pin"); err != nil || r == nil || r.Posluzitelj != "smtp|drugi.test|587|" {
		t.Errorf("račun sustava bez otiska: %+v %v", r, err)
	}
}
