package razmjena

// Oblik udarca, ključevi smjerova, okno brojača i ciljevi udaraca
// (udarac.go).

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"net/netip"
	"testing"
)

const sesijaUdaraca = "00112233445566778899aabbccddeeff"

var tajnaUdaraca = []byte("tajna iz potpisanog poziva, 32 B!")

// kljuceviPara su ključevi udaraca obiju strana iste sesije: a je
// računalo s manjim ključem
func kljuceviPara(t *testing.T) (a, b KljuceviUdarca) {
	t.Helper()
	ka, kb := newKey(t).Public().(ed25519.PublicKey), newKey(t).Public().(ed25519.PublicKey)
	if bytes.Compare(ka, kb) > 0 {
		ka, kb = kb, ka
	}
	a, err := NoviKljuceviUdarca("testproto", sesijaUdaraca, tajnaUdaraca, ka, kb)
	bezGreske(t, err)
	b, err = NoviKljuceviUdarca("testproto", sesijaUdaraca, tajnaUdaraca, kb, ka)
	bezGreske(t, err)
	return a, b
}

// Ključ za pisanje jedne strane je ključ za čitanje druge; A je računalo s
// manjim ključem; ključevi ovise o protokolu, sesiji i tajni.
func TestKljuceviUdarca(t *testing.T) {
	a, b := kljuceviPara(t)
	if !a.A() || b.A() || a.saljem != b.primam || a.primam != b.saljem || a.saljem == a.primam {
		t.Fatalf("smjerovi: A %v, B %v", a.A(), b.A())
	}
	ka, kb := newKey(t).Public().(ed25519.PublicKey), newKey(t).Public().(ed25519.PublicKey)
	osnovni, err := NoviKljuceviUdarca("testproto", sesijaUdaraca, tajnaUdaraca, ka, kb)
	bezGreske(t, err)
	for ime, c := range map[string]struct {
		protokol, sesija string
		tajna            []byte
	}{
		"drugi protokol": {"gocop", sesijaUdaraca, tajnaUdaraca},
		"druga sesija":   {"testproto", "ffeeddccbbaa99887766554433221100", tajnaUdaraca},
		"druga tajna":    {"testproto", sesijaUdaraca, []byte("neka sasvim druga tajna poziva")},
	} {
		k, err := NoviKljuceviUdarca(c.protokol, c.sesija, c.tajna, ka, kb)
		if err != nil || k.saljem == osnovni.saljem || k.primam == osnovni.primam {
			t.Errorf("%s: isti ključevi (%v)", ime, err)
		}
	}
	for ime, c := range map[string]struct {
		sesija     string
		tajna      []byte
		moj, drugi ed25519.PublicKey
	}{
		"sesija nije heks":      {"nije-heks", tajnaUdaraca, ka, kb},
		"sesija od 8 B":         {"0011223344556677", tajnaUdaraca, ka, kb},
		"kratka tajna":          {sesijaUdaraca, []byte("kratko"), ka, kb},
		"sesija sa samim sobom": {sesijaUdaraca, tajnaUdaraca, ka, ka},
	} {
		if _, err := NoviKljuceviUdarca("testproto", c.sesija, c.tajna, c.moj, c.drugi); !errors.Is(err, ErrKljuceviUdarca) {
			t.Errorf("%s: %v", ime, err)
		}
	}
}

// Udarac ima 60 B, gornja dva bita prvog bajta su 0, ne počinje kao STUN i
// ne izgleda kao STUN; druga strana ga pročita, a pošiljatelj ne.
func TestOblikUdarca(t *testing.T) {
	a, b := kljuceviPara(t)
	for _, vrsta := range []byte{vrstaUdarac, vrstaOdgovor} {
		p := a.sastavi(vrsta, 7)
		if len(p) != 60 || p[0]&0xC0 != 0 || p[0] == 0x00 || p[0] == 0x01 || jeSTUN(p) || !jeUdarac(p) {
			t.Fatalf("oblik %x", p)
		}
		if v, n, err := b.provjeri(p); err != nil || v != vrsta || n != 7 {
			t.Errorf("druga strana: %x %d %v", v, n, err)
		}
		if _, _, err := a.provjeri(p); !errors.Is(err, errVlastitiUdarac) {
			t.Errorf("vlastiti udarac: %v", err)
		}
	}
	if jeUdarac(zahtjevBinding([12]byte{1})) {
		t.Error("STUN zahtjev izgleda kao udarac")
	}
	// isti par i sesija, ali tajna drugog poziva
	tudji, err := NoviKljuceviUdarca("testproto", sesijaUdaraca, []byte("tajna nekog drugog poziva, 32 B"), newKey(t).Public().(ed25519.PublicKey), newKey(t).Public().(ed25519.PublicKey))
	bezGreske(t, err)
	tudji.smjer = b.smjer
	ispravan := a.sastavi(vrstaUdarac, 1)
	izmijeni := func(i int, v byte) []byte {
		p := bytes.Clone(ispravan)
		p[i] = v
		return p
	}
	for ime, c := range map[string]struct {
		p    []byte
		k    KljuceviUdarca
		want error
	}{
		"kratak":            {ispravan[:59], b, errOblikUdarca},
		"druga vrsta":       {izmijeni(0, 0x12), b, errOblikUdarca},
		"druga inačica":     {izmijeni(2, 2), b, errOblikUdarca},
		"druga sesija":      {izmijeni(4, ispravan[4]^1), b, errOblikUdarca},
		"izmijenjen brojač": {izmijeni(27, ispravan[27]^1), b, errPotpisUdarca},
		"izmijenjen potpis": {izmijeni(59, ispravan[59]^1), b, errPotpisUdarca},
		"tuđi smjer":        {izmijeni(1, 7), b, errPotpisUdarca},
		"tuđi ključevi":     {ispravan, tudji, errPotpisUdarca},
	} {
		if _, _, err := c.k.provjeri(c.p); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, želim %v", ime, err, c.want)
		}
	}
}

// Okno pamti zadnja 64 brojača: ponovljen, prestar i nula se odbijaju, a
// brojači koji stignu izvan reda prolaze jednom.
func TestOknoUdaraca(t *testing.T) {
	var o oknoUdaraca
	for _, c := range []struct {
		n    uint64
		want bool
	}{
		{0, false}, {1, true}, {1, false}, {3, true}, {2, true}, {2, false}, {3, false},
		{70, true}, {6, false}, {7, true}, {7, false}, {69, true}, {1000, true}, {999, true}, {936, false}, {937, true},
	} {
		if got := o.primi(c.n); got != c.want {
			t.Errorf("brojač %d: %v, želim %v", c.n, got, c.want)
		}
	}
}

// Udaraju se samo ispravne IPv4 adrese s portom iz potpisanih kandidata,
// bez vlastitih adresa i dvojnika, najviše 8, redom iz poruke.
func TestCiljeviUdaraca(t *testing.T) {
	vlastita := netip.MustParseAddrPort("192.168.1.23:4713")
	kandidati := []KandidatPoruke{
		{Vrsta: KandidatJavni, Adresa: "198.51.100.7:4713"},
		{Vrsta: KandidatLokalni, Adresa: "192.168.1.23:4713"}, // vlastita adresa (druga kuća, ista podmreža)
		{Vrsta: KandidatLokalni, Adresa: "192.168.1.40:4713"},
		{Vrsta: KandidatJavni, Adresa: "198.51.100.7:4713"}, // dvojnik
		{Vrsta: KandidatJavni, Adresa: "[2001:db8::1]:4713"},
		{Vrsta: KandidatJavni, Adresa: "198.51.100.8:0"},
		{Vrsta: KandidatJavni, Adresa: "127.0.0.1:4713"},
		{Vrsta: KandidatJavni, Adresa: "224.0.0.1:4713"},
		{Vrsta: KandidatJavni, Adresa: "255.255.255.255:4713"},
		{Vrsta: KandidatJavni, Adresa: "nije adresa"},
		{Vrsta: KandidatJavni, Adresa: "[::ffff:198.51.100.9]:4713"},
	}
	for i := range 10 {
		kandidati = append(kandidati, KandidatPoruke{Vrsta: KandidatLokalni, Adresa: netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, 0, 0, byte(i + 1)}), 4713).String()})
	}
	c := ciljeviUdaraca(kandidati, []netip.AddrPort{vlastita})
	want := []string{"198.51.100.7:4713", "192.168.1.40:4713", "198.51.100.9:4713", "10.0.0.1:4713", "10.0.0.2:4713", "10.0.0.3:4713", "10.0.0.4:4713", "10.0.0.5:4713"}
	if len(c) != len(want) {
		t.Fatalf("ciljevi %+v", c)
	}
	for i, w := range want {
		if c[i].adresa.String() != w || c[i].lokalni != (i != 0 && i != 2) || c[i].razmak != prviRazmakUdarca {
			t.Errorf("%d: %+v, želim %s", i, c[i], w)
		}
	}
}
