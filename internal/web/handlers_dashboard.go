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

	zid      func() *service.ZidService
	izvjesca func() *service.IzvjescaService
	mts      func() *service.MtsService
}

// SetZid daje naslovnoj zid događanja i traku stanja
func (h *DashboardHandler) SetZid(zid func() *service.ZidService, izvjesca func() *service.IzvjescaService, mts func() *service.MtsService) {
	h.zid, h.izvjesca, h.mts = zid, izvjesca, mts
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
	Stanja         []service.StanjeObrane
	Zid            []service.Dogadjaj
	Moduli         []struct{ ID, Naziv, Ikona string }
	Precaci        []Precac
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
		Moduli:       service.ModuliZida,
		ActiveNav:    "dashboard",
		ViewAsBanner: viewBanner(r),
	}
	if h.zid != nil {
		if zid := h.zid(); zid != nil {
			sektori, _ := h.userService.ListSectors()
			var izv *service.IzvjescaService
			var mts *service.MtsService
			if h.izvjesca != nil {
				izv = h.izvjesca()
			}
			if h.mts != nil {
				mts = h.mts()
			}
			data.Stanja = zid.Stanje(r.Context(), perms, sektori, izv, mts)
			data.Zid, _, _ = zid.Zadnji(r.Context(), perms, service.FiltarZida{Modul: r.URL.Query().Get("modul"), Limit: 40})
		}
	}
	data.Precaci = precaciZa(perms)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "dashboard.html", data); err != nil {
		http.Error(w, "Greška pri renderiranju početne stranice: "+err.Error(), http.StatusInternalServerError)
	}
}

// Precac je gumb na naslovnoj za ono što se u toj ulozi radi svaki dan
type Precac struct {
	Naslov, Link, Ikona string
}

// precaciZa slaže prečace po dužnostima osobe
func precaciZa(perms *models.UserPermissions) []Precac {
	if perms == nil {
		return nil
	}
	var out []Precac
	dodaj := func(naslov, link, ikona string) {
		for _, p := range out {
			if p.Link == link {
				return
			}
		}
		out = append(out, Precac{Naslov: naslov, Link: link, Ikona: ikona})
	}
	uprava := perms.IsGlobalAdmin || len(perms.AdminSectors) > 0
	if uprava {
		dodaj("Dnevnik COP-a", "/dnevnici/popis?vrsta=OBRANA", "notebook")
		dodaj("Izvješće sektora", "/izvjesca", "file-text")
	}
	if len(perms.AllowedSections) > 0 || len(perms.AdminAreas) > 0 {
		dodaj("Novo dnevno izvješće", "/izvjesca/novo", "file-text")
	}
	skladistar := false
	for _, d := range perms.User.Duties {
		if d.IsActive && d.Role == models.RoleWarehouseKeeper {
			skladistar = true
		}
	}
	if skladistar || uprava || len(perms.AdminAreas) > 0 {
		dodaj("Skladišta i sredstva", "/sredstva", "package")
	}
	if perms.User.IsFieldUser() {
		dodaj("Teren", "/teren", "map-pin")
	}
	dodaj("Događanja", "/dogadjanja", "activity")
	return out
}

// PoDanima grupira zid po danima
func (d DashboardViewData) PoDanima() []DanDogadjaja { return grupirajPoDanima(d.Zid) }
