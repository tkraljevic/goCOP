// Package oborine preuzima živu oborinu za kvazi-kišomjere iz registra
// slivova: zadnjih sedam dana analize i sedam dana prognoze s Open-Meteo, po
// satu, za sve točke jednim zahtjevom. Čuva ih u zasebnoj bazi (oborine.db)
// uz bazu prognoza — to nije registar ni knjiga verzija, nego radni niz koji
// se svaki krug osvježi i uvijek se može ponovno preuzeti. Povijest za
// učenje modela ne ide ovuda nego kroz hidrološku arhivu.
package oborine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gocop/internal/db"
)

// Adresa je Open-Meteo prognoza; ista analiza (ICON, GFS, IFS — što najbolje
// pokriva točku) daje i prošle dane. Reanaliza (ERA5) kasni pet dana, pa za
// živi rad ne dolazi u obzir.
const Adresa = "https://api.open-meteo.com/v1/forecast"

// Unatrag i Unaprijed su koliko se dana prošlosti i prognoze traži. Dnevni
// model gleda zbroj zadnjih sedam dana, a prognoza kiše čeka svoj red.
const (
	Unatrag   = 7
	Unaprijed = 7
)

// Tocka je kišomjer za koji se oborina preuzima.
type Tocka struct {
	Code     string
	Lat, Lon float64
}

// Sat je jedan sat na jednoj točki.
type Sat struct {
	Kisomjer    string
	Sat         int64 // redni sat od epohe (unix / 3600)
	Oborina     float64
	Snijeg      float64 // mm vodenog ekvivalenta
	Temperatura float64
	Prognoza    bool // sat je bio u budućnosti kad je preuzet
}

// Uvoznik preuzima i čuva oborinu.
type Uvoznik struct {
	DB      *sql.DB
	Tocke   func() ([]Tocka, error)
	Adresa  string       // prazno znači Adresa
	Klijent *http.Client // prazno znači zadani s vremenskim ograničenjem
}

// Otvori otvara (i po potrebi stvara) bazu oborina.
func Otvori(put string) (*sql.DB, error) {
	d, err := db.OpenDB(put)
	if err != nil {
		return nil, err
	}
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS satne (
		kisomjer TEXT NOT NULL,
		sat INTEGER NOT NULL,
		oborina REAL,
		snijeg REAL,
		temperatura REAL,
		prognoza INTEGER NOT NULL DEFAULT 0,
		preuzeto INTEGER NOT NULL,
		PRIMARY KEY (kisomjer, sat)
	)`); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// TockeIzRegistra čita aktivne kišomjere iz registra slivova (gocop.db).
func TockeIzRegistra(registar *sql.DB) func() ([]Tocka, error) {
	return func() ([]Tocka, error) {
		r, err := registar.Query(`SELECT code, latitude, longitude FROM kisomjeri WHERE aktivan = 1 ORDER BY code`)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		var out []Tocka
		for r.Next() {
			var t Tocka
			if err := r.Scan(&t.Code, &t.Lat, &t.Lon); err != nil {
				return nil, err
			}
			out = append(out, t)
		}
		return out, r.Err()
	}
}

type odgovor struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Hourly    struct {
		Time          []string   `json:"time"`
		Precipitation []*float64 `json:"precipitation"`
		Snowfall      []*float64 `json:"snowfall"`
		Temperature   []*float64 `json:"temperature_2m"`
	} `json:"hourly"`
}

// Preuzmi dohvaća sve točke jednim zahtjevom i upiše sate; vraća koliko je
// sati upisano. Sat koji je već u bazi prepisuje se, jer analiza s vremenom
// postaje bolja od prognoze koja je ondje stajala.
func (u *Uvoznik) Preuzmi(ctx context.Context) (int, error) {
	tocke, err := u.Tocke()
	if err != nil {
		return 0, err
	}
	if len(tocke) == 0 {
		return 0, nil
	}
	lats := make([]string, len(tocke))
	lons := make([]string, len(tocke))
	for i, t := range tocke {
		lats[i] = strconv.FormatFloat(t.Lat, 'f', 4, 64)
		lons[i] = strconv.FormatFloat(t.Lon, 'f', 4, 64)
	}
	q := url.Values{}
	q.Set("latitude", strings.Join(lats, ","))
	q.Set("longitude", strings.Join(lons, ","))
	q.Set("hourly", "precipitation,snowfall,temperature_2m")
	q.Set("past_days", strconv.Itoa(Unatrag))
	q.Set("forecast_days", strconv.Itoa(Unaprijed))
	q.Set("timezone", "UTC")
	adresa := u.Adresa
	if adresa == "" {
		adresa = Adresa
	}
	req, err := http.NewRequestWithContext(ctx, "GET", adresa+"?"+q.Encode(), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "goCOP oborine")
	klijent := u.Klijent
	if klijent == nil {
		klijent = &http.Client{Timeout: 90 * time.Second}
	}
	resp, err := klijent.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	tijelo, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("open-meteo: HTTP %d: %s", resp.StatusCode, kratko(tijelo))
	}
	var lista []odgovor
	if err := json.Unmarshal(tijelo, &lista); err != nil {
		var jedan odgovor
		if err2 := json.Unmarshal(tijelo, &jedan); err2 != nil {
			return 0, fmt.Errorf("open-meteo: nerazumljiv odgovor: %w", err)
		}
		lista = []odgovor{jedan}
	}
	if len(lista) != len(tocke) {
		return 0, fmt.Errorf("open-meteo: traženo %d točaka, vraćeno %d", len(tocke), len(lista))
	}
	sada := time.Now().Unix() / 3600
	var sati []Sat
	for i, o := range lista {
		h := o.Hourly
		for j, ts := range h.Time {
			t, err := time.Parse("2006-01-02T15:04", ts)
			if err != nil {
				continue
			}
			s := Sat{Kisomjer: tocke[i].Code, Sat: t.Unix() / 3600}
			s.Prognoza = s.Sat > sada
			var ima bool
			if j < len(h.Precipitation) && h.Precipitation[j] != nil {
				s.Oborina, ima = *h.Precipitation[j], true
			}
			if j < len(h.Snowfall) && h.Snowfall[j] != nil {
				s.Snijeg = *h.Snowfall[j] / 0.7 // cm snijega → mm vode, kako Open-Meteo računa
			}
			if j < len(h.Temperature) && h.Temperature[j] != nil {
				s.Temperatura = *h.Temperature[j]
			}
			if ima {
				sati = append(sati, s)
			}
		}
	}
	return len(sati), u.upisi(ctx, sati)
}

func (u *Uvoznik) upisi(ctx context.Context, sati []Sat) error {
	tx, err := u.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.PrepareContext(ctx, `INSERT INTO satne (kisomjer, sat, oborina, snijeg, temperatura, prognoza, preuzeto)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(kisomjer, sat) DO UPDATE SET oborina = excluded.oborina, snijeg = excluded.snijeg,
			temperatura = excluded.temperatura, prognoza = excluded.prognoza, preuzeto = excluded.preuzeto`)
	if err != nil {
		return err
	}
	defer st.Close()
	sada := time.Now().Unix()
	for _, s := range sati {
		p := 0
		if s.Prognoza {
			p = 1
		}
		if _, err := st.ExecContext(ctx, s.Kisomjer, s.Sat, s.Oborina, s.Snijeg, s.Temperatura, p, sada); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Satne vraća oborinu po kišomjeru i satu za sate od–do (uključivo), u mm.
func (u *Uvoznik) Satne(od, do int64) (map[string]map[int64]float64, error) {
	r, err := u.DB.Query(`SELECT kisomjer, sat, oborina FROM satne WHERE sat BETWEEN ? AND ? AND oborina IS NOT NULL`, od, do)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out := map[string]map[int64]float64{}
	for r.Next() {
		var k string
		var sat int64
		var v float64
		if err := r.Scan(&k, &sat, &v); err != nil {
			return nil, err
		}
		if out[k] == nil {
			out[k] = map[int64]float64{}
		}
		out[k][sat] = v
	}
	return out, r.Err()
}

// Zadnje javlja kad je zadnji put nešto preuzeto; nula kad ništa.
func (u *Uvoznik) Zadnje() time.Time {
	var t sql.NullInt64
	if err := u.DB.QueryRow(`SELECT max(preuzeto) FROM satne`).Scan(&t); err != nil || !t.Valid {
		return time.Time{}
	}
	return time.Unix(t.Int64, 0)
}

func kratko(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
