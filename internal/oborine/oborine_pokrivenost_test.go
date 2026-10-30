package oborine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
)

// Iz registra idu samo aktivne izvedene točke, poredane po šifri; stvarni
// kišomjeri imaju svoja mjerenja.
func TestTockeIzRegistra(t *testing.T) {
	registar, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer registar.Close()
	if err := db.InitSchema(registar); err != nil {
		t.Fatal(err)
	}
	for _, k := range []struct {
		code, vrsta string
		aktivan     int
		lat, lon    float64
	}{
		{"k-g-ravnica-1", "izvedeni", 1, 45.5, 18.6},
		{"k-a-gorje-1", "izvedeni", 1, 46.8, 14.7},
		{"k-stari", "izvedeni", 0, 45, 17},
		{"k-osijek", "stvarni", 1, 45.55, 18.69},
	} {
		if _, err := registar.Exec(`INSERT INTO kisomjeri (code, naziv, latitude, longitude, aktivan, vrsta, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
			k.code, "točka "+k.code, k.lat, k.lon, k.aktivan, k.vrsta); err != nil {
			t.Fatal(err)
		}
	}
	tocke, err := TockeIzRegistra(registar)()
	if err != nil {
		t.Fatal(err)
	}
	zelim := []Tocka{{"k-a-gorje-1", 46.8, 14.7}, {"k-g-ravnica-1", 45.5, 18.6}}
	if len(tocke) != len(zelim) || tocke[0] != zelim[0] || tocke[1] != zelim[1] {
		t.Fatalf("točke %+v, želim %+v", tocke, zelim)
	}

	registar.Close()
	if _, err := TockeIzRegistra(registar)(); err == nil {
		t.Error("zatvoren registar bez greške")
	}
}

func probnaBaza(t *testing.T) *Uvoznik {
	t.Helper()
	d, err := Otvori(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &Uvoznik{DB: d, Tocke: func() ([]Tocka, error) { return []Tocka{{"k-a-1", 45, 18}}, nil }}
}

// Prazna baza nema zadnjeg preuzimanja, a bez točaka se ništa ne traži.
func TestBezTocakaIBezPreuzimanja(t *testing.T) {
	u := probnaBaza(t)
	if !u.Zadnje().IsZero() {
		t.Error("prazna baza ima zadnje preuzimanje")
	}
	u.Adresa = "http://127.0.0.1:1"
	u.Tocke = func() ([]Tocka, error) { return nil, nil }
	n, err := u.Preuzmi(context.Background())
	if n != 0 || err != nil {
		t.Fatalf("bez točaka: %d %v", n, err)
	}
	u.Tocke = func() ([]Tocka, error) { return nil, os.ErrPermission }
	if _, err := u.Preuzmi(context.Background()); err != os.ErrPermission {
		t.Fatalf("greška točaka %v", err)
	}
}

// Jedna točka: Open-Meteo vraća objekt, ne listu. Stari sati nisu prognoza.
// Sat bez oborine se preskače i kad ima snijeg i temperaturu; kraći nizovi
// snijega i temperature ne smetaju.
func TestPreuzmiJednuTocku(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "goCOP oborine" || r.URL.Query().Get("hourly") != "precipitation,snowfall,temperature_2m" {
			t.Errorf("upit %s %v", r.URL.RawQuery, r.Header)
		}
		w.Write([]byte(`{"latitude":45,"longitude":18,"hourly":{
			"time":["2020-03-01T00:00","2020-03-01T01:00","loše","2020-03-01T03:00"],
			"precipitation":[1.5,null,2,0.25],
			"snowfall":[1.4],
			"temperature_2m":[-1.5,2]}}`))
	}))
	defer srv.Close()
	u := probnaBaza(t)
	u.Adresa = srv.URL
	n, err := u.Preuzmi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("upisano %d, želim 2", n)
	}
	sat := time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC).Unix() / 3600
	var oborina, snijeg, temp float64
	var prognoza int
	if err := u.DB.QueryRow(`SELECT oborina, snijeg, temperatura, prognoza FROM satne WHERE kisomjer='k-a-1' AND sat=?`, sat).
		Scan(&oborina, &snijeg, &temp, &prognoza); err != nil {
		t.Fatal(err)
	}
	if oborina != 1.5 || snijeg < 1.99 || snijeg > 2.01 || temp != -1.5 || prognoza != 0 {
		t.Errorf("prvi sat: %v %v %v %d", oborina, snijeg, temp, prognoza)
	}
	m, err := u.Satne(sat, sat+3)
	if err != nil {
		t.Fatal(err)
	}
	if len(m["k-a-1"]) != 2 || m["k-a-1"][sat+3] != 0.25 {
		t.Errorf("satne %v", m)
	}
	if err := u.DB.QueryRow(`SELECT snijeg, temperatura FROM satne WHERE sat=?`, sat+3).Scan(&snijeg, &temp); err != nil {
		t.Fatal(err)
	}
	if snijeg != 0 || temp != 0 {
		t.Errorf("sat bez snijega i temperature: %v %v", snijeg, temp)
	}
	if u.Zadnje().IsZero() {
		t.Error("nema zadnjeg preuzimanja")
	}
}

// Greške usluge: status uz skraćeno tijelo, nerazumljiv odgovor i krivi broj
// točaka; ništa se ne upisuje.
func TestPreuzmiGreske(t *testing.T) {
	dugo := strings.Repeat("x", 300)
	slucajevi := []struct {
		ime    string
		status int
		tijelo string
		zelim  string
	}{
		{"status", http.StatusTooManyRequests, "  " + dugo + "  ", "open-meteo: HTTP 429: " + strings.Repeat("x", 200) + "…"},
		{"kratki status", http.StatusBadRequest, ` {"reason":"loše"} `, `open-meteo: HTTP 400: {"reason":"loše"}`},
		{"nerazumljivo", http.StatusOK, `<html>`, "nerazumljiv odgovor"},
		{"broj točaka", http.StatusOK, `[{"hourly":{}},{"hourly":{}}]`, "traženo 1 točaka, vraćeno 2"},
	}
	for _, s := range slucajevi {
		t.Run(s.ime, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(s.status)
				w.Write([]byte(s.tijelo))
			}))
			defer srv.Close()
			u := probnaBaza(t)
			u.Adresa = srv.URL
			_, err := u.Preuzmi(context.Background())
			if err == nil || !strings.Contains(err.Error(), s.zelim) {
				t.Fatalf("greška %v, želim %q", err, s.zelim)
			}
			if !u.Zadnje().IsZero() {
				t.Error("upisano uz grešku")
			}
		})
	}
}

func TestPreuzmiBezVeze(t *testing.T) {
	u := probnaBaza(t)
	u.Adresa = "http://oborine.test"
	u.Klijent = &http.Client{Transport: prekinutaVeza{}}
	if _, err := u.Preuzmi(context.Background()); err == nil {
		t.Fatal("bez poslužitelja nema greške")
	}
	u.Adresa = "http://[::1"
	if _, err := u.Preuzmi(context.Background()); err == nil {
		t.Fatal("neispravna adresa prošla")
	}
}

// Otvori ne prolazi kad baza nije baza.
func TestOtvoriNijeBaza(t *testing.T) {
	put := filepath.Join(t.TempDir(), "oborine.db")
	if err := os.WriteFile(put, []byte(strings.Repeat("ovo nije sqlite ", 512)), 0o600); err != nil {
		t.Fatal(err)
	}
	if d, err := Otvori(put); err == nil {
		d.Close()
		t.Fatal("datoteka koja nije baza otvorena")
	}
}

// Zatvorena baza: Satne javlja grešku, a Zadnje je nula.
func TestZatvorenaBaza(t *testing.T) {
	u := probnaBaza(t)
	u.DB.Close()
	if _, err := u.Satne(0, 1); err == nil {
		t.Error("Satne bez greške")
	}
	if !u.Zadnje().IsZero() {
		t.Error("Zadnje nije nula")
	}
}

// prekinutaVeza je prijenos koji ne dođe do poslužitelja
type prekinutaVeza struct{}

func (prekinutaVeza) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("veza prekinuta")
}
