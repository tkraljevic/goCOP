package ulaganje

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gocop/internal/arhiva"
	"gocop/internal/models"
)

// razvrstaj dijeli očitanja po tome smiju li i kako u arhivu. Sumnjivo se ne
// ulaže: arhiva nema mjesto za sumnju po vrijednosti, pa bi ondje izgledalo
// kao mjerenje.
func razvrstaj(o []models.Reading) (iz razvrstano, sumnjivo, bezVrijednosti int) {
	for _, r := range o {
		if r.LevelCm == nil {
			bezVrijednosti++
			continue
		}
		if r.Quality == models.QualityUncertain {
			sumnjivo++
			continue
		}
		red := arhiva.Redak{Vrijeme: r.MeasuredAt.UTC(), Vrijednost: float64(*r.LevelCm)}
		switch {
		case r.Quality == models.QualityReconstructed:
			iz.preracunato = append(iz.preracunato, red)
		case r.Source == models.ReadingSourceManual:
			// Čovjek pred letvom nije dojava. Podrijetlo se ne smije stopiti:
			// pri maloj vodi je ručno očitanje jedina neovisna provjera onoga
			// što telemetrija javlja.
			iz.rucno = append(iz.rucno, red)
		default:
			iz.mjereno = append(iz.mjereno, red)
		}
		if r.Note != "" || r.VrstaBiljeske != "" {
			iz.biljeske = append(iz.biljeske, sBiljeskom{
				Vrijeme: r.MeasuredAt.UTC(), Vrsta: r.VrstaBiljeske, Tekst: r.Note, Tko: r.Observer})
		}
		iz.ulozeniID = append(iz.ulozeniID, r.ID.String())
	}
	return iz, sumnjivo, bezVrijednosti
}

// zatecenaVrsta javlja pod kojom vrstom taj izvor već stoji u arhivi. Prazno
// znači da ga ondje nema, pa se vrsta tek bira.
func zatecenaVrsta(arhivaPut, letva, izvor string) string {
	if arhivaPut == "" {
		return ""
	}
	db, err := sql.Open("sqlite", arhivaPut+"?mode=ro")
	if err != nil {
		return ""
	}
	defer db.Close()
	var v string
	err = db.QueryRow(`SELECT vrsta FROM nizovi WHERE letva=? AND izvor=? AND velicina='vodostaj'
		ORDER BY zapisa DESC LIMIT 1`, letva, izvor).Scan(&v)
	if err != nil {
		return ""
	}
	return v
}

// pogodiVrstu bira vrstu niza po gustoći očitanja. Jedno dnevno je jutarnje
// očitanje, više od toga je satni niz.
func pogodiVrstu(r []arhiva.Redak) string {
	if len(r) < 2 {
		return "jutarnji"
	}
	poredano := append([]arhiva.Redak(nil), r...)
	sort.Slice(poredano, func(a, b int) bool { return poredano[a].Vrijeme.Before(poredano[b].Vrijeme) })
	raspon := poredano[len(poredano)-1].Vrijeme.Sub(poredano[0].Vrijeme).Hours()
	if raspon <= 0 {
		return "jutarnji"
	}
	if float64(len(poredano))/(raspon/24) > 1.5 {
		return "satni"
	}
	return "jutarnji"
}

func biljeskeZa(letva, vrsta string, o []sBiljeskom) []models.ArhivaBiljeska {
	korak := "satni"
	if vrsta != "satni" {
		korak = "dnevni"
	}
	out := make([]models.ArhivaBiljeska, 0, len(o))
	for _, b := range o {
		out = append(out, models.ArhivaBiljeska{
			Letva: letva, Velicina: "vodostaj", Korak: korak,
			Vrijeme: b.Vrijeme, Vrsta: b.Vrsta, Tekst: b.Tekst, Tko: b.Tko,
		})
	}
	return out
}

// upisiIzvorRucnog otvara mjesto ručnom očitanju u tablici izvora, uključeno.
// Nepoznat izvor inače ulazi isključen i čeka odluku — ali za ono što je naš
// čovjek očitao na letvi odluka je već donesena time što je upisano.
func upisiIzvorRucnog(arhivaPut, naziv string) error {
	db, err := sql.Open("sqlite", arhivaPut)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`INSERT OR IGNORE INTO izvori (naziv, tocnost, red, ukljucen, napomena)
		VALUES (?, 1, 15, 1, 'očitanje s letve, upisano u programu; ispred telemetrije jer
			je čovjek pred letvom jedina neovisna provjera onoga što mjerilo javlja')`, naziv)
	return err
}

// PostajaPoSifri nalazi postaju po šifri letve.
func PostajaPoSifri(ctx context.Context, baza *sql.DB, sifra string) (models.Station, error) {
	var st models.Station
	var id string
	err := baza.QueryRowContext(ctx, `SELECT id, code, name FROM stations WHERE lower(code)=lower(?)`,
		sifra).Scan(&id, &st.Code, &st.Name)
	if err == sql.ErrNoRows {
		return st, fmt.Errorf("postaja %q nije u registru", sifra)
	}
	if err != nil {
		return st, err
	}
	st.ID, err = uuid.Parse(id)
	return st, err
}

// ocitanjaZaUlaganje vraća ona koja još nisu uložena. Već uloženo se preskače:
// drugo ulaganje istoga ne bi ništa pokvarilo, ali bi ga drugi put označilo
// novim izdanjem i sakrilo kad je zapravo ušlo.
func ocitanjaZaUlaganje(ctx context.Context, baza *sql.DB, stationID string,
	od, do time.Time) ([]models.Reading, error) {
	rows, err := baza.QueryContext(ctx, `
		SELECT id, measured_at, level_cm, quality, source, observer, note, vrsta_biljeske, izdanje
		FROM readings WHERE station_id = ? AND measured_at BETWEEN ? AND ?
		ORDER BY measured_at`, stationID, od.UTC(), do.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Reading
	for rows.Next() {
		var r models.Reading
		var id string
		var level sql.NullInt64
		if err := rows.Scan(&id, &r.MeasuredAt, &level, &r.Quality, &r.Source, &r.Observer,
			&r.Note, &r.VrstaBiljeske, &r.Izdanje); err != nil {
			return nil, err
		}
		if r.Izdanje != "" {
			continue // već uloženo
		}
		if level.Valid {
			v := int(level.Int64)
			r.LevelCm = &v
		}
		r.ID, _ = uuid.Parse(id)
		out = append(out, r)
	}
	return out, rows.Err()
}

// UNizu je jedan niz u arhivi i vrijednosti koje se u njemu traže.
type UNizu struct {
	Izvor    string
	Velicina string
	Vrsta    string
	Redci    []arhiva.Redak
}

// ProvjeriUArhivi broji koliko zadanih vrijednosti nema u arhivi, i to u
// vlastitom nizu, na vlastitom trenutku i s vlastitom vrijednošću.
//
// Prva izvedba je gledala samo postoji li u spojenom nizu bilo kakav zapis iste
// letve i trenutka. To ne dokazuje ono što tvrdi: spoj po trenutku drži jednu
// vrijednost, onu najtočnijeg izvora, pa naše očitanje može izgubiti sudar a
// provjera svejedno prođe. Kroz tu rupu bi prošao i dvostruki sat pri povratku
// na zimsko vrijeme, gdje dopuna dva zapisa sažme u jedan: vrijednost bi
// nestala iz stabla, u spoju bi na tom trenutku stajao netko drugi, i original
// bi se obrisao iz operative.
//
// Zato se gleda sirova tablica ocitanja kroz identitet niza — letva, izvor,
// veličina, vrsta — i traži se baš ta vrijednost.
func ProvjeriUArhivi(put, letva string, nizovi []UNizu) (int, error) {
	a, err := sql.Open("sqlite", put+"?mode=ro")
	if err != nil {
		return 0, err
	}
	defer a.Close()

	nedostaje := 0
	for _, n := range nizovi {
		if len(n.Redci) == 0 {
			continue
		}
		var id int64
		err := a.QueryRow(`SELECT id FROM nizovi WHERE letva=? AND izvor=? AND velicina=? AND vrsta=?`,
			letva, n.Izvor, n.Velicina, n.Vrsta).Scan(&id)
		if err == sql.ErrNoRows {
			// Niza uopće nema: nijedna njegova vrijednost nije u arhivi.
			nedostaje += len(n.Redci)
			continue
		}
		if err != nil {
			return nedostaje, err
		}
		for _, r := range n.Redci {
			var k int
			// Vrijednosti su cjelobrojni centimetri pisani kao realan broj;
			// usporedba ide preko razlike, ne preko jednakosti.
			if err := a.QueryRow(`SELECT count(*) FROM ocitanja
				WHERE niz=? AND vrijeme=? AND abs(vrijednost - ?) < 0.001`,
				id, r.Vrijeme.Unix(), r.Vrijednost).Scan(&k); err != nil {
				return nedostaje, err
			}
			if k == 0 {
				nedostaje++
			}
		}
	}
	return nedostaje, nil
}

func oznaciUlozeno(ctx context.Context, baza *sql.DB, ids []string, oznaka string) (int, error) {
	tx, err := baza.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n := 0
	for _, id := range ids {
		res, err := tx.ExecContext(ctx, `UPDATE readings SET izdanje=?, updated_at=? WHERE id=?`,
			oznaka, time.Now().UTC(), id)
		if err != nil {
			return n, err
		}
		k, _ := res.RowsAffected()
		n += int(k)
	}
	return n, tx.Commit()
}
