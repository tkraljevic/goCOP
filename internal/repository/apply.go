package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// ApplyVersions osvježava površinu (obične tablice) iz verzija primljenih
// s drugog čvora. Ovo je jedino mjesto koje piše u sinkronizirane tablice
// BEZ Recordera — namjerno: verzije već postoje u knjizi, primljene su, i
// stvaranje novih bi ih samo umnožilo u beskraj između čvorova.
//
// Za svaki zapis primjenjuje se samo ako je primljena verzija ujedno
// najnovija koju ovaj čvor drži. Starija verzija koja stigne zaobilazno
// ostaje u povijesti, ali ne vraća površinu unatrag.
// ReadingHistoryPolicy kaže koliko povijesti očitanja ovaj čvor drži.
// Months je razdoblje koje se drži za sve letve (0 = sve, bez ograničenja),
// a Followed su letve čiju povijest čvor drži u cijelosti — obično onih
// nekoliko koje čovjeka za tim računalom zaista zanimaju.
//
// Ograda vrijedi samo za ono što stiže razmjenom. Što čvor sam upiše ili
// uveze, ostaje kod njega bez obzira na razdoblje.
type ReadingHistoryPolicy struct {
	Months   int
	Followed map[string]bool
}

var readingPolicy atomic.Pointer[ReadingHistoryPolicy]

// SetReadingHistoryPolicy postavlja politiku ovog čvora
func SetReadingHistoryPolicy(p ReadingHistoryPolicy) {
	readingPolicy.Store(&p)
}

// KeepReadingVersion javlja hoće li čvor uopće zadržati primljeno očitanje.
// Koristi je i sloj razmjene, prije upisa u knjigu, pa odbačeno očitanje ne
// zauzima mjesto ni u knjizi ni na površini.
func KeepReadingVersion(payload []byte) bool {
	p := readingPolicy.Load()
	if p == nil || p.Months <= 0 {
		return true
	}
	var rd struct {
		MeasuredAt  time.Time `json:"measured_at"`
		StationID   string    `json:"station_id"`
		StructureID string    `json:"structure_id"`
	}
	if err := json.Unmarshal(payload, &rd); err != nil || rd.MeasuredAt.IsZero() {
		return true
	}
	if rd.MeasuredAt.After(time.Now().AddDate(0, -p.Months, 0)) {
		return true
	}
	key := (models.Reading{StationID: rd.StationID, StructureID: rd.StructureID}).GaugeKey()
	return p.Followed[key]
}

// KeepVersion javlja želi li čvor primljenu verziju; ograda za sada vrijedi
// samo za očitanja, sve ostalo je maleno i drže ga svi čvorovi
func KeepVersion(v ledger.Version) bool {
	if v.Entity != EntityReadings {
		return true
	}
	return KeepReadingVersion(v.Payload)
}

func ApplyVersions(ctx context.Context, db *sql.DB, rec *ledger.Recorder, versions []ledger.Version) error {
	// zadnja primljena verzija po zapisu — ostale su već u povijesti
	latestReceived := map[string]ledger.Version{}
	for _, v := range versions {
		key := v.Entity + "|" + v.EntityID
		if cur, ok := latestReceived[key]; !ok || v.VersionID > cur.VersionID {
			latestReceived[key] = v
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, v := range latestReceived {
		top, err := rec.Latest(ctx, v.Entity, v.EntityID)
		if err != nil {
			return err
		}
		if top.VersionID != v.VersionID {
			continue // netko je već zapisao noviju; površina je već njezina
		}
		if err := applyOne(ctx, tx, v); err != nil {
			return fmt.Errorf("primjena verzije %s (%s/%s): %w", v.VersionID, v.Entity, v.EntityID, err)
		}
	}

	return tx.Commit()
}

func applyOne(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	if v.Archived {
		return removeFromSurface(ctx, tx, v)
	}

	switch v.Entity {
	case EntityStations:
		var st models.Station
		if err := json.Unmarshal(v.Payload, &st); err != nil {
			return err
		}
		return upsertStation(ctx, tx, st)

	case EntitySections:
		var sec models.Section
		if err := json.Unmarshal(v.Payload, &sec); err != nil {
			return err
		}
		return upsertSection(ctx, tx, sec)

	case EntityWatercourses:
		var w models.Watercourse
		if err := json.Unmarshal(v.Payload, &w); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO watercourses (code, official_name, name, kind, category, subcategory, wiki_slug, origin,
				length_km, basin_km2, avg_flow_m3s, source, mouth, flows_into)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(code) DO UPDATE SET
				official_name = excluded.official_name, name = excluded.name, kind = excluded.kind,
				category = excluded.category, subcategory = excluded.subcategory, wiki_slug = excluded.wiki_slug,
				origin = excluded.origin, length_km = excluded.length_km, basin_km2 = excluded.basin_km2,
				avg_flow_m3s = excluded.avg_flow_m3s, source = excluded.source, mouth = excluded.mouth,
				flows_into = excluded.flows_into
		`, w.Code, w.OfficialName, w.Name, w.Kind, w.Category, w.Subcategory, w.WikiSlug, w.Origin,
			w.LengthKm, w.BasinKm2, w.AvgFlowM3S, w.Source, w.Mouth, w.FlowsInto)
		return err

	case EntityMaintainedWaters:
		var m models.MaintainedWater
		if err := json.Unmarshal(v.Payload, &m); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO maintained_waters (id, area_id, program, watercourse_code, structure_id, name, seq, water_order, water_group,
				kind, source, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				area_id = excluded.area_id, program = excluded.program, watercourse_code = excluded.watercourse_code, structure_id = excluded.structure_id,
				name = excluded.name, seq = excluded.seq, water_order = excluded.water_order, water_group = excluded.water_group,
				kind = excluded.kind, source = excluded.source, updated_at = excluded.updated_at`,
			m.ID, m.AreaID, m.ProgramOf(), m.WatercourseCode, m.StructureID, m.Name, m.Seq, m.Order, m.Group,
			m.Kind, m.Source, m.CreatedAt, m.UpdatedAt)
		return err

	case EntityWorkItems:
		var w models.WorkItem
		if err := json.Unmarshal(v.Payload, &w); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO work_items (`+workItemColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				area_id = excluded.area_id, number = excluded.number, description = excluded.description, unit = excluded.unit,
				active = excluded.active, sort_order = excluded.sort_order, origin = excluded.origin, source = excluded.source,
				updated_at = excluded.updated_at`,
			w.ID, w.AreaID, w.Number, w.Description, w.Unit, boolInt(w.Active), w.SortOrder, w.Origin, w.Source,
			w.CreatedAt, w.UpdatedAt)
		return err

	case EntityJournals:
		var j models.Journal
		if err := json.Unmarshal(v.Payload, &j); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, journalUpsert, journalArgs(&j)...)
		return err

	case EntityJournalSheets:
		var s models.JournalSheet
		if err := json.Unmarshal(v.Payload, &s); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, sheetUpsert, sheetArgs(&s)...)
		return err

	case EntityJournalEntries:
		var e models.JournalEntry
		if err := json.Unmarshal(v.Payload, &e); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, entryUpsert, entryArgs(&e)...)
		return err

	case EntityRoleModules:
		var rm models.RoleModules
		if err := json.Unmarshal(v.Payload, &rm); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO role_modules (role, modules, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(role) DO UPDATE SET modules = excluded.modules, updated_at = excluded.updated_at`,
			rm.Role, models.JoinModules(rm.Modules), rm.UpdatedAt)
		return err

	case EntityUserModules:
		var um models.UserModules
		if err := json.Unmarshal(v.Payload, &um); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO user_modules (user_id, shown, hidden, updated_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(user_id) DO UPDATE SET shown = excluded.shown, hidden = excluded.hidden, updated_at = excluded.updated_at`,
			um.UserID, models.JoinModules(um.Shown), models.JoinModules(um.Hidden), um.UpdatedAt)
		return err

	case EntityReadings:
		if !KeepReadingVersion(v.Payload) {
			return nil // izvan razdoblja koje ovaj čvor drži
		}
		var rd models.Reading
		if err := json.Unmarshal(v.Payload, &rd); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO readings (`+readingColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				station_id = excluded.station_id, structure_id = excluded.structure_id, measured_at = excluded.measured_at,
				level_cm = excluded.level_cm, level2_cm = excluded.level2_cm, source = excluded.source, origin = excluded.origin,
				source_ref = excluded.source_ref, observer = excluded.observer, user_id = excluded.user_id,
				structure_state = excluded.structure_state, gate = excluded.gate, ag_hours_1 = excluded.ag_hours_1,
				ag_hours_2 = excluded.ag_hours_2, ag_hours_3 = excluded.ag_hours_3, note = excluded.note,
				updated_at = excluded.updated_at
		`, readingArgs(&rd)...)
		return err

	case EntityStructures:
		var st models.Structure
		if err := json.Unmarshal(v.Payload, &st); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO structures (id, code, name, kind, sector_id, area_id, watercourse_code, station_id,
				zero_datum, zero_datum_system, capacity_text, start_cm, start_text, stop_cm, stop_text,
				notes, origin, latitude, longitude, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				code = excluded.code, name = excluded.name, kind = excluded.kind, sector_id = excluded.sector_id,
				area_id = excluded.area_id, watercourse_code = excluded.watercourse_code, station_id = excluded.station_id,
				zero_datum = excluded.zero_datum, zero_datum_system = excluded.zero_datum_system,
				capacity_text = excluded.capacity_text, start_cm = excluded.start_cm, start_text = excluded.start_text,
				stop_cm = excluded.stop_cm, stop_text = excluded.stop_text, notes = excluded.notes, origin = excluded.origin,
				latitude = excluded.latitude, longitude = excluded.longitude, updated_at = excluded.updated_at
		`, st.ID.String(), st.Code, st.Name, st.Kind, st.SectorID, st.AreaID, st.WatercourseCode, st.StationID,
			st.ZeroDatum, st.ZeroDatumSystem, st.CapacityText, st.StartCm, st.StartText, st.StopCm, st.StopText,
			st.Notes, st.Origin, st.Latitude, st.Longitude, st.CreatedAt, st.UpdatedAt)
		return err

	case EntityUsers:
		var uv userVersion
		if err := json.Unmarshal(v.Payload, &uv); err != nil {
			return err
		}
		u := uv.User
		u.PasswordHash = uv.PasswordHash
		var lastLogin any
		if u.LastLoginAt != nil {
			lastLogin = *u.LastLoginAt
		}
		// Lozinka: verzije zapisane prije nego što je sažetak počeo putovati
		// nemaju ga, pa se u tom slučaju čuva onaj koji na ovom čvoru već
		// stoji — inače bi sinkronizacija zaključala korisnika.
		// Prijava: uzima se kasnija od dviju, jer je svaki čvor vidio svoje.
		_, err := tx.ExecContext(ctx, `
			INSERT INTO users (id, username, password_hash, full_name, title, is_global_admin,
				must_change_password, org_type, org_name, phone, mobile_phone, short_phone, email,
				is_active, last_login_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				username = excluded.username,
				password_hash = CASE WHEN excluded.password_hash = '' THEN users.password_hash
				                     ELSE excluded.password_hash END,
				full_name = excluded.full_name, title = excluded.title, is_global_admin = excluded.is_global_admin,
				must_change_password = excluded.must_change_password, org_type = excluded.org_type,
				org_name = excluded.org_name, phone = excluded.phone, mobile_phone = excluded.mobile_phone,
				short_phone = excluded.short_phone, email = excluded.email, is_active = excluded.is_active,
				last_login_at = MAX(COALESCE(excluded.last_login_at, users.last_login_at),
				                    COALESCE(users.last_login_at, excluded.last_login_at)),
				updated_at = excluded.updated_at
		`, u.ID.String(), u.Username, u.PasswordHash, u.FullName, u.Title, boolToInt(u.IsGlobalAdmin),
			boolToInt(u.MustChangePassword), string(u.OrgType), u.OrgName, u.Phone, u.MobilePhone, u.ShortPhone, u.Email,
			boolToInt(u.IsActive), lastLogin, u.CreatedAt, u.UpdatedAt)
		return err

	case EntityDuties:
		var d models.Duty
		if err := json.Unmarshal(v.Payload, &d); err != nil {
			return err
		}
		var assignedBy *string
		if d.AssignedBy != nil {
			s := d.AssignedBy.String()
			assignedBy = &s
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO duties (id, user_id, title, role, scope_type, sector_id, area_id, section_codes,
				is_primary, is_temporary, reason, assigned_by, created_at, expires_at, is_active)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				user_id = excluded.user_id, title = excluded.title, role = excluded.role,
				scope_type = excluded.scope_type, sector_id = excluded.sector_id, area_id = excluded.area_id,
				section_codes = excluded.section_codes, is_primary = excluded.is_primary,
				is_temporary = excluded.is_temporary, reason = excluded.reason, assigned_by = excluded.assigned_by,
				expires_at = excluded.expires_at, is_active = excluded.is_active
		`, d.ID.String(), d.UserID.String(), d.Title, string(d.Role), string(d.ScopeType), d.SectorID, d.AreaID,
			d.SectionCodes, boolToInt(d.IsPrimary), boolToInt(d.IsTemporary), d.Reason, assignedBy,
			d.CreatedAt, d.ExpiresAt, boolToInt(d.IsActive))
		return err

	case EntitySectors:
		var sec models.Sector
		if err := json.Unmarshal(v.Payload, &sec); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO sectors (id, name, vgo_name, center_cop, address, phone, email)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET name = excluded.name, vgo_name = excluded.vgo_name,
				center_cop = excluded.center_cop, address = excluded.address, phone = excluded.phone, email = excluded.email
		`, sec.ID, sec.Name, sec.VgoName, sec.CenterCop, sec.Address, sec.Phone, sec.Email)
		return err

	case EntityAreas:
		var a models.Area
		if err := json.Unmarshal(v.Payload, &a); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO areas (id, sector_id, name, vgi_name, subcenter, contractor_name)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET sector_id = excluded.sector_id, name = excluded.name,
				vgi_name = excluded.vgi_name, subcenter = excluded.subcenter, contractor_name = excluded.contractor_name
		`, a.ID, a.SectorID, a.Name, a.VgiName, a.Subcenter, a.ContractorName)
		return err

	case EntityCounties:
		var c models.County
		if err := json.Unmarshal(v.Payload, &c); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO counties (id, code, name, seat, prefect, area_sqkm, population, email, phone)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				code = excluded.code, name = excluded.name, seat = excluded.seat, prefect = excluded.prefect,
				area_sqkm = excluded.area_sqkm, population = excluded.population, email = excluded.email, phone = excluded.phone
		`, c.ID, c.Code, c.Name, c.Seat, c.Prefect, c.AreaSqKm, c.Population, c.Email, c.Phone)
		return err

	case EntityMunicipalities:
		var m models.Municipality
		if err := json.Unmarshal(v.Payload, &m); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO municipalities (id, county_id, name, type, head_title, head_name, postal_code, area_sqkm, population)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				county_id = excluded.county_id, name = excluded.name, type = excluded.type,
				head_title = excluded.head_title, head_name = excluded.head_name, postal_code = excluded.postal_code,
				area_sqkm = excluded.area_sqkm, population = excluded.population
		`, m.ID, m.CountyID, m.Name, m.Type, m.HeadTitle, m.HeadName, m.PostalCode, m.AreaSqKm, m.Population)
		return err

	case EntitySettlements:
		var st models.Settlement
		if err := json.Unmarshal(v.Payload, &st); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO settlements (id, municipality_id, county_id, name, postal_code, population)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				municipality_id = excluded.municipality_id, county_id = excluded.county_id, name = excluded.name,
				postal_code = excluded.postal_code, population = excluded.population
		`, st.ID, st.MunicipalityID, st.CountyID, st.Name, st.PostalCode, st.Population)
		return err

	case "memberships":
		var m struct {
			Network   string    `json:"network"`
			DeviceID  string    `json:"deviceId"`
			DeviceKey string    `json:"deviceKey"`
			IssuedBy  string    `json:"issuedBy"`
			IssuedAt  time.Time `json:"issuedAt"`
			ExpiresAt time.Time `json:"expiresAt"`
			Signature string    `json:"signature"`
		}
		if err := json.Unmarshal(v.Payload, &m); err != nil {
			return err
		}
		// Potpis se ne provjerava ovdje nego na vratima razmjene — ovdje se
		// samo pamti što je stiglo; tuđa ili kriva potvrda nikad ne prolazi
		// provjeru i ne šteti time što postoji u tablici.
		_, err := tx.ExecContext(ctx, `
			INSERT INTO memberships (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(node_id) DO UPDATE SET
				public_key = excluded.public_key, network = excluded.network, issued_by = excluded.issued_by,
				issued_at = excluded.issued_at, expires_at = excluded.expires_at, signature = excluded.signature
		`, m.DeviceID, m.DeviceKey, m.Network, m.IssuedBy, m.IssuedAt, m.ExpiresAt, m.Signature, v.CreatedAt)
		return err

	case "peers":
		var p struct {
			NodeID       string     `json:"node_id"`
			Name         string     `json:"name"`
			PublicKey    string     `json:"public_key"`
			Addresses    []string   `json:"addresses"`
			IsBootstrap  bool       `json:"is_bootstrap"`
			LastSeen     *time.Time `json:"last_seen"`
			LastSync     *time.Time `json:"last_sync"`
			LastSyncNote string     `json:"last_sync_note"`
			CreatedAt    time.Time  `json:"created_at"`
		}
		if err := json.Unmarshal(v.Payload, &p); err != nil {
			return err
		}
		addrs, _ := json.Marshal(p.Addresses)
		// bilješke o zadnjoj sinkronizaciji opisuju odnos dva čvora, ne čvor —
		// ne prepisuju se tuđima
		_, err := tx.ExecContext(ctx, `
			INSERT INTO peers (node_id, name, public_key, addresses, is_bootstrap, last_seen, last_sync, last_sync_note, created_at)
			VALUES (?, ?, ?, ?, ?, ?, NULL, '', ?)
			ON CONFLICT(node_id) DO UPDATE SET
				name = excluded.name, public_key = excluded.public_key, addresses = excluded.addresses,
				is_bootstrap = excluded.is_bootstrap
		`, p.NodeID, p.Name, p.PublicKey, string(addrs), boolToInt(p.IsBootstrap), p.LastSeen, p.CreatedAt)
		return err
	}

	// nepoznat entitet — verzija ostaje u knjizi, površinu nema što osvježiti
	return nil
}

// removeFromSurface uklanja arhivirani zapis s površine
func removeFromSurface(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var stmt string
	var arg any = v.EntityID

	switch v.Entity {
	case EntityStations:
		stmt = `DELETE FROM stations WHERE id = ?`
	case EntitySections:
		stmt = `DELETE FROM sections WHERE code = ?`
	case EntityWatercourses:
		stmt = `DELETE FROM watercourses WHERE code = ?`
	case EntityRoleModules:
		stmt = `DELETE FROM role_modules WHERE role = ?`
	case EntityUserModules:
		stmt = `DELETE FROM user_modules WHERE user_id = ?`
	case EntityReadings:
		stmt = `DELETE FROM readings WHERE id = ?`
	case EntityJournals:
		stmt = `DELETE FROM journals WHERE id = ?`
	case EntityJournalSheets:
		stmt = `DELETE FROM journal_sheets WHERE id = ?`
	case EntityJournalEntries:
		stmt = `DELETE FROM journal_entries WHERE id = ?`
	case EntityMaintainedWaters:
		stmt = `DELETE FROM maintained_waters WHERE id = ?`
	case EntityWorkItems:
		stmt = `DELETE FROM work_items WHERE id = ?`
	case EntityStructures:
		stmt = `DELETE FROM structures WHERE id = ?`
	case EntityUsers:
		stmt = `DELETE FROM users WHERE id = ?`
	case EntityDuties:
		stmt = `DELETE FROM duties WHERE id = ?`
	case EntitySectors:
		stmt = `DELETE FROM sectors WHERE id = ?`
	case EntityAreas:
		stmt = `DELETE FROM areas WHERE id = ?`
		arg, _ = strconv.Atoi(v.EntityID)
	case EntityCounties:
		stmt = `DELETE FROM counties WHERE id = ?`
		arg, _ = strconv.Atoi(v.EntityID)
	case EntityMunicipalities:
		stmt = `DELETE FROM municipalities WHERE id = ?`
		arg, _ = strconv.Atoi(v.EntityID)
	case EntitySettlements:
		stmt = `DELETE FROM settlements WHERE id = ?`
		arg, _ = strconv.Atoi(v.EntityID)
	case "peers":
		stmt = `DELETE FROM peers WHERE node_id = ?`
	case "memberships":
		stmt = `DELETE FROM memberships WHERE node_id = ?`
	default:
		return nil
	}

	_, err := tx.ExecContext(ctx, stmt, arg)
	return err
}

func upsertStation(ctx context.Context, tx *sql.Tx, st models.Station) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO stations (
			id, code, name, watercourse, watercourse_code, watercourse_source, water_area, stationing,
			zero_datum, zero_datum_system, zero_datum_new, zero_datum_new_system,
			prep_cm, prep_raw, regular_cm, regular_raw, emergency_cm, emergency_raw, state_cm, state_raw,
			record_cm, record_raw, notes, source_name, needs_review, review_note,
			latitude, longitude, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			code = excluded.code, name = excluded.name, watercourse = excluded.watercourse,
			watercourse_code = excluded.watercourse_code, watercourse_source = excluded.watercourse_source,
			water_area = excluded.water_area, stationing = excluded.stationing,
			zero_datum = excluded.zero_datum, zero_datum_system = excluded.zero_datum_system,
			zero_datum_new = excluded.zero_datum_new, zero_datum_new_system = excluded.zero_datum_new_system,
			prep_cm = excluded.prep_cm, prep_raw = excluded.prep_raw, regular_cm = excluded.regular_cm,
			regular_raw = excluded.regular_raw, emergency_cm = excluded.emergency_cm, emergency_raw = excluded.emergency_raw,
			state_cm = excluded.state_cm, state_raw = excluded.state_raw, record_cm = excluded.record_cm,
			record_raw = excluded.record_raw, notes = excluded.notes, source_name = excluded.source_name,
			needs_review = excluded.needs_review, review_note = excluded.review_note,
			latitude = excluded.latitude, longitude = excluded.longitude, updated_at = excluded.updated_at
	`,
		st.ID.String(), st.Code, st.Name, st.Watercourse, st.WatercourseCode, st.WatercourseSource, st.WaterArea, st.Stationing,
		st.ZeroDatum, defaultSystem(st.ZeroDatumSystem, models.ZeroDatumSystemOld),
		st.ZeroDatumNew, defaultSystem(st.ZeroDatumNewSystem, models.ZeroDatumSystemNew),
		st.Prep.Cm, st.Prep.Raw, st.Regular.Cm, st.Regular.Raw, st.Emergency.Cm, st.Emergency.Raw, st.State.Cm, st.State.Raw,
		st.Record.Cm, st.Record.Raw, st.Notes, st.SourceName, boolToInt(st.NeedsReview), st.ReviewNote,
		st.Latitude, st.Longitude, st.CreatedAt, st.UpdatedAt,
	)
	return err
}

func upsertSection(ctx context.Context, tx *sql.Tx, s models.Section) error {
	// dionica stiže kao cijeli dokument; kazala veza se iz njega obnavljaju
	return db.WriteSection(ctx, tx, &s)
}
