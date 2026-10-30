package weather

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func posluzi(t *testing.T, tijelo string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(tijelo)) // test ne provjerava slanje
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Vrijeme u krivom obliku, i ono kraće od pet znakova, javlja grešku, a list
// dnevnika se i dalje otvara.
func TestVrijemeUKrivomObliku(t *testing.T) {
	dan := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	for _, vrijeme := range []string{`"x"`, `""`, `"12:00"`, `"2026-10-30 12:00"`} {
		srv := posluzi(t, `{"hourly":{"time":[`+vrijeme+`],"temperature_2m":[1]}}`)
		c := &Client{BaseURL: srv.URL, Archive: srv.URL}
		d, err := c.Fetch(context.Background(), 45.55, 18.69, dan, 12)
		if err == nil || !strings.Contains(err.Error(), "neočekivanom obliku") {
			t.Errorf("vrijeme %s: %+v %v", vrijeme, d, err)
		}
	}
	if err := provjeriSate(nil, nil); err == nil || !strings.Contains(err.Error(), "nema podataka") {
		t.Errorf("bez sati: %v", err)
	}
	// ispravno vrijeme uz jedno krivo: krivo se preskoči
	srv := posluzi(t, `{"hourly":{"time":["x","2026-10-30T12:00"],"temperature_2m":[1,14]}}`)
	c := &Client{BaseURL: srv.URL, Archive: srv.URL}
	d, err := c.Fetch(context.Background(), 45.55, 18.69, dan, 12)
	if err != nil || d.Temperature != 14 {
		t.Errorf("ispravno vrijeme: %+v %v", d, err)
	}
}

// Odgovor veći od granice ne čita se do kraja.
func TestOdgovorDoGranice(t *testing.T) {
	brojevi := strings.Repeat("1,", najveciOdgovor)
	srv := posluzi(t, `{"hourly":{"time":["2026-10-30T12:00"],"temperature_2m":[`+brojevi+`1]}}`)
	c := &Client{BaseURL: srv.URL, Archive: srv.URL}
	if _, err := c.Fetch(context.Background(), 45.55, 18.69, time.Now(), 12); err == nil {
		t.Error("prevelik odgovor vremenske usluge je pročitan")
	}

	g := &Geokoder{BaseURL: posluzi(t, `[{"lat":"45.5","lon":"18.6","display_name":"`+strings.Repeat("a", najveciOdgovor)+`"}]`).URL}
	if _, err := g.Nadji(context.Background(), "Osijek"); err == nil {
		t.Error("prevelik odgovor OpenStreetMapa je pročitan")
	}
	g = &Geokoder{BaseURL: posluzi(t, `[{"lat":"45.5","lon":"18.6","display_name":"Osijek"}]`).URL}
	if m, err := g.Nadji(context.Background(), "Osijek"); err != nil || m.Naziv != "Osijek" || m.Lat != 45.5 {
		t.Errorf("ispravan odgovor OpenStreetMapa: %+v %v", m, err)
	}
}

func TestSatIzVremena(t *testing.T) {
	if h, ok := satIzVremena("2026-10-30T07:00"); !ok || h != 7 {
		t.Errorf("07:00: %d %v", h, ok)
	}
	if _, ok := satIzVremena("7"); ok {
		t.Error("kratko vrijeme je pročitano")
	}
}

// Koordinate s OpenStreetMapa upisuju se samo kad su brojevi na Zemlji.
func TestKoordinateNaZemlji(t *testing.T) {
	for _, c := range [][2]string{{"NaN", "18.6"}, {"45.5", "Inf"}, {"-Inf", "18.6"}, {"91", "18.6"},
		{"45.5", "-180.5"}, {"", "18.6"}, {"45,5", "18.6"}} {
		g := &Geokoder{BaseURL: posluzi(t, `[{"lat":"`+c[0]+`","lon":"`+c[1]+`","display_name":"Osijek"}]`).URL}
		if m, err := g.Nadji(context.Background(), "Osijek"); err == nil {
			t.Errorf("koordinate %q, %q upisane su: %+v", c[0], c[1], m)
		}
	}
	if lat, lon, ok := koordinate(" -90 ", "180"); !ok || lat != -90 || lon != 180 {
		t.Errorf("rubne koordinate: %v %v %v", lat, lon, ok)
	}
}
