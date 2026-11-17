package web

import (
	"html/template"
	"net/http"
	"strings"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Vidljivost modula: tablica uloge × moduli za globalnog administratora.
// Iznimke po računu uređuju se na stranici djelatnika.

type ModulesHandler struct {
	moduleService *service.ModuleService
	tmpl          *template.Template
}

func NewModulesHandler(modules *service.ModuleService, tmpl *template.Template) *ModulesHandler {
	return &ModulesHandler{moduleService: modules, tmpl: tmpl}
}

type ModulesPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	Modules     []models.Module
	Rows        []service.RoleRow
	// Stupci i Skupine su ista tablica po crtežu: stupac modula s ikonom,
	// retci uloga pod međunaslovom svoje skupine
	Stupci  []StupacModula
	Skupine []SkupinaUloga

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// StupacModula je stupac tablice vidljivosti: modul s ikonom iz izbornika
type StupacModula struct {
	models.Module
	Ikona string
}

// ikoneModula su ikone modula kao u izborniku
var ikoneModula = map[string]string{
	models.ModuleField: "map-pin", models.ModuleReadings: "ruler", models.ModuleJournals: "file-text",
	models.ModuleRegisters: "layers", models.ModuleUsers: "users", models.ModuleAdmin: "settings",
}

func stupciModula() []StupacModula {
	out := make([]StupacModula, 0, len(models.Modules))
	for _, m := range models.Modules {
		out = append(out, StupacModula{Module: m, Ikona: ikoneModula[m.ID]})
	}
	return out
}

// SkupinaUloga su retci tablice jedne skupine sudionika (razina, teren...)
type SkupinaUloga struct {
	Group string
	Rows  []service.RoleRow
}

// skupineUloga slaže retke pod skupine iz kataloga uloga, redom kojim stižu
func skupineUloga(rows []service.RoleRow) []SkupinaUloga {
	skupina := map[models.Role]string{}
	for _, d := range models.RoleCatalog {
		skupina[d.Role] = d.Group
	}
	var out []SkupinaUloga
	for _, r := range rows {
		g := skupina[r.Role]
		if len(out) == 0 || out[len(out)-1].Group != g {
			out = append(out, SkupinaUloga{Group: g})
		}
		out[len(out)-1].Rows = append(out[len(out)-1].Rows, r)
	}
	return out
}

// matrixRoles su uloge u tablici, redom od vrha prema terenu (kao obrazac dužnosti)
func matrixRoles() []models.Role {
	var out []models.Role
	for _, d := range models.RoleCatalog {
		out = append(out, d.Role)
	}
	return out
}

func (h *ModulesHandler) ShowMatrix(w http.ResponseWriter, r *http.Request) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if perms == nil || !perms.IsGlobalAdmin {
		http.Error(w, "Vidljivost modula uređuje samo globalni administrator", http.StatusForbidden)
		return
	}
	data := ModulesPageData{
		CurrentUser: u, Permissions: perms, Modules: models.Modules,
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "moduli", ViewAsBanner: viewBanner(r),
	}
	rows, err := h.moduleService.RoleMatrix(r.Context(), matrixRoles())
	if err != nil {
		data.ErrorMessage = err.Error()
	}
	data.Rows, data.Stupci, data.Skupine = rows, stupciModula(), skupineUloga(rows)
	if err := h.tmpl.ExecuteTemplate(w, "moduli.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleSave sprema cijelu tablicu: polje "m" nosi vrijednosti "ULOGA:modul"
func (h *ModulesHandler) HandleSave(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Neispravan obrazac", http.StatusBadRequest)
		return
	}
	checked := map[string][]string{}
	for _, v := range r.Form["m"] {
		role, mod, ok := strings.Cut(v, ":")
		if ok && models.IsModule(mod) {
			checked[role] = append(checked[role], mod)
		}
	}
	for _, role := range matrixRoles() {
		if err := h.moduleService.SetRoleRule(r.Context(), perms, role, checked[string(role)]); err != nil {
			redirectWith(w, r, "/moduli", "error", err.Error())
			return
		}
	}
	redirectWith(w, r, "/moduli", "success", "Vidljivost modula po ulogama je spremljena.")
}
