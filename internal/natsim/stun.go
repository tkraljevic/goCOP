package natsim

// Lažni STUN poslužitelj (plan WEB synca, F3a): na Binding zahtjev odgovori
// adresom s koje je zahtjev stigao, u atributu XOR-MAPPED-ADDRESS (RFC
// 8489), kao poslužitelji koje program pita (razmjena/stun.go). Iza NAT-a
// je to vanjska adresa preslikavanja prema tom poslužitelju, pa dva
// poslužitelja na različitim adresama razlikuju lak NAT od simetričnog.

import (
	"encoding/binary"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go/testutils/simnet"
)

// Adrese lažnih STUN poslužitelja, iste kao Googleov i Cloudflareov
var (
	STUNGoogle     = netip.MustParseAddrPort("74.125.250.129:19302")
	STUNCloudflare = netip.MustParseAddrPort("162.159.207.0:3478")
)

const (
	stunKolacic     = 0x2112A442
	stunZahtjev     = 0x0001 // Binding Request
	stunUspjeh      = 0x0101 // Binding Success Response
	stunXorAdresa   = 0x0020 // XOR-MAPPED-ADDRESS
	kasnjenjeSTUN   = 5 * time.Millisecond
	velicinaOdgovor = 32
)

// STUN je lažni STUN poslužitelj na internetu
type STUN struct {
	conn  *simnet.SimConn
	upiti atomic.Int64
	gotov sync.WaitGroup
}

// STUN pokrene lažni STUN poslužitelj na javnoj adresi; paketi do njega
// kasne koliko kaže kasnjenje
func (m *Mreza) STUN(a netip.AddrPort, kasnjenje time.Duration) *STUN {
	s := &STUN{conn: m.Racunalo(a, PostavkeRacunala{Kasnjenje: kasnjenje})}
	s.gotov.Go(s.vrti)
	m.mu.Lock()
	m.stunovi = append(m.stunovi, s)
	m.mu.Unlock()
	return s
}

// DvaSTUN pokrene oba lažna STUN poslužitelja, na STUNGoogle i
// STUNCloudflare, 5 ms od interneta
func (m *Mreza) DvaSTUN() (google, cloudflare *STUN) {
	return m.STUN(STUNGoogle, kasnjenjeSTUN), m.STUN(STUNCloudflare, kasnjenjeSTUN)
}

// Upiti je broj Binding zahtjeva na koje je poslužitelj odgovorio
func (s *STUN) Upiti() int64 { return s.upiti.Load() }

// vrti odgovara na zahtjeve dok se utičnica ne zatvori
func (s *STUN) vrti() {
	b := make([]byte, 1500)
	for {
		n, od, err := s.conn.ReadFrom(b)
		if err != nil {
			return
		}
		if o := odgovorSTUN(b[:n], adresa(od)); o != nil {
			s.upiti.Add(1)
			_, _ = s.conn.WriteTo(o, od) // simnet ne vraća grešku slanja; izgubljen odgovor nadoknadi idući upit
		}
	}
}

func (s *STUN) zatvori() {
	_ = s.conn.Close() // SimConn se zatvara bez greške
	s.gotov.Wait()
}

// odgovorSTUN je uspješan Binding odgovor s adresom od na zahtjev z; nil
// kad z nije Binding zahtjev ili od nije IPv4
func odgovorSTUN(z []byte, od netip.AddrPort) []byte {
	if len(z) < 20 || binary.BigEndian.Uint16(z[0:2]) != stunZahtjev || binary.BigEndian.Uint32(z[4:8]) != stunKolacic || !od.Addr().Is4() {
		return nil
	}
	o := make([]byte, velicinaOdgovor)
	binary.BigEndian.PutUint16(o[0:2], stunUspjeh)
	binary.BigEndian.PutUint16(o[2:4], velicinaOdgovor-20)
	copy(o[4:20], z[4:20]) // kolačić i ID transakcije
	binary.BigEndian.PutUint16(o[20:22], stunXorAdresa)
	binary.BigEndian.PutUint16(o[22:24], 8)
	o[25] = 0x01 // IPv4
	binary.BigEndian.PutUint16(o[26:28], od.Port()^uint16(stunKolacic>>16))
	ip := od.Addr().As4()
	binary.BigEndian.PutUint32(o[28:32], binary.BigEndian.Uint32(ip[:])^stunKolacic)
	return o
}
