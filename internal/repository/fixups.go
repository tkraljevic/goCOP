package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Popravci podataka koji se izvode jednom, a mijenjaju sinkronizirane
// zapise. Za razliku od migracija sheme, ovi prolaze kroz knjigu verzija:
// ispravak titule je nova verzija zapisa kao i svaki drugi upis, pa stiže
// na ostale čvorove i ostaje u povijesti. Svaki popravak ima ime i izvodi
// se samo jednom po čvoru (tablica data_fixups).

type fixup struct {
	name string
	run  func(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error)
}

var fixups = []fixup{
	{
		// Batina najviši vodostaj iz 1965. nije izmjerila — preračunat je iz
		// postaje Bezdan. Dotad je stajao kao napomena uz prag, pa se čitao kao
		// mjerenje ove letve. Izmjereni maksimum ostaje +775 cm iz 2013.
		name: "batina-ekstremi-podrijetlo",
		run: func(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error) {
			var id string
			if err := tx.QueryRowContext(ctx, `SELECT id FROM stations WHERE code = 'batina'`).Scan(&id); err == sql.ErrNoRows {
				return 0, nil
			} else if err != nil {
				return 0, err
			}
			cm := func(v int) *int { return &v }
			extremes := []models.StationExtreme{
				{Kind: models.ExtremeMax, LevelCm: cm(775), OnDate: "2013-06-14",
					Quality: models.QualityMeasured, Source: "DHMZ"},
				{Kind: models.ExtremeMax, LevelCm: cm(795), OnDate: "1965-06-24",
					Quality: models.QualityReconstructed, Source: "postaja Bezdan",
					Method: "preračun iz vodostaja Bezdana"},
				{Kind: models.ExtremeMin, LevelCm: cm(-127), OnDate: "1909-01-07",
					Quality: models.QualityMeasured, Source: "DHMZ"},
			}
			b, err := json.Marshal(extremes)
			if err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE stations SET extremes = ?, updated_at = ? WHERE id = ?`,
				string(b), time.Now().UTC(), id); err != nil {
				return 0, err
			}
			st, err := getStationTx(ctx, tx, id)
			if err != nil {
				return 0, err
			}
			if _, err := rec.Record(ctx, tx, EntityStations, id, st); err != nil {
				return 0, err
			}
			return 1, nil
		},
	},
	{
		// Najniži vodostaj Batine iz 1909. stajao je kao IZMJERENO, izvor DHMZ.
		// Ne može biti: letva je utemeljena 2001. Rekonstruiran je iz Bezdana,
		// koji je 740 m uzvodno i utemeljen 1856.; odnos Batina − Bezdan iznosi
		// mjerenih +21 cm pri niskoj vodi, s raspršenošću od 10 cm na 8.126
		// dana. Apatin, 23 km nizvodno, daje -159 cm i time potvrđuje red
		// veličine.
		name: "batina-minimum-1909-podrijetlo",
		run: func(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error) {
			var id, sirovi string
			err := tx.QueryRowContext(ctx, `SELECT id, coalesce(extremes,'') FROM stations WHERE code = 'batina'`).Scan(&id, &sirovi)
			if err == sql.ErrNoRows {
				return 0, nil
			} else if err != nil {
				return 0, err
			}
			var extremes []models.StationExtreme
			if sirovi != "" {
				if err := json.Unmarshal([]byte(sirovi), &extremes); err != nil {
					return 0, err
				}
			}
			nasao := false
			for i := range extremes {
				e := &extremes[i]
				if e.Kind != models.ExtremeMin || e.OnDate != "1909-01-07" {
					continue
				}
				e.Quality = models.QualityReconstructed
				e.Source = "postaja Bezdan"
				e.Method = "preračun iz vodostaja Bezdana (-146 cm), pomak +21 cm"
				e.Note = "Bezdan je 740 m uzvodno, utemeljen 1856. Preračun iz Apatina daje -159 cm. Preračun iz Mohácsa daje -298 cm i odudara; uzrok nije utvrđen."
				nasao = true
			}
			if !nasao {
				return 0, nil
			}
			b, err := json.Marshal(extremes)
			if err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE stations SET extremes = ?, updated_at = ? WHERE id = ?`,
				string(b), time.Now().UTC(), id); err != nil {
				return 0, err
			}
			st, err := getStationTx(ctx, tx, id)
			if err != nil {
				return 0, err
			}
			if _, err := rec.Record(ctx, tx, EntityStations, id, st); err != nil {
				return 0, err
			}
			return 1, nil
		},
	},
	{
		// Napomena uz taj minimum govorila je da preračun iz Mohácsa daje -298
		// cm i odudara, uzrok neutvrđen. Uzrok je u međuvremenu nađen: kote
		// nule dunavskih letvi od Paksa do Mohácsa spuštene su 1.1.1943. za
		// 200 cm (VITUKI 1976). Uz taj ispravak Mohács daje -108 cm, dakle
		// sve tri procjene se slažu i napomena više ne stoji.
		name: "batina-minimum-1909-mohacs-1943",
		run: func(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error) {
			var id, sirovi string
			err := tx.QueryRowContext(ctx, `SELECT id, coalesce(extremes,'') FROM stations WHERE code = 'batina'`).Scan(&id, &sirovi)
			if err == sql.ErrNoRows {
				return 0, nil
			} else if err != nil {
				return 0, err
			}
			var extremes []models.StationExtreme
			if sirovi == "" {
				return 0, nil
			}
			if err := json.Unmarshal([]byte(sirovi), &extremes); err != nil {
				return 0, err
			}
			nasao := false
			for i := range extremes {
				e := &extremes[i]
				if e.Kind != models.ExtremeMin || e.OnDate != "1909-01-07" {
					continue
				}
				e.Note = "Bezdan je 740 m uzvodno, utemeljen 1856. Preračun iz Apatina daje -159 cm, iz Mohácsa -108 cm uz ispravak kote nule iz 1943. (-200 cm, VITUKI 1976)."
				nasao = true
			}
			if !nasao {
				return 0, nil
			}
			b, err := json.Marshal(extremes)
			if err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE stations SET extremes = ?, updated_at = ? WHERE id = ?`,
				string(b), time.Now().UTC(), id); err != nil {
				return 0, err
			}
			st, err := getStationTx(ctx, tx, id)
			if err != nil {
				return 0, err
			}
			if _, err := rec.Record(ctx, tx, EntityStations, id, st); err != nil {
				return 0, err
			}
			return 1, nil
		},
	},
	{
		// Ljeto 2026. donijelo je najnižu vodu u nizu Batine: -151 cm 22.8. u
		// 4 sata, po telemetriji DHMZ-a. Dotad je najniži zabilježeni bio
		// rekonstruirani -127 cm iz 1909., pa se izmjereni rekord nije nigdje
		// vidio. Ovjereni niz HIS-2000 seže do 31.7.2026., dakle kolovoz još
		// nije ovjeren — i to uz vrijednost piše.
		name: "batina-minimum-2026",
		run: func(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error) {
			var id, sirovi string
			err := tx.QueryRowContext(ctx, `SELECT id, coalesce(extremes,'') FROM stations WHERE code = 'batina'`).Scan(&id, &sirovi)
			if err == sql.ErrNoRows {
				return 0, nil
			} else if err != nil {
				return 0, err
			}
			var extremes []models.StationExtreme
			if sirovi != "" {
				if err := json.Unmarshal([]byte(sirovi), &extremes); err != nil {
					return 0, err
				}
			}
			for _, e := range extremes {
				if e.Kind == models.ExtremeMin && e.OnDate == "2026-08-22" {
					return 0, nil // već upisan
				}
			}
			cm := -151
			extremes = append(extremes, models.StationExtreme{
				Kind: models.ExtremeMin, LevelCm: &cm, OnDate: "2026-08-22",
				Quality: models.QualityMeasured, Source: "telemetrija, DHMZ",
				Method: "satno očitanje u 4 sata",
				Note:   "Najniži izmjereni vodostaj u nizu. Ovjereni niz HIS-2000 seže do 31.7.2026., pa kolovoška vrijednost još nije ovjerena.",
			})
			b, err := json.Marshal(extremes)
			if err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE stations SET extremes = ?, updated_at = ? WHERE id = ?`,
				string(b), time.Now().UTC(), id); err != nil {
				return 0, err
			}
			st, err := getStationTx(ctx, tx, id)
			if err != nil {
				return 0, err
			}
			if _, err := rec.Record(ctx, tx, EntityStations, id, st); err != nil {
				return 0, err
			}
			return 1, nil
		},
	},
	{
		name: "batina-zero-datum-2025",
		run: func(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error) {
			var id string
			if err := tx.QueryRowContext(ctx, `SELECT id FROM stations WHERE code = 'batina'`).Scan(&id); err == sql.ErrNoRows {
				return 0, nil
			} else if err != nil {
				return 0, err
			}

			if _, err := tx.ExecContext(ctx, `UPDATE stations SET
				zero_datum_new = 80.189,
				zero_datum_new_system = 'HVRS71',
				zero_datum_source = 'Geodetski elaborat 250 BATINA, CADCOM',
				zero_datum_method = 'Preuzeta zadana kota i transformirana u HVRS71; letva nije pronađena na terenu.',
				zero_datum_survey_date = '2024-09-10',
				zero_datum_document_date = '2025-01',
				updated_at = ?
				WHERE id = ?`, time.Now().UTC(), id); err != nil {
				return 0, err
			}
			st, err := getStationTx(ctx, tx, id)
			if err != nil {
				return 0, err
			}
			if _, err := rec.Record(ctx, tx, EntityStations, id, st); err != nil {
				return 0, err
			}
			return 1, nil
		},
	},
	{
		// Epizode obrane rekonstruirane iz niza očitanja upisane su prije nego
		// što je epizoda znala razlikovati proglašenje od prelaska praga. Sve
		// su počele prelaskom praga, pa im se to i upisuje — bez toga bi na
		// kartici stajale bez osnove, kao da su nastale bez razloga.
		name: "epizode-osnova-prag",
		run: func(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error) {
			rows, err := tx.QueryContext(ctx, `SELECT id FROM defense_episodes
				WHERE origin = ? AND (basis = '' OR threshold_at IS NULL)`, models.EpisodeFromReadings)
			if err != nil {
				return 0, err
			}
			var ids []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return 0, err
				}
				ids = append(ids, id)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return 0, err
			}

			now := time.Now().UTC()
			for _, id := range ids {
				if _, err := tx.ExecContext(ctx, `UPDATE defense_episodes
					SET basis = ?, threshold_at = started_at, updated_at = ? WHERE id = ?`,
					models.BasisThreshold, now, id); err != nil {
					return 0, err
				}
				e, err := getEpisodeTx(ctx, tx, id)
				if err != nil {
					return 0, err
				}
				if _, err := rec.Record(ctx, tx, EntityEpisodes, id, e); err != nil {
					return 0, err
				}
			}
			return len(ids), nil
		},
	},
	{
		// Napomena uz Batinu nosila je "M = +795 (preračun. 24.06.1965.!)".
		// Ta brojka sad stoji kao ekstrem, propisno označena kao rekonstruirana
		// i s izvorom, pa je u napomeni bila dvostruka — a polje napomene treba
		// govoriti o postaji, ne prepisivati podatak koji već ima svoje mjesto.
		//
		// Uz sam ekstrem upisuje se neovisna potvrda: preračun iz Mohácsa daje
		// 790 cm za isti dan, drugom postajom i drugom metodom.
		name: "batina-napomena-u-ekstrem",
		run: func(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error) {
			var id, biljeska, ekstremi string
			err := tx.QueryRowContext(ctx, `SELECT id, notes, extremes FROM stations WHERE code = 'batina'`).
				Scan(&id, &biljeska, &ekstremi)
			if err == sql.ErrNoRows {
				return 0, nil
			} else if err != nil {
				return 0, err
			}
			if !strings.Contains(biljeska, "M = +795") {
				return 0, nil
			}
			var eks []models.StationExtreme
			if err := json.Unmarshal([]byte(ekstremi), &eks); err != nil {
				return 0, err
			}
			for i := range eks {
				if eks[i].Quality == models.QualityReconstructed && eks[i].OnDate == "1965-06-24" {
					eks[i].Note = "Neovisna potvrda: preračun iz vodostaja Mohácsa daje 790 cm za isti dan."
				}
			}
			b, err := json.Marshal(eks)
			if err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE stations SET notes = '', extremes = ?, updated_at = ?
				WHERE id = ?`, string(b), time.Now().UTC(), id); err != nil {
				return 0, err
			}
			st, err := getStationTx(ctx, tx, id)
			if err != nil {
				return 0, err
			}
			if _, err := rec.Record(ctx, tx, EntityStations, id, st); err != nil {
				return 0, err
			}
			return 1, nil
		},
	},
}

// RunFixups izvodi popravke koji na ovom čvoru još nisu izvedeni
func RunFixups(ctx context.Context, db *sql.DB, rec *ledger.Recorder) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS data_fixups (
		name TEXT PRIMARY KEY, applied_at DATETIME NOT NULL, changed INTEGER NOT NULL DEFAULT 0)`); err != nil {
		return err
	}
	for _, f := range fixups {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM data_fixups WHERE name = ?`, f.name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		changed, err := f.run(ctx, tx, rec)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("popravak %s: %w", f.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO data_fixups (name, applied_at, changed) VALUES (?, ?, ?)`,
			f.name, time.Now().UTC(), changed); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if changed > 0 {
			log.Printf("Popravak podataka %s: promijenjeno %d zapisa", f.name, changed)
		}
	}
	return nil
}
