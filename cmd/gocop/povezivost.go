package main

// WEB sync u pokretanju (plan WEB synca, F1a): uz [povezivost]
// web_sync_prijava = true računalo otvori UDP utičnicu (zadano 4713) s
// quic.Transportom i provjeri mrežu preko STUN poslužitelja. Sve ide u
// pozadini i tek kad web već sluša, jer Postava vraća staro izdanje kad
// novo ne odgovori u 90 s. Prije otvaranja uz bazu se upiše oznaka padova
// (internal/peers/pad_povezivosti.go), a uredno gašenje je obriše. Uz
// web_sync_prijava = false nema stalne utičnice ni oznake: gumb Provjeri
// vezu tada otvori privremenu utičnicu (udp_port, a kad je zauzet,
// nasumičan), bez oznake padova, i zatvori je nakon mjerenja.
//
// U dnevnik idu redci „povezivost:”, bez tuđih javnih adresa i bez
// vlastite.

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocop/internal/config"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
	"gocop/internal/web"
)

// posluziteljPovezivosti je web poslužitelj: kad sluša i komu javiti
// stanje WEB synca, provjeru veze i sastajalište (web.Server)
type posluziteljPovezivosti interface {
	Slusa() <-chan struct{}
	SetPovezivost(stanje func() web.StanjePovezivosti, ukljuci func() error)
	SetProvjeraVeze(provjeri func(context.Context) web.ProvjeraVeze, zadnja func() *web.ProvjeraVeze)
	SetSignal(h http.Handler)
	SetSignalStanje(stanje func() web.SignalStanje)
}

// rokRazrjesenjaSTUN je najdulje čekanje na adresu jednog STUN poslužitelja
const rokRazrjesenjaSTUN = 3 * time.Second

// stabilanRadPovezivosti: nakon toliko rada bez pada brojač padova ide na
// nulu; var, da ga test skrati
var stabilanRadPovezivosti = peers.StabilanRadPovezivosti

// otvoriUticnicu otvara UDP utičnicu WEB synca; var, da je test podmetne
var otvoriUticnicu = razmjena.OtvoriUDP

// povezivost je WEB sync u ovom pokretanju i provjera veze. Mjerenja idu
// pod kontekstom programa, jedno po jedno; gorutine (radi i mjerenja) prate
// se u teku, pa zaustavljanje pričeka i mjerenje pokrenuto gumbom.
type povezivost struct {
	cfg         config.Povezivost
	pad         peers.PadPovezivosti
	javi        func(format string, args ...any)
	stabilanRad time.Duration
	ctx         context.Context
	teku        sync.WaitGroup

	mu                       sync.Mutex
	nakonPadova, ukljucujeSe bool
	gasi                     bool                 // zaustavljanje je počelo: novih mjerenja nema
	udp                      *razmjena.UDP        // stalna utičnica WEB synca; gumb je čita
	mreza                    razmjena.StanjeMreze // vrsta mreže s histerezom, samo sa stalne utičnice
	zadnja                   *web.ProvjeraVeze
	uTijeku                  chan struct{} // zatvori se kad mjerenje u tijeku završi

	// prijava kod sastajališta ili samo sastajalište (signal.go); postave se
	// prije nego web sluša, a poslije se samo čitaju. signal je ista prijava
	// kao klijent, za WEB sync (ključ sastajališta i slanje).
	klijent      prijavaSignala
	signal       *razmjena.KlijentSignala
	sastajaliste *razmjena.Sastajaliste

	// WEB sync (web_sync.go): čvor koji ga drži (nil: nema ga) i WEB sync
	// koji upravo radi, pod mu; pokretanje i zaustavljanje idu jedno po
	// jedno (webMu)
	webCvor cvorWeba
	webMu   sync.Mutex
	web     *webPovezivosti

	upisano bool // oznaka je upisana, pa je uredno gašenje briše; samo radi i zatvori
}

// pokreniPovezivost pokrene WEB sync u pozadini i vrati zaustavljanje.
// Zove se prije nego web počne slušati, jer poslužitelju predaje stanje.
// Provjera veze radi i uz isključenu prijavu za WEB sync: tada mjeri na
// privremenoj utičnici.
func pokreniPovezivost(ctx context.Context, cfg config.Povezivost, dir string, cvor cvorSignala, w posluziteljPovezivosti, javi func(string, ...any)) func() {
	ctx, otkazi := context.WithCancel(ctx)
	p := &povezivost{cfg: cfg, javi: javi, stabilanRad: stabilanRadPovezivosti, ctx: ctx}
	p.webCvor, _ = cvor.(cvorWeba) // bez njega (test) nema WEB synca
	w.SetProvjeraVeze(p.provjeriVezu, p.zadnjaProvjera)
	stani := func() {
		p.mu.Lock()
		p.gasi = true
		p.mu.Unlock()
		otkazi()
		p.teku.Wait()
		p.zaustaviWeb()
		p.zatvoriSastajaliste()
		p.zatvori()
	}
	if !cfg.WebSyncPrijava {
		return stani
	}
	p.pad = peers.ProcitajPadPovezivosti(dir)
	w.SetPovezivost(p.stanje, p.ukljuci)
	if p.pad.Iskljucen() {
		p.nakonPadova = true
		javi("povezivost: WEB sync je isključen, jer se program dvaput zaredom prekinuo dok je WEB sync radio; " +
			"ponovno se uključuje u Administraciji, na stranici Ovo računalo i mreža")
		return stani
	}
	p.pokreniSignal(cvor, w)
	p.teku.Go(func() { p.radi(ctx, w.Slusa()) })
	return stani
}

func (p *povezivost) stanje() web.StanjePovezivosti {
	p.mu.Lock()
	defer p.mu.Unlock()
	return web.StanjePovezivosti{NakonPadova: p.nakonPadova, UkljucujeSe: p.ukljucujeSe}
}

// ukljuci obriše oznaku padova; WEB sync kreće idućim pokretanjem
func (p *povezivost) ukljuci() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.pad.Obrisi(); err != nil {
		return err
	}
	p.ukljucujeSe = true
	return nil
}

// radi čeka da web sluša, upiše oznaku, otvori utičnicu i provjeri mrežu;
// nakon stabilnog rada vrati brojač padova na nulu
func (p *povezivost) radi(ctx context.Context, slusa <-chan struct{}) {
	select {
	case <-ctx.Done():
		return
	case <-slusa:
	}
	if err := p.pad.Upisi(); err != nil {
		p.javi("povezivost: oznaka padova uz bazu ne da se upisati (%v), pa WEB sync ne kreće", err)
		return
	}
	p.upisano = true
	u := p.otvori()
	if u == nil {
		p.zatvori() // bez utičnice nema ni quic-go-a: prekid ovog pokretanja nije pad WEB synca
		return
	}
	p.mu.Lock()
	p.udp = u
	p.mu.Unlock()
	if len(p.cfg.Stun) == 0 {
		p.javi("povezivost: provjera mreže je isključena (stun = [])")
	} else {
		p.provjeriVezu(ctx)
	}
	p.pokreniWeb()
	p.prijaviSe(ctx)
	p.cuvajMrezu(ctx)
	t := time.NewTimer(p.stabilanRad)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
		if err := p.pad.Stabilno(); err != nil {
			p.javi("povezivost: oznaka padova uz bazu ne da se upisati: %v", err)
		}
	}
}

// otvori otvara UDP utičnicu; nil kad se nikako ne da otvoriti
func (p *povezivost) otvori() *razmjena.UDP {
	utisajQuic()
	conn, upozorenje, err := otvoriUticnicu(p.cfg.UDPPort)
	if upozorenje != nil {
		p.javi("povezivost: %v", upozorenje)
	}
	if err != nil {
		p.javi("povezivost: UDP utičnica se ne da otvoriti, pa WEB sync ne radi: %v", err)
		return nil
	}
	u := razmjena.NoviUDPCvora(conn, p.kljucCvora())
	p.javi("povezivost: UDP radi na :%d", u.Port())
	return u
}

// zatvori zatvori utičnicu i obriše oznaku koju je ovo pokretanje upisalo
func (p *povezivost) zatvori() {
	p.mu.Lock()
	u := p.udp
	p.udp = nil
	p.mu.Unlock()
	if u != nil {
		_ = u.Close() // gašenje: utičnica se zatvara bez obzira na grešku
	}
	if !p.upisano {
		return
	}
	p.upisano = false
	if err := p.pad.Obrisi(); err != nil {
		p.javi("povezivost: oznaka padova uz bazu ne da se obrisati, pa iduće pokretanje broji pad: %v", err)
	}
}

// provjeriVezu izmjeri mrežu, na gumb i pri pokretanju. Drugi poziv dok
// mjerenje traje pričeka to isto mjerenje, a zahtjev koji ode ne prekida
// mjerenje: ono ide pod kontekstom programa.
func (p *povezivost) provjeriVezu(ctx context.Context) web.ProvjeraVeze {
	gotovo, ok := p.zapocniMjerenje()
	if !ok {
		return web.ProvjeraVeze{Kad: time.Now(), Greska: "Program se gasi, pa provjere nema."}
	}
	select {
	case <-gotovo:
		return *p.zadnjaProvjera()
	case <-ctx.Done():
		return web.ProvjeraVeze{Kad: time.Now(), Greska: "Provjera nije dočekana, jer je zahtjev prekinut."}
	}
}

// zapocniMjerenje pokrene mjerenje ako ne traje i vrati kanal koji se
// zatvori kad završi; ne pokreće ništa kad se program gasi
func (p *povezivost) zapocniMjerenje() (<-chan struct{}, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gasi {
		return nil, false
	}
	if p.uTijeku == nil {
		gotovo := make(chan struct{})
		p.uTijeku = gotovo
		p.teku.Go(func() { p.izmjeri(gotovo) })
	}
	return p.uTijeku, true
}

// izmjeri odradi mjerenje i zapamti ishod
func (p *povezivost) izmjeri(gotovo chan struct{}) {
	r := p.mjerenje(p.ctx)
	p.mu.Lock()
	p.zadnja, p.uTijeku = &r, nil
	p.mu.Unlock()
	close(gotovo)
}

// zadnjaProvjera je kopija zadnjeg ishoda; nil dok provjere nije bilo
func (p *povezivost) zadnjaProvjera() *web.ProvjeraVeze {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.zadnja == nil {
		return nil
	}
	z := *p.zadnja
	return &z
}

// mjerenje pita STUN poslužitelje: na stalnoj utičnici WEB synca kad je
// ima, inače na privremenoj
func (p *povezivost) mjerenje(ctx context.Context) web.ProvjeraVeze {
	if len(p.cfg.Stun) == 0 {
		return web.ProvjeraVeze{Kad: time.Now(), Greska: "Provjera mreže je isključena (stun = [] u gocop.toml)."}
	}
	posl := posluziteljiSTUN(ctx, p.cfg.Stun)
	izlazi := razmjena.IzlazPremaSTUN(posl)
	p.mu.Lock()
	u := p.udp
	p.mu.Unlock()
	if u != nil {
		return p.naStalnoj(ctx, u, posl, izlazi)
	}
	return p.naPrivremenoj(ctx, posl, izlazi)
}

// naStalnoj mjeri na utičnici WEB synca. Ishod ide u histerezu i u dnevnik
// kad je prvi ili kad se vrsta mreže promijeni; prekinuto mjerenje ne ide
// nikamo, jer bi izgledalo kao blokiran UDP.
func (p *povezivost) naStalnoj(ctx context.Context, u *razmjena.UDP, posl []razmjena.PosluziteljSTUN, izlazi []razmjena.IzlazSTUN) web.ProvjeraVeze {
	r := web.ProvjeraVeze{Kad: time.Now(), Port: u.Port(), Izlazi: izlazi}
	if !u.Radi() {
		r.Greska = "UDP utičnica WEB synca više ne radi. Ponovno pokrenite čvor."
		return r
	}
	r.Mjerenje = u.ProvjeriMrezu(ctx, posl)
	if ctx.Err() != nil {
		r.Greska = "Provjera je prekinuta."
		return r
	}
	p.mu.Lock()
	if p.udp != u {
		// utičnica je zamijenjena usred mjerenja: bez odgovora bi izgledalo
		// kao blokiran UDP, pa ishod ne ide ni u histerezu ni u dnevnik
		p.mu.Unlock()
		r.Greska = greskaZamjene
		return r
	}
	prva := p.zadnja == nil
	promjena := p.mreza.Dodaj(r.Mjerenje)
	r.Vrijedi = p.mreza.Vrsta
	p.mu.Unlock()
	if prva || promjena {
		p.javi("povezivost: %s", r.Mjerenje.Opis())
	}
	r.Kandidati, r.Razlozi = kandidatiZa(r.Port, p.cfg.LanAdresa, r.Mjerenje, izlazi)
	return r
}

// naPrivremenoj otvori utičnicu na udp_port (kad je zauzet, na nasumičnom
// portu), izmjeri i zatvori je. Oznaku padova ne dira, a ishod ne ide u
// histerezu: vrijedi samo za ovu provjeru.
func (p *povezivost) naPrivremenoj(ctx context.Context, posl []razmjena.PosluziteljSTUN, izlazi []razmjena.IzlazSTUN) web.ProvjeraVeze {
	r := web.ProvjeraVeze{Kad: time.Now(), Izlazi: izlazi}
	utisajQuic()
	conn, upozorenje, err := otvoriUticnicu(p.cfg.UDPPort)
	if err != nil {
		r.Greska = "UDP utičnica se ne da otvoriti: " + err.Error()
		return r
	}
	u := razmjena.NoviUDP(conn)
	defer u.Close() //nolint:errcheck // privremena utičnica je samo za mjerenje, pa zatvaranje nema što izgubiti
	if upozorenje != nil {
		r.Upozorenje = upozorenje.Error()
	}
	r.Port, r.Privremena = u.Port(), true
	r.Mjerenje = u.ProvjeriMrezu(ctx, posl)
	if ctx.Err() != nil {
		r.Greska = "Provjera je prekinuta."
		return r
	}
	r.Kandidati, r.Razlozi = kandidatiZa(r.Port, p.cfg.LanAdresa, r.Mjerenje, izlazi)
	return r
}

// kandidatiZa su vlastiti kandidati za port, s javnim kad ga mjerenje
// dopušta
func kandidatiZa(port int, lanAdresa string, m razmjena.MjerenjeMreze, izlazi []razmjena.IzlazSTUN) ([]razmjena.Kandidat, []string) {
	k, razlozi := razmjena.VlastitiKandidati(port, lanAdresa, izlazi)
	return razmjena.SJavnim(k, m, izlazi), razlozi
}

// utisajQuic gasi upozorenje quic-go-a da ne može povećati međuspremnik
// za UDP (bez prava ga ne može ni u spremniku ni na Windowsu, a STUN-u ne
// treba). Postavlja se samo kad ga administrator nije sam postavio.
func utisajQuic() {
	const k = "QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING"
	if _, ima := os.LookupEnv(k); !ima {
		_ = os.Setenv(k, "1") // ime je ispravno, pa Setenv ne vraća grešku
	}
}

// posluziteljiSTUN su poslužitelji s popisa, s razriješenim adresama;
// poslužitelj čija se adresa ne da razriješiti ostaje bez adrese
func posluziteljiSTUN(ctx context.Context, popis []string) []razmjena.PosluziteljSTUN {
	out := make([]razmjena.PosluziteljSTUN, 0, len(popis))
	for _, a := range popis {
		p := razmjena.PosluziteljSTUN{Ime: imeSTUN(a)}
		if addr, err := razrijesiSTUN(ctx, a); err == nil {
			p.Adresa = addr
		}
		out = append(out, p)
	}
	return out
}

func razrijesiSTUN(ctx context.Context, adresa string) (net.Addr, error) {
	host, port, err := net.SplitHostPort(adresa)
	if err != nil {
		return nil, err
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return nil, err
	}
	ctx, otkazi := context.WithTimeout(ctx, rokRazrjesenjaSTUN)
	defer otkazi()
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("%s nema IPv4 adresu", host)
	}
	return net.UDPAddrFromAddrPort(netip.AddrPortFrom(ips[0].Unmap(), uint16(n))), nil
}

// imeSTUN je ime poslužitelja za dnevnik: operater za zadane, inače ime
// računala
func imeSTUN(adresa string) string {
	host, _, err := net.SplitHostPort(adresa)
	if err != nil {
		host = adresa
	}
	for dio, ime := range map[string]string{"google": "Google", "cloudflare": "Cloudflare"} {
		if strings.Contains(strings.ToLower(host), dio) {
			return ime
		}
	}
	return host
}
