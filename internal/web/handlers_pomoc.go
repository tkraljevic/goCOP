package web

import (
	"html/template"
	"net/http"

	"gocop/internal/models"
)

// Stranica pomoći. Jedna stranica za cijeli program, a znak "?" u zaglavlju
// svake stranice vodi na njezin odjeljak — sidro je ActiveNav, isto ono po
// kojem se u izborniku zna gdje ste. Tako pomoć ne treba znati ništa o
// stranici s koje se došlo, a ipak otvara pravo mjesto.

type PomocHandler struct {
	tmpl *template.Template
}

func NewPomocHandler(tmpl *template.Template) *PomocHandler {
	return &PomocHandler{tmpl: tmpl}
}

type PomocPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

func (h *PomocHandler) ShowPomoc(w http.ResponseWriter, r *http.Request) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	data := PomocPageData{
		CurrentUser: u, Permissions: perms,
		ActiveNav: "pomoc", ViewAsBanner: viewBanner(r),
	}
	if err := h.tmpl.ExecuteTemplate(w, "pomoc.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
