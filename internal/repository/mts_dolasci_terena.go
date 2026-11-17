package repository

import (
	"context"

	"gocop/internal/models"
)

// DolazakNaTeren je jedan redak koji je nešto donio na mjesto na terenu, sa
// skladištem iz kojeg je potez krenuo (prazno kad ga nema, npr. pražnjenje)
type DolazakNaTeren struct {
	Mjesto      models.MjestoTerena
	VrstaID     string
	Oblik       string
	Datum       string // dan, 2006-01-02
	SkladisteID string
}

// DolasciNaTeren su svi dolasci na teren po danu, najstariji prvi. Skladište
// poteza nalazi se po istoj vezi; zaprimanje robe na putu nosi vezu slanja.
func (r *MtsRepository) DolasciNaTeren(ctx context.Context, journalID, sektor string) ([]DolazakNaTeren, error) {
	q := `SELECT y.area_id, y.section_code, y.structure_id, y.mjesto, y.vrsta_id, y.oblik, y.datum,
		COALESCE((SELECT x.skladiste_id FROM mts_promet x WHERE y.veza_id <> '' AND x.veza_id = y.veza_id AND x.skladiste_id <> '' LIMIT 1), '')
		FROM mts_promet y WHERE y.skladiste_id = '' AND y.kolicina > 0`
	var args []any
	if journalID != "" {
		q += ` AND y.journal_id = ?`
		args = append(args, journalID)
	}
	if sektor != "" {
		q += ` AND y.sektor = ?`
		args = append(args, sektor)
	}
	rows, err := r.db.QueryContext(ctx, q+` ORDER BY y.datum, y.created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DolazakNaTeren
	for rows.Next() {
		var d DolazakNaTeren
		if err := rows.Scan(&d.Mjesto.AreaID, &d.Mjesto.SectionCode, &d.Mjesto.StructureID, &d.Mjesto.Mjesto, &d.VrstaID, &d.Oblik, &d.Datum, &d.SkladisteID); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
