package razmjena

import (
	"context"
	"errors"
	"net"
	"os"
	"slices"
	"syscall"
	"testing"
	"time"
)

// Nova utičnica je na istom portu, sa svojim čitačem, a stara više ne
// radi; kad je port u međuvremenu zauzet, nova je na nasumičnom uz
// upozorenje, a kad se nikako ne da otvoriti, greška
func TestObnoviUDP(t *testing.T) {
	conn, _, err := OtvoriUDP(0)
	bezGreske(t, err)
	stara := NoviUDP(conn)
	port := stara.Port()
	nova, upozorenje, err := ObnoviUDP(stara)
	if err != nil || upozorenje != nil || nova.Port() != port || !nova.Radi() || stara.Radi() {
		t.Fatalf("nova utičnica: port %d (bio %d), %v, %v", nova.Port(), port, upozorenje, err)
	}
	staro := otvoriUDP
	t.Cleanup(func() { otvoriUDP = staro })
	otvoriUDP = func(p int) (net.PacketConn, error) {
		if p == nova.Port() {
			return nil, errors.New("zauzet")
		}
		return staro(p)
	}
	treca, upozorenje, err := ObnoviUDP(nova)
	if err != nil || upozorenje == nil || treca.Port() == port {
		t.Errorf("zauzet port: %v, %v", upozorenje, err)
	}
	otvoriUDP = func(int) (net.PacketConn, error) { return nil, errors.New("nema mreže") }
	if _, _, err := ObnoviUDP(treca); err == nil {
		t.Error("utičnica bez mreže")
	}
}

// bezRuteConn je utičnica čije slanje uvijek javi da nema rute
type bezRuteConn struct{ net.PacketConn }

func (bezRuteConn) WriteTo([]byte, net.Addr) (int, error) {
	return 0, &net.OpError{Op: "write", Net: "udp", Err: os.NewSyscallError("sendto", syscall.EHOSTUNREACH)}
}

// Slanje bez rute utičnica zapamti; obično slanje ne
func TestUticnicaBezRute(t *testing.T) {
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	bezGreske(t, err)
	u := NoviUDP(bezRuteConn{c})
	defer u.Close()
	na := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 3478}
	if u.BezRute() {
		t.Error("bez rute prije slanja")
	}
	u.posalji([]byte{0}, na)
	if !u.BezRute() {
		t.Error("slanje bez rute nije zapamćeno")
	}
	c2, err := net.ListenPacket("udp4", "127.0.0.1:0")
	bezGreske(t, err)
	u2 := NoviUDP(c2)
	defer u2.Close()
	u2.posalji([]byte{0}, c.LocalAddr())
	if u2.BezRute() {
		t.Error("obično slanje zapamćeno kao bez rute")
	}
}

// Ruta se čeka: 100, 200 i 400 ms između pokušaja, a bez nje najviše 5 s
func TestCekajRutu(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		staro := poveziUDP
		t.Cleanup(func() { poveziUDP = staro })
		pokusaja := 0
		poveziUDP = func(string) error {
			if pokusaja++; pokusaja < 4 {
				return errors.New("nema rute")
			}
			return nil
		}
		pocetak := time.Now()
		if err := CekajRutu(t.Context(), "192.0.2.1:3478"); err != nil || time.Since(pocetak) != 700*time.Millisecond {
			t.Errorf("ruta nakon tri pokušaja: %v za %v", err, time.Since(pocetak))
		}
		poveziUDP = func(string) error { return errors.New("nema rute") }
		pocetak = time.Now()
		if err := CekajRutu(t.Context(), "192.0.2.1:3478"); err == nil || time.Since(pocetak) != rokRute {
			t.Errorf("bez rute: %v za %v", err, time.Since(pocetak))
		}
		poveziUDP = staro
		if err := CekajRutu(context.Background(), "127.0.0.1:9"); err != nil {
			t.Errorf("ruta do petlje: %v", err)
		}
	})
}

// Lokalne adrese su poredane, samo s uključenih sučelja koja nisu izbačena
func TestLokalneAdrese(t *testing.T) {
	staro := procitajSucelja
	t.Cleanup(func() { procitajSucelja = staro })
	gore := net.FlagUp | net.FlagBroadcast
	procitajSucelja = func() []sucelje {
		return []sucelje{
			{ime: "wlan0", zastavice: gore, adrese: []net.Addr{ipMreza(t, "192.168.1.20/24")}},
			{ime: "eth0", zastavice: gore, adrese: []net.Addr{ipMreza(t, "10.0.0.5/8")}},
			{ime: "docker0", zastavice: gore, adrese: []net.Addr{ipMreza(t, "172.17.0.1/16")}},
			{ime: "eth1", zastavice: net.FlagBroadcast, adrese: []net.Addr{ipMreza(t, "10.9.9.9/8")}},
		}
	}
	if got := LokalneAdrese(); !slices.Equal(got, []string{"10.0.0.5/8", "192.168.1.20/24"}) {
		t.Errorf("lokalne adrese: %v", got)
	}
}
