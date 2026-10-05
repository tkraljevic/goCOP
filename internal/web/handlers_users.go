package web

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"

	"github.com/google/uuid"
)

type UsersHandler struct {
	userService   *service.UserService
	moduleService *service.ModuleService
	tmpl          *template.Template // popis
	tmplDetail    *template.Template // jedan djelatnik
	tmplForm      *template.Template // obrazac
	tmplDuty      *template.Template // zaduženje
	tmplProfile   *template.Template // vlastiti profil
	// potpisSlika javlja ima li osoba sken potpisa, za profil; nil dok se ne spoji
	potpisSlika   func(ctx context.Context, userID string) bool
	potpisniKljuc func(ctx context.Context, userID string) PodaciKljuca
	// postaRacun daje stanje računa e-pošte osobe, za profil; nil dok se ne spoji
	postaRacun func(ctx context.Context, userID string) (string, time.Time)
	// planovi daje planove dežurstava osobe, za profil; nil dok se ne spoji
	planovi func(ctx context.Context, userID string) ([]models.PlanOsobe, error)
	// drugiKorak daje drugi korak prijave izvana (zapamćena računala,
	// rezervni i privremeni kodovi); nil ili nil servis dok se ne spoji
	drugiKorak func() *service.DrugiKorak
	// cvor daje ime ovog čvora: rezervni i privremeni kodovi vrijede samo
	// na čvoru na kojem su napravljeni; nil dok se ne spoji
	cvor func() string
}

// SetPlanovi spaja profil s planom dežurstava: osoba na profilu vidi svoje
// planove i izvješća o satima
func (h *UsersHandler) SetPlanovi(f func(ctx context.Context, userID string) ([]models.PlanOsobe, error)) {
	h.planovi = f
}

func NewUsersHandler(userService *service.UserService, tmpl *template.Template) *UsersHandler {
	return &UsersHandler{
		userService: userService,
		tmpl:        tmpl,
	}
}

type UsersPageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	Users          []models.User
	Sectors        []models.Sector
	Areas          []models.Area
	SelectedSector string
	SelectedArea   int
	SelectedRole   string
	SelectedStatus string
	SearchQuery    string
	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	Pager          Pager
	ViewAsBanner
}

// ShowUsers prikazuje listu djelatnika s njihovim funkcijama i zaduženjima
func (h *UsersHandler) ShowUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)

	sectorFilter := r.URL.Query().Get("sector")
	roleFilter := r.URL.Query().Get("role")
	areaFilter, _ := strconv.Atoi(r.URL.Query().Get("area"))
	statusFilter := r.URL.Query().Get("status")
	searchQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	if searchQuery == "" {
		searchQuery = strings.TrimSpace(r.URL.Query().Get("search"))
	}

	users, err := h.userService.ListUsers(sectorFilter, areaFilter, roleFilter, searchQuery, statusFilter)
	if err != nil {
		http.Error(w, "Greška pri dohvatu: "+err.Error(), http.StatusInternalServerError)
		return
	}

	sectors, _ := h.userService.ListSectors()
	areas, _ := h.userService.ListAreas(sectorFilter)

	page, pager := paginate(users, r, registryPerPage)
	data := UsersPageData{
		CurrentUser:    currUser,
		Permissions:    perms,
		Users:          page,
		Pager:          pager,
		Sectors:        sectors,
		Areas:          areas,
		SelectedSector: sectorFilter,
		SelectedArea:   areaFilter,
		SelectedRole:   roleFilter,
		SelectedStatus: statusFilter,
		SearchQuery:    searchQuery,
		SuccessMessage: r.URL.Query().Get("success"),
		ErrorMessage:   r.URL.Query().Get("error"),
		ActiveNav:      "users",
		ViewAsBanner:   viewBanner(r),
	}

	if err := h.tmpl.ExecuteTemplate(w, "users.html", data); err != nil {
		http.Error(w, "Greška renderiranja: "+err.Error(), http.StatusInternalServerError)
	}
}

// HandleCreateUser stvara novog korisnika i početno zaduženje
func (h *UsersHandler) HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/users?error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}

	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)

	var sectorPtr *string
	sectorVal := r.FormValue("sector_id")
	if sectorVal != "" {
		sectorPtr = &sectorVal
	}

	var areaPtr *int
	areaVal, _ := strconv.Atoi(r.FormValue("area_id"))
	if areaVal > 0 {
		areaPtr = &areaVal
	}

	isGlobalAdmin := r.FormValue("is_global_admin") == "1" || r.FormValue("is_global_admin") == "on"

	req := service.CreateUserRequest{
		Username:      r.FormValue("username"),
		Password:      r.FormValue("password"),
		FullName:      r.FormValue("full_name"),
		Title:         r.FormValue("title"),
		IsGlobalAdmin: isGlobalAdmin,
		OrgType:       models.OrgType(r.FormValue("org_type")),
		OrgName:       r.FormValue("org_name"),
		Phone:         r.FormValue("phone"),
		MobilePhone:   r.FormValue("mobile_phone"),
		ShortPhone:    r.FormValue("short_phone"),
		ShortMobile:   r.FormValue("short_mobile"),
		Email:         r.FormValue("email"),
		DutyTitle:     r.FormValue("duty_title"),
		Role:          models.Role(r.FormValue("role")),
		SectorID:      sectorPtr,
		AreaID:        areaPtr,
		SectionCodes:  r.FormValue("section_codes"),
		// okvir „adresa je provjerena” stoji samo na obrascu globalnog
		// administratora, i to ne dok gleda tuđim očima
		PotvrdaAdrese: r.FormValue("pin_adresa_potvrdena") == "1",
		TudjimOcima:   viewBanner(r).Viewing,
	}

	created, err := h.userService.CreateUser(perms, req)
	if err != nil {
		redirectWith(w, r, "/users/new", "error", err.Error())
		return
	}

	redirectWith(w, r, "/users/"+created.ID.String(), "success", "Djelatnik je dodan.")
}

// HandleUpdateUser ažurira profil
func (h *UsersHandler) HandleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/users?error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}

	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	userID, err := uuid.Parse(r.FormValue("id"))
	if err != nil {
		http.Redirect(w, r, "/users?error=Neispravan+ID", http.StatusSeeOther)
		return
	}

	isActive := r.FormValue("is_active") == "1" || r.FormValue("is_active") == "on"
	isGlobalAdmin := r.FormValue("is_global_admin") == "1" || r.FormValue("is_global_admin") == "on"

	// Svoju lozinku administrator mijenja na profilu, uz trenutnu: obrazac
	// djelatnika postavlja tuđu (i time opoziva njezina zapamćena računala)
	vlastiti := perms != nil && perms.User.ID == userID
	if vlastiti && r.FormValue("password") != "" {
		redirectWith(w, r, "/users/"+userID.String()+"/edit", "error", "Svoju lozinku mijenjate na profilu, uz trenutnu lozinku.")
		return
	}

	req := service.UpdateUserRequest{
		ID:            userID,
		Username:      r.FormValue("username"),
		Password:      r.FormValue("password"),
		FullName:      r.FormValue("full_name"),
		Title:         r.FormValue("title"),
		IsGlobalAdmin: isGlobalAdmin,
		OrgType:       models.OrgType(r.FormValue("org_type")),
		OrgName:       r.FormValue("org_name"),
		Phone:         r.FormValue("phone"),
		MobilePhone:   r.FormValue("mobile_phone"),
		ShortPhone:    r.FormValue("short_phone"),
		ShortMobile:   r.FormValue("short_mobile"),
		Email:         r.FormValue("email"),
		IsActive:      isActive,
		// vlastita adresa e-pošte mijenja se uz trenutnu lozinku i ne izvana
		TrenutnaLozinka: r.FormValue("trenutna_lozinka"),
		Izvana:          dolaziIzvana(r),
	}
	// Okvir „adresa je provjerena” stoji samo na obrascu globalnog
	// administratora za tuđi račun, uz oznaku obrasca: neoznačen okvir tada
	// briše potvrdu, a bez oznake (drugi obrasci) potvrda se ne dira. Tuđim
	// očima okvira nema, a poslan se odbija.
	if r.FormValue("pin_adresa_obrazac") == "1" {
		potvrdi := r.FormValue("pin_adresa_potvrdena") == "1"
		req.PotvrdaAdrese = &potvrdi
	}
	req.TudjimOcima = viewBanner(r).Viewing

	_, err = h.azurirajUzLozinku(perms, req)
	if err != nil {
		redirectWith(w, r, "/users/"+userID.String()+"/edit", "error", err.Error())
		return
	}

	redirectWith(w, r, "/users/"+userID.String(), "success", "Profil je spremljen.")
}

// HandleUpdateProfile omogućuje prijavljenom djelatniku izmjenu vlastitog profila sa bilo koje stranice sustava
func (h *UsersHandler) HandleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/?error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}

	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if perms == nil || perms.User.ID == uuid.Nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	returnURL := povratnaAdresaProfila(r, "/")

	req := service.UpdateUserRequest{
		ID:            perms.User.ID,
		Username:      perms.User.Username,
		FullName:      r.FormValue("full_name"),
		Title:         r.FormValue("title"),
		IsGlobalAdmin: perms.User.IsGlobalAdmin,
		OrgType:       perms.User.OrgType,
		OrgName:       perms.User.OrgName,
		Phone:         r.FormValue("phone"),
		MobilePhone:   r.FormValue("mobile_phone"),
		ShortPhone:    r.FormValue("short_phone"),
		ShortMobile:   r.FormValue("short_mobile"),
		Email:         r.FormValue("email"),
		IsActive:      perms.User.IsActive,
		// na adresu e-pošte ide PIN za prijavu izvana: mijenja se uz
		// trenutnu lozinku, samo na dopuštenu domenu i ne izvana
		TrenutnaLozinka: r.FormValue("trenutna_lozinka"),
		Izvana:          dolaziIzvana(r),
	}

	updated, err := h.azurirajUzLozinku(perms, req)
	if err != nil {
		http.Redirect(w, r, returnURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	if updated != nil {
		perms.User = *updated
	}
	http.Redirect(w, r, returnURL+"?success="+url.QueryEscape("Vaš profil je uspješno ažuriran"), http.StatusSeeOther)
}

// azurirajUzLozinku sprema izmjenu računa; kad je upisana trenutna lozinka
// (promjena vlastite adrese e-pošte), kriva se broji kao i pri promjeni
// lozinke, da obrazac ne postane proročište za pogađanje
func (h *UsersHandler) azurirajUzLozinku(perms *models.UserPermissions, req service.UpdateUserRequest) (*models.User, error) {
	if req.TrenutnaLozinka == "" || perms == nil || perms.User.ID != req.ID {
		return h.userService.UpdateUser(perms, req)
	}
	kljuc := kljucPonovneLozinke("", perms.User.ID.String())
	if err := ponovnaLozinkaDopustena(kljuc); err != nil {
		return nil, err
	}
	u, err := h.userService.UpdateUser(perms, req)
	// točna lozinka briše brojač samo kad je doista provjerena (adresa se
	// mijenjala); inače bi spremanje bez promjene adrese brisalo krive upise
	if err != nil || !strings.EqualFold(strings.TrimSpace(req.Email), strings.TrimSpace(perms.User.Email)) {
		ishodPonovneLozinke(kljuc, err)
	}
	return u, err
}

// HandleAddDuty dodjeljuje dodatnu funkciju, zaduženje dionica ili privremenu ispomoć
func (h *UsersHandler) HandleAddDuty(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/users?error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}

	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	userID, err := uuid.Parse(r.FormValue("user_id"))
	if err != nil {
		http.Redirect(w, r, "/users?error=Neispravan+korisnik", http.StatusSeeOther)
		return
	}

	req := dutyRequestFromForm(r, userID)
	// primarnu dužnost i vlastiti naziv daje samo tko uređuje cijeli račun;
	// drugome se dužnost doda kao ispomoć, i to mu se kaže
	ispomoc := (req.IsPrimary || req.Title != "") && !h.userService.SmijeUredjivatiRacun(perms, userID)

	err = h.userService.AddDuty(perms, req)
	if err != nil {
		redirectWith(w, r, "/users/"+userID.String()+"/duties/new", "error", err.Error())
		return
	}

	if ispomoc {
		redirectWith(w, r, "/users/"+userID.String(), "success", "Zaduženje je dodano kao ispomoć, pod nazivom uloge: primarnu dužnost i vlastiti naziv daje onaj tko uređuje cijeli račun (viša razina ili globalni administrator).")
		return
	}
	redirectWith(w, r, "/users/"+userID.String(), "success", "Zaduženje je dodano.")
}

// dutyRequestFromForm čita polja obrasca zaduženja, ista za novo i za izmjenu
func dutyRequestFromForm(r *http.Request, userID uuid.UUID) service.AddDutyRequest {
	var sectorPtr *string
	if v := r.FormValue("sector_id"); v != "" {
		sectorPtr = &v
	}
	var areaPtr *int
	if v, _ := strconv.Atoi(r.FormValue("area_id")); v > 0 {
		areaPtr = &v
	}
	return service.AddDutyRequest{
		UserID:       userID,
		Title:        strings.TrimSpace(r.FormValue("title")),
		Role:         models.Role(r.FormValue("role")),
		SectorID:     sectorPtr,
		AreaID:       areaPtr,
		SectionCodes: strings.TrimSpace(r.FormValue("section_codes")),
		IsPrimary:    r.FormValue("is_primary") == "1" || r.FormValue("is_primary") == "on",
		IsTemporary:  r.FormValue("is_temporary") == "1" || r.FormValue("is_temporary") == "on",
		Reason:       strings.TrimSpace(r.FormValue("reason")),
		ExpiresAt:    rokIzObrasca(r), // „Vrijedi zaključno s”: prestaje idući dan u 0 h
		// istek s obranom ima smisla samo uz privremenu; servis ga za stalnu briše
		IsticeSObranom: r.FormValue("istece_s_obranom") == "1" || r.FormValue("istece_s_obranom") == "on",
	}
}

// HandleUpdateDuty sprema izmjenu postojećeg zaduženja
func (h *UsersHandler) HandleUpdateDuty(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/users?error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	dutyID, err := uuid.Parse(r.PathValue("duty"))
	if err != nil {
		http.Redirect(w, r, "/users?error=Neispravan+ID+dužnosti", http.StatusSeeOther)
		return
	}
	userID, err := uuid.Parse(r.FormValue("user_id"))
	if err != nil {
		http.Redirect(w, r, "/users?error=Neispravan+korisnik", http.StatusSeeOther)
		return
	}
	if err := h.userService.UpdateDuty(perms, dutyID, dutyRequestFromForm(r, userID)); err != nil {
		redirectWith(w, r, "/users/duties/"+dutyID.String()+"/edit", "error", err.Error())
		return
	}
	redirectWith(w, r, "/users/"+userID.String(), "success", "Zaduženje je izmijenjeno.")
}

// HandleRevokeDuty opoziva funkciju ili privremenu ispomoć
func (h *UsersHandler) HandleRevokeDuty(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/users?error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}

	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	dutyID, err := uuid.Parse(r.FormValue("duty_id"))
	if err != nil {
		http.Redirect(w, r, "/users?error=Neispravan+ID+dužnosti", http.StatusSeeOther)
		return
	}

	back := "/users"
	if uid := strings.TrimSpace(r.FormValue("user_id")); uid != "" {
		back = "/users/" + uid
	}
	err = h.userService.RevokeDuty(perms, dutyID)
	if err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}

	redirectWith(w, r, back, "success", "Zaduženje je opozvano; u povijesti ostaje.")
}

// HandleGetAreasAPI vraća JSON listu branjenih područja
func (h *UsersHandler) HandleGetAreasAPI(w http.ResponseWriter, r *http.Request) {
	sectorID := r.URL.Query().Get("sector")
	areas, err := h.userService.ListAreas(sectorID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(areas)
}

// HandleDeleteUser briše profil djelatnika
func (h *UsersHandler) HandleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Metoda nije dozvoljena", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/users?error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}

	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	userID, err := uuid.Parse(r.FormValue("user_id"))
	if err != nil {
		http.Redirect(w, r, "/users?error=Neispravan+ID+korisnika", http.StatusSeeOther)
		return
	}

	if err := h.userService.DeleteUser(perms, userID); err != nil {
		redirectWith(w, r, "/users/"+userID.String(), "error", err.Error())
		return
	}

	http.Redirect(w, r, "/users?success=Profil+djelatnika+uspješno+obrisan", http.StatusSeeOther)
}
