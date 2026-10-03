package web

import (
	"context"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/dhmz"
	"gocop/internal/kisomjeri"
	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Stranica kišomjera ima isti raspored kao letva: Očitanja su svježe — zadnji
// dani po satima, zbrojevi i prognoza — a Historijat je arhiva po godinama i
// mjesecima. Svježe stoji u radnoj bazi oborina (oborine.db: izmjerene za
// stvarne kišomjere, satne s Open-Meteo za izvedene točke), povijest u
// hidrološkoj arhivi pod šifrom kišomjera.
//
// Izvedena točka ne mjeri: njezine su brojke model (ERA5-Land, Open-Meteo),
// pa to stranica mora reći uz svaku brojku, da se ne čitaju kao kišomjer.

type KisomjerStranicaHandler struct {
	svc      func() *service.KisomjerService
	arhiva   func() *repository.ArhivaRepository
	mjerenja func() *kisomjeri.Spremiste
	tmpl     func(string) *template.Template
}

// RedKise je jedna vrijednost u popisu: sat (ili 12 sati) koji završava u
// Kad, ili dan.
type RedKise struct {
	Kad      time.Time
	Sati     int // 1, 12 ili 24
	Mm       float64
	Izvor    string
	Nepotpun bool // dan još traje
}

// Oznaka je vrijeme retka kako ga dežurni čita: sat kraja razdoblja po
// zagrebačkom vremenu, a dan kao datum.
func (r RedKise) Oznaka() string {
	l := r.Kad.In(models.Zagreb)
	switch r.Sati {
	case 24:
		return r.Kad.UTC().Format("02.01.2006.")
	case 12:
		return l.Add(-12*time.Hour).Format("02.01. 15:04") + " – " + l.Format("15:04")
	}
	return l.Add(-time.Hour).Format("02.01.2006. 15:04") + " – " + l.Format("15:04")
}

// DanPrognoze je prognozirana kiša jednog dana
type DanPrognoze struct {
	Dan  time.Time
	Mm   float64
	Sati int // koliko sati dana prognoza pokriva
}

// MjesecKise je zbroj jednog mjeseca u historijatu
type MjesecKise struct {
	Mjesec     int
	Mm         float64
	Dana       int // dana s podatkom
	KisnihDana int // dana s barem 1 mm
	NajviseDan float64
	NajviseNa  time.Time
}

// GodinaKise je zbroj jedne godine u historijatu
type GodinaKise struct {
	Godina     int
	Mm         float64
	Dana       int
	KisnihDana int
	NajviseDan float64
	NajviseNa  time.Time
}

type KisomjerStranica struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	ActiveNav      string
	SuccessMessage string
	ErrorMessage   string
	ViewAsBanner

	Tocka         models.Kisomjer
	SlivNaziv     string
	Stranica      string // ocitanja | historijat
	Procjena      bool   // izvedena točka: brojke su model, ne mjerenje
	UPrognozi     bool   // dnevni model čita kišu ove točke
	RazlogVan     string // zašto ne ulazi
	PrognozaLetve string // letve čija prognoza je čita

	// Očitanja
	Pogled      string // 7, 30, 90
	Korak       string // satni, dnevni
	SamoKisa    bool   // popis bez suhih sati i dana
	Redovi      []RedKise
	Pager       Pager
	Zbroj       float64
	KisnihDana  int
	NajvisiDan  *RedKise
	ZadnjiSat   *RedKise
	Zadnjih24   *float64
	Zadnjih72   *float64
	Graf        template.HTML
	GrafOpis    string
	NemaSvjezih bool

	Prognoza       []DanPrognoze
	Prognoza24     float64
	Prognoza72     float64
	PrognozaIz     string // naziv izvedene točke čija je prognoza
	PrognozaKm     float64
	PrognozaSebe   bool
	PrognozaIzdana time.Time
	prognozaCode   string

	// Historijat
	ArhivaPogled
	Zbrojiva      bool
	ArhGraf       template.HTML
	Mjeseci       []MjesecKise
	Godine        []GodinaKise
	GodinaZbroj   float64
	GodinaDana    int
	ProsjekGodine float64
	PunihGodina   int
}

// kisaPoStranici je koliko redaka popis ima odjednom: dva dana po satima
const kisaPoStranici = 48

func (h *KisomjerStranicaHandler) podaci(w http.ResponseWriter, r *http.Request) (*KisomjerStranica, bool) {
	ctx := r.Context()
	svc := h.svc()
	if svc == nil {
		http.Error(w, "Registar kišomjera nije uključen", http.StatusServiceUnavailable)
		return nil, false
	}
	t, err := svc.GetKisomjer(ctx, r.PathValue("code"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, false
	}
	if t == nil {
		http.NotFound(w, r)
		return nil, false
	}
	u, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	p := &KisomjerStranica{CurrentUser: u, Permissions: perms, ActiveNav: "slivovi",
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ViewAsBanner: viewBanner(r), Tocka: *t, Procjena: !t.JeStvarni()}
	tezina := 0.0
	if t.Tezina != nil {
		tezina = *t.Tezina
	}
	slivovi := prognoza.SlivoviUPrognozi()
	p.UPrognozi, p.RazlogVan = prognoza.UlaziUPrognozu(t.JeStvarni(), t.Aktivan, t.Sliv, tezina, slivovi)
	if p.UPrognozi {
		p.PrognozaLetve = strings.Join(slivovi[t.Sliv], ", ")
	}
	if t.Sliv != "" {
		if sl, err := svc.ListSlivovi(ctx); err == nil {
			for _, s := range sl {
				if s.Oznaka == t.Sliv {
					p.SlivNaziv = s.Naziv
				}
			}
		}
	}
	return p, true
}

// ShowOcitanja prikazuje svježe: zadnje dane, zbrojeve i prognozu
func (h *KisomjerStranicaHandler) ShowOcitanja(w http.ResponseWriter, r *http.Request) {
	p, ok := h.podaci(w, r)
	if !ok {
		return
	}
	p.Stranica = "ocitanja"
	p.Pogled = r.URL.Query().Get("pogled")
	if p.Pogled != "30" && p.Pogled != "90" {
		p.Pogled = "7"
	}
	dana, _ := strconv.Atoi(p.Pogled)
	p.Korak = r.URL.Query().Get("korak")
	if p.Korak != "satni" && p.Korak != "dnevni" {
		p.Korak = "satni"
		if dana > 7 {
			p.Korak = "dnevni"
		}
	}
	sada := time.Now()
	od := sada.Add(-time.Duration(dana) * 24 * time.Hour)
	satni, dnevni := h.nizKise(r.Context(), p.Tocka, od, sada)
	p.NemaSvjezih = len(satni) == 0 && len(dnevni) == 0

	// Sažetak: zadnji sat i zadnja 24 i 72 sata iz satnih (i 12-satnih)
	// vrijednosti — dnevni zbroj ne zna kad je u danu palo.
	var z24, z72 float64
	n24, n72 := 0, 0
	for i := range satni {
		x := satni[i]
		if x.Sati == 1 && (p.ZadnjiSat == nil || x.Kad.After(p.ZadnjiSat.Kad)) {
			kopija := x // popis se poslije preslaže, pokazivač u njega bi odlutao
			p.ZadnjiSat = &kopija
		}
		starost := sada.Sub(x.Kad)
		if starost < 0 {
			continue
		}
		if starost < 24*time.Hour {
			z24 += x.Mm
			n24++
		}
		if starost < 72*time.Hour {
			z72 += x.Mm
			n72++
		}
	}
	if n24 > 0 {
		v := math.Round(z24*10) / 10
		p.Zadnjih24 = &v
	}
	if n72 > 0 {
		v := math.Round(z72*10) / 10
		p.Zadnjih72 = &v
	}
	for i := range dnevni {
		p.Zbroj += dnevni[i].Mm
		if dnevni[i].Mm >= 1 {
			p.KisnihDana++
		}
		if p.NajvisiDan == nil || dnevni[i].Mm > p.NajvisiDan.Mm {
			kopija := dnevni[i]
			p.NajvisiDan = &kopija
		}
	}
	p.Zbroj = math.Round(p.Zbroj*10) / 10
	if p.NajvisiDan != nil && p.NajvisiDan.Mm <= 0 {
		p.NajvisiDan = nil
	}

	h.prognoza(r.Context(), p, sada)

	// Graf: tjedan po satima (s tri dana prognoze), dulje po danima (s
	// tjednom prognoze). Prognoza je svjetlija i odvojena crtom „sada".
	var stupci []stupacKise
	if dana <= 7 {
		for _, x := range satni {
			if x.Sati == 1 {
				stupci = append(stupci, stupacKise{Kad: x.Kad.Add(-time.Hour), Sati: 1, Mm: x.Mm})
			}
		}
		if len(stupci) == 0 {
			// samo 12-satni kišomjer: stupac je pola dana
			for _, x := range satni {
				stupci = append(stupci, stupacKise{Kad: x.Kad.Add(-12 * time.Hour), Sati: 12, Mm: x.Mm})
			}
		}
		for _, x := range h.prognozaSati(r.Context(), p, sada, 72) {
			stupci = append(stupci, x)
		}
		p.GrafOpis = "po satima, s prognozom za tri dana"
	} else {
		for _, x := range dnevni {
			stupci = append(stupci, stupacKise{Kad: danUZagrebu(x.Kad), Sati: 24, Mm: x.Mm, Nepotpun: x.Nepotpun})
		}
		for _, d := range p.Prognoza {
			stupci = append(stupci, stupacKise{Kad: d.Dan, Sati: 24, Mm: d.Mm, Prognoza: true})
		}
		p.GrafOpis = "po danima, s prognozom za tjedan"
	}
	p.Graf = crtajKisu(stupci, sada)

	var redovi []RedKise
	if p.Korak == "satni" {
		redovi = satni
		if len(redovi) == 0 {
			redovi = dnevni
			p.Korak = "dnevni"
		}
	} else {
		redovi = dnevni
	}
	if p.SamoKisa = r.URL.Query().Get("kisa") == "1"; p.SamoKisa {
		var kisni []RedKise
		for _, x := range redovi {
			if x.Mm > 0 {
				kisni = append(kisni, x)
			}
		}
		redovi = kisni
	} else {
		redovi = append([]RedKise(nil), redovi...)
	}
	sort.Slice(redovi, func(a, b int) bool { return redovi[a].Kad.After(redovi[b].Kad) })
	p.Pager = pagerZa(r, "str", len(redovi), kisaPoStranici)
	do := p.Pager.Odmak() + p.Pager.PerPage
	if do > len(redovi) {
		do = len(redovi)
	}
	p.Redovi = redovi[p.Pager.Odmak():do]

	if err := h.tmpl("kisomjer.html").ExecuteTemplate(w, "kisomjer.html", p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowHistorijat prikazuje arhivu: veličine, godine, mjesece i zbrojeve
func (h *KisomjerStranicaHandler) ShowHistorijat(w http.ResponseWriter, r *http.Request) {
	p, ok := h.podaci(w, r)
	if !ok {
		return
	}
	p.Stranica = "historijat"
	a := h.arhiva()
	st := &models.Station{Code: p.Tocka.Code, Name: p.Tocka.Naziv}
	popuniArhivu(r.Context(), r, a, nil, nil, &p.ArhivaPogled, st)
	p.Zbrojiva = p.ArhVelicina == "oborina" || p.ArhVelicina == "snijeg"
	if p.Zbrojiva && a != nil {
		// godinu po godinu: čitanje spoja ne vraća više od 20 000 redaka
		var sve []models.SpojenaVrijednost
		godine, _ := a.SpojGodine(r.Context(), p.Tocka.Code, p.ArhVelicina, "dnevni")
		for _, g := range godine {
			od := time.Date(g, 1, 1, 0, 0, 0, 0, time.UTC)
			vs, _ := a.SpojRaspon(r.Context(), p.Tocka.Code, p.ArhVelicina, "dnevni",
				od, od.AddDate(1, 0, 0).Add(-time.Second), 400, 0, repository.PoVremenu)
			sve = append(sve, vs...)
		}
		p.Godine = godineKise(sve)
		var zbroj float64
		for _, g := range p.Godine {
			if g.Dana >= 360 {
				zbroj += g.Mm
				p.PunihGodina++
			}
		}
		if p.PunihGodina > 0 {
			p.ProsjekGodine = math.Round(zbroj/float64(p.PunihGodina)*10) / 10
		}
		var godina []models.SpojenaVrijednost
		for _, v := range sve {
			if v.Kad.UTC().Year() == p.ArhGodina {
				godina = append(godina, v)
			}
		}
		p.Mjeseci = mjeseciKise(godina)
		for _, m := range p.Mjeseci {
			p.GodinaZbroj += m.Mm
			p.GodinaDana += m.Dana
		}
		p.GodinaZbroj = math.Round(p.GodinaZbroj*10) / 10
		// graf: dani odabrane godine ili mjeseca
		var stupci []stupacKise
		for _, v := range godina {
			if p.ArhMjesec > 0 && int(v.Kad.UTC().Month()) != p.ArhMjesec {
				continue
			}
			stupci = append(stupci, stupacKise{Kad: danUZagrebu(v.Kad), Sati: 24, Mm: v.Vrijednost})
		}
		p.ArhGraf = crtajKisu(stupci, time.Time{})
	}
	if err := h.tmpl("kisomjer.html").ExecuteTemplate(w, "kisomjer.html", p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// danUZagrebu pretvara dan arhive (ponoć UTC) u ponoć istog datuma po
// zagrebačkom vremenu, da graf dan crta ondje gdje ga sat na zidu vidi.
func danUZagrebu(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, models.Zagreb)
}

func godineKise(vals []models.SpojenaVrijednost) []GodinaKise {
	po := map[int]*GodinaKise{}
	for _, v := range vals {
		y := v.Kad.UTC().Year()
		g := po[y]
		if g == nil {
			g = &GodinaKise{Godina: y}
			po[y] = g
		}
		g.Mm += v.Vrijednost
		g.Dana++
		if v.Vrijednost >= 1 {
			g.KisnihDana++
		}
		if v.Vrijednost > g.NajviseDan {
			g.NajviseDan, g.NajviseNa = v.Vrijednost, v.Kad
		}
	}
	out := make([]GodinaKise, 0, len(po))
	for _, g := range po {
		g.Mm = math.Round(g.Mm*10) / 10
		out = append(out, *g)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Godina > out[b].Godina })
	return out
}

func mjeseciKise(vals []models.SpojenaVrijednost) []MjesecKise {
	var out [12]MjesecKise
	for i := range out {
		out[i].Mjesec = i + 1
	}
	for _, v := range vals {
		m := &out[v.Kad.UTC().Month()-1]
		m.Mm += v.Vrijednost
		m.Dana++
		if v.Vrijednost >= 1 {
			m.KisnihDana++
		}
		if v.Vrijednost > m.NajviseDan {
			m.NajviseDan, m.NajviseNa = v.Vrijednost, v.Kad
		}
	}
	var r []MjesecKise
	for _, m := range out {
		if m.Dana > 0 {
			m.Mm = math.Round(m.Mm*10) / 10
			r = append(r, m)
		}
	}
	return r
}

// nizKise skuplja satne (1 i 12 h) i dnevne vrijednosti od od do do: arhiva
// daje što je uloženo, radna baza oborina zadnje dane. Isto razdoblje iz
// radne baze ima prednost, jer izvor zna naknadno dopuniti sat.
func (h *KisomjerStranicaHandler) nizKise(ctx context.Context, t models.Kisomjer, od, do time.Time) (satni, dnevni []RedKise) {
	type kljuc struct {
		kad  int64
		sati int
	}
	sat := map[kljuc]RedKise{}
	dan := map[string]RedKise{}
	if a := h.arhiva(); a != nil {
		if vs, err := a.SpojRaspon(ctx, t.Code, "oborina", "satni", od, do, 20000, 0, repository.PoVremenu); err == nil {
			for _, v := range vs {
				sat[kljuc{v.Kad.Unix(), 1}] = RedKise{Kad: v.Kad, Sati: 1, Mm: v.Vrijednost, Izvor: v.Izvor}
			}
		}
		odDana := time.Date(od.Year(), od.Month(), od.Day(), 0, 0, 0, 0, time.UTC)
		if vs, err := a.SpojRaspon(ctx, t.Code, "oborina", "dnevni", odDana, do, 20000, 0, repository.PoVremenu); err == nil {
			for _, v := range vs {
				dan[v.Kad.UTC().Format("2006-01-02")] = RedKise{Kad: v.Kad.UTC(), Sati: 24, Mm: v.Vrijednost, Izvor: v.Izvor}
			}
		}
	}
	danas := do.In(models.Zagreb).Format("2006-01-02")
	// Dani iz radne baze: zbroj po zagrebačkom danu. Dan koji arhiva već ima
	// ostaje arhivski, osim današnjeg koji arhiva nema.
	zbrojDana := map[string]*RedKise{}
	izSati := map[string]bool{} // dan je zbroj satnih, ne dnevna vrijednost izvora
	dodajDanu := func(d string, mm float64, izvor string) {
		z := zbrojDana[d]
		if z == nil {
			z = &RedKise{Kad: mustDan(d), Sati: 24, Izvor: izvor, Nepotpun: d == danas}
			zbrojDana[d] = z
			izSati[d] = true
		}
		z.Mm += mm
	}
	if sp := h.mjerenja(); sp != nil {
		if t.JeStvarni() {
			if ms, err := sp.Od(t.Code, od); err == nil {
				// Orahovica javlja i sate i 12-satne termine: isto razdoblje
				// dvaput. Kad ima satnih, 12-satni se ne broje.
				imaSatne := false
				for _, m := range ms {
					imaSatne = imaSatne || m.Sati == 1
				}
				imaDnevni := map[string]bool{}
				for _, m := range ms {
					if m.Sati == 24 {
						d := m.Kraj.In(models.Zagreb).Add(-time.Minute).Format("2006-01-02")
						imaDnevni[d] = true
						zbrojDana[d] = &RedKise{Kad: mustDan(d), Sati: 24, Mm: m.Oborina, Izvor: m.Izvor, Nepotpun: d == danas}
					}
				}
				for _, m := range ms {
					switch m.Sati {
					case 1, 12:
						if m.Kraj.After(do) || (m.Sati == 12 && imaSatne) {
							continue
						}
						sat[kljuc{m.Kraj.Unix(), m.Sati}] = RedKise{Kad: m.Kraj, Sati: m.Sati, Mm: m.Oborina, Izvor: m.Izvor}
						d := m.Kraj.In(models.Zagreb).Add(-time.Minute).Format("2006-01-02")
						if m.Sati == 12 {
							// DHMZ-ov dan su termini u 6 i 18 h istog datuma
							d = m.Kraj.In(models.Zagreb).Format("2006-01-02")
						}
						if !imaDnevni[d] {
							dodajDanu(d, m.Oborina, m.Izvor)
						}
					}
				}
			}
		} else if sp.DB != nil {
			rows, err := sp.DB.QueryContext(ctx, `SELECT sat, oborina FROM satne
				WHERE kisomjer = ? AND prognoza = 0 AND oborina IS NOT NULL AND sat > ? AND sat <= ?`,
				t.Code, od.Unix()/3600, do.Unix()/3600)
			if err == nil {
				for rows.Next() {
					var s int64
					var v float64
					if rows.Scan(&s, &v) != nil {
						continue
					}
					kad := time.Unix(s*3600, 0).UTC()
					sat[kljuc{kad.Unix(), 1}] = RedKise{Kad: kad, Sati: 1, Mm: v, Izvor: "openmeteo"}
					dodajDanu(kad.In(models.Zagreb).Add(-time.Minute).Format("2006-01-02"), v, "openmeteo")
				}
				rows.Close()
			}
		}
	}
	// Dan iz radne baze ulazi samo ako ga arhiva nema; zbroj satnih ulazi
	// samo za dane koje radna baza pokriva cijele (ili današnji), inače bi
	// prvi dan razdoblja bio krnj.
	prvi := od.In(models.Zagreb).Format("2006-01-02")
	for d, z := range zbrojDana {
		if _, ima := dan[d]; ima && d != danas {
			continue
		}
		if d == prvi && izSati[d] {
			continue
		}
		z.Mm = math.Round(z.Mm*10) / 10
		dan[d] = *z
	}
	for _, v := range sat {
		if !v.Kad.Before(od) {
			satni = append(satni, v)
		}
	}
	for d, v := range dan {
		if d >= od.In(models.Zagreb).Format("2006-01-02") {
			dnevni = append(dnevni, v)
		}
	}
	sort.Slice(satni, func(a, b int) bool { return satni[a].Kad.Before(satni[b].Kad) })
	sort.Slice(dnevni, func(a, b int) bool { return dnevni[a].Kad.Before(dnevni[b].Kad) })
	return satni, dnevni
}

func mustDan(d string) time.Time {
	t, _ := time.Parse("2006-01-02", d)
	return t
}

// prognozaTocka je izvedena točka čija se prognoza pokazuje: sama točka, a za
// stvarni kišomjer najbliža aktivna izvedena točka. Open-Meteo se za stvarne
// kišomjere namjerno ne preuzima — pod njihovom šifrom izgledao bi kao mjerenje.
func (h *KisomjerStranicaHandler) prognozaTocka(ctx context.Context, p *KisomjerStranica) (string, bool) {
	if !p.Tocka.JeStvarni() {
		p.PrognozaIz, p.PrognozaSebe, p.prognozaCode = p.Tocka.Naziv, true, p.Tocka.Code
		return p.Tocka.Code, true
	}
	svc := h.svc()
	if svc == nil {
		return "", false
	}
	tocke, err := svc.ListKisomjeri(ctx)
	if err != nil {
		return "", false
	}
	najbliza, km := "", math.Inf(1)
	defer func() { p.prognozaCode = najbliza }()
	for _, t := range tocke {
		if !t.Aktivan || t.JeStvarni() {
			continue
		}
		if d := dhmz.Udaljenost(p.Tocka.Latitude, p.Tocka.Longitude, t.Latitude, t.Longitude); d < km {
			najbliza, km, p.PrognozaIz = t.Code, d, t.Naziv
		}
	}
	if najbliza == "" || km > 60 {
		p.PrognozaIz, najbliza = "", ""
		return "", false
	}
	p.PrognozaKm = math.Round(km)
	return najbliza, true
}

// prognoza puni zbrojeve prognoze za 24 i 72 sata i po danima
func (h *KisomjerStranicaHandler) prognoza(ctx context.Context, p *KisomjerStranica, sada time.Time) {
	sp := h.mjerenja()
	if sp == nil || sp.DB == nil {
		return
	}
	code, ok := h.prognozaTocka(ctx, p)
	if !ok {
		return
	}
	sat := sada.Unix() / 3600
	rows, err := sp.DB.QueryContext(ctx, `SELECT sat, oborina, preuzeto FROM satne
		WHERE kisomjer = ? AND sat > ? AND oborina IS NOT NULL ORDER BY sat`, code, sat)
	if err != nil {
		return
	}
	defer rows.Close()
	po := map[string]*DanPrognoze{}
	var dani []string
	for rows.Next() {
		var s, preuzeto int64
		var v float64
		if rows.Scan(&s, &v, &preuzeto) != nil {
			continue
		}
		if s <= sat+24 {
			p.Prognoza24 += v
		}
		if s <= sat+72 {
			p.Prognoza72 += v
		}
		if pr := time.Unix(preuzeto, 0); pr.After(p.PrognozaIzdana) {
			p.PrognozaIzdana = pr
		}
		kad := time.Unix(s*3600, 0).In(models.Zagreb).Add(-time.Minute)
		d := kad.Format("2006-01-02")
		if po[d] == nil {
			po[d] = &DanPrognoze{Dan: time.Date(kad.Year(), kad.Month(), kad.Day(), 0, 0, 0, 0, models.Zagreb)}
			dani = append(dani, d)
		}
		po[d].Mm += v
		po[d].Sati++
	}
	for _, d := range dani {
		x := po[d]
		x.Mm = math.Round(x.Mm*10) / 10
		p.Prognoza = append(p.Prognoza, *x)
	}
	p.Prognoza24 = math.Round(p.Prognoza24*10) / 10
	p.Prognoza72 = math.Round(p.Prognoza72*10) / 10
}

// prognozaSati vraća satne stupce prognoze za graf
func (h *KisomjerStranicaHandler) prognozaSati(ctx context.Context, p *KisomjerStranica, sada time.Time, sati int64) []stupacKise {
	sp := h.mjerenja()
	if sp == nil || sp.DB == nil {
		return nil
	}
	code := p.prognozaCode
	if code == "" {
		return nil
	}
	sat := sada.Unix() / 3600
	rows, err := sp.DB.QueryContext(ctx, `SELECT sat, oborina FROM satne
		WHERE kisomjer = ? AND sat > ? AND sat <= ? AND oborina IS NOT NULL ORDER BY sat`, code, sat, sat+sati)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []stupacKise
	for rows.Next() {
		var s int64
		var v float64
		if rows.Scan(&s, &v) == nil {
			out = append(out, stupacKise{Kad: time.Unix((s-1)*3600, 0), Sati: 1, Mm: v, Prognoza: true})
		}
	}
	return out
}

// stupacKise je jedan stupac grafa: razdoblje od Kad u trajanju Sati
type stupacKise struct {
	Kad      time.Time
	Sati     int
	Mm       float64
	Prognoza bool
	Nepotpun bool
}

// crtajKisu crta stupce kiše kao SVG: izmjereno punom bojom, prognoza
// svjetlije, crta „sada" između. Kiša se zbraja, pa je stupac, a ne crta —
// crta između dva kišna sata lagala bi da je kišilo i između.
func crtajKisu(stupci []stupacKise, sada time.Time) template.HTML {
	if len(stupci) == 0 {
		return ""
	}
	sort.Slice(stupci, func(a, b int) bool { return stupci[a].Kad.Before(stupci[b].Kad) })
	const W, H, lijevo, desno, vrh, dno = 900.0, 220.0, 40.0, 10.0, 12.0, 26.0
	od := stupci[0].Kad
	kraj := stupci[len(stupci)-1].Kad.Add(time.Duration(stupci[len(stupci)-1].Sati) * time.Hour)
	raspon := kraj.Sub(od).Hours()
	if raspon <= 0 {
		return ""
	}
	najv := 0.0
	for _, s := range stupci {
		najv = math.Max(najv, s.Mm)
	}
	// os: okrugli korak, najmanje 2 mm da suh tjedan ne izgleda kao pljusak
	korak := 0.5
	for _, k := range []float64{0.5, 1, 2, 5, 10, 20, 25, 50, 100} {
		korak = k
		if najv/k <= 4 {
			break
		}
	}
	gornja := math.Max(korak*math.Ceil(najv/korak), math.Max(2, korak))
	sirina := W - lijevo - desno
	visina := H - vrh - dno
	x := func(t time.Time) float64 { return lijevo + t.Sub(od).Hours()/raspon*sirina }
	y := func(v float64) float64 { return vrh + visina*(1-v/gornja) }
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="graf-kise" viewBox="0 0 %.0f %.0f" preserveAspectRatio="none" role="img" aria-label="Oborina">`, W, H)
	for v := 0.0; v <= gornja+1e-9; v += korak {
		fmt.Fprintf(&b, `<line x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f" style="stroke:var(--border, #dcdfe4);stroke-width:1"/>`,
			lijevo, W-desno, y(v), y(v))
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" text-anchor="end" style="fill:var(--text-muted, #64748b);font-size:11px">%s</text>`,
			lijevo-5, y(v)+4, brojHRf(v, decimalaOsi(korak)))
	}
	// oznake dana na osi, prorijeđene da stanu
	dani := int(math.Ceil(raspon / 24))
	svaki := 1
	for dani/svaki > 14 {
		svaki++
	}
	pocetak := od.In(models.Zagreb)
	d0 := time.Date(pocetak.Year(), pocetak.Month(), pocetak.Day(), 0, 0, 0, 0, models.Zagreb)
	if d0.Before(od) {
		d0 = d0.AddDate(0, 0, 1)
	}
	for i, d := 0, d0; !d.After(kraj); i, d = i+1, d.AddDate(0, 0, 1) {
		if i%svaki != 0 {
			continue
		}
		fmt.Fprintf(&b, `<line x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f" style="stroke:var(--border, #dcdfe4);stroke-width:1;stroke-dasharray:2 3"/>`,
			x(d), x(d), vrh, H-dno)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" text-anchor="middle" style="fill:var(--text-muted, #64748b);font-size:11px">%s</text>`,
			x(d), H-8, d.Format("2.1."))
	}
	for _, s := range stupci {
		x0 := x(s.Kad)
		x1 := x(s.Kad.Add(time.Duration(s.Sati) * time.Hour))
		w := math.Max(1, (x1-x0)*0.85)
		boja := "var(--primary)"
		prozirnost := "1"
		if s.Prognoza {
			boja, prozirnost = "var(--primary-muted, #7c97bd)", "0.75"
		} else if s.Nepotpun {
			prozirnost = "0.6"
		}
		opis := s.Kad.In(models.Zagreb).Format("02.01.2006.")
		if s.Sati < 24 {
			opis = s.Kad.In(models.Zagreb).Format("02.01. 15:04") + "–" + s.Kad.Add(time.Duration(s.Sati)*time.Hour).In(models.Zagreb).Format("15:04")
		}
		if s.Prognoza {
			opis += ", prognoza"
		} else if s.Nepotpun {
			opis += ", dan još traje"
		}
		if s.Mm <= 0 {
			continue
		}
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" style="fill:%s;opacity:%s"><title>%s: %s mm</title></rect>`,
			x0+(x1-x0-w)/2, y(math.Min(s.Mm, gornja)), w, H-dno-y(math.Min(s.Mm, gornja)), boja, prozirnost,
			template.HTMLEscapeString(opis), brojHRf(s.Mm, 1))
	}
	if !sada.IsZero() && sada.After(od) && sada.Before(kraj) {
		fmt.Fprintf(&b, `<line x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f" style="stroke:var(--accent, #20ba70);stroke-width:2"/>`,
			x(sada), x(sada), vrh, H-dno)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" style="fill:var(--accent-dark, #168250);font-size:11px">sada</text>`, x(sada)+4, vrh+10)
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}
