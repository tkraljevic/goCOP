package web

import (
	"html/template"
	"net/http"
	"strings"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Stranice registra dionica: jedna dionica sa svime što se na nju veže, i
// obrazac. Dionica je središnji zapis programa — na nju se vežu vodomjeri,
// teritorij, objekti, nasipi i ljudi — pa joj puna stranica pripada više
// nego ijednom drugom registru.

// SectionPageData je stranica jedne dionice ili njezina obrasca
type SectionPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	Section     models.Section
	CanEdit     bool

	Stations          []models.Station          // mjerodavni vodomjeri iz registra
	Criteria          []models.GaugeItem        // ostali kriteriji iz dokumentacije
	Territories       []models.SectionTerritory // pridružene teritorijalne jedinice
	Counties          []models.County           // za obrazac pridruživanja
	AvailableStations []models.Station          // postaje koje još nisu na dionici

	// obrazac
	Sectors             []models.Sector
	Areas               []models.Area
	IsEdit              bool
	DescriptionMain     string // opis bez stacionaže
	DescriptionChainage string // stacionaža, ako je izdvojena

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// SetPageTemplates daje rukovatelju predloške stranica i servise koje one trebaju
func (h *SectionsHandler) SetPageTemplates(detail, form *template.Template,
	stations *service.StationService, territories *service.TerritoryService) {
	h.tmplDetail = detail
	h.tmplForm = form
	h.stationService = stations
	h.territoryService = territories
}

func (h *SectionsHandler) pageData(r *http.Request) SectionPageData {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return SectionPageData{
		CurrentUser:    currUser,
		Permissions:    perms,
		SuccessMessage: r.URL.Query().Get("success"),
		ErrorMessage:   r.URL.Query().Get("error"),
		ActiveNav:      "sections",
		ViewAsBanner:   viewBanner(r),
	}
}

// ShowSection prikazuje jednu dionicu sa svime što se na nju veže
func (h *SectionsHandler) ShowSection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.pageData(r)

	sec, err := h.sectionService.GetSectionWithDetails(strings.TrimSpace(r.PathValue("code")))
	if err != nil || sec == nil {
		http.NotFound(w, r)
		return
	}
	data.Section = *sec
	data.CanEdit = h.sectionService.CanEditSection(data.Permissions, sec)

	if h.stationService != nil {
		if st, err := h.stationService.GetSectionStations(ctx, sec.Code); err == nil {
			data.Stations = st
		}
		if crit, err := h.stationService.GetSectionGaugeCriteria(ctx, sec.Code); err == nil {
			data.Criteria = crit
		}
		if data.CanEdit {
			onSection := map[string]bool{}
			for _, st := range data.Stations {
				onSection[st.ID.String()] = true
			}
			if all, err := h.stationService.ListStations(ctx, "", "", false); err == nil {
				for _, st := range all {
					if !onSection[st.ID.String()] {
						data.AvailableStations = append(data.AvailableStations, st)
					}
				}
			}
		}
	}
	if h.territoryService != nil {
		if terr, err := h.territoryService.GetSectionTerritories(ctx, sec.Code); err == nil {
			data.Territories = terr
		}
		if data.CanEdit {
			if counties, err := h.territoryService.ListCounties(ctx); err == nil {
				data.Counties = counties
			}
		}
	}

	if err := h.tmplDetail.ExecuteTemplate(w, "section_detail.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowSectionForm prikazuje obrazac za novu dionicu ili izmjenu postojeće
func (h *SectionsHandler) ShowSectionForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)

	if code := strings.TrimSpace(r.PathValue("code")); code != "" {
		sec, err := h.sectionService.GetSectionWithDetails(code)
		if err != nil || sec == nil {
			http.NotFound(w, r)
			return
		}
		if !h.sectionService.CanEditSection(data.Permissions, sec) {
			http.Error(w, "Nemate pravo uređivati ovu dionicu", http.StatusForbidden)
			return
		}
		data.Section = *sec
		data.IsEdit = true
		data.DescriptionMain, data.DescriptionChainage = splitChainage(sec.Description)
	} else {
		perms := data.Permissions
		canCreate := perms != nil && (perms.IsGlobalAdmin || len(perms.AdminSectors) > 0 ||
			len(perms.AdminAreas) > 0 || len(perms.AllowedSectors) > 0)
		if !canCreate {
			http.Error(w, "Nemate pravo dodavati dionice", http.StatusForbidden)
			return
		}
		data.Sectors, _ = h.userService.ListSectors()
		data.Areas, _ = h.userService.ListAreas("")
	}

	if err := h.tmplForm.ExecuteTemplate(w, "section_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// splitChainage odvaja stacionažu od ostatka opisa dionice: obrazac je nudi
// kao zasebno polje, a u zapis se vraća spojena istim pravilom kao pri upisu.
// Stacionaža počinje prvim dijelom koji nosi kilometražu (rkm, km, pkm, kkm)
// i teče do kraja, pa i duljina u zagradi iza nje ostaje uz nju.
func splitChainage(description string) (main, chainage string) {
	parts := strings.Split(description, ";")
	start := -1
	for i, part := range parts {
		lower := strings.ToLower(strings.TrimSpace(part))
		for _, prefix := range []string{"rkm", "pkm", "kkm", "km ", "km+", "stac"} {
			if strings.HasPrefix(lower, prefix) {
				start = i
				break
			}
		}
		if start >= 0 {
			break
		}
	}
	if start <= 0 {
		return strings.TrimSpace(description), ""
	}
	return strings.TrimSpace(strings.Join(parts[:start], ";")),
		strings.TrimSpace(strings.Join(parts[start:], ";"))
}
