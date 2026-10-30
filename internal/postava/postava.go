package postava

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocop/internal/imecvora"
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
	Starije         string   // starije izdanje koje je globalni administrator dopustio na čvoru, dok vrijedi
	NovijaPostava   string
	StranicaPostave string
	Zauzeto         string // posao u tijeku (nadogradnja…)
	GreskaProvjere  string
	ZadnjaProvjera  time.Time
	UslugaSustava   bool // čvor je usluga sustava (Linux), ne dijete Postave
}

// Postava povezuje nadzor čvora, provjeru izdanja i ugradnju
type Postava struct {
	M        Mjesta
	Exe      string // ova Postava
	Verzija  string // izdanje Postave
	Cvor     Upravljac
	Ugradnja Ugradnja
	dnevnik  *log.Logger
	usluga   *Usluga // kad je čvor usluga sustava; inače nil i Cvor je *Cvor

	mu       sync.Mutex
	ponuda   Ponuda
	zadnja   time.Time
	greska   string
	zauzeto  string
	izdanje  string
	port     int
	starije  string // dopušteno starije izdanje iz zadnjeg /zdravlje
	promjena func()

	// izdanje programa na disku; pita se ponovno kad se datoteka promijeni,
	// a kad program nije odgovorio, najranije nakon minute
	izdDatoteke       string
	izdDatotekeKad    time.Time
	izdDatotekePitano time.Time
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
	if imaUsluge() {
		p.KaoUsluga(NovaUsluga())
	}
	return p
}

// KaoUsluga: čvor je usluga sustava, pa njime upravlja systemd, a ne
// Postava. Vlastiti čvor se ne pokreće ni ne preuzima; program je onaj
// usluge (/usr/local/bin/gocop), a dnevnik, brava i oznake Postave ostaju
// u korisnikovoj mapi.
func (p *Postava) KaoUsluga(u *Usluga) {
	p.usluga, p.Cvor = u, u
	p.M.Program = filepath.Dir(u.Program)
	p.Ugradnja.M, p.Ugradnja.Cvor = p.M, u
	u.NaPromjenu(p.javi)
	p.Pisi("goCOP radi kao usluga sustava (%s); Postava ne pokreće vlastiti čvor", u.Ime)
}

// UslugaSustava javlja je li čvor usluga sustava
func (p *Postava) UslugaSustava() bool { return p.usluga != nil }

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
	instaliran := p.Cvor.Instaliran()
	s := Stanje{Izdanje: p.izdanje, Port: p.port, Zauzeto: p.zauzeto, GreskaProvjere: p.greska,
		ZadnjaProvjera: p.zadnja, NovijaPostava: p.ponuda.Postava, StranicaPostave: p.ponuda.PostavaStranica,
		UslugaSustava: p.usluga != nil}
	switch {
	case !instaliran:
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
	p.ponudeZakljucano(&s, instaliran)
	return s
}

// ponudeZakljucano: novije izdanje s GitHuba i dopušteno starije, svako
// samo prema izdanju koje je sada postavljeno (poziva se uz p.mu)
func (p *Postava) ponudeZakljucano(s *Stanje, instaliran bool) {
	postavljeno := veceIzdanje(p.izdanje, p.izdanjeDatotekeZakljucano())
	if g := p.ponuda.GoCOP; g != nil && ponuditi(g, postavljeno, instaliran) {
		s.Novije = g
	}
	if instaliran && ponuditiStarije(p.starije, postavljeno) {
		s.Starije = p.starije
	}
}

// starijeOd: izdanje a je starije od b; kad se ijedno ne zna, nije
func starijeOd(a, b string) bool {
	va, okA := izdanje.ParsirajOznaku(a)
	vb, okB := izdanje.ParsirajOznaku(b)
	return okA && okB && va.Usporedi(vb) < 0
}

// ponuditi: izdanje g nudi se samo kad je novije od postavljenoga. Kad se
// ne zna koje je postavljeno, nudi se samo za prvu instalaciju.
func ponuditi(g *Izdanje, trenutno string, instaliran bool) bool {
	v, ok := izdanje.ParsirajOznaku(trenutno)
	if !ok {
		return !instaliran
	}
	return g.Verzija.Usporedi(v) > 0
}

// izdanjeDatotekeZakljucano pita gocop -version kad se program promijenio,
// a kad nije odgovorio, najranije nakon minute (poziva se uz p.mu)
func (p *Postava) izdanjeDatotekeZakljucano() string {
	fi, err := os.Stat(p.M.Gocop())
	if err != nil {
		return ""
	}
	isti := fi.ModTime().Equal(p.izdDatotekeKad)
	if !isti || (p.izdDatoteke == "" && time.Since(p.izdDatotekePitano) >= time.Minute) {
		p.izdDatoteke, _ = IzdanjeDatoteke(context.Background(), p.M.Gocop())
		p.izdDatotekeKad, p.izdDatotekePitano = fi.ModTime(), time.Now()
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

// OsvjeziZdravlje odmah pita čvor za izdanje i dopuštenje, bez čekanja
// idućeg kruga praćenja (npr. prije pitanja o vraćanju)
func (p *Postava) OsvjeziZdravlje(ctx context.Context) { p.osvjeziZdravlje(ctx) }

func (p *Postava) osvjeziZdravlje(ctx context.Context) {
	var o OdgovorZdravlja
	if p.Cvor.Proces().Radi {
		o = p.pitajCvor(ctx)
	}
	p.mu.Lock()
	promijenjeno := o.Izdanje != p.izdanje || o.Port != p.port || o.Dopusteno != p.starije
	p.izdanje, p.port, p.starije = o.Izdanje, o.Port, o.Dopusteno
	p.mu.Unlock()
	if promijenjeno {
		p.javi()
	}
}

// pitajCvor pita čvor /zdravlje, najviše tri sekunde. Upravljač koji ne
// javlja cijeli odgovor javlja samo izdanje, pa dopuštenja nema.
func (p *Postava) pitajCvor(ctx context.Context) OdgovorZdravlja {
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if z, ok := p.Cvor.(zdravljeIDopustenje); ok {
		return z.PitajZdravlje(c)
	}
	izd, port := p.Cvor.Zdravlje(c)
	return OdgovorZdravlja{Izdanje: izd, Port: port}
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
		if p.usluga != nil {
			// uz uslugu Postava dolazi s izdanjem goCOP-a; izdanja postava-v*
			// su samo za Windows
			p.ponuda.Postava, p.ponuda.PostavaStranica = "", ""
		}
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
	staro, err := p.dopustenaNadogradnja(ctx, iz, s.Izdanje)
	if err != nil {
		return "", err
	}
	return p.ugradiIzdanje(ctx, iz, staro, nadogradnja)
}

// posaoUgradnje kaže ugradiIzdanje radi li nadogradnju ili vraćanje na
// starije izdanje
type posaoUgradnje struct {
	ime, odbijeno, gotovo string // za dnevnik Postave
	sPostavom             bool   // uz uslugu postavlja i Postavu iz izdanja
	// jos, kad nije nil, neposredno prije zamjene programa pita smije li se
	// izdanje još postaviti, i do kad
	jos func(context.Context) (time.Time, error)
}

// nadogradnja je posao Nadogradi: Postava iz izdanja ide s njim
var nadogradnja = posaoUgradnje{ime: "Nadogradnja", odbijeno: "Nadogradnja odbijena", gotovo: "Nadogradnja na %s gotova", sPostavom: true}

// provjeri pita jos; bez njega nema ni roka
func (g posaoUgradnje) provjeri(ctx context.Context) (time.Time, error) {
	if g.jos == nil {
		return time.Time{}, nil
	}
	return g.jos(ctx)
}

// zapisiGotovo zapisuje u dnevnik da je posao gotov, kad za to ima redak
func (p *Postava) zapisiGotovo(g posaoUgradnje, izd string) {
	if g.gotovo != "" {
		p.Pisi(g.gotovo, izd)
	}
}

// ugradiIzdanje preuzima, provjerava i postavlja izdanje iz
func (p *Postava) ugradiIzdanje(ctx context.Context, iz Izdanje, staro string, g posaoUgradnje) (string, error) {
	if p.usluga != nil {
		return p.nadogradiUslugu(ctx, iz, g)
	}
	novi, err := p.Ugradnja.Pripremi(ctx, iz)
	if err != nil {
		p.Pisi("%s: %v", g.odbijeno, err)
		return "", err
	}
	if _, err := g.provjeri(ctx); err != nil {
		_ = os.Remove(novi) // pripremljeni program više ne treba; ostatak ne smeta
		return "", err
	}
	if !p.Cvor.Instaliran() {
		return iz.Verzija.String(), p.Ugradnja.Postavi(novi)
	}
	if err := p.Ugradnja.Ugradi(ctx, novi, staro, iz.Verzija.String()); err != nil {
		p.Pisi("%s: %v", g.ime, err)
		return "", err
	}
	p.zapisiGotovo(g, iz.Verzija.String())
	return iz.Verzija.String(), nil
}

// Izlaz: Postava se gasi. Vlastiti čvor gasi se s njom, a usluga sustava
// radi dalje, jer o njoj ovise i drugi u uredu.
func (p *Postava) Izlaz() {
	if p.usluga != nil {
		p.Pisi("Izlaz: usluga %s radi dalje", p.usluga.Ime)
		return
	}
	p.Pisi("Izlaz: gasim čvor")
	_ = p.Cvor.Zaustavi(30 * time.Second)
}

// DnevnikCvora vraća datoteku s dnevnikom čvora. Usluga piše u dnevnik
// sustava, pa se njegovi zadnji redci najprije zapišu u mapu Postave.
func (p *Postava) DnevnikCvora(ctx context.Context) (string, error) {
	if p.usluga == nil {
		return p.M.DnevnikCvora(), nil
	}
	return p.M.DnevnikUsluge(), p.usluga.Dnevnik(ctx, p.M.DnevnikUsluge())
}

// ukljuciPriPrijavi je varijabla samo zato da je test zamijeni (na Windowsu
// pravi upis ide u registar)
var ukljuciPriPrijavi = UkljuciPriPrijavi

// PrviPutUzUslugu: pri prvom pokretanju uz uslugu sustava Postava se sama
// stavi u pokretanje pri prijavi, samo jednom. Oznaka u mapi Postave pamti
// da je to učinjeno, pa kasnije isključivanje u izborniku ostaje.
func (p *Postava) PrviPutUzUslugu() {
	if p.usluga == nil {
		return
	}
	if _, err := os.Stat(p.M.OznakaPriPrijavi()); err == nil {
		return
	}
	if err := ukljuciPriPrijavi(p.Exe); err != nil {
		p.Pisi("Pokretanje pri prijavi: %v", err)
		return
	}
	_ = os.WriteFile(p.M.OznakaPriPrijavi(), []byte(p.Exe+"\n"), 0o644)
	p.Pisi("Pokretanje pri prijavi uključeno (prvo pokretanje uz uslugu sustava)")
}

// Opcije instalacije (instalacijski program ih predaje Postavi)
type Opcije struct {
	PriPrijavi bool
	IzMape     string // izdanje bez interneta: mapa s programom, SHA256SUMS i .sig
	ImeCvora   string // ime čvora u mreži iz instalacijskog programa (prazno: čvor ga izabere sam)
	Prvi       string // "nova" ili "postojeca": što otvoriti pri prvom pokretanju svježeg čvora
}

// Prvi koraci svježeg čvora: stranica koju Postava otvori jednom, kad čvor
// prvi put odgovori
const (
	PrviNova      = "nova"
	PrviPostojeca = "postojeca"
)

// OznakaPrvogOtvaranja je datoteka u data/ s putanjom stranice za prvo otvaranje
func (m Mjesta) OznakaPrvogOtvaranja() string {
	return filepath.Join(m.Podaci, "postava-prvo-otvaranje")
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
	// svjež čvor (baze još nema): ime iz instalacijskog programa prije prvog
	// pokretanja, i stranica Postavljanje kad prvi put odgovori
	if _, err := os.Stat(p.M.BazaCvora()); errors.Is(err, os.ErrNotExist) {
		if o.ImeCvora != "" {
			if err := imecvora.Provjeri(o.ImeCvora); err != nil {
				return err
			}
			if izlaz, err := p.pripremiCvor(ctx, o.ImeCvora); err != nil {
				p.Pisi("Ime čvora nije upisano (%v): %s; čvor će izabrati jedinstveno ime sam", err, izlaz)
			} else {
				p.Pisi("%s", izlaz)
			}
		}
		putanja := "postavljanje"
		if o.Prvi == PrviPostojeca {
			putanja = "postavljanje?put=postojeca"
		}
		_ = os.WriteFile(p.M.OznakaPrvogOtvaranja(), []byte(putanja), 0o644)
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

// pripremiCvor je gocop -pripremi: ime čvora u gocop.toml prije prvog pokretanja
func (p *Postava) pripremiCvor(ctx context.Context, ime string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.M.Gocop(), "-pripremi", "-db", p.M.BazaCvora(), "-node", ime)
	cmd.Dir = p.M.Podaci
	bezProzora(cmd)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// PrvoOtvaranje vraća stranicu koju treba otvoriti jednom, kad svjež čvor
// prvi put odgovori, i briše oznaku; prazno kad je već otvorena
func (p *Postava) PrvoOtvaranje() string {
	b, err := os.ReadFile(p.M.OznakaPrvogOtvaranja())
	if err != nil {
		return ""
	}
	_ = os.Remove(p.M.OznakaPrvogOtvaranja())
	return strings.TrimSpace(string(b))
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
