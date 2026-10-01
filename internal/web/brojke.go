package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Brojke podataka na naslovnoj: koliko sustav zna. Svaki broj je izbrojen,
// ne procijenjen, i raste kako stižu nova očitanja i izdanja; stranica ih
// pita svake minute, a poslužitelj ih broji najviše svakih pet minuta, jer
// se veličina izvornog stabla i datoteka provjere čita s diska.

// BrojkeOsvjezavanje je koliko dugo izbrojene brojke vrijede.
const BrojkeOsvjezavanje = 5 * time.Minute

// Velicina je koliko mjesta zauzima jedan dio podataka.
type Velicina struct {
	Naziv   string `json:"naziv"`
	Bajtova int64  `json:"bajtova"`
}

// BrojkePodataka su brojke za naslovnu.
type BrojkePodataka struct {
	Zapisa  int64 `json:"zapisa"`  // svi zapisi arhive
	Bajtova int64 `json:"bajtova"` // sve baze i izvorne datoteke
	Godina  int   `json:"godina"`  // koliko godina unatrag seže najstariji vodostaj

	Letvi      int `json:"letvi"`      // letve s nizom u arhivi ili s očitanjem u zadnja 24 h
	LetviUzivo int `json:"letviUzivo"` // od toga s očitanjem u zadnja 24 h

	SatnihVodostaja  int64 `json:"satnihVodostaja"`
	SatniOd          int   `json:"satniOd"`
	DnevnihVodostaja int64 `json:"dnevnihVodostaja"`
	DnevniOd         int   `json:"dnevniOd"`
	Protoka          int64 `json:"protoka"`
	ProtokOd         int   `json:"protokOd"`

	OborinaPrave    int64 `json:"oborinaPrave"`
	PostajaPravih   int   `json:"postajaPravih"`
	PraveOd         int   `json:"praveOd"`
	OborinaIzvedene int64 `json:"oborinaIzvedene"`
	TocakaIzvedenih int   `json:"tocakaIzvedenih"`
	IzvedeneOd      int   `json:"izvedeneOd"`
	Meteo           int64 `json:"meteo"` // snijeg, temperatura zraka, visina snijega

	Prognoza int64 `json:"prognoza"` // vrijednosti izdanih prognoza koje baza čuva
	Izdanja  int   `json:"izdanja"`
	Provjera int64 `json:"provjera"` // prognoze puštene unatrag kroz arhivu

	Ocitanja    int64 `json:"ocitanja"`    // očitanja u radnoj bazi
	Ocitanja24h int64 `json:"ocitanja24h"` // od toga u zadnja 24 h

	Profili     int64 `json:"profili"`
	ProfilTocke int64 `json:"profilTocke"`
	HQ          int64 `json:"hq"`

	NajstarijiDan   string `json:"najstarijiDan"`   // „1. 1. 1900.”
	NajstarijaLetva string `json:"najstarijaLetva"` // ime letve

	Rekordi    []Rekord                      `json:"rekordi"`
	Desetljeca []repository.DesetljeceZapisa `json:"desetljeca"` // prazno dok se prvi put ne izbroji
	Velicine   []Velicina                    `json:"velicine"`
	Izracunato time.Time                     `json:"izracunato"`
}

// Rekord je najviši izmjereni vodostaj jedne letve.
type Rekord struct {
	Voda  string  `json:"voda"`
	Letva string  `json:"letva"`
	Cm    float64 `json:"cm"`
	Kad   string  `json:"kad"` // „25. 6. 1965.” ili „13. 6. 2013. u 19 h”
}

// letveRekorda su letve čiji se rekord pokazuje: glavne naše na Dunavu i
// Dravi, redom niz tok.
var letveRekorda = []struct{ voda, letva string }{
	{"Dunav", "batina"}, {"Dunav", "vukovar"}, {"Dunav", "ilok"},
	{"Drava", "botovo"}, {"Drava", "donji-miholjac"}, {"Drava", "osijek"},
}

// DesetljecaOsvjezavanje je koliko dugo vrijedi brojanje po desetljećima:
// prolazi kroz svaki zapis arhive, pa se radi jednom na dan, u pozadini.
const DesetljecaOsvjezavanje = 24 * time.Hour

type brojkeStanje struct {
	mu  sync.Mutex
	b   *BrojkePodataka
	kad time.Time

	desetljeca    []repository.DesetljeceZapisa
	desetljecaKad time.Time
	broji         bool // brojanje po desetljećima u tijeku
}

// ShowBrojke vraća brojke podataka kao JSON.
func (s *Server) ShowBrojke(w http.ResponseWriter, r *http.Request) {
	st := &s.brojkeStanje
	st.mu.Lock()
	if st.b == nil || time.Since(st.kad) > BrojkeOsvjezavanje {
		st.b = s.izbrojiPodatke(r.Context())
		st.kad = time.Now()
	}
	if !st.broji && (st.desetljeca == nil || time.Since(st.desetljecaKad) > DesetljecaOsvjezavanje) {
		if a := s.Arhiva(); a != nil {
			st.broji = true
			go func() {
				d, err := a.PoDesetljecima(context.Background())
				st.mu.Lock()
				defer st.mu.Unlock()
				st.broji = false
				st.desetljecaKad = time.Now()
				if err == nil {
					st.desetljeca = d
				}
			}()
		}
	}
	kopija := *st.b
	kopija.Desetljeca = st.desetljeca
	b := &kopija
	st.mu.Unlock()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(b)
}

// izbrojiPodatke broji sve što sustav nosi. Što se ne može izbrojiti (čvor
// bez arhive, bez prognoza) ostaje nula i stranica tu pločicu ne pokazuje.
func (s *Server) izbrojiPodatke(ctx context.Context) *BrojkePodataka {
	b := &BrojkePodataka{Izracunato: time.Now()}
	godina := func(od string) int {
		if len(od) >= 4 {
			g, _ := strconv.Atoi(od[:4])
			return g
		}
		return 0
	}
	najranija := func(trenutna *int, od string) {
		if g := godina(od); g > 0 && (*trenutna == 0 || g < *trenutna) {
			*trenutna = g
		}
	}
	letve := map[string]bool{}
	if a := s.Arhiva(); a != nil {
		if ab, err := a.Brojke(ctx); err == nil && ab != nil {
			// Stvarni kišomjer ima satni i dnevni niz: postaje se broje po
			// izvoru, većim od dvaju.
			postaja := map[string]int{}
			for _, red := range ab.Redovi {
				b.Zapisa += red.Zapisa
				switch {
				case red.Velicina == "vodostaj" && red.Vrsta == "satni":
					b.SatnihVodostaja += red.Zapisa
					najranija(&b.SatniOd, red.Od)
				case red.Velicina == "vodostaj":
					b.DnevnihVodostaja += red.Zapisa
					najranija(&b.DnevniOd, red.Od)
				case red.Velicina == "protok":
					b.Protoka += red.Zapisa
					najranija(&b.ProtokOd, red.Od)
				case red.Velicina == "oborina" && strings.HasPrefix(red.Izvor, "kisomjer-"):
					b.OborinaPrave += red.Zapisa
					postaja[red.Izvor] = max(postaja[red.Izvor], red.Letvi)
					najranija(&b.PraveOd, red.Od)
				case red.Velicina == "oborina":
					b.OborinaIzvedene += red.Zapisa
					if strings.HasPrefix(red.Izvor, "openmeteo-era5") && red.Letvi > b.TocakaIzvedenih {
						b.TocakaIzvedenih = red.Letvi
					}
					najranija(&b.IzvedeneOd, red.Od)
				case red.Velicina == "snijeg" || red.Velicina == "temperatura-zraka" || red.Velicina == "visina-snijega":
					b.Meteo += red.Zapisa
				}
			}
			for _, n := range postaja {
				b.PostajaPravih += n
			}
			for _, l := range ab.Letve {
				letve[l] = true
			}
			b.Profili, b.ProfilTocke, b.HQ = ab.Profili, ab.ProfilTocke, ab.HQ
			if g := godina(ab.NajstarijiOd); g > 0 {
				b.Godina = time.Now().Year() - g
				if t, err := time.Parse("2006-01-02", ab.NajstarijiOd[:min(10, len(ab.NajstarijiOd))]); err == nil {
					b.NajstarijiDan = strconv.Itoa(t.Day()) + ". " + strconv.Itoa(int(t.Month())) + ". " + strconv.Itoa(t.Year()) + "."
				}
				b.NajstarijaLetva = ab.NajstarijaLetva
				if s.db != nil {
					var ime string
					if s.db.QueryRowContext(ctx, `SELECT name FROM stations WHERE code = ?`, ab.NajstarijaLetva).Scan(&ime) == nil && ime != "" {
						b.NajstarijaLetva = ime
					}
				}
			}
		}
	}
	if a := s.Arhiva(); a != nil {
		for _, lr := range letveRekorda {
			cm, kad, ok := a.NajviseIzmjereno(ctx, lr.letva)
			if !ok {
				continue
			}
			ime := lr.letva
			if s.db != nil {
				var n string
				if s.db.QueryRowContext(ctx, `SELECT name FROM stations WHERE code = ?`, lr.letva).Scan(&n) == nil && n != "" {
					ime = n
				}
			}
			b.Rekordi = append(b.Rekordi, Rekord{Voda: lr.voda, Letva: ime, Cm: cm, Kad: danRekorda(kad)})
		}
	}
	if s.db != nil {
		_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM readings`).Scan(&b.Ocitanja)
		od := time.Now().UTC().Add(-24 * time.Hour)
		_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM readings WHERE measured_at >= ?`, od).Scan(&b.Ocitanja24h)
		if rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT s.code FROM readings r JOIN stations s ON s.id = r.station_id
			WHERE r.measured_at >= ?`, od); err == nil {
			for rows.Next() {
				var l string
				if rows.Scan(&l) == nil {
					letve[l] = true
					b.LetviUzivo++
				}
			}
			rows.Close()
		}
	}
	b.Letvi = len(letve)
	if p := s.Prognoze(); p != nil {
		b.Prognoza, b.Izdanja = p.Brojke()
	}
	podaci := ""
	if s.dbPath != "" {
		podaci = filepath.Dir(s.dbPath)
		b.Provjera = redakaProvjere(podaci)
	}
	b.Velicine = s.velicinePodataka(podaci)
	for _, v := range b.Velicine {
		b.Bajtova += v.Bajtova
	}
	return b
}

// velicinePodataka mjeri baze i izvorne datoteke, po dijelovima.
func (s *Server) velicinePodataka(podaci string) []Velicina {
	baza := func(ime string) int64 {
		if podaci == "" {
			return 0
		}
		var n int64
		for _, dodatak := range []string{"", "-wal"} {
			if fi, err := os.Stat(filepath.Join(podaci, ime+dodatak)); err == nil {
				n += fi.Size()
			}
		}
		return n
	}
	var provjera int64
	if podaci != "" {
		if m, _ := filepath.Glob(filepath.Join(podaci, "hindcast*")); len(m) > 0 {
			for _, f := range m {
				if fi, err := os.Stat(f); err == nil && !fi.IsDir() {
					provjera += fi.Size()
				}
			}
		}
	}
	out := []Velicina{
		{"arhiva vodostaja i meteorologije", s.velicinaArhive(baza)},
		{"izvorne datoteke nizova", velicinaStabla(s.podaciDir)},
		{"radna baza", baza("gocop.db")},
		{"prognoze i provjera unatrag", baza("prognoze.db") + provjera},
		{"oborine uživo", baza("oborine.db")},
	}
	var ima []Velicina
	for _, v := range out {
		if v.Bajtova > 0 {
			ima = append(ima, v)
		}
	}
	return ima
}

// velicinaArhive mjeri arhivu ondje gdje stvarno stoji: može biti na drugom
// disku, odvojena od baze (postavka arhiva).
func (s *Server) velicinaArhive(baza func(string) int64) int64 {
	if s.arhivaPut == "" {
		return baza("vodostaji.db")
	}
	var n int64
	for _, dodatak := range []string{"", "-wal"} {
		if fi, err := os.Stat(s.arhivaPut + dodatak); err == nil {
			n += fi.Size()
		}
	}
	return n
}

// velicinaStabla zbraja veličine svih datoteka u stablu.
func velicinaStabla(koren string) int64 {
	if koren == "" {
		return 0
	}
	var n int64
	_ = filepath.WalkDir(koren, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			n += fi.Size()
		}
		return nil
	})
	return n
}

// redakaProvjere broji prognoze puštene unatrag kroz arhivu: retke datoteka
// provjere (satni lanac 2023.–2025. i poplavni valovi), bez zaglavlja.
func redakaProvjere(podaci string) int64 {
	m, _ := filepath.Glob(filepath.Join(podaci, "hindcast*.csv"))
	var n int64
	for _, f := range m {
		n += brojRedaka(f) - 1
	}
	return max(n, 0)
}

func brojRedaka(put string) int64 {
	f, err := os.Open(put)
	if err != nil {
		return 0
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	buf := make([]byte, 1<<20)
	var n int64
	for {
		k, err := r.Read(buf)
		n += int64(bytes.Count(buf[:k], []byte{'\n'}))
		if err == io.EOF || err != nil {
			return n
		}
	}
}

// danRekorda piše kad je rekord bio: dnevna vrijednost samo danom, satna i
// satom, po lokalnom vremenu.
func danRekorda(t time.Time) string {
	if t.Hour() == 0 && t.Minute() == 0 {
		return strconv.Itoa(t.Day()) + ". " + strconv.Itoa(int(t.Month())) + ". " + strconv.Itoa(t.Year()) + "."
	}
	l := t.In(models.Zagreb)
	return strconv.Itoa(l.Day()) + ". " + strconv.Itoa(int(l.Month())) + ". " + strconv.Itoa(l.Year()) + ". u " + strconv.Itoa(l.Hour()) + " h"
}
