package web

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"gocop/internal/models"
)

// Mjerodavni vodomjeri nasipa. Nasip rijetko ima svoju letvu, ali svaka
// poddionica na kojoj stoji ima mjerodavne vodomjere iz plana obrane — na
// samom nasipu ili negdje drugdje na vodi. Stranica nasipa ih skuplja iz
// dionica. Pragovi su sa same letve, kao na stranici dionice i na kartici
// letve: po njima se računa stupanj obrane, pa ista letva nigdje ne smije
// pokazati drugi prag. Ništa se ne upisuje: sve se čita iz dionica i
// registra letvi.

// MjerodavniNasipa su mjerodavni vodomjeri svih poddionica na kojima nasip
// stoji: letve iz registra i kriteriji iz zapisa dionice koji nisu letve.
type MjerodavniNasipa struct {
	Objekt    string // „nasip” ili „brana”, za tekst na stranici
	Letve     []MjerodavniVodomjer
	Kriteriji []MjerodavniVodomjer
}

// Ima javlja ima li nasip ijedan mjerodavni vodomjer ili kriterij
func (m *MjerodavniNasipa) Ima() bool {
	return m != nil && (len(m.Letve) > 0 || len(m.Kriteriji) > 0)
}

// Broj je broj kartica i redaka u bloku
func (m *MjerodavniNasipa) Broj() int {
	return len(m.Letve) + len(m.Kriteriji)
}

// MjerodavniVodomjer je jedna kartica (letva) ili jedan redak (kriterij) s
// dionicama za koje vrijedi. Ista letva na više poddionica stoji jednom, sa
// svim oznakama dionica.
type MjerodavniVodomjer struct {
	Letva    *models.Station  // letva iz registra; nil kod kriterija
	Kriterij models.GaugeItem // zapis iz dionice, kad letve u registru nema
	Dionice  []OznakaDionice
}

// OznakaDionice je dionica i, kad ih ima više, poddionica
type OznakaDionice struct {
	Code       string
	Poddionica int // 0 kad dionica ima samo jednu poddionicu
}

// Oznaka je tekst za karticu: „B.34.19” ili „B.34.19, poddionica 1”
func (o OznakaDionice) Oznaka() string {
	if o.Poddionica == 0 {
		return o.Code
	}
	return o.Code + ", poddionica " + strconv.Itoa(o.Poddionica)
}

// imaMjerodavneVodomjere javlja pokazuje li stranica objekta te vrste
// mjerodavne vodomjere iz dionica: nasip i brana stoje među nasipima
// poddionice
func imaMjerodavneVodomjere(kind string) bool {
	return kind == models.StructureKindEmbankment || kind == models.StructureKindDam
}

// mjerodavniVodomjeri skuplja mjerodavne vodomjere poddionica na kojima je
// objekt upisan među nasipima (po vezi na registar, ne po nazivu)
func (h *StructuresHandler) mjerodavniVodomjeri(ctx context.Context, id, kind string, dionice []models.Section) *MjerodavniNasipa {
	s := &skupMjerodavnih{indeks: map[string]int{}}
	for _, sec := range dionice {
		dijelovi := poddioniceNasipa(sec, id)
		if len(dijelovi) == 0 {
			continue
		}
		letve := h.letveDionice(ctx, sec.Code)
		for _, p := range dijelovi {
			o := OznakaDionice{Code: sec.Code}
			if len(sec.Parts) > 1 {
				o.Poddionica = p.Seq
			}
			s.dodajPoddionicu(p, letve, o)
		}
	}
	m := s.slozi()
	m.Objekt = models.StructureKindLabel(kind)
	return m
}

// poddioniceNasipa su poddionice dionice na kojima je objekt id među nasipima
func poddioniceNasipa(sec models.Section, id string) []models.SectionPart {
	var out []models.SectionPart
	for _, p := range sec.Parts {
		if slices.ContainsFunc(p.Embankments, func(e models.PartEmbankment) bool { return e.StructureID == id }) {
			out = append(out, p)
		}
	}
	return out
}

// letveDionice su letve dionice iz registra po ID-u, kao na stranici dionice
func (h *StructuresHandler) letveDionice(ctx context.Context, code string) map[string]models.Station {
	out := map[string]models.Station{}
	if h.stationService == nil {
		return out
	}
	if st, err := h.stationService.GetSectionStations(ctx, code); err == nil {
		for _, s := range st {
			out[s.ID.String()] = s
		}
	}
	return out
}

// skupMjerodavnih slaže kartice redom kojim se pojavljuju, bez ponavljanja
type skupMjerodavnih struct {
	popis  []MjerodavniVodomjer
	indeks map[string]int
}

// dodajPoddionicu dodaje letve i kriterije jedne poddionice. Kriterij je ono
// što stranica dionice pokazuje ispod letvi: uputa ili mjerilo koje nije
// letva iz registra. Zapis koji letva pokriva ne pokazuje se, kao ni na
// stranici dionice.
func (s *skupMjerodavnih) dodajPoddionicu(p models.SectionPart, letve map[string]models.Station, o OznakaDionice) {
	var naPoddionici []models.Station
	for _, id := range p.StationIDs {
		if l, ok := letve[id]; ok {
			naPoddionici = append(naPoddionici, l)
		}
	}
	for i := range naPoddionici {
		l := naPoddionici[i]
		s.dodaj(MjerodavniVodomjer{Letva: &l}, o)
	}
	for _, g := range p.Gauges {
		if !g.IsGauge() || !coveredBy(naPoddionici, g) {
			s.dodaj(MjerodavniVodomjer{Kriterij: g}, o)
		}
	}
}

func (s *skupMjerodavnih) dodaj(v MjerodavniVodomjer, o OznakaDionice) {
	k := v.kljuc()
	i, ok := s.indeks[k]
	if !ok {
		i = len(s.popis)
		s.indeks[k] = i
		s.popis = append(s.popis, v)
	}
	if !slices.Contains(s.popis[i].Dionice, o) {
		s.popis[i].Dionice = append(s.popis[i].Dionice, o)
	}
}

// slozi dijeli skup na letve i kriterije
func (s *skupMjerodavnih) slozi() *MjerodavniNasipa {
	m := &MjerodavniNasipa{}
	for _, v := range s.popis {
		if v.Letva == nil {
			m.Kriteriji = append(m.Kriteriji, v)
			continue
		}
		m.Letve = append(m.Letve, v)
	}
	return m
}

// kljuc razlikuje kartice: letvu po ID-u, kriterij po zapisu
func (v MjerodavniVodomjer) kljuc() string {
	if v.Letva != nil {
		return "letva|" + v.Letva.ID.String()
	}
	g := v.Kriterij
	return strings.Join([]string{"kriterij", strings.ToLower(strings.TrimSpace(g.StationName)),
		g.PrepCm, g.RegularCm, g.EmergCm, g.CriticalCm, g.RecordCm, g.Notes}, "|")
}
