// Paket poslovi drži ono što traje dulje od jednog klika.
//
// Dosad se svaki dugi posao — izgradnja letve, izdavanje paketa, sažimanje
// baze — vrtio unutar jednog HTTP zahtjeva. Preglednik je stajao na bijelom
// jer poslužitelj nije imao odakle javiti gdje je: bio je zauzet baš time.
// Ovdje posao dobiva svoj broj, radi u pozadini i javlja korake, a stranica ga
// pita kako stoji.
//
// Poslovi žive u memoriji, ne u bazi. Posao koji je prekinut gašenjem programa
// nije ni bio dovršen, pa nema što ni pamtiti; ono što je stiglo na disk ostaje
// na disku i vidi se na svom mjestu.
package poslovi

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Koliko se gotov posao još drži da ga stranica stigne pročitati i pokazati
// ispis. Posao koji traje nema granicu — gradnja cijele arhive traje koliko
// traje.
const trajanjeGotovog = 30 * time.Minute

// Stanja posla. Posao je ili u tijeku, ili je prošao, ili je pao; četvrtog nema.
const (
	UTijeku = "traje"
	Gotov   = "gotov"
	Pao     = "pao"
)

// Posao je jedan dugi posao u tijeku. Sve što o sebi javlja prolazi kroz
// bravu, jer ga čita druga dretva — ona koja poslužuje pitanje stranice.
type Posao struct {
	ID       string
	Naziv    string
	Korisnik string
	// Odrediste je stranica na koju se ide kad posao završi. Prazno znači da
	// stranica sama zna što će.
	Odrediste string

	mu      sync.Mutex
	poceo   time.Time
	zavrsio time.Time
	stanje  string
	sto     string // čime se posao bavi upravo sad
	gotovo  int
	ukupno  int // 0 znači da se ne zna koliko ih je — traka tad samo putuje
	redci   []string
	nedovrs string // dio retka koji je stigao bez završnog prijeloga
	greska  string
	sazetak string // jedna rečenica koju stranica pokaže na kraju
	plod    any    // ono što je posao izradio, za stranicu koja ga čeka
}

// Zavrsi zapisuje kako je prošlo: rečenica koju čovjek pročita i ono što je
// posao izradio, da stranica poslije ne mora računati isto iznova.
func (p *Posao) Zavrsi(sazetak string, plod any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sazetak, p.plod = sazetak, plod
}

// Plod vraća ono što je posao izradio; nil ako ništa.
func (p *Posao) Plod() any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.plod
}

// Pogled je ono što odlazi stranici. Zaseban tip, jer Posao ima bravu i polja
// koja se ne serijaliziraju.
type Pogled struct {
	ID        string   `json:"id"`
	Naziv     string   `json:"naziv"`
	Stanje    string   `json:"stanje"`
	Sto       string   `json:"sto"`
	Gotovo    int      `json:"gotovo"`
	Ukupno    int      `json:"ukupno"`
	Postotak  int      `json:"postotak"` // -1 kad se ne zna koliko posla ima
	Sekundi   int      `json:"sekundi"`
	Redci     []string `json:"redci"`
	Greska    string   `json:"greska"`
	Sazetak   string   `json:"sazetak"`
	Odrediste string   `json:"odrediste"`
}

// Korak javlja čime se posao bavi. Ukupno 0 znači da se broj koraka ne zna
// unaprijed — bolje reći "spajam" bez postotka nego izmisliti postotak.
func (p *Posao) Korak(sto string, gotovo, ukupno int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sto, p.gotovo, p.ukupno = sto, gotovo, ukupno
}

// Write čini posao odredištem za ispis. Gradnja već piše svoj tijek u
// io.Writer, pa joj ne treba ništa mijenjati: svaki redak koji napiše postaje
// redak dnevnika koji stranica pokazuje dok posao traje.
func (p *Posao) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nedovrs += string(b)
	for {
		i := strings.IndexByte(p.nedovrs, '\n')
		if i < 0 {
			break
		}
		redak := strings.TrimRight(p.nedovrs[:i], "\r")
		p.nedovrs = p.nedovrs[i+1:]
		p.redci = append(p.redci, redak)
	}
	return len(b), nil
}

// Stanje daje presliku za stranicu.
func (p *Posao) Stanje() Pogled {
	p.mu.Lock()
	defer p.mu.Unlock()
	kraj := p.zavrsio
	if kraj.IsZero() {
		kraj = time.Now()
	}
	g := Pogled{
		ID: p.ID, Naziv: p.Naziv, Stanje: p.stanje, Sto: p.sto,
		Gotovo: p.gotovo, Ukupno: p.ukupno, Postotak: -1,
		Sekundi: int(kraj.Sub(p.poceo).Seconds()),
		Greska:  p.greska, Sazetak: p.sazetak, Odrediste: p.Odrediste,
	}
	if p.ukupno > 0 {
		g.Postotak = p.gotovo * 100 / p.ukupno
		if g.Postotak > 100 {
			g.Postotak = 100
		}
	}
	if p.stanje != UTijeku {
		g.Postotak = 100
	}
	g.Redci = append([]string(nil), p.redci...)
	return g
}

// Dnevnik vraća sve što je posao ispisao, kao jedan tekst.
func (p *Posao) Dnevnik() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := strings.Join(p.redci, "\n")
	if p.nedovrs != "" {
		s += "\n" + p.nedovrs
	}
	return s
}

// Greska vraća zašto je posao pao; prazno ako nije.
func (p *Posao) Greska() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.greska
}

// Traje javlja je li posao još u tijeku.
func (p *Posao) Traje() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stanje == UTijeku
}

func (p *Posao) zavrsi(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.nedovrs != "" {
		p.redci = append(p.redci, p.nedovrs)
		p.nedovrs = ""
	}
	p.zavrsio = time.Now()
	if err != nil {
		p.stanje, p.greska = Pao, err.Error()
		return
	}
	p.stanje = Gotov
}

// Registar drži poslove ovog programa.
type Registar struct {
	mu sync.Mutex
	m  map[string]*Posao
	br int64
}

func NoviRegistar() *Registar { return &Registar{m: map[string]*Posao{}} }

// Pokreni daje poslu broj, pušta ga u pozadinu i odmah se vraća. Posao koji
// padne ne ruši program: greška se zapiše i čovjek je vidi na stranici.
func (r *Registar) Pokreni(naziv, korisnik, odrediste string, f func(*Posao) error) *Posao {
	r.mu.Lock()
	r.pospremi()
	r.br++
	p := &Posao{
		ID:       strconv.FormatInt(time.Now().Unix(), 36) + "-" + strconv.FormatInt(r.br, 36),
		Naziv:    naziv,
		Korisnik: korisnik,
		poceo:    time.Now(),
		stanje:   UTijeku,
		sto:      "počinjem",
	}
	// Odrediste zna za svoj broj tek sad, jer ga stranica traži u putanji.
	p.Odrediste = strings.ReplaceAll(odrediste, "{id}", p.ID)
	r.m[p.ID] = p
	r.mu.Unlock()

	go func() {
		defer func() {
			if x := recover(); x != nil {
				p.zavrsi(fmt.Errorf("posao je pukao: %v", x))
			}
		}()
		p.zavrsi(f(p))
	}()
	return p
}

// Nadi vraća posao ako pripada tome tko pita. Tuđi posao se ne pokazuje: u
// njegovu dnevniku piše što je netko drugi radio s arhivom.
func (r *Registar) Nadi(id, korisnik string) (*Posao, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ima := r.m[id]
	if !ima || p.Korisnik != korisnik {
		return nil, false
	}
	return p, true
}

// pospremi briše davno gotove poslove. Zove se pod bravom.
func (r *Registar) pospremi() {
	sad := time.Now()
	for id, p := range r.m {
		p.mu.Lock()
		staro := p.stanje != UTijeku && sad.Sub(p.zavrsio) > trajanjeGotovog
		p.mu.Unlock()
		if staro {
			delete(r.m, id)
		}
	}
}
