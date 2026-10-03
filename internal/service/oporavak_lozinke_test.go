package service_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
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

// cvorKonzole je čvor u čije ime poništenje s konzole piše knjigu
const cvorKonzole = "cvor-konzola"

// okolinaOporavka: baza čvora s globalnim administratorom tkraljevic
// (lozinka "stara-lozinka"), drugim administratorom koji izdaje kodove i
// drugim korakom prijave s uključenim PIN-om. Poništenje s konzole ide kroz
// PonistiLozinkuNaCvoru, kao iz naredbenog retka; ovdašnji servisi samo
// pripremaju i provjeravaju stanje.
type okolinaOporavka struct {
	db    *sql.DB
	rec   *ledger.Recorder
	repo  *repository.UserRepository
	auth  *service.AuthService
	dk    *service.DrugiKorak
	posta *laznaPosta
	admin *models.UserPermissions // drugi globalni administrator
	sef   *models.User            // jedini aktivni globalni administrator mreže
	sad   time.Time
}

func (o *okolinaOporavka) racun(t *testing.T, ime, lozinka, email string, globalni, aktivan bool) *models.User {
	t.Helper()
	hash, err := o.auth.HashPassword(lozinka)
	if err != nil {
		t.Fatal(err)
	}
	u := &models.User{Username: ime, PasswordHash: hash, FullName: ime, Email: email,
		IsGlobalAdmin: globalni, IsActive: aktivan, OrgType: models.OrgHrvatskeVode}
	if err := o.repo.CreateUser(u, nil); err != nil {
		t.Fatal(err)
	}
	svjez, err := o.repo.GetUserByID(u.ID)
	if err != nil || svjez == nil {
		t.Fatalf("račun %s: %v", ime, err)
	}
	return svjez
}

func novaOkolinaOporavka(t *testing.T) *okolinaOporavka {
	t.Helper()
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	o := &okolinaOporavka{db: database, rec: ledger.New(database, "cvor-web"), posta: &laznaPosta{},
		sad: time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)}
	o.repo = repository.NewUserRepository(database, o.rec)
	o.auth = service.NewAuthService(o.repo, repository.NewSessionRepository(database))
	o.dk = service.NewDrugiKorak(repository.NewDrugiKorakRepository(database), repository.NewRacuniSustavaRepository(database), o.repo, repository.NewAktiRepository(database, o.rec))
	o.dk.SetKljuc([]byte("sjeme-cvora-za-test-32-bajta-xxx"))
	o.dk.SetPostar(o.posta)
	o.dk.SetSat(func() time.Time { return o.sad })
	o.dk.SetPosta(func(context.Context) posta.Postavke {
		return posta.Postavke{Nacin: posta.NacinEWS, Posluzitelj: "owa.primjer.hr"}
	})
	o.auth.SetZastitaPrijave(o.dk)

	uprava := o.racun(t, "uprava", "lozinka1", "uprava@voda.hr", true, true)
	o.admin = &models.UserPermissions{IsGlobalAdmin: true, User: *uprava}
	o.sef = o.racun(t, "tkraljevic", "stara-lozinka", "glavni.admin@voda.hr", true, true)

	ctx := context.Background()
	if _, err := o.dk.SpremiPosiljatelja(ctx, o.admin, "VODA\\pin", "tajna", "pin@voda.hr"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); err != nil {
		t.Fatal(err)
	}
	if err := o.dk.PostaviUkljuceno(ctx, o.admin, true, true); err != nil {
		t.Fatal(err)
	}
	return o
}

// verzijeRacuna su verzije računa u knjizi, od najnovije
func (o *okolinaOporavka) verzijeRacuna(t *testing.T, id uuid.UUID) []ledger.Version {
	t.Helper()
	v, err := o.rec.History(context.Background(), repository.EntityUsers, id.String())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (o *okolinaOporavka) brojVerzija(t *testing.T) int {
	t.Helper()
	var n int
	if err := o.db.QueryRow(`SELECT COUNT(*) FROM record_versions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Lozinka s konzole prepisuje se s ekrana: četiri skupine po četiri znaka,
// bez znakova koji se pri prepisivanju zamijene, i svaki put drukčija.
func TestLozinkaZaKonzolu(t *testing.T) {
	oblik := regexp.MustCompile(`^[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}$`)
	vidjene := map[string]bool{}
	for i := 0; i < 200; i++ {
		l, err := service.GenerirajLozinkuZaKonzolu()
		if err != nil {
			t.Fatal(err)
		}
		if !oblik.MatchString(l) {
			t.Fatalf("lozinka %q nije oblika xxxx-xxxx-xxxx-xxxx", l)
		}
		if strings.ContainsAny(l, "01ilo") {
			t.Fatalf("lozinka %q nosi znak koji se zamijeni pri prepisivanju", l)
		}
		vidjene[l] = true
	}
	if len(vidjene) != 200 {
		t.Errorf("od 200 lozinki samo %d različitih — izvor slučajnosti je preslab", len(vidjene))
	}
}

// Jedini globalni administrator zaboravio je lozinku i izgubio kodove:
// poništenje s konzole daje mu privremenu lozinku i čini sve što i
// Korisnici → Poništi lozinku, a račun ostaje aktivan globalni administrator.
func TestPonistenjeSKonzole(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaOporavka(t)

	// stanje koje poništenje mora opozvati
	token, err := o.dk.ZapamtiRacunalo(ctx, o.sef, "", "203.0.113.9", "Firefox")
	if err != nil {
		t.Fatal(err)
	}
	p, err := o.dk.ZapocniPrijavu(ctx, o.sef, "", "")
	if err != nil {
		t.Fatal(err)
	}
	pin := o.posta.pin(t)
	kod, _, err := o.dk.IzdajPrivremeniKod(ctx, o.admin, o.sef.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	rezervni, err := o.dk.NapraviRezervneKodove(ctx, o.sef, "stara-lozinka", false)
	if err != nil {
		t.Fatal(err)
	}
	sesija, err := o.auth.OtvoriSesiju(o.sef, "", "")
	if err != nil {
		t.Fatal(err)
	}
	tudja, err := o.auth.OtvoriSesiju(&o.admin.User, "", "")
	if err != nil {
		t.Fatal(err)
	}
	prije := len(o.verzijeRacuna(t, o.sef.ID))

	ishod, err := service.PonistiLozinkuNaCvoru(o.db, cvorKonzole, "  TKraljevic ", false)
	if err != nil {
		t.Fatal(err)
	}
	if ishod.Lozinka == "" || ishod.Korisnik == nil || ishod.Korisnik.ID != o.sef.ID {
		t.Fatalf("ishod: %+v", ishod)
	}
	if ishod.Aktiviran || !ishod.Opozvano || ishod.KljucUklonjen {
		t.Errorf("ishod za aktivan račun bez potpisnog ključa: %+v", ishod)
	}

	svjez, err := o.repo.GetUserByID(o.sef.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !o.auth.CheckPassword(svjez.PasswordHash, ishod.Lozinka) {
		t.Fatal("privremena lozinka ne prolazi provjeru")
	}
	if !svjez.MustChangePassword || !ishod.Korisnik.MustChangePassword {
		t.Error("račun nije zaključan na promjenu lozinke")
	}
	if !svjez.IsActive || !svjez.IsGlobalAdmin {
		t.Errorf("poništenje je diralo račun: aktivan=%v globalni=%v", svjez.IsActive, svjez.IsGlobalAdmin)
	}
	if _, err := o.auth.ProvjeriPrijavu("tkraljevic", ishod.Lozinka); err != nil {
		t.Errorf("prijava privremenom lozinkom: %v", err)
	}
	if _, err := o.auth.ProvjeriPrijavu("tkraljevic", "stara-lozinka"); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Errorf("stara lozinka još vrijedi: %v", err)
	}

	// prijave i drugi korak
	if _, _, err := o.auth.AuthenticateSession(sesija.ID); err == nil {
		t.Error("otvorena prijava ostala nakon poništenja")
	}
	if _, _, err := o.auth.AuthenticateSession(tudja.ID); err != nil {
		t.Errorf("ugašena je i tuđa prijava: %v", err)
	}
	if r, _ := o.dk.Racunala(ctx, o.sef.ID, token); len(r) != 0 {
		t.Errorf("zapamćeno računalo ostalo: %+v", r)
	}
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, pin); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Errorf("prijava na čekanju ostala: %v", err)
	}
	o.sad = o.sad.Add(6 * time.Minute)
	p, err = o.dk.ZapocniPrijavu(ctx, svjez, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, kod); !errors.Is(err, service.ErrKrivKod) {
		t.Errorf("privremeni kod ostao: %v", err)
	}
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, rezervni[0]); err != nil {
		t.Errorf("rezervni kodovi moraju ostati: %v", err)
	}

	// knjiga: nova verzija u ime ovog čvora, sa sažetkom za druge čvorove
	verzije := o.verzijeRacuna(t, o.sef.ID)
	if len(verzije) != prije+1 {
		t.Fatalf("u knjizi %d verzija računa, očekivano %d", len(verzije), prije+1)
	}
	zadnja := verzije[0]
	if zadnja.NodeID != cvorKonzole {
		t.Errorf("verzija je u ime čvora %q, očekivano %q", zadnja.NodeID, cvorKonzole)
	}
	var tijelo struct {
		PasswordHash string `json:"password_hash"`
		MustChange   bool   `json:"must_change_password"`
		IsActive     bool   `json:"is_active"`
		IsGlobal     bool   `json:"is_global_admin"`
	}
	if err := json.Unmarshal(zadnja.Payload, &tijelo); err != nil {
		t.Fatal(err)
	}
	if tijelo.PasswordHash != svjez.PasswordHash || !tijelo.MustChange || !tijelo.IsActive || !tijelo.IsGlobal {
		t.Errorf("verzija ne nosi poništenje: %+v", tijelo)
	}
}

// Nepostojeće ime je greška i ništa se ne mijenja: ni knjiga, ni prijave,
// ni lozinka nekog drugog računa.
func TestPonistenjeSKonzoleNepostojeceIme(t *testing.T) {
	o := novaOkolinaOporavka(t)
	sesija, err := o.auth.OtvoriSesiju(o.sef, "", "")
	if err != nil {
		t.Fatal(err)
	}
	prije := o.brojVerzija(t)
	for _, ime := range []string{"nepostojeci", "tkraljevic2", "", "   "} {
		ishod, err := service.PonistiLozinkuNaCvoru(o.db, cvorKonzole, ime, true)
		if err == nil || ishod != nil {
			t.Fatalf("%q: ishod %+v, greška %v", ime, ishod, err)
		}
		if strings.TrimSpace(ime) != "" && !errors.Is(err, service.ErrUserNotFound) {
			t.Errorf("%q: greška %v nije ErrUserNotFound", ime, err)
		}
	}
	if n := o.brojVerzija(t); n != prije {
		t.Errorf("knjiga se promijenila: %d → %d verzija", prije, n)
	}
	if _, _, err := o.auth.AuthenticateSession(sesija.ID); err != nil {
		t.Errorf("prijava je ugašena: %v", err)
	}
	if _, err := o.auth.ProvjeriPrijavu("tkraljevic", "stara-lozinka"); err != nil {
		t.Errorf("lozinka se promijenila: %v", err)
	}
}

// Isključen račun koji nije globalni administrator dobije lozinku, ali ostaje
// isključen i bez prava; tek -aktiviraj ga uključi, a globalnim ga ne čini.
func TestPonistenjeSKonzoleIskljucenRacun(t *testing.T) {
	o := novaOkolinaOporavka(t)
	ana := o.racun(t, "ana", "lozinka-ane", "ana@voda.hr", false, false)

	ishod, err := service.PonistiLozinkuNaCvoru(o.db, cvorKonzole, "ana", false)
	if err != nil {
		t.Fatal(err)
	}
	if ishod.Aktiviran || ishod.Korisnik.IsActive || ishod.Korisnik.IsGlobalAdmin {
		t.Fatalf("bez -aktiviraj račun se promijenio: %+v", ishod)
	}
	if _, err := o.auth.ProvjeriPrijavu("ana", ishod.Lozinka); !errors.Is(err, service.ErrAccountInactive) {
		t.Errorf("isključen račun: %v", err)
	}

	prije := len(o.verzijeRacuna(t, ana.ID))
	ishod, err = service.PonistiLozinkuNaCvoru(o.db, cvorKonzole, "ana", true)
	if err != nil {
		t.Fatal(err)
	}
	if !ishod.Aktiviran || !ishod.Korisnik.IsActive || ishod.Korisnik.IsGlobalAdmin || !ishod.Korisnik.MustChangePassword {
		t.Fatalf("uz -aktiviraj: %+v", ishod.Korisnik)
	}
	if _, err := o.auth.ProvjeriPrijavu("ana", ishod.Lozinka); err != nil {
		t.Errorf("uključen račun se ne prijavljuje: %v", err)
	}
	// i uključenje i lozinka ostavljaju verziju, obje u ime ovog čvora
	verzije := o.verzijeRacuna(t, ana.ID)
	if len(verzije) != prije+2 {
		t.Fatalf("u knjizi %d verzija, očekivano %d", len(verzije), prije+2)
	}
	for _, v := range verzije[:2] {
		if v.NodeID != cvorKonzole {
			t.Errorf("verzija %s u ime čvora %q", v.VersionID, v.NodeID)
		}
	}

	// već aktivan račun -aktiviraj ne dira
	ishod, err = service.PonistiLozinkuNaCvoru(o.db, cvorKonzole, "tkraljevic", true)
	if err != nil || ishod.Aktiviran {
		t.Fatalf("aktivan račun uz -aktiviraj: %+v %v", ishod, err)
	}
}

// Naredba ne stvara shemu: baza bez tablica čvora i čvor bez identifikatora
// su greška, ne prazan rad.
func TestPonistenjeSKonzoleBezBaze(t *testing.T) {
	prazna, err := db.OpenDB(filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer prazna.Close()
	if _, err := service.PonistiLozinkuNaCvoru(prazna, cvorKonzole, "admin", false); err == nil || !strings.Contains(err.Error(), "nema tablice") {
		t.Errorf("prazna baza: %v", err)
	}
	var n int
	if err := prazna.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("naredba je stvorila tablice: %d %v", n, err)
	}

	o := novaOkolinaOporavka(t)
	if _, err := service.PonistiLozinkuNaCvoru(o.db, " ", "tkraljevic", false); err == nil {
		t.Error("čvor bez identifikatora prošao")
	}
	if _, err := o.auth.ProvjeriPrijavu("tkraljevic", "stara-lozinka"); err != nil {
		t.Errorf("lozinka se promijenila: %v", err)
	}
}

// Uz -aktiviraj račun se uključuje tek kad su lozinka poništena i prijave
// opozvane. Ne uspije li poništenje, isključen račun ostaje isključen sa
// starom lozinkom; ne uspije li uključenje, ostaje isključen s novom.
func TestPonistenjeSKonzoleUkljucujeTekNakonLozinke(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaOporavka(t)
	ana := o.racun(t, "ana", "lozinka-ane", "ana@voda.hr", false, true)
	sesija, err := o.auth.OtvoriSesiju(ana, "", "")
	if err != nil {
		t.Fatal(err)
	}
	token, err := o.dk.ZapamtiRacunalo(ctx, ana, "", "203.0.113.9", "Firefox")
	if err != nil {
		t.Fatal(err)
	}
	// osoba je otišla: račun se isključi, a prijava i računalo ostaju zapisani
	iskljucen := *ana
	iskljucen.IsActive, iskljucen.PasswordHash = false, ""
	if err := o.repo.UpdateUser(&iskljucen); err != nil {
		t.Fatal(err)
	}

	// poništenje lozinke ne uspije (pun disk, baza predugo zauzeta)
	if _, err := o.db.Exec(`CREATE TRIGGER proba_lozinka BEFORE UPDATE OF password_hash ON users
		BEGIN SELECT RAISE(ABORT, 'disk je pun'); END`); err != nil {
		t.Fatal(err)
	}
	prije := len(o.verzijeRacuna(t, ana.ID))
	ishod, err := service.PonistiLozinkuNaCvoru(o.db, cvorKonzole, "ana", true)
	if err == nil {
		t.Fatalf("poništenje je prošlo unatoč grešci baze: %+v", ishod)
	}
	if ishod != nil {
		t.Errorf("lozinka nije upisana, a ishod je vraćen: %+v", ishod)
	}
	svjez, err := o.repo.GetUserByID(ana.ID)
	if err != nil {
		t.Fatal(err)
	}
	if svjez.IsActive {
		t.Error("račun je uključen iako lozinka nije poništena")
	}
	if _, err := o.auth.ProvjeriPrijavu("ana", "lozinka-ane"); !errors.Is(err, service.ErrAccountInactive) {
		t.Errorf("stara lozinka isključenog računa: %v", err)
	}
	if _, _, err := o.auth.AuthenticateSession(sesija.ID); err == nil {
		t.Error("stara prijava isključenog računa opet vrijedi")
	}
	if n := len(o.verzijeRacuna(t, ana.ID)); n != prije {
		t.Errorf("u knjizi %d verzija računa, očekivano %d", n, prije)
	}

	// lozinka prolazi, a uključenje ne: račun ostaje isključen s novom lozinkom
	if _, err := o.db.Exec(`DROP TRIGGER proba_lozinka`); err != nil {
		t.Fatal(err)
	}
	if _, err := o.db.Exec(`CREATE TRIGGER proba_ukljucenje BEFORE UPDATE OF is_active ON users
		WHEN NEW.is_active = 1 BEGIN SELECT RAISE(ABORT, 'disk je pun'); END`); err != nil {
		t.Fatal(err)
	}
	ishod, err = service.PonistiLozinkuNaCvoru(o.db, cvorKonzole, "ana", true)
	if err == nil || ishod == nil {
		t.Fatalf("neuspjelo uključenje: ishod %+v, greška %v", ishod, err)
	}
	if !strings.Contains(err.Error(), "-aktiviraj") {
		t.Errorf("greška ne kaže kako ponoviti: %v", err)
	}
	if ishod.Aktiviran || ishod.Korisnik.IsActive || !ishod.Opozvano {
		t.Errorf("ishod: %+v", ishod)
	}
	svjez, err = o.repo.GetUserByID(ana.ID)
	if err != nil {
		t.Fatal(err)
	}
	if svjez.IsActive || !svjez.MustChangePassword || !o.auth.CheckPassword(svjez.PasswordHash, ishod.Lozinka) {
		t.Errorf("očekivan isključen račun s novom privremenom lozinkom: %+v", svjez)
	}
	if _, err := o.auth.ProvjeriPrijavu("ana", "lozinka-ane"); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Errorf("stara lozinka još vrijedi: %v", err)
	}
	if _, _, err := o.auth.AuthenticateSession(sesija.ID); !errors.Is(err, service.ErrSessionExpired) {
		t.Errorf("stara prijava nije ugašena: %v", err)
	}
	if r, _ := o.dk.Racunala(ctx, ana.ID, token); len(r) != 0 {
		t.Errorf("zapamćeno računalo ostalo: %+v", r)
	}

	// ponovljeno uz -aktiviraj kad uključenje prolazi
	if _, err := o.db.Exec(`DROP TRIGGER proba_ukljucenje`); err != nil {
		t.Fatal(err)
	}
	ishod, err = service.PonistiLozinkuNaCvoru(o.db, cvorKonzole, "ana", true)
	if err != nil || !ishod.Aktiviran || !ishod.Korisnik.IsActive || !ishod.Korisnik.MustChangePassword {
		t.Fatalf("ponovljeno uz -aktiviraj: %+v %v", ishod, err)
	}
	if _, err := o.auth.ProvjeriPrijavu("ana", ishod.Lozinka); err != nil {
		t.Errorf("uključen račun se ne prijavljuje: %v", err)
	}
}

// Korisničko ime je jedinstveno uz razlikovanje slova, pa mogu postojati
// tkraljevic i TKraljevic. Naredba tada uzima račun točno tog imena, a ime
// koje odgovara samo bez obzira na slova je greška s popisom i ništa ne mijenja.
func TestPonistenjeSKonzoleImenaRazlicitaSamoUSlovima(t *testing.T) {
	o := novaOkolinaOporavka(t)
	drugi := o.racun(t, "TKraljevic", "druga-lozinka", "drugi@voda.hr", false, true)
	lozinkaVrijedi := func(id uuid.UUID, lozinka string) bool {
		t.Helper()
		u, err := o.repo.GetUserByID(id)
		if err != nil || u == nil {
			t.Fatalf("račun %s: %v", id, err)
		}
		return o.auth.CheckPassword(u.PasswordHash, lozinka)
	}

	prije := o.brojVerzija(t)
	ishod, err := service.PonistiLozinkuNaCvoru(o.db, cvorKonzole, "TKRALJEVIC", false)
	if err == nil || ishod != nil {
		t.Fatalf("dvoznačno ime: ishod %+v, greška %v", ishod, err)
	}
	for _, ime := range []string{`"tkraljevic"`, `"TKraljevic"`} {
		if !strings.Contains(err.Error(), ime) {
			t.Errorf("greška ne navodi račun %s: %v", ime, err)
		}
	}
	if n := o.brojVerzija(t); n != prije {
		t.Errorf("knjiga se promijenila: %d → %d verzija", prije, n)
	}
	if !lozinkaVrijedi(o.sef.ID, "stara-lozinka") || !lozinkaVrijedi(drugi.ID, "druga-lozinka") {
		t.Error("dvoznačno ime promijenilo je lozinku")
	}

	ishod, err = service.PonistiLozinkuNaCvoru(o.db, cvorKonzole, "TKraljevic", false)
	if err != nil {
		t.Fatal(err)
	}
	if ishod.Korisnik.ID != drugi.ID {
		t.Fatalf("poništen je račun %s, a traži se TKraljevic", ishod.Korisnik.Username)
	}
	if !lozinkaVrijedi(drugi.ID, ishod.Lozinka) || !lozinkaVrijedi(o.sef.ID, "stara-lozinka") {
		t.Error("lozinka nije poništena računu točnog imena")
	}

	ishod, err = service.PonistiLozinkuNaCvoru(o.db, cvorKonzole, " tkraljevic ", false)
	if err != nil {
		t.Fatal(err)
	}
	if ishod.Korisnik.ID != o.sef.ID || !lozinkaVrijedi(o.sef.ID, ishod.Lozinka) {
		t.Fatalf("poništen je račun %s, a traži se tkraljevic", ishod.Korisnik.Username)
	}
}

// Osobni potpisni ključ zaključan je lozinkom računa. Poništenje s konzole
// ga uklanja (kroz knjigu, pa i na drugim čvorovima), inače prisilna
// promjena lozinke privremenom lozinkom zapne na prekljucavanju ključa i
// osoba ostane zaključana izvan programa.
func TestPonistenjeSKonzoleUklanjaPotpisniKljuc(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaOporavka(t)
	_, kljucCvora, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ps := service.NewPotpisService(repository.NewPotpisRepository(o.db, o.rec),
		service.NewUserService(o.repo, o.auth, service.NewSSEBroker()), "cvor-web", kljucCvora, o.auth.CheckPassword)
	if err := ps.Pokreni(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Novi(ctx, o.sef, "stara-lozinka"); err != nil {
		t.Fatal(err)
	}

	ishod, err := service.PonistiLozinkuNaCvoru(o.db, cvorKonzole, "tkraljevic", false)
	if err != nil {
		t.Fatal(err)
	}
	if ps.Ima(ctx, o.sef.ID.String()) || !ishod.KljucUklonjen {
		t.Errorf("potpisni ključ zaključan starom lozinkom ostao je (javljeno uklonjen=%v)", ishod.KljucUklonjen)
	}
	v, err := o.rec.History(ctx, repository.EntityPotpisniKljucevi, o.sef.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(v) == 0 || !v[0].Archived || v[0].NodeID != cvorKonzole {
		t.Errorf("uklanjanje ključa nije verzija u knjizi u ime ovog čvora (%d verzija)", len(v))
	}

	// prisilna promjena lozinke, kao na /profile?force=1: privremena lozinka
	// je trenutna, ključ se prekljucava, pa se mijenja lozinka
	const nova = "nova-lozinka-2026"
	if err := service.ProvjeriNovuLozinku(ishod.Lozinka, nova); err != nil {
		t.Fatal(err)
	}
	if err := ps.Prekljucaj(ctx, o.sef.ID.String(), ishod.Lozinka, nova); err != nil {
		t.Fatalf("promjena lozinke zapinje na potpisnom ključu: %v", err)
	}
	if err := o.auth.ChangePassword(o.sef.ID, ishod.Lozinka, nova, uuid.Nil); err != nil {
		t.Fatal(err)
	}
	svjez, err := o.repo.GetUserByID(o.sef.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Novi(ctx, svjez, nova); err != nil {
		t.Errorf("novi ključ novom lozinkom: %v", err)
	}
}
