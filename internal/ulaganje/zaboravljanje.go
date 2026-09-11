package ulaganje

// Zaboravljanje je jedino mjesto u programu koje briše izmjerene vrijednosti.
//
// Zato je odvojeno od ulaganja i zato provjerava iznova. Ulaganje je već jednom
// provjerilo da je sve u arhivi, ali između ta dva koraka arhiva se mogla
// izgraditi iznova, datoteka se mogla maknuti iz stabla ili je netko mogao
// vratiti stariju arhivu iz paketa. Mjerenje se ne da ponoviti, pa se provjera
// ponavlja neposredno prije brisanja.

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"time"

	"gocop/internal/arhiva"
)

// ZaUpospremanje je jedna letva s očitanjima koja su uložena a još stoje u
// operativnoj bazi.
type ZaPospremanje struct {
	StationID string
	Letva     string
	Naziv     string
	Broj      int
	Od, Do    time.Time
	Oznake    string // izdanja pod kojima su uložena
}

// Pospremivo popisuje što čeka zaboravljanje, po letvi.
func Pospremivo(ctx context.Context, baza *sql.DB) ([]ZaPospremanje, error) {
	rows, err := baza.QueryContext(ctx, `
		SELECT s.id, s.code, s.name, count(*), min(r.measured_at), max(r.measured_at),
		       group_concat(DISTINCT r.izdanje)
		FROM readings r JOIN stations s ON s.id = r.station_id
		WHERE r.izdanje IS NOT NULL AND r.izdanje <> ''
		GROUP BY s.id ORDER BY s.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ZaPospremanje
	for rows.Next() {
		var z ZaPospremanje
		if err := rows.Scan(&z.StationID, &z.Letva, &z.Naziv, &z.Broj, &z.Od, &z.Do, &z.Oznake); err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

// IshodPospremanja je što je zaboravljanje napravilo.
type IshodPospremanja struct {
	Letva      string
	Provjereno int
	Obrisano   int
	Verzija    int
}

// Zaboravi briše uložena očitanja jedne letve i njihove verzije — ali tek
// nakon što je svaka vrijednost ponovno nađena u arhivi. Ako i jedna nedostaje,
// ne briše se ništa: bolje da baza ostane velika nego da mjerenje nestane.
func Zaboravi(ctx context.Context, baza *sql.DB, arhivaPut, stationID string,
	zapisi io.Writer) (*IshodPospremanja, error) {
	if zapisi == nil {
		zapisi = io.Discard
	}
	rows, err := baza.QueryContext(ctx, `
		SELECT r.id, r.measured_at, r.level_cm, s.code
		FROM readings r JOIN stations s ON s.id = r.station_id
		WHERE r.station_id = ? AND r.izdanje IS NOT NULL AND r.izdanje <> ''
		ORDER BY r.measured_at`, stationID)
	if err != nil {
		return nil, err
	}
	var ids []string
	var redci []arhiva.Redak
	letva := ""
	for rows.Next() {
		var id, code string
		var kad time.Time
		var level sql.NullInt64
		if err := rows.Scan(&id, &kad, &level, &code); err != nil {
			rows.Close()
			return nil, err
		}
		letva = code
		ids = append(ids, id)
		if level.Valid {
			redci = append(redci, arhiva.Redak{Vrijeme: kad.UTC(), Vrijednost: float64(level.Int64)})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("ta letva nema uloženih očitanja koja bi se pospremila")
	}

	javi(zapisi, "provjeravam je li sve u arhivi", 0, 0)
	nedostaje, err := ProvjeriUArhivi(arhivaPut, letva, redci)
	if err != nil {
		return nil, err
	}
	if nedostaje > 0 {
		return nil, fmt.Errorf("provjera pala: %d od %d vrijednosti nije u arhivi — ništa se ne briše",
			nedostaje, len(redci))
	}
	fmt.Fprintf(zapisi, "provjera: svih %d vrijednosti je u arhivi\n", len(redci))

	javi(zapisi, "brišem uložena očitanja", 0, 0)
	obrisano, verzija, err := zaboraviUlozena(ctx, baza, ids)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(zapisi, "obrisano: %d očitanja i %d verzija\n", obrisano, verzija)
	fmt.Fprintln(zapisi, "prostor se vraća tek nakon VACUUM (Administracija → Održavanje baze)")
	return &IshodPospremanja{Letva: letva, Provjereno: len(redci),
		Obrisano: obrisano, Verzija: verzija}, nil
}

// zaboraviUlozena briše očitanja i njihove verzije. Radi se tek nakon što je
// provjereno da su u arhivi, i samo nad onima koji su označeni izdanjem.
func zaboraviUlozena(ctx context.Context, baza *sql.DB, ids []string) (int, int, error) {
	tx, err := baza.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	var ocitanja, verzija int
	for _, id := range ids {
		res, err := tx.ExecContext(ctx, `DELETE FROM readings WHERE id=? AND izdanje<>''`, id)
		if err != nil {
			return ocitanja, verzija, err
		}
		k, _ := res.RowsAffected()
		if k == 0 {
			continue // nije označeno kao uloženo; ne dira se
		}
		ocitanja += int(k)
		res, err = tx.ExecContext(ctx,
			`DELETE FROM record_versions WHERE entity='readings' AND entity_id=?`, id)
		if err != nil {
			return ocitanja, verzija, err
		}
		k, _ = res.RowsAffected()
		verzija += int(k)
	}
	return ocitanja, verzija, tx.Commit()
}
