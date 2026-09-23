// Package racun otključava račun za Geolux HydroView upisan u aplikaciju,
// da ga naredbe na ovom čvoru koriste isto kako ga koristi poslužitelj.
package racun

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	"gocop/internal/hidroview"
	"gocop/internal/peers"
	"gocop/internal/posta"
	"gocop/internal/razmjena"
	"gocop/internal/repository"

	_ "modernc.org/sqlite"
)

// Racun je otključan račun.
type Racun struct{ Korisnik, Lozinka, Adresa string }

// Cvora otključava račun čvora uz zadanu bazu, onako kako to radi
// poslužitelj: ključ čvora leži uz bazu, a lozinka je zaključana ključem
// izvedenim iz njega. Ključ se samo čita — naredba ne smije stvoriti novi
// identitet čvora.
func Cvora(db string) (Racun, error) {
	kljuc, err := razmjena.LoadKey(filepath.Join(filepath.Dir(db), peers.KeyFileName))
	if err != nil {
		return Racun{}, fmt.Errorf("ključ čvora: %w", err)
	}
	baza, err := sql.Open("sqlite", db+"?mode=ro")
	if err != nil {
		return Racun{}, err
	}
	defer baza.Close()
	r, err := repository.NewHidroViewRepository(baza).Racun(context.Background(), "")
	if err != nil {
		return Racun{}, err
	}
	if r == nil {
		return Racun{}, fmt.Errorf("u aplikaciji nije upisan račun čvora za HydroView")
	}
	lozinka, err := posta.Otkljucaj(hidroview.Kljuc(kljuc.Seed()), r.Lozinka)
	if err != nil {
		return Racun{}, fmt.Errorf("lozinka se ne da otključati: %w", err)
	}
	return Racun{Korisnik: r.Korisnik, Lozinka: lozinka, Adresa: r.Adresa}, nil
}
