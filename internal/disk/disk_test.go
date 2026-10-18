package disk

import (
	"testing"
)

// Upozorava se ispod 10 GB ili ispod 10 % diska; točno na rubu još ne
func TestPragovi(t *testing.T) {
	for _, s := range []struct {
		ime              string
		ukupno, slobodno uint64
		malo             bool
	}{
		{"točno 10 GB na disku od 50 GB", 50e9, 10e9, false},
		{"bajt manje od 10 GB", 50e9, 10e9 - 1, true},
		{"točno 10 % diska od 200 GB", 200e9, 20e9, false},
		{"bajt manje od 10 % diska", 200e9, 20e9 - 1, true},
		{"99 GB na disku od 1 TB je ispod 10 %", 1e12, 99e9, true},
		{"100 GB na disku od 1 TB", 1e12, 100e9, false},
		{"malen disk, a skoro prazan", 12e9, 11e9, false},
		{"malen disk, ispod 10 GB slobodnog", 12e9, 9e9, true},
	} {
		d := Disk{Mjera: Mjera{Ukupno: s.ukupno, Slobodno: s.slobodno}}
		if d.Malo() != s.malo {
			t.Errorf("%s: malo mjesta = %v, očekivano %v", s.ime, d.Malo(), s.malo)
		}
	}
}

// Brojevi su hrvatski: decimalni zarez, razmak prije jedinice i znaka %
func TestTekstUpozorenja(t *testing.T) {
	for _, s := range []struct {
		d       Disk
		ocekuje string
	}{
		{Disk{Mjera: Mjera{Ukupno: 128e9, Slobodno: 8_290_000_000}, Nazivi: []string{"bazom", "arhivom"}, Premjesti: "arhivu"},
			"Na disku s bazom i arhivom ostalo je 8,2 GB (6 % diska). Oslobodite mjesto ili premjestite arhivu na drugi disk."},
		{Disk{Mjera: Mjera{Ukupno: 64e9, Slobodno: 420_500_000}, Nazivi: []string{"bazom"}},
			"Na disku s bazom ostalo je 420 MB (manje od 1 % diska). Oslobodite mjesto na tom disku."},
		{Disk{Mjera: Mjera{Ukupno: 4e12, Slobodno: 399_999_999_999}, Nazivi: []string{"bazom", "arhivom", "paketima"}, Premjesti: "arhivu"},
			"Na disku s bazom, arhivom i paketima ostalo je 399 GB (9 % diska). Oslobodite mjesto ili premjestite arhivu na drugi disk."},
	} {
		if got := s.d.Upozorenje(); got != s.ocekuje {
			t.Errorf("upozorenje\n  %q\nočekivano\n  %q", got, s.ocekuje)
		}
	}
	d := Disk{Mjera: Mjera{Ukupno: 500e9, Slobodno: 125e9}, Nazivi: []string{"paketima"}}
	if got, o := d.dovoljno(), "Na disku s paketima opet ima dovoljno mjesta: slobodno je 125 GB (25 % diska)."; got != o {
		t.Errorf("dovoljno mjesta: %q, očekivano %q", got, o)
	}
}

// Na pločici poruka kaže i na kojem je čvoru disk, a savjet je zaseban, jer
// ga vidi samo administrator
func TestPorukaISavjet(t *testing.T) {
	d := Disk{Mjera: Mjera{Ukupno: 128e9, Slobodno: 8_290_000_000}, Nazivi: []string{"bazom", "arhivom"}, Premjesti: "arhivu"}
	if got, o := d.Poruka("COP Osijek (kućni server)"), "Na disku čvora COP Osijek (kućni server) s bazom i arhivom ostalo je 8,2 GB (6 % diska)."; got != o {
		t.Errorf("poruka %q, očekivano %q", got, o)
	}
	if got, o := d.Poruka(""), "Na disku s bazom i arhivom ostalo je 8,2 GB (6 % diska)."; got != o {
		t.Errorf("poruka bez čvora %q, očekivano %q", got, o)
	}
	if got, o := d.Savjet(), "Oslobodite mjesto ili premjestite arhivu na drugi disk."; got != o {
		t.Errorf("savjet %q, očekivano %q", got, o)
	}
}

func TestVelicina(t *testing.T) {
	for b, o := range map[uint64]string{
		0:                 "0 MB",
		420_500_000:       "420 MB",
		999_999_999:       "999 MB",
		1e9:               "1,0 GB",
		8_299_999_999:     "8,2 GB",
		9_999_999_999:     "9,9 GB",
		10e9:              "10 GB",
		999_999_999_999:   "999 GB",
		1e12:              "1,0 TB",
		1_250_000_000_000: "1,2 TB",
		9_999_999_999_999: "9,9 TB",
		10e12:             "10 TB",
		12_345e9:          "12 TB",
	} {
		if got := Velicina(b); got != o {
			t.Errorf("Velicina(%d) = %q, očekivano %q", b, got, o)
		}
	}
}

func TestSpoji(t *testing.T) {
	for o, r := range map[string][]string{
		"":                   nil,
		"bazom":              {"bazom"},
		"bazom i arhivom":    {"bazom", "arhivom"},
		"a, b, c i paketima": {"a", "b", "c", "paketima"},
	} {
		if got := spoji(r); got != o {
			t.Errorf("spoji(%q) = %q, očekivano %q", r, got, o)
		}
	}
}
