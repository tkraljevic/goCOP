package mrezatest

import (
	"bytes"
	"io"
	"net"
	"os"
	"sync"
	"syscall"
	"time"
)

// medjuspremnik je koliko bajtova drži jedan smjer veze, zajedno s onima koji
// još putuju; otprilike toliko drži i TCP prije nego što pisanje stane
const medjuspremnik = 64 << 10

// Par vraća dva kraja jedne veze u memoriji. Prvi ima lokalnu adresu a i
// udaljenu b, drugi obrnuto; adrese ne smiju biti nil. Pisanje ne čeka
// čitatelja dok u smjeru ima mjesta (64 KiB). Kad se jedan kraj zatvori,
// druga strana pročita sve što joj je poslano i zatim dobije io.EOF, a njezino
// pisanje vraća grešku (broken pipe). Rokovi vraćaju os.ErrDeadlineExceeded.
func Par(a, b net.Addr) (net.Conn, net.Conn) { return ParSKasnjenjem(a, b, 0) }

// ParSKasnjenjem je Par u kojem ono što se napiše, i zatvaranje, stiže na
// drugu stranu tek nakon kašnjenja. Pitanje i odgovor zato traju dva
// kašnjenja.
func ParSKasnjenjem(a, b net.Addr, kasnjenje time.Duration) (net.Conn, net.Conn) {
	v := &veza{}
	v.cond = sync.NewCond(&v.mu)
	v.smjer[0].kasnjenje = kasnjenje
	v.smjer[1].kasnjenje = kasnjenje
	return &kraj{v: v, i: 0, lokalna: a, udaljena: b}, &kraj{v: v, i: 1, lokalna: b, udaljena: a}
}

// veza je stanje zajedničko objema stranama. Jedna brava i jedan uvjet
// pokrivaju oba smjera: svaka promjena budi sve koji čekaju, a svatko sam
// provjeri je li dočekao svoje.
type veza struct {
	mu    sync.Mutex
	cond  *sync.Cond
	smjer [2]smjer // smjer[i] je ono što piše kraj i
}

// probudi budi sve koji čekaju. Zovu je tajmeri kad stigne komad ili istekne
// rok; uzima bravu, da se buđenje ne izgubi između provjere i čekanja.
func (v *veza) probudi() {
	v.mu.Lock()
	v.cond.Broadcast()
	v.mu.Unlock()
}

// komad je ono što je u jednom pisanju stalo u smjer, a na drugu stranu stiže
// u trenutku stize. Zadnji komad nema bajtova: pisač je zatvorio vezu.
type komad struct {
	stize   time.Time
	bajtovi []byte
	zadnji  bool
}

// smjer drži komade redom kojim su napisani. Jedan tajmer budi čitatelja kad
// stigne prvi komad u redu, a sam ništa ne prenosi, pa komadi koji stižu u
// istom trenutku ne mogu zamijeniti mjesta.
type smjer struct {
	red       []komad
	uLetu     int // bajtovi u redu, i oni koji još putuju
	kasnjenje time.Duration
	tajmer    *time.Timer
	odbacen   bool // čitatelj je zatvorio svoj kraj
}

// dodaj stavi u red koliko stane od b i vrati koliko je stalo
func (d *smjer) dodaj(b []byte, sad time.Time) int {
	n := min(len(b), medjuspremnik-d.uLetu)
	if n <= 0 {
		return 0
	}
	d.red = append(d.red, komad{stize: sad.Add(d.kasnjenje), bajtovi: bytes.Clone(b[:n])})
	d.uLetu += n
	return n
}

// uzmi prepiše u b ono što je do sad stiglo. Gotovo javlja da je stigao
// zadnji komad, a prije njega nema ništa nepročitano.
func (d *smjer) uzmi(b []byte, sad time.Time) (n int, gotovo bool) {
	for len(d.red) > 0 && n < len(b) && !d.red[0].stize.After(sad) {
		k := &d.red[0]
		if k.zadnji {
			return n, n == 0
		}
		m := copy(b[n:], k.bajtovi)
		n += m
		d.uLetu -= m
		k.bajtovi = k.bajtovi[m:]
		if len(k.bajtovi) == 0 {
			d.red = d.red[1:]
		}
	}
	return n, false
}

// budi namjesti tajmer smjera na trenutak kad stiže prvi komad u redu
func (d *smjer) budi(sad time.Time, probudi func()) {
	if len(d.red) == 0 || !d.red[0].stize.After(sad) {
		return
	}
	za := d.red[0].stize.Sub(sad)
	if d.tajmer == nil {
		d.tajmer = time.AfterFunc(za, probudi)
		return
	}
	d.tajmer.Reset(za)
}

// zatvori stavi u red zadnji komad: druga strana dobije io.EOF kad pročita
// sve prije njega
func (d *smjer) zatvori(sad time.Time) {
	d.red = append(d.red, komad{stize: sad.Add(d.kasnjenje), zadnji: true})
}

// odbaci baci nepročitano kad čitatelj zatvori svoj kraj; pisač s druge
// strane od tada dobiva grešku
func (d *smjer) odbaci() {
	d.red, d.uLetu, d.odbacen = nil, 0, true
	zaustavi(d.tajmer)
}

// rok je rok čitanja ili pisanja. Tajmer samo budi one koji čekaju, a je li
// rok istekao svatko provjeri po satu.
type rok struct {
	kad    time.Time
	tajmer *time.Timer
}

func (r *rok) postavi(kad time.Time, probudi func()) {
	zaustavi(r.tajmer)
	r.kad, r.tajmer = kad, nil
	if !kad.IsZero() {
		r.tajmer = time.AfterFunc(time.Until(kad), probudi)
	}
}

func (r *rok) istekao(sad time.Time) bool { return !r.kad.IsZero() && !sad.Before(r.kad) }

func zaustavi(t *time.Timer) {
	if t != nil {
		t.Stop()
	}
}

// kraj je jedna strana veze: piše u smjer[i], a čita iz smjer[1-i]
type kraj struct {
	v                 *veza
	i                 int
	lokalna, udaljena net.Addr
	zatvoren          bool
	citanje, pisanje  rok
}

// Read čeka dok nešto ne stigne. Kad je druga strana zatvorila vezu, vraća
// io.EOF nakon svega što je poslala.
func (k *kraj) Read(b []byte) (int, error) {
	v := k.v
	v.mu.Lock()
	defer v.mu.Unlock()
	d := &v.smjer[1-k.i]
	for {
		sad := time.Now()
		if err := k.zapreka(&k.citanje, sad); err != nil {
			return 0, k.greska("read", err)
		}
		if len(b) == 0 {
			return 0, nil
		}
		n, gotovo := d.uzmi(b, sad)
		if gotovo {
			return 0, io.EOF
		}
		if n > 0 {
			v.cond.Broadcast() // pisač možda čeka mjesto
			return n, nil
		}
		d.budi(sad, v.probudi)
		v.cond.Wait()
	}
}

// Write stavlja b u smjer i čeka samo kad u njemu nema mjesta
func (k *kraj) Write(b []byte) (int, error) {
	v := k.v
	v.mu.Lock()
	defer v.mu.Unlock()
	d := &v.smjer[k.i]
	n := 0
	for {
		sad := time.Now()
		if err := k.zapreka(&k.pisanje, sad); err != nil {
			return n, k.greska("write", err)
		}
		if d.odbacen {
			return n, k.greska("write", os.NewSyscallError("write", syscall.EPIPE))
		}
		if m := d.dodaj(b[n:], sad); m > 0 {
			n += m
			v.cond.Broadcast() // čitatelj možda čeka
		}
		if n == len(b) {
			return n, nil
		}
		v.cond.Wait()
	}
}

// zapreka javlja zašto ovaj kraj ne smije čitati ili pisati
func (k *kraj) zapreka(r *rok, sad time.Time) error {
	switch {
	case k.zatvoren:
		return net.ErrClosed
	case r.istekao(sad):
		return os.ErrDeadlineExceeded
	}
	return nil
}

// Close zatvara ovaj kraj. Druga strana pročita sve što je ovaj kraj poslao
// i zatim dobije io.EOF; ono što joj ovaj kraj nije pročitao se baca.
func (k *kraj) Close() error {
	v := k.v
	v.mu.Lock()
	defer v.mu.Unlock()
	if k.zatvoren {
		return k.greska("close", net.ErrClosed)
	}
	k.zatvoren = true
	v.smjer[k.i].zatvori(time.Now())
	v.smjer[1-k.i].odbaci()
	zaustavi(k.citanje.tajmer)
	zaustavi(k.pisanje.tajmer)
	v.cond.Broadcast()
	return nil
}

func (k *kraj) LocalAddr() net.Addr  { return k.lokalna }
func (k *kraj) RemoteAddr() net.Addr { return k.udaljena }

func (k *kraj) SetDeadline(t time.Time) error {
	return k.postaviRok(t, &k.citanje, &k.pisanje)
}

func (k *kraj) SetReadDeadline(t time.Time) error { return k.postaviRok(t, &k.citanje) }

func (k *kraj) SetWriteDeadline(t time.Time) error { return k.postaviRok(t, &k.pisanje) }

// postaviRok postavlja rokove i budi one koji čekaju, da ih provjere
func (k *kraj) postaviRok(t time.Time, rokovi ...*rok) error {
	v := k.v
	v.mu.Lock()
	defer v.mu.Unlock()
	if k.zatvoren {
		return k.greska("set", net.ErrClosed)
	}
	for _, r := range rokovi {
		r.postavi(t, v.probudi)
	}
	v.cond.Broadcast()
	return nil
}

// greska omota grešku kao što to radi net za TCP vezu, pa errors.Is i
// net.Error rade kao na pravoj vezi
func (k *kraj) greska(op string, err error) error {
	return greska(op, k.lokalna, k.udaljena, err)
}

func greska(op string, izvor, odrediste net.Addr, err error) error {
	return &net.OpError{Op: op, Net: odrediste.Network(), Source: izvor, Addr: odrediste, Err: err}
}
