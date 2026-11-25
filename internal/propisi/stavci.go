package propisi

import (
	"regexp"
	"strings"

	"gocop/internal/models"
)

// uzorakStavka je oznaka stavka na početku odlomka: „(1) ”, „(2a) ”.
var uzorakStavka = regexp.MustCompile(`^(\(\d+[a-z]?\))\s+`)

// oznakaTocke je oblik oznake točke nabrajanja i njezina razina: podtočka
// „2.1.” ide pod točku „2.”, slovo „a)” pod točku, a podstavak „–” pod
// slovo ili točku koja završava dvotočkom.
type oznakaTocke struct {
	uzorak *regexp.Regexp
	vrsta  string
	razina int
}

// oznakeTocaka su oblici oznaka točaka; podbroj je ispred broja, jer i
// „2.1.” počinje s „2.”. Za podstavak NN piše samo crticu U+2013.
var oznakeTocaka = []oznakaTocke{
	{regexp.MustCompile(`^(\d+\.\d+\.)\s+`), models.TockaPodbroj, 2},
	{regexp.MustCompile(`^(\d+\.[a-z]?)\s+`), models.TockaBroj, 1},
	{regexp.MustCompile(`^(\d+\))\s+`), models.TockaZagrada, 2},
	{regexp.MustCompile(`^([a-z]\))\s+`), models.TockaSlovo, 3},
	{regexp.MustCompile(`^(–)\s+`), models.TockaPodstavak, 4},
}

// otvorenaTocka je točka pod koju može doći sljedeća točka niže razine.
type otvorenaTocka struct {
	tocka  *models.TockaPropisa
	razina int
}

// slaganjeStavaka slaže stavke jednog dijela odlomak po odlomak.
type slaganjeStavaka struct {
	stavci []models.StavakPropisa
	stog   []otvorenaTocka
}

// slozStavke slaže odlomke dijela u stavke: „(1) …” otvara stavak, točke
// i podstavci idu u nabrajanje tekućeg stavka, a odlomak bez oznake iza
// nabrajanja u označenom stavku rečenica je iza nabrajanja (Nastavak).
// Odlomak bez oznake inače je novi stavak bez oznake. Tablica i redak
// bloka u navodnicima su zasebni stavci.
func slozStavke(o []Odlomak) []models.StavakPropisa {
	s := &slaganjeStavaka{}
	for _, od := range o {
		s.dodaj(od)
	}
	return s.stavci
}

func (s *slaganjeStavaka) dodaj(o Odlomak) {
	switch {
	case o.Tablica != nil:
		s.novi(models.StavakPropisa{Tablica: o.Tablica})
	case o.navod:
		s.novi(models.StavakPropisa{Tekst: o.Tekst, Navod: true})
	case uzorakStavka.MatchString(o.Tekst):
		g := uzorakStavka.FindStringSubmatch(o.Tekst)
		s.novi(models.StavakPropisa{Oznaka: g[1], Tekst: o.Tekst[len(g[0]):]})
	default:
		if !s.tocka(o.Tekst) {
			s.bezOznake(o.Tekst)
		}
	}
}

func (s *slaganjeStavaka) novi(st models.StavakPropisa) {
	s.stavci = append(s.stavci, st)
	s.stog = nil
}

// bezOznake dodaje odlomak bez oznake: iza nabrajanja u označenom stavku
// to je nastavak stavka, a inače novi stavak.
func (s *slaganjeStavaka) bezOznake(t string) {
	if n := len(s.stavci); n > 0 && s.stavci[n-1].Oznaka != "" && len(s.stavci[n-1].Tocke) > 0 {
		st := &s.stavci[n-1]
		st.Nastavak = strings.TrimPrefix(st.Nastavak+"\n"+t, "\n")
		s.stog = nil
		return
	}
	s.novi(models.StavakPropisa{Tekst: t})
}

// tocka dodaje odlomak koji počinje oznakom točke, pod otvorenu točku više
// razine ili u nabrajanje stavka; vraća false kad odlomak nije točka.
func (s *slaganjeStavaka) tocka(t string) bool {
	for _, ot := range oznakeTocaka {
		g := ot.uzorak.FindStringSubmatch(t)
		if g == nil {
			continue
		}
		nova := models.TockaPropisa{Vrsta: ot.vrsta, Oznaka: g[1], Tekst: t[len(g[0]):]}
		if ot.vrsta == models.TockaPodstavak {
			nova.Oznaka = ""
		}
		s.umetni(nova, ot.razina)
		return true
	}
	return false
}

// umetni stavlja točku pod najbližu otvorenu točku pod koju smije: nižu
// razinu koja završava dvotočkom, ili točku „2.” za podtočku „2.1.”.
func (s *slaganjeStavaka) umetni(nova models.TockaPropisa, razina int) {
	for len(s.stog) > 0 && !smijePod(s.stog[len(s.stog)-1], nova, razina) {
		s.stog = s.stog[:len(s.stog)-1]
	}
	if len(s.stavci) == 0 {
		s.stavci = append(s.stavci, models.StavakPropisa{})
	}
	popis := &s.stavci[len(s.stavci)-1].Tocke
	if n := len(s.stog); n > 0 {
		popis = &s.stog[n-1].tocka.Tocke
	}
	*popis = append(*popis, nova)
	s.stog = append(s.stog, otvorenaTocka{tocka: &(*popis)[len(*popis)-1], razina: razina})
}

func smijePod(roditelj otvorenaTocka, nova models.TockaPropisa, razina int) bool {
	if roditelj.razina >= razina {
		return false
	}
	return strings.HasSuffix(roditelj.tocka.Tekst, ":") ||
		(nova.Vrsta == models.TockaPodbroj && strings.HasPrefix(nova.Oznaka, roditelj.tocka.Oznaka))
}

var (
	uvodBloka = regexp.MustCompile(`glas[ie]:$`)
	krajBloka = regexp.MustCompile(`«[.;,]?$`)
)

// blokUNavodnicima vraća kraj bloka u navodnicima koji počinje odlomkom i:
// novi tekst iz zakona o izmjenama („… koji glasi:” pa „»…«.”). Blok traje
// dok se svi navodnici ne zatvore i odlomak ne završi s „«.”, a odlomak
// koji odmah iza opet počinje s » nastavlja ga. Bez bloka vraća i. Blok koji
// se nikad ne zatvori je samo prvi odlomak.
func blokUNavodnicima(o []Odlomak, i int) int {
	if !pocinjeBlok(o, i) {
		return i
	}
	dubina := 0
	for j := i; j < len(o); j++ {
		dubina += strings.Count(o[j].Tekst, "»") - strings.Count(o[j].Tekst, "«")
		if dubina > 0 || !krajBloka.MatchString(o[j].Tekst) {
			continue
		}
		if j+1 < len(o) && strings.HasPrefix(o[j+1].Tekst, "»") {
			dubina = 0
			continue
		}
		return j + 1
	}
	return i + 1
}

// pocinjeBlok javlja počinje li blok u navodnicima odlomkom i: prethodni
// odlomak završava s „glasi:” ili „glase:”, a ovaj počinje s ».
func pocinjeBlok(o []Odlomak, i int) bool {
	return i > 0 && i < len(o) && uvodBloka.MatchString(o[i-1].Tekst) && strings.HasPrefix(o[i].Tekst, "»")
}
