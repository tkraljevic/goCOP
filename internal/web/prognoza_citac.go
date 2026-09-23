package web

import (
	"database/sql"
	"math"
	"time"

	"gocop/internal/prognoza"
)

// Čitač prognoza za prikaz. Baza prognoza stoji odvojeno od operativne i od
// arhive, pa je i ovdje zasebna veza: prognoza je račun, a ne zapis, i program
// mora raditi i kad je nema.

// CitacPrognoza čita izdane prognoze iz baze prognoza.
type CitacPrognoza struct {
	db *sql.DB
}

// OtvoriPrognoze otvara bazu prognoza za čitanje.
func OtvoriPrognoze(put string) (*CitacPrognoza, error) {
	db, err := sql.Open("sqlite", put+"?mode=ro&_pragma=busy_timeout(3000)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &CitacPrognoza{db: db}, nil
}

func (c *CitacPrognoza) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

// ZaLetvu vraća najnoviju prognozu jedne letve, ali samo u traženoj veličini.
// Gornja Drava se prognozira u protoku jer se korito ispod lanca
// hidroelektrana produbljuje, pa joj vodostaj kroz desetljeća mijenja
// značenje — na grafu vodostaja ondje prognoze zasad nema.
func (c *CitacPrognoza) ZaLetvu(letva, velicina string) *PrognozaNiza {
	if c == nil || c.db == nil || letva == "" {
		return nil
	}
	var izdano sql.NullInt64
	err := c.db.QueryRow(`SELECT max(izdano) FROM izdane WHERE letva = ? AND velicina = ?`,
		letva, velicina).Scan(&izdano)
	if err != nil || !izdano.Valid {
		return nil
	}
	r, err := c.db.Query(`SELECT ciljni, vrijednost, raspon FROM izdane
		WHERE letva = ? AND velicina = ? AND izdano = ? ORDER BY ciljni`,
		letva, velicina, izdano.Int64)
	if err != nil {
		return nil
	}
	defer r.Close()
	p := &PrognozaNiza{
		Izdano: time.Unix(izdano.Int64*3600, 0).UTC(),
		Udio:   int(math.Round(prognoza.UdioURasponu * 100)),
	}
	for r.Next() {
		var ciljni int64
		var v, raspon float64
		if err := r.Scan(&ciljni, &v, &raspon); err != nil {
			return nil
		}
		p.Tocke = append(p.Tocke, TockaPrognoze{
			Kad: time.Unix(ciljni*3600, 0).UTC(), Vrijednost: v, Raspon: raspon,
		})
	}
	if r.Err() != nil || len(p.Tocke) < 2 {
		return nil
	}
	return p
}
