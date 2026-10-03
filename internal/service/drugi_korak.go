package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/mail"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"time"

	"gocop/internal/hidroview"
	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/hkdf"
)

// Drugi korak prijave izvana. Kad je uključen (postavka "prijava_izvana"),
// prijava koja dolazi izvana — kroz posrednika (tunel) ili s javne adrese —
// nakon točne lozinke traži još PIN poslan na službenu e-poštu, rezervni kod
// ili privremeni kod od administratora. Prijava iz lokalne mreže PIN nikad
// ne traži. Zapamćeno računalo preskače PIN, nikad lozinku. Službena je
// adresa u dopuštenoj domeni ili adresa izvan nje koju je potvrdio globalni
// administrator (models.User.PotvrdaAdreseVrijedi).
//
// Sve stoji samo na ovom čvoru (prijave na čekanju, zapamćena računala,
// kodovi) i ne ide u knjigu; dijeli se samo sklopka u postavkama. Tokeni i
// kodovi spremaju se samo kao HMAC ključem HKDF(sjeme čvora, "goCOP drugi
// korak prijave"). PIN se šalje računom sustava "posta-pin" (racuni_sustava,
// lozinka zaključana kao za mletvu) i nikad se ne piše u zapisnik.
//
// Ograničenja drži servis sam, u memoriji (ponovno pokretanje ih briše, a
// broj pokušaja po prijavi na čekanju ostaje u bazi):
//   - krivi PIN ili kod: 5 po prijavi na čekanju (tada se ona briše) i 10 na
//     sat po osobi, nakon čega se unos osobi zaključa na sat;
//   - slanje PIN-a: 3 u 15 minuta po osobi, ukupno 60 na sat s ovog čvora,
//     a novi PIN za istu prijavu najranije minutu nakon prethodnog.
//
// Rukovatelj (internal/web) uz ovo broji i svoje ključeve ograničenja
// prijave (ip:, user+ip:) po greškama ErrKrivKod i ErrPrevisePokusaja.

// Postavka i zadane vrijednosti
const (
	// PostavkaPrijavaIzvana je ključ dijeljene postavke (JSON OpcijePrijaveIzvana)
	PostavkaPrijavaIzvana = "prijava_izvana"
	// ZadanaDomenaPIN je jedina domena na koju ide PIN dok nije zadana druga
	ZadanaDomenaPIN = "voda.hr"
	// SustavPostaPIN je ključ računa sustava koji šalje PIN (racuni_sustava)
	SustavPostaPIN = "posta-pin"
)

// Rokovi i granice drugog koraka
const (
	TrajanjePrijaveNaCekanju = 10 * time.Minute    // rok PIN-a i kolačića prijave na čekanju
	TrajanjeRacunala         = 30 * 24 * time.Hour // rok zapamćenog računala (ne produljuje se)
	TrajanjePrivremenogKoda  = 24 * time.Hour      // rok privremenog koda od administratora
	NajviseKrivihUnosa       = 5                   // po prijavi na čekanju
	RazmakPonovnogSlanja     = time.Minute         // najmanji razmak dvaju PIN-ova iste prijave
	IstekSlanja              = 15 * time.Second    // najdulje slanje PIN-a unutar zahtjeva prijave
	BrojRezervnihKodova      = 10
	// PamtiPrijavuIzvana: toliko nakon zadnje prijave izvana čvor još diže
	// uzbunu kad PIN nema čime slati; čvor koji prijave izvana ne prima
	// (npr. samo u lokalnoj mreži) pošiljatelja ne treba
	PamtiPrijavuIzvana = 7 * 24 * time.Hour

	krivihNaSat      = 10
	zakljucavanjePIN = time.Hour
	slanjaPoOsobi    = 3
	prozorSlanja     = 15 * time.Minute
	slanjaNaSat      = 60
	stankaVeze       = 5 * time.Minute
)

// Vrste kojima je drugi korak prošao (IshodDrugogKoraka.Vrsta)
const (
	VrstaPIN        = "pin"
	VrstaRezervni   = repository.KodRezervni
	VrstaPrivremeni = repository.KodPrivremeni
)

// Greške drugog koraka; poruke su za korisnika
var (
	ErrDrugiKorakIstekao     = errors.New("prijava je istekla; prijavite se ponovno")
	ErrKrivKod               = errors.New("kod nije točan")
	ErrPrevisePokusaja       = errors.New("previše krivih kodova; prijavite se ponovno")
	ErrKodoviZakljucani      = errors.New("previše krivih kodova u zadnjih sat vremena; pokušajte ponovno za sat")
	ErrOblikKoda             = errors.New("upišite PIN od 6 znamenki ili kod oblika R3-XXXX-XXXX ili P-XXXX-XXXX")
	ErrPonovnoPrerano        = errors.New("novi PIN može se tražiti najranije minutu nakon prethodnog")
	ErrSlanjeOgraniceno      = errors.New("poslano je previše PIN-ova; pričekajte nekoliko minuta ili upišite rezervni kod")
	ErrNemaPosiljatelja      = errors.New("na ovom čvoru nije upisan račun za slanje PIN-a")
	ErrPosiljateljNeispravan = errors.New("poslužitelj e-pošte odbio je lozinku računa za slanje PIN-a; administrator je mora upisati ponovno")
	ErrSlanjeZastalo         = errors.New("slanje PIN-a privremeno ne radi (veza s poslužiteljem e-pošte)")
	ErrPINNijePoslan         = errors.New("PIN nije poslan")
	ErrNemaAdrese            = errors.New("u profilu nema adrese e-pošte")
	ErrNeispravnaAdresa      = errors.New("adresa e-pošte nije ispravna")
	ErrAdresaNijeDopustena   = errors.New("PIN se šalje samo na službenu adresu e-pošte ili na adresu koju je potvrdio administrator")
	ErrZajednickaAdresa      = errors.New("istu adresu e-pošte ima još netko, pa se PIN na nju ne šalje")
	ErrTudjimOcima           = errors.New("dok gledate tuđim očima, ovo nije dopušteno; vratite se svojim očima")
	ErrProbaPotrebna         = errors.New("PIN izvana može se uključiti tek nakon uspješnog probnog PIN-a na ovom čvoru")
	ErrNemaKljucaPrijave     = errors.New("ključ čvora za drugi korak prijave nije postavljen")
	ErrPromjenaAdreseIzvana  = errors.New("adresu e-pošte ne možete mijenjati izvana: promijenite je iz ureda (lokalna mreža) ili zamolite administratora")
	ErrAdresaPrijeLozinke    = errors.New("adresu e-pošte možete promijeniti tek nakon što postavite novu lozinku")
)

// Greške uključivanja PIN-a i promjene adrese; poruke su za korisnika
var (
	ErrUkljuciIzvana            = errors.New("PIN se uključuje s čvora kroz koji se ljudi prijavljuju izvana (npr. https://cop-osijek.com), nakon probnog PIN-a na tom čvoru")
	ErrVlastitaAdresaIzAdresara = errors.New("svoju adresu e-pošte ne mijenjate iz adresara: promijenite je na svom profilu (iz ureda, uz trenutnu lozinku)")
	ErrAdresaZauzeta            = errors.New("tu adresu e-pošte već ima drugi djelatnik; javite administratoru")
)

// AdresaIzvanDomene je adresa izvan dopuštene domene (DopustenaAdresa);
// errors.Is je prepoznaje kao ErrAdresaNijeDopustena. Vlastita: osoba je
// sama upisuje na profilu, gdje potvrda administratora ne pomaže.
type AdresaIzvanDomene struct {
	Domena   string
	Vlastita bool
}

func (e AdresaIzvanDomene) Error() string {
	if e.Vlastita {
		return "svoju adresu e-pošte mijenjate samo na adresu @" + e.Domena +
			"; adresu izvan te domene (npr. u tvrtki izvođača) upisuje i potvrđuje administrator"
	}
	return "PIN se šalje samo na službenu adresu e-pošte (@" + e.Domena + ") ili na adresu koju je potvrdio administrator"
}

// Is čini AdresaIzvanDomene jednakom ErrAdresaNijeDopustena za errors.Is
func (e AdresaIzvanDomene) Is(target error) bool { return target == ErrAdresaNijeDopustena }

// KrivKod je krivi PIN ili kod uz broj preostalih pokušaja; errors.Is ga
// prepoznaje kao ErrKrivKod
type KrivKod struct{ Preostalo int }

func (e KrivKod) Error() string {
	return fmt.Sprintf("kod nije točan; preostalo pokušaja: %d", e.Preostalo)
}

// Is čini KrivKod jednakim ErrKrivKod za errors.Is
func (e KrivKod) Is(target error) bool { return target == ErrKrivKod }

// OpcijePrijaveIzvana je dijeljena postavka drugog koraka.
type OpcijePrijaveIzvana struct {
	PIN    bool   `json:"pin"`    // traži li se PIN za prijavu izvana
	Domena string `json:"domena"` // domena na koju ide PIN bez potvrde administratora (i jedina na koju osoba sama mijenja adresu)
}

// Postar šalje e-poštu; u programu je to paket posta (PravaPosta), u
// testovima zamjena.
type Postar interface {
	Prijavi(ctx context.Context, p posta.Postavke, r posta.Racun) (string, error)
	Posalji(ctx context.Context, p posta.Postavke, r posta.Racun, poruke []posta.Poruka) ([]error, error)
	Imenik(ctx context.Context, p posta.Postavke, r posta.Racun, upit string) ([]posta.Kontakt, error)
}

// PravaPosta šalje paketom posta (Exchange ili SMTP).
type PravaPosta struct{}

// Prijavi se prijavi bez slanja (posta.Prijavi)
func (PravaPosta) Prijavi(ctx context.Context, p posta.Postavke, r posta.Racun) (string, error) {
	return posta.Prijavi(ctx, p, r)
}

// Posalji predaje poruke poslužitelju (posta.Posalji)
func (PravaPosta) Posalji(ctx context.Context, p posta.Postavke, r posta.Racun, poruke []posta.Poruka) ([]error, error) {
	return posta.Posalji(ctx, p, r, poruke)
}

// Imenik traži u adresaru tvrtke (posta.Imenik)
func (PravaPosta) Imenik(ctx context.Context, p posta.Postavke, r posta.Racun, upit string) ([]posta.Kontakt, error) {
	return posta.Imenik(ctx, p, r, upit)
}

type korisniciDrugogKoraka interface {
	GetUserByID(id uuid.UUID) (*models.User, error)
	ListAreas(sectorID string) ([]models.Area, error) // doseg za privremeni kod (smijePonistiti)
}

type postavkeDrugogKoraka interface {
	GetPostavka(ctx context.Context, id string) (string, error)
	SavePostavka(ctx context.Context, id, vrijednost string) error
}

// DrugiKorak je servis drugog koraka prijave izvana.
type DrugiKorak struct {
	repo      *repository.DrugiKorakRepository
	racuni    *repository.RacuniSustavaRepository
	korisnici korisniciDrugogKoraka
	postavke  postavkeDrugogKoraka
	posta     func(ctx context.Context) posta.Postavke
	postar    Postar
	sad       func() time.Time

	kljuc       []byte // HMAC ključ, HKDF iz sjemena čvora
	kljucRacuna []byte // ključ lozinke pošiljatelja, kao za mletvu (hidroview.Kljuc)

	// slanje je red za slanje (mjesto za jedno): slanja idu jedno po jedno,
	// pa se kriva lozinka pokuša najviše jednom; čeka se najviše istekSlanja
	slanje      chan struct{}
	istekSlanja time.Duration // 0 = IstekSlanja (SetIstekSlanja, za testove)

	mu           sync.Mutex
	dogadaji     map[string][]time.Time // brojila ograničenja po ključu
	blokDo       map[string]time.Time   // zaključan unos koda po osobi
	uTijeku      map[string]int         // zauzeti unosi koda po osobi, još neusporedeni
	stankaDo     time.Time              // slanje stoji zbog greške veze
	probaKad     time.Time              // zadnji uspješan probni PIN s ovim pošiljateljem
	upozorenoKad time.Time              // zadnje upozorenje da je PIN uključen bez pošiljatelja
	izvanaKad    time.Time              // zadnja prijava izvana na ovom čvoru (od pokretanja)
}

// NewDrugiKorak sastavlja servis. korisnici je repozitorij korisnika,
// postavke spremište dijeljenih postavki (AktiRepository). Ključ se daje
// sa SetKljuc, a postavke poslužitelja e-pošte sa SetPosta.
func NewDrugiKorak(repo *repository.DrugiKorakRepository, racuni *repository.RacuniSustavaRepository,
	korisnici korisniciDrugogKoraka, postavke postavkeDrugogKoraka) *DrugiKorak {
	return &DrugiKorak{repo: repo, racuni: racuni, korisnici: korisnici, postavke: postavke,
		postar: PravaPosta{}, sad: time.Now, slanje: make(chan struct{}, 1),
		dogadaji: map[string][]time.Time{}, blokDo: map[string]time.Time{}, uTijeku: map[string]int{}}
}

// SetKljuc izvodi ključeve iz sjemena ključa čvora (ed25519 Seed): HMAC
// ključ za tokene i kodove i ključ kojim je zaključana lozinka pošiljatelja.
func (d *DrugiKorak) SetKljuc(sjeme []byte) {
	if len(sjeme) == 0 {
		d.kljuc, d.kljucRacuna = nil, nil
		return
	}
	k := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, sjeme, nil, []byte("goCOP drugi korak prijave")), k); err != nil {
		panic(err) // HKDF sa SHA-256 daje do 8160 bajtova
	}
	d.kljuc, d.kljucRacuna = k, hidroview.Kljuc(sjeme)
}

// SetPosta daje izvor važećih postavki poslužitelja e-pošte (AktService.Posta)
func (d *DrugiKorak) SetPosta(f func(ctx context.Context) posta.Postavke) { d.posta = f }

// SetPostar zamjenjuje slanje e-pošte (za testove)
func (d *DrugiKorak) SetPostar(p Postar) { d.postar = p }

// SetSat zamjenjuje sat (za testove)
func (d *DrugiKorak) SetSat(f func() time.Time) { d.sad = f }

// SetIstekSlanja zamjenjuje IstekSlanja: najdulje čekanje reda za slanje i
// najdulje slanje (za testove)
func (d *DrugiKorak) SetIstekSlanja(t time.Duration) { d.istekSlanja = t }

func (d *DrugiKorak) istek() time.Duration {
	if d.istekSlanja > 0 {
		return d.istekSlanja
	}
	return IstekSlanja
}

func (d *DrugiKorak) spreman() error {
	if d == nil || len(d.kljuc) != 32 {
		return ErrNemaKljucaPrijave
	}
	return nil
}

func (d *DrugiKorak) mac(dijelovi ...string) []byte {
	m := hmac.New(sha256.New, d.kljuc)
	for _, x := range dijelovi {
		m.Write([]byte(x))
		m.Write([]byte{0})
	}
	return m.Sum(nil)
}

func (d *DrugiKorak) idPrijave(token string) []byte { return d.mac("prijava", token) }
func (d *DrugiKorak) sazetakRacunala(token string) []byte {
	return d.mac("racunalo", token)
}
func (d *DrugiKorak) otisakLozinke(hash string) []byte { return d.mac("lozinka", hash)[:16] }
func (d *DrugiKorak) sazetakPINa(id []byte, pin string) []byte {
	return d.mac("pin", hex.EncodeToString(id), pin)
}
func (d *DrugiKorak) sazetakKoda(userID uuid.UUID, oznaka, kod string) []byte {
	return d.mac("kod", userID.String(), strings.ToUpper(oznaka), kod)
}

// noviToken je 32 nasumična bajta za kolačić
func noviToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ispravanToken javlja ima li vrijednost kolačića oblik tokena
func ispravanToken(t string) bool {
	b, err := base64.RawURLEncoding.DecodeString(t)
	return err == nil && len(b) == 32
}

// ---- odluka i postavka ----

// IzvanaAdresa javlja dolazi li zahtjev izvana: kroz posrednika (bilo
// kakvo zaglavlje posrednika, pouzdano ili ne) ili s adrese koja nije
// privatna, ovo računalo ni lokalna veza. Lažno zaglavlje tako samo
// nametne PIN, nikad ga ne preskoči; neispravna adresa je izvana.
func IzvanaAdresa(krozPosrednika bool, adresa netip.Addr) bool {
	if krozPosrednika || !adresa.IsValid() {
		return true
	}
	a := adresa.Unmap()
	return !(a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast())
}

// Opcije vraća dijeljenu postavku; bez nje (ili neispravne) PIN je
// isključen, a domena zadana.
func (d *DrugiKorak) Opcije(ctx context.Context) OpcijePrijaveIzvana {
	o := OpcijePrijaveIzvana{Domena: ZadanaDomenaPIN}
	if d == nil || d.postavke == nil {
		return o
	}
	v, err := d.postavke.GetPostavka(ctx, PostavkaPrijavaIzvana)
	if err != nil || v == "" {
		return o
	}
	var x OpcijePrijaveIzvana
	if json.Unmarshal([]byte(v), &x) != nil {
		return o
	}
	if dom := normalizirajDomenu(x.Domena); dom != "" {
		o.Domena = dom
	}
	o.PIN = x.PIN
	return o
}

// Ukljuceno javlja traži li se PIN za prijavu izvana
func (d *DrugiKorak) Ukljuceno(ctx context.Context) bool { return d.Opcije(ctx).PIN }

// TrebaDrugiKorak javlja traži li prijava s ove strane drugi korak:
// samo izvana (IzvanaAdresa) i samo kad je PIN uključen. Zapamćeno
// računalo rukovatelj provjerava posebno (ProvjeriRacunalo). Zove se za
// točnu lozinku, pa usput bilježi da ovaj čvor prima prijave izvana
// (uzbuna bez pošiljatelja).
func (d *DrugiKorak) TrebaDrugiKorak(ctx context.Context, izvana bool) bool {
	if d == nil || !izvana {
		return false
	}
	d.zabiljeziIzvana()
	return d.Ukljuceno(ctx)
}

// zabiljeziIzvana pamti da je ovaj čvor upravo primio prijavu izvana
func (d *DrugiKorak) zabiljeziIzvana() {
	sad := d.sad()
	d.mu.Lock()
	d.izvanaKad = sad
	d.mu.Unlock()
}

// primaIzvana javlja je li ovaj čvor primio prijavu izvana u zadnjih
// PamtiPrijavuIzvana. Pamti se samo u memoriji: nakon ponovnog pokretanja
// uzbuna čeka prvu prijavu izvana (koja i sama upozori u zapisniku).
func (d *DrugiKorak) primaIzvana() bool {
	sad := d.sad()
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.izvanaKad.IsZero() && sad.Sub(d.izvanaKad) < PamtiPrijavuIzvana
}

func normalizirajDomenu(s string) string {
	s = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "@")))
	if s == "" || strings.ContainsAny(s, "@ \t/\\") || !strings.Contains(s, ".") {
		return ""
	}
	return s
}

// SpremiOpcije sprema dijeljenu postavku; smije samo globalni
// administrator. Uključiti PIN (iz isključenog) može se samo zahtjevom
// koji je došao izvana (izvana: IzvanaAdresa), dakle na čvoru koji prima
// prijave izvana, i tek nakon uspješnog probnog PIN-a na tom čvoru
// (PosaljiProbniPIN), s ispravnim pošiljateljem; isključiti uvijek.
// Postavka vrijedi na svim čvorovima, a pošiljatelj i proba samo na ovom:
// uključen s čvora u lokalnoj mreži, PIN bi na javnom čvoru bez pošiljatelja
// pustio izvana samo kodove.
func (d *DrugiKorak) SpremiOpcije(ctx context.Context, actor *models.UserPermissions, o OpcijePrijaveIzvana, izvana bool) error {
	if actor == nil || !actor.IsGlobalAdmin {
		return ErrUnauthorized
	}
	if strings.TrimSpace(o.Domena) == "" {
		o.Domena = ZadanaDomenaPIN
	}
	dom := normalizirajDomenu(o.Domena)
	if dom == "" {
		return fmt.Errorf("domena %q nije ispravna (npr. voda.hr)", o.Domena)
	}
	o.Domena = dom
	if o.PIN && !d.Ukljuceno(ctx) {
		if !izvana {
			return ErrUkljuciIzvana
		}
		if err := d.mozeUkljuciti(ctx); err != nil {
			return err
		}
	}
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	if err := d.postavke.SavePostavka(ctx, PostavkaPrijavaIzvana, string(b)); err != nil {
		return err
	}
	log.Printf("prijava izvana: %s je PIN %s (domena %s)", actor.User.Username, map[bool]string{true: "uključio", false: "isključio"}[o.PIN], o.Domena)
	return nil
}

// PostaviUkljuceno uključuje ili isključuje PIN za prijavu izvana, uz
// postojeću domenu; pravila kao SpremiOpcije.
func (d *DrugiKorak) PostaviUkljuceno(ctx context.Context, actor *models.UserPermissions, ukljuci, izvana bool) error {
	o := d.Opcije(ctx)
	o.PIN = ukljuci
	return d.SpremiOpcije(ctx, actor, o, izvana)
}

func (d *DrugiKorak) mozeUkljuciti(ctx context.Context) error {
	r, err := d.racuni.Racun(ctx, SustavPostaPIN)
	if err != nil {
		return err
	}
	if r == nil {
		return ErrNemaPosiljatelja
	}
	if r.NeispravanOd != nil {
		return ErrPosiljateljNeispravan
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.probaKad.IsZero() {
		return ErrProbaPotrebna
	}
	return nil
}

// DopustenaAdresa javlja smije li PIN ići na adresu sama po sebi: ispravna
// adresa (bez imena) u dopuštenoj domeni; inače AdresaIzvanDomene. Adresu
// koju je potvrdio administrator i zajedničku adresu provjerava tek
// adresaZaPIN, jer ovise o osobi.
func (d *DrugiKorak) DopustenaAdresa(ctx context.Context, adresa string) error {
	adresa = strings.TrimSpace(adresa)
	if adresa == "" {
		return ErrNemaAdrese
	}
	a, err := mail.ParseAddress(adresa)
	if err != nil || a.Name != "" || !strings.EqualFold(a.Address, adresa) {
		return fmt.Errorf("%w: %q", ErrNeispravnaAdresa, adresa)
	}
	dom := d.Opcije(ctx).Domena
	i := strings.LastIndex(a.Address, "@")
	if i < 0 || !strings.EqualFold(a.Address[i+1:], dom) {
		return AdresaIzvanDomene{Domena: dom}
	}
	return nil
}

// dopustenaOsobi je DopustenaAdresa za adresu osobe: adresa izvan dopuštene
// domene prolazi kad ju je potvrdio globalni administrator, a potvrda još
// vrijedi (jednaka je adresi računa)
func (d *DrugiKorak) dopustenaOsobi(ctx context.Context, u *models.User) error {
	err := d.DopustenaAdresa(ctx, u.Email)
	if errors.Is(err, ErrAdresaNijeDopustena) && u.PotvrdaAdreseVrijedi() {
		return nil
	}
	return err
}

// adresaZaPIN vraća adresu osobe na koju smije ići PIN: u dopuštenoj domeni
// ili potvrđenu izvan nje, a nikad adresu koju ima još jedan aktivni račun
// (ni potvrđenu)
func (d *DrugiKorak) adresaZaPIN(ctx context.Context, u *models.User) (string, error) {
	adresa := strings.TrimSpace(u.Email)
	if err := d.dopustenaOsobi(ctx, u); err != nil {
		return "", err
	}
	n, err := d.repo.AktivnihSAdresom(ctx, adresa, u.ID)
	if err != nil {
		return "", err
	}
	if n > 0 {
		return "", ErrZajednickaAdresa
	}
	return adresa, nil
}

// MaskirajAdresu skriva adresu za prikaz: tomislav@voda.hr → t***@voda.hr
func MaskirajAdresu(a string) string {
	a = strings.TrimSpace(a)
	i := strings.LastIndex(a, "@")
	if i <= 0 {
		return ""
	}
	r := []rune(a[:i])
	return string(r[0]) + "***" + a[i:]
}

// ---- ograničenja ----

// broj vraća događaje ključa unutar prozora (i usput čisti starije)
func (d *DrugiKorak) broj(kljuc string, sad time.Time, prozor time.Duration) int {
	ostali := d.dogadaji[kljuc][:0]
	for _, t := range d.dogadaji[kljuc] {
		if sad.Sub(t) < prozor {
			ostali = append(ostali, t)
		}
	}
	if len(ostali) == 0 {
		delete(d.dogadaji, kljuc)
		return 0
	}
	d.dogadaji[kljuc] = ostali
	return len(ostali)
}

func (d *DrugiKorak) zakljucano(userID uuid.UUID, sad time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return sad.Before(d.blokDo[userID.String()])
}

// rezervirajUnos zauzme unos koda osobe prije usporedbe: krivi u zadnjem
// satu i unosi koji su u tijeku zajedno ne prelaze krivihNaSat, pa ih ni
// istodobni unosi ne prođu; false kad je unos osobi zaključan ili je
// granica puna. Zauzeti unos završava krivUnos ili vratiUnos.
func (d *DrugiKorak) rezervirajUnos(userID uuid.UUID, sad time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	u := userID.String()
	if sad.Before(d.blokDo[u]) {
		return false
	}
	if d.broj("pin:"+u, sad, time.Hour)+d.uTijeku[u] >= krivihNaSat {
		return false
	}
	d.uTijeku[u]++
	return true
}

func (d *DrugiKorak) zavrsiUnos(u string) {
	if d.uTijeku[u]--; d.uTijeku[u] <= 0 {
		delete(d.uTijeku, u)
	}
}

// vratiUnos otpusti zauzeti unos koji se ne broji (točan kod, ili pokušaj
// prijave na čekanju nije ni zauzet)
func (d *DrugiKorak) vratiUnos(userID uuid.UUID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.zavrsiUnos(userID.String())
}

// krivUnos broji zauzeti unos kao krivi; na krivihNaSat krivih u satu
// zaključa unos osobi na sat
func (d *DrugiKorak) krivUnos(userID uuid.UUID, sad time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	u := userID.String()
	d.zavrsiUnos(u)
	k := "pin:" + u
	d.dogadaji[k] = append(d.dogadaji[k], sad)
	if d.broj(k, sad, time.Hour) >= krivihNaSat {
		d.blokDo[u] = sad.Add(zakljucavanjePIN)
		delete(d.dogadaji, k)
	}
}

// smijeSlati provjeri i zabilježi slanje: po osobi i ukupno s čvora
func (d *DrugiKorak) smijeSlati(kljucOsobe string, sad time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if sad.Before(d.stankaDo) {
		return ErrSlanjeZastalo
	}
	if d.broj("pinmail:*", sad, time.Hour) >= slanjaNaSat {
		return ErrSlanjeOgraniceno
	}
	if kljucOsobe != "" && d.broj("pinmail:"+kljucOsobe, sad, prozorSlanja) >= slanjaPoOsobi {
		return ErrSlanjeOgraniceno
	}
	d.dogadaji["pinmail:*"] = append(d.dogadaji["pinmail:*"], sad)
	if kljucOsobe != "" {
		d.dogadaji["pinmail:"+kljucOsobe] = append(d.dogadaji["pinmail:"+kljucOsobe], sad)
	}
	return nil
}

// ---- slanje ----

// Posiljatelj je račun sustava kojim ovaj čvor šalje PIN.
type Posiljatelj struct {
	Korisnik     string     // ime za prijavu na poslužitelj (može biti DOMENA\ime)
	Adresa       string     // adresa s koje se šalje
	Spremljen    time.Time  // kad je lozinka upisana
	NeispravanOd *time.Time // poslužitelj je odbio lozinku; do novog upisa se ne šalje
}

// StanjeSlanja je stanje pošiljatelja za stranicu administracije.
type StanjeSlanja struct {
	Opcije       OpcijePrijaveIzvana
	Posiljatelj  *Posiljatelj // nil = nije upisan na ovom čvoru
	StankaDo     time.Time    // slanje stoji zbog greške veze do tog trenutka; nula = ne stoji
	ProbaUspjela bool         // probni PIN s ovim pošiljateljem je prošao (od pokretanja programa)
	ProbaKad     time.Time
	// BezPosiljatelja: PIN izvana je uključen, ovaj čvor prima prijave
	// izvana, a nema ispravnog pošiljatelja (DrugiKorak.BezPosiljatelja),
	// pa prijava izvana kroz njega prolazi samo kodovima
	BezPosiljatelja bool
	// PrimaIzvana: ovaj čvor je primio prijavu izvana u zadnjih
	// PamtiPrijavuIzvana (od pokretanja programa)
	PrimaIzvana bool
}

// Stanje vraća stanje drugog koraka za administraciju
func (d *DrugiKorak) Stanje(ctx context.Context) (*StanjeSlanja, error) {
	s := &StanjeSlanja{Opcije: d.Opcije(ctx)}
	r, err := d.racuni.Racun(ctx, SustavPostaPIN)
	if err != nil {
		return nil, err
	}
	if r != nil {
		s.Posiljatelj = &Posiljatelj{Korisnik: r.Korisnik, Adresa: r.Adresa, Spremljen: r.UpdatedAt, NeispravanOd: r.NeispravanOd}
	}
	s.PrimaIzvana = d.primaIzvana()
	s.BezPosiljatelja = s.Opcije.PIN && s.PrimaIzvana && neispravanPosiljatelj(r, d.postavkePoste(ctx))
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sad().Before(d.stankaDo) {
		s.StankaDo = d.stankaDo
	}
	s.ProbaKad, s.ProbaUspjela = d.probaKad, !d.probaKad.IsZero()
	return s, nil
}

// postavkePoste su postavke poslužitelja za račun sustava: vlastiti skup
// veza, odvojen od osobnih sandučića (posta.Postavke.Klijent)
func (d *DrugiKorak) postavkePoste(ctx context.Context) posta.Postavke {
	if d.posta == nil {
		return posta.Postavke{}
	}
	p := d.posta(ctx)
	p.Klijent = SustavPostaPIN
	return p
}

// neispravanPosiljatelj: računa nema, poslužitelj ga je odbio ili pošta
// nije podešena
func neispravanPosiljatelj(r *repository.RacunSustava, p posta.Postavke) bool {
	return r == nil || r.NeispravanOd != nil || !p.Podesena()
}

// BezPosiljatelja javlja traži li se PIN izvana, a ovaj čvor, koji prima
// prijave izvana (primaIzvana), nema ispravnog pošiljatelja (nije upisan,
// poslužitelj ga je odbio ili pošta nije podešena): prijava izvana kroz
// ovaj čvor tada prolazi samo kodovima. Čvor samo u lokalnoj mreži
// pošiljatelja ne treba, pa ni uzbune nema.
func (d *DrugiKorak) BezPosiljatelja(ctx context.Context) bool {
	if d == nil || d.racuni == nil || !d.Ukljuceno(ctx) || !d.primaIzvana() {
		return false
	}
	r, err := d.racuni.Racun(ctx, SustavPostaPIN)
	if err != nil {
		return false
	}
	return neispravanPosiljatelj(r, d.postavkePoste(ctx))
}

// upozoriBezPosiljatelja zapiše u zapisnik, najviše jednom na sat, da je
// PIN uključen, a ovaj čvor ga nema čime slati
func (d *DrugiKorak) upozoriBezPosiljatelja(ctx context.Context) {
	if !d.BezPosiljatelja(ctx) {
		return
	}
	sad := d.sad()
	d.mu.Lock()
	if !d.upozorenoKad.IsZero() && sad.Sub(d.upozorenoKad) < time.Hour {
		d.mu.Unlock()
		return
	}
	d.upozorenoKad = sad
	d.mu.Unlock()
	log.Printf("prijava izvana: UPOZORENJE: PIN izvana je uključen, a ovaj čvor nema ispravnog računa za slanje PIN-a; prijava izvana kroz njega prolazi samo rezervnim i privremenim kodovima (Administracija › E-pošta)")
}

// SpremiPosiljatelja provjeri prijavu na poslužitelj e-pošte i spremi račun
// koji šalje PIN, šifrirano ključem čvora. Prazna adresa traži se u
// adresaru tvrtke po imenu za prijavu. Kad poslužitelj odbije lozinku, ne
// sprema se ništa. Nova lozinka briše oznaku neispravnog računa i stanku,
// a probni PIN treba poslati ponovno. Smije samo globalni administrator.
func (d *DrugiKorak) SpremiPosiljatelja(ctx context.Context, actor *models.UserPermissions, korisnik, lozinka, adresa string) (*Posiljatelj, error) {
	if actor == nil || !actor.IsGlobalAdmin {
		return nil, ErrUnauthorized
	}
	if err := d.spreman(); err != nil {
		return nil, err
	}
	korisnik, adresa = strings.TrimSpace(korisnik), strings.TrimSpace(adresa)
	if korisnik == "" || lozinka == "" {
		return nil, errors.New("upišite korisničko ime i lozinku računa za slanje")
	}
	p := d.postavkePoste(ctx)
	if !p.Podesena() {
		return nil, errors.New("poslužitelj e-pošte nije podešen (Administracija › E-pošta)")
	}
	// lozinka se provjerava novom vezom: već prijavljena veza (npr. osobnog
	// sandučića istog imena) primila bi i krivu
	p.SvjezaVeza = true
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	ime, err := d.postar.Prijavi(ctx, p, posta.Racun{Korisnik: korisnik, Lozinka: lozinka})
	if err != nil {
		return nil, fmt.Errorf("prijava na poslužitelj e-pošte nije prošla: %w", err)
	}
	if adresa == "" {
		upit := ime
		if i := strings.LastIndex(upit, "\\"); i >= 0 {
			upit = upit[i+1:]
		}
		if i := strings.Index(upit, "@"); i > 0 {
			upit = upit[:i]
		}
		kontakti, err := d.postar.Imenik(ctx, p, posta.Racun{Korisnik: ime, Lozinka: lozinka}, upit)
		if err != nil || len(kontakti) != 1 || kontakti[0].Email == "" {
			return nil, nakonPrijave{errors.New("adresa pošiljatelja nije pronađena u adresaru; upišite je")}
		}
		adresa = kontakti[0].Email
	}
	if a, err := mail.ParseAddress(adresa); err != nil || a.Name != "" {
		return nil, nakonPrijave{fmt.Errorf("adresa pošiljatelja %q nije ispravna", adresa)}
	}
	zakljucana, err := posta.Zakljucaj(d.kljucRacuna, lozinka)
	if err != nil {
		return nil, nakonPrijave{err}
	}
	if err := d.racuni.Spremi(ctx, &repository.RacunSustava{Sustav: SustavPostaPIN, Korisnik: ime, Lozinka: zakljucana, Adresa: adresa}); err != nil {
		return nil, nakonPrijave{err}
	}
	d.mu.Lock()
	d.stankaDo, d.probaKad = time.Time{}, time.Time{}
	d.mu.Unlock()
	log.Printf("prijava izvana: %s je upisao račun za slanje PIN-a %s <%s>", actor.User.Username, ime, adresa)
	return &Posiljatelj{Korisnik: ime, Adresa: adresa, Spremljen: d.sad()}, nil
}

// nakonPrijave je greška koraka nakon što je poslužitelj e-pošte primio
// lozinku (adresa, spremanje): lozinka nije bila kriva, pa se prijava ne
// broji u pokušaje računa u domeni
type nakonPrijave struct{ error }

func (e nakonPrijave) Unwrap() error { return e.error }

// PrijavaProsla javlja je li poslužitelj e-pošte primio lozinku prije
// greške err
func PrijavaProsla(err error) bool {
	var n nakonPrijave
	return errors.As(err, &n)
}

// ObrisiPosiljatelja miče račun za slanje PIN-a s ovog čvora; dok je PIN
// uključen, prijava izvana tada prolazi samo kodovima.
func (d *DrugiKorak) ObrisiPosiljatelja(ctx context.Context, actor *models.UserPermissions) error {
	if actor == nil || !actor.IsGlobalAdmin {
		return ErrUnauthorized
	}
	d.mu.Lock()
	d.probaKad = time.Time{}
	d.mu.Unlock()
	return d.racuni.Obrisi(ctx, SustavPostaPIN)
}

// zauzmiSlanje čeka red za slanje najviše istek(), i samo dok pozivatelj
// čeka; kad red ne dođe, slanje je zastalo (ErrSlanjeZastalo), bez stanke
func (d *DrugiKorak) zauzmiSlanje(ctx context.Context) error {
	select {
	case d.slanje <- struct{}{}:
		return nil
	default:
	}
	t := time.NewTimer(d.istek())
	defer t.Stop()
	select {
	case d.slanje <- struct{}{}:
		return nil
	case <-t.C:
		return fmt.Errorf("%w: slanje nije došlo na red za %s", ErrSlanjeZastalo, d.istek())
	case <-ctx.Done():
		return fmt.Errorf("%w: %v", ErrSlanjeZastalo, ctx.Err())
	}
}

// posalji šalje jednu poruku računom za slanje PIN-a, bez kopije u Poslano.
// Lozinku koju je poslužitelj odbio označi i više je ne pokušava (svaki
// pokušaj je neuspjela prijava na račun domene, a nekoliko njih ga
// zaključa); greška veze zaustavi slanje na pet minuta. Slanje ne ovisi o
// zahtjevu koji ga je pokrenuo: preglednik koji odustane ne prekida
// poruku i ne zaustavlja slanje drugima. provjera (probni PIN) šalje novom,
// još neprijavljenom vezom, da proba stvarno provjeri lozinku.
func (d *DrugiKorak) posalji(ctx context.Context, kljucOsobe string, m posta.Poruka, provjera bool) error {
	if err := d.zauzmiSlanje(ctx); err != nil {
		return err
	}
	defer func() { <-d.slanje }()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), d.istek())
	defer cancel()
	r, err := d.racuni.Racun(ctx, SustavPostaPIN)
	if err != nil {
		return err
	}
	if r == nil {
		return ErrNemaPosiljatelja
	}
	if r.NeispravanOd != nil {
		return ErrPosiljateljNeispravan
	}
	p := d.postavkePoste(ctx)
	if !p.Podesena() {
		return fmt.Errorf("%w: poslužitelj e-pošte nije podešen", ErrNemaPosiljatelja)
	}
	p.SvjezaVeza = provjera
	if err := d.smijeSlati(kljucOsobe, d.sad()); err != nil {
		return err
	}
	lozinka, err := posta.Otkljucaj(d.kljucRacuna, r.Lozinka)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNemaPosiljatelja, err)
	}
	m.Od = mail.Address{Name: "goCOP", Address: r.Adresa}
	m.BezKopije = true
	greske, err := d.postar.Posalji(ctx, p, posta.Racun{Korisnik: r.Korisnik, Lozinka: lozinka}, []posta.Poruka{m})
	if errors.Is(err, posta.ErrPrijava) {
		if e := d.racuni.OznaciNeispravan(context.WithoutCancel(ctx), SustavPostaPIN, d.sad()); e != nil {
			log.Printf("prijava izvana: račun za slanje PIN-a nije označen neispravnim: %v", e)
		}
		d.mu.Lock()
		d.probaKad = time.Time{}
		d.mu.Unlock()
		log.Printf("prijava izvana: poslužitelj e-pošte odbio je lozinku računa %s; PIN se ne šalje dok administrator ne upiše novu", r.Korisnik)
		return ErrPosiljateljNeispravan
	}
	if errors.Is(err, context.Canceled) {
		// prekid nije kvar poslužitelja: slanje drugima ne stoji
		log.Printf("prijava izvana: slanje PIN-a je prekinuto (%v)", err)
		return fmt.Errorf("%w: %v", ErrSlanjeZastalo, err)
	}
	if err != nil {
		d.mu.Lock()
		d.stankaDo = d.sad().Add(stankaVeze)
		d.mu.Unlock()
		log.Printf("prijava izvana: slanje PIN-a nije uspjelo (%v); slanje stoji %s", err, stankaVeze)
		return fmt.Errorf("%w: %v", ErrSlanjeZastalo, err)
	}
	if len(greske) > 0 && greske[0] != nil {
		log.Printf("prijava izvana: poslužitelj nije primio poruku za %s: %v", MaskirajAdresu(m.Za.Address), greske[0])
		return fmt.Errorf("%w: %v", ErrPINNijePoslan, greske[0])
	}
	return nil
}

// razloziPIN su razlozi zbog kojih PIN nije poslan, s oznakom koja se
// sprema uz prijavu na čekanju (i stoji u adresi stranice s upisom koda)
var razloziPIN = []struct {
	oznaka string
	err    error
}{
	{"adresa", ErrNemaAdrese},
	{"domena", ErrAdresaNijeDopustena},
	{"zajednicka", ErrZajednickaAdresa},
	{"posiljatelj", ErrNemaPosiljatelja},
	{"neispravan", ErrPosiljateljNeispravan},
	{"zastalo", ErrSlanjeZastalo},
	{"ograniceno", ErrSlanjeOgraniceno},
	{"nije", ErrPINNijePoslan},
}

// razlogZaKorisnika svodi grešku slanja na poruku koja ne otkriva
// pojedinosti poslužitelja
func razlogZaKorisnika(err error) error {
	for _, x := range razloziPIN {
		if errors.Is(err, x.err) {
			return x.err
		}
	}
	return ErrPINNijePoslan
}

// OznakaRazloga je kratka oznaka razloga neposlanog PIN-a (npr. "zastalo"),
// ista kao u adresi stranice s upisom koda; prazno za nil
func OznakaRazloga(err error) string {
	if err == nil {
		return ""
	}
	r := razlogZaKorisnika(err)
	for _, x := range razloziPIN {
		if x.err == r {
			return x.oznaka
		}
	}
	return "nije"
}

// RazlogIzOznake vraća razlog (grešku za korisnika) za oznaku iz
// OznakaRazloga; nil za praznu ili nepoznatu oznaku
func RazlogIzOznake(oznaka string) error {
	for _, x := range razloziPIN {
		if x.oznaka == oznaka {
			return x.err
		}
	}
	return nil
}

// ProlazanRazlog javlja može li novi PIN otići bez administratora (veza,
// ograničenje slanja), pa ima smisla nuditi „Pošalji novi PIN”
func ProlazanRazlog(err error) bool {
	return errors.Is(err, ErrSlanjeZastalo) || errors.Is(err, ErrSlanjeOgraniceno) || errors.Is(err, ErrPINNijePoslan)
}

func kadTekst(t time.Time) string { return t.Local().Format("02.01.2006. u 15:04") }

func porukaPIN(za, pin, ip string, kad time.Time) posta.Poruka {
	odakle := ""
	if ip != "" {
		odakle = ", s adrese " + ip
	}
	tekst := "Kod za prijavu u goCOP: " + pin + "\n\n" +
		"Vrijedi " + fmt.Sprint(int(TrajanjePrijaveNaCekanju/time.Minute)) + " minuta i samo za jednu prijavu.\n" +
		"Prijava: " + kadTekst(kad) + odakle + ".\n\n" +
		"Ako se niste vi prijavljivali, netko zna vašu lozinku: promijenite je odmah na svom profilu u goCOP-u i javite administratoru.\n"
	return posta.Poruka{Za: mail.Address{Address: za}, Predmet: "goCOP: kod za prijavu", Tekst: tekst, Kad: kad}
}

func noviPIN() (string, error) {
	n, err := randomInt(1000000)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n), nil
}

// PosaljiProbniPIN šalje probni PIN administratoru na njegovu adresu, istim
// putem kao pri prijavi. Uspjeh dopušta uključiti PIN (SpremiOpcije);
// vraća maskiranu adresu. Grešku vraća s pojedinostima, za administratora.
func (d *DrugiKorak) PosaljiProbniPIN(ctx context.Context, actor *models.UserPermissions) (string, error) {
	if actor == nil || !actor.IsGlobalAdmin {
		return "", ErrUnauthorized
	}
	if err := d.spreman(); err != nil {
		return "", err
	}
	u, err := d.korisnici.GetUserByID(actor.User.ID)
	if err != nil {
		return "", err
	}
	if u == nil {
		return "", ErrUserNotFound
	}
	adresa, err := d.adresaZaPIN(ctx, u)
	if err != nil {
		return "", err
	}
	pin, err := noviPIN()
	if err != nil {
		return "", err
	}
	m := porukaPIN(adresa, pin, "", d.sad())
	m.Predmet = "goCOP: probni kod za prijavu"
	m.Tekst = "Ovo je probna poruka: račun za slanje PIN-a na ovom čvoru radi.\n\n" + m.Tekst
	if err := d.posalji(ctx, u.ID.String(), m, true); err != nil {
		return "", err
	}
	d.mu.Lock()
	d.probaKad = d.sad()
	d.mu.Unlock()
	log.Printf("prijava izvana: probni PIN poslan administratoru %s", u.Username)
	return MaskirajAdresu(adresa), nil
}

// JaviPromjenuAdrese javlja na staru adresu da je adresa računa promijenjena,
// ako je na nju smio ići PIN: u dopuštenoj domeni ili potvrđena (u nosi
// potvrdu kakva je bila prije promjene). Najbolje što se može: bez
// pošiljatelja ili kad slanje ne uspije, samo se zapiše u zapisnik. Traje
// najviše IstekSlanja; zove se iz pozadine.
func (d *DrugiKorak) JaviPromjenuAdrese(ctx context.Context, u *models.User, stara, nova string) {
	if d.spreman() != nil || u == nil {
		return
	}
	prije := *u
	prije.Email = stara
	if d.dopustenaOsobi(ctx, &prije) != nil {
		return
	}
	kad := d.sad()
	novaTekst := strings.TrimSpace(nova)
	if novaTekst == "" {
		novaTekst = "(prazno)"
	}
	m := posta.Poruka{Za: mail.Address{Address: strings.TrimSpace(stara)}, Predmet: "goCOP: promijenjena adresa e-pošte", Kad: kad,
		Tekst: "Adresa e-pošte vašeg računa " + u.Username + " u goCOP-u promijenjena je " + kadTekst(kad) + " u: " + novaTekst + ".\n" +
			"Na nju ubuduće stiže kod za prijavu izvana.\n\n" +
			"Ako to niste bili vi, odmah javite administratoru.\n"}
	if err := d.posalji(ctx, u.ID.String(), m, false); err != nil {
		log.Printf("prijava izvana: obavijest o promjeni adrese za %s nije poslana: %v", u.Username, err)
	}
}

// ---- prijava na čekanju ----

// PocetakPrijave je ishod točne lozinke izvana: prijava čeka PIN ili kod.
type PocetakPrijave struct {
	// Token je vrijednost kolačića prijave na čekanju (__Host-gocop_prijava,
	// Max-Age = TrajanjePrijaveNaCekanju); u bazi stoji samo njegov HMAC
	Token     string
	PINPoslan bool
	Adresa    string    // maskirana adresa na koju je PIN poslan; prazno kad nije
	Razlog    error     // zašto PIN nije poslan (nil kad jest); poruka je za korisnika
	Istjece   time.Time // rok prijave na čekanju
}

// ZapocniPrijavu otvara prijavu na čekanju za osobu kojoj je lozinka točna
// i pokuša poslati PIN (sinkrono, najviše IstekSlanja). Starije prijave na
// čekanju iste osobe se brišu. Kad PIN ne ode (nema adrese, adresa nije
// dopuštena ili je zajednička, nema pošiljatelja, prekidač, ograničenje),
// prijava svejedno čeka — rezervni ili privremeni kod; Razlog kaže zašto.
// Greška je samo za kvar (baza, ključ).
func (d *DrugiKorak) ZapocniPrijavu(ctx context.Context, u *models.User, ip, ua string) (*PocetakPrijave, error) {
	if err := d.spreman(); err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrUserNotFound
	}
	token, err := noviToken()
	if err != nil {
		return nil, err
	}
	d.zabiljeziIzvana()
	sad := d.sad()
	id := d.idPrijave(token)
	p := &repository.PrijavaNaCekanju{ID: id, UserID: u.ID, IPAddress: ip, UserAgent: ua,
		CreatedAt: sad, ExpiresAt: sad.Add(TrajanjePrijaveNaCekanju)}
	ishod := &PocetakPrijave{Token: token, Istjece: p.ExpiresAt}
	if hash, adresa, err := d.posaljiPIN(ctx, u, id, ip); err != nil {
		ishod.Razlog = razlogZaKorisnika(err)
		p.Razlog = OznakaRazloga(ishod.Razlog)
		if errors.Is(err, ErrNemaPosiljatelja) || errors.Is(err, ErrPosiljateljNeispravan) {
			d.upozoriBezPosiljatelja(ctx)
		}
	} else {
		p.PinHash, p.PoslanoNa, p.PosljednjeSlanje = hash, MaskirajAdresu(adresa), &sad
		ishod.PINPoslan, ishod.Adresa = true, p.PoslanoNa
	}
	// PIN je možda već otišao: prijava se sprema i kad je zahtjev prekinut
	if err := d.repo.ZapocniPrijavu(context.WithoutCancel(ctx), p); err != nil {
		return nil, err
	}
	return ishod, nil
}

// posaljiPIN smisli PIN, pošalje ga i vrati njegov sažetak i adresu
func (d *DrugiKorak) posaljiPIN(ctx context.Context, u *models.User, id []byte, ip string) ([]byte, string, error) {
	adresa, err := d.adresaZaPIN(ctx, u)
	if err != nil {
		return nil, "", err
	}
	pin, err := noviPIN()
	if err != nil {
		return nil, "", err
	}
	if err := d.posalji(ctx, u.ID.String(), porukaPIN(adresa, pin, ip, d.sad()), false); err != nil {
		return nil, "", err
	}
	return d.sazetakPINa(id, pin), adresa, nil
}

// StanjePrijave je prijava na čekanju, za stranicu s upisom PIN-a.
type StanjePrijave struct {
	UserID    uuid.UUID
	PINPoslan bool
	Adresa    string // maskirana adresa na koju je PIN poslan
	Preostalo int    // preostali pokušaji
	Istjece   time.Time
	PonovnoOd time.Time // od kada se smije tražiti novi PIN
	// Razlog je zašto PIN nije poslan (nil kad je poslan i vrijedi): ista
	// greška kao PocetakPrijave.Razlog; ProlazanRazlog kaže ima li smisla
	// tražiti novi
	Razlog error
}

// prijavaIzTokena vraća valjanu prijavu na čekanju; isteklu briše
func (d *DrugiKorak) prijavaIzTokena(ctx context.Context, token string) (*repository.PrijavaNaCekanju, error) {
	if err := d.spreman(); err != nil {
		return nil, err
	}
	if !ispravanToken(token) {
		return nil, ErrDrugiKorakIstekao
	}
	p, err := d.repo.Prijava(ctx, d.idPrijave(token))
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrDrugiKorakIstekao
	}
	if !d.sad().Before(p.ExpiresAt) {
		_, _ = d.repo.ObrisiPrijavu(ctx, p.ID)
		return nil, ErrDrugiKorakIstekao
	}
	return p, nil
}

func ponovnoOd(p *repository.PrijavaNaCekanju) time.Time {
	od := p.CreatedAt
	if p.PosljednjeSlanje != nil && p.PosljednjeSlanje.After(od) {
		od = *p.PosljednjeSlanje
	}
	return od.Add(RazmakPonovnogSlanja)
}

// Cekanje vraća stanje prijave na čekanju po tokenu iz kolačića;
// ErrDrugiKorakIstekao kad je nema ili je istekla.
func (d *DrugiKorak) Cekanje(ctx context.Context, token string) (*StanjePrijave, error) {
	p, err := d.prijavaIzTokena(ctx, token)
	if err != nil {
		return nil, err
	}
	st := &StanjePrijave{UserID: p.UserID, PINPoslan: p.PinHash != nil, Adresa: p.PoslanoNa,
		Preostalo: NajviseKrivihUnosa - p.Pokusaja, Istjece: p.ExpiresAt, PonovnoOd: ponovnoOd(p)}
	if !st.PINPoslan {
		st.Razlog = RazlogIzOznake(p.Razlog)
		if st.Razlog == nil {
			st.Razlog = ErrPINNijePoslan
		}
	}
	return st, nil
}

// PonovnoPosalji šalje novi PIN za prijavu na čekanju (stari prestaje
// vrijediti), najranije RazmakPonovnogSlanja nakon prethodnog i unutar
// ograničenja slanja. Pokušaji se ne vraćaju. Greške: ErrDrugiKorakIstekao,
// ErrPonovnoPrerano; kad novi PIN ne ode, ishod ima Razlog, a stari PIN
// vrijedi i dalje (PINPoslan i Adresa opisuju PIN koji vrijedi).
func (d *DrugiKorak) PonovnoPosalji(ctx context.Context, token string) (*PocetakPrijave, error) {
	p, err := d.prijavaIzTokena(ctx, token)
	if err != nil {
		return nil, err
	}
	sad := d.sad()
	if sad.Before(ponovnoOd(p)) {
		return nil, ErrPonovnoPrerano
	}
	ishod := &PocetakPrijave{Token: token, Istjece: p.ExpiresAt, PINPoslan: p.PinHash != nil, Adresa: p.PoslanoNa}
	u, err := d.korisnici.GetUserByID(p.UserID)
	if err != nil {
		return nil, err
	}
	if u == nil || !u.IsActive {
		_, _ = d.repo.ObrisiPrijavu(ctx, p.ID)
		return nil, ErrDrugiKorakIstekao
	}
	hash, adresa, err := d.posaljiPIN(ctx, u, p.ID, p.IPAddress)
	if err != nil {
		ishod.Razlog = razlogZaKorisnika(err)
		if p.PinHash == nil {
			// PIN-a još nema: prijava pamti zašto ni ovaj nije otišao
			if e := d.repo.PostaviRazlog(context.WithoutCancel(ctx), p.ID, OznakaRazloga(ishod.Razlog)); e != nil {
				log.Printf("prijava izvana: razlog neposlanog PIN-a nije spremljen: %v", e)
			}
		}
		return ishod, nil
	}
	// novi PIN je već otišao: sprema se i kad je zahtjev prekinut, inače bi
	// vrijedio stari, a osoba ima novi
	if err := d.repo.PostaviPIN(context.WithoutCancel(ctx), p.ID, hash, MaskirajAdresu(adresa), sad); err != nil {
		return nil, err
	}
	ishod.PINPoslan, ishod.Adresa, ishod.Razlog = true, MaskirajAdresu(adresa), nil
	return ishod, nil
}

// IshodDrugogKoraka je prošla provjera PIN-a ili koda.
type IshodDrugogKoraka struct {
	Korisnik           *models.User // svjež iz baze, za OtvoriSesiju
	Vrsta              string       // VrstaPIN, VrstaRezervni ili VrstaPrivremeni
	PreostaloRezervnih int          // nakon rezervnog koda: koliko ih je još ostalo
}

var (
	oblikPINa = regexp.MustCompile(`^[0-9]{6}$`)
	oblikKoda = regexp.MustCompile(`^(R[0-9]{1,2}|P)-?([A-Z0-9]{4})-?([A-Z0-9]{4})$`)
)

// razloziUnos čita upis: PIN od šest znamenki ili kod s oznakom
func razloziUnos(unos string) (vrsta, oznaka, tajna string, ok bool) {
	s := strings.ToUpper(strings.Join(strings.Fields(unos), ""))
	if oblikPINa.MatchString(s) {
		return VrstaPIN, "", s, true
	}
	m := oblikKoda.FindStringSubmatch(s)
	if m == nil {
		return "", "", "", false
	}
	vrsta = VrstaRezervni
	if m[1] == "P" {
		vrsta = VrstaPrivremeni
	}
	return vrsta, m[1], m[2] + "-" + m[3], true
}

// ProvjeriKod provjerava PIN, rezervni ili privremeni kod za prijavu na
// čekanju. Uspjeh briše prijavu na čekanju i troši kod; rukovatelj tada
// otvara sesiju. Greške:
//   - ErrDrugiKorakIstekao: nema prijave (istekla, iskorištena, nova je zamijenila);
//   - ErrOblikKoda: upis nije ni PIN ni kod (ne broji se kao pokušaj);
//   - KrivKod (errors.Is ErrKrivKod): krivo, s brojem preostalih pokušaja;
//   - ErrPrevisePokusaja: peti krivi unos, prijava na čekanju je obrisana;
//   - ErrKodoviZakljucani: osoba ima deset krivih unosa u satu;
//   - ErrAccountInactive: račun je u međuvremenu isključen.
func (d *DrugiKorak) ProvjeriKod(ctx context.Context, token, unos string) (*IshodDrugogKoraka, error) {
	p, err := d.prijavaIzTokena(ctx, token)
	if err != nil {
		return nil, err
	}
	sad := d.sad()
	if d.zakljucano(p.UserID, sad) {
		return nil, ErrKodoviZakljucani
	}
	vrsta, oznaka, tajna, ok := razloziUnos(unos)
	if !ok {
		return nil, ErrOblikKoda
	}
	// Pokušaj se zauzme prije usporedbe — po osobi i po prijavi na čekanju —
	// pa istodobni unosi ne dobiju više pokušaja nego što granice daju
	if !d.rezervirajUnos(p.UserID, sad) {
		return nil, ErrKodoviZakljucani
	}
	n, zauzet, err := d.repo.RezervirajPokusaj(ctx, p.ID, NajviseKrivihUnosa)
	if err != nil {
		d.vratiUnos(p.UserID)
		return nil, err
	}
	if !zauzet {
		d.vratiUnos(p.UserID)
		if obrisana, err := d.repo.ObrisiPrijavu(ctx, p.ID); err == nil && !obrisana {
			return nil, ErrDrugiKorakIstekao // iskorištena ili zamijenjena u međuvremenu
		}
		return nil, ErrPrevisePokusaja
	}
	var kod *repository.KodPrijave
	tocno := false
	switch vrsta {
	case VrstaPIN:
		tocno = p.PinHash != nil && hmac.Equal(d.sazetakPINa(p.ID, tajna), p.PinHash)
	default:
		kod, err = d.repo.Kod(ctx, p.UserID, oznaka)
		if err != nil {
			d.vratiUnos(p.UserID)
			return nil, err
		}
		tocno = kod != nil && kod.Vrsta == vrsta && (kod.ExpiresAt == nil || sad.Before(*kod.ExpiresAt)) &&
			hmac.Equal(d.sazetakKoda(p.UserID, oznaka, tajna), kod.KodHash)
	}
	if !tocno {
		d.krivUnos(p.UserID, sad)
		if n >= NajviseKrivihUnosa {
			_, _ = d.repo.ObrisiPrijavu(ctx, p.ID)
			return nil, ErrPrevisePokusaja
		}
		if d.zakljucano(p.UserID, sad) {
			return nil, ErrKodoviZakljucani
		}
		return nil, KrivKod{Preostalo: NajviseKrivihUnosa - n}
	}
	// točan unos se osobi ne broji
	d.vratiUnos(p.UserID)
	// Prijava na čekanju se troši prva: od dva istodobna točna unosa prolazi jedan
	if obrisana, err := d.repo.ObrisiPrijavu(ctx, p.ID); err != nil {
		return nil, err
	} else if !obrisana {
		return nil, ErrDrugiKorakIstekao
	}
	if kod != nil {
		if iskoristen, err := d.repo.IskoristiKod(ctx, kod.ID, sad); err != nil {
			return nil, err
		} else if !iskoristen {
			return nil, ErrDrugiKorakIstekao
		}
	}
	u, err := d.korisnici.GetUserByID(p.UserID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrDrugiKorakIstekao
	}
	if !u.IsActive {
		return nil, ErrAccountInactive
	}
	ishod := &IshodDrugogKoraka{Korisnik: u, Vrsta: vrsta}
	switch vrsta {
	case VrstaRezervni:
		ishod.PreostaloRezervnih, _, _ = d.repo.StanjeRezervnih(ctx, u.ID)
		log.Printf("prijava izvana: %s ušao rezervnim kodom %s (ostalo %d)", u.Username, oznaka, ishod.PreostaloRezervnih)
	case VrstaPrivremeni:
		log.Printf("prijava izvana: %s ušao privremenim kodom od administratora", u.Username)
	}
	return ishod, nil
}

// ---- zapamćeno računalo ----

// ZapamtiRacunalo pamti preglednik za osobu na TrajanjeRacunala. token je
// postojeća vrijednost kolačića __Host-gocop_racunalo (jedan token po
// pregledniku, za sve osobe koje se s njega prijavljuju) ili prazno; vraća
// token koji treba (ponovno) postaviti u kolačić. u mora biti svjež iz baze
// (s PasswordHash), npr. IshodDrugogKoraka.Korisnik.
func (d *DrugiKorak) ZapamtiRacunalo(ctx context.Context, u *models.User, token, ip, ua string) (string, error) {
	if err := d.spreman(); err != nil {
		return "", err
	}
	if u == nil || u.PasswordHash == "" {
		return "", ErrUserNotFound
	}
	if !ispravanToken(token) {
		var err error
		if token, err = noviToken(); err != nil {
			return "", err
		}
	}
	sad := d.sad()
	if len(ua) > 300 {
		ua = ua[:300]
	}
	err := d.repo.SpremiRacunalo(ctx, &repository.ZapamcenoRacunalo{UserID: u.ID, TokenHash: d.sazetakRacunala(token),
		LozinkaOtisak: d.otisakLozinke(u.PasswordHash), UserAgent: ua, IPPrvi: ip, IPZadnji: ip,
		CreatedAt: sad, ExpiresAt: sad.Add(TrajanjeRacunala)})
	if err != nil {
		return "", err
	}
	return token, nil
}

// ProvjeriRacunalo javlja preskače li ovaj preglednik osobi PIN: redak za
// (token, osoba) postoji, nije istekao i lozinka se od pamćenja nije
// mijenjala (ni na drugom čvoru: sažetak lozinke putuje razmjenom). Zove se
// tek nakon točne lozinke, s osobom iz ProvjeriPrijavu. Bilježi upotrebu;
// rok se ne produljuje.
func (d *DrugiKorak) ProvjeriRacunalo(ctx context.Context, u *models.User, token, ip string) bool {
	if d.spreman() != nil || u == nil || u.PasswordHash == "" || !ispravanToken(token) {
		return false
	}
	x, err := d.repo.Racunalo(ctx, d.sazetakRacunala(token), u.ID)
	if err != nil || x == nil {
		return false
	}
	sad := d.sad()
	if !sad.Before(x.ExpiresAt) || !hmac.Equal(x.LozinkaOtisak, d.otisakLozinke(u.PasswordHash)) {
		_, _ = d.repo.ObrisiRacunalo(ctx, u.ID, x.ID)
		return false
	}
	if err := d.repo.OznaciKoristenje(ctx, x.ID, ip, sad); err != nil {
		log.Printf("prijava izvana: upotreba zapamćenog računala nije zabilježena: %v", err)
	}
	return true
}

// Racunalo je zapamćeno računalo za prikaz na profilu.
type Racunalo struct {
	ID          string
	UserAgent   string
	IPPrvi      string
	IPZadnji    string
	Zapamceno   time.Time
	Koristeno   *time.Time
	Istjece     time.Time
	OvoRacunalo bool // preglednik iz kojeg se gleda (po tokenu iz kolačića)
}

// Racunala vraća zapamćena računala osobe na ovom čvoru; token je kolačić
// preglednika koji gleda (može biti prazan), za oznaku OvoRacunalo.
func (d *DrugiKorak) Racunala(ctx context.Context, userID uuid.UUID, token string) ([]Racunalo, error) {
	if err := d.spreman(); err != nil {
		return nil, err
	}
	xs, err := d.repo.Racunala(ctx, userID)
	if err != nil {
		return nil, err
	}
	var moj []byte
	if ispravanToken(token) {
		moj = d.sazetakRacunala(token)
	}
	sad := d.sad()
	var out []Racunalo
	for _, x := range xs {
		if !sad.Before(x.ExpiresAt) {
			continue
		}
		out = append(out, Racunalo{ID: x.ID, UserAgent: x.UserAgent, IPPrvi: x.IPPrvi, IPZadnji: x.IPZadnji,
			Zapamceno: x.CreatedAt, Koristeno: x.LastUsedAt, Istjece: x.ExpiresAt,
			OvoRacunalo: moj != nil && hmac.Equal(moj, x.TokenHash)})
	}
	return out, nil
}

// Zaboravi zaboravlja jedno računalo osobe. userID mora biti stvarno
// prijavljena osoba; dok se gleda tuđim očima, odbija se (ErrTudjimOcima).
func (d *DrugiKorak) Zaboravi(ctx context.Context, userID uuid.UUID, id string, tudjimOcima bool) error {
	if tudjimOcima {
		return ErrTudjimOcima
	}
	ok, err := d.repo.ObrisiRacunalo(ctx, userID, id)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("računalo nije pronađeno")
	}
	return nil
}

// ZaboraviSva zaboravlja sva računala osobe na ovom čvoru i vraća koliko ih
// je bilo; pravila kao Zaboravi.
func (d *DrugiKorak) ZaboraviSva(ctx context.Context, userID uuid.UUID, tudjimOcima bool) (int, error) {
	if tudjimOcima {
		return 0, ErrTudjimOcima
	}
	return d.repo.ObrisiRacunalaKorisnika(ctx, userID)
}

// ZaboraviOvo zaboravlja preglednik s tokenom za osobu (npr. „Zaboravi ovo
// računalo” pri odjavi); bez greške kad ga nema.
func (d *DrugiKorak) ZaboraviOvo(ctx context.Context, userID uuid.UUID, token string) error {
	if d.spreman() != nil || !ispravanToken(token) {
		return nil
	}
	return d.repo.ObrisiRacunaloPoTokenu(ctx, userID, d.sazetakRacunala(token))
}

// ---- kodovi ----

// abecedaKodova su znakovi kodova bez onih koji se miješaju (0/O, 1/I)
const abecedaKodova = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// noviKod je osam znakova (40 bita) oblika XXXX-XXXX
func noviKod() (string, error) {
	var b strings.Builder
	for i := 0; i < 8; i++ {
		if i == 4 {
			b.WriteByte('-')
		}
		n, err := randomInt(len(abecedaKodova))
		if err != nil {
			return "", err
		}
		b.WriteByte(abecedaKodova[n])
	}
	return b.String(), nil
}

// NapraviRezervneKodove pravi novi niz od BrojRezervnihKodova rezervnih
// kodova (R1-XXXX-XXXX … R10-XXXX-XXXX) i briše stari. Kodovi se vraćaju
// samo ovdje, jednom (stranica no-store; .txt preuzimanje zove ovo ponovno
// i tako pravi novi niz). korisnik je stvarno prijavljena osoba, uz
// njezinu lozinku (kriva: errors.Is ErrKrivaLozinka); dok se gleda tuđim
// očima, odbija se.
func (d *DrugiKorak) NapraviRezervneKodove(ctx context.Context, korisnik *models.User, lozinka string, tudjimOcima bool) ([]string, error) {
	if tudjimOcima {
		return nil, ErrTudjimOcima
	}
	if err := d.spreman(); err != nil {
		return nil, err
	}
	if korisnik == nil {
		return nil, ErrUserNotFound
	}
	u, err := d.korisnici.GetUserByID(korisnik.ID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrUserNotFound
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(lozinka)) != nil {
		return nil, greskaLozinke("lozinka nije točna")
	}
	sad := d.sad()
	kodovi := make([]string, 0, BrojRezervnihKodova)
	zapisi := make([]repository.KodPrijave, 0, BrojRezervnihKodova)
	for i := 1; i <= BrojRezervnihKodova; i++ {
		tajna, err := noviKod()
		if err != nil {
			return nil, err
		}
		oznaka := fmt.Sprintf("R%d", i)
		kodovi = append(kodovi, oznaka+"-"+tajna)
		zapisi = append(zapisi, repository.KodPrijave{Oznaka: oznaka, KodHash: d.sazetakKoda(u.ID, oznaka, tajna),
			Izdao: u.Username, CreatedAt: sad})
	}
	if err := d.repo.ZamijeniRezervne(ctx, u.ID, zapisi); err != nil {
		return nil, err
	}
	log.Printf("prijava izvana: %s je napravio nove rezervne kodove", u.Username)
	return kodovi, nil
}

// StanjeRezervnih vraća koliko je osobi ostalo rezervnih kodova i kad je
// niz napravljen (nil: nikad)
func (d *DrugiKorak) StanjeRezervnih(ctx context.Context, userID uuid.UUID) (int, *time.Time, error) {
	return d.repo.StanjeRezervnih(ctx, userID)
}

// IzdajPrivremeniKod daje osobi privremeni kod za prijavu izvana (P-XXXX-XXXX),
// za jednu prijavu u TrajanjePrivremenogKoda; stariji neiskorišteni prestaje
// vrijediti. Smije tko smije poništiti lozinku te osobe (smijePonistiti:
// smije uređivati cijeli račun),
// nikad sebi, i ne dok gleda tuđim očima (actor su stvarne ovlasti
// prijavljenog). Kod se vraća jednom i ne zapisuje; u zapisnik ide tko je
// kome izdao. Poništavanje lozinke briše privremene kodove, pa uz
// poništavanje kod treba izdati poslije njega.
func (d *DrugiKorak) IzdajPrivremeniKod(ctx context.Context, actor *models.UserPermissions, targetID uuid.UUID, tudjimOcima bool) (string, time.Time, error) {
	if tudjimOcima {
		return "", time.Time{}, ErrTudjimOcima
	}
	if err := d.spreman(); err != nil {
		return "", time.Time{}, err
	}
	target, err := d.korisnici.GetUserByID(targetID)
	if err != nil {
		return "", time.Time{}, err
	}
	if target == nil {
		return "", time.Time{}, ErrUserNotFound
	}
	if actor != nil && actor.User.ID == target.ID {
		return "", time.Time{}, errors.New("privremeni kod ne izdaje se samome sebi; napravite rezervne kodove na svom profilu")
	}
	if err := smijePonistiti(actor, target, sektoriPodrucja(d.korisnici.ListAreas)); err != nil {
		return "", time.Time{}, err
	}
	tajna, err := noviKod()
	if err != nil {
		return "", time.Time{}, err
	}
	sad := d.sad()
	istjece := sad.Add(TrajanjePrivremenogKoda)
	if err := d.repo.DodajPrivremeni(ctx, &repository.KodPrijave{UserID: target.ID, Oznaka: "P",
		KodHash: d.sazetakKoda(target.ID, "P", tajna), Izdao: actor.User.Username, CreatedAt: sad, ExpiresAt: &istjece}); err != nil {
		return "", time.Time{}, err
	}
	log.Printf("prijava izvana: %s je izdao privremeni kod za %s (vrijedi do %s)", actor.User.Username, target.Username, kadTekst(istjece))
	return "P-" + tajna, istjece, nil
}

// ---- opoziv i čišćenje ----

// Opozovi briše osobi na ovom čvoru zapamćena računala, prijave na čekanju
// i privremene kodove; rezervni kodovi ostaju. Zove se pri promjeni,
// poništavanju i administratorskoj izmjeni lozinke.
func (d *DrugiKorak) Opozovi(ctx context.Context, userID uuid.UUID) error {
	if d == nil || d.repo == nil {
		return nil
	}
	if _, err := d.repo.ObrisiRacunalaKorisnika(ctx, userID); err != nil {
		return err
	}
	if err := d.repo.ObrisiPrijaveKorisnika(ctx, userID); err != nil {
		return err
	}
	return d.repo.ObrisiPrivremene(ctx, userID)
}

// Ocisti briše istekle prijave na čekanju, računala i privremene kodove i
// stara brojila; zove se iz satnog čišćenja uz sesije. Usput upozori u
// zapisniku kad je PIN uključen, a ovaj čvor nema ispravnog pošiljatelja.
func (d *DrugiKorak) Ocisti(ctx context.Context) (int, error) {
	d.upozoriBezPosiljatelja(ctx)
	sad := d.sad()
	d.mu.Lock()
	for k := range d.dogadaji {
		d.broj(k, sad, time.Hour)
	}
	for k, do := range d.blokDo {
		if !sad.Before(do) {
			delete(d.blokDo, k)
		}
	}
	d.mu.Unlock()
	return d.repo.OcistiIstekle(ctx, sad)
}
