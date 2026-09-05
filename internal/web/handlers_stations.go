package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/service"
)

type StationsHandler struct {
	stationService     *service.StationService
	sectionService     *service.SectionService
	watercourseService *service.WatercourseService
	tmpl               *template.Template // popis
	tmplDetail         *template.Template // jedna postaja
	tmplForm           *template.Template // obrazac
}

func NewStationsHandler(stationService *service.StationService, tmpl *template.Template) *StationsHandler {
	return &StationsHandler{
		stationService: stationService,
		tmpl:           tmpl,
	}
}

type StationsPageData struct {
	CurrentUser        *models.User
	Permissions        *models.UserPermissions
	Stations           []models.Station
	Watercourses       []string
	SearchQuery        string
	SelectedRiver      string
	OnlyNeedsReview    bool
	TotalStations      int
	NeedsReview        int
	SectionLinks       int
	WithoutWatercourse int
	SuccessMessage     string
	ErrorMessage       string
	ActiveNav          string
	Pager              Pager
	ViewAsBanner
}

// ShowStations prikazuje registar vodomjernih postaja
func (h *StationsHandler) ShowStations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)

	search := strings.TrimSpace(r.URL.Query().Get("q"))
	river := strings.TrimSpace(r.URL.Query().Get("watercourse"))
	onlyReview := r.URL.Query().Get("review") == "1"

	data := StationsPageData{
		CurrentUser:     currUser,
		Permissions:     perms,
		SearchQuery:     search,
		SelectedRiver:   river,
		OnlyNeedsReview: onlyReview,
		SuccessMessage:  r.URL.Query().Get("success"),
		ErrorMessage:    r.URL.Query().Get("error"),
		ActiveNav:       "stations",
		ViewAsBanner:    viewBanner(r),
	}

	stations, err := h.stationService.ListStations(ctx, search, river, onlyReview)
	if err != nil {
		data.ErrorMessage = err.Error()
	}
	for _, st := range stations {
		if !st.HasWatercourse() {
			data.WithoutWatercourse++
		}
	}
	data.Stations, data.Pager = paginate(stations, r, registryPerPage)

	if rivers, err := h.stationService.ListWatercourses(ctx); err == nil {
		data.Watercourses = rivers
	}
	if total, review, links, err := h.stationService.Counts(ctx); err == nil {
		data.TotalStations, data.NeedsReview, data.SectionLinks = total, review, links
	}
	if err := h.tmpl.ExecuteTemplate(w, "stations.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// sectionStations dohvaća mjerodavne vodomjere dionice i retke dokumentacije
// koji nisu postaje nego kriteriji obrane
func (h *StationsHandler) sectionStations(ctx context.Context, code string) ([]models.Station, []models.GaugeItem, error) {
	stations, err := h.stationService.GetSectionStations(ctx, code)
	if err != nil {
		return nil, nil, err
	}
	criteria, err := h.stationService.GetSectionGaugeCriteria(ctx, code)
	if err != nil {
		return nil, nil, err
	}
	return stations, criteria, nil
}

// HandleListStationsAPI vraća registar postaja u JSON obliku, za odabir
// vodomjera koji se dodaje na dionicu
func (h *StationsHandler) HandleListStationsAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	search := strings.TrimSpace(r.URL.Query().Get("q"))
	river := strings.TrimSpace(r.URL.Query().Get("watercourse"))

	stations, err := h.stationService.ListStations(ctx, search, river, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{"success": true, "stations": stations})
}

// HandleGetSectionStationsAPI vraća mjerodavne vodomjere dionice
func (h *StationsHandler) HandleGetSectionStationsAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	code := r.PathValue("code")
	if code == "" {
		http.Error(w, "Šifra dionice je obavezna", http.StatusBadRequest)
		return
	}

	stations, criteria, err := h.sectionStations(ctx, code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeStationsJSON(w, code, stations, criteria)
}

// HandleCreateStationAPI unosi novu vodomjernu postaju u registar
func (h *StationsHandler) HandleCreateStationAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)

	if currUser == nil {
		http.Error(w, "Neautorizirani pristup", http.StatusUnauthorized)
		return
	}

	form, err := decodeStationForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	station := form.toStation()
	if err := h.stationService.CreateStation(ctx, perms, &station, form.SectionCode); err != nil {
		if wantsPage(r) {
			redirectWith(w, r, "/stations/new", "error", err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if wantsPage(r) {
		redirectWith(w, r, "/stations/"+station.ID.String(), "success", "Postaja je upisana u registar.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{"success": true, "station": station})
}

// HandleUpdateStationAPI mijenja podatke postaje u registru
func (h *StationsHandler) HandleUpdateStationAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)

	if currUser == nil {
		http.Error(w, "Neautorizirani pristup", http.StatusUnauthorized)
		return
	}

	form, err := decodeStationForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	stationID, err := uuid.Parse(strings.TrimSpace(form.ID))
	if err != nil {
		http.Error(w, "Neispravan identifikator postaje", http.StatusBadRequest)
		return
	}

	station := form.toStation()
	station.ID = stationID

	if err := h.stationService.UpdateStation(ctx, perms, &station); err != nil {
		if wantsPage(r) {
			redirectWith(w, r, "/stations/"+stationID.String()+"/edit", "error", err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if wantsPage(r) {
		redirectWith(w, r, "/stations/"+stationID.String(), "success", "Izmjene su spremljene.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{"success": true, "station": station})
}

// HandleDeleteStationAPI briše postaju iz registra
func (h *StationsHandler) HandleDeleteStationAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)

	if currUser == nil {
		http.Error(w, "Neautorizirani pristup", http.StatusUnauthorized)
		return
	}

	stationIDs, err := parseStationIDs(r)
	if err != nil || len(stationIDs) != 1 {
		http.Error(w, "Neispravan identifikator postaje", http.StatusBadRequest)
		return
	}

	if err := h.stationService.DeleteStation(ctx, perms, stationIDs[0]); err != nil {
		if wantsPage(r) {
			redirectWith(w, r, "/stations/"+stationIDs[0].String(), "error", err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	if wantsPage(r) {
		redirectWith(w, r, "/stations", "success", "Postaja je obrisana iz registra; zapis ostaje u povijesti.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{"success": true})
}

func writeStationsJSON(w http.ResponseWriter, sectionCode string, stations []models.Station, criteria []models.GaugeItem) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{
		"success":      true,
		"section_code": sectionCode,
		"stations":     stations,
		"criteria":     criteria,
	})
}

// parseStationIDs čita identifikatore postaja iz JSON tijela ili obrasca
func parseStationIDs(r *http.Request) ([]uuid.UUID, error) {
	var raw []string

	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var req struct {
			StationID  string   `json:"station_id"`
			StationIDs []string `json:"station_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, errBadJSON(err)
		}
		raw = req.StationIDs
		if req.StationID != "" {
			raw = append(raw, req.StationID)
		}
	} else {
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		raw = r.Form["station_ids"]
		if single := r.FormValue("station_id"); single != "" {
			raw = append(raw, single)
		}
	}

	var ids []uuid.UUID
	for _, value := range raw {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parsed, err := uuid.Parse(value)
		if err != nil {
			return nil, errBadStationID(value)
		}
		ids = append(ids, parsed)
	}

	return ids, nil
}

// stationForm su podaci obrasca za unos i izmjenu postaje
type stationForm struct {
	ID                 string `json:"id"`
	SectionCode        string `json:"section_code"`
	Code               string `json:"code"`
	Name               string `json:"name"`
	Watercourse        string `json:"watercourse"`
	WaterArea          string `json:"water_area"`
	Stationing         string `json:"stationing"`
	ZeroDatum          string `json:"zero_datum"`
	ZeroDatumSystem    string `json:"zero_datum_system"`
	ZeroDatumNew       string `json:"zero_datum_new"`
	ZeroDatumNewSystem string `json:"zero_datum_new_system"`
	Prep               string `json:"prep"`
	Regular            string `json:"regular"`
	Emergency          string `json:"emergency"`
	State              string `json:"state"`
	Record             string `json:"record"`
	Notes              string `json:"notes"`
}

func decodeStationForm(r *http.Request) (stationForm, error) {
	var form stationForm

	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&form); err != nil {
			return form, errBadJSON(err)
		}
		return form, nil
	}

	if err := r.ParseForm(); err != nil {
		return form, err
	}

	form.ID = r.FormValue("id")
	form.SectionCode = r.FormValue("section_code")
	form.Code = r.FormValue("code")
	form.Name = r.FormValue("name")
	form.Watercourse = r.FormValue("watercourse")
	form.WaterArea = r.FormValue("water_area")
	form.Stationing = r.FormValue("stationing")
	form.ZeroDatum = r.FormValue("zero_datum")
	form.ZeroDatumSystem = r.FormValue("zero_datum_system")
	form.ZeroDatumNew = r.FormValue("zero_datum_new")
	form.ZeroDatumNewSystem = r.FormValue("zero_datum_new_system")
	form.Prep = r.FormValue("prep")
	form.Regular = r.FormValue("regular")
	form.Emergency = r.FormValue("emergency")
	form.State = r.FormValue("state")
	form.Record = r.FormValue("record")
	form.Notes = r.FormValue("notes")

	return form, nil
}

func (f stationForm) toStation() models.Station {
	return models.Station{
		Code:               strings.TrimSpace(f.Code),
		Name:               strings.TrimSpace(f.Name),
		Watercourse:        strings.TrimSpace(f.Watercourse),
		WaterArea:          strings.TrimSpace(f.WaterArea),
		Stationing:         strings.TrimSpace(f.Stationing),
		ZeroDatum:          parseOptionalFloat(f.ZeroDatum),
		ZeroDatumSystem:    strings.TrimSpace(f.ZeroDatumSystem),
		ZeroDatumNew:       parseOptionalFloat(f.ZeroDatumNew),
		ZeroDatumNewSystem: strings.TrimSpace(f.ZeroDatumNewSystem),
		Prep:               parseThresholdInput(f.Prep),
		Regular:            parseThresholdInput(f.Regular),
		Emergency:          parseThresholdInput(f.Emergency),
		State:              parseThresholdInput(f.State),
		Record:             parseThresholdInput(f.Record),
		Notes:              strings.TrimSpace(f.Notes),
	}
}

// parseThresholdInput prihvaća prag u centimetrima, a svaki drugi zapis
// (kota, uputa iz pravilnika) zadržava kao tekst izvan automatskog izračuna
func parseThresholdInput(value string) models.Threshold {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return models.Threshold{}
	}

	cleaned := strings.ReplaceAll(strings.TrimPrefix(trimmed, "+"), " ", "")
	if parsed, err := strconv.Atoi(cleaned); err == nil {
		return models.Threshold{Cm: &parsed, Raw: trimmed}
	}

	return models.Threshold{Raw: trimmed}
}

func parseOptionalFloat(value string) *float64 {
	trimmed := strings.ReplaceAll(strings.TrimSpace(value), ",", ".")
	if trimmed == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func errBadJSON(err error) error {
	return fmt.Errorf("neispravan JSON: %w", err)
}

func errBadStationID(value string) error {
	return fmt.Errorf("neispravan identifikator postaje: %s", value)
}
