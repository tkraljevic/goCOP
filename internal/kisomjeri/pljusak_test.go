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

// redak je stvarni redak tablice pljusak.com (26. 9. 2026.)
const redakValpovo = `<tr>
<td><a class='graf' href="https://pljusak.com/hcharts_graf.php?stanica=valpovo"><img src="/img/graf.png" /></a></td>
<td><a href="https://pljusak.com/prognoza.php?stanica=valpovo" target="_blank"><img src="/img/pro_yr.png" /></a></td>
<td align="left"><h2><a href="meteo.php?stanica=valpovo" target="_blank">Valpovo</a></h2></td>
<td class="sat" align="center">85</td>
<td class="sat" align="center">17:38</td>
<td class="podaci" align="right">22.7</td>
<td><img src="/img/trendikone/trendgore1.png" alt="pljusak" /></td>
<td class="min_podaci" align="right">5.5<img src="img/prozgif.gif" width="2" height="1" alt="pljusak" /></td>
<td class="maks_podaci" align="right">23.1<img src="img/prozgif.gif" width="2" height="1" alt="pljusak" /></td>
<td class="podaci" align="right">W</td>
<td class="podaci" align="right">0.6<img src="img/prozgif.gif" width="2" height="1" alt="pljusak" /></td>
<td class="vjmaks_podaci" align="right">1.9<img src="img/prozgif.gif" width="4" height="1" alt="pljusak" /></td>
<td class="podaci" align="right">1017.0</td>
<td><img src="/img/trendikone/trend0.png" alt="pljusak" /></td>
<td class="podaci" align="center">47</td>
<td class="podaci" align="center">-</td>
<td class="OBO_1" align="right">1.2<img src="img/prozgif.gif" width="6" height="1"></td>
<td class="OBO_D" align="right">3.4<img src="img/prozgif.gif" width="6" height="1" alt="pljusak" /></td>
<td class="OBO_M" align="right">17.8<img src="img/prozgif.gif" width="6" height="1"></td>
</tr>`

// ugasena se zadnji put javila jučer navečer, pa je preskočena
const redakUgasena = `<tr><td><a href="meteo.php?stanica=ugasena">x</a></td><td></td><td>Ugašena</td><td>100</td>
<td>23:50</td><td>1</td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td>
<td class="OBO_1">0.5</td><td class="OBO_D">9.9</td><td class="OBO_M">20</td></tr>`

func TestPljusak(t *testing.T) {
	sada := time.Date(2026, 9, 26, 17, 45, 0, 0, Zagreb)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<table>" + redakValpovo + redakUgasena + "</table>"))
	}))
	defer srv.Close()
	p := &Pljusak{Adresa: srv.URL, Sada: func() time.Time { return sada }}
	postaje := []Postaja{{Code: "pljusak-valpovo", Izvor: "pljusak", IzvorSifra: "valpovo"},
		{Code: "pljusak-ugasena", Izvor: "pljusak", IzvorSifra: "ugasena"},
		{Code: "pljusak-nema", Izvor: "pljusak", IzvorSifra: "nema"}}
	m, err := p.Preuzmi(context.Background(), postaje)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 {
		t.Fatalf("mjerenja: %+v", m)
	}
	sat, dan := m[0], m[1]
	if sat.Sati != 1 || sat.Oborina != 1.2 || !sat.Kraj.Equal(time.Date(2026, 9, 26, 18, 0, 0, 0, Zagreb)) {
		t.Errorf("satno: %+v", sat)
	}
	if dan.Sati != 24 || dan.Oborina != 3.4 || !dan.Kraj.Equal(time.Date(2026, 9, 27, 0, 0, 0, 0, Zagreb)) {
		t.Errorf("dnevno: %+v", dan)
	}

	// spremište: ponovni upis istog razdoblja prepisuje vrijednost
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	s := &Spremiste{DB: baza}
	if err := s.Pripremi(); err != nil {
		t.Fatal(err)
	}
	u := &Uvoznik{Spremiste: s, Postaje: func() ([]Postaja, error) { return postaje, nil },
		Citaci: map[string]Citac{"pljusak": p}}
	if n, err := u.Preuzmi(context.Background()); err != nil || n != 2 {
		t.Fatalf("Preuzmi = %d, %v", n, err)
	}
	dan.Oborina = 5.0
	if _, err := s.Upisi(context.Background(), []Mjerenje{dan}); err != nil {
		t.Fatal(err)
	}
	sva, err := s.Od("pljusak-valpovo", sada.Add(-24*time.Hour))
	if err != nil || len(sva) != 2 || sva[1].Oborina != 5.0 {
		t.Errorf("Od = %+v, %v", sva, err)
	}
}

func TestUlozi(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	s := &Spremiste{DB: baza}
	if err := s.Pripremi(); err != nil {
		t.Fatal(err)
	}
	sada := time.Date(2026, 9, 27, 9, 0, 0, 0, Zagreb)
	jucer := time.Date(2026, 9, 27, 0, 0, 0, 0, Zagreb) // kraj 26. 9.
	danas := time.Date(2026, 9, 28, 0, 0, 0, 0, Zagreb) // kraj 27. 9., još traje
	_, err = s.Upisi(context.Background(), []Mjerenje{
		{Kisomjer: "p", Kraj: time.Date(2026, 9, 26, 18, 0, 0, 0, Zagreb), Sati: 1, Oborina: 1.2, Izvor: "kisomjer-pljusak"},
		{Kisomjer: "p", Kraj: jucer, Sati: 24, Oborina: 3.4, Izvor: "kisomjer-pljusak"},
		{Kisomjer: "p", Kraj: danas, Sati: 24, Oborina: 0.5, Izvor: "kisomjer-pljusak"},
	})
	if err != nil {
		t.Fatal(err)
	}
	koren := t.TempDir()
	prom, err := Ulozi(koren, s, []Postaja{{Code: "p"}}, sada.Add(-72*time.Hour), sada)
	if err != nil || len(prom) != 1 {
		t.Fatalf("Ulozi = %v, %v", prom, err)
	}
	dnevni, err := arhiva.PostojeciRedci(koren, SlivStabla, "p", "kisomjer-pljusak", "oborina", "dnevni")
	if err != nil || len(dnevni) != 1 || dnevni[0].Vrijednost != 3.4 || dnevni[0].Vrijeme.Format("2006-01-02") != "2026-09-26" {
		t.Errorf("dnevni = %+v, %v", dnevni, err)
	}
	satni, err := arhiva.PostojeciRedci(koren, SlivStabla, "p", "kisomjer-pljusak", "oborina", "satni")
	if err != nil || len(satni) != 1 || satni[0].Vrijednost != 1.2 {
		t.Errorf("satni = %+v, %v", satni, err)
	}
}
