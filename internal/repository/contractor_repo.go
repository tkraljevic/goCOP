package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"gocop/internal/models"

	"github.com/google/uuid"
)

// EntityContractors i EntityContractorAssignments su nazivi entiteta u knjizi verzija
const (
	EntityContractors           = "contractors"
	EntityContractorAssignments = "contractor_assignments"
)

const contractorSelect = `SELECT id, name, short_name, oib, address, phone, email, contact, notes, active, updated_at FROM contractors`

func scanContractor(row rowScannerOrg) (models.Contractor, error) {
	var c models.Contractor
	var active int
	err := row.Scan(&c.ID, &c.Name, &c.ShortName, &c.OIB, &c.Address, &c.Phone, &c.Email, &c.Contact, &c.Notes, &active, &c.UpdatedAt)
	c.Active = active != 0
	return c, err
}

// ListContractors vraća izvođače abecedno, svakog s njegovim vezama
func (r *OrgRepository) ListContractors(ctx context.Context) ([]models.Contractor, error) {
	rows, err := r.db.QueryContext(ctx, contractorSelect+` ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Contractor
	byID := map[string]int{}
	for rows.Next() {
		c, err := scanContractor(rows)
		if err != nil {
			return nil, err
		}
		byID[c.ID] = len(out)
		out = append(out, c)
	}
	assigns, err := r.ListAssignments(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, a := range assigns {
		if i, ok := byID[a.ContractorID]; ok {
			out[i].Assignments = append(out[i].Assignments, a)
		}
	}
	return out, nil
}

// GetContractor čita jednog izvođača s vezama; nil kad ga nema
func (r *OrgRepository) GetContractor(ctx context.Context, id string) (*models.Contractor, error) {
	c, err := scanContractor(r.db.QueryRowContext(ctx, contractorSelect+` WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.Assignments, err = r.ListAssignments(ctx, id)
	return &c, err
}

// ListAssignments vraća veze jednog izvođača, ili sve kad je id prazan
func (r *OrgRepository) ListAssignments(ctx context.Context, contractorID string) ([]models.ContractorAssignment, error) {
	q, args := `SELECT id, contractor_id, sector_id, area_id, note, updated_at FROM contractor_assignments`, []any{}
	if contractorID != "" {
		q += ` WHERE contractor_id = ?`
		args = append(args, contractorID)
	}
	rows, err := r.db.QueryContext(ctx, q+` ORDER BY sector_id, area_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ContractorAssignment
	for rows.Next() {
		var a models.ContractorAssignment
		if err := rows.Scan(&a.ID, &a.ContractorID, &a.SectorID, &a.AreaID, &a.Note, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

const contractorUpsert = `INSERT INTO contractors (id, name, short_name, oib, address, phone, email, contact, notes, active, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET name = excluded.name, short_name = excluded.short_name, oib = excluded.oib,
		address = excluded.address, phone = excluded.phone, email = excluded.email, contact = excluded.contact,
		notes = excluded.notes, active = excluded.active, updated_at = excluded.updated_at`

const assignmentUpsert = `INSERT INTO contractor_assignments (id, contractor_id, sector_id, area_id, note, updated_at)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET contractor_id = excluded.contractor_id, sector_id = excluded.sector_id,
		area_id = excluded.area_id, note = excluded.note, updated_at = excluded.updated_at`

// SaveContractor upisuje izvođača i postavlja njegove veze na zadani skup:
// veze koje ostaju ne dobivaju novu verziju, maknute se arhiviraju, nove upišu
func (r *OrgRepository) SaveContractor(ctx context.Context, c *models.Contractor, wanted []models.ContractorAssignment) error {
	now := time.Now().UTC()
	c.UpdatedAt = now
	if c.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		c.ID = id.String()
	}
	existing, err := r.ListAssignments(ctx, c.ID)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, contractorUpsert, c.ID, c.Name, c.ShortName, c.OIB, c.Address, c.Phone, c.Email,
		c.Contact, c.Notes, boolToInt(c.Active), now); err != nil {
		return fmt.Errorf("upis izvođača: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityContractors, c.ID, c); err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, w := range wanted {
		keep[w.Key()] = true
	}
	have := map[string]bool{}
	for _, e := range existing {
		if keep[e.Key()] {
			have[e.Key()] = true
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM contractor_assignments WHERE id = ?`, e.ID); err != nil {
			return err
		}
		if _, err := r.rec.Archive(ctx, tx, EntityContractorAssignments, e.ID, e); err != nil {
			return err
		}
	}
	for _, w := range wanted {
		if have[w.Key()] {
			continue
		}
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		w.ID, w.ContractorID, w.UpdatedAt = id.String(), c.ID, now
		if _, err := tx.ExecContext(ctx, assignmentUpsert, w.ID, w.ContractorID, w.SectorID, w.AreaID, w.Note, w.UpdatedAt); err != nil {
			return fmt.Errorf("upis veze izvođača: %w", err)
		}
		if _, err := r.rec.Record(ctx, tx, EntityContractorAssignments, w.ID, w); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteContractor uklanja izvođača i njegove veze s površine; u knjizi ostaju arhivirani
func (r *OrgRepository) DeleteContractor(ctx context.Context, id string) error {
	c, err := r.GetContractor(ctx, id)
	if err != nil {
		return err
	}
	if c == nil {
		return fmt.Errorf("izvođač nije pronađen")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, a := range c.Assignments {
		if _, err := tx.ExecContext(ctx, `DELETE FROM contractor_assignments WHERE id = ?`, a.ID); err != nil {
			return err
		}
		if _, err := r.rec.Archive(ctx, tx, EntityContractorAssignments, a.ID, a); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM contractors WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := r.rec.Archive(ctx, tx, EntityContractors, id, c); err != nil {
		return err
	}
	return tx.Commit()
}

// ContractorIndex je tko gdje radi: po sektoru (cijeli sektor) i po području
type ContractorIndex struct {
	BySector map[string][]models.Contractor
	ByArea   map[int][]models.Contractor
}

// ContractorIndex slaže aktivne izvođače po mjestu rada, za tablice ustroja
func (r *OrgRepository) ContractorIndex(ctx context.Context) (ContractorIndex, error) {
	idx := ContractorIndex{BySector: map[string][]models.Contractor{}, ByArea: map[int][]models.Contractor{}}
	all, err := r.ListContractors(ctx)
	if err != nil {
		return idx, err
	}
	for _, c := range all {
		if !c.Active {
			continue
		}
		for _, a := range c.Assignments {
			if a.AreaID > 0 {
				idx.ByArea[a.AreaID] = append(idx.ByArea[a.AreaID], c)
			} else {
				idx.BySector[a.SectorID] = append(idx.BySector[a.SectorID], c)
			}
		}
	}
	return idx, nil
}
