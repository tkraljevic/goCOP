package web

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Uređivanje na Karti (Odluke (4. 11. 2026.)): objekt, vodotok i letva
// crtaju se, pomiču i ispravljaju na samoj Karti, istim alatima kao na
// stranici objekta i vode. Spremanje ide postojećim putem
// (/api/structures/geometry, /api/watercourses/geometry), uz istu provjeru i
// isti zapis u knjizi; letva dobiva samo novi položaj. Karta crtu za
// uređivanje učita cijelu, jer za prikaz drži pojednostavljeni tok i nasip
// bez točaka km.

// KartaUredjivanje su izvori i prava za uređivanje na Karti. Bez njih Karta
// samo prikazuje.
type KartaUredjivanje struct {
	SmijeObjekt  func(*models.UserPermissions, *models.Structure) bool
	SmijeLetvu   func(*models.UserPermissions, *models.Station) bool
	Objekt       func(context.Context, uuid.UUID) (*models.Structure, error)
	Tok          func(context.Context, string) (*models.Watercourse, error)
	PolozajLetve func(ctx context.Context, perms *models.UserPermissions, id uuid.UUID, lat, lon float64) error
}

// SetUredjivanje uključuje uređivanje na Karti
func (h *KartaHandler) SetUredjivanje(u KartaUredjivanje) { h.ured = u }

// pravaKarte kažu što prijavljeni smije mijenjati na Karti
type pravaKarte struct {
	perms *models.UserPermissions
	ured  KartaUredjivanje
}

func (h *KartaHandler) prava(ctx context.Context) pravaKarte {
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return pravaKarte{perms: perms, ured: h.ured}
}

func (p pravaKarte) objekt(s models.Structure) bool {
	return p.perms != nil && p.ured.SmijeObjekt != nil && p.ured.SmijeObjekt(p.perms, &s)
}

func (p pravaKarte) letva(l models.Station) bool {
	return p.perms != nil && p.ured.SmijeLetvu != nil && p.ured.SmijeLetvu(p.perms, &l)
}

// vodotok: tok vode mijenja samo globalni administrator, kao na stranici vode
func (p pravaKarte) vodotok() bool {
	return p.perms != nil && p.perms.IsGlobalAdmin && p.ured.Tok != nil
}

// ista: smije li prijavljeni ucrtati išta, objekt, tok ili letvu
func (p pravaKarte) ista(objekti []models.Structure, letve []models.Station) bool {
	if p.vodotok() {
		return true
	}
	for _, s := range objekti {
		if p.objekt(s) {
			return true
		}
	}
	for _, l := range letve {
		if p.letva(l) {
			return true
		}
	}
	return false
}

// oznaci kaže uz svaki zapis koji nedostaje smije li ga prijavljeni ucrtati
func (p pravaKarte) oznaci(popis []nedostajeNaKarti, objekti []models.Structure, letve []models.Station) {
	poID := map[string]models.Structure{}
	for _, s := range objekti {
		poID[s.ID.String()] = s
	}
	letvePoID := map[string]models.Station{}
	for _, l := range letve {
		letvePoID[l.ID.String()] = l
	}
	for i := range popis {
		r := &popis[i]
		switch {
		case r.Kod != "":
			r.Uredjuje = p.vodotok()
		case r.Vrsta == models.VrstaLetva:
			r.Uredjuje = p.letva(letvePoID[r.ID])
		default:
			r.Uredjuje = p.objekt(poID[r.ID])
		}
	}
}

// objektZaUredjivanje je cijela geometrija objekta, s točkama km nasipa, i
// službena stacionaža nasipa iz dionica
type objektZaUredjivanje struct {
	ID      string   `json:"id"`
	Naziv   string   `json:"naziv"`
	Vrsta   string   `json:"vrsta"`
	Adresa  string   `json:"adresa"`
	GeoJSON string   `json:"geojson"`
	Lat     *float64 `json:"lat,omitempty"` // položaj iz registra, kad crte ili točke nema
	Lon     *float64 `json:"lon,omitempty"`
	stacionazaNasipa
}

// stacionazaNasipa je duljina nasipa po dionicama i km njegova početka, za
// mjeru crte i za stacionažu koju Karta generira
type stacionazaNasipa struct {
	DuljinaKm float64  `json:"duljina_km,omitempty"`
	OdKm      *float64 `json:"od_km,omitempty"` // najmanji km po nasipu
	Gdje      string   `json:"gdje,omitempty"`  // „B.16.1 · km 0+000 – 2+185”
	// Odsjeci su km po nasipu u svakoj dionici, od najmanjeg; kad je nasip
	// nacrtan u isto toliko dijelova, svaki dio dobije km svoje dionice
	Odsjeci []odsjekNasipa `json:"odsjeci,omitempty"`
}

type odsjekNasipa struct {
	Dionica string  `json:"dionica"`
	Od      float64 `json:"od"`
	Do      float64 `json:"do"`
}

// stacionazaObjekta zbraja odsjeke nasipa u svim dionicama: duljina iz
// dionice, inače razlika km po nasipu, inače razlika km uz vodu
func stacionazaObjekta(dionice []models.Section, id string) stacionazaNasipa {
	var out stacionazaNasipa
	var mjesta []string
	for _, d := range dionice {
		for _, p := range d.Parts {
			for _, e := range p.Embankments {
				if e.StructureID != id {
					continue
				}
				out.DuljinaKm += duljinaOdsjeka(e)
				mjesto := d.Code
				if e.EmbFrom != nil && e.EmbTo != nil {
					mjesto += " · km " + models.FormatStationing(*e.EmbFrom) + " – " + models.FormatStationing(*e.EmbTo)
					od, do := math.Min(*e.EmbFrom, *e.EmbTo), math.Max(*e.EmbFrom, *e.EmbTo)
					if out.OdKm == nil || od < *out.OdKm {
						out.OdKm = &od
					}
					out.Odsjeci = append(out.Odsjeci, odsjekNasipa{Dionica: d.Code, Od: od, Do: do})
				}
				mjesta = append(mjesta, mjesto)
			}
		}
	}
	out.DuljinaKm = math.Round(out.DuljinaKm*1000) / 1000
	out.Gdje = strings.Join(mjesta, "; ")
	sort.SliceStable(out.Odsjeci, func(i, j int) bool { return out.Odsjeci[i].Od < out.Odsjeci[j].Od })
	return out
}

func duljinaOdsjeka(e models.PartEmbankment) float64 {
	switch {
	case e.LengthKm != nil:
		return *e.LengthKm
	case e.EmbFrom != nil && e.EmbTo != nil:
		return math.Abs(*e.EmbTo - *e.EmbFrom)
	case e.WaterFrom != nil && e.WaterTo != nil:
		return math.Abs(*e.WaterTo - *e.WaterFrom)
	}
	return 0
}

// HandleObjekt vraća cijelu geometriju objekta za uređivanje na Karti
func (h *KartaHandler) HandleObjekt(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil || h.ured.Objekt == nil {
		http.Error(w, "Objekt nije pronađen", http.StatusNotFound)
		return
	}
	s, err := h.ured.Objekt(r.Context(), id)
	if err != nil {
		http.Error(w, "Objekt se nije učitao: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if s == nil {
		http.Error(w, "Objekt nije pronađen", http.StatusNotFound)
		return
	}
	out := objektZaUredjivanje{ID: s.ID.String(), Naziv: s.Name, Vrsta: s.Kind, Adresa: s.Path(), GeoJSON: s.Geometry, Lat: s.Latitude, Lon: s.Longitude}
	if h.dionice != nil {
		// bez dionica nema službene duljine, a crtati se i dalje može
		if dionice, err := h.dionice(r.Context()); err == nil {
			out.stacionazaNasipa = stacionazaObjekta(dionice, out.ID)
		}
	}
	writeJSON(w, out)
}

// HandleTok vraća cijeli tok vode za uređivanje na Karti; Karta za prikaz
// drži pojednostavljeni, a spremanje pojednostavljenog izgubilo bi zavoje
func (h *KartaHandler) HandleTok(w http.ResponseWriter, r *http.Request) {
	if h.ured.Tok == nil {
		http.Error(w, "Vodotok nije pronađen", http.StatusNotFound)
		return
	}
	v, err := h.ured.Tok(r.Context(), r.PathValue("kod"))
	if err != nil {
		http.Error(w, "Vodotok se nije učitao: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if v == nil {
		http.Error(w, "Vodotok nije pronađen", http.StatusNotFound)
		return
	}
	// stacionažu Dunava i Drave program računa prema ENC-u, pa je Karta ne generira
	writeJSON(w, map[string]any{"kod": v.Code, "naziv": prvoNeprazno(v.OfficialName, v.Name), "adresa": "/watercourses/" + v.Code,
		"geojson": v.Geometry, "stacionaza_enc": stacionazaIzEnc(v.Code), "povrsina": v.JePovrsina()})
}

// HandlePolozajLetve upisuje letvi položaj s Karte
func (h *KartaHandler) HandlePolozajLetve(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil || h.ured.PolozajLetve == nil {
		http.Error(w, "Letva nije pronađena", http.StatusNotFound)
		return
	}
	if !jsonTijelo(r) {
		http.Error(w, "Položaj se šalje kao JSON (Content-Type: application/json)", http.StatusUnsupportedMediaType)
		return
	}
	var t struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	}
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil || !polozajNaKarti(t.Lat, t.Lon) {
		http.Error(w, "Položaj letve nije ispravan", http.StatusBadRequest)
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if err := h.ured.PolozajLetve(r.Context(), perms, id, zaokruzi6(t.Lat), zaokruzi6(t.Lon)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"success": true, "message": "Položaj letve je spremljen."})
}

// polozajNaKarti: širina i dužina u stupnjevima, ne nula i ne izvan Zemlje
func polozajNaKarti(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && math.Abs(lat) <= 90 && math.Abs(lon) <= 180 && (lat != 0 || lon != 0)
}
