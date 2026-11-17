package web

import (
	"context"
	"errors"
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
	svc        func() *service.MtsService
	users      *service.UserService
	sections   *service.SectionService
	journals   *service.JournalService
	structures *service.StructureService
	tmpl       func(string) *template.Template
}

// SetStructures daje rukovatelju registar objekata, za mjesta na terenu
func (h *MtsHandler) SetStructures(s *service.StructureService) { h.structures = s }

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

	// ZaInventuru su skladišta u kojima osoba smije otvoriti inventuru
	ZaInventuru []models.Skladiste

	Promet       []models.Promet
	Zahvat       service.Zahvat
	VrstePrometa []struct{ ID, Naziv, Opis string }
	Dionice      []models.Section
	Obrane       []models.Journal
	NaTerenu     []models.Stanje

	Popis  *models.Popis
	Popisi []models.Popis
	Godina int

	Objekti  []models.Structure // objekti i nasipi sektora, za mjesto na terenu
	MjestaNa []models.Stanje    // mjesta na terenu na kojima nešto stoji

	Potrebe        map[string]models.Potreba // po vrsti, za obrazac skladišta
	PotrebeSektora []models.Potreba
	GodinePotreba  []int

	GdjeIma []service.MjestoZalihe

	Potvrda         string // veza zahvata koji je upravo upisan, za ispis potvrde
	SmijePisati     bool
	SmijeUrediti    bool
	SmijeOtkljucati bool // zaključenu inventuru vraća u nacrt
	IsEdit          bool
	Danas           string
	Od, Do          string
	VrstaID         string

	Kartica string // kartica modula na kojoj se stoji (sredstva_traka.go)

	// Obrazac vraćen zbog greške pokazuje upisano, a ne spremljeno
	// (sredstva_obrazac_brojeva.go)
	VratiUpisano bool
	Upisano      map[string]string
	Neispravno   map[string]bool

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

// SkupinaSkladista su skladišta jednog branjenog područja na pregledu
type SkupinaSkladista struct {
	Naslov    string
	Skladista []models.Skladiste
}

// SkladistaPoPodrucjima slaže skladišta po branjenim područjima, redom kojim
// ih daje baza (po području); skladište bez područja ide pod sektor
func (d MtsPageData) SkladistaPoPodrucjima() []SkupinaSkladista {
	var out []SkupinaSkladista
	for _, sk := range d.Skladista {
		naslov := "Sektor " + sk.Sektor
		if sk.AreaID > 0 {
			naslov = fmt.Sprintf("BP %d · %s", sk.AreaID, sk.AreaName)
		}
		if len(out) == 0 || out[len(out)-1].Naslov != naslov {
			out = append(out, SkupinaSkladista{Naslov: naslov})
		}
		out[len(out)-1].Skladista = append(out[len(out)-1].Skladista, sk)
	}
	return out
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
		Danas:   time.Now().In(models.Zagreb).Format("2006-01-02"),
		Potvrda: r.URL.Query().Get("potvrda"),
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
	moji = append(moji, perms.SektoriRada()...)
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
	data.Podrucja, _ = h.users.ListAreas(data.Sektor) // bez područja gumb vidi samo uprava sektora
	data.SmijeUrediti = svc.SmijeDodatiSkladiste(data.Permissions, data.Sektor, data.Podrucja)
	data.Kartica = "pregled"
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
	if r.PathValue("id") == "" {
		h.obrazacNovogSkladista(w, r, data)
		return
	}
	if _, ok := h.ucitajSkladiste(w, r, &data); !ok {
		return
	}
	if !data.SmijeUrediti {
		http.Error(w, "Skladišta uređuje uprava branjenog područja ili sektora", http.StatusForbidden)
		return
	}
	data.IsEdit = true
	data.Podrucja, _ = h.users.ListAreas(data.Sektor) // bez popisa područja ostaje upisano područje skladišta
	h.render(w, "skladiste_form.html", data)
}

// obrazacNovogSkladista nudi samo područja u koja osoba smije upisati
// skladište; tko ne smije ni u jedno, obrazac ne dobiva
func (h *MtsHandler) obrazacNovogSkladista(w http.ResponseWriter, r *http.Request, data MtsPageData) {
	data.Skladiste = &models.Skladiste{Sektor: data.Sektor, Aktivno: true}
	if a, err := strconv.Atoi(r.URL.Query().Get("area")); err == nil {
		data.Skladiste.AreaID = a
	}
	sva, _ := h.users.ListAreas(data.Sektor) // bez popisa područja obrazac nudi samo ono što smije uprava sektora
	data.Podrucja = h.svc().PodrucjaZaSkladista(data.Permissions, data.Sektor, sva)
	data.SmijeUrediti = len(data.Podrucja) > 0 || h.svc().SmijeUrediti(data.Permissions, data.Sektor, 0)
	if !data.SmijeUrediti {
		http.Error(w, "Skladišta upisuje uprava branjenog područja ili sektora.", http.StatusForbidden)
		return
	}
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
		h.ponoviObrazacSkladista(w, r, data, sk, err.Error())
		return
	}
	redirectWith(w, r, "/sredstva/skladista/"+sk.ID, "success", "Skladište je upisano.")
}

// ponoviObrazacSkladista vraća obrazac s upisanim i porukom; tko skladište
// ne smije ni upisati ni urediti, vraća se na prazan obrazac s porukom
func (h *MtsHandler) ponoviObrazacSkladista(w http.ResponseWriter, r *http.Request, data MtsPageData, sk *models.Skladiste, poruka string) {
	natrag := "/sredstva/skladista/novo?sektor=" + sk.Sektor
	sva, _ := h.users.ListAreas(sk.Sektor) // bez popisa područja obrazac nudi samo ono što smije uprava sektora
	data.Podrucja = h.svc().PodrucjaZaSkladista(data.Permissions, sk.Sektor, sva)
	smije := len(data.Podrucja) > 0
	if sk.ID != "" {
		natrag = "/sredstva/skladista/" + sk.ID + "/uredi"
		data.IsEdit, data.Podrucja = true, sva
		cur, _ := h.svc().Skladiste(r.Context(), sk.ID) // postoji: provjereno prije spremanja
		smije = cur != nil && h.svc().SmijeUrediti(data.Permissions, cur.Sektor, cur.AreaID)
	}
	if !smije {
		redirectWith(w, r, natrag, "error", poruka)
		return
	}
	data.Skladiste, data.Sektor, data.SmijeUrediti, data.ErrorMessage = sk, sk.Sektor, true, poruka
	h.render(w, "skladiste_form.html", data)
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
	if data.Zahvat.Oblik == "" {
		data.Zahvat.Oblik = zadaniOblik(data.Zahvat.Vrsta)
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
	data.Podrucja, _ = h.users.ListAreas(sk.Sektor)
	if h.structures != nil {
		data.Objekti, _ = h.structures.List(r.Context(), sk.Sektor, 0, "", "")
	}
	data.MjestaNa = h.svc().MjestaNaTerenu(r.Context(), "", sk.Sektor)
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
	u := noviUpisaniBrojevi(r)
	z := service.Zahvat{Vrsta: f("vrsta"), SkladisteID: id, VrstaID: f("sredstvo"), Oblik: f("oblik"), UOblik: f("u_oblik"),
		NaSkladisteID: f("na_skladiste"), SectionCode: f("dionica"), StructureID: f("objekt"), Mjesto: f("mjesto"), JournalID: f("obrana"),
		Nalozio: f("nalozio"), Preuzeo: f("preuzeo"), Dokument: f("dokument"), Napomena: f("napomena")}
	z.AreaID, _ = strconv.Atoi(f("podrucje"))
	if t, err := time.ParseInLocation("2006-01-02", f("datum"), models.Zagreb); err == nil {
		z.Datum = t
	}
	k, ok := parseBroj(u.tekst("kolicina"))
	if !ok {
		u.Neispravno["kolicina"] = true
		h.ponoviObrazacZahvata(w, r, data, z, u, "Količina nije broj; upišite je brojkama, npr. 4 200.")
		return
	}
	z.Kolicina = k
	redci, err := h.svc().Provedi(r.Context(), data.CurrentUser, data.Permissions, z)
	if err != nil {
		h.ponoviObrazacZahvata(w, r, data, z, u, err.Error())
		return
	}
	poruka := models.PrometNaziv(z.Vrsta) + ": " + kolicinaHR(z.Kolicina)
	if len(redci) > 0 && redci[0].Jedinica != "" {
		poruka += " " + redci[0].Jedinica
	}
	veza := ""
	if len(redci) > 0 {
		veza = redci[0].VezaID
	}
	if r.FormValue("jos") == "1" {
		redirectWith(w, r, "/sredstva/skladista/"+id+"/promet/novo?vrsta="+z.Vrsta+"&potvrda="+veza, "success", poruka+" je upisano; sljedeći zahvat.")
		return
	}
	redirectWith(w, r, "/sredstva/skladista/"+id+"?potvrda="+veza, "success", poruka+" je upisano.")
}

// ponoviObrazacZahvata vraća obrazac zahvata s upisanim i porukom; tko u
// skladište ne smije pisati, vraća se na prazan obrazac s porukom
func (h *MtsHandler) ponoviObrazacZahvata(w http.ResponseWriter, r *http.Request, data MtsPageData, z service.Zahvat, u *upisaniBrojevi, poruka string) {
	sk, ok := h.ucitajSkladiste(w, r, &data)
	if !ok {
		return
	}
	if !data.SmijePisati {
		redirectWith(w, r, "/sredstva/skladista/"+sk.ID+"/promet/novo?vrsta="+z.Vrsta+"&sredstvo="+z.VrstaID, "error", poruka)
		return
	}
	h.popuniObrazacPrometa(r, &data, sk)
	data.Zahvat = z
	data.vratiUpisano(u, poruka)
	h.render(w, "promet_form.html", data)
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
	data.Kartica = "promet"
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
	data.Kartica = "teren"
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
	for _, sk := range data.Skladista {
		if h.svc().SmijePisati(data.Permissions, &sk) {
			data.ZaInventuru = append(data.ZaInventuru, sk)
		}
	}
	data.Kartica = "inventura"
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
		http.Error(w, "Inventuru vodi uprava branjenog područja ili sektora", http.StatusForbidden)
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
	data.SmijeOtkljucati = h.svc().SmijeOtkljucati(data.Permissions)
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
		http.Error(w, "Zaključena inventura se ne mijenja", http.StatusForbidden)
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
	u := noviUpisaniBrojevi(r)
	p := &models.Popis{ID: r.PathValue("id"), SkladisteID: f("skladiste"), Napomena: u.tekst("napomena")}
	if t, err := time.ParseInLocation("2006-01-02", f("dan"), models.Zagreb); err == nil {
		p.Dan = t
	}
	vrste, _ := h.svc().SveVrste(r.Context()) // bez kataloga obrazac nema redaka, pa servis javi praznu inventuru
	for _, v := range vrste {
		for _, o := range v.SviOblici() {
			kljuc := v.ID + ":" + o
			kn, _ := parseBroj(f("k:" + kljuc)) // knjižno je skriveno polje koje je program sam upisao
			st := models.PopisnaStavka{VrstaID: v.ID, Oblik: o, Utvrdjeno: u.broj("u:"+kljuc, redakVrste(v, o)), Knjizno: kn, Napomena: u.tekst("n:" + kljuc)}
			p.Stavke = append(p.Stavke, st)
		}
	}
	if poruka := u.Poruka("Prebrojano"); poruka != "" {
		h.ponoviObrazacPopisa(w, r, data, p, u, poruka)
		return
	}
	if err := h.svc().SpremiPopis(r.Context(), data.CurrentUser, data.Permissions, p); err != nil {
		h.ponoviObrazacPopisa(w, r, data, p, u, err.Error())
		return
	}
	if r.FormValue("zakljuci") == "1" {
		if err := h.svc().ZakljuciPopis(r.Context(), data.CurrentUser, data.Permissions, p.ID); err != nil {
			redirectWith(w, r, "/sredstva/popisi/"+p.ID, "error", "Inventura je spremljena, ali nije zaključena: "+err.Error())
			return
		}
		redirectWith(w, r, "/sredstva/popisi/"+p.ID, "success", "Inventura je zaključena; razlike su proknjižene.")
		return
	}
	redirectWith(w, r, "/sredstva/popisi/"+p.ID, "success", "Inventura je spremljena kao nacrt.")
}

// ponoviObrazacPopisa vraća obrazac inventure s upisanim vrijednostima i
// porukom. Inventura bez skladišta ili dana nema što pokazati, pa se vraća
// na obrazac kao prije, samo s porukom.
func (h *MtsHandler) ponoviObrazacPopisa(w http.ResponseWriter, r *http.Request, data MtsPageData, p *models.Popis, u *upisaniBrojevi, poruka string) {
	natrag := "/sredstva/skladista/" + p.SkladisteID + "/popis?dan=" + strings.TrimSpace(r.FormValue("dan"))
	if p.ID != "" {
		natrag = "/sredstva/popisi/" + p.ID + "/uredi"
	}
	sk, err := h.svc().Skladiste(r.Context(), p.SkladisteID)
	if err != nil || sk == nil || p.Dan.IsZero() {
		redirectWith(w, r, natrag, "error", poruka)
		return
	}
	prikaz, err := h.svc().PredlozakPopisa(r.Context(), sk.ID, p.Dan)
	if err != nil {
		redirectWith(w, r, natrag, "error", poruka)
		return
	}
	data.Skladiste, data.Sektor, data.Popis = sk, sk.Sektor, prikaz
	data.SmijePisati = h.svc().SmijePisati(data.Permissions, sk)
	data.IsEdit = prikaz.ID != ""
	data.vratiUpisano(u, poruka)
	h.render(w, "popis_form.html", data)
}

// HandleZakljuciPopis proknjižava razlike
func (h *MtsHandler) HandleZakljuciPopis(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	id := r.PathValue("id")
	if err := h.svc().ZakljuciPopis(r.Context(), data.CurrentUser, data.Permissions, id); err != nil {
		redirectWith(w, r, "/sredstva/popisi/"+id, "error", err.Error())
		return
	}
	redirectWith(w, r, "/sredstva/popisi/"+id, "success", "Inventura je zaključena; razlike su proknjižene.")
}

// HandleOtkljucajPopis vraća zaključenu inventuru u nacrt; smije samo
// globalni administrator
func (h *MtsHandler) HandleOtkljucajPopis(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if !h.svc().SmijeOtkljucati(data.Permissions) {
		http.Error(w, "Inventuru otključava samo globalni administrator.", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	if err := h.svc().OtkljucajPopis(r.Context(), data.CurrentUser, data.Permissions, id); err != nil {
		redirectWith(w, r, "/sredstva/popisi/"+id, "error", err.Error())
		return
	}
	redirectWith(w, r, "/sredstva/popisi/"+id, "success", "Inventura je vraćena u nacrt. Upišite prebrojano i zaključite je ponovno.")
}

// zadaniOblik: na teren idu napunjene vreće, u skladište stižu prazne
func zadaniOblik(vrstaPrometa string) string {
	switch vrstaPrometa {
	case models.PrometIzdano, models.PrometPovrat, models.PrometUtrosak:
		return models.OblikPunjeno
	}
	return models.OblikPrazno
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

// ---- katalog vrsta

// smijeKatalog: katalog je podatak organizacije, uređuje ga uprava sektora ili administrator
func smijeKatalog(perms *models.UserPermissions) bool {
	return perms != nil && (perms.IsGlobalAdmin || len(perms.AdminSectors) > 0)
}

// ShowKatalog prikazuje sve vrste sredstava, i ugašene, s obrascem za novu
func (h *MtsHandler) ShowKatalog(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	var err error
	if data.Vrste, err = h.svc().SveVrste(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.SmijeUrediti = smijeKatalog(data.Permissions)
	if id := r.URL.Query().Get("uredi"); id != "" && data.SmijeUrediti {
		data.Vrsta, _ = h.svc().Vrsta(r.Context(), id)
	}
	data.Kartica = "katalog"
	h.render(w, "katalog.html", data)
}

// HandleSaveVrsta upisuje novu vrstu ili mijenja postojeću
func (h *MtsHandler) HandleSaveVrsta(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if !smijeKatalog(data.Permissions) {
		http.Error(w, "Katalog sredstava uređuje uprava sektora ili administrator", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/sredstva/katalog", "error", "Neispravan zahtjev")
		return
	}
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	v := &models.VrstaSredstva{ID: r.PathValue("id"), Grupa: f("grupa"), Naziv: f("naziv"), Jedinica: f("jedinica"),
		Napomena: f("napomena"), Aktivna: r.FormValue("aktivna") != "0"}
	v.Redoslijed, _ = strconv.Atoi(f("redoslijed"))
	if r.FormValue("oblici") == "vrece" {
		v.Oblici = []string{models.OblikPrazno, models.OblikPunjeno}
	}
	if v.ID != "" {
		cur, err := h.svc().Vrsta(r.Context(), v.ID)
		if err != nil || cur == nil {
			http.NotFound(w, r)
			return
		}
		if v.Redoslijed == 0 {
			v.Redoslijed = cur.Redoslijed
		}
	} else if v.Redoslijed == 0 {
		// nova vrsta ide na kraj svoje skupine
		sve, _ := h.svc().SveVrste(r.Context())
		for _, x := range sve {
			if x.Grupa == v.Grupa && x.Redoslijed >= v.Redoslijed {
				v.Redoslijed = x.Redoslijed + 1
			}
		}
	}
	if err := h.svc().SpremiVrstu(r.Context(), v); err != nil {
		redirectWith(w, r, "/sredstva/katalog", "error", err.Error())
		return
	}
	redirectWith(w, r, "/sredstva/katalog", "success", "Vrsta „"+v.Naziv+"“ je upisana.")
}

// HandleObrisiVrstu miče nekorištenu vrstu iz kataloga
func (h *MtsHandler) HandleObrisiVrstu(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if !smijeKatalog(data.Permissions) {
		http.Error(w, "Katalog sredstava uređuje uprava sektora ili administrator", http.StatusForbidden)
		return
	}
	if err := h.svc().ObrisiVrstu(r.Context(), r.PathValue("id")); err != nil {
		redirectWith(w, r, "/sredstva/katalog", "error", err.Error())
		return
	}
	redirectWith(w, r, "/sredstva/katalog", "success", "Vrsta je uklonjena iz kataloga.")
}

// ---- potrebe za nabavom

// ShowPotrebe prikazuje obrazac potreba skladišta za godinu, uz stanje
func (h *MtsHandler) ShowPotrebe(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	sk, ok := h.ucitajSkladiste(w, r, &data)
	if !ok {
		return
	}
	data.Godina, _ = strconv.Atoi(r.URL.Query().Get("godina"))
	if err := h.prikaziPotrebe(w, r, data, sk); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// prikaziPotrebe slaže obrazac potreba skladišta za godinu iz podataka
func (h *MtsHandler) prikaziPotrebe(w http.ResponseWriter, r *http.Request, data MtsPageData, sk *models.Skladiste) error {
	if data.Godina == 0 {
		data.Godina = models.GodinaPotreba(time.Now().In(models.Zagreb))
	}
	var gStanje, gPotrebe error
	data.Stanje, gStanje = h.svc().StanjeSkladista(r.Context(), sk.ID, nil)
	data.Potrebe, gPotrebe = h.svc().PotrebeSkladista(r.Context(), sk.ID, data.Godina)
	if err := errors.Join(gStanje, gPotrebe); err != nil {
		return err
	}
	data.GodinePotreba = h.svc().GodinePotreba(r.Context(), sk.Sektor)
	h.render(w, "potrebe_form.html", data)
	return nil
}

// HandleSavePotrebe upisuje potrebe skladišta za godinu; polja su pt:<vrsta> i pn:<vrsta>
func (h *MtsHandler) HandleSavePotrebe(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/sredstva", "error", "Neispravan zahtjev")
		return
	}
	id := r.PathValue("id")
	godina, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("godina"))) // kriva godina je 0, a servis je odbije porukom
	u := noviUpisaniBrojevi(r)
	vrste, _ := h.svc().Vrste(r.Context()) // bez kataloga nema ni redaka potreba
	var potrebe []models.Potreba
	for _, v := range vrste {
		potrebe = append(potrebe, models.Potreba{VrstaID: v.ID, Kolicina: u.broj("pt:"+v.ID, v.Naziv), Napomena: u.tekst("pn:" + v.ID)})
	}
	poruka := u.Poruka("Potrebno")
	if poruka == "" {
		if err := h.svc().SpremiPotrebe(r.Context(), data.CurrentUser, data.Permissions, id, godina, potrebe); err != nil {
			poruka = err.Error()
		}
	}
	if poruka != "" {
		sk, ok := h.ucitajSkladiste(w, r, &data)
		if !ok {
			return
		}
		data.Godina = godina
		data.vratiUpisano(u, poruka)
		if err := h.prikaziPotrebe(w, r, data, sk); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	redirectWith(w, r, "/sredstva/skladista/"+id+"/potrebe?godina="+strconv.Itoa(godina), "success", "Potrebe za nabavom u "+strconv.Itoa(godina)+". su upisane.")
}

// ShowPotrebeSektora prikazuje potrebe sektora za godinu: vrsta × skladište
func (h *MtsHandler) ShowPotrebeSektora(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	data.Godina, _ = strconv.Atoi(r.URL.Query().Get("godina"))
	if data.Godina == 0 {
		data.Godina = models.GodinaPotreba(time.Now().In(models.Zagreb))
	}
	var err error
	if data.PotrebeSektora, err = h.svc().PotrebeSektora(r.Context(), data.Sektor, data.Godina); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.Skladista, _ = h.svc().Skladista(r.Context(), data.Sektor, 0, false)
	data.Vrste, _ = h.svc().Vrste(r.Context())
	data.GodinePotreba = h.svc().GodinePotreba(r.Context(), data.Sektor)
	data.Kartica = "potrebe"
	h.render(w, "potrebe.html", data)
}

// PotrebaU vraća potrebu vrste u skladištu iz popisa sektora, za tablicu
func (d MtsPageData) PotrebaU(skladisteID, vrstaID string) float64 {
	for _, p := range d.PotrebeSektora {
		if p.SkladisteID == skladisteID && p.VrstaID == vrstaID {
			return p.Kolicina
		}
	}
	return 0
}

// UkupnoPotreba zbraja potrebu vrste kroz skladišta
func (d MtsPageData) UkupnoPotreba(vrstaID string) float64 {
	var s float64
	for _, p := range d.PotrebeSektora {
		if p.VrstaID == vrstaID {
			s += p.Kolicina
		}
	}
	return s
}

// Potreba vraća potrebu vrste u obrascu skladišta
func (d MtsPageData) Potreba(vrstaID string) models.Potreba { return d.Potrebe[vrstaID] }
