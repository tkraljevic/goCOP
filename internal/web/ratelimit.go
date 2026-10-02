package web

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"gocop/internal/service"
)

// Ograničenje pokušaja prijave. Bez toga bi javni čvor dopuštao pogađanje
// lozinki u petlji, a zadana lozinka piše u dokumentaciji. Broji se na tri
// razine: ime s adrese (5 u 15 min) zaustavlja pogađanje jednog računa s
// jednog mjesta; adresa (20 u 15 min) napadača koji mijenja imena; ime (30 u
// sat) napadača koji mijenja adrese. Jedan napadač s pet pokušaja tako više
// ne zaključava tuđi račun, a da bi zaključao ime treba mu više adresa.
// Sve živi u memoriji; ponovno pokretanje briše brojače, što je za alfu
// prihvatljivo — cilj je usporiti pogađanje, ne voditi evidenciju.

const (
	loginMaxAttempts = 5
	loginWindow      = 15 * time.Minute
	loginBlock       = 15 * time.Minute

	// najviseKljuceva je granica mape: napadač s mnogo adresa ne smije
	// njome napuniti memoriju
	najviseKljuceva = 50000
)

// Vrste ključeva; granicu ključa određuje njegov prefiks
const (
	kljucImeAdresa = "user+ip:"
	kljucAdresa    = "ip:"
	kljucIme       = "user:"
	kljucPonovna   = "reauth:" // ponovni upis lozinke unutar prijave
)

// granicaKljuca je granica jedne vrste ključa
type granicaKljuca struct {
	najvise int           // neuspjeha u prozoru do blokade
	prozor  time.Duration // koliko se dugo neuspjesi zbrajaju
	blokada time.Duration // koliko blokada traje
}

func granicaZa(kljuc string) granicaKljuca {
	switch {
	case strings.HasPrefix(kljuc, kljucAdresa):
		return granicaKljuca{20, 15 * time.Minute, 15 * time.Minute}
	case strings.HasPrefix(kljuc, kljucIme):
		return granicaKljuca{30, time.Hour, 30 * time.Minute}
	case strings.HasPrefix(kljuc, kljucPonovna):
		return granicaKljuca{3, 15 * time.Minute, 15 * time.Minute}
	}
	return granicaKljuca{loginMaxAttempts, loginWindow, loginBlock}
}

// kljuceviPrijave su ključevi jednog pokušaja prijave. Ključ samog imena
// broji samo pokušaje izvana (kroz tunel): inače bi napadač s interneta s
// nekoliko adresa zaključao račun i za prijavu iz ureda ili s ovog računala.
func kljuceviPrijave(ime string, k Klijent) []string {
	ime = strings.ToLower(ime)
	adresa := adresaZaOgranicenje(k)
	kljucevi := []string{kljucImeAdresa + ime + "|" + adresa, kljucAdresa + adresa}
	if k.KrozPosrednika {
		kljucevi = append(kljucevi, kljucIme+ime)
	}
	return kljucevi
}

// adresaZaOgranicenje: IPv4 adresa, a IPv6 izvana mreža /64, jer jedan
// priključak dobiva čitavu mrežu adresa i mogao bi ih mijenjati po pokušaju.
// U lokalnoj mreži svako računalo je svoja adresa, inače bi jedno zaključalo
// cijeli ured.
func adresaZaOgranicenje(k Klijent) string {
	a := k.Adresa
	if k.KrozPosrednika && a.IsValid() && a.Is6() && !a.Is4In6() {
		if p, err := a.Prefix(64); err == nil {
			return p.String()
		}
	}
	return k.String()
}

type attemptRecord struct {
	failures     int
	firstFailure time.Time
	blockedUntil time.Time
}

// loginLimiter pamti neuspjele prijave po ključu (ime, adresa, ime s adrese)
type loginLimiter struct {
	mu      sync.Mutex
	records map[string]*attemptRecord
	now     func() time.Time
	najvise int // granica broja ključeva
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{records: map[string]*attemptRecord{}, now: time.Now, najvise: najviseKljuceva}
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

// Fail bilježi neuspjeh; nakon najviše neuspjeha u prozoru ključ se blokira
func (l *loginLimiter) Fail(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for _, k := range keys {
		if k == "" {
			continue
		}
		p := granicaZa(k)
		rec, ok := l.records[k]
		if !ok || now.Sub(rec.firstFailure) > p.prozor {
			rec = &attemptRecord{firstFailure: now, blockedUntil: rec.blokiranDo()}
			l.records[k] = rec
		}
		rec.failures++
		if rec.failures >= p.najvise {
			rec.blockedUntil = now.Add(p.blokada)
		}
	}
	l.sweep(now)
}

// blokiranDo čuva blokadu koja još traje kad prozor brojanja istekne
func (rec *attemptRecord) blokiranDo() time.Time {
	if rec == nil {
		return time.Time{}
	}
	return rec.blockedUntil
}

// Reset briše brojače nakon uspješne prijave
func (l *loginLimiter) Reset(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		delete(l.records, k)
	}
}

// sweep čisti zastarjele zapise da mapa ne raste bez granice; kad ni to ne
// pomogne, izbacuje najstarije, prvo one koji nisu blokirani
func (l *loginLimiter) sweep(now time.Time) {
	if len(l.records) < 1000 && len(l.records) <= l.najvise {
		return
	}
	for k, rec := range l.records {
		if now.Sub(rec.firstFailure) > granicaZa(k).prozor && !rec.blockedUntil.After(now) {
			delete(l.records, k)
		}
	}
	if len(l.records) <= l.najvise {
		return
	}
	type zapis struct {
		kljuc    string
		blokiran bool
		prvi     time.Time
	}
	svi := make([]zapis, 0, len(l.records))
	for k, rec := range l.records {
		svi = append(svi, zapis{k, rec.blockedUntil.After(now), rec.firstFailure})
	}
	sort.Slice(svi, func(i, j int) bool {
		if svi[i].blokiran != svi[j].blokiran {
			return !svi[i].blokiran
		}
		return svi[i].prvi.Before(svi[j].prvi)
	})
	// izbaci desetinu ispod granice, da se rezanje ne ponavlja pri svakom neuspjehu
	cilj := l.najvise - l.najvise/10
	for _, z := range svi {
		if len(l.records) <= cilj {
			break
		}
		delete(l.records, z.kljuc)
	}
}

// Ponovni upis lozinke unutar prijave (promjena lozinke, potpisni ključ,
// lozinka e-pošte) je proročište za pogađanje kome je sesija ukradena: ključ
// reauth:+osoba dopušta tri kriva upisa u 15 minuta. Brojač je zajednički
// svim rukovateljima, pa se ne zaobilazi prelaskom na drugu stranicu.
var ponovnaLozinka = newLoginLimiter()

// kljucPonovneLozinke je ključ osobe; vrsta odvaja brojače (npr. posta, čiji
// poslužitelj sam zaključava račun nakon nekoliko krivih lozinki)
func kljucPonovneLozinke(vrsta, userID string) string {
	if vrsta == "" {
		return kljucPonovna + userID
	}
	return kljucPonovna + vrsta + ":" + userID
}

// ponovnaLozinkaDopustena javlja grešku dok je osoba blokirana
func ponovnaLozinkaDopustena(kljuc string) error {
	if blocked, wait := ponovnaLozinka.Blocked(kljuc); blocked {
		return fmt.Errorf("previše krivih lozinki; pokušajte ponovno za %d min", int(wait.Minutes())+1)
	}
	return nil
}

// ishodPonovneLozinke broji krivu lozinku, a točna briše brojač
func ishodPonovneLozinke(kljuc string, err error) {
	switch {
	case err == nil:
		ponovnaLozinka.Reset(kljuc)
	case errors.Is(err, service.ErrKrivaLozinka):
		ponovnaLozinka.Fail(kljuc)
	}
}

// clientIP vraća adresu klijenta za ograničenje pokušaja i zapis sesije;
// porijeklo zahtjeva određuje klijent.go
func clientIP(r *http.Request) string {
	return klijentIz(r).String()
}

// passwordChangeAllowed javlja smije li se putanja otvoriti dok korisnik
// još radi sa zadanom lozinkom: samo vlastiti profil, promjena lozinke,
// odjava i statika. Sve ostalo čeka dok lozinka nije postavljena.
func passwordChangeAllowed(path string) bool {
	switch path {
	case "/profile", "/profile/update", "/profile/change-password", "/logout", "/logo":
		return true
	}
	return strings.HasPrefix(path, "/static/")
}
