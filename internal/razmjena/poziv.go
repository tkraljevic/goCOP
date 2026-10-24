package razmjena

// Potpisane poruke WEB synca (plan WEB synca, F2b): poziv, odziv i sinkro.
// Idu preko sastajališta, koje ih samo prosljeđuje: čita kome idu i koja je
// sesija, ali tijelo ne mijenja. Pošiljatelj potpiše bajtove tijela svojim
// ključem, uz prefiks iz imena protokola i vrste („gocop-poziv-v1|”), pa
// potpis ne vrijedi ni za drugu vrstu poruke ni za drugu mrežu. Primatelj
// provjeri pošiljatelja (član mreže), potpis, da je poruka za njega i da je
// stigla preko njegova sastajališta, vrijeme (±10 min) i da je nije već
// vidio (PricuvaPoruka). Što poruka traži, čitaju F3a i F3b.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// Vrste potpisanih poruka
const (
	VrstaPoziv  = "poziv"
	VrstaOdziv  = "odziv"
	VrstaSinkro = "sinkro"
)

// odstupanjePoruke je koliko izdano smije odstupati od sata primatelja:
// uredska i terenska računala znaju odstupati minutama
const odstupanjePoruke = 10 * time.Minute

// najvisePoruka je koliko viđenih poruka pričuva drži; puna odbija nove
const najvisePoruka = 8192

// najvisePokusaja je koliko pokušaja probijanja jedna sesija ima (sinkro)
const najvisePokusajaSinkra = 3

// KandidatPoruke je adresa na kojoj pošiljatelj prima udarce
type KandidatPoruke struct {
	Vrsta  string `json:"vrsta"` // javna ili lokalna
	Adresa string `json:"adresa"`
}

// NATPoruke je vrsta mreže pošiljatelja kad je poruka nastala
type NATPoruke struct {
	Vrsta       string `json:"vrsta,omitempty"`
	UDPBlokiran bool   `json:"udpBlokiran,omitempty"`
}

// TijeloPoruke je ono što pošiljatelj potpiše. Od, Za i Sastajaliste su
// javni ključevi (PublicKeyString).
type TijeloPoruke struct {
	Ver          int              `json:"ver"`
	Sesija       string           `json:"sesija"`
	Od           string           `json:"od"`
	Za           string           `json:"za"`
	Sastajaliste string           `json:"sastajaliste"`
	Izdano       int64            `json:"izdano"`
	Tajna        []byte           `json:"tajna,omitempty"`
	Kandidati    []KandidatPoruke `json:"kandidati,omitempty"`
	NAT          *NATPoruke       `json:"nat,omitempty"`
	Odluka       string           `json:"odluka,omitempty"`   // odziv: probaj ili odbij
	Razlog       string           `json:"razlog,omitempty"`   // odziv: zašto odbija
	ObradaMs     int              `json:"obradaMs,omitempty"` // odziv: koliko je primatelj radio
	Pokusaj      int              `json:"pokusaj,omitempty"`  // sinkro: 1–3
	RTTMs        int              `json:"rttMs,omitempty"`    // sinkro
}

// PotpisanaPoruka je tijelo kako ga je pošiljatelj zapisao i potpis nad
// njim. Vrsta putuje u omotu signala.
type PotpisanaPoruka struct {
	Vrsta  string `json:"-"`
	Tijelo []byte `json:"tijelo"`
	Potpis []byte `json:"potpis"`
}

// Greške provjere poruke
var (
	ErrNepoznatPosiljatelj = errors.New("poruku je poslalo računalo koje nije član mreže")
	ErrPotpisPoruke        = errors.New("potpis poruke ne vrijedi")
	ErrNijeZaMene          = errors.New("poruka nije za ovo računalo")
	ErrDrugoSastajaliste   = errors.New("poruka je stigla preko drugog sastajališta")
	ErrVrijemePoruke       = errors.New("vrijeme poruke odstupa više od 10 min")
	ErrPonovljenaPoruka    = errors.New("ista poruka je već stigla")
	ErrPricuvaPuna         = errors.New("previše poruka u zadnjih 10 min")
	ErrOblikPoruke         = errors.New("poruka nije ispravnog oblika")
)

func prefiksPoruke(protokol, vrsta string) []byte { return []byte(protokol + "-" + vrsta + "-v1|") }

// NovaSesija je nasumična oznaka sesije: 16 B kao 32 heksadecimalna znaka
func NovaSesija() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand ne vraća grešku
	return hex.EncodeToString(b[:])
}

// Potpisi zapiše tijelo i potpiše ga ključem; Ver i Od postavi sam, a
// Izdano kad ga nema
func Potpisi(priv ed25519.PrivateKey, protokol, vrsta string, t TijeloPoruke) PotpisanaPoruka {
	t.Ver = 1
	t.Od = PublicKeyString(priv.Public().(ed25519.PublicKey))
	if t.Izdano == 0 {
		t.Izdano = time.Now().Unix()
	}
	b, _ := json.Marshal(t) // tijelo je od tipova koji se uvijek daju zapisati
	return PotpisanaPoruka{Vrsta: vrsta, Tijelo: b, Potpis: ed25519.Sign(priv, append(prefiksPoruke(protokol, vrsta), b...))}
}

// Zaglavlje je tijelo pročitano bez provjere potpisa: sastajalište iz
// njega čita kome poruka ide i koja je sesija
func (p PotpisanaPoruka) Zaglavlje() (TijeloPoruke, error) {
	var t TijeloPoruke
	if err := json.Unmarshal(p.Tijelo, &t); err != nil {
		return TijeloPoruke{}, fmt.Errorf("%w: %v", ErrOblikPoruke, err)
	}
	return t, nil
}

// ProvjeraPoruka je ono što primatelj zna kad provjerava poruku
type ProvjeraPoruka struct {
	Protokol string
	// Moj je ključ ovog računala, a Sastajaliste ključ njegovog sastajališta
	// (na samom sastajalištu isti kao Moj)
	Moj, Sastajaliste ed25519.PublicKey
	Vjeruje           func(ed25519.PublicKey) bool // je li pošiljatelj član mreže
	Sat               func() time.Time
	Pricuva           *PricuvaPoruka
}

// Provjeri provjeri poruku i zapamti je; vraća tijelo kad prođe
func (pp ProvjeraPoruka) Provjeri(p PotpisanaPoruka) (TijeloPoruke, error) {
	t, err := pp.potpisana(p)
	if err != nil {
		return TijeloPoruke{}, err
	}
	sad := pp.Sat()
	for _, greska := range []error{pp.adresirana(t), vrijemeUnutar(t, sad), oblikVrste(p.Vrsta, t)} {
		if greska != nil {
			return TijeloPoruke{}, greska
		}
	}
	return t, pp.Pricuva.Zapamti(p.Vrsta, t, sad)
}

// potpisana pročita tijelo i provjeri da ga je potpisao član mreže
func (pp ProvjeraPoruka) potpisana(p PotpisanaPoruka) (TijeloPoruke, error) {
	t, err := p.Zaglavlje()
	if err != nil {
		return TijeloPoruke{}, err
	}
	od, err := ParsePublicKey(t.Od)
	if err != nil || !pp.Vjeruje(od) {
		return TijeloPoruke{}, ErrNepoznatPosiljatelj
	}
	if !ed25519.Verify(od, append(prefiksPoruke(pp.Protokol, p.Vrsta), p.Tijelo...), p.Potpis) {
		return TijeloPoruke{}, ErrPotpisPoruke
	}
	return t, nil
}

// adresirana: poruka je za ovo računalo i stigla je preko njegova
// sastajališta, pa se ne da ponoviti kod drugoga
func (pp ProvjeraPoruka) adresirana(t TijeloPoruke) error {
	if t.Za != PublicKeyString(pp.Moj) {
		return ErrNijeZaMene
	}
	if t.Sastajaliste != PublicKeyString(pp.Sastajaliste) {
		return ErrDrugoSastajaliste
	}
	return nil
}

func vrijemeUnutar(t TijeloPoruke, sad time.Time) error {
	if (sad.Sub(time.Unix(t.Izdano, 0))).Abs() > odstupanjePoruke {
		return ErrVrijemePoruke
	}
	return nil
}

// oblikVrste: inačica, sesija od 32 heksadecimalna znaka, a pokušaj samo uz
// sinkro, od 1 do 3
func oblikVrste(vrsta string, t TijeloPoruke) error {
	b, err := hex.DecodeString(t.Sesija)
	sinkro := vrsta == VrstaSinkro
	if t.Ver < 1 || err != nil || len(b) != 16 || sinkro != (t.Pokusaj > 0) || t.Pokusaj > najvisePokusajaSinkra {
		return ErrOblikPoruke
	}
	return nil
}

// PricuvaPoruka pamti viđene poruke dok im ne istekne vrijeme, pa se ista
// poruka ne prima dvaput. Puna odbija nove, a ne izbacuje stare: inače bi
// poplava novih poruka otvorila mjesto ponovljenoj staroj.
type PricuvaPoruka struct {
	mu      sync.Mutex
	vidjeno map[string]time.Time // poruka → kad smije otići
}

// NovaPricuvaPoruka je prazna pričuva
func NovaPricuvaPoruka() *PricuvaPoruka { return &PricuvaPoruka{vidjeno: map[string]time.Time{}} }

// Zapamti zapamti poruku; greška kad je već viđena ili je pričuva puna
func (c *PricuvaPoruka) Zapamti(vrsta string, t TijeloPoruke, sad time.Time) error {
	kljuc := vrsta + "|" + t.Od + "|" + t.Sesija + "|" + strconv.Itoa(t.Pokusaj)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.vidjeno) >= najvisePoruka {
		c.ocisti(sad)
	}
	if _, ima := c.vidjeno[kljuc]; ima {
		return ErrPonovljenaPoruka
	}
	if len(c.vidjeno) >= najvisePoruka {
		return ErrPricuvaPuna
	}
	// minuta više od prozora: poruka na samom rubu prozora ne smije proći
	// nakon što ode iz pričuve
	c.vidjeno[kljuc] = time.Unix(t.Izdano, 0).Add(odstupanjePoruke + time.Minute)
	return nil
}

// ocisti makne poruke kojima je vrijeme isteklo
func (c *PricuvaPoruka) ocisti(sad time.Time) {
	for k, do := range c.vidjeno {
		if sad.After(do) {
			delete(c.vidjeno, k)
		}
	}
}
