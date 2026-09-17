package web

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Materijalno-tehnička sredstva na ekranu: pregled sektora, kartica
// skladišta s prometom, obrazac zahvata, knjiga prometa, godišnji popis i
// pogled „gdje ima“ kroz sve sektore. Sve pod /sredstva.

type MtsHandler struct {
	svc      func() *service.MtsService
	users    *service.UserService
	sections *service.SectionService
	journals *service.JournalService
	tmpl     func(string) *template.Template
}

func NewMtsHandler(svc func() *service.MtsService, users *service.UserService, sections *service.SectionService,
	journals *service.JournalService, tmpl func(string) *template.Template) *MtsHandler {
	return &MtsHandler{svc: svc, users: users, sections: sections, journals: journals, tmpl: tmpl}
}

// MtsPageData je ono što stranice sredstava trebaju
type MtsPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions

	Sektor   string
	Sektori  []models.Sector
	Podrucja []models.Area

	Skladista []models.Skladiste
	Skladiste *models.Skladiste
	Stanje    []models.StanjeVrste
	Grupe     []struct{ ID, Rimski, Naziv string }
	Vrste     []models.VrstaSredstva
	Vrsta     *models.VrstaSredstva

	Promet       []models.Promet
	Zahvat       service.Zahvat
	VrstePrometa []struct{ ID, Naziv, Opis string }
	Dionice      []models.Section
	Obrane       []models.Journal
	NaTerenu     []models.Stanje

	Popis  *models.Popis
	Popisi []models.Popis
	Godina int

	GdjeIma []service.MjestoZalihe

	SmijePisati  bool
	SmijeUrediti bool
	IsEdit       bool
	Danas        string
	Od, Do       string
	VrstaID      string

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// Oblici su svi oblici koji se u stanju pojavljuju, za zaglavlje tablice
func (d MtsPageData) Oblici() []string {
	vidjeno := map[string]bool{}
	var out []string
	for _, sv := range d.Stanje {
		for _, o := range sv.Vrsta.SviOblici() {
			if o != models.OblikOsnovni && !vidjeno[o] {
				vidjeno[o] = true
				out = append(out, o)
			}
		}
	}
	sort.Strings(out)
	return out
}

// PoGrupama slaže stanje po skupinama redom popisa
func (d MtsPageData) PoGrupama() []GrupaStanja {
	var out []GrupaStanja
	for _, g := range models.GrupeSredstava {
		gs := GrupaStanja{ID: g.ID, Rimski: g.Rimski, Naziv: g.Naziv}
		for _, sv := range d.Stanje {
			if sv.Vrsta.Grupa == g.ID {
				gs.Redci = append(gs.Redci, sv)
			}
		}
		if len(gs.Redci) > 0 {
			out = append(out, gs)
		}
	}
	return out
}

// GrupaStanja je skupina sa svojim redcima stanja
type GrupaStanja struct {
	ID, Rimski, Naziv string
	Redci             []models.StanjeVrste
}

// StavkePoGrupama slaže stavke popisa po skupinama, redom popisa
func (d MtsPageData) StavkePoGrupama() []GrupaStavki {
	if d.Popis == nil {
		return nil
	}
	var out []GrupaStavki
	for _, g := range models.GrupeSredstava {
		gs := GrupaStavki{ID: g.ID, Rimski: g.Rimski, Naziv: g.Naziv}
		for _, st := range d.Popis.Stavke {
			if st.Grupa == g.ID {
				gs.Stavke = append(gs.Stavke, st)
			}
		}
		if len(gs.Stavke) > 0 {
			out = append(out, gs)
		}
	}
	return out
}

// GrupaStavki je skupina sa svojim stavkama popisa
type GrupaStavki struct {
	ID, Rimski, Naziv string
	Stavke            []models.PopisnaStavka
}

func (h *MtsHandler) pageData(r *http.Request) MtsPageData {
	ctx := r.Context()
	u, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	d := MtsPageData{
		CurrentUser: u, Permissions: perms,
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "sredstva", ViewAsBanner: viewBanner(r),
		Grupe: models.GrupeSredstava, VrstePrometa: models.VrstePrometa,
		Danas: time.Now().In(models.Zagreb).Format("2006-01-02"),
	}
	d.Sektori, _ = h.users.ListSectors()
	d.Sektor = strings.TrimSpace(r.URL.Query().Get("sektor"))
	if d.Sektor == "" {
		d.Sektor = sektorKorisnika(perms, d.Sektori)
	}
	return d
}

// sektorKorisnika bira sektor koji se pokazuje kad ga zahtjev ne kaže: onaj
// u kojem osoba radi, a administratoru prvi po redu
func sektorKorisnika(perms *models.UserPermissions, sektori []models.Sector) string {
	if perms == nil {
		return ""
	}
	var moji []string
	for s := range perms.AdminSectors {
		moji = append(moji, s)
	}
	for s := range perms.AllowedSectors {
		moji = append(moji, s)
	}
	sort.Strings(moji)
	if len(moji) > 0 {
		return moji[0]
	}
	for _, s := range sektori {
		if s.ID != "DIREKCIJA" {
			return s.ID
		}
	}
	return ""
}

func (h *MtsHandler) render(w http.ResponseWriter, name string, data MtsPageData) {
	if err := h.tmpl(name).ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowPregled prikazuje stanje sektora po vrstama i njegova skladišta
func (h *MtsHandler) ShowPregled(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	svc := h.svc()
	if svc == nil {
		http.Error(w, "sredstva nisu dostupna", http.StatusServiceUnavailable)
		return
	}
	var err error
	if data.Skladista, err = svc.Skladista(r.Context(), data.Sektor, 0, false); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if data.Stanje, err = svc.StanjeSektora(r.Context(), data.Sektor, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.SmijeUrediti = svc.SmijeUrediti(data.Permissions, data.Sektor, 0)
	data.Podrucja, _ = h.users.ListAreas(data.Sektor)
	h.render(w, "sredstva.html", data)
}

// ---- skladišta

func (h *MtsHandler) ucitajSkladiste(w http.ResponseWriter, r *http.Request, data *MtsPageData) (*models.Skladiste, bool) {
	sk, err := h.svc().Skladiste(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, false
	}
	if sk == nil {
		http.NotFound(w, r)
		return nil, false
	}
	data.Skladiste, data.Sektor = sk, sk.Sektor
	data.SmijePisati = h.svc().SmijePisati(data.Permissions, sk)
	data.SmijeUrediti = h.svc().SmijeUrediti(data.Permissions, sk.Sektor, sk.AreaID)
	return sk, true
}

// ShowSkladiste prikazuje karticu skladišta: stanje i zadnji promet
func (h *MtsHandler) ShowSkladiste(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	sk, ok := h.ucitajSkladiste(w, r, &data)
	if !ok {
		return
	}
	var err error
	if data.Stanje, err = h.svc().StanjeSkladista(r.Context(), sk.ID, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.Promet, _ = h.svc().Promet(r.Context(), repository.FiltarPrometa{SkladisteID: sk.ID, Limit: 25})
	data.Popisi, _ = h.svc().Popisi(r.Context(), "", sk.ID, 0)
	h.render(w, "skladiste.html", data)
}

// ShowSkladisteForm prikazuje obrazac za novo skladište ili izmjenu
func (h *MtsHandler) ShowSkladisteForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if id := r.PathValue("id"); id != "" {
		if _, ok := h.ucitajSkladiste(w, r, &data); !ok {
			return
		}
		if !data.SmijeUrediti {
			http.Error(w, "Skladišta uređuje uprava branjenog područja ili sektora", http.StatusForbidden)
			return
		}
		data.IsEdit = true
	} else {
		data.Skladiste = &models.Skladiste{Sektor: data.Sektor, Aktivno: true}
		if a, err := strconv.Atoi(r.URL.Query().Get("area")); err == nil {
			data.Skladiste.AreaID = a
		}
		data.SmijeUrediti = h.svc().SmijeUrediti(data.Permissions, data.Sektor, 0)
	}
	data.Podrucja, _ = h.users.ListAreas(data.Sektor)
	h.render(w, "skladiste_form.html", data)
}

// HandleSaveSkladiste upisuje skladište
func (h *MtsHandler) HandleSaveSkladiste(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/sredstva", "error", "Neispravan zahtjev")
		return
	}
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	sk := &models.Skladiste{ID: r.PathValue("id"), Sektor: f("sektor"), Naziv: f("naziv"), Adresa: f("adresa"),
		StructureID: f("structure_id"), ContractorID: f("contractor_id"), Centralno: r.FormValue("centralno") == "1",
		Aktivno: r.FormValue("aktivno") != "0", Napomena: f("napomena")}
	sk.AreaID, _ = strconv.Atoi(f("area_id"))
	if sk.ID != "" {
		cur, err := h.svc().Skladiste(r.Context(), sk.ID)
		if err != nil || cur == nil {
			http.NotFound(w, r)
			return
		}
		sk.CreatedAt = cur.CreatedAt
	}
	if err := h.svc().SpremiSkladiste(r.Context(), data.Permissions, sk); err != nil {
		back := "/sredstva/skladista/novo?sektor=" + sk.Sektor
		if sk.ID != "" {
			back = "/sredstva/skladista/" + sk.ID + "/uredi"
		}
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, "/sredstva/skladista/"+sk.ID, "success", "Skladište je upisano.")
}

// ---- promet

// ShowPrometForm prikazuje obrazac zahvata u skladištu
func (h *MtsHandler) ShowPrometForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	sk, ok := h.ucitajSkladiste(w, r, &data)
	if !ok {
		return
	}
	if !data.SmijePisati {
		http.Error(w, "Promet sredstava upisuje uprava branjenog područja ili sektora", http.StatusForbidden)
		return
	}
	h.popuniObrazacPrometa(r, &data, sk)
	q := r.URL.Query()
	data.Zahvat = service.Zahvat{Vrsta: q.Get("vrsta"), SkladisteID: sk.ID, VrstaID: q.Get("sredstvo"), Oblik: q.Get("oblik"),
		Datum: time.Now().In(models.Zagreb)}
	if data.Zahvat.Vrsta == "" {
		data.Zahvat.Vrsta = models.PrometPrimka
	}
	h.render(w, "promet_form.html", data)
}

// popuniObrazacPrometa skuplja što obrazac nudi: vrste, druga skladišta,
// dionice sektora i otvorene obrane
func (h *MtsHandler) popuniObrazacPrometa(r *http.Request, data *MtsPageData, sk *models.Skladiste) {
	data.Vrste, _ = h.svc().Vrste(r.Context())
	data.Skladista, _ = h.svc().Skladista(r.Context(), "", 0, false)
	if h.sections != nil {
		data.Dionice, _ = h.sections.ListSections(sk.Sektor, 0, "")
	}
	if h.journals != nil {
		if sve, err := h.journals.ListCOPJournals(r.Context(), sk.Sektor); err == nil {
			for _, j := range sve {
				if j.EndedAt == nil && !j.Reconstruction {
					data.Obrane = append(data.Obrane, j)
				}
			}
		}
	}
}

// HandleSavePromet provodi zahvat
func (h *MtsHandler) HandleSavePromet(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/sredstva", "error", "Neispravan zahtjev")
		return
	}
	id := r.PathValue("id")
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	z := service.Zahvat{Vrsta: f("vrsta"), SkladisteID: id, VrstaID: f("sredstvo"), Oblik: f("oblik"), UOblik: f("u_oblik"),
		NaSkladisteID: f("na_skladiste"), SectionCode: f("dionica"), JournalID: f("obrana"),
		Nalozio: f("nalozio"), Preuzeo: f("preuzeo"), Dokument: f("dokument"), Napomena: f("napomena")}
	if t, err := time.ParseInLocation("2006-01-02", f("datum"), models.Zagreb); err == nil {
		z.Datum = t
	}
	back := "/sredstva/skladista/" + id + "/promet/novo?vrsta=" + z.Vrsta + "&sredstvo=" + z.VrstaID
	k, ok := parseBroj(f("kolicina"))
	if !ok {
		redirectWith(w, r, back, "error", "Količina nije broj")
		return
	}
	z.Kolicina = k
	redci, err := h.svc().Provedi(r.Context(), data.CurrentUser, data.Permissions, z)
	if err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	poruka := models.PrometNaziv(z.Vrsta) + ": " + kolicinaHR(z.Kolicina)
	if len(redci) > 0 && redci[0].Jedinica != "" {
		poruka += " " + redci[0].Jedinica
	}
	if r.FormValue("jos") == "1" {
		redirectWith(w, r, "/sredstva/skladista/"+id+"/promet/novo?vrsta="+z.Vrsta, "success", poruka+" je upisano; sljedeći zahvat.")
		return
	}
	redirectWith(w, r, "/sredstva/skladista/"+id, "success", poruka+" je upisano.")
}

// ShowPromet prikazuje knjigu prometa po filtru
func (h *MtsHandler) ShowPromet(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	q := r.URL.Query()
	fl := repository.FiltarPrometa{Sektor: data.Sektor, SkladisteID: q.Get("skladiste"), VrstaID: q.Get("vrsta"), Limit: 500}
	data.VrstaID = fl.VrstaID
	if t, err := time.ParseInLocation("2006-01-02", q.Get("od"), models.Zagreb); err == nil {
		fl.Od, data.Od = &t, q.Get("od")
	}
	if t, err := time.ParseInLocation("2006-01-02", q.Get("do"), models.Zagreb); err == nil {
		fl.Do, data.Do = &t, q.Get("do")
	}
	if fl.SkladisteID != "" {
		data.Skladiste, _ = h.svc().Skladiste(r.Context(), fl.SkladisteID)
	}
	var err error
	if data.Promet, err = h.svc().Promet(r.Context(), fl); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.Skladista, _ = h.svc().Skladista(r.Context(), data.Sektor, 0, true)
	data.Vrste, _ = h.svc().Vrste(r.Context())
	h.render(w, "promet.html", data)
}

// ShowGdjeIma prikazuje gdje jedne vrste ima, kroz sve sektore
func (h *MtsHandler) ShowGdjeIma(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	v, err := h.svc().Vrsta(r.Context(), r.PathValue("vrsta"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if v == nil {
		http.NotFound(w, r)
		return
	}
	data.Vrsta = v
	if data.GdjeIma, err = h.svc().GdjeIma(r.Context(), v.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.render(w, "gdje_ima.html", data)
}

// ShowNaTerenu prikazuje što je na terenu jedne obrane
func (h *MtsHandler) ShowNaTerenu(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	journalID := r.URL.Query().Get("obrana")
	var err error
	if data.NaTerenu, err = h.svc().NaTerenu(r.Context(), journalID, data.Sektor); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.Vrste, _ = h.svc().Vrste(r.Context())
	if h.journals != nil {
		if sve, err := h.journals.ListCOPJournals(r.Context(), data.Sektor); err == nil {
			data.Obrane = sve
		}
	}
	if journalID != "" {
		data.Promet, _ = h.svc().Promet(r.Context(), repository.FiltarPrometa{JournalID: journalID, Limit: 500})
	}
	data.VrstaID = journalID
	h.render(w, "na_terenu.html", data)
}

// ---- godišnji popis

// ShowPopisi prikazuje popise sektora po godinama
func (h *MtsHandler) ShowPopisi(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	data.Godina, _ = strconv.Atoi(r.URL.Query().Get("godina"))
	var err error
	if data.Popisi, err = h.svc().Popisi(r.Context(), data.Sektor, "", data.Godina); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.Skladista, _ = h.svc().Skladista(r.Context(), data.Sektor, 0, false)
	h.render(w, "popisi.html", data)
}

// ShowPopisNovo otvara popis skladišta na dan: postojeći ili predložak
func (h *MtsHandler) ShowPopisNovo(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	sk, ok := h.ucitajSkladiste(w, r, &data)
	if !ok {
		return
	}
	if !data.SmijePisati {
		http.Error(w, "Popis vodi uprava branjenog područja ili sektora", http.StatusForbidden)
		return
	}
	dan := time.Now().In(models.Zagreb)
	if t, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("dan"), models.Zagreb); err == nil {
		dan = t
	}
	p, err := h.svc().PredlozakPopisa(r.Context(), sk.ID, dan)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if p.ID != "" {
		http.Redirect(w, r, "/sredstva/popisi/"+p.ID, http.StatusSeeOther)
		return
	}
	data.Popis = p
	h.render(w, "popis_form.html", data)
}

func (h *MtsHandler) ucitajPopis(w http.ResponseWriter, r *http.Request, data *MtsPageData) (*models.Popis, bool) {
	p, err := h.svc().Popis(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, false
	}
	if p == nil {
		http.NotFound(w, r)
		return nil, false
	}
	sk, err := h.svc().Skladiste(r.Context(), p.SkladisteID)
	if err != nil || sk == nil {
		http.NotFound(w, r)
		return nil, false
	}
	data.Popis, data.Skladiste, data.Sektor = p, sk, sk.Sektor
	data.SmijePisati = h.svc().SmijePisati(data.Permissions, sk)
	return p, true
}

// ShowPopis prikazuje popis kao dokument
func (h *MtsHandler) ShowPopis(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if _, ok := h.ucitajPopis(w, r, &data); !ok {
		return
	}
	h.render(w, "popis.html", data)
}

// ShowPopisUredi prikazuje obrazac nezaključenog popisa
func (h *MtsHandler) ShowPopisUredi(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	p, ok := h.ucitajPopis(w, r, &data)
	if !ok {
		return
	}
	if !data.SmijePisati || p.Zakljucen() {
		http.Error(w, "Zaključeni popis se ne mijenja", http.StatusForbidden)
		return
	}
	data.IsEdit = true
	h.render(w, "popis_form.html", data)
}

// HandleSavePopis upisuje popis iz obrasca; polja su u:<vrsta>:<oblik>,
// p:<vrsta>:<oblik> i n:<vrsta>:<oblik>
func (h *MtsHandler) HandleSavePopis(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/sredstva/popisi", "error", "Neispravan zahtjev")
		return
	}
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	p := &models.Popis{ID: r.PathValue("id"), SkladisteID: f("skladiste"), Napomena: f("napomena")}
	if t, err := time.ParseInLocation("2006-01-02", f("dan"), models.Zagreb); err == nil {
		p.Dan = t
	}
	vrste, _ := h.svc().SveVrste(r.Context())
	for _, v := range vrste {
		for _, o := range v.SviOblici() {
			kljuc := v.ID + ":" + o
			u, _ := parseBroj(f("u:" + kljuc))
			pot, _ := parseBroj(f("p:" + kljuc))
			kn, _ := parseBroj(f("k:" + kljuc))
			st := models.PopisnaStavka{VrstaID: v.ID, Oblik: o, Utvrdjeno: u, Knjizno: kn, Potrebno: pot, Napomena: f("n:" + kljuc)}
			p.Stavke = append(p.Stavke, st)
		}
	}
	if err := h.svc().SpremiPopis(r.Context(), data.CurrentUser, data.Permissions, p); err != nil {
		back := "/sredstva/skladista/" + p.SkladisteID + "/popis?dan=" + f("dan")
		if p.ID != "" {
			back = "/sredstva/popisi/" + p.ID + "/uredi"
		}
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	if r.FormValue("zakljuci") == "1" {
		if err := h.svc().ZakljuciPopis(r.Context(), data.CurrentUser, data.Permissions, p.ID); err != nil {
			redirectWith(w, r, "/sredstva/popisi/"+p.ID, "error", "Popis je spremljen, ali nije zaključen: "+err.Error())
			return
		}
		redirectWith(w, r, "/sredstva/popisi/"+p.ID, "success", "Popis je zaključen; razlike su proknjižene.")
		return
	}
	redirectWith(w, r, "/sredstva/popisi/"+p.ID, "success", "Popis je spremljen kao nacrt.")
}

// HandleZakljuciPopis proknjižava razlike
func (h *MtsHandler) HandleZakljuciPopis(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	id := r.PathValue("id")
	if err := h.svc().ZakljuciPopis(r.Context(), data.CurrentUser, data.Permissions, id); err != nil {
		redirectWith(w, r, "/sredstva/popisi/"+id, "error", err.Error())
		return
	}
	redirectWith(w, r, "/sredstva/popisi/"+id, "success", "Popis je zaključen; razlike su proknjižene.")
}

// kolicinaHR piše količinu bez suvišnih decimala, sa zarezom i razmakom
// tisućica: 102550 → "102 550", 12,5 → "12,5"
func kolicinaHR(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := strconv.FormatFloat(v, 'f', 3, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	cijeli, dec, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range cijeli {
		if i > 0 && (len(cijeli)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if dec != "" {
		out += "," + dec
	}
	if neg {
		out = "−" + out
	}
	return out
}

// kolicinaSaZnakom piše količinu s predznakom, za knjigu prometa
func kolicinaSaZnakom(v float64) string {
	if v > 0 {
		return "+" + kolicinaHR(v)
	}
	return kolicinaHR(v)
}

// obraneSektora vraća otvorene dnevnike COP-a sektora, za obrasce
func obraneSektora(ctx context.Context, js *service.JournalService, sektor string) []models.Journal {
	if js == nil {
		return nil
	}
	sve, err := js.ListCOPJournals(ctx, sektor)
	if err != nil {
		return nil
	}
	var out []models.Journal
	for _, j := range sve {
		if j.EndedAt == nil && !j.Reconstruction {
			out = append(out, j)
		}
	}
	return out
}

var _ = fmt.Sprintf
