package web

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
)

// Uvoz zalijepljenih očitanja s letva.voda.hr.
//
// Ispis s letve izgleda ovako:
//
//	Vodostaj (cm) - satni podaci za razdoblje 07.09.2026. - 07.09.2026.
//	Dunav - Batina (DHMZ)
//	07.09.2026. 00 h    -118
//	07.09.2026. 01 h    -118
//
// Zaglavlja se preskaču, a redak s datumom i satom prepoznaje se po obliku.
// Prima se i oblik bez sata, za dnevne vrijednosti.
//
// Vrijeme s letve je LOKALNO, sa zimskim i ljetnim pomakom, i tako se i
// pretvara u UTC — jednako kao što obrazac za ručni upis pretvara ono što
// operater utipka. Bez toga bi isto očitanje s letve i iz obrasca završilo na
// dva različita sata i izgledalo kao dvije različite vrijednosti.

var (
	// „07.09.2026. 00 h  -118“ s letvina zaslona i „08.09.2026. 00:00  -123“ iz
	// njezina Excel ispisa — isti podatak, dva zapisa istog sata
	reSatni  = regexp.MustCompile(`^\s*(\d{1,2})\.\s*(\d{1,2})\.\s*(\d{4})\.?\s+(\d{1,2})(?::(\d{2}))?\s*h?\s+(-?[\d.,]+)\s*$`)
	reDnevni = regexp.MustCompile(`^\s*(\d{1,2})\.\s*(\d{1,2})\.\s*(\d{4})\.?\s+(-?[\d.,]+)\s*$`)
	reISO    = regexp.MustCompile(`^\s*(\d{4})-(\d{2})-(\d{2})[ T]?(\d{2})?(?::(\d{2}))?:?\d*\s*[;\t ]\s*(-?[\d.,]+)\s*$`)

	// Redak koji počinje datumom, ali se nije dao pročitati do kraja. Bez ovoga
	// bi „07.09.2026. 00 h  x118“ prošao kao zaglavlje i tiho nestao — a upravo
	// je tiho preskakanje najlakši način da se izgubi pola dana.
	reMozdaRedak = regexp.MustCompile(`^\s*(?:\d{1,2}\.\s*\d{1,2}\.\s*\d{4}|\d{4}-\d{2}-\d{2})`)
)

// ZalijepljenoOcitanje je jedan pročitani redak.
type ZalijepljenoOcitanje struct {
	Kad     time.Time
	Vrijedi float64
	Redak   int
	Greska  string
	// Ispis je vrijeme iz ispisa kad ima minute („07:25”); upisuje se puni sat,
	// pa pregled to kaže u retku (Odluke (15. 11. 2026.), 3).
	Ispis string
	// IzvanRaspona je redak koji je pročitan, ali mu je vrijeme ili vodostaj
	// izvan raspona: pregled pokazuje i vrijeme i vrijednost, a ne upisuje ga.
	IzvanRaspona bool

	// Usporedba s onim što već imamo
	Postoji  bool
	Staro    float64
	Razlicit bool
}

// citajZalijepljeno čita zalijepljeni ispis. Vraća i redak po redak, da se
// vidi što je pročitano, a što nije — tiho preskakanje redaka najlakše je
// mjesto da se izgubi pola dana.
func citajZalijepljeno(tekst string) ([]ZalijepljenoOcitanje, bool, error) {
	var out []ZalijepljenoOcitanje
	satni := false
	for i, r := range strings.Split(strings.ReplaceAll(tekst, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(r) == "" {
			continue
		}
		z, poSatu, ok := citajRedak(r, i+1)
		if !ok {
			continue // zaglavlje
		}
		satni = satni || poSatu
		out = append(out, z)
	}
	if len(out) == 0 {
		return nil, false, fmt.Errorf("nijedan redak nije prepoznat — očekuje se oblik „07.09.2026. 00 h  -118“")
	}
	return out, satni, nil
}

// dijeloviRetka su datum, sat, minute i vrijednost retka u jednom od oblika
// ispisa; ok je false kad redak nije ni u jednom.
func dijeloviRetka(r string) (d, m, g, h, min, v string, ok bool) {
	switch {
	case reSatni.MatchString(r):
		p := reSatni.FindStringSubmatch(r)
		return p[1], p[2], p[3], p[4], p[5], p[6], true
	case reISO.MatchString(r):
		p := reISO.FindStringSubmatch(r)
		return p[3], p[2], p[1], p[4], p[5], p[6], true
	case reDnevni.MatchString(r):
		p := reDnevni.FindStringSubmatch(r)
		return p[1], p[2], p[3], "", "", p[4], true
	}
	return
}

// vrijemeRetka je lokalno vrijeme retka u UTC-u, na puni sat. Kad je ispis
// imao minute, vraća ih kao „07:25”, da pregled kaže što se zaokružilo. Dan
// koji ne postoji (31. 9.) i sat iznad 23 nisu vrijeme.
func vrijemeRetka(d, m, g, h, min string) (time.Time, string, bool) {
	dd, mm, gg, sat := znamenke(d), znamenke(m), znamenke(g), znamenke(h)
	if p := time.Date(gg, time.Month(mm), dd, sat, 0, 0, 0, time.UTC); p.Day() != dd || int(p.Month()) != mm || p.Hour() != sat {
		return time.Time{}, "", false
	}
	ispis := ""
	if min != "" && min != "00" {
		ispis = fmt.Sprintf("%02d:%s", sat, min)
	}
	return time.Date(gg, time.Month(mm), dd, sat, 0, 0, 0, models.Zagreb).UTC(), ispis, true
}

// znamenke su broj iz znamenki; prazno je 0.
func znamenke(s string) int {
	n, _ := strconv.Atoi(s) // regex propušta samo znamenke, a prazno je 0
	return n
}

// citajRedak čita jedan redak ispisa. Vraća i je li redak satni te je li
// redak uopće očitanje; redak koji počinje datumom, a ne da se pročitati,
// vraća se s greškom, a ne preskače.
func citajRedak(r string, redak int) (ZalijepljenoOcitanje, bool, bool) {
	z := ZalijepljenoOcitanje{Redak: redak}
	d, m, g, h, min, v, ok := dijeloviRetka(r)
	if !ok {
		if !reMozdaRedak.MatchString(r) {
			return z, false, false
		}
		z.Greska = "redak počinje datumom, ali se ne da pročitati: " + strings.TrimSpace(r)
		return z, false, true
	}
	kad, ispis, ok := vrijemeRetka(d, m, g, h, min)
	if !ok {
		z.Greska = "datum ili sat izvan raspona"
		return z, h != "", true
	}
	z.Kad, z.Ispis = kad, ispis
	x, ok := parseBroj(v)
	if !ok {
		z.Greska = "vrijednost nije broj: " + v
		return z, h != "", true
	}
	z.Vrijedi = x
	return z, h != "", true
}

// usporedi označava koji su redci novi, koji isti, a koji se razlikuju od
// onoga što već imamo.
func usporedi(redci []ZalijepljenoOcitanje, postojece map[int64]models.SpojenaVrijednost) (novih, istih, razlicitih int) {
	for i := range redci {
		if redci[i].Greska != "" {
			continue
		}
		s, ima := postojece[redci[i].Kad.Unix()]
		if !ima {
			novih++
			continue
		}
		redci[i].Postoji, redci[i].Staro = true, s.Vrijednost
		if s.Vrijednost != redci[i].Vrijedi {
			redci[i].Razlicit = true
			razlicitih++
			continue
		}
		istih++
	}
	return
}

// PregledUvoza je ono što se pokaže prije nego što se išta upiše.
// Pokazuje se u kartici letve „Uvoz i izvoz”, na mjestu obrazaca.
type PregledUvoza struct {
	Station    *models.Station
	GaugeName  string
	Satni      bool
	Redci      []ZalijepljenoOcitanje
	Novih      int
	Istih      int
	Razlicitih int
	Greske     int
	Od, Do     time.Time
}

// Zaokruzenih broji retke čije je vrijeme u ispisu imalo minute.
func (p PregledUvoza) Zaokruzenih() int {
	n := 0
	for _, r := range p.Redci {
		if r.Ispis != "" && r.Greska == "" {
			n++
		}
	}
	return n
}
