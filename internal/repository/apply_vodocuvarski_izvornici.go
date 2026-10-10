package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"gocop/internal/models"
)

// Izvornici lista predanog na dva čvora (Odluke (8. 10. 2026.), pitanje 7).
// Izvornik je PDF lista kako je potpisan, pa pripada potpisu, a ne oznaci:
// kad je isti list predan na dva čvora, oba izvornika stoje u zapisu lista
// (zapisIzvornika), a površina svakom listu (glavnom i dvojniku,
// apply_vodocuvarski_dvojnici.go) daje zadnju verziju izvornika njegova
// potpisa (models.IzvornikLista.PredanoAt). Ovjera tako ide na PDF lista
// koji se ovjerava. Izvornik bez potpisa (čvor 0.0.34, starije verzije)
// ostaje uz list pod svojom oznakom, a od više verzija za isti list
// vrijedi zadnja, kao i dosad. Ishod ovisi samo o skupu verzija, ne o
// redoslijedu kojim su stigle.

// verzijaIzvornika je jedna verzija izvornika lista iz knjige
type verzijaIzvornika struct {
	arhivirana bool
	kanal      string
	iz         models.IzvornikLista
}

// primijeniIzvornikeIzPovijesti slaže izvornike listova jednog zapisa iz
// povijesti izvornika i lista, unutar transakcije primjene
func primijeniIzvornikeIzPovijesti(ctx context.Context, tx *sql.Tx, id string) error {
	listovi, err := verzijeListaTx(ctx, tx, id)
	if err != nil {
		return err
	}
	return primijeniIzvornike(ctx, tx, id, slozi(id, listovi))
}

// primijeniIzvornike upisuje na površinu izvornike listova zapisa složenih
// u s; zapis bez izvornika u knjizi ne dira
func primijeniIzvornike(ctx context.Context, tx *sql.Tx, id string, s slozeniListovi) error {
	verzije, err := verzijeIzvornikaTx(ctx, tx, id)
	if err != nil || len(verzije) == 0 {
		return err
	}
	zadnje := zadnjiIzvornici(id, s, verzije)
	for _, listID := range listoviIzvornika(id, s, zadnje) {
		if err := upisiIzvornikLista(ctx, tx, id, listID, zadnje[listID]); err != nil {
			return err
		}
	}
	return nil
}

// verzijeIzvornikaTx čita sve verzije izvornika zapisa iz knjige, od
// najstarije
func verzijeIzvornikaTx(ctx context.Context, tx *sql.Tx, id string) ([]verzijaIzvornika, error) {
	rows, err := tx.QueryContext(ctx, `SELECT archived, channel, payload FROM record_versions
		WHERE entity = ? AND entity_id = ? ORDER BY version_id`, EntityVodocuvarskiIzvornici, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []verzijaIzvornika
	for rows.Next() {
		var arhivirana int
		var payload string
		var v verzijaIzvornika
		if err := rows.Scan(&arhivirana, &v.kanal, &payload); err != nil {
			return nil, err
		}
		v.arhivirana = arhivirana != 0
		if err := json.Unmarshal([]byte(payload), &v.iz); err != nil {
			return nil, fmt.Errorf("verzija izvornika lista %s: %w", id, err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// zadnjiIzvornici daje za svaki list zadnju verziju njegova izvornika
func zadnjiIzvornici(id string, s slozeniListovi, verzije []verzijaIzvornika) map[string]*verzijaIzvornika {
	zadnje := map[string]*verzijaIzvornika{}
	for i := range verzije {
		zadnje[listIzvornika(id, s, verzije[i].iz)] = &verzije[i]
	}
	return zadnje
}

// listIzvornika je oznaka lista kojemu izvornik pripada: glavni list ili
// dvojnik istog potpisa, a bez takvog lista oznaka iz izvornika
func listIzvornika(id string, s slozeniListovi, iz models.IzvornikLista) string {
	if iz.PredanoAt == nil {
		return iz.ListID
	}
	if s.glavni != nil && s.glavni.Predan() && s.glavni.PredanoAt.Equal(*iz.PredanoAt) {
		return id
	}
	for _, d := range s.dvojnici {
		if d.PredanoAt.Equal(*iz.PredanoAt) {
			return d.ID
		}
	}
	return iz.ListID
}

// listoviIzvornika su listovi kojima se izvornik upisuje ili briše, redom:
// oni koji ga dobivaju, glavni list i list svakog potpisa iz povijesti
// zapisa, koji ga možda više nema
func listoviIzvornika(id string, s slozeniListovi, zadnje map[string]*verzijaIzvornika) []string {
	ima := map[string]bool{id: true}
	for listID := range zadnje {
		ima[listID] = true
	}
	for _, p := range s.potpisi {
		ima[models.IDDvostrukogLista(id, p)] = true
	}
	out := make([]string, 0, len(ima))
	for listID := range ima {
		out = append(out, listID)
	}
	sort.Strings(out)
	return out
}

// upisiIzvornikLista upisuje na površinu izvornik lista iz verzije, a bez
// verzije ili s obrisanom ga briše. Izvornik lista koji ima svoj zapis
// izvornika u knjizi (dvojnik lista bez oznake osobe i dana, vidi
// upisiDvojnika) ravna se po njemu.
func upisiIzvornikLista(ctx context.Context, tx *sql.Tx, id, listID string, v *verzijaIzvornika) error {
	if listID != id {
		var vlastitih int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = ?`,
			EntityVodocuvarskiIzvornici, listID).Scan(&vlastitih); err != nil || vlastitih > 0 {
			return err
		}
	}
	if v == nil || v.arhivirana {
		_, err := tx.ExecContext(ctx, `DELETE FROM vodocuvarski_izvornici WHERE list_id = ?`, listID)
		return err
	}
	iz := v.iz
	iz.ListID = listID
	if err := primiIzvornik(ctx, EntityVodocuvarski, v.kanal, &iz); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, izvornikListaUpsert, iz.ListID, iz.Otisak, iz.Bajtova, iz.Vrsta, iz.Sazetak, iz.UpdatedAt.UTC())
	return err
}
