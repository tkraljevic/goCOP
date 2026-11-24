package oborine

// Snijeg na tlu iz SNOWGRID-a (GeoSphere Austria, otvoreni podaci, CC BY 4.0):
// vodni ekvivalent snijega na tlu po danu, u mrežu od 1 km, od 1961. do
// jučer ili prekjučer. Pokriva Austriju; izvan nje vraća prazno.
//
// Za svaku izvedenu točku alpskih slivova čita se devet čvorova oko nje
// (±0,1° širine i ±0,14° dužine), a vrijednost točke je srednjak čvorova s
// podatkom kad ih ima barem pet. Povijest se preuzme jednom, desetljeće po
// desetljeće s predahom između zahtjeva, a poslije samo zadnji dani kad
// izvor objavi novi dan. Svaki čvor to radi sam, iz javnog izvora; ništa od
// toga ne ide razmjenom.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"gocop/internal/models"
)

// AdresaSnowgrida je skup dnevnih podataka SNOWGRID-CL na GeoSphere Austria.
const AdresaSnowgrida = "https://dataset.api.hub.geosphere.at/v1/timeseries/historical/snowgrid_cl-v2-1d-1km"

// Snijeg na tlu: mreža oko točke, najmanje čvorova s podatkom, prvi dan
// izvora, koliko dana unatrag se ponovno čita i predah između zahtjeva.
const (
	korakSirine        = 0.1
	korakDuzine        = 0.14
	najmanjeCvorova    = 5
	ponovnoDana        = 9
	predahSnowgrida    = 3 * time.Second
	provjeraSnowgrida  = 3 * time.Hour
	ponovnoNakonGreske = 30 * time.Minute
	pokusajaSnowgrida  = 4
)

// obuhvatSnowgrida je pravokutnik koji izvor pokriva; čvor izvan njega izvor
// odbije za cijeli zahtjev, pa se ne traži.
var obuhvatSnowgrida = struct{ sjever, jug, istok, zapad float64 }{49.1775, 46.1614, 17.3821, 9.3940}

// SnijegNaTlu preuzima i čuva snijeg na tlu iz SNOWGRID-a.
type SnijegNaTlu struct {
	DB      *sql.DB
	Tocke   func() ([]Tocka, error)
	Adresa  string       // prazno znači AdresaSnowgrida
	Klijent *http.Client // prazno znači zadani s vremenskim ograničenjem
	// Cekaj čeka između zahtjeva i prije ponovnog pokušaja; prazno znači
	// pravo čekanje (testovi ne čekaju)
	Cekaj func(context.Context, time.Duration) error
	// NakonUpisa se zove kad je preuzimanje upisalo nove dane
	NakonUpisa func()
	// Javi piše u dnevnik programa; prazno znači ništa
	Javi func(format string, args ...any)
}

// pripremiSnijegNaTlu stvara tablice snijega na tlu: vrijednost točke po
// danu (dani od 1970., mm vode) i dokle je koje razdoblje preuzeto.
func pripremiSnijegNaTlu(d *sql.DB) error {
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS snijeg_na_tlu (
		kisomjer TEXT NOT NULL,
		dan INTEGER NOT NULL,
		swe REAL NOT NULL,
		PRIMARY KEY (kisomjer, dan)
	);
	CREATE TABLE IF NOT EXISTS snijeg_na_tlu_preuzeto (
		kisomjer TEXT NOT NULL,
		od INTEGER NOT NULL,
		do INTEGER NOT NULL,
		preuzeto INTEGER NOT NULL,
		PRIMARY KEY (kisomjer, od)
	)`)
	return err
}

// TockeAlpskihSlivova čita aktivne izvedene točke zadanih međuslivova iz
// registra (gocop.db).
func TockeAlpskihSlivova(registar *sql.DB, slivovi []string) func() ([]Tocka, error) {
	return func() ([]Tocka, error) {
		upit := `SELECT code, latitude, longitude, sliv FROM kisomjeri
			WHERE aktivan = 1 AND vrsta <> 'stvarni' ORDER BY code`
		r, err := registar.Query(upit)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		var out []Tocka
		for r.Next() {
			var t Tocka
			var sliv string
			if err := r.Scan(&t.Code, &t.Lat, &t.Lon, &sliv); err != nil {
				return nil, err
			}
			if sadrzi(slivovi, sliv) {
				out = append(out, t)
			}
		}
		return out, r.Err()
	}
}

func sadrzi(popis []string, s string) bool {
	for _, p := range popis {
		if p == s {
			return true
		}
	}
	return false
}

// razdobljeSnowgrida je desetljeće povijesti koje se preuzima jednim
// zahtjevom za jednu točku: od 1. 1. godine do 31. 12. devete iza nje, a
// zadnje do zadnjeg dana izvora.
type razdobljeSnowgrida struct{ od, do int64 }

// razdobljaDo su desetljeća od 1961. do dana kraj, od najnovijeg, da
// tekuća zima stigne prva.
func razdobljaDo(kraj int64) []razdobljeSnowgrida {
	var out []razdobljeSnowgrida
	for g := 1961; ; g += 10 {
		od := time.Date(g, 1, 1, 0, 0, 0, 0, time.UTC).Unix() / 86400
		if od > kraj {
			break
		}
		do := time.Date(g+10, 1, 1, 0, 0, 0, 0, time.UTC).Unix()/86400 - 1
		out = append(out, razdobljeSnowgrida{od, min(do, kraj)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].od > out[j].od })
	return out
}

// cvoroviOko su čvorovi mreže oko točke unutar obuhvata izvora.
func cvoroviOko(t Tocka) [][2]float64 {
	var out [][2]float64
	for _, ds := range []float64{-korakSirine, 0, korakSirine} {
		for _, dd := range []float64{-korakDuzine, 0, korakDuzine} {
			lat, lon := math.Round((t.Lat+ds)*1e4)/1e4, math.Round((t.Lon+dd)*1e4)/1e4
			if lat < obuhvatSnowgrida.jug || lat > obuhvatSnowgrida.sjever ||
				lon < obuhvatSnowgrida.zapad || lon > obuhvatSnowgrida.istok {
				continue
			}
			out = append(out, [2]float64{lat, lon})
		}
	}
	return out
}

// odgovorSnowgrida je GeoJSON odgovor izvora: dani i, po čvoru redom kako
// je tražen, vodni ekvivalent (kg/m² = mm vode) ili null.
type odgovorSnowgrida struct {
	Timestamps []string `json:"timestamps"`
	Features   []struct {
		Properties struct {
			Parameters struct {
				SweTot struct {
					Data []*float64 `json:"data"`
				} `json:"swe_tot"`
			} `json:"parameters"`
		} `json:"properties"`
	} `json:"features"`
}

// vrijednostiTocke čita odgovor za jednu točku: po danu srednjak čvorova s
// podatkom, a dan s manje od pet takvih čvorova izostaje.
func vrijednostiTocke(tijelo []byte, cvorova int) (map[int64]float64, error) {
	var o odgovorSnowgrida
	if err := json.Unmarshal(tijelo, &o); err != nil {
		return nil, fmt.Errorf("snowgrid: nerazumljiv odgovor: %w", err)
	}
	if len(o.Features) != cvorova {
		return nil, fmt.Errorf("snowgrid: traženo %d čvorova, vraćeno %d", cvorova, len(o.Features))
	}
	out := map[int64]float64{}
	for i, ts := range o.Timestamps {
		t, err := time.Parse("2006-01-02T15:04-07:00", ts)
		if err != nil {
			return nil, fmt.Errorf("snowgrid: dan %q: %w", ts, err)
		}
		if v, ima := srednjakCvorova(o, i); ima {
			out[t.UTC().Unix()/86400] = v
		}
	}
	return out, nil
}

// srednjakCvorova je srednjak čvorova s podatkom i-tog dana; bez barem pet
// čvorova nema ga.
func srednjakCvorova(o odgovorSnowgrida, i int) (float64, bool) {
	zbroj, n := 0.0, 0
	for _, f := range o.Features {
		d := f.Properties.Parameters.SweTot.Data
		if i < len(d) && d[i] != nil {
			zbroj += *d[i]
			n++
		}
	}
	if n < najmanjeCvorova {
		return 0, false
	}
	return math.Round(zbroj/float64(n)*10) / 10, true
}

// datum je dan od 1970. kako ga izvor traži.
func datum(dan int64) string { return time.Unix(dan*86400, 0).UTC().Format("2006-01-02") }

// adresa je izvor, zadani ili iz polja.
func (s *SnijegNaTlu) adresa() string {
	if s.Adresa == "" {
		return AdresaSnowgrida
	}
	return s.Adresa
}

// zahtjev pošalje GET i vrati tijelo; na HTTP 429 pričeka koliko izvor kaže
// (ili sve dulje) i pokuša ponovno.
func (s *SnijegNaTlu) zahtjev(ctx context.Context, adresa string) ([]byte, error) {
	cekanje := 30 * time.Second
	for pokusaj := 1; ; pokusaj++ {
		tijelo, status, poslije, err := s.jedanZahtjev(ctx, adresa)
		if err != nil || status == http.StatusOK {
			return tijelo, err
		}
		if status != http.StatusTooManyRequests || pokusaj >= pokusajaSnowgrida {
			return nil, fmt.Errorf("snowgrid: HTTP %d: %s", status, kratko(tijelo))
		}
		if poslije > 0 {
			cekanje = poslije
		}
		if err := s.cekaj(ctx, cekanje); err != nil {
			return nil, err
		}
		cekanje *= 2
	}
}

// jedanZahtjev je jedan GET: tijelo, status i koliko izvor traži da se
// pričeka (Retry-After), kad to kaže.
func (s *SnijegNaTlu) jedanZahtjev(ctx context.Context, adresa string) ([]byte, int, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", adresa, nil)
	if err != nil {
		return nil, 0, 0, err
	}
	req.Header.Set("User-Agent", "goCOP snijeg na tlu")
	klijent := s.Klijent
	if klijent == nil {
		klijent = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := klijent.Do(req)
	if err != nil {
		return nil, 0, 0, err
	}
	defer resp.Body.Close()
	tijelo, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, 0, 0, err
	}
	return tijelo, resp.StatusCode, sekundi(resp.Header.Get("Retry-After")), nil
}

// sekundi je trajanje iz zaglavlja Retry-After u sekundama; bez njega nula.
func sekundi(s string) time.Duration {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return time.Duration(n) * time.Second
}

// cekaj čeka d ili dok se ne otkaže.
func (s *SnijegNaTlu) cekaj(ctx context.Context, d time.Duration) error {
	if s.Cekaj != nil {
		return s.Cekaj(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ZadnjiDanIzvora je zadnji dan koji izvor ima (dani od 1970.), iz opisa
// skupa podataka.
func (s *SnijegNaTlu) ZadnjiDanIzvora(ctx context.Context) (int64, error) {
	tijelo, err := s.zahtjev(ctx, s.adresa()+"/metadata")
	if err != nil {
		return 0, err
	}
	var m struct {
		EndTime string `json:"end_time"`
	}
	if err := json.Unmarshal(tijelo, &m); err != nil {
		return 0, fmt.Errorf("snowgrid: nerazumljiv opis: %w", err)
	}
	t, err := time.Parse("2006-01-02T15:04-07:00", m.EndTime)
	if err != nil {
		return 0, fmt.Errorf("snowgrid: kraj %q: %w", m.EndTime, err)
	}
	return t.UTC().Unix() / 86400, nil
}

// dohvati čita dane od–do (uključivo) za jednu točku.
func (s *SnijegNaTlu) dohvati(ctx context.Context, t Tocka, od, do int64) (map[int64]float64, error) {
	cvorovi := cvoroviOko(t)
	if len(cvorovi) < najmanjeCvorova {
		return map[int64]float64{}, nil // točka je izvan izvora
	}
	q := url.Values{}
	q.Set("parameters", "swe_tot")
	q.Set("start", datum(od))
	q.Set("end", datum(do))
	q.Set("output_format", "geojson")
	for _, c := range cvorovi {
		q.Add("lat_lon", strconv.FormatFloat(c[0], 'f', -1, 64)+","+strconv.FormatFloat(c[1], 'f', -1, 64))
	}
	tijelo, err := s.zahtjev(ctx, s.adresa()+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	return vrijednostiTocke(tijelo, len(cvorovi))
}

// preuzetoDo su razdoblja točaka i dan dokle je koje preuzeto.
func (s *SnijegNaTlu) preuzetoDo() (map[string]map[int64]int64, error) {
	r, err := s.DB.Query(`SELECT kisomjer, od, do FROM snijeg_na_tlu_preuzeto`)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out := map[string]map[int64]int64{}
	for r.Next() {
		var k string
		var od, do int64
		if err := r.Scan(&k, &od, &do); err != nil {
			return nil, err
		}
		if out[k] == nil {
			out[k] = map[int64]int64{}
		}
		out[k][od] = do
	}
	return out, r.Err()
}

// upisi zapiše dane točke od–do: dan s vrijednošću upiše ili prepiše, a dan
// bez nje obriše, jer izvor zna naknadno ispraviti zadnje dane. Uz to
// zapamti dokle je razdoblje preuzeto.
func (s *SnijegNaTlu) upisi(ctx context.Context, tocka string, r razdobljeSnowgrida, od int64, dani map[int64]float64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM snijeg_na_tlu WHERE kisomjer = ? AND dan BETWEEN ? AND ?`, tocka, od, r.do); err != nil {
		return err
	}
	for d, v := range dani {
		if d < od || d > r.do {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO snijeg_na_tlu (kisomjer, dan, swe) VALUES (?, ?, ?)`, tocka, d, v); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO snijeg_na_tlu_preuzeto (kisomjer, od, do, preuzeto) VALUES (?, ?, ?, ?)
		ON CONFLICT(kisomjer, od) DO UPDATE SET do = excluded.do, preuzeto = excluded.preuzeto`,
		tocka, r.od, r.do, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// zadatak je jedan zahtjev: točka i dani od–do u razdoblju.
type zadatak struct {
	tocka     Tocka
	razdoblje razdobljeSnowgrida
	od        int64
}

// zadaci su zahtjevi koji još trebaju: razdoblje koje nije preuzeto cijelo,
// od najnovijeg. Razdoblje koje je već preuzeto do nekog dana čita se od
// devet dana prije tog dana, jer izvor zadnje dane zna ispraviti.
func zadaci(tocke []Tocka, preuzeto map[string]map[int64]int64, kraj int64) []zadatak {
	var out []zadatak
	for _, r := range razdobljaDo(kraj) {
		for _, t := range tocke {
			do, ima := preuzeto[t.Code][r.od]
			if ima && do >= r.do {
				continue
			}
			od := r.od
			if ima {
				od = max(r.od, do-ponovnoDana)
			}
			out = append(out, zadatak{t, r, od})
		}
	}
	return out
}

// Nadopuni preuzme što nedostaje do zadnjeg dana izvora: prvi put cijelu
// povijest, poslije samo nove dane. Između zahtjeva čeka predah. Vraća
// koliko je zahtjeva za dane poslano.
func (s *SnijegNaTlu) Nadopuni(ctx context.Context) (int, error) {
	tocke, err := s.Tocke()
	if err != nil || len(tocke) == 0 {
		return 0, err
	}
	kraj, err := s.ZadnjiDanIzvora(ctx)
	if err != nil {
		return 0, err
	}
	preuzeto, err := s.preuzetoDo()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, z := range zadaci(tocke, preuzeto, kraj) {
		if err := s.cekaj(ctx, predahSnowgrida); err != nil {
			return n, err
		}
		dani, err := s.dohvati(ctx, z.tocka, z.od, z.razdoblje.do)
		if err != nil {
			return n, fmt.Errorf("%s %s–%s: %w", z.tocka.Code, datum(z.od), datum(z.razdoblje.do), err)
		}
		if err := s.upisi(ctx, z.tocka.Code, z.razdoblje, z.od, dani); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Vrti preuzima snijeg na tlu dok se ne otkaže: odmah, pa svaka tri sata
// (izvor objavljuje jednom dnevno), a nakon greške za pola sata.
func (s *SnijegNaTlu) Vrti(ctx context.Context) {
	for {
		pauza := s.krug(ctx)
		if s.cekaj(ctx, pauza) != nil {
			return
		}
	}
}

// krug je jedno preuzimanje; vraća koliko čekati do idućeg.
func (s *SnijegNaTlu) krug(ctx context.Context) time.Duration {
	n, err := s.Nadopuni(ctx)
	if n > 0 && s.NakonUpisa != nil {
		s.NakonUpisa()
	}
	if err != nil {
		s.javi("snijeg na tlu (SNOWGRID): %v", err)
		return ponovnoNakonGreske
	}
	if n > 0 {
		s.javi("snijeg na tlu (SNOWGRID): %d %s", n, models.UzBrojHR(n, "zahtjev", "zahtjeva", "zahtjeva"))
	}
	return provjeraSnowgrida
}

func (s *SnijegNaTlu) javi(format string, args ...any) {
	if s.Javi != nil {
		s.Javi(format, args...)
	}
}

// Nizovi vraća snijeg na tlu (mm vode) po točki i danu za dane od–do,
// uključivo.
func (s *SnijegNaTlu) Nizovi(tocke []string, od, do int64) (map[string]map[int64]float64, error) {
	out := map[string]map[int64]float64{}
	for _, t := range tocke {
		r, err := s.DB.Query(`SELECT dan, swe FROM snijeg_na_tlu WHERE kisomjer = ? AND dan BETWEEN ? AND ?`, t, od, do)
		if err != nil {
			return nil, err
		}
		niz, err := citajNiz(r)
		if err != nil {
			return nil, err
		}
		out[t] = niz
	}
	return out, nil
}

func citajNiz(r *sql.Rows) (map[int64]float64, error) {
	defer r.Close()
	niz := map[int64]float64{}
	for r.Next() {
		var d int64
		var v float64
		if err := r.Scan(&d, &v); err != nil {
			return nil, err
		}
		niz[d] = v
	}
	return niz, r.Err()
}

// Preuzeta javlja je li za točku preuzeto barem jedno razdoblje: tada
// prazan niz znači da izvor ondje nema podataka, a ne da još nije stigao.
func (s *SnijegNaTlu) Preuzeta(tocka string) (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT count(*) FROM snijeg_na_tlu_preuzeto WHERE kisomjer = ?`, tocka).Scan(&n)
	return n > 0, err
}
