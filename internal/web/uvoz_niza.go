package web

import (
	"bufio"
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gocop/internal/arhiva"
)

// Vrata arhive: datoteka bilo kakvog oblika ulazi, kanonski CSV izlazi.
// Normalizacija se događa ovdje, na ulazu, i samo ovdje — gradnja i dalje čita
// jedan oblik. Dva čitača bi se s vremenom tiho razišla, a razlika bi se
// vidjela tek kad netko usporedi dva broja koja bi morala biti ista.

// najveciUvoz je gornja granica datoteke koja se prima. Godina satnih
// vrijednosti je oko 200 kB; deset godina stane u nekoliko megabajta.
const najveciUvoz = 64 << 20

// uzorakRedaka je koliko se redaka pokazuje čovjeku prije upisa.
const uzorakRedaka = 8

// Pogodak je ono što je program pročitao iz datoteke prije nego je čovjek
// išta potvrdio. Sve se smije ispraviti; ništa se ne upisuje dok se ne potvrdi.
type Pogodak struct {
	Razdjelnik       string
	Zaglavlje        []string
	StupacVrijeme    int
	StupacVrijednost int
	Uzorak           [][]string
	Redaka           int // koliko redaka datoteka ima, bez zaglavlja
}

// citajTablicu vadi retke iz CSV-a ili Excela. Excel se čita postojećim
// čitačem koji već služi uvozu očitanja.
func citajTablicu(ime string, sadrzaj []byte) ([][]string, string, error) {
	if strings.EqualFold(filepath.Ext(ime), ".xlsx") {
		redci, err := procitajXLSX(sadrzaj)
		return redci, "", err
	}
	razdjelnik := pogodiRazdjelnik(sadrzaj)
	var out [][]string
	s := bufio.NewScanner(bytes.NewReader(sadrzaj))
	s.Buffer(make([]byte, 1<<20), 1<<20)
	for s.Scan() {
		redak := strings.TrimPrefix(strings.TrimRight(s.Text(), "\r"), "\ufeff")
		if strings.TrimSpace(redak) == "" || strings.HasPrefix(redak, "#") {
			continue
		}
		out = append(out, strings.Split(redak, razdjelnik))
	}
	return out, razdjelnik, s.Err()
}

// pogodiRazdjelnik bira onaj koji u prvim redcima daje najviše stupaca i
// jednak broj u svakom retku. Točka-zarez ide prvi jer je to ono što
// hrvatski Excel izvozi.
func pogodiRazdjelnik(sadrzaj []byte) string {
	uzorak := sadrzaj
	if len(uzorak) > 8192 {
		uzorak = uzorak[:8192]
	}
	redci := strings.Split(string(uzorak), "\n")
	if len(redci) > 10 {
		redci = redci[:10]
	}
	najbolji, najviše := ";", 0
	for _, r := range []string{";", "\t", ",", "|"} {
		stupaca, jednako := 0, true
		for i, redak := range redci {
			redak = strings.TrimSpace(redak)
			if redak == "" || strings.HasPrefix(redak, "#") {
				continue
			}
			n := len(strings.Split(redak, r))
			if stupaca == 0 {
				stupaca = n
			} else if n != stupaca {
				jednako = false
			}
			_ = i
		}
		if jednako && stupaca > najviše {
			najbolji, najviše = r, stupaca
		}
	}
	return najbolji
}

// pogodi traži stupac vremena i stupac vrijednosti. Vrijeme je prvi stupac u
// kojem se svi uzorci daju pročitati kao datum; vrijednost prvi sljedeći u
// kojem se daju pročitati kao broj.
func pogodi(ime string, sadrzaj []byte) (*Pogodak, [][]string, error) {
	redci, razdjelnik, err := citajTablicu(ime, sadrzaj)
	if err != nil {
		return nil, nil, err
	}
	if len(redci) < 2 {
		return nil, nil, fmt.Errorf("datoteka nema ni zaglavlje ni jedan redak")
	}
	p := &Pogodak{Razdjelnik: razdjelnik, Zaglavlje: redci[0], StupacVrijeme: -1, StupacVrijednost: -1}
	tijelo := redci[1:]
	p.Redaka = len(tijelo)

	proba := tijelo
	if len(proba) > 20 {
		proba = proba[:20]
	}
	for stupac := 0; stupac < len(p.Zaglavlje); stupac++ {
		if p.StupacVrijeme < 0 && sviSuVrijeme(proba, stupac) {
			p.StupacVrijeme = stupac
			continue
		}
		if p.StupacVrijeme >= 0 && p.StupacVrijednost < 0 && sviSuBroj(proba, stupac) {
			p.StupacVrijednost = stupac
		}
	}
	for i, r := range tijelo {
		if i >= uzorakRedaka {
			break
		}
		p.Uzorak = append(p.Uzorak, r)
	}
	return p, tijelo, nil
}

func celija(r []string, i int) string {
	if i < 0 || i >= len(r) {
		return ""
	}
	return strings.TrimSpace(r[i])
}

func sviSuVrijeme(redci [][]string, stupac int) bool {
	n := 0
	for _, r := range redci {
		s := celija(r, stupac)
		if s == "" {
			continue
		}
		if _, _, ok := procitajVrijeme(s); !ok {
			return false
		}
		n++
	}
	return n > 0
}

func sviSuBroj(redci [][]string, stupac int) bool {
	n := 0
	for _, r := range redci {
		s := celija(r, stupac)
		if s == "" {
			continue
		}
		if _, ok := procitajBroj(s); !ok {
			return false
		}
		n++
	}
	return n > 0
}

// oblici su zapisi vremena koji se pojavljuju u onome što stiže: ISO iz
// baza, hrvatski s točkom iz Excela, i oba s minutama ili bez njih.
var oblici = []struct {
	uzorak string
	poDanu bool
}{
	{"2006-01-02 15:04:05", false},
	{"2006-01-02T15:04:05", false},
	{"2006-01-02 15:04", false},
	{"2006-01-02T15:04", false},
	{"2006-01-02", true},
	{"02.01.2006. 15:04:05", false},
	{"02.01.2006 15:04:05", false},
	{"02.01.2006. 15:04", false},
	{"02.01.2006 15:04", false},
	{"02.01.2006.", true},
	{"02.01.2006", true},
	{"2.1.2006. 15:04", false},
	{"2.1.2006.", true},
	{"2.1.2006", true},
}

// procitajVrijeme vraća vrijeme i javlja je li zapis imao samo datum. Zona se
// ne primjenjuje ovdje nego pri upisu, jer o njoj odlučuje čovjek.
func procitajVrijeme(s string) (time.Time, bool, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false, false
	}
	for _, o := range oblici {
		if t, err := time.Parse(o.uzorak, s); err == nil {
			return t, o.poDanu, true
		}
	}
	// Excelov serijski broj: dana od 30.12.1899. Godine prije 1900. tako se ne
	// daju zapisati, pa se serijski broj prima samo ondje gdje je razuman.
	if f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64); err == nil && f > 1 && f < 80000 {
		dani := int(f)
		ostatak := f - float64(dani)
		t := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, dani).
			Add(time.Duration(ostatak * 24 * float64(time.Hour)))
		return t.Round(time.Minute), ostatak == 0, true
	}
	return time.Time{}, false, false
}

// procitajBroj prima i decimalni zarez i točku. Tisućice se ne razdvajaju, pa
// se točka u "1.234" čita kao decimalna — kako je i u našim datotekama.
func procitajBroj(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, " ", ""))
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
	return v, err == nil
}

// UvozNiza je ono što je čovjek potvrdio prije upisa.
type UvozNiza struct {
	Sliv, Letva, Izvor, Velicina, Vrsta, Zona string
	StupacVrijeme, StupacVrijednost           int
}

// pretvori pretvara pročitane retke u ono što se upisuje, i javlja što je
// preskočeno. Prazan redak nije greška — izvoz zna imati rep; redak koji se ne
// da pročitati jest, i broji se.
func pretvori(tijelo [][]string, u UvozNiza) (redci []arhiva.Redak, preskoceno int, err error) {
	zona := time.UTC
	if u.Zona != "" && u.Zona != "UTC" {
		l, e := time.LoadLocation(u.Zona)
		if e != nil {
			return nil, 0, fmt.Errorf("zona %q: %w", u.Zona, e)
		}
		zona = l
	}
	vidjeno := map[int64]bool{}
	for _, r := range tijelo {
		sv, sb := celija(r, u.StupacVrijeme), celija(r, u.StupacVrijednost)
		if sv == "" && sb == "" {
			continue
		}
		t, poDanu, ok := procitajVrijeme(sv)
		if !ok {
			preskoceno++
			continue
		}
		v, ok := procitajBroj(sb)
		if !ok {
			preskoceno++
			continue
		}
		if !arhiva.MogucaVrijednost(u.Velicina, v) {
			preskoceno++
			continue
		}
		if poDanu {
			// Dan je dan bez obzira na zonu; sat se čita u zoni izvora.
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		} else {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, zona).UTC()
		}
		if vidjeno[t.Unix()] {
			preskoceno++
			continue
		}
		vidjeno[t.Unix()] = true
		redci = append(redci, arhiva.Redak{Vrijeme: t, PoDanu: poDanu, Vrijednost: v})
	}
	return redci, preskoceno, nil
}
