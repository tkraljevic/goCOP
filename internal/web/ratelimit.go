package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"gocop/internal/service"
)

// Ograničenje pokušaja prijave. Bez toga bi javni čvor dopuštao pogađanje
// lozinki u petlji. Broji se na tri
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
	kljucPonovna   = "reauth:" // ponovni upis lozinke unutar prijave (e-pošta)
	// prijave na poslužitelj e-pošte jednim računom u domeni (vidi
	// kljucPrijaveAD); počinje s kljucPonovna, ali ima svoju granicu
	kljucPonovnaAD = kljucPonovna + "ad:"
	// ponovni upis lozinke računa: po prijavi (sesiji) i po osobi, vidi
	// kljuceviLozinkeRacuna
	kljucPonovnaSesija = "reauth-sesija:"
	kljucPonovnaOsoba  = "reauth-osoba:"
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
	case strings.HasPrefix(kljuc, kljucPonovnaAD):
		return granicaKljuca{najviseKrivihAD, 30 * time.Minute, 30 * time.Minute}
	case strings.HasPrefix(kljuc, kljucPonovna), strings.HasPrefix(kljuc, kljucPonovnaSesija):
		return granicaKljuca{3, 15 * time.Minute, 15 * time.Minute}
	case strings.HasPrefix(kljuc, kljucPonovnaOsoba):
		return granicaKljuca{najviseKrivihOsobe, 15 * time.Minute, 15 * time.Minute}
	}
	return granicaKljuca{loginMaxAttempts, loginWindow, loginBlock}
}

// kljuceviPrijave su ključevi jednog pokušaja prijave. Ključ samog imena
// broji samo pokušaje izvana, kroz tunel ili s javne adrese: inače bi
// pokušaji s interneta s nekoliko adresa zaključali račun i za prijavu iz
// ureda ili s ovog računala.
func kljuceviPrijave(ime string, k Klijent) []string {
	ime = strings.ToLower(ime)
	adresa := adresaZaOgranicenje(k)
	kljucevi := []string{kljucImeAdresa + ime + "|" + adresa, kljucAdresa + adresa}
	if k.Izvana() {
		kljucevi = append(kljucevi, kljucIme+ime)
	}
	return kljucevi
}

// oznakaZaglavlja stoji ispred adrese svakog zahtjeva koji nosi zaglavlje
// posrednika, i kad se adresa iz zaglavlja nije pročitala (tada je to
// adresa posrednika). Takva se adresa broji odvojeno od iste adrese kad
// zahtjev stigne izravno, pa pokušaji kroz posrednika ne zaključavaju
// računalo koje se prijavljuje izravno.
const oznakaZaglavlja = "kroz:"

// oznakaTudjegImena stoji ispred adrese zahtjeva na ime koje ne vodi na ovo
// računalo (Klijent.TudjeIme). Pokušaji na takvo ime broje se odvojeno, pa
// ne zaključavaju isto računalo kad se prijavljuje na adresu ili ime čvora.
const oznakaTudjegImena = "ime:"

// adresaZaOgranicenje: IPv4 adresa, a IPv6 izvana mreža /64, jer jedan
// priključak dobiva čitavu mrežu adresa i mogao bi ih mijenjati po pokušaju.
// U lokalnoj mreži svako računalo je svoja adresa, inače bi jedno zaključalo
// cijeli ured; isto vrijedi za Tailscale, gdje računala dijele početak adrese.
// Mrežu određuje porijeklo zahtjeva, ne ime na koje je upućen.
func adresaZaOgranicenje(k Klijent) string {
	a := k.Adresa
	adresa := k.String()
	poPorijeklu := k
	poPorijeklu.TudjeIme = false
	if poPorijeklu.Izvana() && !k.izravnoNaTailscale() && a.IsValid() && a.Is6() && !a.Is4In6() {
		if p, err := a.Prefix(64); err == nil {
			adresa = p.String()
		}
	}
	if k.TudjeIme {
		adresa = oznakaTudjegImena + adresa
	}
	if k.KrozPosrednika {
		return oznakaZaglavlja + adresa
	}
	return adresa
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
	d := l.blokada(l.now(), keys)
	return d > 0, d
}

// blokada je najdulje preostalo čekanje među ključevima; 0 kad nijedan
// nije blokiran. Zove se pod bravom.
func (l *loginLimiter) blokada(now time.Time, keys []string) time.Duration {
	var longest time.Duration
	for _, k := range keys {
		if rec, ok := l.records[k]; ok && rec.blockedUntil.After(now) {
			longest = max(longest, rec.blockedUntil.Sub(now))
		}
	}
	return longest
}

// Fail bilježi neuspjeh; nakon najviše neuspjeha u prozoru ključ se blokira
func (l *loginLimiter) Fail(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.zabiljezi(l.now(), keys)
}

// Rezerviraj unaprijed broji pokušaj na svim ključevima, pod istom bravom
// pod kojom provjerava blokadu, pa istodobni zahtjevi ne prolaze granicu
// prije nego što se prvi neuspjeh upiše. Kad je neki ključ blokiran, ne
// broji ništa i javi koliko treba čekati. Točna lozinka pokušaj vraća
// (VratiSve), kriva ga ostavlja kao neuspjeh.
func (l *loginLimiter) Rezerviraj(keys ...string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if d := l.blokada(now, keys); d > 0 {
		return false, d
	}
	l.zabiljezi(now, keys)
	return true, 0
}

// VratiSve vraća po jedan pokušaj na svakom ključu (Rezerviraj)
func (l *loginLimiter) VratiSve(keys ...string) {
	for _, k := range keys {
		l.Vrati(k, 1)
	}
}

// zabiljezi broji neuspjeh na svakom ključu; zove se pod bravom
func (l *loginLimiter) zabiljezi(now time.Time, keys []string) {
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

// Naplati unaprijed broji n pokušaja na ključu, ali samo ako ih granica u
// prozoru još dopušta; inače ne broji ništa i javi koliko treba čekati.
// Za radnje koje jednim zahtjevom troše više pokušaja (npr. prijava na
// poslužitelj e-pošte s imenom, pa s domenom).
func (l *loginLimiter) Naplati(kljuc string, n int) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	p := granicaZa(kljuc)
	rec := l.records[kljuc]
	if rec != nil && rec.blockedUntil.After(now) {
		return false, rec.blockedUntil.Sub(now)
	}
	if rec == nil || now.Sub(rec.firstFailure) > p.prozor {
		rec = &attemptRecord{firstFailure: now, blockedUntil: rec.blokiranDo()}
	}
	if rec.failures+n > p.najvise {
		return false, rec.firstFailure.Add(p.prozor).Sub(now)
	}
	rec.failures += n
	if rec.failures >= p.najvise {
		rec.blockedUntil = now.Add(p.blokada)
	}
	l.records[kljuc] = rec
	l.sweep(now)
	return true, 0
}

// Vrati poništava n pokušaja koje je zahtjev unaprijed naplatio (Fail ili
// Naplati), npr. kad se pokazalo da lozinka nije bila kriva; blokadu koju
// je taj pokušaj postavio skida. Ostale pokušaje u prozoru ne dira.
func (l *loginLimiter) Vrati(kljuc string, n int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.records[kljuc]
	if !ok || n <= 0 {
		return
	}
	rec.failures -= n
	if rec.failures < 0 {
		rec.failures = 0
	}
	if rec.failures < granicaZa(kljuc).najvise {
		rec.blockedUntil = time.Time{}
	}
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
// lozinka e-pošte) je proročište za pogađanje kome je sesija ukradena.
// Lozinka računa broji se po prijavi, tri kriva upisa u 15 minuta, i po
// osobi, najviseKrivihOsobe: krivi upisi iz jedne prijave ne zaključavaju
// promjenu lozinke i potpis u drugim prijavama iste osobe. Lozinka e-pošte broji
// se po osobi (kljucPonovneLozinke). Brojač je zajednički svim
// rukovateljima, pa se ne zaobilazi prelaskom na drugu stranicu.
var ponovnaLozinka = newLoginLimiter()

// najviseKrivihOsobe su krivi upisi lozinke računa jedne osobe iz svih
// njezinih prijava zajedno u 15 minuta
const najviseKrivihOsobe = 10

// kljucPonovneLozinke je ključ osobe za lozinku koja nije lozinka računa;
// vrsta odvaja brojače (npr. posta, čiji poslužitelj sam zaključava račun
// nakon nekoliko krivih lozinki)
func kljucPonovneLozinke(vrsta, userID string) string {
	return kljucPonovna + vrsta + ":" + userID
}

// kljuceviLozinkeRacuna su ključevi ponovnog upisa lozinke računa: prijava
// iz koje je upisana (sesija iz konteksta zahtjeva) i osoba. Bez sesije u
// kontekstu sve prijave osobe dijele jedan ključ prijave.
func kljuceviLozinkeRacuna(ctx context.Context, userID string) []string {
	prijava := "osoba:" + userID
	if id, ok := ctx.Value(contextKeySession).(uuid.UUID); ok && id != uuid.Nil {
		prijava = id.String()
	}
	return []string{kljucPonovnaSesija + prijava, kljucPonovnaOsoba + userID}
}

// rezervirajPonovnuLozinku unaprijed broji jedan upis lozinke na svakom
// ključu; dok je neki blokiran, javlja grešku i ne broji ništa. Svaki uspjeh
// mora imati svoj ishod (ishodRezervacije ili ponovnaLozinka.VratiSve),
// inače upis ostaje brojen kao kriv.
func rezervirajPonovnuLozinku(kljucevi ...string) error {
	if ok, wait := ponovnaLozinka.Rezerviraj(kljucevi...); !ok {
		return fmt.Errorf("previše krivih lozinki; pokušajte ponovno za %d min", int(wait.Minutes())+1)
	}
	return nil
}

// Prijava na poslužitelj e-pošte tvrtke (Exchange) troši pokušaje računa u
// domeni, koji nakon nekoliko krivih lozinki zaključava račun (e-pošta,
// Windows, VPN). Broje se zato prijave, ne obrasci, i to po računu u domeni,
// zajednički za lozinku osobnog sandučića (/profile/posta) i za račun koji
// šalje PIN (/administracija/posta/pin, i „Uzmi moj račun e-pošte”):
// najviše najviseKrivihAD u pola sata. Uspjela prijava briše brojač.
const najviseKrivihAD = 3

// imeAD je ime računa u domeni bez domene i bez @dijela, malim slovima
// (tkraljevic, VODA\tkraljevic i tkraljevic@voda.hr su isti račun)
func imeAD(korisnik string) string {
	ime := strings.TrimSpace(korisnik)
	if i := strings.LastIndex(ime, `\`); i >= 0 {
		ime = ime[i+1:]
	}
	if i := strings.Index(ime, "@"); i > 0 {
		ime = ime[:i]
	}
	return strings.ToLower(ime)
}

// kljucPrijaveAD je ključ računa u domeni koji upisuje osoba userID. Osoba
// je dio ključa, inače bi svatko tko tri puta upiše tuđe ime s krivom
// lozinkom vlasniku pola sata zatvorio oba obrasca.
func kljucPrijaveAD(userID, korisnik string) string {
	return kljucPonovnaAD + userID + ":" + imeAD(korisnik)
}

// prijavaBezDomene: ime bez dijela DOMENA\; poslužitelj ga tada odbije pa
// goCOP pokuša i s domenom, što je još jedna prijava istog računa
func prijavaBezDomene(korisnik string) bool {
	return !strings.Contains(korisnik, `\`)
}

// naplatiPrijaveAD unaprijed broji n prijava računa korisnik, koji upisuje
// osoba userID, na poslužitelj e-pošte; kad ih granica više ne dopušta,
// vraća grešku i ne broji ništa. Vraća ključ, da ga uspjela prijava obriše
// (ishodPrijaveAD).
func naplatiPrijaveAD(userID, korisnik string, n int) (string, error) {
	ime := imeAD(korisnik)
	if n <= 0 || ime == "" {
		return "", nil
	}
	kljuc := kljucPrijaveAD(userID, korisnik)
	if ok, wait := ponovnaLozinka.Naplati(kljuc, n); !ok {
		poruka := fmt.Sprintf("previše prijava na poslužitelj e-pošte računom %s; pokušajte ponovno za %d min. "+
			"Svaka kriva lozinka broji se u domeni, koja nakon nekoliko zaključava račun", ime, int(wait.Minutes())+1)
		if prijavaBezDomene(korisnik) {
			poruka += `. Upišite ime s domenom (npr. VODA\ime): tada je jedan upis jedna prijava`
		}
		return "", errors.New(poruka)
	}
	return kljuc, nil
}

// ishodPrijaveAD: kad je poslužitelj primio lozinku (i kad kasniji korak
// obrasca nije prošao, service.PrijavaProsla), brojač tog računa u domeni
// se briše, a osobi se vraća samo pokušaj koji je ovaj upis naplatio.
// Osobni brojač se ne briše: inače bi vlastita točna lozinka između
// krivih upisa tuđih imena brisala granicu, i goCOP bi postao alat za
// raspršeno pogađanje lozinki u domeni.
func ishodPrijaveAD(prosla bool, kljucAD, kljucOsobe string) {
	if !prosla {
		return
	}
	if kljucAD != "" {
		ponovnaLozinka.Reset(kljucAD)
	}
	if kljucOsobe != "" {
		ponovnaLozinka.Vrati(kljucOsobe, 1)
	}
}

// ishodRezervacije: točna lozinka briše brojač, kriva ostaje brojena, a
// druga greška (npr. osoba nema potpisni ključ) vraća upis koji je
// rezervirajPonovnuLozinku unaprijed izbrojio
func ishodRezervacije(err error, kljucevi ...string) {
	switch {
	case err == nil:
		ponovnaLozinka.Reset(kljucevi...)
	case !errors.Is(err, service.ErrKrivaLozinka):
		ponovnaLozinka.VratiSve(kljucevi...)
	}
}

// uzGranicuPonovne provjerava lozinku osobe uz granicu krivih upisa: upis se
// broji prije provjere, pa istodobni zahtjevi ne zaobilaze granicu
func uzGranicuPonovne(kljucevi []string, provjeri func() error) error {
	if err := rezervirajPonovnuLozinku(kljucevi...); err != nil {
		return err
	}
	err := provjeri()
	ishodRezervacije(err, kljucevi...)
	return err
}

// clientIP vraća adresu klijenta za ograničenje pokušaja i zapis sesije;
// porijeklo zahtjeva određuje klijent.go
func clientIP(r *http.Request) string {
	return klijentIz(r).String()
}

// passwordChangeAllowed javlja smije li se putanja otvoriti dok korisnik
// još radi s privremenom lozinkom: samo vlastiti profil, promjena lozinke,
// odjava i statika. Sve ostalo čeka dok lozinka nije postavljena.
func passwordChangeAllowed(path string) bool {
	switch path {
	case "/profile", "/profile/update", "/profile/change-password", "/logout", "/logo", "/tema.css":
		return true
	}
	return strings.HasPrefix(path, "/static/")
}
