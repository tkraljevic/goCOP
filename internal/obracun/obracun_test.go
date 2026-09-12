package obracun

import (
	"testing"
	"time"

	"gocop/internal/models"
)

func kad(g, m, d, h, min int) time.Time {
	return time.Date(g, time.Month(m), d, h, min, 0, 0, models.Zagreb)
}

// Blagdani se ne prepisuju po godinama nego računaju. Provjera prema popisu
// koji obrazac IORS nosi za 2020., 2023. i 2026. — sve tri godine, svih 14.
func TestHrvatskiBlagdaniKaoUObrascu(t *testing.T) {
	zelim := map[int][]string{
		2020: {"01-01", "01-06", "04-12", "04-13", "05-01", "05-30", "06-11", "06-22", "08-05", "08-15", "11-01", "11-18", "12-25", "12-26"},
		2023: {"01-01", "01-06", "04-09", "04-10", "05-01", "05-30", "06-08", "06-22", "08-05", "08-15", "11-01", "11-18", "12-25", "12-26"},
		2026: {"01-01", "01-06", "04-05", "04-06", "05-01", "05-30", "06-04", "06-22", "08-05", "08-15", "11-01", "11-18", "12-25", "12-26"},
	}
	for godina, dani := range zelim {
		var dobio []string
		for _, d := range (Hrvatski{}).Blagdani(godina) {
			dobio = append(dobio, d.Format("01-02"))
		}
		if len(dobio) != len(dani) {
			t.Fatalf("%d: %v, a obrazac ima %v", godina, dobio, dani)
		}
		for i := range dani {
			if dobio[i] != dani[i] {
				t.Errorf("%d: %s, a obrazac ima %s", godina, dobio[i], dani[i])
			}
		}
	}
}

// Stvarni redak iz IORS-a: nedjelja 11.6.2023., 15:00–24:00, teren — 7 h
// dnevnih i 2 h noćnih, kao nedjelja i blagdan. Formule tablice obrasca su
// to svrstavale u vikend s koeficijentom subote; ovdje se ne ponavlja.
func TestRazvrstajNedjeljaJeBlagdan(t *testing.T) {
	s := Razvrstaj(kad(2023, 6, 11, 15, 0), kad(2023, 6, 12, 0, 0), Hrvatski{})
	if s[BLD] != 7*time.Hour || s[BLN] != 2*time.Hour || len(s) != 2 {
		t.Errorf("nedjelja 15–24: %v", s)
	}
	if o := IORS2026.Obracunski(s, Teren); o != 7*2.2+2*2.55 {
		t.Errorf("obračunski: %v, očekuje %v", o, 7*2.2+2*2.55)
	}
}

// Subota ostaje svoje: 12.9.2026. 15–24 su 7 h subotnjih dnevnih i 2 noćna.
func TestRazvrstajSubota(t *testing.T) {
	s := Razvrstaj(kad(2026, 9, 12, 15, 0), kad(2026, 9, 13, 0, 0), Hrvatski{})
	if s[VID] != 7*time.Hour || s[VIN] != 2*time.Hour || len(s) != 2 {
		t.Errorf("subota 15–24: %v", s)
	}
}

func TestRazvrstajRadniDanPoPojasevima(t *testing.T) {
	// petak 5:30–23:30: noćni 0:30, dnevni 2 (6–8) + 6 (16–22), redovno 8, noćni 1:30
	s := Razvrstaj(kad(2026, 9, 11, 5, 30), kad(2026, 9, 11, 23, 30), Hrvatski{})
	zelim := Sati{NRD: 2 * time.Hour, DRD: 8 * time.Hour, RRV: 8 * time.Hour}
	for r, d := range zelim {
		if s[r] != d {
			t.Errorf("%s: %v, očekuje %v", r, s[r], d)
		}
	}
	if s.Ukupno() != 18*time.Hour {
		t.Errorf("ukupno %v", s.Ukupno())
	}
	// U uredu redovno vrijeme nosi 0: to je plaća, ne prekovremeni.
	if o := IORS2026.Obracunski(s, Ured); o != 8*1.5+2*1.85 {
		t.Errorf("ured: %v", o)
	}
}

// Noćna smjena preko ponoći: petak 22:00 – subota 06:00. Prvi dio je radni
// dan noćni, drugi subotnji noćni — dan se mijenja u ponoć.
func TestRazvrstajPrekoPonoci(t *testing.T) {
	s := Razvrstaj(kad(2026, 9, 11, 22, 0), kad(2026, 9, 12, 6, 0), Hrvatski{})
	if s[NRD] != 2*time.Hour || s[VIN] != 6*time.Hour || len(s) != 2 {
		t.Errorf("preko ponoći: %v", s)
	}
}

// Nedjelja 1.11.2026. je i blagdan — jedno te isto, bez dvostrukog brojanja.
func TestRazvrstajNedjeljaKojaJeBlagdan(t *testing.T) {
	s := Razvrstaj(kad(2026, 11, 1, 8, 0), kad(2026, 11, 1, 23, 0), Hrvatski{})
	if s[BLD] != 14*time.Hour || s[BLN] != 1*time.Hour || len(s) != 2 {
		t.Errorf("blagdan: %v", s)
	}
}

func TestRazvrstajPrazanIliObrnut(t *testing.T) {
	if s := Razvrstaj(kad(2026, 9, 11, 8, 0), kad(2026, 9, 11, 8, 0), Hrvatski{}); len(s) != 0 {
		t.Errorf("prazan: %v", s)
	}
	if s := Razvrstaj(kad(2026, 9, 11, 9, 0), kad(2026, 9, 11, 8, 0), Hrvatski{}); len(s) != 0 {
		t.Errorf("obrnut: %v", s)
	}
}

// Prijelaz na zimsko vrijeme: 25.10.2026. sat 02:00 dolazi dvaput. Zidni sat
// se ne mijenja — smjena 22–06 ostaje 8 zidnih sati, stvarnih devet.
// Obrazac računa po zidnom satu, i tako se isplaćuje; ovdje se to samo
// zapisuje da se ne bi "popravilo".
func TestRazvrstajPoZidnomSatu(t *testing.T) {
	s := Razvrstaj(kad(2026, 10, 24, 22, 0), kad(2026, 10, 25, 6, 0), Hrvatski{})
	if s.Ukupno() != 8*time.Hour {
		t.Errorf("preko prijelaza: %v (%v)", s, s.Ukupno())
	}
}
