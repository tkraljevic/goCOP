package web

import (
	"context"
	"html/template"
	"net/http"
	"time"

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
	VidiImenik  bool // ima modul „Djelatnici”; bez njega vidi samo svoj karton
	IsEdit      bool
	CanDelete   bool // račun se nitko nije prijavio, pa se smije obrisati

	Roles   []option
	Orgs    []option
	Sectors []models.Sector
	Areas   []models.Area

	// Zaduženje koje se uređuje; prazno za novo. Pomoćna polja su za
	// predodabir u obrascu.
	Duty         *models.Duty
	DutySector   string
	DutyArea     int
	DutyExpires  string
	Prijasnja    []models.PrijasnjeZaduzenje // opozvana i istekla zaduženja, povijest profila
	IzvoriIsteka map[uuid.UUID]string        // odakle je istek privremene dužnosti (prestanak obrane i akt)

	ModuleRows     []ModuleOverrideRow // vidljivost modula za ovaj račun (samo globalni administrator)
	Planovi        []models.PlanOsobe  // planovi dežurstava u kojima osoba ima sate
	ImaPotpisSliku bool                // sken vlastoručnog potpisa je spremljen
	Kljuc          PodaciKljuca        // osobni potpisni ključ, ako ga ima
	PostaRacun     string              // korisničko ime računa e-pošte, prazno kad lozinka nije upisana
	PostaKad       time.Time

	// Privremena lozinka nakon poništavanja: pokazuje se jednom, na stranici
	// koja slijedi odmah iza radnje. Ne ide u adresu ni u poruku o uspjehu,
	// da ne ostane u povijesti preglednika.
	TempPassword string
	// Privremeni kod za prijavu izvana (P-XXXX-XXXX), isto samo jednom
	TempKod        string
	TempKodIstjece time.Time
	TempKodCvor    string // ime čvora na kojem kod vrijedi
	// DrugiKorak: čvor ima drugi korak prijave izvana (gumb privremenog koda)
	DrugiKorak bool

	// Prijava izvana na vlastitom profilu: PIN, zapamćena računala i
	// rezervni kodovi; nil kad drugi korak na čvoru nije spojen
	PrijavaIzvana *ProfilPrijaveIzvana
	// Izvana: stranica je otvorena izvana (tunel ili javna adresa), pa se
	// vlastita adresa e-pošte ne mijenja
	Izvana bool

	// DomenaPIN je domena na koju ide PIN bez potvrde administratora
	DomenaPIN string
	// PotvrdaAdrese: obrazac nudi okvir „adresa je provjerena” (globalni
	// administrator svojim očima, tuđi ili novi račun)
	PotvrdaAdrese bool
	// AdresaPIN kaže ide li PIN na adresu osobe; samo osobi i onome tko
	// njome upravlja, nil inače ili kad drugi korak na čvoru nije spojen
	AdresaPIN *service.StanjeAdresePIN

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

// SetPotpisniKljuc daje rukovatelju uvid u potpisni ključ osobe
func (h *UsersHandler) SetPotpisniKljuc(f func(ctx context.Context, userID string) PodaciKljuca) {
	h.potpisniKljuc = f
}

// SetPotpisSlika daje rukovatelju uvid ima li osoba sken potpisa
func (h *UsersHandler) SetPotpisSlika(f func(ctx context.Context, userID string) bool) {
	h.potpisSlika = f
}

// SetPosta daje rukovatelju uvid u stanje računa e-pošte za profil
func (h *UsersHandler) SetPosta(f func(ctx context.Context, userID string) (string, time.Time)) {
	h.postaRacun = f
}

func (h *UsersHandler) pageData(r *http.Request) UserPageData {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	mods, _ := ctx.Value(contextKeyModules).(models.Visibility)
	return UserPageData{
		CurrentUser:    currUser,
		Permissions:    perms,
		CanManage:      canManageUsers(perms),
		VidiImenik:     mods.Sees(models.ModuleUsers),
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
// osobe se gase, a ona pri prijavi mora postaviti svoju lozinku. Uz kvačicu
// „i kod za prijavu izvana” osoba dobije i privremeni kod: poništavanje
// briše stare kodove, pa se novi izdaje tek iza njega.
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
	tajne := jednokratno{lozinka: temp}
	if r.FormValue("i_kod") == "1" {
		tajne.kod, tajne.kodIstjece, tajne.greskaKoda = h.izdajKod(r, perms, updated.ID)
	}
	h.showUser(w, r, updated, tajne)
}

// HandleKodPrijave izdaje osobi privremeni kod za prijavu izvana (24 sata,
// jedna prijava) i pokaže ga jednom, kao privremenu lozinku. Smije tko smije
// poništiti lozinku te osobe, nikad sebi ni tuđim očima.
func (h *UsersHandler) HandleKodPrijave(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	u, ok := h.loadUser(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	kod, istjece, err := h.izdajKod(r, perms, u.ID)
	if err != nil {
		redirectWith(w, r, "/users/"+u.ID.String(), "error", err.Error())
		return
	}
	h.showUser(w, r, u, jednokratno{kod: kod, kodIstjece: istjece})
}

// izdajKod izdaje privremeni kod; tuđim očima se odbija
func (h *UsersHandler) izdajKod(r *http.Request, perms *models.UserPermissions, targetID uuid.UUID) (string, time.Time, error) {
	d := h.dk()
	if d == nil {
		return "", time.Time{}, errDrugiKorakNedostupan
	}
	viewing, _ := r.Context().Value(contextKeyViewing).(bool)
	return d.IzdajPrivremeniKod(r.Context(), perms, targetID, viewing)
}

// jednokratno su tajne koje se pokazuju samo na stranici odmah iza radnje
type jednokratno struct {
	lozinka    string
	kod        string
	kodIstjece time.Time
	greskaKoda error // lozinka je poništena, a kod nije izdan
}

// ShowUser prikazuje jednog djelatnika s kontaktima i zaduženjima
func (h *UsersHandler) ShowUser(w http.ResponseWriter, r *http.Request) {
	u, ok := h.loadUser(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.showUser(w, r, u, jednokratno{})
}

func (h *UsersHandler) showUser(w http.ResponseWriter, r *http.Request, u *models.User, tajne jednokratno) {
	data := h.pageData(r)
	data.TempPassword = tajne.lozinka
	data.TempKod, data.TempKodIstjece = tajne.kod, tajne.kodIstjece
	if tajne.kod != "" {
		data.TempKodCvor = h.imeCvora()
	}
	if tajne.greskaKoda != nil {
		data.ErrorMessage = "Lozinka je poništena, ali kod za prijavu izvana nije izdan: " + tajne.greskaKoda.Error()
	}
	data.DrugiKorak = h.dk() != nil
	data.User = u
	data.IsSelf = data.CurrentUser != nil && data.CurrentUser.ID == u.ID
	data.AdresaPIN = h.stanjeAdrese(r, data.Permissions, u)
	data.PotvrdaAdrese = service.SmijePotvrditiAdresu(data.Permissions, u.ID, data.Viewing)
	data.CanDelete = deletable(u)
	data.Prijasnja, _ = h.userService.PastDuties(u.ID)
	data.IzvoriIsteka = h.userService.IzvoriIsteka(u.Duties)
	if h.moduleService != nil && data.Permissions != nil && data.Permissions.IsGlobalAdmin && !u.IsGlobalAdmin {
		data.ModuleRows = h.moduleRows(r, u)
	}

	// privremena lozinka i kod ne smiju ostati u pregledniku ni posredniku
	if tajne.lozinka != "" || tajne.kod != "" {
		w.Header().Set("Cache-Control", "no-store")
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
		data.AdresaPIN = h.stanjeAdrese(r, data.Permissions, u)
	} else {
		data.User = &models.User{OrgType: models.OrgType("HRVATSKE_VODE"), IsActive: true}
	}
	data.DomenaPIN = h.domenaPINa(r)
	data.PotvrdaAdrese = service.SmijePotvrditiAdresu(data.Permissions, data.User.ID, data.Viewing)
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

// ShowDutyEditForm prikazuje postojeće zaduženje u obrascu za izmjenu
func (h *UsersHandler) ShowDutyEditForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if !data.CanManage {
		http.Error(w, "Zaduženja uređuju administratori sektora, područja ili sustava", http.StatusForbidden)
		return
	}
	dutyID, err := uuid.Parse(r.PathValue("duty"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	duty, err := h.userService.GetDuty(dutyID)
	if err != nil || duty == nil || !duty.IsActive {
		http.NotFound(w, r)
		return
	}
	u, err := h.userService.GetUserByID(duty.UserID)
	if err != nil || u == nil {
		http.NotFound(w, r)
		return
	}
	data.User, data.Duty, data.IsEdit = u, duty, true
	if duty.SectorID != nil {
		data.DutySector = *duty.SectorID
	}
	if duty.AreaID != nil {
		data.DutyArea = *duty.AreaID
	}
	// obrazac nudi zadani dan, ne stvarni istek (kraj obrane)
	data.DutyExpires = zadnjiDanUObrascu(duty.ZadaniRok())
	data.Sectors, _ = h.userService.ListSectors()
	data.Areas, _ = h.userService.ListAreas("")

	if err := h.tmplDuty.ExecuteTemplate(w, "duty_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowProfile prikazuje vlastiti profil: kontakti i promjena lozinke
func (h *UsersHandler) ShowProfile(w http.ResponseWriter, r *http.Request) {
	h.prikaziProfil(w, r, nil)
}

// prikaziProfil slaže profil; kodovi su upravo napravljeni rezervni kodovi,
// koji se pokazuju samo na ovoj stranici
func (h *UsersHandler) prikaziProfil(w http.ResponseWriter, r *http.Request, kodovi []string) {
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
	data.Izvana = dolaziIzvana(r)
	if h.planovi != nil {
		data.Planovi, _ = h.planovi(r.Context(), data.User.ID.String())
	}
	if h.postaRacun != nil {
		data.PostaRacun, data.PostaKad = h.postaRacun(r.Context(), data.User.ID.String())
	}
	if h.potpisSlika != nil {
		data.ImaPotpisSliku = h.potpisSlika(r.Context(), data.User.ID.String())
	}
	if h.potpisniKljuc != nil {
		data.Kljuc = h.potpisniKljuc(r.Context(), data.User.ID.String())
	}
	data.PrijavaIzvana = h.profilPrijaveIzvana(r, data.User, kodovi)

	if kodovi != nil {
		w.Header().Set("Cache-Control", "no-store")
	}
	if err := h.tmplProfile.ExecuteTemplate(w, "profile.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// domenaPINa je domena na koju ide PIN bez potvrde administratora
func (h *UsersHandler) domenaPINa(r *http.Request) string {
	if d := h.dk(); d != nil {
		return d.Opcije(r.Context()).Domena
	}
	return service.ZadanaDomenaPIN
}

// stanjeAdrese kaže ide li PIN na adresu osobe, samo osobi i onome tko njome
// upravlja; nil inače, bez drugog koraka ili kad se stanje ne da pročitati
func (h *UsersHandler) stanjeAdrese(r *http.Request, perms *models.UserPermissions, u *models.User) *service.StanjeAdresePIN {
	d := h.dk()
	if d == nil || !service.VidiStanjeAdrese(perms, u) {
		return nil
	}
	s, err := d.StanjeAdrese(r.Context(), u)
	if err != nil {
		return nil
	}
	return s
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
