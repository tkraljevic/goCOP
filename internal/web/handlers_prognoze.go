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

// VrijednostPrognoze je jedna brojka na pregledu, u obje veličine. Vodostaj je
// glavni jer se u obrani čita on; protok stoji ispod, sitnije. Ondje gdje
// krivulje nema, druge veličine nema ni na pregledu.
type VrijednostPrognoze struct {
	DosegH   int
	Ima      bool
	Cm       string
	CmRaspon string
	Q        string
	QRaspon  string
	Slabija  bool // na tom dosegu postojanost je bolja
}

// LetvaPrognoze je jedan redak pregleda.
type LetvaPrognoze struct {
	Kod, Naziv  string
	Voda        string
	Stacionaza  string
	Racuna      string // u čemu model radi; druga veličina dolazi iz krivulje
	URL         string
	SadaCm      string
	SadaQ       string
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
			Kod: l.Letva, Naziv: l.Letva, Racuna: l.Racuna, Doseg: l.Doseg,
			SadaCm: uVelicini(l.Sada, "vodostaj"), SadaQ: uVelicini(l.Sada, "protok"),
		}
		if st, ima := popis[l.Letva]; ima {
			red.Naziv, red.Voda, red.Stacionaza = st.Name, st.Watercourse, st.Stationing
			red.URL = "/readings/station/" + st.ID.String()
		}
		for _, d := range DoseziPregleda {
			cm, imaCm := l.Po["vodostaj"][d]
			q, imaQ := l.Po["protok"][d]
			v := VrijednostPrognoze{DosegH: d, Ima: imaCm || imaQ}
			if imaCm {
				v.Cm, v.CmRaspon = brojHRf(cm.Vrijednost, 0), granice(cm)
				v.Slabija = !cm.BoljaOdPostojanosti
			}
			if imaQ {
				v.Q, v.QRaspon = brojHRf(q.Vrijednost, 0), granice(q)
				if !imaCm {
					v.Slabija = !q.BoljaOdPostojanosti
				}
			}
			red.Vrijednosti = append(red.Vrijednosti, v)
		}
		out = append(out, red)
	}
	return out
}

// granice ispisuje raspon kao dvije brojke. Simetričan bi se dao pisati i s ±,
// ali onaj dobiven krivuljom to nije, pa bi dvije vrste zapisa u istoj tablici
// zbunjivale više nego što bi skratile.
func granice(v PregledVrijednost) string {
	if v.Gore <= v.Dolje {
		return ""
	}
	return brojHRf(v.Dolje, 0) + "–" + brojHRf(v.Gore, 0)
}

func uVelicini(sada map[string]float64, velicina string) string {
	v, ima := sada[velicina]
	if !ima {
		return ""
	}
	return brojHRf(v, 0)
}

// PregledVrijednost je jedna prognozirana vrijednost na pregledu.
type PregledVrijednost struct {
	Vrijednost          float64
	Dolje, Gore         float64
	BoljaOdPostojanosti bool
}

// PregledLetve je prognoza jedne letve, po veličini pa po dosegu. Racuna kaže
// u kojoj veličini model doista radi; druga dolazi iz krivulje.
type PregledLetve struct {
	Letva  string
	Racuna string
	Sada   map[string]float64
	Doseg  int
	Po     map[string]map[int]PregledVrijednost
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
		p := PregledLetve{Letva: letva, Sada: map[string]float64{},
			Po: map[string]map[int]PregledVrijednost{}}
		if len(pojasi[letva]) > 0 {
			p.Racuna = pojasi[letva][0].Velicina
		}
		for _, i := range niz {
			d := int(i.Ciljni - izdano)
			if d > p.Doseg {
				p.Doseg = d
			}
			if d == 0 {
				p.Sada[i.Velicina] = i.Vrijednost
			}
			if !uDosezima(d) {
				continue
			}
			// Je li prognoza bolja od postojanosti mjereno je u veličini u
			// kojoj model radi; krivulja to ne mijenja, samo preslikava.
			bolja := true
			if pr, ima := promasaji[letva][d]; ima {
				bolja = pr.BoljaOdPostojanosti()
			}
			if p.Po[i.Velicina] == nil {
				p.Po[i.Velicina] = map[int]PregledVrijednost{}
			}
			p.Po[i.Velicina][d] = PregledVrijednost{Vrijednost: i.Vrijednost,
				Dolje: i.Dolje, Gore: i.Gore, BoljaOdPostojanosti: bolja}
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
