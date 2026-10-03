package postava

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocop/internal/izdanje"
)

// StanjeCvora je ono što ikona u traci pokazuje
type StanjeCvora int

const (
	NijeInstaliran StanjeCvora = iota
	Zaustavljen
	Pokrece    // proces radi, još ne odgovara
	Radi       // odgovara na /zdravlje
	NeOdgovara // proces radi, a dulje od 90 s ne odgovara
	Pao        // pao je tri puta; čeka korisnika
)

// Stanje je slika za sučelje
type Stanje struct {
	Cvor            StanjeCvora
	Izdanje         string // izdanje čvora koji radi
	Port            int
	Novije          *Izdanje // novije izdanje goCOP-a, ako postoji
	NovijaPostava   string
	StranicaPostave string
	Zauzeto         string // posao u tijeku (nadogradnja…)
	GreskaProvjere  string
	ZadnjaProvjera  time.Time
}

// Postava povezuje nadzor čvora, provjeru izdanja i ugradnju
type Postava struct {
	M        Mjesta
	Exe      string // ova Postava
	Verzija  string // izdanje Postave
	Cvor     *Cvor
	Ugradnja Ugradnja
	dnevnik  *log.Logger

	mu       sync.Mutex
	ponuda   Ponuda
	zadnja   time.Time
	greska   string
	zauzeto  string
	izdanje  string
	port     int
	promjena func()

	// izdanje programa na disku, dok čvor ne radi; pita se ponovno tek kad
	// se datoteka promijeni
	izdDatoteke    string
	izdDatotekeKad time.Time
}

// Nova priprema Postavu za instalaciju u kojoj stoji exe
func Nova(exe, verzija string) *Postava {
	m := OdrediMjesta(exe)
	p := &Postava{M: m, Exe: exe, Verzija: verzija}
	_ = os.MkdirAll(m.Podaci, 0o755)
	zarotiraj(m.DnevnikPostave(), 2<<20)
	if f, err := os.OpenFile(m.DnevnikPostave(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		p.dnevnik = log.New(f, "", log.LstdFlags)
	} else {
		p.dnevnik = log.New(os.Stderr, "", log.LstdFlags)
	}
	p.Cvor = NoviCvor(m, p.Pisi)
	p.Cvor.NaPromjenu(p.javi)
	p.Ugradnja = Ugradnja{
		M: m, Cvor: p.Cvor, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Pisi: p.Pisi,
		Izdanja: Izdanja{
			API: "https://api.github.com", Repo: izdanje.Repozitorij,
			Klijent: &http.Client{Timeout: 10 * time.Minute},
			Agent:   "goCOP-Postava/" + verzija,
		},
	}
	return p
}

// Pisi bilježi u data/postava.log
func (p *Postava) Pisi(f string, a ...any) { p.dnevnik.Printf(f, a...) }

// NaPromjenu zadaje što pozvati kad se stanje promijeni (osvježi traku)
func (p *Postava) NaPromjenu(f func()) {
	p.mu.Lock()
	p.promjena = f
	p.mu.Unlock()
}

func (p *Postava) javi() {
	p.mu.Lock()
	f := p.promjena
	p.mu.Unlock()
	if f != nil {
		f()
	}
}

// Stanje vraća sliku za sučelje
func (p *Postava) Stanje() Stanje {
	proc := p.Cvor.Proces()
	p.mu.Lock()
	defer p.mu.Unlock()
	s := Stanje{Izdanje: p.izdanje, Port: p.port, Zauzeto: p.zauzeto, GreskaProvjere: p.greska,
		ZadnjaProvjera: p.zadnja, NovijaPostava: p.ponuda.Postava, StranicaPostave: p.ponuda.PostavaStranica}
	switch {
	case !p.Cvor.Instaliran():
		s.Cvor = NijeInstaliran
	case proc.Pao:
		s.Cvor = Pao
	case !proc.Radi:
		s.Cvor = Zaustavljen
	case p.izdanje != "":
		s.Cvor = Radi
	case time.Since(proc.Pokrenut) > rokZdravlja:
		s.Cvor = NeOdgovara
	default:
		s.Cvor = Pokrece
	}
	if g := p.ponuda.GoCOP; g != nil {
		trenutno := p.izdanje
		if trenutno == "" {
			trenutno = p.izdanjeDatotekeZakljucano()
		}
		if v, ok := izdanje.ParsirajOznaku(trenutno); !ok || g.Verzija.Usporedi(v) > 0 {
			s.Novije = g
		}
	}
	return s
}

// izdanjeDatotekeZakljucano pita gocop -version samo kad se program
// promijenio (poziva se uz p.mu)
func (p *Postava) izdanjeDatotekeZakljucano() string {
	fi, err := os.Stat(p.M.Gocop())
	if err != nil {
		return ""
	}
	if !fi.ModTime().Equal(p.izdDatotekeKad) {
		p.izdDatoteke, _ = IzdanjeDatoteke(context.Background(), p.M.Gocop())
		p.izdDatotekeKad = fi.ModTime()
	}
	return p.izdDatoteke
}

// Prati osvježava stanje čvora svakih nekoliko sekundi i traži izdanja
// pri pokretanju i svakih šest sati, dok ctx traje
func (p *Postava) Prati(ctx context.Context) {
	zdravlje := time.NewTicker(3 * time.Second)
	defer zdravlje.Stop()
	izdanja := time.NewTimer(10 * time.Second)
	defer izdanja.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-zdravlje.C:
			p.osvjeziZdravlje(ctx)
		case <-izdanja.C:
			if err := p.Provjeri(ctx); err != nil {
				p.Pisi("Provjera izdanja: %v", err)
			}
			izdanja.Reset(6 * time.Hour)
		}
	}
}

func (p *Postava) osvjeziZdravlje(ctx context.Context) {
	izd, port := "", 0
	if p.Cvor.Proces().Radi {
		c, cancel := context.WithTimeout(ctx, 3*time.Second)
		izd, port = p.Cvor.Zdravlje(c)
		cancel()
	}
	p.mu.Lock()
	promijenjeno := izd != p.izdanje || port != p.port
	p.izdanje, p.port = izd, port
	p.mu.Unlock()
	if promijenjeno {
		p.javi()
	}
}

// Provjeri pita GitHub ima li novijeg izdanja
func (p *Postava) Provjeri(ctx context.Context) error {
	ponuda, err := p.Ugradnja.Izdanja.Provjeri(ctx, runtime.GOOS, runtime.GOARCH, p.Verzija)
	p.mu.Lock()
	p.zadnja = time.Now()
	if err != nil {
		p.greska = err.Error()
	} else {
		p.greska = ""
		p.ponuda = ponuda
	}
	p.mu.Unlock()
	p.javi()
	return err
}

func (p *Postava) zauzmi(posao string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.zauzeto != "" {
		return fmt.Errorf("već je u tijeku: %s", p.zauzeto)
	}
	p.zauzeto = posao
	go p.javi()
	return nil
}

func (p *Postava) oslobodi() {
	p.mu.Lock()
	p.zauzeto = ""
	p.mu.Unlock()
	p.javi()
}

// Nadogradi ugrađuje najnovije izdanje koje je provjera našla
func (p *Postava) Nadogradi(ctx context.Context) (string, error) {
	s := p.Stanje()
	if s.Novije == nil {
		return "", errors.New("nema novijeg izdanja")
	}
	iz := *s.Novije
	if err := p.zauzmi("Nadogradnja na " + iz.Verzija.String()); err != nil {
		return "", err
	}
	defer p.oslobodi()
	p.Pisi("Nadogradnja na %s (%s)", iz.Oznaka, iz.Stranica)
	novi, err := p.Ugradnja.Pripremi(ctx, iz)
	if err != nil {
		p.Pisi("Nadogradnja odbijena: %v", err)
		return "", err
	}
	if !p.Cvor.Instaliran() {
		return iz.Verzija.String(), p.Ugradnja.Postavi(novi)
	}
	staro := s.Izdanje
	if staro == "" {
		staro, _ = IzdanjeDatoteke(ctx, p.M.Gocop())
	}
	if err := p.Ugradnja.Ugradi(ctx, novi, staro, iz.Verzija.String()); err != nil {
		p.Pisi("Nadogradnja: %v", err)
		return "", err
	}
	return iz.Verzija.String(), nil
}

// Opcije instalacije (instalacijski program ih predaje Postavi)
type Opcije struct {
	PriPrijavi bool
	IzMape     string // izdanje bez interneta: mapa s programom, SHA256SUMS i .sig
}

// Instaliraj priprema instalaciju: preuzme najnovije izdanje goCOP-a (ili
// ga uzme iz mape), stavi program u PATH i, ako je izabrano, Postavu u
// pokretanje pri prijavi. Postojeći noviji ili isti program se ne dira.
func (p *Postava) Instaliraj(ctx context.Context, o Opcije) error {
	for _, d := range []string{p.M.Program, p.M.Podaci} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	trenutno, _ := IzdanjeDatoteke(ctx, p.M.Gocop())
	tv, imaTrenutno := izdanje.ParsirajOznaku(trenutno)
	switch {
	case o.IzMape != "":
		novi, v, err := p.Ugradnja.PripremiIzMape(ctx, o.IzMape)
		if err != nil {
			return err
		}
		if !imaTrenutno || v.Usporedi(tv) > 0 {
			if err := p.Ugradnja.Postavi(novi); err != nil {
				return err
			}
			p.Pisi("Instaliran goCOP %s iz %s", v, o.IzMape)
		}
	default:
		ponuda, err := p.Ugradnja.Izdanja.Provjeri(ctx, runtime.GOOS, runtime.GOARCH, p.Verzija)
		if err != nil {
			if imaTrenutno {
				p.Pisi("Provjera izdanja pri instalaciji: %v; ostaje %s", err, trenutno)
				break
			}
			return fmt.Errorf("najnovije izdanje goCOP-a ne može se preuzeti: %w", err)
		}
		if ponuda.GoCOP == nil {
			if imaTrenutno {
				break
			}
			return errors.New("na GitHubu još nema potpisanog izdanja goCOP-a za ovaj sustav")
		}
		if imaTrenutno && ponuda.GoCOP.Verzija.Usporedi(tv) <= 0 {
			break
		}
		novi, err := p.Ugradnja.Pripremi(ctx, *ponuda.GoCOP)
		if err != nil {
			return err
		}
		if err := p.Ugradnja.Postavi(novi); err != nil {
			return err
		}
		p.Pisi("Instaliran goCOP %s", ponuda.GoCOP.Verzija)
	}
	if err := DodajUPut(p.M.Program); err != nil {
		p.Pisi("PATH: %v", err)
	}
	if o.PriPrijavi {
		if err := UkljuciPriPrijavi(p.Exe); err != nil {
			return fmt.Errorf("pokretanje pri prijavi: %w", err)
		}
	} else if err := IskljuciPriPrijavi(); err != nil {
		return err
	}
	return nil
}

// Ukloni priprema deinstalaciju: ugasi Postavu i čvor, makne unos za
// pokretanje pri prijavi i mapu programa iz PATH-a. Datoteke briše
// instalacijski program; podaci ostaju.
func Ukloni(m Mjesta) error {
	if err := Zaustavi(m); err != nil {
		return err
	}
	var greske []string
	if err := IskljuciPriPrijavi(); err != nil {
		greske = append(greske, err.Error())
	}
	if err := MakniIzPuta(m.Program); err != nil {
		greske = append(greske, err.Error())
	}
	if len(greske) > 0 {
		return errors.New(strings.Join(greske, "; "))
	}
	return nil
}

// Zaustavi gasi Postavu koja radi i čeka da se čvor ugasi (instalacijski
// program to traži prije zamjene datoteka)
func Zaustavi(m Mjesta) error {
	if err := ZaustaviPostavu(m, 40*time.Second); err != nil {
		return err
	}
	if pid, err := citajPID(m.PIDCvora()); err == nil {
		if !CekajIzlazak(pid, 40*time.Second) {
			return fmt.Errorf("čvor (pid %d) se nije ugasio", pid)
		}
	}
	return nil
}

func citajPID(put string) (int, error) {
	b, err := os.ReadFile(put)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("neispravan PID u %s", put)
	}
	return pid, nil
}
