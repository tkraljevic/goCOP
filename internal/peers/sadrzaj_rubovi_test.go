package peers

// Sadržaj na jednom čvoru, bez mreže: otpuštanje primljenog sadržaja kad mu
// istekne rok, što se šalje drugom čvoru na njegove želje, što se od
// primljenog upiše i opis za dnevnik razmjene.

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/sadrzaj"

	_ "modernc.org/sqlite"
)

// spremisteNaDisku otvara spremište u datoteci i vezu na istu datoteku,
// kojom test pomiče vrijeme primitka
func spremisteNaDisku(t *testing.T) (*sadrzaj.Spremiste, *sql.DB) {
	t.Helper()
	put := filepath.Join(t.TempDir(), "sadrzaj.db")
	sp, err := sadrzaj.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sp.Zatvori() })
	veza, err := sql.Open("sqlite", put+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { veza.Close() })
	return sp, veza
}

// primljenPrije upiše sadržaj kao da je stigao s drugog čvora prije zadanog
// broja dana
func primljenPrije(t *testing.T, sp *sadrzaj.Spremiste, veza *sql.DB, izvor, kanal, tekst string, dana int) string {
	t.Helper()
	b := []byte("%PDF-1.4 " + tekst)
	o, err := sp.Upisi(ctxRub, "application/pdf", b, izvor, sadrzaj.Veza{Entitet: "prijave", EntitetID: tekst, Uloga: "izvornik", Kanal: kanal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := veza.Exec(`UPDATE sadrzaj SET primljeno = ? WHERE otisak = ?`, time.Now().UTC().AddDate(0, 0, -dana), o); err != nil {
		t.Fatal(err)
	}
	return o
}

// Primljeni sadržaj kojem je po pretplati istekao rok miče se s računala;
// sadržaj u roku, sadržaj kanala bez roka i vlastiti sadržaj ostaju.
func TestOtpustanjeStarogSadrzaja(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	if n, b, err := s.OtpustiStare(ctxRub); n != 0 || b != 0 || err != nil {
		t.Errorf("bez spremišta: %d %d %v", n, b, err)
	}
	sp, veza := spremisteNaDisku(t)
	s.SetSpremiste(sp)
	star := primljenPrije(t, sp, veza, "cvor:ured", "prijave/16/2026", "stara prijava BP 16", 100)
	nov := primljenPrije(t, sp, veza, "cvor:ured", "prijave/16/2026", "nova prijava BP 16", 1)
	trajan := primljenPrije(t, sp, veza, "cvor:ured", "prijave/3/2026", "stara prijava BP 3", 100)
	duzi := primljenPrije(t, sp, veza, "cvor:ured", "prijave/17/2026", "prijava BP 17 s duljim rokom", 50)
	vlastit := primljenPrije(t, sp, veza, "ovdje", "prijave/16/2026", "vlastita prijava", 100)

	// bez pravila s rokom ništa se ne otpušta
	if _, err := s.AddSubscription(ctxRub, Subscription{Kind: "prijave", AreaID: 3}); err != nil {
		t.Fatal(err)
	}
	if n, _, err := s.OtpustiStare(ctxRub); n != 0 || err != nil {
		t.Errorf("bez roka: %d %v", n, err)
	}
	for _, r := range []Subscription{{Kind: "prijave", AreaID: 16, DrziDana: 30}, {Kind: "prijave", AreaID: 17, DrziDana: 90}} {
		if _, err := s.AddSubscription(ctxRub, r); err != nil {
			t.Fatal(err)
		}
	}
	n, bajtova, err := s.OtpustiStare(ctxRub)
	if err != nil || n != 1 || bajtova != int64(len("%PDF-1.4 stara prijava BP 16")) {
		t.Fatalf("otpušteno %d (%d B), %v", n, bajtova, err)
	}
	if sp.Ima(ctxRub, star) || !sp.Ima(ctxRub, nov) || !sp.Ima(ctxRub, trajan) || !sp.Ima(ctxRub, vlastit) || !sp.Ima(ctxRub, duzi) {
		t.Errorf("nakon otpuštanja: stari %v, novi %v, bez roka %v, vlastiti %v, s duljim rokom %v",
			sp.Ima(ctxRub, star), sp.Ima(ctxRub, nov), sp.Ima(ctxRub, trajan), sp.Ima(ctxRub, vlastit), sp.Ima(ctxRub, duzi))
	}

	// uredski čvor drži sve
	s.SetWantsAll(true)
	primljenPrije(t, sp, veza, "cvor:ured", "prijave/16/2026", "još jedna stara", 100)
	if n, _, err := s.OtpustiStare(ctxRub); n != 0 || err != nil {
		t.Errorf("uredski čvor otpušta: %d %v", n, err)
	}
	s.SetWantsAll(false)

	if err := sp.Zatvori(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.OtpustiStare(ctxRub); err == nil {
		t.Error("otpuštanje iz zatvorenog spremišta bez greške")
	}
	pokvariTablicu(t, s, "subscriptions")
	if _, _, err := s.OtpustiStare(ctxRub); err == nil {
		t.Error("otpuštanje bez čitljivih pretplata bez greške")
	}
}

// Na želje druge strane čvor šalje ono što drži i što druga strana po
// svojim pretplatama smije dobiti, najviše 200 stavki jednom razmjenom.
func TestSadrzajNaZeljeDrugeStrane(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if s.sadrzajZa(ctxRub, []string{"x"}, nil) != nil {
		t.Error("bez spremišta poslan sadržaj")
	}
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sp.Zatvori() })
	s.SetSpremiste(sp)
	upisi := func(kanal, tekst string) string {
		t.Helper()
		o, err := sp.Upisi(ctxRub, "application/pdf", []byte(tekst), "ovdje", sadrzaj.Veza{Entitet: "prijave", EntitetID: tekst, Uloga: "izvornik", Kanal: kanal})
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	o16 := upisi("prijave/16/2026", "prijava BP 16")
	o3 := upisi("prijave/3/2026", "prijava BP 3")
	zajednicki := upisi("", "akt bez kanala")
	if s.sadrzajZa(ctxRub, nil, nil) != nil {
		t.Error("bez želja poslan sadržaj")
	}
	njihove := &Wants{Rules: []Subscription{{Kind: "prijave", AreaID: 16}}}
	stavke := s.sadrzajZa(ctxRub, []string{"nema-ga", o3, o16, zajednicki}, njihove)
	if len(stavke) != 2 || stavke[0].Otisak != o16 || stavke[0].Kanal != "prijave/16/2026" || string(stavke[0].Podaci) != "prijava BP 16" ||
		stavke[1].Otisak != zajednicki || stavke[1].Bajtova != len("akt bez kanala") {
		t.Fatalf("poslano: %+v", stavke)
	}

	var mnogo []string
	for i := 0; i < najviseSadrzajaPoRazmjeni+5; i++ {
		mnogo = append(mnogo, upisi("prijave/16/2026", fmt.Sprintf("prijava br. %d", i)))
	}
	if stavke := s.sadrzajZa(ctxRub, mnogo, nil); len(stavke) != najviseSadrzajaPoRazmjeni {
		t.Errorf("jednom razmjenom poslano %d stavki", len(stavke))
	}
}

// Od primljenog se upiše samo ono što prođe provjeru otiska; čvor bez
// spremišta ne upisuje ništa.
func TestPrimljeniSadrzajProlaziOtisak(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	dobar := []byte("%PDF-1.4 ispravan izvornik")
	stavke := []stavkaSadrzaja{
		{Otisak: sadrzaj.Otisak(dobar), Vrsta: "application/pdf", Kanal: "prijave/16/2026", Podaci: dobar},
		{Otisak: sadrzaj.Otisak([]byte("nešto drugo")), Vrsta: "application/pdf", Podaci: []byte("%PDF-1.4 podmetnuto")},
	}
	if n := s.primiSadrzaj(ctxRub, "ured-vukovar", stavke); n != 0 {
		t.Errorf("bez spremišta upisano %d", n)
	}
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sp.Zatvori() })
	s.SetSpremiste(sp)
	if n := s.primiSadrzaj(ctxRub, "ured-vukovar", stavke); n != 1 {
		t.Errorf("upisano %d, očekivan samo ispravan", n)
	}
	if !sp.Ima(ctxRub, stavke[0].Otisak) || sp.Ima(ctxRub, stavke[1].Otisak) {
		t.Error("u spremištu je krivi sadržaj")
	}
}

// Dnevnik razmjene kaže koliko je sadržaja i megabajta, s pravim oblikom
// riječi.
func TestOpisSadrzajaZaDnevnik(t *testing.T) {
	for _, p := range []struct {
		stavke []stavkaSadrzaja
		opis   string
	}{
		{nil, "0 sadržaja, 0.0 MB"},
		{[]stavkaSadrzaja{{Bajtova: 1_500_000}}, "1 sadržaj, 1.5 MB"},
		{[]stavkaSadrzaja{{Bajtova: 1_500_000}, {Bajtova: 500_000}}, "2 sadržaja, 2.0 MB"},
		{make([]stavkaSadrzaja, 5), "5 sadržaja, 0.0 MB"},
	} {
		if got := opisSadrzaja(p.stavke); got != p.opis {
			t.Errorf("%q, očekivano %q", got, p.opis)
		}
	}
}
