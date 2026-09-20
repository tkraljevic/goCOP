package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

// Spremište sadržaja je jedno za cijeli program: repozitoriji ga koriste
// pri objavi, a primjena razmjene pri ugradnji tuđih verzija. Postavlja se
// jednom pri pokretanju; bez njega se izvornici ne mogu ni spremiti ni
// pročitati, i to je greška, ne tiha degradacija.
var spremiste *sadrzaj.Spremiste

// SetSpremiste postavlja spremište sadržaja za sve repozitorije
func SetSpremiste(s *sadrzaj.Spremiste) { spremiste = s }

// Spremiste vraća postavljeno spremište; nil kad nije otvoreno
func Spremiste() *sadrzaj.Spremiste { return spremiste }

var errBezSpremista = errors.New("spremište sadržaja nije otvoreno")

// spremiPDF upisuje PDF u spremište i vraća zapis izvornika bez bajtova,
// kakav ide u glavnu bazu i knjigu verzija
func spremiPDF(ctx context.Context, entitet, id string, pdf []byte, sazetak string, now time.Time) (models.IzvornikLista, error) {
	if spremiste == nil {
		return models.IzvornikLista{}, errBezSpremista
	}
	otisak, err := spremiste.Upisi(ctx, "application/pdf", pdf, "ovdje", sadrzaj.Veza{Entitet: entitet, EntitetID: id, Uloga: "izvornik"})
	if err != nil {
		return models.IzvornikLista{}, err
	}
	return models.IzvornikLista{ListID: id, Otisak: otisak, Bajtova: len(pdf), Vrsta: "application/pdf", Sazetak: sazetak, UpdatedAt: now}, nil
}

// ucitajPDF puni bajtove izvornika iz spremišta; kad ih čvor nema, zapis
// ostaje bez bajtova, s otiskom po kojem se mogu dohvatiti
func ucitajPDF(ctx context.Context, iz *models.IzvornikLista) {
	if spremiste == nil || iz.Otisak == "" {
		return
	}
	if b, _, err := spremiste.Citaj(ctx, iz.Otisak); err == nil {
		iz.PDF = b
	}
}

// primiIzvornik ugrađuje izvornik primljen razmjenom: stariji čvorovi šalju
// bajtove u zapisu, pa ih spremimo i dalje vodimo po otisku; noviji šalju
// samo otisak, a bajtovi se traže posebno
func primiIzvornik(ctx context.Context, entitet string, iz *models.IzvornikLista) error {
	if len(iz.PDF) > 0 {
		if iz.Otisak == "" {
			iz.Otisak = sadrzaj.Otisak(iz.PDF)
		}
		if iz.Bajtova == 0 {
			iz.Bajtova = len(iz.PDF)
		}
		if spremiste != nil {
			if err := spremiste.UpisiProvjereno(ctx, iz.Otisak, "application/pdf", iz.PDF, "razmjena", sadrzaj.Veza{Entitet: entitet, EntitetID: iz.ListID, Uloga: "izvornik"}); err != nil {
				return err
			}
		}
		return nil
	}
	if spremiste != nil && iz.Otisak != "" {
		if err := spremiste.Vezi(ctx, iz.Otisak, sadrzaj.Veza{Entitet: entitet, EntitetID: iz.ListID, Uloga: "izvornik"}); err != nil {
			return err
		}
		return spremiste.Zeli(ctx, iz.Otisak, "application/pdf", iz.Bajtova, "pretplata")
	}
	return nil
}

// PreseliIzvornikePrijava jednokratno seli PDF-ove prijava iz stare tablice
// (s bajtovima) u spremište sadržaja i prepisuje njihove verzije u knjizi
// tako da nose samo otisak. Jedina iznimka od pravila da se knjiga ne
// prepisuje: radi se jednom, prije nego mreža dobije drugi čvor, i zapis
// zadržava isti version_id. Vraća koliko je preseljeno.
func PreseliIzvornikePrijava(ctx context.Context, db *sql.DB) (int, int64, error) {
	var ima int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'prijave_izvornici_stari'`).Scan(&ima); err != nil {
		return 0, 0, err
	}
	if ima == 0 {
		return 0, 0, nil
	}
	if spremiste == nil {
		return 0, 0, errBezSpremista
	}
	rows, err := db.QueryContext(ctx, `SELECT prijava_id, pdf, sazetak, updated_at FROM prijave_izvornici_stari`)
	if err != nil {
		return 0, 0, err
	}
	type red struct {
		id, sazetak string
		pdf         []byte
		kad         time.Time
	}
	var redovi []red
	for rows.Next() {
		var r red
		if err := rows.Scan(&r.id, &r.pdf, &r.sazetak, &r.kad); err != nil {
			rows.Close()
			return 0, 0, err
		}
		redovi = append(redovi, r)
	}
	rows.Close()
	var n int
	var bajtova int64
	for _, r := range redovi {
		if len(r.pdf) == 0 {
			continue
		}
		otisak, err := spremiste.Upisi(ctx, "application/pdf", r.pdf, "ovdje", sadrzaj.Veza{Entitet: EntityPrijave, EntitetID: r.id, Uloga: "izvornik"})
		if err != nil {
			return n, bajtova, err
		}
		iz := models.IzvornikLista{ListID: r.id, Otisak: otisak, Bajtova: len(r.pdf), Vrsta: "application/pdf", Sazetak: r.sazetak, UpdatedAt: r.kad}
		// spremište je potvrdilo da ima sadržaj: tek sad glavna baza
		if spremiste.Ima(ctx, otisak) == false {
			return n, bajtova, fmt.Errorf("izvornik %s nije u spremištu nakon upisa", r.id)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return n, bajtova, err
		}
		if _, err := tx.ExecContext(ctx, prijavaIzvornikUpsert, iz.ListID, iz.Otisak, iz.Bajtova, iz.Vrsta, iz.Sazetak, iz.UpdatedAt); err != nil {
			tx.Rollback()
			return n, bajtova, err
		}
		body, _ := json.Marshal(iz)
		if _, err := tx.ExecContext(ctx, `UPDATE record_versions SET payload = ? WHERE entity = ? AND entity_id = ?`, string(body), EntityPrijaveIzvornici, r.id); err != nil {
			tx.Rollback()
			return n, bajtova, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM prijave_izvornici_stari WHERE prijava_id = ?`, r.id); err != nil {
			tx.Rollback()
			return n, bajtova, err
		}
		if err := tx.Commit(); err != nil {
			return n, bajtova, err
		}
		n++
		bajtova += int64(len(r.pdf))
	}
	var ostalo int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM prijave_izvornici_stari`).Scan(&ostalo); err != nil {
		return n, bajtova, err
	}
	if ostalo == 0 {
		if _, err := db.ExecContext(ctx, `DROP TABLE prijave_izvornici_stari`); err != nil {
			return n, bajtova, err
		}
	}
	return n, bajtova, nil
}
