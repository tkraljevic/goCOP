package web

import (
	"encoding/json"
	"html/template"
	"net/http"

	"gocop/internal/models"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// Stranice registra postaja: jedna postaja i obrazac. Isti razlog kao kod
// vodotoka: puna stranica radi na telefonu i bez skripte.

// StationPageData je stranica jedne postaje ili njezina obrasca
type StationPageData struct {
	CurrentUser          *models.User
	Permissions          *models.UserPermissions
	Station              models.Station
	ZeroDatumHistoryJSON template.JS // promjene kote nule za obrazac, kao JS literal
	ExtremesJSON         template.JS // zabilježeni ekstremi za obrazac
	Sections             []models.Section
	WaterRegistry        []models.Watercourse
	CanEdit              bool
	IsEdit               bool
	SuccessMessage       string
	ErrorMessage         string
	ActiveNav            string
	ViewAsBanner
}

// SetPageTemplates daje rukovatelju predloške stranica i servise koje one trebaju
func (h *StationsHandler) SetPageTemplates(detail, form *template.Template,
	sections *service.SectionService, waters *service.WatercourseService) {
	h.tmplDetail = detail
	h.tmplForm = form
	h.sectionService = sections
	h.watercourseService = waters
}

func (h *StationsHandler) pageData(r *http.Request) StationPageData {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return StationPageData{
		CurrentUser:    currUser,
		Permissions:    perms,
		SuccessMessage: r.URL.Query().Get("success"),
		ErrorMessage:   r.URL.Query().Get("error"),
		ActiveNav:      "stations",
		ViewAsBanner:   viewBanner(r),
	}
}

// canEditStation: globalni administrator sve; ostali postaju koja im je
// mjerodavna na dionici za koju smiju pisati (isto pravilo kao u servisu)
func (h *StationsHandler) canEditStation(perms *models.UserPermissions, st models.Station) bool {
	if perms == nil {
		return false
	}
	if perms.IsGlobalAdmin {
		return true
	}
	for _, code := range st.SectionCodes {
		if perms.AllowedSections[code] {
			return true
		}
	}
	return false
}

// ShowStation prikazuje jednu postaju s pragovima, kotama i dionicama
func (h *StationsHandler) ShowStation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.pageData(r)

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	st, err := h.stationService.GetStation(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if st == nil {
		http.NotFound(w, r)
		return
	}
	data.Station = *st
	data.CanEdit = h.canEditStation(data.Permissions, *st)

	if h.sectionService != nil {
		for _, code := range st.SectionCodes {
			if sec, err := h.sectionService.GetSectionWithDetails(code); err == nil && sec != nil {
				data.Sections = append(data.Sections, *sec)
			}
		}
	}
	if data.CanEdit && h.watercourseService != nil {
		if waters, err := h.watercourseService.ListWatercourses(ctx, "", "", false); err == nil {
			data.WaterRegistry = waters
		}
	}

	if err := h.tmplDetail.ExecuteTemplate(w, "station_detail.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowStationForm prikazuje obrazac za novu postaju ili izmjenu postojeće
func (h *StationsHandler) ShowStationForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)

	if raw := r.PathValue("id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		st, err := h.stationService.GetStation(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if st == nil {
			http.NotFound(w, r)
			return
		}
		if !h.canEditStation(data.Permissions, *st) {
			http.Error(w, "Nemate pravo uređivati ovu postaju", http.StatusForbidden)
			return
		}
		data.Station = *st
		data.IsEdit = true
	} else if !data.Permissions.IsGlobalAdmin && len(data.Permissions.AllowedSections) == 0 {
		http.Error(w, "Nemate pravo dodavati postaje", http.StatusForbidden)
		return
	}

	data.ExtremesJSON = template.JS("[]")
	if b, err := json.Marshal(data.Station.Extremes); err == nil && len(data.Station.Extremes) > 0 {
		data.ExtremesJSON = template.JS(b)
	}
	data.ZeroDatumHistoryJSON = template.JS("[]")
	if b, err := json.Marshal(data.Station.ZeroDatumHistory); err == nil && len(data.Station.ZeroDatumHistory) > 0 {
		data.ZeroDatumHistoryJSON = template.JS(b)
	}
	if err := h.tmplForm.ExecuteTemplate(w, "station_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
