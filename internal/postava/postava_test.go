package postava

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/izdanje"
)

// lazniGitHub poslužuje popis izdanja i njihove datoteke
type lazniGitHub struct {
	mu       sync.Mutex
	izdanja  []ghIzdanje
	datoteke map[string][]byte
	srv      *httptest.Server
}

func noviGitHub(t *testing.T) *lazniGitHub {
	g := &lazniGitHub{datoteke: map[string][]byte{}}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		if r.URL.Path == "/repos/"+izdanje.Repozitorij+"/releases" {
			_ = json.NewEncoder(w).Encode(g.izdanja)
			return
		}
		if b, ok := g.datoteke[r.URL.Path]; ok {
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(g.srv.Close)
	return g
}

// dodaj objavljuje izdanje: program, SHA256SUMS i potpis zadanim ključem.
// podmetni mijenja program nakon potpisa.
func (g *lazniGitHub) dodaj(oznaka string, program []byte, kljuc ed25519.PrivateKey, podmetni bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	ime := izdanje.ImeDatoteke(runtime.GOOS, runtime.GOARCH)
	zbrojevi := []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(program), ime))
	if podmetni {
		program = append(append([]byte{}, program...), 0)
	}
	put := func(n string) string { return "/dl/" + oznaka + "/" + n }
	g.datoteke[put(ime)] = program
	g.datoteke[put(izdanje.ImeZbrojeva)] = zbrojevi
	g.datoteke[put(izdanje.ImePotpisa)] = izdanje.Potpisi(kljuc, zbrojevi)
	var datoteke []ghDatoteka
	for _, n := range []string{ime, izdanje.ImeZbrojeva, izdanje.ImePotpisa} {
		datoteke = append(datoteke, ghDatoteka{Ime: n, URL: g.srv.URL + put(n)})
	}
	g.izdanja = append(g.izdanja, ghIzdanje{Oznaka: oznaka, Stranica: "https://github.com/x/" + oznaka, Datoteke: datoteke})
}

func (g *lazniGitHub) izdanjaZa() Izdanja {
	return Izdanja{API: g.srv.URL, Repo: izdanje.Repozitorij, Klijent: g.srv.Client(), Agent: "test"}
}

func TestProvjeraBiraNajnovijePotpisanoIzdanje(t *testing.T) {
	_, kljuc, _ := ed25519.GenerateKey(rand.Reader)
	g := noviGitHub(t)
	g.dodaj("v0.0.27-alfa", []byte("a"), kljuc, false)
	g.dodaj("v0.0.28-alfa", []byte("b"), kljuc, false)
	g.mu.Lock()
	ime := izdanje.ImeDatoteke(runtime.GOOS, runtime.GOARCH)
	g.izdanja = append(g.izdanja,
		ghIzdanje{Oznaka: "v0.0.29-alfa", Nacrt: true, Datoteke: g.izdanja[1].Datoteke},     // nacrt: još nije potpisan i objavljen
		ghIzdanje{Oznaka: "v0.0.30-alfa", Datoteke: []ghDatoteka{{Ime: ime, URL: "x"}}},     // bez potpisa
		ghIzdanje{Oznaka: "v0.0.31-alfa", Datoteke: []ghDatoteka{{Ime: "gocop-plan9-386"}}}, // nema programa za ovaj sustav
		ghIzdanje{Oznaka: "postava-v1.2.0", Stranica: "https://github.com/x/postava-v1.2.0"},
		ghIzdanje{Oznaka: "postava-v0.9.0"},
		ghIzdanje{Oznaka: "probna-gradnja"},
	)
	g.mu.Unlock()

	p, err := g.izdanjaZa().Provjeri(context.Background(), runtime.GOOS, runtime.GOARCH, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if p.GoCOP == nil || p.GoCOP.Oznaka != "v0.0.28-alfa" {
		t.Fatalf("izabrano %+v, želim v0.0.28-alfa", p.GoCOP)
	}
	if p.Postava != "postava-v1.2.0" {
		t.Errorf("nova Postava: %q", p.Postava)
	}
	if p, _ := g.izdanjaZa().Provjeri(context.Background(), runtime.GOOS, runtime.GOARCH, "1.2.0"); p.Postava != "" {
		t.Errorf("ista Postava javljena kao nova: %q", p.Postava)
	}
}

// prevedi gradi lažni čvor zadanog izdanja
func prevedi(t *testing.T, dir, izd, nacin string) []byte {
	t.Helper()
	out := filepath.Join(dir, "lazni-"+izd+"-"+nacin+nastavak())
	cmd := exec.Command("go", "build", "-o", out,
		"-ldflags", "-X main.izdanje="+izd+" -X main.nacin="+nacin, "./testdata/laznicvor")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("lažni čvor: %v\n%s", err, b)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func slobodanPort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func cekajIzdanje(t *testing.T, c *Cvor, zelim string) {
	t.Helper()
	rok := time.Now().Add(30 * time.Second)
	for time.Now().Before(rok) {
		if izd, _ := c.Zdravlje(context.Background()); izd == zelim {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	b, _ := os.ReadFile(c.m.DnevnikCvora())
	t.Fatalf("čvor ne javlja izdanje %s\n%s", zelim, b)
}

// Cijeli put Postave: instalacija najnovijeg izdanja, nadogradnja s
// kopijom baze, odbijanje podmetnutog i tuđim ključem potpisanog izdanja,
// i vraćanje prethodnog kad novo ne krene
func TestInstalacijaNadogradnjaIVracanje(t *testing.T) {
	if testing.Short() {
		t.Skip("prevodi lažne čvorove")
	}
	if runtime.GOOS == "windows" {
		t.Skip("PATH i pokretanje pri prijavi na Windowsu diraju registar korisnika")
	}
	staroRazmak := razmakPada
	razmakPada = 100 * time.Millisecond
	defer func() { razmakPada = staroRazmak }()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	gradnja := t.TempDir()
	v27 := prevedi(t, gradnja, "0.0.27-alfa", "radi")
	v28 := prevedi(t, gradnja, "0.0.28-alfa", "radi")
	v29 := prevedi(t, gradnja, "0.0.29-alfa", "pada")

	_, kljuc, _ := ed25519.GenerateKey(rand.Reader)
	javni := kljuc.Public().(ed25519.PublicKey)
	_, tudji, _ := ed25519.GenerateKey(rand.Reader)
	g := noviGitHub(t)
	g.dodaj("v0.0.27-alfa", v27, kljuc, false)

	baza := t.TempDir()
	exe := filepath.Join(baza, ImeMapePostave, "gocop-postava"+nastavak())
	p := Nova(exe, "1.0.0")
	p.Ugradnja.Izdanja = g.izdanjaZa()
	p.Ugradnja.Kljucevi = []ed25519.PublicKey{javni}
	m := p.M
	if m.Baza != baza {
		t.Fatalf("baza instalacije %s, želim %s", m.Baza, baza)
	}
	ctx := context.Background()

	// instalacija: najnovije potpisano izdanje, PATH
	if err := p.Instaliraj(ctx, Opcije{}); err != nil {
		t.Fatal(err)
	}
	if izd, err := IzdanjeDatoteke(ctx, m.Gocop()); err != nil || izd != "0.0.27-alfa" {
		t.Fatalf("instalirano %q %v", izd, err)
	}
	if !UPutu(m.Program) {
		t.Error("program nije u PATH-u")
	}
	if err := os.WriteFile(m.PostavkeCvora(), []byte(fmt.Sprintf("addr = \"127.0.0.1:%d\"\n", slobodanPort(t))), 0o644); err != nil {
		t.Fatal(err)
	}
	for n, s := range map[string]string{"": "baza", "-wal": "wal"} {
		if err := os.WriteFile(m.BazaCvora()+n, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Cvor.Pokreni(); err != nil {
		t.Fatal(err)
	}
	defer p.Cvor.Zaustavi(10 * time.Second)
	cekajIzdanje(t, p.Cvor, "0.0.27-alfa")

	// podmetnut program i tuđi ključ se odbijaju, postojeći ostaje
	g.dodaj("v0.0.30-alfa", v28, kljuc, true)
	if err := p.Provjeri(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Nadogradi(ctx); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("podmetnut program: %v", err)
	}
	g.mu.Lock()
	g.izdanja = g.izdanja[:1]
	g.mu.Unlock()
	g.dodaj("v0.0.31-alfa", v28, tudji, false)
	_ = p.Provjeri(ctx)
	if _, err := p.Nadogradi(ctx); err == nil || !strings.Contains(err.Error(), "potpis") {
		t.Fatalf("tuđi ključ: %v", err)
	}
	cekajIzdanje(t, p.Cvor, "0.0.27-alfa")

	// prava nadogradnja
	g.mu.Lock()
	g.izdanja = g.izdanja[:1]
	g.mu.Unlock()
	g.dodaj("v0.0.28-alfa", v28, kljuc, false)
	if err := p.Provjeri(ctx); err != nil {
		t.Fatal(err)
	}
	if s := p.Stanje(); s.Novije == nil || s.Novije.Oznaka != "v0.0.28-alfa" {
		t.Fatalf("ponuda: %+v", s.Novije)
	}
	if izd, err := p.Nadogradi(ctx); err != nil || izd != "0.0.28-alfa" {
		t.Fatalf("nadogradnja: %q %v", izd, err)
	}
	cekajIzdanje(t, p.Cvor, "0.0.28-alfa")
	if izd, _ := IzdanjeDatoteke(ctx, m.Prethodni()); izd != "0.0.27-alfa" {
		t.Errorf("prethodni program: %q", izd)
	}
	kopije, _ := filepath.Glob(filepath.Join(m.Kopije(), "gocop-*-0.0.27-alfa.db*"))
	if len(kopije) != 2 {
		t.Errorf("kopija baze i WAL-a prije nadogradnje: %v", kopije)
	}

	// novo izdanje pada: vraća se prethodno
	g.dodaj("v0.0.29-alfa", v29, kljuc, false)
	if err := p.Provjeri(ctx); err != nil {
		t.Fatal(err)
	}
	_, err := p.Nadogradi(ctx)
	if err == nil || !strings.Contains(err.Error(), "vraćeno") {
		t.Fatalf("izdanje koje pada: %v", err)
	}
	cekajIzdanje(t, p.Cvor, "0.0.28-alfa")
	if izd, _ := IzdanjeDatoteke(ctx, m.Gocop()); izd != "0.0.28-alfa" {
		t.Errorf("nakon vraćanja program je %q", izd)
	}
	if izd, _ := IzdanjeDatoteke(ctx, m.Neuspjeli()); izd != "0.0.29-alfa" {
		t.Errorf("neuspjelo izdanje nije ostavljeno za pregled: %q", izd)
	}

	// zaustavljanje: zatvoren ulaz, čvor izlazi
	if err := p.Cvor.Zaustavi(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if p.Stanje().Cvor != Zaustavljen {
		t.Errorf("stanje nakon zaustavljanja: %v", p.Stanje().Cvor)
	}
}

func TestKopijeBazeCuvajuTriNajnovije(t *testing.T) {
	m := MjestaU(t.TempDir())
	_ = os.MkdirAll(m.Podaci, 0o755)
	_ = os.WriteFile(m.BazaCvora(), []byte("baza"), 0o644)
	pocetak := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if err := KopirajBazu(m, fmt.Sprintf("0.0.%d-alfa", 20+i), pocetak.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	ostale, _ := filepath.Glob(filepath.Join(m.Kopije(), "gocop-*.db"))
	if len(ostale) != 3 {
		t.Fatalf("kopija: %v", ostale)
	}
	for _, k := range ostale {
		if strings.Contains(k, "0.0.20-alfa") || strings.Contains(k, "0.0.21-alfa") {
			t.Errorf("ostala je stara kopija %s", k)
		}
	}
	// bez baze nema ni kopije ni greške
	if err := KopirajBazu(MjestaU(t.TempDir()), "x", pocetak); err != nil {
		t.Error(err)
	}
}

func TestMjestaInstalacije(t *testing.T) {
	baza := filepath.Join(t.TempDir(), "Moj goCOP")
	m := OdrediMjesta(filepath.Join(baza, "postava", "gocop-postava.exe"))
	if m.Baza != baza || m.Program != filepath.Join(baza, "program") || m.Podaci != filepath.Join(baza, "data") {
		t.Errorf("mjesta: %+v", m)
	}
	// proba iz mape gradnje: zadana baza, ne mapa gradnje
	if m := OdrediMjesta(filepath.Join(t.TempDir(), "gocop-postava")); m.Baza != ZadanaBaza() {
		t.Errorf("izvan instalacije baza je %s", m.Baza)
	}
}
