package razmjena

// Vrata prije dokaza ključa. Dok TLS ne dokaže ključ, o drugoj strani se zna
// samo adresa s koje zove, pa se rukovanja ograđuju po njoj: najviše četiri
// odjednom s jedne adrese (javna IPv6 po mreži /64) i osam s jedne javne
// mreže (IPv4 /24, IPv6 /48). Adresa s koje je član nedavno dokazao ključ
// ima osam rukovanja, bez granice mreže, a kad su sva mjesta zauzeta, još
// osam svojih. Veza koja ne dobije mjesto odmah se zatvara, umjesto da čeka
// u redu iza veza koje šute; druga strana pokuša opet sljedećim krugom. Veza
// koja u dvije sekunde (kroz tunel tri) ne pošalje cijelu prvu poruku
// rukovanja gubi mjesto, jer čvor svoju pošalje odmah.

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"net"
	"net/netip"
	"sync"
	"time"

	"gocop/internal/tailscale"
)

const (
	najviseRukovanjaPoAdresi   = 4
	najviseRukovanjaPoznate    = 8 // poznata adresa, npr. ured iza jedne adrese
	najviseRukovanjaPoMrezi    = 8
	pricuvaPoznatih            = 8
	rokClientHello             = 2 * time.Second
	rokClientHelloTunela       = 3 * time.Second // uključuje obilazak kroz Cloudflare nakon nadogradnje
	najvisePoznatih            = 256
	najviseAdresaPoKljucu      = 4
	pamtiPoznatu               = 24 * time.Hour
	bitovaMreze4, bitovaMreze6 = 24, 48
	bitovaKlijenta6            = 64
)

// javna je adresa s interneta; privatne (i IPv6 fc00::/7), lokalne i
// Tailscaleove adrese nisu
func javna(a netip.Addr) bool {
	return a.IsGlobalUnicast() && !a.IsPrivate() && !tailscale.JeTailscale(a)
}

// KljucAdrese je adresa po kojoj se ograđuju veze prije dokaza ključa: IPv4
// adresa, a za javnu IPv6 njezina mreža /64, jer jedno računalo u njoj ima
// mnogo adresa. Privatna i lokalna IPv6 adresa ostaje cijela, jer u lokalnoj
// mreži i u Tailscaleu sva računala dijele istu mrežu /64. Neispravna adresa
// ostaje neispravna.
func KljucAdrese(a netip.Addr) netip.Addr {
	a = a.Unmap().WithZone("")
	if !a.Is6() || !javna(a) {
		return a
	}
	p, _ := a.Prefix(bitovaKlijenta6) // IPv6 bez zone uvijek ima mrežu /64
	return p.Addr()
}

// MrezaAdrese je mreža adrese: IPv4 /24, IPv6 /48 (jedan pretplatnik dobiva
// čitavu takvu mrežu). Neispravna adresa daje neispravnu mrežu.
func MrezaAdrese(a netip.Addr) netip.Addr {
	a = a.Unmap().WithZone("")
	bitova := bitovaMreze4
	if a.Is6() {
		bitova = bitovaMreze6
	}
	p, _ := a.Prefix(bitova) // neispravna adresa daje praznu mrežu
	return p.Addr()
}

// izvor je odakle veza dolazi prije dokaza ključa: adresa (KljucAdrese) i,
// za javnu adresu izvan lokalne mreže, mreža (MrezaAdrese). Neispravno se ne
// ograđuje.
type izvor struct{ adresa, mreza netip.Addr }

// ipAdresa je IP adresa veze, bez zone; neispravna za vezu bez IP adrese
// (tunel, veza u memoriji)
func ipAdresa(a net.Addr) netip.Addr {
	if a == nil {
		return netip.Addr{}
	}
	ap, err := netip.ParseAddrPort(a.String())
	if err != nil {
		return netip.Addr{}
	}
	return ap.Addr().Unmap().WithZone("")
}

// izvorVeze je izvor veze s udaljene adrese na lokalnu. Javna IPv6 adresa
// iz iste mreže /64 kao lokalna je u istoj lokalnoj mreži, pa se broji sama,
// bez mreže, kao i privatna.
func izvorVeze(udaljena, lokalna net.Addr) izvor {
	a := ipAdresa(udaljena)
	if !javna(a) || istaMreza64(a, ipAdresa(lokalna)) {
		return izvor{adresa: a}
	}
	return izvor{adresa: KljucAdrese(a), mreza: MrezaAdrese(a)}
}

func istaMreza64(a, b netip.Addr) bool {
	if !a.Is6() || !b.Is6() {
		return false
	}
	pa, _ := a.Prefix(bitovaKlijenta6) // obje su IPv6 bez zone
	pb, _ := b.Prefix(bitovaKlijenta6) // probni podaci su ispravni; kvar bi pokazala provjera niže
	return pa == pb
}

// poznata je adresa s koje je član dokazao ključ: kad i koji ključ
type poznata struct {
	kad   time.Time
	kljuc string
}

// PoznateAdrese pamte adrese s kojih je član dokazao ključ, najviše 256 i
// najviše 4 po ključu, svaku 24 sata od zadnjeg dokaza. Samo u memoriji.
type PoznateAdrese struct {
	mu  sync.Mutex
	kad map[netip.Addr]poznata
	sat func() time.Time
}

// NovePoznateAdrese daje prazno pamćenje
func NovePoznateAdrese() *PoznateAdrese {
	return &PoznateAdrese{kad: map[netip.Addr]poznata{}, sat: time.Now}
}

// Zapamti bilježi adresu s koje je član upravo dokazao ključ. Ključ koji već
// ima četiri adrese zaboravi svoju najstariju, a kad je pamćenje puno,
// zaboravi se najstarija.
func (p *PoznateAdrese) Zapamti(a netip.Addr, kljuc ed25519.PublicKey) {
	if !a.IsValid() {
		return
	}
	k := string(kljuc)
	p.mu.Lock()
	defer p.mu.Unlock()
	if z, ima := p.kad[a]; !ima || z.kljuc != k {
		p.napraviMjesto(k, !ima)
	}
	p.kad[a] = poznata{kad: p.sat(), kljuc: k}
}

// napraviMjesto oslobodi mjesto za novu adresu ključa k; nova je adresa
// koje u pamćenju još nema
func (p *PoznateAdrese) napraviMjesto(k string, nova bool) {
	broj := 0
	for _, z := range p.kad {
		if z.kljuc == k {
			broj++
		}
	}
	switch {
	case broj >= najviseAdresaPoKljucu:
		p.zaboraviNajstariju(func(z poznata) bool { return z.kljuc == k })
	case nova && len(p.kad) >= najvisePoznatih:
		p.zaboraviNajstariju(func(poznata) bool { return true })
	}
}

// Zna javlja je li član s te adrese dokazao ključ u zadnja 24 sata
func (p *PoznateAdrese) Zna(a netip.Addr) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	z, ima := p.kad[a]
	if ima && p.sat().Sub(z.kad) > pamtiPoznatu {
		delete(p.kad, a)
		return false
	}
	return ima
}

// zaboraviNajstariju zaboravi najstariju adresu među onima koje uzima
func (p *PoznateAdrese) zaboraviNajstariju(uzmi func(poznata) bool) {
	var naj netip.Addr
	var najKad time.Time
	for a, z := range p.kad {
		if uzmi(z) && (!naj.IsValid() || z.kad.Before(najKad)) {
			naj, najKad = a, z.kad
		}
	}
	delete(p.kad, naj)
}

// mjestoRukovanja daje vezi iz izvora iz mjesto za rukovanje i funkciju koja
// ga pušta (jednom); false kad mjesta za nju nema
func (o *Ograda) mjestoRukovanja(iz izvor) (func(), bool) {
	poznata := o.poznate.Zna(iz.adresa)
	granica := najviseRukovanjaPoAdresi
	if poznata {
		granica, iz.mreza = najviseRukovanjaPoznate, netip.Addr{}
	}
	if !o.uzmiIzvor(iz, granica) {
		return nil, false
	}
	mjesta, ok := o.slobodnaMjesta(poznata)
	if !ok {
		o.pustiIzvor(iz)
		return nil, false
	}
	return sync.OnceFunc(func() {
		<-mjesta
		o.pustiIzvor(iz)
	}), true
}

// slobodnaMjesta zauzme mjesto među rukovanjima, a kad su zauzeta, poznatoj
// adresi u pričuvi; vraća skup iz kojeg je mjesto uzeto
func (o *Ograda) slobodnaMjesta(poznata bool) (chan struct{}, bool) {
	select {
	case o.rukovanja <- struct{}{}:
		return o.rukovanja, true
	default:
	}
	if !poznata {
		return nil, false
	}
	select {
	case o.pricuva <- struct{}{}:
		return o.pricuva, true
	default:
		return nil, false
	}
}

// uzmiIzvor broji rukovanje izvora, najviše poAdresi s adrese i
// najviseRukovanjaPoMrezi s mreže
func (o *Ograda) uzmiIzvor(iz izvor, poAdresi int) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if broj(o.poAdresi, iz.adresa) >= poAdresi || broj(o.poMrezi, iz.mreza) >= najviseRukovanjaPoMrezi {
		return false
	}
	promijeni(o.poAdresi, iz.adresa, 1)
	promijeni(o.poMrezi, iz.mreza, 1)
	return true
}

func (o *Ograda) pustiIzvor(iz izvor) {
	o.mu.Lock()
	defer o.mu.Unlock()
	promijeni(o.poAdresi, iz.adresa, -1)
	promijeni(o.poMrezi, iz.mreza, -1)
}

// broj je koliko rukovanja teče s a; neispravna se ne broji
func broj(brojac map[netip.Addr]int, a netip.Addr) int {
	if !a.IsValid() {
		return 0
	}
	return brojac[a]
}

func promijeni(brojac map[netip.Addr]int, a netip.Addr, za int) {
	if !a.IsValid() {
		return
	}
	if brojac[a] += za; brojac[a] <= 0 {
		delete(brojac, a)
	}
}

// odbijNaUlazu javlja vezu koja nije dobila mjesto za rukovanje. Kad ih
// stigne mnogo odjednom, javlja se jedna po jedna, a ostale se za to vrijeme
// samo zatvore, da zapis ne usporava vrata.
func (o *Ograda) odbijNaUlazu() {
	if o.odbijeno == nil || !o.javlja.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer o.javlja.Store(false)
		o.odbij(VrataRukovanje, nil)
	}()
}

// doPozdrava je veza prije rukovanja: dok ne stigne cijela prva poruka
// rukovanja (ClientHello), vrijedi kratki rok (Ograda.prvi), a zatim rok
// cijelog rukovanja
type doPozdrava struct {
	net.Conn
	rok time.Time
}

// produzi postavi rok cijelog rukovanja
func (c *doPozdrava) produzi() {
	_ = c.Conn.SetDeadline(c.rok) // na zatvorenoj vezi rukovanje ionako padne
}

// NetConn je veza ispod, kao kod tls.Conn
func (c *doPozdrava) NetConn() net.Conn { return c.Conn }

// sRokomNakonPozdrava je TLS poslužitelja koji vezi postavi rok cijelog
// rukovanja tek kad pročita cijeli ClientHello; dotad vrijedi kratki rok
func sRokomNakonPozdrava(cfg *tls.Config) *tls.Config {
	c := cfg.Clone()
	prije := cfg.GetConfigForClient
	c.GetConfigForClient = func(chi *tls.ClientHelloInfo) (*tls.Config, error) {
		if v, ok := chi.Conn.(*doPozdrava); ok {
			v.produzi()
		}
		if prije == nil {
			return nil, nil
		}
		return prije(chi)
	}
	return c
}

// MjestoVeze je mjesto koje je web ograda dala vezi prije nego što se zna tko
// je s druge strane. Clan javlja da je druga strana dokazala ključ člana:
// ograda tada pusti mjesto stranca i da mjesto člana, a false vrati kad za
// taj ključ mjesta više nema.
type MjestoVeze interface {
	Clan(kljuc ed25519.PublicKey) bool
}

type mjestoKljuc struct{}

// SMjestom stavlja mjesto web ograde u kontekst zahtjeva; tunel i
// sastajalište ga uzmu iz WebSocketa
func SMjestom(ctx context.Context, m MjestoVeze) context.Context {
	return context.WithValue(ctx, mjestoKljuc{}, m)
}

func mjestoIz(ctx context.Context) MjestoVeze {
	m, _ := ctx.Value(mjestoKljuc{}).(MjestoVeze)
	return m
}

// naMjestu je veza koja zna svoje mjesto u web ogradi (wsVeza)
type naMjestu interface{ clan(ed25519.PublicKey) bool }

// clanNaMjestu javlja web ogradi ispod veze da je ključ dokazan; true kad
// ograde nema ili je mjesto člana dala
func clanNaMjestu(c any, kljuc ed25519.PublicKey) bool {
	for {
		switch x := c.(type) {
		case naMjestu:
			return x.clan(kljuc)
		case interface{ NetConn() net.Conn }:
			c = x.NetConn()
		default:
			return true
		}
	}
}
