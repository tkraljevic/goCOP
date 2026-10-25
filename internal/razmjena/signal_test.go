package razmjena

// Signal i sastajalište kroz mrežu u memoriji (sastajaliste.go, signal.go):
// web poslužitelj s certifikatom za cop.primjer.hr, kao Cloudflare ispred
// stalnog čvora, i sastajalište R na /razmjena/signal. Sve teče u mjehuru,
// pa rokovi od 75 s i 10 min prolaze odmah.

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"gocop/internal/mrezatest"
)

const (
	protokolSignala = "gocop-test"
	adresaSignala   = "https://cop.primjer.hr"
)

// sastajalisteUMemoriji je R u mreži u memoriji i članovi kojima vjeruje
type sastajalisteUMemoriji struct {
	m        *mrezatest.Mreza
	r        *Sastajaliste
	kljucR   ed25519.PrivateKey
	korijeni *x509.CertPool

	mu       sync.Mutex
	clanovi  map[string]string // ključ → ime
	dnevnik  []string
	naR      chan PotpisanaPoruka
	sljedeci int // izvorna adresa idućeg klijenta
}

func novoSastajalisteUMemoriji(t *testing.T) *sastajalisteUMemoriji {
	t.Helper()
	cert, korijeni := webCertifikat(t, "cop.primjer.hr")
	su := &sastajalisteUMemoriji{m: mrezatest.NovaMreza(), kljucR: newKey(t), korijeni: korijeni, clanovi: map[string]string{}, naR: make(chan PotpisanaPoruka, 8)}
	su.m.Kasnjenje = kasnjenjeTunela
	r, err := NovoSastajaliste(PostavkeSastajalista{Priv: su.kljucR, Protokol: protokolSignala, Vjeruje: su.vjeruje, Ime: su.ime, Javi: su.javi, Sat: time.Now})
	bezGreske(t, err)
	r.NaPoruku = func(p PotpisanaPoruka) { su.naR <- p }
	su.r = r
	mux := http.NewServeMux()
	h := r.Handler()
	mux.Handle("GET "+PutSignala, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		klijent, _, _ := net.SplitHostPort(req.RemoteAddr) // mrezatest daje adresu s portom
		h.ServeHTTP(w, req.WithContext(SKlijentom(req.Context(), klijent)))
	}))
	s, err := su.m.Slusaj("cop.primjer.hr:443")
	bezGreske(t, err)
	srv := &http.Server{Handler: mux, ErrorLog: log.New(io.Discard, "", 0)}
	t.Cleanup(func() { r.Zatvori(); srv.Close() })
	go func() {
		_ = srv.Serve(tls.NewListener(s, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13})) // staje sa srv.Close na kraju testa
	}()
	return su
}

func (su *sastajalisteUMemoriji) vjeruje(k ed25519.PublicKey, _ []byte) bool {
	su.mu.Lock()
	defer su.mu.Unlock()
	_, ok := su.clanovi[PublicKeyString(k)]
	return ok
}

func (su *sastajalisteUMemoriji) ime(k ed25519.PublicKey) string {
	su.mu.Lock()
	defer su.mu.Unlock()
	return su.clanovi[PublicKeyString(k)]
}

func (su *sastajalisteUMemoriji) javi(format string, args ...any) {
	su.mu.Lock()
	defer su.mu.Unlock()
	su.dnevnik = append(su.dnevnik, fmt.Sprintf(format, args...))
}

func (su *sastajalisteUMemoriji) uDnevniku(dio string) bool {
	su.mu.Lock()
	defer su.mu.Unlock()
	for _, r := range su.dnevnik {
		if strings.Contains(r, dio) {
			return true
		}
	}
	return false
}

// clan upiše ključ kao člana s imenom
func (su *sastajalisteUMemoriji) clan(ime string, k ed25519.PrivateKey) {
	su.mu.Lock()
	defer su.mu.Unlock()
	su.clanovi[PublicKeyString(k.Public().(ed25519.PublicKey))] = ime
}

func (su *sastajalisteUMemoriji) opozovi(k ed25519.PrivateKey) {
	su.mu.Lock()
	delete(su.clanovi, PublicKeyString(k.Public().(ed25519.PublicKey)))
	su.mu.Unlock()
}

// tunelKlijenta je klijent tunela s vlastitom izvornom adresom
func (su *sastajalisteUMemoriji) tunelKlijenta() *KlijentTunela {
	su.mu.Lock()
	su.sljedeci++
	ip := fmt.Sprintf("10.0.%d.%d", su.sljedeci/250, su.sljedeci%250+1)
	su.mu.Unlock()
	return &KlijentTunela{TLS: &tls.Config{RootCAs: su.korijeni}, Dial: su.m.DialContextOd(ip)}
}

// klijent je signalni klijent s ključem, koji očekuje ključ R
func (su *sastajalisteUMemoriji) klijent(kljuc ed25519.PrivateKey) *KlijentSignala {
	javniR := su.kljucR.Public().(ed25519.PublicKey)
	return &KlijentSignala{Priv: kljuc, Protokol: protokolSignala, Adresa: adresaSignala, Tunel: su.tunelKlijenta(), Program: "0.0.42-alfa",
		Kljuc: func(context.Context) (ed25519.PublicKey, error) { return javniR, nil }}
}

// pokreni pokrene klijenta do kraja testa
func pokreni(t *testing.T, k *KlijentSignala) {
	t.Helper()
	ctx, otkazi := context.WithCancel(t.Context())
	gotovo := make(chan struct{})
	go func() { k.Radi(ctx); close(gotovo) }()
	t.Cleanup(func() { otkazi(); <-gotovo })
}

// cekajStanje čeka da klijent dođe u stanje (u mjehuru vrijeme teče samo)
func cekajStanje(t *testing.T, k *KlijentSignala, opis string, uvjet func(StanjeSignala) bool) {
	t.Helper()
	for range 600 {
		if uvjet(k.Stanje()) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s: stanje %+v", opis, k.Stanje())
}

func prijavljen(st StanjeSignala) bool { return st.Prijavljen }

// Dva člana se prijave; sastajalište ih broji, a sat sastajališta se vidi
// kao odstupanje. Poziv od A ide B bajt po bajt, B ga provjeri, a odziv
// stigne natrag A-u. Poziv kojem je cilj samo sastajalište ne ide dalje, a
// poziv nepoznatom ključu vrati „nema”.
func TestSignalPrijavaIProsljedivanje(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		kljucA, kljucB := newKey(t), newKey(t)
		su.clan("laptop", kljucA)
		su.clan("ured", kljucB)
		a, b := su.klijent(kljucA), su.klijent(kljucB)
		kodA, kodB := make(chan PotpisanaPoruka, 4), make(chan PotpisanaPoruka, 4)
		nema := make(chan [2]string, 4)
		a.NaPoruku = func(p PotpisanaPoruka) { kodA <- p }
		a.NaNemu = func(sesija, razlog string) { nema <- [2]string{sesija, razlog} }
		b.NaPoruku = func(p PotpisanaPoruka) { kodB <- p }
		pokreni(t, a)
		pokreni(t, b)
		cekajStanje(t, a, "A prijavljen", prijavljen)
		cekajStanje(t, b, "B prijavljen", prijavljen)
		if st := a.Stanje(); st.Kod != "" || st.OdstupanjeSata.Abs() > time.Second {
			t.Errorf("stanje A: %+v", st)
		}
		if st := su.r.Stanje(); st.Prijavljenih != 2 {
			t.Errorf("sastajalište: %+v", st)
		}

		javniR := su.kljucR.Public().(ed25519.PublicKey)
		javniB := kljucB.Public().(ed25519.PublicKey)
		poziv := Potpisi(kljucA, protokolSignala, VrstaPoziv, TijeloPoruke{Sesija: NovaSesija(), Za: PublicKeyString(javniB), Sastajaliste: PublicKeyString(javniR)})
		bezGreske(t, a.Posalji(poziv))
		stiglo := <-kodB
		if string(stiglo.Tijelo) != string(poziv.Tijelo) || string(stiglo.Potpis) != string(poziv.Potpis) || stiglo.Vrsta != VrstaPoziv {
			t.Fatalf("B je primio drukčiji poziv: %+v", stiglo)
		}
		provjera := ProvjeraPoruka{Protokol: protokolSignala, Moj: javniB, Sastajaliste: javniR, Sat: time.Now, Pricuva: NovaPricuvaPoruka(),
			Vjeruje: su.vjeruje}
		tijelo, err := provjera.Provjeri(stiglo)
		bezGreske(t, err)
		odziv := Potpisi(kljucB, protokolSignala, VrstaOdziv, TijeloPoruke{Sesija: tijelo.Sesija, Za: tijelo.Od, Sastajaliste: PublicKeyString(javniR), Odluka: "probaj"})
		bezGreske(t, b.Posalji(odziv))
		if p := <-kodA; p.Vrsta != VrstaOdziv || string(p.Tijelo) != string(odziv.Tijelo) {
			t.Errorf("A je primio %+v", p)
		}
		if st := su.r.Stanje(); st.PozivaSat != 1 {
			t.Errorf("poziva u satu: %+v", st)
		}

		sebi := Potpisi(kljucA, protokolSignala, VrstaPoziv, TijeloPoruke{Sesija: NovaSesija(), Za: PublicKeyString(javniR), Sastajaliste: PublicKeyString(javniR)})
		bezGreske(t, a.Posalji(sebi))
		if p := <-su.naR; string(p.Tijelo) != string(sebi.Tijelo) {
			t.Errorf("sastajalište je primilo %+v", p)
		}
		nepoznat := TijeloPoruke{Sesija: NovaSesija(), Za: PublicKeyString(newKey(t).Public().(ed25519.PublicKey)), Sastajaliste: PublicKeyString(javniR)}
		bezGreske(t, a.Posalji(Potpisi(kljucA, protokolSignala, VrstaPoziv, nepoznat)))
		if n := <-nema; n != [2]string{nepoznat.Sesija, NemaNijePrijavljen} {
			t.Errorf("nema: %v", n)
		}

		// sastajalište samo zove B
		bezGreske(t, su.r.Posalji(Potpisi(su.kljucR, protokolSignala, VrstaPoziv, TijeloPoruke{Sesija: NovaSesija(), Za: PublicKeyString(javniB), Sastajaliste: PublicKeyString(javniR)})))
		if p := <-kodB; p.Vrsta != VrstaPoziv {
			t.Errorf("poziv sastajališta: %+v", p)
		}
		if err := su.r.Posalji(Potpisi(su.kljucR, protokolSignala, VrstaPoziv, nepoznat)); err == nil {
			t.Error("sastajalište zove računalo koje nije na vezi")
		}
		if err := su.r.Posalji(PotpisanaPoruka{Vrsta: VrstaPoziv, Tijelo: []byte("{")}); err == nil {
			t.Error("sastajalište šalje poruku bez tijela")
		}
	})
}

// Poruka čiji pošiljatelj nije računalo na vezi se odbacuje i zapisuje;
// cilj je ne dobije, a pošiljatelj ne dobije ni „nema”
func TestSignalKrivotvorenPosiljatelj(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		kljucA, kljucB := newKey(t), newKey(t)
		su.clan("laptop", kljucA)
		su.clan("ured", kljucB)
		a, b := su.klijent(kljucA), su.klijent(kljucB)
		primljeno := make(chan PotpisanaPoruka, 4)
		b.NaPoruku = func(p PotpisanaPoruka) { primljeno <- p }
		a.NaNemu = func(string, string) { t.Error("pošiljatelj krivotvorene poruke dobio je odgovor") }
		pokreni(t, a)
		pokreni(t, b)
		cekajStanje(t, a, "A prijavljen", prijavljen)
		cekajStanje(t, b, "B prijavljen", prijavljen)
		javniR := su.kljucR.Public().(ed25519.PublicKey)
		// A šalje poruku potpisanu ključem B, kao da je od B
		lazna := Potpisi(kljucB, protokolSignala, VrstaPoziv, TijeloPoruke{Sesija: NovaSesija(), Za: PublicKeyString(kljucB.Public().(ed25519.PublicKey)), Sastajaliste: PublicKeyString(javniR)})
		bezGreske(t, a.Posalji(lazna))
		bezGreske(t, a.Posalji(PotpisanaPoruka{Vrsta: VrstaPoziv, Tijelo: []byte("nije json")}))
		time.Sleep(time.Second)
		synctest.Wait()
		su.mu.Lock()
		puta := strings.Count(strings.Join(su.dnevnik, "\n"), "pošiljatelj u poruci nije računalo na vezi")
		su.mu.Unlock()
		if len(primljeno) != 0 || puta != 1 {
			t.Errorf("krivotvoreno: primljeno %d, u dnevniku %d puta", len(primljeno), puta)
		}
	})
}

// Nečlan i računalo koje očekuje drugi ključ sastajališta ne dođu do
// prijave; oba stanja su trajna. Ključ sastajališta koji se ne zna iz
// članstva je trajno stanje. Opozvani član ispadne iz registra čim se
// opoziv primijeni.
func TestSignalOdbijeni(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		stranac := su.klijent(newKey(t))
		pokreni(t, stranac)
		cekajStanje(t, stranac, "stranac odbijen", func(st StanjeSignala) bool { return st.Kod == KodSignalaNijeClan })

		kljucA := newKey(t)
		su.clan("laptop", kljucA)
		krivi := su.klijent(kljucA)
		drugi := newKey(t).Public().(ed25519.PublicKey)
		krivi.Kljuc = func(context.Context) (ed25519.PublicKey, error) { return drugi, nil }
		pokreni(t, krivi)
		cekajStanje(t, krivi, "krivi ključ", func(st StanjeSignala) bool { return st.Kod == KodSignalaKriviKljuc })

		bez := su.klijent(kljucA)
		bez.Kljuc = func(context.Context) (ed25519.PublicKey, error) {
			return nil, fmt.Errorf("%w: cop-osijek", ErrNepoznatCvorSastajalista)
		}
		pokreni(t, bez)
		cekajStanje(t, bez, "nepoznat čvor", func(st StanjeSignala) bool { return st.Kod == KodSignalaNepoznatCvor })
		baza := su.klijent(kljucA)
		baza.Kljuc = func(context.Context) (ed25519.PublicKey, error) { return nil, errors.New("database is locked") }
		pokreni(t, baza)
		cekajStanje(t, baza, "greška baze je prolazna", func(st StanjeSignala) bool { return st.Kod == KodSignalaNedostupno })

		kljucB := newKey(t)
		su.clan("ured", kljucB)
		b := su.klijent(kljucB)
		pokreni(t, b)
		cekajStanje(t, b, "B prijavljen", prijavljen)
		su.opozovi(kljucB)
		su.r.ProvjeriSve()
		cekajStanje(t, b, "B opozvan", func(st StanjeSignala) bool { return !st.Prijavljen && st.Kod == KodSignalaNijeClan })
		if n := su.r.Stanje().Prijavljenih; n != 0 {
			t.Errorf("u registru ostalo %d", n)
		}
		if st := b.Stanje(); !st.Kod.Trajan() {
			t.Errorf("opozvani: %+v", st)
		}
	})
}

// Isti proces koji se ponovno spoji zamijeni svoju vezu. Drugi proces s
// istim ključem, dok stara veza odgovara, dobije „kopija” i čeka 10 min, a
// stara ostane prijavljena s upozorenjem; sastajalište zapamti ime.
func TestSignalKopija(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		kljuc := newKey(t)
		su.clan("laptop", kljuc)
		prvi := su.klijent(kljuc)
		pokreni(t, prvi)
		cekajStanje(t, prvi, "prvi prijavljen", prijavljen)
		od := prvi.Stanje().Od
		time.Sleep(time.Second)
		prvi.PonovnoSpoji()
		cekajStanje(t, prvi, "prvi ponovno prijavljen", func(st StanjeSignala) bool { return st.Prijavljen && st.Od.After(od) })
		if st := su.r.Stanje(); st.Prijavljenih != 1 || st.Prekida24h != 1 {
			t.Errorf("nakon ponovnog spajanja: %+v", st)
		}

		kopija := su.klijent(kljuc)
		pocetak := time.Now()
		ctxKopije, ugasiKopiju := context.WithCancel(t.Context())
		gotovaKopija := make(chan struct{})
		go func() { kopija.Radi(ctxKopije); close(gotovaKopija) }()
		cekajStanje(t, kopija, "kopija odbijena", func(st StanjeSignala) bool { return st.Kod == KodSignalaKopija && st.Odbijen })
		if st := kopija.Stanje(); st.PonovnoU.Sub(pocetak) < 9*time.Minute || st.Prijavljen {
			t.Errorf("kopija: %+v", st)
		}
		cekajStanje(t, prvi, "prvi dobio upozorenje", func(st StanjeSignala) bool { return st.Prijavljen && st.Kod == KodSignalaKopija && !st.Odbijen })
		if st := su.r.Stanje(); len(st.Kopije) != 1 || st.Kopije[0] != "laptop" || st.Prijavljenih != 1 {
			t.Errorf("sastajalište: %+v", st)
		}
		if !su.uDnevniku("odbilo drugu kopiju računala laptop") {
			t.Error("dnevnik sastajališta bez kopije")
		}
		// ugašena kopija se više ne javlja, pa je sastajalište zaboravi
		ugasiKopiju()
		<-gotovaKopija
		time.Sleep(2 * time.Hour)
		if st := su.r.Stanje(); len(st.Kopije) != 0 {
			t.Errorf("kopija se pamti dulje od sata: %+v", st)
		}
	})
}

// rucnaVeza je veza signala koju test vodi sam: prijava s procesom, pa
// tišina ili poruke po volji
func (su *sastajalisteUMemoriji) rucnaVeza(t *testing.T, kljuc ed25519.PrivateKey, proces string) *vezaSignala {
	t.Helper()
	tun := su.tunelKlijenta()
	nc, baza, err := tun.uTunel(t.Context(), adresaSignala, PutSignala)
	bezGreske(t, err)
	cfg, err := tlsKlijentaSignala(kljuc, protokolSignala, nil, baza, su.kljucR.Public().(ed25519.PublicKey))
	bezGreske(t, err)
	tc := tls.Client(nc, cfg)
	bezGreske(t, tc.HandshakeContext(t.Context()))
	v := &vezaSignala{c: tc}
	t.Cleanup(v.zatvori)
	if proces != "" {
		bezGreske(t, v.posalji(SignalPrijava, prijavaSignala{Ver: 1, Proces: proces}))
		if p, err := v.primi(time.Minute); err != nil || p.Vrsta != SignalPrijavljen {
			t.Fatalf("ručna prijava: %+v, %v", p, err)
		}
	}
	return v
}

// Stara veza koja ne odgovara na puls (poluotvorena) ustupi mjesto drugom
// procesu; veza bez ijedne poruke 75 s ispadne iz registra; više od 20
// poruka u sekundi prekida vezu; nepoznata vrsta se zanemaruje i zapiše
// jednom; prva poruka koja nije prijava zatvara vezu.
func TestSignalStareIBucneVeze(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		kljuc := newKey(t)
		su.clan("laptop", kljuc)
		_ = su.rucnaVeza(t, kljuc, "mrtvi")
		novi := su.klijent(kljuc)
		pokreni(t, novi)
		cekajStanje(t, novi, "novi zamijenio mrtvog", prijavljen)
		if st := su.r.Stanje(); st.Prijavljenih != 1 || len(st.Kopije) != 0 {
			t.Errorf("nakon mrtve veze: %+v", st)
		}

		kljucT := newKey(t)
		su.clan("tihi", kljucT)
		_ = su.rucnaVeza(t, kljucT, "tihi")
		if n := su.r.Stanje().Prijavljenih; n != 2 {
			t.Fatalf("tihi nije prijavljen: %d", n)
		}
		time.Sleep(rokCitanjaSignala + time.Second)
		synctest.Wait()
		if n := su.r.Stanje().Prijavljenih; n != 1 {
			t.Errorf("tihi ostao u registru: %d", n)
		}

		kljucB := newKey(t)
		su.clan("bučni", kljucB)
		bucni := su.rucnaVeza(t, kljucB, "bucni")
		bezGreske(t, bucni.posalji("nova-vrsta", struct{}{}))
		bezGreske(t, bucni.posalji("nova-vrsta", struct{}{}))
		for range najvisePorukaUSekundi {
			_ = bucni.posalji(SignalPuls, pulsSignala{}) // sastajalište vezu prekine usred petlje; to se i ispituje
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if n := su.r.Stanje().Prijavljenih; n != 1 {
			t.Errorf("bučni ostao u registru: %d", n)
		}
		su.mu.Lock()
		puta := strings.Count(strings.Join(su.dnevnik, "\n"), "nova-vrsta")
		su.mu.Unlock()
		if puta != 1 {
			t.Errorf("nepoznata vrsta u dnevniku %d puta", puta)
		}

		kljucN := newKey(t)
		su.clan("bez prijave", kljucN)
		v := su.rucnaVeza(t, kljucN, "")
		bezGreske(t, v.posalji(SignalPuls, pulsSignala{}))
		if _, err := v.primi(time.Minute); err == nil {
			t.Error("veza bez prijave dobila je odgovor")
		}
	})
}

// Granice poziva: treća sesija u tijeku prema istom cilju od istog
// pozivatelja i sedmi poziv u minuti dobiju „nema”; odziv zatvori sesiju,
// a sesija bez odziva istekne nakon 30 s
func TestSignalGranicePoziva(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		kljucA, kljucB := newKey(t), newKey(t)
		su.clan("laptop", kljucA)
		su.clan("ured", kljucB)
		a, b := su.klijent(kljucA), su.klijent(kljucB)
		nema := make(chan string, 16)
		a.NaNemu = func(_, razlog string) { nema <- razlog }
		kodB := make(chan PotpisanaPoruka, 16)
		b.NaPoruku = func(p PotpisanaPoruka) { kodB <- p }
		pokreni(t, a)
		pokreni(t, b)
		cekajStanje(t, a, "A prijavljen", prijavljen)
		cekajStanje(t, b, "B prijavljen", prijavljen)
		javniR := PublicKeyString(su.kljucR.Public().(ed25519.PublicKey))
		zaB := PublicKeyString(kljucB.Public().(ed25519.PublicKey))
		pozovi := func() string {
			s := NovaSesija()
			bezGreske(t, a.Posalji(Potpisi(kljucA, protokolSignala, VrstaPoziv, TijeloPoruke{Sesija: s, Za: zaB, Sastajaliste: javniR})))
			return s
		}
		pozovi()
		pozovi()
		pozovi()
		if r := <-nema; r != NemaPreviseSesija {
			t.Errorf("treća sesija: %q", r)
		}
		<-kodB
		<-kodB
		time.Sleep(rokSesijeSignala + time.Second)
		// dva poziva su prošla; još četiri u istoj minuti, uz odziv
		for range 4 {
			s := pozovi()
			<-kodB
			bezGreske(t, b.Posalji(Potpisi(kljucB, protokolSignala, VrstaOdziv, TijeloPoruke{Sesija: s, Za: PublicKeyString(kljucA.Public().(ed25519.PublicKey)), Sastajaliste: javniR})))
			time.Sleep(100 * time.Millisecond)
		}
		pozovi()
		if r := <-nema; r != NemaPrevisePoziva {
			t.Errorf("sedmi poziv u minuti: %q", r)
		}
		if st := su.r.Stanje(); st.PozivaSat != 6 {
			t.Errorf("poziva u satu: %d", st.PozivaSat)
		}
	})
}

// Osam sesija prema istom cilju od četiri pozivatelja popuni cilj; peti
// pozivatelj dobije „nema”
func TestSignalSesijeCilja(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		kljucB := newKey(t)
		su.clan("ured", kljucB)
		b := su.klijent(kljucB)
		pokreni(t, b)
		cekajStanje(t, b, "B prijavljen", prijavljen)
		javniR := PublicKeyString(su.kljucR.Public().(ed25519.PublicKey))
		zaB := PublicKeyString(kljucB.Public().(ed25519.PublicKey))
		nema := make(chan string, 4)
		for i := range 5 {
			k := newKey(t)
			su.clan(fmt.Sprintf("laptop-%d", i), k)
			a := su.klijent(k)
			a.NaNemu = func(_, razlog string) { nema <- razlog }
			pokreni(t, a)
			cekajStanje(t, a, "pozivatelj prijavljen", prijavljen)
			for range 2 {
				bezGreske(t, a.Posalji(Potpisi(k, protokolSignala, VrstaPoziv, TijeloPoruke{Sesija: NovaSesija(), Za: zaB, Sastajaliste: javniR})))
			}
		}
		if r := <-nema; r != NemaPreviseSesija {
			t.Errorf("deveta sesija: %q", r)
		}
	})
}

// Sastajalište drži 64 računala; 65. ne uđe i pokušava ponovno
func TestSignalPunRegistar(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		for i := range najvisePrijavljenih {
			k := newKey(t)
			su.clan(fmt.Sprintf("r-%d", i), k)
			_ = su.rucnaVeza(t, k, "p")
		}
		k := newKey(t)
		su.clan("višak", k)
		visak := su.klijent(k)
		pokreni(t, visak)
		cekajStanje(t, visak, "65. odbijen", func(st StanjeSignala) bool { return st.Kod == KodSignalaNedostupno })
		if st := su.r.Stanje(); st.Prijavljenih != najvisePrijavljenih {
			t.Errorf("u registru %d", st.Prijavljenih)
		}
	})
}

// S jednog klijenta weba idu najviše dva rukovanja odjednom, a rukovanje
// koje ne završi u 2 s se prekida
func TestSignalRukovanjaKlijenta(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		tun := su.tunelKlijenta()
		var veze []net.Conn
		for range 3 {
			nc, _, err := tun.uTunel(t.Context(), adresaSignala, PutSignala)
			bezGreske(t, err)
			veze = append(veze, nc)
			t.Cleanup(func() { nc.Close() })
		}
		synctest.Wait()
		zatvorena := func(nc net.Conn) bool {
			_ = nc.SetReadDeadline(time.Now().Add(100 * time.Millisecond)) // na zatvorenoj vezi ionako padne čitanje
			_, err := nc.Read(make([]byte, 1))
			return errors.Is(err, io.EOF)
		}
		if !zatvorena(veze[2]) {
			t.Error("treće rukovanje s istog klijenta nije odbijeno")
		}
		time.Sleep(rokRukovanjaSignala + time.Second)
		if !zatvorena(veze[0]) || !zatvorena(veze[1]) {
			t.Error("rukovanje bez kraja nije prekinuto")
		}
	})
}

// Certifikat sastajališta nema članstvo: vidi ga i strani klijent
func TestSignalPosluziteljBezVjerodajnica(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		nc, _, err := su.tunelKlijenta().uTunel(t.Context(), adresaSignala, PutSignala)
		bezGreske(t, err)
		cfg, err := tlsConfig(newKey(t), protokolSignala)
		bezGreske(t, err)
		cfg.NextProtos = []string{alpnSignala(protokolSignala)}
		tc := tls.Client(nc, cfg)
		bezGreske(t, tc.HandshakeContext(t.Context()))
		defer tc.Close()
		certs := tc.ConnectionState().PeerCertificates
		if len(certs) == 0 || peerVjerodajnice([][]byte{certs[0].Raw}) != nil {
			t.Error("strani klijent vidi vjerodajnice sastajališta")
		}
	})
}

// Kad sastajalište prekine vezu, klijent se ponovno spoji; dok ga nema,
// odgoda raste do 60 s, a PonovnoSpoji je vrati na početak. Puls nakon
// kratkog spavanja na živoj vezi ne spaja ponovno.
func TestSignalPonovnoSpajanje(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		kljuc := newKey(t)
		su.clan("laptop", kljuc)
		k := su.klijent(kljuc)
		prijava := make(chan struct{}, 4)
		k.NaPrijavu = func() { prijava <- struct{}{} } // svaka prijava javi programu, da izmjeri mrežu
		pokreni(t, k)
		cekajStanje(t, k, "prijavljen", prijavljen)
		<-prijava
		od := k.Stanje().Od
		k.Puls()
		if st := k.Stanje(); !st.Prijavljen || !st.Od.Equal(od) {
			t.Errorf("puls na živoj vezi: %+v", st)
		}
		su.r.Zatvori()
		cekajStanje(t, k, "ponovno prijavljen", func(st StanjeSignala) bool { return st.Prijavljen && st.Od.After(od) })
		<-prijava // i ponovna prijava javi programu

		su.opozovi(kljuc)
		su.r.ProvjeriSve()
		cekajStanje(t, k, "odbijen", func(st StanjeSignala) bool { return st.Kod == KodSignalaNijeClan })
		su.clan("laptop", kljuc)
		pocetak := time.Now()
		k.PonovnoSpoji()
		cekajStanje(t, k, "odmah nakon PonovnoSpoji", prijavljen)
		if d := time.Since(pocetak); d > 5*time.Second {
			t.Errorf("PonovnoSpoji čekao %v", d)
		}
	})
}

// Odgoda: 2 s · 2ⁿ, najviše 60 s, uz raspršenje ±20 %
func TestOdgodaSignala(t *testing.T) {
	for n, sredina := range map[int]time.Duration{0: 2 * time.Second, 1: 4 * time.Second, 4: 32 * time.Second, 5: 60 * time.Second, 9: 60 * time.Second} {
		for range 50 {
			if d := odgoda(n); d < sredina*8/10 || d > sredina*12/10 {
				t.Fatalf("odgoda(%d) = %v", n, d)
			}
		}
	}
}

// Klijent bez veze ne šalje i ne pulsira; odgovor koji nije od
// sastajališta nije prijava
func TestKlijentSignalaBezVeze(t *testing.T) {
	k := &KlijentSignala{}
	k.Puls()
	if err := k.Posalji(PotpisanaPoruka{Vrsta: VrstaPoziv}); !errors.Is(err, ErrNijePrijavljen) {
		t.Error("slanje bez veze")
	}
	if kodNakon(errNijeSastajaliste) != KodSignalaNedostupno || kodNakon(odbijenSignal{odbijenoSignala{}}) != KodSignalaNedostupno {
		t.Error("kod nakon greške")
	}
}
