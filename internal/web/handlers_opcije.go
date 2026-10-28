package web

import (
	"html/template"
	"net/http"
	"strconv"

	"gocop/internal/models"
)

// Opcije: opći prekidači programa u Administraciji, s objašnjenjem

// OpcijeData je stranica opcija
type OpcijeData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage string
	ErrorMessage   string

	Opcije models.Opcije
}

// SetOpcije daje rukovatelju predložak stranice opcija
func (h *AktiHandler) SetOpcije(t *template.Template) { h.tmplOpcije = t }

// ShowOpcije prikazuje prekidače
func (h *AktiHandler) ShowOpcije(w http.ResponseWriter, r *http.Request) {
	u, perms, base := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	d := OpcijeData{CurrentUser: u, Permissions: perms, ActiveNav: "admin", ViewAsBanner: viewBanner(r), SuccessMessage: base.SuccessMessage, ErrorMessage: base.ErrorMessage,
		Opcije: s.Opcije(r.Context())}
	if err := h.tmplOpcije.ExecuteTemplate(w, "administracija_opcije.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleOpcije sprema prekidače
func (h *AktiHandler) HandleOpcije(w http.ResponseWriter, r *http.Request) {
	_, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	o := models.Opcije{
		BrisanjeOvjerenihAkata:   r.FormValue("brisanje_ovjerenih_akata") == "1",
		BrisanjePovijestiVerzija: r.FormValue("brisanje_povijesti_verzija") == "1",
		BrisanjeSOglasnePloce:    r.FormValue("brisanje_s_oglasne_ploce") == "1",
		UpisTudjimOcima:          r.FormValue("upis_tudjim_ocima") == "1",
		SimulacijaKljuca:         r.FormValue("simulacija_kljuca") == "1",
	}
	o.CuvanjeSlikaDana, _ = strconv.Atoi(r.FormValue("cuvanje_slika_dana"))
	o.SlikeOdmah = r.FormValue("slike_odmah") == "1"
	if err := s.SpremiOpcije(r.Context(), perms, o); err != nil {
		redirectWith(w, r, "/administracija/opcije", "error", err.Error())
		return
	}
	redirectWith(w, r, "/administracija/opcije", "success", "Opcije su spremljene i vrijede na svim čvorovima.")
}
