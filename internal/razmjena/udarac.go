package razmjena

// Udarci WEB synca (plan WEB synca, F3a): paketi od 60 B kojima dva
// računala otvaraju NAT jedno prema drugome prije QUIC-a. Udarac nosi
// sesiju i brojač i potpisan je HMAC-om ključem svog smjera. Oba računala
// izvode ključeve oba smjera iz tajni potpisanog poziva i potpisanog
// odziva, tim redom (HKDF-SHA256 bez soli, 32 B, oznaka
// <protokol>-udarac-v1, sesija i smjer), pa udarac
// vrijedi samo u toj sesiji, samo na toj mreži i samo u jednom smjeru:
// udarac odbijen natrag pošiljatelju (javna Wi-Fi mreža) ili primljen od
// samog sebe (dvije kuće s istom podmrežom) ne prolazi (po Tailscaleu, gdje
// paket nosi ključ pošiljatelja). Smjer A je računalo s manjim ključem.
// Brojač je big-endian i zaseban za svaki smjer, a primatelj pamti zadnja
// 64 i ponovljeni odbacuje, kao WireGuard.
//
// Tajnu daju obje strane (F3a-2): pozivatelj u pozivu, pozvani u odzivu.
// Tako ključevi ne ovise samo o slučajnosti jednog računala, a ponovljen
// poziv (pričuva poruka nakon ponovnog pokretanja je prazna, a poziv
// vrijedi 10 min) ne daje ključeve stare sesije: pozvani svaki put doda
// novu tajnu, pa snimljeni stari udarci ne otvaraju vrata za QUIC. Obje
// tajne vidi sastajalište, kao i prije; od njega ovo ne štiti.
//
// Gornja dva bita prvog bajta su 0, inače quic-go paket ne pusti čitaču
// paketa koji nisu QUIC, a prvi bajt nije 0x00 ni 0x01, kojima počinje
// STUN (pravilo 20):
//
//	0      vrsta: 0x10 udarac, 0x11 odgovor
//	1      smjer pošiljatelja: 0 A→B, 1 B→A
//	2      inačica: 1
//	3      0
//	4–19   sesija
//	20–27  brojač
//	28–59  HMAC-SHA256 nad bajtovima 0–27

import (
	"bytes"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/netip"
	"slices"
	"time"
)

const (
	velicinaUdarca      = 60
	okvirUdarca         = 28 // bajtovi pod HMAC-om
	vrstaUdarac    byte = 0x10
	vrstaOdgovor   byte = 0x11
	inacicaUdarca  byte = 1
	smjerAB        byte = 0
	smjerBA        byte = 1
	najmanjaTajna       = 16
)

// ErrKljuceviUdarca: ključevi udarca se ne daju izvesti (sesija nije
// 16 B, jedna od tajni je prekratka ili su oba ključa ista)
var ErrKljuceviUdarca = errors.New("ključevi udarca se ne daju izvesti")

// Greške čitanja udarca
var (
	errOblikUdarca    = errors.New("paket nije udarac ove sesije")
	errVlastitiUdarac = errors.New("udarac vlastitog smjera")
	errPotpisUdarca   = errors.New("potpis udarca ne vrijedi")
)

// KljuceviUdarca su ključevi udaraca jedne sesije, po jedan za svaki smjer
type KljuceviUdarca struct {
	sesija         [16]byte
	saljem, primam [32]byte
	smjer          byte // smjer ovog računala
}

// NoviKljuceviUdarca izvede ključeve sesije (32 heksadecimalna znaka) iz
// tajne potpisanog poziva i tajne potpisanog odziva; moj i drugi su
// ključevi ovog i drugog računala. Sesija sa samim sobom (isti ključ) nema
// ključeva.
func NoviKljuceviUdarca(protokol, sesija string, tajnaPoziva, tajnaOdziva []byte, moj, drugi ed25519.PublicKey) (KljuceviUdarca, error) {
	s, err := hex.DecodeString(sesija)
	if err != nil || len(s) != 16 || min(len(tajnaPoziva), len(tajnaOdziva)) < najmanjaTajna || bytes.Equal(moj, drugi) {
		return KljuceviUdarca{}, ErrKljuceviUdarca
	}
	tajna := slices.Concat(tajnaPoziva, tajnaOdziva)
	k := KljuceviUdarca{sesija: [16]byte(s), smjer: smjerBA}
	if bytes.Compare(moj, drugi) < 0 {
		k.smjer = smjerAB
	}
	k.saljem = kljucSmjera(protokol, k.sesija, tajna, k.smjer)
	k.primam = kljucSmjera(protokol, k.sesija, tajna, k.smjer^1)
	return k, nil
}

// A javlja je li ovo računalo A, ono s manjim ključem
func (k KljuceviUdarca) A() bool { return k.smjer == smjerAB }

// kljucSmjera je ključ HMAC-a za jedan smjer: HKDF bez soli, s oznakom iz
// imena protokola, sesijom i smjerom
func kljucSmjera(protokol string, sesija [16]byte, tajna []byte, smjer byte) [32]byte {
	oznaka := slices.Concat([]byte(protokol+"-udarac-v1"), sesija[:], []byte{smjer})
	b, _ := hkdf.Key(sha256.New, tajna, nil, string(oznaka), 32) // 32 B je daleko ispod granice HKDF-a (255 · 32 B), pa greške nema
	return [32]byte(b)
}

// sastavi je udarac ili odgovor ovog računala s brojačem
func (k KljuceviUdarca) sastavi(vrsta byte, brojac uint64) []byte {
	b := make([]byte, okvirUdarca, velicinaUdarca)
	b[0], b[1], b[2] = vrsta, k.smjer, inacicaUdarca
	copy(b[4:20], k.sesija[:])
	binary.BigEndian.PutUint64(b[20:28], brojac)
	return append(b, potpisUdarca(k.saljem, b)...)
}

// provjeri pročita paket druge strane: vrstu i brojač, kad je oblik
// udarca ove sesije, smjer tuđi i potpis ključem druge strane
func (k KljuceviUdarca) provjeri(b []byte) (vrsta byte, brojac uint64, err error) {
	switch {
	case !jeUdarac(b) || b[2] != inacicaUdarca || sesijaUdarca(b) != k.sesija:
		return 0, 0, errOblikUdarca
	case b[1] == k.smjer:
		return 0, 0, errVlastitiUdarac
	case !hmac.Equal(b[okvirUdarca:], potpisUdarca(k.primam, b[:okvirUdarca])):
		return 0, 0, errPotpisUdarca
	}
	return b[0], binary.BigEndian.Uint64(b[20:28]), nil
}

func potpisUdarca(kljuc [32]byte, okvir []byte) []byte {
	m := hmac.New(sha256.New, kljuc[:])
	m.Write(okvir)
	return m.Sum(nil)
}

// jeUdarac: paket je oblika udarca (duljina i prvi bajt); sesiju i potpis
// provjerava sesija kojoj pripada
func jeUdarac(b []byte) bool {
	return len(b) == velicinaUdarca && (b[0] == vrstaUdarac || b[0] == vrstaOdgovor)
}

// sesijaUdarca je sesija iz paketa oblika udarca
func sesijaUdarca(b []byte) [16]byte { return [16]byte(b[4:20]) }

// oknoUdaraca pamti zadnja 64 brojača druge strane: ponovljen brojač, ili
// stariji od zadnjih 64, se odbija. Nula nije brojač.
type oknoUdaraca struct{ najvisi, vidjeni uint64 }

// primi javlja je li brojač nov i zapamti ga
func (o *oknoUdaraca) primi(n uint64) bool {
	switch {
	case n > o.najvisi:
		o.vidjeni = o.vidjeni<<(n-o.najvisi) | 1 // pomak od 64 i više daje 0
		o.najvisi = n
		return true
	case n == 0 || o.najvisi-n >= 64:
		return false
	}
	bit := uint64(1) << (o.najvisi - n)
	nov := o.vidjeni&bit == 0
	o.vidjeni |= bit
	return nov
}

// ciljUdarca je potpisani kandidat druge strane i kad je na redu
type ciljUdarca struct {
	adresa   netip.AddrPort
	lokalni  bool
	sljedeci time.Time
	razmak   time.Duration
}

// ciljeviUdaraca su potpisani kandidati druge strane koje ovo računalo
// udara: IPv4 adrese s portom, bez vlastitih adresa ovog računala (po
// irohu) i bez dvojnika, najviše 8, redom iz poruke
func ciljeviUdaraca(kandidati []KandidatPoruke, vlastite []netip.AddrPort) []ciljUdarca {
	var out []ciljUdarca
	for _, k := range kandidati {
		a, ok := adresaKandidata(k.Adresa)
		if ok && !slices.Contains(vlastite, a) && !slices.ContainsFunc(out, func(c ciljUdarca) bool { return c.adresa == a }) {
			out = append(out, ciljUdarca{adresa: a, lokalni: k.Vrsta == KandidatLokalni, razmak: prviRazmakUdarca})
		}
	}
	return out[:min(len(out), najviseKandidata)]
}

// adresaKandidata je IPv4 adresa s portom na koju se smije udarati: ne
// nula, petlja, link-local, višeodredišna ni broadcast
func adresaKandidata(s string) (netip.AddrPort, bool) {
	a, err := netip.ParseAddrPort(s)
	a = netip.AddrPortFrom(a.Addr().Unmap(), a.Port())
	return a, err == nil && a.Addr().Is4() && a.Addr().IsGlobalUnicast() && a.Port() != 0
}
