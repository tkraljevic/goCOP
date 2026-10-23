package web

// Provjera veze (plan WEB synca, F1b-3). Administrator gumbom „Provjeri
// vezu” u kartici Ovaj čvor izmjeri mrežu ovog računala preko STUN
// poslužitelja: kad WEB sync radi, na njegovoj utičnici, a kad ne radi, na
// privremenoj utičnici koju program zatvori nakon mjerenja. Mjeri program
// (cmd/gocop/povezivost.go), a ovdje su gumb i prikaz. Kartica pokazuje samo
// sažetak bez adresa; vlastitu javnu adresu, kandidate i izlaz prema STUN-u
// pokazuje blok Dohvatljivost na stranici Sinkronizacija, koju vidi samo
// administrator.

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gocop/internal/razmjena"
)

// ProvjeraVeze je ishod jedne provjere mreže ovog računala
type ProvjeraVeze struct {
	Kad        time.Time
	Port       int    // UDP port s kojeg je mjereno; 0: utičnice nije bilo
	Privremena bool   // utičnica je otvorena samo za ovu provjeru
	Upozorenje string // npr. udp_port je zauzet, pa je mjereno s nasumičnog
	Greska     string // provjera nije išla: nema STUN poslužitelja, utičnica ne radi
	Mjerenje   razmjena.MjerenjeMreze
	Vrijedi    string               // vrsta mreže koja vrijedi (histereza); prazno uz privremenu utičnicu
	Kandidati  []razmjena.Kandidat  // vlastite adrese za WEB sync
	Razlozi    []string             // zašto koja vlastita adresa nije kandidat
	Izlazi     []razmjena.IzlazSTUN // kuda promet prema STUN-u izlazi
}

// Sazetak je ishod jednom rečenicom, bez adresa, kao u planu: „UDP radi
// na :4713. NAT je lak i čuva port. STUN: Cloudflare 8 ms.”
func (p ProvjeraVeze) Sazetak() string {
	if p.Greska != "" {
		return p.Greska
	}
	pocetak := "UDP radi na :%d. "
	if p.Mjerenje.Vrsta == razmjena.UDPBlokiran {
		pocetak = "UDP utičnica je na :%d, ali odgovora nema. "
	}
	return fmt.Sprintf(pocetak, p.Port) + velikimSlovom(p.Mjerenje.Opis())
}

func velikimSlovom(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

// SetProvjeraVeze daje poslužitelju provjeru mreže i zadnji ishod; zove se
// prije Start
func (s *Server) SetProvjeraVeze(provjeri func(context.Context) ProvjeraVeze, zadnja func() *ProvjeraVeze) {
	s.povezivost.provjeri, s.povezivost.zadnja = provjeri, zadnja
}

func (p povezivostCvora) zadnjaProvjera() *ProvjeraVeze {
	if p.zadnja == nil {
		return nil
	}
	return p.zadnja()
}

// ProvjeraVezePrikaz je dio kartice Ovaj čvor s gumbom „Provjeri vezu”
type ProvjeraVezePrikaz struct {
	Nudi    bool   // administrator, svojim očima, izravno iz lokalne mreže
	Zabrana string // zašto ne s ovog zahtjeva
	Zadnja  *ProvjeraVeze
}

// prikazProvjereVeze je nil kad program provjeru ne nudi
func (s *Server) prikazProvjereVeze(zabrana *zabranaCvora) *ProvjeraVezePrikaz {
	if s.povezivost.provjeri == nil {
		return nil
	}
	p := &ProvjeraVezePrikaz{Nudi: zabrana == nil, Zadnja: s.povezivost.zadnjaProvjera()}
	if zabrana != nil {
		p.Zabrana = zabrana.provjera
	}
	return p
}

// handleProvjeriVezu: POST, samo administrator, izravno iz lokalne mreže.
// Mjerenje traje nekoliko sekundi; ishod se pamti u programu, pa ga
// stranica iscrta nakon preusmjeravanja.
func (s *Server) handleProvjeriVezu(w http.ResponseWriter, r *http.Request) {
	if z := smijeMijenjatiCvor(r); z != nil {
		http.Error(w, z.provjera, http.StatusForbidden)
		return
	}
	if s.povezivost.provjeri == nil {
		redirectWith(w, r, "/settings#provjera-veze", "error", "Provjera veze na ovom čvoru nije dostupna.")
		return
	}
	ishod := s.povezivost.provjeri(r.Context())
	log.Printf("povezivost: provjeru veze pokrenuo %s s %s", korisnikZahtjeva(r), klijentIz(r))
	if ishod.Greska != "" {
		redirectWith(w, r, "/settings#provjera-veze", "error", "Provjera veze nije prošla: "+ishod.Greska)
		return
	}
	redirectWith(w, r, "/settings#provjera-veze", "success", "Provjera veze je gotova: "+ishod.Sazetak())
}

// DohvatljivostPrikaz je blok „Dohvatljivost ovog računala” na stranici
// Sinkronizacija. Vidi ga samo administrator, pa smije imati vlastitu
// javnu adresu i vlastite adrese u lokalnoj mreži.
type DohvatljivostPrikaz struct {
	Provjera   *ProvjeraVeze // nil: provjere još nije bilo
	Vrijedi    string        // vrsta mreže koja vrijedi, riječima
	Javna      string
	Kandidati  []string
	Izlazi     []string // rečenice za izlaz prema STUN-u koji nije običan
	Upozorenja []string
}

// rijeciVrste je vrsta mreže riječima, za „vrijedi”
var rijeciVrste = map[string]string{
	razmjena.NATLak:      "NAT je lak",
	razmjena.NATOvisan:   "NAT ovisi o odredištu",
	razmjena.UDPBlokiran: "UDP do STUN poslužitelja ne prolazi",
}

// rijeciIzlaza je izlaz prema STUN poslužitelju riječima
var rijeciIzlaza = map[string]string{
	razmjena.IzlazTailscale: "Promet prema %s ide kroz Tailscale (izlazni čvor), pa se javna adresa ne nudi.",
	razmjena.IzlazVPN:       "Promet prema %s ide kroz VPN, pa se javna adresa ne nudi.",
	razmjena.IzlazBezRute:   "Prema %s nema rute.",
}

// dohvatljivost slaže blok za stranicu Sinkronizacija iz zadnje provjere
func (s *Server) dohvatljivost() *DohvatljivostPrikaz {
	d := &DohvatljivostPrikaz{Provjera: s.povezivost.zadnjaProvjera()}
	if u := s.upozorenjeLanAdrese(d.Provjera != nil); u != "" {
		d.Upozorenja = append(d.Upozorenja, u)
	}
	if d.Provjera == nil {
		return d
	}
	p := d.Provjera
	d.Vrijedi = rijeciVrste[p.Vrijedi]
	if p.Mjerenje.Javna.IsValid() {
		d.Javna = p.Mjerenje.Javna.String()
	}
	for _, k := range p.Kandidati {
		d.Kandidati = append(d.Kandidati, opisKandidata(k))
	}
	for _, i := range p.Izlazi {
		if tekst, ok := rijeciIzlaza[i.Kroz]; ok {
			d.Izlazi = append(d.Izlazi, fmt.Sprintf(tekst, i.Ime))
		}
	}
	return d
}

func opisKandidata(k razmjena.Kandidat) string {
	if k.Vrsta == razmjena.KandidatJavni {
		return k.Adresa.String() + " (javna)"
	}
	return k.Adresa.String() + " (" + k.Sucelje + ")"
}

// upozorenjaPovezivosti su rečenice WEB synca za pločicu razmjene: nakon
// padova i u spremniku bez lan_adresa
func (s *Server) upozorenjaPovezivosti() []string {
	out := s.povezivost.upozorenja()
	if u := s.upozorenjeLanAdrese(false); u != "" {
		out = append(out, u)
	}
	return out
}

// upozorenjeLanAdrese: spremnik u mostu bez lan_adresa ne zna adresu
// računala u kućnoj mreži, pa ga druga računala u kući WEB syncom ne nađu.
// Na pločici vrijedi uz prijavu za WEB sync, a u bloku Dohvatljivost i
// nakon provjere bez nje (bezPrijave). Kad je zadnja provjera
// našla adresu izvan Dockerove mreže, spremnik radi s mrežom računala
// (host, macvlan) i adresa ne treba.
func (s *Server) upozorenjeLanAdrese(bezPrijave bool) string {
	if s.ponovno.nacin != USpremniku || s.postavke == nil {
		return ""
	}
	p := s.postavke.snimka().Vrijedi.Povezivost
	if !p.WebSyncPrijava && !bezPrijave || strings.TrimSpace(p.LanAdresa) != "" || s.imaAdresuIzvanDockera() {
		return ""
	}
	return "Ako goCOP u spremniku radi u mostu (bridge), WEB sync ne zna adresu računala u lokalnoj mreži, pa ga druga " +
		"računala u kući tim putem ne nađu. Administrator tada upiše [povezivost] lan_adresa u gocop.toml."
}

// dockerMreza je raspon iz kojeg Docker daje adrese spremnicima u mostu
var dockerMreza = netip.MustParsePrefix("172.16.0.0/12")

// imaAdresuIzvanDockera: zadnja provjera našla je lokalnu adresu izvan
// Dockerove mreže, pa spremnik vidi kućnu mrežu
func (s *Server) imaAdresuIzvanDockera() bool {
	z := s.povezivost.zadnjaProvjera()
	return z != nil && slices.ContainsFunc(z.Kandidati, func(k razmjena.Kandidat) bool {
		return k.Vrsta == razmjena.KandidatLokalni && !dockerMreza.Contains(k.Adresa.Addr())
	})
}
