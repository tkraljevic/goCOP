package postava

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/izdanje"
)

// lazniUpravljac glumi čvor: pamti pozive i vraća zadano
type lazniUpravljac struct {
	mu                     sync.Mutex
	instaliran             bool
	radi, zeljeno, pao     bool
	pokrenut               time.Time
	izdanje                string // što Zdravlje javi nakon pokretanja
	greskaZaustavi         error
	greskaPokreni          error
	pokretanja, gasenja    int
	odgovaraNakonPokreni   bool
	nakonPokretanjaIzdanje string
}

func (l *lazniUpravljac) Instaliran() bool { return l.instaliran }
func (l *lazniUpravljac) Pokreni() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pokretanja++
	if l.greskaPokreni != nil {
		return l.greskaPokreni
	}
	l.radi, l.zeljeno = !l.pao, true // čvor koji pada odmah izađe
	if l.odgovaraNakonPokreni {
		l.izdanje = l.nakonPokretanjaIzdanje
	}
	return nil
}
func (l *lazniUpravljac) Zaustavi(time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gasenja++
	if l.greskaZaustavi != nil {
		return l.greskaZaustavi
	}
	l.radi = false
	return nil
}
func (l *lazniUpravljac) Proces() StanjeProcesa {
	l.mu.Lock()
	defer l.mu.Unlock()
	return StanjeProcesa{Radi: l.radi, Zeljeno: l.zeljeno, Pao: l.pao, Pokrenut: l.pokrenut}
}
func (l *lazniUpravljac) Zdravlje(context.Context) (string, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.radi {
		return "", 0
	}
	return l.izdanje, 8080
}
func (l *lazniUpravljac) NaPromjenu(func()) {}

func procitaj(t *testing.T, put string) string {
	t.Helper()
	b, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func zapisi(t *testing.T, put, sadrzaj string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(put, []byte(sadrzaj), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAdresaPloce(t *testing.T) {
	for port, zelim := range map[int]string{
		0:    "http://localhost/",
		80:   "http://localhost/",
		8080: "http://localhost:8080/",
		1160: "http://localhost:1160/",
	} {
		if a := AdresaPloce(port); a != zelim {
			t.Errorf("port %d: %q, želim %q", port, a, zelim)
		}
	}
}

func TestZarotirajSamoVeliki(t *testing.T) {
	dir := t.TempDir()
	velik, malen := filepath.Join(dir, "velik.log"), filepath.Join(dir, "malen.log")
	zapisi(t, velik, strings.Repeat("x", 11))
	zapisi(t, malen, strings.Repeat("x", 10))
	zarotiraj(velik, 10)
	zarotiraj(malen, 10)
	zarotiraj(filepath.Join(dir, "nema.log"), 10) // nema datoteke: ništa
	if _, err := os.Stat(velik); !os.IsNotExist(err) {
		t.Error("veliki dnevnik nije premješten")
	}
	if procitaj(t, velik+".1") != strings.Repeat("x", 11) {
		t.Error("u .1 nije stari dnevnik")
	}
	if procitaj(t, malen) != strings.Repeat("x", 10) {
		t.Error("mali dnevnik je diran")
	}
	if _, err := os.Stat(malen + ".1"); !os.IsNotExist(err) {
		t.Error("mali dnevnik ima kopiju")
	}
}

func TestCitajPID(t *testing.T) {
	dir := t.TempDir()
	put := filepath.Join(dir, "cvor.pid")
	zapisi(t, put, " 4321\n")
	if pid, err := citajPID(put); err != nil || pid != 4321 {
		t.Errorf("pid %d, %v", pid, err)
	}
	for _, los := range []string{"abc", "0", "-5", ""} {
		zapisi(t, put, los)
		if _, err := citajPID(put); err == nil || !strings.Contains(err.Error(), "neispravan PID") {
			t.Errorf("%q: %v", los, err)
		}
	}
	if _, err := citajPID(filepath.Join(dir, "nema.pid")); err == nil {
		t.Error("PID iz datoteke koje nema")
	}
}

func TestPostavaNaPromjenu(t *testing.T) {
	p := novaPostava(t, false)
	n := 0
	p.NaPromjenu(func() { n++ })
	p.javi()
	p.javi()
	if n != 2 {
		t.Errorf("javljeno %d puta, želim 2", n)
	}
	p.NaPromjenu(nil)
	p.javi() // bez primatelja ne smije pasti
	if n != 2 {
		t.Errorf("javljeno i bez primatelja: %d", n)
	}
}

func TestPratiStajeKadKontekstZavrsi(t *testing.T) {
	p := novaPostava(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	gotovo := make(chan struct{})
	go func() {
		p.Prati(ctx)
		close(gotovo)
	}()
	<-gotovo // test bi visio da Prati ne izađe
}

func TestStanjeCvoraPoProcesu(t *testing.T) {
	staro := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	slucajevi := []struct {
		ime     string
		l       *lazniUpravljac
		izdanje string
		zelim   StanjeCvora
	}{
		{"nije instaliran", &lazniUpravljac{}, "", NijeInstaliran},
		{"pao", &lazniUpravljac{instaliran: true, pao: true}, "", Pao},
		{"zaustavljen", &lazniUpravljac{instaliran: true}, "", Zaustavljen},
		{"radi", &lazniUpravljac{instaliran: true, radi: true}, "0.0.40-alfa", Radi},
		{"ne odgovara", &lazniUpravljac{instaliran: true, radi: true, pokrenut: staro}, "", NeOdgovara},
		// Stanje računa time.Since(pokrenut), pa ovdje mora biti sada; rok
		// zdravlja je 90 s, pa brzina stroja ne mijenja ishod.
		{"pokreće se", &lazniUpravljac{instaliran: true, radi: true, pokrenut: time.Now()}, "", Pokrece},
	}
	for _, s := range slucajevi {
		t.Run(s.ime, func(t *testing.T) {
			p := novaPostava(t, false)
			p.Cvor = s.l
			p.izdanje = s.izdanje
			if g := p.Stanje().Cvor; g != s.zelim {
				t.Errorf("stanje %v, želim %v", g, s.zelim)
			}
		})
	}
}

func TestNadogradiBezNovijegIZauzeto(t *testing.T) {
	p := novaPostava(t, false)
	p.Cvor = &lazniUpravljac{instaliran: true}
	if _, err := p.Nadogradi(context.Background()); err == nil || !strings.Contains(err.Error(), "nema novijeg") {
		t.Errorf("bez ponude: %v", err)
	}
	if err := p.zauzmi("prvi posao"); err != nil {
		t.Fatal(err)
	}
	if err := p.zauzmi("drugi posao"); err == nil || !strings.Contains(err.Error(), "prvi posao") {
		t.Errorf("drugi posao: %v", err)
	}
	if s := p.Stanje(); s.Zauzeto != "prvi posao" {
		t.Errorf("zauzeto %q", s.Zauzeto)
	}
	p.oslobodi()
	if err := p.zauzmi("treći posao"); err != nil {
		t.Errorf("nakon oslobađanja: %v", err)
	}
}

// ugradnja s lažnim čvorom: stari program na mjestu, novi pripremljen
func pripremljenaUgradnja(t *testing.T, l *lazniUpravljac) (Ugradnja, string) {
	t.Helper()
	m := MjestaU(t.TempDir())
	zapisi(t, m.Gocop(), "staro")
	zapisi(t, m.Novi(), "novo")
	var mu sync.Mutex
	var dnevnik bytes.Buffer
	u := Ugradnja{M: m, Cvor: l, Pisi: func(f string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(&dnevnik, f+"\n", a...)
	}}
	return u, m.Novi()
}

func TestUgradiZamjenaIPokretanje(t *testing.T) {
	l := &lazniUpravljac{instaliran: true, radi: true, zeljeno: true, izdanje: "0.0.39-alfa",
		odgovaraNakonPokreni: true, nakonPokretanjaIzdanje: "0.0.40-alfa"}
	u, novi := pripremljenaUgradnja(t, l)
	zapisi(t, u.M.BazaCvora(), "baza")
	if err := u.Ugradi(context.Background(), novi, "0.0.39-alfa", "0.0.40-alfa"); err != nil {
		t.Fatal(err)
	}
	if procitaj(t, u.M.Gocop()) != "novo" || procitaj(t, u.M.Prethodni()) != "staro" {
		t.Error("program nije zamijenjen, ili prethodni nije sačuvan")
	}
	if _, err := os.Stat(filepath.Dir(u.M.Novi())); !os.IsNotExist(err) {
		t.Error("mapa novo je ostala")
	}
	kopije, _ := filepath.Glob(filepath.Join(u.M.Kopije(), "gocop-*-0.0.39-alfa.db")) // probni podaci su ispravni; kvar bi pokazala provjera niže
	if len(kopije) != 1 {
		t.Errorf("kopija baze: %v", kopije)
	}
	if l.gasenja != 1 || l.pokretanja != 1 {
		t.Errorf("gašenja %d, pokretanja %d", l.gasenja, l.pokretanja)
	}
}

func TestUgradiVracaPrethodniKadNovoPadne(t *testing.T) {
	l := &lazniUpravljac{instaliran: true, radi: true, zeljeno: true, pao: true}
	u, novi := pripremljenaUgradnja(t, l)
	err := u.Ugradi(context.Background(), novi, "0.0.39-alfa", "0.0.40-alfa")
	if err == nil || !strings.Contains(err.Error(), "vraćeno 0.0.39-alfa") {
		t.Fatalf("greška %v", err)
	}
	if procitaj(t, u.M.Gocop()) != "staro" || procitaj(t, u.M.Neuspjeli()) != "novo" {
		t.Error("prethodni nije vraćen ili neuspjeli nije ostavljen za pregled")
	}
	if l.pokretanja != 2 {
		t.Errorf("pokretanja %d, želim 2 (novi pa vraćeni)", l.pokretanja)
	}
}

func TestUgradiNiPrethodniNeKrece(t *testing.T) {
	l := &lazniUpravljac{instaliran: true, radi: true, zeljeno: true, greskaPokreni: errors.New("ne kreće")}
	u, novi := pripremljenaUgradnja(t, l)
	err := u.Ugradi(context.Background(), novi, "0.0.39-alfa", "0.0.40-alfa")
	if err == nil || !strings.Contains(err.Error(), "ni on ne kreće") {
		t.Fatalf("greška %v", err)
	}
	if procitaj(t, u.M.Gocop()) != "staro" {
		t.Error("prethodni program nije vraćen")
	}
}

func TestUgradiStajeKadSeCvorNeGasi(t *testing.T) {
	l := &lazniUpravljac{instaliran: true, radi: true, zeljeno: true, greskaZaustavi: errors.New("ne gasi se")}
	u, novi := pripremljenaUgradnja(t, l)
	if err := u.Ugradi(context.Background(), novi, "a", "b"); err == nil || err.Error() != "ne gasi se" {
		t.Fatalf("greška %v", err)
	}
	if procitaj(t, u.M.Gocop()) != "staro" || procitaj(t, novi) != "novo" {
		t.Error("program je diran iako se čvor nije ugasio")
	}
	if l.pokretanja != 0 {
		t.Errorf("pokretanja %d", l.pokretanja)
	}
}

// Kad se baza ne da kopirati, program se ne mijenja, a čvor koji je radio
// se vraća.
func TestUgradiBezKopijeBazeNeMijenja(t *testing.T) {
	l := &lazniUpravljac{instaliran: true, radi: true, zeljeno: true}
	u, novi := pripremljenaUgradnja(t, l)
	zapisi(t, u.M.BazaCvora(), "baza")
	zapisi(t, u.M.Kopije(), "datoteka umjesto mape")
	err := u.Ugradi(context.Background(), novi, "0.0.39-alfa", "0.0.40-alfa")
	if err == nil || !strings.Contains(err.Error(), "kopija baze") {
		t.Fatalf("greška %v", err)
	}
	if procitaj(t, u.M.Gocop()) != "staro" {
		t.Error("program je zamijenjen bez kopije baze")
	}
	if l.pokretanja != 1 {
		t.Errorf("čvor koji je radio nije vraćen: pokretanja %d", l.pokretanja)
	}
	// čvor koji nije radio ostaje ugašen
	l2 := &lazniUpravljac{instaliran: true}
	u2, novi2 := pripremljenaUgradnja(t, l2)
	zapisi(t, u2.M.BazaCvora(), "baza")
	zapisi(t, u2.M.Kopije(), "x")
	_ = u2.Ugradi(context.Background(), novi2, "a", "b") // probni podaci su ispravni; kvar bi pokazala provjera niže
	if l2.pokretanja != 0 {
		t.Errorf("ugašeni čvor pokrenut: %d", l2.pokretanja)
	}
}

func TestPostaviBezMapePrograma(t *testing.T) {
	m := MjestaU(t.TempDir())
	zapisi(t, m.Program, "datoteka umjesto mape")
	if err := (Ugradnja{M: m}).Postavi(filepath.Join(t.TempDir(), "novi")); err == nil {
		t.Error("postavljanje u mapu koja je datoteka je prošlo")
	}
}

func TestKopirajBazuIWAL(t *testing.T) {
	m := MjestaU(t.TempDir())
	zapisi(t, m.BazaCvora(), "baza")
	zapisi(t, m.BazaCvora()+"-wal", "wal")
	sad := time.Date(2026, 10, 30, 8, 15, 0, 0, time.UTC)
	if err := KopirajBazu(m, "", sad); err != nil {
		t.Fatal(err)
	}
	ime := filepath.Join(m.Kopije(), "gocop-20261030-081500-nepoznato.db")
	if procitaj(t, ime) != "baza" || procitaj(t, ime+"-wal") != "wal" {
		t.Error("kopija baze ili WAL-a ne odgovara")
	}
	if _, err := os.Stat(ime + ".dio"); !os.IsNotExist(err) {
		t.Error("ostao je .dio")
	}
}

func TestKopirajGreske(t *testing.T) {
	dir := t.TempDir()
	if err := kopiraj(filepath.Join(dir, "nema"), filepath.Join(dir, "u")); err == nil {
		t.Error("kopija datoteke koje nema")
	}
	zapisi(t, filepath.Join(dir, "iz"), "x")
	if err := kopiraj(filepath.Join(dir, "iz"), filepath.Join(dir, "nema-mape", "u")); err == nil {
		t.Error("kopija u mapu koje nema")
	}
	if err := pocisti(filepath.Join(dir, "nema-mape"), 3); err == nil {
		t.Error("čišćenje mape koje nema")
	}
}

func TestPocistiOstavljaNajnovije(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{
		"gocop-20261001-000000-a.db", "gocop-20261001-000000-a.db-wal",
		"gocop-20261002-000000-b.db", "gocop-20261003-000000-c.db",
		"drugo.db", // nije kopija, ne dira se
	} {
		zapisi(t, filepath.Join(dir, n), "x")
	}
	if err := pocisti(dir, 2); err != nil {
		t.Fatal(err)
	}
	stavke, _ := os.ReadDir(dir) // probni podaci su ispravni; kvar bi pokazala provjera niže
	var imena []string
	for _, s := range stavke {
		imena = append(imena, s.Name())
	}
	zelim := "drugo.db gocop-20261002-000000-b.db gocop-20261003-000000-c.db"
	if strings.Join(imena, " ") != zelim {
		t.Errorf("ostalo %v", imena)
	}
}

func TestIzdanjaGreskeGitHuba(t *testing.T) {
	odgovor := http.StatusOK
	tijelo := "["
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(odgovor)
		_, _ = w.Write([]byte(tijelo))
	}))
	defer srv.Close()
	iz := Izdanja{API: srv.URL, Repo: izdanje.Repozitorij, Klijent: srv.Client(), Agent: "test"}
	ctx := context.Background()

	if _, err := iz.Provjeri(ctx, "linux", "amd64", "1.0.0"); err == nil || !strings.Contains(err.Error(), "popis izdanja") {
		t.Errorf("pokvaren JSON: %v", err)
	}
	odgovor = http.StatusForbidden
	if _, err := iz.Provjeri(ctx, "linux", "amd64", "1.0.0"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("odbijen upit: %v", err)
	}
	loš := Izdanja{API: "http://[::1", Klijent: srv.Client()}
	if _, err := loš.Provjeri(ctx, "linux", "amd64", "1.0.0"); err == nil {
		t.Error("neispravna adresa je prošla")
	}
	srv2 := httptest.NewServer(http.NotFoundHandler())
	adresa := srv2.URL
	srv2.Close()
	nedostupan := Izdanja{API: adresa, Klijent: &http.Client{Timeout: time.Second}}
	if _, err := nedostupan.Provjeri(ctx, "linux", "amd64", "1.0.0"); err == nil || !strings.Contains(err.Error(), "nije dostupan") {
		t.Errorf("nedostupan GitHub: %v", err)
	}

	// preuzimanje
	var b bytes.Buffer
	if err := iz.preuzmi(ctx, srv.URL+"/x", 10, &b); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("preuzimanje s odbijanjem: %v", err)
	}
	odgovor, tijelo = http.StatusOK, strings.Repeat("x", 11)
	if err := iz.preuzmi(ctx, srv.URL+"/x", 10, &b); err == nil || !strings.Contains(err.Error(), "veća je od dopuštenog") {
		t.Errorf("prevelika datoteka: %v", err)
	}
	b.Reset()
	tijelo = strings.Repeat("x", 10)
	if err := iz.preuzmi(ctx, srv.URL+"/x", 10, &b); err != nil || b.Len() != 10 {
		t.Errorf("datoteka točno na granici: %v, %d", err, b.Len())
	}
	if err := iz.preuzmi(ctx, "http://[::1", 10, &b); err == nil {
		t.Error("neispravna adresa preuzimanja")
	}
	if err := nedostupan.preuzmi(ctx, adresa, 10, &b); err == nil || !strings.Contains(err.Error(), "preuzimanje") {
		t.Errorf("nedostupno preuzimanje: %v", err)
	}
}

func TestBrojeviPostave(t *testing.T) {
	for s, zelim := range map[string][3]int{
		"postava-v1.2.3": {1, 2, 3},
		"1.4.0":          {1, 4, 0},
		"2.0.1-rc1":      {2, 0, 1},
		"razvoj":         {0, 0, 0},
		"":               {0, 0, 0},
	} {
		if b := brojeviPostave(s); b != zelim {
			t.Errorf("%q → %v, želim %v", s, b, zelim)
		}
	}
}

// Bez zadanih ključeva vrijede ugrađeni ključevi izdanja, a zadani ih
// zamjenjuju, ne dopunjuju.
func TestKljuceviUgradeniIliZadani(t *testing.T) {
	ugradeni := izdanje.Kljucevi(izdanje.JavniKljucevi)
	g := (Ugradnja{}).kljucevi()
	if len(g) == 0 || len(g) != len(ugradeni) {
		t.Fatalf("bez zadanih: %d ključeva, ugrađenih %d", len(g), len(ugradeni))
	}
	for i := range g {
		if !g[i].Equal(ugradeni[i]) {
			t.Errorf("ključ %d nije ugrađeni ključ izdanja", i)
		}
	}
	javni, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	z := (Ugradnja{Kljucevi: []ed25519.PublicKey{javni}}).kljucevi()
	if len(z) != 1 || !z[0].Equal(javni) {
		t.Errorf("zadani ključ se ne poštuje: %d ključeva", len(z))
	}
}
