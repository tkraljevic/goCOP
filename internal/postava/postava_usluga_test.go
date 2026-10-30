package postava

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/izdanje"
)

// objavi stavlja izdanje sa zadanim datotekama, SHA256SUMS svih njih i
// potpisom. podmetni je ime datoteke koja se promijeni nakon potpisa.
func (g *lazniGitHub) objavi(oznaka string, datoteke map[string][]byte, kljuc ed25519.PrivateKey, podmetni string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	imena := make([]string, 0, len(datoteke))
	for n := range datoteke {
		imena = append(imena, n)
	}
	sort.Strings(imena)
	var zbrojevi bytes.Buffer
	for _, n := range imena {
		fmt.Fprintf(&zbrojevi, "%x  %s\n", sha256.Sum256(datoteke[n]), n)
	}
	put := func(n string) string { return "/dl/" + oznaka + "/" + n }
	var popis []ghDatoteka
	for _, n := range imena {
		b := datoteke[n]
		if n == podmetni {
			b = append(append([]byte{}, b...), '#')
		}
		g.datoteke[put(n)] = b
		popis = append(popis, ghDatoteka{Ime: n, URL: g.srv.URL + put(n)})
	}
	g.datoteke[put(izdanje.ImeZbrojeva)] = zbrojevi.Bytes()
	g.datoteke[put(izdanje.ImePotpisa)] = izdanje.Potpisi(kljuc, zbrojevi.Bytes())
	for _, n := range []string{izdanje.ImeZbrojeva, izdanje.ImePotpisa} {
		popis = append(popis, ghDatoteka{Ime: n, URL: g.srv.URL + put(n)})
	}
	g.izdanja = append(g.izdanja, ghIzdanje{Oznaka: oznaka, Stranica: "https://github.com/x/" + oznaka, Datoteke: popis})
}

// datotekeIzdanja su datoteke izdanja za Linux kakve gradi izdanje.yml, za
// sustav na kojem test radi
func datotekeIzdanja(sPostavom bool) map[string][]byte {
	d := map[string][]byte{
		izdanje.ImeDatoteke(runtime.GOOS, runtime.GOARCH): []byte("program"),
		imeSkripte:  []byte("#!/usr/bin/env bash\necho skripta\n"),
		imeJedinice: []byte("[Unit]\n"),
	}
	if sPostavom {
		d[imePostave(runtime.GOOS, runtime.GOARCH)] = []byte("postava")
	}
	return d
}

// izdanjeSProgramom su datoteke izdanja s lažnim čvorom koji na -version
// javlja izd
func izdanjeSProgramom(t *testing.T, izd string, sPostavom bool) map[string][]byte {
	t.Helper()
	d := datotekeIzdanja(sPostavom)
	d[izdanje.ImeDatoteke(runtime.GOOS, runtime.GOARCH)] = prevedi(t, t.TempDir(), izd, "radi")
	return d
}

// postaviProgram stavlja program usluge u privremenu mapu: lažni čvor
// izdanja izd, ili ništa kad je izd prazno
func postaviProgram(t *testing.T, p *Postava, izd string) {
	t.Helper()
	p.M.Program = t.TempDir()
	if izd == "" {
		return
	}
	if err := os.WriteFile(p.M.Gocop(), prevedi(t, t.TempDir(), izd, "radi"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// novaPostava je Postava u privremenoj mapi; usluga kaže ima li na
// „računalu” usluge sustava
func novaPostava(t *testing.T, usluga bool) *Postava {
	t.Helper()
	stara := imaUsluge
	imaUsluge = func() bool { return usluga }
	defer func() { imaUsluge = stara }()
	return Nova(filepath.Join(t.TempDir(), ImeMapePostave, "gocop-postava"), "1.2.0")
}

// postavaUzUslugu je Postava u privremenoj mapi, s lažnim systemd-om
func postavaUzUslugu(t *testing.T, l *lazniSustav, postavke string) *Postava {
	t.Helper()
	p := novaPostava(t, true)
	if p.usluga.Program != "/usr/local/bin/gocop" || p.M.Program != "/usr/local/bin" {
		t.Fatalf("usluga koju je našla Nova: %+v, %+v", p.usluga, p.M)
	}
	u := l.usluga()
	if postavke != "" {
		u.Postavke = postavke
	}
	p.KaoUsluga(u)
	return p
}

func TestPostavaUzUsluguNePokreceVlastitiCvor(t *testing.T) {
	var mu sync.Mutex
	izd := ""
	_, postavke := posluziZdravlje(t, &izd, &mu)
	l := noviSustav("active")
	l.pocetak = time.Now().UTC().Format("Mon 2006-01-02 15:04:05 UTC")
	p := postavaUzUslugu(t, l, postavke)
	if !p.UslugaSustava() || p.M.Program != "/usr/local/bin" || p.M.Gocop() != "/usr/local/bin/gocop" {
		t.Fatalf("mjesta uz uslugu: %+v", p.M)
	}
	if !strings.HasPrefix(p.M.Podaci, filepath.Dir(filepath.Dir(p.Exe))) {
		t.Errorf("datoteke Postave nisu u korisnikovoj mapi: %s", p.M.Podaci)
	}

	ctx := context.Background()
	if s := p.Stanje(); s.Cvor != Pokrece || !s.UslugaSustava {
		t.Errorf("tek pokrenuta usluga: %+v", s)
	}
	mu.Lock()
	izd = "0.0.35-alfa"
	mu.Unlock()
	p.osvjeziZdravlje(ctx)
	if s := p.Stanje(); s.Cvor != Radi || s.Izdanje != "0.0.35-alfa" {
		t.Errorf("usluga koja odgovara: %+v", s)
	}

	mu.Lock()
	izd = ""
	mu.Unlock()
	l.mu.Lock()
	l.pocetak = time.Now().Add(-2 * time.Minute).UTC().Format("Mon 2006-01-02 15:04:05 UTC")
	l.mu.Unlock()
	p.usluga.zaboravi()
	p.osvjeziZdravlje(ctx)
	if s := p.Stanje(); s.Cvor != NeOdgovara {
		t.Errorf("usluga koja dulje ne odgovara: %v", s.Cvor)
	}
	for aktivnost, zelim := range map[string]StanjeCvora{"failed": Pao, "inactive": Zaustavljen} {
		l.mu.Lock()
		l.aktivnost = aktivnost
		l.mu.Unlock()
		p.usluga.zaboravi()
		if s := p.Stanje(); s.Cvor != zelim {
			t.Errorf("%s: %v, želim %v", aktivnost, s.Cvor, zelim)
		}
	}

	// Izlaz iz Postave ne zaustavlja uslugu
	p.Izlaz()
	if n := l.pozvano("systemctl", "stop"); n != 0 {
		t.Errorf("Izlaz je zaustavio uslugu (%d)", n)
	}

	// Dnevnik čvora: zadnji redci dnevnika usluge u mapi Postave
	l.odgovori["journalctl"] = odgovor{out: "poslužitelj spreman\n"}
	put, err := p.DnevnikCvora(ctx)
	if err != nil || put != p.M.DnevnikUsluge() {
		t.Fatalf("dnevnik čvora: %q %v", put, err)
	}
	if b, _ := os.ReadFile(put); !strings.Contains(string(b), "poslužitelj spreman") {
		t.Errorf("dnevnik usluge: %s", b)
	}
}

func TestPrviPutUzUsluguUkljucujePriPrijaviJednom(t *testing.T) {
	stari := ukljuciPriPrijavi
	defer func() { ukljuciPriPrijavi = stari }()
	var pozvano []string
	greska := errors.New("nema mape autostart")
	ukljuciPriPrijavi = func(exe string) error {
		pozvano = append(pozvano, exe)
		return greska
	}

	p := postavaUzUslugu(t, noviSustav("active"), "")
	p.PrviPutUzUslugu()
	if _, err := os.Stat(p.M.OznakaPriPrijavi()); err == nil {
		t.Error("oznaka zapisana, a pokretanje pri prijavi nije uključeno")
	}
	greska = nil
	p.PrviPutUzUslugu()
	p.PrviPutUzUslugu() // korisnik je u međuvremenu možda isključio: ne dira se
	if !reflect.DeepEqual(pozvano, []string{p.Exe, p.Exe}) {
		t.Errorf("pokretanje pri prijavi uključivano: %v", pozvano)
	}
	if _, err := os.Stat(p.M.OznakaPriPrijavi()); err != nil {
		t.Errorf("oznaka: %v", err)
	}

	// vlastiti čvor: Postavu u prijavu stavlja instalacija, ne ovo
	pozvano = nil
	vlastiti := novaPostava(t, false)
	vlastiti.PrviPutUzUslugu()
	if len(pozvano) != 0 {
		t.Errorf("bez usluge uključeno pokretanje pri prijavi: %v", pozvano)
	}
}

// lazniCvor bilježi što je Postava tražila od vlastitog čvora; nema znači
// da čvor još nije instaliran
type lazniCvor struct {
	zaustavljen int
	nema        bool
}

func (c *lazniCvor) Instaliran() bool                       { return !c.nema }
func (c *lazniCvor) Pokreni() error                         { return nil }
func (c *lazniCvor) Zaustavi(time.Duration) error           { c.zaustavljen++; return nil }
func (c *lazniCvor) Proces() StanjeProcesa                  { return StanjeProcesa{Radi: c.zaustavljen == 0} }
func (c *lazniCvor) Zdravlje(context.Context) (string, int) { return "", 0 }
func (c *lazniCvor) NaPromjenu(func())                      {}

func TestIzlazGasiSamoVlastitiCvor(t *testing.T) {
	p := novaPostava(t, false)
	c := &lazniCvor{}
	p.Cvor = c
	p.Izlaz()
	if c.zaustavljen != 1 {
		t.Errorf("vlastiti čvor zaustavljen %d puta", c.zaustavljen)
	}
	if put, err := p.DnevnikCvora(context.Background()); err != nil || put != p.M.DnevnikCvora() {
		t.Errorf("dnevnik vlastitog čvora: %q %v", put, err)
	}
	if s := p.Stanje(); s.UslugaSustava {
		t.Error("vlastiti čvor javljen kao usluga")
	}
}

func TestUzUsluguNemaPonudeNovePostave(t *testing.T) {
	g := noviGitHub(t)
	g.mu.Lock()
	g.izdanja = append(g.izdanja, ghIzdanje{Oznaka: "postava-v9.0.0", Stranica: "https://github.com/x/postava-v9.0.0"})
	g.mu.Unlock()

	p := postavaUzUslugu(t, noviSustav("active"), "")
	p.Ugradnja.Izdanja = g.izdanjaZa()
	if err := p.Provjeri(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := p.Stanje(); s.NovijaPostava != "" || s.StranicaPostave != "" {
		t.Errorf("uz uslugu ponuđena Postava za Windows: %q", s.NovijaPostava)
	}
}

func TestNadogradnjaUsluge(t *testing.T) {
	_, kljuc, _ := ed25519.GenerateKey(rand.Reader)
	javni := kljuc.Public().(ed25519.PublicKey)
	var mu sync.Mutex
	izd := "0.0.35-alfa"
	_, postavke := posluziZdravlje(t, &izd, &mu)
	l := noviSustav("active")
	p := postavaUzUslugu(t, l, postavke)
	postaviProgram(t, p, "0.0.35-alfa")
	p.Ugradnja.Kljucevi = []ed25519.PublicKey{javni}
	g := noviGitHub(t)
	p.Ugradnja.Izdanja = g.izdanjaZa()
	ctx := context.Background()
	program := izdanje.ImeDatoteke(runtime.GOOS, runtime.GOARCH)

	// pkexec: provjeri što je pripremljeno i „nadogradi” lažni čvor
	var naredba, uMapiImena []string
	var uMapi map[string]string
	l.pkexec = func(arg []string) ([]byte, error) {
		naredba = arg
		uMapi = map[string]string{}
		uMapiImena = nil
		stavke, _ := os.ReadDir(arg[4]) // prazna mapa ispadne u usporedbi niže
		for _, s := range stavke {
			b, _ := os.ReadFile(filepath.Join(arg[4], s.Name())) // nepročitano ispadne u usporedbi niže
			uMapi[s.Name()] = string(b)
			uMapiImena = append(uMapiImena, s.Name())
		}
		mu.Lock()
		izd = "0.0.36-alfa"
		mu.Unlock()
		return []byte("→ provjeravam potpis izdanja\ngoCOP 0.0.36-alfa radi kao usluga gocop\n"), nil
	}

	zelim := izdanjeSProgramom(t, "0.0.36-alfa", true)
	g.objavi("v0.0.36-alfa", zelim, kljuc, "")
	if err := p.Provjeri(ctx); err != nil {
		t.Fatal(err)
	}
	s := p.Stanje()
	if s.Novije == nil || s.Novije.Program.Ime != program {
		t.Fatalf("ponuda uz Postavu u izdanju: %+v", s.Novije)
	}
	novo, err := p.Nadogradi(ctx)
	if err != nil || novo != "0.0.36-alfa" {
		t.Fatalf("nadogradnja: %q %v", novo, err)
	}
	// bash -c prijenosKaoRoot ime mapa zbroj-SHA256SUMS zbroj-skripte program datoteke...
	if len(naredba) < 8 || naredba[0] != "/usr/bin/bash" || naredba[1] != "-c" || naredba[2] != prijenosKaoRoot ||
		!strings.HasPrefix(naredba[4], p.M.Podaci) || naredba[7] != program {
		t.Fatalf("pkexec %v", naredba)
	}
	if naredba[5] != fmt.Sprintf("%x", sha256.Sum256([]byte(uMapi[izdanje.ImeZbrojeva]))) ||
		naredba[6] != fmt.Sprintf("%x", sha256.Sum256(zelim[imeSkripte])) {
		t.Errorf("zbrojevi za provjeru kao root: %v", naredba[5:7])
	}
	dat := append([]string{}, naredba[8:]...)
	sort.Strings(dat)
	if !reflect.DeepEqual(dat, uMapiImena) {
		t.Errorf("datoteke za kopiju: %v, u mapi %v", naredba[8:], uMapiImena)
	}
	for n, b := range zelim {
		if uMapi[n] != string(b) {
			t.Errorf("u mapi za skriptu %s = %q", n, uMapi[n])
		}
	}
	if uMapi[izdanje.ImeZbrojeva] == "" || uMapi[izdanje.ImePotpisa] == "" || len(uMapi) != len(zelim)+2 {
		t.Errorf("u mapi za skriptu: %d datoteka", len(uMapi))
	}
	if _, err := os.Stat(naredba[4]); !os.IsNotExist(err) {
		t.Error("mapa s preuzetim izdanjem ostala je nakon nadogradnje")
	}
	if b, _ := os.ReadFile(p.M.DnevnikPostave()); !strings.Contains(string(b), "radi kao usluga gocop") {
		t.Errorf("ispis skripte nije u dnevniku Postave:\n%s", b)
	}

	// odustajanje od lozinke i usluga koja nakon skripte ne radi
	g.objavi("v0.0.37-alfa", izdanjeSProgramom(t, "0.0.37-alfa", false), kljuc, "")
	_ = p.Provjeri(ctx)
	l.pkexec = func([]string) ([]byte, error) { return nil, izlazGreska(126) }
	if _, err := p.Nadogradi(ctx); err == nil || !strings.Contains(err.Error(), "lozinka administratora nije upisana") {
		t.Errorf("bez lozinke: %v", err)
	}
	l.pkexec = func([]string) ([]byte, error) {
		l.mu.Lock()
		l.aktivnost = "failed"
		l.mu.Unlock()
		return nil, nil
	}
	if _, err := p.Nadogradi(ctx); err == nil || !strings.Contains(err.Error(), "ne javlja izdanje 0.0.37-alfa") {
		t.Errorf("usluga ne radi nakon skripte: %v", err)
	}
	l.mu.Lock()
	l.aktivnost = "active"
	l.mu.Unlock()

	// izdanje bez Postave: skripta dobiva samo čvor
	l.pkexec = func(arg []string) ([]byte, error) {
		_, err := os.Stat(filepath.Join(arg[4], imePostave(runtime.GOOS, runtime.GOARCH)))
		uMapi = map[string]string{"postava": fmt.Sprint(err == nil || slices.Contains(arg[8:], imePostave(runtime.GOOS, runtime.GOARCH)))}
		mu.Lock()
		izd = "0.0.37-alfa"
		mu.Unlock()
		return nil, nil
	}
	_ = p.Provjeri(ctx)
	if novo, err := p.Nadogradi(ctx); err != nil || novo != "0.0.37-alfa" || uMapi["postava"] != "false" {
		t.Errorf("izdanje bez Postave: %q %v %v", novo, err, uMapi)
	}
}

func TestNadogradnjaUslugeOdbijaPodmetnuto(t *testing.T) {
	_, kljuc, _ := ed25519.GenerateKey(rand.Reader)
	_, tudji, _ := ed25519.GenerateKey(rand.Reader)
	l := noviSustav("active")
	p := postavaUzUslugu(t, l, "")
	postaviProgram(t, p, "0.0.35-alfa")
	p.Ugradnja.Kljucevi = []ed25519.PublicKey{kljuc.Public().(ed25519.PublicKey)}
	g := noviGitHub(t)
	p.Ugradnja.Izdanja = g.izdanjaZa()
	ctx := context.Background()
	pokusaj := func() error {
		t.Helper()
		if err := p.Provjeri(ctx); err != nil {
			t.Fatal(err)
		}
		_, err := p.Nadogradi(ctx)
		g.mu.Lock()
		g.izdanja = nil
		g.mu.Unlock()
		return err
	}

	for ime := range datotekeIzdanja(true) {
		g.objavi("v0.0.36-alfa", datotekeIzdanja(true), kljuc, ime)
		if err := pokusaj(); err == nil || !strings.Contains(err.Error(), ime+" ne odgovara potpisanom SHA-256") {
			t.Errorf("podmetnut %s: %v", ime, err)
		}
	}
	g.objavi("v0.0.36-alfa", datotekeIzdanja(true), tudji, "")
	if err := pokusaj(); err == nil || !strings.Contains(err.Error(), "potpis") {
		t.Errorf("tuđi ključ: %v", err)
	}
	g.objavi("v0.0.36-alfa", datotekeIzdanja(true), kljuc, "")
	g.mu.Lock()
	var bezJedinice []ghDatoteka
	for _, d := range g.izdanja[0].Datoteke {
		if d.Ime != imeJedinice {
			bezJedinice = append(bezJedinice, d)
		}
	}
	g.izdanja[0].Datoteke = bezJedinice
	g.mu.Unlock()
	if err := pokusaj(); err == nil || !strings.Contains(err.Error(), "nema datoteke gocop.service") ||
		!strings.Contains(err.Error(), "docs/linux.md") {
		t.Errorf("izdanje bez jedinice: %v", err)
	}
	if n := l.pozvano("/usr/bin/pkexec"); n != 0 {
		t.Errorf("skripta pokrenuta uz neispravno izdanje (%d)", n)
	}
	if ostalo, _ := filepath.Glob(filepath.Join(p.M.Podaci, "nadogradnja-*")); len(ostalo) != 0 {
		t.Errorf("ostale mape: %v", ostalo)
	}
}

func TestPripremaZaUsluguBezMreze(t *testing.T) {
	g := noviGitHub(t)
	u := Ugradnja{M: MjestaU(t.TempDir()), Izdanja: g.izdanjaZa(), GOOS: "linux", GOARCH: "amd64"}
	ctx := context.Background()
	if _, err := u.PripremiZaUslugu(ctx, Izdanje{Oznaka: "v1", Zbrojevi: ghDatoteka{URL: g.srv.URL + "/nema"}}); err == nil {
		t.Error("bez SHA256SUMS")
	}
	g.mu.Lock()
	g.datoteke["/z"] = []byte("x")
	g.mu.Unlock()
	if _, err := u.PripremiZaUslugu(ctx, Izdanje{Oznaka: "v1", Zbrojevi: ghDatoteka{URL: g.srv.URL + "/z"},
		Potpis: ghDatoteka{URL: g.srv.URL + "/nema"}}); err == nil {
		t.Error("bez potpisa")
	}
	if err := u.preuzmiZaUslugu(ctx, Izdanje{}, nil, nil, filepath.Join(t.TempDir(), "nema")); err == nil {
		t.Error("mapa ne postoji")
	}
	if got := u.datotekeZaUslugu([]byte(fmt.Sprintf("%064x  gocop-postava-linux-amd64\n", 1))); !reflect.DeepEqual(got,
		[]string{"gocop-linux-amd64", "instaliraj.sh", "gocop.service", "gocop-postava-linux-amd64"}) {
		t.Errorf("datoteke za uslugu: %v", got)
	}
	if err := u.preuzmiIzIzdanja(ctx, Izdanje{Datoteke: []ghDatoteka{{Ime: "instaliraj.sh"}}}, nil, t.TempDir(), "instaliraj.sh"); err == nil {
		t.Error("datoteka koje nema u SHA256SUMS")
	}
	if err := u.preuzmiProvjereno(ctx, "", filepath.Join(t.TempDir(), "nema", "x"), ""); err == nil {
		t.Error("datoteka se ne može zapisati")
	}
	// Postava bez mape podataka ne može pripremiti izdanje (mapa nije stvorena)
	_, kljuc, _ := ed25519.GenerateKey(rand.Reader)
	g.objavi("v0.0.36-alfa", datotekeIzdanja(true), kljuc, "")
	u.Kljucevi = []ed25519.PublicKey{kljuc.Public().(ed25519.PublicKey)}
	ponuda, _ := g.izdanjaZa().Provjeri(ctx, runtime.GOOS, runtime.GOARCH, "1.2.0")
	if _, err := u.PripremiZaUslugu(ctx, *ponuda.GoCOP); err == nil {
		t.Error("mapa za preuzimanje stvorena bez mape podataka")
	}
}

func TestPkexecZaNadogradnjuUsluge(t *testing.T) {
	l := noviSustav("active")
	u := l.usluga()
	ctx := context.Background()
	mapa := t.TempDir()
	var dnevnik strings.Builder
	pisi := func(f string, a ...any) { fmt.Fprintf(&dnevnik, f+"\n", a...) }

	z := ZaUslugu{Mapa: mapa, Program: "gocop-linux-amd64", Datoteke: []string{"SHA256SUMS", "instaliraj.sh", "gocop-linux-amd64"},
		Zbrojevi: strings.Repeat("a", 64), Skripta: strings.Repeat("b", 64)}

	if err := u.Ugradi(ctx, z, pisi); err != nil {
		t.Fatal(err)
	}
	zelim := []string{"/usr/bin/pkexec", "/usr/bin/bash", "-c", prijenosKaoRoot, "gocop-nadogradnja", mapa,
		strings.Repeat("a", 64), strings.Repeat("b", 64), "gocop-linux-amd64", "SHA256SUMS", "instaliraj.sh", "gocop-linux-amd64"}
	if l.pozvano(zelim...) != 1 {
		t.Errorf("pozivi: %v, želim %v", l.pozivi, zelim)
	}

	for izlaz, poruka := range map[error]string{
		izlazGreska(126):                 "lozinka administratora nije upisana",
		izlazGreska(127):                 "nije mogao otvoriti prozor za lozinku",
		izlazGreska(1):                   "Greška: potpis SHA256SUMS ne odgovara",
		errors.New("fork/exec: nema ga"): "pkexec nije pokrenut",
	} {
		l.odgovori["/usr/bin/pkexec"] = odgovor{"→ provjeravam potpis izdanja\nGreška: potpis SHA256SUMS ne odgovara ključu izdanja\n", izlaz}
		err := u.Ugradi(ctx, z, pisi)
		if err == nil || !strings.Contains(err.Error(), poruka) {
			t.Errorf("%v: %v", izlaz, err)
		}
		if izlaz != izlazGreska(1) && !strings.Contains(err.Error(), "docs/linux.md, poglavlje 9") {
			t.Errorf("%v bez upute za ručnu nadogradnju: %v", izlaz, err)
		}
	}
	if !strings.Contains(dnevnik.String(), "Greška: potpis SHA256SUMS") {
		t.Errorf("ispis skripte nije u dnevniku: %s", dnevnik.String())
	}

	// bez SHA-256 za provjeru kopije ništa se ne pokreće
	for _, krivo := range []ZaUslugu{{Zbrojevi: z.Zbrojevi}, {Zbrojevi: "a", Skripta: z.Skripta}, {Zbrojevi: z.Zbrojevi, Skripta: strings.Repeat("g", 64)}} {
		krivo.Mapa, krivo.Program, krivo.Datoteke = z.Mapa, z.Program, z.Datoteke
		prije := len(l.pozivi)
		if err := u.Ugradi(ctx, krivo, pisi); err == nil || !strings.Contains(err.Error(), "SHA-256") || len(l.pozivi) != prije {
			t.Errorf("bez SHA-256 %+v: %v", krivo, err)
		}
	}

	delete(l.programi, "bash")
	if err := u.Ugradi(ctx, z, pisi); err == nil || !strings.Contains(err.Error(), "bash") {
		t.Errorf("bez bash-a: %v", err)
	}
	delete(l.programi, "pkexec")
	if err := u.Ugradi(ctx, z, pisi); err == nil || !strings.Contains(err.Error(), "pkexec") ||
		!strings.Contains(err.Error(), "docs/linux.md, poglavlje 9") {
		t.Errorf("bez pkexec-a: %v", err)
	}
}

func TestNadogradnjaUslugeProvjeravaIzdanjePrograma(t *testing.T) {
	_, kljuc, _ := ed25519.GenerateKey(rand.Reader) // rand.Reader ne javlja grešku
	var mu sync.Mutex
	izd := "0.0.35-alfa"
	_, postavke := posluziZdravlje(t, &izd, &mu)
	l := noviSustav("active")
	p := postavaUzUslugu(t, l, postavke)
	postaviProgram(t, p, "0.0.35-alfa")
	p.Ugradnja.Kljucevi = []ed25519.PublicKey{kljuc.Public().(ed25519.PublicKey)}
	g := noviGitHub(t)
	p.Ugradnja.Izdanja = g.izdanjaZa()
	ctx := context.Background()

	// oznaka 0.0.36-alfa, a program javlja 0.0.30-alfa
	g.objavi("v0.0.36-alfa", izdanjeSProgramom(t, "0.0.30-alfa", true), kljuc, "")
	if err := p.Provjeri(ctx); err != nil {
		t.Fatal(err)
	}
	if p.Stanje().Novije == nil {
		t.Fatal("izdanje nije ponuđeno")
	}
	_, err := p.Nadogradi(ctx)
	if err == nil || !strings.Contains(err.Error(), "javlja izdanje 0.0.30-alfa") {
		t.Errorf("program drugog izdanja: %v", err)
	}
	if n := l.pozvano("/usr/bin/pkexec"); n != 0 {
		t.Errorf("skripta pokrenuta uz program drugog izdanja (%d)", n)
	}
	ostalo, _ := filepath.Glob(filepath.Join(p.M.Podaci, "nadogradnja-*")) // uzorak je ispravan
	if len(ostalo) != 0 {
		t.Errorf("ostale mape: %v", ostalo)
	}
}

func TestNadogradnjaUslugeBezPoznatogIzdanja(t *testing.T) {
	_, kljuc, _ := ed25519.GenerateKey(rand.Reader) // rand.Reader ne javlja grešku
	l := noviSustav("active")
	p := postavaUzUslugu(t, l, "") // usluga ne odgovara
	postaviProgram(t, p, "")       // i programa nema
	p.Ugradnja.Kljucevi = []ed25519.PublicKey{kljuc.Public().(ed25519.PublicKey)}
	g := noviGitHub(t)
	p.Ugradnja.Izdanja = g.izdanjaZa()
	ctx := context.Background()
	g.objavi("v0.0.36-alfa", datotekeIzdanja(true), kljuc, "")
	if err := p.Provjeri(ctx); err != nil {
		t.Fatal(err)
	}
	if s := p.Stanje(); s.Novije != nil {
		t.Errorf("ponuđeno izdanje, a ne zna se koje je postavljeno: %+v", s.Novije)
	}
	if _, err := p.Nadogradi(ctx); err == nil {
		t.Error("nadogradnja bez poznatog izdanja")
	}
	// i kad se ponuda zatekne, Nadogradi ponovno pita i odbija
	_, err := p.dopustenaNadogradnja(ctx, *p.ponuda.GoCOP, "")
	if err == nil || !strings.Contains(err.Error(), "ne zna se koje je izdanje") || !strings.Contains(err.Error(), "docs/linux.md") {
		t.Errorf("bez poznatog izdanja: %v", err)
	}
	if n := l.pozvano("/usr/bin/pkexec"); n != 0 {
		t.Errorf("skripta pokrenuta bez poznatog izdanja (%d)", n)
	}
}

// Usluga ne odgovara, a rootov program javlja svoje izdanje: novije se
// postavlja
func TestNadogradnjaUslugeKadUslugaNeOdgovara(t *testing.T) {
	_, kljuc, _ := ed25519.GenerateKey(rand.Reader) // rand.Reader ne javlja grešku
	var mu sync.Mutex
	izd := ""
	_, postavke := posluziZdravlje(t, &izd, &mu)
	l := noviSustav("active")
	p := postavaUzUslugu(t, l, postavke)
	postaviProgram(t, p, "0.0.35-alfa")
	p.Ugradnja.Kljucevi = []ed25519.PublicKey{kljuc.Public().(ed25519.PublicKey)}
	g := noviGitHub(t)
	p.Ugradnja.Izdanja = g.izdanjaZa()
	ctx := context.Background()
	l.pkexec = func([]string) ([]byte, error) {
		mu.Lock()
		izd = "0.0.36-alfa"
		mu.Unlock()
		return nil, nil
	}
	g.objavi("v0.0.36-alfa", izdanjeSProgramom(t, "0.0.36-alfa", false), kljuc, "")
	if err := p.Provjeri(ctx); err != nil {
		t.Fatal(err)
	}
	p.osvjeziZdravlje(ctx)
	if s := p.Stanje(); s.Izdanje != "" || s.Novije == nil {
		t.Fatalf("usluga ne odgovara, program 0.0.35-alfa: %+v", s)
	}
	if novo, err := p.Nadogradi(ctx); err != nil || novo != "0.0.36-alfa" {
		t.Fatalf("nadogradnja: %q %v", novo, err)
	}
	if n := l.pozvano("/usr/bin/pkexec"); n != 1 {
		t.Errorf("skripta pokrenuta %d puta", n)
	}
}

// Na /zdravlje javlja se niže izdanje od onoga koje javlja rootov program:
// vrijedi više, pa se starije izdanje ne nudi ni ne postavlja
func TestNadogradnjaUslugePremaVecemIzdanju(t *testing.T) {
	_, kljuc, _ := ed25519.GenerateKey(rand.Reader) // rand.Reader ne javlja grešku
	var mu sync.Mutex
	izd := "0.0.1-alfa"
	_, postavke := posluziZdravlje(t, &izd, &mu)
	l := noviSustav("active")
	p := postavaUzUslugu(t, l, postavke)
	postaviProgram(t, p, "0.0.40-alfa")
	p.Ugradnja.Kljucevi = []ed25519.PublicKey{kljuc.Public().(ed25519.PublicKey)}
	g := noviGitHub(t)
	p.Ugradnja.Izdanja = g.izdanjaZa()
	ctx := context.Background()
	g.objavi("v0.0.36-alfa", izdanjeSProgramom(t, "0.0.36-alfa", false), kljuc, "")
	if err := p.Provjeri(ctx); err != nil {
		t.Fatal(err)
	}
	p.osvjeziZdravlje(ctx)
	if s := p.Stanje(); s.Izdanje != "0.0.1-alfa" || s.Novije != nil {
		t.Errorf("ponuđeno starije izdanje: %+v", s.Novije)
	}
	if _, err := p.Nadogradi(ctx); err == nil {
		t.Error("postavljeno starije izdanje")
	}
	if _, err := p.dopustenaNadogradnja(ctx, *p.ponuda.GoCOP, "0.0.1-alfa"); err == nil ||
		!strings.Contains(err.Error(), "nije novije od postavljenog 0.0.40-alfa") {
		t.Errorf("starije od rootova programa: %v", err)
	}
	if n := l.pozvano("/usr/bin/pkexec"); n != 0 {
		t.Errorf("skripta pokrenuta za starije izdanje (%d)", n)
	}
}

// Vlastiti čvor (Windows, macOS): isto pravilo, nikad starije ni nepoznato
func TestNadogradnjaVlastitogCvoraSamoNovije(t *testing.T) {
	_, kljuc, _ := ed25519.GenerateKey(rand.Reader) // rand.Reader ne javlja grešku
	p := novaPostava(t, false)
	c := &lazniCvor{}
	p.Cvor, p.Ugradnja.Cvor = c, c
	postaviProgram(t, p, "") // program ne javlja izdanje
	p.Ugradnja.M = p.M
	p.Ugradnja.Kljucevi = []ed25519.PublicKey{kljuc.Public().(ed25519.PublicKey)}
	g := noviGitHub(t)
	p.Ugradnja.Izdanja = g.izdanjaZa()
	ctx := context.Background()
	g.dodaj("v0.0.30-alfa", prevedi(t, t.TempDir(), "0.0.30-alfa", "radi"), kljuc, false)
	if err := p.Provjeri(ctx); err != nil {
		t.Fatal(err)
	}
	if s := p.Stanje(); s.Novije != nil {
		t.Errorf("ponuđeno uz nepoznato postavljeno izdanje: %+v", s.Novije)
	}
	if _, err := p.Nadogradi(ctx); err == nil {
		t.Error("nadograđeno uz nepoznato postavljeno izdanje")
	}
	iz := *p.ponuda.GoCOP
	if _, err := p.dopustenaNadogradnja(ctx, iz, ""); err == nil ||
		!strings.Contains(err.Error(), "ne zna se") || strings.Contains(err.Error(), "linux") {
		t.Errorf("nepoznato izdanje vlastitog čvora: %v", err)
	}
	if _, err := p.dopustenaNadogradnja(ctx, iz, "0.0.31-alfa"); err == nil {
		t.Error("prihvaćeno starije izdanje")
	}
	if c.zaustavljen != 0 {
		t.Errorf("čvor zaustavljen %d puta", c.zaustavljen)
	}

	// ispravna ponuda: postavljeno je 0.0.27-alfa
	postaviProgram(t, p, "0.0.27-alfa")
	if s := p.Stanje(); s.Novije == nil || s.Novije.Oznaka != "v0.0.30-alfa" {
		t.Errorf("novije izdanje nije ponuđeno: %+v", s.Novije)
	}
	if staro, err := p.dopustenaNadogradnja(ctx, iz, ""); err != nil || staro != "0.0.27-alfa" {
		t.Errorf("novije izdanje: %q %v", staro, err)
	}
	// prva instalacija: nema s čim usporediti
	c.nema = true
	postaviProgram(t, p, "")
	if s := p.Stanje(); s.Cvor != NijeInstaliran || s.Novije == nil {
		t.Errorf("prva instalacija, ponuda: %v %+v", s.Cvor, s.Novije)
	}
	if staro, err := p.dopustenaNadogradnja(ctx, iz, ""); err != nil || staro != "" {
		t.Errorf("prva instalacija: %q %v", staro, err)
	}
}

func TestVeceIzdanje(t *testing.T) {
	for _, s := range [][3]string{
		{"0.0.35-alfa", "0.0.40-alfa", "0.0.40-alfa"},
		{"0.0.40-alfa", "0.0.35-alfa", "0.0.40-alfa"},
		{"", "0.0.35-alfa", "0.0.35-alfa"},
		{"0.0.35-alfa", "", "0.0.35-alfa"},
		{"0.0.35-alfa", "nepoznato", "0.0.35-alfa"},
		{"", "", ""},
	} {
		if got := veceIzdanje(s[0], s[1]); got != s[2] {
			t.Errorf("veceIzdanje(%q, %q) = %q, želim %q", s[0], s[1], got, s[2])
		}
	}
}

// Program koji nije odgovorio pita se ponovno nakon minute, i kad se
// datoteka nije promijenila
func TestIzdanjeProgramaPonovnoNakonMinute(t *testing.T) {
	p := novaPostava(t, false)
	p.M.Program = t.TempDir()
	kad := time.Now().Add(-time.Hour)
	postavi := func(b []byte) {
		t.Helper()
		if err := os.WriteFile(p.M.Gocop(), b, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p.M.Gocop(), kad, kad); err != nil {
			t.Fatal(err)
		}
	}
	postavi([]byte("nije program"))
	p.mu.Lock()
	defer p.mu.Unlock()
	if izd := p.izdanjeDatotekeZakljucano(); izd != "" {
		t.Fatalf("neispravan program: %q", izd)
	}
	postavi(prevedi(t, t.TempDir(), "0.0.35-alfa", "radi"))
	if izd := p.izdanjeDatotekeZakljucano(); izd != "" {
		t.Errorf("unutar minute pitano ponovno: %q", izd)
	}
	p.izdDatotekePitano = time.Now().Add(-2 * time.Minute)
	if izd := p.izdanjeDatotekeZakljucano(); izd != "0.0.35-alfa" {
		t.Errorf("nakon minute: %q", izd)
	}
}

// Preuzeti program koji se ne da pokrenuti: greška nosi uputu za ručnu
// nadogradnju, a program drugog izdanja ne
func TestPripremaZaUsluguProgramSeNePokrece(t *testing.T) {
	_, kljuc, _ := ed25519.GenerateKey(rand.Reader) // rand.Reader ne javlja grešku
	g := noviGitHub(t)
	u := Ugradnja{M: MjestaU(t.TempDir()), Izdanja: g.izdanjaZa(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Kljucevi: []ed25519.PublicKey{kljuc.Public().(ed25519.PublicKey)}}
	if err := os.MkdirAll(u.M.Podaci, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	g.objavi("v0.0.36-alfa", datotekeIzdanja(false), kljuc, "") // program je tekst
	ponuda, err := g.izdanjaZa().Provjeri(ctx, runtime.GOOS, runtime.GOARCH, "1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = u.PripremiZaUslugu(ctx, *ponuda.GoCOP)
	if err == nil || !strings.Contains(err.Error(), "-version") || !strings.Contains(err.Error(), "docs/linux.md, poglavlje 9") {
		t.Errorf("program se ne pokreće: %v", err)
	}
	if err := u.provjeriZaUslugu(ctx, filepath.Join(t.TempDir(), "x"), "0.0.36-alfa"); err == nil {
		t.Error("program kojeg nema")
	}
	put := filepath.Join(t.TempDir(), "gocop")
	if err := os.WriteFile(put, prevedi(t, t.TempDir(), "0.0.30-alfa", "radi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := u.provjeriZaUslugu(ctx, put, "0.0.36-alfa"); err == nil || strings.Contains(err.Error(), "docs/linux.md") {
		t.Errorf("program drugog izdanja: %v", err)
	}
}

func TestNovijeOd(t *testing.T) {
	novo, _ := izdanje.ParsirajOznaku("0.0.36-alfa")
	for trenutno, prolazi := range map[string]bool{
		"0.0.35-alfa": true,
		"0.0.36-alfa": false,
		"0.0.40-alfa": false,
		"0.1.0-beta":  false,
		"":            false,
		"nepoznato":   false,
	} {
		if err := novijeOd(novo, trenutno); (err == nil) != prolazi {
			t.Errorf("%q: %v", trenutno, err)
		}
	}
}

// TestPrijenosKaoRoot pokreće pravi bash: skripta radi iz kopije, a ne iz
// mape koju je pripremila Postava, i ne pokreće se kad se datoteka nakon
// provjere promijeni
func TestPrijenosKaoRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("samo uz bash")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("nema bash-a")
	}
	for _, n := range []string{"sha256sum", "mktemp", "cp"} {
		if _, err := exec.LookPath(n); err != nil {
			t.Skipf("nema %s", n)
		}
	}
	l := noviSustav("active")
	l.programi["bash"] = bash
	l.pkexec = func(arg []string) ([]byte, error) { return exec.Command(arg[0], arg[1:]...).CombinedOutput() }
	u := l.usluga()
	ctx := context.Background()
	var ispis string
	pisi := func(f string, a ...any) { ispis = fmt.Sprintf(f, a...) }

	skripta := []byte("#!/usr/bin/env bash\necho \"mapa=$(cd \"$(dirname \"$0\")\" && pwd)\"\necho \"program=$1\"\ncat \"$(dirname \"$0\")/SHA256SUMS\"\n")
	pripremi := func() ZaUslugu {
		t.Helper()
		mapa := t.TempDir()
		datoteke := map[string][]byte{izdanje.ImeZbrojeva: []byte("zbrojevi izdanja\n"), izdanje.ImePotpisa: []byte("potpis"),
			imeSkripte: skripta, imeJedinice: []byte("[Unit]\n"), "gocop-linux-amd64": []byte("program")}
		var imena []string
		for n, b := range datoteke {
			if err := os.WriteFile(filepath.Join(mapa, n), b, 0o644); err != nil {
				t.Fatal(err)
			}
			imena = append(imena, n)
		}
		sort.Strings(imena)
		return ZaUslugu{Mapa: mapa, Program: "gocop-linux-amd64", Datoteke: imena,
			Zbrojevi: fmt.Sprintf("%x", sha256.Sum256(datoteke[izdanje.ImeZbrojeva])), Skripta: fmt.Sprintf("%x", sha256.Sum256(skripta))}
	}

	// ispravno izdanje: skripta radi iz kopije, kopija se nakon toga briše
	z := pripremi()
	if err := u.Ugradi(ctx, z, pisi); err != nil {
		t.Fatalf("%v\n%s", err, ispis)
	}
	kopija, program := "", ""
	for _, r := range strings.Split(ispis, "\n") {
		if v, ok := strings.CutPrefix(r, "mapa="); ok {
			kopija = v
		}
		if v, ok := strings.CutPrefix(r, "program="); ok {
			program = v
		}
	}
	if kopija == "" || kopija == z.Mapa || !strings.Contains(kopija, "gocop-nadogradnja.") ||
		program != kopija+"/gocop-linux-amd64" || !strings.Contains(ispis, "zbrojevi izdanja") {
		t.Fatalf("skripta nije radila iz kopije:\n%s", ispis)
	}
	if _, err := os.Stat(kopija); !os.IsNotExist(err) {
		t.Errorf("kopija ostala: %v", err)
	}

	// datoteke promijenjene nakon provjere: skripta se ne pokreće
	trag := filepath.Join(t.TempDir(), "trag")
	for ime, sadrzaj := range map[string]string{
		imeSkripte:          "#!/usr/bin/env bash\ntouch " + trag + "\n",
		izdanje.ImeZbrojeva: "drugi zbrojevi\n",
	} {
		z := pripremi()
		if err := os.WriteFile(filepath.Join(z.Mapa, ime), []byte(sadrzaj), 0o644); err != nil {
			t.Fatal(err)
		}
		err := u.Ugradi(ctx, z, pisi)
		if err == nil || !strings.Contains(err.Error(), "promijenile su se nakon provjere") {
			t.Errorf("promijenjen %s: %v", ime, err)
		}
		if _, err := os.Stat(trag); err == nil {
			t.Fatalf("promijenjen %s: pokrenuta je promijenjena skripta", ime)
		}
	}

	// datoteke nema: ništa se ne pokreće
	z = pripremi()
	if err := os.Remove(filepath.Join(z.Mapa, imeJedinice)); err != nil {
		t.Fatal(err)
	}
	if err := u.Ugradi(ctx, z, pisi); err == nil || strings.Contains(ispis, "mapa=") {
		t.Errorf("bez jedinice: %v\n%s", err, ispis)
	}

	// poveznica umjesto datoteke: ne kopira se, ništa se ne pokreće
	z = pripremi()
	program = filepath.Join(z.Mapa, "gocop-linux-amd64")
	if err := os.Remove(program); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(z.Mapa, imeJedinice), program); err != nil {
		t.Fatal(err)
	}
	if err := u.Ugradi(ctx, z, pisi); err == nil || !strings.Contains(err.Error(), "nije obična datoteka") || strings.Contains(ispis, "mapa=") {
		t.Errorf("poveznica: %v\n%s", err, ispis)
	}

	// cijev umjesto datoteke: ne čeka se na nju
	if mkfifo, err := exec.LookPath("mkfifo"); err == nil {
		z = pripremi()
		program = filepath.Join(z.Mapa, "gocop-linux-amd64")
		if err := os.Remove(program); err != nil {
			t.Fatal(err)
		}
		if b, err := exec.Command(mkfifo, program).CombinedOutput(); err != nil {
			t.Fatalf("mkfifo: %v %s", err, b)
		}
		if err := u.Ugradi(ctx, z, pisi); err == nil || !strings.Contains(err.Error(), "nije obična datoteka") {
			t.Errorf("cijev: %v\n%s", err, ispis)
		}
	}

	// prazan SHA-256 skripte: redak neispravnog oblika ne prolazi
	z = pripremi()
	arg := append([]string{"-c", prijenosKaoRoot, "gocop-nadogradnja", z.Mapa, z.Zbrojevi, "", z.Program}, z.Datoteke...)
	if out, err := exec.Command(bash, arg...).CombinedOutput(); err == nil || strings.Contains(string(out), "mapa=") {
		t.Errorf("bez SHA-256 skripte: %v\n%s", err, out)
	}
	if !strings.Contains(prijenosKaoRoot, "head -c "+strconv.Itoa(najveciProgram)+" ") {
		t.Error("granica kopiranja nije najveciProgram")
	}
}
