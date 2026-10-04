package peers

// Primanje na daljinu: kad novo računalo i čvor koji ga prima nisu u istoj
// lokalnoj mreži (laptop kod kuće, poslužitelj u uredu), umjesto uparivanja
// putuju dvije datoteke, npr. e-poštom:
//
//  1. zahtjev s novog računala: ime i javni ključ, potpisan tim ključem;
//  2. potvrda od primatelja (nositelj ključa mreže ili ovlašteni primatelj):
//     mreža, članstvo za taj ključ i čvorovi s kojima se može sinkronizirati.
//
// Datoteke se mogu presresti i podmetnuti, pa ih veže tajni kod za primanje
// (8 znakova) koji novo računalo pokaže na ekranu, a čovjek ga telefonom
// pročita primatelju — nikad e-poštom. Zahtjev i potvrda nose dokaz (HMAC)
// ključem izvedenim iz tog koda (scrypt): primatelj prima samo zahtjev čiji
// dokaz odgovara kodu koji mu je pročitan, a novo računalo prihvaća samo
// potvrdu čiji dokaz odgovara njegovom kodu. Kratak kod za usporedbu nije
// dovoljan: ključ s istim kratkim kodom nađe se za nekoliko sekundi, a
// tajni kod iz presretnute datoteke nije moguće pogoditi (31^8 mogućnosti, a
// svaka traži skupi izvod ključa).

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"

	"gocop/internal/imecvora"
	"gocop/internal/razmjena"
)

const (
	verzijaZahtjeva = "gocop-zahtjev/1"
	verzijaPotvrde  = "gocop-potvrda/1"
	najveciZahtjev  = 64 << 10
	najvecaPotvrda  = 1 << 20

	// zahtjevNaCekanjuDatoteka drži zadnji zahtjev ovog čvora i njegov kod
	// dok potvrda ne stigne; uz ključ čvora, ne u bazi (ne sinkronizira se)
	zahtjevNaCekanjuDatoteka = "zahtjev-na-cekanju.json"

	znakoviKoda = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // bez 0/O, 1/I/L
	duljinaKoda = 8
)

// scryptN je cijena izvoda ključa iz koda (64 MiB, desetinke sekunde)
var scryptN = 1 << 16

// ErrKodPrimanja: kod za primanje ne odgovara datoteci
var ErrKodPrimanja = errors.New("kod za primanje ne odgovara")

// Zahtjev je molba novog računala za primanje u mrežu
type Zahtjev struct {
	V       string    `json:"v"`
	Cvor    string    `json:"cvor"`
	Naziv   string    `json:"naziv,omitempty"`
	Kljuc   string    `json:"kljuc"`
	Izdanje string    `json:"izdanje,omitempty"`
	Vrijeme time.Time `json:"vrijeme"`
	Sol     string    `json:"sol"`    // nasumična, za izvod ključa iz koda za primanje
	Dokaz   string    `json:"dokaz"`  // HMAC ključem iz koda za primanje
	Potpis  string    `json:"potpis"` // ključ čvora
}

// signedBytes je ono što pokrivaju i potpis i dokaz
func (z Zahtjev) signedBytes() []byte {
	z.Potpis, z.Dokaz = "", ""
	b, _ := json.Marshal(z)
	return b
}

// noviKodPrimanja je osam znakova bez sličnih: 7KQ4-M2XD
func noviKodPrimanja() string {
	b := make([]byte, duljinaKoda)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = znakoviKoda[int(b[i])%len(znakoviKoda)]
	}
	return string(b[:4]) + "-" + string(b[4:])
}

// normalizirajKod prima kod kako ga čovjek upiše: mala slova, razmaci, crtica
func normalizirajKod(kod string) (string, error) {
	var b strings.Builder
	for _, r := range strings.ToUpper(kod) {
		switch {
		case r == ' ' || r == '-':
			continue
		case strings.ContainsRune(znakoviKoda, r):
			b.WriteRune(r)
		default:
			return "", fmt.Errorf("%w: kod ima samo slova i brojke s ekrana novog računala (npr. 7KQ4-M2XD)", ErrKodPrimanja)
		}
	}
	if b.Len() != duljinaKoda {
		return "", fmt.Errorf("%w: kod ima %d znakova (npr. 7KQ4-M2XD)", ErrKodPrimanja, duljinaKoda)
	}
	return b.String(), nil
}

// kljucIzKoda je ključ dokaza izveden iz koda za primanje i soli zahtjeva
func kljucIzKoda(kod string, sol []byte) ([]byte, error) {
	k, err := normalizirajKod(kod)
	if err != nil {
		return nil, err
	}
	return scrypt.Key([]byte(k), append([]byte("gocop-primanje/1|"), sol...), scryptN, 8, 1, 32)
}

func dokaz(kljuc []byte, vrsta string, b []byte) string {
	m := hmac.New(sha256.New, kljuc)
	m.Write([]byte(vrsta + "|"))
	m.Write(b)
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

func dokazOdgovara(kljuc []byte, vrsta string, b []byte, zapisan string) bool {
	return hmac.Equal([]byte(dokaz(kljuc, vrsta, b)), []byte(zapisan))
}

// zahtjevNaCekanju je zapis zadnjeg zahtjeva ovog čvora
type zahtjevNaCekanju struct {
	Kod     string `json:"kod"`
	Zahtjev string `json:"zahtjev"` // datoteka bajt za bajt (RawMessage bi je sabio)
}

func (s *Service) putZahtjevaNaCekanju() string {
	return filepath.Join(s.node.Dir, zahtjevNaCekanjuDatoteka)
}

// ZahtjevNaCekanju vraća zadnji zahtjev ovog čvora (datoteka) i njegov kod,
// dok ga potvrda ne zatvori
func (s *Service) ZahtjevNaCekanju() (zahtjev []byte, kod string, ok bool) {
	b, err := os.ReadFile(s.putZahtjevaNaCekanju())
	if err != nil {
		return nil, "", false
	}
	var z zahtjevNaCekanju
	if json.Unmarshal(b, &z) != nil || z.Kod == "" || len(z.Zahtjev) == 0 {
		return nil, "", false
	}
	return []byte(z.Zahtjev), z.Kod, true
}

// NapraviZahtjev slaže novi zahtjev ovog čvora (JSON za datoteku) s novim
// kodom za primanje; prethodni zahtjev time više ne vrijedi
func (s *Service) NapraviZahtjev() ([]byte, string, error) {
	if n := s.NetworkInfo(); n != nil {
		return nil, "", fmt.Errorf("ovaj čvor je već u mreži %q", n.Name)
	}
	kod := noviKodPrimanja()
	sol := make([]byte, 16)
	_, _ = rand.Read(sol)             // od Go 1.24 ne vraća grešku
	kljuc, _ := kljucIzKoda(kod, sol) // kod je ispravan po izradi
	z := Zahtjev{
		V: verzijaZahtjeva, Cvor: s.node.ID, Naziv: s.node.Name, Kljuc: s.node.PublicKey(),
		Izdanje: s.node.Version, Vrijeme: time.Now().UTC().Truncate(time.Second),
		Sol: base64.StdEncoding.EncodeToString(sol),
	}
	z.Dokaz = dokaz(kljuc, verzijaZahtjeva, z.signedBytes())
	z.Potpis = base64.StdEncoding.EncodeToString(ed25519.Sign(s.node.key, z.signedBytes()))
	b, _ := json.MarshalIndent(z, "", "  ") // samo nizovi i vrijeme: zapis ne pada
	zapis, _ := json.Marshal(zahtjevNaCekanju{Kod: kod, Zahtjev: string(b)})
	if err := os.WriteFile(s.putZahtjevaNaCekanju(), zapis, 0o600); err != nil {
		return nil, "", fmt.Errorf("zahtjev se ne može zapisati: %w", err)
	}
	return b, kod, nil
}

// ProcitajZahtjev provjerava oblik, ime i potpis zahtjeva (bez koda: kod
// provjerava PrimiZahtjev)
func ProcitajZahtjev(raw []byte) (Zahtjev, error) {
	var z Zahtjev
	if len(raw) > najveciZahtjev {
		return z, errors.New("datoteka je prevelika za zahtjev")
	}
	if err := json.Unmarshal(raw, &z); err != nil || z.V != verzijaZahtjeva {
		return z, errors.New("to nije zahtjev za primanje u goCOP mrežu (ili je iz drugog izdanja programa)")
	}
	if err := imecvora.Provjeri(z.Cvor); err != nil {
		return z, fmt.Errorf("ime čvora u zahtjevu: %w", err)
	}
	return z, z.provjeriPotpis()
}

// provjeriPotpis: zahtjev je potpisao ključ koji nosi, i nosi dokaz koda
func (z Zahtjev) provjeriPotpis() error {
	pub, err := razmjena.ParsePublicKey(z.Kljuc)
	if err != nil {
		return fmt.Errorf("ključ u zahtjevu: %w", err)
	}
	potpis, err := base64.StdEncoding.DecodeString(z.Potpis)
	if err != nil || !ed25519.Verify(pub, z.signedBytes(), potpis) {
		return errors.New("potpis zahtjeva ne štima — datoteka je mijenjana")
	}
	if sol, err := base64.StdEncoding.DecodeString(z.Sol); err != nil || len(sol) < 16 || z.Dokaz == "" {
		return errors.New("zahtjevu nedostaje dokaz koda za primanje")
	}
	return nil
}

// Potvrda je odgovor primatelja: članstvo i s kim se novi čvor sinkronizira
type Potvrda struct {
	V          string              `json:"v"`
	Mreza      string              `json:"mreza"`
	KljucMreze string              `json:"kljuc_mreze"`
	Clanstvo   razmjena.Membership `json:"clanstvo"`
	Ovlast     *razmjena.Ovlast    `json:"ovlast,omitempty"`
	Izdao      string              `json:"izdao"`
	Izdano     time.Time           `json:"izdano"`
	Cvorovi    []CvorPotvrde       `json:"cvorovi"`
	Sol        string              `json:"sol"`             // sol zahtjeva na koji odgovara
	Dokaz      string              `json:"dokaz,omitempty"` // HMAC ključem iz koda za primanje
}

// CvorPotvrde je čvor s kojim se novi čvor može sinkronizirati, s članstvom
// da mu novi čvor vjeruje prije prve razmjene
type CvorPotvrde struct {
	Peer
	Clanstvo *razmjena.Membership `json:"clanstvo,omitempty"`
}

// kanonPotvrde je potvrda bez dokaza, u obliku koji ne ovisi o razmacima i
// redu vrhovnih polja: isti bajtovi kod primatelja (iz strukture) i kod
// novog računala (iz datoteke)
func kanonPotvrde(raw []byte) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	delete(m, "dokaz")
	return json.Marshal(m)
}

// PrimiZahtjev prima čvor iz zahtjeva u mrežu, ako kod za primanje (koji je
// čovjek s tog računala pročitao telefonom) odgovara zahtjevu, i vraća
// potvrdu (JSON za datoteku). sOvlascu mu daje i ovlast da sam prima druge
// (samo nositelj ključa mreže).
//
// Sve provjere (kod, mreža, ovlast, ime) prolaze prije ikakvog upisa:
// odbijen zahtjev ne ostavlja ni članstvo ni ovlast.
func (s *Service) PrimiZahtjev(ctx context.Context, raw []byte, kod string, sOvlascu bool) ([]byte, error) {
	z, kljuc, err := procitajZahtjevSKodom(raw, kod)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	if network == nil {
		return nil, errors.New("ovaj čvor nije ni u jednoj mreži")
	}
	pub, _ := razmjena.ParsePublicKey(z.Kljuc) // ProcitajZahtjev ga je provjerio
	ovlast, err := ovlastAkoJeTrazena(*network, z.Cvor, pub, s.node.ID, sOvlascu)
	if err != nil {
		return nil, err
	}
	if err := s.provjeriImeDrugog(ctx, z.Cvor, pub); err != nil {
		return nil, err
	}
	m, err := s.izdajClanstvo(ctx, z.Cvor, pub)
	if err != nil {
		return nil, err
	}
	if err := s.upisiPrimljenog(ctx, m, ovlast); err != nil {
		return nil, err
	}
	cvorovi, err := s.cvoroviZaPotvrdu(ctx)
	if err != nil {
		return nil, err
	}
	p := Potvrda{
		V: verzijaPotvrde, Mreza: network.Name, KljucMreze: razmjena.PublicKeyString(network.Public),
		Clanstvo: m, Ovlast: ovlast, Izdao: s.node.ID, Izdano: time.Now().UTC().Truncate(time.Second),
		Cvorovi: cvorovi, Sol: z.Sol,
	}
	return p.potpisanKodom(kljuc), nil
}

// procitajZahtjevSKodom je ProcitajZahtjev uz kod za primanje: vraća
// zahtjev i ključ dokaza, ako kod odgovara zahtjevu
func procitajZahtjevSKodom(raw []byte, kod string) (Zahtjev, []byte, error) {
	z, err := ProcitajZahtjev(raw)
	if err != nil {
		return z, nil, err
	}
	kljuc, err := z.kljucAkoKodOdgovara(kod)
	return z, kljuc, err
}

// ovlastAkoJeTrazena je ovlast za primanje za novi čvor kad je tražena;
// daje je samo nositelj ključa mreže (NetworkKey.Ovlasti)
func ovlastAkoJeTrazena(network razmjena.NetworkKey, id string, pub ed25519.PublicKey, izdao string, trazena bool) (*razmjena.Ovlast, error) {
	if !trazena {
		return nil, nil
	}
	o, err := network.Ovlasti(id, pub, izdao, OvlastValidity)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// upisiPrimljenog upisuje članstvo primljenog čvora i, ako je dana, ovlast
func (s *Service) upisiPrimljenog(ctx context.Context, m razmjena.Membership, o *razmjena.Ovlast) error {
	if err := s.saveMembership(ctx, m); err != nil {
		return err
	}
	if o == nil {
		return nil
	}
	return s.saveOvlast(ctx, *o)
}

// kljucAkoKodOdgovara je ključ dokaza iz koda za primanje, ako kod odgovara
// dokazu u zahtjevu (kod koji je čovjek s tog računala pročitao telefonom)
func (z Zahtjev) kljucAkoKodOdgovara(kod string) ([]byte, error) {
	sol, _ := base64.StdEncoding.DecodeString(z.Sol) // ProcitajZahtjev ju je provjerio
	kljuc, err := kljucIzKoda(kod, sol)
	if err != nil {
		return nil, err
	}
	if !dokazOdgovara(kljuc, verzijaZahtjeva, z.signedBytes(), z.Dokaz) {
		return nil, fmt.Errorf("%w zahtjevu: provjerite kod s čovjekom na tom računalu; ako se i dalje ne slaže, zahtjev je možda podmetnut — neka napravi novi", ErrKodPrimanja)
	}
	return kljuc, nil
}

// potpisanKodom je potvrda za datoteku, s dokazom ključem iz koda za
// primanje nad svim ostalim poljima. Potvrda nema ničeg što JSON ne zna
// zapisati, pa zapis ne može pasti.
func (p Potvrda) potpisanKodom(kljuc []byte) []byte {
	p.Dokaz = ""
	bez, _ := json.Marshal(p)
	kanon, _ := kanonPotvrde(bez)
	p.Dokaz = dokaz(kljuc, verzijaPotvrde, kanon)
	b, _ := json.MarshalIndent(p, "", "  ")
	return b
}

// cvoroviZaPotvrdu su ovaj čvor i poznati čvorovi s adresom i važećim
// članstvom; stalno izloženi (domena, tunel) prvi
func (s *Service) cvoroviZaPotvrdu(ctx context.Context) ([]CvorPotvrde, error) {
	self, err := s.SelfPeer(ctx)
	if err != nil {
		return nil, err
	}
	ostali, err := s.ListPeers(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()

	var izlozeni, lokalni []CvorPotvrde
	for _, p := range append([]Peer{self}, ostali...) {
		c, ok := s.cvorZaPotvrdu(ctx, network.Public, p)
		switch {
		case !ok:
		case p.IsBootstrap:
			izlozeni = append(izlozeni, c)
		default:
			lokalni = append(lokalni, c)
		}
	}
	return append(izlozeni, lokalni...), nil
}

// cvorZaPotvrdu: čvor ide u potvrdu ako ima adresu i važeće članstvo
// (novi čvor mu tako vjeruje prije prve razmjene); bilješke o razmjeni ne
func (s *Service) cvorZaPotvrdu(ctx context.Context, mrezni ed25519.PublicKey, p Peer) (CvorPotvrde, bool) {
	if len(p.Addresses) == 0 {
		return CvorPotvrde{}, false
	}
	pub, err := razmjena.ParsePublicKey(p.PublicKey)
	if err != nil {
		return CvorPotvrde{}, false
	}
	m, err := s.getMembership(ctx, p.NodeID)
	if err != nil || m == nil || s.provjeriClanstvo(ctx, mrezni, *m, pub) != nil {
		return CvorPotvrde{}, false
	}
	p.LastSeen, p.LastSync, p.LastSyncNote = nil, nil, ""
	return CvorPotvrde{Peer: p, Clanstvo: m}, true
}

// procitajPotvrdu provjerava potvrdu za ovaj čvor: dokaz kodom za primanje
// zadnjeg zahtjeva, članstvo za ključ ovog čvora i mrežu
func (s *Service) procitajPotvrdu(raw []byte) (Potvrda, error) {
	var p Potvrda
	if len(raw) > najvecaPotvrda {
		return p, errors.New("datoteka je prevelika za potvrdu")
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.V != verzijaPotvrde {
		return p, errors.New("to nije potvrda primanja u goCOP mrežu (ili je iz drugog izdanja programa)")
	}
	if err := s.provjeriDokazPotvrde(raw, p); err != nil {
		return p, err
	}
	return p, s.provjeriSadrzajPotvrde(p)
}

// provjeriSadrzajPotvrde: članstvo je za ovaj čvor i vrijedi u mreži iz
// potvrde, a čvor nije već u nekoj drugoj mreži
func (s *Service) provjeriSadrzajPotvrde(p Potvrda) error {
	mrezni, err := razmjena.ParsePublicKey(p.KljucMreze)
	if err != nil {
		return fmt.Errorf("ključ mreže u potvrdi: %w", err)
	}
	if p.Clanstvo.DeviceID != s.node.ID || p.Clanstvo.DeviceKey != s.node.PublicKey() {
		return fmt.Errorf("potvrda je izdana čvoru %s, ne ovom (%s)", p.Clanstvo.DeviceID, s.node.ID)
	}
	if err := p.Clanstvo.Verify(mrezni, s.node.key.Public().(ed25519.PublicKey), time.Now()); err != nil {
		return fmt.Errorf("članstvo u potvrdi ne vrijedi: %w", err)
	}
	if n := s.NetworkInfo(); n != nil && n.PublicKey != p.KljucMreze {
		return fmt.Errorf("ovaj čvor je već u drugoj mreži (%q)", n.Name)
	}
	return nil
}

// provjeriDokazPotvrde: potvrda odgovara zadnjem zahtjevu ovog čvora i
// njegovom kodu za primanje (inače je mijenjana ili podmetnuta)
func (s *Service) provjeriDokazPotvrde(raw []byte, p Potvrda) error {
	zahtjev, kod, ok := s.ZahtjevNaCekanju()
	if !ok {
		return errors.New("ovo računalo nema zahtjev na čekanju — napravite zahtjev, pošaljite ga primatelju i učitajte potvrdu koju vrati")
	}
	var z Zahtjev
	if err := json.Unmarshal(zahtjev, &z); err != nil {
		return err
	}
	if p.Sol != z.Sol {
		return fmt.Errorf("potvrda odgovara starijem zahtjevu ovog računala; tražite potvrdu za zadnji zahtjev (kod %s)", kod)
	}
	sol, _ := base64.StdEncoding.DecodeString(z.Sol)
	kljuc, err := kljucIzKoda(kod, sol)
	if err != nil {
		return err
	}
	kanon, err := kanonPotvrde(raw)
	if err != nil || !dokazOdgovara(kljuc, verzijaPotvrde, kanon, p.Dokaz) {
		return errors.New("potvrda ne odgovara kodu za primanje ovog računala — mijenjana je ili podmetnuta i ne prihvaća se")
	}
	return nil
}

// UvozPotvrde je ishod uvoza za ekran
type UvozPotvrde struct {
	Mreza   string   `json:"mreza"`
	Izdao   string   `json:"izdao"`
	Cvorovi []string `json:"cvorovi"` // s kojima se novi čvor sada može sinkronizirati
	Ovlast  bool     `json:"ovlast"`
}

// UveziPotvrdu prihvaća potvrdu: čvor ulazi u mrežu, upisuje čvorove s
// kojima se sinkronizira i njihova članstva, a zahtjev na čekanju se briše.
// Ista mreža smije ponovno (obnova članstva, nova ovlast).
func (s *Service) UveziPotvrdu(ctx context.Context, raw []byte) (UvozPotvrde, error) {
	p, err := s.procitajPotvrdu(raw)
	if err != nil {
		return UvozPotvrde{}, err
	}
	if err := s.udjiIliObnovi(ctx, p); err != nil {
		return UvozPotvrde{}, err
	}
	_ = os.Remove(s.putZahtjevaNaCekanju())

	mrezni, _ := razmjena.ParsePublicKey(p.KljucMreze) // procitajPotvrdu ga je provjerio
	out := UvozPotvrde{Mreza: p.Mreza, Izdao: p.Izdao}
	if out.Ovlast, err = s.spremiSvojuOvlast(ctx, mrezni, p.Ovlast); err != nil {
		return out, err
	}
	out.Cvorovi, err = s.upisiCvoroveIzPotvrde(ctx, mrezni, p.Cvorovi)
	return out, err
}

// upisiCvoroveIzPotvrde upisuje čvorove iz potvrde; vraća one koji su upisani
func (s *Service) upisiCvoroveIzPotvrde(ctx context.Context, mrezni ed25519.PublicKey, cvorovi []CvorPotvrde) ([]string, error) {
	var upisani []string
	for _, c := range cvorovi {
		ok, err := s.upisiCvorIzPotvrde(ctx, mrezni, c)
		if err != nil {
			return upisani, err
		}
		if ok {
			upisani = append(upisani, c.NodeID)
		}
	}
	return upisani, nil
}

// udjiIliObnovi: čvor bez mreže ulazi u mrežu iz potvrde, a čvor iste
// mreže samo upisuje novo članstvo
func (s *Service) udjiIliObnovi(ctx context.Context, p Potvrda) error {
	if s.NetworkInfo() != nil {
		return s.saveMembership(ctx, p.Clanstvo)
	}
	return s.joinNetwork(ctx, welcomePack{NetworkName: p.Mreza, NetworkKey: p.KljucMreze, ForYou: &p.Clanstvo})
}

// spremiSvojuOvlast upisuje ovlast za primanje iz potvrde, ako je izdana
// baš ovom čvoru i vrijedi; javlja je li upisana
func (s *Service) spremiSvojuOvlast(ctx context.Context, mrezni ed25519.PublicKey, o *razmjena.Ovlast) (bool, error) {
	if o == nil || o.DeviceID != s.node.ID || o.DeviceKey != s.node.PublicKey() || s.provjeriOvlast(ctx, mrezni, *o) != nil {
		return false, nil
	}
	return true, s.saveOvlast(ctx, *o)
}

// upisiCvorIzPotvrde upisuje čvor iz potvrde i njegovo članstvo, ako mu
// članstvo vrijedi i ime nije tuđe; adrese spoji s već poznatima. Javlja je
// li čvor upisan.
func (s *Service) upisiCvorIzPotvrde(ctx context.Context, mrezni ed25519.PublicKey, c CvorPotvrde) (bool, error) {
	if c.NodeID == s.node.ID || c.Clanstvo == nil || c.Clanstvo.DeviceID != c.NodeID {
		return false, nil
	}
	pub, err := razmjena.ParsePublicKey(c.PublicKey)
	if err != nil || s.provjeriClanstvo(ctx, mrezni, *c.Clanstvo, pub) != nil || s.provjeriImeDrugog(ctx, c.NodeID, pub) != nil {
		return false, nil
	}
	if err := s.saveMembership(ctx, *c.Clanstvo); err != nil {
		return false, err
	}
	peer := c.Peer
	peer.LastSeen, peer.LastSync, peer.LastSyncNote, peer.CreatedAt = nil, nil, "", time.Time{}
	if postojeci, _ := s.GetPeer(ctx, peer.NodeID); postojeci != nil {
		peer.Addresses = dedupe(append(postojeci.Addresses, peer.Addresses...))
		peer.CreatedAt = postojeci.CreatedAt
		peer.IsBootstrap = peer.IsBootstrap || postojeci.IsBootstrap
	}
	return true, s.SavePeer(ctx, peer)
}
