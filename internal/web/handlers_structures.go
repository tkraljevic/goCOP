package web

import (
	"context"
	"encoding/json"
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
	karta            func() KartaPostavke
	vodotociSvc      *service.WatercourseService
	tmplPovezanost   *template.Template
}

// SetPovezanost daje registar vodotoka (izbor na obrascu objekta) i
// predložak stranice povezanosti ustava i crpnih stanica
func (h *StructuresHandler) SetPovezanost(vodotoci *service.WatercourseService, tmpl *template.Template) {
	h.vodotociSvc, h.tmplPovezanost = vodotoci, tmpl
}

// SetKarta daje rukovatelju izvor pločica za kartu objekta
func (h *StructuresHandler) SetKarta(f func() KartaPostavke) { h.karta = f }

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

	// Register je registar jedne vrste (nasipi, crpne stanice…); prazan
	// kod popisa svih objekata
	Register models.StructureRegister
}

type StructurePageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	Structure   *models.Structure
	Sections    []models.Section
	Station     *models.Station
	StationDown *models.Station // nizvodna letva ustave
	// Mjerodavni su mjerodavni vodomjeri nasipa ili brane iz dionica na
	// kojima stoji; nil kod drugih vrsta objekata
	Mjerodavni *MjerodavniNasipa
	CanEdit    bool
	IsEdit     bool

	Karta        KartaPostavke
	GeometryJSON template.JS // ucrtani objekt za kartu
	Register     models.StructureRegister

	Sectors  []models.Sector
	Areas    []models.Area
	Kinds    []string
	Stations []models.Station // za odabir vodomjera na obrascu
	// CrpneStanice su ponuda za „u sklopu crpne stanice” na obrascu ustave
	CrpneStanice []models.Structure
	// Vodotoci su ponuda za vodotok prve i druge strane objekta
	Vodotoci []models.Watercourse
	// USklopu je crpna stanica u čijem je sklopu ustava; Ustave su ustave u
	// sklopu crpne stanice
	USklopu *models.Structure
	Ustave  []models.Structure

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

// ShowStructures: zajedničkog popisa objekata više nema; stara adresa vodi
// na registar vrste (iz ?kind=), inače na nasipe, s istim filtrima
func (h *StructuresHandler) ShowStructures(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cilj := models.StructureRegisters[0]
	if reg, ok := models.StructureRegisterFor(q.Get("kind")); ok {
		cilj = reg
		if len(reg.Kinds) == 1 {
			q.Del("kind")
		}
	}
	put := cilj.Path
	if enc := q.Encode(); enc != "" {
		put += "?" + enc
	}
	http.Redirect(w, r, put, http.StatusSeeOther)
}

// ShowRegister prikazuje registar: nasipe, crpne stanice… Pretraga i filtri
// traže samo u vrstama tog registra.
func (h *StructuresHandler) ShowRegister(reg models.StructureRegister) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.prikaziPopis(w, r, reg) })
}

func (h *StructuresHandler) prikaziPopis(w http.ResponseWriter, r *http.Request, reg models.StructureRegister) {
	ctx := r.Context()
	u, perms := h.base(r)
	q := r.URL.Query()
	areaID, _ := strconv.Atoi(q.Get("area"))
	data := StructuresPageData{
		CurrentUser: u, Permissions: perms, Kinds: reg.Kinds,
		SelectedSector: strings.TrimSpace(q.Get("sector")), SelectedArea: areaID,
		SearchQuery: strings.TrimSpace(q.Get("q")),
		CanCreate:   h.structureService.CanCreate(perms), ByKind: map[string]int{},
		SuccessMessage: q.Get("success"), ErrorMessage: q.Get("error"),
		ActiveNav: navObjekta(reg.Kind), ViewAsBanner: viewBanner(r), Register: reg,
	}
	if k := strings.TrimSpace(q.Get("kind")); reg.Ima(k) {
		data.SelectedKind = k
	}
	all, err := h.popisRegistra(ctx, reg, data)
	if err != nil {
		data.ErrorMessage = err.Error()
	}
	if everything, err := h.structureService.List(ctx, "", 0, "", ""); err == nil {
		for _, s := range everything {
			data.ByKind[s.Kind]++
			if reg.Ima(s.Kind) {
				data.Total++
			}
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
	if preusmjeriNaRegistar(w, r, st.Path()) {
		return
	}
	data.Structure = st
	data.CanEdit = h.structureService.CanEdit(data.Permissions, st)
	h.oznaciRegistar(&data)
	for _, code := range st.SectionCodes {
		if sec, err := h.sectionService.GetSectionWithDetails(code); err == nil && sec != nil {
			data.Sections = append(data.Sections, *sec)
		}
	}
	data.Station, data.StationDown = h.letvaObjekta(ctx, st.StationID), h.letvaObjekta(ctx, st.StationDownID)
	data.USklopu, data.Ustave = h.structureService.Sklop(ctx, st)
	if imaMjerodavneVodomjere(st.Kind) {
		data.Mjerodavni = h.mjerodavniVodomjeri(ctx, st.ID.String(), st.Kind, data.Sections)
	}
	h.napuniKartu(&data)
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
		if preusmjeriNaRegistar(w, r, st.Path()+"/edit") {
			return
		}
		if !h.structureService.CanEdit(data.Permissions, st) {
			http.Error(w, "Nemate pravo uređivati ovaj objekt", http.StatusForbidden)
			return
		}
		data.Structure, data.IsEdit = st, true
		h.oznaciRegistar(&data)
	} else {
		if !h.structureService.CanCreate(data.Permissions) {
			http.Error(w, "Nemate pravo dodavati objekte", http.StatusForbidden)
			return
		}
		st := &models.Structure{Kind: models.StructureKindPumpingStation, ZeroDatumSystem: "TRST"}
		if areaID, _ := strconv.Atoi(r.URL.Query().Get("area")); areaID > 0 {
			st.AreaID = areaID
		}
		// novi zapis iz registra jedne vrste dolazi s tom vrstom
		if reg, ok := models.StructureRegisterFor(r.URL.Query().Get("kind")); ok {
			st.Kind = reg.Kind
		}
		data.Structure = st
		h.oznaciRegistar(&data)
	}
	data.Sectors, _ = h.userService.ListSectors()
	data.Areas, _ = h.userService.ListAreas("")
	data.Stations, _ = h.stationService.ListStations(ctx, "", "", "", false)
	if cs, err := h.structureService.List(ctx, "", 0, models.StructureKindPumpingStation, ""); err == nil {
		data.CrpneStanice = cs
	}
	data.Vodotoci = h.vodotoci(ctx)

	if err := h.tmplForm.ExecuteTemplate(w, "structure_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// vodotoci su vodotoci iz registra za izbor na obrascu objekta
func (h *StructuresHandler) vodotoci(ctx context.Context) []models.Watercourse {
	if h.vodotociSvc == nil {
		return nil
	}
	v, err := h.vodotociSvc.ListWatercourses(ctx, "", "", false)
	if err != nil {
		return nil
	}
	return v
}

func structureFromForm(r *http.Request) models.Structure {
	areaID, _ := strconv.Atoi(r.FormValue("area_id"))
	st := models.Structure{
		Code:               strings.TrimSpace(r.FormValue("code")),
		Name:               strings.TrimSpace(r.FormValue("name")),
		Kind:               strings.TrimSpace(r.FormValue("kind")),
		SectorID:           strings.TrimSpace(r.FormValue("sector_id")),
		AreaID:             areaID,
		WatercourseCode:    strings.TrimSpace(r.FormValue("watercourse_code")),
		StationID:          strings.TrimSpace(r.FormValue("station_id")),
		StationDownID:      strings.TrimSpace(r.FormValue("station_down_id")),
		ZeroDatum:          parseOptionalFloat(r.FormValue("zero_datum")),
		ZeroDatumSystem:    strings.TrimSpace(r.FormValue("zero_datum_system")),
		CapacityText:       strings.TrimSpace(r.FormValue("capacity_text")),
		StartText:          strings.TrimSpace(r.FormValue("start")),
		StopText:           strings.TrimSpace(r.FormValue("stop")),
		ImaUstavu:          r.FormValue("ima_ustavu") == "1",
		USklopuID:          strings.TrimSpace(r.FormValue("u_sklopu_id")),
		VodotokDrugeStrane: strings.TrimSpace(r.FormValue("vodotok_druge_strane")),
		Reverzibilna:       r.FormValue("reverzibilna") == "1",
		ImaSifon:           r.FormValue("ima_sifon") == "1",
		Notes:              strings.TrimSpace(r.FormValue("notes")),
		Latitude:           parseOptionalFloat(r.FormValue("latitude")),
		Longitude:          parseOptionalFloat(r.FormValue("longitude")),
	}
	st.SifonMinCm = najniziZaSifon(r, st.ImaSifon)
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
	redirectWith(w, r, st.Path(), "success", "Objekt je upisan u registar.")
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
		redirectWith(w, r, st.Path()+"/edit", "error", err.Error())
		return
	}
	redirectWith(w, r, st.Path(), "success", "Izmjene su spremljene.")
}

// HandleDelete briše objekt iz registra (u povijesti ostaje)
func (h *StructuresHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	_, perms := h.base(r)
	id, err := uuid.Parse(r.FormValue("id"))
	if err != nil {
		redirectWith(w, r, models.StructureRegisters[0].Path, "error", "Neispravan identifikator objekta")
		return
	}
	natrag := models.StructureRegisters[0].Path
	if st, err := h.structureService.Get(r.Context(), id); err == nil && st != nil {
		if reg, ok := models.StructureRegisterFor(st.Kind); ok {
			natrag = reg.Path
		}
	}
	if err := h.structureService.Delete(r.Context(), perms, id); err != nil {
		redirectWith(w, r, "/structures/"+id.String(), "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Objekt je obrisan iz registra; zapis ostaje u povijesti.")
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

// popisRegistra vraća objekte vrsta registra, po filtrima stranice
func (h *StructuresHandler) popisRegistra(ctx context.Context, reg models.StructureRegister, data StructuresPageData) ([]models.Structure, error) {
	kind := data.SelectedKind
	if kind == "" && len(reg.Kinds) == 1 {
		kind = reg.Kinds[0]
	}
	sve, err := h.structureService.List(ctx, data.SelectedSector, data.SelectedArea, kind, data.SearchQuery)
	if err != nil || kind != "" {
		return sve, err
	}
	var out []models.Structure
	for _, s := range sve {
		if reg.Ima(s.Kind) {
			out = append(out, s)
		}
	}
	return out, nil
}

// oznaciRegistar veže stranicu objekta uz registar njegove vrste: izbornik
// i put natrag vode na nasipe, crpne stanice…
func (h *StructuresHandler) oznaciRegistar(data *StructurePageData) {
	if reg, ok := models.StructureRegisterFor(data.Structure.Kind); ok {
		data.Register, data.ActiveNav = reg, navObjekta(reg.Kind)
	}
}

// letvaObjekta čita letvu objekta po ID-u; prazan ili kriv ID nema letve
func (h *StructuresHandler) letvaObjekta(ctx context.Context, id string) *models.Station {
	sid, err := uuid.Parse(id)
	if err != nil {
		return nil
	}
	st, err := h.stationService.GetStation(ctx, sid)
	if err != nil {
		return nil
	}
	return st
}

// preusmjeriNaRegistar šalje staru adresu /structures/<id> na adresu u
// registru vrste (/nasipi/<id>…), s istim upitom; javlja je li preusmjerio
func preusmjeriNaRegistar(w http.ResponseWriter, r *http.Request, put string) bool {
	if !strings.HasPrefix(r.URL.Path, "/structures/") || r.URL.Path == put {
		return false
	}
	if r.URL.RawQuery != "" {
		put += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, put, http.StatusMovedPermanently)
	return true
}

// napuniKartu daje stranici pločice karte i ucrtani objekt
func (h *StructuresHandler) napuniKartu(data *StructurePageData) {
	if h.karta != nil {
		data.Karta = h.karta()
	}
	if data.Structure.HasGeometry() {
		data.GeometryJSON, _ = jsonZaSkriptu([]byte(data.Structure.Geometry))
	}
}

type structureGeometryForm struct {
	ID      string `json:"id"`
	GeoJSON string `json:"geojson"`
}

// HandleGeometryAPI upisuje ili briše ucrtani objekt. Geometrija stiže samo
// JSON-om iz skripte stranice, kao kod vodotoka: običan obrazac može poslati
// i tuđa stranica, a geometrija se crta na karti.
func (h *StructuresHandler) HandleGeometryAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	if !jsonTijelo(r) {
		http.Error(w, "Geometrija se šalje kao JSON (Content-Type: application/json)", http.StatusUnsupportedMediaType)
		return
	}
	var req structureGeometryForm
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Neispravan JSON format", http.StatusBadRequest)
		return
	}
	id, err := uuid.Parse(strings.TrimSpace(req.ID))
	if err != nil {
		http.Error(w, "Objekt nije naveden", http.StatusBadRequest)
		return
	}
	if err := h.structureService.SetGeometry(ctx, perms, id, req.GeoJSON); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	poruka := "Objekt je ucrtan na kartu."
	if strings.TrimSpace(req.GeoJSON) == "" {
		poruka = "Crta objekta je obrisana s karte."
	}
	writeJSON(w, map[string]any{"success": true, "message": poruka})
}
