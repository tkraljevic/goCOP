package web

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/potpis"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// Ime s adrese: pet neuspjeha blokira to ime s te adrese, ali ne i ime s
// druge adrese — jedan napadač s pet pokušaja više ne zaključava tuđi račun
func TestPetNeuspjelihPrijavaBlokiraImeSAdrese(t *testing.T) {
	l := newLoginLimiter()
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }

	napadac := kp("Tomislav", "10.0.0.1", true)
	for i := 0; i < loginMaxAttempts-1; i++ {
		l.Fail(napadac...)
	}
	if blocked, _ := l.Blocked(napadac...); blocked {
		t.Fatal("blokada prije petog neuspjeha")
	}
	l.Fail(napadac...)
	blocked, wait := l.Blocked(napadac...)
	if !blocked || wait != loginBlock {
		t.Fatalf("nakon %d neuspjeha očekivana blokada od %v, dobiveno %v/%v", loginMaxAttempts, loginBlock, blocked, wait)
	}
	if blocked, _ := l.Blocked(kp("tomislav", "10.0.0.2", true)...); blocked {
		t.Error("vlasnik računa s druge adrese ne smije biti blokiran zbog tuđih pet pokušaja")
	}
	if blocked, _ := l.Blocked(kp("netko-drugi", "10.0.0.1", true)...); blocked {
		t.Error("adresa s pet neuspjeha još nije blokirana za druga imena")
	}

	now = now.Add(loginBlock + time.Second)
	if blocked, _ := l.Blocked(napadac...); blocked {
		t.Error("blokada mora isteći")
	}
}

// Adresa: dvadeset neuspjeha u 15 minuta blokira adresu za sva imena
func TestAdresaKojaMijenjaImenaSeBlokira(t *testing.T) {
	l := newLoginLimiter()
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	for i := 0; i < 20; i++ {
		if blocked, _ := l.Blocked(kp(fmt.Sprintf("ime%d", i), "10.0.0.1", true)...); blocked {
			t.Fatalf("adresa blokirana nakon %d neuspjeha", i)
		}
		l.Fail(kp(fmt.Sprintf("ime%d", i), "10.0.0.1", true)...)
	}
	if blocked, _ := l.Blocked(kp("novo-ime", "10.0.0.1", true)...); !blocked {
		t.Error("adresa s dvadeset neuspjeha mora biti blokirana i za novo ime")
	}
	if blocked, _ := l.Blocked(kp("novo-ime", "10.0.0.2", true)...); blocked {
		t.Error("druga adresa nije blokirana")
	}
}

// Ime: trideset neuspjeha u satu s različitih adresa blokira ime
func TestImeKojeNapadajuSMnogoAdresaSeBlokira(t *testing.T) {
	l := newLoginLimiter()
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	for i := 0; i < 30; i++ {
		adresa := fmt.Sprintf("10.0.%d.1", i/4) // četiri s adrese: ispod granice imena s adrese
		if blocked, _ := l.Blocked(kp("ana", adresa, true)...); blocked {
			t.Fatalf("ime blokirano nakon %d neuspjeha", i)
		}
		l.Fail(kp("ana", adresa, true)...)
		now = now.Add(time.Minute)
	}
	if blocked, _ := l.Blocked(kp("ana", "192.168.1.9", true)...); !blocked {
		t.Error("ime s trideset neuspjeha u satu mora biti blokirano s bilo koje adrese izvana")
	}
	// iz lokalne mreže i s ovog računala ime se ne zaključava: vlasnik se
	// prijavi i dok ga napadaju s interneta
	if blocked, _ := l.Blocked(kp("ana", "192.168.1.9", false)...); blocked {
		t.Error("napad izvana ne smije zaključati račun za lokalnu prijavu")
	}
}

// IPv6: jedan priključak ima čitavu mrežu /64 i ne smije pokušavati
// mijenjajući adresu
func TestIPv6AdreseIsteMrezeSeBrojeZajedno(t *testing.T) {
	l := newLoginLimiter()
	for i := 0; i < loginMaxAttempts; i++ {
		l.Fail(kp("ana", fmt.Sprintf("2001:db8:1:2::%x", i+1), true)...)
	}
	if blocked, _ := l.Blocked(kp("ana", "2001:db8:1:2::ffff", true)...); !blocked {
		t.Error("adrese iste mreže /64 broje se kao jedan klijent")
	}
	if blocked, _ := l.Blocked(kp("ana", "2001:db8:1:3::1", true)...); blocked {
		t.Error("druga mreža /64 je drugi klijent")
	}
}

// kp su ključevi prijave za ime s adrese; izvana = kroz tunel
func kp(ime, adresa string, izvana bool) []string {
	return kljuceviPrijave(ime, Klijent{Adresa: netip.MustParseAddr(adresa), KrozPosrednika: izvana})
}

func TestUspjesnaPrijavaBriseBrojac(t *testing.T) {
	l := newLoginLimiter()
	k := kp("ana", "1.1.1.1", true)
	l.Fail(k...)
	l.Fail(k...)
	l.Reset(k...)
	for i := 0; i < loginMaxAttempts-1; i++ {
		l.Fail(k...)
	}
	if blocked, _ := l.Blocked(k...); blocked {
		t.Error("nakon uspješne prijave brojanje kreće ispočetka")
	}
}

func TestNeuspjesiIzvanProzoraSeNeZbrajaju(t *testing.T) {
	l := newLoginLimiter()
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	k := kljucImeAdresa + "x|1.1.1.1"
	for i := 0; i < loginMaxAttempts-1; i++ {
		l.Fail(k)
	}
	now = now.Add(loginWindow + time.Minute)
	l.Fail(k)
	if blocked, _ := l.Blocked(k); blocked {
		t.Error("stari neuspjesi izvan prozora ne smiju brojati")
	}
}

// Mapa ima granicu: napadač s mnogo adresa ne puni memoriju, a blokirani
// ključevi ostaju dok ima neblokiranih za izbaciti
func TestOgranicenjeImaGranicuKljuceva(t *testing.T) {
	l := newLoginLimiter()
	l.najvise = 100
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	blokiran := kljucImeAdresa + "ana|10.0.0.1"
	for i := 0; i < loginMaxAttempts; i++ {
		l.Fail(blokiran)
	}
	for i := 0; i < 500; i++ {
		now = now.Add(time.Millisecond)
		l.Fail(kljucAdresa + fmt.Sprintf("203.0.%d.%d", i/250, i%250))
	}
	if n := len(l.records); n > l.najvise {
		t.Errorf("mapa ima %d ključeva, granica je %d", n, l.najvise)
	}
	if blocked, _ := l.Blocked(blokiran); !blocked {
		t.Error("blokiran ključ izbačen prije neblokiranih")
	}
	if _, ok := l.records[kljucAdresa+"203.0.1.249"]; !ok {
		t.Error("najnoviji ključ ne smije biti izbačen")
	}
}

// Ponovni upis lozinke: tri kriva upisa blokiraju osobu 15 minuta, ostale
// greške ne broje, a točna lozinka briše brojač
func TestPonovnaLozinkaTriKrivaUpisa(t *testing.T) {
	ponovnaLozinka = newLoginLimiter()
	t.Cleanup(func() { ponovnaLozinka = newLoginLimiter() })
	kljuc := kljucPonovneLozinke("", "osoba-1")
	kriva := fmt.Errorf("omotano: %w", service.ErrKrivaLozinka)

	ishodPonovneLozinke(kljuc, kriva)
	ishodPonovneLozinke(kljuc, kriva)
	ishodPonovneLozinke(kljuc, errors.New("nemate potpisni ključ")) // ne broji
	if err := ponovnaLozinkaDopustena(kljuc); err != nil {
		t.Fatalf("dva kriva upisa ne smiju blokirati: %v", err)
	}
	ishodPonovneLozinke(kljuc, nil) // točna briše
	for i := 0; i < 3; i++ {
		ishodPonovneLozinke(kljuc, kriva)
	}
	if err := ponovnaLozinkaDopustena(kljuc); err == nil || !strings.Contains(err.Error(), "previše krivih lozinki") {
		t.Errorf("nakon tri kriva upisa očekivana blokada, dobiveno %v", err)
	}
	if err := ponovnaLozinkaDopustena(kljucPonovneLozinke("", "osoba-2")); err != nil {
		t.Errorf("druga osoba nije blokirana: %v", err)
	}

	// otključavanje potpisnog ključa: blokirana osoba ne stiže do provjere
	pozvano := false
	u := &models.User{ID: uuid.New()}
	for i := 0; i < 3; i++ {
		_, _ = otkljucajUzOgranicenje(u, func() (*potpis.Potpisnik, error) { return nil, kriva })
	}
	_, err := otkljucajUzOgranicenje(u, func() (*potpis.Potpisnik, error) { pozvano = true; return nil, nil })
	if pozvano || err == nil {
		t.Errorf("nakon tri kriva otključavanja provjera se ne smije ni pokušati (pozvano=%v, err=%v)", pozvano, err)
	}
}

func TestAdresaIzaPosrednika(t *testing.T) {
	r := httptest.NewRequest("POST", "/login", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	if got := clientIP(r); got != "127.0.0.1" {
		t.Errorf("bez zaglavlja očekivan 127.0.0.1, dobiveno %s", got)
	}
	if klijentIz(r).KrozPosrednika {
		t.Error("izravan zahtjev nije prošao kroz posrednika")
	}
	// zadano se čita samo CF-Connecting-IP; lijevi kraj X-Forwarded-For piše klijent
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	if got := clientIP(r); got != "127.0.0.1" {
		t.Errorf("X-Forwarded-For se zadano ne čita, dobiveno %s", got)
	}
	if !klijentIz(r).KrozPosrednika {
		t.Error("zahtjev s X-Forwarded-For je prošao kroz posrednika")
	}
	r.Header.Set("CF-Connecting-IP", "198.51.100.9")
	if got := clientIP(r); got != "198.51.100.9" {
		t.Errorf("Cloudflare zaglavlje od pouzdanog posrednika, dobiveno %s", got)
	}
	r.Header.Set("CF-Connecting-IP", "nije-adresa")
	if got := clientIP(r); got != "127.0.0.1" {
		t.Errorf("neispravno zaglavlje se ne uvažava, dobiveno %s", got)
	}

	// s interneta izravno: zaglavlje se ne smije uvažiti, inače ga napadač mijenja po volji
	direct := httptest.NewRequest("POST", "/login", nil)
	direct.RemoteAddr = "203.0.113.200:4444"
	direct.Header.Set("X-Forwarded-For", "10.9.9.9")
	direct.Header.Set("CF-Connecting-IP", "10.8.8.8")
	if got := clientIP(direct); got != "203.0.113.200" {
		t.Errorf("izravno s interneta: zaglavlje podmetnuto, očekivan 203.0.113.200, dobiveno %s", got)
	}
	if !klijentIz(direct).KrozPosrednika {
		t.Error("podmetnuto zaglavlje posrednika čini zahtjev vanjskim")
	}
}

// Suženi popis: vjeruje se samo cloudflaredu, a iza nginxa X-Forwarded-For
// zdesna, preskačući pouzdane
func TestSuzeniPosrednici(t *testing.T) {
	p, err := NoviPosrednici([]string{"172.17.0.1", "10.0.0.0/8"}, "X-Forwarded-For")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.17.0.1:40000"
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.5, 10.1.1.1")
	r.Header.Set("X-Forwarded-Proto", "https")
	k := p.Klijent(r)
	if k.Adresa.String() != "203.0.113.5" || !k.HTTPS || !k.KrozPosrednika {
		t.Errorf("iza pouzdanog posrednika: %+v", k)
	}
	r.RemoteAddr = "192.168.1.50:40000" // izravno iz LAN-a, nije na popisu
	k = p.Klijent(r)
	if k.Adresa.String() != "192.168.1.50" || k.HTTPS {
		t.Errorf("nepouzdan posrednik: %+v", k)
	}
	if _, err := NoviPosrednici([]string{"nije"}, ""); err == nil {
		t.Error("neispravan unos mora biti greška")
	}

	// samo zaglavlje, bez popisa: posrednici su zadane mreže, kako piše u
	// gocop.toml, a ne nitko (inače bi svi s interneta bili jedna adresa)
	p, _ = NoviPosrednici(nil, "X-Forwarded-For")
	r.RemoteAddr = "172.17.0.1:40000"
	if k := p.Klijent(r); k.Adresa.String() != "203.0.113.5" {
		t.Errorf("samo zaglavlje bez popisa: %+v", k)
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

// Zaglavlje iz gocop.toml čini zahtjev vanjskim; popis samo s praznim
// stavkama su zadane mreže; IPv6 iz lokalne mreže broji se po adresi
func TestPosredniciRubniSlucajevi(t *testing.T) {
	p, _ := NoviPosrednici(nil, "True-Client-IP")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("True-Client-IP", "203.0.113.9")
	if k := p.Klijent(r); !k.KrozPosrednika || k.Adresa.String() != "203.0.113.9" {
		t.Errorf("zadano zaglavlje: %+v", k)
	}
	p, _ = NoviPosrednici([]string{"", "  "}, "")
	r = httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.17.0.1:1"
	r.Header.Set("CF-Connecting-IP", "198.51.100.1")
	if k := p.Klijent(r); k.Adresa.String() != "198.51.100.1" {
		t.Errorf("popis praznih stavki mora biti zadane mreže: %+v", k)
	}
	a := kljuceviPrijave("ana", Klijent{Adresa: netip.MustParseAddr("fd00::1")})
	b := kljuceviPrijave("ana", Klijent{Adresa: netip.MustParseAddr("fd00::2")})
	if a[1] == b[1] {
		t.Error("dva računala u lokalnoj IPv6 mreži su dvije adrese")
	}
}
