package web

import (
	"bytes"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	webassets "gocop/web"
)

// lozinkeZaTest su privremene lozinke dvaju računa iz imenika
var lozinkeZaTest = []db.PocetnaLozinka{
	{Korisnik: "pperic", Ime: "Pero Perić", Lozinka: "k7mx-2pqa-hr4t-wz9c"},
	{Korisnik: "aanic", Ime: "Ana Anić", Lozinka: "m3nb-8qrs-tu2v-wx4y"},
}

// okolinaLozinki je čvor napunjen iz imenika: popis lozinki čeka preuzimanje
func okolinaLozinki(t *testing.T) (*PostavljanjeHandler, http.Handler) {
	t.Helper()
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tp, err := template.New("pocetne_lozinke.html").Funcs(templateFuncs()).ParseFS(templatesFS, "pocetne_lozinke.html")
	if err != nil {
		t.Fatal(err)
	}
	h := &PostavljanjeHandler{kod: "7KQ4-M2XD", tmplLozinke: tp}
	h.SetPocetneLozinke(append([]db.PocetnaLozinka(nil), lozinkeZaTest...), time.Time{}, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /postavljanje/lozinke", h.PrikaziLozinke)
	mux.HandleFunc("POST /postavljanje/lozinke", h.PreuzmiLozinke)
	return h, (&Server{}).klijentSloj(mux)
}

func zahtjevLozinki(srv http.Handler, porijeklo func(*http.Request) *http.Request, metoda, kod string) *httptest.ResponseRecorder {
	var r *http.Request
	if metoda == http.MethodPost {
		r = httptest.NewRequest(metoda, "/postavljanje/lozinke", strings.NewReader(url.Values{"kod": {kod}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(metoda, "/postavljanje/lozinke?"+url.Values{"kod": {kod}}.Encode(), nil)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, porijeklo(r))
	return w
}

// naOvomRacunalu je preglednik na računalu čvora: loopback, ime localhost
func naOvomRacunalu(r *http.Request) *http.Request {
	r.RemoteAddr = "127.0.0.1:40000"
	r.Host = "localhost:1160"
	return r
}

// imaLozinku javlja nosi li odgovor ijednu lozinku s popisa
func imaLozinku(w *httptest.ResponseRecorder) bool {
	for _, p := range lozinkeZaTest {
		if strings.Contains(w.Body.String(), p.Lozinka) {
			return true
		}
	}
	return false
}

// S ovog računala popis se pokaže jednom; drugi put ga više nema
func TestPopisLozinkiJednomSOvogRacunala(t *testing.T) {
	h, srv := okolinaLozinki(t)

	w := zahtjevLozinki(srv, naOvomRacunalu, http.MethodGet, "")
	if w.Code != http.StatusOK || imaLozinku(w) || !strings.Contains(w.Body.String(), "Pokaži popis lozinki") {
		t.Fatalf("stranica prije preuzimanja: %d %q", w.Code, w.Body.String())
	}

	w = zahtjevLozinki(srv, naOvomRacunalu, http.MethodPost, "")
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("preuzimanje: %d, Cache-Control %q", w.Code, w.Header().Get("Cache-Control"))
	}
	for _, p := range lozinkeZaTest {
		if !strings.Contains(w.Body.String(), p.Lozinka) || !strings.Contains(w.Body.String(), p.Korisnik) {
			t.Errorf("na popisu nema računa %s s lozinkom", p.Korisnik)
		}
	}
	if h.CekaLozinki() != 0 {
		t.Error("popis je i nakon preuzimanja u memoriji")
	}

	w = zahtjevLozinki(srv, naOvomRacunalu, http.MethodPost, "")
	if w.Code != http.StatusOK || imaLozinku(w) || !strings.Contains(w.Body.String(), "više nema") {
		t.Errorf("drugo preuzimanje: %d %q", w.Code, w.Body.String())
	}
}

// Izvana stranice nema, a iz lokalne mreže treba kod iz dnevnika
func TestPopisLozinkiNeIzvanaNiBezKoda(t *testing.T) {
	h, srv := okolinaLozinki(t)

	for ime, porijeklo := range map[string]func(*http.Request) *http.Request{
		"kroz tunel": krozTunel("198.51.100.7"), "s javne adrese": javnoIzravno,
	} {
		for _, metoda := range []string{http.MethodGet, http.MethodPost} {
			w := zahtjevLozinki(srv, porijeklo, metoda, "7KQ4-M2XD")
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" || imaLozinku(w) {
				t.Errorf("%s %s: %d %q", ime, metoda, w.Code, w.Header().Get("Location"))
			}
		}
	}

	w := zahtjevLozinki(srv, izravno, http.MethodGet, "")
	if w.Code != http.StatusOK || imaLozinku(w) || !strings.Contains(w.Body.String(), "Kod iz dnevnika") {
		t.Errorf("iz lokalne mreže bez koda mora tražiti kod: %d %q", w.Code, w.Body.String())
	}
	w = zahtjevLozinki(srv, izravno, http.MethodGet, "AAAA-BBBB")
	if w.Code != http.StatusForbidden || imaLozinku(w) {
		t.Errorf("krivi kod na stranici: %d", w.Code)
	}
	for _, kod := range []string{"", "AAAA-BBBB"} {
		if w := zahtjevLozinki(srv, izravno, http.MethodPost, kod); w.Code != http.StatusForbidden || imaLozinku(w) {
			t.Errorf("preuzimanje iz lokalne mreže s kodom %q: %d", kod, w.Code)
		}
	}
	if h.CekaLozinki() != len(lozinkeZaTest) {
		t.Fatal("odbijeni zahtjevi ne smiju potrošiti popis")
	}

	// s točnim kodom iz lokalne mreže popis se preuzima
	if w := zahtjevLozinki(srv, izravno, http.MethodPost, "7kq4-m2xd"); w.Code != http.StatusOK || !imaLozinku(w) {
		t.Errorf("iz lokalne mreže s kodom: %d %q", w.Code, w.Body.String())
	}
}

// Dnevnik javlja gdje je popis, a same lozinke u njega ne idu
func TestJaviPostavljanjeNePiseLozinke(t *testing.T) {
	h, _ := okolinaLozinki(t)
	var dnevnik bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(stari) })

	s := &Server{postavljanje: h}
	s.JaviPostavljanje(":8080")
	zapis := dnevnik.String()
	if !strings.Contains(zapis, "2 računa") || !strings.Contains(zapis, "localhost:8080/postavljanje/lozinke") ||
		!strings.Contains(zapis, "?kod=7KQ4-M2XD") {
		t.Errorf("dnevnik ne kaže gdje je popis: %q", zapis)
	}
	for _, p := range lozinkeZaTest {
		if strings.Contains(zapis, p.Lozinka) {
			t.Errorf("lozinka računa %s je u dnevniku", p.Korisnik)
		}
	}

	// preuzet popis se više ne najavljuje
	s.SetPocetneLozinke(nil, time.Time{}, nil)
	dnevnik.Reset()
	s.JaviPostavljanje(":8080")
	if strings.Contains(dnevnik.String(), "/postavljanje/lozinke") {
		t.Errorf("preuzet popis se i dalje najavljuje: %q", dnevnik.String())
	}
	// bez stranice Postavljanje nema ni najave
	(&Server{}).JaviPostavljanje(":8080")
	(&Server{}).SetPocetneLozinke(lozinkeZaTest, time.Time{}, nil)
}

// Zahtjev s ovog računala upućen na drugo ime iz lokalne mreže (ne
// localhost, 127.0.0.1 ni [::1]) bez koda ne dobiva popis; s kodom ga dobiva
func TestPopisLozinkiNaDrugoImeTraziKod(t *testing.T) {
	h, srv := okolinaLozinki(t)
	naDrugoIme := func(r *http.Request) *http.Request {
		r.RemoteAddr = "127.0.0.1:40000"
		r.Host = "tower.lan:1160"
		return r
	}
	if w := zahtjevLozinki(srv, naDrugoIme, http.MethodGet, ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Kod iz dnevnika") {
		t.Errorf("stranica na drugo ime mora tražiti kod: %d", w.Code)
	}
	if w := zahtjevLozinki(srv, naDrugoIme, http.MethodPost, ""); w.Code != http.StatusForbidden || imaLozinku(w) {
		t.Errorf("preuzimanje na drugo ime bez koda: %d", w.Code)
	}
	if h.CekaLozinki() != len(lozinkeZaTest) {
		t.Fatal("odbijen zahtjev ne smije potrošiti popis")
	}
	if w := zahtjevLozinki(srv, naDrugoIme, http.MethodPost, "7KQ4-M2XD"); w.Code != http.StatusOK || !imaLozinku(w) {
		t.Errorf("na drugo ime s kodom: %d", w.Code)
	}
}

// Kroz tunel ni točan kod ne vrijedi, ni za popis ni za Postavljanje
func TestKodNeVrijediKrozTunel(t *testing.T) {
	h, _ := okolinaLozinki(t)
	r := krozTunel("198.51.100.7")(httptest.NewRequest(http.MethodGet, "/postavljanje/lozinke", nil))
	if h.dopustenoLozinke(r, "7KQ4-M2XD") || h.dopusteno(r, "7KQ4-M2XD") {
		t.Error("kroz tunel kod ne smije vrijediti")
	}
}

func TestHostOvogRacunala(t *testing.T) {
	for host, ocekivano := range map[string]bool{
		"localhost": true, "localhost:1160": true, "LOCALHOST.": true, "gocop.localhost:8080": true,
		"127.0.0.1": true, "127.0.0.1:1160": true, "[::1]:1160": true, "::1": true,
		"192.168.1.10:1160": false, "cvor.primjer.hr": false, "localhost.primjer.hr": false, "": false,
	} {
		if hostOvogRacunala(host) != ocekivano {
			t.Errorf("hostOvogRacunala(%q) = %v", host, !ocekivano)
		}
	}
}

// Preuzimanje se zapiše prije nego što popis ode u preglednik; kad se ne da
// zapisati, popis ostaje
func TestPopisLozinkiZapisPreuzimanja(t *testing.T) {
	h, srv := okolinaLozinki(t)
	zapisa := 0
	greska := errors.New("baza je zaključana")
	h.preuzete = func() error { zapisa++; return greska }

	if w := zahtjevLozinki(srv, naOvomRacunalu, http.MethodPost, ""); w.Code != http.StatusInternalServerError || imaLozinku(w) {
		t.Errorf("nezapisano preuzimanje: %d", w.Code)
	}
	if h.CekaLozinki() != len(lozinkeZaTest) {
		t.Fatal("nezapisano preuzimanje potrošilo je popis")
	}
	greska = nil
	if w := zahtjevLozinki(srv, naOvomRacunalu, http.MethodPost, ""); w.Code != http.StatusOK || !imaLozinku(w) {
		t.Errorf("preuzimanje: %d", w.Code)
	}
	if zapisa != 2 || h.CekaLozinki() != 0 {
		t.Errorf("zapisa %d, na čekanju %d", zapisa, h.CekaLozinki())
	}
	// prazan popis nema što zapisati
	if w := zahtjevLozinki(srv, naOvomRacunalu, http.MethodPost, ""); w.Code != http.StatusOK || zapisa != 2 {
		t.Errorf("prazan popis: %d, zapisa %d", w.Code, zapisa)
	}
}

// Popis ostaje kad se stranica ne da složiti
func TestPopisLozinkiOstajeKadStranicaNeUspije(t *testing.T) {
	h, srv := okolinaLozinki(t)
	h.tmplLozinke = template.Must(template.New("pocetne_lozinke.html").Parse(`{{if .Popis}}{{.Nepostojece}}{{end}}`))
	zapisano := false
	h.preuzete = func() error { zapisano = true; return nil }
	if w := zahtjevLozinki(srv, naOvomRacunalu, http.MethodPost, ""); w.Code != http.StatusInternalServerError || imaLozinku(w) {
		t.Errorf("stranica koja se ne da složiti: %d", w.Code)
	}
	if h.CekaLozinki() != len(lozinkeZaTest) || zapisano {
		t.Errorf("popis potrošen (na čekanju %d, zapisano %v)", h.CekaLozinki(), zapisano)
	}
}

// zatvorenaVeza je preglednik koji je zatvorio vezu
type zatvorenaVeza struct {
	*httptest.ResponseRecorder
	pisanje, slanje error
}

func (v zatvorenaVeza) Write(b []byte) (int, error) {
	if v.pisanje != nil {
		return 0, v.pisanje
	}
	return v.ResponseRecorder.Write(b)
}

func (v zatvorenaVeza) FlushError() error { return v.slanje }

// Popis koji nije stigao do preglednika ostaje za sljedeće preuzimanje
func TestPopisLozinkiOstajeKadVezaPukne(t *testing.T) {
	prekinuta := errors.New("veza je prekinuta")
	for ime, veza := range map[string]zatvorenaVeza{
		"pisanje": {pisanje: prekinuta}, "slanje": {slanje: prekinuta},
	} {
		h, srv := okolinaLozinki(t)
		veza.ResponseRecorder = httptest.NewRecorder()
		r := naOvomRacunalu(httptest.NewRequest(http.MethodPost, "/postavljanje/lozinke", strings.NewReader("kod=")))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		srv.ServeHTTP(veza, r)
		if h.CekaLozinki() != len(lozinkeZaTest) {
			t.Errorf("%s: popis je potrošen", ime)
		}
	}
	if err := posalji(httptest.NewRecorder(), []byte("x")); err != nil {
		t.Errorf("ispravna veza: %v", err)
	}
	if err := posalji(struct{ http.ResponseWriter }{httptest.NewRecorder()}, []byte("x")); err != nil {
		t.Errorf("veza bez slanja: %v", err)
	}
}
