package web

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

// Adrese iste mreže /48 dijele mjesta u tunelu kao jedan klijent; IPv4 i
// druga mreža /48 su zasebni klijenti
func TestTunelIPv6PoMrezi(t *testing.T) {
	o := novaOgradaTunela(najviseTunela, najviseTunelaPoKlijentu)
	for i := 1; i <= najviseTunelaPoKlijentu; i++ {
		if !o.uzmi(kljucKlijentaTunela(netip.MustParseAddr("2001:db8:1:2::" + string(rune('0'+i))))) {
			t.Fatalf("mjesto %d odbijeno", i)
		}
	}
	if o.uzmi(kljucKlijentaTunela(netip.MustParseAddr("2001:db8:1:7::ff"))) {
		t.Error("treća adresa iste mreže /48 dobila je mjesto")
	}
	if !o.uzmi(kljucKlijentaTunela(netip.MustParseAddr("2001:db8:2::1"))) {
		t.Error("druga mreža /48 je drugi klijent")
	}
	if !o.uzmi(kljucKlijentaTunela(netip.MustParseAddr("203.0.113.5"))) {
		t.Error("IPv4 klijent")
	}
	if najviseTunela < 16 {
		t.Errorf("ukupna granica %d preuska: dala bi se popuniti s nekoliko adresa", najviseTunela)
	}
}

// Ograda tunela kroz pravi rukovatelj: klijent je adresa iz CF-Connecting-IP
// od posrednika, a adrese iste mreže /48 dijele mjesta
func TestOgradaTunelaKrozRukovatelj(t *testing.T) {
	o := novaOgradaTunela(najviseTunela, najviseTunelaPoKlijentu)
	pusti := make(chan struct{})
	h := (&Server{}).klijentSloj(o.omotaj(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-pusti })))
	zahtjev := func(adresa string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/razmjena/tunel", nil)
		r.RemoteAddr = "127.0.0.1:5555"
		r.Header.Set("CF-Connecting-IP", adresa)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	gotovo := make(chan struct{}, 4)
	for _, a := range []string{"2001:db8:aa:1::1", "2001:db8:aa:2::1"} {
		go func(a string) { zahtjev(a); gotovo <- struct{}{} }(a)
	}
	for i := 0; i < 50 && func() bool { o.mu.Lock(); defer o.mu.Unlock(); return o.ukupno < 2 }(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if w := zahtjev("2001:db8:aa:3::1"); w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" {
		t.Errorf("treća veza iste mreže /48: %d", w.Code)
	}
	close(pusti)
	<-gotovo
	<-gotovo
}
