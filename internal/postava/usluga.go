package postava

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Usluga upravlja čvorom koji je na Linuxu postavljen kao usluga sustava
// (build/linux/instaliraj.sh, docs/linux.md). Takav čvor pali systemd pri
// pokretanju računala, kao korisnik gocop, s podacima u /var/lib/gocop.
// Postava ga tada ne pokreće sama, nego pita systemd za stanje i traži od
// njega da uslugu pokrene ili zaustavi; za to systemd preko polkita traži
// lozinku administratora.
//
// Sve naredbe idu kroz Naredba i Trazi, pa se ponašanje može ispitati bez
// systemd-a, i na macOS-u.
type Usluga struct {
	Ime      string // gocop.service
	Program  string // /usr/local/bin/gocop
	Postavke string // /var/lib/gocop/gocop.toml; korisnik ga obično ne može čitati
	Naredba  func(ctx context.Context, ime string, arg ...string) ([]byte, error)
	Trazi    func(ime string) (string, error) // put do programa koji se pokreće kao root

	klijent *http.Client
	// povlasten javlja smije li se s tog porta primiti dopuštenje za
	// starije izdanje; nil: port ispod 1024 (sPovlastenogPorta). Test ga
	// zamijeni, jer probni poslužitelj ne sluša na takvom portu.
	povlasten func(port int) bool

	mu        sync.Mutex
	stanje    StanjeProcesa
	aktivnost string    // zadnji ActiveState
	kad       time.Time // kad je stanje zadnji put pročitano
	promjena  func()
}

// JedinicaUsluge je jedinica koju postavlja build/linux/instaliraj.sh
const JedinicaUsluge = "/etc/systemd/system/gocop.service"

const (
	// svjezinaStanja: Stanje se pita često, a systemctl ne treba pitati
	// češće od toga
	svjezinaStanja = 2 * time.Second
	// rokLozinke: systemctl start i stop čekaju dok korisnik upisuje lozinku
	rokLozinke = 5 * time.Minute
)

// NovaUsluga je usluga gocop kakvu postavlja instaliraj.sh
func NovaUsluga() *Usluga {
	return &Usluga{
		Ime: "gocop.service", Program: "/usr/local/bin/gocop", Postavke: "/var/lib/gocop/gocop.toml",
		Naredba: pokreniNaredbu, Trazi: traziUSustavu,
		klijent: &http.Client{Timeout: 3 * time.Second},
	}
}

// otkrijUslugu javlja je li čvor na ovom računalu usluga sustava: samo na
// Linuxu, kad postoji jedinica iz instaliraj.sh
func otkrijUslugu(goos, jedinica string) bool {
	if goos != "linux" {
		return false
	}
	_, err := os.Stat(jedinica)
	return err == nil
}

// sistemskeMape: pkexec i bash, koji se pokreću kao root, traže se samo u
// mapama sustava, a ne u korisnikovu PATH-u
var sistemskeMape = []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"}

func traziUSustavu(ime string) (string, error) {
	for _, d := range sistemskeMape {
		if fi, err := os.Stat(filepath.Join(d, ime)); err == nil && !fi.IsDir() {
			return filepath.Join(d, ime), nil
		}
	}
	return "", fmt.Errorf("%s nije ni u jednoj od mapa %s", ime, strings.Join(sistemskeMape, ", "))
}

// imaUsluge pita otkrijUslugu za ovo računalo; varijabla samo zato da je
// test zamijeni
var imaUsluge = func() bool { return otkrijUslugu(runtime.GOOS, JedinicaUsluge) }

func pokreniNaredbu(ctx context.Context, ime string, arg ...string) ([]byte, error) {
	return exec.CommandContext(ctx, ime, arg...).CombinedOutput()
}

// NaPromjenu zadaje što pozvati kad usluga stane ili krene
func (u *Usluga) NaPromjenu(f func()) {
	u.mu.Lock()
	u.promjena = f
	u.mu.Unlock()
}

func (u *Usluga) javi() {
	u.mu.Lock()
	f := u.promjena
	u.mu.Unlock()
	if f != nil {
		go f()
	}
}

// Instaliran: jedinica postoji, inače Postava ne bi radila s uslugom
func (u *Usluga) Instaliran() bool { return true }

// Proces vraća stanje usluge iz systemd-a, najviše dvije sekunde staro
func (u *Usluga) Proces() StanjeProcesa {
	u.mu.Lock()
	s, svjeze := u.stanje, time.Since(u.kad) < svjezinaStanja
	u.mu.Unlock()
	if svjeze {
		return s
	}
	return u.osvjezi()
}

// osvjezi pita systemctl show; kad systemctl ne odgovori, usluga se vodi
// kao zaustavljena
func (u *Usluga) osvjezi() StanjeProcesa {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := u.Naredba(ctx, "systemctl", "show", u.Ime, "--property=ActiveState,ActiveEnterTimestamp")
	aktivnost, s := "", StanjeProcesa{}
	if err == nil {
		aktivnost, s = citajStanje(out)
	}
	u.mu.Lock()
	promijenjeno := aktivnost != u.aktivnost
	u.stanje, u.aktivnost, u.kad = s, aktivnost, time.Now()
	u.mu.Unlock()
	if promijenjeno {
		u.javi()
	}
	return s
}

// zaboravi: sljedeći Proces opet pita systemd
func (u *Usluga) zaboravi() {
	u.mu.Lock()
	u.kad = time.Time{}
	u.mu.Unlock()
}

// citajStanje čita ispis systemctl show (redci IME=vrijednost)
func citajStanje(out []byte) (aktivnost string, s StanjeProcesa) {
	var pocetak string
	for _, r := range strings.Split(string(out), "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(r), "=")
		switch k {
		case "ActiveState":
			aktivnost = v
		case "ActiveEnterTimestamp":
			pocetak = v
		}
	}
	switch aktivnost {
	case "active", "activating", "reloading":
		s.Radi, s.Zeljeno, s.Pokrenut = true, true, vrijemeSystemd(pocetak)
	case "failed":
		s.Pao = true
	}
	return aktivnost, s
}

// vrijemeSystemd čita vrijeme kako ga piše systemctl show, npr. „Sun
// 2026-10-11 09:12:44 CEST”. Dan u tjednu se preskače; nepoznato vrijeme je
// nula.
func vrijemeSystemd(s string) time.Time {
	p := strings.Fields(s)
	if len(p) != 4 {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02 15:04:05 MST", strings.Join(p[1:], " "))
	if err != nil {
		return time.Time{}
	}
	return t
}

// Pokreni pali uslugu ako ne radi. Kad radi, ne radi ništa, pa pri prijavi
// nema pitanja za lozinku. Inače systemctl start, a systemd preko polkita
// otvori prozor za lozinku administratora.
func (u *Usluga) Pokreni() error {
	u.zaboravi()
	if u.Proces().Radi {
		return nil
	}
	return u.systemctl("start", "pokrenuta")
}

// Zaustavi zaustavlja uslugu (systemctl stop, uz lozinku administratora).
// Koliko systemd čeka da se čvor ugasi, zadaje jedinica, pa rok ovdje ne
// vrijedi.
func (u *Usluga) Zaustavi(time.Duration) error {
	return u.systemctl("stop", "zaustavljena")
}

func (u *Usluga) systemctl(radnja, ishod string) error {
	ctx, cancel := context.WithTimeout(context.Background(), rokLozinke)
	defer cancel()
	out, err := u.Naredba(ctx, "systemctl", radnja, u.Ime)
	u.zaboravi()
	u.javi()
	if err != nil {
		return greskaSystemctl(ishod, out, err)
	}
	return nil
}

// greskaSystemctl: kad je korisnik odustao od lozinke (ili nema prozora za
// nju), systemctl javlja Access denied ili Interactive authentication
// required
func greskaSystemctl(ishod string, out []byte, err error) error {
	r := ispisIliGreska(out, err)
	if strings.Contains(r, "Access denied") || strings.Contains(r, "authentication required") {
		return fmt.Errorf("usluga gocop nije %s jer lozinka administratora nije upisana", ishod)
	}
	return fmt.Errorf("usluga gocop nije %s (systemctl: %s)", ishod, r)
}

// ispisIliGreska je zadnji redak ispisa naredbe, a bez ispisa sama greška
func ispisIliGreska(out []byte, err error) string {
	redci := strings.Split(strings.TrimSpace(string(out)), "\n")
	if r := strings.TrimSpace(redci[len(redci)-1]); r != "" {
		return r
	}
	return err.Error()
}

// Zdravlje pita čvor isto kao Cvor. gocop.toml usluge korisnik obično ne
// može pročitati, pa tada vrijede zadani portovi 80 i 8080.
func (u *Usluga) Zdravlje(ctx context.Context) (izdanje string, port int) {
	o := u.PitajZdravlje(ctx)
	return o.Izdanje, o.Port
}

// PitajZdravlje je Zdravlje s cijelim odgovorom. Usluzi Postava ne daje
// ključ za potpis, pa dopuštenje za starije izdanje prima samo s porta
// ispod 1024: na Linuxu njega otvara samo root ili usluga kojoj je to
// dopušteno, a ne bilo koji program na računalu (npr. na rezervnom 8080).
func (u *Usluga) PitajZdravlje(ctx context.Context) OdgovorZdravlja {
	o := pitajZdravlje(ctx, u.klijent, portoviIz(u.Postavke))
	return uzDopustenje(o, u.sPovlastenogPorta(o.Port), time.Now())
}

// sPovlastenogPorta: s porta se smije primiti dopuštenje za starije izdanje
func (u *Usluga) sPovlastenogPorta(port int) bool {
	if u.povlasten != nil {
		return u.povlasten(port)
	}
	return port > 0 && port < 1024
}

// Dnevnik zapisuje zadnjih 2000 redaka dnevnika usluge (journalctl) u
// datoteku, da se otvori kao i dnevnik vlastitog čvora
func (u *Usluga) Dnevnik(ctx context.Context, put string) error {
	out, err := u.Naredba(ctx, "journalctl", "-u", u.Ime, "-n", "2000", "--no-pager")
	if err != nil {
		return fmt.Errorf("dnevnik usluge nije pročitan (journalctl: %s)", ispisIliGreska(out, err))
	}
	zaglavlje := fmt.Sprintf("# Dnevnik usluge gocop, zadnjih 2000 redaka (journalctl -u %s), %s\n"+
		"# Ako je prazan: dnevnik sustava smiju čitati samo članovi grupe adm ili systemd-journal.\n\n",
		u.Ime, time.Now().Format("2. 1. 2006. 15:04"))
	return os.WriteFile(put, append([]byte(zaglavlje), out...), 0o600)
}
