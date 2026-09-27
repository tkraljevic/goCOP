package kisomjeri

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/db"
)

// isječak stvarne tablice letva.voda.hr (grupiranje 0), s praznim satom 23
const tablicaLetve = `<table><tr class="trZaglavljeKolone"><td>Datum i sat</td><td class="tdPostaja" colspan="2">Varaždin</td></tr>
<tr><td class="tdDatum">26.09.2026. 06</td><td class="tdPostaja">0,40</td><td>12,20</td></tr>
<tr><td class="tdDatum">25.09.2026. 23</td><td class="tdPostaja"></td><td></td></tr>
<tr><td class="tdDatum">25.09.2026. 22</td><td class="tdPostaja">1,10</td><td>11,80</td></tr></table>`

func TestLetva(t *testing.T) {
	var forma string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		forma = r.PostForm.Encode()
		w.Write([]byte(tablicaLetve))
	}))
	defer srv.Close()
	l := &Letva{Adresa: srv.URL, HTTP: srv.Client(), Racun: func() (string, string, bool) { return "k", "l", true },
		Sada: func() time.Time { return time.Date(2026, 9, 26, 7, 0, 0, 0, Zagreb) }}
	m, err := l.Preuzmi(context.Background(), []Postaja{{Code: "dhmz-varazdin", IzvorSifra: "239", Korak: "satni"}})
	if err != nil {
		t.Fatal(err)
	}
	if forma != "datumDo=26.09.2026&datumOd=24.09.2026&grupiranje=0&postaje=239" {
		t.Errorf("forma %q", forma)
	}
	if len(m) != 2 || m[0].Oborina != 0.4 || !m[0].Kraj.Equal(time.Date(2026, 9, 26, 6, 0, 0, 0, Zagreb)) ||
		m[1].Oborina != 1.1 || m[0].Sati != 1 || m[0].Izvor != "kisomjer-dhmz" {
		t.Errorf("mjerenja %+v", m)
	}
	if _, err := (&Letva{Adresa: srv.URL, Racun: func() (string, string, bool) { return "", "", false }}).Preuzmi(context.Background(), nil); err == nil {
		t.Error("bez računa mora javiti grešku")
	}
}

// 12-satni zbrojevi Osijeka: dan D = oznake D 06 i D 18
func TestUlozi12Satni(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	s := &Spremiste{DB: baza}
	if err := s.Pripremi(); err != nil {
		t.Fatal(err)
	}
	z := func(d, h int, v float64) Mjerenje {
		return Mjerenje{Kisomjer: "dhmz-osijek", Kraj: time.Date(2026, 9, d, h, 0, 0, 0, Zagreb).UTC(), Sati: 12, Oborina: v, Izvor: "kisomjer-dhmz"}
	}
	if _, err := s.Upisi(context.Background(), []Mjerenje{z(25, 6, 1.5), z(25, 18, 2.0), z(26, 6, 0.3)}); err != nil {
		t.Fatal(err)
	}
	koren := t.TempDir()
	sada := time.Date(2026, 9, 26, 9, 0, 0, 0, Zagreb)
	if _, err := Ulozi(koren, s, []Postaja{{Code: "dhmz-osijek"}}, sada.Add(-72*time.Hour), sada); err != nil {
		t.Fatal(err)
	}
	r, err := arhiva.PostojeciRedci(koren, SlivStabla, "dhmz-osijek", "kisomjer-dhmz", "oborina", "dnevni")
	if err != nil || len(r) != 1 || r[0].Vrijednost != 3.5 || r[0].Vrijeme.Format("2006-01-02") != "2026-09-25" {
		t.Errorf("dnevni = %+v, %v", r, err)
	}
}
