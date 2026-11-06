package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

func servisPrijavaGreske(t *testing.T) (*PrijavaGreskeService, *models.User) {
	t.Helper()
	s, u, _ := servisPrijavaGreskeSBazom(t)
	return s, u
}

func servisPrijavaGreskeSBazom(t *testing.T) (*PrijavaGreskeService, *models.User, *sql.DB) {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "prijave.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	s := NewPrijavaGreskeService(repository.NewPrijavaGreskeRepository(baza, ledger.New(baza, "ured")), "ured")
	return s, &models.User{ID: uuid.New(), FullName: "Mara Marić"}, baza
}

// Kvar baze ide pozivatelju kao greška, s bilo kojeg koraka primanja
func TestPosaljiKvarBaze(t *testing.T) {
	s, u, baza := servisPrijavaGreskeSBazom(t)
	ctx := context.Background()
	if err := s.provjeriBroj(ctx, u.ID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`CREATE TRIGGER kvar BEFORE INSERT ON prijave_gresaka BEGIN SELECT RAISE(ABORT, 'kvar'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "x"}); err == nil {
		t.Error("upis")
	}
	baza.Close()
	if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "x"}); err == nil {
		t.Error("čitanje")
	}
	if err := s.provjeriBroj(ctx, u.ID.String()); err == nil {
		t.Error("provjera broja")
	}
	if _, err := s.Odgovori(ctx, u, "id", models.StanjePrijaveURadu, ""); err == nil {
		t.Error("odgovor")
	}
}

func TestPosaljiCistiIBiljezi(t *testing.T) {
	s, u := servisPrijavaGreske(t)
	ctx := context.Background()
	p, err := s.Posalji(ctx, u, UlazPrijaveGreske{
		Vrsta: "bilo-sto", Opis: "  Graf se ne crta.\r\nDrugi redak \x07 ",
		Stranica: "https://cop.primjer.hr/x?token=a", NazivStranice: "Prognoze\n· Drava",
		Uloga: "vodočuvar", Izdanje: "0.0.54-alfa", Sustav: "Windows", Preglednik: "Chrome 142", Zaslon: "1440 × 900",
		Greske: []string{"Greška za /api/x?token=tajno"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Vrsta != models.VrstaPrijaveGreska || p.Stanje != models.StanjePrijaveNova || p.Ime != "Mara Marić" || p.Cvor != "ured" {
		t.Errorf("%+v", p)
	}
	if p.Opis != "Graf se ne crta.\nDrugi redak" || p.Stranica != "" || p.NazivStranice != "Prognoze · Drava" {
		t.Errorf("čišćenje: %q %q %q", p.Opis, p.Stranica, p.NazivStranice)
	}
	if len(p.Greske) != 1 || strings.Contains(p.Greske[0], "tajno") {
		t.Errorf("greške: %q", p.Greske)
	}
	moje, err := s.MojePrijave(ctx, u.ID.String())
	if err != nil || len(moje) != 1 {
		t.Errorf("moje prijave: %d %v", len(moje), err)
	}
}

func TestPosaljiOdbija(t *testing.T) {
	s, u := servisPrijavaGreske(t)
	ctx := context.Background()
	if _, err := s.Posalji(ctx, nil, UlazPrijaveGreske{Opis: "x"}); !errors.Is(err, ErrPrijavaBezKorisnika) {
		t.Errorf("bez korisnika: %v", err)
	}
	if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: " \n "}); !errors.Is(err, ErrPrijavaBezOpisa) {
		t.Errorf("prazno: %v", err)
	}
	if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: strings.Repeat("š", models.NajviseOpisaPrijave+1)}); !errors.Is(err, ErrPrijavaPreduga) {
		t.Errorf("predugačko: %v", err)
	}
	if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: strings.Repeat("š", models.NajviseOpisaPrijave), Vrsta: models.VrstaPrijavePrijedlog}); err != nil {
		t.Errorf("baš na granici: %v", err)
	}
}

// Dvadeset prijava u satu prolazi, dvadeset prva ne, a nakon sata opet prolazi.
// Druga osoba za to vrijeme šalje normalno.
func TestPosaljiOgranicavaBrojPoOsobi(t *testing.T) {
	s, u := servisPrijavaGreske(t)
	ctx := context.Background()
	for i := range najvisePrijavaUSatu {
		if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: fmt.Sprintf("prijava %d", i)}); err != nil {
			t.Fatalf("prijava %d: %v", i, err)
		}
	}
	if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "još jedna"}); !errors.Is(err, ErrPrevisePrijava) {
		t.Errorf("preko granice: %v", err)
	}
	druga := &models.User{ID: uuid.New(), FullName: "Ivo Ivić"}
	if _, err := s.Posalji(ctx, druga, UlazPrijaveGreske{Opis: "x"}); err != nil {
		t.Errorf("druga osoba: %v", err)
	}
	s.sat = func() time.Time { return time.Now().Add(prozorPrijava + time.Minute) }
	if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "x"}); err != nil {
		t.Errorf("nakon sata: %v", err)
	}
}

// Skripta koja šalje istodobno ne prolazi preko granice
func TestPosaljiIstodobnoNePrelaziGranicu(t *testing.T) {
	s, u := servisPrijavaGreske(t)
	ctx := context.Background()
	const slanja = 100
	var wg sync.WaitGroup
	var uspjelo, odbijeno, ostalo atomic.Int32
	for i := range slanja {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: fmt.Sprintf("prijava %d", i)})
			switch {
			case err == nil:
				uspjelo.Add(1)
			case errors.Is(err, ErrPrevisePrijava):
				odbijeno.Add(1)
			default:
				ostalo.Add(1)
				t.Errorf("slanje %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if uspjelo.Load() != najvisePrijavaUSatu || odbijeno.Load() != slanja-najvisePrijavaUSatu {
		t.Errorf("uspjelo %d, odbijeno %d, ostalo %d", uspjelo.Load(), odbijeno.Load(), ostalo.Load())
	}
}

// Kad se veza prekine, osoba ne zna je li prijava stigla pa je šalje ponovno.
// Isti tekst s iste stranice u kratkom roku ne pravi drugu prijavu, ni kad je
// granica već dosegnuta, a nakon roka je to nova prijava.
func TestPosaljiPonovljenoSlanje(t *testing.T) {
	s, u := servisPrijavaGreske(t)
	ctx := context.Background()
	ulaz := UlazPrijaveGreske{Opis: "Graf se ne crta.", Stranica: "/prognoze?sektor=B"}
	prva, err := s.Posalji(ctx, u, ulaz)
	if err != nil {
		t.Fatal(err)
	}
	ponovo, err := s.Posalji(ctx, u, ulaz)
	if err != nil || ponovo.ID != prva.ID {
		t.Errorf("ponovljeno slanje: %+v %v", ponovo, err)
	}
	druga := ulaz
	druga.Stranica = "/karta"
	if d, err := s.Posalji(ctx, u, druga); err != nil || d.ID == prva.ID {
		t.Errorf("druga stranica je druga prijava: %+v %v", d, err)
	}
	for i := range najvisePrijavaUSatu {
		if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: fmt.Sprintf("prijava %d", i)}); err != nil && !errors.Is(err, ErrPrevisePrijava) {
			t.Fatal(err)
		}
	}
	if ponovo, err := s.Posalji(ctx, u, ulaz); err != nil || ponovo.ID != prva.ID {
		t.Errorf("ponovljeno uz dosegnutu granicu: %+v %v", ponovo, err)
	}
	s.sat = func() time.Time { return time.Now().Add(prozorPrijava + time.Minute) }
	if nova, err := s.Posalji(ctx, u, ulaz); err != nil || nova.ID == prva.ID {
		t.Errorf("nakon roka: %+v %v", nova, err)
	}
}

func TestOdgovori(t *testing.T) {
	s, u := servisPrijavaGreske(t)
	ctx := context.Background()
	p, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "x"})
	if err != nil {
		t.Fatal(err)
	}
	admin := &models.User{ID: uuid.New(), FullName: "Ivo Ivić"}
	if _, err := s.Odgovori(ctx, nil, p.ID, models.StanjePrijaveURadu, ""); !errors.Is(err, ErrPrijavaBezKorisnika) {
		t.Errorf("bez korisnika: %v", err)
	}
	if _, err := s.Odgovori(ctx, admin, p.ID, "zatvoreno", ""); !errors.Is(err, ErrKrivoStanje) {
		t.Errorf("krivo stanje: %v", err)
	}
	if _, err := s.Odgovori(ctx, admin, p.ID, models.StanjePrijaveURadu, strings.Repeat("x", models.NajviseOdgovoraPrijave+1)); !errors.Is(err, ErrOdgovorPredug) {
		t.Errorf("predug odgovor: %v", err)
	}
	if _, err := s.Odgovori(ctx, admin, "nema", models.StanjePrijaveURadu, ""); !errors.Is(err, ErrPrijavaNePostoji) {
		t.Errorf("nepostojeća: %v", err)
	}
	x, err := s.Odgovori(ctx, admin, p.ID, models.StanjePrijaveRijeseno, " Popravljeno. ")
	if err != nil || len(x.Odgovori) != 1 || x.Odgovori[0].Tekst != "Popravljeno." || x.Odgovori[0].Odgovorio != "Ivo Ivić" {
		t.Errorf("%+v %v", x, err)
	}
	broj, err := s.BrojPoStanju(ctx)
	if err != nil || broj[models.StanjePrijaveRijeseno] != 1 {
		t.Errorf("%v %v", broj, err)
	}
	if svi, err := s.Popis(ctx, ""); err != nil || len(svi) != 1 {
		t.Errorf("%d %v", len(svi), err)
	}
	if g, err := s.Get(ctx, p.ID); err != nil || g == nil {
		t.Errorf("%+v %v", g, err)
	}
	if n, err := s.RedniBroj(ctx, p.ID); err != nil || n != 1 {
		t.Errorf("%d %v", n, err)
	}
}
