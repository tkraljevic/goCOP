package razmjena

// WEB sync od poziva do razgovora (plan WEB synca, F3a-2), za obje strane.
// F3b samo zove:
//   - NoviWebSync s ključem, imenom protokola, vjerodajnicama, trusted,
//     zajedničkom ogradom razmjena, obradom razgovora, slanjem preko
//     sastajališta i vlastitim kandidatima;
//   - Radi dok vrijedi web_sync = true: slušalica QUIC-a i red poruka
//     sastajališta;
//   - NaPoruku i NaNemu iz signalnog klijenta ili sastajališta;
//   - Pripremi kad ovo računalo zove drugo: poziv, odziv, provjera
//     kandidata druge strane, udarci, probijanje, QUIC i pozdrav. Vraća
//     sesiju na kojoj Razgovor otvara kanal 0x01;
//   - Sesija za živu sesiju s računalom i ProvjeriSve nakon primjene
//     članstava, ovlasti ili opoziva.
//
// Pozvani sve to radi sam kad stigne poziv, a gotovu sesiju (obiju uloga)
// javlja NaSesiju.
//
// Pravila:
//   - NaPoruku samo stavi poruku u red od 64 (pun red je odbacuje), pa je
//     kratak na petlji čitanja signala; red čita jedna gorutina.
//   - Odziv i sinkro idu pripremi svoje sesije, a ona prije provjere potpisa
//     i pričuve poruka (ProvjeraPoruka.Provjeri) pita stroj prima li ih
//     (PrimaOdziv, PrimaSinkro), pa tuđ, ponovljen ili zakašnjeli odziv i
//     sinkro ne pune pričuvu. Poruka nepoznate sesije odbacuje se odmah.
//   - Poziv i odziv nose vjerodajnice pošiljatelja (poziv.go), a sesija
//     drugu stranu provjerava s njima (vjDruge): slušalica šalje
//     certifikat bez njih, pa ih A inače ne bi imao.
//   - Ključevi udaraca slažu se iz tajne poziva i tajne odziva (udarac.go).
//   - Kandidati druge strane prolaze kandidatiDruge (bez Tailscalea).
//   - QUIC zove A, a ishod poziva vrijedi samo za radnju QUIC koja ga je
//     pokrenula: ishod starije radnje se odbacuje, a njegova veza zatvara.
//   - Jedna priprema po računalu. Drugi Pripremi prema istom računalu vraća
//     ErrPripremaUTijeku, a noviji poziv istog računala prekida stariji.
//     Kad se dva računala zovu istodobno, vrijedi poziv računala s manjim
//     ključem: ono tuđi poziv odbije (RazlogIstodobno), a računalo s većim
//     ključem svoju pripremu prekine (RazlogDrugaZove) i primi tuđi poziv.
//   - Najviše 4 pripreme pozvanog odjednom; peti poziv dobije „zauzet”.
//   - Veza koja stigne kad je priprema gotova, ili ostane na ulazu, se
//     zatvara (predaj, pocisti).
//   - Jedna sesija po računalu: nova zatvori stariju.
//   - Sesija sa samim sobom (isti ključ) se ne priprema i ne prima.

import (
	"bytes"
	"cmp"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"maps"
	"net"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"

	"gocop/internal/tailscale"
)

const (
	najvisePripremaPozvanog = 4
	redPorukaWeb            = 64
	velicinaTajne           = 32
)

// Razlozi prekida pripreme, uz razloge probijanja (RazlogBezOdziva i ostali)
const (
	RazlogIstodobno       = "istodobno"  // druga strana zove istodobno, a vrijedi poziv ovog računala
	RazlogDrugaZove       = "druga-zove" // druga strana zove istodobno, a vrijedi njezin poziv
	RazlogNoviPoziv       = "novi-poziv" // druga strana je poslala noviji poziv
	RazlogPripremaZauzeta = "zauzet"     // pozvani već priprema najviše odjednom
	RazlogNema            = "nema"       // sastajalište javlja da druge strane nema (uz razlog sastajališta)
	RazlogStao            = "stao"       // WEB sync na ovom računalu je stao
)

// Greške pripreme
var (
	ErrSesijaSaSobom   = errors.New("WEB sync sa samim sobom se ne priprema")
	ErrPripremaUTijeku = errors.New("WEB sync s tim računalom već se priprema")
	ErrWebSyncNeRadi   = errors.New("WEB sync na ovom računalu ne radi")
	ErrWebSyncVecRadi  = errors.New("WEB sync na ovom računalu već radi")
	errNePusta         = errors.New("s te adrese nije stigao valjan udarac")
	errNijeDrugaStrana = errors.New("ključ nije ključ druge strane ove sesije")
	errDrugaSesija     = errors.New("pozdrav nosi drugu sesiju")
)

// GreskaPripreme je priprema koja nije uspjela: razlog (Razlog*) i u kojem
// je pokušaju stala
type GreskaPripreme struct {
	Razlog  string
	Pokusaj int
}

func (e GreskaPripreme) Error() string { return "WEB sync nije uspio: " + e.Razlog }

// PostavkeWeb su ono što WEB sync traži od programa. Priv, Protokol,
// Trusted, Handle, Posalji, Sastajaliste i Mreza su obavezni; ostalo ima
// zadanu vrijednost.
type PostavkeWeb struct {
	Priv     ed25519.PrivateKey
	Protokol string
	Vj       func() []byte // vjerodajnice ovog računala; zadano bez njih
	Trusted  KeyChecker
	Ograda   *Ograda     // ista kao uz port i tunel; zadano vlastita
	Handle   func(*Conn) // dolazni razgovor na kanalu 0x01
	// Posalji šalje potpisanu poruku preko sastajališta (KlijentSignala ili
	// Sastajaliste)
	Posalji      func(PotpisanaPoruka) error
	Sastajaliste ed25519.PublicKey // ključ vlastitog sastajališta; na sastajalištu vlastiti
	// Mreza su vlastiti kandidati i vrsta mreže za poziv i odziv
	Mreza func() ([]KandidatPoruke, *NATPoruke)
	// Prima je razlog zašto ovo računalo ne prima poziv od ključa; prazno
	// kad ga prima (zadano)
	Prima    func(ed25519.PublicKey) string
	NaSesiju func(*SesijaWeb) // nova sesija, u obje uloge; zadano ništa
	Znanje   func() *Znanje   // za pozdrav; zadano bez njega
}

// WebSync je WEB sync jednog računala na njegovoj UDP utičnici
type WebSync struct {
	PostavkeWeb
	u        *UDP
	moj      ed25519.PublicKey
	mojS     string
	tls      *tls.Config
	vrata    *vrata
	pricuva  *PricuvaPoruka
	red      chan primljena
	Odbaceno atomic.Int64 // poruke koje nisu stale u red
	radi     atomic.Bool
	izlaz    izlazFunc
	ts       func() []netip.Addr

	mu       sync.Mutex
	pripreme map[string]*priprema  // po ključu druge strane
	sesije   map[string]*SesijaWeb // po ključu druge strane
	gorutine sync.WaitGroup
}

// primljena je poruka sastajališta u redu, i kad je stigla
type primljena struct {
	pp  PotpisanaPoruka
	kad time.Time
}

// NoviWebSync je WEB sync na utičnici u; ne radi dok ga ne pokrene Radi
func NoviWebSync(u *UDP, p PostavkeWeb) (*WebSync, error) {
	w := &WebSync{PostavkeWeb: p, u: u, pricuva: NovaPricuvaPoruka(), red: make(chan primljena, redPorukaWeb),
		izlaz: izlaznaAdresa, ts: tailscale.VlastiteAdrese, pripreme: map[string]*priprema{}, sesije: map[string]*SesijaWeb{}}
	cfg, err := tlsPosluziteljaWeb(p.Priv, p.Protokol, w.provjeriKlijenta)
	if err != nil {
		return nil, err
	}
	w.tls, w.moj = cfg, p.Priv.Public().(ed25519.PublicKey)
	w.mojS = PublicKeyString(w.moj)
	w.zadano()
	w.vrata = &vrata{protokol: p.Protokol, trusted: p.Trusted, handle: p.Handle, o: w.Ograda}
	return w, nil
}

// zadano popuni neobavezne postavke
func (w *WebSync) zadano() {
	if w.Vj == nil {
		w.Vj = func() []byte { return nil }
	}
	if w.Ograda == nil {
		w.Ograda = NovaOgrada()
	}
	if w.Prima == nil {
		w.Prima = func(ed25519.PublicKey) string { return "" }
	}
	if w.NaSesiju == nil {
		w.NaSesiju = func(*SesijaWeb) {}
	}
	if w.Znanje == nil {
		w.Znanje = func() *Znanje { return nil }
	}
}

// Radi drži slušalicu QUIC-a i obrađuje poruke sastajališta dok ctx traje;
// na kraju zatvori sesije i prekine pripreme. Greška kad već radi ili kad
// se slušalica ne da otvoriti. Na jednoj utičnici radi jedan WebSync.
func (w *WebSync) Radi(ctx context.Context) error {
	if !w.radi.CompareAndSwap(false, true) {
		return ErrWebSyncVecRadi
	}
	ln, err := slusajWeb(w.u.tr, w.tls, w.vrataQUIC)
	if err != nil {
		w.radi.Store(false)
		return err
	}
	ctx, otkazi := context.WithCancel(ctx)
	w.gorutine.Go(func() { w.primaj(ctx, ln) })
	w.razvrstavaj(ctx)
	w.radi.Store(false)
	otkazi()
	_ = ln.Close() // slušalica staje s Radi; greška zatvaranja nema kome reći
	w.zatvoriSve()
	w.gorutine.Wait()
	return nil
}

// NaPoruku preda potpisanu poruku sastajališta WEB syncu. Samo je stavi u
// red, pa je smije zvati petlja čitanja signala; pun red poruku odbaci.
func (w *WebSync) NaPoruku(pp PotpisanaPoruka) {
	select {
	case w.red <- primljena{pp: pp, kad: time.Now()}:
	default:
		w.Odbaceno.Add(1)
	}
}

// NaNemu: sastajalište javlja da poruku sesije nije proslijedilo (druga
// strana nije prijavljena, previše poziva), pa njezina priprema staje odmah
func (w *WebSync) NaNemu(sesija, razlog string) {
	if p := w.pripremaSesije(sesija); p != nil {
		p.otkazi(GreskaPripreme{Razlog: RazlogNema + ": " + razlog})
	}
}

// Pripremi pripremi WEB sync s drugim računalom: pošalje poziv i vodi
// probijanje do sesije ili do neuspjeha (GreskaPripreme)
func (w *WebSync) Pripremi(ctx context.Context, druga ed25519.PublicKey) (*SesijaWeb, error) {
	if druga.Equal(w.moj) {
		return nil, ErrSesijaSaSobom
	}
	if !w.radi.Load() {
		return nil, ErrWebSyncNeRadi
	}
	ctx, otkazi := context.WithCancelCause(ctx)
	defer otkazi(nil)
	p := w.novaPriprema(otkazi, druga, NovaSesija(), true)
	if err := w.zauzmiPozivatelja(p); err != nil {
		return nil, err
	}
	poziv := p.potpisi(VrstaPoziv, TijeloPoruke{Tajna: p.tajna, Kandidati: p.kandidati, NAT: p.nat, Vj: w.Vj()})
	t, _ := poziv.Zaglavlje() // tijelo je upravo zapisano, pa se da pročitati
	p.p = NovoProbijanjePozivatelja(t, p.a, time.Now())
	if err := w.Posalji(poziv); err != nil {
		w.otpusti(p)
		return nil, err
	}
	return p.vozi(ctx)
}

// Sesija je živa sesija s računalom; nil kad je nema
func (w *WebSync) Sesija(druga ed25519.PublicKey) *SesijaWeb {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sesije[PublicKeyString(druga)]
}

// ProvjeriSve ponovno provjeri članstvo druge strane svake sesije: nakon
// primjene novih članstava, ovlasti ili opoziva nečlan dobiva 0x0104.
// Zatvaranja idu usporedo, a ProvjeriSve ih čeka, do 1 s.
func (w *WebSync) ProvjeriSve() {
	var zatvaranja sync.WaitGroup
	for _, s := range w.zive() {
		if !w.Trusted(s.Druga, s.vjDruge) {
			zatvaranja.Go(func() { s.Zatvori(KodNijeClan) })
		}
	}
	zatvaranja.Wait()
}

// zive su sesije sada
func (w *WebSync) zive() []*SesijaWeb {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Collect(maps.Values(w.sesije))
}

// zatvoriSve zatvori sesije i prekine pripreme; Radi zatim čeka gorutine
func (w *WebSync) zatvoriSve() {
	for _, s := range w.zive() {
		w.gorutine.Go(func() {
			s.Zatvori(0)
			s.Cekaj()
		})
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, p := range w.pripreme {
		p.otkazi(GreskaPripreme{Razlog: RazlogStao})
	}
}

// razvrstavaj čita red poruka dok ctx traje
func (w *WebSync) razvrstavaj(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-w.red:
			w.razvrstaj(ctx, m)
		}
	}
}

// razvrstaj: poziv ide pozvanome, a odziv i sinkro pripremi svoje sesije,
// čiji ih stroj prima ili odbaci prije provjere potpisa
func (w *WebSync) razvrstaj(ctx context.Context, m primljena) {
	t, err := m.pp.Zaglavlje()
	switch {
	case err != nil:
	case m.pp.Vrsta == VrstaPoziv:
		w.naPoziv(ctx, m)
	default:
		if p := w.pripremaSesije(t.Sesija); p != nil {
			p.ponudi(func() (Dogadjaj, bool) { return p.naPoruku(m.pp, t) })
		}
	}
}

// pripremaSesije je priprema sesije; nil kad je nema
func (w *WebSync) pripremaSesije(sesija string) *priprema {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, p := range w.pripreme {
		if p.sesija == sesija {
			return p
		}
	}
	return nil
}

// provjera je provjera potpisanih poruka ovog računala
func (w *WebSync) provjera() ProvjeraPoruka {
	return ProvjeraPoruka{Protokol: w.Protokol, Moj: w.moj, Sastajaliste: w.Sastajaliste, Vjeruje: w.Trusted, Sat: time.Now, Pricuva: w.pricuva}
}

// naPoziv: provjeren poziv drugog računala; pozvani odgovori odzivom i
// krene probijanje, ili odbije
func (w *WebSync) naPoziv(ctx context.Context, m primljena) {
	t, err := w.provjera().Provjeri(m.pp)
	if err != nil || t.Od == w.mojS {
		return
	}
	p, err := w.novaPripremaPozvanog(ctx, t)
	if razlog := w.razlogOdbijanja(p, err); razlog != "" {
		p.pocisti()
		w.odbijPoziv(t, razlog)
		return
	}
	w.prihvatiPoziv(p, t, m.kad)
	w.gorutine.Go(func() {
		_, _ = p.vozi(p.ctx) // sesiju javlja NaSesiju, a neuspjeh pozvanoga nema kome reći
	})
}

// razlogOdbijanja je zašto pozvani ne prima poziv: ključevi udaraca se ne
// daju izvesti, program ga ne prima (Prima) ili ga ne prima zauzmiPozvanog,
// koji pripremu inače upiše; prazno kad ga prima
func (w *WebSync) razlogOdbijanja(p *priprema, greskaUdaraca error) string {
	if greskaUdaraca != nil {
		return RazlogOdbijeno
	}
	if razlog := w.Prima(p.druga); razlog != "" {
		return razlog
	}
	return w.zauzmiPozvanog(p)
}

// novaPripremaPozvanog je priprema računala koje je primilo poziv t, s
// udarcima: tajna poziva i vlastita tajna odziva
func (w *WebSync) novaPripremaPozvanog(ctx context.Context, t TijeloPoruke) (*priprema, error) {
	druga, _ := ParsePublicKey(t.Od) // Provjeri ga je već pročitao
	ctx, otkazi := context.WithCancelCause(ctx)
	p := w.novaPriprema(otkazi, druga, t.Sesija, false)
	p.ctx, p.vjDruge = ctx, t.Vj
	return p, p.pokreniUdarce(t.Tajna, p.tajna, t.Kandidati)
}

// prihvatiPoziv pošalje odziv „probaj” i postavi stroj pozvanoga
func (w *WebSync) prihvatiPoziv(p *priprema, t TijeloPoruke, primljen time.Time) {
	odziv := p.potpisi(VrstaOdziv, TijeloPoruke{Odluka: OdlukaProbaj, Tajna: p.tajna, Kandidati: p.kandidati, NAT: p.nat,
		Vj: w.Vj(), ObradaMs: int(time.Since(primljen).Milliseconds())})
	_ = w.Posalji(odziv) // bez odziva pozivatelj stane nakon 10 s, a pozvani nakon 5 s bez sinkra
	p.p = NovoProbijanjePozvanog(t, p.a, p.nat, time.Now())
}

// odbijPoziv pošalje odziv „odbij” s razlogom
func (w *WebSync) odbijPoziv(t TijeloPoruke, razlog string) {
	odziv := w.potpisi(VrstaOdziv, t.Sesija, t.Od, TijeloPoruke{Odluka: OdlukaOdbij, Razlog: razlog})
	_ = w.Posalji(odziv) // bez odziva pozivatelj stane nakon 10 s
}

// potpisi potpiše poruku sesije za računalo za preko vlastitog sastajališta
func (w *WebSync) potpisi(vrsta, sesija, za string, t TijeloPoruke) PotpisanaPoruka {
	t.Sesija, t.Za, t.Sastajaliste = sesija, za, PublicKeyString(w.Sastajaliste)
	return Potpisi(w.Priv, w.Protokol, vrsta, t)
}

// zauzmiPozivatelja upiše pripremu ovog računala kao pozivatelja
func (w *WebSync) zauzmiPozivatelja(p *priprema) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pripreme[p.drugaS] != nil {
		return ErrPripremaUTijeku
	}
	w.pripreme[p.drugaS] = p
	return nil
}

// zauzmiPozvanog upiše pripremu pozvanog; razlog kad je ne prima. Stariju
// pripremu s istim računalom prekida, osim kad istodobno zove ovo računalo
// s manjim ključem (pravila u zaglavlju).
func (w *WebSync) zauzmiPozvanog(p *priprema) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	stara := w.pripreme[p.drugaS]
	switch {
	case stara == nil && w.pozvanih() >= najvisePripremaPozvanog:
		return RazlogPripremaZauzeta
	case stara != nil && stara.pozivatelj && p.a:
		return RazlogIstodobno
	case stara != nil:
		stara.otkazi(GreskaPripreme{Razlog: razlogZamjene(stara)})
	}
	w.pripreme[p.drugaS] = p
	return ""
}

// razlogZamjene je zašto stara priprema s istim računalom staje
func razlogZamjene(stara *priprema) string {
	if stara.pozivatelj {
		return RazlogDrugaZove
	}
	return RazlogNoviPoziv
}

// pozvanih je broj priprema pozvanoga; pod w.mu
func (w *WebSync) pozvanih() int {
	n := 0
	for _, p := range w.pripreme {
		if !p.pozivatelj {
			n++
		}
	}
	return n
}

// otpusti makne pripremu, kad je još upisana
func (w *WebSync) otpusti(p *priprema) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pripreme[p.drugaS] == p {
		delete(w.pripreme, p.drugaS)
	}
}

// vrataQUIC je ConnContext slušalice: pušta samo adresu s koje je u zadnjih
// 30 s stigao valjan paket druge strane neke pripreme, i uz vezu zapamti te
// pripreme. Odbijenu adresu broji ograda kao rukovanje (pravilo 18): ključ
// se tu još ne zna, a do TLS-a nije došla.
func (w *WebSync) vrataQUIC(ctx context.Context, ci *quic.ClientInfo) (context.Context, error) {
	pp := w.pustaju(ci.RemoteAddr)
	if len(pp) == 0 {
		w.Ograda.odbij(VrataRukovanje, nil)
		return ctx, errNePusta
	}
	return context.WithValue(ctx, kljucPustenih{}, pp), nil
}

// kljucPustenih je ključ konteksta veze s pripremama koje su je pustile
type kljucPustenih struct{}

// pustaju su pripreme čiji udarci puštaju adresu
func (w *WebSync) pustaju(a net.Addr) []*priprema {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []*priprema
	for _, p := range w.pripreme {
		if x := p.x.Load(); x != nil && x.Pusta(a) {
			out = append(out, p)
		}
	}
	return out
}

// provjeriKlijenta usred rukovanja: certifikat čvora s istim imenom
// protokola, a ključ je ključ druge strane jedne od priprema koje su vezu
// pustile, nikad vlastiti
func (w *WebSync) provjeriKlijenta(ctx context.Context, cs tls.ConnectionState) error {
	kljuc, _, err := dokazaniKljuc(cs.PeerCertificates, w.Protokol)
	if err != nil {
		w.Ograda.odbij(razlogKljuca(err), nil)
		return err
	}
	pp, _ := ctx.Value(kljucPustenih{}).([]*priprema) // bez priprema ključ ne odgovara nijednoj
	if kljuc.Equal(w.moj) || !slices.ContainsFunc(pp, func(p *priprema) bool { return p.druga.Equal(kljuc) }) {
		w.Ograda.odbij(VrataNijeClan, kljuc)
		return errNijeDrugaStrana
	}
	return nil
}

// primaj prima veze sa slušalice dok ctx traje
func (w *WebSync) primaj(ctx context.Context, ln *quic.Listener) {
	for {
		veza, err := ln.Accept(ctx)
		if err != nil {
			return
		}
		w.gorutine.Go(func() { w.prihvati(ctx, veza) })
	}
}

// prihvati: B čeka kontrolni tok s pozdravom sesije neke pripreme koja je
// vezu pustila i preda vezu njezinom stroju; inače vezu zatvara (i vezu s
// pozdravom starijeg pokušaja)
func (w *WebSync) prihvati(ctx context.Context, veza *quic.Conn) {
	v, p, err := w.pozdravljen(ctx, veza)
	if err != nil {
		w.Ograda.odbij(VrataRukovanje, nil)
		_ = veza.CloseWithError(quic.ApplicationErrorCode(KodOdbijeno), "") // veza bez pozdrava ove sesije ne treba nikome
		return
	}
	p.predaj(v, func() (Dogadjaj, bool) { return p.naDolaznu(v) })
}

// pozdravljen čeka kontrolni tok i pozdrav, u roku od 5 s, i nađe
// pripremu po sesiji iz pozdrava i ključu iz TLS-a
func (w *WebSync) pozdravljen(ctx context.Context, veza *quic.Conn) (*vezaSesije, *priprema, error) {
	ctx, otkazi := context.WithTimeout(ctx, rokPozdrava)
	defer otkazi()
	tok, err := veza.AcceptStream(ctx)
	if err != nil {
		return nil, nil, err
	}
	_ = tok.SetReadDeadline(time.Now().Add(rokPozdrava)) // rok toka QUIC-a uvijek se da postaviti
	kanal, err := citajKanal(tok)
	if err != nil || kanal != kanalKontrola {
		return nil, nil, errNijePozdrav
	}
	poz, err := procitajPozdrav(tok)
	if err != nil {
		return nil, nil, err
	}
	_ = tok.SetReadDeadline(time.Time{}) // isto
	p := pripremaVeze(veza, poz.Sesija)
	if p == nil {
		return nil, nil, errDrugaSesija
	}
	return &vezaSesije{veza: veza, kontrola: tok, znanje: poz.Znanje}, p, nil
}

// pripremaVeze je priprema koja je vezu pustila, sa sesijom iz pozdrava i
// ključem druge strane iz TLS-a; nil kad je nema
func pripremaVeze(veza *quic.Conn, sesija string) *priprema {
	pp, _ := veza.Context().Value(kljucPustenih{}).([]*priprema) // ConnContext ih je uvijek zapamtio
	kljuc := veza.ConnectionState().TLS.PeerCertificates[0].PublicKey
	for _, p := range pp {
		if p.sesija == sesija && p.druga.Equal(kljuc) {
			return p
		}
	}
	return nil
}

// zovi: A nazove adresu druge strane QUIC-om, otvori kontrolni tok i
// razmijeni pozdrav
func (w *WebSync) zovi(ctx context.Context, p *priprema, na netip.AddrPort) (*vezaSesije, error) {
	cfg, _ := tlsKlijentaWeb(w.Priv, w.Protokol, w.Vj, na.String(), p.druga) // ključ je provjerio NoviWebSync; bez konfiguracije Dial vrati grešku
	pocetak := time.Now()
	veza, err := w.u.tr.Dial(ctx, net.UDPAddrFromAddrPort(na), cfg, postavkeQUIC())
	if err != nil {
		return nil, err
	}
	v := &vezaSesije{veza: veza, rtt: time.Since(pocetak)}
	return v, v.pozdravi(ctx, p.sesija, w.Znanje())
}

// novaSesija je sesija iz veze uspjele pripreme. B tek sada pozdravi A, pa
// A zna da je B u istoj sesiji. Vjerodajnice druge strane su iz njezinog
// provjerenog poziva ili odziva: slušalica šalje certifikat bez njih, pa
// ih A inače ne bi imao. Sesija je postavljena prije nego što je upisana,
// jer je odmah nakon upisa smiju zatvoriti ProvjeriSve, kraj Radi i nova
// sesija.
func (w *WebSync) novaSesija(p *priprema) *SesijaWeb {
	v := p.veza
	if !p.a {
		_ = pisiKontrolu(v.kontrola, porukaKontrole{Vrsta: kontrolaPozdrav, Ver: 1, Sesija: p.sesija, Znanje: w.Znanje()}) // kad ne prođe, A ne dobije pozdrav i zatvori vezu, a s njom i ovu sesiju
	}
	druga, _ := adresaPosluzitelja(v.veza.RemoteAddr()) // QUIC veza uvijek ima UDP adresu
	s := &SesijaWeb{Sesija: p.sesija, Druga: p.druga, Put: putQUIC(druga, w.ts(), w.izlaz), RTT: v.rtt, Pokusaj: p.p.Pokusaj,
		Znanje: v.znanje, veza: v.veza, kontrola: v.kontrola, vjDruge: p.vjDruge, v: w.vrata}
	s.postavi()
	w.zapamtiSesiju(s)
	s.pokreni(w.zaboraviSesiju)
	w.NaSesiju(s)
	return s
}

// zapamtiSesiju upiše sesiju za računalo; starija sesija s njim se zatvara
func (w *WebSync) zapamtiSesiju(s *SesijaWeb) {
	k := PublicKeyString(s.Druga)
	w.mu.Lock()
	stara := w.sesije[k]
	w.sesije[k] = s
	w.mu.Unlock()
	if stara != nil {
		stara.gorutine.Go(func() { stara.Zatvori(0) })
	}
}

// zaboraviSesiju makne zatvorenu sesiju, kad je još upisana
func (w *WebSync) zaboraviSesiju(s *SesijaWeb) {
	k := PublicKeyString(s.Druga)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sesije[k] == s {
		delete(w.sesije, k)
	}
}

// vezaSesije je QUIC veza s kontrolnim tokom prije nego što postane sesija
type vezaSesije struct {
	veza     *quic.Conn
	kontrola *quic.Stream
	znanje   *Znanje
	rtt      time.Duration
}

// pozdravi: A otvori kontrolni tok, pošalje pozdrav sa sesijom i čeka B-ov
// pozdrav s istom sesijom, dok ctx traje. Kad ne uspije, veza se zatvara.
func (v *vezaSesije) pozdravi(ctx context.Context, sesija string, z *Znanje) error {
	stani := context.AfterFunc(ctx, v.odbaci)
	err := v.razmijeniPozdrav(ctx, sesija, z)
	if !stani() || err != nil {
		v.odbaci()
		return cmp.Or(err, ctx.Err())
	}
	return nil
}

func (v *vezaSesije) razmijeniPozdrav(ctx context.Context, sesija string, z *Znanje) error {
	tok, err := v.veza.OpenStreamSync(ctx)
	if err != nil {
		return err
	}
	v.kontrola = tok
	_, _ = tok.Write([]byte{kanalKontrola})                                                          // kad ne prođe, ne stigne ni odgovor
	_ = pisiKontrolu(tok, porukaKontrole{Vrsta: kontrolaPozdrav, Ver: 1, Sesija: sesija, Znanje: z}) // isto
	odg, err := procitajPozdrav(tok)
	if err != nil || odg.Sesija != sesija {
		return cmp.Or(err, errDrugaSesija)
	}
	v.znanje = odg.Znanje
	return nil
}

// odbaci zatvori vezu koja nije postala sesija
func (v *vezaSesije) odbaci() {
	if v != nil {
		_ = v.veza.CloseWithError(quic.ApplicationErrorCode(KodOdbijeno), "") // veza nikome ne treba; greške zatvaranja nema
	}
}

// priprema je probijanje jedne sesije s drugim računalom, u jednoj ulozi:
// stroj (Probijanje) vozi jedna gorutina (vozi), a sve što stiže izvana
// (odziv, sinkro, ishod poziva, dolazna veza) ide joj kao funkcija na
// ulaz, pa stanje stroja čita i mijenja samo ona. Veza koja stigne na ulaz
// kad je priprema gotova (ili ostane u njemu) se zatvara.
type priprema struct {
	w          *WebSync
	druga      ed25519.PublicKey
	drugaS     string
	sesija     string
	pozivatelj bool
	a          bool
	tajna      []byte
	kandidati  []KandidatPoruke
	nat        *NATPoruke
	vlastite   []netip.AddrPort
	x          atomic.Pointer[Udarci]
	ctx        context.Context
	otkazi     context.CancelCauseFunc
	ulaz       chan stavka
	kraj       chan struct{}
	vjDruge    []byte // iz provjerenog poziva (B) ili odziva (A)

	// predaja nakon kraja zatvara vezu (predaj, pocisti)
	mu      sync.Mutex
	gotova  bool
	predaje sync.WaitGroup

	// samo gorutina koja vozi stroj
	p           Probijanje
	radnja      int
	otkaziPoziv context.CancelFunc
	pozivi      sync.WaitGroup
	veza        *vezaSesije
}

// novaPriprema je priprema s računalom druga, s vlastitom tajnom i
// kandidatima
func (w *WebSync) novaPriprema(otkazi context.CancelCauseFunc, druga ed25519.PublicKey, sesija string, pozivatelj bool) *priprema {
	kandidati, nat := w.Mreza()
	tajna := make([]byte, velicinaTajne)
	_, _ = rand.Read(tajna) // crypto/rand ne vraća grešku
	return &priprema{w: w, druga: druga, drugaS: PublicKeyString(druga), sesija: sesija, pozivatelj: pozivatelj,
		a: bytes.Compare(w.moj, druga) < 0, tajna: tajna, kandidati: kandidati, nat: nat, vlastite: adreseVlastitih(kandidati),
		otkazi: otkazi, ulaz: make(chan stavka, 16), kraj: make(chan struct{}), otkaziPoziv: func() {}}
}

// adreseVlastitih su adrese vlastitih kandidata, koje se ne udaraju
func adreseVlastitih(k []KandidatPoruke) []netip.AddrPort {
	var out []netip.AddrPort
	for _, c := range k {
		if a, ok := adresaKandidata(c.Adresa); ok {
			out = append(out, a)
		}
	}
	return out
}

// potpisi potpiše poruku ove sesije za drugu stranu
func (p *priprema) potpisi(vrsta string, t TijeloPoruke) PotpisanaPoruka {
	return p.w.potpisi(vrsta, p.sesija, p.drugaS, t)
}

// pokreniUdarce izvede ključeve udaraca iz obiju tajni i pokrene udarce na
// kandidate druge strane koji ne idu kroz Tailscale
func (p *priprema) pokreniUdarce(tajnaPoziva, tajnaOdziva []byte, kandidati []KandidatPoruke) error {
	k, err := NoviKljuceviUdarca(p.w.Protokol, p.sesija, tajnaPoziva, tajnaOdziva, p.w.moj, p.druga)
	if err != nil {
		return err
	}
	ostali, _ := kandidatiDruge(kandidati, p.w.ts(), p.w.izlaz) // razloge izbacivanja pokazuje tek gumb Provjeri vezu (F3b)
	x, err := NoviUdarci(p.w.u, k, ostali, p.vlastite)
	p.x.Store(x)
	return err
}

// vozi vozi stroj do kraja: sesija kad je uspio, inače GreskaPripreme ili
// greška konteksta
func (p *priprema) vozi(ctx context.Context) (*SesijaWeb, error) {
	defer p.pocisti()
	p.ctx = ctx
	for !p.p.Zavrseno() {
		d, ima := p.cekaj(ctx)
		if ctx.Err() != nil {
			return nil, prekinuta(ctx, p.p.Pokusaj)
		}
		if ima {
			p.korak(d)
		}
	}
	if p.p.Faza != FazaUspjeh {
		return nil, GreskaPripreme{Razlog: p.p.Razlog, Pokusaj: p.p.Pokusaj}
	}
	return p.w.novaSesija(p), nil
}

// prekinuta je greška prekinute pripreme: razlog prekida, ili greška
// konteksta pozivatelja
func prekinuta(ctx context.Context, pokusaj int) error {
	var g GreskaPripreme
	if errors.As(context.Cause(ctx), &g) {
		g.Pokusaj = pokusaj
		return g
	}
	return ctx.Err()
}

// cekaj čeka idući događaj: rok stroja, nešto s ulaza ili valjan paket
func (p *priprema) cekaj(ctx context.Context) (Dogadjaj, bool) {
	t := time.NewTimer(time.Until(p.p.Budjenje()))
	defer t.Stop()
	select {
	case <-ctx.Done():
		return Dogadjaj{}, false
	case <-t.C:
		return Dogadjaj{Vrsta: DogadjajSat, Sad: time.Now()}, true
	case s := <-p.ulaz:
		return s.f()
	case v := <-p.valjani():
		return v.Dogadjaj(), true
	}
}

// valjani su valjani paketi druge strane; bez udaraca nema ih
func (p *priprema) valjani() <-chan Valjan {
	if x := p.x.Load(); x != nil {
		return x.Valjani()
	}
	return nil
}

// korak preda događaj stroju i izvrši radnje; veza koja nije donijela
// uspjeh se zatvara
func (p *priprema) korak(d Dogadjaj) {
	var radnje []Radnja
	p.p, radnje = p.p.Prijelaz(d)
	for _, r := range radnje {
		radnjePripreme()[r.Vrsta](p, r)
	}
	if p.p.Faza != FazaUspjeh {
		p.veza.odbaci()
		p.veza = nil
	}
}

// radnjePripreme izvršavaju radnje stroja. Funkcija, a ne varijabla
// paketa: varijabla bi u program povukla sav kod WEB synca i dok ga program
// ne zove (oko 1 MB).
func radnjePripreme() map[VrstaRadnje]func(*priprema, Radnja) {
	return map[VrstaRadnje]func(*priprema, Radnja){
		RadnjaSinkro:      (*priprema).posaljiSinkro,
		RadnjaUdaraj:      (*priprema).udaraj,
		RadnjaStani:       (*priprema).stani,
		RadnjaQUIC:        (*priprema).quic,
		RadnjaPrekiniQUIC: (*priprema).prekiniQUIC,
		RadnjaKraj:        func(*priprema, Radnja) {},
	}
}

func (p *priprema) posaljiSinkro(r Radnja) {
	_ = p.w.Posalji(p.potpisi(VrstaSinkro, TijeloPoruke{Pokusaj: r.Pokusaj, RTTMs: r.RTTMs})) // bez sinkra pozvani ne krene, a pokušaj istekne
}

func (p *priprema) udaraj(r Radnja) { p.x.Load().Pokusaj(r.Od, r.Do) }

// stani: udarci staju; priprema koja nije došla do udaraca (odbijen poziv)
// nema što zaustaviti
func (p *priprema) stani(Radnja) {
	if x := p.x.Load(); x != nil {
		x.Stani()
	}
}

// quic: A nazove adresu iz radnje do kraja pokušaja, a ishod veže uz broj
// radnje; B čeka dolaznu vezu
func (p *priprema) quic(r Radnja) {
	p.radnja++
	if !p.a {
		return
	}
	p.otkaziPoziv()
	ctx, otkazi := context.WithDeadline(p.ctx, p.p.Budjenje())
	p.otkaziPoziv = otkazi
	n := p.radnja
	p.pozivi.Go(func() {
		v, err := p.w.zovi(ctx, p, r.Adresa)
		p.predaj(v, func() (Dogadjaj, bool) { return p.naIshod(n, v, err) })
	})
}

func (p *priprema) prekiniQUIC(Radnja) { p.otkaziPoziv() }

// stavka je ono što ide stroju: funkcija i veza koju nosi (uz poruke nil),
// da je pocisti zatvori kad je stroj nije uzeo
type stavka struct {
	f func() (Dogadjaj, bool)
	v *vezaSesije
}

// predaj preda funkciju stroju; kad je priprema već gotova, ili postane
// gotova dok predaja čeka, veza se zatvara
func (p *priprema) predaj(v *vezaSesije, f func() (Dogadjaj, bool)) {
	if !p.pocniPredaju() {
		v.odbaci()
		return
	}
	defer p.predaje.Done()
	select {
	case p.ulaz <- stavka{f: f, v: v}:
	case <-p.kraj:
		v.odbaci()
	}
}

// pocniPredaju upiše predaju koja počinje; false kad je priprema gotova.
// Upis i oznaka kraja su pod istom bravom, pa pocisti čeka svaku predaju
// koja je počela, a nova nakon kraja ne počne.
func (p *priprema) pocniPredaju() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gotova {
		return false
	}
	p.predaje.Add(1)
	return true
}

// ponudi preda funkciju stroju kad ima mjesta; inače je odbaci, jer poruka
// sastajališta ne smije čekati
func (p *priprema) ponudi(f func() (Dogadjaj, bool)) {
	select {
	case p.ulaz <- stavka{f: f}:
	default:
	}
}

// isprazni zatvori veze koje su ostale na ulazu gotove pripreme
func (p *priprema) isprazni() {
	for {
		select {
		case s := <-p.ulaz:
			s.v.odbaci()
		default:
			return
		}
	}
}

// naIshod: ishod poziva vrijedi samo za radnju QUIC koja je u tijeku
func (p *priprema) naIshod(n int, v *vezaSesije, err error) (Dogadjaj, bool) {
	if n != p.radnja || p.p.Faza != FazaQUIC {
		v.odbaci()
		return Dogadjaj{}, false
	}
	p.veza = v
	return Dogadjaj{Vrsta: DogadjajQUIC, Sad: time.Now(), Uspjeh: err == nil}, true
}

// naDolaznu: B je primio vezu s pozdravom ove sesije
func (p *priprema) naDolaznu(v *vezaSesije) (Dogadjaj, bool) {
	p.veza = v
	return Dogadjaj{Vrsta: DogadjajPrihvacen, Sad: time.Now()}, true
}

// naPoruku: odziv i sinkro idu na provjeru potpisa i pričuve tek kad ih
// stroj prima; odziv „probaj” pokreće udarce
func (p *priprema) naPoruku(pp PotpisanaPoruka, t TijeloPoruke) (Dogadjaj, bool) {
	vrsta, prima := p.prima(pp.Vrsta, t)
	if !prima {
		return Dogadjaj{}, false
	}
	t, err := p.provjera().Provjeri(pp)
	if err != nil {
		return Dogadjaj{}, false
	}
	if vrsta == DogadjajOdziv {
		t.Odluka = p.naOdziv(t)
	}
	return Dogadjaj{Vrsta: vrsta, Sad: time.Now(), Poruka: t}, true
}

// provjera je provjera poruka ove pripreme. Sinkro ne nosi vjerodajnice,
// pa se pošiljatelj bez njih provjerava s onima iz njegovog poziva: tek
// upareni pozivatelj inače ne bi prošao do udaraca.
func (p *priprema) provjera() ProvjeraPoruka {
	pp := p.w.provjera()
	pp.Vjeruje = func(k ed25519.PublicKey, vj []byte) bool {
		if len(vj) == 0 {
			vj = p.vjDruge
		}
		return p.w.Trusted(k, vj)
	}
	return pp
}

// naOdziv: A zapamti vjerodajnice druge strane iz provjerenog odziva, a
// odziv „probaj” pokreće udarce; vraća odluku za stroj
func (p *priprema) naOdziv(t TijeloPoruke) string {
	p.vjDruge = t.Vj
	if t.Odluka == OdlukaProbaj && p.pokreniUdarce(p.tajna, t.Tajna, t.Kandidati) != nil {
		return OdlukaOdbij // bez ključeva udaraca nema probijanja
	}
	return t.Odluka
}

// prima javlja prima li stroj poruku te vrste sada, po zaglavlju
func (p *priprema) prima(vrsta string, t TijeloPoruke) (VrstaDogadjaja, bool) {
	switch vrsta {
	case VrstaOdziv:
		return DogadjajOdziv, p.p.PrimaOdziv(t)
	case VrstaSinkro:
		return DogadjajSinkro, p.p.PrimaSinkro(t)
	}
	return 0, false
}

// pocisti: priprema je gotova. Pozivi koji još traju se prekidaju, veze
// koje stroj nije uzeo se zatvaraju, udarci staju, a priprema se odjavi.
func (p *priprema) pocisti() {
	p.mu.Lock()
	p.gotova = true
	p.mu.Unlock()
	close(p.kraj)
	p.otkaziPoziv()
	p.pozivi.Wait()
	p.predaje.Wait()
	p.isprazni()
	if x := p.x.Load(); x != nil {
		x.Close()
	}
	p.w.otpusti(p)
	p.otkazi(nil)
}
