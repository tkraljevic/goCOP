package postava

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/izdanje"
)

// cvorSDopustenjem je lažni čvor koji u /zdravlje javlja i dopušteno
// starije izdanje; dopusteno dobiva redni broj pitanja (od 1)
type cvorSDopustenjem struct {
	*lazniUpravljac
	mu        sync.Mutex
	pitano    int
	dopusteno func(pitano int) string
}

func (c *cvorSDopustenjem) PitajZdravlje(ctx context.Context) OdgovorZdravlja {
	izd, port := c.Zdravlje(ctx)
	c.mu.Lock()
	c.pitano++
	n := c.pitano
	c.mu.Unlock()
	return OdgovorZdravlja{Izdanje: izd, Port: port, Dopusteno: c.dopusteno(n)}
}

func uvijek(izd string) func(int) string { return func(int) string { return izd } }

// vlastitiSPostavljenim je Postava s vlastitim čvorom izdanja 0.0.47-alfa
// (program na disku i /zdravlje), ključem za potpis i lažnim GitHubom na
// kojem su v0.0.45-alfa i v0.0.44-alfa
func vlastitiSPostavljenim(t *testing.T, dopusteno func(int) string) (*Postava, *cvorSDopustenjem, *lazniGitHub) {
	t.Helper()
	_, kljuc, _ := ed25519.GenerateKey(rand.Reader) // rand.Reader ne javlja grešku
	p := novaPostava(t, false)
	c := &cvorSDopustenjem{lazniUpravljac: &lazniUpravljac{instaliran: true, radi: true, zeljeno: true, izdanje: "0.0.47-alfa",
		odgovaraNakonPokreni: true, nakonPokretanjaIzdanje: "0.0.45-alfa"}, dopusteno: dopusteno}
	p.Cvor, p.Ugradnja.Cvor = c, c
	postaviProgram(t, p, "0.0.47-alfa")
	p.Ugradnja.M = p.M
	p.Ugradnja.Kljucevi = []ed25519.PublicKey{kljuc.Public().(ed25519.PublicKey)}
	g := noviGitHub(t)
	p.Ugradnja.Izdanja = g.izdanjaZa()
	for _, izd := range []string{"0.0.45-alfa", "0.0.44-alfa"} {
		g.dodaj("v"+izd, prevedi(t, t.TempDir(), izd, "radi"), kljuc, false)
	}
	return p, c, g
}

func izdanjeNaDisku(t *testing.T, p *Postava) string {
	t.Helper()
	izd, err := IzdanjeDatoteke(context.Background(), p.M.Gocop())
	if err != nil {
		t.Fatal(err)
	}
	return izd
}

func dnevnikPostave(t *testing.T, p *Postava) string {
	t.Helper()
	b, _ := os.ReadFile(p.M.DnevnikPostave()) // bez dnevnika je prazan, pa provjera koja ga čita padne
	return string(b)
}

// Jedina iznimka od „samo novije”: točno dopušteno izdanje, starije od
// postavljenoga
func TestDopustenoIzdanje(t *testing.T) {
	v := func(s string) izdanje.Verzija {
		x, ok := izdanje.ParsirajOznaku(s)
		if !ok {
			t.Fatalf("oznaka %q", s)
		}
		return x
	}
	for _, c := range []struct {
		novo, trenutno, dopusteno string
		greska                    string
	}{
		{"0.0.45-alfa", "0.0.47-alfa", "0.0.45-alfa", ""},
		{"0.0.45-alfa", "0.0.47-alfa", "v0.0.45-alfa", ""},
		{"0.0.45-alfa", "0.0.47-alfa", "", "više nije dopušteno"},
		{"0.0.45-alfa", "0.0.47-alfa", "0.0.44-alfa", "više nije dopušteno"},
		{"0.0.45-alfa", "0.0.47-alfa", "0.0.45", "više nije dopušteno"},
		{"0.0.47-alfa", "0.0.47-alfa", "0.0.47-alfa", "nije novije"},
		{"0.0.45-alfa", "", "0.0.45-alfa", "ne zna se"},
		{"0.0.48-alfa", "0.0.47-alfa", "", ""},
		{"0.0.48-alfa", "0.0.47-alfa", "0.0.45-alfa", ""},
		{"0.0.37-alfa", "0.0.47-alfa", "0.0.37-alfa", ""},
		{"0.0.36-alfa", "0.0.47-alfa", "0.0.36-alfa", "starije je od 0.0.37-alfa"},
		{"0.0.5-alfa", "0.0.47-alfa", "0.0.5-alfa", "vraća ručno"},
	} {
		err := dopustenoIzdanje(v(c.novo), c.trenutno, c.dopusteno)
		if c.greska == "" && err != nil || c.greska != "" && (err == nil || !strings.Contains(err.Error(), c.greska)) {
			t.Errorf("%s uz %q, dopušteno %q: %v", c.novo, c.trenutno, c.dopusteno, err)
		}
	}
	for _, c := range []struct {
		a, b  string
		zelim bool
	}{
		{"0.0.45-alfa", "0.0.47-alfa", true},
		{"0.0.47-alfa", "0.0.47-alfa", false},
		{"0.0.48-alfa", "0.0.47-alfa", false},
		{"", "0.0.47-alfa", false},
		{"0.0.45-alfa", "", false},
	} {
		if starijeOd(c.a, c.b) != c.zelim {
			t.Errorf("starijeOd(%q, %q) != %v", c.a, c.b, c.zelim)
		}
	}
	for dopusteno, zelim := range map[string]bool{"0.0.45-alfa": true, "0.0.37-alfa": true, "0.0.36-alfa": false,
		"0.0.47-alfa": false, "nije izdanje": false} {
		if ponuditiStarije(dopusteno, "0.0.47-alfa") != zelim {
			t.Errorf("ponuditiStarije(%q) != %v", dopusteno, zelim)
		}
	}
}

// Izdanje po oznaci: samo potpisano, objavljeno, s programom za sustav
func TestIzdanjePoOznaci(t *testing.T) {
	_, kljuc, _ := ed25519.GenerateKey(rand.Reader) // rand.Reader ne javlja grešku
	g := noviGitHub(t)
	g.dodaj("v0.0.45-alfa", []byte("program"), kljuc, false)
	g.mu.Lock()
	g.izdanja = append(g.izdanja,
		ghIzdanje{Oznaka: "v0.0.44-alfa", Nacrt: true, Datoteke: g.izdanja[0].Datoteke},
		ghIzdanje{Oznaka: "v0.0.43-alfa"})
	g.mu.Unlock()
	iz := g.izdanjaZa()
	ctx := context.Background()
	nadjeno, err := iz.PoOznaci(ctx, "plan9", "amd64", "0.0.45-alfa")
	if err == nil {
		t.Errorf("program drugog sustava prihvaćen: %+v", nadjeno)
	}
	nadjeno, err = iz.PoOznaci(ctx, runtime.GOOS, runtime.GOARCH, "0.0.45-alfa")
	if err != nil || nadjeno.Oznaka != "v0.0.45-alfa" || nadjeno.Program.URL == "" || nadjeno.Potpis.URL == "" {
		t.Fatalf("izdanje po oznaci: %+v %v", nadjeno, err)
	}
	for oznaka, greska := range map[string]string{
		"0.0.44-alfa": "nema potpisanog izdanja 0.0.44-alfa", // nacrt
		"0.0.43-alfa": "nema potpisanog izdanja 0.0.43-alfa", // bez programa i potpisa
		"0.0.42-alfa": "404",
		"najnovije":   "nije oznaka izdanja",
	} {
		if _, err := iz.PoOznaci(ctx, runtime.GOOS, runtime.GOARCH, oznaka); err == nil || !strings.Contains(err.Error(), greska) {
			t.Errorf("%s: %v", oznaka, err)
		}
	}
	// pokvaren odgovor GitHuba
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{")) // klijent javlja grešku čitanja, a nju test provjerava
	}))
	defer srv.Close()
	iz.API = srv.URL
	if _, err := iz.PoOznaci(ctx, runtime.GOOS, runtime.GOARCH, "0.0.45-alfa"); err == nil || !strings.Contains(err.Error(), "izdanje:") {
		t.Errorf("pokvaren odgovor: %v", err)
	}
}

// Rokovi u testovima: daleko u budućnosti i daleko u prošlosti, da test
// ne ovisi o satu
const (
	rok2100 = 4102444800 // 1. 1. 2100.
	rok2000 = 946684800  // 1. 1. 2000.
)

// tijeloZdravlja je odgovor čvora 0.0.47-alfa na /zdravlje s dopuštenjem
func tijeloZdravlja(dopusteno string, do int64, potpis string) string {
	return fmt.Sprintf(`{"izdanje":"0.0.47-alfa","radi":true,"dopusteno_izdanje":%q,"dopusteno_do":%d,"potpis_dopustenja":%q}`,
		dopusteno, do, potpis)
}

// Čvor i usluga čitaju dopušteno izdanje iz /zdravlje. Vlastitom čvoru
// Postava vjeruje samo uz potpis ključem koji mu je dala, a usluzi samo s
// porta ispod 1024; drugi program na istom portu (npr. na rezervnom 8080)
// ne može lažno javiti dopuštenje. Isteklo dopuštenje ne vrijedi, a stariji
// čvor ga ne šalje.
func TestZdravljeCitaDopustenje(t *testing.T) {
	var mu sync.Mutex
	tijelo := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = w.Write([]byte(tijelo)) // Postava koja ne primi odgovor javlja prazno, a to test provjerava
	}))
	defer srv.Close()
	m := MjestaU(t.TempDir())
	zapisi(t, m.PostavkeCvora(), fmt.Sprintf("addr = %q\n", srv.Listener.Addr().String()))
	c := NoviCvor(m, nil)
	c.kljuc = "kljuc-cvora" // kao da ga je Postava pokrenula
	bezKljuca := NoviCvor(m, nil)
	u := noviSustav("active").usluga()
	u.Postavke = m.PostavkeCvora()
	povlastena := noviSustav("active").usluga()
	povlastena.Postavke = m.PostavkeCvora()
	povlastena.povlasten = func(int) bool { return true }
	potpis := func(kljuc, dopusteno string, do int64) string {
		return izdanje.PotpisDopustenja(kljuc, "0.0.47-alfa", dopusteno, do)
	}
	for _, c2 := range []struct {
		ime     string
		tijelo  string
		cvor    string // dopušteno koje vidi vlastiti čvor s ključem
		usluga  string // dopušteno koje vidi usluga s povlaštenog porta
		drugdje string // bez ključa, i usluga s običnog porta
	}{
		{"potpisano, u roku", tijeloZdravlja("0.0.45-alfa", rok2100, potpis("kljuc-cvora", "0.0.45-alfa", rok2100)), "0.0.45-alfa", "0.0.45-alfa", ""},
		{"bez potpisa", tijeloZdravlja("0.0.45-alfa", rok2100, ""), "", "0.0.45-alfa", ""},
		{"drugi ključ", tijeloZdravlja("0.0.45-alfa", rok2100, potpis("drugi", "0.0.45-alfa", rok2100)), "", "0.0.45-alfa", ""},
		{"potpis drugog izdanja", tijeloZdravlja("0.0.44-alfa", rok2100, potpis("kljuc-cvora", "0.0.45-alfa", rok2100)), "", "0.0.44-alfa", ""},
		{"potpis drugog roka", tijeloZdravlja("0.0.45-alfa", rok2100, potpis("kljuc-cvora", "0.0.45-alfa", rok2000)), "", "0.0.45-alfa", ""},
		{"isteklo", tijeloZdravlja("0.0.45-alfa", rok2000, potpis("kljuc-cvora", "0.0.45-alfa", rok2000)), "", "", ""},
		{"bez roka", `{"izdanje":"0.0.47-alfa","radi":true,"dopusteno_izdanje":"0.0.45-alfa"}`, "", "", ""},
		{"stariji čvor", `{"izdanje":"0.0.47-alfa","radi":true}`, "", "", ""},
	} {
		mu.Lock()
		tijelo = c2.tijelo
		mu.Unlock()
		for ime, x := range map[string]struct {
			z     zdravljeIDopustenje
			zelim string
		}{"čvor": {c, c2.cvor}, "usluga s povlaštenog porta": {povlastena, c2.usluga},
			"čvor bez ključa": {bezKljuca, c2.drugdje}, "usluga s porta iznad 1023": {u, c2.drugdje}} {
			o := x.z.PitajZdravlje(context.Background())
			if o.Izdanje != "0.0.47-alfa" || o.Port == 0 || o.Dopusteno != x.zelim || (x.zelim == "") != o.Do.IsZero() {
				t.Errorf("%s, %s: %+v", c2.ime, ime, o)
			}
		}
	}
	if izd, port := c.Zdravlje(context.Background()); izd != "0.0.47-alfa" || port == 0 {
		t.Errorf("Zdravlje: %q %d", izd, port)
	}
	if !(&Usluga{}).sPovlastenogPorta(80) || (&Usluga{}).sPovlastenogPorta(8080) || (&Usluga{}).sPovlastenogPorta(0) {
		t.Error("povlašteni port")
	}
	// rok: dopuštenje vrijedi samo prije njega
	sad := time.Unix(rok2000, 0)
	o := OdgovorZdravlja{Izdanje: "0.0.47-alfa", Dopusteno: "0.0.45-alfa", Do: sad}
	if uzDopustenje(o, true, sad).Dopusteno != "" || uzDopustenje(o, true, sad.Add(-time.Second)).Dopusteno != "0.0.45-alfa" ||
		uzDopustenje(o, false, sad.Add(-time.Second)).Dopusteno != "" {
		t.Error("rok dopuštenja")
	}
}

// pravoZdravlje je lažni upravljač koji /zdravlje pita kao pravi Cvor, s
// njegovim ključem
type pravoZdravlje struct {
	*lazniUpravljac
	c *Cvor
}

func (z pravoZdravlje) PitajZdravlje(ctx context.Context) OdgovorZdravlja {
	return z.c.PitajZdravlje(ctx)
}

// Drugi program na portu čvora javlja dopuštenje bez potpisa (vlastiti
// čvor) ili s porta iznad 1023 (usluga): Postava ništa ne nudi i ništa ne
// postavlja
func TestLaznoDopustenjeNeProlazi(t *testing.T) {
	ctx := context.Background()
	lazni := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(tijeloZdravlja("0.0.45-alfa", rok2100, ""))) // bez odgovora Postava ionako ništa ne nudi
	}))
	defer lazni.Close()
	p, c, _ := vlastitiSPostavljenim(t, uvijek(""))
	m := MjestaU(t.TempDir())
	zapisi(t, m.PostavkeCvora(), fmt.Sprintf("addr = %q\n", lazni.Listener.Addr().String()))
	pravi := NoviCvor(m, nil)
	pravi.kljuc = "kljuc-cvora"
	p.Cvor = pravoZdravlje{lazniUpravljac: c.lazniUpravljac, c: pravi}
	p.osvjeziZdravlje(ctx)
	if s := p.Stanje(); s.Starije != "" {
		t.Errorf("vlastiti čvor: ponuđeno %q", s.Starije)
	}
	if _, err := p.Vrati(ctx); err == nil || izdanjeNaDisku(t, p) != "0.0.47-alfa" {
		t.Errorf("vlastiti čvor: vraćeno (%v)", err)
	}

	postavke := filepath.Join(t.TempDir(), "gocop.toml")
	zapisi(t, postavke, fmt.Sprintf("addr = %q\n", lazni.Listener.Addr().String()))
	l := noviSustav("active")
	pu := postavaUzUslugu(t, l, postavke)
	postaviProgram(t, pu, "0.0.47-alfa")
	pu.osvjeziZdravlje(ctx)
	if s := pu.Stanje(); s.Starije != "" || s.Izdanje != "0.0.47-alfa" {
		t.Errorf("usluga: %+v", s)
	}
	if _, err := pu.Vrati(ctx); err == nil || l.pozvano("/usr/bin/pkexec") != 0 {
		t.Errorf("usluga: vraćeno (%v)", err)
	}
}

// Stavka za vraćanje nudi se samo dok je dopušteno izdanje starije od
// postavljenoga; upravljač koji ne javlja dopuštenje (stariji čvor) ga
// nikad nema
func TestStanjeNudiSamoDopustenoStarije(t *testing.T) {
	ctx := context.Background()
	p, c, _ := vlastitiSPostavljenim(t, uvijek("0.0.45-alfa"))
	p.osvjeziZdravlje(ctx)
	if s := p.Stanje(); s.Starije != "0.0.45-alfa" || s.Novije != nil {
		t.Errorf("dopušteno starije: %+v", s)
	}
	for _, dopusteno := range []string{"", "0.0.47-alfa", "0.0.48-alfa", "nije izdanje"} {
		c.dopusteno = uvijek(dopusteno)
		p.osvjeziZdravlje(ctx)
		if s := p.Stanje(); s.Starije != "" {
			t.Errorf("dopušteno %q: ponuđeno %q", dopusteno, s.Starije)
		}
	}
	// bez dopuštenja na GitHubu postoji samo starije izdanje: ništa se ne nudi
	if err := p.Provjeri(ctx); err != nil {
		t.Fatal(err)
	}
	if s := p.Stanje(); s.Novije != nil || s.Starije != "" {
		t.Errorf("bez dopuštenja: %+v", s)
	}
	if _, err := p.Nadogradi(ctx); err == nil {
		t.Error("nadograđeno na starije bez dopuštenja")
	}
	if _, err := p.Vrati(ctx); err == nil || !strings.Contains(err.Error(), "nije dopušteno") {
		t.Errorf("vraćeno bez dopuštenja: %v", err)
	}
	// čvor bez polja u /zdravlje
	p.Cvor = c.lazniUpravljac
	p.osvjeziZdravlje(ctx)
	if s := p.Stanje(); s.Starije != "" || s.Izdanje != "0.0.47-alfa" {
		t.Errorf("stariji čvor: %+v", s)
	}
	if izdanjeNaDisku(t, p) != "0.0.47-alfa" || c.gasenja != 0 {
		t.Errorf("program promijenjen bez dopuštenja (gašenja %d)", c.gasenja)
	}
}

// Vlastiti čvor: dopušteno starije izdanje preuzme se, provjeri i postavi
func TestVratiVlastitiCvor(t *testing.T) {
	ctx := context.Background()
	p, c, _ := vlastitiSPostavljenim(t, uvijek("0.0.45-alfa"))
	p.osvjeziZdravlje(ctx)
	novo, err := p.Vrati(ctx)
	if err != nil || novo != "0.0.45-alfa" {
		t.Fatalf("vraćanje: %q %v", novo, err)
	}
	if izdanjeNaDisku(t, p) != "0.0.45-alfa" || c.gasenja != 1 || c.pokretanja != 1 {
		t.Errorf("program %s, gašenja %d, pokretanja %d", izdanjeNaDisku(t, p), c.gasenja, c.pokretanja)
	}
	if prethodni, err := IzdanjeDatoteke(ctx, p.M.Prethodni()); err != nil || prethodni != "0.0.47-alfa" {
		t.Errorf("prethodni program: %q %v", prethodni, err)
	}
	d := dnevnikPostave(t, p)
	if !strings.Contains(d, "Vraćanje na starije izdanje 0.0.45-alfa, uz dopuštenje s čvora") ||
		!strings.Contains(d, "Vraćeno na starije izdanje 0.0.45-alfa (prije je bilo 0.0.47-alfa)") {
		t.Errorf("dnevnik Postave:\n%s", d)
	}
	p.OsvjeziZdravlje(ctx) // Prati to radi svake tri sekunde
	if s := p.Stanje(); s.Starije != "" || s.Zauzeto != "" || s.Izdanje != "0.0.45-alfa" {
		t.Errorf("nakon vraćanja: %+v", s)
	}
}

// Dopuštenje istekne ili se zamijeni drugim izdanjem prije preuzimanja ili
// prije zamjene programa: ništa se ne mijenja
func TestVratiBezValjanogDopustenja(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		ime       string
		dopusteno func(int) string
	}{
		// 1. pitanje je osvježavanje stanja, 2. prije preuzimanja, 3. prije zamjene
		{"isteklo prije preuzimanja", func(n int) string { return map[bool]string{true: "0.0.45-alfa"}[n < 2] }},
		{"isteklo prije zamjene", func(n int) string { return map[bool]string{true: "0.0.45-alfa"}[n < 3] }},
		{"drugo izdanje prije zamjene", func(n int) string {
			return map[bool]string{true: "0.0.45-alfa", false: "0.0.44-alfa"}[n < 3]
		}},
	} {
		t.Run(c.ime, func(t *testing.T) {
			p, cvor, _ := vlastitiSPostavljenim(t, c.dopusteno)
			p.osvjeziZdravlje(ctx)
			if s := p.Stanje(); s.Starije != "0.0.45-alfa" {
				t.Fatalf("ponuda: %+v", s)
			}
			if _, err := p.Vrati(ctx); err == nil || !strings.Contains(err.Error(), "više nije dopušteno") {
				t.Errorf("vraćeno: %v", err)
			}
			if izdanjeNaDisku(t, p) != "0.0.47-alfa" || cvor.gasenja != 0 {
				t.Errorf("program %s, gašenja %d", izdanjeNaDisku(t, p), cvor.gasenja)
			}
			if _, err := os.Stat(p.M.Novi()); !os.IsNotExist(err) {
				t.Error("preuzeti program ostao je u mapi novo")
			}
			if d := dnevnikPostave(t, p); !strings.Contains(d, "Vraćanje odbijeno") {
				t.Errorf("dnevnik Postave:\n%s", d)
			}
		})
	}
	// izdanja nema na GitHubu
	p, _, g := vlastitiSPostavljenim(t, uvijek("0.0.45-alfa"))
	g.mu.Lock()
	g.izdanja = nil
	g.mu.Unlock()
	p.osvjeziZdravlje(ctx)
	if _, err := p.Vrati(ctx); err == nil || !strings.Contains(err.Error(), "0.0.45-alfa") {
		t.Errorf("bez izdanja na GitHubu: %v", err)
	}
	// dok traje drugi posao, vraćanje čeka
	p.osvjeziZdravlje(ctx)
	if err := p.zauzmi("Nadogradnja"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Vrati(ctx); err == nil || !strings.Contains(err.Error(), "već je u tijeku") {
		t.Errorf("uz drugi posao: %v", err)
	}
}

// lazniZdravlje je usluga koja u /zdravlje javlja izdanje i dopuštenje;
// dopuštenje istekne nakon zadanog broja pitanja (0: ne istječe)
type lazniZdravlje struct {
	mu                 sync.Mutex
	izdanje, dopusteno string
	pitano, istjece    int
}

func (z *lazniZdravlje) posluzi(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		z.mu.Lock()
		defer z.mu.Unlock()
		z.pitano++
		dop := z.dopusteno
		if z.istjece > 0 && z.pitano > z.istjece {
			dop = ""
		}
		_, _ = fmt.Fprintf(w, `{"izdanje":%q,"radi":true,"dopusteno_izdanje":%q,"dopusteno_do":%d}`, z.izdanje, dop, rok2100) // Postava bez odgovora javlja prazno, a to test provjerava
	}))
	t.Cleanup(srv.Close)
	postavke := filepath.Join(t.TempDir(), "gocop.toml")
	zapisi(t, postavke, fmt.Sprintf("addr = %q\n", srv.Listener.Addr().String()))
	return postavke
}

// Usluga sustava: dopušteno starije izdanje ide kroz instaliraj.sh kao i
// nadogradnja, s rokom dopuštenja za skriptu i bez Postave iz starijeg
// izdanja; kad dopuštenje istekne prije skripte, skripta se ne pokreće
func TestVratiUslugu(t *testing.T) {
	_, kljuc, _ := ed25519.GenerateKey(rand.Reader) // rand.Reader ne javlja grešku
	ctx := context.Background()
	for _, istjece := range []int{0, 2} {
		z := &lazniZdravlje{izdanje: "0.0.47-alfa", dopusteno: "0.0.45-alfa", istjece: istjece}
		l := noviSustav("active")
		p := postavaUzUslugu(t, l, z.posluzi(t))
		p.usluga.povlasten = func(int) bool { return true } // probni poslužitelj ne sluša ispod 1024
		postaviProgram(t, p, "0.0.47-alfa")
		p.Ugradnja.Kljucevi = []ed25519.PublicKey{kljuc.Public().(ed25519.PublicKey)}
		g := noviGitHub(t)
		p.Ugradnja.Izdanja = g.izdanjaZa()
		g.objavi("v0.0.45-alfa", izdanjeSProgramom(t, "0.0.45-alfa", true), kljuc, "")
		skripta := 0
		l.pkexec = func(arg []string) ([]byte, error) {
			skripta++
			// bash -c prijenosKaoRoot ime mapa zbroj zbroj program rok datoteke...
			if len(arg) < 10 || arg[8] != strconv.Itoa(rok2100) || slices.Contains(arg[9:], imePostave(runtime.GOOS, runtime.GOARCH)) {
				t.Errorf("skripta uz vraćanje: %v", arg)
			}
			if _, err := os.Stat(filepath.Join(arg[4], imePostave(runtime.GOOS, runtime.GOARCH))); !os.IsNotExist(err) {
				t.Errorf("Postava iz starijeg izdanja preuzeta: %v", err)
			}
			z.mu.Lock()
			z.izdanje = "0.0.45-alfa"
			z.mu.Unlock()
			return []byte("goCOP 0.0.45-alfa radi kao usluga gocop\n"), nil
		}
		p.osvjeziZdravlje(ctx)
		if s := p.Stanje(); s.Starije != "0.0.45-alfa" {
			t.Fatalf("ponuda uz uslugu: %+v", s)
		}
		novo, err := p.Vrati(ctx)
		if istjece == 0 && (err != nil || novo != "0.0.45-alfa" || skripta != 1) {
			t.Errorf("vraćanje usluge: %q %v, skripta %d", novo, err, skripta)
		}
		if istjece > 0 && (err == nil || skripta != 0) {
			t.Errorf("isteklo prije skripte: %q %v, skripta %d", novo, err, skripta)
		}
	}
}

// Nadogradnja i dalje odbija dok traje drugi posao, i kad je program na
// disku u međuvremenu postao noviji od ponuđenog izdanja
func TestNadogradiOdbija(t *testing.T) {
	ctx := context.Background()
	_, kljuc, _ := ed25519.GenerateKey(rand.Reader) // rand.Reader ne javlja grešku
	p, _, g := vlastitiSPostavljenim(t, uvijek(""))
	g.dodaj("v0.0.48-alfa", prevedi(t, t.TempDir(), "0.0.48-alfa", "radi"), kljuc, false)
	p.osvjeziZdravlje(ctx)
	if err := p.Provjeri(ctx); err != nil {
		t.Fatal(err)
	}
	if s := p.Stanje(); s.Novije == nil || s.Novije.Verzija.String() != "0.0.48-alfa" {
		t.Fatalf("ponuda: %+v", s.Novije)
	}
	if err := p.zauzmi("Vraćanje"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Nadogradi(ctx); err == nil || !strings.Contains(err.Error(), "već je u tijeku") {
		t.Errorf("uz drugi posao: %v", err)
	}
	p.oslobodi()
	// program na disku zamijenjen novijim, s istim vremenom izmjene: ponuda
	// još stoji, ali Nadogradi pita sam program
	fi, err := os.Stat(p.M.Gocop())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.M.Gocop(), prevedi(t, t.TempDir(), "0.0.49-alfa", "radi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p.M.Gocop(), fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Nadogradi(ctx); err == nil || !strings.Contains(err.Error(), "nije novije od postavljenog 0.0.49-alfa") {
		t.Errorf("noviji program na disku: %v", err)
	}
	if izdanjeNaDisku(t, p) != "0.0.49-alfa" {
		t.Error("program promijenjen")
	}
}
