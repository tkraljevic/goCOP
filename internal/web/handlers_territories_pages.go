package web

import (
	"html/template"
	"net/http"
	"strconv"

	"gocop/internal/models"
)

// Stranice registra teritorijalnih jedinica: obrazac županije, obrazac grada
// ili općine, i stranica grada ili općine s naseljima.

// TerritoryPageData je stranica jedne jedinice ili njezina obrasca
type TerritoryPageData struct {
	CurrentUser  *models.User
	Permissions  *models.UserPermissions
	County       models.County
	Municipality models.Municipality
	Settlements  []models.Settlement
	Counties     []models.County
	IsEdit       bool

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// SetPageTemplates daje rukovatelju predloške stranica
func (h *TerritoriesHandler) SetPageTemplates(countyForm, muniForm, muniDetail *template.Template) {
	h.tmplCountyForm = countyForm
	h.tmplMuniForm = muniForm
	h.tmplMuniDetail = muniDetail
}

func (h *TerritoriesHandler) pageData(r *http.Request) TerritoryPageData {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return TerritoryPageData{
		CurrentUser:    currUser,
		Permissions:    perms,
		SuccessMessage: r.URL.Query().Get("success"),
		ErrorMessage:   r.URL.Query().Get("error"),
		ActiveNav:      "territories",
		ViewAsBanner:   viewBanner(r),
	}
}

// ShowCountyForm prikazuje obrazac za novu županiju ili izmjenu postojeće
func (h *TerritoriesHandler) ShowCountyForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if data.Permissions == nil || !data.Permissions.IsGlobalAdmin {
		http.Error(w, "Županije uređuje globalni administrator", http.StatusForbidden)
		return
	}
	if raw := r.PathValue("id"); raw != "" {
		id, _ := strconv.Atoi(raw)
		c, err := h.territoryService.GetCountyByID(r.Context(), id)
		if err != nil || c == nil {
			http.NotFound(w, r)
			return
		}
		data.County = *c
		data.IsEdit = true
	}
	if err := h.tmplCountyForm.ExecuteTemplate(w, "county_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowMunicipalityForm prikazuje obrazac za novi grad ili općinu ili izmjenu postojećeg
func (h *TerritoriesHandler) ShowMunicipalityForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if data.Permissions == nil || !data.Permissions.IsGlobalAdmin {
		http.Error(w, "Gradove i općine uređuje globalni administrator", http.StatusForbidden)
		return
	}
	data.Counties, _ = h.territoryService.ListCounties(r.Context())
	if raw := r.PathValue("id"); raw != "" {
		id, _ := strconv.Atoi(raw)
		m, err := h.territoryService.GetMunicipalityByID(r.Context(), id)
		if err != nil || m == nil {
			http.NotFound(w, r)
			return
		}
		data.Municipality = *m
		data.IsEdit = true
	} else {
		data.Municipality.Type = "OPCINA"
		if cid, _ := strconv.Atoi(r.URL.Query().Get("county_id")); cid > 0 {
			data.Municipality.CountyID = cid
		}
	}
	if err := h.tmplMuniForm.ExecuteTemplate(w, "municipality_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowMunicipality prikazuje grad ili općinu s naseljima
func (h *TerritoriesHandler) ShowMunicipality(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	id, _ := strconv.Atoi(r.PathValue("id"))
	m, err := h.territoryService.GetMunicipalityByID(r.Context(), id)
	if err != nil || m == nil {
		http.NotFound(w, r)
		return
	}
	data.Municipality = *m
	if c, err := h.territoryService.GetCountyByID(r.Context(), m.CountyID); err == nil && c != nil {
		data.County = *c
	}
	if st, err := h.territoryService.ListSettlements(r.Context(), m.ID, 0, ""); err == nil {
		data.Settlements = st
	}
	if err := h.tmplMuniDetail.ExecuteTemplate(w, "municipality_detail.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
