package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"gocop/internal/hydro"
	"gocop/internal/models"
)

// Ovaj dio seeda izvodi registar vodomjernih postaja iz dokumentacije dionica
// (sections.json, polje "gauges"). Isti vodomjer naveden je na svakoj dionici
// za koju je mjerodavan — npr. "Crnac" na 14 dionica — pa se 534 zapisa svodi
// na oko 280 postaja povezanih preko tablice section_stations.
//
// Izvorni zapisi ostaju netaknuti u sections.gauges; ovdje se ništa ne briše,
// samo normalizira. Što se ne pročita strojno, postaja nosi needs_review.

// seedGauge je jedan zapis vodomjera iz dokumentacije dionice
type seedGauge struct {
	StationName string `json:"station_name"`
	PrepCm      string `json:"prep_cm"`
	RegularCm   string `json:"regular_cm"`
	EmergCm     string `json:"emerg_cm"`
	CriticalCm  string `json:"critical_cm"`
	RecordCm    string `json:"record_cm"`
	Notes       string `json:"notes"`
}

// seedSectionGauges su vodomjeri jedne dionice sakupljeni iz svih poddionica
type seedSectionGauges struct {
	Code        string
	Description string
	Gauges      []seedGauge
}

// gaugesFromSections skuplja vodomjere iz poddionica ugrađenog prijepisa;
// mjerila koja nisu vodomjeri (kota u metrima, pravilnik) ostaju uz poddionicu
func gaugesFromSections(sections []models.Section) []seedSectionGauges {
	var out []seedSectionGauges
	for _, s := range sections {
		g := seedSectionGauges{Code: s.Code, Description: s.Description}
		for _, p := range s.Parts {
			for _, x := range p.Gauges {
				if !x.IsGauge() {
					continue
				}
				g.Gauges = append(g.Gauges, seedGauge{StationName: x.StationName, PrepCm: x.PrepCm, RegularCm: x.RegularCm,
					EmergCm: x.EmergCm, CriticalCm: x.CriticalCm, RecordCm: x.RecordCm, Notes: x.Notes})
			}
		}
		out = append(out, g)
	}
	return out
}

// stationDraft je postaja u izradi, prije upisa u bazu
type stationDraft struct {
	Key               string
	Code              string
	Name              string
	Stationing        string
	Watercourse       string
	WatercourseSource string
	sectionDescs      []string
	ZeroDatum         *float64
	Prep              thresholdDraft
	Regular           thresholdDraft
	Emergency         thresholdDraft
	State             thresholdDraft
	Record            thresholdDraft
	Notes             string
	SourceName        string
	NeedsReview       bool
	ReviewNotes       []string
	SectionCodes      []string
}

type thresholdDraft struct {
	Cm  *int
	Raw string
}

func newThresholdDraft(raw string) thresholdDraft {
	return thresholdDraft{Cm: hydro.ParseThresholdCm(raw), Raw: strings.TrimSpace(raw)}
}

func (t thresholdDraft) sameAs(other thresholdDraft) bool {
	if (t.Cm == nil) != (other.Cm == nil) {
		return false
	}
	if t.Cm != nil && *t.Cm != *other.Cm {
		return false
	}
	return strings.EqualFold(t.Raw, other.Raw)
}

// buildStationDrafts pretvara zapise vodomjera svih dionica u registar postaja.
// Vraća postaje redoslijedom prvog pojavljivanja i broj preskočenih zapisa.
func buildStationDrafts(sections []seedSectionGauges) (drafts []*stationDraft, skipped []string) {
	byKey := make(map[string]*stationDraft)
	usedCodes := make(map[string]int)

	for _, section := range sections {
		for _, gauge := range section.Gauges {
			source := strings.TrimSpace(gauge.StationName)
			if source == "" {
				continue
			}

			name, stationing := hydro.ParseStationName(source)
			zeroDatum := hydro.ParseZeroDatum(source)
			prep := newThresholdDraft(gauge.PrepCm)
			regular := newThresholdDraft(gauge.RegularCm)
			emergency := newThresholdDraft(gauge.EmergCm)
			state := newThresholdDraft(gauge.CriticalCm)

			// Zapisi bez stacionaže, kote i ijednog brojčanog praga nisu vodomjeri
			// nego upute ("Prema Pravilniku akumulacije Borovik i prema").
			// Ostaju u sections.gauges, ali ne ulaze u registar postaja.
			hasNumericThreshold := prep.Cm != nil || regular.Cm != nil || emergency.Cm != nil || state.Cm != nil
			if stationing == "" && zeroDatum == nil && !hasNumericThreshold {
				skipped = append(skipped, fmt.Sprintf("%s: %s", section.Code, source))
				continue
			}

			key := hydro.StationKey(name)
			if key == "" {
				key = hydro.StationKey(source)
			}

			existing, found := byKey[key]
			if !found {
				code := hydro.Slug(name)
				if code == "" {
					code = hydro.Slug(source)
				}
				if seen := usedCodes[code]; seen > 0 {
					code = fmt.Sprintf("%s-%d", code, seen+1)
				}
				usedCodes[code]++

				draft := &stationDraft{
					Key:          key,
					Code:         code,
					Name:         name,
					Stationing:   stationing,
					sectionDescs: []string{section.Description},
					ZeroDatum:    zeroDatum,
					Prep:         prep,
					Regular:      regular,
					Emergency:    emergency,
					State:        state,
					Record:       newThresholdDraft(gauge.RecordCm),
					Notes:        strings.TrimSpace(gauge.Notes),
					SourceName:   source,
					SectionCodes: []string{section.Code},
				}

				// Za pregled se označava samo ono što blokira automatski izračun
				// faze obrane. Nedostatak kote nule ili stacionaže vidi se iz samog
				// zapisa, pa ne diže zastavicu — inače bi je nosila više od pola
				// registra i operater bi je prestao gledati.
				if !hasNumericThreshold {
					draft.NeedsReview = true
					draft.ReviewNotes = append(draft.ReviewNotes,
						"nijedan prag nije zapisan u centimetrima — faza obrane se ne računa automatski")
				}

				byKey[key] = draft
				drafts = append(drafts, draft)
				continue
			}

			existing.SectionCodes = append(existing.SectionCodes, section.Code)

			// Ista postaja s različitim pragovima na dvjema dionicama znači da se
			// dokumentacija razilazi — zadržava se prvi zapis, ali uz upozorenje.
			if !existing.Prep.sameAs(prep) || !existing.Regular.sameAs(regular) ||
				!existing.Emergency.sameAs(emergency) || !existing.State.sameAs(state) {
				existing.NeedsReview = true
				existing.ReviewNotes = append(existing.ReviewNotes,
					fmt.Sprintf("dionica %s navodi drukčije pragove za isti vodomjer", section.Code))
			}

			if existing.ZeroDatum == nil && zeroDatum != nil {
				existing.ZeroDatum = zeroDatum
			}
			if existing.Stationing == "" && stationing != "" {
				existing.Stationing = stationing
			}
			existing.sectionDescs = append(existing.sectionDescs, section.Description)
		}
	}

	for _, d := range drafts {
		d.Watercourse, d.WatercourseSource = hydro.ResolveStationWatercourse(d.SourceName, d.Stationing, d.sectionDescs)
	}

	return drafts, skipped
}

// seedStations puni registar vodomjernih postaja i njihovu vezu s dionicama
func seedStations(database *sql.DB) error {
	var stationCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM stations").Scan(&stationCount); err != nil {
		return err
	}
	if stationCount > 0 {
		return nil
	}

	embedded, err := LoadSections()
	if err != nil {
		return err
	}
	sections := gaugesFromSections(embedded)

	drafts, skipped := buildStationDrafts(sections)
	if len(drafts) == 0 {
		return nil
	}

	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Kota iz dokumentacije je u starom visinskom sustavu; kota u novom sustavu
	// ostaje prazna dok je operater ne upiše.
	insertStation, err := tx.Prepare(`
		INSERT INTO stations (
			id, code, name, watercourse, watercourse_source, water_area, stationing,
			zero_datum, zero_datum_system, zero_datum_new, zero_datum_new_system,
			prep_cm, prep_raw, regular_cm, regular_raw,
			emergency_cm, emergency_raw, state_cm, state_raw,
			record_cm, record_raw,
			notes, source_name, needs_review, review_note,
			latitude, longitude, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, '', ?, ?, 'TRST', NULL, 'HVRS71', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer insertStation.Close()

	now := time.Now().UTC()

	for _, d := range drafts {
		// isti identitet postaje na svakom čvoru — slijedi iz šifre
		stationID := StableID("station", d.Code)

		needsReview := 0
		if d.NeedsReview {
			needsReview = 1
		}

		_, err = insertStation.Exec(
			stationID.String(), d.Code, d.Name, d.Watercourse, d.WatercourseSource, d.Stationing, d.ZeroDatum,
			d.Prep.Cm, d.Prep.Raw, d.Regular.Cm, d.Regular.Raw,
			d.Emergency.Cm, d.Emergency.Raw, d.State.Cm, d.State.Raw,
			d.Record.Cm, d.Record.Raw,
			d.Notes, d.SourceName, needsReview, strings.Join(d.ReviewNotes, "; "),
			now, now,
		)
		if err != nil {
			return fmt.Errorf("greška pri unosu vodomjerne postaje %s: %w", d.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	log.Printf("Vodomjerne postaje: %d postaja iz dokumentacije dionica, %d zapisa preskočeno (nisu vodomjeri)",
		len(drafts), len(skipped))

	return nil
}
