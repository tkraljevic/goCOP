package web

import (
	"context"
	"html/template"
	"math"
	"net/http"
	"sort"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/service"
)

// Odjeljak za prognoze. Jedna stranica za cijeli sliv, složena kako voda teče —
// uzvodne letve prije nizvodnih, a ne po abecedi. Uz svaku vrijednost stoji
// raspon unutar kojeg se očekuje da ostane, a ondje gdje prognoza ne pobjeđuje
// postojanost to i piše: prognoza koja ne zna više od "bit će kao i sad" nije
// prognoza nego trošak.

// Dosezi su vremena koja se na pregledu pokazuju. Dalje od toga prognoza i
// dalje postoji, ali se na jednom retku ne da čitati.
var DoseziPregleda = []int{6, 12, 24, 48, 72}

type PrognozeHandler struct {
	tmpl     *template.Template
	citac    func() *CitacPrognoza
	stations *service.StationService
}

func NewPrognozeHandler(tmpl *template.Template, citac func() *CitacPrognoza,
	stations *service.StationService) *PrognozeHandler {
	return &PrognozeHandler{tmpl: tmpl, citac: citac, stations: stations}
}

// VrijednostPrognoze je jedna brojka na pregledu.
type VrijednostPrognoze struct {
	DosegH  int
	Ima     bool
	Iznos   string
	Raspon  string
	Slabija bool // na tom dosegu postojanost je bolja
}

// LetvaPrognoze je jedan redak pregleda.
type LetvaPrognoze struct {
	Kod, Naziv  string
	Voda        string
	Stacionaza  string
	Velicina    string
	Jedinica    string
	URL         string
	Sada        string
	Vrijednosti []VrijednostPrognoze
	Doseg       int // dokle prognoza ide, u satima
}

type PrognozePageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner

	Izdano string
	Nema   bool
	Razlog string
	Udio   int
	Dosezi []int
	Letve  []LetvaPrognoze
}

func (h *PrognozeHandler) ShowPrognoze(w http.ResponseWriter, r *http.Request) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	data := PrognozePageData{
		CurrentUser: u, Permissions: perms,
		ActiveNav: "prognoze", ViewAsBanner: viewBanner(r),
		Dosezi: DoseziPregleda, Udio: int(math.Round(prognoza.UdioURasponu * 100)),
	}

	var c *CitacPrognoza
	if h.citac != nil {
		c = h.citac()
	}
	if c == nil {
		data.Nema, data.Razlog = true, "Baza prognoza nije otvorena."
		h.iscrtaj(w, data)
		return
	}
	izdano, letve, err := c.Pregled()
	if err != nil || len(letve) == 0 {
		data.Nema, data.Razlog = true, "Nijedna prognoza još nije izdana."
		h.iscrtaj(w, data)
		return
	}
	data.Izdano = izdano.In(models.Zagreb).Format("2.1.2006. u 15:04")
	data.Letve = h.opisiLetve(r.Context(), letve)
	h.iscrtaj(w, data)
}

func (h *PrognozeHandler) iscrtaj(w http.ResponseWriter, data PrognozePageData) {
	if err := h.tmpl.ExecuteTemplate(w, "prognoze.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// opisiLetve dodaje ono što u bazi prognoza ne stoji: kako se letva zove, na
// kojoj je vodi i gdje joj je stranica.
func (h *PrognozeHandler) opisiLetve(ctx context.Context, letve []PregledLetve) []LetvaPrognoze {
	popis := map[string]models.Station{}
	if h.stations != nil {
		if sve, err := h.stations.ListStations(ctx, "", "", "", false); err == nil {
			for _, st := range sve {
				popis[st.Code] = st
			}
		}
	}
	out := make([]LetvaPrognoze, 0, len(letve))
	for _, l := range letve {
		red := LetvaPrognoze{
			Kod: l.Letva, Naziv: l.Letva, Velicina: l.Velicina,
			Jedinica: models.JedinicaVelicine(l.Velicina),
			Doseg:    l.Doseg, Sada: brojHRf(l.Sada, 0),
		}
		if st, ima := popis[l.Letva]; ima {
			red.Naziv, red.Voda, red.Stacionaza = st.Name, st.Watercourse, st.Stationing
			red.URL = "/readings/station/" + st.ID.String()
		}
		for _, d := range DoseziPregleda {
			v, ima := l.Po[d]
			red.Vrijednosti = append(red.Vrijednosti, VrijednostPrognoze{
				DosegH: d, Ima: ima,
				Iznos:   brojHRf(v.Vrijednost, 0),
				Raspon:  brojHRf(v.Raspon, 0),
				Slabija: ima && !v.BoljaOdPostojanosti,
			})
		}
		out = append(out, red)
	}
	return out
}

// PregledVrijednost je jedna prognozirana vrijednost na pregledu.
type PregledVrijednost struct {
	Vrijednost          float64
	Raspon              float64
	BoljaOdPostojanosti bool
}

// PregledLetve je prognoza jedne letve, složena po dosezima.
type PregledLetve struct {
	Letva    string
	Velicina string
	Sada     float64
	Doseg    int
	Po       map[int]PregledVrijednost
}

// Pregled čita najnovije izdanje: za svaku letvu vrijednost u satu izdavanja i
// na svakom dosegu, uz podatak pobjeđuje li ondje postojanost.
func (c *CitacPrognoza) Pregled() (time.Time, []PregledLetve, error) {
	if c == nil || c.db == nil {
		return time.Time{}, nil, nil
	}
	izdano, ima, err := prognoza.ZadnjeIzdanje(c.db)
	if err != nil || !ima {
		return time.Time{}, nil, err
	}
	sve, err := prognoza.Izdanje(c.db, izdano)
	if err != nil {
		return time.Time{}, nil, err
	}
	promasaji, err := prognoza.Promasaji(c.db)
	if err != nil {
		return time.Time{}, nil, err
	}
	pojasi, err := prognoza.SviPojasi(c.db)
	if err != nil {
		return time.Time{}, nil, err
	}

	redom := prognoza.Redom(pojasi)
	mjesto := map[string]int{}
	for i, l := range redom {
		mjesto[l] = i
	}
	out := make([]PregledLetve, 0, len(sve))
	for letva, niz := range sve {
		if len(niz) == 0 {
			continue
		}
		p := PregledLetve{Letva: letva, Velicina: niz[0].Velicina,
			Sada: niz[0].Vrijednost, Po: map[int]PregledVrijednost{}}
		for _, i := range niz {
			d := int(i.Ciljni - izdano)
			if d > p.Doseg {
				p.Doseg = d
			}
			if !uDosezima(d) {
				continue
			}
			bolja := true
			if pr, ima := promasaji[letva][d]; ima {
				bolja = pr.BoljaOdPostojanosti()
			}
			p.Po[d] = PregledVrijednost{Vrijednost: i.Vrijednost, Raspon: i.Raspon,
				BoljaOdPostojanosti: bolja}
		}
		out = append(out, p)
	}
	// Popis se čita kao lanac, kako voda teče. Letva koja u lancu ne stoji
	// ide na kraj, da red ostane razumljiv i kad se pojavi nešto novo.
	red := func(l string) int {
		if i, ima := mjesto[l]; ima {
			return i
		}
		return len(redom)
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := red(out[i].Letva), red(out[j].Letva); a != b {
			return a < b
		}
		return out[i].Letva < out[j].Letva
	})
	return time.Unix(izdano*3600, 0).UTC(), out, nil
}

func uDosezima(d int) bool {
	for _, x := range DoseziPregleda {
		if x == d {
			return true
		}
	}
	return false
}
