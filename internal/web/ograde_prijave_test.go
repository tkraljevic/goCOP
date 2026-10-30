package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// lanSaZaglavljem je zahtjev s računala u lokalnoj mreži koji sam nosi
// zaglavlje posrednika s tuđom adresom
func lanSaZaglavljem(adresa string) func(*http.Request) *http.Request {
	return func(r *http.Request) *http.Request {
		r.RemoteAddr = "192.168.1.50:40000"
		r.Header.Set(ZaglavljeCloudflare, adresa)
		return r
	}
}

func izravnoS(adresa string) func(*http.Request) *http.Request {
	return func(r *http.Request) *http.Request {
		r.RemoteAddr = adresa
		return r
	}
}

// Krive prijave koje u zaglavlju nose adresu drugog računala ne zaključavaju
// to računalo kad se prijavljuje izravno. Adresa iz zaglavlja i dalje se
// broji: nakon dvadeset krivih ista adresa kroz posrednika čeka.
func TestAdresaIzZaglavljaNeZakljucavaIzravnogKlijenta(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.racun("ana", "anina-lozinka", false, false)
	for _, kolega := range []string{"192.168.1.20", "127.0.0.1"} {
		for i := 0; i < 20; i++ {
			o.prijava(lanSaZaglavljem(kolega), fmt.Sprintf("ime%d", i), "kriva-lozinka")
		}
		if w := o.prijava(lanSaZaglavljem(kolega), "ana", "anina-lozinka"); w.Code != http.StatusTooManyRequests {
			t.Errorf("%s kroz zaglavlje nakon dvadeset krivih: %d", kolega, w.Code)
		}
		if w := o.prijava(izravnoS(kolega+":5000"), "ana", "anina-lozinka"); w.Code != http.StatusSeeOther || kolacicSesije(w) == nil {
			t.Errorf("%s izravno ne smije biti zaključan: %d", kolega, w.Code)
		}
	}
}

// Zahtjev koji nosi samo zaglavlje posrednika koje program ne čita (npr.
// X-Forwarded-For, a postavka je CF-Connecting-IP) broji se kao zahtjev
// kroz posrednika, ne pod adresom posrednika: ne zaključava preglednik na
// ovom računalu
func TestNeprocitanoZaglavljeNeZakljucavaOvoRacunalo(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.racun("ana", "anina-lozinka", false, false)
	samoXFF := func(r *http.Request) *http.Request {
		r.RemoteAddr = "127.0.0.1:5555"
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		return r
	}
	for i := 0; i < 20; i++ {
		o.prijava(samoXFF, fmt.Sprintf("ime%d", i), "kriva-lozinka")
	}
	if w := o.prijava(samoXFF, "ana", "anina-lozinka"); w.Code != http.StatusTooManyRequests {
		t.Errorf("kroz posrednika nakon dvadeset krivih: %d", w.Code)
	}
	if w := o.prijava(izravnoS("127.0.0.1:5000"), "ana", "anina-lozinka"); w.Code != http.StatusSeeOther || kolacicSesije(w) == nil {
		t.Errorf("ovo računalo izravno ne smije biti zaključano: %d", w.Code)
	}
}

// Izravan zahtjev s javne adrese, bez tunela, broji se kao i kroz tunel:
// ime se zaključava nakon trideset krivih s različitih adresa, a IPv6 se
// broji po mreži /64. Iz lokalne mreže ime se i dalje prijavljuje.
func TestJavnaAdresaBezPosrednikaBrojiIme(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.racun("ana", "anina-lozinka", false, false)
	for i := 0; i < 30; i++ {
		o.prijava(izravnoS(fmt.Sprintf("[2001:db8:%x::1]:5000", i)), "ana", "kriva-lozinka")
	}
	if w := o.prijava(izravnoS("[2001:db8:ff::1]:5000"), "ana", "anina-lozinka"); w.Code != http.StatusTooManyRequests {
		t.Errorf("ime s trideset krivih s javnih adresa: %d", w.Code)
	}
	if w := o.prijava(izravno, "ana", "anina-lozinka"); w.Code != http.StatusSeeOther {
		t.Errorf("iz lokalne mreže ime se prijavljuje: %d", w.Code)
	}

	p := ZadaniPosrednici()
	kljuc := func(adresa string) string {
		return kljuceviPrijave("ivo", p.Klijent(izravnoS(adresa)(httptest.NewRequest(http.MethodPost, "/login", nil))))[1]
	}
	if kljuc("[2001:db8:1:2::1]:5000") != kljuc("[2001:db8:1:2::ffff]:5000") {
		t.Error("javne IPv6 adrese iste mreže /64 broje se zajedno i bez posrednika")
	}
	if kljuc("[fd00::1]:5000") == kljuc("[fd00::2]:5000") {
		t.Error("računala u lokalnoj IPv6 mreži broje se odvojeno")
	}
}

// Tailscale: računala dijele početak adrese, pa se ne broje po mreži /64
func TestTailscaleSeBrojiPoAdresi(t *testing.T) {
	klijent := func(adresa string) Klijent {
		k := ZadaniPosrednici().Klijent(izravnoS("[" + adresa + "]:5000")(httptest.NewRequest(http.MethodPost, "/login", nil)))
		k.NaTailscale = true
		return k
	}
	a, b := kljuceviPrijave("ana", klijent("fd7a:115c:a1e0::1")), kljuceviPrijave("ana", klijent("fd7a:115c:a1e0::2"))
	if a[1] == b[1] {
		t.Errorf("dva računala na Tailscaleu su dvije adrese: %q", a[1])
	}
	if len(a) != 3 {
		t.Errorf("izvana preko Tailscalea broji se i ime: %v", a)
	}
}

// Moduli: stranice i izvozi prognoza, izvješća, sredstava i firmi te
// ucrtavanje objekta ne otvaraju se bez modula, ni izravnom adresom
func TestModulPutanjeIzvozaIAPIja(t *testing.T) {
	for put, modul := range map[string]string{
		"/prognoze":                             models.ModuleReadings,
		"/prognoze.xlsx":                        models.ModuleReadings,
		"/prognoze/podaci/batina":               models.ModuleReadings,
		"/izvjesca":                             models.ModuleJournals,
		"/izvjesca/novo":                        models.ModuleJournals,
		"/sektorsko-izvjesce/novo":              models.ModuleJournals,
		"/sredstva":                             models.ModuleRegisters,
		"/sredstva/mts.xlsx":                    models.ModuleRegisters,
		"/firme.csv":                            models.ModuleRegisters,
		"/api/structures/geometry":              models.ModuleRegisters,
		"/firmeX":                               "",
		"/administracija/uvoz-niza.xlsx":        models.ModuleAdmin,
		"/administracija/uvoz-niza/pregled":     "",
		"/organizacija/sektori.csv":             models.ModuleRegisters,
		"/dashboard":                            "",
		"/profile":                              "",
		"/podaci/brojke":                        "",
		"/administracija/uvoz-izvora/hidroview": "",
	} {
		if got := moduleForPath(put); got != modul {
			t.Errorf("%s: modul %q, a treba %q", put, got, modul)
		}
	}
}

// Kroz pravu provjeru prijave: bez modula 403 i zahtjev ne stiže do
// rukovatelja; s modulom i administratoru prolazi
func TestBezModulaIzravnaAdresaSeOdbija(t *testing.T) {
	o := novaOkolinaPrijave(t)
	moduli := repository.NewModuleRepository(o.baza, ledger.New(o.baza, "test"))
	bez := o.osoba("pperic", "perina-lozinka", "pperic@example.com", false)
	if err := moduli.SetUserOverride(context.Background(), bez.ID.String(), nil,
		[]string{models.ModuleReadings, models.ModuleRegisters, models.ModuleJournals}); err != nil {
		t.Fatal(err)
	}
	o.osoba("aivic", "anina-lozinka", "aivic@example.com", false) // zadano: Teren, Očitanja, Registri
	o.osoba("sef", "sefova-lozinka", "sef@example.com", true)
	sesija := func(ime, lozinka string) string {
		s, _, err := o.auth.Login(ime, lozinka, "192.168.1.50", "test")
		if err != nil {
			t.Fatal(err)
		}
		return s.ID.String()
	}
	pperic, aivic, sef := sesija("pperic", "perina-lozinka"), sesija("aivic", "anina-lozinka"), sesija("sef", "sefova-lozinka")

	for _, x := range []struct {
		metoda, put string
		vidi        bool // aivic, sa zadanim modulima
	}{
		{http.MethodGet, "/prognoze.xlsx", true},
		{http.MethodGet, "/prognoze", true},
		{http.MethodGet, "/firme.csv", true},
		{http.MethodGet, "/sredstva", true},
		{http.MethodPost, "/api/structures/geometry", true},
		{http.MethodGet, "/izvjesca", false},
		{http.MethodGet, "/sektorsko-izvjesce/novo", false},
	} {
		z := &mwZapis{}
		if w := mwZahtjev(mwPosluzitelj(o, z), x.metoda, x.put, pperic); w.Code != http.StatusForbidden || z.pozvan {
			t.Errorf("%s bez modula: %d, rukovatelj pozvan %v", x.put, w.Code, z.pozvan)
		}
		z = &mwZapis{}
		w := mwZahtjev(mwPosluzitelj(o, z), x.metoda, x.put, aivic)
		if prosao := w.Code == http.StatusNoContent && z.pozvan; prosao != x.vidi {
			t.Errorf("%s sa zadanim modulima: %d, a treba prolaz=%v", x.put, w.Code, x.vidi)
		}
		z = &mwZapis{}
		if w := mwZahtjev(mwPosluzitelj(o, z), x.metoda, x.put, sef); w.Code != http.StatusNoContent || !z.pozvan {
			t.Errorf("%s administratoru: %d", x.put, w.Code)
		}
	}
}

// Brojke podataka nose najviše izmjerene vodostaje, pa ih vidi samo
// prijavljeni, bez obzira na module
func TestBrojkePodatakaSamoPrijavljenima(t *testing.T) {
	o := novaOkolinaPrijave(t)
	moduli := service.NewModuleService(repository.NewModuleRepository(o.baza, ledger.New(o.baza, "test")))
	s, err := NewServer("", o.auth, nil, nil, nil, nil, nil, nil, nil, nil, moduli, nil, nil, nil, SupportContact{}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, izravno(httptest.NewRequest(http.MethodGet, "/podaci/brojke", nil)))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("neprijavljen dobiva brojke: %d %q", w.Code, w.Header().Get("Location"))
	}

	u := o.osoba("pperic", "perina-lozinka", "pperic@example.com", false)
	if err := repository.NewModuleRepository(o.baza, ledger.New(o.baza, "test")).SetUserOverride(context.Background(), u.ID.String(), nil,
		[]string{models.ModuleReadings, models.ModuleRegisters, models.ModuleJournals}); err != nil {
		t.Fatal(err)
	}
	sesija, _, err := o.auth.Login("pperic", "perina-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	r := izravno(httptest.NewRequest(http.MethodGet, "/podaci/brojke", nil))
	r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija.ID.String()})
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("prijavljeni dobiva brojke: %d %q", w.Code, w.Body.String())
	}
}
