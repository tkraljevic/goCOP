package peers

// Šifrirana kopija ključa mreže (G9b; Odluke (18. 10. 2026.) i
// (23. 10. 2026.)). Ključ mreže ne smije biti samo na jednom računalu: kad
// ono propadne, nitko ne može obnoviti ovlasti za primanje, pa se mreža za
// najviše dvije godine raspadne. Računalo s ključem zato sprema kopiju za
// hladni disk, šifriranu posebnom lozinkom od najmanje 12 znakova (scrypt i
// AES-256-GCM), a drugo računalo iste mreže kopiju učita i time i ono drži
// ključ mreže. Lozinka se ne pamti nigdje.

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/scrypt"

	"gocop/internal/models"
	"gocop/internal/razmjena"
)

const (
	// oznakaKopijeKljuca je oznaka formata datoteke kopije; ostaje ista i
	// uz drugo ime protokola
	oznakaKopijeKljuca = "gocop-kljuc-mreze/1"
	// NajkracaLozinkaKopije je najmanji broj znakova lozinke kopije
	NajkracaLozinkaKopije = 12
	// najveciScryptN ograđuje cijenu iz datoteke: tuđa datoteka ne smije
	// zauzeti gigabajte memorije
	najveciScryptN = 1 << 20
	// najvecaKopija je najveća datoteka kopije koja se čita
	najvecaKopija = 16 << 10
)

// ErrLozinkaKopije: lozinka ne odgovara kopiji ili je datoteka mijenjana
var ErrLozinkaKopije = errors.New("lozinka ne odgovara kopiji ključa mreže, ili je datoteka promijenjena")

// kopijaKljuca je datoteka kopije: što je u njoj otvoreno (mreža, javni
// ključ, cijena izvoda) i šifrirano sjeme ključa mreže
type kopijaKljuca struct {
	V         string `json:"v"`
	Mreza     string `json:"mreza"`
	Javni     string `json:"javni"`
	N         int    `json:"n"`
	Sol       string `json:"sol"`
	Nonce     string `json:"nonce"`
	Sifrirano string `json:"sifrirano"`
}

// vezano je ono što šifra veže uz ključ: oznaka, mreža, javni ključ i
// cijena izvoda ne smiju se promijeniti
func (k kopijaKljuca) vezano() []byte {
	return []byte(strings.Join([]string{k.V, k.Mreza, k.Javni, strconv.Itoa(k.N)}, "\n"))
}

// sifraKopije je AES-256-GCM s ključem izvedenim iz lozinke
func sifraKopije(lozinka string, sol []byte, n int) (cipher.AEAD, error) {
	k, err := scrypt.Key([]byte(lozinka), sol, n, 8, 1, 32)
	if err != nil {
		return nil, err
	}
	blok, _ := aes.NewCipher(k) // ključ od 32 bajta je uvijek ispravan
	return cipher.NewGCM(blok)
}

// KopijaKljucaMreze sastavlja šifriranu kopiju ključa mreže i pamti kad je
// napravljena. Samo računalo koje drži ključ mreže. Vraća sadržaj i ime
// datoteke.
func (s *Service) KopijaKljucaMreze(ctx context.Context, lozinka string) ([]byte, string, error) {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	if network == nil || !network.CanSign() {
		return nil, "", errors.New("kopiju ključa mreže sprema samo računalo koje drži ključ mreže")
	}
	if utf8.RuneCountInString(lozinka) < NajkracaLozinkaKopije {
		return nil, "", fmt.Errorf("lozinka kopije mora imati barem %d znakova", NajkracaLozinkaKopije)
	}
	k := kopijaKljuca{V: oznakaKopijeKljuca, Mreza: network.Name, Javni: razmjena.PublicKeyString(network.Public), N: scryptN}
	sol, nonce := make([]byte, 16), make([]byte, 12)
	_, _ = rand.Read(sol)   // crypto/rand ne vraća grešku
	_, _ = rand.Read(nonce) // crypto/rand ne vraća grešku
	aead, err := sifraKopije(lozinka, sol, k.N)
	if err != nil {
		return nil, "", err
	}
	k.Sol, k.Nonce = base64.StdEncoding.EncodeToString(sol), base64.StdEncoding.EncodeToString(nonce)
	k.Sifrirano = base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, network.Private().Seed(), k.vezano()))
	raw, _ := json.MarshalIndent(k, "", "  ") // polja su tekst i broj
	if _, err := s.db.ExecContext(ctx, `UPDATE network SET kopija_kljuca = ? WHERE id = 1`, time.Now().UTC()); err != nil {
		return nil, "", fmt.Errorf("kopija nije zapamćena: %w", err)
	}
	return raw, "gocop-kljuc-mreze-" + imeDatoteke(network.Name) + ".json", nil
}

// bezKvacica pretvara hrvatska slova u imenu datoteke u obična
var bezKvacica = strings.NewReplacer("č", "c", "ć", "c", "ž", "z", "š", "s", "đ", "dj")

// imeDatoteke je naziv mreže za ime datoteke: mala slova i brojke, a
// između riječi crtica
func imeDatoteke(naziv string) string {
	return strings.Join(strings.FieldsFunc(bezKvacica.Replace(strings.ToLower(naziv)), nijeZnakImena), "-")
}

// nijeZnakImena: znak koji ne ide u ime datoteke
func nijeZnakImena(r rune) bool { return (r < 'a' || r > 'z') && (r < '0' || r > '9') }

// KopijaKljucaKad je kad je ovo računalo zadnji put spremilo kopiju ključa
// mreže; nil kad nije nikad
func (s *Service) KopijaKljucaKad(ctx context.Context) *time.Time {
	var kad *time.Time
	if err := s.db.QueryRowContext(ctx, `SELECT kopija_kljuca FROM network WHERE id = 1`).Scan(&kad); err != nil {
		return nil
	}
	return kad
}

// UcitajKopijuKljuca učita kopiju ključa mreže na računalo iste mreže koje
// ključa nema; nakon toga i ono drži ključ mreže i prima članove. Kopija
// tada postoji, pa se pamti i datum; vlastitu ovlast za primanje, ako je
// ima, računalo odmah obnovi, da je druga računala ne vide kao isteklu.
func (s *Service) UcitajKopijuKljuca(ctx context.Context, raw []byte, lozinka string) error {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	switch {
	case network == nil:
		return errors.New("ovo računalo nije ni u jednoj mreži; najprije ga primite u mrežu")
	case network.CanSign():
		return errors.New("ovo računalo već drži ključ mreže")
	}
	priv, err := otvoriKopiju(raw, lozinka, network.Public)
	if err != nil {
		return err
	}
	if err := razmjena.SaveKey(s.networkKeyPath(), priv); err != nil {
		return fmt.Errorf("ključ mreže se ne može zapisati: %w", err)
	}
	kljuc := razmjena.LoadNetworkKey(network.Name, priv)
	s.mu.Lock()
	s.network = &kljuc
	s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx, `UPDATE network SET kopija_kljuca = ? WHERE id = 1`, time.Now().UTC()); err != nil {
		log.Printf("razmjena: datum učitane kopije ključa nije zapamćen: %v", err)
	}
	s.obnoviVlastituOvlast(ctx, true)
	return nil
}

// otvoriKopiju čita kopiju i vraća ključ mreže; kopija mora biti ključ
// mreže s javnim ključem mreza
func otvoriKopiju(raw []byte, lozinka string, mreza ed25519.PublicKey) (ed25519.PrivateKey, error) {
	k, err := procitajKopiju(raw, mreza)
	if err != nil {
		return nil, err
	}
	sol, nonce, sifrirano, err := k.dijelovi()
	if err != nil {
		return nil, ErrLozinkaKopije
	}
	aead, err := sifraKopije(lozinka, sol, k.N)
	if err != nil {
		return nil, ErrLozinkaKopije
	}
	sjeme, err := aead.Open(nil, nonce, sifrirano, k.vezano())
	if err != nil || len(sjeme) != ed25519.SeedSize {
		return nil, ErrLozinkaKopije
	}
	priv := ed25519.NewKeyFromSeed(sjeme)
	if !priv.Public().(ed25519.PublicKey).Equal(mreza) {
		return nil, ErrLozinkaKopije
	}
	return priv, nil
}

// procitajKopiju čita datoteku kopije ključa mreže mreza
func procitajKopiju(raw []byte, mreza ed25519.PublicKey) (kopijaKljuca, error) {
	var k kopijaKljuca
	if len(raw) > najvecaKopija || json.Unmarshal(raw, &k) != nil || k.V != oznakaKopijeKljuca {
		return k, errors.New("to nije kopija ključa goCOP mreže")
	}
	if k.Javni != razmjena.PublicKeyString(mreza) {
		return k, fmt.Errorf("kopija je ključ druge mreže (%q)", k.Mreza)
	}
	return k, nil
}

// dijelovi su sol, nonce i šifrirano sjeme iz kopije; greška kad se ne daju
// pročitati ili je cijena izvoda izvan granica
func (k kopijaKljuca) dijelovi() (sol, nonce, sifrirano []byte, err error) {
	sol, e1 := base64.StdEncoding.DecodeString(k.Sol)
	nonce, e2 := base64.StdEncoding.DecodeString(k.Nonce)
	sifrirano, e3 := base64.StdEncoding.DecodeString(k.Sifrirano)
	if err = errors.Join(e1, e2, e3); err == nil && (len(nonce) != 12 || k.N < 2 || k.N > najveciScryptN) {
		err = ErrLozinkaKopije
	}
	return sol, nonce, sifrirano, err
}

// upozorenjaKljucaMreze su rečenice za pločicu računala s ključem mreže:
// kopija ključa još nije spremljena, a ovlast za primanje imaju manje od
// dva računala. Bez toga gubitak ovog računala raspada mrežu.
func (s *Service) upozorenjaKljucaMreze(ctx context.Context) []string {
	n := s.NetworkInfo()
	if n == nil || !n.DrziKljuc {
		return nil
	}
	var out []string
	if s.KopijaKljucaKad(ctx) == nil {
		out = append(out, "Kopija ključa mreže još nije spremljena. Ako ovo računalo propadne, nitko ne može obnoviti ovlasti za primanje i mreža se raspadne. Spremite šifriranu kopiju na hladni disk u Postavkama, pod Mreža.")
	}
	if b := s.brojPrimatelja(ctx); b < 2 {
		out = append(out, fmt.Sprintf("Ovlast za primanje ima %d %s. Dajte je još jednom računalu na drugom mjestu, da mreža radi i kad ovo računalo nije dostupno.", b, models.UzBrojHR(b, "računalo", "računala", "računala")))
	}
	return out
}

// brojPrimatelja je broj drugih računala koja stvarno primaju: članstvo
// vrijedi i nije u pričuvi, a ovlast za primanje glasi na njihov ključ i
// vrijedi
func (s *Service) brojPrimatelja(ctx context.Context) int {
	clanovi, err := s.ListMembers(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, m := range clanovi {
		if !m.IsSelf && m.prima() {
			n++
		}
	}
	return n
}

// prima javlja prima li član u mrežu: članstvo vrijedi i nije u pričuvi,
// a ovlast za primanje vrijedi za njegov ključ
func (m Member) prima() bool {
	return m.Valid && m.Rok.Stanje != RokPricuva && m.OvlastZaPrimanje != nil && m.OvlastZaPrimanje.Valid
}
