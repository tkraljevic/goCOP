package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Prvo punjenje (db.SeedInitialData) upisuje djelatnike, dužnosti, sektore
// i branjena područja izravno u tablice, prije nego što postoji knjiga
// verzija u ime ovog čvora, a retke koje je stvarno upisalo označi u
// tablici punjenje_za_knjigu. UpisiPunjenje za označene retke piše verzije
// istim putem kao registar Organizacija i Djelatnici, pa stižu i na druga
// računala (Odluke (26. 11. 2026.)). Zove se pri pokretanju, kad računalo
// dobije ključ mreže (osnuje mrežu ili učita kopiju ključa) i kad se popis
// početnih lozinki preuzme ili mu prođe rok.
// Retke bez oznake ne dira: postojeće baze se ne upisuju same.
//
// Piše samo na računalu koje je osnovalo novu mrežu, jedinom izvoru tih
// podataka u njoj. Dok računalo nije ni u jednoj mreži, oznake čekaju:
// verzije bez razmjene ionako nikamo ne idu, a računalo možda još bude
// primljeno u postojeću mrežu. Kad bude primljeno, oznake se odbace bez
// upisa (db.OdbaciPunjenje), jer bi verzije iz datoteka uz bazu nosile
// današnje vrijeme i na svim računalima pregazile izmjene napravljene u
// mreži.

// korakPunjenja upisuje označene retke jednog entiteta
type korakPunjenja struct {
	entitet string
	// ceka je upit koji za id javlja da redak još ne smije u knjigu (prazno:
	// nikad ne čeka)
	ceka string
	// upisi čita redak s površine i piše njegovu verziju; sql.ErrNoRows kad
	// retka više nema
	upisi func(ctx context.Context, tx *sql.Tx, id string) error
}

// računi s popisa početnih lozinki i njihove dužnosti čekaju dok se popis ne
// preuzme ili mu ne prođe rok: dok račun nema verziju, obnova popisa zna da
// ga nitko nije dirao (db.ObnoviPocetneLozinke)
const (
	racunCekaPopis    = `SELECT EXISTS (SELECT 1 FROM pocetne_lozinke_cekaju WHERE user_id = ?)`
	duznostCekaPopis  = `SELECT EXISTS (SELECT 1 FROM duties d JOIN pocetne_lozinke_cekaju c ON c.user_id = d.user_id WHERE d.id = ?)`
	imaVerziju        = `SELECT EXISTS (SELECT 1 FROM record_versions WHERE entity = ? AND entity_id = ?)`
	oznaceniZaKnjigu  = `SELECT entity_id FROM punjenje_za_knjigu WHERE entity = ? ORDER BY rowid`
	brisiOznakuKnjige = `DELETE FROM punjenje_za_knjigu WHERE entity = ? AND entity_id = ?`
	racunaloUMrezi    = `SELECT EXISTS (SELECT 1 FROM network)`
)

// UpisiPunjenje piše verzije za retke koje je prvo punjenje upisalo na ovom
// računalu, a još nemaju verziju: sektore i branjena područja prije dužnosti
// koje se na njih vežu, zatim račune i dužnosti. Račun s popisa početnih
// lozinki koji još čeka preskače, s dužnostima, i ostavlja mu oznaku. Redak
// koji već ima verziju (npr. od prijave) ili ga više nema ne dobiva novu.
// Obrađenim recima briše oznaku, pa drugi poziv ne piše ništa. Dok
// računalo nije u mreži ne piše ništa i oznake ostavlja. Vraća koliko je
// verzija napisao.
func (r *UserRepository) UpisiPunjenje(ctx context.Context, org *OrgRepository) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() // nakon Commita ne radi ništa; prije njega poništava upis
	if uMrezi, err := upitPostoji(ctx, tx, racunaloUMrezi); err != nil || !uMrezi {
		return 0, err
	}
	koraci := []korakPunjenja{
		{entitet: EntitySectors, upisi: org.zapisiSektor},
		{entitet: EntityAreas, upisi: org.zapisiPodrucje},
		{entitet: EntityUsers, ceka: racunCekaPopis, upisi: r.zapisiRacunPunjenja},
		{entitet: EntityDuties, ceka: duznostCekaPopis, upisi: r.zapisiDuznostPunjenja},
	}
	ukupno := 0
	for _, k := range koraci {
		n, err := k.izvedi(ctx, tx)
		if err != nil {
			return 0, fmt.Errorf("upis punjenja u knjigu (%s): %w", k.entitet, err)
		}
		ukupno += n
	}
	return ukupno, tx.Commit()
}

// izvedi upisuje sve označene retke entiteta i vraća koliko je verzija
// napisao
func (k korakPunjenja) izvedi(ctx context.Context, tx *sql.Tx) (int, error) {
	ids, err := oznaceniRetci(ctx, tx, k.entitet)
	n := 0
	for _, id := range ids {
		var upisan bool
		if err == nil {
			upisan, err = k.obradi(ctx, tx, id)
		}
		if upisan {
			n++
		}
	}
	return n, err
}

// obradi upisuje jedan označeni redak i javlja je li napisao verziju
func (k korakPunjenja) obradi(ctx context.Context, tx *sql.Tx, id string) (bool, error) {
	ceka, err := upitPostoji(ctx, tx, k.ceka, id)
	if err != nil || ceka {
		return false, err
	}
	ima, err := upitPostoji(ctx, tx, imaVerziju, k.entitet, id)
	if err == nil && !ima {
		err = k.upisi(ctx, tx, id)
	}
	upisan := err == nil && !ima
	if errors.Is(err, sql.ErrNoRows) {
		err = nil // retka više nema; nema se što upisati
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, brisiOznakuKnjige, k.entitet, id)
	}
	return upisan && err == nil, err
}

// upitPostoji izvodi upit SELECT EXISTS; prazan upit ne postoji
func upitPostoji(ctx context.Context, tx *sql.Tx, upit string, args ...any) (bool, error) {
	if upit == "" {
		return false, nil
	}
	var ima bool
	err := tx.QueryRowContext(ctx, upit, args...).Scan(&ima)
	return ima, err
}

// oznaceniRetci su id-ovi redaka entiteta koje je punjenje označilo, redom upisa
func oznaceniRetci(ctx context.Context, tx *sql.Tx, entitet string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, oznaceniZaKnjigu, entitet)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// zapisiRacunPunjenja piše verziju računa kakav je na površini, sa
// sažetkom lozinke i obaveznom promjenom, istim putem kao CreateUser
func (r *UserRepository) zapisiRacunPunjenja(ctx context.Context, tx *sql.Tx, id string) error {
	u, err := getUserTx(ctx, tx, id)
	if err == nil {
		_, err = r.zapisi(ctx, tx, EntityUsers, id, versionOfUser(u))
	}
	return err
}

// zapisiDuznostPunjenja piše verziju dužnosti kakva je na površini
func (r *UserRepository) zapisiDuznostPunjenja(ctx context.Context, tx *sql.Tx, id string) error {
	d, err := getDutyTx(ctx, tx, id)
	if err == nil {
		_, err = r.zapisi(ctx, tx, EntityDuties, id, d)
	}
	return err
}

// zapisiSektor piše verziju sektora kakav je na površini, kao SaveSector
func (r *OrgRepository) zapisiSektor(ctx context.Context, tx *sql.Tx, id string) error {
	s, err := scanSector(tx.QueryRowContext(ctx, sectorSelect+` WHERE id = ?`, id))
	if err == nil {
		err = zabiljeziSDjecom(ctx, tx, r.rec, EntitySectors, s.ID, &s)
	}
	return err
}

// zapisiPodrucje piše verziju branjenog područja kakvo je na površini, kao
// SaveArea
func (r *OrgRepository) zapisiPodrucje(ctx context.Context, tx *sql.Tx, id string) error {
	a, err := scanArea(tx.QueryRowContext(ctx, areaSelect+` WHERE id = ?`, id))
	if err == nil {
		err = zabiljeziSDjecom(ctx, tx, r.rec, EntityAreas, id, &a)
	}
	return err
}
