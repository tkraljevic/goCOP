package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/poslovi"
	"gocop/internal/repository"
	"gocop/internal/service"
	"gocop/internal/weather"
)

// Vodočuvarski dnevnik: dnevni listovi, kao papirna knjiga

type VodocuvarHandler struct {
	svc                 func() *service.VodocuvarService
	users               *service.UserService
	org                 func() *repository.OrgRepository // registar organizacije, spojen poslije pokretanja
	geokoder            *weather.Geokoder
	poslovi             *poslovi.Registar
	tmplPopis, tmplList *template.Template
	tmplKalendar        *template.Template
	tmplPosao           *template.Template
	potpisSlika         func(ctx context.Context, userID string) *models.PotpisSlika // sken potpisa, za ispis
}

// SetPotpisSlika daje rukovatelju izvor skeniranih potpisa za ispis listova
func (h *VodocuvarHandler) SetPotpisSlika(f func(ctx context.Context, userID string) *models.PotpisSlika) {
	h.potpisSlika = f
}

// otisci skuplja skenirane potpise svih koji su listove potpisali
func (h *VodocuvarHandler) otisci(ctx context.Context, listovi ...*models.VodocuvarskiList) models.OtisciLista {
	o := models.OtisciLista{}
	if h.potpisSlika == nil {
		return o
	}
	for _, l := range listovi {
		for _, id := range []string{l.UserID, l.PotvrdioID} {
			if id == "" {
				continue
			}
			if _, ima := o[id]; !ima {
				o[id] = h.potpisSlika(ctx, id)
			}
		}
	}
	return o
}

func NewVodocuvarHandler(svc func() *service.VodocuvarService, users *service.UserService, org func() *repository.OrgRepository, popis, list *template.Template) *VodocuvarHandler {
	if org == nil {
		org = func() *repository.OrgRepository { return nil }
	}
	return &VodocuvarHandler{svc: svc, users: users, org: org, geokoder: &weather.Geokoder{}, tmplPopis: popis, tmplList: list}
}

// SetPoslovi daje rukovatelju registar poslova i predložak stranice s trakom napretka
func (h *VodocuvarHandler) SetPoslovi(p *poslovi.Registar, t *template.Template) {
	h.poslovi, h.tmplPosao = p, t
}

// SetGeokoder daje rukovatelju drugi geokoder (za testove)
func (h *VodocuvarHandler) SetGeokoder(g *weather.Geokoder) { h.geokoder = g }

// KnjigaVodocuvara su listovi jednog vodočuvara, jer svaki ima svoju knjigu
type KnjigaVodocuvara struct {
	UserID  string
	Ime     string
	AreaID  int
	Listovi []models.VodocuvarskiList
	Cekaju  int              // predani, a neovjereni
	Zadaci  []models.Zadatak // otvoreni zadaci koji čekaju vodočuvara
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

// pristup zatvara dnevnike onima koji u njih ne gledaju: strojar, rukovatelj,
// terenski radnik i skladištar dobiju obavijest, ne popis
func (h *VodocuvarHandler) pristup(w http.ResponseWriter, u *models.User) bool {
	if u == nil || u.VidiVodocuvarskiDnevnik() {
		return true
	}
	http.Error(w, "Vodočuvarski dnevnik vode vodočuvari, a čitaju ga rukovoditelji i ovlaštenici područja; strojari, rukovatelji i skladištari imaju svoje dnevnike.", http.StatusForbidden)
	return false
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
	if !h.pristup(w, u) {
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
	// vodočuvari kojima se zadaje: samo iz odabranog sektora i područja;
	// svaki ima svoju knjigu i kad je još prazna
	for _, v := range s.Vodocuvari(r.Context(), perms) {
		pd := v.PrimaryDuty()
		if d.Filtar.Sektor != "" && (pd == nil || pd.SectorID == nil || *pd.SectorID != d.Filtar.Sektor) {
			continue
		}
		if d.Filtar.AreaID > 0 && (pd == nil || pd.AreaID == nil || *pd.AreaID != d.Filtar.AreaID) {
			continue
		}
		d.Vodocuvari = append(d.Vodocuvari, v)
		if v.ID.String() == u.ID.String() || poOsobi[v.ID.String()] != nil {
			continue
		}
		k := KnjigaVodocuvara{UserID: v.ID.String(), Ime: v.FullName}
		if pd != nil && pd.AreaID != nil {
			k.AreaID = *pd.AreaID
		}
		poOsobi[k.UserID] = &k
		d.Knjige = append(d.Knjige, k)
	}
	for i := range d.Knjige {
		for _, z := range s.Zadaci(r.Context(), d.Knjige[i].UserID) {
			if z.Otvoren() {
				d.Knjige[i].Zadaci = append(d.Knjige[i].Zadaci, z)
			}
		}
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
	if !h.pristup(w, u) {
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
	if org := h.org(); org != nil && l.AreaID > 0 {
		d.Podrucje, _ = org.GetArea(r.Context(), l.AreaID)
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

// HandleUpis upisuje bilješku rukovoditelja u dnevnik vodočuvara
func (h *VodocuvarHandler) HandleUpis(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	natrag := r.FormValue("natrag")
	if natrag == "" {
		natrag = "/vodocuvar"
	}
	l, err := s.Upisi(r.Context(), perms, u, r.FormValue("vodocuvar"), danIzObrasca(r.FormValue("datum")), r.FormValue("tekst"))
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, "/vodocuvar/"+l.ID, "success", "Upis je u dnevniku, na listu "+strconv.Itoa(l.Broj)+" od "+l.Datum.Format("02.01.2006.")+", s vašim imenom i vremenom.")
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
	var za time.Time
	if v := r.FormValue("za"); v != "" {
		za, _ = time.ParseInLocation("2006-01-02", v, models.Zagreb)
	}
	z, err := s.ZadajZadatak(r.Context(), perms, u, r.FormValue("vodocuvar"), r.FormValue("tekst"), za)
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	kad := "na sljedećem listu"
	if !z.Za.IsZero() {
		kad = "na listu za " + z.Za.Format("02.01.2006.")
	}
	redirectWith(w, r, natrag, "success", "Zadatak je zadan; pojavit će se "+kad+" pod naredbama, s vašim imenom, dok ga vodočuvar ne obavi ("+z.Tekst+").")
}

// IzvoziPDF daje list u obliku papirnate stranice
func (h *VodocuvarHandler) IzvoziPDF(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	if !h.pristup(w, u) {
		return
	}
	l, err := s.Get(r.Context(), perms, r.PathValue("id"))
	if err != nil || l == nil {
		http.NotFound(w, r)
		return
	}
	var area *models.Area
	if org := h.org(); org != nil && l.AreaID > 0 {
		area, _ = org.GetArea(r.Context(), l.AreaID)
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="dnevni-list-`+l.Datum.In(models.Zagreb).Format("2006-01-02")+`.pdf"`)
	_, _ = w.Write(PDFVodocuvarskiList(l, models.Terms(), area, h.otisci(r.Context(), l)))
}

// IzvoziKnjigu daje cijelu godišnju knjigu vodočuvara kao PDF
func (h *VodocuvarHandler) IzvoziKnjigu(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	if !h.pristup(w, u) {
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
		if org := h.org(); org != nil && listovi[0].AreaID > 0 {
			area, _ = org.GetArea(r.Context(), listovi[0].AreaID)
		}
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="vodocuvarski-dnevnik-`+strconv.Itoa(godina)+`-`+sigurnoIme(ime)+`.pdf"`)
	pok := make([]*models.VodocuvarskiList, len(listovi))
	for i := range listovi {
		pok[i] = &listovi[i]
	}
	_, _ = w.Write(PDFVodocuvarskaKnjiga(listovi, ime, godina, models.Terms(), area, h.otisci(r.Context(), pok...)))
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

// PosaoData je stranica s trakom napretka dugog posla
type PosaoData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage, ErrorMessage string
	PosaoID, PosaoNaziv, Natrag  string
}

// HandleKoordinatePodrucja nalazi koordinate svim područjima bez njih, po
// mjestu ispostave ili podcentra: posao u pozadini s trakom napretka, jer
// OpenStreetMap dopušta jedan upit u sekundi
func (h *VodocuvarHandler) HandleKoordinatePodrucja(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		http.Redirect(w, r, "/organizacija", http.StatusSeeOther)
		return
	}
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	u, perms, _ := h.base(r)
	org := h.org()
	if org == nil || h.poslovi == nil || h.tmplPosao == nil {
		redirectWith(w, r, "/organizacija", "error", "Traženje koordinata nije spremno")
		return
	}
	areas, err := org.ListAreas(r.Context(), "")
	if err != nil {
		redirectWith(w, r, "/organizacija", "error", err.Error())
		return
	}
	var bez []models.Area
	for _, a := range areas {
		if !a.ImaKoordinate() {
			bez = append(bez, a)
		}
	}
	if len(bez) == 0 {
		redirectWith(w, r, "/organizacija", "success", "Sva područja već imaju koordinate.")
		return
	}
	geo := h.geokoder
	p := h.poslovi.Pokreni("Koordinate branjenih područja", u.ID.String(), "/organizacija", func(zad *poslovi.Posao) error {
		n, greske := 0, []string{}
		for i, a := range bez {
			mjesto := weather.MjestoIzNaziva(a.VgiName)
			if mjesto == "" {
				mjesto = weather.MjestoIzNaziva(a.Subcenter)
			}
			zad.Korak(fmt.Sprintf("BP %d: %s", a.ID, mjesto), i, len(bez))
			if mjesto == "" {
				greske = append(greske, fmt.Sprintf("%d (nema mjesta)", a.ID))
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			t, err := geo.Nadji(ctx, mjesto)
			cancel()
			if err != nil {
				greske = append(greske, fmt.Sprintf("%d (%s)", a.ID, mjesto))
				fmt.Fprintf(zad, "BP %d, %s: %v\n", a.ID, mjesto, err)
			} else {
				a.Latitude, a.Longitude = t.Lat, t.Lon
				if err := org.SaveArea(context.Background(), &a); err != nil {
					greske = append(greske, fmt.Sprint(a.ID))
				} else {
					n++
					fmt.Fprintf(zad, "BP %d, %s: %.5f, %.5f (%s)\n", a.ID, mjesto, t.Lat, t.Lon, t.Naziv)
				}
			}
			if i < len(bez)-1 {
				time.Sleep(1100 * time.Millisecond) // OpenStreetMap traži najviše jedan upit u sekundi
			}
		}
		poruka := fmt.Sprintf("Koordinate su nađene za %d područja po mjestu ispostave; provjerite ih na obrascu područja.", n)
		if len(greske) > 0 {
			zad.Odrediste = "/organizacija?" + url.Values{"error": {poruka + " Nije nađeno za: " + strings.Join(greske, ", ")}}.Encode()
		} else {
			zad.Odrediste = "/organizacija?" + url.Values{"success": {poruka}}.Encode()
		}
		zad.Zavrsi(poruka, nil)
		return nil
	})
	d := PosaoData{CurrentUser: u, Permissions: perms, ActiveNav: "registers", ViewAsBanner: viewBanner(r), PosaoID: p.ID, PosaoNaziv: p.Naziv, Natrag: "/organizacija"}
	if err := h.tmplPosao.ExecuteTemplate(w, "posao.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// KalendarData je kalendarski pregled zadataka jednog vodočuvara
type KalendarData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage string
	ErrorMessage   string

	Vodocuvar   *models.User
	Vodocuvari  []models.User
	Mjesec      time.Time
	Prethodni   string
	Sljedeci    string
	Tjedni      [][]DanKalendara
	Danas       string
	Najdalje    string // zadnji dan do kojeg se smije planirati
	SmijeZadati bool
	Otvorenih   int
}

// DanKalendara je jedno polje kalendara
type DanKalendara struct {
	Datum     time.Time
	Kljuc     string
	UMjesecu  bool
	Danas     bool
	Proslost  bool
	Predaleko bool
	Zadaci    []models.Zadatak
}

// SetKalendar daje rukovatelju predložak kalendara
func (h *VodocuvarHandler) SetKalendar(t *template.Template) { h.tmplKalendar = t }

// ShowKalendar prikazuje zadatke vodočuvara po danima mjeseca
func (h *VodocuvarHandler) ShowKalendar(w http.ResponseWriter, r *http.Request) {
	u, perms, base := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	if !h.pristup(w, u) {
		return
	}
	q := r.URL.Query()
	d := KalendarData{CurrentUser: u, Permissions: perms, ActiveNav: "journals", ViewAsBanner: viewBanner(r), SuccessMessage: base.SuccessMessage, ErrorMessage: base.ErrorMessage}
	n := time.Now().In(models.Zagreb)
	danas := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb)
	d.Danas = danas.Format("2006-01-02")
	d.Najdalje = danas.AddDate(0, 0, models.NajdaljePlaniranje).Format("2006-01-02")
	d.Mjesec = time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, models.Zagreb)
	if m, err := time.ParseInLocation("2006-01", q.Get("mjesec"), models.Zagreb); err == nil {
		d.Mjesec = m
	}
	d.Prethodni = d.Mjesec.AddDate(0, -1, 0).Format("2006-01")
	d.Sljedeci = d.Mjesec.AddDate(0, 1, 0).Format("2006-01")
	d.Vodocuvari = s.Vodocuvari(r.Context(), perms)
	id := q.Get("vodocuvar")
	if id == "" {
		if service.VodiDnevnik(u) {
			id = u.ID.String()
		} else if len(d.Vodocuvari) > 0 {
			id = d.Vodocuvari[0].ID.String()
		}
	}
	if id != "" {
		if vid, err := uuid.Parse(id); err == nil {
			d.Vodocuvar, _ = h.users.GetUserByID(vid)
		}
	}
	zadaci := map[string][]models.Zadatak{}
	if d.Vodocuvar != nil {
		var err error
		zadaci, err = s.Kalendar(r.Context(), perms, d.Vodocuvar.ID.String(), d.Mjesec)
		if err != nil {
			d.ErrorMessage = err.Error()
		}
		for _, v := range d.Vodocuvari {
			if v.ID == d.Vodocuvar.ID {
				d.SmijeZadati = true
			}
		}
		for _, zs := range zadaci {
			for _, z := range zs {
				if z.Otvoren() {
					d.Otvorenih++
				}
			}
		}
	}
	// mreža: ponedjeljak do nedjelje
	prvi := d.Mjesec
	pomak := (int(prvi.Weekday()) + 6) % 7
	dan := prvi.AddDate(0, 0, -pomak)
	for t := 0; t < 6; t++ {
		var tjedan []DanKalendara
		for i := 0; i < 7; i++ {
			k := dan.Format("2006-01-02")
			tjedan = append(tjedan, DanKalendara{Datum: dan, Kljuc: k, UMjesecu: dan.Month() == d.Mjesec.Month(), Danas: k == d.Danas,
				Proslost: dan.Before(danas), Predaleko: k > d.Najdalje, Zadaci: zadaci[k]})
			dan = dan.AddDate(0, 0, 1)
		}
		d.Tjedni = append(d.Tjedni, tjedan)
		if dan.Month() != d.Mjesec.Month() && dan.After(d.Mjesec.AddDate(0, 1, -1)) {
			break
		}
	}
	if err := h.tmplKalendar.ExecuteTemplate(w, "vodocuvar_kalendar.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
