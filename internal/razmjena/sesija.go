package razmjena

// Sesija WEB synca (plan WEB synca, F3a, „Pravila sesije”): jedna QUIC veza
// dvaju računala nakon probijanja, s kanalima. Prvi bajt svakog toka je
// kanal:
//   - 0x00 je kontrolni: jedan tok, koji A otvori odmah nakon rukovanja.
//     Poruke su okviri (okvir.go) s JSON-om: pozdrav (sesija iz potpisanog
//     poziva), ping i zatvaram (s kodom). Nepoznata vrsta se zanemaruje.
//   - 0x01 nosi današnji razgovor razmjene bajt po bajt. Dolazni tok ide
//     kroz ista vrata kao port i tunel (primiVezu: CN, trusted, ista četiri
//     zajednička mjesta, peti dobiva „zauzet”).
//   - 0x02 i 0x03 („stranice”) su samo rezervirani, pa se za njih, kao i za
//     svaki drugi kanal, tok zatvara kodom 0x0101.
//
// Sesija vrijedi tek kad stigne pozdrav s njezinom sesijom (web_sync.go): B
// ga čita prije ičega drugog, a A tek na B-ov pozdrav zna da je B u istoj
// sesiji. Živi 6 min nakon zadnjeg toka 0x01, dulje od kruga razmjene od
// 5 min, pa idući krug ne probija ponovno; održavanje veze svakih 15 s
// (postavkeQUIC) drži NAT otvorenim. trusted() se provjerava na početku
// svakog toka 0x01, u oba smjera, i nakon primjene novih članstava, ovlasti
// ili opoziva (WebSync.ProvjeriSve); tada sesija dobiva 0x0104 i zatvara
// se. Zatvaranje pošalje zatvaram s kodom i do 1 s čeka da druga strana
// zatvori vezu, pa kod stigne i kad se zadnji paket QUIC-a izgubi.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
)

// Kanali sesije; 0x02 i 0x03 („stranice”) su rezervirani
const (
	kanalKontrola byte = 0x00
	kanalRazmjena byte = 0x01
)

// Vrste poruka kontrolnog kanala
const (
	kontrolaPozdrav  = "pozdrav"
	kontrolaPing     = "ping"
	kontrolaZatvaram = "zatvaram"
)

// Rokovi sesije; var, da ih test skrati
var (
	zivotSesije   = 6 * time.Minute
	rokKanala     = 15 * time.Second // za bajt kanala na dolaznom toku
	rokZatvaranja = time.Second      // koliko se čeka da druga strana zatvori vezu
	rokPozdrava   = 5 * time.Second  // za tok i pozdrav nakon rukovanja
)

// ErrNijeClanSesije: druga strana sesije više nije član mreže
var ErrNijeClanSesije = errors.New("druga strana WEB sync sesije više nije član mreže")

// errNijePozdrav: na kontrolnom kanalu nije stigao pozdrav
var errNijePozdrav = errors.New("na vezi WEB synca nije stigao pozdrav")

// porukaKontrole je jedna poruka kontrolnog kanala
type porukaKontrole struct {
	Vrsta    string     `json:"vrsta"`
	Ver      int        `json:"ver,omitempty"`
	Sesija   string     `json:"sesija,omitempty"`   // pozdrav
	Znanje   *Znanje    `json:"znanje,omitempty"`   // pozdrav
	Odgovori bool       `json:"odgovori,omitempty"` // ping: druga strana odmah vrati ping
	Br       uint64     `json:"br,omitempty"`       // ping: redni broj, a odgovor vraća isti
	Kod      KodRazloga `json:"kod,omitempty"`      // zatvaram
}

// pisiKontrolu zapiše poruku kontrole u okvir
func pisiKontrolu(w io.Writer, p porukaKontrole) error {
	b, _ := json.Marshal(p) // tekst, brojevi i Znanje uvijek se daju zapisati
	return PisiOkvir(w, b)
}

// procitajKontrolu pročita jednu poruku kontrole
func procitajKontrolu(r io.Reader) (porukaKontrole, error) {
	b, err := CitajOkvir(r)
	if err != nil {
		return porukaKontrole{}, err
	}
	var p porukaKontrole
	if err := json.Unmarshal(b, &p); err != nil || p.Vrsta == "" {
		return porukaKontrole{}, ErrOblikPoruke
	}
	return p, nil
}

// procitajPozdrav pročita poruku kontrole koja mora biti pozdrav; greška
// čitanja ostaje kakva jest (npr. kod kojim je druga strana zatvorila vezu)
func procitajPozdrav(r io.Reader) (porukaKontrole, error) {
	p, err := procitajKontrolu(r)
	if err == nil && p.Vrsta != kontrolaPozdrav {
		err = errNijePozdrav
	}
	return p, err
}

// citajKanal pročita bajt kanala s početka toka
func citajKanal(r io.Reader) (byte, error) {
	var b [1]byte
	_, err := io.ReadFull(r, b[:])
	return b[0], err
}

// odbijTok zatvori tok u oba smjera kodom
func odbijTok(tok *quic.Stream, k KodRazloga) {
	tok.CancelRead(quic.StreamErrorCode(k))
	tok.CancelWrite(quic.StreamErrorCode(k))
}

// SesijaWeb je živa sesija WEB synca s drugim računalom
type SesijaWeb struct {
	Sesija  string            // iz potpisanog poziva
	Druga   ed25519.PublicKey // ključ druge strane, dokazan u TLS-u
	Put     Put               // probijeno, ili tailscale kad promet ide kroz Tailscale
	RTT     time.Duration     // rukovanje QUIC-a na strani koja je zvala; kod druge 0
	Pokusaj int               // u kojem je pokušaju probijanje uspjelo
	Znanje  *Znanje           // što je druga strana javila u pozdravu

	veza     *quic.Conn
	kontrola *quic.Stream
	vjDruge  []byte
	v        *vrata
	pisanje  sync.Mutex
	mu       sync.Mutex
	tokova   int
	istek    *time.Timer
	ping     chan uint64   // broj zadnjeg odgovora na ping
	pingRed  chan struct{} // jedan Ping odjednom
	brPinga  atomic.Uint64
	// zatvara je prvo zatvaranje, a ostala čekaju na zatvoreno (kanal, ne
	// brava: u mjehuru testa gorutina na bravi nije trajno blokirana, pa
	// sat ne bi išao dok prvo zatvaranje čeka drugu stranu)
	zatvara   atomic.Bool
	zatvoreno chan struct{}
	gorutine  sync.WaitGroup
}

// postavi pripremi kanale i istek; ide prije nego što sesiju itko drugi
// vidi, jer je Zatvori i Ping smiju zvati odmah
func (s *SesijaWeb) postavi() {
	s.ping, s.pingRed, s.zatvoreno = make(chan uint64, 1), make(chan struct{}, 1), make(chan struct{})
	s.istek = time.AfterFunc(zivotSesije, s.istekla)
}

// pokreni pokrene čitanje kontrole i primanje tokova postavljene sesije;
// naKraj se zove kad se veza zatvori
func (s *SesijaWeb) pokreni(naKraj func(*SesijaWeb)) {
	s.gorutine.Go(s.citajKontrolu)
	s.gorutine.Go(s.primajTokove)
	s.gorutine.Go(func() {
		<-s.veza.Context().Done()
		s.istek.Stop()
		naKraj(s)
	})
}

// Razgovor otvori tok na kanalu 0x01 za današnji razgovor razmjene. Prije
// toga provjeri da je druga strana još član; kad nije, sesija se zatvara
// kodom 0x0104. Vezu zatvara pozivatelj: dok je otvorena, sesija ne
// istječe.
func (s *SesijaWeb) Razgovor(ctx context.Context) (*Conn, error) {
	if !s.v.trusted(s.Druga, s.vjDruge) {
		s.Zatvori(KodNijeClan)
		return nil, ErrNijeClanSesije
	}
	tok, err := s.veza.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	s.pocetakToka()
	_, _ = tok.Write([]byte{kanalRazmjena}) // ne prođe samo na mrtvoj vezi, a tada ne prođe ni prva poruka razgovora
	c := newConnKljuc(s.tok(tok), s.Druga, s.vjDruge)
	c.Put, c.RTT = s.Put, s.RTT
	return c, nil
}

// Ping pošalje ping i čeka odgovor druge strane; vraća koliko je trajalo.
// Pingovi idu jedan po jedan, s rednim brojem, pa zakašnjeli odgovor na
// raniji ping (kojem je istekao kontekst) ne vrijedi za idući.
func (s *SesijaWeb) Ping(ctx context.Context) (time.Duration, error) {
	select {
	case s.pingRed <- struct{}{}:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	defer func() { <-s.pingRed }()
	br := s.brPinga.Add(1)
	pocetak := time.Now()
	if err := s.posaljiKontrolu(porukaKontrole{Vrsta: kontrolaPing, Odgovori: true, Br: br}); err != nil {
		return 0, err
	}
	return s.cekajPing(ctx, br, pocetak)
}

// cekajPing čeka odgovor s brojem br; ostale odgovore preskače
func (s *SesijaWeb) cekajPing(ctx context.Context, br uint64, pocetak time.Time) (time.Duration, error) {
	for {
		select {
		case b := <-s.ping:
			if b == br {
				return time.Since(pocetak), nil
			}
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-s.veza.Context().Done():
			return 0, context.Cause(s.veza.Context())
		}
	}
}

// Zatvori zatvori sesiju kodom (0 je uredan kraj): javi zatvaram, do 1 s
// pričeka da druga strana zatvori vezu, pa je zatvori i sama. Drugi poziv
// čeka prvi i ne radi ništa.
func (s *SesijaWeb) Zatvori(kod KodRazloga) {
	if !s.zatvara.CompareAndSwap(false, true) {
		<-s.zatvoreno
		return
	}
	defer close(s.zatvoreno)
	_ = s.posaljiKontrolu(porukaKontrole{Vrsta: kontrolaZatvaram, Kod: kod}) // kad ne prođe, kod nosi i zatvaranje veze
	t := time.NewTimer(rokZatvaranja)
	defer t.Stop()
	select {
	case <-s.veza.Context().Done():
	case <-t.C:
	}
	_ = s.veza.CloseWithError(quic.ApplicationErrorCode(kod), "") // veza odlazi; zatvorena veza grešku nema kome reći
}

// Gotova se zatvori kad se sesija zatvori, s bilo koje strane
func (s *SesijaWeb) Gotova() <-chan struct{} { return s.veza.Context().Done() }

// Kod je kod s kojim je sesija zatvorena; 0 dok je otvorena ili kad je
// zatvorena bez koda (istek mirovanja, prekid)
func (s *SesijaWeb) Kod() KodRazloga {
	var ae *quic.ApplicationError
	if errors.As(context.Cause(s.veza.Context()), &ae) {
		return KodRazloga(ae.ErrorCode)
	}
	return 0
}

// Cekaj čeka da stanu gorutine sesije; zove se nakon zatvaranja
func (s *SesijaWeb) Cekaj() { s.gorutine.Wait() }

// posaljiKontrolu pošalje poruku na kontrolnom kanalu, jednu po jednu
func (s *SesijaWeb) posaljiKontrolu(p porukaKontrole) error {
	s.pisanje.Lock()
	defer s.pisanje.Unlock()
	_ = s.kontrola.SetWriteDeadline(time.Now().Add(rokPisanjaSignala)) // rok toka QUIC-a uvijek se da postaviti
	return pisiKontrolu(s.kontrola, p)
}

// citajKontrolu čita kontrolni kanal dok veza traje; bez njega sesija ne
// radi, pa se veza zatvara
func (s *SesijaWeb) citajKontrolu() {
	for {
		p, err := procitajKontrolu(s.kontrola)
		if err != nil {
			_ = s.veza.CloseWithError(quic.ApplicationErrorCode(KodOpce), "") // bez kontrolnog kanala nema sesije; zatvorena veza grešku nema kome reći
			return
		}
		s.naKontrolu(p)
	}
}

// naKontrolu: ping koji traži odgovor dobije ga odmah, s istim brojem,
// odgovor na vlastiti ping se javi Pingu, a zatvaram zatvori vezu istim
// kodom
func (s *SesijaWeb) naKontrolu(p porukaKontrole) {
	switch {
	case p.Vrsta == kontrolaPing && p.Odgovori:
		_ = s.posaljiKontrolu(porukaKontrole{Vrsta: kontrolaPing, Br: p.Br}) // kad ne prođe, druga strana ne dobije odgovor i zna da veza ne radi
	case p.Vrsta == kontrolaPing:
		s.javiPing(p.Br)
	case p.Vrsta == kontrolaZatvaram:
		_ = s.veza.CloseWithError(quic.ApplicationErrorCode(p.Kod), "") // druga strana zatvara; greške zatvaranja nema
	}
}

// javiPing: zadnji odgovor zamijeni stariji koji Ping nije uzeo. Šalje
// samo čitač kontrole, pa nakon pražnjenja mjesta uvijek ima.
func (s *SesijaWeb) javiPing(br uint64) {
	select {
	case <-s.ping:
	default:
	}
	s.ping <- br
}

// primajTokove prima tokove druge strane dok veza traje
func (s *SesijaWeb) primajTokove() {
	for {
		tok, err := s.veza.AcceptStream(s.veza.Context())
		if err != nil {
			return
		}
		s.gorutine.Go(func() { s.primiTok(tok) })
	}
}

// primiTok pročita kanal toka i razgovor preda vratima razmjene; ostale
// kanale odbije kodom 0x0101. Tok koji u roku ne javi kanal se odbija isto.
// Ograda to ne broji: tok otvara član iste sesije, a nepoznat kanal je
// najčešće noviji program (stranice, 0x02 i 0x03), što nije odbijena
// veza; vidi ga druga strana po kodu 0x0101.
func (s *SesijaWeb) primiTok(tok *quic.Stream) {
	_ = tok.SetReadDeadline(time.Now().Add(rokKanala)) // rok toka QUIC-a uvijek se da postaviti
	if kanal, err := citajKanal(tok); err != nil || kanal != kanalRazmjena {
		odbijTok(tok, KodNepoznatKanal)
		return
	}
	s.pocetakToka()
	t := s.tok(tok)
	defer t.Close() //nolint:errcheck // razgovor je gotov; što je trebalo stići, potvrdio je sam razgovor
	// greška u jednom razgovoru ne smije srušiti čvor, kao uz primiTLS; tok
	// se odbije prije zatvaranja, pa druga strana vidi grešku, a ne kraj
	defer func() {
		if r := recover(); r != nil {
			log.Printf("razmjena: razgovor WEB synca prekinut zbog greške: %v", r)
			odbijTok(tok, KodOpce)
		}
	}()
	s.v.primiVezu(t, s.veza.ConnectionState().TLS.PeerCertificates, func() {})
}

// pocetakToka: dok teče razgovor, sesija ne istječe
func (s *SesijaWeb) pocetakToka() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokova++
	s.istek.Stop()
}

// krajToka: nakon zadnjeg razgovora sesija živi još 6 min
func (s *SesijaWeb) krajToka() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokova--
	if s.tokova == 0 {
		s.istek.Reset(zivotSesije)
	}
}

// istekla: 6 min bez razgovora; razgovor koji je upravo počeo je drži
func (s *SesijaWeb) istekla() {
	s.mu.Lock()
	tece := s.tokova > 0
	s.mu.Unlock()
	if !tece {
		s.Zatvori(0)
	}
}

// tokSesije je tok razgovora sesije: zatvaranje broji kraj razgovora, a
// nečlan zatvara cijelu sesiju
type tokSesije struct {
	*tokQUIC
	s    *SesijaWeb
	kraj sync.Once
}

var (
	_ odbijaKodom = (*tokSesije)(nil)
	_ znaPut      = (*tokSesije)(nil)
	_ znaVj       = (*tokSesije)(nil)
)

func (s *SesijaWeb) tok(t *quic.Stream) *tokSesije {
	return &tokSesije{tokQUIC: noviTokQUIC(s.veza, t), s: s}
}

// Close zatvori tok i jednom zabilježi kraj razgovora
func (t *tokSesije) Close() error {
	err := t.tokQUIC.Close()
	t.kraj.Do(t.s.krajToka)
	return err
}

// odbijKodom zatvori cijelu sesiju kodom (0x0104 za nečlana)
func (t *tokSesije) odbijKodom(k KodRazloga) { t.s.Zatvori(k) }

// put je put sesije, i za dolazni razgovor
func (t *tokSesije) put() Put { return t.s.Put }

// vjerodajnice su vjerodajnice druge strane iz poziva ili odziva sesije,
// i za dolazni razgovor (slušalica ih u certifikatu ne šalje)
func (t *tokSesije) vjerodajnice() []byte { return t.s.vjDruge }
