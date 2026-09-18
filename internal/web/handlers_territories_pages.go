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

	// Službe uz županiju ili grad, za obavijesti u aktima o obrani
	Sluzbe         []models.Sluzba
	Municipalities []models.Municipality
	VrsteSluzbi    []string
	SmijeSluzbe    bool
	Sluzba         *models.Sluzba // koja se uređuje

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
	if sve, err := h.territoryService.Sluzbe(r.Context(), m.CountyID); err == nil {
		for _, x := range sve {
			if x.MunicipalityID == 0 || x.MunicipalityID == m.ID {
				data.Sluzbe = append(data.Sluzbe, x)
			}
		}
	}
	if err := h.tmplMuniDetail.ExecuteTemplate(w, "municipality_detail.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// SetCountyTemplate daje rukovatelju predložak stranice županije
func (h *TerritoriesHandler) SetCountyTemplate(t *template.Template) { h.tmplCounty = t }

func smijeSluzbe(p *models.UserPermissions) bool {
	return p != nil && (p.IsGlobalAdmin || len(p.AdminSectors) > 0)
}

// ShowCounty prikazuje županiju sa službama koje akti o obrani obavještavaju
func (h *TerritoriesHandler) ShowCounty(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	id, _ := strconv.Atoi(r.PathValue("id"))
	c, err := h.territoryService.GetCountyByID(r.Context(), id)
	if err != nil || c == nil {
		http.NotFound(w, r)
		return
	}
	data.County = *c
	data.Sluzbe, _ = h.territoryService.Sluzbe(r.Context(), c.ID)
	data.Municipalities, _ = h.territoryService.ListMunicipalities(r.Context(), c.ID, "", "")
	data.VrsteSluzbi = models.VrsteSluzbi
	data.SmijeSluzbe = smijeSluzbe(data.Permissions)
	if uredi := r.URL.Query().Get("uredi"); uredi != "" {
		for i := range data.Sluzbe {
			if data.Sluzbe[i].ID == uredi {
				data.Sluzba = &data.Sluzbe[i]
			}
		}
	}
	if err := h.tmplCounty.ExecuteTemplate(w, "county_detail.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleSaveSluzba dodaje ili mijenja službu uz županiju
func (h *TerritoriesHandler) HandleSaveSluzba(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "neispravan zahtjev", http.StatusBadRequest)
		return
	}
	countyID, _ := strconv.Atoi(r.PathValue("id"))
	x := models.Sluzba{ID: r.FormValue("sluzba_id"), CountyID: countyID, Vrsta: r.FormValue("vrsta"), Naziv: r.FormValue("naziv"),
		Email: r.FormValue("email"), Phone: r.FormValue("phone"), Napomena: r.FormValue("napomena")}
	x.MunicipalityID, _ = strconv.Atoi(r.FormValue("municipality_id"))
	x.Redoslijed, _ = strconv.Atoi(r.FormValue("redoslijed"))
	natrag := "/territories/counties/" + strconv.Itoa(countyID)
	if err := h.territoryService.SpremiSluzbu(r.Context(), perms, &x); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Služba "+x.Naziv+" je upisana.")
}

// HandleDeleteSluzba briše službu
func (h *TerritoriesHandler) HandleDeleteSluzba(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	natrag := "/territories/counties/" + r.PathValue("id")
	if err := h.territoryService.ObrisiSluzbu(r.Context(), perms, r.PathValue("sluzba")); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Služba je obrisana.")
}
