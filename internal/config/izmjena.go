package config

// Izmjena gocop.toml iz programa (Administracija, 12. 10. 2026.). Datoteka
// se ne piše iznova iz strukture, jer bi tada nestali ručni komentari i
// ključevi koje ovaj program ne zna (npr. iz novijeg izdanja). Mijenja se na
// mjestu: vrijednost postojećeg ključa zamijeni se u svom retku, a ključ
// kojeg nema doda se u svoj odjeljak s objašnjenjem, kao u primjeru pri
// prvom pokretanju. Novi tekst zatim se čita istim čitačem kao pri
// pokretanju; vrijedi samo ako se pročita i promijeni točno ono što je
// traženo.

import (
	"bytes"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// ProcitajTekst čita sadržaj datoteke postavki preko zadanih vrijednosti,
// isto kao Load pri pokretanju
func ProcitajTekst(data []byte) (Config, error) {
	cfg := Default()
	err := toml.Unmarshal(data, &cfg)
	return cfg, err
}

// Primjer je sadržaj nove datoteke postavki: uvod, pa sve postavke iz cfg s
// objašnjenjima i primjerima
func Primjer(cfg Config) ([]byte, error) {
	data, err := toml.Marshal(cfg)
	return append([]byte(zaglavlje), data...), err
}

// Izmijeni upisuje izmjene (puni ključ → vrijednost vrste te postavke) u
// tekst datoteke i vraća novi tekst s postavkama kakve iz njega čita
// program. Vraća grešku kad se stari ili novi tekst ne da pročitati, ili kad
// bi se promijenilo išta osim traženog.
func Izmijeni(stari []byte, izmjene map[string]any) ([]byte, Config, error) {
	prije, err := ProcitajTekst(stari)
	if err != nil {
		return nil, Config{}, fmt.Errorf("gocop.toml se ne da pročitati: %w", err)
	}
	tekst := stari
	ocekivano := prije
	for _, k := range Kljucevi() {
		v, ok := izmjene[k.Puni()]
		if !ok {
			continue
		}
		if reflect.TypeOf(v) != k.Tip {
			return nil, Config{}, fmt.Errorf("%s: vrijednost %v nije vrste %s", k.Oznaka(), v, k.Tip)
		}
		if tekst, err = postaviKljuc(tekst, k, v); err != nil {
			return nil, Config{}, err
		}
		ocekivano.Postavi(k, v)
	}
	poslije, err := ProcitajTekst(tekst)
	if err != nil {
		return nil, Config{}, fmt.Errorf("izmijenjen gocop.toml ne bi se dao pročitati: %w", err)
	}
	for _, k := range Kljucevi() {
		if !IstaVrijednost(poslije.Vrijednost(k), ocekivano.Vrijednost(k)) {
			return nil, Config{}, fmt.Errorf("izmjena bi u gocop.toml promijenila i %s", k.Oznaka())
		}
	}
	return tekst, poslije, nil
}

// IstaVrijednost uspoređuje dvije vrijednosti postavki onako kako bi stajale
// u datoteci, pa je prazan popis isti kao nepostojeći
func IstaVrijednost(a, b any) bool {
	la, errA := Literal(a)
	lb, errB := Literal(b)
	return errA == nil && errB == nil && la == lb
}

// Literal je vrijednost zapisana kao u datoteci: 'tekst', 12, true, ['a']
func Literal(v any) (string, error) {
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Slice && rv.IsNil() {
		v = reflect.MakeSlice(rv.Type(), 0, 0).Interface()
	}
	b, err := toml.Marshal(map[string]any{"v": v})
	if err != nil {
		return "", err
	}
	lit, ok := strings.CutPrefix(strings.TrimSuffix(string(b), "\n"), "v = ")
	if !ok || strings.Contains(lit, "\n") {
		return "", fmt.Errorf("vrijednost %v ne da se zapisati u jednom retku", v)
	}
	return lit, nil
}

// postaviKljuc upisuje jednu vrijednost: zamijeni postojeću ili doda redak
func postaviKljuc(tekst []byte, k Kljuc, v any) ([]byte, error) {
	m, err := procitajMjesta(tekst)
	if err != nil {
		return nil, fmt.Errorf("gocop.toml se ne da pročitati: %w", err)
	}
	lit, err := Literal(v)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", k.Oznaka(), err)
	}
	if r, ok := m.vrijednost[k.Puni()]; ok {
		return zamijeni(tekst, r[0], r[1], lit), nil
	}
	nl := krajRetka(tekst)
	redak := redakKljuca(k, lit, nl)
	if kraj, ok := m.krajOdjeljka[k.Odjeljak]; ok {
		return umetni(tekst, kraj, redak, nl), nil
	}
	if k.Odjeljak == "" {
		return umetni(tekst, m.mjestoNaVrhu(tekst), redak+nl, nl), nil
	}
	odjeljak := komentar(k.OpisOdjeljka, nl) + "[" + k.Odjeljak + "]" + nl + redak
	return umetni(tekst, len(tekst), nl+odjeljak, nl), nil
}

// mjestaUTekstu su položaji ključeva i odjeljaka u tekstu datoteke
type mjestaUTekstu struct {
	vrijednost   map[string][2]int // puni ključ → [početak, kraj) vrijednosti
	krajOdjeljka map[string]int    // odjeljak → kraj njegova zadnjeg retka
	prviOdjeljak int               // početak retka prvog [odjeljka]; -1 kad ga nema
}

// procitajMjesta prolazi izrazima datoteke (go-toml, bez komentara)
func procitajMjesta(tekst []byte) (mjestaUTekstu, error) {
	m := mjestaUTekstu{vrijednost: map[string][2]int{}, krajOdjeljka: map[string]int{}, prviOdjeljak: -1}
	var p unstable.Parser
	p.Reset(tekst)
	odjeljak := ""
	for p.NextExpression() {
		e := p.Expression()
		switch e.Kind {
		case unstable.Table:
			odjeljak = m.zaglavlje(tekst, e, true)
		case unstable.ArrayTable:
			m.zaglavlje(tekst, e, false)
			odjeljak = "[[niz]]" // ključevi niza tablica nisu postavke
		case unstable.KeyValue:
			m.kljuc(tekst, odjeljak, e)
		}
	}
	return m, p.Error()
}

// zaglavlje bilježi redak [odjeljka] i vraća ime odjeljka; niz tablica
// ([[ime]]) samo pomiče početak prvog odjeljka
func (m *mjestaUTekstu) zaglavlje(tekst []byte, e *unstable.Node, odjeljak bool) string {
	dijelovi, pocetak, _ := dijeloviKljuca(e.Key())
	if m.prviOdjeljak < 0 {
		m.prviOdjeljak = pocetakRetka(tekst, pocetak)
	}
	ime := strings.Join(dijelovi, ".")
	if odjeljak {
		m.krajOdjeljka[ime] = krajRetkaOd(tekst, pocetak)
	}
	return ime
}

// kljuc bilježi gdje je vrijednost ključa i do kuda seže njegov odjeljak
func (m *mjestaUTekstu) kljuc(tekst []byte, odjeljak string, e *unstable.Node) {
	dijelovi, _, krajKljuca := dijeloviKljuca(e.Key())
	puni := strings.Join(dijelovi, ".")
	if odjeljak != "" {
		puni = odjeljak + "." + puni
	}
	kraj := int(e.Raw.Offset + e.Raw.Length)
	pocetak := krajKljuca + bytes.IndexByte(tekst[krajKljuca:kraj], '=') + 1
	for pocetak < kraj && (tekst[pocetak] == ' ' || tekst[pocetak] == '\t') {
		pocetak++
	}
	m.vrijednost[puni] = [2]int{pocetak, kraj}
	m.krajOdjeljka[odjeljak] = krajRetkaOd(tekst, kraj)
}

// mjestoNaVrhu je gdje ide novi ključ s vrha datoteke kad na vrhu nema
// nijednog: ispred prvog odjeljka i komentara iznad njega, inače na kraj
func (m *mjestaUTekstu) mjestoNaVrhu(tekst []byte) int {
	if m.prviOdjeljak < 0 {
		return len(tekst)
	}
	mjesto := m.prviOdjeljak
	for mjesto > 0 {
		prethodni := pocetakRetka(tekst, mjesto-1)
		if !bytes.HasPrefix(bytes.TrimSpace(tekst[prethodni:mjesto]), []byte("#")) {
			break
		}
		mjesto = prethodni
	}
	return mjesto
}

// dijeloviKljuca su dijelovi (možda s točkom) ključa, s početkom i krajem
func dijeloviKljuca(it unstable.Iterator) (dijelovi []string, pocetak, kraj int) {
	pocetak = -1
	for it.Next() {
		n := it.Node()
		dijelovi = append(dijelovi, string(n.Data))
		if pocetak < 0 {
			pocetak = int(n.Raw.Offset)
		}
		kraj = int(n.Raw.Offset + n.Raw.Length)
	}
	return dijelovi, pocetak, kraj
}

func pocetakRetka(tekst []byte, o int) int {
	return bytes.LastIndexByte(tekst[:o], '\n') + 1
}

func krajRetkaOd(tekst []byte, o int) int {
	if i := bytes.IndexByte(tekst[o:], '\n'); i >= 0 {
		return o + i + 1
	}
	return len(tekst)
}

// krajRetka je oznaka kraja retka kakvu datoteka već ima (Windows: CRLF)
func krajRetka(tekst []byte) string {
	if bytes.Contains(tekst, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

func zamijeni(tekst []byte, od, do int, novo string) []byte {
	out := make([]byte, 0, len(tekst)+len(novo))
	out = append(out, tekst[:od]...)
	out = append(out, novo...)
	return append(out, tekst[do:]...)
}

// umetni stavlja s na mjesto; redak bez kraja na kraju datoteke prvo završi
func umetni(tekst []byte, mjesto int, s, nl string) []byte {
	if mjesto > 0 && tekst[mjesto-1] != '\n' {
		s = nl + s
	}
	return zamijeni(tekst, mjesto, mjesto, s)
}

// redakKljuca je ključ s objašnjenjem iznad, kao u primjeru postavki
func redakKljuca(k Kljuc, lit, nl string) string {
	return komentar(k.Opis, nl) + imeKljuca(k.Ime) + " = " + lit + nl
}

func komentar(opis, nl string) string {
	if opis == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range strings.Split(opis, "\n") {
		b.WriteString(strings.TrimRight("# "+r, " ") + nl)
	}
	return b.String()
}

// goloIme je ključ koji smije bez navodnika
var goloIme = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// imeKljuca: golo ime kad smije, inače u navodnicima
func imeKljuca(ime string) string {
	if goloIme.MatchString(ime) {
		return ime
	}
	return strconv.Quote(ime)
}
