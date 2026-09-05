package web

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Registar organizacije: sektori i branjena područja. Program ih ne zna sam,
// pa je ovo prvo što globalni administrator upisuje na novom čvoru; sve
// ostalo (ovlasti, dionice, zaduženja) veže se na njih.

type OrgHandler struct {
	org        *service.OrgService
	tmplList   *template.Template
	tmplSector *template.Template
	tmplArea   *template.Template
}

func NewOrgHandler(org *service.OrgService, list, sector, area *template.Template) *OrgHandler {
	return &OrgHandler{org: org, tmplList: list, tmplSector: sector, tmplArea: area}
}

// SectorView je sektor sa svojim branjenim područjima
type SectorView struct {
	models.Sector
	Areas []models.Area
}

type OrgPageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	Sectors        []SectorView
	Orphans        []models.Area // područja čiji sektor ne postoji
	TotalAreas     int
	Sector         models.Sector
	Area           models.Area
	SectorOptions  []models.Sector
	IsEdit         bool
	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

func (h *OrgHandler) pageData(r *http.Request) OrgPageData {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return OrgPageData{
		CurrentUser: currUser, Permissions: perms,
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "registri", ViewAsBanner: viewBanner(r),
	}
}

// ShowOrganization prikazuje sektore s branjenim područjima
func (h *OrgHandler) ShowOrganization(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.pageData(r)
	sectors, err := h.org.ListSectors(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	areas, _ := h.org.ListAreas(ctx, "")
	data.TotalAreas = len(areas)
	bySector := map[string][]models.Area{}
	known := map[string]bool{}
	for _, s := range sectors {
		known[s.ID] = true
	}
	for _, a := range areas {
		if known[a.SectorID] {
			bySector[a.SectorID] = append(bySector[a.SectorID], a)
		} else {
			data.Orphans = append(data.Orphans, a)
		}
	}
	for _, s := range sectors {
		data.Sectors = append(data.Sectors, SectorView{Sector: s, Areas: bySector[s.ID]})
	}
	if err := h.tmplList.ExecuteTemplate(w, "organizacija.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *OrgHandler) requireAdmin(w http.ResponseWriter, data OrgPageData) bool {
	if data.Permissions == nil || !data.Permissions.IsGlobalAdmin {
		http.Error(w, "Organizaciju uređuje globalni administrator", http.StatusForbidden)
		return false
	}
	return true
}

// ShowSectorForm prikazuje obrazac za novi sektor ili izmjenu postojećeg
func (h *OrgHandler) ShowSectorForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if !h.requireAdmin(w, data) {
		return
	}
	if id := r.PathValue("id"); id != "" {
		s, err := h.org.GetSector(r.Context(), id)
		if err != nil || s == nil {
			http.NotFound(w, r)
			return
		}
		data.Sector, data.IsEdit = *s, true
	}
	if err := h.tmplSector.ExecuteTemplate(w, "sector_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowAreaForm prikazuje obrazac za novo branjeno područje ili izmjenu postojećeg
func (h *OrgHandler) ShowAreaForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if !h.requireAdmin(w, data) {
		return
	}
	data.SectorOptions, _ = h.org.ListSectors(r.Context())
	if raw := r.PathValue("id"); raw != "" {
		id, _ := strconv.Atoi(raw)
		a, err := h.org.GetArea(r.Context(), id)
		if err != nil || a == nil {
			http.NotFound(w, r)
			return
		}
		data.Area, data.IsEdit = *a, true
	} else {
		data.Area.SectorID = strings.ToUpper(r.URL.Query().Get("sector"))
	}
	if err := h.tmplArea.ExecuteTemplate(w, "area_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func sectorFromForm(r *http.Request) *models.Sector {
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	return &models.Sector{ID: f("id"), Name: f("name"), VgoName: f("vgo_name"), CenterCop: f("center_cop"),
		Address: f("address"), Phone: f("phone"), Email: f("email")}
}

func areaFromForm(r *http.Request) *models.Area {
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	id, _ := strconv.Atoi(f("id"))
	return &models.Area{ID: id, SectorID: f("sector_id"), Name: f("name"), VgiName: f("vgi_name"),
		Subcenter: f("subcenter"), ContractorName: f("contractor_name")}
}

// HandleSaveSector upisuje sektor iz obrasca (nov ili izmijenjen)
func (h *OrgHandler) HandleSaveSector(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/organizacija", "error", "Neispravan zahtjev")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	sec := sectorFromForm(r)
	isNew := r.FormValue("is_new") == "1"
	if err := h.org.SaveSector(r.Context(), perms, sec, isNew); err != nil {
		back := "/organizacija/sektori/new"
		if !isNew {
			back = "/organizacija/sektori/" + sec.ID + "/edit"
		}
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, "/organizacija", "success", "Sektor "+sec.ID+" je upisan.")
}

// HandleDeleteSector briše sektor na koji se ništa ne veže
func (h *OrgHandler) HandleDeleteSector(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/organizacija", "error", "Neispravan zahtjev")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if err := h.org.DeleteSector(r.Context(), perms, r.FormValue("id")); err != nil {
		redirectWith(w, r, "/organizacija", "error", err.Error())
		return
	}
	redirectWith(w, r, "/organizacija", "success", "Sektor je obrisan.")
}

// HandleSaveArea upisuje branjeno područje iz obrasca (novo ili izmijenjeno)
func (h *OrgHandler) HandleSaveArea(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/organizacija", "error", "Neispravan zahtjev")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	a := areaFromForm(r)
	isNew := r.FormValue("is_new") == "1"
	if err := h.org.SaveArea(r.Context(), perms, a, isNew); err != nil {
		back := "/organizacija/podrucja/new?sector=" + a.SectorID
		if !isNew {
			back = "/organizacija/podrucja/" + strconv.Itoa(a.ID) + "/edit"
		}
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, "/organizacija", "success", "Branjeno područje "+strconv.Itoa(a.ID)+" je upisano.")
}

// HandleDeleteArea briše branjeno područje na koje se ništa ne veže
func (h *OrgHandler) HandleDeleteArea(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/organizacija", "error", "Neispravan zahtjev")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	id, _ := strconv.Atoi(r.FormValue("id"))
	if err := h.org.DeleteArea(r.Context(), perms, id); err != nil {
		redirectWith(w, r, "/organizacija", "error", err.Error())
		return
	}
	redirectWith(w, r, "/organizacija", "success", "Branjeno područje je obrisano.")
}
