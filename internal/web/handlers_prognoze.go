package web

import (
	"context"
	"html/template"
	"math"
	"net/http"
	"sort"
	"time"

	"gocop/internal/hydro"
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

// BliziDosezi su satni stupci pregleda prije stupaca po danima.
var BliziDosezi = []int{6, 12}

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
	Vrh         bool   // vrh lanca: stoji samo mjerenje, prognoze nema
	URL         string
	SadaCm      string
	SadaQ       string
	Vrijednosti []VrijednostPrognoze
	Doseg       int  // dokle prognoza ide, u satima
	Ulaz        bool // ulaz dnevne prognoze: stoji samo mjerenje
	TudiVrh     bool // vrh lanca koji dalje ide po mađarskoj prognozi
	Dani        []CelijaDana
}

// CelijaDana je jedan dan pregleda, za 07 h — termin u kojem prognozu daju i
// Mađari i uredska tablica. Vrijednost je iz satnog lanca ili iz dnevnog
// modela, već prema tome koji je na tom danu za tu letvu provjerom točniji;
// ispod stoji mađarska prognoza za isti termin, gdje je imaju.
type CelijaDana struct {
	Cm, Raspon     string
	HU, HURaspon   string
	Dnevna         bool // vrijednost daje dnevni model
	Slabija        bool
	Razina, Moguce string
}

type PrognozePageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner

	Izdano     string
	Nema       bool
	Razlog     string
	Udio       int
	Dosezi     []int
	Letve      []LetvaPrognoze
	Profili    []*UzduzniProfil
	BezProfila string // zašto profila nema, kad ga nema

	Bliski   []int
	Dani     []string // naslovi stupaca po danima
	ImaTudih bool
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
	postaje := h.postaje(r.Context())
	data.Letve = h.opisiLetve(postaje, letve)
	data.Bliski = BliziDosezi
	_, dnevne, _ := c.Dnevno()
	tude := c.Tude(prognoza.Podrijetlo, izdano)
	var ciljevi []int64
	data.Dani, ciljevi = daniPregleda(izdano)
	for i := range data.Letve {
		data.Letve[i].Dani = celijeDana(data.Letve[i].Kod, letve[i], ciljevi,
			dnevne[data.Letve[i].Kod], tude[data.Letve[i].Kod], postaje[data.Letve[i].Kod])
		for _, d := range data.Letve[i].Dani {
			if d.HU != "" {
				data.ImaTudih = true
			}
		}
	}
	data.Profili = uzduzniProfili(postaje, letve)
	if len(data.Profili) == 0 {
		data.BezProfila = "Za uzdužni profil treba barem dvije letve s poznatom " +
			"stacionažom i kotom nule u novom visinskom sustavu."
	}
	h.iscrtaj(w, data)
}

func (h *PrognozeHandler) iscrtaj(w http.ResponseWriter, data PrognozePageData) {
	if err := h.tmpl.ExecuteTemplate(w, "prognoze.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// opisiLetve dodaje ono što u bazi prognoza ne stoji: kako se letva zove, na
// kojoj je vodi i gdje joj je stranica.
func (h *PrognozeHandler) postaje(ctx context.Context) map[string]models.Station {
	popis := map[string]models.Station{}
	if h.stations == nil {
		return popis
	}
	if sve, err := h.stations.ListStations(ctx, "", "", "", false); err == nil {
		for _, st := range sve {
			popis[st.Code] = st
		}
	}
	return popis
}

func (h *PrognozeHandler) opisiLetve(popis map[string]models.Station, letve []PregledLetve) []LetvaPrognoze {
	out := make([]LetvaPrognoze, 0, len(letve))
	for _, l := range letve {
		ulaz := l.Racuna == "" && !l.UlazLanca && jeDnevniUlaz(l.Letva)
		_, tudi := prognoza.VrhoviSTudomPrognozom[l.Letva]
		red := LetvaPrognoze{
			Kod: l.Letva, Naziv: l.Letva, Racuna: l.Racuna, Doseg: l.Doseg,
			Vrh: l.Racuna == "" && !ulaz, Ulaz: ulaz, TudiVrh: l.Racuna == "" && tudi,
			SadaCm: uVelicini(l.Sada, "vodostaj"), SadaQ: uVelicini(l.Sada, "protok"),
		}
		if st, ima := popis[l.Letva]; ima {
			red.Naziv, red.Voda, red.Stacionaza = st.Name, st.Watercourse, st.Stationing
			red.URL = "/readings/station/" + st.ID.String()
		}
		for _, d := range BliziDosezi {
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
	return rasponHR(v.Dolje, v.Gore, 0)
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
	Letva     string
	Racuna    string
	Sada      map[string]float64
	Doseg     int
	Po        map[string]map[int]PregledVrijednost
	Satno     map[int64]PregledVrijednost // vodostaj po ciljnom satu, za dane na pregledu
	UlazLanca bool                        // letva ulazi u neki pojas satnog lanca
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

	ulazLanca := map[string]bool{}
	for _, ps := range pojasi {
		for _, p := range ps {
			for _, u := range p.Ulazi {
				ulazLanca[u.Letva] = true
			}
		}
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
		p := PregledLetve{Letva: letva, UlazLanca: ulazLanca[letva], Sada: map[string]float64{},
			Po: map[string]map[int]PregledVrijednost{}, Satno: map[int64]PregledVrijednost{}}
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
			// Je li prognoza bolja od postojanosti mjereno je u veličini u
			// kojoj model radi; krivulja to ne mijenja, samo preslikava.
			bolja := true
			if pr, ima := promasaji[letva][d]; ima {
				bolja = pr.BoljaOdPostojanosti()
			}
			if i.Velicina == "vodostaj" && d > 0 {
				p.Satno[i.Ciljni] = PregledVrijednost{Vrijednost: i.Vrijednost,
					Dolje: i.Dolje, Gore: i.Gore, BoljaOdPostojanosti: bolja}
			}
			if !uDosezima(d) {
				continue
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

// uzduzniProfili slaže profil za svaki tok na kojem ima dovoljno letvi.
// Rijeke se ne miješaju: Drava i Dunav imaju svoje kote i svoj nagib, a jedan
// crtež kroz obje pokazivao bi skok na ušću koji nije val nego spoj dvaju
// tokova.
func uzduzniProfili(postaje map[string]models.Station, letve []PregledLetve) []*UzduzniProfil {
	poVodi := map[string][]LetvaProfila{}
	var redom []string
	for _, l := range letve {
		st, ima := postaje[l.Letva]
		if !ima || st.ZeroDatumNew == nil {
			continue
		}
		rkm, ok := hydro.ParseStationingKm(st.Stationing)
		if !ok {
			continue
		}
		lp := LetvaProfila{
			Letva: l.Letva, Naziv: st.Name, Rkm: rkm, KotaNule: *st.ZeroDatumNew,
			Cm: map[int]float64{}, Granice: map[int][2]float64{},
			Pragovi: map[string]float64{},
		}
		if v, ima := l.Sada["vodostaj"]; ima {
			lp.SadaCm, lp.ImaSada = v, true
		}
		for d, v := range l.Po["vodostaj"] {
			lp.Cm[d] = v.Vrijednost
			lp.Granice[d] = [2]float64{v.Dolje, v.Gore}
		}
		for kljuc, prag := range map[string]models.Threshold{
			"prep": st.Prep, "regular": st.Regular, "emerg": st.Emergency} {
			if prag.IsUsable() {
				lp.Pragovi[kljuc] = float64(*prag.Cm)
			}
		}
		voda := st.Watercourse
		if voda == "" {
			voda = "ostalo"
		}
		if _, bilo := poVodi[voda]; !bilo {
			redom = append(redom, voda)
		}
		poVodi[voda] = append(poVodi[voda], lp)
	}
	var out []*UzduzniProfil
	for _, voda := range redom {
		if p := crtajUzduzni(voda, poVodi[voda]); p != nil {
			out = append(out, p)
		}
	}
	return out
}

// razinaObrane je najviša faza obrane čiji je prag dosegnut.
func razinaObrane(st models.Station, cm float64) string {
	razina := ""
	for _, p := range []struct {
		ime  string
		prag models.Threshold
	}{{"prep", st.Prep}, {"regular", st.Regular}, {"emerg", st.Emergency}, {"crit", st.State}} {
		if p.prag.IsUsable() && cm >= float64(*p.prag.Cm) {
			razina = p.ime
		}
	}
	return razina
}

var daniUTjednu = []string{"ned", "pon", "uto", "sri", "čet", "pet", "sub"}

// daniPregleda su termini stupaca po danima: prvih šest 07 h poslije izdanja.
func daniPregleda(izdano time.Time) ([]string, []int64) {
	lok := izdano.In(models.Zagreb)
	jutro := time.Date(lok.Year(), lok.Month(), lok.Day(), 7, 0, 0, 0, models.Zagreb)
	if !jutro.After(lok) {
		jutro = jutro.AddDate(0, 0, 1)
	}
	var naslovi []string
	var sati []int64
	for k := 0; k < prognoza.DnevniDosezi; k++ {
		d := jutro.AddDate(0, 0, k)
		naslovi = append(naslovi, daniUTjednu[d.Weekday()]+" "+d.Format("2.1."))
		sati = append(sati, d.UTC().Unix()/3600)
	}
	return naslovi, sati
}

func jeDnevniUlaz(letva string) bool {
	for _, u := range prognoza.DnevniUlazi() {
		if u == letva {
			return true
		}
	}
	return false
}

// celijeDana slaže dane jedne letve: za svaki termin vrijednost iz modela koji
// je ondje točniji, raspon, mađarsku prognozu i fazu obrane.
func celijeDana(letva string, l PregledLetve, ciljevi []int64, dnevne []prognoza.DnevnaIzdana,
	tude map[int64]TudaVrijednost, st models.Station) []CelijaDana {
	odDana := prognoza.DnevnaOdDana[letva]
	out := make([]CelijaDana, len(ciljevi))
	for k, t := range ciljevi {
		c := &out[k]
		var v, dolje, gore float64
		ima := false
		satni, imaSatni := l.Satno[t]
		dnevni, imaDnevni := dnevniU(dnevne, t)
		switch {
		case imaDnevni && odDana > 0 && k+1 >= odDana:
			v, dolje, gore, ima, c.Dnevna = dnevni.Vrijednost, dnevni.Dolje, dnevni.Gore, true, true
		case imaSatni:
			v, dolje, gore, ima = satni.Vrijednost, satni.Dolje, satni.Gore, true
			c.Slabija = !satni.BoljaOdPostojanosti
		case imaDnevni:
			v, dolje, gore, ima, c.Dnevna = dnevni.Vrijednost, dnevni.Dolje, dnevni.Gore, true, true
		}
		if ima {
			c.Cm = brojHRf(v, 0)
			if gore > dolje {
				c.Raspon = rasponHR(dolje, gore, 0)
			}
			c.Razina = razinaObrane(st, v)
			if g := razinaObrane(st, gore); g != c.Razina {
				c.Moguce = g
			}
		}
		if hu, imaHU := tude[t]; imaHU {
			c.HU = brojHRf(hu.Cm, 0)
			if hu.PlusMin > 0 {
				c.HURaspon = "±" + brojHRf(hu.PlusMin, 0)
			}
		}
	}
	return out
}

// dnevniU procjenjuje dnevnu prognozu u zadanom satu. Dnevna vrijednost je
// srednjak 24 sata, pa se pripisuje njihovoj sredini, a između dviju sredina
// ide pravocrtno.
func dnevniU(dnevne []prognoza.DnevnaIzdana, t int64) (PregledVrijednost, bool) {
	for i := 0; i+1 < len(dnevne); i++ {
		a, b := dnevne[i], dnevne[i+1]
		ca, cb := a.Ciljni-12, b.Ciljni-12
		if t < ca || t > cb || cb == ca {
			continue
		}
		u := float64(t-ca) / float64(cb-ca)
		ip := func(x, y float64) float64 { return x + u*(y-x) }
		return PregledVrijednost{Vrijednost: ip(a.Vrijednost, b.Vrijednost),
			Dolje: ip(a.Dolje, b.Dolje), Gore: ip(a.Gore, b.Gore), BoljaOdPostojanosti: true}, true
	}
	return PregledVrijednost{}, false
}
