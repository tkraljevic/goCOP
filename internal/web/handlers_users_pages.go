package web

import (
	"html/template"
	"net/http"

	"gocop/internal/models"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// Stranice registra djelatnika: jedan djelatnik, obrazac, zaduženje i
// vlastiti profil. Skočni prozori su na telefonu mučenje, a stranica radi
// i bez skripte.

type option struct{ Value, Label string }

// roleOptions su uloge koje obrazac nudi, redom od najviše prema terenu
var roleOptions = []option{
	{"NATIONAL_LEADER", "Glavni rukovoditelj (za cijelu RH)"},
	{"NATIONAL_DEPUTY", "Zamjenik Glavnog rukovoditelja (za cijelu RH)"},
	{"SECTOR_MAIN_DEPUTY", "Zamjenik Glavnog rukovoditelja za sektor"},
	{"SECTOR_LEADER", "Rukovoditelj sektora"},
	{"SECTOR_DEPUTY", "Zamjenik rukovoditelja sektora"},
	{"SECTOR_AREA_DEPUTY", "Zamjenik rukovoditelja sektora za branjeno područje"},
	{"COP_LEADER", "Voditelj Centra obrane od poplava"},
	{"COP_DEPUTY", "Zamjenik voditelja Centra obrane od poplava"},
	{"AREA_LEADER", "Rukovoditelj branjenog područja"},
	{"AREA_DEPUTY", "Zamjenik rukovoditelja branjenog područja"},
	{"SECTION_LEADER", "Rukovoditelj dionice"},
	{"SECTION_DEPUTY", "Zamjenik rukovoditelja dionice"},
	{"CONTRACT_OFFICER_A2", "Ovlaštenik za praćenje ugovora programa usluga A2"},
	{"CONTRACT_OFFICER_A3", "Ovlaštenik za praćenje ugovora programa usluga A3"},
	{"SERVICE_LEADER_FOREMAN", "Voditelj usluga / Poslovođa"},
	{"OPERATOR", "Dežurni operater u COP-u"},
	{"WATER_GUARD", "Vodočuvar"},
	{"MACHINIST", "Strojar"},
	{"FACILITY_OPERATOR", "Rukovatelj"},
	{"CREW_LEADER", "Voditelj posade objekta"},
	{"VIEWER", "Preglednik"},
}

var scopeOptions = []option{
	{"SECTOR", "Cijeli sektor"},
	{"AREA", "Branjeno područje"},
	{"SECTION", "Dionice"},
	{"ALL", "Cijela RH"},
}

var orgOptions = []option{
	{"HRVATSKE_VODE", "Hrvatske vode"},
	{"PRAVNA_OSOBA", "Pravna osoba (Vodoprivreda)"},
	{"VANJSKI", "Vanjska služba (CZ / MUP / 112)"},
}

// UserPageData je stranica jednog djelatnika, njegova obrasca ili zaduženja
type UserPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	User        *models.User
	IsSelf      bool
	CanManage   bool // smije uređivati tuđe profile i zaduženja
	IsEdit      bool

	Roles   []option
	Scopes  []option
	Orgs    []option
	Sectors []models.Sector
	Areas   []models.Area

	ModuleRows []ModuleOverrideRow // vidljivost modula za ovaj račun (samo globalni administrator)

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// ModuleOverrideRow je jedan modul na stranici djelatnika: što uloga daje i
// je li administrator napravio iznimku
type ModuleOverrideRow struct {
	ID       string
	Label    string
	Desc     string
	ByRole   bool   // vidi po ulozi
	Override string // "", "show" ili "hide"
	Visible  bool   // stvarno stanje
}

// SetModuleService daje rukovatelju vidljivost modula
func (h *UsersHandler) SetModuleService(modules *service.ModuleService) {
	h.moduleService = modules
}

// SetPageTemplates daje rukovatelju predloške stranica
func (h *UsersHandler) SetPageTemplates(detail, form, duty, profile *template.Template) {
	h.tmplDetail = detail
	h.tmplForm = form
	h.tmplDuty = duty
	h.tmplProfile = profile
}

// canManageUsers: globalni administrator, ili administrator sektora ili područja
func canManageUsers(p *models.UserPermissions) bool {
	return p != nil && (p.IsGlobalAdmin || len(p.AdminSectors) > 0 || len(p.AdminAreas) > 0)
}

func (h *UsersHandler) pageData(r *http.Request) UserPageData {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return UserPageData{
		CurrentUser:    currUser,
		Permissions:    perms,
		CanManage:      canManageUsers(perms),
		Roles:          roleOptions,
		Scopes:         scopeOptions,
		Orgs:           orgOptions,
		SuccessMessage: r.URL.Query().Get("success"),
		ErrorMessage:   r.URL.Query().Get("error"),
		ActiveNav:      "users",
		ViewAsBanner:   viewBanner(r),
	}
}

func (h *UsersHandler) loadUser(r *http.Request) (*models.User, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, false
	}
	u, err := h.userService.GetUserByID(id)
	if err != nil || u == nil {
		return nil, false
	}
	return u, true
}

// ShowUser prikazuje jednog djelatnika s kontaktima i zaduženjima
func (h *UsersHandler) ShowUser(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	u, ok := h.loadUser(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	data.User = u
	data.IsSelf = data.CurrentUser != nil && data.CurrentUser.ID == u.ID
	if h.moduleService != nil && data.Permissions != nil && data.Permissions.IsGlobalAdmin && !u.IsGlobalAdmin {
		data.ModuleRows = h.moduleRows(r, u)
	}

	if err := h.tmplDetail.ExecuteTemplate(w, "user_detail.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowUserForm prikazuje obrazac za novog djelatnika ili izmjenu postojećeg
func (h *UsersHandler) ShowUserForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if !data.CanManage {
		http.Error(w, "Djelatnike uređuju administratori sektora, područja ili sustava", http.StatusForbidden)
		return
	}
	if r.PathValue("id") != "" {
		u, ok := h.loadUser(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		data.User = u
		data.IsEdit = true
		data.IsSelf = data.CurrentUser != nil && data.CurrentUser.ID == u.ID
	} else {
		data.User = &models.User{OrgType: models.OrgType("HRVATSKE_VODE"), IsActive: true}
	}
	data.Sectors, _ = h.userService.ListSectors()
	data.Areas, _ = h.userService.ListAreas("")

	if err := h.tmplForm.ExecuteTemplate(w, "user_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowDutyForm prikazuje obrazac za novo zaduženje djelatnika
func (h *UsersHandler) ShowDutyForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if !data.CanManage {
		http.Error(w, "Zaduženja dodjeljuju administratori sektora, područja ili sustava", http.StatusForbidden)
		return
	}
	u, ok := h.loadUser(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	data.User = u
	data.Sectors, _ = h.userService.ListSectors()
	data.Areas, _ = h.userService.ListAreas("")

	if err := h.tmplDuty.ExecuteTemplate(w, "duty_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowProfile prikazuje vlastiti profil: kontakti i promjena lozinke
func (h *UsersHandler) ShowProfile(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if data.CurrentUser == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if u, err := h.userService.GetUserByID(data.CurrentUser.ID); err == nil && u != nil {
		data.User = u
	} else {
		data.User = data.CurrentUser
	}
	data.IsSelf = true
	data.ActiveNav = "profile"

	if err := h.tmplProfile.ExecuteTemplate(w, "profile.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// moduleRows slaže vidljivost modula za račun: što daje uloga, što je iznimka
func (h *UsersHandler) moduleRows(r *http.Request, u *models.User) []ModuleOverrideRow {
	ctx := r.Context()
	byRole, _ := h.moduleService.Visibility(ctx, &models.User{ID: u.ID, Duties: u.Duties}, nil)
	override, _ := h.moduleService.UserOverride(ctx, u.ID.String())
	var rows []ModuleOverrideRow
	for _, m := range models.Modules {
		row := ModuleOverrideRow{ID: m.ID, Label: m.Label, Desc: m.Desc, ByRole: byRole[m.ID]}
		row.Visible = row.ByRole
		if override != nil {
			for _, s := range override.Shown {
				if s == m.ID {
					row.Override, row.Visible = "show", true
				}
			}
			for _, s := range override.Hidden {
				if s == m.ID {
					row.Override, row.Visible = "hide", false
				}
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// HandleUserModules sprema iznimke računa: za svaki modul "", "show" ili "hide"
func (h *UsersHandler) HandleUserModules(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	u, ok := h.loadUser(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var shown, hidden []string
	for _, m := range models.Modules {
		switch r.FormValue("mod_" + m.ID) {
		case "show":
			shown = append(shown, m.ID)
		case "hide":
			hidden = append(hidden, m.ID)
		}
	}
	if err := h.moduleService.SetUserOverride(r.Context(), perms, u.ID.String(), shown, hidden); err != nil {
		redirectWith(w, r, "/users/"+u.ID.String(), "error", err.Error())
		return
	}
	redirectWith(w, r, "/users/"+u.ID.String(), "success", "Vidljivost modula za ovaj račun je spremljena.")
}
