// Paket prognoza čita tuđe objavljene prognoze vodostaja, da se naše mogu
// s njima usporediti. Vlastitu prognozu ovaj paket ne računa.
//
// Mađarska vodoprivredna služba (Országos Vízjelző Szolgálat) objavljuje
// tablicu na hydroinfo.hu: za svaku letvu jutrošnja vrijednost i šest dana
// unaprijed, svaki uz raspon pogreške koji sami navode. Tablica pokriva i
// naše letve — Aljmaš na Dunavu te Botovo, Terezino Polje, Donji Miholjac,
// Belišće i Osijek na Dravi.
//
// Stranica je stara, u kodnoj stranici ISO-8859-2, a tablica je HTML, ne
// slika, pa se čita bez pogađanja.
package prognoza

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
)

// Podrijetlo je ono što stoji uz prognozu kao izvor
const Podrijetlo = "hydroinfo.hu"

// Adrese tablica: Dunav sa svojim pritocima, te Drava i Mura
const (
	AdresaDunav = "https://www.hydroinfo.hu/tables/ENG/dunelotH.html"
	AdresaDrava = "https://www.hydroinfo.hu/tables/ENG/draelotH.html"
)

// Dan je jedna prognozirana vrijednost.
type Dan struct {
	Kad     time.Time // trenutak na koji se prognoza odnosi, u UTC
	Cm      int       // prognozirani vodostaj
	PlusMin int       // raspon pogreške koji služba navodi, u cm; 0 kad ga nema
}

// Letva je prognoza za jednu postaju.
type Letva struct {
	Rijeka   string
	Naziv    string    // naziv kakav stoji u njihovoj tablici
	Izdano   time.Time // kad je prognoza izdana, po njihovu vremenu
	Danas    int       // jutrošnja vrijednost, polazište prognoze
	ImaDanas bool
	Dani     []Dan
}

var (
	reOznake    = regexp.MustCompile(`<[^>]+>`)
	reZaglavlje = regexp.MustCompile(`(\d{2})\.(\d{2})\.\s*07h`)
	// zaglavlje letve: rijeka, naziv i vrijeme izdanja, sve u istoj ćeliji
	reLetva = regexp.MustCompile(`(Danube|Drava|Mura|Rába|Raba|Ipoly|Zala|Lajta|Sió|Sio)\s+(.+?)\s+\((\d{2})\.(\d{2})\.(\d{4})\s+(\d{1,2}):(\d{2})\)`)
	// iza zaglavlja slijede brojevi i rasponi pogreške, svaki u svojoj ćeliji
	reVrijednost = regexp.MustCompile(`±\s*(\d+)|(-?\d+)`)
)

// Citaj razlaže tablicu prognoze. Svaki dan je u tablici zaseban okvir s
// dvije vrijednosti, prognozom i rasponom pogreške, pa se ne čitaju ćelije
// nego redoslijed brojeva iza zaglavlja letve.
func Citaj(stranica string) ([]Letva, error) {
	tekst := popraviZnakove(html.UnescapeString(izISO88592(stranica)))
	tekst = reOznake.ReplaceAllString(tekst, " ")
	tekst = strings.Join(strings.Fields(tekst), " ")

	datumi := danoviZaglavlja(tekst)
	zaglavlja := reLetva.FindAllStringSubmatchIndex(tekst, -1)

	var out []Letva
	for i, z := range zaglavlja {
		uzmi := func(a, b int) string { return tekst[z[a]:z[b]] }
		l := Letva{Rijeka: uzmi(2, 3), Naziv: strings.TrimSpace(uzmi(4, 5))}
		l.Izdano = time.Date(broj(uzmi(10, 11)), time.Month(broj(uzmi(8, 9))), broj(uzmi(6, 7)),
			broj(uzmi(12, 13)), broj(uzmi(14, 15)), 0, 0, models.Zagreb)

		kraj := len(tekst)
		if i+1 < len(zaglavlja) {
			kraj = zaglavlja[i+1][0]
		}
		var brojevi []int
		var rasponi []int // -1 znači da uz taj broj nije stajao raspon
		for _, m := range reVrijednost.FindAllStringSubmatch(tekst[z[1]:kraj], -1) {
			if m[1] != "" {
				if len(rasponi) > 0 {
					rasponi[len(rasponi)-1] = broj(m[1])
				}
				continue
			}
			brojevi = append(brojevi, broj(m[2]))
			rasponi = append(rasponi, -1)
		}
		if len(brojevi) == 0 {
			continue
		}
		l.Danas, l.ImaDanas = brojevi[0], true
		for j := 1; j < len(brojevi) && j-1 < len(datumi); j++ {
			d := Dan{Kad: datumi[j-1].UTC(), Cm: brojevi[j]}
			if rasponi[j] >= 0 {
				d.PlusMin = rasponi[j]
			}
			l.Dani = append(l.Dani, d)
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("u tablici nema nijedne letve — je li se stranica promijenila?")
	}
	return out, nil
}

// danoviZaglavlja čita dane iz zaglavlja tablice („22.09. 07h"). Godina u
// zaglavlju ne piše, pa se uzima iz današnjeg datuma, uz prijelaz godine.
func danoviZaglavlja(tekst string) []time.Time {
	sada := time.Now().In(models.Zagreb)
	var out []time.Time
	for _, m := range reZaglavlje.FindAllStringSubmatch(tekst, -1) {
		godina := sada.Year()
		if time.Month(broj(m[2])) == time.January && sada.Month() == time.December {
			godina++
		}
		out = append(out, time.Date(godina, time.Month(broj(m[2])), broj(m[1]), 7, 0, 0, 0, models.Zagreb))
	}
	return out
}

// Dohvati čita obje tablice, dunavsku i dravsku.
func Dohvati(ctx context.Context, klijent *http.Client) ([]Letva, error) {
	if klijent == nil {
		klijent = &http.Client{Timeout: 30 * time.Second}
	}
	var sve []Letva
	for _, adresa := range []string{AdresaDunav, AdresaDrava} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, adresa, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "goCOP (usporedba prognoza vodostaja)")
		resp, err := klijent.Do(req)
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s: %s", adresa, resp.Status)
		}
		letve, err := Citaj(string(b))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", adresa, err)
		}
		sve = append(sve, letve...)
	}
	return sve, nil
}

// NaseLetve preslikava njihove nazive u naše šifre postaja. Njihova tablica
// pokriva i hrvatske letve, jer ih po međudržavnom dogovoru i prognoziraju.
var NaseLetve = map[string]string{
	"Aljmaš":         "aljmas",
	"Botovo":         "botovo",
	"Terezino Polje": "terezino-polje",
	"Donji Miholjac": "donji-miholjac",
	"Belišće":        "belisce",
	"Osijek":         "osijek",
}

// Sifra vraća našu šifru postaje za njihov naziv; prazno kad letva nije naša
func Sifra(naziv string) string {
	return NaseLetve[strings.TrimSpace(naziv)]
}

func broj(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }

// izISO88592 pretvara zapis iz srednjoeuropske kodne stranice, u kojoj je
// stranica pisana.
func izISO88592(b string) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c < 0x80 {
			sb.WriteByte(c)
			continue
		}
		if r, ok := iso88592[c]; ok {
			sb.WriteRune(r)
			continue
		}
		sb.WriteRune('?')
	}
	return sb.String()
}

// popraviZnakove ispravlja naša slova. Stranica ih piše kao HTML entitete,
// ali po kodnoj stranici 1250, a ne po Unicodeu: &aelig; ondje nije „æ" nego
// „ć", a &#154; je „š". Zato se sve iznad ASCII-ja provuče kroz tablicu 1250.
func popraviZnakove(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		if r >= 0x80 && r <= 0xFF {
			if z, ok := cp1250[byte(r)]; ok {
				sb.WriteRune(z)
				continue
			}
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// cp1250 su znakovi kodne stranice 1250 iznad ASCII-ja, koliko ih tablica
// prognoze koristi
var cp1250 = map[byte]rune{
	0x8A: 'Š', 0x8C: 'Ś', 0x8D: 'Ť', 0x8E: 'Ž', 0x9A: 'š', 0x9C: 'ś', 0x9E: 'ž',
	0xC1: 'Á', 0xC4: 'Ä', 0xC6: 'Ć', 0xC8: 'Č', 0xC9: 'É', 0xCB: 'Ë', 0xCD: 'Í',
	0xD0: 'Đ', 0xD3: 'Ó', 0xD5: 'Ő', 0xD6: 'Ö', 0xDA: 'Ú', 0xDB: 'Ű', 0xDC: 'Ü',
	0xDD: 'Ý', 0xE1: 'á', 0xE4: 'ä', 0xE6: 'ć', 0xE8: 'č', 0xE9: 'é', 0xEB: 'ë',
	0xED: 'í', 0xF0: 'đ', 0xF3: 'ó', 0xF5: 'ő', 0xF6: 'ö', 0xFA: 'ú', 0xFB: 'ű',
	0xFC: 'ü', 0xFD: 'ý',
}

// iso88592 su znakovi iznad ASCII-ja koje tablica koristi
var iso88592 = map[byte]rune{
	0xA1: 'Ą', 0xA3: 'Ł', 0xA5: 'Ľ', 0xA6: 'Ś', 0xA9: 'Š', 0xAA: 'Ş', 0xAB: 'Ť', 0xAC: 'Ź',
	0xAE: 'Ž', 0xAF: 'Ż', 0xB1: 'ą', 0xB3: 'ł', 0xB5: 'ľ', 0xB6: 'ś', 0xB9: 'š', 0xBA: 'ş',
	0xBB: 'ť', 0xBC: 'ź', 0xBE: 'ž', 0xBF: 'ż', 0xC1: 'Á', 0xC4: 'Ä', 0xC6: 'Ć', 0xC8: 'Č',
	0xC9: 'É', 0xCB: 'Ë', 0xCD: 'Í', 0xD0: 'Đ', 0xD3: 'Ó', 0xD4: 'Ô', 0xD6: 'Ö', 0xDA: 'Ú',
	0xDC: 'Ü', 0xDD: 'Ý', 0xE1: 'á', 0xE4: 'ä', 0xE6: 'ć', 0xE8: 'č', 0xE9: 'é', 0xEB: 'ë',
	0xED: 'í', 0xF0: 'đ', 0xF3: 'ó', 0xF4: 'ô', 0xF6: 'ö', 0xFA: 'ú', 0xFC: 'ü', 0xFD: 'ý',
	0xB0: '°', 0xD5: 'Ő', 0xF5: 'ő', 0xDB: 'Ű', 0xFB: 'ű',
}
