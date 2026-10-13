package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Bilješka i ispravak arhivske vrijednosti vezani su uz trenutak niza
// (letva, veličina, korak, vrijeme), i tablica na jednom trenutku drži samo
// jedan zapis. Knjiga ih zna imati dva s različitim identitetom: isti trenutak
// zapisan dvaput, na dva čvora ili prije nego što je jedan vidio drugi. Bez
// pravila bi na svakom čvoru ostao onaj koji je prvi stigao, a drugi bi pri
// svakoj razmjeni padao na jedinstvenom ključu. Zato na trenutku ostaje
// noviji zapis, isti na svim čvorovima ma kojim redom verzije stigle. Kad
// onaj koji stoji na trenutku bude obrisan, trenutak dobiva noviji od onih
// koji su u knjizi još živi (makniSTrenutka). Brisanje bilješke na izvoru
// zato daje spomenik svim bilješkama trenutka (BiljeskaRepository.Spremi).

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

// zivNaTrenutku je zadnja verzija bilješke ili ispravka koji je u knjizi živ
type zivNaTrenutku struct {
	id, kanal  string
	sadrzaj    json.RawMessage // nil kad se sadržaj ne da pročitati
	vrijeme    time.Time
	izmijenjen time.Time
}

// naTrenutku javlja stoji li zapis na trenutku t; sadržaj koji se ne da
// pročitati ne stoji ni na jednom
func (z zivNaTrenutku) naTrenutku(t trenutakNiza) bool {
	return z.sadrzaj != nil && z.vrijeme.Equal(t.vrijeme)
}

// ziviNaNizu su zadnje verzije zapisa entiteta koji su u knjizi živi i stoje
// na nizu (letva, veličina, korak). Sadržaj koji nije JSON preskače se, da
// jedna pokvarena verzija ne zaustavi čitanje.
const ziviNaNizu = `SELECT v.entity_id, v.channel, v.payload FROM record_versions v
	WHERE v.entity = ? AND v.archived = 0
		AND v.version_id = (SELECT MAX(w.version_id) FROM record_versions w
			WHERE w.entity = v.entity AND w.entity_id = v.entity_id)
		AND CASE WHEN json_valid(v.payload) THEN json_extract(v.payload, '$.letva') = ?
			AND json_extract(v.payload, '$.velicina') = ? AND json_extract(v.payload, '$.korak') = ? END
	ORDER BY v.version_id`

// ziviNaTrenutku su bilješke ili ispravci (entitet) koji su u knjizi živi i
// stoje na trenutku t, i oni kojih na površini nema jer je trenutak dobio
// noviji. Vrijeme se uspoređuje kao trenutak, ne kao tekst, jer ga sadržaj
// zna nositi u drugoj vremenskoj zoni.
func ziviNaTrenutku(ctx context.Context, tx *sql.Tx, entitet string, t trenutakNiza) ([]zivNaTrenutku, error) {
	rows, err := tx.QueryContext(ctx, ziviNaNizu, entitet, t.letva, t.velicina, t.korak)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []zivNaTrenutku
	for err == nil && rows.Next() {
		var z zivNaTrenutku
		z, err = procitajZivog(rows)
		if z.naTrenutku(t) {
			out = append(out, z)
		}
	}
	return out, errors.Join(err, rows.Err())
}

// procitajZivog čita redak upita ziviNaNizu
func procitajZivog(rows *sql.Rows) (zivNaTrenutku, error) {
	var z zivNaTrenutku
	var sadrzaj string
	err := rows.Scan(&z.id, &z.kanal, &sadrzaj)
	var polja struct {
		Vrijeme   time.Time `json:"vrijeme"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	if json.Unmarshal([]byte(sadrzaj), &polja) == nil {
		z.sadrzaj, z.vrijeme, z.izmijenjen = json.RawMessage(sadrzaj), polja.Vrijeme, polja.UpdatedAt
	}
	return z, err
}

// najnoviji je od živih zapisa trenutka onaj koji ima prednost (novijiNaTrenutku)
func najnoviji(zivi []zivNaTrenutku) zivNaTrenutku {
	n := zivi[0]
	for _, z := range zivi[1:] {
		if novijiNaTrenutku(z.izmijenjen, z.id, n.izmijenjen, n.id) {
			n = z
		}
	}
	return n
}

// makniSTrenutka miče s površine bilješku ili ispravak kojem je v (spomenik)
// zadnja verzija. Kad je on stajao na trenutku, trenutak dobiva onaj od
// zapisa koji su u knjizi još živi koji ima prednost, kao da maknuti nikad
// nije bio upisan. Bez toga bi trenutak ostao prazan na čvoru koji je imao
// oba zapisa, a pun na čvoru kojem je stariji stigao nakon brisanja i pri
// ponovnom pokretanju (ObnoviPovrsinu). primijeni stavlja zapis na površinu.
func makniSTrenutka(ctx context.Context, tx *sql.Tx, tablica string, v ledger.Version,
	primijeni func(context.Context, *sql.Tx, []byte) error) error {
	var t trenutakNiza
	err := tx.QueryRowContext(ctx, `SELECT letva, velicina, korak, vrijeme FROM `+tablica+` WHERE id = ?`,
		v.EntityID).Scan(&t.letva, &t.velicina, &t.korak, &t.vrijeme)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // nije na površini: trenutak drži drugi zapis ili nitko
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM `+tablica+` WHERE id = ?`, v.EntityID)
	}
	var zivi []zivNaTrenutku
	if err == nil {
		zivi, err = ziviNaTrenutku(ctx, tx, v.Entity, t)
	}
	if err != nil || len(zivi) == 0 {
		return err
	}
	return primijeni(ctx, tx, najnoviji(zivi).sadrzaj)
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
