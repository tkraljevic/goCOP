package kisomjeri

import (
	"context"
	"database/sql"
	"time"
)

// IzvorOcitanja je izvor kiše koju vodočuvar upiše uz očitanje letve, s
// kišomjera koji uz tu letvu stoji (Borovik). Zaseban je od povijesnog niza
// iz tablica Hrvatskih voda (izvor hv), da usklađivanje dira samo ono što je
// došlo s očitanja.
const IzvorOcitanja = "hv-ocitanje"

// KisaSOcitanja čita kišu upisanu uz očitanja letvi iz registra (gocop.db)
// i pripisuje je stvarnom kišomjeru uz tu letvu. Kiša se očitava ujutro i
// vrijedi za prethodni dan, pa razdoblje od 24 sata završava u ponoć na
// početku dana očitanja: očitanje 22. 11. u 7 h daje kišu za 21. 11.
func KisaSOcitanja(ctx context.Context, registar *sql.DB) ([]Mjerenje, error) {
	r, err := registar.QueryContext(ctx, `SELECT k.code, o.measured_at, o.rain_mm
		FROM readings o JOIN stations s ON s.id = o.station_id
		JOIN kisomjeri k ON k.letva = s.code AND k.letva <> '' AND k.vrsta = 'stvarni' AND k.aktivan = 1
		WHERE o.rain_mm IS NOT NULL ORDER BY k.code, o.measured_at`)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []Mjerenje
	for r.Next() {
		var m Mjerenje
		var kad time.Time
		if err := r.Scan(&m.Kisomjer, &kad, &m.Oborina); err != nil {
			return nil, err
		}
		d := kad.In(Zagreb)
		m.Kraj, m.Sati, m.Izvor = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, Zagreb), 24, IzvorOcitanja
		out = append(out, m)
	}
	return out, r.Err()
}

// ZamijeniIzvor briše sva mjerenja jednog izvora i upiše nova, u jednoj
// transakciji: ispravljena kiša zamijeni staru, a obrisano očitanje odnese i
// svoju kišu. Isti dan iz drugog izvora (povijesni niz) novo mjerenje
// prepiše.
func (s *Spremiste) ZamijeniIzvor(ctx context.Context, izvor string, m []Mjerenje) (int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM izmjerene WHERE izvor = ?`, izvor); err != nil {
		return 0, err
	}
	sada := time.Now().Unix()
	for _, x := range m {
		if _, err := tx.ExecContext(ctx, `INSERT INTO izmjerene (kisomjer, kraj, sati, oborina, izvor, preuzeto)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(kisomjer, kraj, sati) DO UPDATE SET oborina = excluded.oborina, izvor = excluded.izvor,
				preuzeto = excluded.preuzeto`, x.Kisomjer, x.Kraj.Unix(), x.Sati, x.Oborina, izvor, sada); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(m), nil
}

// prenesiOcitanja usklađuje kišu s očitanja letvi; bez registra ne radi
// ništa.
func (u *Uvoznik) prenesiOcitanja(ctx context.Context) (int, error) {
	if u.Registar == nil {
		return 0, nil
	}
	m, err := KisaSOcitanja(ctx, u.Registar)
	if err != nil {
		return 0, err
	}
	return u.Spremiste.ZamijeniIzvor(ctx, IzvorOcitanja, m)
}
