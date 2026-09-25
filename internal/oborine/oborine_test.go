package oborine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Preuzimanje za dvije točke: odgovor je lista, sati se upišu i čitaju, a
// ponovni upis prepisuje sat.
func TestPreuzmiISatne(t *testing.T) {
	sada := time.Now().UTC().Truncate(time.Hour)
	var sati []string
	var oborina, snijeg, temp []string
	for i := -3; i <= 2; i++ {
		ts := sada.Add(time.Duration(i) * time.Hour)
		sati = append(sati, `"`+ts.Format("2006-01-02T15:04")+`"`)
		oborina = append(oborina, fmt.Sprintf("%.1f", float64(i+4)))
		snijeg = append(snijeg, "0.7")
		temp = append(temp, "10.5")
	}
	// jedna vrijednost null: taj sat se ne upisuje
	oborina[1] = "null"
	tocka := func(lat, lon float64) string {
		return fmt.Sprintf(`{"latitude":%g,"longitude":%g,"hourly":{"time":[%s],"precipitation":[%s],"snowfall":[%s],"temperature_2m":[%s]}}`,
			lat, lon, strings.Join(sati, ","), strings.Join(oborina, ","), strings.Join(snijeg, ","), strings.Join(temp, ","))
	}
	var zahtjev string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zahtjev = r.URL.RawQuery
		fmt.Fprint(w, "["+tocka(46.8, 14.7)+","+tocka(45.5, 18.6)+"]")
	}))
	defer srv.Close()

	d, err := Otvori(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u := &Uvoznik{DB: d, Adresa: srv.URL, Tocke: func() ([]Tocka, error) {
		return []Tocka{{"k-a-gorje-1", 46.8, 14.7}, {"k-g-ravnica-1", 45.5, 18.6}}, nil
	}}
	n, err := u.Preuzmi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 10 {
		t.Errorf("upisano %d sati, želim 10 (6 sati × 2 točke, bez jednog null)", n)
	}
	for _, dio := range []string{"latitude=46.8000%2C45.5000", "past_days=7", "forecast_days=7", "timezone=UTC"} {
		if !strings.Contains(zahtjev, dio) {
			t.Errorf("zahtjev bez %q: %s", dio, zahtjev)
		}
	}
	s := sada.Unix() / 3600
	m, err := u.Satne(s-3, s+2)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 || len(m["k-a-gorje-1"]) != 5 {
		t.Fatalf("satne: %v", m)
	}
	if v := m["k-g-ravnica-1"][s]; v != 4 {
		t.Errorf("sat sada = %v, želim 4", v)
	}
	if _, ima := m["k-a-gorje-1"][s-2]; ima {
		t.Errorf("sat s null vrijednošću ne smije biti upisan")
	}
	var prognoza int
	var snijegMM float64
	if err := d.QueryRow(`SELECT prognoza, snijeg FROM satne WHERE kisomjer='k-a-gorje-1' AND sat=?`, s+2).Scan(&prognoza, &snijegMM); err != nil {
		t.Fatal(err)
	}
	if prognoza != 1 || snijegMM < 0.99 || snijegMM > 1.01 {
		t.Errorf("budući sat: prognoza=%d snijeg=%.2f mm (0,7 cm → 1 mm)", prognoza, snijegMM)
	}
	if u.Zadnje().IsZero() {
		t.Error("Zadnje je nula nakon preuzimanja")
	}

	// ponovni krug prepisuje: nova vrijednost za sat sada
	oborina[3] = "9.0"
	if _, err := u.Preuzmi(context.Background()); err != nil {
		t.Fatal(err)
	}
	m, _ = u.Satne(s, s)
	if v := m["k-a-gorje-1"][s]; v != 9 {
		t.Errorf("nakon ponovnog kruga sat sada = %v, želim 9", v)
	}
}
