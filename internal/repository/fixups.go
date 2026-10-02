package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Popravci podataka koji se izvode jednom, a mijenjaju sinkronizirane
// zapise. Za razliku od migracija sheme, ovi prolaze kroz knjigu verzija:
// ispravak titule je nova verzija zapisa kao i svaki drugi upis, pa stiže
// na ostale čvorove i ostaje u povijesti. Svaki popravak ima ime i izvodi
// se samo jednom po čvoru (tablica data_fixups).
//
// Pravilo za nove popravke (popraviZapis): ulaz je zadnja verzija zapisa iz
// knjige, ne površina; popravak mijenja njezin JSON, a nova verzija se piše
// samo kad se nešto zaista promijenilo. Popravak se izvodi na svakom čvoru,
// pa bi inače svaki čvor upisao svoju verziju istog ispravka i mreža bi ih
// nosila sve. Ovako čvor koji je zapis već popravio pošalje jednu verziju, a
// čvor koji ju je primio prije svog popravka ne nađe što mijenjati i ne piše
// ništa — u cijeloj mreži ostaje jedna verzija. U tijelo se zato ne upisuje
// ništa što ovisi o čvoru ili satu (updated_at = sada), jer bi takva verzija
// na svakom čvoru ispala drukčija.
//
// Isti ispravak na dva čvora prije nego što se sretnu ipak daje dvije
// verzije. To se ne rješava izvedenim (determinističkim) oznakama verzija:
// granica razmjene je MAX(version_id) po autoru i kanalu, a oznaka koja ne
// raste s vremenom kao UUIDv7 zna ispasti ispod granice, pa je druga strana
// nikad ne zatraži. Dvije verzije istog sadržaja su bezopasne: zadnja
// pobjeđuje, a sadržaj je isti.
//
// Zapis koji je zadnji izmijenio noviji program (ledger.ErrNovijaShema) ovaj
// ne smije prepisati: popravak ga preskače, ostale popravi, a ne bilježi se
// kao izveden, pa se pokuša iznova pri sljedećem pokretanju.

type fixup struct {
	name string
	run  func(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error)
}

var fixups = []fixup{
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

			changed := 0
			var preskoceni preskoceniZapisi
			for _, id := range ids {
				ok, err := popraviZapis(ctx, tx, rec, EntityEpisodes, id, popraviOsnovuPraga)
				if errors.Is(err, ledger.ErrNovijaShema) {
					preskoceni.dodaj(err)
					continue
				}
				if err != nil {
					return changed, err
				}
				if ok {
					changed++
				}
			}
			return changed, preskoceni.greska()
		},
	},
	{
		// Ivanec (Varaždinska županija) i Vrbovec (Zagrebačka županija) su nedostajali u registru
		name: "gradovi-ivanec-vrbovec",
		run: func(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error) {
			// Novi čvor nema registar dok ga ne primi razmjenom, a s njim i
			// oba grada (popravak je odavno izveden na čvoru od kojeg prima).
			// Bez županija nema se kamo upisati.
			var zupanija int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM counties WHERE id IN (1, 5)`).Scan(&zupanija); err != nil {
				return 0, err
			}
			if zupanija < 2 {
				return 0, nil
			}
			changed := 0
			// Ivanec
			var nIvanec int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM municipalities WHERE name = 'Ivanec'`).Scan(&nIvanec); err != nil {
				return 0, err
			}
			if nIvanec == 0 {
				mIvanec := models.Municipality{
					ID:         555,
					CountyID:   5,
					Name:       "Ivanec",
					Type:       "GRAD",
					HeadTitle:  "Gradonačelnik",
					HeadName:   "", // ime čelnika upisuje se u registru, ne u kodu
					PostalCode: "42240",
					AreaSqKm:   95.81,
					Population: 12723,
					Email:      "grad@ivanec.hr",
					Phone:      "042/404-100",
					Website:    "https://www.ivanec.hr",
				}
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO municipalities (id, county_id, name, type, head_title, head_name, postal_code, area_sqkm, population, email, phone, website)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					mIvanec.ID, mIvanec.CountyID, mIvanec.Name, mIvanec.Type, mIvanec.HeadTitle, mIvanec.HeadName,
					mIvanec.PostalCode, mIvanec.AreaSqKm, mIvanec.Population, mIvanec.Email, mIvanec.Phone, mIvanec.Website,
				); err != nil {
					return 0, fmt.Errorf("unos Ivanec: %w", err)
				}
				if _, err := rec.Record(ctx, tx, EntityMunicipalities, "555", mIvanec); err != nil {
					return 0, err
				}
				changed++
			}

			// Vrbovec
			var nVrbovec int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM municipalities WHERE name = 'Vrbovec'`).Scan(&nVrbovec); err != nil {
				return 0, err
			}
			if nVrbovec == 0 {
				mVrbovec := models.Municipality{
					ID:         556,
					CountyID:   1,
					Name:       "Vrbovec",
					Type:       "GRAD",
					HeadTitle:  "Gradonačelnik",
					HeadName:   "", // ime čelnika upisuje se u registru, ne u kodu
					PostalCode: "10340",
					AreaSqKm:   159.05,
					Population: 12981,
					Email:      "grad-vrbovec@vrbovec.hr",
					Phone:      "01/2799-900",
					Website:    "https://www.vrbovec.hr",
				}
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO municipalities (id, county_id, name, type, head_title, head_name, postal_code, area_sqkm, population, email, phone, website)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					mVrbovec.ID, mVrbovec.CountyID, mVrbovec.Name, mVrbovec.Type, mVrbovec.HeadTitle, mVrbovec.HeadName,
					mVrbovec.PostalCode, mVrbovec.AreaSqKm, mVrbovec.Population, mVrbovec.Email, mVrbovec.Phone, mVrbovec.Website,
				); err != nil {
					return 0, fmt.Errorf("unos Vrbovec: %w", err)
				}
				if _, err := rec.Record(ctx, tx, EntityMunicipalities, "556", mVrbovec); err != nil {
					return 0, err
				}
				changed++
			}

			return changed, nil
		},
	},
}

// popraviOsnovuPraga upisuje epizodi utvrđenoj iz očitanja da je počela
// prelaskom praga: osnova PRAG, prag u trenutku početka
func popraviOsnovuPraga(m map[string]json.RawMessage) (bool, error) {
	var origin, basis string
	if raw, ok := m["origin"]; ok {
		if err := json.Unmarshal(raw, &origin); err != nil {
			return false, err
		}
	}
	if origin != models.EpisodeFromReadings {
		return false, nil
	}
	if raw, ok := m["basis"]; ok {
		if err := json.Unmarshal(raw, &basis); err != nil {
			return false, err
		}
	}
	prag, imaPrag := m["threshold_at"]
	if basis != "" && imaPrag && string(prag) != "null" {
		return false, nil
	}
	pocetak, ok := m["started_at"]
	if !ok {
		return false, nil
	}
	osnova, err := json.Marshal(models.BasisThreshold)
	if err != nil {
		return false, err
	}
	m["basis"] = osnova
	m["threshold_at"] = pocetak
	return true, nil
}

// popraviZapis je put kojim popravak mijenja sinkronizirani zapis: čita
// zadnju verziju iz knjige (ne površinu), daje popravku njezin JSON i piše
// novu verziju samo ako se sadržaj zaista promijenio. Površina se zatim
// osvježi iz te nove verzije istim putem kao primljena razmjena (applyOne),
// pa je površina jednaka knjizi. Polja koja ovaj program ne poznaje ostaju u
// mapi kakva jesu. Arhiviran zapis ili zapis bez verzije se ne dira.
// Vraća je li upisana nova verzija; zapis novije sheme daje
// ledger.ErrNovijaShema i ostaje netaknut.
func popraviZapis(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, entity, id string,
	popravi func(m map[string]json.RawMessage) (bool, error)) (bool, error) {
	top, err := zadnjaVerzijaTx(ctx, tx, entity, id)
	if errors.Is(err, ledger.ErrNoVersion) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if top.Archived {
		return false, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(top.Payload, &m); err != nil {
		return false, fmt.Errorf("verzija %s/%s nije JSON objekt: %w", entity, id, err)
	}
	if m == nil {
		return false, nil
	}
	if ok, err := popravi(m); err != nil || !ok {
		return false, err
	}
	novo, err := json.Marshal(m)
	if err != nil {
		return false, err
	}
	if isto, err := istiJSON(top.Payload, novo); err != nil || isto {
		return false, err
	}
	versionID, err := rec.RecordIn(ctx, tx, top.Channel, entity, id, m)
	if err != nil {
		return false, err
	}
	nova, err := zadnjaVerzijaTx(ctx, tx, entity, id)
	if err != nil {
		return false, err
	}
	if nova.VersionID != versionID {
		// Na vrhu je verzija drugog čvora kojemu sat žuri: popravak je
		// upisan u povijest, a površina već pokazuje tu noviju verziju.
		// Pokretanje zbog toga ne smije stati.
		log.Printf("popravak %s/%s: na vrhu je novija verzija %s s čvora %s, površina ostaje njezina", entity, id, nova.VersionID, nova.NodeID)
		return true, nil
	}
	if err := applyOne(ctx, tx, *nova); err != nil {
		return false, fmt.Errorf("površina %s/%s nakon popravka: %w", entity, id, err)
	}
	return true, nil
}

// zadnjaVerzijaTx čita zadnju verziju zapisa u transakciji popravka
func zadnjaVerzijaTx(ctx context.Context, tx *sql.Tx, entity, id string) (*ledger.Version, error) {
	v := ledger.Version{Entity: entity, EntityID: id}
	var archived int
	var payload string
	err := tx.QueryRowContext(ctx, `SELECT version_id, node_id, supersedes, archived, payload, created_at, schema_version, channel
		FROM record_versions WHERE entity = ? AND entity_id = ? ORDER BY version_id DESC LIMIT 1`, entity, id).
		Scan(&v.VersionID, &v.NodeID, &v.Supersedes, &archived, &payload, &v.CreatedAt, &v.SchemaVersion, &v.Channel)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ledger.ErrNoVersion
	}
	if err != nil {
		return nil, err
	}
	v.Archived = archived != 0
	v.Payload = json.RawMessage(payload)
	return &v, nil
}

// istiJSON uspoređuje dva tijela po sadržaju, ne po zapisu: redoslijed
// polja i razmaci nisu promjena, a brojevi se uspoređuju kako su napisani
func istiJSON(a, b []byte) (bool, error) {
	ka, err := kanonskiJSON(a)
	if err != nil {
		return false, err
	}
	kb, err := kanonskiJSON(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ka, kb), nil
}

func kanonskiJSON(b []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// preskoceniZapisi skuplja zapise koje popravak nije smio dirati jer ih je
// zadnji izmijenio noviji program
type preskoceniZapisi struct {
	n    int
	prvi error
}

func (p *preskoceniZapisi) dodaj(err error) {
	if p.prvi == nil {
		p.prvi = err
	}
	p.n++
}

func (p *preskoceniZapisi) greska() error {
	if p.n == 0 {
		return nil
	}
	return &preskoceniError{n: p.n, prvi: p.prvi}
}

// preskoceniError: popravak je izveden na svemu osim na zapisima novije sheme
type preskoceniError struct {
	n    int
	prvi error
}

func (e *preskoceniError) Error() string {
	return fmt.Sprintf("preskočeno %d zapisa koje je izmijenio noviji program (prvi: %v)", e.n, e.prvi)
}

func (e *preskoceniError) Unwrap() error { return e.prvi }

// RunFixups izvodi popravke koji na ovom čvoru još nisu izvedeni. Zapis
// novije sheme ne ruši pokretanje: što je popravak stigao popraviti se
// potvrdi (ili, kad je odustao cijeli, vrati), ali se ne bilježi kao
// izveden, pa se pokuša iznova kad se program ažurira.
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
		var preskoceno *preskoceniError
		switch {
		case errors.As(err, &preskoceno):
			if err := tx.Commit(); err != nil {
				return err
			}
			log.Printf("Popravak podataka %s nije dovršen: promijenjeno %d zapisa, %v — ažurirajte goCOP", f.name, changed, err)
			continue
		case errors.Is(err, ledger.ErrNovijaShema):
			tx.Rollback()
			log.Printf("Popravak podataka %s preskočen: %v", f.name, err)
			continue
		case err != nil:
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
