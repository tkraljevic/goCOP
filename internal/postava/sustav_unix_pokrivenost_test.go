//go:build darwin || linux

package postava

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
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

// skriptaCvora je lažni program čvora: javi izdanje, „upiše” ime čvora, a
// kao upravitelj spava i ne gleda ulaz
func skriptaCvora(izd string) []byte {
	return []byte(fmt.Sprintf(`#!/bin/sh
case "$1" in
-version) echo "goCOP %s";;
-pripremi) echo "ime čvora upisano: $5";;
-upravitelj) exec sleep 30;;
esac
`, izd))
}

// mapaIzdanja slaže izdanje za instalaciju bez interneta: program za ovaj
// sustav, SHA256SUMS i potpis. podmetni mijenja program nakon potpisa.
func mapaIzdanja(t *testing.T, program []byte, kljuc ed25519.PrivateKey, podmetni bool) string {
	t.Helper()
	mapa := t.TempDir()
	ime := izdanje.ImeDatoteke(runtime.GOOS, runtime.GOARCH)
	zbrojevi := []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(program), ime))
	if podmetni {
		program = append(append([]byte{}, program...), '#')
	}
	if err := os.WriteFile(filepath.Join(mapa, ime), program, 0o755); err != nil {
		t.Fatal(err)
	}
	zapisi(t, filepath.Join(mapa, izdanje.ImeZbrojeva), string(zbrojevi))
	zapisi(t, filepath.Join(mapa, izdanje.ImePotpisa), string(izdanje.Potpisi(kljuc, zbrojevi)))
	return mapa
}

func noviKljuc(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	javni, tajni, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return javni, tajni
}

func ugradnjaU(t *testing.T, javni ed25519.PublicKey) Ugradnja {
	return Ugradnja{M: MjestaU(t.TempDir()), Kljucevi: []ed25519.PublicKey{javni},
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
}

func TestPripremiIzMapeProvjereniProgram(t *testing.T) {
	javni, tajni := noviKljuc(t)
	u := ugradnjaU(t, javni)
	program := skriptaCvora("0.0.40-alfa")
	put, v, err := u.PripremiIzMape(context.Background(), mapaIzdanja(t, program, tajni, false))
	if err != nil {
		t.Fatal(err)
	}
	if put != u.M.Novi() || v.String() != "0.0.40-alfa" {
		t.Errorf("pripremljeno %s, izdanje %s", put, v)
	}
	if procitaj(t, put) != string(program) {
		t.Error("pripremljeni program nije onaj iz mape")
	}
	if _, err := os.Stat(put + ".dio"); !os.IsNotExist(err) {
		t.Error("ostao je .dio")
	}
}

func TestPripremiIzMapeOdbija(t *testing.T) {
	javni, tajni := noviKljuc(t)
	_, tudji := noviKljuc(t)
	ime := izdanje.ImeDatoteke(runtime.GOOS, runtime.GOARCH)
	dobar := skriptaCvora("0.0.40-alfa")
	slucajevi := []struct {
		ime    string
		mapa   func(t *testing.T) string
		u      func(u *Ugradnja)
		greska string
	}{
		{"bez zbrojeva", func(t *testing.T) string {
			m := mapaIzdanja(t, dobar, tajni, false)
			os.Remove(filepath.Join(m, izdanje.ImeZbrojeva))
			return m
		}, nil, izdanje.ImeZbrojeva},
		{"bez potpisa", func(t *testing.T) string {
			m := mapaIzdanja(t, dobar, tajni, false)
			os.Remove(filepath.Join(m, izdanje.ImePotpisa))
			return m
		}, nil, izdanje.ImePotpisa},
		{"tuđi ključ", func(t *testing.T) string { return mapaIzdanja(t, dobar, tudji, false) }, nil, "odbijeno"},
		{"bez programa", func(t *testing.T) string {
			m := mapaIzdanja(t, dobar, tajni, false)
			os.Remove(filepath.Join(m, ime))
			return m
		}, nil, ime},
		{"podmetnut program", func(t *testing.T) string { return mapaIzdanja(t, dobar, tajni, true) }, nil,
			"ne odgovara potpisanom SHA-256"},
		{"zbrojevi za drugi sustav", func(t *testing.T) string { return mapaIzdanja(t, dobar, tajni, false) },
			func(u *Ugradnja) { u.GOOS = "plan9" }, "plan9"},
		{"program bez izdanja", func(t *testing.T) string {
			return mapaIzdanja(t, []byte("#!/bin/sh\necho nešto drugo\n"), tajni, false)
		}, nil, "-version ispisuje"},
		{"nepoznato izdanje", func(t *testing.T) string {
			return mapaIzdanja(t, []byte("#!/bin/sh\necho goCOP razvoj\n"), tajni, false)
		}, nil, "nepoznato izdanje"},
		{"mapa programa je datoteka", func(t *testing.T) string { return mapaIzdanja(t, dobar, tajni, false) },
			func(u *Ugradnja) { zapisi(t, u.M.Program, "x") }, "not a directory"},
	}
	for _, s := range slucajevi {
		t.Run(s.ime, func(t *testing.T) {
			u := ugradnjaU(t, javni)
			if s.u != nil {
				s.u(&u)
			}
			_, _, err := u.PripremiIzMape(context.Background(), s.mapa(t))
			if err == nil {
				t.Fatal("izdanje je prihvaćeno")
			}
			if !strings.Contains(err.Error(), s.greska) {
				t.Errorf("greška %q ne spominje %q", err, s.greska)
			}
			if s.ime == "podmetnut program" {
				if _, e := os.Stat(u.M.Novi() + ".dio"); !os.IsNotExist(e) {
					t.Error("podmetnuti program je ostao na disku")
				}
				if _, e := os.Stat(u.M.Novi()); !os.IsNotExist(e) {
					t.Error("podmetnuti program je pripremljen")
				}
			}
		})
	}
}

func TestProvjeriIzdanjeBriseKrivi(t *testing.T) {
	dir := t.TempDir()
	put := filepath.Join(dir, "gocop")
	if err := os.WriteFile(put, skriptaCvora("0.0.39-alfa"), 0o755); err != nil {
		t.Fatal(err)
	}
	u := Ugradnja{}
	if err := u.provjeriIzdanje(context.Background(), put, "0.0.40-alfa"); err == nil ||
		!strings.Contains(err.Error(), "javlja izdanje 0.0.39-alfa") {
		t.Errorf("krivo izdanje: %v", err)
	}
	if _, err := os.Stat(put); !os.IsNotExist(err) {
		t.Error("program krivog izdanja nije obrisan")
	}
	zapisi(t, put, "nije program")
	if err := u.provjeriIzdanje(context.Background(), put, "0.0.40-alfa"); err == nil {
		t.Error("datoteka koja se ne da pokrenuti je prošla")
	}
	if _, err := os.Stat(put); !os.IsNotExist(err) {
		t.Error("neupotrebljiva datoteka nije obrisana")
	}
}

// postavaZaInstalaciju je Postava u privremenoj mapi, s korisnikovim
// mapama u privremenoj mapi (PATH i pokretanje pri prijavi)
func postavaZaInstalaciju(t *testing.T, javni ed25519.PublicKey) *Postava {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	p := novaPostava(t, false)
	p.Ugradnja.Kljucevi = []ed25519.PublicKey{javni}
	return p
}

func TestInstalirajIzMape(t *testing.T) {
	javni, tajni := noviKljuc(t)
	p := postavaZaInstalaciju(t, javni)
	ctx := context.Background()

	if err := p.Instaliraj(ctx, Opcije{IzMape: filepath.Join(t.TempDir(), "nema")}); err == nil {
		t.Error("instalacija iz mape koje nema")
	}
	mapa := mapaIzdanja(t, skriptaCvora("0.0.40-alfa"), tajni, false)
	if err := p.Instaliraj(ctx, Opcije{IzMape: mapa, PriPrijavi: true, ImeCvora: "pperic-mac"}); err != nil {
		t.Fatal(err)
	}
	if izd, err := IzdanjeDatoteke(ctx, p.M.Gocop()); err != nil || izd != "0.0.40-alfa" {
		t.Fatalf("instalirano %q, %v", izd, err)
	}
	if !UPutu(p.M.Program) {
		t.Error("program nije u PATH-u")
	}
	if uklj, ispravno := PriPrijavi(p.Exe); !uklj || !ispravno {
		t.Errorf("pokretanje pri prijavi: %v %v", uklj, ispravno)
	}
	dnevnik := procitaj(t, p.M.DnevnikPostave())
	if !strings.Contains(dnevnik, "ime čvora upisano: pperic-mac") || !strings.Contains(dnevnik, "Instaliran goCOP 0.0.40-alfa iz") {
		t.Errorf("dnevnik:\n%s", dnevnik)
	}
	if put := p.PrvoOtvaranje(); put != "postavljanje" {
		t.Errorf("prvo otvaranje: %q", put)
	}

	// starije izdanje iz mape ne zamjenjuje novije; isključuje se prijava
	starija := mapaIzdanja(t, skriptaCvora("0.0.39-alfa"), tajni, false)
	if err := p.Instaliraj(ctx, Opcije{IzMape: starija}); err != nil {
		t.Fatal(err)
	}
	if izd, _ := IzdanjeDatoteke(ctx, p.M.Gocop()); izd != "0.0.40-alfa" {
		t.Errorf("starije izdanje je zamijenilo novije: %q", izd)
	}
	if uklj, _ := PriPrijavi(p.Exe); uklj {
		t.Error("pokretanje pri prijavi nije isključeno")
	}
}

// Ime čvora koje ne da program pripreme ne zaustavlja instalaciju: čvor ga
// izabere sam.
func TestInstalirajImeKojeProgramNePrimi(t *testing.T) {
	javni, tajni := noviKljuc(t)
	p := postavaZaInstalaciju(t, javni)
	program := []byte("#!/bin/sh\n[ \"$1\" = -version ] && echo 'goCOP 0.0.40-alfa' && exit 0\necho 'ne mogu'; exit 3\n")
	if err := p.Instaliraj(context.Background(), Opcije{IzMape: mapaIzdanja(t, program, tajni, false),
		ImeCvora: "pperic-mac", Prvi: PrviPostojeca}); err != nil {
		t.Fatal(err)
	}
	if d := procitaj(t, p.M.DnevnikPostave()); !strings.Contains(d, "Ime čvora nije upisano") || !strings.Contains(d, "ne mogu") {
		t.Errorf("dnevnik:\n%s", d)
	}
	if put := p.PrvoOtvaranje(); put != "postavljanje?put=postojeca" {
		t.Errorf("prvo otvaranje: %q", put)
	}
}

func TestInstalirajSGitHuba(t *testing.T) {
	javni, tajni := noviKljuc(t)
	ctx := context.Background()

	t.Run("prazan GitHub, ništa instalirano", func(t *testing.T) {
		p := postavaZaInstalaciju(t, javni)
		p.Ugradnja.Izdanja = noviGitHub(t).izdanjaZa()
		if err := p.Instaliraj(ctx, Opcije{}); err == nil || !strings.Contains(err.Error(), "još nema potpisanog izdanja") {
			t.Errorf("greška %v", err)
		}
	})
	t.Run("GitHub nedostupan, ništa instalirano", func(t *testing.T) {
		p := postavaZaInstalaciju(t, javni)
		g := noviGitHub(t)
		p.Ugradnja.Izdanja = g.izdanjaZa()
		g.srv.Close()
		if err := p.Instaliraj(ctx, Opcije{}); err == nil || !strings.Contains(err.Error(), "ne može se preuzeti") {
			t.Errorf("greška %v", err)
		}
	})
	t.Run("podmetnut program", func(t *testing.T) {
		p := postavaZaInstalaciju(t, javni)
		g := noviGitHub(t)
		g.dodaj("v0.0.40-alfa", skriptaCvora("0.0.40-alfa"), tajni, true)
		p.Ugradnja.Izdanja = g.izdanjaZa()
		if err := p.Instaliraj(ctx, Opcije{}); err == nil || !strings.Contains(err.Error(), "SHA-256") {
			t.Errorf("greška %v", err)
		}
		if p.Cvor.Instaliran() {
			t.Error("podmetnut program je instaliran")
		}
	})
	t.Run("instalacija pa bez promjene", func(t *testing.T) {
		p := postavaZaInstalaciju(t, javni)
		g := noviGitHub(t)
		g.dodaj("v0.0.40-alfa", skriptaCvora("0.0.40-alfa"), tajni, false)
		p.Ugradnja.Izdanja = g.izdanjaZa()
		if err := p.Instaliraj(ctx, Opcije{}); err != nil {
			t.Fatal(err)
		}
		if izd, _ := IzdanjeDatoteke(ctx, p.M.Gocop()); izd != "0.0.40-alfa" {
			t.Fatalf("instalirano %q", izd)
		}
		// isto izdanje: ne preuzima se ponovno
		fi, _ := os.Stat(p.M.Gocop())
		if err := p.Instaliraj(ctx, Opcije{}); err != nil {
			t.Fatal(err)
		}
		if fi2, _ := os.Stat(p.M.Gocop()); !os.SameFile(fi, fi2) {
			t.Error("isto izdanje je ponovno postavljeno")
		}
		// GitHub bez izdanja ili nedostupan: instalirano ostaje
		g.mu.Lock()
		g.izdanja = nil
		g.mu.Unlock()
		if err := p.Instaliraj(ctx, Opcije{}); err != nil {
			t.Errorf("bez izdanja na GitHubu: %v", err)
		}
		g.srv.Close()
		if err := p.Instaliraj(ctx, Opcije{}); err != nil {
			t.Errorf("nedostupan GitHub uz instalirano: %v", err)
		}
		if d := procitaj(t, p.M.DnevnikPostave()); !strings.Contains(d, "ostaje 0.0.40-alfa") {
			t.Errorf("dnevnik:\n%s", d)
		}
	})
}

func TestUkloniMicePrijavuIPut(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	m := MjestaU(t.TempDir())
	if err := DodajUPut(m.Program); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(m.Postava, "gocop-postava")
	if err := UkljuciPriPrijavi(exe); err != nil {
		t.Fatal(err)
	}
	if err := Ukloni(m); err != nil {
		t.Fatal(err)
	}
	if UPutu(m.Program) {
		t.Error("program je ostao u PATH-u")
	}
	if uklj, _ := PriPrijavi(exe); uklj {
		t.Error("pokretanje pri prijavi je ostalo")
	}
	// poveznica druge instalacije se ne dira
	druga := MjestaU(t.TempDir())
	if err := DodajUPut(druga.Program); err != nil {
		t.Fatal(err)
	}
	if err := Ukloni(m); err != nil {
		t.Fatal(err)
	}
	if !UPutu(druga.Program) {
		t.Error("uklanjanje je maknulo poveznicu druge instalacije")
	}
}

// nepostojeciPID je iznad najvećeg PID-a i na macOS-u (99 999) i na Linuxu
// (4 194 304), pa ga nijedan proces ne može dobiti: signal na njega ne
// stigne nikome
const nepostojeciPID = 2_000_000_000

func TestZaustaviPostavuKadNeRadi(t *testing.T) {
	m := MjestaU(t.TempDir())
	if err := ZaustaviPostavu(m, time.Second); err != nil {
		t.Errorf("bez brave: %v", err)
	}
	// brava postoji, ali je nitko ne drži: zastarjeli PID se ne dira
	zapisi(t, filepath.Join(m.Baza, "postava.lock"), "")
	zapisi(t, m.PIDPostave(), "abc")
	if err := ZaustaviPostavu(m, time.Second); err != nil {
		t.Errorf("slobodna brava: %v", err)
	}
	if err := Zaustavi(m); err != nil {
		t.Errorf("zaustavljanje bez Postave i čvora: %v", err)
	}
}

func TestZaustaviPostavuKojaRadi(t *testing.T) {
	m := MjestaU(t.TempDir())
	otkljucaj, vec, err := Zakljucaj(m)
	if err != nil || vec {
		t.Fatalf("brava: %v %v", vec, err)
	}
	defer otkljucaj()

	// neispravan PID uz zauzetu bravu je greška
	zapisi(t, m.PIDPostave(), "abc")
	if err := ZaustaviPostavu(m, time.Second); err == nil || !strings.Contains(err.Error(), "neispravan PID") {
		t.Errorf("neispravan PID: %v", err)
	}
	// proces kojeg više nema: signal ne prolazi, nema se što čekati
	zapisi(t, m.PIDPostave(), fmt.Sprint(nepostojeciPID))
	if err := ZaustaviPostavu(m, time.Second); err != nil {
		t.Errorf("mrtvi proces: %v", err)
	}
	// živa „Postava” dobije SIGTERM i izađe
	spava := exec.Command("sleep", "30")
	if err := spava.Start(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	var izlaz error
	go func() {
		defer wg.Done()
		izlaz = spava.Wait()
	}()
	zapisi(t, m.PIDPostave(), fmt.Sprint(spava.Process.Pid))
	if err := ZaustaviPostavu(m, 20*time.Second); err != nil {
		t.Errorf("zaustavljanje: %v", err)
	}
	wg.Wait()
	if izlaz == nil || !strings.Contains(izlaz.Error(), "terminated") {
		t.Errorf("proces nije ugašen signalom: %v", izlaz)
	}
}

func TestZaustaviCekaCvor(t *testing.T) {
	m := MjestaU(t.TempDir())
	zapisi(t, m.PIDCvora(), fmt.Sprint(nepostojeciPID))
	if err := Zaustavi(m); err != nil {
		t.Errorf("čvor koji je već izašao: %v", err)
	}
}

func TestCekajIzlazak(t *testing.T) {
	if !CekajIzlazak(nepostojeciPID, time.Second) {
		t.Error("proces koji je izašao javljen kao živ")
	}
	// rok dulji od jednog kruga, pa se živ proces doista i pita
	if CekajIzlazak(os.Getpid(), 250*time.Millisecond) {
		t.Error("živ proces javljen kao izašao")
	}
}

func TestAppleTekstIDeinstaliraj(t *testing.T) {
	if s := appleTekst(`C:\put "s navodnicima"`); s != `C:\\put \"s navodnicima\"` {
		t.Errorf("appleTekst: %s", s)
	}
	if err := Deinstaliraj(); err == nil || !strings.Contains(err.Error(), "ručno") {
		t.Errorf("Deinstaliraj: %v", err)
	}
	bezProzora(exec.Command("true")) // na Macu i Linuxu ništa ne radi
}

// sigurniDnevnik skuplja retke iz više gorutina
type sigurniDnevnik struct {
	mu    sync.Mutex
	retci []string
}

func (d *sigurniDnevnik) pisi(f string, a ...any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.retci = append(d.retci, fmt.Sprintf(f, a...))
}

func (d *sigurniDnevnik) sadrzi(s string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return strings.Contains(strings.Join(d.retci, "\n"), s)
}

// Čvor koji ne izađe kad mu se zatvori ulaz prekida se nakon roka.
func TestCvorZaustaviPrekidaNakonRoka(t *testing.T) {
	m := MjestaU(t.TempDir())
	if err := os.MkdirAll(m.Program, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.Gocop(), skriptaCvora("0.0.40-alfa"), 0o755); err != nil {
		t.Fatal(err)
	}
	var d sigurniDnevnik
	c := NoviCvor(m, d.pisi)
	if err := c.Pokreni(); err != nil {
		t.Fatal(err)
	}
	if err := c.Pokreni(); err != nil { // već radi: ništa
		t.Fatal(err)
	}
	if !c.Proces().Radi {
		t.Fatal("čvor ne radi")
	}
	pid, err := citajPID(m.PIDCvora())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Zaustavi(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if !d.sadrzi("nije ugasio") {
		t.Errorf("dnevnik: %v", d.retci)
	}
	if c.Proces().Radi || c.Proces().Zeljeno {
		t.Errorf("stanje nakon zaustavljanja: %+v", c.Proces())
	}
	if !CekajIzlazak(pid, 5*time.Second) {
		t.Error("proces čvora je ostao živ")
	}
	if err := c.Zaustavi(time.Second); err != nil { // već stoji
		t.Error(err)
	}
}

func TestCvorPokreniGreske(t *testing.T) {
	m := MjestaU(t.TempDir())
	c := NoviCvor(m, nil)
	if err := c.Pokreni(); err == nil || !strings.Contains(err.Error(), "nije instaliran") {
		t.Errorf("bez programa: %v", err)
	}
	zapisi(t, m.Gocop(), "nije program") // bez prava pokretanja
	if err := c.Pokreni(); err == nil || !strings.Contains(err.Error(), "pokretanje čvora") {
		t.Errorf("program koji se ne da pokrenuti: %v", err)
	}
	if c.Proces().Radi {
		t.Error("čvor javljen kao da radi")
	}
	m2 := MjestaU(t.TempDir())
	zapisi(t, m2.Gocop(), "x")
	zapisi(t, m2.Podaci, "datoteka umjesto mape")
	if err := NoviCvor(m2, nil).Pokreni(); err == nil {
		t.Error("pokretanje bez mape podataka")
	}
}
