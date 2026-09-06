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

// roleOptions su uloge koje obrazac nudi, redom od najviše prema terenu, s
// nazivima kako ih zove organizacija
func roleOptions() []option {
	out := make([]option, 0, len(models.RoleCatalog))
	for _, d := range models.RoleCatalog {
		out = append(out, option{string(d.Role), d.Role.Label()})
	}
	return out
}

// orgOptions su vrste organizacije; matična nosi naziv iz postavki
func orgOptions() []option {
	return []option{
		{string(models.OrgHrvatskeVode), models.OrgHrvatskeVode.Label()},
		{string(models.OrgPravnaOsoba), models.OrgPravnaOsoba.Label()},
		{string(models.OrgVanjski), models.OrgVanjski.Label()},
	}
}

// UserPageData je stranica jednog djelatnika, njegova obrasca ili zaduženja
type UserPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	User        *models.User
	IsSelf      bool
	CanManage   bool // smije uređivati tuđe profile i zaduženja
	IsEdit      bool
	CanDelete   bool // račun se nitko nije prijavio, pa se smije obrisati

	Roles   []option
	Orgs    []option
	Sectors []models.Sector
	Areas   []models.Area

	ModuleRows []ModuleOverrideRow // vidljivost modula za ovaj račun (samo globalni administrator)

	// Privremena lozinka nakon poništavanja: pokazuje se jednom, na stranici
	// koja slijedi odmah iza radnje. Ne ide u adresu ni u poruku o uspjehu,
	// da ne ostane u povijesti preglednika.
	TempPassword string

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

// deletable javlja smije li se račun obrisati: samo onaj koji se nikad nije prijavio
func deletable(u *models.User) bool { return u != nil && u.LastLoginAt == nil }

func (h *UsersHandler) pageData(r *http.Request) UserPageData {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return UserPageData{
		CurrentUser:    currUser,
		Permissions:    perms,
		CanManage:      canManageUsers(perms),
		Roles:          roleOptions(),
		Orgs:           orgOptions(),
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

// HandleResetPassword daje osobi privremenu lozinku i pokaže je
// administratoru koji će je pročitati preko telefona. Otvorene sesije te
// osobe se gase, a ona pri prijavi mora postaviti svoju lozinku.
func (h *UsersHandler) HandleResetPassword(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	u, ok := h.loadUser(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	updated, temp, err := h.userService.ResetPassword(perms, u.ID)
	if err != nil {
		redirectWith(w, r, "/users/"+u.ID.String(), "error", err.Error())
		return
	}
	h.showUser(w, r, updated, temp)
}

// ShowUser prikazuje jednog djelatnika s kontaktima i zaduženjima
func (h *UsersHandler) ShowUser(w http.ResponseWriter, r *http.Request) {
	u, ok := h.loadUser(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.showUser(w, r, u, "")
}

func (h *UsersHandler) showUser(w http.ResponseWriter, r *http.Request, u *models.User, tempPassword string) {
	data := h.pageData(r)
	data.TempPassword = tempPassword
	data.User = u
	data.IsSelf = data.CurrentUser != nil && data.CurrentUser.ID == u.ID
	data.CanDelete = deletable(u)
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
