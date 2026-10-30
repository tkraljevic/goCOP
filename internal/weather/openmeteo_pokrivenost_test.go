package weather

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Dan u budućnosti ide na prognozu, a ne na arhivu; bez zadanog sata
// temperatura i tlak su iz podneva.
func TestFetchNoviDanIdeNaPrognozu(t *testing.T) {
	arhiva := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("novi dan ne smije ići na arhivu")
	}))
	defer arhiva.Close()
	prognoza := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("latitude") != "45.5000" || q.Get("longitude") != "18.7000" || q.Get("timezone") != "Europe/Zagreb" ||
			q.Get("start_date") != "2099-01-15" || q.Get("end_date") != "2099-01-15" {
			t.Errorf("upit %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"hourly":{
			"time":["2099-01-15T06:00","2099-01-15T12:00","2099-01-15T18:00"],
			"temperature_2m":[-2,3,1],
			"wind_speed_10m":[36,36,36],
			"surface_pressure":[1020,1021,1022],
			"precipitation":[0.25,0.25,0.25],
			"weather_code":[71,73,3]}}`))
	}))
	defer prognoza.Close()

	c := &Client{BaseURL: prognoza.URL, Archive: arhiva.URL}
	d, err := c.Fetch(context.Background(), 45.5, 18.7, time.Date(2099, 1, 15, 0, 0, 0, 0, time.UTC), 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Temperature != 3 || d.Pressure != 1021 {
		t.Errorf("podne: %+v", d)
	}
	// 36 km/h = 10 m/s, bez udara
	if d.WindFrom != 10 || d.WindTo != 10 {
		t.Errorf("vjetar %v - %v", d.WindFrom, d.WindTo)
	}
	// 0,75 se zaokružuje na jednu decimalu
	if d.Precipitation != 0.8 {
		t.Errorf("oborine %v", d.Precipitation)
	}
	if d.Description != "snijeg, hladno, vjetrovito" || d.Source != "OPEN_METEO" {
		t.Errorf("opis %q, izvor %q", d.Description, d.Source)
	}
}

// Dan stariji od 85 dana ide na arhivu, a ne na prognozu; traženi sat
// daje temperaturu i tlak.
func TestFetchStariDanIdeNaArhivu(t *testing.T) {
	prognoza := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("stari dan ne smije ići na prognozu")
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer prognoza.Close()
	arhiva := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("start_date") != "2023-08-21" || q.Get("end_date") != "2023-08-21" {
			t.Errorf("upit %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"hourly":{
			"time":["2023-08-21T08:00","2023-08-21T12:00"],
			"temperature_2m":[24,31],
			"surface_pressure":[1012,1010],
			"weather_code":[0,0]}}`))
	}))
	defer arhiva.Close()

	c := &Client{BaseURL: prognoza.URL, Archive: arhiva.URL}
	d, err := c.Fetch(context.Background(), 45.5, 18.7, time.Date(2023, 8, 21, 0, 0, 0, 0, time.UTC), 8)
	if err != nil {
		t.Fatal(err)
	}
	if d.Temperature != 24 || d.Pressure != 1012 {
		t.Errorf("osam sati: %+v", d)
	}
}

// Usluga koja odbije upit javlja svoj razlog; nečitljiv odgovor je greška.
func TestFetchGreskeUsluge(t *testing.T) {
	slucajevi := []struct {
		ime    string
		status int
		tijelo string
		zelim  string
	}{
		{"razlog", http.StatusBadRequest, `{"error":true,"reason":" Latitude must be in range "}`, "vremenska usluga: Latitude must be in range"},
		{"nečitljivo", http.StatusBadGateway, `<html>`, "neočekivan odgovor"},
		{"bez sati", http.StatusOK, `{"hourly":{"time":[]}}`, "nema podataka za taj dan"},
		{"nepotpuno", http.StatusOK, `{"hourly":{"time":["2023-08-21T12:00"],"temperature_2m":[]}}`, "nema podataka za taj dan"},
	}
	for _, s := range slucajevi {
		t.Run(s.ime, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(s.status)
				w.Write([]byte(s.tijelo))
			}))
			defer srv.Close()
			c := &Client{BaseURL: srv.URL, Archive: srv.URL}
			_, err := c.Fetch(context.Background(), 45, 18, time.Date(2023, 8, 21, 0, 0, 0, 0, time.UTC), 12)
			if err == nil || !strings.Contains(err.Error(), s.zelim) {
				t.Fatalf("greška %v, želim %q", err, s.zelim)
			}
		})
	}
}

func TestFetchBezVeze(t *testing.T) {
	c := &Client{BaseURL: "http://prognoza.test", Archive: "http://arhiva.test", HTTP: &http.Client{Transport: prekinutaVeza{}}}
	_, err := c.Fetch(context.Background(), 45, 18, time.Date(2023, 8, 21, 0, 0, 0, 0, time.UTC), 12)
	if err == nil || !strings.Contains(err.Error(), "nije dostupna") {
		t.Fatalf("greška %v", err)
	}
}

// Kad traženog sata nema, uzima se sredina dana; bez vjetra u radnom danu
// vjetar je nula, a ne beskonačno; nepotpuni nizovi ne ruše sažetak.
func TestSazetakBezTrazenogSata(t *testing.T) {
	var r response
	r.Hourly.Time = []string{"2023-08-21T00:00", "2023-08-21T01:00", "2023-08-21T02:00", "2023-08-21T23:00"}
	r.Hourly.Temperature = []float64{14, 15, 16, 17}
	r.Hourly.Pressure = []float64{1001, 1002}
	d, err := summarize(r, 12)
	if err != nil {
		t.Fatal(err)
	}
	// sredina od četiri sata je treći, a tlaka za njega nema
	if d.Temperature != 16 || !math.IsNaN(d.Pressure) {
		t.Errorf("sredina dana: %+v", d)
	}
	if d.WindFrom != 0 || d.WindTo != 0 || d.Precipitation != 0 {
		t.Errorf("bez vjetra i kiše: %+v", d)
	}
	if d.Description != "" {
		t.Errorf("bez oznaka vremena opis %q", d.Description)
	}
}

// Riječi obrasca: prevladava najčešće vrijeme, pri izjednačenju vedrije, a
// kiša, snijeg i grmljavina spominju se i kad nisu prevladali.
func TestDescribe(t *testing.T) {
	slucajevi := []struct {
		ime    string
		kodovi map[int]int
		temp   float64
		vjetar float64
		zelim  string
	}{
		{"vedro", map[int]int{0: 5, 1: 3}, 15, 2, "sunčano"},
		{"oblačno s kišom", map[int]int{3: 8, 61: 2}, 12, 2, "oblačno, kiša"},
		{"magla", map[int]int{45: 6, 48: 2, 2: 3}, 4, 1, "magla, hladno"},
		{"pljusak", map[int]int{80: 4, 1: 2}, 26, 9, "kiša, toplo, vjetrovito"},
		{"snijeg i grmljavina", map[int]int{85: 3, 95: 1, 3: 1}, 0, 0, "snijeg, grmljavina, hladno"},
		{"izjednačeno", map[int]int{0: 3, 61: 3}, 15, 0, "sunčano, kiša"},
		{"nepoznata oznaka je oblačno", map[int]int{20: 4}, math.NaN(), 0, "oblačno"},
		{"bez oznaka", map[int]int{}, 30, 8, "toplo, vjetrovito"},
		{"granice", map[int]int{51: 1, 67: 1, 71: 1, 77: 1, 86: 1}, 5, 8, "snijeg, kiša, hladno, vjetrovito"},
	}
	for _, s := range slucajevi {
		if got := describe(s.kodovi, s.temp, s.vjetar); got != s.zelim {
			t.Errorf("%s: %q, želim %q", s.ime, got, s.zelim)
		}
	}
}
