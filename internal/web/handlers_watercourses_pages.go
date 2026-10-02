package web

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
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

// WatercourseStationMapItem predstavlja postaju prikazanu na karti vodotoka
type WatercourseStationMapItem struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Stationing string   `json:"stationing"`
	Country    string   `json:"country"`
	Lat        *float64 `json:"lat"`
	Lon        *float64 `json:"lon"`
	DetailURL  string   `json:"detail_url"`
}

// WatercoursePageData je stranica jedne vode ili njezina obrasca
type WatercoursePageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	Water       models.Watercourse
	Sections    []models.Section
	Stations    []models.Station
	// DodatneLetve su letve s drugih voda mjerodavne i za ovu (Batina uz
	// baranjsku Karašicu); SveLetve nudi obrazac za dodavanje
	DodatneLetve []models.Station
	SveLetve     []models.Station
	// NapomenaToka je tekst uz crtu toka na karti; uređuje se kad je tok
	// upisan u bazi, a nije Dunav ili Drava, kojima ga piše kalibracija ENC-a
	NapomenaToka          string
	NapomenaTokaUredljiva bool
	Kinds                 []string
	Maintenance           []models.MaintainedWater // popisi lokacija u kojima se voda održava
	IsEdit                bool
	SuccessMessage        string
	ErrorMessage          string
	ActiveNav             string
	ViewAsBanner
	Karta           KartaPostavke
	GeometryJSON    template.JS
	MapStationsJSON template.JS
}

// watercourseKinds su vrste voda koje obrazac nudi
var watercourseKinds = []string{"rijeka", "potok", "kanal", "prokop", "jezero", "akumulacija", "retencija", "rukavac", "bujica"}

// SetMaintenanceService daje rukovatelju pristup popisu održavanih voda
func (h *WatercoursesHandler) SetMaintenanceService(m *service.MaintenanceService) {
	h.maintenanceService = m
}

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
	var kp KartaPostavke
	if h.karta != nil {
		kp = h.karta()
	}
	return WatercoursePageData{
		CurrentUser:    currUser,
		Permissions:    perms,
		Kinds:          watercourseKinds,
		SuccessMessage: r.URL.Query().Get("success"),
		ErrorMessage:   r.URL.Query().Get("error"),
		ActiveNav:      "watercourses",
		ViewAsBanner:   viewBanner(r),
		Karta:          kp,
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
	h.napuniLetve(ctx, &data)

	if geom, err := h.watercourseService.GetWatercourseGeometry(ctx, water.Code); err == nil && len(geom) > 0 {
		data.GeometryJSON, _ = jsonZaSkriptu(geom)
	}

	var mapStations []WatercourseStationMapItem
	for _, s := range append(append([]models.Station(nil), data.Stations...), data.DodatneLetve...) {
		if s.ImaKoordinate() {
			idStr := s.ID.String()
			mapStations = append(mapStations, WatercourseStationMapItem{
				ID:         idStr,
				Name:       s.Name,
				Stationing: s.Stationing,
				Country:    s.Zemlja(),
				Lat:        s.Latitude,
				Lon:        s.Longitude,
				DetailURL:  "/stations/" + idStr,
			})
		}
	}
	if len(mapStations) > 0 {
		if b, err := json.Marshal(mapStations); err == nil {
			data.MapStationsJSON = template.JS(b)
		}
	}

	if h.maintenanceService != nil {
		data.Maintenance, _ = h.maintenanceService.WatersFor(ctx, water.Code, "")
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
		data.NapomenaToka = service.NapomenaToka(water.Geometry)
		data.NapomenaTokaUredljiva = napomenaTokaUredljiva(*water)
		h.napuniLetve(r.Context(), &data)
		if h.stationService != nil {
			data.SveLetve, _ = h.stationService.ListStations(r.Context(), "", "", "", false)
		}
	}

	if err := h.tmplForm.ExecuteTemplate(w, "watercourse_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// napomenaTokaUredljiva: napomenu uz tok Dunava i Drave piše kalibracija
// prema ENC-u, pa bi upisana ionako bila prepisana pri prikazu
func napomenaTokaUredljiva(w models.Watercourse) bool {
	return strings.TrimSpace(w.Geometry) != "" && w.Code != "rijeka-dunav" && w.Code != "rijeka-drava"
}

// napuniLetve puni letve vode: vlastite (letva nosi ovu vodu) i dodatne s
// drugih voda koje je voda sama navela
func (h *WatercoursesHandler) napuniLetve(ctx context.Context, data *WatercoursePageData) {
	if h.stationService == nil {
		return
	}
	water := data.Water
	if st, err := h.stationService.ListStations(ctx, "", water.Name, "", false); err == nil {
		// popis po nazivu vode hvata i istoimene vode; zadrži samo one s ovom šifrom ili povezanim nazivom
		for _, s := range st {
			if s.WatercourseCode == water.Code || (s.WatercourseCode == "" && s.Watercourse == water.Name) {
				data.Stations = append(data.Stations, s)
			}
		}
	}
	for _, id := range water.ExtraStationIDs {
		uid, err := uuid.Parse(id)
		if err != nil {
			continue
		}
		if s, err := h.stationService.GetStation(ctx, uid); err == nil && s != nil {
			data.DodatneLetve = append(data.DodatneLetve, *s)
		}
	}
}

// jsonZaSkriptu priprema spremljeni JSON za <script type="application/json">.
// html/template vrijednost tipa template.JS ne escapira, a geometrija vode je
// tekst kako je stigao (učitana datoteka, razmjena s drugim čvorom): niz
// "</script>" u nekom svojstvu zatvorio bi oznaku i ostatak bi se izvršio kao
// skripta. HTMLEscape piše <, > i & kao \u003c…, što je isti JSON. Neispravan
// JSON se ne prikazuje.
func jsonZaSkriptu(raw []byte) (template.JS, bool) {
	if !json.Valid(raw) {
		return "", false
	}
	var b bytes.Buffer
	json.HTMLEscape(&b, raw)
	return template.JS(b.String()), true
}

// wantsPage javlja je li zahtjev došao iz običnog HTML obrasca, kojem se
// odgovara preusmjeravanjem, a ne iz skripte koja čeka JSON
func wantsPage(r *http.Request) bool {
	return !jsonTijelo(r)
}

// redirectWith preusmjerava na stranicu s porukom u upitu
func redirectWith(w http.ResponseWriter, r *http.Request, path, key, msg string) {
	q := url.Values{}
	q.Set(key, msg)
	// sidro ostaje na kraju: upit ide prije njega
	path, fragment, _ := strings.Cut(path, "#")
	if fragment != "" {
		fragment = "#" + fragment
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	http.Redirect(w, r, path+sep+q.Encode()+fragment, http.StatusSeeOther)
}
