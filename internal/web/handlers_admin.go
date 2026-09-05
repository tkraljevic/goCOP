package web

import (
	"html/template"
	"net/http"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Administracija je ulazna stranica za posao koji radi samo administrator:
// ustroj obrane (sektori i branjena područja), ovlasti i vidljivost modula,
// čvor i mreža, te uvozi podataka. Sve to postoji i na svojim stranicama;
// ovdje stoji na jednom mjestu, da administrator ne traži po izborniku.

type AdminHandler struct {
	org        *service.OrgService
	users      *service.UserService
	tmplAdmin  *template.Template
	tmplImport *template.Template
}

func NewAdminHandler(org *service.OrgService, users *service.UserService, admin, imports *template.Template) *AdminHandler {
	return &AdminHandler{org: org, users: users, tmplAdmin: admin, tmplImport: imports}
}

// AdminPageData je ulazna stranica administracije sa stanjem ustroja
type AdminPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions

	Sectors   int
	Areas     int
	Users     int
	Locked    int // računi koji čekaju promjenu lozinke
	Inactive  int
	NoContact bool // administrator nema kontakt, pa ga stranica prijave ne pokazuje

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

func (h *AdminHandler) pageData(r *http.Request) AdminPageData {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return AdminPageData{
		CurrentUser: currUser, Permissions: perms,
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "admin", ViewAsBanner: viewBanner(r),
	}
}

// ShowAdmin prikazuje ulaznu stranicu administracije
func (h *AdminHandler) ShowAdmin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.pageData(r)

	if sectors, err := h.org.ListSectors(ctx); err == nil {
		data.Sectors = len(sectors)
	}
	if areas, err := h.org.ListAreas(ctx, ""); err == nil {
		data.Areas = len(areas)
	}
	if users, err := h.users.ListUsers("", 0, "", "", ""); err == nil {
		data.Users = len(users)
		for _, u := range users {
			if u.MustChangePassword {
				data.Locked++
			}
			if !u.IsActive {
				data.Inactive++
			}
		}
	}
	if _, _, _, ok := h.users.GlobalAdminContact(); !ok {
		data.NoContact = true
	}

	if err := h.tmplAdmin.ExecuteTemplate(w, "administracija.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowImports prikazuje što se i odakle uvozi u ovaj čvor
func (h *AdminHandler) ShowImports(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if err := h.tmplImport.ExecuteTemplate(w, "uvozi.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
