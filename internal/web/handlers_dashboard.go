package web

import (
	"html/template"
	"net/http"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

type DashboardHandler struct {
	userService  *service.UserService
	tmpl         *template.Template
	registriTmpl *template.Template
}

func NewDashboardHandler(userService *service.UserService, tmpl, registriTmpl *template.Template) *DashboardHandler {
	return &DashboardHandler{
		userService:  userService,
		tmpl:         tmpl,
		registriTmpl: registriTmpl,
	}
}

// ShowRegisters prikazuje ulaznu stranicu za registre dostupne korisniku.
func (h *DashboardHandler) ShowRegisters(w http.ResponseWriter, r *http.Request) {
	user, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	data := DashboardViewData{
		CurrentUser:  user,
		Perms:        perms,
		ActiveNav:    "registers",
		ViewAsBanner: viewBanner(r),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.registriTmpl.ExecuteTemplate(w, "registri.html", data); err != nil {
		http.Error(w, "Greška pri renderiranju registara: "+err.Error(), http.StatusInternalServerError)
	}
}

type DashboardViewData struct {
	CurrentUser    *models.User
	Perms          *models.UserPermissions
	Stats          repository.DashboardStats
	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

func (h *DashboardHandler) ShowDashboard(w http.ResponseWriter, r *http.Request) {
	user, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)

	stats, err := h.userService.GetDashboardStats()
	if err != nil {
		stats = repository.DashboardStats{
			TotalUsers:   545,
			TotalDuties:  746,
			TotalSectors: 7,
			TotalAreas:   34,
		}
	}

	data := DashboardViewData{
		CurrentUser:  user,
		Perms:        perms,
		Stats:        stats,
		ActiveNav:    "dashboard",
		ViewAsBanner: viewBanner(r),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "dashboard.html", data); err != nil {
		http.Error(w, "Greška pri renderiranju početne stranice: "+err.Error(), http.StatusInternalServerError)
	}
}
