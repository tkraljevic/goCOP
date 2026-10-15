package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// primjeneHidrologije su letve, očitanja i odbačene vrijednosti, kišomjeri,
// slivovi, ispravci i bilješke arhivskih nizova te epizode obrane
var primjeneHidrologije = map[string]primjena{
	EntityStations:  {upis: primijeniLetvu, brisanje: obrisi(`DELETE FROM stations WHERE id = ?`)},
	EntityReadings:  {upis: primijeniOcitanje, brisanje: obrisi(`DELETE FROM readings WHERE id = ?`)},
	EntityOdbacena:  {upis: poSadrzaju(upsertOdbacena), brisanje: obrisi(`DELETE FROM odbacena_ocitanja WHERE id = ?`)},
	EntityKisomjeri: {upis: primijeniKisomjer, brisanje: obrisi(`DELETE FROM kisomjeri WHERE code = ?`)},
	EntitySlivovi:   {upis: primijeniSliv, brisanje: obrisi(`DELETE FROM slivovi WHERE oznaka = ?`)},
	EntityIspravci:  {upis: poSadrzaju(primijeniIspravak), brisanje: makniIspravak},
	EntityBiljeske:  {upis: poSadrzaju(primijeniBiljesku), brisanje: makniBiljesku},
	EntityEpisodes:  {upis: primijeniEpizodu, brisanje: obrisi(`DELETE FROM defense_episodes WHERE id = ?`)},
}

// primijeniLetvu stavlja primljenu letvu na površinu
func primijeniLetvu(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var st models.Station
	if err := json.Unmarshal(v.Payload, &st); err != nil {
		return err
	}
	return upsertStation(ctx, tx, st)
}

func upsertStation(ctx context.Context, tx *sql.Tx, st models.Station) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO stations (
			id, code, name, watercourse, watercourse_code, watercourse_source, water_area, stationing,
			zero_datum, zero_datum_system, zero_datum_new, zero_datum_new_system,
			zero_datum_source, zero_datum_method, zero_datum_survey_date, zero_datum_document_date,
			zero_datum_history, extremes, return_levels,
			zero_datum_baltic, zero_datum_baltic_system, zero_datum_baltic_source,
			prep_cm, prep_raw, regular_cm, regular_raw, emergency_cm, emergency_raw, state_cm, state_raw,
			record_cm, record_raw, notes, source_name, needs_review, review_note,
			latitude, longitude, created_at, updated_at, javni_url, javni_uvoz,
			ograde_niza, telemetrija_site, telemetrija_uvoz, povijest, opis_vodokaza, datum_osnivanja, kota_nm,
			prate_sektori, prate_podrucja
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			code = excluded.code, name = excluded.name, watercourse = excluded.watercourse,
			watercourse_code = excluded.watercourse_code, watercourse_source = excluded.watercourse_source,
			water_area = excluded.water_area, stationing = excluded.stationing,
			zero_datum = excluded.zero_datum, zero_datum_system = excluded.zero_datum_system,
			zero_datum_new = excluded.zero_datum_new, zero_datum_new_system = excluded.zero_datum_new_system,
			zero_datum_baltic = excluded.zero_datum_baltic,
			zero_datum_baltic_system = excluded.zero_datum_baltic_system,
			zero_datum_baltic_source = excluded.zero_datum_baltic_source,
			zero_datum_history = excluded.zero_datum_history, extremes = excluded.extremes,
			return_levels = excluded.return_levels,
			zero_datum_source = excluded.zero_datum_source, zero_datum_method = excluded.zero_datum_method,
			zero_datum_survey_date = excluded.zero_datum_survey_date,
			zero_datum_document_date = excluded.zero_datum_document_date,
			prep_cm = excluded.prep_cm, prep_raw = excluded.prep_raw, regular_cm = excluded.regular_cm,
			regular_raw = excluded.regular_raw, emergency_cm = excluded.emergency_cm, emergency_raw = excluded.emergency_raw,
			state_cm = excluded.state_cm, state_raw = excluded.state_raw, record_cm = excluded.record_cm,
			record_raw = excluded.record_raw, notes = excluded.notes, source_name = excluded.source_name,
			needs_review = excluded.needs_review, review_note = excluded.review_note,
			latitude = excluded.latitude, longitude = excluded.longitude, updated_at = excluded.updated_at,
			javni_url = excluded.javni_url, javni_uvoz = excluded.javni_uvoz,
			ograde_niza = excluded.ograde_niza, telemetrija_site = excluded.telemetrija_site,
			telemetrija_uvoz = excluded.telemetrija_uvoz, povijest = excluded.povijest,
			opis_vodokaza = excluded.opis_vodokaza, datum_osnivanja = excluded.datum_osnivanja,
			kota_nm = excluded.kota_nm, prate_sektori = excluded.prate_sektori,
			prate_podrucja = excluded.prate_podrucja
	`,
		st.ID.String(), st.Code, st.Name, st.Watercourse, st.WatercourseCode, st.WatercourseSource, st.WaterArea, st.Stationing,
		st.ZeroDatum, defaultSystem(st.ZeroDatumSystem, models.ZeroDatumSystemOld),
		st.ZeroDatumNew, defaultSystem(st.ZeroDatumNewSystem, models.ZeroDatumSystemNew),
		st.ZeroDatumSource, st.ZeroDatumMethod, st.ZeroDatumSurveyDate, st.ZeroDatumDocumentDate,
		zeroDatumHistoryJSON(&st), extremesJSON(&st), returnLevelsJSON(&st),
		st.ZeroDatumBaltic, st.ZeroDatumBalticSystem, st.ZeroDatumBalticSource,
		st.Prep.Cm, st.Prep.Raw, st.Regular.Cm, st.Regular.Raw, st.Emergency.Cm, st.Emergency.Raw, st.State.Cm, st.State.Raw,
		st.Record.Cm, st.Record.Raw, st.Notes, st.SourceName, boolToInt(st.NeedsReview), st.ReviewNote,
		st.Latitude, st.Longitude, st.CreatedAt.UTC(), st.UpdatedAt.UTC(), st.JavniURL, boolToInt(st.JavniUvoz),
		ogradeNizaJSON(&st), st.TelemetrijaSite, boolToInt(st.TelemetrijaUvoz), st.Povijest, st.OpisVodokaza, st.DatumOsnivanja,
		boolToInt(st.KotaNM), sektoriUTekst(st.PrateSektori), podrucjaUTekst(st.PratePodrucja),
	)
	return err
}

// primijeniOcitanje stavlja primljeno očitanje na površinu, ako je u
// razdoblju koje ovaj čvor drži
func primijeniOcitanje(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	if !KeepReadingVersion(v.Payload) {
		return nil // izvan razdoblja koje ovaj čvor drži
	}
	var rd models.Reading
	if err := json.Unmarshal(v.Payload, &rd); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO readings (`+readingColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			station_id = excluded.station_id, structure_id = excluded.structure_id, measured_at = excluded.measured_at,
			level_cm = excluded.level_cm, level2_cm = excluded.level2_cm, temp_c = excluded.temp_c, flow_m3s = excluded.flow_m3s,
			temp_note = excluded.temp_note, flow_method = excluded.flow_method, flow_note = excluded.flow_note,
			source = excluded.source, origin = excluded.origin,
			source_ref = excluded.source_ref,
			quality = excluded.quality, derived_from = excluded.derived_from, method = excluded.method,
			observer = excluded.observer, user_id = excluded.user_id,
			structure_state = excluded.structure_state, gate = excluded.gate, ag_hours_1 = excluded.ag_hours_1,
			ag_hours_2 = excluded.ag_hours_2, ag_hours_3 = excluded.ag_hours_3, note = excluded.note,
			vrsta_biljeske = excluded.vrsta_biljeske, izdanje = excluded.izdanje,
			updated_at = excluded.updated_at
	`, readingArgs(&rd)...)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE readings SET channel = ? WHERE id = ?`, v.Channel, rd.ID.String())
	return err
}

// primijeniKisomjer stavlja primljeni kišomjer na površinu
func primijeniKisomjer(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var k models.Kisomjer
	if err := json.Unmarshal(v.Payload, &k); err != nil {
		return err
	}
	return upsertKisomjer(ctx, tx, k)
}

// primijeniSliv stavlja primljeni sliv na površinu
func primijeniSliv(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var m models.Sliv
	if err := json.Unmarshal(v.Payload, &m); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, upsertSlivSQL, m.Oznaka, m.Naziv, m.Km2, m.Geometry, m.Napomena)
	return err
}

// makniIspravak miče ispravak arhivske vrijednosti s površine
func makniIspravak(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	// trenutak koji ostane prazan dobiva ispravak koji je u knjizi još živ
	return makniSTrenutka(ctx, tx, "arhiva_ispravci", v, primijeniIspravak)
}

// makniBiljesku miče bilješku arhivske vrijednosti s površine; trenutak koji
// ostane prazan dobiva bilješku koja je u knjizi još živa
func makniBiljesku(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	return makniSTrenutka(ctx, tx, "arhiva_biljeske", v, primijeniBiljesku)
}
