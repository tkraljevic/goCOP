// Čitanje HIS-2000 izvoza: prepoznavanje po zaglavlju i razlaganje datoteka.
package his2000

import (
	"bufio"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
)

// Vrsta govori što je u datoteci: koja veličina i kako gusto.
type Vrsta struct {
	Velicina string // vodostaj, protok, temperatura, koncentracija, pronos
	Gustoca  string // satni, srednjak, dnevni
	Stupac   string // naziv stupca u zapisu, npr. vodostaj_cm
}

// Sadrzaj je jedna pročitana datoteka izvoza.
type Sadrzaj struct {
	Ime        string
	Vrsta      Vrsta // prazno kad datoteka nije niz
	Niz        []Vrijednost
	Krivulje   []Krivulja // kad je datoteka krivulja protoka
	Profil     *Profil    // kad je datoteka snimka korita
	Postaja    Postaja    // zaglavlje uz krivulje: kota nule i koordinate
	Preskoceno int        // sati koji lokalno ne postoje
	OdGodine   int        // prva i zadnja godina koju datoteka pokriva, i kad
	DoGodine   int        // je prazna: izvoz je prošao, mjerenja nema
}

// Vrijednost je jedno očitanje; Dan je true kad izvor daje samo datum.
type Vrijednost struct {
	Kad time.Time
	Dan bool
	V   string
}

// Krivulja je jedno razdoblje krivulje protoka s odsječcima.
type Krivulja struct {
	Od, Do   time.Time
	Odsjecci []Odsjecak
}

// Odsjecak vrijedi u rasponu vodostaja i nosi koeficijente.
//
// HIS krivulju slaže od dva oblika, a stupac „Tip" kaže od kojeg:
//
//	Tip 0   Q = A(H + B)^C + D     potencija, s pomakom
//	Tip 1   Q = A·H² + B·H + C     polinom
//
// Potencija je gotovo uvijek najniži odsječak krivulje, a pomak D drži protok
// na malom vodostaju. Dugo se čitao samo polinom, pa je malom vodostaju
// ispadala krivulja: na Novom Virju 45 od 131 odsječka, i šest godina bez
// ijedne krivulje.
type Odsjecak struct {
	OdCm, DoCm     int
	Oblik          string // models.OblikPolinom ili models.OblikPotencija
	P1, P2, P3, P4 string
}

// Profil je snimka poprečnog profila korita.
type Profil struct {
	Datum    time.Time
	Vodostaj int    // vodostaj pri mjerenju, cm
	KotaNule string // kota nule kakvu uz snimku vodi HIS
	// Sustav visina u kojem su kote snimke. HIS ga ne piše — izvodi se pri
	// uvozu, usporedbom kote nule s onom koju letva vodi. Bez njega se ne zna
	// je li snimka u trščanskom ili HVRS71, a razlika je na Dravi dvadesetak
	// centimetara: premalo da iskoči kao greška, dovoljno da izgleda kao da
	// se korito produbilo.
	Sustav string
	Tocke  []Tocka
}

// Tocka je udaljenost od početka i apsolutna kota dna.
type Tocka struct{ Stacionaza, Visina string }

// Zaglavlje postaje iz datoteke krivulja: kota nule i koordinate.
type Postaja struct {
	Sifra, Naziv, Vodotok string
	KotaNule              string
	Sirina, Duzina        string
}

var (
	reSatni  = regexp.MustCompile(`(?i)^Satni podaci postaje\s+(.+?)\s+za godinu\s+\d{4},\s+(\p{L}+)`)
	reDnevni = regexp.MustCompile(`(?i)^Dnevni podaci postaje\s+(.+?),\s+(\p{L}+)`)
	reRedSat = regexp.MustCompile(`^\s*(\d{1,2})\.\s*(\d{1,2})\.(\d{4})\s+(\d{1,2});([^;]*);`)
	// dnevni redak: 01.01.1962;176; ili 01/01/1962;176;
	reRedDan  = regexp.MustCompile(`^\s*(\d{1,2})[./](\d{1,2})[./](\d{4});([^;]*);`)
	reRazdob  = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})/(\d{4})\s*-\s*(\d{1,2})/(\d{1,2})/(\d{4})`)
	reDatumUS = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})/(\d{4})$`)
)

// velicine preslikava naziv iz zaglavlja u ono kako se veličina zove kod nas.
// Dnevna vrijednost vodostaja, protoka i temperature je srednjak — provjereno
// usporedbom s satnim nizom; koncentracija i pronos dolaze samo kao dnevni
// podatak, pa se tako i zovu.
var velicine = map[string]Vrsta{
	"VODOSTAJ":      {"vodostaj", "srednjak", "vodostaj_cm"},
	"PROTOK":        {"protok", "srednjak", "protok_m3s"},
	"TEMPERATURA":   {"temperatura", "srednjak", "temperatura_c"},
	"KONCENTRACIJA": {"koncentracija", "dnevni", "koncentracija_gm3"},
	"PRONOS":        {"pronos", "dnevni", "pronos_t"},
}

// Procitaj razlaže jednu datoteku izvoza. Datoteke su u cp1250 i imaju
// zaglavlje koje kaže što je unutra; po njemu se i prepoznaju, jer ime
// datoteke bira onaj tko izvozi i razlikuje se od postaje do postaje.
func Procitaj(ime string, sirovo []byte) (*Sadrzaj, error) {
	redci := razloziRedke(sirovo)
	if len(redci) == 0 {
		return nil, fmt.Errorf("datoteka je prazna")
	}
	s := &Sadrzaj{Ime: ime}
	prvi := strings.TrimSpace(redci[0])
	switch {
	case strings.HasPrefix(strings.ToLower(prvi), "krivulje protoka"):
		k, p, err := citajKrivulje(redci)
		if err != nil {
			return nil, err
		}
		s.Krivulje, s.Postaja = k, p
		return s, nil
	case strings.HasPrefix(strings.ToLower(prvi), "mjerenje poprečnog profila"):
		p, err := citajProfil(redci)
		if err != nil {
			return nil, err
		}
		s.Profil = p
		return s, nil
	}
	if strings.HasPrefix(prvi, "# letva.voda.hr") {
		// Satni vodostaj preuzet s letva.voda.hr (Hidrologija → Satni podaci
		// → Tekst. datoteka), godina po godinu u jednu datoteku. Vrijeme je
		// lokalno, kao u HIS-u.
		// isti oblik daje i protok elektrana iz Obrane od poplava
		v := velicine["VODOSTAJ"]
		if strings.Contains(prvi, "Protok") {
			v = velicine["PROTOK"]
		}
		v.Gustoca = "satni"
		s.Vrsta = v
		s.Niz, s.Preskoceno = citajLetvu(redci)
		if len(s.Niz) > 0 {
			s.OdGodine, s.DoGodine = s.Niz[0].Kad.Year(), s.Niz[len(s.Niz)-1].Kad.Year()
		}
		return s, nil
	}
	if m := reSatni.FindStringSubmatch(prvi); m != nil {
		v, ok := velicine[strings.ToUpper(m[2])]
		if !ok {
			return nil, fmt.Errorf("nepoznata veličina %q", m[2])
		}
		v.Gustoca = "satni"
		s.Vrsta = v
		s.Niz, s.Preskoceno = citajSatne(redci)
		s.OdGodine, s.DoGodine = razdobljeSatnih(redci)
		if len(s.Niz) == 0 && imaIspisPoMjesecima(redci) {
			return nil, fmt.Errorf("satni podaci su u obliku ispisa, koji se ne čita; izvezite ih kao CSV")
		}
		return s, nil
	}
	if m := reDnevni.FindStringSubmatch(prvi); m != nil {
		v, ok := velicine[strings.ToUpper(m[2])]
		if !ok {
			return nil, fmt.Errorf("nepoznata veličina %q", m[2])
		}
		s.Vrsta = v
		// ista veličina dolazi i kao popis redaka i kao ispis po mjesecima
		if s.Niz = citajDnevne(redci); len(s.Niz) == 0 {
			s.Niz = citajIspisDnevni(redci)
		}
		s.OdGodine, s.DoGodine = razdobljeDnevnih(redci)
		return s, nil
	}
	return nil, fmt.Errorf("zaglavlje ne kaže što je u datoteci: %.60q", prvi)
}

func razloziRedke(sirovo []byte) []string {
	tekst := izCP1250(sirovo)
	sc := bufio.NewScanner(strings.NewReader(tekst))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var out []string
	for sc.Scan() {
		out = append(out, strings.TrimRight(sc.Text(), "\r"))
	}
	return out
}

// izCP1250 pretvara zapis iz srednjoeuropske kodne stranice. Pretvorba je
// kratka tablica jer HIS piše samo naša slova i stupnjeve.
func izCP1250(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		if c < 0x80 {
			sb.WriteByte(c)
			continue
		}
		if r, ok := cp1250[c]; ok {
			sb.WriteRune(r)
			continue
		}
		sb.WriteRune('?')
	}
	return sb.String()
}

func citajSatne(redci []string) ([]Vrijednost, int) {
	var out []Vrijednost
	preskoceno := 0
	for _, r := range redci {
		m := reRedSat.FindStringSubmatch(r)
		if m == nil {
			continue
		}
		v := strings.TrimSpace(m[5])
		if v == "" {
			continue
		}
		d, mj, g, h := broj(m[1]), broj(m[2]), broj(m[3]), broj(m[4])
		kad := time.Date(g, time.Month(mj), d, h, 0, 0, 0, time.UTC)
		if NepostojeciSat(kad) {
			preskoceno++
			continue
		}
		out = append(out, Vrijednost{Kad: kad, V: v})
	}
	return out, preskoceno
}

// reRedLetva je redak satnih podataka s letva.voda.hr: "01.06.2010. 00 h     134"
var reRedLetva = regexp.MustCompile(`^(\d{2})\.(\d{2})\.(\d{4})\. (\d{2}) h\s+(-?\d+(?:,\d+)?)\s*$`)

// citajLetvu čita satne retke s letva.voda.hr. Sat koji se pri prelasku na
// zimsko vrijeme ponovi uzima se jednom, prvi put — lokalni sat ga ne može
// razlikovati, a vrijeme niza je lokalno.
func citajLetvu(redci []string) ([]Vrijednost, int) {
	var out []Vrijednost
	vidjeno := map[time.Time]bool{}
	preskoceno := 0
	for _, r := range redci {
		m := reRedLetva.FindStringSubmatch(strings.TrimSpace(r))
		if m == nil {
			continue
		}
		kad := time.Date(broj(m[3]), time.Month(broj(m[2])), broj(m[1]), broj(m[4]), 0, 0, 0, time.UTC)
		if NepostojeciSat(kad) || vidjeno[kad] {
			preskoceno++
			continue
		}
		vidjeno[kad] = true
		out = append(out, Vrijednost{Kad: kad, V: m[5]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kad.Before(out[j].Kad) })
	return out, preskoceno
}

func citajDnevne(redci []string) []Vrijednost {
	var polja [][4]string
	for _, r := range redci {
		if m := reRedDan.FindStringSubmatch(r); m != nil && strings.TrimSpace(m[4]) != "" {
			polja = append(polja, [4]string{m[1], m[2], m[3], strings.TrimSpace(m[4])})
		}
	}
	danPrvi := danIdePrvi(polja)
	var out []Vrijednost
	for _, p := range polja {
		d, mj := broj(p[0]), broj(p[1])
		if !danPrvi {
			d, mj = mj, d
		}
		if d < 1 || d > 31 || mj < 1 || mj > 12 {
			continue
		}
		out = append(out, Vrijednost{Kad: time.Date(broj(p[2]), time.Month(mj), d, 0, 0, 0, 0, time.UTC), Dan: true, V: p[3]})
	}
	return out
}

// danIdePrvi javlja piše li izvoz datum kao dan.mjesec ili mjesec.dan. HIS
// oba oblika koristi: dnevne tablice daju dan prvi, a razdoblja krivulja
// mjesec prvi. Odlučuje se po podacima: vrijednost veća od dvanaest može biti
// samo dan.
func danIdePrvi(polja [][4]string) bool {
	for _, p := range polja {
		if broj(p[0]) > 12 {
			return true
		}
		if broj(p[1]) > 12 {
			return false
		}
	}
	return true
}

// razdobljeSatnih vraća prvu i zadnju godinu koju datoteka pokriva, bez
// obzira ima li u njoj ijedne vrijednosti. Prazna datoteka tako i dalje kaže
// za koje je razdoblje izvoz prošao.
func razdobljeSatnih(redci []string) (int, int) {
	return razdoblje(redci, reRedSat, 3)
}

// razdobljeDnevnih isto to za dnevne datoteke
func razdobljeDnevnih(redci []string) (int, int) {
	return razdoblje(redci, reRedDan, 3)
}

func razdoblje(redci []string, uzorak *regexp.Regexp, skupina int) (od, do_ int) {
	for _, r := range redci {
		m := uzorak.FindStringSubmatch(r)
		if m == nil {
			continue
		}
		g := broj(m[skupina])
		if g == 0 {
			continue
		}
		if od == 0 || g < od {
			od = g
		}
		if g > do_ {
			do_ = g
		}
	}
	return od, do_
}

// NepostojeciSat javlja je li to sat koji u našoj zoni ne postoji — noć
// prelaska na ljetno vrijeme. HIS ga svejedno ispiše, a mi ga preskačemo,
// jer bi se pri pretvorbi u UTC slio sa sljedećim satom i pregazio ga.
func NepostojeciSat(kad time.Time) bool {
	lokalno := time.Date(kad.Year(), kad.Month(), kad.Day(), kad.Hour(), 0, 0, 0, models.Zagreb)
	return lokalno.Hour() != kad.Hour()
}

func citajKrivulje(redci []string) ([]Krivulja, Postaja, error) {
	var out []Krivulja
	var p Postaja
	var tek *Krivulja
	for i, r := range redci {
		if strings.HasPrefix(r, "Šifra;") && i+1 < len(redci) {
			d := strings.Split(redci[i+1], ";")
			if len(d) >= 8 {
				p = Postaja{Sifra: d[0], Naziv: d[3], Vodotok: d[4], KotaNule: d[5], Sirina: d[6], Duzina: d[7]}
			}
			continue
		}
		if m := reRazdob.FindStringSubmatch(strings.TrimSpace(r)); m != nil {
			if tek != nil {
				out = append(out, *tek)
			}
			tek = &Krivulja{
				Od: time.Date(broj(m[3]), time.Month(broj(m[1])), broj(m[2]), 0, 0, 0, 0, time.UTC),
				Do: time.Date(broj(m[6]), time.Month(broj(m[4])), broj(m[5]), 0, 0, 0, 0, time.UTC),
			}
			continue
		}
		d := strings.Split(r, ";")
		if tek == nil || len(d) < 7 {
			continue
		}
		// HIS oba oblika piše istim stupcima A;B;C;D, ali im značenje nije
		// isto, pa se ne smiju prepisati redom:
		//
		//	Tip 1   Q = A·H² + B·H + C        → p1=A, p2=B, p3=C
		//	Tip 0   Q = A(H + B)^C + D        → p1=A, p2=C, p3=B, p4=D
		//
		// Kod potencije su eksponent i pomak zamijenjeni, jer naš zapis drži
		// pomak uz H, a eksponent izvan zagrade. Prepiše li ih se redom,
		// protok ispadne kriv, a ništa ne pukne.
		o := Odsjecak{OdCm: cijeli(d[1]), DoCm: cijeli(d[2]), P1: bezNula(d[3])}
		switch strings.TrimSpace(d[0]) {
		case "1":
			o.Oblik = models.OblikPolinom
			o.P2, o.P3 = bezNula(d[4]), bezNula(d[5])
		case "0":
			o.Oblik = models.OblikPotencija
			o.P2, o.P3, o.P4 = bezNula(d[5]), bezNula(d[4]), bezNula(d[6])
		default:
			continue
		}
		tek.Odsjecci = append(tek.Odsjecci, o)
	}
	if tek != nil {
		out = append(out, *tek)
	}
	if len(out) == 0 {
		return nil, p, fmt.Errorf("u datoteci nema nijedne krivulje")
	}
	return out, p, nil
}

func citajProfil(redci []string) (*Profil, error) {
	p := &Profil{}
	zaglavlje := -1 // redak s podacima o postaji, koji nije točka profila
	for i, r := range redci {
		if i == zaglavlje {
			continue
		}
		if strings.HasPrefix(r, "Šifra;") && i+1 < len(redci) {
			zaglavlje = i + 1
			d := strings.Split(redci[i+1], ";")
			if len(d) >= 10 {
				p.KotaNule = strings.TrimSpace(d[5])
				p.Vodostaj = cijeli(d[8])
				if m := reDatumUS.FindStringSubmatch(strings.TrimSpace(d[9])); m != nil {
					p.Datum = time.Date(broj(m[3]), time.Month(broj(m[1])), broj(m[2]), 0, 0, 0, 0, time.UTC)
				}
			}
			continue
		}
		d := strings.Split(r, ";")
		if len(d) < 2 {
			continue
		}
		st, vi := strings.TrimSpace(d[0]), strings.TrimSpace(d[1])
		if st == "" || vi == "" || !decimalan(st) || !decimalan(vi) {
			continue
		}
		p.Tocke = append(p.Tocke, Tocka{Stacionaza: mjera(st), Visina: mjera(vi)})
	}
	if p.Datum.IsZero() {
		return nil, fmt.Errorf("snimka nema datum mjerenja")
	}
	if len(p.Tocke) < 3 {
		return nil, fmt.Errorf("snimka ima samo %d točaka", len(p.Tocke))
	}
	return p, nil
}

func decimalan(s string) bool {
	_, err := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
	return err == nil
}

func broj(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }

func cijeli(s string) int {
	f, err := strconv.ParseFloat(strings.Replace(strings.TrimSpace(s), ",", ".", 1), 64)
	if err != nil {
		return 0
	}
	return int(f)
}

// bezNula skraćuje 43,2420 na 43,242 — nule iza zareza ništa ne kažu, a
// koeficijent se poslije čita kao broj.
func bezNula(s string) string {
	s = strings.TrimSpace(s)
	// HIS decimale piše zarezom, ali izvoz zna doći i s točkom — Podravska
	// Moslavina tako. Arhiva ih vodi zarezom, pa se točka prvo pretvori;
	// bez toga bi „0.00" prošlo kao cijeli broj i dobilo još jednu decimalu.
	if strings.Contains(s, ".") && !strings.Contains(s, ",") {
		s = strings.ReplaceAll(s, ".", ",")
	}
	if !strings.Contains(s, ",") {
		return s
	}
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ",")
}

// mjera skraćuje izmjerenu duljinu: 110,00 je 110,0, a 90,113 ostaje kakav
// jest. Jedna decimala ostaje i kad su sve nule, jer se vidi da je mjereno u
// decimetrima, a ne zaokruženo na metar.
func mjera(s string) string {
	s = bezNula(s)
	if !strings.Contains(s, ",") {
		return s + ",0"
	}
	return s
}

// cp1250 su znakovi iznad ASCII-ja koje HIS uopće piše.
var cp1250 = map[byte]rune{
	0x8A: 'Š', 0x8C: 'Ś', 0x8D: 'Ť', 0x8E: 'Ž', 0x9A: 'š', 0x9C: 'ś', 0x9E: 'ž',
	0xA9: '©', 0xB0: '°', 0xBC: 'Ľ', 0xC8: 'Č', 0xC9: 'É', 0xCC: 'Ě', 0xD0: 'Đ',
	0xE8: 'č', 0xE9: 'é', 0xEC: 'ě', 0xF0: 'đ', 0xC6: 'Ć', 0xE6: 'ć', 0xDA: 'Ú',
	0xFA: 'ú', 0xDD: 'Ý', 0xFD: 'ý', 0xC1: 'Á', 0xE1: 'á', 0xCD: 'Í', 0xED: 'í',
	0xD3: 'Ó', 0xF3: 'ó', 0xD6: 'Ö', 0xF6: 'ö', 0xDC: 'Ü', 0xFC: 'ü',
}
