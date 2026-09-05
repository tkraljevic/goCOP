package web

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"gocop/internal/models"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// Registar objekata: popis, jedan objekt, obrazac. Isti uzorak kao ostali
// registri — kartice s listanjem, pune stranice, obični obrasci.

type StructuresHandler struct {
	structureService *service.StructureService
	stationService   *service.StationService
	sectionService   *service.SectionService
	userService      *service.UserService
	tmplList         *template.Template
	tmplDetail       *template.Template
	tmplForm         *template.Template
}

func NewStructuresHandler(structures *service.StructureService, stations *service.StationService,
	sections *service.SectionService, users *service.UserService, list, detail, form *template.Template) *StructuresHandler {
	return &StructuresHandler{structureService: structures, stationService: stations, sectionService: sections,
		userService: users, tmplList: list, tmplDetail: detail, tmplForm: form}
}

type StructuresPageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	Structures     []models.Structure
	Sectors        []models.Sector
	Areas          []models.Area
	Kinds          []string
	SelectedSector string
	SelectedArea   int
	SelectedKind   string
	SearchQuery    string
	CanCreate      bool
	Total          int
	ByKind         map[string]int
	Pager          Pager
	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

type StructurePageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	Structure   *models.Structure
	Sections    []models.Section
	Station     *models.Station
	CanEdit     bool
	IsEdit      bool

	Sectors  []models.Sector
	Areas    []models.Area
	Kinds    []string
	Stations []models.Station // za odabir vodomjera na obrascu

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

func (h *StructuresHandler) base(r *http.Request) (*models.User, *models.UserPermissions) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	p, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	return u, p
}

// ShowStructures prikazuje registar objekata
func (h *StructuresHandler) ShowStructures(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, perms := h.base(r)
	q := r.URL.Query()
	areaID, _ := strconv.Atoi(q.Get("area"))
	data := StructuresPageData{
		CurrentUser: u, Permissions: perms, Kinds: models.StructureKinds,
		SelectedSector: strings.TrimSpace(q.Get("sector")), SelectedArea: areaID,
		SelectedKind: strings.TrimSpace(q.Get("kind")), SearchQuery: strings.TrimSpace(q.Get("q")),
		CanCreate: h.structureService.CanCreate(perms), ByKind: map[string]int{},
		SuccessMessage: q.Get("success"), ErrorMessage: q.Get("error"),
		ActiveNav: "structures", ViewAsBanner: viewBanner(r),
	}
	all, err := h.structureService.List(ctx, data.SelectedSector, data.SelectedArea, data.SelectedKind, data.SearchQuery)
	if err != nil {
		data.ErrorMessage = err.Error()
	}
	if everything, err := h.structureService.List(ctx, "", 0, "", ""); err == nil {
		data.Total = len(everything)
		for _, s := range everything {
			data.ByKind[s.Kind]++
		}
	}
	data.Structures, data.Pager = paginate(all, r, registryPerPage)
	data.Sectors, _ = h.userService.ListSectors()
	data.Areas, _ = h.userService.ListAreas(data.SelectedSector)

	if err := h.tmplList.ExecuteTemplate(w, "structures.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *StructuresHandler) pageData(r *http.Request) StructurePageData {
	u, perms := h.base(r)
	return StructurePageData{
		CurrentUser: u, Permissions: perms, Kinds: models.StructureKinds,
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "structures", ViewAsBanner: viewBanner(r),
	}
}

func (h *StructuresHandler) load(r *http.Request) *models.Structure {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil
	}
	st, err := h.structureService.Get(r.Context(), id)
	if err != nil {
		return nil
	}
	return st
}

// ShowStructure prikazuje jedan objekt s vodomjerom i dionicama
func (h *StructuresHandler) ShowStructure(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.pageData(r)
	st := h.load(r)
	if st == nil {
		http.NotFound(w, r)
		return
	}
	data.Structure = st
	data.CanEdit = h.structureService.CanEdit(data.Permissions, st)
	for _, code := range st.SectionCodes {
		if sec, err := h.sectionService.GetSectionWithDetails(code); err == nil && sec != nil {
			data.Sections = append(data.Sections, *sec)
		}
	}
	if st.StationID != "" {
		if sid, err := uuid.Parse(st.StationID); err == nil {
			data.Station, _ = h.stationService.GetStation(ctx, sid)
		}
	}
	if err := h.tmplDetail.ExecuteTemplate(w, "structure_detail.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowStructureForm prikazuje obrazac za novi objekt ili izmjenu postojećeg
func (h *StructuresHandler) ShowStructureForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.pageData(r)
	if r.PathValue("id") != "" {
		st := h.load(r)
		if st == nil {
			http.NotFound(w, r)
			return
		}
		if !h.structureService.CanEdit(data.Permissions, st) {
			http.Error(w, "Nemate pravo uređivati ovaj objekt", http.StatusForbidden)
			return
		}
		data.Structure, data.IsEdit = st, true
	} else {
		if !h.structureService.CanCreate(data.Permissions) {
			http.Error(w, "Nemate pravo dodavati objekte", http.StatusForbidden)
			return
		}
		st := &models.Structure{Kind: models.StructureKindPumpingStation, ZeroDatumSystem: "TRST"}
		if areaID, _ := strconv.Atoi(r.URL.Query().Get("area")); areaID > 0 {
			st.AreaID = areaID
		}
		data.Structure = st
	}
	data.Sectors, _ = h.userService.ListSectors()
	data.Areas, _ = h.userService.ListAreas("")
	data.Stations, _ = h.stationService.ListStations(ctx, "", "", false)

	if err := h.tmplForm.ExecuteTemplate(w, "structure_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func structureFromForm(r *http.Request) models.Structure {
	areaID, _ := strconv.Atoi(r.FormValue("area_id"))
	st := models.Structure{
		Code:            strings.TrimSpace(r.FormValue("code")),
		Name:            strings.TrimSpace(r.FormValue("name")),
		Kind:            strings.TrimSpace(r.FormValue("kind")),
		SectorID:        strings.TrimSpace(r.FormValue("sector_id")),
		AreaID:          areaID,
		WatercourseCode: strings.TrimSpace(r.FormValue("watercourse_code")),
		StationID:       strings.TrimSpace(r.FormValue("station_id")),
		ZeroDatum:       parseOptionalFloat(r.FormValue("zero_datum")),
		ZeroDatumSystem: strings.TrimSpace(r.FormValue("zero_datum_system")),
		CapacityText:    strings.TrimSpace(r.FormValue("capacity_text")),
		StartText:       strings.TrimSpace(r.FormValue("start")),
		StopText:        strings.TrimSpace(r.FormValue("stop")),
		Notes:           strings.TrimSpace(r.FormValue("notes")),
		Latitude:        parseOptionalFloat(r.FormValue("latitude")),
		Longitude:       parseOptionalFloat(r.FormValue("longitude")),
	}
	// vodostaj pogona: jedan broj ulazi u izračun, sve drugo ostaje kao tekst
	if t := parseThresholdInput(st.StartText); t.Cm != nil {
		st.StartCm = t.Cm
	}
	if t := parseThresholdInput(st.StopText); t.Cm != nil {
		st.StopCm = t.Cm
	}
	// sektor slijedi iz područja ako nije zadan
	return st
}

// HandleCreate upisuje novi objekt
func (h *StructuresHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	_, perms := h.base(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/structures/new", "error", "Neispravan zahtjev")
		return
	}
	st := structureFromForm(r)
	h.fillSector(&st)
	if err := h.structureService.Create(r.Context(), perms, &st); err != nil {
		redirectWith(w, r, "/structures/new", "error", err.Error())
		return
	}
	redirectWith(w, r, "/structures/"+st.ID.String(), "success", "Objekt je upisan u registar.")
}

// HandleUpdate mijenja objekt
func (h *StructuresHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	_, perms := h.base(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/structures", "error", "Neispravan zahtjev")
		return
	}
	id, err := uuid.Parse(r.FormValue("id"))
	if err != nil {
		redirectWith(w, r, "/structures", "error", "Neispravan identifikator objekta")
		return
	}
	st := structureFromForm(r)
	st.ID = id
	h.fillSector(&st)
	if err := h.structureService.Update(r.Context(), perms, &st); err != nil {
		redirectWith(w, r, "/structures/"+id.String()+"/edit", "error", err.Error())
		return
	}
	redirectWith(w, r, "/structures/"+id.String(), "success", "Izmjene su spremljene.")
}

// HandleDelete briše objekt iz registra (u povijesti ostaje)
func (h *StructuresHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	_, perms := h.base(r)
	id, err := uuid.Parse(r.FormValue("id"))
	if err != nil {
		redirectWith(w, r, "/structures", "error", "Neispravan identifikator objekta")
		return
	}
	if err := h.structureService.Delete(r.Context(), perms, id); err != nil {
		redirectWith(w, r, "/structures/"+id.String(), "error", err.Error())
		return
	}
	redirectWith(w, r, "/structures", "success", "Objekt je obrisan iz registra; zapis ostaje u povijesti.")
}

// fillSector popunjava sektor iz područja kad obrazac šalje samo područje
func (h *StructuresHandler) fillSector(st *models.Structure) {
	if st.SectorID != "" || st.AreaID == 0 {
		return
	}
	areas, err := h.userService.ListAreas("")
	if err != nil {
		return
	}
	for _, a := range areas {
		if a.ID == st.AreaID {
			st.SectorID = a.SectorID
			return
		}
	}
}
