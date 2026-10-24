package razmjena

// Sastajalište za WEB sync (plan WEB synca, F2b). Računala se prijave
// WebSocketom na /razmjena/signal i drže vezu satima; unutra je TLS signala
// (signal_tls.go). Sastajalište zna tko je na vezi i prosljeđuje potpisane
// poruke (poziv, odziv, sinkro) računalu kojem idu, bajt po bajt. Poruku
// kojoj je cilj ono samo preda NaPoruku. Registar je samo u memoriji.
//
// Pravila:
//   - najviše 64 računala i jedna veza po ključu; isti proces zamijeni svoju
//     staru vezu (Cloudflare zna ostaviti poluotvorenu), a drugi proces s
//     istim ključem dobije mjesto samo kad stara veza ne odgovori na puls u
//     5 s; inače je kopija: nova se odbije i javi se tek za 10 min, a stara
//     dobije upozorenje;
//   - član se provjeri odmah nakon rukovanja, i ponovno nakon primjene
//     članstava, ovlasti ili opoziva (ProvjeriSve);
//   - poruka čiji od nije ključ iz TLS-a se odbacuje;
//   - 6 poziva u minuti po pozivatelju, 2 sesije u tijeku prema istom cilju
//     od istog pozivatelja, 8 prema istom cilju ukupno; sesija je u tijeku
//     do odziva ili 30 s;
//   - više od 20 poruka u sekundi na jednoj vezi prekida vezu;
//   - po 2 rukovanja odjednom s jednog klijenta weba, s rokom od 2 s.

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"errors"
	"net/http"
	"slices"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

// Granice sastajališta
const (
	najvisePrijavljenih      = 64
	najviseRukovanjaKlijenta = 2
	najvisePozivaUMinuti     = 6
	najviseSesijaPara        = 2
	najviseSesijaCilja       = 8
	najvisePorukaUSekundi    = 20
	ponovnoNakonKopijeSek    = 600
	pamcenjeKopije           = time.Hour
)

// Rokovi sastajališta; var, da ih test skrati
var (
	rokRukovanjaSignala = 2 * time.Second
	rokStareVeze        = 5 * time.Second
	rokSesijeSignala    = 30 * time.Second
)

var (
	errNijePrijava = errors.New("prva poruka nije prijava")
	errKopija      = errors.New("isti ključ je spojen s drugog računala")
	errPunRegistar = errors.New("sastajalište drži najviše računala")
	errBrzina      = errors.New("previše poruka u sekundi")
	errNijeClan    = errors.New("računalo nije član mreže")
)

// Sastajaliste je poslužitelj signala
type Sastajaliste struct {
	// NaPoruku dobije potpisanu poruku kojoj je cilj samo sastajalište (F3);
	// postavlja se prije prve veze
	NaPoruku func(PotpisanaPoruka)

	priv     ed25519.PrivateKey
	moj      string // vlastiti ključ (PublicKeyString)
	protokol string
	tls      *tls.Config
	vjeruje  KeyChecker
	ime      func(ed25519.PublicKey) string // ime računala za dnevnik i kopije
	javi     func(string, ...any)
	sat      func() time.Time

	mu          sync.Mutex
	prijavljeni map[string]*prijavljeni // ključ → veza
	rukovanja   map[string]int          // klijent weba → rukovanja u tijeku
	sesije      map[string]sesijaSignala
	pozivi      map[string][]time.Time // pozivatelj → pozivi u zadnjoj minuti
	pozivaSat   []time.Time
	prekidi     []time.Time
	kopije      map[string]time.Time // ime računala → kad je kopija odbijena
	nepoznate   map[string]bool      // vrste koje su već zapisane u dnevnik
}

// sesijaSignala je poziv u tijeku: od koga, kome i do kad
type sesijaSignala struct {
	od, za string
	do     time.Time
}

// prijavljeni je jedno računalo na vezi
type prijavljeni struct {
	v        *vezaSignala
	kljuc    string
	proces   string
	ziv      chan struct{} // svaka primljena poruka; puls stare veze čeka na njemu
	sekunda  time.Time
	uSekundi int
}

// PostavkeSastajalista su ono što sastajalište treba od čvora
type PostavkeSastajalista struct {
	Priv     ed25519.PrivateKey
	Protokol string
	Vjeruje  KeyChecker
	Ime      func(ed25519.PublicKey) string
	Javi     func(string, ...any)
	Sat      func() time.Time
}

// NovoSastajaliste priprema sastajalište; veze prima Handler
func NovoSastajaliste(p PostavkeSastajalista) (*Sastajaliste, error) {
	cfg, err := tlsPosluziteljaSignala(p.Priv, p.Protokol)
	if err != nil {
		return nil, err
	}
	return &Sastajaliste{priv: p.Priv, moj: PublicKeyString(p.Priv.Public().(ed25519.PublicKey)), protokol: p.Protokol, tls: cfg,
		vjeruje: p.Vjeruje, ime: p.Ime, javi: p.Javi, sat: p.Sat,
		prijavljeni: map[string]*prijavljeni{}, rukovanja: map[string]int{}, sesije: map[string]sesijaSignala{},
		pozivi: map[string][]time.Time{}, kopije: map[string]time.Time{}, nepoznate: map[string]bool{}}, nil
}

// Handler prima WebSocket signala; drži vezu dok je računalo prijavljeno
func (s *Sastajaliste) Handler() http.Handler {
	return websocket.Server{
		// Izvor se ne provjerava, kao u tunelu: tko je s druge strane,
		// dokazuje se ključem unutar TLS-a
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler:   s.posluzi,
	}
}

// posluzi vodi jednu vezu: rukovanje, prijava, pa poruke do prekida
func (s *Sastajaliste) posluzi(ws *websocket.Conn) {
	ws.PayloadType = websocket.BinaryFrame
	nc := &wsVeza{Conn: ws, kraj: make(chan struct{}), put: PutSignala}
	defer nc.Close()
	v, err := s.rukovanje(klijentIz(ws.Request().Context()), nc)
	if err != nil {
		return
	}
	p, err := s.prijava(v)
	if err != nil {
		return
	}
	defer s.odjavi(p)
	s.citaj(p)
}

// rukovanje dovrši TLS signala u roku, najviše dva odjednom s istog
// klijenta weba, i provjeri da je druga strana član
func (s *Sastajaliste) rukovanje(klijent string, nc *wsVeza) (*vezaSignala, error) {
	if !s.uzmiRukovanje(klijent) {
		return nil, errPunRegistar
	}
	defer s.pustiRukovanje(klijent)
	tc := tls.Server(nc, s.tls)
	ctx, otkazi := context.WithTimeout(context.Background(), rokRukovanjaSignala)
	defer otkazi()
	if err := tc.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	kljuc, vj, _ := dokazaniKljuc(tc.ConnectionState().PeerCertificates, s.protokol) // VerifyConnection ga je već provjerio
	v := &vezaSignala{c: tc, kljuc: kljuc, vj: vj}
	if !s.vjeruje(kljuc, vj) {
		_ = v.posalji(SignalOdbijeno, odbijenoSignala{Kod: KodSignalaNijeClan}) // veza se ionako zatvara
		return nil, errNijeClan
	}
	return v, nil
}

func (s *Sastajaliste) uzmiRukovanje(klijent string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rukovanja[klijent] >= najviseRukovanjaKlijenta {
		return false
	}
	s.rukovanja[klijent]++
	return true
}

func (s *Sastajaliste) pustiRukovanje(klijent string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rukovanja[klijent]--; s.rukovanja[klijent] <= 0 {
		delete(s.rukovanja, klijent)
	}
}

// prijava čeka prijavu, upiše računalo u registar i potvrdi ga
func (s *Sastajaliste) prijava(v *vezaSignala) (*prijavljeni, error) {
	por, err := v.primi(rokPrijave)
	if err != nil {
		return nil, err
	}
	var pr prijavaSignala
	if por.Vrsta != SignalPrijava || tijeloIz(por, &pr) != nil || pr.Proces == "" {
		return nil, errNijePrijava
	}
	p := &prijavljeni{v: v, kljuc: PublicKeyString(v.kljuc), proces: pr.Proces, ziv: make(chan struct{}, 1)}
	if err := s.upisi(p); err != nil {
		_ = v.posalji(SignalOdbijeno, odbijenicaZa(err)) // veza se ionako zatvara
		return nil, err
	}
	return p, v.posalji(SignalPrijavljen, prijavljenSignala{Ver: 1, Oznaka: oznakaSastajalista, Vrijeme: s.sat().UnixMilli(),
		PulsSek: int(razmakPulsa / time.Second)})
}

// odbijenicaZa je odbijenica za grešku upisa
func odbijenicaZa(err error) odbijenoSignala {
	if errors.Is(err, errKopija) {
		return odbijenoSignala{Kod: KodSignalaKopija, Razlog: err.Error(), PonovnoZaSek: ponovnoNakonKopijeSek}
	}
	return odbijenoSignala{Kod: KodSignalaNedostupno, Razlog: err.Error()}
}

// upisi upiše računalo u registar. Isti proces zamijeni svoju staru vezu;
// drugi proces s istim ključem samo kad stara veza ne odgovori na puls.
func (s *Sastajaliste) upisi(p *prijavljeni) error {
	stara, err := s.zauzmi(p)
	if err != nil || stara == nil {
		return err
	}
	if stara.proces != p.proces && stara.odgovara() {
		s.zapamtiKopiju(p.v.kljuc)
		_ = stara.v.posalji(SignalUpozorenje, upozorenjeSignala{Kod: KodSignalaKopija}) // stara ostaje; kad ne primi, ionako je mrtva
		return errKopija
	}
	s.zamijeni(p)
	return nil
}

// zauzmi upiše novo računalo kad ključa nema; inače vrati staru vezu
func (s *Sastajaliste) zauzmi(p *prijavljeni) (*prijavljeni, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stara := s.prijavljeni[p.kljuc]; stara != nil {
		return stara, nil
	}
	if len(s.prijavljeni) >= najvisePrijavljenih {
		return nil, errPunRegistar
	}
	s.prijavljeni[p.kljuc] = p
	return nil, nil
}

// zamijeni stavi novu vezu na mjesto ključa i zatvori onu koja je bila na
// njemu (stara, ili u međuvremenu neka treća)
func (s *Sastajaliste) zamijeni(nova *prijavljeni) {
	s.mu.Lock()
	bila := s.prijavljeni[nova.kljuc]
	s.prijavljeni[nova.kljuc] = nova
	s.mu.Unlock()
	if bila != nil && bila != nova {
		bila.v.zatvori()
	}
}

// odgovara pošalje puls koji traži odgovor i čeka bilo koju poruku
func (p *prijavljeni) odgovara() bool {
	select {
	case <-p.ziv:
	default:
	}
	_ = p.v.posalji(SignalPuls, pulsSignala{Odgovori: true}) // kad ne prođe, odgovora ionako nema
	t := time.NewTimer(rokStareVeze)
	defer t.Stop()
	select {
	case <-p.ziv:
		return true
	case <-t.C:
		return false
	}
}

// zapamtiKopiju zapiše računalo čija je kopija odbijena
func (s *Sastajaliste) zapamtiKopiju(kljuc ed25519.PublicKey) {
	ime := s.ime(kljuc)
	s.javi("povezivost: sastajalište je odbilo drugu kopiju računala %s (isti ključ, drugi proces)", ime)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kopije[ime] = s.sat()
}

// odjavi makne vezu iz registra, kad je to još ona, i broji prekid
func (s *Sastajaliste) odjavi(p *prijavljeni) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prijavljeni[p.kljuc] == p {
		delete(s.prijavljeni, p.kljuc)
	}
	s.prekidi = append(s.prekidi, s.sat())
}

// citaj prima poruke dok veza traje
func (s *Sastajaliste) citaj(p *prijavljeni) {
	for {
		por, err := p.v.primi(rokCitanjaSignala)
		if err != nil || !p.unutarBrzine(s.sat()) {
			return
		}
		select {
		case p.ziv <- struct{}{}:
		default:
		}
		s.obradi(p, por)
	}
}

// unutarBrzine broji poruke u tekućoj sekundi; čita samo petlja veze
func (p *prijavljeni) unutarBrzine(sad time.Time) bool {
	if sad.Sub(p.sekunda) >= time.Second {
		p.sekunda, p.uSekundi = sad, 0
	}
	p.uSekundi++
	return p.uSekundi <= najvisePorukaUSekundi
}

// obradi jednu poruku računala
func (s *Sastajaliste) obradi(p *prijavljeni, por porukaSignala) {
	switch {
	case por.Vrsta == SignalPuls:
		_ = p.v.posalji(SignalPuls, pulsSignala{}) // odgovor na puls; kad ne prođe, petlja čitanja vidi prekid
	case jePotpisana(por.Vrsta):
		s.proslijediOd(p, por)
	default:
		s.nepoznataVrsta(por.Vrsta)
	}
}

// nepoznataVrsta zapiše nepoznatu vrstu u dnevnik, jednom
func (s *Sastajaliste) nepoznataVrsta(vrsta string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.nepoznate[vrsta] && len(s.nepoznate) < 16 {
		s.nepoznate[vrsta] = true
		s.javi("povezivost: sastajalište ne zna poruku signala %.32q, pa je zanemaruje", vrsta)
	}
}

// proslijediOd provjeri da je poruka od računala na toj vezi i preda je
// cilju; kad ne može, javi pošiljatelju zašto
func (s *Sastajaliste) proslijediOd(p *prijavljeni, por porukaSignala) {
	pp, err := potpisanaIz(por)
	t, errZ := pp.Zaglavlje()
	if err != nil || errZ != nil || t.Od != p.kljuc {
		s.javi("povezivost: sastajalište je odbacilo poruku %s od %s: pošiljatelj u poruci nije računalo na vezi", por.Vrsta, s.ime(p.v.kljuc))
		return
	}
	razlog := s.dopusteno(por.Vrsta, t)
	if razlog == "" {
		razlog = s.isporuci(por, pp, t)
	}
	if razlog != "" {
		_ = p.v.posalji(SignalNema, nemaSignala{Sesija: t.Sesija, Razlog: razlog}) // pošiljatelju; kad ne prođe, veza je prekinuta
	}
}

// isporuci preda poruku cilju: sebi (NaPoruku) ili računalu na vezi;
// vraća razlog kad cilja nema
func (s *Sastajaliste) isporuci(por porukaSignala, pp PotpisanaPoruka, t TijeloPoruke) string {
	if t.Za == s.moj {
		if s.NaPoruku != nil {
			s.NaPoruku(pp)
		}
		return ""
	}
	s.mu.Lock()
	cilj := s.prijavljeni[t.Za]
	s.mu.Unlock()
	if cilj == nil || cilj.v.proslijedi(por) != nil {
		return NemaNijePrijavljen
	}
	return ""
}

// dopusteno primijeni granice poziva i vodi sesije u tijeku; vraća razlog
// kad poziv ne smije dalje
func (s *Sastajaliste) dopusteno(vrsta string, t TijeloPoruke) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	sad := s.sat()
	s.ocistiSesije(sad)
	switch vrsta {
	case VrstaOdziv:
		delete(s.sesije, t.Sesija)
		return ""
	case VrstaPoziv:
		return s.noviPoziv(t, sad)
	}
	return ""
}

// noviPoziv: 6 poziva u minuti po pozivatelju i sesije u tijeku prema
// cilju; prolaz upiše sesiju
func (s *Sastajaliste) noviPoziv(t TijeloPoruke, sad time.Time) string {
	nedavni := slices.DeleteFunc(s.pozivi[t.Od], func(k time.Time) bool { return sad.Sub(k) >= time.Minute })
	s.pozivi[t.Od] = nedavni
	if len(nedavni) >= najvisePozivaUMinuti {
		return NemaPrevisePoziva
	}
	para, cilja := s.sesijePrema(t.Od, t.Za)
	if para >= najviseSesijaPara || cilja >= najviseSesijaCilja {
		return NemaPreviseSesija
	}
	s.pozivi[t.Od] = append(nedavni, sad)
	s.pozivaSat = append(s.pozivaSat, sad)
	s.sesije[t.Sesija] = sesijaSignala{od: t.Od, za: t.Za, do: sad.Add(rokSesijeSignala)}
	return ""
}

// sesijePrema broji sesije u tijeku od pozivatelja prema cilju i sve prema
// cilju
func (s *Sastajaliste) sesijePrema(od, za string) (para, cilja int) {
	for _, ss := range s.sesije {
		if ss.za != za {
			continue
		}
		cilja++
		if ss.od == od {
			para++
		}
	}
	return para, cilja
}

// ocistiSesije makne sesije kojima je prošao rok, a pozive i prekide
// starije od onoga što se broji
func (s *Sastajaliste) ocistiSesije(sad time.Time) {
	for k, ss := range s.sesije {
		if sad.After(ss.do) {
			delete(s.sesije, k)
		}
	}
	s.pozivaSat = slices.DeleteFunc(s.pozivaSat, func(k time.Time) bool { return sad.Sub(k) > time.Hour })
	s.prekidi = slices.DeleteFunc(s.prekidi, func(k time.Time) bool { return sad.Sub(k) > 24*time.Hour })
}

// ProvjeriSve odjavi računala koja više nisu članovi; zove se nakon
// primjene članstava, ovlasti i opoziva
func (s *Sastajaliste) ProvjeriSve() {
	s.mu.Lock()
	var van []*prijavljeni
	for k, p := range s.prijavljeni {
		if !s.vjeruje(p.v.kljuc, p.v.vj) {
			van = append(van, p)
			delete(s.prijavljeni, k)
		}
	}
	s.mu.Unlock()
	for _, p := range van {
		_ = p.v.posalji(SignalOdbijeno, odbijenoSignala{Kod: KodSignalaNijeClan}) // veza se ionako zatvara
		p.v.zatvori()
	}
}

// Posalji preda potpisanu poruku sastajališta računalu kojem ide (kad
// sastajalište samo zove); greška kad ono nije na vezi
func (s *Sastajaliste) Posalji(pp PotpisanaPoruka) error {
	t, err := pp.Zaglavlje()
	if err != nil {
		return err
	}
	s.mu.Lock()
	cilj := s.prijavljeni[t.Za]
	s.mu.Unlock()
	if cilj == nil {
		return errors.New(NemaNijePrijavljen)
	}
	return cilj.v.posalji(pp.Vrsta, pp)
}

// StanjeSastajalista su brojke za pločicu stalnog čvora
type StanjeSastajalista struct {
	Prijavljenih, PozivaSat, Prekida24h int
	Kopije                              []string // računala čiju je kopiju odbilo u zadnjih sat vremena
}

// Stanje su brojke sastajališta sada
func (s *Sastajaliste) Stanje() StanjeSastajalista {
	s.mu.Lock()
	defer s.mu.Unlock()
	sad := s.sat()
	s.ocistiSesije(sad)
	st := StanjeSastajalista{Prijavljenih: len(s.prijavljeni), PozivaSat: len(s.pozivaSat), Prekida24h: len(s.prekidi)}
	for ime, kad := range s.kopije {
		if sad.Sub(kad) > pamcenjeKopije {
			delete(s.kopije, ime)
			continue
		}
		st.Kopije = append(st.Kopije, ime)
	}
	slices.Sort(st.Kopije)
	return st
}

// Zatvori prekine sve veze; nove i dalje prima dok ga web poslužuje
func (s *Sastajaliste) Zatvori() {
	s.mu.Lock()
	sve := make([]*prijavljeni, 0, len(s.prijavljeni))
	for _, p := range s.prijavljeni {
		sve = append(sve, p)
	}
	s.mu.Unlock()
	for _, p := range sve {
		p.v.zatvori()
	}
}
