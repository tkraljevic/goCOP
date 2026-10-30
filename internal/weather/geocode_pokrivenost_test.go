package weather

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Upit ide samo za Hrvatsku, s imenom programa, a prvo nađeno mjesto
// vraća se s koordinatama i punim nazivom.
func TestNadjiPrvoMjesto(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("q") != "Osijek" || q.Get("countrycodes") != "hr" || q.Get("limit") != "1" ||
			q.Get("format") != "jsonv2" || q.Get("accept-language") != "hr" {
			t.Errorf("upit %s", r.URL.RawQuery)
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "goCOP/") {
			t.Errorf("bez imena programa: %q", r.Header.Get("User-Agent"))
		}
		w.Write([]byte(`[{"lat":"45.5550","lon":"18.6955","display_name":"Osijek, Osječko-baranjska županija, Hrvatska"},
			{"lat":"1","lon":"2","display_name":"drugo"}]`))
	}))
	defer srv.Close()

	g := &Geokoder{BaseURL: srv.URL}
	tk, err := g.Nadji(context.Background(), "  Osijek  ")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Lat != 45.555 || tk.Lon != 18.6955 || !strings.HasPrefix(tk.Naziv, "Osijek,") {
		t.Errorf("nađeno %+v", tk)
	}
}

// Prazan upit ne ide na mrežu.
func TestNadjiPrazanUpit(t *testing.T) {
	g := &Geokoder{BaseURL: "http://127.0.0.1:1", HTTP: &http.Client{Transport: odbijaSve{t}}}
	if _, err := g.Nadji(context.Background(), "   "); err == nil || !strings.Contains(err.Error(), "upišite") {
		t.Fatalf("greška %v", err)
	}
}

// prekinutaVeza je prijenos koji ne dođe do poslužitelja
type prekinutaVeza struct{}

func (prekinutaVeza) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("veza prekinuta")
}

type odbijaSve struct{ t *testing.T }

func (o odbijaSve) RoundTrip(*http.Request) (*http.Response, error) {
	o.t.Error("prazan upit je otišao na mrežu")
	return nil, http.ErrNotSupported
}

// Greške usluge javljaju se riječima koje korisnik razumije.
func TestNadjiGreske(t *testing.T) {
	slucajevi := []struct {
		ime    string
		status int
		tijelo string
		zelim  string
	}{
		{"status", http.StatusTooManyRequests, `[]`, "OpenStreetMap je odgovorio 429"},
		{"nečitljivo", http.StatusOK, `<html>`, "nije čitljiv"},
		{"ništa nađeno", http.StatusOK, `[]`, "za „Nigdjevac” nije nađeno mjesto"},
	}
	for _, s := range slucajevi {
		t.Run(s.ime, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(s.status)
				w.Write([]byte(s.tijelo))
			}))
			defer srv.Close()
			_, err := (&Geokoder{BaseURL: srv.URL}).Nadji(context.Background(), "Nigdjevac")
			if err == nil || !strings.Contains(err.Error(), s.zelim) {
				t.Fatalf("greška %v, želim %q", err, s.zelim)
			}
		})
	}
}

// Kad se poslužitelj ne javi, poruka pita za internet.
func TestNadjiBezVeze(t *testing.T) {
	g := &Geokoder{BaseURL: "http://geokoder.test", HTTP: &http.Client{Transport: prekinutaVeza{}}}
	_, err := g.Nadji(context.Background(), "Osijek")
	if err == nil || !strings.Contains(err.Error(), "nema interneta") {
		t.Fatalf("greška %v", err)
	}
}

func TestMjestoIzNaziva(t *testing.T) {
	for ulaz, zelim := range map[string]string{
		"VGI Vuka, Osijek":      "Osijek",
		"Podcentar Osijek":      "Osijek",
		"  Vukovar  ":           "Vukovar",
		"Ured, Slavonski Brod ": "Slavonski Brod",
		"a, b, Đakovo":          "Đakovo",
		"":                      "",
		"   ":                   "",
		"kraj,":                 "",
	} {
		if got := MjestoIzNaziva(ulaz); got != zelim {
			t.Errorf("MjestoIzNaziva(%q) = %q, želim %q", ulaz, got, zelim)
		}
	}
}
