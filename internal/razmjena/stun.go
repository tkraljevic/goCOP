package razmjena

// STUN Binding (RFC 5389 i 8489) za provjeru mreže (plan WEB synca, F1a):
// računalo pita poslužitelj kojom ga javnom adresom i portom vidi. Samo
// zahtjev i čitanje odgovora, bez vanjske ovisnosti i bez ulaza i izlaza,
// pa se ispituje vektorima iz RFC 5769.
//
// Odgovor se prihvaća samo kao uspješan Binding odgovor s istim ID-om
// transakcije: tko nije na putu morao bi pogoditi 96 slučajnih bitova.
// XOR-MAPPED-ADDRESS ima prednost, a MAPPED-ADDRESS je samo rezerva.
// Ostali atributi (SOFTWARE, FINGERPRINT, MESSAGE-INTEGRITY i nepoznati) se
// preskaču: popis poslužitelja je stalan, a odbaciti odgovor zbog atributa
// koji ne trebamo samo bi pokvarilo provjeru.

import (
	"encoding/binary"
	"errors"
	"net/netip"
)

const (
	stunKolacic   = 0x2112A442
	stunZahtjev   = 0x0001 // Binding Request
	stunUspjeh    = 0x0101 // Binding Success Response
	stunZaglavlje = 20
	atrMapped     = 0x0001 // MAPPED-ADDRESS
	atrXorMapped  = 0x0020 // XOR-MAPPED-ADDRESS
	obiteljIPv4   = 0x01
	obiteljIPv6   = 0x02
)

// ErrNijeOdgovorSTUN: paket nije uspješan Binding odgovor na naš zahtjev ili
// u njemu nema ispravne adrese
var ErrNijeOdgovorSTUN = errors.New("paket nije STUN odgovor s adresom")

// zahtjevBinding je Binding zahtjev bez atributa s ID-om transakcije id
func zahtjevBinding(id [12]byte) []byte {
	b := make([]byte, stunZaglavlje)
	binary.BigEndian.PutUint16(b[0:2], stunZahtjev)
	binary.BigEndian.PutUint32(b[4:8], stunKolacic)
	copy(b[8:20], id[:])
	return b
}

// jeSTUN javlja izgleda li paket kao STUN poruka: gornja dva bita prvog
// bajta su 0, kolačić je na mjestu, a duljina atributa je višekratnik 4 i
// točno do kraja paketa
func jeSTUN(b []byte) bool {
	if len(b) < stunZaglavlje || b[0]&0xC0 != 0 || binary.BigEndian.Uint32(b[4:8]) != stunKolacic {
		return false
	}
	d := int(binary.BigEndian.Uint16(b[2:4]))
	return d%4 == 0 && stunZaglavlje+d == len(b)
}

// idTransakcije je ID transakcije STUN poruke (jeSTUN mora biti istina)
func idTransakcije(b []byte) [12]byte {
	var id [12]byte
	copy(id[:], b[8:20])
	return id
}

// procitajOdgovor čita javnu adresu iz uspješnog Binding odgovora na
// zahtjev s ID-om id
func procitajOdgovor(b []byte, id [12]byte) (netip.AddrPort, error) {
	if !jeSTUN(b) || binary.BigEndian.Uint16(b[0:2]) != stunUspjeh || idTransakcije(b) != id {
		return netip.AddrPort{}, ErrNijeOdgovorSTUN
	}
	xor, mapped := adreseOdgovora(b[stunZaglavlje:], id)
	for _, a := range []netip.AddrPort{xor, mapped} {
		if a.IsValid() {
			return a, nil
		}
	}
	return netip.AddrPort{}, ErrNijeOdgovorSTUN
}

// adreseOdgovora su prva ispravna XOR-MAPPED-ADDRESS i zadnja ispravna
// MAPPED-ADDRESS iz atributa; prazne kad ih nema
func adreseOdgovora(atr []byte, id [12]byte) (xor, mapped netip.AddrPort) {
	for vrsta, v, ostatak, ok := sljedeciAtribut(atr); ok; vrsta, v, ostatak, ok = sljedeciAtribut(ostatak) {
		a, valjana := adresaAtributa(v, vrsta == atrXorMapped, id)
		switch {
		case !valjana:
		case vrsta == atrXorMapped && !xor.IsValid():
			xor = a
		case vrsta == atrMapped:
			mapped = a
		}
	}
	return xor, mapped
}

// sljedeciAtribut je prvi atribut iz atr (vrsta i vrijednost) i ostatak iza
// njega i dopune; ok je laž kad atributa nema ili prelazi kraj poruke
func sljedeciAtribut(atr []byte) (vrsta uint16, v, ostatak []byte, ok bool) {
	if len(atr) < 4 {
		return 0, nil, nil, false
	}
	vrsta, d := binary.BigEndian.Uint16(atr[0:2]), int(binary.BigEndian.Uint16(atr[2:4]))
	if 4+d > len(atr) {
		return 0, nil, nil, false
	}
	return vrsta, atr[4 : 4+d], atr[min(len(atr), 4+(d+3)/4*4):], true
}

// adresaAtributa čita vrijednost (XOR-)MAPPED-ADDRESS: nula, obitelj, port i
// adresa; uz xor se port i adresa vraćaju iz XOR-a s kolačićem i ID-om
func adresaAtributa(v []byte, xor bool, id [12]byte) (netip.AddrPort, bool) {
	if len(v) < 4 {
		return netip.AddrPort{}, false
	}
	duljina := map[byte]int{obiteljIPv4: 4, obiteljIPv6: 16}[v[1]]
	if duljina == 0 || len(v) != 4+duljina {
		return netip.AddrPort{}, false
	}
	port := binary.BigEndian.Uint16(v[2:4])
	ip := append([]byte(nil), v[4:]...)
	if xor {
		port ^= stunKolacic >> 16
		maska := binary.BigEndian.AppendUint32(nil, stunKolacic)
		maska = append(maska, id[:]...)
		for i := range ip {
			ip[i] ^= maska[i]
		}
	}
	a, _ := netip.AddrFromSlice(ip) // duljina je 4 ili 16
	return netip.AddrPortFrom(a.Unmap(), port), true
}
