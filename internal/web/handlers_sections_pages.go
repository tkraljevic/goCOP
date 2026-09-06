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

// Stranice registra dionica: jedna dionica sa svime što se na nju veže, i
// obrazac. Dionica je središnji zapis programa — na nju se vežu vodomjeri,
// teritorij, objekti, nasipi i ljudi — pa joj puna stranica pripada više
// nego ijednom drugom registru.

// PartView je poddionica s razriješenim vezama za prikaz
type PartView struct {
	models.SectionPart
	Stations    []models.Station
	Territories []models.SectionTerritory
	Criteria    []models.GaugeItem // zapisi iz dokumentacije koji nisu postaje
	Rows        []EmbankmentRow    // nasipi s objektima koji na njima leže, kao u Privitku
}

// SectionPageData je stranica jedne dionice ili njezina obrasca
type SectionPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	Section     models.Section
	Parts       []PartView
	CanEdit     bool

	// obrazac
	Sectors         []models.Sector
	Areas           []models.Area
	IsEdit          bool
	Watercourses    []models.Watercourse
	Stations        []models.Station
	Structures      []models.Structure // objekti područja koji nisu nasipi
	Embankments     []models.Structure // nasipi i brane područja
	Counties        []models.County
	StationingKinds []string
	Banks           []struct{ Code, Label string }
	SectionJSON     template.JS // dionica za obrazac
	TerritoryLabels template.JS // ključ → naziv, za čipove u obrascu

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// SetStructureService daje rukovatelju registar objekata
func (h *SectionsHandler) SetStructureService(structures *service.StructureService) {
	h.structureService = structures
}

// SetWatercourseService daje rukovatelju registar voda
func (h *SectionsHandler) SetWatercourseService(waters *service.WatercourseService) {
	h.watercourseService = waters
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
		CurrentUser: currUser, Permissions: perms,
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "sections", ViewAsBanner: viewBanner(r),
		StationingKinds: hydro.StationingKinds, Banks: models.Banks,
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

	// registri koje poddionice spominju, dohvaćeni jednom
	stationByID := map[string]models.Station{}
	if h.stationService != nil {
		if st, err := h.stationService.GetSectionStations(ctx, sec.Code); err == nil {
			for _, s := range st {
				stationByID[s.ID.String()] = s
			}
		}
	}
	terrByKey := map[string]models.SectionTerritory{}
	if h.territoryService != nil {
		if terr, err := h.territoryService.GetSectionTerritories(ctx, sec.Code); err == nil {
			for _, t := range terr {
				terrByKey[models.PartTerritory{CountyID: t.CountyID, MunicipalityID: t.MunicipalityID, SettlementID: t.SettlementID}.Key()] = t
			}
		}
	}
	for _, p := range sec.Parts {
		v := PartView{SectionPart: p, Rows: embankmentRows(p)}
		for _, id := range p.StationIDs {
			if s, ok := stationByID[id]; ok {
				v.Stations = append(v.Stations, s)
			}
		}
		for _, t := range p.Territories {
			if x, ok := terrByKey[t.Key()]; ok {
				v.Territories = append(v.Territories, x)
			}
		}
		for _, g := range p.Gauges {
			if !g.IsGauge() || !coveredBy(v.Stations, g) {
				v.Criteria = append(v.Criteria, g)
			}
		}
		data.Parts = append(data.Parts, v)
	}

	if err := h.tmplDetail.ExecuteTemplate(w, "section_detail.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// coveredBy javlja je li vodomjer iz dokumentacije već prikazan kao postaja
func coveredBy(stations []models.Station, g models.GaugeItem) bool {
	name, _ := hydro.ParseStationName(g.StationName)
	key := hydro.StationKey(name)
	if key == "" {
		key = hydro.StationKey(g.StationName)
	}
	for _, s := range stations {
		if hydro.StationKey(s.Name) == key || strings.EqualFold(strings.TrimSpace(s.SourceName), strings.TrimSpace(g.StationName)) {
			return true
		}
	}
	return false
}

// ShowSectionForm prikazuje obrazac za novu dionicu ili izmjenu postojeće
func (h *SectionsHandler) ShowSectionForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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
	} else {
		perms := data.Permissions
		canCreate := perms != nil && (perms.IsGlobalAdmin || len(perms.AdminSectors) > 0 ||
			len(perms.AdminAreas) > 0 || len(perms.AllowedSectors) > 0)
		if !canCreate {
			http.Error(w, "Nemate pravo dodavati dionice", http.StatusForbidden)
			return
		}
		data.Section = models.Section{Parts: []models.SectionPart{{Seq: 1}}}
		if a, _ := strconv.Atoi(r.URL.Query().Get("area")); a > 0 {
			data.Section.AreaID = a
		}
		data.Sectors, _ = h.userService.ListSectors()
	}
	data.Areas, _ = h.userService.ListAreas("")

	if h.watercourseService != nil {
		data.Watercourses, _ = h.watercourseService.ListWatercourses(ctx, "", "", false)
	}
	if h.stationService != nil {
		data.Stations, _ = h.stationService.ListStations(ctx, "", "", false)
	}
	if h.structureService != nil && data.Section.AreaID > 0 {
		if all, err := h.structureService.List(ctx, "", data.Section.AreaID, "", ""); err == nil {
			for _, s := range all {
				if s.Kind == models.StructureKindEmbankment || s.Kind == models.StructureKindDam {
					data.Embankments = append(data.Embankments, s)
				} else {
					data.Structures = append(data.Structures, s)
				}
			}
		}
	}
	if h.territoryService != nil {
		data.Counties, _ = h.territoryService.ListCounties(ctx)
		labels := map[string]string{}
		if data.IsEdit {
			if terr, err := h.territoryService.GetSectionTerritories(ctx, data.Section.Code); err == nil {
				for _, t := range terr {
					key := models.PartTerritory{CountyID: t.CountyID, MunicipalityID: t.MunicipalityID, SettlementID: t.SettlementID}.Key()
					if t.SettlementName != "" {
						labels[key] = t.SettlementName + " (" + t.MunicipalityName + ")"
					} else {
						labels[key] = strings.TrimSpace(models.MunicipalityTypeLabel(t.MunicipalityType) + " " + t.MunicipalityName)
					}
				}
			}
		}
		if b, err := json.Marshal(labels); err == nil {
			data.TerritoryLabels = template.JS(b)
		}
	}
	if b, err := json.Marshal(data.Section); err == nil {
		data.SectionJSON = template.JS(b)
	}

	if err := h.tmplForm.ExecuteTemplate(w, "section_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
