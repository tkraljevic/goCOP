package peers

// Naučene adrese i tvrdnje o adresama (plan WEB synca, F0). Adresu drugog
// računala ovo računalo nauči samo kad je samo nazvalo i druga strana je na
// njoj dokazala ključ iz članstva. Veza je pritom išla izravno, u lokalnoj
// mreži ili na adresu (ne preko Tailscalea, kroz tunel ni probijanjem), a ne
// s adrese iz Tailscaleovih raspona ovog računala (ruta podmreže). Adresa
// druge strane mora biti u privatnoj mreži. Naučene adrese ostaju na ovom
// računalu, u peer_sync.adrese, i nikad ne putuju: zapis računala zbog
// razmjene ne dobiva novu verziju (Odluke (14. 10. 2026.), 2).
//
// Sve drugo je tvrdnja i ostaje samo u memoriji: adresa na kojoj se računalo
// javilo u lokalnoj mreži (Discover) i adresa s koje je stigla dolazna
// razmjena. U lokalnoj mreži može se javiti bilo tko, a dolazna veza može
// stići preko usmjerivača ili posrednika (NAT), uvijek s prolaznog porta.
// Razmjena tvrdnju nazove jednom, kad dođe na red (adreseZaSpajanje). Ako se
// na njoj javi očekivani ključ, adresa se nauči s portom na koji je poziv
// otišao.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/netip"
	"slices"
	"time"

	"gocop/internal/razmjena"
	"gocop/internal/tailscale"
)

// najviseNaucenih je koliko naučenih adresa drugog računala ovo pamti
// (Odluke (14. 10. 2026.), 2); ostaju one s najnovijim uspjehom
const najviseNaucenih = 4

// najviseTvrdnji je koliko tvrdnji o adresama jednog računala ovo pamti.
// Adresu u lokalnoj mreži može javiti bilo tko, svaki put drugu, pa ostaju
// najnovije.
const najviseTvrdnji = 4

// naucena je naučena adresa drugog računala i kad je spajanje na nju zadnji
// put uspjelo
type naucena struct {
	Adresa string    `json:"adresa"`
	Uspjeh time.Time `json:"uspjeh"`
}

// nauci su jedina vrata za učenje adresa (noteSync). Odlazna veza nauči
// adresu na koju je otišla. Dolazna samo tvrdi da je druga strana na adresi
// s koje je veza stigla, pa ta adresa, s portom razmjene druge strane,
// postaje tvrdnja koju razmjena nazove kad dođe na red.
func (s *Service) nauci(ctx context.Context, peer *Peer, c *razmjena.Conn, o syncOutcome) {
	adresa, ok := s.adresaZaUcenje(peer, c, o.dolazna)
	if !ok {
		return
	}
	if o.dolazna {
		s.dodajTvrdnju(peer.NodeID, kandidat{adresa: adresa, put: razmjena.PutAdresa})
		return
	}
	s.upisiNaucenu(ctx, peer.NodeID, adresa, time.Now())
}

// adresaZaUcenje je adresa druge strane koju veza smije naučiti (odlazna) ili
// predložiti kao tvrdnju (dolazna). Port odlazne je onaj na koji je poziv
// otišao, a dolazne port razmjene koji se za računalo zna, jer je izvorni
// port dolazne veze prolazni.
func (s *Service) adresaZaUcenje(peer *Peer, c *razmjena.Conn, dolazna bool) (string, bool) {
	if !vezaZaUcenje(c) {
		return "", false
	}
	ap, ok := privatnaIP(c.RemoteAddr())
	switch {
	case !ok:
		return "", false
	case dolazna:
		return withPort(ap.Addr().String(), s.exchangePortOf(*peer)), true
	}
	return ap.String(), true
}

// vezaZaUcenje javlja smije li veza učiti: uspostavljena je, pa je druga
// strana dokazala očekivani ključ, išla je izravno (uciPut) i nije išla s
// adrese iz Tailscaleovih raspona ovog računala. Za to zadnje kartice se ne
// gledaju: Mac kojem je u Tailscaleu isključen IPv6 svoju Tailscale adresu ne
// prepozna (putVeze tada kaže adresa), a ni preko rute podmreže adresa druge
// strane ne govori kako do nje doći izravno.
func vezaZaUcenje(c *razmjena.Conn) bool {
	return c != nil && uciPut(c.Put) && !naTailscaleRasponu(c.LocalAddr())
}

// uciPut javlja smije li veza koja je išla tim putem učiti: samo izravna veza
// u lokalnoj mreži ili na adresu
func uciPut(p razmjena.Put) bool { return p == razmjena.PutLAN || p == razmjena.PutAdresa }

// naTailscaleRasponu javlja je li kraj veze na adresi iz Tailscaleovih
// raspona. Veza kroz tunel nema IP adresu, pa nije.
func naTailscaleRasponu(a net.Addr) bool {
	ip, ok := ipVeze(a)
	return ok && tailscale.JeTailscale(ip)
}

// privatnaIP je IP adresa s portom kraja veze, kad je u privatnoj mreži
// (10/8, 172.16/12, 192.168/16, fc00::/7) i nije Tailscaleova. Tailscaleov
// fd7a:115c:a1e0::/48 je u fc00::/7, pa se izbacuje posebno. 100.64.0.0/10
// (Tailscale, mobilni operateri), loopback, link-local i javne adrese nisu
// privatne, a IPv4 zapisan kao IPv6 (::ffff:192.168.1.2) vrijedi kao IPv4.
func privatnaIP(a net.Addr) (netip.AddrPort, bool) {
	ap, err := netip.ParseAddrPort(a.String())
	ip := ap.Addr().Unmap().WithZone("")
	return netip.AddrPortFrom(ip, ap.Port()), err == nil && ip.IsPrivate() && !tailscale.JeTailscale(ip)
}

// upisiNaucenu pamti adresu drugog računala na kojoj je spajanje uspjelo u
// trenutku kada. Ista adresa se ne ponavlja, popis ide od najnovijeg uspjeha,
// a iznad najviseNaucenih odlaze najstariji. Upisuje se kao upisiTailscale.
func (s *Service) upisiNaucenu(ctx context.Context, nodeID, adresa string, kada time.Time) {
	l := slices.DeleteFunc(s.citajNaucene(ctx, nodeID), func(n naucena) bool { return n.Adresa == adresa })
	l = append(l, naucena{Adresa: adresa, Uspjeh: kada.UTC()})
	slices.SortStableFunc(l, func(a, b naucena) int { return b.Uspjeh.Compare(a.Uspjeh) })
	b, _ := json.Marshal(l[:min(len(l), najviseNaucenih)]) // popis adresa i vremena uvijek se da zapisati
	if _, err := s.db.ExecContext(ctx, `INSERT INTO peer_sync (node_id, adrese) VALUES (?, ?)
		ON CONFLICT(node_id) DO UPDATE SET adrese = excluded.adrese`, nodeID, string(b)); err != nil {
		log.Printf("razmjena: naučena adresa računala %s nije spremljena: %v", nodeID, err)
	}
}

// naucene su naučene adrese drugog računala, od najnovijeg uspjeha
func (s *Service) naucene(ctx context.Context, nodeID string) []string {
	var out []string
	for _, n := range s.citajNaucene(ctx, nodeID) {
		out = append(out, n.Adresa)
	}
	return out
}

// citajNaucene čita naučene adrese drugog računala; kad ih nema ili se ne
// daju pročitati, nema nijedne
func (s *Service) citajNaucene(ctx context.Context, nodeID string) []naucena {
	var b string
	if err := s.db.QueryRowContext(ctx, `SELECT adrese FROM peer_sync WHERE node_id = ?`, nodeID).Scan(&b); err != nil {
		return nil
	}
	var l []naucena
	_ = json.Unmarshal([]byte(b), &l) // prazno ili pokvareno: nijedna
	return l
}

// tvrdnjaIz je adresa iz pronalaženja u lokalnoj mreži kao kandidat za
// spajanje. Tailscale adresa nije tvrdnja: Tailscale adrese razmjena zove
// samo iz peer_sync.tailscale, i to dok je Tailscale u razmjeni uključen.
func tvrdnjaIz(f razmjena.Found) (kandidat, bool) {
	return kandidat{adresa: withPort(f.Addr, fmt.Sprint(f.ExchangePort)), put: razmjena.PutLAN}, !jeTailscaleAdresa(f.Addr)
}

// zapamtiTvrdnju pamti adresu na kojoj se poznato računalo javilo u lokalnoj
// mreži (Discover) i kad je viđeno. Kad je viđeno upisuje se izravno, kao
// bilješka o razmjeni, bez nove verzije zapisa računala.
func (s *Service) zapamtiTvrdnju(ctx context.Context, f razmjena.Found) {
	if k, ok := tvrdnjaIz(f); ok {
		s.dodajTvrdnju(f.DeviceID, k)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE peers SET last_seen = ? WHERE node_id = ?`, time.Now().UTC(), f.DeviceID); err != nil {
		log.Printf("razmjena: nije spremljeno kad je računalo %s viđeno u lokalnoj mreži: %v", f.DeviceID, err)
	}
}

// dodajTvrdnju pamti tvrdnju o adresi računala kao najnoviju. Ista adresa se
// ne ponavlja, a iznad najviseTvrdnji odlaze najstarije.
func (s *Service) dodajTvrdnju(nodeID string, k kandidat) {
	s.veze.mu.Lock()
	defer s.veze.mu.Unlock()
	if s.veze.tvrdnje == nil {
		s.veze.tvrdnje = map[string][]kandidat{}
	}
	l := slices.DeleteFunc(s.veze.tvrdnje[nodeID], func(t kandidat) bool { return t.adresa == k.adresa })
	l = append([]kandidat{k}, l...)
	s.veze.tvrdnje[nodeID] = l[:min(len(l), najviseTvrdnji)]
}

// tvrdnjeZa su tvrdnje o adresama računala koje razmjena još nije nazvala,
// najnovija prva. Tvrdnja ostaje dok je razmjena ne nazove (potrosiTvrdnju),
// pa ne nestane kad razmjena uspije na adresi ispred nje.
func (s *Service) tvrdnjeZa(nodeID string) []kandidat {
	s.veze.mu.Lock()
	defer s.veze.mu.Unlock()
	return slices.Clone(s.veze.tvrdnje[nodeID])
}

// tvrdnjePutem su tvrdnje o adresama računala koje idu tim putem: lan su
// adrese iz pronalaženja, a adresa one s kojih je stigla dolazna razmjena
func (s *Service) tvrdnjePutem(nodeID string, put razmjena.Put) []kandidat {
	return slices.DeleteFunc(s.tvrdnjeZa(nodeID), func(k kandidat) bool { return k.put != put })
}

// potrosiTvrdnju zaboravlja tvrdnju o adresi koju je razmjena upravo nazvala
// (nazovi). Tvrdnja vrijedi za jedan poziv: ako se na adresi javio očekivani
// ključ, bilješka o razmjeni je nauči (nauci).
func (s *Service) potrosiTvrdnju(nodeID, adresa string) {
	s.veze.mu.Lock()
	defer s.veze.mu.Unlock()
	if l, ok := s.veze.tvrdnje[nodeID]; ok {
		s.veze.tvrdnje[nodeID] = slices.DeleteFunc(l, func(t kandidat) bool { return t.adresa == adresa })
	}
}
