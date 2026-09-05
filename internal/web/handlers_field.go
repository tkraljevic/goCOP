package web

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Teren: pogled vodočuvara i strojara. Nema registara ni popisa — samo
// letve koje osoba obilazi, što je danas obavljeno i jedan dodir do upisa.

type FieldHandler struct {
	readingService *service.ReadingService
	userService    *service.UserService
	tmpl           *template.Template
}

func NewFieldHandler(readings *service.ReadingService, users *service.UserService, tmpl *template.Template) *FieldHandler {
	return &FieldHandler{readingService: readings, userService: users, tmpl: tmpl}
}

type FieldPageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	Overview       *service.FieldOverview
	Sectors        []models.Sector
	SelectedSector string
	Today          string
	Duty           *models.Duty
	Percent        int
	Remaining      int
	NextURL        string // prva letva koja danas još nije očitana
	NextName       string

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

func (h *FieldHandler) ShowField(w http.ResponseWriter, r *http.Request) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	areaID, _ := strconv.Atoi(r.URL.Query().Get("area"))
	sectorID := strings.TrimSpace(r.URL.Query().Get("sector"))
	data := FieldPageData{
		CurrentUser: u, Permissions: perms, SelectedSector: sectorID,
		Today:          time.Now().In(models.Zagreb).Format("02.01.2006."),
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "teren", ViewAsBanner: viewBanner(r),
	}
	if u != nil {
		data.Duty = u.PrimaryDuty()
	}
	fo, err := h.readingService.FieldOverview(r.Context(), perms, u, areaID)
	if err != nil {
		data.ErrorMessage = err.Error()
		fo = &service.FieldOverview{}
	}
	allAllowedAreas := append([]models.Area(nil), fo.Areas...)
	seenSectors := map[string]bool{}
	for _, area := range allAllowedAreas {
		seenSectors[area.SectorID] = true
	}
	if sectors, e := h.userService.ListSectors(); e == nil {
		for _, sector := range sectors {
			if seenSectors[sector.ID] {
				data.Sectors = append(data.Sectors, sector)
			}
		}
	}
	if sectorID != "" {
		var filtered []models.Area
		validArea := false
		for _, area := range allAllowedAreas {
			if area.SectorID == sectorID {
				filtered = append(filtered, area)
				if area.ID == areaID {
					validArea = true
				}
			}
		}
		if !validArea && len(filtered) > 0 {
			areaID = filtered[0].ID
			if refreshed, e := h.readingService.FieldOverview(r.Context(), perms, u, areaID); e == nil {
				fo = refreshed
			}
		}
		fo.Areas = filtered
	}
	data.Overview = fo
	if fo.Total > 0 {
		data.Percent = fo.Done * 100 / fo.Total
		data.Remaining = fo.Total - fo.Done
	}
	for _, g := range fo.Mine {
		if !g.DoneToday {
			data.NextURL, data.NextName = g.NewURL+"&back=/teren", g.Name
			break
		}
	}
	if err := h.tmpl.ExecuteTemplate(w, "teren.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
