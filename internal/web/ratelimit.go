package web

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Ograničenje pokušaja prijave. Bez toga bi javni čvor dopuštao pogađanje
// lozinki u petlji, a zadana lozinka piše u dokumentaciji. Broji se po
// korisničkom imenu i po adresi: napadač koji mijenja imena udara u
// ograničenje adrese, a onaj koji mijenja adrese u ograničenje imena.
// Sve živi u memoriji; ponovno pokretanje briše brojače, što je za alfu
// prihvatljivo — cilj je usporiti pogađanje, ne voditi evidenciju.

const (
	loginMaxAttempts = 5
	loginWindow      = 15 * time.Minute
	loginBlock       = 15 * time.Minute
)

type attemptRecord struct {
	failures     int
	firstFailure time.Time
	blockedUntil time.Time
}

// loginLimiter pamti neuspjele prijave po ključu (ime ili adresa)
type loginLimiter struct {
	mu      sync.Mutex
	records map[string]*attemptRecord
	now     func() time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{records: map[string]*attemptRecord{}, now: time.Now}
}

// Blocked javlja je li ključ trenutno blokiran i koliko još čeka
func (l *loginLimiter) Blocked(keys ...string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	var longest time.Duration
	for _, k := range keys {
		if rec, ok := l.records[k]; ok && rec.blockedUntil.After(now) {
			if d := rec.blockedUntil.Sub(now); d > longest {
				longest = d
			}
		}
	}
	return longest > 0, longest
}

// Fail bilježi neuspjeh; nakon loginMaxAttempts u loginWindow ključ se blokira
func (l *loginLimiter) Fail(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for _, k := range keys {
		if k == "" {
			continue
		}
		rec, ok := l.records[k]
		if !ok || now.Sub(rec.firstFailure) > loginWindow {
			rec = &attemptRecord{firstFailure: now}
			l.records[k] = rec
		}
		rec.failures++
		if rec.failures >= loginMaxAttempts {
			rec.blockedUntil = now.Add(loginBlock)
		}
	}
	l.sweep(now)
}

// Reset briše brojače nakon uspješne prijave
func (l *loginLimiter) Reset(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		delete(l.records, k)
	}
}

// sweep čisti zastarjele zapise da mapa ne raste bez granice
func (l *loginLimiter) sweep(now time.Time) {
	if len(l.records) < 1000 {
		return
	}
	for k, rec := range l.records {
		if now.Sub(rec.firstFailure) > loginWindow && !rec.blockedUntil.After(now) {
			delete(l.records, k)
		}
	}
}

// clientIP vraća adresu s koje je zahtjev došao. Iza Cloudflare tunela ili
// obratnog posrednika prava adresa stoji u zaglavlju, a RemoteAddr je
// posrednik — bez toga bi jedno ograničenje vrijedilo za sve korisnike.
// Zaglavlju se vjeruje samo kad zahtjev stvarno dolazi od posrednika na
// istom stroju ili u lokalnoj mreži; s interneta ga svatko može podmetnuti
// i tako mijenjati "adresu" pri svakom pokušaju.
func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}
	if !fromProxy(host) {
		return host
	}
	if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
			return first
		}
	}
	return host
}

// fromProxy: loopback ili privatna adresa — tamo stoje tunel i posrednik
func fromProxy(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

// passwordChangeAllowed javlja smije li se putanja otvoriti dok korisnik
// još radi sa zadanom lozinkom: samo vlastiti profil, promjena lozinke,
// odjava i statika. Sve ostalo čeka dok lozinka nije postavljena.
func passwordChangeAllowed(path string) bool {
	switch path {
	case "/profile", "/profile/update", "/profile/change-password", "/logout":
		return true
	}
	return strings.HasPrefix(path, "/static/")
}
