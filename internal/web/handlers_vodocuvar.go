package web

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	"gocop/internal/weather"
)

// Vodočuvarski dnevnik: dnevni listovi, kao papirna knjiga

type VodocuvarHandler struct {
	svc                 func() *service.VodocuvarService
	users               *service.UserService
	org                 *repository.OrgRepository
	geokoder            *weather.Geokoder
	tmplPopis, tmplList *template.Template
}

func NewVodocuvarHandler(svc func() *service.VodocuvarService, users *service.UserService, org *repository.OrgRepository, popis, list *template.Template) *VodocuvarHandler {
	return &VodocuvarHandler{svc: svc, users: users, org: org, geokoder: &weather.Geokoder{}, tmplPopis: popis, tmplList: list}
}

// SetGeokoder daje rukovatelju drugi geokoder (za testove)
func (h *VodocuvarHandler) SetGeokoder(g *weather.Geokoder) { h.geokoder = g }

// KnjigaVodocuvara su listovi jednog vodočuvara, jer svaki ima svoju knjigu
type KnjigaVodocuvara struct {
	UserID  string
	Ime     string
	AreaID  int
	Listovi []models.VodocuvarskiList
	Cekaju  int // predani, a neovjereni
}

// VodocuvarPageData je stranica popisa ili jednog lista
type VodocuvarPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage string
	ErrorMessage   string

	VodiDnevnik bool
	Godina      int
	Godine      []int
	Moji        []models.VodocuvarskiList
	Tudji       []models.VodocuvarskiList
	Knjige      []KnjigaVodocuvara // tuđi listovi po vodočuvaru
	Podrucja    []models.Area
	Sektori     []models.Sector
	Filtar      repository.FiltarListova
	Danas       string
	Arhivirana  bool // odabrana godina je zaključena

	Vodocuvari      []models.User    // kojima osoba smije zadavati zadatke
	ZadaciOsobe     []models.Zadatak // zadaci vodočuvara čiji je list otvoren
	List            *models.VodocuvarskiList
	Moj             bool
	SmijeOvjeriti   bool
	SmijeParafirati bool
	Parafirao       bool
	Podrucje        *models.Area
}

func (h *VodocuvarHandler) base(r *http.Request) (*models.User, *models.UserPermissions, VodocuvarPageData) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	q := r.URL.Query()
	d := VodocuvarPageData{CurrentUser: u, Permissions: perms, ActiveNav: "journals", ViewAsBanner: viewBanner(r),
		SuccessMessage: q.Get("success"), ErrorMessage: q.Get("error"), Danas: time.Now().In(models.Zagreb).Format("2006-01-02")}
	if u != nil {
		if cijeli, err := h.users.GetUserByID(u.ID); err == nil && cijeli != nil {
			d.VodiDnevnik = service.VodiDnevnik(cijeli)
			d.CurrentUser = cijeli
		}
	}
	d.Sektori, _ = h.users.ListSectors()
	return d.CurrentUser, perms, d
}

func (h *VodocuvarHandler) service(w http.ResponseWriter) *service.VodocuvarService {
	s := h.svc()
	if s == nil {
		http.Error(w, "vodočuvarski dnevnik nije spreman", http.StatusServiceUnavailable)
	}
	return s
}

// ShowPopis prikazuje moj dnevnik i listove vodočuvara koje smijem vidjeti
func (h *VodocuvarHandler) ShowPopis(w http.ResponseWriter, r *http.Request) {
	u, perms, d := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	q := r.URL.Query()
	d.Godina, _ = strconv.Atoi(q.Get("godina"))
	if d.Godina == 0 {
		d.Godina = time.Now().In(models.Zagreb).Year()
	}
	for g := time.Now().In(models.Zagreb).Year(); g >= 2024; g-- {
		d.Godine = append(d.Godine, g)
	}
	d.Arhivirana = service.Arhivirana(d.Godina)
	if d.VodiDnevnik {
		d.Moji, _ = s.Moji(r.Context(), u, d.Godina)
	}
	d.Filtar = repository.FiltarListova{Sektor: q.Get("sektor"), Godina: d.Godina, CekaPotvrdu: q.Get("ceka") == "1", Limit: 500}
	d.Filtar.AreaID, _ = strconv.Atoi(q.Get("podrucje"))
	if _, ima := q["sektor"]; !ima {
		// zadano: sektor osobe po glavnom zaduženju
		if pd := u.PrimaryDuty(); pd != nil && pd.SectorID != nil {
			d.Filtar.Sektor = *pd.SectorID
		}
	}
	if d.Filtar.Sektor != "" {
		d.Podrucja, _ = h.users.ListAreas(d.Filtar.Sektor)
	}
	d.Tudji, _ = s.Tudji(r.Context(), perms, d.Filtar)
	// po vodočuvaru: svaki ima svoju knjigu
	poOsobi := map[string]*KnjigaVodocuvara{}
	for _, l := range d.Tudji {
		k := poOsobi[l.UserID]
		if k == nil {
			k = &KnjigaVodocuvara{UserID: l.UserID, Ime: l.Ime, AreaID: l.AreaID}
			poOsobi[l.UserID] = k
			d.Knjige = append(d.Knjige, *k)
		}
	}
	for i := range d.Knjige {
		for _, l := range d.Tudji {
			if l.UserID == d.Knjige[i].UserID {
				d.Knjige[i].Listovi = append(d.Knjige[i].Listovi, l)
				if l.Predan() && !l.Potvrden() {
					d.Knjige[i].Cekaju++
				}
			}
		}
	}
	// vodočuvari kojima se zadaje: samo iz odabranog sektora i područja
	for _, v := range s.Vodocuvari(r.Context(), perms) {
		pd := v.PrimaryDuty()
		if d.Filtar.Sektor != "" && (pd == nil || pd.SectorID == nil || *pd.SectorID != d.Filtar.Sektor) {
			continue
		}
		if d.Filtar.AreaID > 0 && (pd == nil || pd.AreaID == nil || *pd.AreaID != d.Filtar.AreaID) {
			continue
		}
		d.Vodocuvari = append(d.Vodocuvari, v)
	}
	if err := h.tmplPopis.ExecuteTemplate(w, "vodocuvar.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func danIzObrasca(s string) time.Time {
	if t, err := time.ParseInLocation("2006-01-02", s, models.Zagreb); err == nil {
		return t
	}
	n := time.Now().In(models.Zagreb)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb)
}

// ShowDan prikazuje list za dan: postojeći ili novi, popunjen
func (h *VodocuvarHandler) ShowDan(w http.ResponseWriter, r *http.Request) {
	u, perms, d := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	l, err := s.Pripremi(r.Context(), u, danIzObrasca(r.URL.Query().Get("datum")))
	if err != nil {
		redirectWith(w, r, "/vodocuvar", "error", err.Error())
		return
	}
	h.prikazi(w, r, d, perms, l, true)
}

// ShowList prikazuje spremljeni list
func (h *VodocuvarHandler) ShowList(w http.ResponseWriter, r *http.Request) {
	u, perms, d := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	l, err := s.Get(r.Context(), perms, r.PathValue("id"))
	if err != nil || l == nil {
		http.NotFound(w, r)
		return
	}
	h.prikazi(w, r, d, perms, l, l.UserID == u.ID.String())
}

func (h *VodocuvarHandler) prikazi(w http.ResponseWriter, r *http.Request, d VodocuvarPageData, perms *models.UserPermissions, l *models.VodocuvarskiList, moj bool) {
	s := h.svc()
	d.List, d.Moj = l, moj
	d.SmijeOvjeriti = s.SmijeOvjeriti(perms, l) && l.Predan() && !l.Potvrden()
	d.SmijeParafirati = s.SmijeParafirati(perms, l) && l.Predan()
	d.Parafirao = perms != nil && l.Parafirao(perms.User.ID.String())
	if h.org != nil && l.AreaID > 0 {
		d.Podrucje, _ = h.org.GetArea(r.Context(), l.AreaID)
	}
	if s.SmijeParafirati(perms, l) || moj {
		d.ZadaciOsobe = s.Zadaci(r.Context(), l.UserID)
	}
	if err := h.tmplList.ExecuteTemplate(w, "vodocuvar_list.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleSpremi sprema ili predaje list za dan
func (h *VodocuvarHandler) HandleSpremi(w http.ResponseWriter, r *http.Request) {
	u, _, _ := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	dan := danIzObrasca(r.FormValue("datum"))
	unos := service.UnosLista{Od: r.FormValue("od"), Do: r.FormValue("do"), Prilike: r.FormValue("prilike"), Naredbe: r.FormValue("naredbe"), Opis: r.FormValue("opis"), Zapazanja: r.FormValue("zapazanja"),
		Zadaci: map[string]service.UnosZadatka{}}
	_ = r.ParseForm()
	for k, v := range r.Form {
		if strings.HasPrefix(k, "zadatak_status_") && len(v) > 0 {
			id := strings.TrimPrefix(k, "zadatak_status_")
			unos.Zadaci[id] = service.UnosZadatka{Status: v[0], Obavljeno: r.FormValue("zadatak_obavljeno_" + id)}
		}
	}
	l, err := s.Spremi(r.Context(), u, dan, unos, r.FormValue("radnja") == "predaj")
	if err != nil {
		redirectWith(w, r, "/vodocuvar/dan?datum="+dan.Format("2006-01-02"), "error", err.Error())
		return
	}
	if l.Predan() {
		redirectWith(w, r, "/vodocuvar/"+l.ID, "success", "List "+strconv.Itoa(l.Broj)+" je potpisan i predan; čeka ovjeru rukovoditelja.")
		return
	}
	redirectWith(w, r, "/vodocuvar/"+l.ID, "success", "List je spremljen; predajte ga kad je dan gotov.")
}

// HandleRadnja: ovjeri, parafiraj ili obriši
func (h *VodocuvarHandler) HandleRadnja(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	id := r.PathValue("id")
	natrag := "/vodocuvar/" + id
	var err error
	poruka := ""
	switch r.FormValue("radnja") {
	case "ovjeri":
		_, err = s.Ovjeri(r.Context(), perms, u, id)
		poruka = "List je ovjeren."
	case "parafiraj":
		_, err = s.Parafiraj(r.Context(), perms, u, id)
		poruka = "List je parafiran."
	case "obrisi":
		err = s.Obrisi(r.Context(), u, id)
		poruka = "List je obrisan."
		natrag = "/vodocuvar"
	default:
		err = service.ErrUnauthorized
	}
	if err != nil {
		redirectWith(w, r, "/vodocuvar/"+id, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", poruka)
}

// HandleZadatak zadaje zadatak vodočuvaru
func (h *VodocuvarHandler) HandleZadatak(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	natrag := r.FormValue("natrag")
	if natrag == "" {
		natrag = "/vodocuvar"
	}
	z, err := s.ZadajZadatak(r.Context(), perms, u, r.FormValue("vodocuvar"), r.FormValue("tekst"))
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Zadatak je zadan; pojavit će se na sljedećem listu vodočuvara pod naredbama, s vašim imenom, dok ga ne obavi ("+z.Tekst+").")
}

// IzvoziPDF daje list u obliku papirnate stranice
func (h *VodocuvarHandler) IzvoziPDF(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	l, err := s.Get(r.Context(), perms, r.PathValue("id"))
	if err != nil || l == nil {
		http.NotFound(w, r)
		return
	}
	var area *models.Area
	if h.org != nil && l.AreaID > 0 {
		area, _ = h.org.GetArea(r.Context(), l.AreaID)
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="dnevni-list-`+l.Datum.In(models.Zagreb).Format("2006-01-02")+`.pdf"`)
	_, _ = w.Write(PDFVodocuvarskiList(l, models.Terms(), area))
}

// IzvoziKnjigu daje cijelu godišnju knjigu vodočuvara kao PDF
func (h *VodocuvarHandler) IzvoziKnjigu(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	q := r.URL.Query()
	vodocuvar := q.Get("vodocuvar")
	if vodocuvar == "" {
		vodocuvar = u.ID.String()
	}
	godina, _ := strconv.Atoi(q.Get("godina"))
	if godina == 0 {
		godina = time.Now().In(models.Zagreb).Year()
	}
	listovi, err := s.Knjiga(r.Context(), perms, vodocuvar, godina)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	ime := u.FullName
	var area *models.Area
	if len(listovi) > 0 {
		ime = listovi[0].Ime
		if h.org != nil && listovi[0].AreaID > 0 {
			area, _ = h.org.GetArea(r.Context(), listovi[0].AreaID)
		}
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="vodocuvarski-dnevnik-`+strconv.Itoa(godina)+`-`+sigurnoIme(ime)+`.pdf"`)
	_, _ = w.Write(PDFVodocuvarskaKnjiga(listovi, ime, godina, models.Terms(), area))
}

// GeokodJSON nalazi koordinate za adresu ili mjesto (obrazac područja)
func (h *VodocuvarHandler) GeokodJSON(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	t, err := h.geokoder.Nadji(r.Context(), r.URL.Query().Get("q"))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"greska": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"lat": t.Lat, "lon": t.Lon, "naziv": t.Naziv})
}

// HandleKoordinatePodrucja nalazi koordinate svim područjima bez njih, po
// mjestu ispostave ili podcentra
func (h *VodocuvarHandler) HandleKoordinatePodrucja(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	areas, err := h.org.ListAreas(r.Context(), "")
	if err != nil {
		redirectWith(w, r, "/organizacija", "error", err.Error())
		return
	}
	n, greske := 0, []string{}
	for i := range areas {
		a := areas[i]
		if a.ImaKoordinate() {
			continue
		}
		mjesto := weather.MjestoIzNaziva(a.VgiName)
		if mjesto == "" {
			mjesto = weather.MjestoIzNaziva(a.Subcenter)
		}
		if mjesto == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		t, err := h.geokoder.Nadji(ctx, mjesto)
		cancel()
		if err != nil {
			greske = append(greske, strconv.Itoa(a.ID)+" ("+mjesto+")")
			continue
		}
		a.Latitude, a.Longitude = t.Lat, t.Lon
		if err := h.org.SaveArea(r.Context(), &a); err != nil {
			greske = append(greske, strconv.Itoa(a.ID))
			continue
		}
		n++
		time.Sleep(1100 * time.Millisecond) // OpenStreetMap traži najviše jedan upit u sekundi
	}
	poruka := "Koordinate su nađene za " + strconv.Itoa(n) + " područja po mjestu ispostave; provjerite ih na obrascu područja."
	if len(greske) > 0 {
		redirectWith(w, r, "/organizacija", "error", poruka+" Nije nađeno za: "+strings.Join(greske, ", "))
		return
	}
	redirectWith(w, r, "/organizacija", "success", poruka)
}
