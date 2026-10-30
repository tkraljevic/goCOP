package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

// adresaCvoraTesta je adresa čvora u lokalnoj mreži na koju testovi šalju
// izravne zahtjeve (zaglavlje Host), kao preglednik u uredu
const adresaCvoraTesta = "192.168.1.10:1160"

// prijavaNaIme šalje prijavu preglednikom koji je stranicu otvorio na
// zadanom imenu (Host i Origin isti, kao stranica s istog porijekla)
func prijavaNaIme(h http.Handler, host, izvor, ime, lozinka string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "http://"+host+"/login", strings.NewReader(url.Values{"username": {ime}, "password": {lozinka}}.Encode()))
	r.Host = host
	r.RemoteAddr = izvor
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://"+host)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func otvorilaSesiju(w *httptest.ResponseRecorder) bool {
	return w.Code == http.StatusSeeOther && kolacicSesije(w) != nil
}

// klijentNaIme je klijent kakvim sloj klijenta vidi prijavu preglednikom
// otvorenom na zadanom imenu
func klijentNaIme(t *testing.T, s *Server, host, izvor string) Klijent {
	t.Helper()
	var k Klijent
	stigao := false
	h := s.klijentSloj(zaglavlja(s.zastitaOdTudjihStranica(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k, stigao = klijentIz(r), true
	}))))
	if w := prijavaNaIme(h, host, izvor, "", ""); !stigao {
		t.Fatalf("zahtjev na %s nije stigao do rukovatelja: %d", host, w.Code)
	}
	return k
}

// Zahtjev upućen imenu koje ne vodi na ovo računalo vrijedi kao izvana, i
// iz lokalne mreže. Na adresu, ime računala u mreži, localhost i upisanu
// javnu adresu zahtjev iz ureda vrijedi kao iz lokalne mreže. Prijava
// običnom lozinkom radi na svako ime, a stara zajednička lozinka ni na
// jedno.
func TestPrijavaNaTudjeImeVrijediKaoIzvana(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.racun("djelatnik", staraPocetnaLozinka, false, true)
	o.racun("ana", "anina-lozinka", false, false)
	s := &Server{}
	s.SetJavnaAdresa("https://cop.primjer.hr/")
	h := s.klijentSloj(zaglavlja(s.zastitaOdTudjihStranica(o.mux)))
	const lan = "192.168.1.50:40000"

	if k := klijentNaIme(t, s, "x.primjer.hr:1160", lan); !k.Izvana() || k.IzLokalneMreze() {
		t.Errorf("tuđe ime iz lokalne mreže mora biti izvana: %+v", k)
	}
	for _, host := range []string{"192.168.1.10:1160", "tower:1160", "tower.local", "[fe80::1]:1160", "COP.primjer.hr.", "cop.primjer.hr:443"} {
		if k := klijentNaIme(t, s, host, lan); k.Izvana() || !k.IzLokalneMreze() {
			t.Errorf("%s iz lokalne mreže mora biti iz ureda: %+v", host, k)
		}
		if w := prijavaNaIme(h, host, lan, "djelatnik", staraPocetnaLozinka); w.Code != http.StatusForbidden || kolacicSesije(w) != nil {
			t.Errorf("stara lozinka na %s iz lokalne mreže: %d", host, w.Code)
		}
	}
	// obična lozinka na tuđe ime radi, samo uz pravila za prijavu izvana
	if w := prijavaNaIme(h, "x.primjer.hr:1160", lan, "ana", "anina-lozinka"); !otvorilaSesiju(w) {
		t.Errorf("obična lozinka na tuđe ime: %d %q", w.Code, w.Body.String())
	}

	// s ovog računala je samo zahtjev na njegovo ime
	if k := klijentNaIme(t, s, "x.primjer.hr:1160", "127.0.0.1:40000"); k.SOvogRacunala() {
		t.Errorf("tuđe ime s ovog računala: %+v", k)
	}
	for _, host := range []string{"localhost:1160", "127.0.0.1:1160", "[::1]:1160"} {
		if k := klijentNaIme(t, s, host, "127.0.0.1:40000"); !k.SOvogRacunala() {
			t.Errorf("%s s ovog računala: %+v", host, k)
		}
	}

	// kroz tunel prijava radi kao i prije, a stara lozinka se odbija
	tunel := func(lozinka, ime string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "https://cop.primjer.hr/login", strings.NewReader(url.Values{"username": {ime}, "password": {lozinka}}.Encode()))
		r.RemoteAddr = "127.0.0.1:5555"
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("CF-Connecting-IP", "198.51.100.7")
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := tunel("anina-lozinka", "ana"); !otvorilaSesiju(w) {
		t.Errorf("kroz tunel: %d %q", w.Code, w.Body.String())
	}
	if w := tunel(staraPocetnaLozinka, "djelatnik"); w.Code != http.StatusForbidden {
		t.Errorf("stara lozinka kroz tunel: %d", w.Code)
	}
}

// Stanje programa za Postavu daje se samo zahtjevu s ovog računala na
// njegovo ime ili adresu
func TestZdravljeNaTudjeIme(t *testing.T) {
	h := (&Server{}).klijentSloj(http.HandlerFunc((&Server{}).serveZdravlje))
	for host, zelim := range map[string]int{"127.0.0.1:1160": http.StatusOK, "localhost:1160": http.StatusOK, "[::1]:1160": http.StatusOK,
		"x.primjer.hr:1160": http.StatusNotFound, "x.local:1160": http.StatusNotFound, "tower:1160": http.StatusNotFound} {
		r := httptest.NewRequest(http.MethodGet, "/zdravlje", nil)
		r.RemoteAddr, r.Host = "127.0.0.1:50000", host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != zelim {
			t.Errorf("%s: %d, želim %d", host, w.Code, zelim)
		}
	}
}

func TestPoznatoIme(t *testing.T) {
	s := &Server{}
	s.SetJavnaAdresa("https://Cop.Primjer.hr:8443/akti")
	s.SetLokalnaImena([]string{"GoCOP.Ured.hr", " https://cop2.ured.hr/ ", ""})
	poznato := func(host string, naTailscale bool) bool { return s.poznatoIme(imeIzZaglavlja(host), naTailscale) }
	poznata := []string{"", "localhost", "localhost:1160", "app.localhost", "127.0.0.1", "[::1]:80", "10.0.0.5:1160",
		"100.101.102.103", "tower", "TOWER:1160", "tower.local.", "nas.lan", "kuca.home.arpa", "gocop.internal",
		"cop.primjer.hr", "cop.primjer.hr:1160", "gocop.ured.hr:1160", "cop2.ured.hr", imeRacunala()}
	for _, h := range poznata {
		if !poznato(h, false) {
			t.Errorf("%q mora biti poznato ime", h)
		}
	}
	for _, h := range []string{"x.primjer.hr", "primjer.hr", "cop.primjer.hr.drugo.hr", "localhost.drugo.hr", "lan.drugo.hr:1160",
		"primjer.com", "ured.hr", "copos-thinkcentre.tail1234.ts.net"} {
		if poznato(h, false) {
			t.Errorf("%q ne smije biti poznato ime", h)
		}
	}
	// Tailscaleovo ime vrijedi samo uz vezu na Tailscale adresu ovog računala
	if !poznato("copos-thinkcentre.tail1234.ts.net:1160", true) {
		t.Error("ime .ts.net uz vezu na Tailscale adresu mora biti poznato")
	}
	if (&Server{}).poznatoIme("cop.primjer.hr", false) {
		t.Error("bez javne adrese njezino ime nije poznato")
	}
	s.SetLokalnaImena(nil)
	if poznato("gocop.ured.hr", false) {
		t.Error("ime izbačeno iz lokalna_imena više nije poznato")
	}
	for adresa, ime := range map[string]string{"": "", "cop.primjer.hr": "cop.primjer.hr", "http://a.hr/x?y": "a.hr", "https://[2001:db8::1]:443/": "2001:db8::1"} {
		if got := imeIzAdrese(adresa); got != ime {
			t.Errorf("imeIzAdrese(%q) = %q, želim %q", adresa, got, ime)
		}
	}
	k := Klijent{Posrednik: netip.MustParseAddr("192.168.1.50"), TudjeIme: true}
	if !k.Izvana() || k.IzLokalneMreze() {
		t.Error("klijent na tuđe ime mora biti izvana")
	}
	k.TudjeIme = false
	if k.Izvana() {
		t.Error("klijent iz lokalne mreže na poznato ime nije izvana")
	}
}

// S ovog računala je samo veza s adrese petlje upućena imenu localhost ili
// adresi petlje; ime iz lokalne mreže (.local, ime bez točke) to nije
func TestImeOvogRacunala(t *testing.T) {
	for ime, zelim := range map[string]bool{"": true, "localhost": true, "app.localhost": true, "127.0.0.1": true,
		"127.1.2.3": true, "::1": true, "::ffff:127.0.0.1": true, "tower": false, "tower.local": false,
		"192.168.1.10": false, "localhost.drugo.hr": false, "cop.primjer.hr": false} {
		if got := imeOvogRacunala(ime); got != zelim {
			t.Errorf("imeOvogRacunala(%q) = %v", ime, got)
		}
	}
	petlja := netip.MustParseAddr("127.0.0.1")
	for _, k := range []Klijent{
		{Posrednik: petlja, ImeOvogRacunala: true, KrozPosrednika: true},
		{Posrednik: netip.MustParseAddr("192.168.1.50"), ImeOvogRacunala: true},
		{Posrednik: petlja},
	} {
		if k.SOvogRacunala() {
			t.Errorf("%+v nije s ovog računala", k)
		}
	}
	if !(Klijent{Posrednik: petlja, ImeOvogRacunala: true}).SOvogRacunala() {
		t.Error("veza s petlje na localhost je s ovog računala")
	}
}

// S ovog računala je samo zahtjev na ime localhost ili adresu petlje, a ne
// na ime iz lokalne mreže ni na Tailscaleovo ime, i kad stigne s ovog
// računala. Tailscaleovo ime s ovog računala (npr. kroz posrednika na istom
// stroju) vrijedi kao izvana, a ime iz lokalne mreže iz ureda kao iz ureda.
func TestImeZahtjevaSOvogRacunala(t *testing.T) {
	s := &Server{}
	for _, host := range []string{"x.local:1160", "tower:1160", "racunalo.lan", "tower.tail1234.ts.net"} {
		if k := klijentNaIme(t, s, host, "127.0.0.1:40000"); k.SOvogRacunala() {
			t.Errorf("%s s ovog računala nije na ime ovog računala: %+v", host, k)
		}
	}
	if k := klijentNaIme(t, s, "localhost:1160", "127.0.0.1:40000"); !k.SOvogRacunala() {
		t.Errorf("localhost: %+v", k)
	}
	if k := klijentNaIme(t, s, "tower.tail1234.ts.net", "127.0.0.1:40000"); !k.Izvana() {
		t.Errorf("Tailscaleovo ime s ovog računala: %+v", k)
	}
	if k := klijentNaIme(t, s, "tower.local:1160", "192.168.1.50:40000"); k.Izvana() || !k.IzLokalneMreze() {
		t.Errorf("iz ureda na tower.local: %+v", k)
	}
}

// Bez sloja klijenta (rukovatelj bez poslužitelja) ime se provjerava jednako
func TestKlijentBezSlojaGledaIme(t *testing.T) {
	for host, tudje := range map[string]bool{"x.primjer.hr": true, "localhost:1160": false, adresaCvoraTesta: false} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr, r.Host = "127.0.0.1:40000", host
		if k := klijentIz(r); k.TudjeIme != tudje {
			t.Errorf("%s: %+v", host, k)
		}
	}
}

// Čvor koji se ne pokreće daje cijeli razlog samo zahtjevu s ovog računala
// na localhost ili adresu petlje
func TestOdbijenoPokretanjeGledaIme(t *testing.T) {
	h := OdbijenoPokretanje("Čvor se ne pokreće: nema ključa čvora /data/node-key.")
	for host, cijeli := range map[string]bool{"x.primjer.hr": false, "x.local": false, "localhost:1160": true, "127.0.0.1:1160": true} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr, r.Host = "127.0.0.1:40000", host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if got := strings.Contains(w.Body.String(), "/data/node-key"); got != cijeli {
			t.Errorf("%s: %q", host, w.Body.String())
		}
	}
}

// Postavljanje svježeg čvora ne otvara se na tuđe ime ni s ovog računala
func TestPostavljanjeNaTudjeIme(t *testing.T) {
	o := novaOkolinaPostavljanja(t)
	tudje := func(r *http.Request) *http.Request {
		r.RemoteAddr, r.Host = "127.0.0.1:40000", "x.primjer.hr:1160"
		return r
	}
	if b := o.get(tudje, "").Body.String(); strings.HasPrefix(b, "dopusteno") {
		t.Errorf("postavljanje na tuđe ime: %q", b)
	}
	if b := o.get(lokalno, "").Body.String(); !strings.HasPrefix(b, "dopusteno") {
		t.Errorf("postavljanje s ovog računala: %q", b)
	}
	// ime iz lokalne mreže s ovog računala traži kod, kao iz mreže
	naImeMreze := func(r *http.Request) *http.Request {
		r.RemoteAddr, r.Host = "127.0.0.1:40000", "tower.local:1160"
		return r
	}
	if b := o.get(naImeMreze, "").Body.String(); strings.HasPrefix(b, "dopusteno") {
		t.Errorf("postavljanje na ime iz mreže bez koda: %q", b)
	}
	if b := o.get(naImeMreze, "?kod=7KQ4-M2XD").Body.String(); !strings.HasPrefix(b, "dopusteno") {
		t.Errorf("postavljanje na ime iz mreže s kodom: %q", b)
	}
}

// Pokušaji prijave na ime koje ne vodi na ovo računalo broje se odvojeno od
// pokušaja istog računala na adresu ili ime čvora, a IPv6 iz lokalne mreže
// ne grupira se po mreži /64 ni kad je ime tuđe
func TestOgranicenjeNaTudjeIme(t *testing.T) {
	ured := netip.MustParseAddr("fd00::25")
	for k, zelim := range map[Klijent]string{
		{Adresa: netip.MustParseAddr("192.168.1.50"), Posrednik: netip.MustParseAddr("192.168.1.50"), TudjeIme: true}: "ime:192.168.1.50",
		{Adresa: ured, Posrednik: ured, TudjeIme: true}:                                                               "ime:fd00::25",
		{Adresa: ured, Posrednik: ured}:                                                                               "fd00::25",
		{Adresa: netip.MustParseAddr("2001:db8:1:2::7"), Posrednik: netip.MustParseAddr("2001:db8:1:2::7")}:           "2001:db8:1:2::/64",
	} {
		if got := adresaZaOgranicenje(k); got != zelim {
			t.Errorf("%+v: %q, želim %q", k, got, zelim)
		}
	}

	o := novaOkolinaPrijave(t)
	o.racun("ana", "anina-lozinka", false, false)
	h := (&Server{}).klijentSloj(zaglavlja(o.mux))
	const lan = "192.168.1.50:40000"
	for i := 0; i < granicaZa(kljucAdresa).najvise; i++ {
		prijavaNaIme(h, "x.primjer.hr", lan, fmt.Sprintf("osoba%d", i), "kriva")
	}
	if w := prijavaNaIme(h, "x.primjer.hr", lan, "ana", "anina-lozinka"); w.Code != http.StatusTooManyRequests {
		t.Errorf("na tuđe ime adresa mora čekati: %d", w.Code)
	}
	if w := prijavaNaIme(h, adresaCvoraTesta, lan, "ana", "anina-lozinka"); !otvorilaSesiju(w) {
		t.Errorf("isto računalo na adresu čvora: %d %q", w.Code, w.Body.String())
	}
}
