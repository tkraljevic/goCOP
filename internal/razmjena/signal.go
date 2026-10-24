package razmjena

// Signalni klijent (plan WEB synca, F2b): računalo se prijavi kod
// sastajališta i drži vezu. WebSocket ide na /razmjena/signal istim putem
// kao tunel (KlijentTunela), unutra TLS signala s ključem sastajališta iz
// članstva, pa prijava i potvrda (prijavljen). Puls ide svakih 25 s, a bez
// ijedne poruke 75 s veza se smatra mrtvom. Kad veza pukne ili ne uspije,
// klijent pričeka 2 s · 2ⁿ, najviše 60 s, ±20 %, i pokuša ponovno; to nikad
// ne znači da tamo nema sastajališta. Nakon buđenja ili promjene adresa
// (PonovnoSpoji) odgoda kreće ispočetka. Kopija istog ključa na drugom
// računalu čeka 10 min, i nakon buđenja: taj rok zadaje sastajalište.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"sync"
	"time"
)

// Odgoda ponovnog spajanja; var, da je test skrati
var (
	prvaOdgodaSignala    = 2 * time.Second
	najvecaOdgodaSignala = 60 * time.Second
	rokSpajanjaSignala   = 15 * time.Second
	rokPulsaBudenja      = 5 * time.Second
	// trajanjeUpozorenjaKopije: kopija koja još radi javi se svakih 10 min,
	// pa upozorenje na računalu koje je ostalo prijavljeno nakon toga ode
	trajanjeUpozorenjaKopije = 2 * ponovnoNakonKopijeSek * time.Second
)

// ErrNepoznatCvorSastajalista: ključ čvora sastajališta iz postavki nije u
// članstvu
var ErrNepoznatCvorSastajalista = errors.New("čvor sastajališta nije poznat član mreže")

var (
	errNijeSastajaliste = errors.New("druga strana se nije javila kao sastajalište")
	errPonovnoSpajanje  = errors.New("ponovno spajanje")
)

// odbijenSignal je odbijenica sastajališta
type odbijenSignal struct{ o odbijenoSignala }

func (e odbijenSignal) Error() string {
	return fmt.Sprintf("sastajalište je odbilo prijavu (%s)", e.o.Kod)
}

// StanjeSignala je stanje klijenta za pločicu
type StanjeSignala struct {
	Prijavljen     bool
	Od             time.Time // otkad traje ovo stanje
	Kod            KodSignala
	PonovnoU       time.Time // kad klijent pokušava ponovno (kopija)
	Odbijen        bool      // uz kopiju: ovo računalo sastajalište nije pustilo
	OdstupanjeSata time.Duration
}

// KlijentSignala drži prijavu kod sastajališta
type KlijentSignala struct {
	Priv     ed25519.PrivateKey
	Protokol string
	Vj       func() []byte
	Adresa   string                                           // https://cop.primjer.hr
	Kljuc    func(context.Context) (ed25519.PublicKey, error) // ključ sastajališta iz članstva
	Tunel    *KlijentTunela                                   // WebSocket; nulta vrijednost je zadano
	Program  string                                           // izdanje programa, za prijavu
	Javi     func(string, ...any)                             // dnevnik
	NaPoruku func(PotpisanaPoruka)                            // potpisana poruka od drugog računala (F3)
	NaNemu   func(sesija, razlog string)                      // sastajalište nije proslijedilo (F3)
	// NaPrijavu se zove nakon svake prijave: plan traži novu provjeru mreže
	// pri ponovnom spajanju na sastajalište
	NaPrijavu func()
	Sat       func() time.Time // zadano time.Now

	pocetak   sync.Once
	proces    string
	mu        sync.Mutex
	stanje    StanjeSignala
	veza      *vezaSignala
	ziv       chan struct{} // svaka primljena poruka
	budi      chan struct{} // PonovnoSpoji: odgoda ispočetka
	ispocetka bool
	prekini   context.CancelFunc // prekida pokušaj u tijeku (PonovnoSpoji)
	nePrije   time.Time          // rok sastajališta: ranije se ne zove (kopija)
	kopijaU   time.Time          // kad je stiglo upozorenje o kopiji
}

func (k *KlijentSignala) pripremi() {
	k.pocetak.Do(func() {
		var b [16]byte
		_, _ = rand.Read(b[:]) // crypto/rand ne vraća grešku
		k.proces = hex.EncodeToString(b[:])
		k.ziv, k.budi = make(chan struct{}, 1), make(chan struct{}, 1)
		if k.Sat == nil {
			k.Sat = time.Now
		}
		if k.Tunel == nil {
			k.Tunel = &KlijentTunela{}
		}
	})
}

// Stanje je stanje prijave sada. Upozorenje o kopiji na računalu koje je
// ostalo prijavljeno ode kad se kopija dugo ne javi.
func (k *KlijentSignala) Stanje() StanjeSignala {
	k.pripremi()
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.stanje.Prijavljen && k.stanje.Kod == KodSignalaKopija && k.Sat().Round(0).Sub(k.kopijaU) > trajanjeUpozorenjaKopije {
		k.stanje.Kod = ""
	}
	return k.stanje
}

// Radi drži prijavu dok ctx traje
func (k *KlijentSignala) Radi(ctx context.Context) {
	k.pripremi()
	k.postavi(StanjeSignala{Kod: KodSignalaSpajaSe, Od: k.Sat()})
	n := 0
	for {
		prijavljen, err := k.pokusaj(ctx)
		if ctx.Err() != nil {
			return // gašenje nije prekid veze
		}
		if prijavljen {
			n = 0
		}
		if k.cekaj(ctx, k.nakonGreske(err, n)) {
			n = 0
		} else {
			n++
		}
	}
}

// pokusaj je jedna veza pod svojim kontekstom, koji PonovnoSpoji prekida i
// dok se veza tek otvara
func (k *KlijentSignala) pokusaj(ctx context.Context) (bool, error) {
	ctx, otkazi := context.WithCancel(ctx)
	defer otkazi()
	k.mu.Lock()
	k.prekini = otkazi
	k.mu.Unlock()
	return k.jednaVeza(ctx)
}

// PonovnoSpoji prekine vezu ili pokušaj u tijeku i spoji se odmah, s
// odgodom ispočetka (nakon buđenja i promjene adresa); rok koji je zadalo
// sastajalište ipak vrijedi
func (k *KlijentSignala) PonovnoSpoji() {
	k.pripremi()
	k.mu.Lock()
	k.ispocetka = true
	prekini := k.prekini
	k.mu.Unlock()
	if prekini != nil {
		prekini()
	}
	select {
	case k.budi <- struct{}{}:
	default:
	}
}

// Puls nakon kratkog spavanja: veza je živa kad sastajalište odgovori u
// 5 s; inače ide ponovno spajanje
func (k *KlijentSignala) Puls() {
	k.pripremi()
	k.mu.Lock()
	v := k.veza
	k.mu.Unlock()
	if v == nil {
		return
	}
	select {
	case <-k.ziv:
	default:
	}
	if v.posalji(SignalPuls, pulsSignala{Odgovori: true}) == nil && k.cekajZiv(rokPulsaBudenja) {
		return
	}
	k.PonovnoSpoji()
}

func (k *KlijentSignala) cekajZiv(rok time.Duration) bool {
	t := time.NewTimer(rok)
	defer t.Stop()
	select {
	case <-k.ziv:
		return true
	case <-t.C:
		return false
	}
}

// Posalji pošalje potpisanu poruku preko sastajališta; greška kad
// prijave nema
func (k *KlijentSignala) Posalji(pp PotpisanaPoruka) error {
	k.mu.Lock()
	v := k.veza
	k.mu.Unlock()
	if v == nil {
		return errors.New("WEB sync nije prijavljen kod sastajališta")
	}
	return v.posalji(pp.Vrsta, pp)
}

// jednaVeza spoji se, prijavi i drži vezu; javlja je li prijava prošla
func (k *KlijentSignala) jednaVeza(ctx context.Context) (bool, error) {
	v, err := k.spoji(ctx)
	if err != nil {
		return false, err
	}
	defer v.zatvori()
	// kraj konteksta zatvori vezu, pa ni čekanje na potvrdu ne drži gašenje
	defer context.AfterFunc(ctx, v.zatvori)()
	prijavljen, err := k.prijavi(v)
	if err != nil {
		return false, err
	}
	k.uVezi(v, prijavljen)
	defer k.izVeze()
	return true, k.drzi(ctx, v)
}

// spoji otvori WebSocket na putu signala i TLS s ključem sastajališta
func (k *KlijentSignala) spoji(ctx context.Context) (*vezaSignala, error) {
	kljuc, err := k.Kljuc(ctx)
	if err != nil {
		return nil, err // ErrNepoznatCvorSastajalista ili prolazna greška baze
	}
	ctx, otkazi := context.WithTimeout(ctx, rokSpajanjaSignala)
	defer otkazi()
	nc, baza, err := k.Tunel.uTunel(ctx, k.Adresa, PutSignala)
	if err != nil {
		return nil, err
	}
	cfg, err := tlsKlijentaSignala(k.Priv, k.Protokol, k.Vj, baza, kljuc)
	if err != nil {
		nc.Close()
		return nil, err
	}
	tc := tls.Client(nc, cfg)
	if err := tc.HandshakeContext(ctx); err != nil {
		nc.Close()
		return nil, porukaTunela(baza, err)
	}
	return &vezaSignala{c: tc, kljuc: kljuc}, nil
}

// prijavi pošalje prijavu i čeka potvrdu sastajališta ili odbijenicu
func (k *KlijentSignala) prijavi(v *vezaSignala) (prijavljenSignala, error) {
	_ = v.posalji(SignalPrijava, prijavaSignala{Ver: 1, Proces: k.proces, Program: k.Program}) // kad ne prođe, ne stigne ni odgovor
	por, err := v.primi(rokPrijave)
	if err != nil {
		return prijavljenSignala{}, err
	}
	var odb odbijenoSignala
	if por.Vrsta == SignalOdbijeno && tijeloIz(por, &odb) == nil {
		return prijavljenSignala{}, odbijenSignal{odb}
	}
	var pr prijavljenSignala
	if por.Vrsta != SignalPrijavljen || tijeloIz(por, &pr) != nil || pr.Oznaka != oznakaSastajalista {
		return prijavljenSignala{}, errNijeSastajaliste
	}
	return pr, nil
}

// uVezi zapamti vezu i stanje prijave: od kad i koliko sat odstupa
func (k *KlijentSignala) uVezi(v *vezaSignala, pr prijavljenSignala) {
	sad := k.Sat()
	k.mu.Lock()
	k.veza, k.nePrije = v, time.Time{}
	k.stanje = StanjeSignala{Prijavljen: true, Od: sad, OdstupanjeSata: sad.Sub(time.UnixMilli(pr.Vrijeme))}
	k.mu.Unlock()
	k.javi("povezivost: WEB sync je prijavljen kod sastajališta")
	if k.NaPrijavu != nil {
		k.NaPrijavu()
	}
}

// izVeze zaboravi vezu; stanje dalje vodi nakonGreske
func (k *KlijentSignala) izVeze() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.veza = nil
}

// drzi šalje puls i čita poruke dok veza traje
func (k *KlijentSignala) drzi(ctx context.Context, v *vezaSignala) error {
	greska := make(chan error, 1)
	go func() { greska <- k.citaj(v) }()
	t := time.NewTicker(razmakPulsa)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			v.zatvori() // AfterFunc je to već učinio; zatvaranje dvaput ne smeta
			<-greska
			return ctx.Err()
		case err := <-greska:
			return err
		case <-t.C:
			_ = v.posalji(SignalPuls, pulsSignala{}) // kad ne prođe, čitanje vidi prekid
		}
	}
}

// citaj prima poruke sastajališta dok veza traje
func (k *KlijentSignala) citaj(v *vezaSignala) error {
	for {
		por, err := v.primi(rokCitanjaSignala)
		if err != nil {
			return err
		}
		select {
		case k.ziv <- struct{}{}:
		default:
		}
		if err := k.obradi(v, por); err != nil {
			return err
		}
	}
}

// obradi jednu poruku sastajališta; greška prekida vezu
func (k *KlijentSignala) obradi(v *vezaSignala, por porukaSignala) error {
	switch {
	case por.Vrsta == SignalPuls:
		return k.naPuls(v, por)
	case por.Vrsta == SignalOdbijeno:
		var odb odbijenoSignala
		_ = tijeloIz(por, &odb) // kriva odbijenica ostaje odbijenica bez koda
		return odbijenSignal{odb}
	case por.Vrsta == SignalUpozorenje:
		k.naUpozorenje(por)
	case por.Vrsta == SignalNema:
		k.naNemu(por)
	case jePotpisana(por.Vrsta):
		k.naPotpisanu(por)
	}
	return nil
}

// naPuls: puls koji traži odgovor dobije ga odmah
func (k *KlijentSignala) naPuls(v *vezaSignala, por porukaSignala) error {
	var p pulsSignala
	if tijeloIz(por, &p) != nil || !p.Odgovori {
		return nil
	}
	return v.posalji(SignalPuls, pulsSignala{})
}

// naUpozorenje: kopija na drugom računalu; prijava ostaje
func (k *KlijentSignala) naUpozorenje(por porukaSignala) {
	var u upozorenjeSignala
	if tijeloIz(por, &u) != nil || u.Kod == "" {
		return
	}
	k.mu.Lock()
	k.stanje.Kod, k.kopijaU = u.Kod, k.Sat().Round(0)
	k.mu.Unlock()
	k.javi("povezivost: sastajalište javlja: %s", u.Kod)
}

func (k *KlijentSignala) naNemu(por porukaSignala) {
	var n nemaSignala
	if tijeloIz(por, &n) == nil && k.NaNemu != nil {
		k.NaNemu(n.Sesija, n.Razlog)
	}
}

func (k *KlijentSignala) naPotpisanu(por porukaSignala) {
	if pp, err := potpisanaIz(por); err == nil && k.NaPoruku != nil {
		k.NaPoruku(pp)
	}
}

// nakonGreske postavi stanje nakon veze koja je pukla ili nije uspjela i
// vrati koliko čekati
func (k *KlijentSignala) nakonGreske(err error, n int) time.Duration {
	st, cekaj := stanjeNakon(err, n, k.Sat().Round(0)) // zidni sat: rok sastajališta teče i dok računalo spava
	k.mu.Lock()
	defer k.mu.Unlock()
	if st.Odbijen {
		k.nePrije = st.PonovnoU
	}
	if !k.stanje.Prijavljen && !k.stanje.Kod.Trajan() && !st.Kod.Trajan() {
		st.Od = k.stanje.Od // prolazno stanje traje od prvog neuspjeha
	}
	if k.stanje.Prijavljen || st.Kod != k.stanje.Kod {
		k.javi("povezivost: WEB sync nije prijavljen kod sastajališta (%s); ponovno za %d s", st.Kod, int(cekaj.Seconds()))
	}
	k.stanje = st
	return cekaj
}

// stanjeNakon je stanje i odgoda za grešku veze: uobičajena odgoda, ili
// rok koji je zadalo sastajalište (kopija)
func stanjeNakon(err error, n int, sad time.Time) (StanjeSignala, time.Duration) {
	st := StanjeSignala{Kod: kodNakon(err), Od: sad}
	cekaj := odgoda(n)
	if zaSek := ponovnoZa(err); zaSek > 0 {
		cekaj = time.Duration(zaSek) * time.Second
		st.Odbijen, st.PonovnoU = true, sad.Add(cekaj)
	}
	return st, cekaj
}

// ponovnoZa je za koliko sekundi sastajalište smije ponovno, kad ga je
// odbilo s rokom (kopija); 0: uobičajena odgoda
func ponovnoZa(err error) int {
	var odb odbijenSignal
	if errors.As(err, &odb) {
		return odb.o.PonovnoZaSek
	}
	return 0
}

// kodNakon je kod stanja za grešku veze
func kodNakon(err error) KodSignala {
	var odb odbijenSignal
	switch {
	case errors.As(err, &odb) && odb.o.Kod != "":
		return odb.o.Kod
	case errors.Is(err, ErrDrugiKljuc):
		return KodSignalaKriviKljuc
	case errors.Is(err, ErrNepoznatCvorSastajalista):
		return KodSignalaNepoznatCvor
	}
	return KodSignalaNedostupno
}

// odgoda je 2 s · 2ⁿ, najviše 60 s, ±20 %
func odgoda(n int) time.Duration {
	d := najvecaOdgodaSignala
	if n < 6 {
		d = min(prvaOdgodaSignala<<n, najvecaOdgodaSignala)
	}
	return time.Duration(float64(d) * (0.8 + 0.4*mrand.Float64())) // raspršenje odgode ne treba sigurnu slučajnost
}

// cekaj čeka odgodu ili PonovnoSpoji, koji je skrati, ali nikad ispod
// roka koji je zadalo sastajalište (nePrije); javlja je li odgoda ispočetka
func (k *KlijentSignala) cekaj(ctx context.Context, d time.Duration) bool {
	ispocetka := k.uzmiIspocetka()
	if ispocetka {
		d = 0
	}
	for {
		d = max(d, k.doRoka())
		if d <= 0 || !k.probudjen(ctx, d) {
			return ispocetka
		}
		k.uzmiIspocetka()
		ispocetka, d = true, 0
	}
}

// uzmiIspocetka vrati i obriše oznaku ponovnog spajanja
func (k *KlijentSignala) uzmiIspocetka() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	ispocetka := k.ispocetka
	k.ispocetka = false
	select {
	case <-k.budi:
	default:
	}
	return ispocetka
}

// doRoka je koliko je još do roka sastajališta; 0 ili manje kad ga nema
func (k *KlijentSignala) doRoka() time.Duration {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.nePrije.IsZero() {
		return 0
	}
	return k.nePrije.Sub(k.Sat().Round(0))
}

// probudjen čeka d; true kad ga je prije probudio PonovnoSpoji
func (k *KlijentSignala) probudjen(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return false
	case <-k.budi:
		return true
	}
}

func (k *KlijentSignala) postavi(st StanjeSignala) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.stanje = st
}

func (k *KlijentSignala) javi(format string, args ...any) {
	if k.Javi != nil {
		k.Javi(format, args...)
	}
}
