package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Registar kvazi-kišomjera: točke na kojima se za prognozu čitaju oborina i
// snijeg, po slivovima i visinskim pojasima. Stranica je popis s kartom
// na kojoj se točke premještaju kao točke toka na karti vodotoka; obrazac
// je puna stranica kao i kod ostalih registara.

type SlivoviHandler struct {
	svc          func() *service.KisomjerService
	watercourses *service.WatercourseService
	stations     *service.StationService
	tmpl         func(string) *template.Template
	karta        func() KartaPostavke
}

func NewSlivoviHandler(svc func() *service.KisomjerService, watercourses *service.WatercourseService,
	stations *service.StationService, tmpl func(string) *template.Template, karta func() KartaPostavke) *SlivoviHandler {
	return &SlivoviHandler{svc: svc, watercourses: watercourses, stations: stations, tmpl: tmpl, karta: karta}
}

// SkupinaKisomjera su točke jednog sliva
type SkupinaKisomjera struct {
	Sliv   models.Sliv
	Tocke  []models.Kisomjer
	Tezina float64 // zbroj težina aktivnih točaka; oko 1 kad je sliv pokriven
}

type SlivoviPageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	ActiveNav      string
	SuccessMessage string
	ErrorMessage   string
	ViewAsBanner

	Skupine  []SkupinaKisomjera
	Ukupno   int
	Aktivnih int
	Pojasi   []string

	Karta       KartaPostavke
	TockeJSON   template.JS
	RijekeJSON  template.JS
	SlivoviJSON template.JS
	LetveJSON   template.JS

	// obrazac
	Tocka   models.Kisomjer
	Slivovi []models.Sliv
	IsEdit  bool
}

// kisomjerNaKarti je točka kako je karta prima
type kisomjerNaKarti struct {
	Code    string   `json:"code"`
	Naziv   string   `json:"naziv"`
	Sliv    string   `json:"sliv"`
	Pojas   string   `json:"pojas"`
	Lat     float64  `json:"lat"`
	Lon     float64  `json:"lon"`
	Visina  *float64 `json:"visina,omitempty"`
	Km2     *float64 `json:"km2,omitempty"`
	Tezina  *float64 `json:"tezina,omitempty"`
	Aktivan bool     `json:"aktivan"`
	EditURL string   `json:"edit_url"`
}

func (h *SlivoviHandler) pageData(r *http.Request) SlivoviPageData {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	var kp KartaPostavke
	if h.karta != nil {
		kp = h.karta()
	}
	return SlivoviPageData{
		CurrentUser:    currUser,
		Permissions:    perms,
		ActiveNav:      "slivovi",
		SuccessMessage: r.URL.Query().Get("success"),
		ErrorMessage:   r.URL.Query().Get("error"),
		ViewAsBanner:   viewBanner(r),
		Pojasi:         models.KisomjerPojasi,
		Karta:          kp,
	}
}

// ShowSlivovi prikazuje registar: kartu i popis po slivovima
func (h *SlivoviHandler) ShowSlivovi(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.pageData(r)
	svc := h.svc()
	if svc == nil {
		http.Error(w, "Registar kišomjera nije uključen", http.StatusServiceUnavailable)
		return
	}

	tocke, err := svc.ListKisomjeri(ctx)
	if err != nil {
		data.ErrorMessage = err.Error()
	}
	slivovi, _ := svc.ListSlivovi(ctx)
	data.Slivovi = slivovi

	// skupine po slivu; točke bez poznatog sliva idu na kraj
	poOznaci := map[string]*SkupinaKisomjera{}
	for _, m := range slivovi {
		s := &SkupinaKisomjera{Sliv: m}
		poOznaci[m.Oznaka] = s
		data.Skupine = append(data.Skupine, *s)
	}
	var ostale SkupinaKisomjera
	for _, t := range tocke {
		data.Ukupno++
		if t.Aktivan {
			data.Aktivnih++
		}
		s, ima := poOznaci[t.Sliv]
		if !ima {
			ostale.Tocke = append(ostale.Tocke, t)
			continue
		}
		s.Tocke = append(s.Tocke, t)
		if t.Aktivan && t.Tezina != nil {
			s.Tezina += *t.Tezina
		}
	}
	for i := range data.Skupine {
		s := poOznaci[data.Skupine[i].Sliv.Oznaka]
		data.Skupine[i].Tocke, data.Skupine[i].Tezina = s.Tocke, s.Tezina
	}
	if len(ostale.Tocke) > 0 {
		ostale.Sliv = models.Sliv{Naziv: "Bez međusliva"}
		data.Skupine = append(data.Skupine, ostale)
	}

	var naKarti []kisomjerNaKarti
	for _, t := range tocke {
		if !t.ImaKoordinate() {
			continue
		}
		naKarti = append(naKarti, kisomjerNaKarti{Code: t.Code, Naziv: t.Naziv, Sliv: t.Sliv, Pojas: t.Pojas,
			Lat: t.Latitude, Lon: t.Longitude, Visina: t.Visina, Km2: t.Km2, Tezina: t.Tezina, Aktivan: t.Aktivan,
			EditURL: "/slivovi/kisomjer/" + t.Code + "/edit"})
	}
	if b, err := json.Marshal(naKarti); err == nil {
		data.TockeJSON = template.JS(b)
	}
	data.RijekeJSON = h.rijekeJSON(r)
	data.SlivoviJSON = slivoviJSON(slivovi)
	data.LetveJSON = h.letveJSON(r)

	if err := h.tmpl("slivovi.html").ExecuteTemplate(w, "slivovi.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// letveJSON slaže vodomjerne postaje s koordinatama za kartu
func (h *SlivoviHandler) letveJSON(r *http.Request) template.JS {
	if h.stations == nil {
		return ""
	}
	postaje, err := h.stations.ListStations(r.Context(), "", "", "", false)
	if err != nil {
		return ""
	}
	var stavke []WatercourseStationMapItem
	for _, s := range postaje {
		if !s.ImaKoordinate() {
			continue
		}
		id := s.ID.String()
		stavke = append(stavke, WatercourseStationMapItem{ID: id, Name: s.Name, Stationing: s.Stationing,
			Country: s.Zemlja(), Lat: s.Latitude, Lon: s.Longitude, DetailURL: "/stations/" + id})
	}
	if len(stavke) == 0 {
		return ""
	}
	b, err := json.Marshal(stavke)
	if err != nil {
		return ""
	}
	return template.JS(b)
}

// rijekeJSON slaže tokove svih vodotoka koji imaju geometriju u jednu zbirku
func (h *SlivoviHandler) rijekeJSON(r *http.Request) template.JS {
	if h.watercourses == nil {
		return ""
	}
	vode, err := h.watercourses.ListWatercourses(r.Context(), "", "", false)
	if err != nil {
		return ""
	}
	var features []json.RawMessage
	for _, v := range vode {
		if !v.HasGeometry() {
			continue
		}
		var fc struct {
			Features []json.RawMessage `json:"features"`
		}
		if json.Unmarshal([]byte(v.Geometry), &fc) == nil {
			features = append(features, fc.Features...)
		}
	}
	if len(features) == 0 {
		return ""
	}
	b, err := json.Marshal(struct {
		Type     string            `json:"type"`
		Features []json.RawMessage `json:"features"`
	}{"FeatureCollection", features})
	if err != nil {
		return ""
	}
	return template.JS(b)
}

// slivoviJSON slaže poligone slivova u zbirku značajki s oznakom i nazivom
func slivoviJSON(ms []models.Sliv) template.JS {
	type feature struct {
		Type       string          `json:"type"`
		Properties map[string]any  `json:"properties"`
		Geometry   json.RawMessage `json:"geometry"`
	}
	var features []feature
	for _, m := range ms {
		if !m.HasGeometry() {
			continue
		}
		var g json.RawMessage
		if json.Unmarshal([]byte(m.Geometry), &g) != nil {
			continue
		}
		// prihvati i cijelu značajku i golu geometriju
		var f struct {
			Type     string          `json:"type"`
			Geometry json.RawMessage `json:"geometry"`
		}
		if json.Unmarshal(g, &f) == nil && f.Type == "Feature" && len(f.Geometry) > 0 {
			g = f.Geometry
		}
		props := map[string]any{"oznaka": m.Oznaka, "naziv": m.Naziv}
		if m.Km2 != nil {
			props["km2"] = *m.Km2
		}
		features = append(features, feature{"Feature", props, g})
	}
	if len(features) == 0 {
		return ""
	}
	b, err := json.Marshal(struct {
		Type     string    `json:"type"`
		Features []feature `json:"features"`
	}{"FeatureCollection", features})
	if err != nil {
		return ""
	}
	return template.JS(b)
}

// ShowKisomjerForm prikazuje obrazac za novu točku ili izmjenu postojeće
func (h *SlivoviHandler) ShowKisomjerForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if data.Permissions == nil || !data.Permissions.IsGlobalAdmin {
		http.Error(w, "Registar kišomjera uređuje globalni administrator", http.StatusForbidden)
		return
	}
	svc := h.svc()
	if svc == nil {
		http.Error(w, "Registar kišomjera nije uključen", http.StatusServiceUnavailable)
		return
	}
	data.Slivovi, _ = svc.ListSlivovi(r.Context())
	data.Tocka.Aktivan = true
	if code := r.PathValue("code"); code != "" {
		t, err := svc.GetKisomjer(r.Context(), code)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if t == nil {
			http.NotFound(w, r)
			return
		}
		data.Tocka = *t
		data.IsEdit = true
	} else if m := r.URL.Query().Get("sliv"); m != "" {
		data.Tocka.Sliv = m
	}
	if err := h.tmpl("kisomjer_form.html").ExecuteTemplate(w, "kisomjer_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// kisomjerIzZahtjeva čita točku iz JSON tijela ili običnog obrasca
func kisomjerIzZahtjeva(r *http.Request) (*models.Kisomjer, error) {
	var k models.Kisomjer
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&k); err != nil {
			return nil, fmt.Errorf("neispravan JSON")
		}
		return &k, nil
	}
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	k.Code = r.FormValue("code")
	k.Naziv = r.FormValue("naziv")
	k.Sliv = r.FormValue("sliv")
	k.Pojas = r.FormValue("pojas")
	k.Napomena = r.FormValue("napomena")
	k.Aktivan = r.FormValue("aktivan") != "" && r.FormValue("aktivan") != "0"
	broj := func(ime string) (float64, bool, error) {
		v := strings.TrimSpace(strings.ReplaceAll(r.FormValue(ime), ",", "."))
		if v == "" {
			return 0, false, nil
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, false, fmt.Errorf("polje %s nije broj", ime)
		}
		return f, true, nil
	}
	var err error
	if k.Latitude, _, err = broj("lat"); err != nil {
		return nil, err
	}
	if k.Longitude, _, err = broj("lon"); err != nil {
		return nil, err
	}
	for ime, cilj := range map[string]**float64{"visina": &k.Visina, "srednja_visina": &k.SrednjaVisina, "km2": &k.Km2, "tezina": &k.Tezina} {
		v, ima, err := broj(ime)
		if err != nil {
			return nil, err
		}
		if ima {
			f := v
			*cilj = &f
		}
	}
	return &k, nil
}

func (h *SlivoviHandler) odgovori(w http.ResponseWriter, r *http.Request, err error, uspjeh, natrag string) {
	if err != nil {
		if wantsPage(r) {
			redirectWith(w, r, natrag, "error", err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if wantsPage(r) {
		redirectWith(w, r, "/slivovi", "success", uspjeh)
		return
	}
	writeJSON(w, map[string]any{"success": true, "message": uspjeh})
}

// HandleCreate upisuje novu točku
func (h *SlivoviHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	k, err := kisomjerIzZahtjeva(r)
	if err == nil {
		err = h.svc().CreateKisomjer(r.Context(), perms, k)
	}
	h.odgovori(w, r, err, "Kišomjer je upisan.", "/slivovi/kisomjer/new")
}

// HandleUpdate mijenja točku
func (h *SlivoviHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	k, err := kisomjerIzZahtjeva(r)
	natrag := "/slivovi"
	if err == nil {
		natrag = "/slivovi/kisomjer/" + k.Code + "/edit"
		err = h.svc().UpdateKisomjer(r.Context(), perms, k)
	}
	h.odgovori(w, r, err, "Kišomjer je izmijenjen.", natrag)
}

// HandleDelete briše točku
func (h *SlivoviHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	var code string
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var req struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		code = req.Code
	} else {
		code = r.FormValue("code")
	}
	err := h.svc().DeleteKisomjer(r.Context(), perms, code)
	h.odgovori(w, r, err, "Kišomjer je obrisan.", "/slivovi")
}

// HandlePolozaji sprema položaje točaka premještenih na karti
func (h *SlivoviHandler) HandlePolozaji(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	var req struct {
		Tocke []service.Polozaj `json:"tocke"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Neispravan JSON", http.StatusBadRequest)
		return
	}
	n, err := h.svc().PomakniKisomjere(r.Context(), perms, req.Tocke)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"success": true, "premjesteno": n,
		"message": fmt.Sprintf("Spremljeno: %d premještenih točaka.", n)})
}

// HandleSliv upisuje ili mijenja sliv (oznaka, naziv, površina, poligon)
func (h *SlivoviHandler) HandleSliv(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	var m models.Sliv
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			http.Error(w, "Neispravan JSON", http.StatusBadRequest)
			return
		}
	} else {
		m.Oznaka, m.Naziv = r.FormValue("oznaka"), r.FormValue("naziv")
		m.Geometry, m.Napomena = r.FormValue("geometry"), r.FormValue("napomena")
		if v := strings.TrimSpace(strings.ReplaceAll(r.FormValue("km2"), ",", ".")); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				m.Km2 = &f
			}
		}
	}
	if strings.TrimSpace(m.Geometry) != "" {
		var js json.RawMessage
		if json.Unmarshal([]byte(m.Geometry), &js) != nil {
			http.Error(w, "Neispravan GeoJSON sliva", http.StatusBadRequest)
			return
		}
	}
	err := h.svc().UpsertSliv(r.Context(), perms, &m)
	h.odgovori(w, r, err, "Sliv je upisan.", "/slivovi")
}

// HandleDeleteSliv briše sliv
func (h *SlivoviHandler) HandleDeleteSliv(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	err := h.svc().DeleteSliv(r.Context(), perms, r.FormValue("oznaka"))
	h.odgovori(w, r, err, "Sliv je obrisan.", "/slivovi")
}

// HandleListAPI vraća točke i slivove u JSON obliku, za preuzimanje oborina
func (h *SlivoviHandler) HandleListAPI(w http.ResponseWriter, r *http.Request) {
	svc := h.svc()
	if svc == nil {
		http.Error(w, "Registar kišomjera nije uključen", http.StatusServiceUnavailable)
		return
	}
	tocke, err := svc.ListKisomjeri(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ms, _ := svc.ListSlivovi(r.Context())
	if r.URL.Query().Get("geometrija") == "" {
		for i := range ms {
			ms[i].Geometry = ""
		}
	}
	writeJSON(w, map[string]any{"success": true, "kisomjeri": tocke, "slivovi": ms})
}
