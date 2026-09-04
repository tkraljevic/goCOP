package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"gocop/internal/hydro"
	"gocop/internal/models"
	"gocop/internal/service"
)

type SectionItemView struct {
	models.Section
	CanEdit bool `json:"can_edit"`
}

type SectionsHandler struct {
	sectionService   *service.SectionService
	userService      *service.UserService
	stationService   *service.StationService
	territoryService *service.TerritoryService
	structureService *service.StructureService
	tmpl             *template.Template // popis
	tmplDetail       *template.Template // jedna dionica
	tmplForm         *template.Template // obrazac
}

func NewSectionsHandler(
	sectionService *service.SectionService,
	userService *service.UserService,
	tmpl *template.Template,
) *SectionsHandler {
	return &SectionsHandler{
		sectionService: sectionService,
		userService:    userService,
		tmpl:           tmpl,
	}
}

type SectionsPageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	Sections       []SectionItemView
	Sectors        []models.Sector
	Areas          []models.Area
	SelectedSector string
	SelectedArea   int
	SearchQuery    string
	CanCreateAny   bool
	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	Pager          Pager
	ViewAsBanner
}

// ShowSections prikazuje registar štićenih dionica
func (h *SectionsHandler) ShowSections(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)

	sectorFilter := strings.TrimSpace(r.URL.Query().Get("sector"))
	areaStr := strings.TrimSpace(r.URL.Query().Get("area"))
	searchQuery := strings.TrimSpace(r.URL.Query().Get("q"))

	areaFilter := 0
	if areaStr != "" {
		if id, err := strconv.Atoi(areaStr); err == nil {
			areaFilter = id
		}
	}

	rawSections, err := h.sectionService.ListSections(sectorFilter, areaFilter, searchQuery)
	if err != nil {
		http.Error(w, "Greška pri dohvatu dionica: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var sections []SectionItemView
	for _, s := range rawSections {
		canEdit := h.sectionService.CanEditSection(perms, &s)
		sections = append(sections, SectionItemView{
			Section: s,
			CanEdit: canEdit,
		})
	}

	sectors, _ := h.userService.ListSectors()
	areas, _ := h.userService.ListAreas(sectorFilter)

	canCreate := false
	if perms != nil {
		canCreate = perms.IsGlobalAdmin || len(perms.AdminSectors) > 0 || len(perms.AdminAreas) > 0 || len(perms.AllowedSectors) > 0
	}

	page, pager := paginate(sections, r, registryPerPage)
	data := SectionsPageData{
		CurrentUser:    currUser,
		Permissions:    perms,
		Sections:       page,
		Pager:          pager,
		Sectors:        sectors,
		Areas:          areas,
		SelectedSector: sectorFilter,
		SelectedArea:   areaFilter,
		SearchQuery:    searchQuery,
		CanCreateAny:   canCreate,
		SuccessMessage: r.URL.Query().Get("success"),
		ErrorMessage:   r.URL.Query().Get("error"),
		ActiveNav:      "sections",
		ViewAsBanner:   viewBanner(r),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "sections.html", data); err != nil {
		http.Error(w, "Greška pri renderiranju dionica: "+err.Error(), http.StatusInternalServerError)
	}
}

// HandleGetSectionAPI vraća detaljne podatke pojedinačne dionice s osobljem kao JSON
func (h *SectionsHandler) HandleGetSectionAPI(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if code == "" {
		code = r.URL.Query().Get("code")
	}
	code = strings.TrimSpace(code)

	sec, err := h.sectionService.GetSectionWithDetails(code)
	if err != nil || sec == nil {
		http.Error(w, `{"error":"Dionica nije pronađena"}`, http.StatusNotFound)
		return
	}

	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	canEdit := h.sectionService.CanEditSection(perms, sec)

	resp := struct {
		*models.Section
		CanEdit bool `json:"can_edit"`
	}{
		Section: sec,
		CanEdit: canEdit,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// HandleCreateSection kreira novu dionicu
func (h *SectionsHandler) HandleCreateSection(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/sections?error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}

	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)

	code := strings.TrimSpace(r.FormValue("code"))
	sectorID := strings.TrimSpace(r.FormValue("sector_id"))
	areaID, _ := strconv.Atoi(r.FormValue("area_id"))

	watercourseName := strings.TrimSpace(r.FormValue("watercourse_name"))
	watercourseChainage := strings.TrimSpace(r.FormValue("watercourse_chainage"))
	watercourse := watercourseName
	if watercourseChainage != "" {
		if watercourse != "" {
			watercourse = watercourse + "; " + watercourseChainage
		} else {
			watercourse = watercourseChainage
		}
	} else if watercourse == "" {
		watercourse = strings.TrimSpace(r.FormValue("watercourse"))
	}

	protectedArea := strings.TrimSpace(r.FormValue("protected_area"))
	notes := strings.TrimSpace(r.FormValue("notes"))

	// Parsiranje struktura, nasipa i vodomjera iz JSON-a ili jednostavnog teksta
	var embankments []models.EmbankmentItem
	if embStr := strings.TrimSpace(r.FormValue("embankments_json")); embStr != "" {
		_ = json.Unmarshal([]byte(embStr), &embankments)
	}

	var structures []models.StructureItem
	if strStr := strings.TrimSpace(r.FormValue("structures_json")); strStr != "" {
		_ = json.Unmarshal([]byte(strStr), &structures)
	}

	var gauges []models.GaugeItem
	if gagStr := strings.TrimSpace(r.FormValue("gauges_json")); gagStr != "" {
		_ = json.Unmarshal([]byte(gagStr), &gauges)
	}

	sec := &models.Section{
		Code:          code,
		AreaID:        areaID,
		SectorID:      sectorID,
		Description:   watercourse,
		ProtectedArea: protectedArea,
		Embankments:   embankments,
		Structures:    structures,
		Gauges:        gauges,
		Notes:         notes,
	}
	applySectionStructure(sec, r)

	if err := h.sectionService.CreateSection(perms, sec); err != nil {
		redirectWith(w, r, "/sections/new", "error", err.Error())
		return
	}

	redirectWith(w, r, "/sections/"+code, "success", "Dionica "+code+" je upisana. Sada joj dodajte ugroženo područje i mjerodavne vodomjere.")
}

// HandleUpdateSection ažurira postojeću dionicu
func (h *SectionsHandler) HandleUpdateSection(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/sections?error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}

	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)

	code := strings.TrimSpace(r.FormValue("code"))
	watercourseName := strings.TrimSpace(r.FormValue("watercourse_name"))
	watercourseChainage := strings.TrimSpace(r.FormValue("watercourse_chainage"))
	watercourse := watercourseName
	if watercourseChainage != "" {
		if watercourse != "" {
			watercourse = watercourse + "; " + watercourseChainage
		} else {
			watercourse = watercourseChainage
		}
	} else if watercourse == "" {
		watercourse = strings.TrimSpace(r.FormValue("watercourse"))
	}
	notes := strings.TrimSpace(r.FormValue("notes"))

	existing, err := h.sectionService.GetSectionWithDetails(code)
	if err != nil || existing == nil {
		http.Redirect(w, r, "/sections?error=Dionica+nije+prona%C4%91ena", http.StatusSeeOther)
		return
	}

	// Ugroženo područje se više ne unosi ručno već se čuva automatski generirano
	protectedArea := existing.ProtectedArea

	embankments := existing.Embankments
	if embStr := strings.TrimSpace(r.FormValue("embankments_json")); embStr != "" && embStr != "[]" {
		var parsed []models.EmbankmentItem
		if err := json.Unmarshal([]byte(embStr), &parsed); err == nil && len(parsed) > 0 {
			embankments = parsed
		}
	}

	structures := existing.Structures
	if strStr := strings.TrimSpace(r.FormValue("structures_json")); strStr != "" && strStr != "[]" {
		var parsed []models.StructureItem
		if err := json.Unmarshal([]byte(strStr), &parsed); err == nil && len(parsed) > 0 {
			structures = parsed
		}
	}

	gauges := existing.Gauges
	if gagStr := strings.TrimSpace(r.FormValue("gauges_json")); gagStr != "" && gagStr != "[]" {
		var parsed []models.GaugeItem
		if err := json.Unmarshal([]byte(gagStr), &parsed); err == nil && len(parsed) > 0 {
			gauges = parsed
		}
	}

	sec := &models.Section{
		Code:          code,
		Description:   watercourse,
		ProtectedArea: protectedArea,
		Embankments:   embankments,
		Structures:    structures,
		Gauges:        gauges,
		Notes:         notes,
	}
	applySectionStructure(sec, r)

	if err := h.sectionService.UpdateSection(perms, sec); err != nil {
		redirectWith(w, r, "/sections/"+code+"/edit", "error", err.Error())
		return
	}

	redirectWith(w, r, "/sections/"+code, "success", "Izmjene su spremljene.")
}

// applySectionStructure popunjava obalu i raspon stacionaže dionice.
// Ako ih obrazac šalje, vrijede oni; inače se čitaju iz opisa istim parserom
// kojim je pročitana dokumentacija — sučelje i seed ne smiju se razilaziti.
func applySectionStructure(sec *models.Section, r *http.Request) {
	parsed := hydro.ParseSectionDescription(sec.Description)

	sec.Bank = strings.ToUpper(strings.TrimSpace(r.FormValue("bank")))
	if sec.Bank == "" {
		sec.Bank = parsed.Bank
	}

	sec.RkmFrom = parseOptionalFloat(r.FormValue("rkm_from"))
	sec.RkmTo = parseOptionalFloat(r.FormValue("rkm_to"))
	if sec.RkmFrom == nil && sec.RkmTo == nil && parsed.HasRange {
		from, to := parsed.RkmFrom, parsed.RkmTo
		sec.RkmFrom, sec.RkmTo = &from, &to
	}
}
