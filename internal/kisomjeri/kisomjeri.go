// Package kisomjeri skuplja mjerenja stvarnih kišomjera iz registra slivova:
// svaki krug pročita što izvori daju za zadnje dane i upiše u radnu bazu uz
// oborine s Open-Meteo (oborine.db). Jednom dnevno se to uloži u arhivsko
// stablo, gdje je povijest.
//
// Izvedene točke (kvazi-kišomjeri) ovuda ne idu: njih preuzima paket oborine.
package kisomjeri

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Zagreb je zona u kojoj izvori pišu sat
var Zagreb = func() *time.Location {
	l, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		return time.FixedZone("CET", 3600)
	}
	return l
}()

// Postaja je stvarni kišomjer iz registra.
type Postaja struct {
	Code, Naziv       string
	Izvor, IzvorSifra string // izvor iz registra (dhmz, pljusak) i šifra postaje ondje
	Korak             string // satni, 12-satni, dnevni
	Lat, Lon          float64
}

// Mjerenje je oborina u jednom razdoblju koje završava u Kraj.
type Mjerenje struct {
	Kisomjer string
	Kraj     time.Time
	Sati     int // trajanje razdoblja: 1, 12 ili 24
	Oborina  float64
	Izvor    string // izvor u arhivi: kisomjer-dhmz, kisomjer-pljusak
}

// PostajeIzRegistra čita aktivne stvarne kišomjere iz registra (gocop.db).
func PostajeIzRegistra(registar *sql.DB) func() ([]Postaja, error) {
	return func() ([]Postaja, error) {
		r, err := registar.Query(`SELECT code, naziv, izvor, izvor_sifra, korak, latitude, longitude
			FROM kisomjeri WHERE aktivan = 1 AND vrsta = 'stvarni' AND izvor_sifra <> '' ORDER BY code`)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		var out []Postaja
		for r.Next() {
			var p Postaja
			if err := r.Scan(&p.Code, &p.Naziv, &p.Izvor, &p.IzvorSifra, &p.Korak, &p.Lat, &p.Lon); err != nil {
				return nil, err
			}
			out = append(out, p)
		}
		return out, r.Err()
	}
}

// Citac dohvaća mjerenja postaja jednog izvora.
type Citac interface {
	Preuzmi(ctx context.Context, postaje []Postaja) ([]Mjerenje, error)
}

// Spremiste drži mjerenja u radnoj bazi oborina.
type Spremiste struct{ DB *sql.DB }

// Pripremi stvara tablicu ako je nema.
func (s *Spremiste) Pripremi() error {
	_, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS izmjerene (
		kisomjer TEXT NOT NULL,
		kraj INTEGER NOT NULL,
		sati INTEGER NOT NULL,
		oborina REAL NOT NULL,
		izvor TEXT NOT NULL,
		preuzeto INTEGER NOT NULL,
		PRIMARY KEY (kisomjer, kraj, sati)
	)`)
	return err
}

// Upisi upisuje mjerenja; isto razdoblje se prepisuje, jer izvor zna
// naknadno ispraviti ili dopuniti sat (dnevni zbroj raste do kraja dana).
func (s *Spremiste) Upisi(ctx context.Context, m []Mjerenje) (int, error) {
	if len(m) == 0 {
		return 0, nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	st, err := tx.PrepareContext(ctx, `INSERT INTO izmjerene (kisomjer, kraj, sati, oborina, izvor, preuzeto)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(kisomjer, kraj, sati) DO UPDATE SET oborina = excluded.oborina, izvor = excluded.izvor,
			preuzeto = excluded.preuzeto`)
	if err != nil {
		return 0, err
	}
	defer st.Close()
	sada := time.Now().Unix()
	for _, x := range m {
		if _, err := st.ExecContext(ctx, x.Kisomjer, x.Kraj.Unix(), x.Sati, x.Oborina, x.Izvor, sada); err != nil {
			return 0, err
		}
	}
	return len(m), tx.Commit()
}

// Od vraća mjerenja jednog kišomjera od zadanog trenutka, po vremenu.
func (s *Spremiste) Od(kisomjer string, od time.Time) ([]Mjerenje, error) {
	r, err := s.DB.Query(`SELECT kraj, sati, oborina, izvor FROM izmjerene
		WHERE kisomjer = ? AND kraj > ? ORDER BY kraj, sati`, kisomjer, od.Unix())
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []Mjerenje
	for r.Next() {
		m := Mjerenje{Kisomjer: kisomjer}
		var kraj int64
		if err := r.Scan(&kraj, &m.Sati, &m.Oborina, &m.Izvor); err != nil {
			return nil, err
		}
		m.Kraj = time.Unix(kraj, 0).UTC()
		out = append(out, m)
	}
	return out, r.Err()
}

// Uvoznik svaki krug preuzme mjerenja svih stvarnih kišomjera.
type Uvoznik struct {
	Spremiste *Spremiste
	Postaje   func() ([]Postaja, error)
	Citaci    map[string]Citac // po izvoru iz registra
}

// Preuzmi dohvaća mjerenja po izvorima. Izvor koji ne uspije ne sprečava
// ostale; greške se vraćaju skupljene.
func (u *Uvoznik) Preuzmi(ctx context.Context) (int, error) {
	postaje, err := u.Postaje()
	if err != nil {
		return 0, err
	}
	poIzvoru := map[string][]Postaja{}
	for _, p := range postaje {
		poIzvoru[p.Izvor] = append(poIzvoru[p.Izvor], p)
	}
	izvori := make([]string, 0, len(poIzvoru))
	for i := range poIzvoru {
		izvori = append(izvori, i)
	}
	sort.Strings(izvori)
	var greske []error
	ukupno := 0
	for _, izvor := range izvori {
		c, ima := u.Citaci[izvor]
		if !ima {
			continue
		}
		m, err := c.Preuzmi(ctx, poIzvoru[izvor])
		if err != nil {
			greske = append(greske, fmt.Errorf("%s: %w", izvor, err))
		}
		n, err := u.Spremiste.Upisi(ctx, m)
		if err != nil {
			greske = append(greske, fmt.Errorf("%s: upis: %w", izvor, err))
		}
		ukupno += n
	}
	return ukupno, errors.Join(greske...)
}

// broj čita broj s točkom ili zarezom; prazno i crtica nisu broj
func broj(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	if s == "" || s == "-" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
