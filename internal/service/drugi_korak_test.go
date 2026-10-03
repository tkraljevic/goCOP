package service_test

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// laznaPosta pamti poslane poruke umjesto da ih šalje
type laznaPosta struct {
	mu       sync.Mutex
	poruke   []posta.Poruka
	greska   error // greška veze ili prijave pri slanju
	pozivi   int
	adresar  []posta.Kontakt
	postavke []string // „radnja klijent svježa” za svaki poziv
	// usred se zove usred slanja, izvan brave, s kontekstom slanja; greška
	// ide van umjesto slanja
	usred func(ctx context.Context) error
}

func (l *laznaPosta) zapisi(radnja string, p posta.Postavke) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.postavke = append(l.postavke, fmt.Sprintf("%s %s %v", radnja, p.Klijent, p.SvjezaVeza))
}

func (l *laznaPosta) Prijavi(_ context.Context, p posta.Postavke, r posta.Racun) (string, error) {
	l.zapisi("prijavi", p)
	return r.Korisnik, nil
}

func (l *laznaPosta) Posalji(ctx context.Context, p posta.Postavke, _ posta.Racun, poruke []posta.Poruka) ([]error, error) {
	l.zapisi("posalji", p)
	l.mu.Lock()
	usred := l.usred
	l.mu.Unlock()
	if usred != nil {
		if err := usred(ctx); err != nil {
			l.mu.Lock()
			l.pozivi++
			l.mu.Unlock()
			return nil, err
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pozivi++
	if l.greska != nil {
		return nil, l.greska
	}
	l.poruke = append(l.poruke, poruke...)
	return make([]error, len(poruke)), nil
}

func (l *laznaPosta) Imenik(_ context.Context, p posta.Postavke, _ posta.Racun, _ string) ([]posta.Kontakt, error) {
	l.zapisi("imenik", p)
	return l.adresar, nil
}

func (l *laznaPosta) zadnja(t *testing.T) posta.Poruka {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.poruke) == 0 {
		t.Fatal("ništa nije poslano")
	}
	return l.poruke[len(l.poruke)-1]
}

func (l *laznaPosta) broj() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.pozivi
}

var pinIzPoruke = regexp.MustCompile(`\b(\d{6})\b`)

func (l *laznaPosta) pin(t *testing.T) string {
	t.Helper()
	m := pinIzPoruke.FindStringSubmatch(l.zadnja(t).Tekst)
	if m == nil {
		t.Fatalf("u poruci nema PIN-a: %q", l.zadnja(t).Tekst)
	}
	return m[1]
}

type okolinaPIN struct {
	dk     *service.DrugiKorak
	posta  *laznaPosta
	users  *service.UserService
	auth   *service.AuthService
	repo   *repository.UserRepository
	dkRepo *repository.DrugiKorakRepository
	admin  *models.UserPermissions
	osoba  *models.User // pero.peric@voda.hr, lozinka "lozinka1"
	sad    time.Time
}

func (o *okolinaPIN) pomakni(d time.Duration) { o.sad = o.sad.Add(d) }

func (o *okolinaPIN) korisnik(t *testing.T, ime, email string) *models.User {
	t.Helper()
	hash, err := o.auth.HashPassword("lozinka1")
	if err != nil {
		t.Fatal(err)
	}
	u := &models.User{Username: ime, PasswordHash: hash, FullName: ime, Email: email, IsActive: true, OrgType: models.OrgHrvatskeVode}
	if err := o.repo.CreateUser(u, nil); err != nil {
		t.Fatal(err)
	}
	svjez, err := o.repo.GetUserByID(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return svjez
}

// okolinaDrugogKoraka: prazan čvor s administratorom, jednom osobom i
// upisanim pošiljateljem PIN-a; PIN još nije uključen
func okolinaDrugogKoraka(t *testing.T) *okolinaPIN {
	t.Helper()
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(database, "cvor-a")
	o := &okolinaPIN{posta: &laznaPosta{}, sad: time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)}
	o.repo = repository.NewUserRepository(database, rec)
	o.auth = service.NewAuthService(o.repo, repository.NewSessionRepository(database))
	o.users = service.NewUserService(o.repo, o.auth, service.NewSSEBroker())
	o.dkRepo = repository.NewDrugiKorakRepository(database)
	o.dk = service.NewDrugiKorak(o.dkRepo, repository.NewRacuniSustavaRepository(database), o.repo, repository.NewAktiRepository(database, rec))
	o.dk.SetKljuc([]byte("sjeme-cvora-za-test-32-bajta-xxx"))
	o.dk.SetPostar(o.posta)
	o.dk.SetSat(func() time.Time { return o.sad })
	o.dk.SetPosta(func(context.Context) posta.Postavke {
		return posta.Postavke{Nacin: posta.NacinEWS, Posluzitelj: "owa.primjer.hr"}
	})
	o.auth.SetZastitaPrijave(o.dk)

	glavni := o.korisnik(t, "uprava", "uprava@voda.hr")
	glavni.IsGlobalAdmin = true
	o.admin = &models.UserPermissions{IsGlobalAdmin: true, User: *glavni}
	o.osoba = o.korisnik(t, "pperic", "pero.peric@voda.hr")
	if _, err := o.dk.SpremiPosiljatelja(context.Background(), o.admin, "VODA\\pin", "tajna", "pin@voda.hr"); err != nil {
		t.Fatal(err)
	}
	return o
}

// ukljuci pošalje probni PIN i uključi PIN izvana
func (o *okolinaPIN) ukljuci(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); err != nil {
		t.Fatal(err)
	}
	if err := o.dk.PostaviUkljuceno(ctx, o.admin, true, true); err != nil {
		t.Fatal(err)
	}
}

// Izvana je sve što dolazi kroz posrednika ili s javne adrese; lokalna
// mreža, ovo računalo i lokalna veza nisu. Lažno zaglavlje posrednika samo
// nametne PIN.
func TestIzvanaAdresa(t *testing.T) {
	cases := []struct {
		adresa    string
		posrednik bool
		izvana    bool
	}{
		{"192.168.1.5", false, false},
		{"10.0.0.7", false, false},
		{"172.16.3.1", false, false},
		{"127.0.0.1", false, false},
		{"::1", false, false},
		{"fe80::1", false, false},
		{"fd00::5", false, false},
		{"::ffff:192.168.1.5", false, false},
		{"8.8.8.8", false, true},
		{"2a00:1450::1", false, true},
		{"192.168.1.5", true, true},
	}
	for _, c := range cases {
		if got := service.IzvanaAdresa(c.posrednik, netip.MustParseAddr(c.adresa)); got != c.izvana {
			t.Errorf("%s (posrednik %v): izvana %v, očekivano %v", c.adresa, c.posrednik, got, c.izvana)
		}
	}
	if !service.IzvanaAdresa(false, netip.Addr{}) {
		t.Error("nepoznata adresa mora biti izvana")
	}
}

// PIN je zadano isključen i uključuje se tek nakon probnog PIN-a s ovog
// čvora; lokalna prijava ga nikad ne traži.
func TestPINSeUkljucujeTekNakonProbe(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	if o.dk.Ukljuceno(ctx) || o.dk.TrebaDrugiKorak(ctx, true) {
		t.Fatal("PIN mora biti zadano isključen")
	}
	if err := o.dk.PostaviUkljuceno(ctx, o.admin, true, true); !errors.Is(err, service.ErrProbaPotrebna) {
		t.Fatalf("uključen bez probe: %v", err)
	}
	nije := &models.UserPermissions{User: *o.osoba}
	if err := o.dk.PostaviUkljuceno(ctx, nije, false, false); !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("djelatnik mijenja postavku: %v", err)
	}
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); err != nil {
		t.Fatal(err)
	}
	if m := o.posta.zadnja(t); m.Za.Address != "uprava@voda.hr" || !m.BezKopije || m.Od.Address != "pin@voda.hr" {
		t.Fatalf("probni PIN: %+v", m)
	}
	if err := o.dk.PostaviUkljuceno(ctx, o.admin, true, true); err != nil {
		t.Fatal(err)
	}
	if !o.dk.TrebaDrugiKorak(ctx, true) || o.dk.TrebaDrugiKorak(ctx, false) {
		t.Fatal("PIN se traži samo izvana")
	}
	if got := o.dk.Opcije(ctx).Domena; got != "voda.hr" {
		t.Fatalf("domena %q", got)
	}
	if err := o.dk.PostaviUkljuceno(ctx, o.admin, false, false); err != nil || o.dk.Ukljuceno(ctx) {
		t.Fatalf("isključivanje: %v", err)
	}
}

// Točna lozinka izvana daje samo prijavu na čekanju; PIN stiže na službenu
// adresu (ne u predmetu, ne u Poslano), vrijedi jednom i 10 minuta.
func TestPINPrijavaJednomIDesetMinuta(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()

	p, err := o.dk.ZapocniPrijavu(ctx, o.osoba, "203.0.113.9", "Firefox")
	if err != nil {
		t.Fatal(err)
	}
	if !p.PINPoslan || p.Adresa != "p***@voda.hr" || p.Razlog != nil || p.Token == "" {
		t.Fatalf("početak: %+v", p)
	}
	m := o.posta.zadnja(t)
	pin := o.posta.pin(t)
	if m.Za.Address != "pero.peric@voda.hr" || !m.BezKopije || strings.Contains(m.Predmet, pin) || !strings.Contains(m.Tekst, "203.0.113.9") {
		t.Fatalf("poruka: %+v", m)
	}
	krivi := "000000"
	if pin == krivi {
		krivi = "111111"
	}
	var kk service.KrivKod
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, krivi); !errors.As(err, &kk) || kk.Preostalo != 4 || !errors.Is(err, service.ErrKrivKod) {
		t.Fatalf("krivi PIN: %v", err)
	}
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, "12ab"); !errors.Is(err, service.ErrOblikKoda) {
		t.Fatalf("neispravan oblik: %v", err)
	}
	if s, err := o.dk.Cekanje(ctx, p.Token); err != nil || s.Preostalo != 4 || !s.PINPoslan {
		t.Fatalf("stanje: %+v %v", s, err)
	}
	ishod, err := o.dk.ProvjeriKod(ctx, p.Token, " "+pin+" ")
	if err != nil || ishod.Korisnik.ID != o.osoba.ID || ishod.Vrsta != service.VrstaPIN {
		t.Fatalf("točan PIN: %+v %v", ishod, err)
	}
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, pin); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Fatalf("PIN dvaput: %v", err)
	}

	// istekao PIN
	p, _ = o.dk.ZapocniPrijavu(ctx, o.osoba, "203.0.113.9", "Firefox")
	pin = o.posta.pin(t)
	o.pomakni(service.TrajanjePrijaveNaCekanju + time.Second)
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, pin); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Fatalf("istekao PIN: %v", err)
	}
	// lažan kolačić
	if _, err := o.dk.ProvjeriKod(ctx, "nije-token", pin); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Fatalf("lažan token: %v", err)
	}
}

// Pet krivih unosa briše prijavu na čekanju; nova prijava brise staru.
func TestPetKrivihBrisePrijavu(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()
	p, _ := o.dk.ZapocniPrijavu(ctx, o.osoba, "203.0.113.9", "")
	pin := o.posta.pin(t)
	krivi := "999999"
	if pin == krivi {
		krivi = "888888"
	}
	for i := 1; i < service.NajviseKrivihUnosa; i++ {
		if _, err := o.dk.ProvjeriKod(ctx, p.Token, krivi); !errors.Is(err, service.ErrKrivKod) {
			t.Fatalf("%d. krivi: %v", i, err)
		}
	}
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, krivi); !errors.Is(err, service.ErrPrevisePokusaja) {
		t.Fatalf("peti krivi: %v", err)
	}
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, pin); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Fatalf("točan nakon petog krivog: %v", err)
	}

	o.pomakni(5 * time.Minute)
	stara, _ := o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
	stariPIN := o.posta.pin(t)
	o.pomakni(5 * time.Minute)
	if _, err := o.dk.ZapocniPrijavu(ctx, o.osoba, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := o.dk.ProvjeriKod(ctx, stara.Token, stariPIN); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Fatalf("stara prijava nakon nove: %v", err)
	}
}

// Deset krivih u satu po osobi zaključa unos na sat, i preko novih
// prijava na čekanju — nova prijava ne daje svježih pokušaja.
func TestKriviKodoviPoOsobiPrekoNovihPrijava(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()
	krivih := 0
	var token, pin string
	for krivih < 10 {
		o.pomakni(6 * time.Minute) // da slanje PIN-a ne udari u svoje ograničenje
		p, err := o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
		if err != nil || !p.PINPoslan {
			t.Fatalf("prijava: %+v %v", p, err)
		}
		token, pin = p.Token, o.posta.pin(t)
		krivi := "000000"
		if pin == krivi {
			krivi = "111111"
		}
		for i := 0; i < 4 && krivih < 10; i++ {
			_, err := o.dk.ProvjeriKod(ctx, token, krivi)
			krivih++
			if krivih < 10 && !errors.Is(err, service.ErrKrivKod) {
				t.Fatalf("%d. krivi: %v", krivih, err)
			}
			if krivih == 10 && !errors.Is(err, service.ErrKodoviZakljucani) {
				t.Fatalf("deseti krivi: %v", err)
			}
		}
	}
	if _, err := o.dk.ProvjeriKod(ctx, token, pin); !errors.Is(err, service.ErrKodoviZakljucani) {
		t.Fatalf("točan PIN dok je zaključano: %v", err)
	}
	o.pomakni(time.Hour + time.Minute)
	p, _ := o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, o.posta.pin(t)); err != nil {
		t.Fatalf("nakon sata: %v", err)
	}
}

// Bez adrese, s tuđom domenom ili zajedničkom adresom PIN ne ide; prijava
// svejedno čeka kod, a „PIN” tada ne prolazi.
func TestBezDopusteneAdreseSamoKodovi(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()
	bez := o.korisnik(t, "bezposte", "")
	tudja := o.korisnik(t, "tudja", "netko@gmail.com")
	zaj1 := o.korisnik(t, "zaj1", "zajednicka@voda.hr")
	o.korisnik(t, "zaj2", "Zajednicka@voda.hr")
	prije := o.posta.broj()
	for _, c := range []struct {
		u    *models.User
		want error
	}{{bez, service.ErrNemaAdrese}, {tudja, service.ErrAdresaNijeDopustena}, {zaj1, service.ErrZajednickaAdresa}} {
		p, err := o.dk.ZapocniPrijavu(ctx, c.u, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if p.PINPoslan || !errors.Is(p.Razlog, c.want) {
			t.Errorf("%s: %+v, očekivan razlog %v", c.u.Username, p, c.want)
		}
		if _, err := o.dk.ProvjeriKod(ctx, p.Token, "123456"); !errors.Is(err, service.ErrKrivKod) {
			t.Errorf("%s: PIN bez poslanog PIN-a: %v", c.u.Username, err)
		}
	}
	if o.posta.broj() != prije {
		t.Fatal("PIN je poslan na nedopuštenu adresu")
	}
}

// Rezervni kodovi: samo uz lozinku, nikad tuđim očima, svaki jednom; novi
// niz poništava stari.
func TestRezervniKodovi(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()
	if _, err := o.dk.NapraviRezervneKodove(ctx, o.osoba, "kriva", false); !errors.Is(err, service.ErrKrivaLozinka) {
		t.Fatalf("kriva lozinka: %v", err)
	}
	if _, err := o.dk.NapraviRezervneKodove(ctx, o.osoba, "lozinka1", true); !errors.Is(err, service.ErrTudjimOcima) {
		t.Fatalf("tuđim očima: %v", err)
	}
	kodovi, err := o.dk.NapraviRezervneKodove(ctx, o.osoba, "lozinka1", false)
	if err != nil {
		t.Fatal(err)
	}
	oblik := regexp.MustCompile(`^R(10|[1-9])-[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$`)
	if len(kodovi) != service.BrojRezervnihKodova {
		t.Fatalf("%d kodova", len(kodovi))
	}
	for _, k := range kodovi {
		if !oblik.MatchString(k) {
			t.Errorf("kod %q nije oblika R3-XXXX-XXXX bez 0, O, 1, I", k)
		}
	}
	if n, kad, _ := o.dk.StanjeRezervnih(ctx, o.osoba.ID); n != 10 || kad == nil {
		t.Fatalf("stanje: %d %v", n, kad)
	}

	p, _ := o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
	ishod, err := o.dk.ProvjeriKod(ctx, p.Token, strings.ToLower(strings.ReplaceAll(kodovi[2], "-", " ")))
	if err != nil || ishod.Vrsta != service.VrstaRezervni || ishod.PreostaloRezervnih != 9 {
		t.Fatalf("rezervni kod: %+v %v", ishod, err)
	}
	o.pomakni(6 * time.Minute)
	p, _ = o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, kodovi[2]); !errors.Is(err, service.ErrKrivKod) {
		t.Fatalf("rezervni kod dvaput: %v", err)
	}
	// tuđi kod s istom oznakom ne vrijedi
	druga := o.korisnik(t, "druga", "druga@voda.hr")
	drugiKodovi, _ := o.dk.NapraviRezervneKodove(ctx, druga, "lozinka1", false)
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, drugiKodovi[4]); !errors.Is(err, service.ErrKrivKod) {
		t.Fatalf("tuđi kod: %v", err)
	}
	novi, _ := o.dk.NapraviRezervneKodove(ctx, o.osoba, "lozinka1", false)
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, kodovi[5]); !errors.Is(err, service.ErrKrivKod) {
		t.Fatalf("stari niz nakon novog: %v", err)
	}
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, novi[5]); err != nil {
		t.Fatalf("novi niz: %v", err)
	}
}

// Privremeni kod od administratora: tko smije poništiti lozinku, nikad
// sebi ni tuđim očima, jednom i 24 sata.
func TestPrivremeniKod(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()
	if _, _, err := o.dk.IzdajPrivremeniKod(ctx, o.admin, o.admin.User.ID, false); err == nil {
		t.Fatal("kod samome sebi")
	}
	if _, _, err := o.dk.IzdajPrivremeniKod(ctx, &models.UserPermissions{User: *o.korisnik(t, "obican", "o@voda.hr")}, o.osoba.ID, false); !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("bez ovlasti: %v", err)
	}
	if _, _, err := o.dk.IzdajPrivremeniKod(ctx, o.admin, o.osoba.ID, true); !errors.Is(err, service.ErrTudjimOcima) {
		t.Fatalf("tuđim očima: %v", err)
	}
	kod, istjece, err := o.dk.IzdajPrivremeniKod(ctx, o.admin, o.osoba.ID, false)
	if err != nil || !regexp.MustCompile(`^P-[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$`).MatchString(kod) || !istjece.Equal(o.sad.Add(24*time.Hour)) {
		t.Fatalf("kod %q do %v: %v", kod, istjece, err)
	}
	p, _ := o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
	if ishod, err := o.dk.ProvjeriKod(ctx, p.Token, kod); err != nil || ishod.Vrsta != service.VrstaPrivremeni {
		t.Fatalf("privremeni kod: %+v %v", ishod, err)
	}
	o.pomakni(6 * time.Minute)
	p, _ = o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, kod); !errors.Is(err, service.ErrKrivKod) {
		t.Fatalf("privremeni kod dvaput: %v", err)
	}

	kod, _, _ = o.dk.IzdajPrivremeniKod(ctx, o.admin, o.osoba.ID, false)
	o.pomakni(24*time.Hour + time.Minute)
	p, _ = o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, kod); !errors.Is(err, service.ErrKrivKod) {
		t.Fatalf("istekao privremeni kod: %v", err)
	}
}

// Zapamćeno računalo: jedan preglednik za više osoba, vrijedi 30 dana i
// dok se lozinka ne promijeni — i kad sažetak stigne s drugog čvora.
func TestZapamcenoRacunalo(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	druga := o.korisnik(t, "druga", "druga@voda.hr")
	token, err := o.dk.ZapamtiRacunalo(ctx, o.osoba, "", "203.0.113.9", "Firefox")
	if err != nil {
		t.Fatal(err)
	}
	if o.dk.ProvjeriRacunalo(ctx, druga, token, "") {
		t.Fatal("tuđe zapamćeno računalo vrijedi")
	}
	if t2, err := o.dk.ZapamtiRacunalo(ctx, druga, token, "203.0.113.9", "Firefox"); err != nil || t2 != token {
		t.Fatalf("isti preglednik, druga osoba: %q %v", t2, err)
	}
	if !o.dk.ProvjeriRacunalo(ctx, o.osoba, token, "203.0.113.10") || !o.dk.ProvjeriRacunalo(ctx, druga, token, "") {
		t.Fatal("zajedničko računalo ne vrijedi za obje osobe")
	}
	r, err := o.dk.Racunala(ctx, o.osoba.ID, token)
	if err != nil || len(r) != 1 || !r[0].OvoRacunalo || r[0].IPZadnji != "203.0.113.10" || r[0].Koristeno == nil {
		t.Fatalf("popis: %+v %v", r, err)
	}
	if err := o.dk.Zaboravi(ctx, o.osoba.ID, r[0].ID, true); !errors.Is(err, service.ErrTudjimOcima) {
		t.Fatalf("tuđim očima: %v", err)
	}
	if err := o.dk.Zaboravi(ctx, druga.ID, r[0].ID, false); err == nil {
		t.Fatal("tuđe računalo zaboravljeno")
	}

	// lozinka promijenjena na drugom čvoru: stigao je samo novi sažetak
	hash, _ := o.auth.HashPassword("nova-lozinka")
	if err := o.repo.ChangePassword(o.osoba.ID, hash); err != nil {
		t.Fatal(err)
	}
	svjez, _ := o.repo.GetUserByID(o.osoba.ID)
	if o.dk.ProvjeriRacunalo(ctx, svjez, token, "") {
		t.Fatal("računalo vrijedi nakon promjene lozinke")
	}
	if r, _ := o.dk.Racunala(ctx, o.osoba.ID, token); len(r) != 0 {
		t.Fatalf("nevaljano računalo ostalo: %+v", r)
	}

	o.pomakni(service.TrajanjeRacunala + time.Minute)
	if o.dk.ProvjeriRacunalo(ctx, druga, token, "") {
		t.Fatal("računalo vrijedi nakon 30 dana")
	}
}

// Promjena lozinke, poništavanje i administratorska izmjena s lozinkom
// opozivaju zapamćena računala, prijave na čekanju i privremene kodove.
func TestPromjenaLozinkeOpozivaDrugiKorak(t *testing.T) {
	ctx := context.Background()
	for _, nacin := range []string{"promjena", "poništavanje", "izmjena"} {
		t.Run(nacin, func(t *testing.T) {
			o := okolinaDrugogKoraka(t)
			o.ukljuci(t)
			token, _ := o.dk.ZapamtiRacunalo(ctx, o.osoba, "", "", "")
			p, _ := o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
			pin := o.posta.pin(t)
			kod, _, _ := o.dk.IzdajPrivremeniKod(ctx, o.admin, o.osoba.ID, false)
			rezervni, _ := o.dk.NapraviRezervneKodove(ctx, o.osoba, "lozinka1", false)
			sesija, err := o.auth.OtvoriSesiju(o.osoba, "", "")
			if err != nil {
				t.Fatal(err)
			}

			switch nacin {
			case "promjena":
				if err := o.auth.ChangePassword(o.osoba.ID, "lozinka1", "lozinka2", uuid.Nil); err != nil {
					t.Fatal(err)
				}
			case "poništavanje":
				if _, _, err := o.users.ResetPassword(o.admin, o.osoba.ID); err != nil {
					t.Fatal(err)
				}
			case "izmjena":
				x := o.osoba
				if _, err := o.users.UpdateUser(o.admin, service.UpdateUserRequest{ID: x.ID, Username: x.Username, FullName: x.FullName,
					OrgType: x.OrgType, Email: x.Email, IsActive: true, Password: "lozinka3"}); err != nil {
					t.Fatal(err)
				}
			}
			if r, _ := o.dk.Racunala(ctx, o.osoba.ID, token); len(r) != 0 {
				t.Errorf("računalo ostalo: %+v", r)
			}
			if _, _, err := o.auth.AuthenticateSession(sesija.ID); err == nil {
				t.Error("otvorena prijava ostala nakon promjene lozinke")
			}
			if _, err := o.dk.ProvjeriKod(ctx, p.Token, pin); !errors.Is(err, service.ErrDrugiKorakIstekao) {
				t.Errorf("prijava na čekanju ostala: %v", err)
			}
			o.pomakni(6 * time.Minute)
			p, _ = o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
			if _, err := o.dk.ProvjeriKod(ctx, p.Token, kod); !errors.Is(err, service.ErrKrivKod) {
				t.Errorf("privremeni kod ostao: %v", err)
			}
			if _, err := o.dk.ProvjeriKod(ctx, p.Token, rezervni[0]); err != nil {
				t.Errorf("rezervni kodovi moraju ostati: %v", err)
			}
		})
	}
}

// Poslužitelj odbije lozinku pošiljatelja: račun se označi i više se ne
// pokušava (svaki pokušaj je neuspjela prijava na račun domene) dok
// administrator ne upiše novu. Greška veze zaustavi slanje na pet minuta.
func TestPrekidacPosiljatelja(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()

	o.posta.greska = posta.ErrPrijava
	prije := o.posta.broj()
	p, err := o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
	if err != nil || p.PINPoslan || !errors.Is(p.Razlog, service.ErrPosiljateljNeispravan) {
		t.Fatalf("odbijena lozinka: %+v %v", p, err)
	}
	o.pomakni(time.Hour)
	p, _ = o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
	if !errors.Is(p.Razlog, service.ErrPosiljateljNeispravan) || o.posta.broj() != prije+1 {
		t.Fatalf("drugi pokušaj s odbijenom lozinkom: %+v, poziva %d", p, o.posta.broj()-prije)
	}
	s, _ := o.dk.Stanje(ctx)
	if s.Posiljatelj == nil || s.Posiljatelj.NeispravanOd == nil || s.ProbaUspjela {
		t.Fatalf("stanje: %+v", s)
	}
	if err := o.dk.PostaviUkljuceno(ctx, o.admin, false, false); err != nil {
		t.Fatal(err)
	}
	if err := o.dk.PostaviUkljuceno(ctx, o.admin, true, true); !errors.Is(err, service.ErrPosiljateljNeispravan) {
		t.Fatalf("uključen s neispravnim pošiljateljem: %v", err)
	}

	// nova lozinka vraća slanje; greška veze ga zaustavi na pet minuta
	o.posta.greska = nil
	if _, err := o.dk.SpremiPosiljatelja(ctx, o.admin, "VODA\\pin", "nova", "pin@voda.hr"); err != nil {
		t.Fatal(err)
	}
	o.posta.greska = errors.New("veza prekinuta")
	prije = o.posta.broj()
	p, _ = o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
	if !errors.Is(p.Razlog, service.ErrSlanjeZastalo) {
		t.Fatalf("greška veze: %+v", p)
	}
	o.posta.greska = nil
	o.pomakni(time.Minute)
	p, _ = o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
	if !errors.Is(p.Razlog, service.ErrSlanjeZastalo) || o.posta.broj() != prije+1 {
		t.Fatalf("slanje u stanci: %+v", p)
	}
	o.pomakni(5 * time.Minute)
	if p, _ = o.dk.ZapocniPrijavu(ctx, o.osoba, "", ""); !p.PINPoslan {
		t.Fatalf("nakon stanke: %+v", p)
	}
}

// Novi PIN najranije minutu nakon prethodnog, stari tada ne vrijedi; po
// osobi najviše tri PIN-a u 15 minuta.
func TestPonovnoSlanjeIOgranicenje(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()
	p, _ := o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
	stari := o.posta.pin(t)
	if _, err := o.dk.PonovnoPosalji(ctx, p.Token); !errors.Is(err, service.ErrPonovnoPrerano) {
		t.Fatalf("odmah ponovno: %v", err)
	}
	o.pomakni(service.RazmakPonovnogSlanja + time.Second)
	n, err := o.dk.PonovnoPosalji(ctx, p.Token)
	if err != nil || !n.PINPoslan {
		t.Fatalf("ponovno: %+v %v", n, err)
	}
	novi := o.posta.pin(t)
	if novi != stari {
		if _, err := o.dk.ProvjeriKod(ctx, p.Token, stari); !errors.Is(err, service.ErrKrivKod) {
			t.Fatalf("stari PIN nakon novog: %v", err)
		}
	}
	o.pomakni(service.RazmakPonovnogSlanja + time.Second)
	if n, err = o.dk.PonovnoPosalji(ctx, p.Token); err != nil || !n.PINPoslan {
		t.Fatalf("treće slanje: %+v %v", n, err)
	}
	treci := o.posta.pin(t)
	o.pomakni(service.RazmakPonovnogSlanja + time.Second)
	if n, err = o.dk.PonovnoPosalji(ctx, p.Token); err != nil || !errors.Is(n.Razlog, service.ErrSlanjeOgraniceno) || !n.PINPoslan {
		t.Fatalf("četvrto slanje u 15 minuta: %+v %v", n, err)
	}
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, treci); err != nil {
		t.Fatalf("zadnji poslani PIN: %v", err)
	}
}

// Čišćenje briše isteklo, a rezervne kodove ostavlja.
func TestOcistiDrugiKorak(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()
	if _, err := o.dk.ZapocniPrijavu(ctx, o.osoba, "", ""); err != nil {
		t.Fatal(err)
	}
	o.dk.ZapamtiRacunalo(ctx, o.osoba, "", "", "")
	o.dk.IzdajPrivremeniKod(ctx, o.admin, o.osoba.ID, false)
	o.dk.NapraviRezervneKodove(ctx, o.osoba, "lozinka1", false)
	if n, err := o.dk.Ocisti(ctx); err != nil || n != 0 {
		t.Fatalf("prije isteka: %d %v", n, err)
	}
	o.pomakni(service.TrajanjeRacunala + time.Hour)
	if n, err := o.dk.Ocisti(ctx); err != nil || n != 3 {
		t.Fatalf("nakon isteka obrisano %d (%v), očekivano 3", n, err)
	}
	if n, _, _ := o.dk.StanjeRezervnih(ctx, o.osoba.ID); n != 10 {
		t.Fatalf("rezervnih ostalo %d", n)
	}
}

// Osoba sama mijenja adresu e-pošte samo uz trenutnu lozinku, iz lokalne
// mreže, na službenu domenu i ne dok mora promijeniti lozinku; stara adresa
// dobije obavijest. Administrator tuđu adresu mijenja kao i prije.
func TestVlastitaPromjenaAdrese(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ja := &models.UserPermissions{User: *o.osoba}
	zahtjev := func(email, lozinka string, izvana bool) service.UpdateUserRequest {
		x := o.osoba
		return service.UpdateUserRequest{ID: x.ID, Username: x.Username, FullName: x.FullName, OrgType: x.OrgType,
			Phone: "031-111", Email: email, IsActive: true, TrenutnaLozinka: lozinka, Izvana: izvana}
	}
	if _, err := o.users.UpdateUser(ja, zahtjev("pero.novi@voda.hr", "", false)); !errors.Is(err, service.ErrKrivaLozinka) {
		t.Fatalf("bez lozinke: %v", err)
	}
	if _, err := o.users.UpdateUser(ja, zahtjev("pero.novi@voda.hr", "kriva", false)); !errors.Is(err, service.ErrKrivaLozinka) {
		t.Fatalf("kriva lozinka: %v", err)
	}
	if _, err := o.users.UpdateUser(ja, zahtjev("pero.novi@voda.hr", "lozinka1", true)); !errors.Is(err, service.ErrPromjenaAdreseIzvana) {
		t.Fatalf("izvana: %v", err)
	}
	if _, err := o.users.UpdateUser(ja, zahtjev("pero@gmail.com", "lozinka1", false)); !errors.Is(err, service.ErrAdresaNijeDopustena) {
		t.Fatalf("tuđa domena: %v", err)
	}
	// ista adresa (i drukčija velika slova) ne traži ništa
	if _, err := o.users.UpdateUser(ja, zahtjev("Pero.Peric@voda.hr", "", true)); err != nil {
		t.Fatalf("ista adresa: %v", err)
	}
	if _, err := o.users.UpdateUser(ja, zahtjev("pero.novi@voda.hr", "lozinka1", false)); err != nil {
		t.Fatal(err)
	}
	if u, _ := o.repo.GetUserByID(o.osoba.ID); u.Email != "pero.novi@voda.hr" {
		t.Fatalf("adresa %q", u.Email)
	}
	rok := time.Now().Add(5 * time.Second)
	for {
		o.posta.mu.Lock()
		var m *posta.Poruka
		for i := range o.posta.poruke {
			if o.posta.poruke[i].Za.Address == "Pero.Peric@voda.hr" || o.posta.poruke[i].Za.Address == "pero.peric@voda.hr" {
				m = &o.posta.poruke[i]
			}
		}
		o.posta.mu.Unlock()
		if m != nil {
			if !strings.Contains(m.Tekst, "pero.novi@voda.hr") || !m.BezKopije {
				t.Fatalf("obavijest: %+v", m)
			}
			break
		}
		if time.Now().After(rok) {
			t.Fatal("stara adresa nije dobila obavijest")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// administrator mijenja tuđu adresu bez lozinke, kao i prije
	x, _ := o.repo.GetUserByID(o.osoba.ID)
	if _, err := o.users.UpdateUser(o.admin, service.UpdateUserRequest{ID: x.ID, Username: x.Username, FullName: x.FullName,
		OrgType: x.OrgType, Email: "pero@udruga.hr", IsActive: true}); err != nil {
		t.Fatalf("administrator: %v", err)
	}

	// dok mora promijeniti lozinku, adresa se ne mijenja
	if _, _, err := o.users.ResetPassword(o.admin, o.osoba.ID); err != nil {
		t.Fatal(err)
	}
	x, _ = o.repo.GetUserByID(o.osoba.ID)
	if _, err := o.users.UpdateUser(&models.UserPermissions{User: *x}, service.UpdateUserRequest{ID: x.ID, Username: x.Username,
		FullName: x.FullName, OrgType: x.OrgType, Email: "pero.treci@voda.hr", IsActive: true}); !errors.Is(err, service.ErrAdresaPrijeLozinke) {
		t.Fatalf("prisilna promjena lozinke: %v", err)
	}
}
