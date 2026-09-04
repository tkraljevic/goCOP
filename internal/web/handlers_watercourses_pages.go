package web

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Stranice registra vodnih tijela: popis, pojedina voda i obrazac.
//
// Sve tri su pune stranice, ne skočni prozori: na telefonu je prozorčić s
// obrascem mučenje, a puna stranica radi bez ijednog retka skripte. Upis ide
// običnim obrascem na isto sučelje koje koriste i JSON pozivi; razlika je
// samo u tome što obrazac dobije preusmjeravanje umjesto JSON odgovora.

// WatercoursePageData je stranica jedne vode ili njezina obrasca
type WatercoursePageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	Water          models.Watercourse
	Sections       []models.Section
	Stations       []models.Station
	Kinds          []string
	IsEdit         bool
	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// watercourseKinds su vrste voda koje obrazac nudi
var watercourseKinds = []string{"rijeka", "potok", "kanal", "prokop", "jezero", "akumulacija", "retencija", "rukavac", "bujica"}

// SetPageTemplates daje rukovatelju predloške stranica pojedine vode i obrasca
func (h *WatercoursesHandler) SetPageTemplates(detail, form *template.Template, stations *service.StationService) {
	h.tmplDetail = detail
	h.tmplForm = form
	h.stationService = stations
}

func (h *WatercoursesHandler) pageData(r *http.Request) WatercoursePageData {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return WatercoursePageData{
		CurrentUser:    currUser,
		Permissions:    perms,
		Kinds:          watercourseKinds,
		SuccessMessage: r.URL.Query().Get("success"),
		ErrorMessage:   r.URL.Query().Get("error"),
		ActiveNav:      "watercourses",
		ViewAsBanner:   viewBanner(r),
	}
}

// ShowWatercourse prikazuje jednu vodu s dionicama i postajama koje se na nju vežu
func (h *WatercoursesHandler) ShowWatercourse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.pageData(r)

	water, err := h.watercourseService.GetWatercourse(ctx, r.PathValue("code"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if water == nil {
		http.NotFound(w, r)
		return
	}
	data.Water = *water

	if codes, err := h.watercourseService.SectionsForWatercourse(ctx, water.Code); err == nil {
		for _, code := range codes {
			if sec, err := h.sectionService.GetSectionWithDetails(code); err == nil && sec != nil {
				data.Sections = append(data.Sections, *sec)
			}
		}
	}
	if h.stationService != nil {
		if st, err := h.stationService.ListStations(ctx, "", water.Name, false); err == nil {
			// popis po nazivu vode hvata i istoimene vode; zadrži samo one s ovom šifrom
			for _, s := range st {
				if s.WatercourseCode == water.Code {
					data.Stations = append(data.Stations, s)
				}
			}
		}
	}

	if err := h.tmplDetail.ExecuteTemplate(w, "watercourse_detail.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowWatercourseForm prikazuje obrazac za novu vodu ili za izmjenu postojeće
func (h *WatercoursesHandler) ShowWatercourseForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if !data.Permissions.IsGlobalAdmin {
		http.Error(w, "Registar vodnih tijela uređuje globalni administrator", http.StatusForbidden)
		return
	}

	if code := r.PathValue("code"); code != "" {
		water, err := h.watercourseService.GetWatercourse(r.Context(), code)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if water == nil {
			http.NotFound(w, r)
			return
		}
		data.Water = *water
		data.IsEdit = true
	}

	if err := h.tmplForm.ExecuteTemplate(w, "watercourse_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// wantsPage javlja je li zahtjev došao iz običnog HTML obrasca, kojem se
// odgovara preusmjeravanjem, a ne iz skripte koja čeka JSON
func wantsPage(r *http.Request) bool {
	return !strings.Contains(r.Header.Get("Content-Type"), "application/json")
}

// redirectWith preusmjerava na stranicu s porukom u upitu
func redirectWith(w http.ResponseWriter, r *http.Request, path, key, msg string) {
	q := url.Values{}
	q.Set(key, msg)
	http.Redirect(w, r, path+"?"+q.Encode(), http.StatusSeeOther)
}
