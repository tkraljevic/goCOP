package main

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gogpu/systray"

	"gocop/internal/izdanje"
	"gocop/internal/postava"
)

// lazniSystemd glumi systemctl i journalctl za uslugu
type lazniSystemd struct {
	mu        sync.Mutex
	aktivnost string
	pozivi    []string
	journal   string
	greska    error
}

func (l *lazniSystemd) naredba(_ context.Context, ime string, arg ...string) ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	poziv := strings.Join(append([]string{ime}, arg...), " ")
	l.pozivi = append(l.pozivi, poziv)
	switch {
	case strings.HasPrefix(poziv, "systemctl show"):
		return []byte("ActiveState=" + l.aktivnost + "\n"), nil
	case strings.HasPrefix(poziv, "systemctl stop"):
		l.aktivnost = "inactive"
	case ime == "journalctl":
		return []byte(l.journal), l.greska
	}
	return nil, nil
}

func (l *lazniSystemd) pozvano(pocetak string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.ContainsFunc(l.pozivi, func(p string) bool { return strings.HasPrefix(p, pocetak) })
}

// lazniCvor je vlastiti čvor koji samo bilježi
type lazniCvor struct {
	zaustavljen int
	greska      error
	pokrenut    int
	neKrece     error
}

func (c *lazniCvor) Instaliran() bool                       { return true }
func (c *lazniCvor) Pokreni() error                         { c.pokrenut++; return c.neKrece }
func (c *lazniCvor) Zaustavi(time.Duration) error           { c.zaustavljen++; return c.greska }
func (c *lazniCvor) Proces() postava.StanjeProcesa          { return postava.StanjeProcesa{Radi: true} }
func (c *lazniCvor) Zdravlje(context.Context) (string, int) { return "", 0 }
func (c *lazniCvor) NaPromjenu(func())                      {}

// lazniProzori bilježe poruke i odgovaraju na pitanja
type lazniProzori struct {
	odgovor    bool
	pitanja    []string
	poruke     []string
	otvoreno   []string
	obavijesti []string
}

// novaTraka je traka bez ikone u sustavu: s pravom Postavom u privremenoj
// mapi i lažnim prozorima. Uz l je čvor usluga sustava.
func novaTraka(t *testing.T, l *lazniSystemd) (*traka, *lazniProzori) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	p := postava.Nova(filepath.Join(t.TempDir(), postava.ImeMapePostave, "gocop-postava"), verzija)
	if l != nil {
		u := postava.NovaUsluga()
		u.Naredba = l.naredba
		p.KaoUsluga(u)
	} else if p.UslugaSustava() {
		t.Skip("na ovom računalu postoji usluga gocop")
	}
	pr := &lazniProzori{}
	tr := &traka{p: p, ctx: context.Background(),
		poruka:     func(_, tekst string, _ bool) { pr.poruke = append(pr.poruke, tekst) },
		pitanje:    func(_, tekst string) bool { pr.pitanja = append(pr.pitanja, tekst); return pr.odgovor },
		pokazi:     func(cilj string) error { pr.otvoreno = append(pr.otvoreno, cilj); return nil },
		obavijesti: func(tekst string) { pr.obavijesti = append(pr.obavijesti, tekst) },
	}
	return tr, pr
}

// natpisi čita natpise stavki izbornika. systray ih ne daje van, pa se
// čitaju iz njegove unutarnje strukture (github.com/gogpu/systray v0.2.8).
func natpisi(m *systray.Menu) []string {
	stavke := reflect.ValueOf(m).Elem().FieldByName("impl").Elem().FieldByName("Items")
	var out []string
	for i := 0; i < stavke.Len(); i++ {
		out = append(out, stavke.Index(i).Elem().FieldByName("Label").String())
	}
	return out
}

func TestIzbornikUzUsluguBezMapePodataka(t *testing.T) {
	tr, _ := novaTraka(t, &lazniSystemd{aktivnost: "active"})
	stavke := natpisi(tr.izbornik())
	if slices.Contains(stavke, "Otvori mapu s podacima") {
		t.Errorf("uz uslugu ponuđena mapa podataka koju korisnik ne može čitati: %v", stavke)
	}
	for _, s := range []string{"Otvori goCOP", "Zaustavi", "Provjeri nadogradnje", "Dnevnik čvora", "Pokreni pri prijavi", "O programu", "Izlaz"} {
		if !slices.Contains(stavke, s) {
			t.Errorf("nema stavke %q: %v", s, stavke)
		}
	}

	vlastiti, _ := novaTraka(t, nil)
	if stavke := natpisi(vlastiti.izbornik()); !slices.Contains(stavke, "Otvori mapu s podacima") {
		t.Errorf("uz vlastiti čvor nema mape podataka: %v", stavke)
	}
}

func TestZaustavljanjeUslugePita(t *testing.T) {
	l := &lazniSystemd{aktivnost: "active"}
	tr, pr := novaTraka(t, l)
	tr.zaustavi()
	if len(pr.pitanja) != 1 || !strings.Contains(pr.pitanja[0], "nitko ga u uredu ne može otvoriti") {
		t.Fatalf("pitanje: %v", pr.pitanja)
	}
	if l.pozvano("systemctl stop") {
		t.Fatal("usluga zaustavljena bez potvrde")
	}
	pr.odgovor = true
	tr.zaustavi()
	if !l.pozvano("systemctl stop gocop.service") {
		t.Error("usluga nije zaustavljena nakon potvrde")
	}

	// vlastiti čvor se zaustavlja bez pitanja; greška ide u poruku
	vlastiti, pr := novaTraka(t, nil)
	c := &lazniCvor{greska: errors.New("čvor se ne da zaustaviti")}
	vlastiti.p.Cvor = c
	vlastiti.zaustavi()
	if c.zaustavljen != 1 || len(pr.pitanja) != 0 || len(pr.poruke) != 1 {
		t.Errorf("vlastiti čvor: zaustavljen %d, pitanja %v, poruke %v", c.zaustavljen, pr.pitanja, pr.poruke)
	}
}

func TestDnevnikCvoraUzUslugu(t *testing.T) {
	l := &lazniSystemd{aktivnost: "active", journal: "poslužitelj spreman\n"}
	tr, pr := novaTraka(t, l)
	tr.dnevnikCvora()
	if len(pr.otvoreno) != 1 || pr.otvoreno[0] != tr.p.M.DnevnikUsluge() {
		t.Fatalf("otvoreno: %v", pr.otvoreno)
	}
	if b, _ := os.ReadFile(pr.otvoreno[0]); !strings.Contains(string(b), "poslužitelj spreman") {
		t.Errorf("dnevnik: %s", b)
	}
	l.journal, l.greska = "Failed to open journal\n", errors.New("exit status 1")
	tr.dnevnikCvora()
	if len(pr.otvoreno) != 1 || len(pr.poruke) != 1 || !strings.Contains(pr.poruke[0], "Failed to open journal") {
		t.Errorf("journalctl ne radi: otvoreno %v, poruke %v", pr.otvoreno, pr.poruke)
	}

	vlastiti, pr := novaTraka(t, nil)
	vlastiti.dnevnikCvora()
	if len(pr.otvoreno) != 1 || pr.otvoreno[0] != vlastiti.p.M.DnevnikCvora() {
		t.Errorf("dnevnik vlastitog čvora: %v", pr.otvoreno)
	}
}

func TestTekstoviZaUslugu(t *testing.T) {
	for s, zelim := range map[*postava.Stanje]string{
		{Cvor: postava.Radi, Izdanje: "0.0.35-alfa", UslugaSustava: true}: "goCOP 0.0.35-alfa radi (usluga sustava)",
		{Cvor: postava.Zaustavljen, UslugaSustava: true}:                  "goCOP zaustavljen (usluga sustava)",
		{Cvor: postava.Radi, Izdanje: "0.0.35-alfa"}:                      "goCOP 0.0.35-alfa radi",
		{Cvor: postava.Radi, Zauzeto: "Nadogradnja na 0.0.36-alfa"}:       "Nadogradnja na 0.0.36-alfa…",
	} {
		if got := tekstStatusa(*s); got != zelim {
			t.Errorf("%+v: %q, želim %q", *s, got, zelim)
		}
	}

	v, _ := izdanje.ParsirajOznaku("0.0.36-alfa")
	usluga := pitanjeNadogradnje(postava.Stanje{Izdanje: "0.0.35-alfa", UslugaSustava: true, Novije: &postava.Izdanje{Verzija: v}})
	if !strings.HasPrefix(usluga, "Nadograditi goCOP s 0.0.35-alfa na 0.0.36-alfa?") || !strings.Contains(usluga, "lozinka administratora") {
		t.Errorf("pitanje uz uslugu: %q", usluga)
	}
	vlastiti := pitanjeNadogradnje(postava.Stanje{Novije: &postava.Izdanje{Verzija: v}})
	if !strings.Contains(vlastiti, "s trenutnog na 0.0.36-alfa") || !strings.Contains(vlastiti, "vraća se prethodno") {
		t.Errorf("pitanje uz vlastiti čvor: %q", vlastiti)
	}
}

func TestIkonaZaIzbornik(t *testing.T) {
	put := filepath.Join(t.TempDir(), "gocop.png")
	if err := zapisiIkonu(put); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(put)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	slika, err := png.Decode(f)
	if err != nil || slika.Bounds().Dx() != 256 || slika.Bounds().Dy() != 256 {
		t.Errorf("ikona: %v %v", slika.Bounds(), err)
	}
	if err := zapisiIkonu(filepath.Join(t.TempDir(), "nema", "gocop.png")); err == nil {
		t.Error("ikona zapisana u mapu koje nema")
	}
	exe, err := putPostave()
	if err != nil || !filepath.IsAbs(exe) {
		t.Errorf("mjesto programa: %q %v", exe, err)
	}
}

func TestPocetakNePaliZaustavljenuUslugu(t *testing.T) {
	// administrator je uslugu namjerno zaustavio: prijava je ne pali i ne
	// traži lozinku
	l := &lazniSystemd{aktivnost: "inactive"}
	tr, pr := novaTraka(t, l)
	tr.pocetak()
	if l.pozvano("systemctl start") {
		t.Error("pri prijavi pokrenuta zaustavljena usluga")
	}
	if len(pr.poruke) != 0 || len(pr.pitanja) != 0 {
		t.Errorf("pri prijavi uz uslugu: poruke %v, pitanja %v", pr.poruke, pr.pitanja)
	}
}

func TestPocetakPaliVlastitiCvor(t *testing.T) {
	tr, pr := novaTraka(t, nil)
	c := &lazniCvor{}
	tr.p.Cvor = c
	tr.pocetak()
	if c.pokrenut != 1 || len(pr.poruke) != 0 {
		t.Fatalf("vlastiti čvor: pokrenut %d, poruke %v", c.pokrenut, pr.poruke)
	}

	// čvor koji ne krene javlja se porukom
	tr, pr = novaTraka(t, nil)
	tr.p.Cvor = &lazniCvor{neKrece: errors.New("port je zauzet")}
	tr.pocetak()
	if len(pr.poruke) != 1 || !strings.Contains(pr.poruke[0], "port je zauzet") {
		t.Errorf("poruke: %v", pr.poruke)
	}
}
