package peers

// Tailscale u razmjeni (Odluke (12. 10. 2026.)). Tailscale je osobna mreža
// jednog korisnika i imaju ga samo neka računala. Zapis čvora (peers) putuje
// knjigom svim čvorovima mreže, pa Tailscale adrese u njega ne ulaze nikad,
// ni vlastite ni tuđe.
//
// Čvor svoje Tailscale adrese javlja samo u razgovoru koji ide preko
// Tailscalea: veza je na Tailscale adresi ovog računala, a druga strana je
// na Tailscale adresi. Takav razgovor može voditi samo računalo iz iste
// Tailscale mreže, pa adrese ne dobije nijedan čvor izvan nje, ma što o
// sebi javio. Prvu Tailscale adresu drugog čvora ovaj dobije kad se s njim
// upari preko te adrese; prvi razgovor preko nje donese ostale, u oba
// smjera. Svaki čvor ih čuva samo kod sebe, u peer_sync.tailscale, i zove
// ih iza izravnih adresa, a ispred tunela. Čvor bez Tailscalea ne šalje ni
// ne prima ništa novo: njegove su poruke iste kao prije.

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// zaboravi njegove Tailscale adrese i zove sljedeću adresu. Čvor 0.0.38 to
// prepozna samo po ovom tekstu, pa se tekst ne mijenja.
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

// prekoTailscalea javlja ide li razgovor preko Tailscalea: druga strana je
// na Tailscale adresi, a veza na Tailscale adresi ovog računala (dok je
// Tailscale u razmjeni uključen). Do te adrese dolazi samo računalo iz iste
// Tailscale mreže. Kartice se čitaju tek kad je druga strana na Tailscale
// adresi, pa čvor bez Tailscalea ništa ne čita.
func (s *Service) prekoTailscalea(ova, druga net.Addr) bool {
	ip, ok := ipVeze(druga)
	if !ok || !tailscale.JeTailscale(ip) {
		return false
	}
	ip, _ = ipVeze(ova)
	return slices.Contains(s.mojeTailscale(), ip)
}

// ipVeze je IP adresa jednog kraja veze; veza kroz tunel je nema
func ipVeze(a net.Addr) (netip.Addr, bool) {
	ap, err := netip.ParseAddrPort(a.String())
	return ap.Addr().Unmap(), err == nil
}

// tailscaleZaDrugoga su adrese ovog čvora, s portom razmjene, koje javlja
// drugome: samo u razgovoru preko Tailscalea i kad ovaj prima razmjene
func (s *Service) tailscaleZaDrugoga(prekoTailscalea bool) []string {
	if !prekoTailscalea || s.ports.Exchange <= 0 {
		return nil
	}
	moje := s.mojeTailscale()
	out := make([]string, 0, len(moje))
	for _, a := range moje {
		out = append(out, netip.AddrPortFrom(a, uint16(s.ports.Exchange)).String())
	}
	return out
}

// primljeniTailscale su Tailscale adrese koje je drugi čvor javio u
// delti; vrijede samo iz razgovora preko Tailscalea
func primljeniTailscale(prekoTailscalea bool, adrese []string) []string {
	if !prekoTailscalea {
		return nil
	}
	return samoTailscale(adrese)
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

// zapamtiTailscale nakon uspješnog razgovora preko Tailscalea pamti
// Tailscale adrese koje je drugi čvor javio. Drugi razgovori ih ne nose, pa
// zapamćeno ostaje; zaboravlja se kad drugi čvor odbije razmjenu na
// Tailscale adresi (odbioTailscale).
func (s *Service) zapamtiTailscale(ctx context.Context, nodeID string, o syncOutcome) {
	if o.err != nil || len(o.tailscale) == 0 {
		return
	}
	s.upisiTailscale(ctx, nodeID, o.tailscale)
}

// zapamtiUparivanje: uparivanje preko Tailscale adrese drugog čvora pamti
// tu adresu, s njegovim portom razmjene, samo na ovom računalu, jer u zapis
// čvora ne ide. Bez toga čvorovi upareni samo preko Tailscalea ne bi
// imali nijednu adresu jedan za drugoga.
func (s *Service) zapamtiUparivanje(ctx context.Context, nodeID, host, port string) {
	ip, err := netip.ParseAddr(host)
	if err != nil || !tailscale.JeTailscale(ip) || len(s.mojeTailscale()) == 0 {
		return
	}
	nova := withPort(host, cmp.Or(port, fmt.Sprint(DefaultExchangePort)))
	s.upisiTailscale(ctx, nodeID, append([]string{nova}, s.tailscaleOd(ctx, nodeID)...))
}

// tailscaleZaSpajanje su spremljene Tailscale adrese čvora, kad ih ovaj
// smije zvati. Kartice se čitaju samo kad ima spremljenih adresa.
func (s *Service) tailscaleZaSpajanje(ctx context.Context, nodeID string) []string {
	adrese := s.tailscaleOd(ctx, nodeID)
	if len(adrese) == 0 || len(s.mojeTailscale()) == 0 {
		return nil
	}
	return adrese
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

// odbioTailscale javlja da je drugi čvor odbio razmjenu na svojoj Tailscale
// adresi jer mu je Tailscale isključen (KodBezTailscalea; čvor 0.0.38 kod
// ne šalje, pa se uzme iz razloga). Ovaj tada zaboravi njegove Tailscale
// adrese i zove sljedeću adresu, kao da ih nikad nije znao.
func (s *Service) odbioTailscale(ctx context.Context, nodeID string, err error) bool {
	var o odbijenaRazmjena
	if !errors.As(err, &o) || o.kod != razmjena.KodBezTailscalea {
		return false
	}
	s.upisiTailscale(ctx, nodeID, nil)
	return true
}

// naVlastitiTailscale javlja je li lokalna adresa veze, dolazne ili
// odlazne, Tailscale adresa ovog računala. Raspon sam nije dovoljan
// (100.64.0.0/10 daju i mobilni operateri), pa adresa mora biti među
// adresama Tailscalea ovog računala.
func (s *Service) naVlastitiTailscale(a net.Addr) bool {
	ip, ok := ipVeze(a)
	if !ok || !tailscale.JeTailscale(ip) {
		return false
	}
	return slices.Contains(s.vlastiteTailscale(), ip)
}

// putVeze je put veze: predloženi, osim kad je veza na Tailscale adresi
// ovog računala. Tada je išla preko Tailscalea i kad je druga strana na
// običnoj adresi (ruta podmreže, izlazni čvor). Vrijedi i dok je Tailscale
// u razmjeni isključen. Veza kroz tunel nema IP adresu, pa ostaje tunel.
// Druga strana na adresi iz 100.64.0.0/10 uz običnu adresu ovog računala
// nije Tailscale: taj raspon daju i mobilni operateri.
func (s *Service) putVeze(predlozen razmjena.Put, lokalna net.Addr) razmjena.Put {
	if s.naVlastitiTailscale(lokalna) {
		return razmjena.PutTailscale
	}
	return predlozen
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
	razmjena.Odbij(c, razmjena.KodBezTailscalea, razlogBezTailscalea)
	return true
}
