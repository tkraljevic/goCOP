package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/service"
)

// Istodobne prijave istim imenom s iste adrese: granica pokušaja vrijedi i
// kad svi zahtjevi stignu u istom trenutku, jer se pokušaj broji prije
// provjere lozinke
func TestIstodobnePrijaveNePrelazeGranicu(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.racun("ana", "anina-lozinka", false, false)

	const zahtjeva = 40
	var provjereno atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < zahtjeva; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if w := o.prijava(izravno, "ana", fmt.Sprintf("kriva-%d", i)); w.Code != http.StatusTooManyRequests {
				provjereno.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if n := provjereno.Load(); n > loginMaxAttempts {
		t.Fatalf("lozinka je provjerena %d puta, granica je %d", n, loginMaxAttempts)
	}
	if w := o.prijava(izravno, "ana", "anina-lozinka"); w.Code != http.StatusTooManyRequests {
		t.Errorf("nakon granice ni točna lozinka s iste adrese ne prolazi: %d", w.Code)
	}

	// kontrola: s druge adrese u uredu točna lozinka i dalje otvara sesiju
	druga := func(r *http.Request) *http.Request {
		r.RemoteAddr = "192.168.1.51:40000"
		r.Host = adresaCvoraTesta
		return r
	}
	if w := o.prijava(druga, "ana", "anina-lozinka"); w.Code != http.StatusSeeOther || kolacicSesije(w) == nil {
		t.Errorf("prijava s druge adrese: %d %q", w.Code, w.Body.String())
	}
}

// Točna lozinka ne troši pokušaje: ni mnogo uzastopnih prijava s iste
// adrese ne dođe do granice adrese
func TestTocnaPrijavaNeTrosiPokusaje(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.racun("ana", "anina-lozinka", false, false)
	for i := 0; i < granicaZa(kljucAdresa).najvise+2; i++ {
		if w := o.prijava(izravno, "ana", "anina-lozinka"); w.Code != http.StatusSeeOther {
			t.Fatalf("%d. prijava točnom lozinkom: %d %q", i+1, w.Code, w.Body.String())
		}
	}
	// i deaktiviran račun s točnom lozinkom ostaje brojen kao neuspjeh
	o.racun("ivo", "ivina-lozinka", false, false)
	u, err := o.repo.GetUserByUsername("ivo")
	if err != nil {
		t.Fatal(err)
	}
	u.IsActive = false
	if err := o.repo.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < loginMaxAttempts; i++ {
		o.prijava(izravno, "ivo", "ivina-lozinka")
	}
	if w := o.prijava(izravno, "ivo", "ivina-lozinka"); w.Code != http.StatusTooManyRequests {
		t.Errorf("deaktiviran račun: %d", w.Code)
	}
}

// Ponovni upis lozinke unutar prijave: istodobni upisi ne prelaze tri kriva
func TestIstodobnaPonovnaLozinka(t *testing.T) {
	ponovnaLozinka = newLoginLimiter()
	t.Cleanup(func() { ponovnaLozinka = newLoginLimiter() })
	kljuc := kljuceviLozinkeRacuna(context.Background(), "osoba-1")

	var provjera atomic.Int32
	kriva := func() error {
		provjera.Add(1)
		time.Sleep(20 * time.Millisecond)
		return service.ErrKrivaLozinka
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = uzGranicuPonovne(kljuc, kriva) // broji se koliko je provjera stiglo do lozinke
		}()
	}
	wg.Wait()
	if n := provjera.Load(); n > int32(granicaZa(kljuc[0]).najvise) {
		t.Fatalf("lozinka je provjerena %d puta, granica je %d", n, granicaZa(kljuc[0]).najvise)
	}

	// kontrola: točna lozinka i greška koja nije kriva lozinka ne troše upise
	drugi := kljuceviLozinkeRacuna(context.Background(), "osoba-2")
	for i := 0; i < 10; i++ {
		greska := error(nil)
		if i%2 == 1 {
			greska = errTestNijeLozinka
		}
		if err := uzGranicuPonovne(drugi, func() error { return greska }); err != greska {
			t.Fatalf("%d. upis: %v", i+1, err)
		}
	}
}

var errTestNijeLozinka = fmt.Errorf("nemate potpisni ključ")

// Rezerviraj ne broji ništa dok je bilo koji ključ blokiran, a VratiSve
// skida blokadu koju je postavio rezervirani pokušaj
func TestRezervacijaPokusaja(t *testing.T) {
	l := newLoginLimiter()
	sad := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return sad }
	kljucevi := []string{kljucImeAdresa + "ana|192.168.1.50", kljucAdresa + "192.168.1.50"}

	for i := 0; i < loginMaxAttempts-1; i++ {
		if ok, _ := l.Rezerviraj(kljucevi...); !ok {
			t.Fatalf("%d. rezervacija odbijena", i+1)
		}
	}
	if ok, _ := l.Rezerviraj(kljucevi...); !ok {
		t.Fatal("peta rezervacija mora proći")
	}
	if ok, wait := l.Rezerviraj(kljucevi...); ok || wait != loginBlock {
		t.Fatalf("šesta: ok=%v, čeka %v", ok, wait)
	}
	if n := l.records[kljucevi[1]].failures; n != loginMaxAttempts {
		t.Errorf("odbijena rezervacija ne smije brojiti: adresa ima %d", n)
	}
	l.VratiSve(kljucevi...)
	if blokiran, _ := l.Blocked(kljucevi...); blokiran {
		t.Error("vraćen pokušaj mora skinuti blokadu koju je postavio")
	}
	l.Fail("")
	if _, ok := l.records[""]; ok {
		t.Error("prazan ključ se ne broji")
	}
}

// Istodobni krivi PIN-ovi s iste adrese za više osoba ne prelaze granicu
// adrese: unos se broji prije provjere koda. Sat drugog koraka namjerno
// usporava provjeru, da svi unosi budu u njoj u isto vrijeme.
func TestIstodobniKriviPINoviNePrelazeGranicuAdrese(t *testing.T) {
	o := novaOkolinaPIN(t)
	tunel := krozTunel("198.51.100.7")
	const osoba, poOsobi = 5, 5 // 25 unosa, granica adrese je 20
	cekanja := make([]*http.Cookie, osoba)
	pinovi := make([]string, osoba)
	for i := range cekanja {
		ime := fmt.Sprintf("osoba%d", i)
		o.osoba(ime, "lozinka-"+ime, ime+"@voda.hr", false)
		cekanja[i] = naCekanju(t, o.prijava(tunel, ime, "lozinka-"+ime))
		pinovi[i] = o.posta.pin(t)
	}
	sad := o.sad
	o.dk.SetSat(func() time.Time { time.Sleep(5 * time.Millisecond); return sad })

	var odbijeno atomic.Int32
	var wg sync.WaitGroup
	for i := range cekanja {
		for j := 0; j < poOsobi; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if w := o.upisi(tunel, cekanja[i], krivPIN(pinovi[i]), false); w.Code == http.StatusTooManyRequests {
					odbijeno.Add(1)
				}
			}()
		}
	}
	wg.Wait()
	if n, zelim := odbijeno.Load(), int32(osoba*poOsobi-granicaZa(kljucAdresa).najvise); n < zelim {
		t.Fatalf("odbijeno %d unosa, a granica adrese traži barem %d", n, zelim)
	}
}

// Točan PIN, kao i točna lozinka, ne troši pokušaj adrese
func TestTocanPINNeTrosiPokusajAdrese(t *testing.T) {
	o := novaOkolinaPIN(t)
	o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	tunel := krozTunel("198.51.100.7")
	cek := naCekanju(t, o.prijava(tunel, "ana", "anina-lozinka"))
	pin := o.posta.pin(t)
	o.upisi(tunel, cek, krivPIN(pin), false)
	if w := o.upisi(tunel, cek, pin, false); kolacicSesije(w) == nil {
		t.Fatalf("točan PIN: %d %q", w.Code, w.Body.String())
	}
	kljuc := kljucAdresa + oznakaZaglavlja + "198.51.100.7"
	o.authH.limiter.mu.Lock()
	defer o.authH.limiter.mu.Unlock()
	if rec := o.authH.limiter.records[kljuc]; rec == nil || rec.failures != 1 {
		t.Errorf("adresa mora imati samo krivi PIN: %+v", rec)
	}
}

// Krivi upisi lozinke iz jedne prijave ne zaključavaju promjenu lozinke u
// drugoj prijavi iste osobe
func TestPonovnaLozinkaPoPrijavi(t *testing.T) {
	o := novaOkolinaPrijave(t)
	ana := o.racun("ana", "anina-lozinka", false, false)
	jedna, druga := uuid.New(), uuid.New()
	for i := 0; i < 3; i++ {
		o.promjenaLozinke(ana, jedna, "kriva", "nova-lozinka-2026")
	}
	if loc := o.promjenaLozinke(ana, jedna, "anina-lozinka", "nova-lozinka-2026"); !strings.Contains(loc, "previše krivih lozinki") {
		t.Fatalf("nakon tri kriva upisa ta prijava mora čekati: %q", loc)
	}
	if loc := o.promjenaLozinke(ana, druga, "anina-lozinka", "nova-lozinka-2026"); strings.Contains(loc, "error=") {
		t.Fatalf("promjena lozinke iz druge prijave: %q", loc)
	}
}

// Sve prijave osobe zajedno imaju granicu najviseKrivihOsobe
func TestPonovnaLozinkaGranicaOsobe(t *testing.T) {
	o := novaOkolinaPrijave(t)
	ana := o.racun("ana", "anina-lozinka", false, false)
	for i := 0; i < najviseKrivihOsobe; i++ {
		o.promjenaLozinke(ana, uuid.New(), "kriva", "treca-lozinka-2026")
	}
	if loc := o.promjenaLozinke(ana, uuid.New(), "anina-lozinka", "treca-lozinka-2026"); !strings.Contains(loc, "previše krivih lozinki") {
		t.Errorf("nakon %d krivih upisa iz više prijava osoba mora čekati: %q", najviseKrivihOsobe, loc)
	}
}
