package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"gocop/internal/models"
	"gocop/internal/service"
)

type WatercoursesHandler struct {
	watercourseService *service.WatercourseService
	sectionService     *service.SectionService
	stationService     *service.StationService
	tmpl               *template.Template // popis
	tmplDetail         *template.Template // jedna voda
	tmplForm           *template.Template // obrazac
}

func NewWatercoursesHandler(
	watercourseService *service.WatercourseService,
	sectionService *service.SectionService,
	tmpl *template.Template,
) *WatercoursesHandler {
	return &WatercoursesHandler{
		watercourseService: watercourseService,
		sectionService:     sectionService,
		tmpl:               tmpl,
	}
}

type WatercoursesPageData struct {
	CurrentUser      *models.User
	Permissions      *models.UserPermissions
	Watercourses     []models.Watercourse
	Categories       []string
	SearchQuery      string
	SelectedCategory string
	OnlyUsed         bool
	Total            int
	FirstOrder       int
	Used             int
	UnlinkedSections int
	UnlinkedStations int
	SuccessMessage   string
	ErrorMessage     string
	ActiveNav        string
	Pager            Pager
	ViewAsBanner
}

// ShowWatercourses prikazuje registar vodnih tijela
func (h *WatercoursesHandler) ShowWatercourses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)

	data := WatercoursesPageData{
		CurrentUser:      currUser,
		Permissions:      perms,
		SearchQuery:      strings.TrimSpace(r.URL.Query().Get("q")),
		SelectedCategory: strings.TrimSpace(r.URL.Query().Get("category")),
		OnlyUsed:         r.URL.Query().Get("used") == "1",
		SuccessMessage:   r.URL.Query().Get("success"),
		ErrorMessage:     r.URL.Query().Get("error"),
		ActiveNav:        "watercourses",
		ViewAsBanner:     viewBanner(r),
	}

	waters, err := h.watercourseService.ListWatercourses(ctx, data.SearchQuery, data.SelectedCategory, data.OnlyUsed)
	if err != nil {
		data.ErrorMessage = err.Error()
	}
	for _, w := range waters {
		if w.IsFirstOrder() {
			data.FirstOrder++
		}
	}
	data.Watercourses, data.Pager = paginate(waters, r, registryPerPage)
	if cats, err := h.watercourseService.ListCategories(ctx); err == nil {
		data.Categories = cats
	}
	if total, used, us, st, err := h.watercourseService.Counts(ctx); err == nil {
		data.Total, data.Used, data.UnlinkedSections, data.UnlinkedStations = total, used, us, st
	}

	if err := h.tmpl.ExecuteTemplate(w, "watercourses.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleListWatercoursesAPI vraća registar vodnih tijela u JSON obliku
func (h *WatercoursesHandler) HandleListWatercoursesAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	waters, err := h.watercourseService.ListWatercourses(ctx,
		strings.TrimSpace(r.URL.Query().Get("q")),
		strings.TrimSpace(r.URL.Query().Get("category")),
		r.URL.Query().Get("used") == "1",
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{"success": true, "watercourses": waters})
}

// watercourseForm su podaci obrasca za unos i izmjenu vodnog tijela
type watercourseForm struct {
	Code         string `json:"code"`
	OfficialName string `json:"official_name"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Category     string `json:"category"`
	Subcategory  string `json:"subcategory"`
	WikiSlug     string `json:"wiki_slug"`
	LengthKm     string `json:"length_km"`
	BasinKm2     string `json:"basin_km2"`
	AvgFlowM3S   string `json:"avg_flow_m3s"`
	Source       string `json:"source"`
	Mouth        string `json:"mouth"`
	FlowsInto    string `json:"flows_into"`
}

func (f watercourseForm) toWatercourse() models.Watercourse {
	return models.Watercourse{
		Code:         strings.TrimSpace(f.Code),
		OfficialName: strings.TrimSpace(f.OfficialName),
		Name:         strings.TrimSpace(f.Name),
		Kind:         strings.TrimSpace(f.Kind),
		Category:     strings.TrimSpace(f.Category),
		Subcategory:  strings.TrimSpace(f.Subcategory),
		WikiSlug:     strings.TrimSpace(f.WikiSlug),
		LengthKm:     parseOptionalFloat(f.LengthKm),
		BasinKm2:     parseOptionalFloat(f.BasinKm2),
		AvgFlowM3S:   parseOptionalFloat(f.AvgFlowM3S),
		Source:       strings.TrimSpace(f.Source),
		Mouth:        strings.TrimSpace(f.Mouth),
		FlowsInto:    strings.TrimSpace(f.FlowsInto),
	}
}

func decodeWatercourseForm(r *http.Request) (watercourseForm, error) {
	var form watercourseForm

	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&form); err != nil {
			return form, errBadJSON(err)
		}
		return form, nil
	}

	if err := r.ParseForm(); err != nil {
		return form, err
	}
	form.Code = r.FormValue("code")
	form.OfficialName = r.FormValue("official_name")
	form.Name = r.FormValue("name")
	form.Kind = r.FormValue("kind")
	form.Category = r.FormValue("category")
	form.Subcategory = r.FormValue("subcategory")
	form.WikiSlug = r.FormValue("wiki_slug")
	form.LengthKm = r.FormValue("length_km")
	form.BasinKm2 = r.FormValue("basin_km2")
	form.AvgFlowM3S = r.FormValue("avg_flow_m3s")
	form.Source = r.FormValue("source")
	form.Mouth = r.FormValue("mouth")
	form.FlowsInto = r.FormValue("flows_into")
	return form, nil
}

// HandleCreateWatercourseAPI upisuje novo vodno tijelo u registar
func (h *WatercoursesHandler) HandleCreateWatercourseAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)

	form, err := decodeWatercourseForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	water := form.toWatercourse()
	if err := h.watercourseService.CreateWatercourse(ctx, perms, &water); err != nil {
		if wantsPage(r) {
			redirectWith(w, r, "/watercourses/new", "error", err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if wantsPage(r) {
		redirectWith(w, r, "/watercourses/"+water.Code, "success", "Vodno tijelo je upisano u registar.")
		return
	}
	writeJSON(w, map[string]any{"success": true, "watercourse": water})
}

// HandleUpdateWatercourseAPI mijenja podatke vodnog tijela
func (h *WatercoursesHandler) HandleUpdateWatercourseAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)

	form, err := decodeWatercourseForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(form.Code) == "" {
		http.Error(w, "Šifra vodnog tijela je obavezna", http.StatusBadRequest)
		return
	}

	water := form.toWatercourse()
	if err := h.watercourseService.UpdateWatercourse(ctx, perms, &water); err != nil {
		if wantsPage(r) {
			redirectWith(w, r, "/watercourses/"+water.Code+"/edit", "error", err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if wantsPage(r) {
		redirectWith(w, r, "/watercourses/"+water.Code, "success", "Izmjene su spremljene.")
		return
	}
	writeJSON(w, map[string]any{"success": true, "watercourse": water})
}

// HandleDeleteWatercourseAPI briše vodno tijelo koje nije u upotrebi
func (h *WatercoursesHandler) HandleDeleteWatercourseAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)

	form, err := decodeWatercourseForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	code := strings.TrimSpace(form.Code)
	if err := h.watercourseService.DeleteWatercourse(ctx, perms, code); err != nil {
		if wantsPage(r) {
			redirectWith(w, r, "/watercourses/"+code, "error", err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if wantsPage(r) {
		redirectWith(w, r, "/watercourses", "success", "Vodno tijelo je obrisano iz registra; zapis ostaje u povijesti.")
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

// HandleAssignSectionWatercourseAPI pridružuje vodno tijelo dionici
func (h *WatercoursesHandler) HandleAssignSectionWatercourseAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)

	code := r.PathValue("code")
	if code == "" {
		http.Error(w, "Šifra dionice je obavezna", http.StatusBadRequest)
		return
	}

	form, err := decodeWatercourseForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.watercourseService.SetSectionWatercourse(ctx, perms, code, strings.TrimSpace(form.Code), h.sectionService); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, map[string]any{"success": true, "section_code": code, "watercourse_code": form.Code})
}

// HandleAssignStationWatercourseAPI pridružuje vodno tijelo vodomjernoj postaji
func (h *WatercoursesHandler) HandleAssignStationWatercourseAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)

	var req struct {
		StationID       string `json:"station_id"`
		WatercourseCode string `json:"watercourse_code"`
	}

	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, errBadJSON(err).Error(), http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.StationID = r.FormValue("station_id")
		req.WatercourseCode = r.FormValue("watercourse_code")
	}

	if strings.TrimSpace(req.StationID) == "" {
		http.Error(w, "Identifikator postaje je obavezan", http.StatusBadRequest)
		return
	}

	if err := h.watercourseService.SetStationWatercourse(ctx, perms,
		strings.TrimSpace(req.StationID), strings.TrimSpace(req.WatercourseCode)); err != nil {
		if wantsPage(r) {
			redirectWith(w, r, "/stations/"+strings.TrimSpace(req.StationID), "error", err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if wantsPage(r) {
		redirectWith(w, r, "/stations/"+strings.TrimSpace(req.StationID), "success", "Vodotok postaje je spremljen.")
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(payload)
}
