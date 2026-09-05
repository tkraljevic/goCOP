package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"gocop/internal/models"
	"gocop/internal/service"
)

type SectionItemView struct {
	models.Section
	CanEdit bool `json:"can_edit"`
}

type SectionsHandler struct {
	sectionService     *service.SectionService
	userService        *service.UserService
	stationService     *service.StationService
	territoryService   *service.TerritoryService
	structureService   *service.StructureService
	watercourseService *service.WatercourseService
	tmpl               *template.Template // popis
	tmplDetail         *template.Template // jedna dionica
	tmplForm           *template.Template // obrazac
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
	searchQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	areaFilter, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("area")))

	rawSections, err := h.sectionService.ListSections(sectorFilter, areaFilter, searchQuery)
	if err != nil {
		http.Error(w, "Greška pri dohvatu dionica: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var sections []SectionItemView
	for _, s := range rawSections {
		sections = append(sections, SectionItemView{Section: s, CanEdit: h.sectionService.CanEditSection(perms, &s)})
	}

	sectors, _ := h.userService.ListSectors()
	areas, _ := h.userService.ListAreas(sectorFilter)

	canCreate := false
	if perms != nil {
		canCreate = perms.IsGlobalAdmin || len(perms.AdminSectors) > 0 || len(perms.AdminAreas) > 0 || len(perms.AllowedSectors) > 0
	}

	page, pager := paginate(sections, r, registryPerPage)
	data := SectionsPageData{
		CurrentUser: currUser, Permissions: perms, Sections: page, Pager: pager,
		Sectors: sectors, Areas: areas, SelectedSector: sectorFilter, SelectedArea: areaFilter, SearchQuery: searchQuery,
		CanCreateAny: canCreate, SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "sections", ViewAsBanner: viewBanner(r),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "sections.html", data); err != nil {
		http.Error(w, "Greška pri renderiranju dionica: "+err.Error(), http.StatusInternalServerError)
	}
}

// HandleGetSectionAPI vraća detaljne podatke pojedinačne dionice s osobljem kao JSON
func (h *SectionsHandler) HandleGetSectionAPI(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.PathValue("code"))
	if code == "" {
		code = strings.TrimSpace(r.URL.Query().Get("code"))
	}
	sec, err := h.sectionService.GetSectionWithDetails(code)
	if err != nil || sec == nil {
		http.Error(w, `{"error":"Dionica nije pronađena"}`, http.StatusNotFound)
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	resp := struct {
		*models.Section
		CanEdit bool `json:"can_edit"`
	}{Section: sec, CanEdit: h.sectionService.CanEditSection(perms, sec)}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// sectionFromForm čita dionicu iz obrasca: zaglavlje iz polja, poddionice iz
// JSON-a koji obrazac slaže iz svojih redaka
func sectionFromForm(r *http.Request) (*models.Section, error) {
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	sec := &models.Section{
		Code: strings.ToUpper(f("code")), Notes: f("notes"),
		Description: f("description"), DescriptionCustom: r.FormValue("description_custom") == "1",
		LengthKm: parseOptionalFloat(f("length_km")), EmbankmentKm: parseOptionalFloat(f("embankment_km")),
	}
	sec.AreaID, _ = strconv.Atoi(f("area_id"))
	sec.SectorID = strings.ToUpper(f("sector_id"))
	if raw := f("parts_json"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &sec.Parts); err != nil {
			return nil, err
		}
	}
	for i := range sec.Parts {
		p := &sec.Parts[i]
		p.Seq = i + 1
		p.Bank = strings.ToUpper(strings.TrimSpace(p.Bank))
		p.Extent = strings.TrimSpace(p.Extent)
		p.WatercourseCode = strings.TrimSpace(p.WatercourseCode)
		// prazan zapis nema što nositi
		objs := p.Objects[:0]
		for _, o := range p.Objects {
			o.Name = strings.TrimSpace(o.Name)
			if o.Name != "" || o.StructureID != "" {
				objs = append(objs, o)
			}
		}
		p.Objects = objs
		embs := p.Embankments[:0]
		for _, e := range p.Embankments {
			e.Name = strings.TrimSpace(e.Name)
			if e.Name != "" || e.StructureID != "" {
				embs = append(embs, e)
			}
		}
		p.Embankments = embs
	}
	return sec, nil
}

// HandleCreateSection kreira novu dionicu
func (h *SectionsHandler) HandleCreateSection(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/sections/new", "error", "Neispravan zahtjev")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	sec, err := sectionFromForm(r)
	if err != nil {
		redirectWith(w, r, "/sections/new", "error", "Poddionice nisu čitljive: "+err.Error())
		return
	}
	if err := h.sectionService.SaveSection(r.Context(), perms, sec, true); err != nil {
		redirectWith(w, r, "/sections/new?area="+strconv.Itoa(sec.AreaID), "error", err.Error())
		return
	}
	redirectWith(w, r, "/sections/"+sec.Code, "success", "Dionica "+sec.Code+" je upisana.")
}

// HandleUpdateSection ažurira postojeću dionicu
func (h *SectionsHandler) HandleUpdateSection(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/sections", "error", "Neispravan zahtjev")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	sec, err := sectionFromForm(r)
	if err != nil {
		redirectWith(w, r, "/sections", "error", "Poddionice nisu čitljive: "+err.Error())
		return
	}
	if err := h.sectionService.SaveSection(r.Context(), perms, sec, false); err != nil {
		redirectWith(w, r, "/sections/"+sec.Code+"/edit", "error", err.Error())
		return
	}
	redirectWith(w, r, "/sections/"+sec.Code, "success", "Izmjene su spremljene.")
}
