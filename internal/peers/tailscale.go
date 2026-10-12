package peers

// Tailscale u razmjeni (Odluke (12. 10. 2026.)). Tailscale je osobna mreža
// jednog korisnika i imaju ga samo neka računala. Zapis čvora (peers) putuje
// knjigom svim čvorovima mreže, pa Tailscale adrese u njega ne ulaze nikad,
// ni vlastite ni tuđe. Dva čvora koja oba imaju Tailscale razmijene svoje
// adrese izravno, u razgovoru: granica kaže ima li ga čvor, a delta nosi
// adrese samo kad ga imaju oba. Svaki ih čuva samo kod sebe, u
// peer_sync.tailscale, i zove ih iza izravnih adresa, a ispred tunela.
// Čvor bez Tailscalea ne šalje ni ne prima ništa novo: njegove su poruke
// iste kao prije.

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	"gocop/internal/razmjena"
	"gocop/internal/tailscale"
)

// najviseTailscaleAdresa je koliko Tailscale adresa drugog čvora ovaj pamti
const najviseTailscaleAdresa = 8

// razlogBezTailscalea je odbijenica razmjene koja stigne na Tailscale
// adresu čvora kojem je Tailscale u razmjeni isključen; pozivatelj tada
// zaboravi njegove Tailscale adrese i zove sljedeću adresu
const razlogBezTailscalea = "na ovom računalu razmjena preko Tailscalea je isključena ([sync] tailscale = false)"

// zadaneTailscaleAdrese su Tailscale adrese ovog računala. Testovi paketa
// ih isključuju, da ishod ne ovisi o tome ima li računalo s testovima
// Tailscale; čvor u testu dobiva svoje (Service.tailscaleAdrese).
var zadaneTailscaleAdrese = tailscale.VlastiteAdrese

// vlastiteTailscale su Tailscale adrese ovog računala
func (s *Service) vlastiteTailscale() []netip.Addr {
	if s.tailscaleAdrese != nil {
		return s.tailscaleAdrese()
	}
	return zadaneTailscaleAdrese()
}

// PostaviTailscale uključuje ili isključuje Tailscale u razmjeni ([sync]
// tailscale u gocop.toml). Vrijedi odmah, pa se smije mijenjati i dok čvor
// radi. Isključen: čvor ne šalje svoje Tailscale adrese, ne prima tuđe, ne
// zove ih i odbija razmjenu koja stigne na njegovu Tailscale adresu.
func (s *Service) PostaviTailscale(ukljucen bool) { s.bezTailscalea.Store(!ukljucen) }

// TailscaleUkljucen javlja smije li razmjena koristiti Tailscale
func (s *Service) TailscaleUkljucen() bool { return !s.bezTailscalea.Load() }

// mojeTailscale su Tailscale adrese koje razmjena smije koristiti; prazno
// kad računalo nema Tailscale ili je u razmjeni isključen
func (s *Service) mojeTailscale() []netip.Addr {
	if !s.TailscaleUkljucen() {
		return nil
	}
	return s.vlastiteTailscale()
}

// tailscaleZaDrugoga su adrese ovog čvora, s portom razmjene, koje javlja
// drugome: samo kad su oba javila Tailscale i ovaj prima razmjene
func (s *Service) tailscaleZaDrugoga(moje []netip.Addr, theirs frontierMsg) []string {
	if !theirs.Tailscale || s.ports.Exchange <= 0 {
		return nil
	}
	out := make([]string, 0, len(moje))
	for _, a := range moje {
		out = append(out, netip.AddrPortFrom(a, uint16(s.ports.Exchange)).String())
	}
	return out
}

// tailscaleSPortom čita jednu primljenu adresu: Tailscale IP s portom
func tailscaleSPortom(a string) (string, bool) {
	ap, err := netip.ParseAddrPort(strings.TrimSpace(a))
	ok := err == nil && ap.Port() != 0 && tailscale.JeTailscale(ap.Addr())
	return ap.String(), ok
}

// samoTailscale ostavlja od primljenih adresa samo Tailscale adrese s
// portom, bez ponavljanja i najviše najviseTailscaleAdresa
func samoTailscale(adrese []string) []string {
	out := []string{}
	for _, a := range adrese {
		ap, ok := tailscaleSPortom(a)
		if !ok || slices.Contains(out, ap) {
			continue
		}
		if out = append(out, ap); len(out) == najviseTailscaleAdresa {
			break
		}
	}
	return out
}

// upisiTailscale pamti Tailscale adrese drugog čvora na ovom računalu;
// prazan popis ih briše
func (s *Service) upisiTailscale(ctx context.Context, nodeID string, adrese []string) {
	b, _ := json.Marshal(samoTailscale(adrese)) // popis teksta se uvijek da zapisati
	if _, err := s.db.ExecContext(ctx, `INSERT INTO peer_sync (node_id, tailscale) VALUES (?, ?)
		ON CONFLICT(node_id) DO UPDATE SET tailscale = excluded.tailscale`, nodeID, string(b)); err != nil {
		log.Printf("razmjena: Tailscale adrese čvora %s nisu spremljene: %v", nodeID, err)
	}
}

// tailscaleOd su Tailscale adrese drugog čvora koje je on javio, a ovaj ih
// spremio
func (s *Service) tailscaleOd(ctx context.Context, nodeID string) []string {
	var b string
	if err := s.db.QueryRowContext(ctx, `SELECT tailscale FROM peer_sync WHERE node_id = ?`, nodeID).Scan(&b); err != nil {
		return nil
	}
	var adrese []string
	_ = json.Unmarshal([]byte(b), &adrese) // prazno ili pokvareno: nema adresa
	return samoTailscale(adrese)
}

// zapamtiTailscale nakon uspješnog razgovora pamti što je drugi čvor javio
// o svom Tailscaleu: adrese kad ga imaju oba, inače prazno (možda ga je
// isključio). Čvor bez Tailscalea ne pamti ništa.
func (s *Service) zapamtiTailscale(ctx context.Context, nodeID string, o syncOutcome) {
	if o.err != nil || len(s.mojeTailscale()) == 0 {
		return
	}
	s.upisiTailscale(ctx, nodeID, o.tailscale)
}

// tailscaleZaSpajanje su spremljene Tailscale adrese čvora, kad ih ovaj
// smije zvati
func (s *Service) tailscaleZaSpajanje(ctx context.Context, nodeID string) []string {
	if len(s.mojeTailscale()) == 0 {
		return nil
	}
	return s.tailscaleOd(ctx, nodeID)
}

// bezTailscalea izbacuje Tailscale adrese iz adresa zapisa čvora, koji
// putuje knjigom
func bezTailscalea(adrese []string) []string {
	out := make([]string, 0, len(adrese))
	for _, a := range adrese {
		if !jeTailscaleAdresa(a) {
			out = append(out, a)
		}
	}
	return out
}

// jeTailscaleAdresa javlja vodi li adresa čvora (IP, IP s portom, adresa
// tunela) u Tailscale: IP iz Tailscaleovih raspona ili ime iz Tailscaleova
// imenika (*.ts.net), koje otkriva ime nečije osobne mreže
func jeTailscaleAdresa(a string) bool {
	host := strings.ToLower(racunaloIzAdrese(a))
	if strings.HasSuffix(host, ".ts.net") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && tailscale.JeTailscale(ip.WithZone(""))
}

// racunaloIzAdrese je računalo iz adrese čvora, bez https://, porta i zagrada
func racunaloIzAdrese(a string) string {
	a = strings.TrimSpace(a)
	if u, err := url.Parse(a); err == nil && razmjena.JeAdresaTunela(a) {
		return u.Hostname()
	}
	if h, _, err := net.SplitHostPort(a); err == nil {
		return h
	}
	return strings.Trim(a, "[]")
}

// odbijenaRazmjena je odbijenica druge strane, s njezinim razlogom
type odbijenaRazmjena struct{ razlog string }

func (e odbijenaRazmjena) Error() string { return "čvor odbio razmjenu: " + e.razlog }

// odbioTailscale javlja da je drugi čvor odbio razmjenu na svojoj Tailscale
// adresi jer mu je Tailscale isključen. Ovaj tada zaboravi njegove Tailscale
// adrese i zove sljedeću adresu, kao da ih nikad nije znao.
func (s *Service) odbioTailscale(ctx context.Context, nodeID string, err error) bool {
	var o odbijenaRazmjena
	if !errors.As(err, &o) || o.razlog != razlogBezTailscalea {
		return false
	}
	s.upisiTailscale(ctx, nodeID, nil)
	return true
}

// naVlastitiTailscale javlja je li veza stigla na Tailscale adresu ovog
// računala. Raspon sam nije dovoljan (100.64.0.0/10 daju i mobilni
// operateri), pa adresa mora biti među adresama Tailscalea ovog računala.
func (s *Service) naVlastitiTailscale(a net.Addr) bool {
	ap, err := netip.ParseAddrPort(a.String())
	if err != nil || !tailscale.JeTailscale(ap.Addr()) {
		return false
	}
	return slices.Contains(s.vlastiteTailscale(), ap.Addr().Unmap())
}

// odbijTailscale odbija dolaznu razmjenu koja je stigla na Tailscale adresu
// ovog računala dok je Tailscale u razmjeni isključen; dnevnik dobije jedan
// redak po čvoru
func (s *Service) odbijTailscale(c *razmjena.Conn, peer *Peer) bool {
	if s.TailscaleUkljucen() || !s.naVlastitiTailscale(c.LocalAddr()) {
		return false
	}
	tko := razmjena.PublicKeyString(c.PeerKey)
	if peer != nil {
		tko = label(*peer)
	}
	if _, vec := s.javljenTailscale.LoadOrStore(tko, true); !vec {
		log.Printf("razmjena: %s je nazvao Tailscale adresu ovog računala (%s), a Tailscale je u razmjeni isključen ([sync] tailscale = false); razmjena je odbijena, a %s zove sljedeću adresu", tko, c.LocalAddr(), tko)
	}
	razmjena.Odbij(c, razlogBezTailscalea)
	return true
}
