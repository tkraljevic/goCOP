package razmjena

// UDP utičnica WEB synca (plan WEB synca, F1a). Jedna utičnica nosi STUN, a
// od F3 i QUIC: drži je jedan quic.Transport, a sve što nije QUIC s nje čita
// jedan čitač (ReadNonQUICPacket). Udarac (F3a) ide sesiji kojoj pripada
// (udaranje.go), STUN odgovor onome tko čeka taj ID transakcije, a ostali
// paketi koji nisu QUIC broje se kao odbačeni. Utičnica se otvara samo uz
// web_sync_prijava; s nje se nakon toga nikad ne čita izravno.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
)

// otvoriUDP otvara UDP utičnicu na portu (0: nasumičan); var, da je test
// podmetne
var otvoriUDP = func(port int) (net.PacketConn, error) {
	return net.ListenPacket("udp4", net.JoinHostPort("", strconv.Itoa(port)))
}

// OtvoriUDP otvori UDP utičnicu WEB synca na portu; kad se on ne da
// otvoriti, iz bilo kojeg razloga, uzme nasumičan port i vrati upozorenje
func OtvoriUDP(port int) (conn net.PacketConn, upozorenje, err error) {
	if conn, err = otvoriUDP(port); err == nil {
		return conn, nil, nil
	}
	upozorenje = fmt.Errorf("UDP port %d se ne da otvoriti (%v), pa je uzet nasumičan", port, err)
	conn, err = otvoriUDP(0)
	return conn, upozorenje, err
}

// UDP je UDP utičnica WEB synca: quic.Transport i čitač paketa koji nisu QUIC
type UDP struct {
	tr       *quic.Transport
	mu       sync.Mutex
	cekaju   map[[12]byte]chan<- odgovorSTUN
	odbaceni atomic.Int64
	radi     atomic.Bool
	bezRute  atomic.Bool                         // slanje je javilo da nema rute (nova_uticnica.go)
	sesije   map[[16]byte]chan<- primljeniUdarac // sesije udaraca (udaranje.go), pod mu
	otkazi   context.CancelFunc
	citac    sync.WaitGroup
}

// odgovorSTUN je STUN paket koji je stigao na čekani ID, i kad je stigao
type odgovorSTUN struct {
	paket []byte
	kad   time.Time
}

// NoviUDP preuzme utičnicu conn: od tada s nje čita i piše samo UDP. Red
// paketa koji nisu QUIC stvori se prije prvog slanja, jer quic-go do tada
// takve pakete tiho baca.
func NoviUDP(conn net.PacketConn) *UDP {
	u := &UDP{tr: &quic.Transport{Conn: conn}, cekaju: map[[12]byte]chan<- odgovorSTUN{}}
	ctx, otkazi := context.WithCancel(context.Background())
	u.otkazi = otkazi
	odmah, stani := context.WithCancel(ctx)
	stani()
	_, _, _ = u.tr.ReadNonQUICPacket(odmah, nil) // otkazan ctx: samo stvori red
	u.radi.Store(true)
	u.citac.Go(func() { u.citaj(ctx) })
	return u
}

// Transport je quic.Transport utičnice (QUIC ide od F3)
func (u *UDP) Transport() *quic.Transport { return u.tr }

// Port je lokalni port utičnice
func (u *UDP) Port() int {
	if a, ok := u.tr.Conn.LocalAddr().(*net.UDPAddr); ok {
		return a.Port
	}
	return 0
}

// Odbaceni je broj primljenih paketa koji nisu QUIC i nisu ničiji STUN
// odgovor
func (u *UDP) Odbaceni() int64 { return u.odbaceni.Load() }

// Radi javlja čita li utičnica još: quic-go zatvori prijenos sam kad
// čitanje utičnice ne uspije
func (u *UDP) Radi() bool { return u.radi.Load() }

// Close zaustavi čitač, zatvori prijenos i utičnicu
func (u *UDP) Close() error {
	u.otkazi()
	u.citac.Wait()
	return errors.Join(u.tr.Close(), u.tr.Conn.Close())
}

// citaj čita pakete koji nisu QUIC dok ctx traje ili se prijenos ne zatvori
func (u *UDP) citaj(ctx context.Context) {
	defer u.radi.Store(false)
	b := make([]byte, 1500)
	for {
		n, od, err := u.tr.ReadNonQUICPacket(ctx, b)
		if err != nil {
			return
		}
		p, kad := append([]byte(nil), b[:n]...), time.Now()
		if !u.predajUdarac(p, od, kad) {
			u.predaj(p, kad)
		}
	}
}

// predaj preda STUN odgovor onome tko čeka njegov ID; ostalo je odbačeno
func (u *UDP) predaj(p []byte, kad time.Time) {
	var c chan<- odgovorSTUN
	if jeSTUN(p) {
		u.mu.Lock()
		c = u.cekaju[idTransakcije(p)]
		u.mu.Unlock()
	}
	select {
	case c <- odgovorSTUN{paket: p, kad: kad}:
	default: // nitko ne čeka (nil kanal), ili je čekač već pun
		u.odbaceni.Add(1)
	}
}

// cekaj prijavi ID-ove čiji odgovori idu na kanal c; vraća odjavu
func (u *UDP) cekaj(c chan<- odgovorSTUN, ids ...[12]byte) func() {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, id := range ids {
		u.cekaju[id] = c
	}
	return func() {
		u.mu.Lock()
		defer u.mu.Unlock()
		for _, id := range ids {
			delete(u.cekaju, id)
		}
	}
}
