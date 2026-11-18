package web

import (
	"context"
	"html/template"
	"net/http"
	"strings"

	"gocop/internal/models"
	"gocop/internal/repository"
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
	// Iznimke su iznimke pojedinih računa (uređuju se na kartonu osobe)
	Iznimke []IznimkaPrikaz

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// IznimkaPrikaz je redak kartice iznimaka: osoba i moduli koje joj iznimka
// prikazuje ili skriva, imenima kao u izborniku
type IznimkaPrikaz struct {
	UserID, Ime         string
	Prikazano, Skriveno string
}

// iznimkePrikaz slaže iznimke računa za karticu; račun kojeg više nema
// zove se „nepoznat račun”
func iznimkePrikaz(iznimke []repository.IznimkaRacuna) []IznimkaPrikaz {
	nazivi := func(mods []string) string {
		out := make([]string, 0, len(mods))
		for _, m := range mods {
			out = append(out, models.ModuleLabel(m))
		}
		return strings.Join(out, ", ")
	}
	out := make([]IznimkaPrikaz, 0, len(iznimke))
	for _, i := range iznimke {
		ime := i.Ime
		if ime == "" {
			ime = "nepoznat račun"
		}
		out = append(out, IznimkaPrikaz{UserID: i.UserID, Ime: ime, Prikazano: nazivi(i.Shown), Skriveno: nazivi(i.Hidden)})
	}
	return out
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
	iznimke, err := h.moduleService.IznimkeRacuna(r.Context())
	if err != nil {
		data.ErrorMessage = "Iznimke računa nisu pročitane: " + err.Error()
	}
	data.Iznimke = iznimkePrikaz(iznimke)
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

// stanjeVidljivosti broji uloge s pravilom drukčijim od zadanog i iznimke
// računa, za karticu Vidljivost modula na ulazu u Administraciju
func (s *Server) stanjeVidljivosti(ctx context.Context) (prilagodjenih, iznimki int, err error) {
	if s.moduleService == nil {
		return 0, 0, errBezBaze
	}
	rows, err := s.moduleService.RoleMatrix(ctx, matrixRoles())
	if err != nil {
		return 0, 0, err
	}
	for _, r := range rows {
		if r.Custom {
			prilagodjenih++
		}
	}
	iz, err := s.moduleService.IznimkeRacuna(ctx)
	return prilagodjenih, len(iz), err
}
