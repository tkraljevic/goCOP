// Vodomjerenja: pojedinačna mjerenja protoka na letvi. HIS ih daje kao
// radnu tablicu u SYLK obliku, s nastavkom .xls, ali to nije Excel nego
// tekst: redci C;K… nose vrijednost ćelije, a Y i X red i stupac.
//
// Njima se krivulje ne postavljaju — to radi DHMZ — ali služe za provjeru i
// za razdoblja u kojima službene krivulje nema.
package his2000

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Mjerenje je jedno vodomjerenje: kad, na kojem vodostaju i koliki protok.
type Mjerenje struct {
	Datum    time.Time
	Vodostaj string // cm, cijeli broj
	Brzina   string // srednja brzina, m/s
	Protok   string // m3/s
	Metoda   string // ADCP, krilo, iz komentara mjeritelja
}

var reCelija = regexp.MustCompile(`^C;(?:.*?;)??K(.*)$`)
var rePolozaj = regexp.MustCompile(`^[YX]\d+$`)

// JeSylk javlja je li datoteka radna tablica u SYLK obliku.
func JeSylk(sirovo []byte) bool {
	return strings.HasPrefix(string(sirovo), "ID;P")
}

// Najveći red i stupac koje radna tablica može imati (kao Excel). Položaj
// izvan toga, ili broj koji se ne da pročitati, znači da datoteka nije
// ispravna tablica.
const (
	najviseRedakaSylka  = 1 << 20
	najviseStupacaSylka = 1 << 14
)

// najviseCelijaSylka je najviše ćelija s vrijednošću. Najveća stvarna
// tablica vodomjerenja ima oko 4500 ćelija. Varijabla je zbog testova.
var najviseCelijaSylka = 500_000

// celijeSylka je pročitana tablica: vrijednosti po (red, stupac) i redovi
// koji imaju barem jednu vrijednost, po redu.
type celijeSylka struct {
	vrijednost map[[2]int]string
	redovi     []int
	vidjeni    map[int]bool
}

func (t *celijeSylka) dodaj(red, stupac int, v string) error {
	k := [2]int{red, stupac}
	if _, ima := t.vrijednost[k]; !ima && len(t.vrijednost) >= najviseCelijaSylka {
		return fmt.Errorf("tablica ima više od %d ćelija; tablica vodomjerenja ih ima nekoliko tisuća", najviseCelijaSylka)
	}
	if !t.vidjeni[red] {
		t.vidjeni[red] = true
		t.redovi = append(t.redovi, red)
	}
	t.vrijednost[k] = v
	return nil
}

// procitajCelije čita zapise ćelija. Zapis F; i C; pomiče položaj (Y red,
// X stupac); samo C; nosi vrijednost, u polju K.
func procitajCelije(sirovo []byte) (celijeSylka, error) {
	t := celijeSylka{vrijednost: map[[2]int]string{}, vidjeni: map[int]bool{}}
	red, stupac := 0, 0
	for _, r := range razloziRedke(sirovo) {
		if err := pomakniPolozaj(r, &red, &stupac); err != nil {
			return t, err
		}
		if v, ima := vrijednostCelije(r); ima {
			if err := t.dodaj(red, stupac, v); err != nil {
				return t, err
			}
		}
	}
	sort.Ints(t.redovi)
	return t, nil
}

// vrijednostCelije je vrijednost zapisa C; iz polja K, bez navodnika.
func vrijednostCelije(r string) (string, bool) {
	if !strings.HasPrefix(r, "C;") {
		return "", false
	}
	m := reCelija.FindStringSubmatch(r)
	if m == nil {
		return "", false
	}
	return strings.Trim(m[1], `"`), true
}

// pomakniPolozaj čita polja Y i X zapisa F; i C;, do polja K (vrijednost
// može nositi bilo kakav tekst). Drugi zapisi položaj ne mijenjaju.
func pomakniPolozaj(r string, red, stupac *int) error {
	if !strings.HasPrefix(r, "F;") && !strings.HasPrefix(r, "C;") {
		return nil
	}
	for _, polje := range strings.Split(r, ";")[1:] {
		if strings.HasPrefix(polje, "K") {
			return nil
		}
		if err := postaviPolozaj(polje, red, stupac); err != nil {
			return err
		}
	}
	return nil
}

// postaviPolozaj upisuje red (Y) ili stupac (X) iz polja; drugo polje ne
// dira. Broj izvan granice radne tablice je greška.
func postaviPolozaj(polje string, red, stupac *int) error {
	if !rePolozaj.MatchString(polje) {
		return nil
	}
	cilj, najvise, sto := red, najviseRedakaSylka, "red"
	if polje[0] == 'X' {
		cilj, najvise, sto = stupac, najviseStupacaSylka, "stupac"
	}
	n, err := strconv.Atoi(polje[1:])
	if err != nil || n < 1 || n > najvise {
		return fmt.Errorf("tablica ima %s %s, a smije imati od 1 do %d", sto, polje[1:], najvise)
	}
	*cilj = n
	return nil
}

// ProcitajMjerenja razlaže SYLK tablicu vodomjerenja. Stupci se traže po
// naslovu, jer im redoslijed nije zajamčen.
func ProcitajMjerenja(sirovo []byte) ([]Mjerenje, error) {
	tablica, err := procitajCelije(sirovo)
	if err != nil {
		return nil, err
	}
	celije := tablica.vrijednost
	stupci := map[string]int{}
	for p, v := range celije {
		if p[0] != 1 {
			continue
		}
		stupci[strings.ToLower(strings.TrimSpace(v))] = p[1]
	}
	datum, imaDatum := stupci["datum mjerenja"]
	protok, imaProtok := stupci["protok"]
	if !imaDatum || !imaProtok {
		return nil, fmt.Errorf("tablica nema stupce „Datum mjerenja” i „Protok”")
	}
	var out []Mjerenje
	for _, y := range tablica.redovi {
		if y < 2 {
			continue
		}
		dan, err := izSerijskog(celije[[2]int{y, datum}])
		if err != nil {
			continue
		}
		q := celije[[2]int{y, protok}]
		if strings.TrimSpace(q) == "" {
			continue
		}
		out = append(out, Mjerenje{
			Datum:    dan,
			Vodostaj: zaokruzi(celije[[2]int{y, stupci["vodostaj"]}], 0),
			Brzina:   zaokruzi(celije[[2]int{y, stupci["srednja brzina"]}], 2),
			Protok:   zaokruzi(q, 1),
			Metoda:   strings.TrimSpace(celije[[2]int{y, stupci["komentar"]}]),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("u tablici nema nijednog mjerenja")
	}
	return out, nil
}

// izSerijskog pretvara datum kakav radne tablice pamte: broj dana od
// 30.12.1899. Godina 1900 ondje je pogrešno prijestupna, pa brojevi manji od
// 61 ne bi bili pouzdani; vodomjerenja su ionako iz ovog stoljeća.
func izSerijskog(s string) (time.Time, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 61 {
		return time.Time{}, fmt.Errorf("%q nije datum radne tablice", s)
	}
	return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n), nil
}

// zaokruzi ispisuje broj na zadani broj decimala, s zarezom, jer radna
// tablica pamti punu preciznost stroja: 0,839999973773956 je 0,84.
func zaokruzi(s string, decimala int) string {
	f, err := strconv.ParseFloat(strings.Replace(strings.TrimSpace(s), ",", ".", 1), 64)
	if err != nil {
		return ""
	}
	return strings.Replace(strconv.FormatFloat(f, 'f', decimala, 64), ".", ",", 1)
}
