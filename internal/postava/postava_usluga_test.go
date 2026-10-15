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
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
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

// lazniCvor bilježi što je Postava tražila od vlastitog čvora
type lazniCvor struct{ zaustavljen int }

func (c *lazniCvor) Instaliran() bool                       { return true }
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
	p.Ugradnja.Kljucevi = []ed25519.PublicKey{javni}
	g := noviGitHub(t)
	p.Ugradnja.Izdanja = g.izdanjaZa()
	ctx := context.Background()
	program := izdanje.ImeDatoteke(runtime.GOOS, runtime.GOARCH)

	// pkexec: provjeri što je pripremljeno i „nadogradi” lažni čvor
	var naredba []string
	var uMapi map[string]string
	l.pkexec = func(arg []string) ([]byte, error) {
		naredba = arg
		uMapi = map[string]string{}
		stavke, _ := os.ReadDir(filepath.Dir(arg[1]))
		for _, s := range stavke {
			b, _ := os.ReadFile(filepath.Join(filepath.Dir(arg[1]), s.Name()))
			uMapi[s.Name()] = string(b)
		}
		mu.Lock()
		izd = "0.0.36-alfa"
		mu.Unlock()
		return []byte("→ provjeravam potpis izdanja\ngoCOP 0.0.36-alfa radi kao usluga gocop\n"), nil
	}

	g.objavi("v0.0.36-alfa", datotekeIzdanja(true), kljuc, "")
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
	if len(naredba) != 3 || naredba[0] != "/usr/bin/bash" || filepath.Base(naredba[1]) != imeSkripte ||
		naredba[2] != filepath.Join(filepath.Dir(naredba[1]), program) || !filepath.IsAbs(naredba[1]) {
		t.Fatalf("pkexec %v", naredba)
	}
	zelim := datotekeIzdanja(true)
	for n, b := range zelim {
		if uMapi[n] != string(b) {
			t.Errorf("u mapi za skriptu %s = %q", n, uMapi[n])
		}
	}
	if uMapi[izdanje.ImeZbrojeva] == "" || uMapi[izdanje.ImePotpisa] == "" || len(uMapi) != len(zelim)+2 {
		t.Errorf("u mapi za skriptu: %v", uMapi)
	}
	if _, err := os.Stat(filepath.Dir(naredba[1])); !os.IsNotExist(err) {
		t.Error("mapa s preuzetim izdanjem ostala je nakon nadogradnje")
	}
	if b, _ := os.ReadFile(p.M.DnevnikPostave()); !strings.Contains(string(b), "radi kao usluga gocop") {
		t.Errorf("ispis skripte nije u dnevniku Postave:\n%s", b)
	}

	// odustajanje od lozinke i usluga koja nakon skripte ne radi
	g.objavi("v0.0.37-alfa", datotekeIzdanja(false), kljuc, "")
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
		_, err := os.Stat(filepath.Join(filepath.Dir(arg[1]), imePostave(runtime.GOOS, runtime.GOARCH)))
		uMapi = map[string]string{"postava": fmt.Sprint(err == nil)}
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

	if err := u.Ugradi(ctx, mapa, "gocop-linux-amd64", pisi); err != nil {
		t.Fatal(err)
	}
	zelim := []string{"/usr/bin/pkexec", "/usr/bin/bash", filepath.Join(mapa, "instaliraj.sh"), filepath.Join(mapa, "gocop-linux-amd64")}
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
		err := u.Ugradi(ctx, mapa, "gocop-linux-amd64", pisi)
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

	delete(l.programi, "bash")
	if err := u.Ugradi(ctx, mapa, "gocop-linux-amd64", pisi); err == nil || !strings.Contains(err.Error(), "bash") {
		t.Errorf("bez bash-a: %v", err)
	}
	delete(l.programi, "pkexec")
	if err := u.Ugradi(ctx, mapa, "gocop-linux-amd64", pisi); err == nil || !strings.Contains(err.Error(), "pkexec") ||
		!strings.Contains(err.Error(), "docs/linux.md, poglavlje 9") {
		t.Errorf("bez pkexec-a: %v", err)
	}
}
