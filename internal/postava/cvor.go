package postava

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"

	"gocop/internal/izdanje"
)

// Upravljac pali, gasi i nadzire čvor. Cvor ga pokreće kao dijete Postave,
// a Usluga preko systemd-a, kad je čvor na Linuxu usluga sustava.
type Upravljac interface {
	Instaliran() bool
	Pokreni() error
	Zaustavi(rok time.Duration) error
	Proces() StanjeProcesa
	Zdravlje(ctx context.Context) (izdanje string, port int)
	NaPromjenu(f func())
}

// Cvor pali i gasi gocop kao dijete Postave i nadzire ga
type Cvor struct {
	m       Mjesta
	pisi    func(format string, a ...any)
	klijent *http.Client

	mu       sync.Mutex
	cmd      *exec.Cmd
	ulaz     io.WriteCloser
	gotov    chan struct{}
	zeljeno  bool        // korisnik želi da čvor radi
	pokrenut time.Time   // kad je zadnji put pokrenut
	padovi   []time.Time // nezatraženi izlasci, za odustajanje
	pao      bool        // previše padova: čeka korisnika
	kljuc    string      // ključ za potpis dopuštenja, nov pri svakom pokretanju
	promjena func()
}

// Najviše padova u prozoru prije nego Postava odustane i javi
const (
	najvisePadova = 3
	prozorPadova  = 10 * time.Minute
)

// razmakPada je razmak prije prvog ponovnog pokretanja (pa dvostruki…);
// varijabla samo zato da ga test skrati
var razmakPada = 5 * time.Second

// NoviCvor priprema nadzor; čvor se ne pali dok se ne pozove Pokreni
func NoviCvor(m Mjesta, pisi func(string, ...any)) *Cvor {
	if pisi == nil {
		pisi = func(string, ...any) {}
	}
	return &Cvor{m: m, pisi: pisi, klijent: &http.Client{Timeout: 3 * time.Second}}
}

// NaPromjenu zadaje što pozvati kad čvor stane ili krene
func (c *Cvor) NaPromjenu(f func()) {
	c.mu.Lock()
	c.promjena = f
	c.mu.Unlock()
}

func (c *Cvor) javi() {
	c.mu.Lock()
	f := c.promjena
	c.mu.Unlock()
	if f != nil {
		go f()
	}
}

// Instaliran javlja postoji li program čvora
func (c *Cvor) Instaliran() bool {
	_, err := os.Stat(c.m.Gocop())
	return err == nil
}

// Pokreni pali čvor ako već ne radi
func (c *Cvor) Pokreni() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.zeljeno = true
	c.pao = false
	c.padovi = nil
	if c.cmd != nil {
		return nil
	}
	return c.pokreniZakljucano()
}

func (c *Cvor) pokreniZakljucano() error {
	if _, err := os.Stat(c.m.Gocop()); err != nil {
		return fmt.Errorf("goCOP nije instaliran (%s)", c.m.Gocop())
	}
	if err := os.MkdirAll(c.m.Podaci, 0o755); err != nil {
		return err
	}
	zarotiraj(c.m.DnevnikCvora(), 10<<20)
	dnevnik, err := os.OpenFile(c.m.DnevnikCvora(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	kljuc := rand.Text()
	cmd := exec.Command(c.m.Gocop(), "-upravitelj", "-db", c.m.BazaCvora())
	cmd.Dir = c.m.Podaci
	cmd.Env = append(os.Environ(), izdanje.OkolinaKljucaPostave+"="+kljuc)
	cmd.Stdout, cmd.Stderr = dnevnik, dnevnik
	bezProzora(cmd)
	ulaz, err := cmd.StdinPipe()
	if err != nil {
		dnevnik.Close()
		return err
	}
	if err := cmd.Start(); err != nil {
		dnevnik.Close()
		return fmt.Errorf("pokretanje čvora: %w", err)
	}
	gotov := make(chan struct{})
	c.cmd, c.ulaz, c.gotov, c.pokrenut, c.kljuc = cmd, ulaz, gotov, time.Now(), kljuc
	_ = os.WriteFile(c.m.PIDCvora(), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
	c.pisi("Čvor pokrenut (pid %d)", cmd.Process.Pid)
	go c.cekaj(cmd, gotov, dnevnik)
	go c.javi()
	return nil
}

// cekaj prati izlazak; nezatražen izlazak podiže čvor ponovno, uz rastući
// razmak, a nakon tri pada u deset minuta odustaje i čeka korisnika
func (c *Cvor) cekaj(cmd *exec.Cmd, gotov chan struct{}, dnevnik *os.File) {
	err := cmd.Wait()
	dnevnik.Close()
	c.mu.Lock()
	if c.cmd == cmd {
		c.cmd, c.ulaz = nil, nil
	}
	close(gotov)
	_ = os.Remove(c.m.PIDCvora())
	zeljeno := c.zeljeno
	var razmak time.Duration
	if zeljeno {
		sad := time.Now()
		var svjezi []time.Time
		for _, t := range c.padovi {
			if sad.Sub(t) < prozorPadova {
				svjezi = append(svjezi, t)
			}
		}
		c.padovi = append(svjezi, sad)
		if len(c.padovi) >= najvisePadova {
			c.pao = true
		} else {
			razmak = time.Duration(len(c.padovi)) * razmakPada
		}
	}
	pao := c.pao
	c.mu.Unlock()

	switch {
	case !zeljeno:
		c.pisi("Čvor zaustavljen")
	case pao:
		c.pisi("Čvor je pao tri puta u deset minuta (%v); ne pokrećem ga dok korisnik ne kaže", err)
	default:
		c.pisi("Čvor je neočekivano izašao (%v); ponovno za %s", err, razmak)
	}
	c.javi()
	if zeljeno && !pao {
		time.Sleep(razmak)
		c.mu.Lock()
		if c.zeljeno && c.cmd == nil && !c.pao {
			if err := c.pokreniZakljucano(); err != nil {
				c.pisi("Ponovno pokretanje: %v", err)
			}
		}
		c.mu.Unlock()
	}
}

// Zaustavi uredno gasi čvor: zatvori mu ulaz i čeka; nakon roka ga ubija
func (c *Cvor) Zaustavi(rok time.Duration) error {
	c.mu.Lock()
	c.zeljeno = false
	cmd, ulaz, gotov := c.cmd, c.ulaz, c.gotov
	c.mu.Unlock()
	if cmd == nil {
		return nil
	}
	_ = ulaz.Close()
	select {
	case <-gotov:
		return nil
	case <-time.After(rok):
	}
	c.pisi("Čvor se nije ugasio za %s; prekidam ga", rok)
	_ = cmd.Process.Kill()
	select {
	case <-gotov:
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("čvor se ne da zaustaviti")
	}
}

// Stanje procesa, bez pitanja čvora
type StanjeProcesa struct {
	Radi     bool      // proces postoji
	Zeljeno  bool      // korisnik želi da radi
	Pao      bool      // Postava je odustala nakon padova
	Pokrenut time.Time // kad je zadnji put pokrenut
}

// Proces vraća stanje procesa
func (c *Cvor) Proces() StanjeProcesa {
	c.mu.Lock()
	defer c.mu.Unlock()
	return StanjeProcesa{Radi: c.cmd != nil, Zeljeno: c.zeljeno, Pao: c.pao, Pokrenut: c.pokrenut}
}

// Zdravlje pita čvor koje je izdanje i radi li; prazno izdanje znači da ne
// odgovara. Pokušava port iz gocop.toml; uz zadani 80 i 8080, na koji čvor
// sam prijeđe kad je 80 zauzet.
func (c *Cvor) Zdravlje(ctx context.Context) (izdanje string, port int) {
	o := c.PitajZdravlje(ctx)
	return o.Izdanje, o.Port
}

// PitajZdravlje je Zdravlje s cijelim odgovorom. Dopušteno starije izdanje
// ostaje samo kad ga je čvor potpisao ključem koji mu je Postava dala pri
// pokretanju: drugi program na istom portu (npr. na rezervnom 8080) taj
// ključ ne zna.
func (c *Cvor) PitajZdravlje(ctx context.Context) OdgovorZdravlja {
	o := pitajZdravlje(ctx, c.klijent, portoviIz(c.m.PostavkeCvora()))
	c.mu.Lock()
	kljuc := c.kljuc
	c.mu.Unlock()
	return uzDopustenje(o, potpisano(o, kljuc), time.Now())
}

// OdgovorZdravlja je ono što čvor javi na GET /zdravlje (docs/postava.md);
// prazno izdanje znači da ne odgovara
type OdgovorZdravlja struct {
	Izdanje string
	Port    int
	// Dopusteno je starije izdanje koje je globalni administrator na čvoru
	// dopustio postaviti, dok dopuštenje vrijedi. Čvor ga šalje od
	// 0.0.47-alfa; stariji ga ne šalje, pa je prazno.
	Dopusteno string
	Do        time.Time // rok dopuštenja
	potpis    string    // potpis dopuštenja (izdanje.PotpisDopustenja)
}

// potpisano: dopuštenje je potpisao vlastiti čvor, ključem koji mu je
// Postava dala pri pokretanju
func potpisano(o OdgovorZdravlja, kljuc string) bool {
	ocekivan := izdanje.PotpisDopustenja(kljuc, o.Izdanje, o.Dopusteno, o.Do.Unix())
	return kljuc != "" && hmac.Equal([]byte(o.potpis), []byte(ocekivan))
}

// uzDopustenje ostavlja dopuštenje u odgovoru samo kad mu se vjeruje i rok
// mu još nije prošao; inače je odgovor bez dopuštenja
func uzDopustenje(o OdgovorZdravlja, vjeruje bool, sad time.Time) OdgovorZdravlja {
	if o.Dopusteno != "" && (!vjeruje || !sad.Before(o.Do)) {
		o.Dopusteno, o.Do, o.potpis = "", time.Time{}, ""
	}
	return o
}

// zdravljeIDopustenje: upravljač koji javlja cijeli odgovor na /zdravlje
// (Cvor i Usluga). Upravljač bez njega javlja samo izdanje, pa Postava uz
// njega starije izdanje ne postavlja.
type zdravljeIDopustenje interface {
	PitajZdravlje(ctx context.Context) OdgovorZdravlja
}

// pitajZdravlje pita GET /zdravlje na 127.0.0.1, redom po portovima
func pitajZdravlje(ctx context.Context, klijent *http.Client, portovi []int) OdgovorZdravlja {
	for _, p := range portovi {
		if z, ok := jedanZdravlje(ctx, klijent, p); ok {
			o := OdgovorZdravlja{Izdanje: z.Izdanje, Port: p, Dopusteno: z.Dopusteno, potpis: z.Potpis}
			if z.Do > 0 {
				o.Do = time.Unix(z.Do, 0)
			}
			return o
		}
	}
	return OdgovorZdravlja{}
}

// jsonZdravlja je tijelo odgovora na /zdravlje; ostala polja se ne čitaju
type jsonZdravlja struct {
	Izdanje   string `json:"izdanje"`
	Radi      bool   `json:"radi"`
	Dopusteno string `json:"dopusteno_izdanje"`
	Do        int64  `json:"dopusteno_do"`
	Potpis    string `json:"potpis_dopustenja"`
}

// jedanZdravlje pita /zdravlje na jednom portu; ok kad čvor radi i javlja
// izdanje
func jedanZdravlje(ctx context.Context, klijent *http.Client, port int) (z jsonZdravlja, ok bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/zdravlje", port), nil)
	if err != nil {
		return z, false
	}
	odg, err := klijent.Do(req)
	if err != nil {
		return z, false
	}
	defer odg.Body.Close()
	err = json.NewDecoder(io.LimitReader(odg.Body, 4096)).Decode(&z)
	return z, err == nil && odg.StatusCode == http.StatusOK && z.Radi && z.Izdanje != ""
}

// portoviIz su port iz gocop.toml (zadano 80), uz 80 i rezervni 8080. Kad
// se datoteka ne može pročitati (usluga sustava: /var/lib/gocop je samo
// korisnika gocop), vrijede zadani.
func portoviIz(postavke string) []int {
	port := 80
	if b, err := os.ReadFile(postavke); err == nil {
		var p struct {
			Addr string `toml:"addr"`
		}
		if toml.Unmarshal(b, &p) == nil && p.Addr != "" {
			if _, s, err := net.SplitHostPort(strings.TrimSpace(p.Addr)); err == nil {
				if n, err := strconv.Atoi(s); err == nil && n > 0 {
					port = n
				}
			}
		}
	}
	if port == 80 {
		// samo uz zadani :80 čvor sam prelazi na :8080 kad je 80 zauzet
		return []int{80, 8080}
	}
	return []int{port}
}

// Adresa ploče u pregledniku
func AdresaPloce(port int) string {
	if port == 80 || port == 0 {
		return "http://localhost/"
	}
	return fmt.Sprintf("http://localhost:%d/", port)
}

// IzdanjeDatoteke pita program za izdanje (gocop -version → "goCOP 0.0.28-alfa")
func IzdanjeDatoteke(ctx context.Context, put string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, put, "-version")
	bezProzora(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s -version: %w", put, err)
	}
	redak := strings.TrimSpace(string(out))
	izd, ok := strings.CutPrefix(redak, "goCOP ")
	if !ok || strings.ContainsAny(izd, " \n") {
		return "", fmt.Errorf("%s -version ispisuje %q, a ne \"goCOP <izdanje>\"", put, redak)
	}
	return izd, nil
}

// zarotiraj premješta dnevnik veći od granice u .1 (jedna stara kopija)
func zarotiraj(put string, granica int64) {
	if fi, err := os.Stat(put); err == nil && fi.Size() > granica {
		_ = os.Rename(put, put+".1")
	}
}
