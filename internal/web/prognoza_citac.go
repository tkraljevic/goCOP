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

// ZaLetvu vraća najnoviju prognozu jedne letve u traženoj veličini. Letva je
// ima u obje ondje gdje postoji krivulja: model radi u jednoj, a krivulja daje
// drugu. Gdje krivulje nema — Vrbovka, Moslavina, Sotin, Mohovo, Osijek —
// postoji samo ona u kojoj se računa.
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
	r, err := c.db.Query(`SELECT ciljni, vrijednost, dolje, gore FROM izdane
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
		var v, dolje, gore float64
		if err := r.Scan(&ciljni, &v, &dolje, &gore); err != nil {
			return nil
		}
		p.Tocke = append(p.Tocke, TockaPrognoze{
			Kad: time.Unix(ciljni*3600, 0).UTC(), Vrijednost: v, Dolje: dolje, Gore: gore,
		})
	}
	if r.Err() != nil || len(p.Tocke) < 2 {
		return nil
	}
	return p
}

// Dnevno čita najnovije dnevno izdanje: za svaku letvu dan 0 (izmjereno) i
// 1.–6. dan.
func (c *CitacPrognoza) Dnevno() (time.Time, map[string][]prognoza.DnevnaIzdana, error) {
	if c == nil || c.db == nil {
		return time.Time{}, nil, nil
	}
	izdano, sve, err := prognoza.ZadnjeDnevno(c.db)
	if err != nil || len(sve) == 0 {
		return time.Time{}, nil, err
	}
	return time.Unix(izdano*3600, 0).UTC(), sve, nil
}

// TudaVrijednost je jedna vrijednost tuđe prognoze.
type TudaVrijednost struct {
	Cm, PlusMin float64
}

// Tude čita najnovije izdanje tuđe prognoze za svaku letvu, ako nije starije
// od dva dana: letva → ciljni sat → vrijednost. Jutarnje mjerenje koje stoji
// uz izdanje kao sidro ne vraća se — to nije prognoza.
func (c *CitacPrognoza) Tude(izvor string, sada time.Time) map[string]map[int64]TudaVrijednost {
	out := map[string]map[int64]TudaVrijednost{}
	if c == nil || c.db == nil {
		return out
	}
	od := sada.Add(-48*time.Hour).Unix() / 3600
	r, err := c.db.Query(`SELECT t.letva, t.ciljni, t.vrijednost, t.raspon FROM tude t
		JOIN (SELECT letva, max(izdano) AS izdano FROM tude WHERE izvor = ? AND izdano >= ?
			GROUP BY letva) z ON z.letva = t.letva AND z.izdano = t.izdano
		WHERE t.izvor = ? AND t.ciljni > t.izdano`, izvor, od, izvor)
	if err != nil {
		return out
	}
	defer r.Close()
	for r.Next() {
		var l string
		var t int64
		var v TudaVrijednost
		if r.Scan(&l, &t, &v.Cm, &v.PlusMin) != nil {
			continue
		}
		if out[l] == nil {
			out[l] = map[int64]TudaVrijednost{}
		}
		out[l][t] = v
	}
	return out
}

// Namjesteno vraća namještene pojase satnog lanca i izmjerene promašaje po
// dosegu — ono iz čega stranica „O prognozi” opisuje svaku postaju.
func (c *CitacPrognoza) Namjesteno() (map[string][]prognoza.Pojas, map[string]map[int]prognoza.Promasaj) {
	if c == nil || c.db == nil {
		return nil, nil
	}
	pojasi, err := prognoza.SviPojasi(c.db)
	if err != nil {
		return nil, nil
	}
	promasaji, _ := prognoza.Promasaji(c.db)
	return pojasi, promasaji
}

// Izbor vraća letve koje su u zadanom izdanju računate iz rezerve.
func (c *CitacPrognoza) Izbor(izdano time.Time) map[string]prognoza.Izbor {
	if c == nil || c.db == nil {
		return nil
	}
	out, err := prognoza.IzborIzdanja(c.db, izdano.Unix()/3600)
	if err != nil {
		return nil
	}
	return out
}
