package razmjena

// STUN Binding (stun.go): vektori iz RFC 5769 (§2.1 zahtjev, §2.2 odgovor s
// IPv4, §2.3 odgovor s IPv6) i fuzz čitanja odgovora.

import (
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"net/netip"
	"testing"
)

// idVektora je ID transakcije iz vektora RFC 5769
var idVektora = [12]byte{0xb7, 0xe7, 0xa7, 0x01, 0xbc, 0x34, 0xd6, 0x86, 0xfa, 0x87, 0xdf, 0xae}

func vektor(t testing.TB, h string) []byte {
	t.Helper()
	b, err := hex.DecodeString(h)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const (
	// RFC 5769 §2.1: Binding zahtjev s atributima SOFTWARE, PRIORITY,
	// ICE-CONTROLLED, USERNAME, MESSAGE-INTEGRITY i FINGERPRINT
	vektorZahtjeva = "000100582112a442b7e7a701bc34d686fa87dfae802200105354554e207465737420636c69656e74002400046e0001ff80290008932ff9b151263b36000600096576746a3a68367659202020000800149aeaa70cbfd8cb56781ef2b5b2d3f249c1b571a280280004e57a3bcf"
	// RFC 5769 §2.2: odgovor s XOR-MAPPED-ADDRESS 192.0.2.1:32853
	vektorIPv4 = "0101003c2112a442b7e7a701bc34d686fa87dfae8022000b7465737420766563746f7220002000080001a147e112a643000800142b91f599fd9e90c38c7489f92af9ba53f06be7d780280004c07d4c96"
	// RFC 5769 §2.3: odgovor s XOR-MAPPED-ADDRESS
	// [2001:db8:1234:5678:11:2233:4455:6677]:32853
	vektorIPv6 = "010100482112a442b7e7a701bc34d686fa87dfae8022000b7465737420766563746f7220002000140002a1470113a9faa5d3f179bc25f4b5bed2b9d900080014a382954e4be67bf11784c97c8292c275bfe3ed4180280004c8fb0b4c"
)

// FINGERPRINT iz vektora odgovara CRC-32 ostatka poruke: vektori su
// prepisani bez greške
func TestVektoriSTUNSuCijeli(t *testing.T) {
	for _, h := range []string{vektorZahtjeva, vektorIPv4, vektorIPv6} {
		b := vektor(t, h)
		n := len(b)
		if got, want := binary.BigEndian.Uint32(b[n-4:]), crc32.ChecksumIEEE(b[:n-8])^0x5354554e; got != want {
			t.Errorf("FINGERPRINT %08x, izračunat %08x: vektor je krivo prepisan", got, want)
		}
	}
}

func TestOdgovorSTUN(t *testing.T) {
	for _, c := range []struct {
		ime, h string
		want   netip.AddrPort
	}{
		{"IPv4", vektorIPv4, netip.MustParseAddrPort("192.0.2.1:32853")},
		{"IPv6", vektorIPv6, netip.MustParseAddrPort("[2001:db8:1234:5678:11:2233:4455:6677]:32853")},
	} {
		b := vektor(t, c.h)
		if !jeSTUN(b) || idTransakcije(b) != idVektora {
			t.Fatalf("%s: nije prepoznat", c.ime)
		}
		if got, err := procitajOdgovor(b, idVektora); err != nil || got != c.want {
			t.Errorf("%s: %v %v, želim %v", c.ime, got, err, c.want)
		}
		drugi := idVektora
		drugi[0] ^= 1
		if _, err := procitajOdgovor(b, drugi); err == nil {
			t.Errorf("%s: odgovor na tuđi zahtjev prihvaćen", c.ime)
		}
	}
	if _, err := procitajOdgovor(vektor(t, vektorZahtjeva), idVektora); err == nil {
		t.Error("zahtjev prihvaćen kao odgovor")
	}
}

func TestZahtjevBinding(t *testing.T) {
	b := zahtjevBinding(idVektora)
	if hex.EncodeToString(b) != "000100002112a442b7e7a701bc34d686fa87dfae" || !jeSTUN(b) {
		t.Errorf("zahtjev %x", b)
	}
}

// odgovorS je uspješan odgovor s atributima atr
func odgovorS(id [12]byte, atr ...[]byte) []byte {
	b := zahtjevBinding(id)
	binary.BigEndian.PutUint16(b[0:2], stunUspjeh)
	for _, a := range atr {
		b = append(b, a...)
	}
	binary.BigEndian.PutUint16(b[2:4], uint16(len(b)-stunZaglavlje))
	return b
}

// atribut je atribut vrste vrsta s vrijednošću v, dopunjen do 4 bajta
func atribut(vrsta uint16, v ...byte) []byte {
	b := binary.BigEndian.AppendUint16(nil, vrsta)
	b = binary.BigEndian.AppendUint16(b, uint16(len(v)))
	b = append(b, v...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// Bez XOR-MAPPED vrijedi MAPPED; kriva obitelj ili duljina adrese, odrezan
// atribut i paket koji nije STUN ne daju adresu.
func TestOdgovorSTUNRubovi(t *testing.T) {
	mapped := atribut(atrMapped, 0, obiteljIPv4, 0x11, 0x5c, 203, 0, 113, 7)
	if got, err := procitajOdgovor(odgovorS(idVektora, atribut(0x8022, 'x'), mapped), idVektora); err != nil || got != netip.MustParseAddrPort("203.0.113.7:4444") {
		t.Errorf("MAPPED kao rezerva: %v %v", got, err)
	}
	for ime, b := range map[string][]byte{
		"bez adrese":        odgovorS(idVektora),
		"kriva obitelj":     odgovorS(idVektora, atribut(atrXorMapped, 0, 0x03, 0, 0, 1, 2, 3, 4)),
		"kratka adresa":     odgovorS(idVektora, atribut(atrXorMapped, 0, obiteljIPv4, 0, 0, 1, 2)),
		"prekratak atribut": odgovorS(idVektora, atribut(atrMapped, 0, obiteljIPv4)),
		"kratak":            make([]byte, 10),
		"nije STUN":         append([]byte{0xc0}, make([]byte, 19)...),
	} {
		if _, err := procitajOdgovor(b, idVektora); err == nil {
			t.Errorf("%s: adresa pročitana", ime)
		}
	}
	// atribut koji tvrdi da je dulji od poruke
	odrezan := odgovorS(idVektora, atribut(atrXorMapped, make([]byte, 8)...))
	binary.BigEndian.PutUint16(odrezan[22:24], 64)
	if _, err := procitajOdgovor(odrezan, idVektora); err == nil {
		t.Error("odrezan atribut pročitan")
	}
}

func FuzzProcitajOdgovor(f *testing.F) {
	for _, h := range []string{vektorZahtjeva, vektorIPv4, vektorIPv6} {
		f.Add(vektor(f, h))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		a, err := procitajOdgovor(b, idVektora)
		if err == nil && (!a.IsValid() || !jeSTUN(b) || idTransakcije(b) != idVektora || binary.BigEndian.Uint16(b[0:2]) != stunUspjeh) {
			t.Errorf("prihvaćen paket koji nije naš odgovor: %x → %v", b, a)
		}
	})
}
