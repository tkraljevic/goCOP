package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// EntitySections je naziv entiteta u knjizi verzija. Dionica je jedan
// dokument s poddionicama; kazala veza (postaje, objekti, teritorij) izvode
// se iz njega i ne putuju zasebno.
const EntitySections = "sections"

type SectionRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewSectionRepository(db *sql.DB, rec *ledger.Recorder) *SectionRepository {
	return &SectionRepository{db: db, rec: rec}
}

const sectionSelect = `
	SELECT s.code, s.area_id, s.sector_id, s.description, s.description_custom, s.length_km, s.embankment_km,
	       s.notes, s.parts, s.created_at, s.updated_at,
	       COALESCE(a.name, '') as area_name, COALESCE(sec.name, '') as sector_name,
	       s.watercourse_code, COALESCE(w.name, '') as watercourse_name
	FROM sections s
	LEFT JOIN areas a ON s.area_id = a.id
	LEFT JOIN sectors sec ON s.sector_id = sec.id
	LEFT JOIN watercourses w ON w.code = s.watercourse_code
`

// scanSection čita jedan redak dionice s poddionicama
func scanSection(scanner interface{ Scan(...any) error }) (models.Section, error) {
	var sec models.Section
	var partsJSON, notes sql.NullString
	var custom int
	err := scanner.Scan(
		&sec.Code, &sec.AreaID, &sec.SectorID, &sec.Description, &custom, &sec.LengthKm, &sec.EmbankmentKm,
		&notes, &partsJSON, &sec.CreatedAt, &sec.UpdatedAt,
		&sec.AreaName, &sec.SectorName, &sec.WatercourseCode, &sec.WatercourseName,
	)
	if err != nil {
		return sec, err
	}
	sec.DescriptionCustom = custom != 0
	sec.Notes = notes.String
	if partsJSON.Valid && partsJSON.String != "" {
		_ = json.Unmarshal([]byte(partsJSON.String), &sec.Parts)
	}
	if sec.Parts == nil {
		sec.Parts = []models.SectionPart{}
	}
	return sec, nil
}

// getSectionTx čita dionicu unutar transakcije — za verziju nakon upisa
func getSectionTx(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, code string) (models.Section, error) {
	return scanSection(q.QueryRowContext(ctx, sectionSelect+` WHERE s.code = ?`, code))
}

// ListSections vraća dionice uz opcionalne filtre po sektoru, području i ključnoj riječi
func (r *SectionRepository) ListSections(sectorID string, areaID int, search string) ([]models.Section, error) {
	query := sectionSelect + ` WHERE 1=1`
	var args []any

	if sectorID != "" {
		query += " AND s.sector_id = ?"
		args = append(args, sectorID)
	}
	if areaID > 0 {
		query += " AND s.area_id = ?"
		args = append(args, areaID)
	}
	if s := strings.TrimSpace(search); s != "" {
		like := "%" + s + "%"
		query += ` AND (s.code LIKE ? OR s.description LIKE ? OR w.name LIKE ? OR s.protected_area LIKE ? OR s.parts LIKE ? OR s.notes LIKE ?)`
		args = append(args, like, like, like, like, like, like)
	}
	query += " ORDER BY s.sector_id ASC, s.area_id ASC, s.code ASC"

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvatu dionica: %w", err)
	}
	defer rows.Close()

	var sections []models.Section
	for rows.Next() {
		sec, err := scanSection(rows)
		if err != nil {
			return nil, fmt.Errorf("greška pri skeniranju dionice: %w", err)
		}
		sections = append(sections, sec)
	}
	return sections, nil
}

// GetSectionByCode dohvaća pojedinačnu dionicu sa svim detaljima
func (r *SectionRepository) GetSectionByCode(code string) (*models.Section, error) {
	sec, err := scanSection(r.db.QueryRow(sectionSelect+` WHERE s.code = ?`, code))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvatu dionice %s: %w", code, err)
	}
	r.decorate(&sec)
	return &sec, nil
}

// decorate puni nazive iz registara uz poddionice: vode, objekte
func (r *SectionRepository) decorate(sec *models.Section) {
	for i := range sec.Parts {
		p := &sec.Parts[i]
		if p.WatercourseCode != "" {
			r.db.QueryRow(`SELECT official_name FROM watercourses WHERE code = ?`, p.WatercourseCode).Scan(&p.WatercourseName)
		}
		for j := range p.Objects {
			if id := p.Objects[j].StructureID; id != "" {
				r.db.QueryRow(`SELECT name, kind FROM structures WHERE id = ?`, id).Scan(&p.Objects[j].StructureName, &p.Objects[j].StructureKind)
			}
		}
		for j := range p.Embankments {
			if id := p.Embankments[j].StructureID; id != "" {
				r.db.QueryRow(`SELECT kind FROM structures WHERE id = ?`, id).Scan(&p.Embankments[j].StructureKind)
			}
		}
	}
}

// SaveSection upisuje novu ili izmijenjenu dionicu s poddionicama, obnavlja
// kazala i bilježi verziju. Sektor slijedi iz područja.
func (r *SectionRepository) SaveSection(ctx context.Context, s *models.Section) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if s.SectorID == "" {
		if err := tx.QueryRowContext(ctx, `SELECT sector_id FROM areas WHERE id = ?`, s.AreaID).Scan(&s.SectorID); err != nil {
			return fmt.Errorf("branjeno područje %d ne postoji", s.AreaID)
		}
	}
	if cur, err := getSectionTx(ctx, tx, s.Code); err == nil {
		s.CreatedAt = cur.CreatedAt
	} else if err != sql.ErrNoRows {
		return err
	}
	// objekti po nazivu na registar; nasip ili brana koje registar nema upisuje se u njega
	linker, err := db.NewLinker(ctx, tx)
	if err != nil {
		return err
	}
	linker.Origin = "RUČNI_UNOS"
	if err := linker.LinkRegistries(ctx, s); err != nil {
		return err
	}
	for _, id := range linker.Created {
		st, err := getStructureTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := r.rec.Record(ctx, tx, EntityStructures, id, st); err != nil {
			return err
		}
	}
	s.UpdatedAt = "" // vlastiti upis nosi sadašnje vrijeme
	if err := db.WriteSection(ctx, tx, s); err != nil {
		return err
	}
	saved, err := getSectionTx(ctx, tx, s.Code)
	if err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntitySections, s.Code, saved); err != nil {
		return err
	}
	return tx.Commit()
}

// GetSectionPersonnel pronalazi sve djelatnike vezane uz dionicu i njezino branjeno područje
func (r *SectionRepository) GetSectionPersonnel(code string, areaID int, sectorID string) ([]models.SectionOfficer, error) {
	query := `
		SELECT DISTINCT u.id, u.full_name, u.title, d.title as duty_title, d.role,
		       u.phone, u.mobile_phone, u.email, u.org_name
		FROM duties d
		JOIN users u ON d.user_id = u.id
		WHERE d.is_active = 1
		  AND (
		      -- Šifra se traži kao cijela stavka popisa, ne kao dio teksta:
		      -- "B.34.1" je inače nalazio i B.34.10 i B.34.12, pa je kartica
		      -- dionice pokazivala rukovoditelje susjednih dionica.
		      (',' || REPLACE(d.section_codes, ' ', '') || ',') LIKE ? OR
		      (d.area_id = ? AND d.role IN ('WATER_GUARD', 'MACHINIST', 'AREA_LEADER', 'AREA_DEPUTY', 'CONTRACT_OFFICER_A2', 'CONTRACT_OFFICER_A3', 'SERVICE_LEADER_FOREMAN')) OR
		      (d.sector_id = ? AND d.role IN ('SECTOR_LEADER', 'SECTOR_DEPUTY', 'COP_LEADER', 'COP_DEPUTY'))
		  )
		ORDER BY u.full_name ASC
	`
	codeLike := "%," + code + ",%"
	rows, err := r.db.Query(query, codeLike, areaID, sectorID)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvatu osoblja za dionicu: %w", err)
	}
	defer rows.Close()

	var officers []models.SectionOfficer
	for rows.Next() {
		var o models.SectionOfficer
		var roleStr string
		var title, phone, mob, email, org sql.NullString
		if err := rows.Scan(&o.UserID, &o.FullName, &title, &o.DutyTitle, &roleStr, &phone, &mob, &email, &org); err != nil {
			return nil, err
		}
		o.Title, o.Phone, o.MobilePhone, o.Email, o.OrgName = title.String, phone.String, mob.String, email.String, org.String
		o.Role = roleStr
		o.RoleLabel = models.Role(roleStr).Label()
		o.RoleGroup = models.Role(roleStr).GroupLabel()
		o.Rank = models.Role(roleStr).Rank()
		officers = append(officers, o)
	}
	// Poredak ide odozgo: uprava organizacije, sektor, područje, dionica, pa
	// teren. Unutar razine ide po težini uloge, kako ih slaže katalog —
	// rukovoditelj pred zamjenikom — pa tek onda po imenu. Razinu i težinu zna
	// katalog, a ne baza, pa se popis slaže ovdje.
	sort.SliceStable(officers, func(i, j int) bool {
		a, b := officers[i], officers[j]
		if a.Rank != b.Rank {
			return a.Rank < b.Rank
		}
		ia, ib := models.Role(a.Role).CatalogIndex(), models.Role(b.Role).CatalogIndex()
		if ia != ib {
			return ia < ib
		}
		return a.FullName < b.FullName
	})
	return spojiDuznosti(officers), nil
}

// spojiDuznosti sažima istu osobu s više dužnosti na istoj razini u jedan
// zapis: Mario Spajić je i zamjenik rukovoditelja sektora i voditelj centra, a
// na kartici je jedan čovjek s jednim brojem telefona. Preko razina se ne
// sažima — na razini područja i na razini dionice odgovara na različito
// pitanje.
func spojiDuznosti(officers []models.SectionOfficer) []models.SectionOfficer {
	var out []models.SectionOfficer
	mjesto := map[string]int{}
	for _, o := range officers {
		kljuc := o.UserID + "|" + strconv.Itoa(o.Rank)
		if i, ok := mjesto[kljuc]; ok {
			if o.DutyTitle != "" && !strings.Contains(out[i].DutyTitle, o.DutyTitle) {
				out[i].DutyTitle += ", " + o.DutyTitle
			}
			continue
		}
		mjesto[kljuc] = len(out)
		out = append(out, o)
	}
	return out
}
