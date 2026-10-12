package web

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gocop/internal/db"
)

// Izmišljene adrese: Tailscale adresa ovog računala, drugi uređaj u istoj
// Tailscale mreži i adresa ovog računala u uredskoj mreži
const (
	tsOvo    = "100.101.102.103"
	tsOvo6   = "fd7a:115c:a1e0::5"
	tsDrugi  = "100.90.1.2"
	lanOvo   = "192.168.1.10"
	poruHTTP = "samo preko HTTPS-a"
)

// naAdresu je izravna veza s izvora na adresu ovog računala: odredište veze
// poslužitelj upisuje u kontekst zahtjeva (http.LocalAddrContextKey)
func naAdresu(izvor, odrediste string) func(*http.Request) *http.Request {
	return func(r *http.Request) *http.Request {
		r.RemoteAddr = net.JoinHostPort(izvor, "40000")
		lok := &net.TCPAddr{IP: net.ParseIP(odrediste), Port: 80}
		return r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, lok))
	}
}

// sTailscaleom daje čvoru zadane Tailscale adrese i vraća poslužitelj i
// broj čitanja kartica; prazan popis je računalo bez Tailscalea
func (o *okolinaPrijave) sTailscaleom(adrese ...string) (*Server, *atomic.Int32) {
	var citanja atomic.Int32
	vlastite := make([]netip.Addr, 0, len(adrese))
	for _, a := range adrese {
		vlastite = append(vlastite, netip.MustParseAddr(a))
	}
	s := &Server{tailscale: noviTailscale(func() []netip.Addr { citanja.Add(1); return vlastite })}
	o.srv = s.klijentSloj(o.mux)
	return s, &citanja
}

// prijavljen: prijava je odmah otvorila sesiju, bez PIN-a
func prijavljen(w *httptest.ResponseRecorder) bool {
	return kolacicSesije(w) != nil && kolacic(w, imeKolacicaPrijave) == nil && w.Header().Get("Location") == "/"
}

// odbijenoKaoIzvana: izvana preko http-a prijava s PIN-om se odbija, bez
// ikakvog kolačića
func odbijenoKaoIzvana(w *httptest.ResponseRecorder) bool {
	return w.Code == http.StatusForbidden && strings.Contains(w.Body.String(), poruHTTP) && len(w.Result().Cookies()) == 0
}

// Na čvoru s Tailscaleom izravan zahtjev na njegovu Tailscale adresu vrijedi
// kao iz lokalne mreže: prijava bez PIN-a, i preko http-a. Isti zahtjev kroz
// posrednika ostaje izvana i traži PIN.
func TestPrijavaPrekoTailscaleaBezPINa(t *testing.T) {
	o := novaOkolinaPIN(t)
	o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	s, _ := o.sTailscaleom(tsOvo, tsOvo6)
	poslano := o.posta.broj()

	for ime, porijeklo := range map[string]func(*http.Request) *http.Request{
		"IPv4":                naAdresu(tsDrugi, tsOvo),
		"IPv6":                naAdresu("fd7a:115c:a1e0::9", tsOvo6),
		"IPv4 kroz IPv6 vezu": naAdresu(tsDrugi, "::ffff:"+tsOvo),
	} {
		if w := o.prijava(porijeklo, "ana", "anina-lozinka"); !prijavljen(w) {
			t.Errorf("%s: prijava bez PIN-a, dobiveno %d %q", ime, w.Code, w.Header().Get("Location"))
		}
	}
	if o.posta.broj() != poslano {
		t.Error("prijavi preko Tailscalea PIN se ne šalje")
	}

	// posrednik na drugom uređaju u Tailscale mreži, upisan kao pouzdan
	posrednici, err := NoviPosrednici([]string{tsDrugi}, "")
	if err != nil {
		t.Fatal(err)
	}
	s.SetPosrednici(posrednici)
	o.srv = s.klijentSloj(o.mux)
	krozPosrednika := func(r *http.Request) *http.Request {
		r = naAdresu(tsDrugi, tsOvo)(r)
		r.Header.Set("CF-Connecting-IP", "198.51.100.7")
		r.Header.Set("X-Forwarded-Proto", "https")
		return r
	}
	naCekanju(t, o.prijava(krozPosrednika, "ana", "anina-lozinka"))
	if o.posta.broj() != poslano+1 {
		t.Errorf("kroz posrednika mora otići jedan PIN, otišlo %d", o.posta.broj()-poslano)
	}
}

// Adresa pošiljatelja 100.x sama ne odlučuje: na uredsku adresu računala, s
// Tailscaleom ili bez njega, zahtjev je izvana kao i prije
func TestTailscaleIzvorNaUredskuAdresuOstajeIzvana(t *testing.T) {
	o := novaOkolinaPIN(t)
	o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	poslano := o.posta.broj()

	_, citanja := o.sTailscaleom() // računalo bez Tailscalea
	if w := o.prijava(naAdresu(tsDrugi, lanOvo), "ana", "anina-lozinka"); !odbijenoKaoIzvana(w) {
		t.Errorf("bez Tailscalea, 100.x na uredsku adresu: %d %q", w.Code, w.Body.String())
	}
	if citanja.Load() != 0 {
		t.Errorf("veza na uredsku adresu ne smije čitati kartice, čitano %d puta", citanja.Load())
	}
	// adresa iz raspona koja nije na Tailscaleovoj kartici (npr. mobilni operater)
	if w := o.prijava(naAdresu(tsDrugi, tsOvo), "ana", "anina-lozinka"); !odbijenoKaoIzvana(w) {
		t.Errorf("bez Tailscalea, na adresu 100.x: %d %q", w.Code, w.Body.String())
	}

	o.sTailscaleom(tsOvo) // isto računalo s Tailscaleom
	if w := o.prijava(naAdresu(tsDrugi, lanOvo), "ana", "anina-lozinka"); !odbijenoKaoIzvana(w) {
		t.Errorf("s Tailscaleom, 100.x na uredsku adresu: %d %q", w.Code, w.Body.String())
	}
	if w := o.prijava(naAdresu("192.168.1.50", lanOvo), "ana", "anina-lozinka"); !prijavljen(w) {
		t.Errorf("iz uredske mreže prijava ostaje bez PIN-a: %d %q", w.Code, w.Header().Get("Location"))
	}
	if o.posta.broj() != poslano {
		t.Error("preko http-a PIN se ne šalje")
	}
}

// [web] tailscale_lokalno = false: zahtjev na Tailscale adresu je izvana.
// Sklopka vrijedi odmah, i dok poslužitelj radi.
func TestTailscaleLokalnoIskljuceno(t *testing.T) {
	o := novaOkolinaPIN(t)
	o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	s, citanja := o.sTailscaleom(tsOvo)

	s.SetTailscaleLokalno(false)
	if w := o.prijava(naAdresu(tsDrugi, tsOvo), "ana", "anina-lozinka"); !odbijenoKaoIzvana(w) {
		t.Errorf("isključeno: %d %q", w.Code, w.Body.String())
	}
	if citanja.Load() != 0 {
		t.Error("isključeno pravilo ne čita kartice")
	}
	s.SetTailscaleLokalno(true)
	if w := o.prijava(naAdresu(tsDrugi, tsOvo), "ana", "anina-lozinka"); !prijavljen(w) {
		t.Errorf("ponovno uključeno: %d %q", w.Code, w.Header().Get("Location"))
	}
}

// Početna lozinka i uparivanje svježeg čvora preko Tailscalea vrijede kao
// iz lokalne mreže, a kroz posrednika ne
func TestSvjezCvorPrekoTailscalea(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.racun("admin", db.ZadanaLozinka, true, true)
	o.authH.SetFresh(func() bool { return true })
	o.sTailscaleom(tsOvo)

	get := func(porijeklo func(*http.Request) *http.Request) string {
		w := httptest.NewRecorder()
		o.srv.ServeHTTP(w, porijeklo(httptest.NewRequest(http.MethodGet, "/login", nil)))
		return w.Body.String()
	}
	if b := get(naAdresu(tsDrugi, tsOvo)); !strings.Contains(b, "svjez=true") {
		t.Errorf("preko Tailscalea: %q", b)
	}
	krozPosrednika := func(r *http.Request) *http.Request {
		r = naAdresu(tsDrugi, tsOvo)(r)
		r.Header.Set("X-Forwarded-For", "198.51.100.7")
		return r
	}
	if b := get(krozPosrednika); !strings.Contains(b, "svjez=false") {
		t.Errorf("kroz posrednika: %q", b)
	}
	if w := o.prijava(krozPosrednika, "admin", db.ZadanaLozinka); w.Code != http.StatusForbidden || kolacicSesije(w) != nil {
		t.Errorf("početna lozinka kroz posrednika: %d, kolačić %v", w.Code, kolacicSesije(w))
	}
	w := o.prijava(naAdresu(tsDrugi, tsOvo), "admin", db.ZadanaLozinka)
	if w.Code != http.StatusSeeOther || kolacicSesije(w) == nil || !strings.HasPrefix(w.Header().Get("Location"), "/profile?force=1") {
		t.Errorf("početna lozinka preko Tailscalea mora proći: %d %q", w.Code, w.Body.String())
	}
}

// /zdravlje ostaje samo za ovo računalo, i preko Tailscalea
func TestZdravljeNePrekoTailscalea(t *testing.T) {
	s := &Server{tailscale: noviTailscale(func() []netip.Addr { return []netip.Addr{netip.MustParseAddr(tsOvo)} })}
	h := s.klijentSloj(http.HandlerFunc(ServeZdravlje))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, naAdresu(tsDrugi, tsOvo)(httptest.NewRequest(http.MethodGet, "/zdravlje", nil)))
	if w.Code != http.StatusNotFound {
		t.Errorf("/zdravlje preko Tailscalea: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, naAdresu("127.0.0.1", "127.0.0.1")(httptest.NewRequest(http.MethodGet, "/zdravlje", nil)))
	if w.Code != http.StatusOK {
		t.Errorf("/zdravlje s ovog računala: %d", w.Code)
	}
}

// Popis vlastitih adresa čita se najviše jednom u 30 s, pa Tailscale
// upaljen nakon čvora vrijedi najkasnije pola minute poslije
func TestTailscaleAdreseSeOsvjezavaju(t *testing.T) {
	sad := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	var vlastite []netip.Addr
	var citanja int
	ts := noviTailscale(func() []netip.Addr { citanja++; return vlastite })
	ts.sad = func() time.Time { return sad }
	zahtjev := naAdresu(tsDrugi, tsOvo)(httptest.NewRequest(http.MethodGet, "/", nil))

	if ts.stigloNa(zahtjev) || citanja != 1 {
		t.Fatalf("prije Tailscalea: čitanja %d", citanja)
	}
	vlastite = []netip.Addr{netip.MustParseAddr(tsOvo)} // Tailscale se upalio
	sad = sad.Add(tailscaleOsvjezi - time.Second)
	if ts.stigloNa(zahtjev) || citanja != 1 {
		t.Fatalf("unutar 30 s vrijedi pročitan popis: čitanja %d", citanja)
	}
	sad = sad.Add(time.Second)
	if !ts.stigloNa(zahtjev) || citanja != 2 {
		t.Fatalf("nakon 30 s popis se čita iznova: čitanja %d", citanja)
	}
	if ts.stigloNa(httptest.NewRequest(http.MethodGet, "/", nil)) || citanja != 2 {
		t.Errorf("zahtjev bez adrese veze nije na Tailscaleu i ne čita kartice: čitanja %d", citanja)
	}
}

// Bez postavke (prazan Server) pravilo je uključeno, kao zadano u gocop.toml
func TestTailscaleZadanoUkljuceno(t *testing.T) {
	s := &Server{}
	if ts := s.tailscaleIliZadani(); ts.iskljuceno.Load() || ts != s.tailscaleIliZadani() {
		t.Error("zadano uključeno, isti objekt za sloj i sklopku")
	}
	s.SetTailscaleLokalno(false)
	if !s.tailscale.iskljuceno.Load() {
		t.Error("sklopka mora isključiti pravilo")
	}
	if a := lokalnaAdresa(httptest.NewRequest(http.MethodGet, "/", nil)); a.IsValid() {
		t.Errorf("bez veze nema adrese: %v", a)
	}
}
