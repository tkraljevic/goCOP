package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gocop/internal/hydro"
	"gocop/internal/models"
)

// Dionica je jedan dokument: šifra, područje i poddionice sa svime što se na
// njima štiti. Tablice section_stations, section_structures i
// section_territories su IZVEDENI kazala iz tog dokumenta — obnavljaju se pri
// svakom upisu dionice, ovdje i pri primjeni verzije s drugog čvora, i nitko
// ih ne uređuje zasebno. Isto vrijedi za stupce description, watercourse_code,
// bank, rkm_from i rkm_to u tablici sections: prva poddionica, radi popisa.

// Execer je ono što upis treba od transakcije ili baze
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// WriteSection upisuje dionicu (novu ili izmijenjenu) s izvedenim stupcima i
// obnavlja kazala. Verziju u knjizi ne piše — to je posao pozivatelja.
func WriteSection(ctx context.Context, tx Execer, sec *models.Section) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if sec.CreatedAt == "" {
		sec.CreatedAt = now
	}
	if sec.UpdatedAt == "" { // primljena verzija nosi svoje vrijeme; vlastiti upis ga briše
		sec.UpdatedAt = now
	}
	for i := range sec.Parts {
		p := &sec.Parts[i]
		p.Seq = i + 1
		// Brojčana stacionaža objekta izvodi se iz zapisa kad je nema. Zapis
		// "rkm 1428+010" je ono što piše u Privitku, broj je ono po čemu
		// program raspoređuje objekte po nasipima; jedan klijent koji broj ne
		// pošalje inače ga zauvijek izbriše.
		for j := range p.Objects {
			o := &p.Objects[j]
			if o.Stationing == nil && o.StationingText != "" {
				if km, ok := hydro.ParseStationingKm(o.StationingText); ok {
					o.Stationing = &km
				}
			}
		}
		// naziv vode služi složenom opisu; u zapis ne ide, čita se iz registra
		if p.WatercourseCode != "" && p.WatercourseName == "" {
			_ = tx.QueryRowContext(ctx, `SELECT official_name FROM watercourses WHERE code = ?`, p.WatercourseCode).Scan(&p.WatercourseName)
		}
	}
	if !sec.DescriptionCustom {
		if d := sec.ComposeDescription(); d != "" {
			sec.Description = d
		}
	}
	first := sec.FirstPart()
	stored := make([]models.SectionPart, len(sec.Parts))
	copy(stored, sec.Parts)
	for i := range stored {
		stored[i].WatercourseName = ""
	}
	partsJSON, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	if sec.Parts == nil {
		partsJSON = []byte("[]")
	}
	protected := strings.TrimSpace(strings.Join(protectedTexts(sec), "; "))
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sections (code, area_id, sector_id, description, description_custom, length_km, embankment_km,
			watercourse_code, bank, rkm_from, rkm_to, protected_area, notes, parts, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(code) DO UPDATE SET
			area_id = excluded.area_id, sector_id = excluded.sector_id, description = excluded.description,
			description_custom = excluded.description_custom, length_km = excluded.length_km, embankment_km = excluded.embankment_km,
			watercourse_code = excluded.watercourse_code, bank = excluded.bank, rkm_from = excluded.rkm_from, rkm_to = excluded.rkm_to,
			protected_area = excluded.protected_area, notes = excluded.notes, parts = excluded.parts, updated_at = excluded.updated_at`,
		sec.Code, sec.AreaID, sec.SectorID, sec.Description, boolInt(sec.DescriptionCustom), sec.LengthKm, sec.EmbankmentKm,
		first.WatercourseCode, first.Bank, first.KmFrom, first.KmTo, protected, sec.Notes, string(partsJSON), sec.CreatedAt, sec.UpdatedAt,
	); err != nil {
		return fmt.Errorf("upis dionice %s: %w", sec.Code, err)
	}
	return RebuildSectionIndexes(ctx, tx, sec)
}

func protectedTexts(sec *models.Section) []string {
	var out []string
	for _, p := range sec.Parts {
		if t := strings.TrimSpace(p.ProtectedText); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// RebuildSectionIndexes obnavlja kazala jedne dionice iz njezinih poddionica
func RebuildSectionIndexes(ctx context.Context, tx Execer, sec *models.Section) error {
	now := time.Now().UTC()
	for _, stmt := range []string{
		`DELETE FROM section_stations WHERE section_code = ?`,
		`DELETE FROM section_structures WHERE section_code = ?`,
		`DELETE FROM section_territories WHERE section_code = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, sec.Code); err != nil {
			return err
		}
	}
	for _, id := range sec.AllStationIDs() {
		linkID := StableID("section_station", sec.Code+"|"+id).String()
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO section_stations (id, section_code, station_id, created_at) VALUES (?, ?, ?, ?)`,
			linkID, sec.Code, id, now); err != nil {
			return err
		}
	}
	for _, id := range sec.AllStructureIDs() {
		linkID := StableID("section_structure", sec.Code+"|"+id).String()
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO section_structures (id, section_code, structure_id, created_at) VALUES (?, ?, ?, ?)`,
			linkID, sec.Code, id, now); err != nil {
			return err
		}
	}
	for _, t := range sec.AllTerritories() {
		linkID := StableID("section_territory", sec.Code+"|"+t.Key()).String()
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO section_territories (id, section_code, county_id, municipality_id, settlement_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			linkID, sec.Code, t.CountyID, t.MunicipalityID, t.SettlementID, now); err != nil {
			return err
		}
	}
	return nil
}

// Linker veže poddionice na registre. Gradi se jednom nad bazom i primjenjuje
// na više dionica: vode po nazivu, postaje po nazivu vodomjera, teritorij iz
// naslijeđenih veza dionice, nasipi i objekti po nazivu unutar područja.
type Linker struct {
	tx        Execer
	waters    map[string][]hydro.Candidate
	stations  map[string][]string // ključ naziva → id postaje
	areas     map[int]models.Area
	territory map[string][]territoryRow // dionica → naslijeđene veze
	names     map[int]string            // id općine/naselja → naziv (za razdiobu po poddionicama)
	Added     struct{ Waters, Embankments int }
	Created   []string // identiteti objekata koje je vezanje upisalo u registar
	Origin    string   // podrijetlo objekata koje vezanje upisuje; prazno je dokumentacija dionica
}

type territoryRow struct {
	county, municipality int
	settlement           *int
	muniName, settName   string
}

// NewLinker učitava registre potrebne za vezanje
func NewLinker(ctx context.Context, tx Execer) (*Linker, error) {
	l := &Linker{tx: tx, stations: map[string][]string{}, areas: map[int]models.Area{}, territory: map[string][]territoryRow{}}
	var err error
	if l.waters, err = watercourseIndexTx(ctx, tx); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, name FROM stations`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		k := hydro.StationKey(name)
		l.stations[k] = append(l.stations[k], id)
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, `SELECT id, sector_id, name, vgi_name, subcenter FROM areas`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a models.Area
		var vgi, sub sql.NullString
		if err := rows.Scan(&a.ID, &a.SectorID, &a.Name, &vgi, &sub); err != nil {
			rows.Close()
			return nil, err
		}
		a.VgiName, a.Subcenter = vgi.String, sub.String
		l.areas[a.ID] = a
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, `SELECT st.section_code, st.county_id, st.municipality_id, st.settlement_id,
		COALESCE(m.name, ''), COALESCE(s.name, '')
		FROM section_territories st LEFT JOIN municipalities m ON m.id = st.municipality_id LEFT JOIN settlements s ON s.id = st.settlement_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var code string
		var r territoryRow
		var sett sql.NullInt64
		if err := rows.Scan(&code, &r.county, &r.municipality, &sett, &r.muniName, &r.settName); err != nil {
			rows.Close()
			return nil, err
		}
		if sett.Valid {
			v := int(sett.Int64)
			r.settlement = &v
		}
		l.territory[code] = append(l.territory[code], r)
	}
	rows.Close()
	return l, nil
}

func watercourseIndexTx(ctx context.Context, tx Execer) (map[string][]hydro.Candidate, error) {
	rows, err := tx.QueryContext(ctx, `SELECT code, name, official_name, kind FROM watercourses`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	index := map[string][]hydro.Candidate{}
	for rows.Next() {
		var code, name, official, kind string
		if err := rows.Scan(&code, &name, &official, &kind); err != nil {
			return nil, err
		}
		qualifier := hydro.Qualifier(official)
		seen := map[string]bool{}
		for _, n := range []string{name, official} {
			key := hydro.WatercourseKey(n)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			dup := false
			for _, c := range index[key] {
				if c.Code == code {
					dup = true
				}
			}
			if !dup {
				index[key] = append(index[key], hydro.Candidate{Code: code, Kind: kind, Qualifier: qualifier})
			}
		}
	}
	return index, rows.Err()
}

// Link veže sve što se u dionici da vezati, bez brisanja postojećih veza:
// što je već vezano ostaje, prazno se popunjava
func (l *Linker) Link(ctx context.Context, sec *models.Section) error {
	if err := l.LinkRegistries(ctx, sec); err != nil {
		return err
	}
	for i := range sec.Parts {
		l.linkStations(&sec.Parts[i])
	}
	l.linkTerritories(sec)
	return nil
}

func (l *Linker) origin() string {
	if l.Origin != "" {
		return l.Origin
	}
	return "DOKUMENTACIJA"
}

// LinkRegistries veže poddionice na vode i objekte. Vodomjere iz
// dokumentacije i naslijeđeni teritorij ne dira: ono što je čovjek u
// obrascu maknuo ne vraća se
func (l *Linker) LinkRegistries(ctx context.Context, sec *models.Section) error {
	area, ok := l.areas[sec.AreaID]
	if !ok {
		return fmt.Errorf("dionica %s: područje %d ne postoji", sec.Code, sec.AreaID)
	}
	if sec.SectorID == "" {
		sec.SectorID = area.SectorID
	}
	areaText := area.Name + " " + area.VgiName + " " + area.Subcenter
	for i := range sec.Parts {
		p := &sec.Parts[i]
		if err := l.linkWater(ctx, p, areaText); err != nil {
			return err
		}
		if err := l.linkStructures(ctx, p, area); err != nil {
			return err
		}
	}
	return nil
}

// linkWater veže poddionicu na vodu iz opisa; vodu koje registar nema upisuje
func (l *Linker) linkWater(ctx context.Context, p *models.SectionPart, areaText string) error {
	if p.WatercourseCode != "" || p.Description == "" {
		return nil
	}
	name, kind := hydro.ParseWatercourseWithKind(p.Description)
	if strings.TrimSpace(name) == "" {
		return nil
	}
	code := hydro.ResolveWatercourse(l.waters, name, kind, areaText)
	if code == "" && len(l.waters[hydro.WatercourseKey(name)]) == 0 {
		official := strings.TrimSpace(kind + " " + name)
		code = hydro.WatercourseCode(official)
		if _, err := l.tx.ExecContext(ctx, `INSERT INTO watercourses (code, official_name, name, kind, category, subcategory, wiki_slug, origin)
			VALUES (?, ?, ?, ?, '', '', '', ?) ON CONFLICT(code) DO NOTHING`, code, official, name, kind, models.WatercourseOriginDocumentation); err != nil {
			return fmt.Errorf("voda %q iz dokumentacije: %w", official, err)
		}
		l.waters[hydro.WatercourseKey(name)] = []hydro.Candidate{{Code: code, Kind: kind}}
		l.Added.Waters++
	}
	p.WatercourseCode = code
	return nil
}

// linkStations veže vodomjere poddionice na postaje po nazivu
func (l *Linker) linkStations(p *models.SectionPart) {
	have := map[string]bool{}
	for _, id := range p.StationIDs {
		have[id] = true
	}
	for _, g := range p.Gauges {
		if !g.IsGauge() {
			continue
		}
		name, _ := hydro.ParseStationName(g.StationName)
		key := hydro.StationKey(name)
		if key == "" {
			key = hydro.StationKey(g.StationName)
		}
		ids := l.stations[key]
		if len(ids) == 0 {
			if base, _, ok := strings.Cut(key, "-"); ok {
				ids = l.stations[strings.TrimSpace(base)]
			}
		}
		for _, id := range ids {
			if !have[id] {
				have[id] = true
				p.StationIDs = append(p.StationIDs, id)
			}
			break
		}
	}
}

// linkStructures veže nasipe i brane na registar objekata (upisuje ih kad ih
// nema) i naše objekte po nazivu; mostovi i propusti ostaju samo redak
func (l *Linker) linkStructures(ctx context.Context, p *models.SectionPart, area models.Area) error {
	type st struct{ id, name, kind string }
	rows, err := l.tx.QueryContext(ctx, `SELECT id, name, kind FROM structures WHERE area_id = ?`, area.ID)
	if err != nil {
		return err
	}
	var all []st
	for rows.Next() {
		var s st
		if err := rows.Scan(&s.id, &s.name, &s.kind); err != nil {
			rows.Close()
			return err
		}
		all = append(all, s)
	}
	rows.Close()

	for i := range p.Embankments {
		e := &p.Embankments[i]
		if e.StructureID != "" || strings.TrimSpace(e.Name) == "" {
			continue
		}
		kind := models.StructureKindEmbankment
		if strings.Contains(strings.ToLower(e.Name), "brana") {
			kind = models.StructureKindDam
		}
		want := normalizeName(e.Name)
		for _, s := range all {
			if (s.kind == models.StructureKindEmbankment || s.kind == models.StructureKindDam) && normalizeName(s.name) == want {
				e.StructureID = s.id
				break
			}
		}
		if e.StructureID != "" {
			continue
		}
		code := fmt.Sprintf("bp%d-%s", area.ID, hydro.Slug(e.Name))
		id := StableID("structure", code).String()
		var n int
		if err := l.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM structures WHERE code = ?`, code).Scan(&n); err != nil {
			return err
		}
		if n > 0 { // ista šifra, drukčiji naziv — dodaje se razlikovni nastavak
			code = fmt.Sprintf("%s-%d", code, len(all)+1)
			id = StableID("structure", code).String()
		}
		now := time.Now().UTC()
		if _, err := l.tx.ExecContext(ctx, `INSERT INTO structures (id, code, name, kind, sector_id, area_id, watercourse_code, station_id,
			zero_datum, zero_datum_system, capacity_text, start_cm, start_text, stop_cm, stop_text, notes, origin, latitude, longitude, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, '', NULL, '', '', NULL, '', NULL, '', '', ?, NULL, NULL, ?, ?)`,
			id, code, strings.TrimSpace(e.Name), kind, area.SectorID, area.ID, p.WatercourseCode, l.origin(), now, now); err != nil {
			return fmt.Errorf("nasip %q: %w", e.Name, err)
		}
		all = append(all, st{id, e.Name, kind})
		e.StructureID = id
		l.Added.Embankments++
		l.Created = append(l.Created, id)
	}

	for i := range p.Objects {
		o := &p.Objects[i]
		if o.StructureID != "" {
			continue
		}
		got := normalizeName(o.Name)
		for _, s := range all {
			if s.kind == models.StructureKindEmbankment || s.kind == models.StructureKindDam {
				continue
			}
			sn := normalizeName(s.name)
			if sn != "" && (strings.Contains(got, sn) || strings.Contains(got, "cs i ustava "+structureBaseName(s.name))) {
				o.StructureID = s.id
				break
			}
		}
	}
	return nil
}

// linkTerritories razdjeljuje naslijeđene veze dionice po poddionicama: jedna
// poddionica dobiva sve; više njih dobiva ono što im ugroženo područje spominje
func (l *Linker) linkTerritories(sec *models.Section) {
	rows := l.territory[sec.Code]
	if len(rows) == 0 || len(sec.Parts) == 0 {
		return
	}
	have := map[int]map[string]bool{}
	for i, p := range sec.Parts {
		have[i] = map[string]bool{}
		for _, t := range p.Territories {
			have[i][t.Key()] = true
		}
	}
	add := func(i int, r territoryRow) {
		t := models.PartTerritory{CountyID: r.county, MunicipalityID: r.municipality, SettlementID: r.settlement}
		if !have[i][t.Key()] {
			have[i][t.Key()] = true
			sec.Parts[i].Territories = append(sec.Parts[i].Territories, t)
		}
	}
	for _, r := range rows {
		if len(sec.Parts) == 1 {
			add(0, r)
			continue
		}
		matched := false
		for i, p := range sec.Parts {
			text := normalizeName(p.ProtectedText)
			if text == "" {
				continue
			}
			if r.settName != "" && strings.Contains(text, normalizeName(r.settName)) ||
				r.settName == "" && r.muniName != "" && strings.Contains(text, normalizeName(r.muniName)) {
				add(i, r)
				matched = true
			}
		}
		if !matched {
			add(0, r)
		}
	}
}

// LinkAllSections veže sve dionice u bazi i obnavlja kazala; poziva se pri
// punjenju, nakon što su vode, postaje, teritorij i objekti upisani
func LinkAllSections(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	linker, err := NewLinker(ctx, tx)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT code, area_id, sector_id, description, description_custom, length_km, embankment_km, notes, parts, created_at, updated_at FROM sections`)
	if err != nil {
		return err
	}
	var secs []models.Section
	for rows.Next() {
		var s models.Section
		var custom int
		var parts sql.NullString
		var notes sql.NullString
		if err := rows.Scan(&s.Code, &s.AreaID, &s.SectorID, &s.Description, &custom, &s.LengthKm, &s.EmbankmentKm, &notes, &parts, &s.CreatedAt, &s.UpdatedAt); err != nil {
			rows.Close()
			return err
		}
		s.DescriptionCustom = custom != 0
		s.Notes = notes.String
		if parts.Valid && parts.String != "" {
			_ = json.Unmarshal([]byte(parts.String), &s.Parts)
		}
		secs = append(secs, s)
	}
	rows.Close()
	for i := range secs {
		before, _ := json.Marshal(secs[i].Parts)
		if err := linker.Link(ctx, &secs[i]); err != nil {
			return err
		}
		after, _ := json.Marshal(secs[i].Parts)
		if string(before) != string(after) {
			if err := WriteSection(ctx, tx, &secs[i]); err != nil {
				return err
			}
		} else if err := RebuildSectionIndexes(ctx, tx, &secs[i]); err != nil {
			return err
		}
	}
	return tx.Commit()
}
