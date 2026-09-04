package web

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestPetNeuspjelihPrijavaBlokiraIme(t *testing.T) {
	l := newLoginLimiter()
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }

	for i := 0; i < loginMaxAttempts-1; i++ {
		l.Fail("user:tomislav", "ip:10.0.0.1")
	}
	if blocked, _ := l.Blocked("user:tomislav", "ip:10.0.0.1"); blocked {
		t.Fatal("blokada prije petog neuspjeha")
	}
	l.Fail("user:tomislav", "ip:10.0.0.1")
	blocked, wait := l.Blocked("user:tomislav")
	if !blocked || wait != loginBlock {
		t.Fatalf("nakon %d neuspjeha očekivana blokada od %v, dobiveno %v/%v", loginMaxAttempts, loginBlock, blocked, wait)
	}
	// ista adresa je blokirana i za drugo ime — napadač mijenja imena
	if blocked, _ := l.Blocked("user:netko-drugi", "ip:10.0.0.1"); !blocked {
		t.Error("adresa s pet neuspjeha mora biti blokirana i za drugo ime")
	}
	// druga adresa, isto ime: ime je blokirano — napadač mijenja adrese
	if blocked, _ := l.Blocked("user:tomislav", "ip:10.0.0.2"); !blocked {
		t.Error("ime s pet neuspjeha mora biti blokirano i s druge adrese")
	}

	now = now.Add(loginBlock + time.Second)
	if blocked, _ := l.Blocked("user:tomislav", "ip:10.0.0.1"); blocked {
		t.Error("blokada mora isteći")
	}
}

func TestUspjesnaPrijavaBriseBrojac(t *testing.T) {
	l := newLoginLimiter()
	l.Fail("user:ana", "ip:1.1.1.1")
	l.Fail("user:ana", "ip:1.1.1.1")
	l.Reset("user:ana", "ip:1.1.1.1")
	for i := 0; i < loginMaxAttempts-1; i++ {
		l.Fail("user:ana", "ip:1.1.1.1")
	}
	if blocked, _ := l.Blocked("user:ana"); blocked {
		t.Error("nakon uspješne prijave brojanje kreće ispočetka")
	}
}

func TestNeuspjesiIzvanProzoraSeNeZbrajaju(t *testing.T) {
	l := newLoginLimiter()
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	for i := 0; i < loginMaxAttempts-1; i++ {
		l.Fail("user:x")
	}
	now = now.Add(loginWindow + time.Minute)
	l.Fail("user:x")
	if blocked, _ := l.Blocked("user:x"); blocked {
		t.Error("stari neuspjesi izvan prozora ne smiju brojati")
	}
}

func TestAdresaIzaPosrednika(t *testing.T) {
	r := httptest.NewRequest("POST", "/login", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	if got := clientIP(r); got != "127.0.0.1" {
		t.Errorf("bez zaglavlja očekivan 127.0.0.1, dobiveno %s", got)
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	if got := clientIP(r); got != "203.0.113.7" {
		t.Errorf("X-Forwarded-For: prva adresa, dobiveno %s", got)
	}
	r.Header.Set("CF-Connecting-IP", "198.51.100.9")
	if got := clientIP(r); got != "198.51.100.9" {
		t.Errorf("Cloudflare zaglavlje ima prednost, dobiveno %s", got)
	}

	// s interneta izravno: zaglavlje se ne smije uvažiti, inače ga napadač mijenja po volji
	direct := httptest.NewRequest("POST", "/login", nil)
	direct.RemoteAddr = "203.0.113.200:4444"
	direct.Header.Set("X-Forwarded-For", "10.9.9.9")
	direct.Header.Set("CF-Connecting-IP", "10.8.8.8")
	if got := clientIP(direct); got != "203.0.113.200" {
		t.Errorf("izravno s interneta: zaglavlje podmetnuto, očekivan 203.0.113.200, dobiveno %s", got)
	}
}

func TestSaZadanomLozinkomProlaziSamoProfil(t *testing.T) {
	allowed := []string{"/profile", "/profile/change-password", "/profile/update", "/logout", "/static/css/style.css"}
	blocked := []string{"/", "/users", "/sections/B.16.2", "/settings", "/api/network/members", "/view-as/stop"}
	for _, p := range allowed {
		if !passwordChangeAllowed(p) {
			t.Errorf("%s mora biti dopušten dok se lozinka ne promijeni", p)
		}
	}
	for _, p := range blocked {
		if passwordChangeAllowed(p) {
			t.Errorf("%s ne smije proći sa zadanom lozinkom", p)
		}
	}
}
