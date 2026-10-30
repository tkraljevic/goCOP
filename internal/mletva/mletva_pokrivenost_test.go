package mletva

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// probniSustav glumi mletva.voda.hr: prijava daje kolačić, stranica bez
// kolačića preusmjerava na prijavu, a test može kolačić poništiti.
type probniSustav struct {
	mu          sync.Mutex
	brojPrijava int
	valjan      string // vrijednost kolačića koja vrijedi
	lozinka     string
	odbijPon    bool // ponovna prijava ne uspije
}

func (p *probniSustav) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+putPrijave, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ReturnUrl") != "/" {
			t.Errorf("ReturnUrl %q", r.URL.RawQuery)
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.Header.Get("User-Agent") != "goCOP" {
			t.Errorf("zaglavlja %v", r.Header)
		}
		r.ParseForm()
		p.mu.Lock()
		defer p.mu.Unlock()
		p.brojPrijava++
		if r.Form.Get("UserName") != "ana.horvat" || r.Form.Get("Password") != p.lozinka ||
			r.Form.Get("RememberMe") != "false" || (p.odbijPon && p.brojPrijava > 1) {
			w.WriteHeader(http.StatusOK) // odbijena prijava vrati formu
			w.Write([]byte("<form>"))
			return
		}
		p.valjan = "k" + string(rune('0'+p.brojPrijava))
		http.SetCookie(w, &http.Cookie{Name: ".ASPXAUTH", Value: p.valjan, Path: "/"})
		http.Redirect(w, r, "/", http.StatusFound)
	})
	mux.HandleFunc("GET /Home/Postaja", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(".ASPXAUTH")
		p.mu.Lock()
		valjan := p.valjan
		p.mu.Unlock()
		if err != nil || c.Value != valjan || valjan == "" {
			http.Redirect(w, r, putPrijave+"?ReturnUrl=%2fHome%2fPostaja", http.StatusFound)
			return
		}
		w.Write([]byte("vodostaj " + r.URL.Query().Get("id")))
	})
	mux.HandleFunc("GET /drugo", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/negdje", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /kvar", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "kvar", http.StatusInternalServerError)
	})
	return mux
}

func (p *probniSustav) prijava() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.brojPrijava
}

func (p *probniSustav) ponisti() {
	p.mu.Lock()
	p.valjan = ""
	p.mu.Unlock()
}

func noviKlijent(t *testing.T, p *probniSustav) *Klijent {
	t.Helper()
	srv := httptest.NewServer(p.handler(t))
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &Klijent{Adresa: srv.URL + "/", HTTP: &http.Client{Jar: jar}}
}

// Prijava pamti račun, a stranica se čita s kolačićem prijave.
func TestPrijavaIStranica(t *testing.T) {
	p := &probniSustav{lozinka: "Tajna-1"}
	k := noviKlijent(t, p)
	if k.Prijavljen("ana.horvat") {
		t.Fatal("prijavljen prije prijave")
	}
	if err := k.Prijava(context.Background(), "ana.horvat", "Tajna-1"); err != nil {
		t.Fatal(err)
	}
	if !k.Prijavljen("ana.horvat") || k.Prijavljen("ivo.kovac") {
		t.Error("Prijavljen ne prati račun")
	}
	b, err := k.Stranica(context.Background(), "/Home/Postaja?id=5022")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "vodostaj 5022" {
		t.Errorf("stranica %q", b)
	}
}

// Kriva lozinka: poslužitelj vrati formu, a greška ne nosi lozinku.
func TestPrijavaOdbijena(t *testing.T) {
	p := &probniSustav{lozinka: "Tajna-1"}
	k := noviKlijent(t, p)
	err := k.Prijava(context.Background(), "ana.horvat", "kriva-lozinka")
	if err == nil || !strings.Contains(err.Error(), "odbio prijavu") {
		t.Fatalf("greška %v", err)
	}
	if strings.Contains(err.Error(), "kriva-lozinka") {
		t.Error("poruka nosi lozinku")
	}
	if k.Prijavljen("ana.horvat") {
		t.Error("odbijena prijava se vodi kao prijava")
	}
}

// Preusmjeravanje natrag na prijavu znači odbijenu prijavu, i uz 302.
func TestPrijavaPreusmjerenaNaPrijavu(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, putPrijave+"?greska=1", http.StatusFound)
	}))
	defer srv.Close()
	k := &Klijent{Adresa: srv.URL, HTTP: &http.Client{}}
	if err := k.Prijava(context.Background(), "ana.horvat", "x"); err == nil {
		t.Fatal("preusmjeravanje na prijavu prošlo kao uspjeh")
	}
}

// Istekla prijava obnavlja se jednom, istim računom, i zahtjev se ponovi.
func TestStranicaObnavljaIstekluPrijavu(t *testing.T) {
	p := &probniSustav{lozinka: "Tajna-1"}
	k := noviKlijent(t, p)
	if err := k.Prijava(context.Background(), "ana.horvat", "Tajna-1"); err != nil {
		t.Fatal(err)
	}
	p.ponisti()
	b, err := k.Stranica(context.Background(), "/Home/Postaja?id=1")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "vodostaj 1" || p.prijava() != 2 {
		t.Errorf("stranica %q, prijava %d", b, p.prijava())
	}
	if !k.Prijavljen("ana.horvat") {
		t.Error("nakon obnove nije prijavljen")
	}
}

// Bez zapamćenog računa stranica ne pokušava prijavu nego javi da je traži.
func TestStranicaBezRacuna(t *testing.T) {
	p := &probniSustav{lozinka: "Tajna-1"}
	k := noviKlijent(t, p)
	_, err := k.Stranica(context.Background(), "/Home/Postaja")
	if err == nil || !strings.Contains(err.Error(), "traži prijavu") {
		t.Fatalf("greška %v", err)
	}
	if p.prijava() != 0 {
		t.Errorf("pokušano %d prijava bez računa", p.prijava())
	}
}

// Kad ni obnovljena prijava ne prođe, greška je greška prijave i prijava
// se ne vodi kao važeća.
func TestStranicaObnovaOdbijena(t *testing.T) {
	p := &probniSustav{lozinka: "Tajna-1", odbijPon: true}
	k := noviKlijent(t, p)
	if err := k.Prijava(context.Background(), "ana.horvat", "Tajna-1"); err != nil {
		t.Fatal(err)
	}
	p.ponisti()
	_, err := k.Stranica(context.Background(), "/Home/Postaja")
	if err == nil || !strings.Contains(err.Error(), "odbio prijavu") {
		t.Fatalf("greška %v", err)
	}
	if k.Prijavljen("ana.horvat") {
		t.Error("istekla prijava se i dalje vodi kao važeća")
	}
}

// Poslužitelj koji i nakon obnove traži prijavu: pokušava se samo jednom.
func TestStranicaObnovaSamoJednom(t *testing.T) {
	var prijava int
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+putPrijave, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		prijava++
		mu.Unlock()
		http.Redirect(w, r, "/", http.StatusFound)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, putPrijave, http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	k := &Klijent{Adresa: srv.URL, HTTP: &http.Client{}}
	if err := k.Prijava(context.Background(), "ana.horvat", "x"); err != nil {
		t.Fatal(err)
	}
	_, err := k.Stranica(context.Background(), "/Home/Postaja")
	if err == nil || !strings.Contains(err.Error(), "traži prijavu") {
		t.Fatalf("greška %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if prijava != 2 {
		t.Errorf("prijava %d, želim 2 (prva i jedna obnova)", prijava)
	}
}

// Preusmjeravanje drugamo i greška poslužitelja javljaju se, ne čitaju kao
// prazna stranica.
func TestStranicaGreske(t *testing.T) {
	p := &probniSustav{lozinka: "Tajna-1"}
	k := noviKlijent(t, p)
	if _, err := k.Stranica(context.Background(), "/drugo"); err == nil || !strings.Contains(err.Error(), "preusmjerava na /negdje") {
		t.Errorf("preusmjeravanje: %v", err)
	}
	if _, err := k.Stranica(context.Background(), "/kvar"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("kvar: %v", err)
	}
	if _, err := k.Stranica(context.Background(), "/%zz"); err == nil {
		t.Error("neispravan put prošao")
	}
}

func TestStranicaBezVeze(t *testing.T) {
	k := &Klijent{Adresa: "http://mletva.test", HTTP: &http.Client{Transport: prekinutaVeza{}}}
	if _, err := k.Stranica(context.Background(), "/"); err == nil {
		t.Error("bez poslužitelja nema greške")
	}
	if err := k.Prijava(context.Background(), "ana.horvat", "x"); err == nil {
		t.Error("prijava bez poslužitelja nema greške")
	}
}

// Prazna adresa je zadani sustav, a kosa crta na kraju se ne udvostručuje.
func TestOsnova(t *testing.T) {
	if got := (&Klijent{}).osnova(); got != ZadanaAdresa {
		t.Errorf("zadano %q", got)
	}
	if got := (&Klijent{Adresa: "https://primjer.test//"}).osnova(); got != "https://primjer.test" {
		t.Errorf("s kosom crtom %q", got)
	}
}

// Klijent sastavljen sam ima kolačiće i ne slijedi preusmjeravanja; isti
// se vraća pri svakom pozivu.
func TestKlijentSastavljenSam(t *testing.T) {
	k := &Klijent{}
	c := k.klijent()
	if c.Jar == nil || c.Timeout != 30*time.Second || c.CheckRedirect == nil {
		t.Fatalf("klijent %+v", c)
	}
	if !errors.Is(c.CheckRedirect(nil, nil), http.ErrUseLastResponse) {
		t.Error("preusmjeravanje se slijedi")
	}
	if k.klijent() != c {
		t.Error("drugi poziv sastavio novi klijent")
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil || tr.TLSClientConfig.Renegotiation != tls.RenegotiateOnceAsClient {
		t.Error("bez TLS-a koji pušta jedno ponovno pregovaranje")
	}
}

// Bez certifikata nema veze.
func TestTLSBezCertifikata(t *testing.T) {
	err := TLSBezIsteka().VerifyConnection(tls.ConnectionState{})
	if err == nil || !strings.Contains(err.Error(), "nije poslao certifikat") {
		t.Fatalf("greška %v", err)
	}
}

// Istek se prešuti, ali potpisnik ne: istekao certifikat koji nije
// potpisalo poznato tijelo odbija se zbog potpisnika, ne zbog datuma.
func TestTLSIstekaoINepoznat(t *testing.T) {
	kljuc, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	predlozak := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: "*.voda.test"},
		DNSNames:     []string{"mletva.voda.test"},
		NotBefore:    time.Date(2024, 6, 21, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2025, 6, 21, 0, 0, 0, 0, time.UTC),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, predlozak, predlozak, &kljuc.PublicKey, kljuc)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	err = TLSBezIsteka().VerifyConnection(tls.ConnectionState{
		ServerName:       "mletva.voda.test",
		PeerCertificates: []*x509.Certificate{cert, cert},
	})
	var nepoznat x509.UnknownAuthorityError
	if !errors.As(err, &nepoznat) {
		t.Fatalf("greška %v (%T), želim nepoznatog potpisnika", err, err)
	}
}

// prekinutaVeza je prijenos koji ne dođe do poslužitelja
type prekinutaVeza struct{}

func (prekinutaVeza) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("veza prekinuta")
}
