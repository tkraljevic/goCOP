package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// primjeneRegistara su teritorij (županije, općine, naselja i njihove
// službe), vodotoci, objekti, dionice s vezama na letve te vode i radovi u
// programu održavanja
var primjeneRegistara = map[string]primjena{
	EntityCounties:       {upis: primijeniZupaniju, brisanje: obrisiPoBroju(`DELETE FROM counties WHERE id = ?`)},
	EntityMunicipalities: {upis: primijeniOpcinu, brisanje: obrisiPoBroju(`DELETE FROM municipalities WHERE id = ?`)},
	EntitySettlements:    {upis: primijeniNaselje, brisanje: obrisiPoBroju(`DELETE FROM settlements WHERE id = ?`)},
	EntitySluzbe:         {upis: primijeniSluzbu, brisanje: obrisi(`DELETE FROM sluzbe WHERE id = ?`)},
	EntityWatercourses:   {upis: primijeniVodotok, brisanje: obrisi(`DELETE FROM watercourses WHERE code = ?`)},
	EntityStructures:     {upis: primijeniObjekt, brisanje: obrisi(`DELETE FROM structures WHERE id = ?`)},
	// s dionicom odlaze i njezina kazala, kao i pri arhiviranju na izvoru
	EntitySections: {upis: primijeniDionicu, brisanje: poIdentitetu(makniDionicuSPovrsine)},
	// veza dionice i letve dolazi s dionicom; zasebno stiže samo njezin
	// spomenik, kad se letva briše (StationRepository)
	EntitySectionStations:  {brisanje: makniVezuDioniceILetve},
	EntityMaintainedWaters: {upis: primijeniOdrzavanuVodu, brisanje: obrisi(`DELETE FROM maintained_waters WHERE id = ?`)},
	EntityWorkItems:        {upis: primijeniStavkuRadova, brisanje: obrisi(`DELETE FROM work_items WHERE id = ?`)},
}

// primijeniZupaniju stavlja primljenu županiju na površinu
func primijeniZupaniju(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var c models.County
	if err := json.Unmarshal(v.Payload, &c); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO counties (id, code, name, seat, prefect, area_sqkm, population, email, phone, website)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			code = excluded.code, name = excluded.name, seat = excluded.seat, prefect = excluded.prefect,
			area_sqkm = excluded.area_sqkm, population = excluded.population, email = excluded.email,
			phone = excluded.phone, website = excluded.website
	`, c.ID, c.Code, c.Name, c.Seat, c.Prefect, c.AreaSqKm, c.Population, c.Email, c.Phone, c.Website)
	return err
}

// primijeniOpcinu stavlja primljenu općinu ili grad na površinu
func primijeniOpcinu(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var m models.Municipality
	if err := json.Unmarshal(v.Payload, &m); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO municipalities (id, county_id, name, type, head_title, head_name, postal_code, area_sqkm, population, email, phone, website)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			county_id = excluded.county_id, name = excluded.name, type = excluded.type,
			head_title = excluded.head_title, head_name = excluded.head_name, postal_code = excluded.postal_code,
			area_sqkm = excluded.area_sqkm, population = excluded.population,
			email = excluded.email, phone = excluded.phone, website = excluded.website
	`, m.ID, m.CountyID, m.Name, m.Type, m.HeadTitle, m.HeadName, m.PostalCode, m.AreaSqKm, m.Population, m.Email, m.Phone, m.Website)
	return err
}

// primijeniNaselje stavlja primljeno naselje na površinu
func primijeniNaselje(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
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
}

// primijeniSluzbu stavlja primljenu službu županije ili grada na površinu
func primijeniSluzbu(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var x models.Sluzba
	if err := json.Unmarshal(v.Payload, &x); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, sluzbaUpsert, sluzbaArgs(&x)...)
	return err
}

// primijeniVodotok stavlja primljeni vodotok na površinu
func primijeniVodotok(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var w models.Watercourse
	if err := json.Unmarshal(v.Payload, &w); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO watercourses (code, official_name, name, kind, category, subcategory, wiki_slug, origin,
			length_km, basin_km2, avg_flow_m3s, source, mouth, flows_into, notes, geometry, extra_station_ids)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(code) DO UPDATE SET
			official_name = excluded.official_name, name = excluded.name, kind = excluded.kind,
			category = excluded.category, subcategory = excluded.subcategory, wiki_slug = excluded.wiki_slug,
			origin = excluded.origin, length_km = excluded.length_km, basin_km2 = excluded.basin_km2,
			avg_flow_m3s = excluded.avg_flow_m3s, source = excluded.source, mouth = excluded.mouth,
			flows_into = excluded.flows_into, notes = excluded.notes, geometry = excluded.geometry,
			extra_station_ids = excluded.extra_station_ids
	`, w.Code, w.OfficialName, w.Name, w.Kind, w.Category, w.Subcategory, w.WikiSlug, w.Origin,
		w.LengthKm, w.BasinKm2, w.AvgFlowM3S, w.Source, w.Mouth, w.FlowsInto, w.Notes, w.Geometry,
		ExtraStationsJSON(w.ExtraStationIDs))
	return err
}

// primijeniObjekt stavlja primljeni objekt na površinu
func primijeniObjekt(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var st models.Structure
	if err := json.Unmarshal(v.Payload, &st); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO structures (id, code, name, kind, sector_id, area_id, watercourse_code, station_id,
			zero_datum, zero_datum_system, capacity_text, start_cm, start_text, stop_cm, stop_text,
			notes, origin, latitude, longitude, created_at, updated_at, station_down_id, geometry)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			code = excluded.code, name = excluded.name, kind = excluded.kind, sector_id = excluded.sector_id,
			area_id = excluded.area_id, watercourse_code = excluded.watercourse_code, station_id = excluded.station_id,
			zero_datum = excluded.zero_datum, zero_datum_system = excluded.zero_datum_system,
			capacity_text = excluded.capacity_text, start_cm = excluded.start_cm, start_text = excluded.start_text,
			stop_cm = excluded.stop_cm, stop_text = excluded.stop_text, notes = excluded.notes, origin = excluded.origin,
			latitude = excluded.latitude, longitude = excluded.longitude, updated_at = excluded.updated_at,
			station_down_id = excluded.station_down_id, geometry = excluded.geometry
	`, st.ID.String(), st.Code, st.Name, st.Kind, st.SectorID, st.AreaID, st.WatercourseCode, st.StationID,
		st.ZeroDatum, st.ZeroDatumSystem, st.CapacityText, st.StartCm, st.StartText, st.StopCm, st.StopText,
		st.Notes, st.Origin, st.Latitude, st.Longitude, st.CreatedAt.UTC(), st.UpdatedAt.UTC(), st.StationDownID, st.Geometry)
	return err
}

// primijeniDionicu stavlja primljenu dionicu na površinu
func primijeniDionicu(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var sec models.Section
	if err := json.Unmarshal(v.Payload, &sec); err != nil {
		return err
	}
	return upsertSection(ctx, tx, sec)
}

func upsertSection(ctx context.Context, tx *sql.Tx, s models.Section) error {
	// dionica stiže kao cijeli dokument; kazala veza se iz njega obnavljaju
	return db.WriteSection(ctx, tx, &s)
}

// makniVezuDioniceILetve miče vezu dionice i letve s površine
func makniVezuDioniceILetve(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	// veza dionice i letve odlazi kad se letva briše; ključ je "dionica|letva"
	var veza struct {
		SectionCode string `json:"section_code"`
		StationID   string `json:"station_id"`
	}
	_ = json.Unmarshal(v.Payload, &veza) // nečitljiv sadržaj: veza se uzima iz oznake zapisa
	if veza.SectionCode+"|"+veza.StationID != v.EntityID {
		// sadržaj bez veze ili s vezom druge oznake: veza je oznaka zapisa
		veza.SectionCode, veza.StationID, _ = strings.Cut(v.EntityID, "|")
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM section_stations WHERE section_code = ? AND station_id = ?`,
		veza.SectionCode, veza.StationID)
	return err
}

// primijeniOdrzavanuVodu stavlja primljenu vodu u programu održavanja na
// površinu
func primijeniOdrzavanuVodu(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
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
		m.Kind, m.Source, m.CreatedAt.UTC(), m.UpdatedAt.UTC())
	return err
}

// primijeniStavkuRadova stavlja primljenu stavku radova održavanja na
// površinu
func primijeniStavkuRadova(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
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
		w.CreatedAt.UTC(), w.UpdatedAt.UTC())
	return err
}
