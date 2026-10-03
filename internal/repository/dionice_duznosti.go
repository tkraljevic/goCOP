package repository

// PodrucjeDionice je područje i sektor dionice iz registra
type PodrucjeDionice struct {
	AreaID   int
	SectorID string
}

// PodrucjaDionica vraća područje i sektor svake dionice iz registra, po
// šifri; dužnost s dionicama mora ih uzeti iz jednog područja (normalizeScope)
func (r *UserRepository) PodrucjaDionica() (map[string]PodrucjeDionice, error) {
	rows, err := r.db.Query(`SELECT code, area_id, sector_id FROM sections`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]PodrucjeDionice{}
	for rows.Next() {
		var code string
		var d PodrucjeDionice
		if err := rows.Scan(&code, &d.AreaID, &d.SectorID); err != nil {
			return nil, err
		}
		m[code] = d
	}
	return m, rows.Err()
}
