package service_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// cvor je jedna cjelovita instalacija: vlastita baza, vlastita knjiga verzija
// i vlastiti pogled na iste djelatnike. Dva čvora u testu stoje na istom
// sjemenu, kao dvije stvarne instalacije prije prve sinkronizacije.
type cvor struct {
	db   *sql.DB
	rec  *ledger.Recorder
	repo *repository.UserRepository
	auth *service.AuthService
	uslu *service.UserService
}

func noviCvor(t *testing.T, nodeID string) *cvor {
	t.Helper()
	if !db.UseRepoImenik() {
		t.Skip("imenik.json nije dostupan — osobni podaci djelatnika stoje izvan repozitorija (data/imenik.json)")
	}
	database, err := db.OpenDB(filepath.Join(t.TempDir(), nodeID+".db"))
	if err != nil {
		t.Fatalf("baza čvora %s: %v", nodeID, err)
	}
	if err := db.InitSchema(database); err != nil {
		t.Fatalf("shema čvora %s: %v", nodeID, err)
	}
	if err := db.SeedInitialData(database); err != nil {
		t.Fatalf("sjeme čvora %s: %v", nodeID, err)
	}
	rec := ledger.New(database, nodeID)
	repo := repository.NewUserRepository(database, rec)
	auth := service.NewAuthService(repo, repository.NewSessionRepository(database))
	return &cvor{db: database, rec: rec, repo: repo, auth: auth,
		uslu: service.NewUserService(repo, auth, service.NewSSEBroker())}
}

// Zastavica is_active znači "smije se prijaviti", pa je svaki uvedeni
// djelatnik aktivan i prije nego što je ikad otvorio program. Stanje računa
// mora to razlikovati, inače popis od pet stotina ljudi ne govori ništa.
func TestUvedeniDjelatnikNijeSePrijavio(t *testing.T) {
	n := noviCvor(t, "cvor-a")

	u, err := n.repo.GetUserByUsername("tkraljevic")
	if err != nil || u == nil {
		t.Fatalf("korisnik nije dohvaćen: %v", err)
	}

	if !u.IsActive {
		t.Error("uvedeni djelatnik mora smjeti prijavu (is_active)")
	}
	if u.LastLoginAt != nil {
		t.Error("prijava nije zabilježena, a vrijeme zadnje prijave postoji")
	}
	if got := u.AccountState(); got != models.AccountPending {
		t.Errorf("stanje računa = %s, očekivano %s", got, models.AccountPending)
	}
	if got := u.AccountState().Label(); got != "Nije se prijavio" {
		t.Errorf("naziv stanja = %q", got)
	}
}

func TestPrijavaPomicRacunUAktivan(t *testing.T) {
	n := noviCvor(t, "cvor-a")

	if _, _, err := n.auth.Login("tkraljevic", "gocop2026", "127.0.0.1", "test"); err != nil {
		t.Fatalf("prijava nije uspjela: %v", err)
	}

	u, _ := n.repo.GetUserByUsername("tkraljevic")
	if u.LastLoginAt == nil {
		t.Fatal("prijava nije zabilježena")
	}
	if got := u.AccountState(); got != models.AccountActive {
		t.Errorf("stanje računa nakon prijave = %s, očekivano %s", got, models.AccountActive)
	}
}

// Knjiga verzija putuje mrežom, pa prijava ne smije pisati verziju svaki put.
func TestVisePrijavaIstiDanPiseJednuVerziju(t *testing.T) {
	n := noviCvor(t, "cvor-a")

	u, _ := n.repo.GetUserByUsername("tkraljevic")
	before := versionCount(t, n.rec, u.ID.String())

	for i := 0; i < 3; i++ {
		if _, _, err := n.auth.Login("tkraljevic", "gocop2026", "127.0.0.1", "test"); err != nil {
			t.Fatalf("prijava %d nije uspjela: %v", i+1, err)
		}
	}

	after := versionCount(t, n.rec, u.ID.String())
	if after != before+1 {
		t.Errorf("tri prijave u istom danu zapisale su %d verzija, očekivana jedna", after-before)
	}

	// Prijava sutradan je nova činjenica i mora se zabilježiti
	if err := n.repo.MarkLogin(u.ID, time.Now().UTC().Add(24*time.Hour)); err != nil {
		t.Fatalf("bilježenje sutrašnje prijave: %v", err)
	}
	if got := versionCount(t, n.rec, u.ID.String()); got != after+1 {
		t.Errorf("prijava idućeg dana nije zapisana (verzija %d, očekivano %d)", got, after+1)
	}
}

func TestFiltarStanjaRacunaDijeliPopisBezPreklapanja(t *testing.T) {
	n := noviCvor(t, "cvor-a")

	if _, _, err := n.auth.Login("tkraljevic", "gocop2026", "127.0.0.1", "test"); err != nil {
		t.Fatalf("prijava: %v", err)
	}
	iskljucen, _ := n.repo.GetUserByUsername("admin")
	iskljucen.IsActive = false
	if err := n.repo.UpdateUser(iskljucen); err != nil {
		t.Fatalf("isključivanje računa: %v", err)
	}

	svi, _ := n.uslu.ListUsers("", 0, "", "", "")
	aktivni, _ := n.uslu.ListUsers("", 0, "", "", string(models.AccountActive))
	neprijavljeni, _ := n.uslu.ListUsers("", 0, "", "", string(models.AccountPending))
	neaktivni, _ := n.uslu.ListUsers("", 0, "", "", string(models.AccountDisabled))

	if len(aktivni)+len(neprijavljeni)+len(neaktivni) != len(svi) {
		t.Errorf("tri stanja daju %d+%d+%d, a svih djelatnika je %d",
			len(aktivni), len(neprijavljeni), len(neaktivni), len(svi))
	}
	if len(aktivni) != 1 || aktivni[0].Username != "tkraljevic" {
		t.Errorf("aktivan mora biti samo prijavljeni korisnik, dobiveno %d", len(aktivni))
	}
	if len(neaktivni) != 1 || neaktivni[0].Username != "admin" {
		t.Errorf("neaktivan mora biti samo isključeni račun, dobiveno %d", len(neaktivni))
	}
	for _, u := range neprijavljeni {
		if u.AccountState() != models.AccountPending {
			t.Errorf("%s je u popisu nepreuzetih, a stanje mu je %s", u.Username, u.AccountState())
		}
	}
}

// Svaki je čvor puna kopija: osoba koja lozinku promijeni u uredu mora se
// istom lozinkom prijaviti na terenskom laptopu. Sažetak zato putuje u
// verziji — bez toga sinkronizacija tiho briše lozinku na drugom čvoru.
func TestLozinkaPutujeNaDrugiCvor(t *testing.T) {
	a := noviCvor(t, "cvor-a")
	b := noviCvor(t, "cvor-b")

	u, _ := a.repo.GetUserByUsername("tkraljevic")
	hash, err := a.auth.HashPassword("nova-lozinka-2026")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.repo.ChangePassword(u.ID, hash); err != nil {
		t.Fatalf("promjena lozinke: %v", err)
	}

	// razmjena: verzije korisnika ulaze u knjigu drugog čvora, pa se s nje
	// prepisuju na površinu — isti put kojim ide i prava sinkronizacija
	ctx := context.Background()
	verzije, err := a.rec.History(ctx, repository.EntityUsers, u.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.rec.Apply(ctx, verzije); err != nil {
		t.Fatalf("upis verzija u knjigu: %v", err)
	}
	if err := repository.ApplyVersions(ctx, b.db, b.rec, verzije); err != nil {
		t.Fatalf("primjena verzija: %v", err)
	}

	naB, err := b.repo.GetUserByUsername("tkraljevic")
	if err != nil || naB == nil {
		t.Fatalf("korisnik na drugom čvoru: %v", err)
	}
	if naB.PasswordHash == "" {
		t.Fatal("sinkronizacija je obrisala lozinku na drugom čvoru")
	}
	if !b.auth.CheckPassword(naB.PasswordHash, "nova-lozinka-2026") {
		t.Error("nova lozinka ne vrijedi na drugom čvoru")
	}
	if naB.MustChangePassword {
		t.Error("preuzimanje računa nije stiglo na drugi čvor")
	}
}

func versionCount(t *testing.T, rec *ledger.Recorder, userID string) int {
	t.Helper()
	h, err := rec.History(context.Background(), repository.EntityUsers, userID)
	if err != nil {
		t.Fatalf("povijest zapisa: %v", err)
	}
	return len(h)
}
