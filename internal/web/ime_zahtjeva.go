package web

// Ime na koje je preglednik poslao zahtjev (zaglavlje Host). Klijent iz
// lokalne mreže smije više od klijenta izvana (početna lozinka, prijava bez
// PIN-a, postavljanje svježeg čvora), pa takav vrijedi samo zahtjev upućen
// imenu ili adresi za koju se zna da vodi na ovo računalo. Zahtjev na drugo
// ime ne odbija se, nego vrijedi kao izvana: radi kao i prije, samo uz
// pravila za prijavu s interneta.
//
// Poznato je:
//   - adresa (IPv4 ili IPv6), jer stranicu na adresi drži samo računalo s tom
//     adresom, pa i čvor u Dockeru, koji vidi samo adresu spremnika;
//   - ime bez točke (tower, copos-thinkcentre) i imena lokalne mreže
//     (.local, .lan, .home.arpa, .internal, localhost), koja javni DNS ne
//     poslužuje;
//   - Tailscaleovo ime računala (.ts.net), ali samo kad je veza stigla na
//     Tailscale adresu ovog računala;
//   - ime ovog računala, ime iz javne adrese (javna_adresa u gocop.toml) i
//     imena iz [web] lokalna_imena.
//
// Ono što smije samo osoba za ovim računalom (početna lozinka pod Postavom,
// svjež čvor bez koda, stanje za Postavu) traži više: vezu s ovog računala i
// ime localhost ili adresu petlje (Klijent.SOvogRacunala). Ime iz lokalne
// mreže može odgovoriti bilo koje računalo u istoj mreži.

import (
	"net"
	"net/netip"
	"os"
	"slices"
	"strings"
	"sync"
)

// lokalniNastavci su nastavci imena koje javni DNS ne poslužuje
var lokalniNastavci = []string{".localhost", ".local", ".lan", ".home.arpa", ".internal"}

// nastavakTailscalea je nastavak Tailscaleovih imena računala (MagicDNS)
const nastavakTailscalea = ".ts.net"

// imeRacunala je ime ovog računala malim slovima, pročitano jednom
var imeRacunala = sync.OnceValue(func() string {
	ime, err := os.Hostname()
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(ime, "."))
})

// imeIzZaglavlja je ime iz zaglavlja Host bez porta, zagrada i završne
// točke, malim slovima
func imeIzZaglavlja(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

// imeLokalneMreze: adresa, ime bez točke ili ime s nastavkom lokalne mreže
func imeLokalneMreze(ime string) bool {
	if _, err := netip.ParseAddr(ime); err == nil {
		return true
	}
	if ime == "localhost" || !strings.Contains(ime, ".") {
		return true
	}
	return slices.ContainsFunc(lokalniNastavci, func(n string) bool { return strings.HasSuffix(ime, n) })
}

// imeOvogRacunala: localhost, ime koje završava na .localhost ili adresa
// petlje (127.0.0.0/8, ::1). Bez zaglavlja Host (HTTP/1.0, alati) nema
// imena za provjeru.
func imeOvogRacunala(ime string) bool {
	if a, err := netip.ParseAddr(ime); err == nil {
		return a.Unmap().IsLoopback()
	}
	return ime == "" || ime == "localhost" || strings.HasSuffix(ime, ".localhost")
}

// poznatoIme javlja je li zahtjev upućen imenu ili adresi ovog računala;
// naTailscale: veza je stigla na Tailscale adresu ovog računala
func (s *Server) poznatoIme(ime string, naTailscale bool) bool {
	if ime == "" || imeLokalneMreze(ime) || ime == imeRacunala() {
		return true
	}
	if strings.HasSuffix(ime, nastavakTailscalea) {
		return naTailscale
	}
	return ime == imeIzAdrese(s.javnaAdresa) || slices.Contains(s.lokalnaImena, ime)
}

// oznaciIme upisuje klijentu što zaglavlje Host kaže o imenu zahtjeva
func (s *Server) oznaciIme(k *Klijent, host string) {
	ime := imeIzZaglavlja(host)
	k.TudjeIme = !s.poznatoIme(ime, k.NaTailscale)
	k.ImeOvogRacunala = imeOvogRacunala(ime)
}

// SetLokalnaImena zadaje imena pod kojima se program otvara u lokalnoj
// mreži, a nisu adresa ni ime računala, npr. ime iz DNS-a ureda pod javnom
// domenom (gocop.toml [web] lokalna_imena)
func (s *Server) SetLokalnaImena(imena []string) {
	s.lokalnaImena = nil
	for _, i := range imena {
		if ime := imeIzAdrese(i); ime != "" {
			s.lokalnaImena = append(s.lokalnaImena, ime)
		}
	}
}

// imeIzAdrese je ime iz javne adrese (https://cop.primjer.hr/ ili
// cop.primjer.hr:8443), malim slovima; prazno kad adrese nema
func imeIzAdrese(adresa string) string {
	a := strings.TrimSpace(adresa)
	if i := strings.Index(a, "://"); i >= 0 {
		a = a[i+3:]
	}
	if i := strings.IndexAny(a, "/?#"); i >= 0 {
		a = a[:i]
	}
	return imeIzZaglavlja(a)
}
