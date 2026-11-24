package web

import (
	"log"
	"math"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/snijeg"
)

// Kartica Snijeg u Alpama na Naslovnoj (crtež „snijeg”, Odluke 24. 11.
// 2026.): snijeg na tlu u četiri alpska međusliva iz SNOWGRID-a, s trakom
// prema pragovima za 3, 5 i 10 zima. Kartica postoji dok je barem jedan
// sliv na razini „jednom u 3 zime”, a nestane kad svi padnu ispod pola
// tog praga (snijeg.Ukljucen). Vide je isti kao i karticu Jača kiša.

const kSnijeg = "snijeg-u-alpama"

// klaseSnijega su boje razina, iste kao kod kiše; ispod praga redak je siv
var klaseSnijega = [4]string{"", "kisa-zuto", "kisa-narancasto", "kisa-crveno"}

// SnijegNaslovne su podaci tijela kartice Snijeg u Alpama.
type SnijegNaslovne struct {
	// Alarm je kuka za upozorenje „Kiša i topljenje na puno snijega!” na
	// vrhu kartice. Pravilo se još dogovara, pa je zasad uvijek nil.
	Alarm   *AlarmSnijega
	Skupine []SkupinaSnijega
	Kasni   string // napomena kad SNOWGRID kasni više od tri dana
	Vise    string // adresa „Više na Prognoze › Vrijeme”; prazno kad je račun ne vidi
}

// AlarmSnijega je crveni okvir na vrhu kartice.
type AlarmSnijega struct {
	Naslov, Tekst string
}

// SkupinaSnijega su retci jedne rijeke (Drava, Dunav).
type SkupinaSnijega struct {
	Rijeka string
	Retci  []RedakSnijega
}

// RedakSnijega je jedan alpski međusliv na kartici.
type RedakSnijega struct {
	Naziv, Podrucje, URL string
	Klasa, Rijetko       string // boja i natpis razine; prazno ispod praga
	Mm, Trend            string
	Popuna               float64 // traka, % širine
	Crtice               []CrticaTrake
	Opis                 string // traka riječima, za čitač zaslona
	// Alarm je crveni redak ispod sliva („idućih 5 dana: 95 mm vode ·
	// jednom u 5 godina”); kuka kao SnijegNaslovne.Alarm, zasad prazna
	Alarm string
}

// rijetkoSnijega: „jednom u 3 zime”, „jednom u 5 zima”, „jednom u 10 zima”
func rijetkoSnijega(i int) string {
	n := snijeg.Zime[i]
	return "jednom u " + brojHRf(float64(n), 0) + " " + models.UzBrojHR(n, "zimu", "zime", "zima")
}

// snijegUkljucen kaže drži li ijedan sliv karticu
func snijegUkljucen(slivovi []snijeg.StanjeSliva) bool {
	for _, s := range slivovi {
		if s.Ukljucen {
			return true
		}
	}
	return false
}

// karticaSnijega je kartica Snijeg u Alpama kad je ijedan sliv drži; bez
// izvora ili kad se snijeg ne da pročitati kartice nema.
func (h *DashboardHandler) karticaSnijega(vis models.Visibility) (KarticaNaslovne, bool) {
	if h.izvori.Snijeg == nil {
		return KarticaNaslovne{}, false
	}
	slivovi, err := h.izvori.Snijeg()
	if err != nil {
		log.Printf("snijeg na tlu: %v", err)
		return KarticaNaslovne{}, false
	}
	if !snijegUkljucen(slivovi) {
		return KarticaNaslovne{}, false
	}
	vise := "/prognoze/vrijeme"
	if m := moduleForPath(vise); m != "" && !vis.Sees(m) {
		vise = ""
	}
	return karticaSnijega(slivovi, time.Now(), vise), true
}

// karticaSnijega slaže karticu iz stanja slivova
func karticaSnijega(slivovi []snijeg.StanjeSliva, sad time.Time, vise string) KarticaNaslovne {
	p := SnijegNaslovne{Vise: vise, Kasni: kasniSnijeg(slivovi, sad)}
	for _, s := range slivovi {
		if n := len(p.Skupine); n == 0 || p.Skupine[n-1].Rijeka != s.Rijeka {
			p.Skupine = append(p.Skupine, SkupinaSnijega{Rijeka: s.Rijeka})
		}
		sk := &p.Skupine[len(p.Skupine)-1]
		sk.Retci = append(sk.Retci, redakSnijega(s))
	}
	return KarticaNaslovne{ID: kSnijeg, Naslov: "Snijeg u Alpama", Tijelo: "snijeg", Podaci: p}
}

// kasniSnijeg: „Snijeg na tlu kasni: zadnji podatak je od 19. 11.” kad je
// zadnji dan SNOWGRID-a stariji od tri dana; inače prazno
func kasniSnijeg(slivovi []snijeg.StanjeSliva, sad time.Time) string {
	for _, s := range slivovi {
		if s.Kasni(sad) {
			return "Podaci o snijegu kasne: zadnji je od " + snijeg.Dan(s.Dan).Format("2. 1.") + ", pa se prikazuje on."
		}
	}
	return ""
}

// mmVode: „170 mm vode”
func mmVode(v float64) string { return brojHRf(math.Round(v), 0) + " mm vode" }

// redakSnijega slaže redak međusliva
func redakSnijega(s snijeg.StanjeSliva) RedakSnijega {
	r := RedakSnijega{Naziv: s.Naziv, Podrucje: s.Podrucje, URL: "/slivovi#sliv-" + s.Oznaka, Mm: "—", Trend: s.Trend}
	if !s.Ima {
		r.Opis = "nema podataka o snijegu"
		return r
	}
	r.Mm = mmVode(s.Mm)
	if s.Razina > 0 {
		r.Klasa, r.Rijetko = klaseSnijega[s.Razina], rijetkoSnijega(s.Razina-1)
	}
	if s.Alarm != nil {
		r.Alarm = "idućih 5 dana: " + mmVode(s.Alarm.Mm) + " · " + s.Alarm.Rijetko
	}
	r.Popuna, r.Crtice = trakaSnijega(s)
	r.Opis = strings.Join(append([]string{r.Mm}, pragoviRijecima(s)...), "; ")
	return r
}

// trakaSnijega: popuna trake i crtice pragova; mjerilo stavlja prag za 10
// zima na 80 % širine, a bez pragova je snijeg na 80 %
func trakaSnijega(s snijeg.StanjeSliva) (float64, []CrticaTrake) {
	mjerilo := s.Mm / 0.8
	if s.ImaPragove && s.Pragovi[2] > 0 {
		mjerilo = s.Pragovi[2] / 0.8
	}
	if mjerilo <= 0 {
		return 0, nil
	}
	posto := func(v float64) float64 { return math.Round(math.Min(v/mjerilo, 1)*1000) / 10 }
	var crtice []CrticaTrake
	if s.ImaPragove {
		for i, p := range s.Pragovi {
			crtice = append(crtice, CrticaTrake{Lijevo: posto(p), Klasa: klaseSnijega[i+1],
				Naslov: rijetkoSnijega(i) + ": " + mmVode(p)})
		}
	}
	return posto(s.Mm), crtice
}

// pragoviRijecima: „jednom u 3 zime 130 mm vode” za svaki prag
func pragoviRijecima(s snijeg.StanjeSliva) []string {
	if !s.ImaPragove {
		return nil
	}
	out := make([]string, 0, len(s.Pragovi))
	for i, p := range s.Pragovi {
		out = append(out, rijetkoSnijega(i)+" "+mmVode(p))
	}
	return out
}
