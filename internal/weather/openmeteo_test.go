package weather

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSazetakDana(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("start_date") != "2023-08-21" {
			t.Errorf("krivi dan u upitu: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"hourly":{
			"time":["2023-08-21T00:00","2023-08-21T06:00","2023-08-21T12:00","2023-08-21T15:00","2023-08-21T18:00"],
			"temperature_2m":[20,22,33,34,30],
			"wind_speed_10m":[3,7.2,14.4,18,10.8],
			"wind_gusts_10m":[5,10,20,21.6,15],
			"surface_pressure":[1014,1015,1015,1014,1013],
			"precipitation":[0,0,0.4,1.2,0],
			"weather_code":[0,0,1,61,2]}}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Archive: srv.URL}
	d, err := c.Fetch(context.Background(), 45.55, 18.69, time.Date(2023, 8, 21, 0, 0, 0, 0, time.UTC), 12)
	if err != nil {
		t.Fatal(err)
	}
	if d.Temperature != 33 || d.Pressure != 1015 {
		t.Errorf("podne: %+v", d)
	}
	// 7,2 km/h = 2 m/s najmanje, udar 21,6 km/h = 6 m/s najviše, ponoć se ne broji
	if d.WindFrom != 2 || d.WindTo != 6 {
		t.Errorf("vjetar %v - %v", d.WindFrom, d.WindTo)
	}
	if d.Precipitation != 1.6 {
		t.Errorf("oborine %v", d.Precipitation)
	}
	if d.Description != "sunčano, kiša, toplo" {
		t.Errorf("opis %q", d.Description)
	}
}
