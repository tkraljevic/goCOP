package oborine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// dan je dan od 1970. za datum
func dan(g int, m time.Month, d int) int64 {
	return time.Date(g, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400
}

// odgovorZa slaže izmišljeni GeoJSON odgovor: za svaki dan i čvor
// vrijednost iz v(dan, čvor), a nil je null.
func odgovorZa(od, do int64, cvorova int, v func(d int64, c int) *float64) string {
	var dani []string
	for d := od; d <= do; d++ {
		dani = append(dani, `"`+time.Unix(d*86400, 0).UTC().Format("2006-01-02T15:04")+`+00:00"`)
	}
	var f []string
	for c := 0; c < cvorova; c++ {
		var data []string
		for d := od; d <= do; d++ {
			if x := v(d, c); x != nil {
				data = append(data, fmt.Sprint(*x))
			} else {
				data = append(data, "null")
			}
		}
		f = append(f, `{"type":"Feature","geometry":{"type":"Point","coordinates":[13,47]},"properties":{"parameters":{"swe_tot":{"name":"total snow water equivalent","unit":"kg m-2","data":[`+strings.Join(data, ",")+`]}}}}`)
	}
	return `{"media_type":"application/json","type":"FeatureCollection","version":"v1","timestamps":[` + strings.Join(dani, ",") +
		`],"features":[` + strings.Join(f, ",") + `]}`
}

func broj(v float64) *float64 { return &v }

// Odgovor izvora: po danu srednjak čvorova s podatkom, a dan s manje od pet
// takvih čvorova izostaje (izmišljeni brojevi).
func TestVrijednostiTocke(t *testing.T) {
	od := dan(2025, 2, 1)
	tijelo := odgovorZa(od, od+2, 9, func(d int64, c int) *float64 {
		switch {
		case d == od && c < 6:
			return broj(float64(10 * (c + 1))) // 10…60, srednjak 35
		case d == od+1 && c < 4:
			return broj(100) // samo četiri čvora
		case d == od+2:
			return broj(0)
		}
		return nil
	})
	got, err := vrijednostiTocke([]byte(tijelo), 9)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[int64]float64{od: 35, od + 2: 0}; !reflect.DeepEqual(got, want) {
		t.Errorf("vrijednosti %v, želim %v", got, want)
	}
	for ime, tijelo := range map[string]string{
		"nije JSON":      "<html>",
		"krivi broj":     odgovorZa(od, od, 3, func(int64, int) *float64 { return nil }),
		"krivi datum":    strings.Replace(odgovorZa(od, od, 9, func(int64, int) *float64 { return nil }), "2025-02-01T00:00+00:00", "1. 2.", 1),
		"prazni čvorovi": `{"timestamps":[],"features":[]}`,
	} {
		t.Run(ime, func(t *testing.T) {
			if v, err := vrijednostiTocke([]byte(tijelo), 9); err == nil {
				t.Errorf("bez greške: %v", v)
			}
		})
	}
}

// Čvorovi oko točke: devet unutar obuhvata, a uz rub obuhvata samo oni
// koji su unutra, jer izvor odbije cijeli zahtjev za čvor izvan njega.
func TestCvoroviOko(t *testing.T) {
	if c := cvoroviOko(Tocka{Lat: 47, Lon: 13}); len(c) != 9 || c[0] != [2]float64{46.9, 12.86} || c[8] != [2]float64{47.1, 13.14} {
		t.Errorf("unutra: %v", c)
	}
	if c := cvoroviOko(Tocka{Lat: 46.2, Lon: 13}); len(c) != 6 {
		t.Errorf("uz južni rub: %d čvorova, želim 6", len(c))
	}
	if c := cvoroviOko(Tocka{Lat: 45.5, Lon: 18.6}); len(c) != 0 {
		t.Errorf("izvan izvora: %v", c)
	}
}

// Desetljeća od 1961., od najnovijeg; zadnje završava zadnjim danom izvora.
func TestRazdobljaDo(t *testing.T) {
	r := razdobljaDo(dan(1975, 6, 30))
	want := []razdobljeSnowgrida{{dan(1971, 1, 1), dan(1975, 6, 30)}, {dan(1961, 1, 1), dan(1970, 12, 31)}}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("razdoblja %v, želim %v", r, want)
	}
	if r := razdobljaDo(dan(1960, 1, 1)); len(r) != 0 {
		t.Errorf("prije izvora: %v", r)
	}
}

// laziIzvor je izmišljeni SNOWGRID: opis s krajem i dani s vrijednošću
// v(dan); pamti tražena razdoblja.
type laziIzvor struct {
	mu      sync.Mutex
	kraj    int64
	trazeno []string
	v       func(d int64) *float64
	status  []int // statusi za redom; prazno je 200
}

func (l *laziIzvor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if strings.HasSuffix(r.URL.Path, "/metadata") {
		fmt.Fprintf(w, `{"title":"SNOWGRID","end_time":%q}`, time.Unix(l.kraj*86400, 0).UTC().Format("2006-01-02T15:04")+"+00:00")
		return
	}
	if len(l.status) > 0 {
		s := l.status[0]
		l.status = l.status[1:]
		if s == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "7")
		}
		w.WriteHeader(s)
		fmt.Fprint(w, `{"detail":"stani"}`)
		return
	}
	q := r.URL.Query()
	l.trazeno = append(l.trazeno, q.Get("start")+"…"+q.Get("end"))
	od, err1 := time.Parse("2006-01-02", q.Get("start"))
	do, err2 := time.Parse("2006-01-02", q.Get("end"))
	if err := errors.Join(err1, err2); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fmt.Fprint(w, odgovorZa(od.Unix()/86400, do.Unix()/86400, len(q["lat_lon"]), func(d int64, _ int) *float64 { return l.v(d) }))
}

func probniSnijeg(t *testing.T, izvor *laziIzvor, tocke ...Tocka) (*SnijegNaTlu, *[]time.Duration) {
	t.Helper()
	srv := httptest.NewServer(izvor)
	t.Cleanup(srv.Close)
	d, err := Otvori(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	cekano := &[]time.Duration{}
	return &SnijegNaTlu{DB: d, Adresa: srv.URL + "/snowgrid", Tocke: func() ([]Tocka, error) { return tocke, nil },
		Cekaj: func(_ context.Context, d time.Duration) error { *cekano = append(*cekano, d); return nil }}, cekano
}

// Povijest se preuzme desetljeće po desetljeće za svaku točku, od
// najnovijeg, s predahom prije svakog zahtjeva; drugi put ne ide ništa, a
// kad izvor objavi nove dane, čita se samo od devet dana prije zadnjeg.
// Dan koji je izvor u međuvremenu ispraznio briše se.
func TestNadopuniPovijestPaNoviDani(t *testing.T) {
	kraj := dan(1975, 6, 30)
	izvor := &laziIzvor{kraj: kraj, v: func(d int64) *float64 { return broj(float64(d % 100)) }}
	s, cekano := probniSnijeg(t, izvor, Tocka{"k-a", 47, 13}, Tocka{"k-b", 47.2, 14.3})
	ctx := context.Background()
	n, err := s.Nadopuni(ctx)
	if err != nil || n != 4 {
		t.Fatalf("prvi put %d zahtjeva (%v), želim 4", n, err)
	}
	if want := []string{"1971-01-01…1975-06-30", "1971-01-01…1975-06-30", "1961-01-01…1970-12-31", "1961-01-01…1970-12-31"}; !reflect.DeepEqual(izvor.trazeno, want) {
		t.Errorf("traženo %v, želim %v", izvor.trazeno, want)
	}
	if len(*cekano) != 4 || (*cekano)[0] != predahSnowgrida {
		t.Errorf("predasi %v", *cekano)
	}
	if n, err := s.Nadopuni(ctx); n != 0 || err != nil {
		t.Errorf("drugi put %d zahtjeva (%v)", n, err)
	}

	izvor.mu.Lock()
	izvor.trazeno, izvor.kraj = nil, kraj+2
	izvor.v = func(d int64) *float64 {
		if d == kraj-1 {
			return nil // izvor je ispravio dan
		}
		return broj(5)
	}
	izvor.mu.Unlock()
	if n, err := s.Nadopuni(ctx); n != 2 || err != nil {
		t.Fatalf("novi dani: %d zahtjeva (%v), želim 2", n, err)
	}
	if want := "1975-06-21…1975-07-02"; izvor.trazeno[0] != want {
		t.Errorf("novi dani traženi %v, želim %s", izvor.trazeno, want)
	}
	nizovi, err := s.Nizovi([]string{"k-a", "k-x"}, kraj-10, kraj+5)
	if err != nil {
		t.Fatal(err)
	}
	a := nizovi["k-a"]
	if _, ima := a[kraj-1]; ima || a[kraj+2] != 5 || a[kraj-10] != float64((kraj-10)%100) || len(nizovi["k-x"]) != 0 {
		t.Errorf("niz k-a: %v; k-x: %v", a, nizovi["k-x"])
	}
	if ima, err := s.Preuzeta("k-a"); !ima || err != nil {
		t.Errorf("k-a preuzeta: %v %v", ima, err)
	}
	if ima, err := s.Preuzeta("k-x"); ima || err != nil {
		t.Errorf("k-x preuzeta: %v %v", ima, err)
	}
}

// Točka izvan izvora ne šalje zahtjev, ali se zapamti kao preuzeta.
func TestNadopuniTockaIzvanIzvora(t *testing.T) {
	izvor := &laziIzvor{kraj: dan(1962, 1, 5), v: func(int64) *float64 { return broj(1) }}
	s, _ := probniSnijeg(t, izvor, Tocka{"k-g", 45.5, 18.6})
	if n, err := s.Nadopuni(context.Background()); n != 1 || err != nil || len(izvor.trazeno) != 0 {
		t.Errorf("%d zahtjeva (%v), traženo %v", n, err, izvor.trazeno)
	}
	if ima, err := s.Preuzeta("k-g"); !ima || err != nil {
		t.Error("točka izvan izvora nije zapamćena")
	}
}

// HTTP 429: pričeka koliko izvor kaže pa sve dulje, i pokuša ponovno; druga
// greška ili previše pokušaja prekine preuzimanje.
func TestSnowgridPreviseZahtjeva(t *testing.T) {
	kraj := dan(1961, 1, 10)
	izvor := &laziIzvor{kraj: kraj, v: func(int64) *float64 { return broj(2) }, status: []int{429, 429}}
	s, cekano := probniSnijeg(t, izvor, Tocka{"k-a", 47, 13})
	if n, err := s.Nadopuni(context.Background()); n != 1 || err != nil {
		t.Fatalf("%d zahtjeva (%v)", n, err)
	}
	if want := []time.Duration{predahSnowgrida, 7 * time.Second, 7 * time.Second}; !reflect.DeepEqual(*cekano, want) {
		t.Errorf("čekano %v, želim %v", *cekano, want)
	}

	izvor.status = []int{429, 429, 429, 429}
	izvor.kraj = kraj + 30
	if _, err := s.Nadopuni(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTP 429") {
		t.Errorf("četiri puta 429: %v", err)
	}
	izvor.status = []int{http.StatusBadRequest}
	if _, err := s.Nadopuni(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("400: %v", err)
	}
}

// Greške: točke, opis izvora, mreža, baza i otkazano čekanje.
func TestNadopuniGreske(t *testing.T) {
	ctx := context.Background()
	izvor := &laziIzvor{kraj: dan(1961, 1, 10), v: func(int64) *float64 { return broj(2) }}
	s, _ := probniSnijeg(t, izvor, Tocka{"k-a", 47, 13})

	s.Tocke = func() ([]Tocka, error) { return nil, errors.New("registar") }
	if _, err := s.Nadopuni(ctx); err == nil {
		t.Error("registar bez greške")
	}
	s.Tocke = func() ([]Tocka, error) { return nil, nil }
	if n, err := s.Nadopuni(ctx); n != 0 || err != nil {
		t.Errorf("bez točaka: %d %v", n, err)
	}
	s.Tocke = func() ([]Tocka, error) { return []Tocka{{"k-a", 47, 13}}, nil }

	s.Cekaj = func(context.Context, time.Duration) error { return context.Canceled }
	if _, err := s.Nadopuni(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("otkazano: %v", err)
	}
	s.Cekaj = func(context.Context, time.Duration) error { return nil }

	adresa := s.Adresa
	s.Adresa = "http://127.0.0.1:1/nema"
	if _, err := s.Nadopuni(ctx); err == nil {
		t.Error("bez mreže bez greške")
	}
	s.Adresa = "://"
	if _, err := s.ZadnjiDanIzvora(ctx); err == nil {
		t.Error("kriva adresa bez greške")
	}
	s.Adresa = adresa

	opis := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("kraj") == "krivi" {
			fmt.Fprint(w, `{"end_time":"jučer"}`)
			return
		}
		fmt.Fprint(w, `nije JSON`)
	}))
	defer opis.Close()
	for _, a := range []string{opis.URL + "/x", opis.URL + "/x?kraj=krivi"} {
		u, err := url.Parse(a)
		if err != nil {
			t.Fatal(err)
		}
		s.Adresa = u.Scheme + "://" + u.Host + u.Path
		if u.RawQuery != "" {
			s.Adresa += "?" + u.RawQuery + "&"
		}
		if _, err := s.ZadnjiDanIzvora(ctx); err == nil {
			t.Errorf("%s: opis bez greške", a)
		}
	}
	s.Adresa = adresa

	if _, err := s.DB.Exec(`DROP TABLE snijeg_na_tlu_preuzeto`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Nadopuni(ctx); err == nil {
		t.Error("bez tablice bez greške")
	}
	if _, err := s.Preuzeta("k-a"); err == nil {
		t.Error("Preuzeta bez tablice bez greške")
	}
	if err := s.upisi(ctx, "k-a", razdobljeSnowgrida{1, 2}, 1, map[int64]float64{1: 1}); err == nil {
		t.Error("upis bez tablice bez greške")
	}
	if _, err := s.DB.Exec(`DROP TABLE snijeg_na_tlu`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Nizovi([]string{"k-a"}, 0, 1); err == nil {
		t.Error("Nizovi bez tablice bez greške")
	}
	if err := s.upisi(ctx, "k-a", razdobljeSnowgrida{1, 2}, 1, nil); err == nil {
		t.Error("brisanje bez tablice bez greške")
	}
	s.DB.Close()
	if err := s.upisi(ctx, "k-a", razdobljeSnowgrida{1, 2}, 1, nil); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Vrti: krug preuzme, javi i pozove NakonUpisa, pa čeka tri sata; nakon
// greške pola sata; otkazano čekanje ga zaustavi.
func TestVrtiSnijegNaTlu(t *testing.T) {
	izvor := &laziIzvor{kraj: dan(1961, 1, 10), v: func(int64) *float64 { return broj(2) }}
	s, _ := probniSnijeg(t, izvor, Tocka{"k-a", 47, 13})
	var dnevnik []string
	upisano := 0
	s.Javi = func(f string, a ...any) { dnevnik = append(dnevnik, fmt.Sprintf(f, a...)) }
	s.NakonUpisa = func() { upisano++ }
	var pauze []time.Duration
	s.Cekaj = func(_ context.Context, d time.Duration) error {
		if d == predahSnowgrida {
			return nil
		}
		pauze = append(pauze, d)
		if len(pauze) == 1 {
			s.Adresa = "http://127.0.0.1:1/nema"
			return nil
		}
		return context.Canceled
	}
	s.Vrti(context.Background())
	if !reflect.DeepEqual(pauze, []time.Duration{provjeraSnowgrida, ponovnoNakonGreske}) || upisano != 1 {
		t.Errorf("pauze %v, upisano %d", pauze, upisano)
	}
	if len(dnevnik) != 2 || dnevnik[0] != "snijeg na tlu (SNOWGRID): 1 zahtjev" || !strings.Contains(dnevnik[1], "snijeg na tlu (SNOWGRID):") {
		t.Errorf("dnevnik %q", dnevnik)
	}
}

// Pravo čekanje: vrati se nakon vremena, a otkazano odmah s greškom.
func TestCekajSnijeg(t *testing.T) {
	s := &SnijegNaTlu{}
	if err := s.cekaj(context.Background(), time.Millisecond); err != nil {
		t.Error(err)
	}
	ctx, otkazi := context.WithCancel(context.Background())
	otkazi()
	if err := s.cekaj(ctx, time.Hour); err == nil {
		t.Error("otkazano bez greške")
	}
	s.javi("bez dnevnika se ništa ne događa")
}

// Točke alpskih slivova: samo aktivne izvedene točke zadanih međuslivova.
func TestTockeAlpskihSlivova(t *testing.T) {
	d, err := Otvori(filepath.Join(t.TempDir(), "registar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec(`CREATE TABLE kisomjeri (code TEXT, latitude REAL, longitude REAL, sliv TEXT, aktivan INTEGER, vrsta TEXT);
		INSERT INTO kisomjeri VALUES ('k-a-1', 47, 13, 'A', 1, 'izvedeni'), ('k-a-2', 47, 13, 'A', 0, 'izvedeni'),
			('dhmz-a', 47, 13, 'A', 1, 'stvarni'), ('k-g-1', 45.5, 18.6, 'G', 1, 'izvedeni'), ('k-l-1', 48, 14, 'L', 1, 'izvedeni')`); err != nil {
		t.Fatal(err)
	}
	tocke, err := TockeAlpskihSlivova(d, []string{"A", "L"})()
	if err != nil {
		t.Fatal(err)
	}
	if want := []Tocka{{"k-a-1", 47, 13}, {"k-l-1", 48, 14}}; !reflect.DeepEqual(tocke, want) {
		t.Errorf("točke %v, želim %v", tocke, want)
	}
	if _, err := d.Exec(`UPDATE kisomjeri SET latitude = 'x' WHERE code = 'k-a-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := TockeAlpskihSlivova(d, []string{"A"})(); err == nil {
		t.Error("pokvaren redak bez greške")
	}
	d.Close()
	if _, err := TockeAlpskihSlivova(d, []string{"A"})(); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}
