package web

// Privremene lozinke računa iz imenika.
//
// Kad se baza prvi put puni iz imenika uz nju, svaki račun dobije svoju
// nasumičnu privremenu lozinku (db.SeedInitialData). Program ih drži samo u
// memoriji, dok ih osoba za ovim računalom jednom ne preuzme na stranici
// /postavljanje/lozinke i ne preda ljudima; svatko svoju mijenja pri prvoj
// prijavi. Stranica vrijedi kao Postavljanje: s ovog računala, ili iz
// lokalne mreže s kodom iz dnevnika. Bez koda vrijedi samo zahtjev s ovog
// računala upućen na localhost, 127.0.0.1 ili [::1]; zahtjev s ovog
// računala upućen na drugo ime treba kod. Dok popis nije preuzet, ponovno
// pokretanje čvora složi nove lozinke i novi popis
// (db.ObnoviPocetneLozinke); nakon preuzimanja popisa više nema, a tko
// lozinku nije dobio, dobiva novu poništenjem (Djelatnici → Poništi
// lozinku ili gocop -ponisti-lozinku). Popis vrijedi 24 sata od prvog punjenja
// (Odluke (30. 10. 2026.)): tada se briše iz memorije i baze, uz redak u
// dnevniku, a stranica kaže da je rok prošao.

import (
	"bytes"
	"errors"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"gocop/internal/db"
	"gocop/internal/models"
)

// pocetneLozinke je popis koji se preuzima jednom, do roka
type pocetneLozinke struct {
	mu      sync.Mutex
	popis   []db.PocetnaLozinka
	rok     time.Time // do kada popis vrijedi; nula: bez roka
	istekao bool      // rok je prošao dok je čvor radio
	// obrisi briše popis iz baze kad mu prođe rok (nil: nema se što brisati)
	obrisi func() error
	// sada i poRoku su sat i budilica; nil znači pravi sat (time.Now,
	// time.AfterFunc)
	sada   func() time.Time
	poRoku func(d time.Duration, f func())
}

// postavi sprema popis nakon prvog punjenja i navije brisanje na rok
func (p *pocetneLozinke) postavi(popis []db.PocetnaLozinka, rok time.Time, obrisi func() error) {
	p.mu.Lock()
	p.popis, p.rok, p.obrisi, p.istekao = popis, rok, obrisi, false
	p.mu.Unlock()
	if len(popis) == 0 || rok.IsZero() {
		return
	}
	poRoku := p.poRoku
	if poRoku == nil {
		poRoku = func(d time.Duration, f func()) { time.AfterFunc(d, f) }
	}
	poRoku(rok.Sub(p.trenutak()), p.istekni)
}

// trenutak je sadašnje vrijeme po satu popisa
func (p *pocetneLozinke) trenutak() time.Time {
	if p.sada != nil {
		return p.sada()
	}
	return time.Now()
}

// istekni briše popis kojemu je prošao rok: iz memorije i iz baze, uz redak
// u dnevniku
func (p *pocetneLozinke) istekni() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.isteklo()
}

// isteklo radi brisanje za istekni i provjeriRok; zove se pod p.mu
func (p *pocetneLozinke) isteklo() {
	if p.popis == nil {
		return
	}
	p.popis, p.istekao = nil, true
	log.Printf("Postavljanje: popis privremenih lozinki iz imenika nitko nije preuzeo u 24 sata od prvog punjenja, pa je obrisan; lozinke se sad daju poništenjem")
	if p.obrisi == nil {
		return
	}
	if err := p.obrisi(); err != nil {
		log.Printf("Postavljanje: istekao popis privremenih lozinki nije obrisan iz baze (%v); briše se pri sljedećem pokretanju", err)
	}
}

// provjeriRok briše popis ako mu je rok prošao, i kad budilica kasni (npr.
// nakon što je računalo spavalo); zove se pod p.mu
func (p *pocetneLozinke) provjeriRok() {
	if !p.rok.IsZero() && !p.trenutak().Before(p.rok) {
		p.isteklo()
	}
}

// ceka javlja koliko lozinki čeka preuzimanje
func (p *pocetneLozinke) ceka() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.provjeriRok()
	return len(p.popis)
}

// stanje javlja koliko lozinki čeka, do kada, i je li rok prošao
func (p *pocetneLozinke) stanje() (ceka int, rok time.Time, istekao bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.provjeriRok()
	return len(p.popis), p.rok, p.istekao
}

// uzmi vraća popis i briše ga, pa ga drugi zahtjev više ne dobije
func (p *pocetneLozinke) uzmi() []db.PocetnaLozinka {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.provjeriRok()
	popis := p.popis
	p.popis = nil
	return popis
}

// vrati vraća popis koji nije stigao do preglednika, ako mu rok nije prošao
func (p *pocetneLozinke) vrati(popis []db.PocetnaLozinka) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.popis == nil && !p.istekao {
		p.popis = popis
		p.provjeriRok()
	}
}

// PocetneLozinkeData su podaci za pocetne_lozinke.html
type PocetneLozinkeData struct {
	Dopusteno bool
	Kod       string
	Error     string
	Izdanje   string
	Ceka      int                 // koliko lozinki čeka preuzimanje
	Rok       string              // do kada se popis smije preuzeti
	Istekao   bool                // popis je obrisan jer mu je prošao rok
	Popis     []db.PocetnaLozinka // samo u odgovoru na preuzimanje
}

// SetPocetneLozinke predaje stranici popis privremenih lozinki iz prvog
// punjenja s rokom do kojega vrijedi; obrisi briše popis iz baze kad je
// preuzet (prije nego što ode u preglednik) ili kad mu prođe rok (nil: nema
// se što brisati)
func (h *PostavljanjeHandler) SetPocetneLozinke(popis []db.PocetnaLozinka, rok time.Time, obrisi func() error) {
	h.preuzete = obrisi
	h.lozinke.postavi(popis, rok, obrisi)
}

// stanjeLozinki upisuje u podatke stranice koliko lozinki čeka, do kada i
// je li rok prošao
func (h *PostavljanjeHandler) stanjeLozinki(d *PocetneLozinkeData) {
	ceka, rok, istekao := h.lozinke.stanje()
	d.Ceka, d.Istekao = ceka, istekao
	if ceka > 0 && !rok.IsZero() {
		d.Rok = rok.In(models.Zagreb).Format("02.01.2006. u 15:04")
	}
}

// hostOvogRacunala javlja glasi li zahtjev na ime koje uvijek znači ovo
// računalo: localhost (i *.localhost), 127.0.0.1 ili [::1]
func hostOvogRacunala(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	a, err := netip.ParseAddr(host)
	return err == nil && a.IsLoopback()
}

// dopustenoLozinke: kao Postavljanje, ali bez koda samo zahtjev s ovog
// računala upućen na njegovo ime
func (h *PostavljanjeHandler) dopustenoLozinke(r *http.Request, kod string) bool {
	k := klijentIz(r)
	if !k.IzLokalneMreze() {
		return false
	}
	if k.Posrednik.IsLoopback() && hostOvogRacunala(r.Host) {
		return true
	}
	return h.tocanKod(kod)
}

// CekaLozinki javlja koliko privremenih lozinki čeka preuzimanje (za dnevnik)
func (h *PostavljanjeHandler) CekaLozinki() int { return h.lozinke.ceka() }

// iscrtajLozinke iscrtava stranicu; odgovor se nigdje ne sprema
func (h *PostavljanjeHandler) iscrtajLozinke(w http.ResponseWriter, status int, d PocetneLozinkeData) {
	w.Header().Set("Cache-Control", "no-store")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	iscrtajPrijavu(h.tmplLozinke, w, "pocetne_lozinke.html", d, "postavljanje: stranica privremenih lozinki")
}

// predajPopis šalje popis u preglednik. Popis se briše tek kad je stranica
// složena i kad je zapisano da je preuzet; ako stranica ne stigne do
// preglednika, popis ostaje za sljedeće preuzimanje.
func (h *PostavljanjeHandler) predajPopis(w http.ResponseWriter, d PocetneLozinkeData) {
	d.Popis = h.lozinke.uzmi()
	if len(d.Popis) == 0 {
		h.stanjeLozinki(&d)
		h.iscrtajLozinke(w, http.StatusOK, d)
		return
	}
	var b bytes.Buffer
	if err := h.tmplLozinke.ExecuteTemplate(&b, "pocetne_lozinke.html", d); err != nil {
		h.lozinke.vrati(d.Popis)
		log.Printf("postavljanje: stranica privremenih lozinki: %v", err)
		http.Error(w, "Popis se sad ne može pokazati. Pokušajte ponovno.", http.StatusInternalServerError)
		return
	}
	if err := h.zapisiPreuzimanje(); err != nil {
		h.lozinke.vrati(d.Popis)
		log.Printf("postavljanje: preuzimanje popisa privremenih lozinki nije zapisano: %v", err)
		http.Error(w, "Popis se sad ne može pokazati. Pokušajte ponovno.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := posalji(w, b.Bytes()); err != nil {
		h.lozinke.vrati(d.Popis)
		log.Printf("postavljanje: popis privremenih lozinki nije stigao do preglednika (%v); ostaje do ponovnog pokretanja", err)
	}
}

// posalji piše odgovor i odmah ga šalje, da se greška veze vidi ovdje
func posalji(w http.ResponseWriter, b []byte) error {
	if _, err := w.Write(b); err != nil {
		return err
	}
	if err := http.NewResponseController(w).Flush(); !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

// zapisiPreuzimanje zapisuje da je popis preuzet
func (h *PostavljanjeHandler) zapisiPreuzimanje() error {
	if h.preuzete == nil {
		return nil
	}
	return h.preuzete()
}

// PrikaziLozinke je GET /postavljanje/lozinke: kod i gumb za preuzimanje
func (h *PostavljanjeHandler) PrikaziLozinke(w http.ResponseWriter, r *http.Request) {
	if !klijentIz(r).IzLokalneMreze() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	kod := r.URL.Query().Get("kod")
	d := PocetneLozinkeData{Kod: kod, Izdanje: izdanjeZaPostavu()}
	h.stanjeLozinki(&d)
	d.Dopusteno = h.dopustenoLozinke(r, kod)
	status := http.StatusOK
	if !d.Dopusteno && kod != "" {
		d.Error = "Kod ne vrijedi. Prepišite ga iz dnevnika čvora (redak „Postavljanje: …”)."
		status = http.StatusForbidden
	}
	h.iscrtajLozinke(w, status, d)
}

// PreuzmiLozinke je POST /postavljanje/lozinke: pokaže popis jednom i obriše ga
func (h *PostavljanjeHandler) PreuzmiLozinke(w http.ResponseWriter, r *http.Request) {
	if !klijentIz(r).IzLokalneMreze() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	kod := r.PostFormValue("kod")
	d := PocetneLozinkeData{Kod: kod, Izdanje: izdanjeZaPostavu()}
	if !h.dopustenoLozinke(r, kod) {
		d.Error = "Popis se preuzima samo s ovog računala ili s kodom iz dnevnika."
		h.stanjeLozinki(&d)
		h.iscrtajLozinke(w, http.StatusForbidden, d)
		return
	}
	d.Dopusteno = true
	h.predajPopis(w, d)
}
