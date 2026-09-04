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

type TerritoriesHandler struct {
	territoryService *service.TerritoryService
	tmpl             *template.Template // popis
	tmplCountyForm   *template.Template
	tmplMuniForm     *template.Template
	tmplMuniDetail   *template.Template
}

func NewTerritoriesHandler(
	territoryService *service.TerritoryService,
	tmpl *template.Template,
) *TerritoriesHandler {
	return &TerritoriesHandler{
		territoryService: territoryService,
		tmpl:             tmpl,
	}
}

type TerritoriesPageData struct {
	CurrentUser      *models.User
	Permissions      *models.UserPermissions
	Counties         []models.County
	Municipalities   []models.Municipality
	SelectedCountyID int
	SelectedType     string
	SearchQuery      string
	ActiveTab        string
	TotalCounties    int
	TotalMunis       int
	TotalCities      int
	TotalSettlements int
	SuccessMessage   string
	ErrorMessage     string
	ActiveNav        string
	Pager            Pager
	ViewAsBanner
}

// ShowTerritories prikazuje stranicu s popisom županija, gradova, općina i naselja
func (h *TerritoriesHandler) ShowTerritories(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)

	countyIDStr := r.URL.Query().Get("county_id")
	selectedCountyID, _ := strconv.Atoi(countyIDStr)
	selectedType := strings.ToUpper(r.URL.Query().Get("type"))
	searchQuery := r.URL.Query().Get("q")
	activeTab := r.URL.Query().Get("tab")
	if activeTab == "" {
		if selectedCountyID > 0 || selectedType != "" || searchQuery != "" {
			activeTab = "municipalities"
		} else {
			activeTab = "counties"
		}
	}

	counties, err := h.territoryService.ListCounties(ctx)
	if err != nil {
		http.Error(w, "Greška pri dohvatu županija: "+err.Error(), http.StatusInternalServerError)
		return
	}

	municipalities, err := h.territoryService.ListMunicipalities(ctx, selectedCountyID, selectedType, searchQuery)
	if err != nil {
		http.Error(w, "Greška pri dohvatu gradova/općina: "+err.Error(), http.StatusInternalServerError)
		return
	}

	totalCounties, totalMunis, totalSettlements, _ := h.territoryService.GetTerritoryCounts(ctx)

	page, pager := paginate(municipalities, r, registryPerPage)
	data := TerritoriesPageData{
		CurrentUser:      currUser,
		Permissions:      perms,
		Counties:         counties,
		Municipalities:   page,
		Pager:            pager,
		SelectedCountyID: selectedCountyID,
		SelectedType:     selectedType,
		SearchQuery:      searchQuery,
		ActiveTab:        activeTab,
		TotalCounties:    totalCounties,
		TotalMunis:       totalMunis,
		TotalCities:      125,
		TotalSettlements: totalSettlements,
		SuccessMessage:   r.URL.Query().Get("success"),
		ErrorMessage:     r.URL.Query().Get("error"),
		ActiveNav:        "territories",
		ViewAsBanner:     viewBanner(r),
	}

	if err := h.tmpl.Execute(w, data); err != nil {
		http.Error(w, "Greška renderiranja: "+err.Error(), http.StatusInternalServerError)
	}
}

// HandleGetCountiesAPI vraća JSON listu svih županija
func (h *TerritoriesHandler) HandleGetCountiesAPI(w http.ResponseWriter, r *http.Request) {
	counties, err := h.territoryService.ListCounties(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(counties)
}

// HandleGetMunicipalitiesAPI vraća JSON listu gradova/općina za određenu županiju
func (h *TerritoriesHandler) HandleGetMunicipalitiesAPI(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("countyID")
	countyID, _ := strconv.Atoi(idStr)

	mType := r.URL.Query().Get("type")
	query := r.URL.Query().Get("q")

	munis, err := h.territoryService.ListMunicipalities(r.Context(), countyID, mType, query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(munis)
}

// HandleGetSettlementsAPI vraća JSON listu naselja za određenu općinu ili grad
func (h *TerritoriesHandler) HandleGetSettlementsAPI(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("municipalityID")
	muniID, _ := strconv.Atoi(idStr)
	query := r.URL.Query().Get("q")

	settlements, err := h.territoryService.ListSettlements(r.Context(), muniID, 0, query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(settlements)
}

// HandleGetSectionTerritoriesAPI vraća sva ugrožena područja pridružena dionici
func (h *TerritoriesHandler) HandleGetSectionTerritoriesAPI(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if code == "" {
		http.Error(w, "Šifra dionice je obavezna", http.StatusBadRequest)
		return
	}

	territories, err := h.territoryService.GetSectionTerritories(r.Context(), code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	protectedArea := service.GenerateProtectedAreaText(territories)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"territories":    territories,
		"protected_area": protectedArea,
	})
}

// HandleAddSectionTerritoryAPI dodaje novo ugroženo područje (naselje ili cijelu općinu) na dionicu
func (h *TerritoriesHandler) HandleAddSectionTerritoryAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)

	if currUser == nil {
		http.Error(w, "Neautorizirani pristup", http.StatusUnauthorized)
		return
	}

	code := r.PathValue("code")
	if code == "" {
		http.Error(w, "Šifra dionice je obavezna", http.StatusBadRequest)
		return
	}

	var req struct {
		CountyID       int   `json:"county_id"`
		MunicipalityID int   `json:"municipality_id"`
		SettlementIDs  []int `json:"settlement_ids"`
	}

	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Neispravan JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		// Form data fallback
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Neispravan unos: "+err.Error(), http.StatusBadRequest)
			return
		}
		cID, _ := strconv.Atoi(r.FormValue("county_id"))
		mID, _ := strconv.Atoi(r.FormValue("municipality_id"))
		req.CountyID = cID
		req.MunicipalityID = mID

		settlementStrs := r.Form["settlement_ids"]
		for _, sStr := range settlementStrs {
			if id, err := strconv.Atoi(sStr); err == nil && id > 0 {
				req.SettlementIDs = append(req.SettlementIDs, id)
			}
		}
	}

	if err := h.territoryService.AddSectionTerritories(ctx, perms, code, req.CountyID, req.MunicipalityID, req.SettlementIDs); err != nil {
		if wantsPage(r) {
			redirectWith(w, r, "/sections/"+code, "error", err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if wantsPage(r) {
		redirectWith(w, r, "/sections/"+code, "success", "Teritorijalne jedinice su pridružene; ugroženo područje je osvježeno.")
		return
	}

	// Vrati osvježenu listu teritorija i generirani tekst za tu dionicu
	territories, err := h.territoryService.GetSectionTerritories(ctx, code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	protectedArea := service.GenerateProtectedAreaText(territories)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"territories":    territories,
		"protected_area": protectedArea,
	})
}

// HandleRemoveSectionTerritoryAPI uklanja vezu ugroženog područja s dionice
func (h *TerritoriesHandler) HandleRemoveSectionTerritoryAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)

	if currUser == nil {
		http.Error(w, "Neautorizirani pristup", http.StatusUnauthorized)
		return
	}

	code := r.PathValue("code")
	var req struct {
		ID string `json:"id"`
	}

	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Neispravan JSON", http.StatusBadRequest)
			return
		}
	} else {
		req.ID = r.FormValue("id")
	}

	if req.ID == "" {
		http.Error(w, "ID teritorija je obavezan", http.StatusBadRequest)
		return
	}

	if err := h.territoryService.RemoveSectionTerritory(ctx, perms, req.ID, code); err != nil {
		if wantsPage(r) {
			redirectWith(w, r, "/sections/"+code, "error", err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if wantsPage(r) {
		redirectWith(w, r, "/sections/"+code, "success", "Teritorijalna jedinica je uklonjena s dionice.")
		return
	}

	territories, _ := h.territoryService.GetSectionTerritories(ctx, code)
	protectedArea := service.GenerateProtectedAreaText(territories)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"territories":    territories,
		"protected_area": protectedArea,
	})
}

// HandleCreateCounty stvara novu županiju
func (h *TerritoriesHandler) HandleCreateCounty(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/territories?tab=counties&error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	area, _ := strconv.Atoi(r.FormValue("area_sqkm"))
	pop, _ := strconv.Atoi(r.FormValue("population"))

	c := &models.County{
		Code:       strings.TrimSpace(r.FormValue("code")),
		Name:       strings.TrimSpace(r.FormValue("name")),
		Seat:       strings.TrimSpace(r.FormValue("seat")),
		Prefect:    strings.TrimSpace(r.FormValue("prefect")),
		AreaSqKm:   area,
		Population: pop,
		Email:      strings.TrimSpace(r.FormValue("email")),
		Phone:      strings.TrimSpace(r.FormValue("phone")),
	}

	if err := h.territoryService.CreateCounty(r.Context(), perms, c); err != nil {
		redirectWith(w, r, "/territories/counties/new", "error", err.Error())
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/territories?tab=counties&success=Uspješno+dodana+županija+%s", strings.ReplaceAll(c.Name, " ", "+")), http.StatusSeeOther)
}

// HandleUpdateCounty ažurira postojeću županiju
func (h *TerritoriesHandler) HandleUpdateCounty(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/territories?tab=counties&error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	id, _ := strconv.Atoi(r.FormValue("id"))
	area, _ := strconv.Atoi(r.FormValue("area_sqkm"))
	pop, _ := strconv.Atoi(r.FormValue("population"))

	c := &models.County{
		ID:         id,
		Code:       strings.TrimSpace(r.FormValue("code")),
		Name:       strings.TrimSpace(r.FormValue("name")),
		Seat:       strings.TrimSpace(r.FormValue("seat")),
		Prefect:    strings.TrimSpace(r.FormValue("prefect")),
		AreaSqKm:   area,
		Population: pop,
		Email:      strings.TrimSpace(r.FormValue("email")),
		Phone:      strings.TrimSpace(r.FormValue("phone")),
	}

	if err := h.territoryService.UpdateCounty(r.Context(), perms, c); err != nil {
		redirectWith(w, r, fmt.Sprintf("/territories/counties/%d/edit", c.ID), "error", err.Error())
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/territories?tab=counties&success=Uspješno+ažurirana+županija+%s", strings.ReplaceAll(c.Name, " ", "+")), http.StatusSeeOther)
}

// HandleDeleteCounty briše županiju
func (h *TerritoriesHandler) HandleDeleteCounty(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/territories?tab=counties&error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	id, _ := strconv.Atoi(r.FormValue("id"))

	if err := h.territoryService.DeleteCounty(r.Context(), perms, id); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/territories?tab=counties&error=%s", strings.ReplaceAll(err.Error(), " ", "+")), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/territories?tab=counties&success=Županija+uspješno+obrisana", http.StatusSeeOther)
}

// HandleCreateMunicipality dodaje novi grad ili općinu
func (h *TerritoriesHandler) HandleCreateMunicipality(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/territories?tab=municipalities&error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	countyID, _ := strconv.Atoi(r.FormValue("county_id"))
	area, _ := strconv.ParseFloat(r.FormValue("area_sqkm"), 64)
	pop, _ := strconv.Atoi(r.FormValue("population"))

	m := &models.Municipality{
		CountyID:   countyID,
		Name:       strings.TrimSpace(r.FormValue("name")),
		Type:       strings.ToUpper(strings.TrimSpace(r.FormValue("type"))),
		HeadTitle:  strings.TrimSpace(r.FormValue("head_title")),
		HeadName:   strings.TrimSpace(r.FormValue("head_name")),
		PostalCode: strings.TrimSpace(r.FormValue("postal_code")),
		AreaSqKm:   area,
		Population: pop,
	}

	if err := h.territoryService.CreateMunicipality(r.Context(), perms, m); err != nil {
		redirectWith(w, r, fmt.Sprintf("/territories/municipalities/new?county_id=%d", countyID), "error", err.Error())
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/territories?tab=municipalities&county_id=%d&success=Uspješno+dodan+%s", countyID, strings.ReplaceAll(m.Name, " ", "+")), http.StatusSeeOther)
}

// HandleUpdateMunicipality ažurira/preimenuje grad ili općinu
func (h *TerritoriesHandler) HandleUpdateMunicipality(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/territories?tab=municipalities&error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	id, _ := strconv.Atoi(r.FormValue("id"))
	countyID, _ := strconv.Atoi(r.FormValue("county_id"))
	area, _ := strconv.ParseFloat(r.FormValue("area_sqkm"), 64)
	pop, _ := strconv.Atoi(r.FormValue("population"))

	m := &models.Municipality{
		ID:         id,
		CountyID:   countyID,
		Name:       strings.TrimSpace(r.FormValue("name")),
		Type:       strings.ToUpper(strings.TrimSpace(r.FormValue("type"))),
		HeadTitle:  strings.TrimSpace(r.FormValue("head_title")),
		HeadName:   strings.TrimSpace(r.FormValue("head_name")),
		PostalCode: strings.TrimSpace(r.FormValue("postal_code")),
		AreaSqKm:   area,
		Population: pop,
	}

	if err := h.territoryService.UpdateMunicipality(r.Context(), perms, m); err != nil {
		redirectWith(w, r, fmt.Sprintf("/territories/municipalities/%d/edit", m.ID), "error", err.Error())
		return
	}
	redirectWith(w, r, fmt.Sprintf("/territories/municipalities/%d", m.ID), "success", "Izmjene su spremljene.")
}

// HandleDeleteMunicipality briše grad ili općinu
func (h *TerritoriesHandler) HandleDeleteMunicipality(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/territories?tab=municipalities&error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	id, _ := strconv.Atoi(r.FormValue("id"))
	countyID, _ := strconv.Atoi(r.FormValue("county_id"))

	if err := h.territoryService.DeleteMunicipality(r.Context(), perms, id); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/territories?tab=municipalities&county_id=%d&error=%s", countyID, strings.ReplaceAll(err.Error(), " ", "+")), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/territories?tab=municipalities&county_id=%d&success=Uspješno+obrisano", countyID), http.StatusSeeOther)
}

// HandleCreateSettlementAPI stvara novo naselje (AJAX)
func (h *TerritoriesHandler) HandleCreateSettlementAPI(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)

	var req struct {
		MunicipalityID int    `json:"municipality_id"`
		CountyID       int    `json:"county_id"`
		Name           string `json:"name"`
		PostalCode     string `json:"postal_code"`
		Population     int    `json:"population"`
	}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Neispravan JSON", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Neispravan unos", http.StatusBadRequest)
			return
		}
		req.MunicipalityID, _ = strconv.Atoi(r.FormValue("municipality_id"))
		req.CountyID, _ = strconv.Atoi(r.FormValue("county_id"))
		req.Name = r.FormValue("name")
		req.PostalCode = r.FormValue("postal_code")
		req.Population, _ = strconv.Atoi(strings.TrimSpace(r.FormValue("population")))
	}

	s := &models.Settlement{
		MunicipalityID: req.MunicipalityID,
		CountyID:       req.CountyID,
		Name:           strings.TrimSpace(req.Name),
		PostalCode:     strings.TrimSpace(req.PostalCode),
		Population:     req.Population,
	}

	back := fmt.Sprintf("/territories/municipalities/%d", s.MunicipalityID)
	if err := h.territoryService.CreateSettlement(r.Context(), perms, s); err != nil {
		if wantsPage(r) {
			redirectWith(w, r, back, "error", err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if wantsPage(r) {
		redirectWith(w, r, back, "success", "Naselje "+s.Name+" je dodano.")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"settlement": s,
	})
}

// HandleUpdateSettlementAPI preimenuje/ažurira naselje (AJAX)
func (h *TerritoriesHandler) HandleUpdateSettlementAPI(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)

	var req struct {
		ID         int    `json:"id"`
		Name       string `json:"name"`
		PostalCode string `json:"postal_code"`
		Population int    `json:"population"`
	}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Neispravan JSON", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Neispravan unos", http.StatusBadRequest)
			return
		}
		req.ID, _ = strconv.Atoi(r.FormValue("id"))
		req.Name = r.FormValue("name")
		req.PostalCode = r.FormValue("postal_code")
		req.Population, _ = strconv.Atoi(strings.TrimSpace(r.FormValue("population")))
	}

	s := &models.Settlement{
		ID:         req.ID,
		Name:       strings.TrimSpace(req.Name),
		PostalCode: strings.TrimSpace(req.PostalCode),
		Population: req.Population,
	}

	if err := h.territoryService.UpdateSettlement(r.Context(), perms, s); err != nil {
		if wantsPage(r) {
			redirectWith(w, r, backToMunicipality(r), "error", err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if wantsPage(r) {
		redirectWith(w, r, backToMunicipality(r), "success", "Naselje je spremljeno.")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// HandleDeleteSettlementAPI briše naselje (AJAX)
func (h *TerritoriesHandler) HandleDeleteSettlementAPI(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)

	var req struct {
		ID int `json:"id"`
	}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Neispravan JSON", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Neispravan unos", http.StatusBadRequest)
			return
		}
		req.ID, _ = strconv.Atoi(r.FormValue("id"))
	}

	if err := h.territoryService.DeleteSettlement(r.Context(), perms, req.ID); err != nil {
		if wantsPage(r) {
			redirectWith(w, r, backToMunicipality(r), "error", err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if wantsPage(r) {
		redirectWith(w, r, backToMunicipality(r), "success", "Naselje je obrisano.")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// backToMunicipality vraća na stranicu grada ili općine iz koje je obrazac
// poslan; bez toga podatka natrag na popis
func backToMunicipality(r *http.Request) string {
	if id, _ := strconv.Atoi(r.FormValue("municipality_id")); id > 0 {
		return fmt.Sprintf("/territories/municipalities/%d", id)
	}
	if ref := r.Header.Get("Referer"); strings.Contains(ref, "/territories/municipalities/") {
		return strings.Split(ref, "?")[0]
	}
	return "/territories?tab=municipalities"
}
