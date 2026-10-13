package his2000

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"gocop/internal/models"
)

// Uvoz krivulja protoka spaja izvoz s datotekom koju letva već ima.
//
// Dosad je uvoz datoteku krivulja zamijenio cijelu. Na Donjem Miholjcu
// (13. 10. 2026.) izvoz postaje 5150 „Donji Miholjac C.S.” ima krivulje
// 1993.–2026., a izvoz spojene postaje 5555 krivulje 1926.–2016. Uvoz
// novijeg izvoza tako bi iz datoteke, a time i iz arhive, odnio krivulje
// 1926.–1992. Zato se krivulje spajaju po razdoblju, a razdoblje je
// određeno početkom (vrijedi_od): izvoz dodaje novo razdoblje ili zamijeni
// isto, a razdoblja kojih u izvozu nema ostaju.

const (
	oblikDana     = "2006-01-02"
	izvorKrivulja = "DHMZ, HIS-2000"
	// najmanjeStupaca je granica ispod koje gradnja arhive redak preskače.
	najmanjeStupaca = 8
)

// stupciOdsjecka su stupci datoteke krivulja iza razdoblja valjanosti.
var stupciOdsjecka = strings.Split(ZaglavljeKrivulja, ";")[2:]

// razdobljeKrivulje je jedna krivulja u datoteci: razdoblje valjanosti i po
// redak za svaki odsječak, sa stupcima od od_cm do napomene.
type razdobljeKrivulje struct {
	od, do  string
	pocetak time.Time
	redci   [][]string
}

// SpojeneKrivulje je datoteka krivulja nakon uvoza, uz ono što pregled
// pokaže prije upisa.
type SpojeneKrivulje struct {
	CSV                []byte
	Krivulja           int // koliko krivulja datoteka ima nakon spajanja
	Zatecenih          int // koliko ih je imala prije
	ZatecenihOdsjecaka int
	// Razlikuju su počeci zatečenih krivulja koje izvoz donosi drukčije, a
	// Ostaju počeci zatečenih krivulja kojih u izvozu nema.
	Razlikuju, Ostaju []string
}

// SpojiKrivulje spaja krivulje izvoza sa zatečenom datotekom krivulja.
// Zadnjoj krivulji kraj ostaje otvoren, kao i u KrivuljeCSV. Prazna
// zatečena datoteka daje isto što i KrivuljeCSV.
func SpojiKrivulje(zateceno []byte, s *Sadrzaj) (SpojeneKrivulje, error) {
	stare, err := procitajZatecene(zateceno)
	if err != nil {
		return SpojeneKrivulje{}, err
	}
	nove := razdobljaIzvoza(s)
	spojeno := zatvoriKrajeve(spoji(stare, nove))
	return SpojeneKrivulje{
		CSV:                zapisiRazdoblja(spojeno),
		Krivulja:           len(spojeno),
		Zatecenih:          len(stare),
		ZatecenihOdsjecaka: brojRedaka(stare),
		Razlikuju:          izmijenjena(stare, nove, spojeno),
		Ostaju:             samoUZatecenima(stare, nove),
	}, nil
}

// SpojiSDatotekom spaja izvoz s datotekom krivulja na putu. Kad datoteke
// nema, letva krivulja dosad nije imala.
func SpojiSDatotekom(put string, s *Sadrzaj) (SpojeneKrivulje, error) {
	zateceno, err := os.ReadFile(put)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return SpojeneKrivulje{}, err
	}
	sp, err := SpojiKrivulje(zateceno, s)
	if err != nil {
		return SpojeneKrivulje{}, fmt.Errorf("%s: %w", put, err)
	}
	return sp, nil
}

// procitajZatecene čita datoteku krivulja kao gradnja arhive: razdoblje iz
// prva dva stupca, ostalo po zaglavlju, a odsječci istog početka čine jednu
// krivulju. Redak koji gradnja ne bi pročitala zaustavlja spajanje, jer bi
// ga spojena datoteka inače tiho izgubila.
func procitajZatecene(b []byte) ([]razdobljeKrivulje, error) {
	sve, err := citajTablicu(b)
	if err != nil {
		return nil, err
	}
	if len(sve) < 2 {
		return nil, nil
	}
	stupci := stupciZaglavlja(sve[0])
	mjesto := map[string]int{}
	var out []razdobljeKrivulje
	for _, r := range sve[1:] {
		pocetak, err := provjeriRedak(r)
		if err != nil {
			return nil, err
		}
		od := strings.TrimSpace(r[0])
		j, ima := mjesto[od]
		if !ima {
			j = len(out)
			mjesto[od] = j
			out = append(out, razdobljeKrivulje{od: od, do: strings.TrimSpace(r[1]), pocetak: pocetak})
		}
		out[j].redci = append(out[j].redci, stupci.odsjecak(r))
	}
	return out, nil
}

func citajTablicu(b []byte) ([][]string, error) {
	cr := csv.NewReader(bytes.NewReader(b))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	sve, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("ne čita se kao tablica: %w", err)
	}
	return sve, nil
}

// provjeriRedak vraća početak razdoblja retka, ili kaže zašto redak nije
// redak krivulje.
func provjeriRedak(r []string) (time.Time, error) {
	redak := strings.Join(r, ";")
	if len(r) < najmanjeStupaca {
		return time.Time{}, fmt.Errorf("redak „%s” ima %d %s, a redak krivulje ih ima barem %d",
			redak, len(r), models.UzBrojHR(len(r), "stupac", "stupca", "stupaca"), najmanjeStupaca)
	}
	pocetak, err := time.Parse(oblikDana, strings.TrimSpace(r[0]))
	if err != nil {
		return time.Time{}, fmt.Errorf("redak „%s” ne počinje datumom oblika 2001-01-01", redak)
	}
	return pocetak, nil
}

// stupci su mjesta stupaca po imenu iz zaglavlja. Stupac p4 došao je
// naknadno, pa ga starija datoteka nema; prazno polje gradnja čita kao i
// stupac kojeg nema.
type stupci map[string]int

func stupciZaglavlja(zaglavlje []string) stupci {
	s := stupci{}
	for i, ime := range zaglavlje {
		s[strings.ToLower(strings.TrimSpace(ime))] = i
	}
	return s
}

func (s stupci) odsjecak(r []string) []string {
	out := make([]string, len(stupciOdsjecka))
	for k, ime := range stupciOdsjecka {
		if i, ima := s[ime]; ima && i < len(r) {
			out[k] = strings.TrimSpace(r[i])
		}
	}
	return out
}

// razdobljaIzvoza su krivulje izvoza. Zadnjoj se kraj ne uzima: DHMZ joj
// upiše kraj razdoblja izvoza, a ne kraj valjanosti (zatvoriKrajeve).
func razdobljaIzvoza(s *Sadrzaj) []razdobljeKrivulje {
	out := make([]razdobljeKrivulje, 0, len(s.Krivulje))
	for _, k := range s.Krivulje {
		r := razdobljeKrivulje{od: k.Od.Format(oblikDana), do: k.Do.Format(oblikDana), pocetak: k.Od}
		for _, o := range k.Odsjecci {
			r.redci = append(r.redci, []string{strconv.Itoa(o.OdCm), strconv.Itoa(o.DoCm), o.Oblik,
				o.P1, o.P2, o.P3, o.P4, izvorKrivulja, ""})
		}
		out = append(out, r)
	}
	if n := len(out); n > 0 {
		out[n-1].do = ""
	}
	return out
}

// spoji slaže zatečena razdoblja i razdoblja izvoza po početku. Kad oboje
// imaju isti početak, vrijedi izvoz. Zadnja krivulja izvoza nema kraj, pa
// zadrži kraj zatečene krivulje istog početka: inače bi stariji izvoz
// uvezen poslije novijeg, ili isti izvoz uvezen ponovno, krivulji vratio
// kraj razdoblja izvoza i iza nje ostavio rupu do sljedeće krivulje.
func spoji(stare, nove []razdobljeKrivulje) []razdobljeKrivulje {
	po := poPocetku(stare)
	for _, r := range nove {
		if r.do == "" {
			r.do = po[r.od].do
		}
		po[r.od] = r
	}
	out := make([]razdobljeKrivulje, 0, len(po))
	for _, r := range po {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pocetak.Before(out[j].pocetak) })
	return out
}

func poPocetku(razdoblja []razdobljeKrivulje) map[string]razdobljeKrivulje {
	po := make(map[string]razdobljeKrivulje, len(razdoblja))
	for _, r := range razdoblja {
		po[r.od] = r
	}
	return po
}

// zatvoriKrajeve ostavlja otvoren kraj samo zadnjoj krivulji: DHMZ joj kraj
// upiše na kraj tekuće godine, a vrijedi dok ne objave novu. Sa zapisanim
// krajem protok bi na Silvestrovo prestao imati krivulju. Krivulja otvorenog
// kraja koja nije zadnja vrijedi do dana prije sljedeće.
func zatvoriKrajeve(r []razdobljeKrivulje) []razdobljeKrivulje {
	for i := range r {
		switch {
		case i == len(r)-1:
			r[i].do = ""
		case r[i].do == "":
			r[i].do = r[i+1].pocetak.AddDate(0, 0, -1).Format(oblikDana)
		}
	}
	return r
}

func zapisiRazdoblja(razdoblja []razdobljeKrivulje) []byte {
	redci := [][]string{strings.Split(ZaglavljeKrivulja, ";")}
	for _, r := range razdoblja {
		for _, o := range r.redci {
			redci = append(redci, append([]string{r.od, r.do}, o...))
		}
	}
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.Comma = ';'
	// Pisanje u bytes.Buffer ne pada, a ';' je dopušten razdjelnik, pa
	// WriteAll nema grešku koju bi vratio.
	_ = w.WriteAll(redci)
	return b.Bytes()
}

func brojRedaka(razdoblja []razdobljeKrivulje) int {
	n := 0
	for _, r := range razdoblja {
		n += len(r.redci)
	}
	return n
}

// izmijenjena su počeci zatečenih krivulja koje izvoz mijenja: nakon
// spajanja krivulja tog početka nije ista kao zatečena.
func izmijenjena(stare, nove, spojeno []razdobljeKrivulje) []string {
	staro, konacno := poPocetku(stare), poPocetku(spojeno)
	var out []string
	for _, n := range nove {
		if z, ima := staro[n.od]; ima && razlikuje(z, konacno[n.od]) {
			out = append(out, n.od)
		}
	}
	sort.Strings(out)
	return out
}

// razlikuje javlja je li krivulja nakon spajanja drukčija od zatečene.
// Otvoren kraj zatečene nije razlika: zadnja krivulja kraj dobije tek kad
// stigne novija.
func razlikuje(zatecena, konacna razdobljeKrivulje) bool {
	if zatecena.do != "" && zatecena.do != konacna.do {
		return true
	}
	return !slices.EqualFunc(zatecena.redci, konacna.redci, slices.Equal[[]string])
}

// samoUZatecenima su počeci zatečenih krivulja kojih u izvozu nema.
func samoUZatecenima(stare, nove []razdobljeKrivulje) []string {
	novo := poPocetku(nove)
	var out []string
	for _, z := range stare {
		if _, ima := novo[z.od]; !ima {
			out = append(out, z.od)
		}
	}
	sort.Strings(out)
	return out
}

// ZapisiKrivulje sprema izvornik izvoza u mapu izvornik uz datoteku
// krivulja, pa zapisuje spojenu datoteku na put. Izvornik se čuva jer se iz
// koeficijenata poslije ne vidi odakle su, a DHMZ-ov ispis je dokument koji
// to kaže. Izvornik ide prvi: kad upis datoteke padne, ponovljeni uvoz nađe
// isti izvornik i ne sprema ga dvaput. Vraća ime spremljenog izvornika.
func ZapisiKrivulje(put string, csv []byte, s *Sadrzaj, izvornik []byte) (string, error) {
	mapa := filepath.Join(filepath.Dir(put), "izvornik")
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		return "", err
	}
	ime, err := imeIzvornika(mapa, s.Ime, s.Postaja.Sifra, izvornik)
	if err != nil {
		return "", fmt.Errorf("izvornik se nije spremio: %w", err)
	}
	if err := os.WriteFile(filepath.Join(mapa, ime), izvornik, 0o644); err != nil {
		return "", fmt.Errorf("izvornik se nije spremio: %w", err)
	}
	if err := os.WriteFile(put, csv, 0o644); err != nil {
		return "", fmt.Errorf("upis nije uspio: %w", err)
	}
	return ime, nil
}

// imeIzvornika bira ime pod kojim se izvornik sprema. Prvo je to ime pod
// kojim je izvoz stigao. HIS svaki izvoz krivulja nazove krivulje.csv, pa
// kad pod tim imenom već stoji drugi izvoz, novi dobiva šifru postaje, a
// zatim i redni broj. Isti izvoz poslan dvaput ostaje jedna datoteka.
func imeIzvornika(mapa, ime, sifra string, sadrzaj []byte) (string, error) {
	kandidati := kandidatiImena(filepath.Base(ime), samoZnamenke(sifra))
	for _, k := range kandidati {
		slobodno, err := mozeNa(filepath.Join(mapa, k), sadrzaj)
		if err != nil {
			return "", err
		}
		if slobodno {
			return k, nil
		}
	}
	n := len(kandidati)
	return "", fmt.Errorf("u mapi %s već je %d %s pod imenima od %s do %s", mapa, n,
		models.UzBrojHR(n, "drugi izvoz", "druga izvoza", "drugih izvoza"), kandidati[0], kandidati[n-1])
}

func kandidatiImena(ime, sifra string) []string {
	nastavak := filepath.Ext(ime)
	osnova := strings.TrimSuffix(ime, nastavak)
	out := []string{ime}
	if sifra != "" {
		osnova += "-" + sifra
		out = append(out, osnova+nastavak)
	}
	for i := 2; i < 100; i++ {
		out = append(out, fmt.Sprintf("%s-%d%s", osnova, i, nastavak))
	}
	return out
}

// samoZnamenke čisti šifru postaje prije nego uđe u ime datoteke: dolazi iz
// poslane datoteke, pa ne smije donijeti kosu crtu ni točke.
func samoZnamenke(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}

// mozeNa javlja smije li se izvornik spremiti na put: ondje nema ničega ili
// već stoji isti izvoz.
func mozeNa(put string, sadrzaj []byte) (bool, error) {
	zateceno, err := os.ReadFile(put)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return bytes.Equal(zateceno, sadrzaj), nil
}
