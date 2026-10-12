package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"gocop/internal/models"
)

// Bilješka i ispravak arhivske vrijednosti vezani su uz trenutak niza
// (letva, veličina, korak, vrijeme), i tablica na jednom trenutku drži samo
// jedan zapis. Knjiga ih zna imati dva s različitim identitetom: isti trenutak
// zapisan dvaput, na dva čvora ili prije nego što je jedan vidio drugi. Bez
// pravila bi na svakom čvoru ostao onaj koji je prvi stigao, a drugi bi pri
// svakoj razmjeni padao na jedinstvenom ključu. Zato na trenutku ostaje
// noviji zapis, isti na svim čvorovima ma kojim redom verzije stigle.

// trenutakNiza je trenutak arhivskog niza uz koji stoji bilješka ili ispravak
type trenutakNiza struct {
	letva, velicina, korak string
	vrijeme                time.Time
}

// novijiNaTrenutku javlja ima li zapis a prednost pred zapisom b na istom
// trenutku: kasnije izmijenjen, a uz isto vrijeme izmjene veći identitet
func novijiNaTrenutku(aKad time.Time, aID string, bKad time.Time, bID string) bool {
	if !aKad.Equal(bKad) {
		return aKad.After(bKad)
	}
	return aID > bID
}

// mjestoNaTrenutku javlja smije li zapis id, izmijenjen u izmijenjeno, stati
// na trenutak u tablici. Kad trenutak drži drugi zapis, ostaje noviji: ako je
// to pridošli, drugi se miče s površine; ako nije, pridošli se preskače bez
// greške. Knjiga ostaje kakva jest, s obje verzije.
func mjestoNaTrenutku(ctx context.Context, tx *sql.Tx, tablica string, t trenutakNiza, id string, izmijenjeno time.Time) (bool, error) {
	var drugi string
	var drugiIzmijenjen time.Time
	err := tx.QueryRowContext(ctx, `SELECT id, updated_at FROM `+tablica+`
		WHERE letva=? AND velicina=? AND korak=? AND vrijeme=? AND id <> ?`,
		t.letva, t.velicina, t.korak, t.vrijeme.UTC(), id).Scan(&drugi, &drugiIzmijenjen)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil || !novijiNaTrenutku(izmijenjeno, id, drugiIzmijenjen, drugi) {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM `+tablica+` WHERE id = ?`, drugi)
	return err == nil, err
}

// primijeniBiljesku stavlja primljenu bilješku arhive na površinu
func primijeniBiljesku(ctx context.Context, tx *sql.Tx, payload []byte) error {
	var b models.ArhivaBiljeska
	if err := json.Unmarshal(payload, &b); err != nil {
		return err
	}
	ide, err := mjestoNaTrenutku(ctx, tx, "arhiva_biljeske", trenutakNiza{b.Letva, b.Velicina, b.Korak, b.Vrijeme},
		b.ID.String(), b.UpdatedAt)
	if err != nil || !ide {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO arhiva_biljeske (id, letva, velicina, korak, vrijeme, vrsta, tekst, tko,
			created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET vrsta=excluded.vrsta, tekst=excluded.tekst,
			tko=excluded.tko, updated_at=excluded.updated_at`,
		b.ID.String(), b.Letva, b.Velicina, b.Korak, b.Vrijeme.UTC(), b.Vrsta, b.Tekst, b.Tko,
		b.CreatedAt.UTC(), b.UpdatedAt.UTC())
	return err
}

// primijeniIspravak stavlja primljeni ispravak arhive na površinu
func primijeniIspravak(ctx context.Context, tx *sql.Tx, payload []byte) error {
	var i models.ArhivaIspravak
	if err := json.Unmarshal(payload, &i); err != nil {
		return err
	}
	ide, err := mjestoNaTrenutku(ctx, tx, "arhiva_ispravci", trenutakNiza{i.Letva, i.Velicina, i.Korak, i.Vrijeme},
		i.ID.String(), i.UpdatedAt)
	if err != nil || !ide {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO arhiva_ispravci (id, letva, velicina, korak, vrijeme, vrijednost, staro,
			razlog, ispravio, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET vrijednost=excluded.vrijednost, staro=excluded.staro,
			razlog=excluded.razlog, ispravio=excluded.ispravio, updated_at=excluded.updated_at`,
		i.ID.String(), i.Letva, i.Velicina, i.Korak, i.Vrijeme.UTC(), i.Vrijednost, i.Staro,
		i.Razlog, i.Ispravio, i.CreatedAt.UTC(), i.UpdatedAt.UTC())
	return err
}
