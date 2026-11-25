package propisi

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"

	"gocop/internal/models"
)

var aktZaDohvat = models.IzdanjeNN{Godina: 2010, Broj: 84, Akt: 2389}

const stranicaZaDohvat = `<html><body><div class="article-column"><p class="T-9-8">Članak o čuvanju nasipa.</p></div></body></html>`

// posluziteljNN je lažni poslužitelj NN: ELI adresa preusmjerava (302,
// relativno) na zadani put, a stranice odgovaraju rukovateljem.
func posluziteljNN(t *testing.T, cilj string, stranica http.HandlerFunc) (*httptest.Server, *[]string) {
	t.Helper()
	var putevi []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		putevi = append(putevi, r.URL.Path+" "+r.Header.Get("User-Agent"))
		if strings.HasPrefix(r.URL.Path, "/eli/") && cilj != "" {
			http.Redirect(w, r, cilj, http.StatusFound)
			return
		}
		stranica(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &putevi
}

// dohvatZa je dohvat na lažni poslužitelj, bez razmaka između upita.
func dohvatZa(t *testing.T, srv *httptest.Server) *Dohvat {
	t.Helper()
	d := NoviDohvat(srv.Client())
	osnova, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	d.osnova = osnova
	d.klijent.Transport.(*razmakUpita).razmak = 0
	return d
}

func html200(tijelo string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, tijelo)
	}
}

// Odluke (25. 11. 2026.): ELI adresa akta preusmjerava (302) na stranicu
// /clanci/…; dohvat to slijedi, a oba zahtjeva idu kroz razmak upita i
// nose User-Agent programa.
func TestDohvatSlijediELIPreusmjeravanje(t *testing.T) {
	srv, putevi := posluziteljNN(t, "/clanci/sluzbeni/2010_07_84_2389.html", html200(stranicaZaDohvat))
	b, err := dohvatZa(t, srv).Akt(context.Background(), aktZaDohvat)
	if err != nil || !bytes.Contains(b, []byte("čuvanju nasipa")) {
		t.Fatalf("dohvat: %v, %q", err, b)
	}
	zeli := []string{"/eli/sluzbeni/2010/84/2389 " + korisnikDohvata, "/clanci/sluzbeni/2010_07_84_2389.html " + korisnikDohvata}
	if strings.Join(*putevi, "|") != strings.Join(zeli, "|") {
		t.Errorf("zahtjevi %q", *putevi)
	}
	// Bez preusmjeravanja vrijedi i sama ELI adresa.
	srv, _ = posluziteljNN(t, "", html200(stranicaZaDohvat))
	if _, err := dohvatZa(t, srv).Akt(context.Background(), aktZaDohvat); err != nil {
		t.Errorf("ELI bez preusmjeravanja: %v", err)
	}
}

// Odluke (25. 11. 2026.): za akt koji ne postoji NN preusmjeravaju na
// /stranica404.aspx, koja vraća 200 ili 404. To nije akt.
func TestDohvatStranica404JeNemaAkta(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound} {
		srv, _ := posluziteljNN(t, "/stranica404.aspx", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(status)
			fmt.Fprint(w, stranicaZaDohvat)
		})
		if _, err := dohvatZa(t, srv).Akt(context.Background(), aktZaDohvat); !errors.Is(err, ErrNemaAkta) {
			t.Errorf("status %d: greška %v, želi ErrNemaAkta", status, err)
		}
	}
}

// Odluke (25. 11. 2026.): konačna adresa mora biti stranica istog akta.
// Stranica drugog akta je „nema akta”, a stranica bez stupca akta „nema
// teksta akta”.
func TestDohvatKrivaKonacnaAdresa(t *testing.T) {
	for _, cilj := range []string{"/clanci/sluzbeni/2010_07_84_2390.html", "/clanci/sluzbeni/2011_07_84_2389.html", "/clanci/sluzbeni/2010_07_85_2389.html"} {
		srv, _ := posluziteljNN(t, cilj, html200(stranicaZaDohvat))
		if _, err := dohvatZa(t, srv).Akt(context.Background(), aktZaDohvat); !errors.Is(err, ErrNemaAkta) {
			t.Errorf("%s: greška %v, želi ErrNemaAkta", cilj, err)
		}
	}
	srv, _ := posluziteljNN(t, "/clanci/sluzbeni/full/2010_07_84_2389.html", html200(`<html><body><p>Obavijest.</p></body></html>`))
	if _, err := dohvatZa(t, srv).Akt(context.Background(), aktZaDohvat); !errors.Is(err, ErrNemaTijelaAkta) {
		t.Errorf("bez stupca akta: greška %v", err)
	}
}

// Odluke (25. 11. 2026.): stranica veća od 8 MB se ne čita (Zakon o vodama
// ima oko 420 KB).
func TestDohvatPrevelikaStranica(t *testing.T) {
	srv, _ := posluziteljNN(t, "", html200(`<div class="article-column">`+strings.Repeat("a", 9<<20)))
	if _, err := dohvatZa(t, srv).Akt(context.Background(), aktZaDohvat); !errors.Is(err, ErrPrevelikaStranica) {
		t.Errorf("greška %v, želi ErrPrevelikaStranica", err)
	}
}

// Odluke (25. 11. 2026.): prihvaća se samo odgovor 200 s HTML-om; 404 na
// adresi akta znači da akta nema, druga greška poslužitelja javlja kod, a
// PDF ili neispravna vrsta nisu stranica akta.
func TestDohvatStatusIVrsta(t *testing.T) {
	for _, s := range []struct {
		status int
		vrsta  string
		zeli   func(error) bool
	}{
		{http.StatusNotFound, "text/html", func(err error) bool { return errors.Is(err, ErrNemaAkta) }},
		{http.StatusInternalServerError, "text/html", func(err error) bool { return err != nil && strings.Contains(err.Error(), "500") }},
		{http.StatusOK, "application/pdf", func(err error) bool { return errors.Is(err, ErrNijeHTML) }},
		{http.StatusOK, "text/html; ;=", func(err error) bool { return errors.Is(err, ErrNijeHTML) }},
	} {
		srv, _ := posluziteljNN(t, "", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", s.vrsta)
			w.WriteHeader(s.status)
			fmt.Fprint(w, stranicaZaDohvat)
		})
		if _, err := dohvatZa(t, srv).Akt(context.Background(), aktZaDohvat); !s.zeli(err) {
			t.Errorf("%d %s: greška %v", s.status, s.vrsta, err)
		}
	}
}

// Odluke (25. 11. 2026.): prekinut zahtjev (rok rukovatelja) vraća grešku
// konteksta, da je servis može objasniti.
func TestDohvatPrekinutKontekst(t *testing.T) {
	srv, putevi := posluziteljNN(t, "", html200(stranicaZaDohvat))
	ctx, prekini := context.WithCancel(context.Background())
	prekini()
	if _, err := dohvatZa(t, srv).Akt(ctx, aktZaDohvat); !errors.Is(err, context.Canceled) || len(*putevi) != 0 {
		t.Errorf("greška %v, zahtjevi %v", err, *putevi)
	}
}

// Odluke (25. 11. 2026.): stranica u drugoj kodnoj stranici (windows-1250)
// pretvara se u UTF-8, pa kvačice ostaju.
func TestDrugaKodnaStranica(t *testing.T) {
	tijelo, err := charmap.Windows1250.NewEncoder().String(stranicaZaDohvat)
	if err != nil {
		t.Fatal(err)
	}
	srv, _ := posluziteljNN(t, "", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=windows-1250")
		fmt.Fprint(w, tijelo)
	})
	b, err := dohvatZa(t, srv).Akt(context.Background(), aktZaDohvat)
	if err != nil || !strings.Contains(string(b), "Članak o čuvanju") {
		t.Errorf("dohvat %v, %q", err, b)
	}
	// Greška čitanja tijela, na početku ili usred stranice, vraća se.
	pokvaren := errors.New("veza je pukla")
	for _, tijelo := range []io.Reader{citacSGreskom{pokvaren}, io.MultiReader(strings.NewReader(strings.Repeat("a", 2000)), citacSGreskom{pokvaren})} {
		resp := &http.Response{Body: io.NopCloser(tijelo), Header: http.Header{"Content-Type": {"text/html"}}}
		if _, err := procitajTijelo(resp); !errors.Is(err, pokvaren) {
			t.Errorf("greška %v, želi grešku čitanja", err)
		}
	}
}

// Odluke (25. 11. 2026.): kad mreža presreće vezu (certifikat koji ovo
// računalo ne poznaje), servis to kaže riječima, pa greška mora biti
// prepoznatljiva i kad je umotana.
func TestJePresretanje(t *testing.T) {
	nepoznat := x509.UnknownAuthorityError{}
	for err, zeli := range map[error]bool{
		nepoznat: true,
		fmt.Errorf("dohvat: %w", &tls.CertificateVerificationError{Err: nepoznat}): true,
		&url.Error{Op: "Get", URL: "https://narodne-novine.nn.hr/", Err: nepoznat}: true,
		errors.New("x509: nešto drugo"):                                            false,
		context.DeadlineExceeded:                                                   false,
	} {
		if got := JePresretanje(err); got != zeli {
			t.Errorf("JePresretanje(%v) = %v, želi %v", err, got, zeli)
		}
	}
}

// Odluke (25. 11. 2026.): dohvat radi s kopijom klijenta: kolačići se ne
// pamte, a klijent programa ostaje kakav je bio.
func TestNoviDohvatKopiraKlijent(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	k := &http.Client{Jar: jar}
	d := NoviDohvat(k)
	r, ok := d.klijent.Transport.(*razmakUpita)
	if !ok || r.dalje != http.DefaultTransport || r.razmak != razmakNN || d.klijent.Jar != nil || k.Jar == nil || k.Transport != nil {
		t.Errorf("dohvat %+v, klijent %+v", d.klijent, k)
	}
	if d.osnova.String() != "https://narodne-novine.nn.hr" {
		t.Errorf("osnova %s", d.osnova)
	}
}
