package db

import (
	"context"
	"database/sql"
	"fmt"
)

// Prvo punjenje upisuje djelatnike, dužnosti, sektore i branjena područja
// izravno u tablice, prije nego što postoji knjiga verzija u ime ovog
// čvora. Da bi stigli i na druga računala, svaki redak koji je punjenje
// stvarno upisalo dobije oznaku u tablici punjenje_za_knjigu, u istoj
// transakciji. Verzije za označene retke piše repository.UpisiPunjenje kad
// je računalo u mreži, a račune s popisa početnih lozinki tek kad se popis
// preuzme ili mu prođe rok. Retke iz starijih baza, bez oznake, nitko ne
// upisuje sam.
//
// Oznake vrijede samo na računalu koje je osnovalo novu mrežu. Računalo
// primljeno u postojeću mrežu djelatnike i organizaciju dobiva od nje, pa
// oznake odbaci (OdbaciPunjenje) i ne šalje ništa iz datoteka uz bazu:
// njegove bi verzije bile novije od svega u mreži i vratile bi stanje iz
// datoteka na sva računala. Iz istog razloga punjenje ne označava ništa dok
// je računalo već u mreži.
//
// Račun admin se ne označava: svako računalo ima svoj, sa svojom lozinkom.

// Nazivi entiteta u knjizi verzija za retke iz punjenja; isti su kao u
// paketu repository (EntityUsers, EntityDuties, EntitySectors, EntityAreas)
const (
	entitetRacuna    = "users"
	entitetDuznosti  = "duties"
	entitetSektora   = "sectors"
	entitetPodrucja  = "areas"
	upisOznakeKnjige = `INSERT OR IGNORE INTO punjenje_za_knjigu (entity, entity_id)
		SELECT ?, ? WHERE NOT EXISTS (SELECT 1 FROM network)`
)

// OdbaciPunjenje briše sve oznake prvog punjenja bez upisa u knjigu, kad je
// računalo primljeno u postojeću mrežu, i vraća koliko ih je bilo
func OdbaciPunjenje(ctx context.Context, tx *sql.Tx) (int64, error) {
	res, err := tx.ExecContext(ctx, `DELETE FROM punjenje_za_knjigu`)
	if err != nil {
		return 0, fmt.Errorf("oznake prvog punjenja nisu odbačene: %w", err)
	}
	return res.RowsAffected()
}

// oznaciZaKnjigu bilježi da je punjenje upisalo redak entiteta s tim id-om,
// osim kad je računalo već u mreži
func oznaciZaKnjigu(tx *sql.Tx, entitet, id string) error {
	if _, err := tx.Exec(upisOznakeKnjige, entitet, id); err != nil {
		return fmt.Errorf("oznaka punjenja %s %s: %w", entitet, id, err)
	}
	return nil
}

// oznaciRacune označava račune iz imenika i njihove dužnosti, osim računa
// admin. Id-ovi su isti kao u upisiRacun.
func oznaciRacune(tx *sql.Tx, users []seedUser) error {
	for _, u := range users {
		if u.Username == racunAdmin.Username {
			continue
		}
		if err := oznaciZaKnjigu(tx, entitetRacuna, StableID("user", u.Username).String()); err != nil {
			return err
		}
		for i := range u.Duties {
			if err := oznaciZaKnjigu(tx, entitetDuznosti, idDuznosti(u.Username, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// idDuznosti je stalni id i-te dužnosti računa iz imenika
func idDuznosti(korisnik string, i int) string {
	return StableID("duty", fmt.Sprintf("%s|%d", korisnik, i)).String()
}

// upisiOznaceno izvodi upis jednog retka i označi ga samo kad je redak
// stvarno upisan (ON CONFLICT DO NOTHING ga preskače)
func upisiOznaceno(tx *sql.Tx, entitet, id, upit string, args ...any) error {
	res, err := tx.Exec(upit, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	return oznaciZaKnjigu(tx, entitet, id)
}
