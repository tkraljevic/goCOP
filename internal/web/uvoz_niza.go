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

// pogodiRazdjelnik bira onaj koji većinu redaka razlomi na isti broj stupaca,
// i to na najviše njih. Ne traži se da SVI redci budu jednaki: HIS2000 iznad
// podataka ima naslov i blok s metapodacima koji imaju svoj broj stupaca, a
// zbog njih je prva izvedba odbacivala točku-zarez i uzimala zarez — pa se
// onda ništa nije čitalo.
func pogodiRazdjelnik(sadrzaj []byte) string {
	uzorak := sadrzaj
	if len(uzorak) > 65536 {
		uzorak = uzorak[:65536]
	}
	var redci []string
	for _, r := range strings.Split(string(uzorak), "\n") {
		r = strings.TrimSpace(r)
		if r != "" && !strings.HasPrefix(r, "#") {
			redci = append(redci, r)
		}
	}
	if len(redci) > 50 {
		redci = redci[:50]
	}
	najbolji, najviseStupaca, najviseRedaka := ";", 1, 0
	for _, r := range []string{";", "\t", ",", "|"} {
		koliko := map[int]int{}
		for _, redak := range redci {
			koliko[len(strings.Split(redak, r))]++
		}
		// najčešći broj stupaca veći od jedan, i koliko ga redaka ima
		for stupaca, redaka := range koliko {
			if stupaca < 2 {
				continue
			}
			if stupaca > najviseStupaca || (stupaca == najviseStupaca && redaka > najviseRedaka) {
				najbolji, najviseStupaca, najviseRedaka = r, stupaca, redaka
			}
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
	// Gdje podaci počinju traži se, ne pretpostavlja. HIS2000 iznad njih ima
	// naslov, prazan redak i blok s metapodacima; drugi izvozi imaju jedno
	// zaglavlje ili nijedno. Prvi redak koji se čita kao vrijeme i vrijednost
	// je početak; onaj iznad njega je zaglavlje, ako ima jednako stupaca.
	pocetak, stVrijeme, stVrijednost := nadiPocetak(redci)
	if pocetak < 0 {
		return nil, nil, fmt.Errorf("ni u jednom retku se ne čitaju vrijeme i vrijednost jedno uz drugo")
	}
	p := &Pogodak{Razdjelnik: razdjelnik, StupacVrijeme: stVrijeme, StupacVrijednost: stVrijednost}
	tijelo := redci[pocetak:]
	p.Redaka = len(tijelo)
	if pocetak > 0 && len(redci[pocetak-1]) == len(redci[pocetak]) {
		p.Zaglavlje = redci[pocetak-1]
	} else {
		p.Zaglavlje = make([]string, len(redci[pocetak]))
		for i := range p.Zaglavlje {
			p.Zaglavlje[i] = fmt.Sprintf("stupac %d", i+1)
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

// nadiPocetak vraća prvi redak u kojem stoje vrijeme i broj jedno uz drugo, i
// koji su to stupci. Traži se prvi takav redak koji ima još barem dva slična
// za sobom — jedan usamljen redak koji slučajno izgleda kao podatak ne smije
// proglasiti početak.
func nadiPocetak(redci [][]string) (pocetak, stVrijeme, stVrijednost int) {
	for i := 0; i < len(redci); i++ {
		v, b := stupciRetka(redci[i])
		if v < 0 || b < 0 {
			continue
		}
		potvrda := 0
		for j := i + 1; j < len(redci) && j < i+4; j++ {
			if v2, b2 := stupciRetka(redci[j]); v2 == v && b2 == b {
				potvrda++
			}
		}
		if potvrda >= 1 || i == len(redci)-1 {
			return i, v, b
		}
	}
	return -1, -1, -1
}

// stupciRetka javlja koji stupac tog retka je vrijeme a koji vrijednost.
// Vrijednost se traži iza vremena, jer datum u brojčanom obliku inače zna
// proći kao broj.
func stupciRetka(r []string) (vrijeme, vrijednost int) {
	vrijeme, vrijednost = -1, -1
	for i := range r {
		s := celija(r, i)
		if s == "" {
			continue
		}
		if vrijeme < 0 {
			if _, _, ok := procitajVrijeme(s); ok {
				vrijeme = i
			}
			continue
		}
		if _, ok := procitajBroj(s); ok {
			vrijednost = i
			return
		}
	}
	return
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
	// HIS2000 daje sat bez minuta, a dan i mjesec poravnava razmacima:
	// " 1. 1.2002  0". Razmaci se prije čitanja skupe u jedan.
	{"2.1.2006 15", false},
	{"2006-01-02 15", false},
}

// procitajVrijeme vraća vrijeme i javlja je li zapis imao samo datum. Zona se
// ne primjenjuje ovdje nego pri upisu, jer o njoj odlučuje čovjek.
func procitajVrijeme(s string) (time.Time, bool, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false, false
	}
	s = skupiRazmake(s)
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

// skupiRazmake svodi poravnavanje razmacima na jedan razmak i miče razmak iza
// točke: " 1. 1.2002  0" postaje "1.1.2002 0". HIS2000 tako poravnava stupce.
func skupiRazmake(s string) string {
	var b strings.Builder
	razmak := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			razmak = true
			continue
		}
		if razmak && b.Len() > 0 && !strings.HasSuffix(b.String(), ".") {
			b.WriteByte(' ')
		}
		razmak = false
		b.WriteRune(r)
	}
	return b.String()
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
	// Nacin je "dopuni" ili "zamijeni": zadržava li se ono što niz već ima u
	// stablu ili se sve baca i piše iznova.
	Nacin string
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
		if sb == "" {
			continue // vrijeme bez vrijednosti: mjerenja nema, nije greška
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
