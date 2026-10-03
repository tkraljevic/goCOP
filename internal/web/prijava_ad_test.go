package web

import (
	"crypto/ed25519"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Prijave na poslužitelj e-pošte broje se po računu u domeni, zajedno za
// lozinku osobnog sandučića i za račun koji šalje PIN: tri u pola sata, ime
// bez domene troši dvije (goCOP pokuša i DOMENA\ime). Uspjela prijava briše
// brojač.
func TestPrijaveRacunaUDomeniZajednickeObrascima(t *testing.T) {
	o := novaOkolinaIzvana(t)
	admin := o.racun("voditelj", "voditeljeva-lozinka", "voditelj@voda.hr", true)
	ja := zahtjevIzvana{stvarni: admin, izvana: true}

	// osobni sandučić: pravi AktService s probnim Exchangeom
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "akti.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	// račun mora postojati i u ovoj bazi: uz lozinku sandučića sprema se
	// otisak lozinke računa
	kopija := *admin
	kopija.Duties = nil
	if err := userRepo.CreateUser(&kopija, nil); err != nil {
		t.Fatal(err)
	}
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	sectionRepo := repository.NewSectionRepository(baza, rec)
	stationRepo := repository.NewStationRepository(baza, rec)
	readingRepo := repository.NewReadingRepository(baza, rec)
	episodes := service.NewEpisodeService(repository.NewEpisodeRepository(baza, rec), readingRepo, stationRepo)
	akti := service.NewAktService(repository.NewAktiRepository(baza, rec), stationRepo, sectionRepo, repository.NewTerritoryRepository(baza, rec), readingRepo, users, episodes, "test")
	_, kljuc, _ := ed25519.GenerateKey(nil)
	akti.SetKljuc(kljuc)
	srv, err := posta.PokreniProbniEWS(`voda.int\voditelj`, "Lozinka-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Zatvori)
	pp := srv.Postavke()
	pp.Domena = "voda.int"
	akti.SetPosta(pp)
	h := NewAktiHandler(func() *service.AktService { return akti }, users, nil, nil, nil, nil, nil)
	o.mux.HandleFunc("POST /profile/posta", h.HandlePosta)

	pin := func(korisnik, lozinka string) string {
		t.Helper()
		w := o.posalji(ja, http.MethodPost, "/administracija/posta/pin", url.Values{"radnja": {"spremi"}, "korisnik": {korisnik}, "lozinka": {lozinka}, "adresa": {"voditelj@voda.hr"}})
		return mustUnescape(w.Header().Get("Location"))
	}
	sanducic := func(korisnik, lozinka string) string {
		t.Helper()
		w := o.posalji(ja, http.MethodPost, "/profile/posta", url.Values{"korisnik": {korisnik}, "lozinka": {lozinka}})
		return mustUnescape(w.Header().Get("Location"))
	}
	const previse = "Previše prijava na poslužitelj e-pošte računom voditelj"

	// 1. račun za PIN s domenom, kriva lozinka: jedna prijava
	o.postar.greska = posta.ErrPrijava
	if loc := pin(`VODA\voditelj`, "kriva-1"); !strings.Contains(loc, "nije prošla") || o.postar.prijava != 1 {
		t.Fatalf("PIN, kriva lozinka: %s (prijava %d)", loc, o.postar.prijava)
	}
	// 2. osobni sandučić s domenom, kriva lozinka: druga prijava istog računa
	if loc := sanducic(`voda.int\voditelj`, "kriva-2"); !strings.Contains(loc, "odbio") {
		t.Fatalf("sandučić, kriva lozinka: %s", loc)
	}
	// 3. račun za PIN bez domene troši dvije, a ostala je jedna: ne ide na
	// poslužitelj i kaže kako dalje
	if loc := pin("voditelj@voda.hr", "kriva-3"); !strings.Contains(loc, previse) || !strings.Contains(loc, `VODA\ime`) || o.postar.prijava != 1 {
		t.Fatalf("PIN bez domene preko granice: %s (prijava %d)", loc, o.postar.prijava)
	}
	// 4. zadnja prijava, s domenom i točnom lozinkom, prolazi i briše brojač
	if loc := sanducic(`voda.int\voditelj`, "Lozinka-1"); !strings.Contains(loc, "success") {
		t.Fatalf("sandučić, točna lozinka: %s", loc)
	}
	o.postar.greska = nil
	if loc := pin("voditelj", "Lozinka-1"); strings.Contains(loc, "error=") || o.postar.prijava != 2 {
		t.Fatalf("nakon uspjele prijave brojač je prazan: %s (prijava %d)", loc, o.postar.prijava)
	}

	// Kriva lozinka bez domene dvaput: druga ne ide na poslužitelj (2+2 > 3)
	ponovnaLozinka = newLoginLimiter()
	o.postar.greska = posta.ErrPrijava
	pin("voditelj", "kriva-4")
	if loc := pin("voditelj", "kriva-5"); !strings.Contains(loc, previse) || o.postar.prijava != 3 {
		t.Fatalf("drugi upis bez domene: %s (prijava %d)", loc, o.postar.prijava)
	}
	// ni osobni sandučić bez domene (adresa, voda.int\ime, ime s domenom
	// poslužitelja: tri prijave) više ne prolazi
	if loc := sanducic("", "Lozinka-1"); !strings.Contains(loc, previse) {
		t.Fatalf("sandučić nakon dva kriva upisa bez domene: %s", loc)
	}

	// Prijava koju je poslužitelj primio briše brojač i kad kasniji korak
	// ne prođe (adresa nije u adresaru): ponavljanje ne zatvara obrazac
	ponovnaLozinka = newLoginLimiter()
	o.postar.greska = nil
	for i := range najviseKrivihAD + 1 {
		w := o.posalji(ja, http.MethodPost, "/administracija/posta/pin", url.Values{"radnja": {"spremi"}, "korisnik": {`VODA\voditelj`}, "lozinka": {"Lozinka-1"}})
		if loc := mustUnescape(w.Header().Get("Location")); !strings.Contains(loc, "nije pronađena u adresaru") {
			t.Fatalf("%d. upis bez adrese: %s", i+1, loc)
		}
	}

	// Vlastita točna lozinka između krivih upisa tuđih imena ne briše
	// granicu osobe: najviše tri kriva upisa u 15 minuta, koliko god imena
	ponovnaLozinka = newLoginLimiter()
	sprej := o.racun("sprej", "spreja-lozinka", "sprej@voda.hr", false)
	kopijaSpreja := *sprej
	kopijaSpreja.Duties = nil
	if err := userRepo.CreateUser(&kopijaSpreja, nil); err != nil {
		t.Fatal(err)
	}
	odbijeno, svoja := 0, 0
	for i := range 5 {
		for _, x := range []url.Values{
			{"korisnik": {fmt.Sprintf(`voda.int\zrtva%da`, i)}, "lozinka": {"Ljeto2026!"}},
			{"korisnik": {fmt.Sprintf(`voda.int\zrtva%db`, i)}, "lozinka": {"Ljeto2026!"}},
			{"korisnik": {`voda.int\voditelj`}, "lozinka": {"Lozinka-1"}},
		} {
			w := o.posalji(zahtjevIzvana{stvarni: sprej, izvana: true}, http.MethodPost, "/profile/posta", x)
			switch loc := mustUnescape(w.Header().Get("Location")); {
			case strings.Contains(loc, "odbio"):
				odbijeno++
			case strings.Contains(loc, "success"):
				svoja++
			}
		}
	}
	if svoja == 0 {
		t.Fatal("vlastita točna lozinka nije prošla ni jednom")
	}
	if odbijeno > 3 {
		t.Fatalf("raspršeno pogađanje: poslužitelj je odbio %d krivih lozinki jedne osobe u 15 minuta", odbijeno)
	}

	// Brojač je i po osobi: kolega koji tri puta upiše tuđe ime s krivom
	// lozinkom ne zatvara obrasce vlasniku računa
	ponovnaLozinka = newLoginLimiter()
	kolega := o.racun("kolega", "kolegina-lozinka", "kolega@voda.hr", false)
	for range najviseKrivihAD + 1 {
		o.posalji(zahtjevIzvana{stvarni: kolega, izvana: true}, http.MethodPost, "/profile/posta", url.Values{"korisnik": {`voda.int\voditelj`}, "lozinka": {"kriva"}})
	}
	o.postar.greska = posta.ErrPrijava
	prije := o.postar.prijava
	if loc := pin(`VODA\voditelj`, "kriva-6"); strings.Contains(loc, previse) || o.postar.prijava != prije+1 {
		t.Fatalf("vlasnik nakon kolegina tri kriva upisa: %s (prijava %d)", loc, o.postar.prijava-prije)
	}
}
