package main

// WEB sync u pokretanju (plan WEB synca, F1a): uz [povezivost]
// web_sync_prijava = true računalo otvori UDP utičnicu (zadano 4713) s
// quic.Transportom i provjeri mrežu preko STUN poslužitelja. Sve ide u
// pozadini i tek kad web već sluša, jer Postava vraća staro izdanje kad
// novo ne odgovori u 90 s. Prije otvaranja uz bazu se upiše oznaka padova
// (internal/peers/pad_povezivosti.go), a uredno gašenje je obriše. Uz
// web_sync_prijava = false nema ni utičnice ni oznake.
//
// U dnevnik idu redci „povezivost:”, bez tuđih javnih adresa i bez
// vlastite.

import (
	"context"
	"fmt"
	"net"
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
// stanje WEB synca (web.Server)
type posluziteljPovezivosti interface {
	Slusa() <-chan struct{}
	SetPovezivost(stanje func() web.StanjePovezivosti, ukljuci func() error)
}

// rokRazrjesenjaSTUN je najdulje čekanje na adresu jednog STUN poslužitelja
const rokRazrjesenjaSTUN = 3 * time.Second

// stabilanRadPovezivosti: nakon toliko rada bez pada brojač padova ide na
// nulu; var, da ga test skrati
var stabilanRadPovezivosti = peers.StabilanRadPovezivosti

// otvoriUticnicu otvara UDP utičnicu WEB synca; var, da je test podmetne
var otvoriUticnicu = razmjena.OtvoriUDP

// povezivost je WEB sync u ovom pokretanju. upisano i udp dira samo
// gorutina koja radi, a zaustavljanje tek kad ona završi.
type povezivost struct {
	cfg         config.Povezivost
	pad         peers.PadPovezivosti
	javi        func(format string, args ...any)
	stabilanRad time.Duration

	mu                       sync.Mutex
	nakonPadova, ukljucujeSe bool

	upisano bool // oznaka je upisana, pa je uredno gašenje briše
	udp     *razmjena.UDP
}

// pokreniPovezivost pokrene WEB sync u pozadini i vrati zaustavljanje.
// Zove se prije nego web počne slušati, jer poslužitelju predaje stanje.
func pokreniPovezivost(ctx context.Context, cfg config.Povezivost, dir string, w posluziteljPovezivosti, javi func(string, ...any)) func() {
	if !cfg.WebSyncPrijava {
		return func() {}
	}
	p := &povezivost{cfg: cfg, pad: peers.ProcitajPadPovezivosti(dir), javi: javi, stabilanRad: stabilanRadPovezivosti}
	w.SetPovezivost(p.stanje, p.ukljuci)
	if p.pad.Iskljucen() {
		p.nakonPadova = true
		javi("povezivost: WEB sync je isključen, jer se program dvaput zaredom prekinuo dok je WEB sync radio; " +
			"ponovno se uključuje u Administraciji, pod Čvor i mreža")
		return func() {}
	}
	ctx, otkazi := context.WithCancel(ctx)
	var teku sync.WaitGroup
	teku.Go(func() { p.radi(ctx, w.Slusa()) })
	return func() {
		otkazi()
		teku.Wait()
		p.zatvori()
	}
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
	if p.udp = p.otvori(); p.udp == nil {
		p.zatvori() // bez utičnice nema ni quic-go-a: prekid ovog pokretanja nije pad WEB synca
		return
	}
	p.provjeri(ctx)
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
	u := razmjena.NoviUDP(conn)
	p.javi("povezivost: UDP radi na :%d", u.Port())
	return u
}

// provjeri pita STUN poslužitelje i zapiše ishod u dnevnik
func (p *povezivost) provjeri(ctx context.Context) {
	if len(p.cfg.Stun) == 0 {
		p.javi("povezivost: provjera mreže je isključena (stun = [])")
		return
	}
	m := p.udp.ProvjeriMrezu(ctx, posluziteljiSTUN(ctx, p.cfg.Stun))
	if ctx.Err() == nil {
		p.javi("povezivost: %s", m.Opis())
	}
}

// zatvori zatvori utičnicu i obriše oznaku koju je ovo pokretanje upisalo
func (p *povezivost) zatvori() {
	if p.udp != nil {
		_ = p.udp.Close() // gašenje: utičnica se zatvara bez obzira na grešku
	}
	if !p.upisano {
		return
	}
	p.upisano = false
	if err := p.pad.Obrisi(); err != nil {
		p.javi("povezivost: oznaka padova uz bazu ne da se obrisati, pa iduće pokretanje broji pad: %v", err)
	}
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
