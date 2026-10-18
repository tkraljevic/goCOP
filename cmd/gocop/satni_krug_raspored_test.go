package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"gocop/internal/config"
)

// Pri pokretanju krug ide odmah kad na čvoru koji izdaje prognozu za sat
// zadnjeg termina nema izdanja. Čvor koji ne izdaje ne žuri: izdanje mu
// stiže razmjenom.
func TestBezIzdanjaZaSat(t *testing.T) {
	sat := time.Date(2026, time.October, 18, 11, 0, 0, 0, time.UTC) // 13 h u Zagrebu
	k, _ := okolinaKruga(t, true)
	if !k.bezIzdanja(sat) {
		t.Error("bez ijednog izdanja: krug ne ide odmah")
	}
	izdaj := func(s time.Time) {
		t.Helper()
		h := s.Unix() / 3600
		if _, err := k.prognoze.Exec(`INSERT INTO izdane (letva, velicina, izdano, ciljni, vrijednost, dolje, gore, model)
			VALUES ('batina', 'vodostaj', ?, ?, 1, 0, 2, 'proba')`, h, h+1); err != nil {
			t.Fatal(err)
		}
	}
	izdaj(sat.Add(-time.Hour))
	if !k.bezIzdanja(sat) {
		t.Error("izdano za prošli sat: krug ne ide odmah")
	}
	izdaj(sat)
	if k.bezIzdanja(sat) {
		t.Error("izdano za ovaj sat: krug ide odmah")
	}

	ne, _ := okolinaKruga(t, false)
	if ne.bezIzdanja(sat) {
		t.Error("čvor koji ne izdaje prognozu traži izdanje")
	}

	// baza prognoza koja se ne da pročitati: krug ide odmah, a greška u
	// dnevnik, jer je jedan krug viška bolji od sata bez prognoze
	dnevnik := dnevnikTesta(t)
	if err := k.prognoze.Close(); err != nil {
		t.Fatal(err)
	}
	if !k.bezIzdanja(sat) || !strings.Contains(dnevnik.String(), "prognoza: zadnje izdanje: ") {
		t.Errorf("baza prognoza zatvorena: dnevnik %q", dnevnik.String())
	}
}

// Minuta iz gocop.toml; neispravna se javi u dnevnik i vrijedi zadana
func TestMinutaPreuzimanjaIzPostavki(t *testing.T) {
	var dnevnik []string
	javi := func(f string, a ...any) { dnevnik = append(dnevnik, fmt.Sprintf(f, a...)) }
	cfg := config.Default()
	cfg.Readings.MinutaPreuzimanja = 5
	if m := minutaPreuzimanja(cfg, javi); m != 5 || len(dnevnik) != 0 {
		t.Errorf("minuta %d, dnevnik %q", m, dnevnik)
	}
	cfg.Readings.MinutaPreuzimanja = 75
	zadana := config.Default().Readings.MinutaPreuzimanja
	if m := minutaPreuzimanja(cfg, javi); m != zadana || len(dnevnik) != 1 ||
		!strings.Contains(dnevnik[0], "minuta_preuzimanja = 75 ne vrijedi") || !strings.Contains(dnevnik[0], fmt.Sprintf(":%02d", zadana)) {
		t.Errorf("minuta %d, dnevnik %q", m, dnevnik)
	}
}
