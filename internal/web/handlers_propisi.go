package web

import (
	"bytes"
	"errors"
	"html/template"
	"log"
	"net/http"
	"strings"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Rečenice stranica propisa kad nešto nije u redu (Odluke (25. 11. 2026.))
const (
	porukaPropisiNisuSpremni = "Propisi na ovom računalu još nisu spremni. Pokušajte za minutu."
	porukaNemaPropisa        = "Propisa s tom adresom nema. Možda je uklonjen ili još nije stigao na ovo računalo."
	porukaPropisNeCita       = "Propis se sada ne može pročitati. Pokušajte ponovno za koji trenutak."
)

// PropisiHandler daje popis propisa, pretragu, naslovnicu propisa, stalnu
// adresu dijela i stranicu izmjene
type PropisiHandler struct {
	svc                       func() *service.PropisiService
	tPopis, tPropis, tIzmjena *template.Template
}

// NewPropisiHandler sastavlja rukovatelja; servis se čita pri svakom
// zahtjevu, jer se spaja nakon ruta
func NewPropisiHandler(svc func() *service.PropisiService, popis, propis, izmjena *template.Template) *PropisiHandler {
	return &PropisiHandler{svc: svc, tPopis: popis, tPropis: propis, tIzmjena: izmjena}
}

// PropisiPageData su stranice propisa: popis s pretragom, naslovnica ili
// dio propisa i izmjena
type PropisiPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions

	Skupine  []service.SkupinaPropisa
	Pretraga *service.RezultatPretrage
	Upit     string // upisano u polje pretrage
	Odabran  string // adresa propisa uz pretragu
	Stranica *service.StranicaPropisa
	Dio      *service.StranicaDijela
	Izmjena  *service.StranicaIzmjene

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// JeAdministrator: gumbi i trake za globalnog administratora
func (d PropisiPageData) JeAdministrator() bool {
	return d.Permissions != nil && d.Permissions.IsGlobalAdmin
}

func (h *PropisiHandler) podaci(r *http.Request) PropisiPageData {
	d := PropisiPageData{ActiveNav: "journals", ViewAsBanner: viewBanner(r),
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error")}
	d.CurrentUser, _ = r.Context().Value(contextKeyUser).(*models.User)
	d.Permissions, _ = r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	return d
}

// servis je servis propisa; dok nije spojen, odgovor je 503 s rečenicom
func (h *PropisiHandler) servis(w http.ResponseWriter) *service.PropisiService {
	svc := h.svc()
	if svc == nil {
		http.Error(w, porukaPropisiNisuSpremni, http.StatusServiceUnavailable)
	}
	return svc
}

// Popis su propisi po skupinama, a s ?q= rezultat pretrage. Broj točke ili
// članka koji pogađa točno jedan dio vodi ravno na njega.
func (h *PropisiHandler) Popis(w http.ResponseWriter, r *http.Request) {
	svc := h.servis(w)
	if svc == nil {
		return
	}
	d := h.podaci(r)
	d.Upit, d.Odabran = strings.TrimSpace(r.URL.Query().Get("q")), r.URL.Query().Get("propis")
	if d.Upit == "" {
		h.popisSkupina(w, r, svc, d, http.StatusOK)
		return
	}
	rez, err := svc.Trazi(r.Context(), d.Upit, d.Odabran)
	if err != nil {
		h.greska(w, r, svc, err, "")
		return
	}
	if rez.Izravno != nil {
		http.Redirect(w, r, rez.Izravno.Adresa, http.StatusSeeOther)
		return
	}
	d.Pretraga = rez
	iscrtajPropise(w, h.tPopis, "propisi.html", http.StatusOK, d)
}

// popisSkupina iscrtava popis propisa po skupinama, uz zadani status
func (h *PropisiHandler) popisSkupina(w http.ResponseWriter, r *http.Request, svc *service.PropisiService, d PropisiPageData, status int) {
	skupine, err := svc.Popis(r.Context())
	if err != nil {
		log.Printf("popis propisa: %v", err)
		http.Error(w, porukaPropisNeCita, http.StatusInternalServerError)
		return
	}
	d.Skupine = skupine
	iscrtajPropise(w, h.tPopis, "propisi.html", status, d)
}

// Propis je naslovnica propisa
func (h *PropisiHandler) Propis(w http.ResponseWriter, r *http.Request) {
	svc := h.servis(w)
	if svc == nil {
		return
	}
	st, err := svc.Propis(r.Context(), r.PathValue("propis"))
	if err != nil {
		h.greska(w, r, svc, err, "")
		return
	}
	d := h.podaci(r)
	d.Stranica = st
	iscrtajPropise(w, h.tPropis, "propis.html", http.StatusOK, d)
}

// Dio je stalna adresa dijela propisa
func (h *PropisiHandler) Dio(w http.ResponseWriter, r *http.Request) {
	svc := h.servis(w)
	if svc == nil {
		return
	}
	sd, err := svc.Dio(r.Context(), r.PathValue("propis"), r.PathValue("dio"))
	if err != nil {
		h.greska(w, r, svc, err, "dijela „"+r.PathValue("dio")+"”")
		return
	}
	d := h.podaci(r)
	d.Stranica, d.Dio = &sd.StranicaPropisa, sd
	iscrtajPropise(w, h.tPropis, "propis.html", http.StatusOK, d)
}

// Izmjena je tekst izmjene propisa s tablicom odredbi
func (h *PropisiHandler) Izmjena(w http.ResponseWriter, r *http.Request) {
	svc := h.servis(w)
	if svc == nil {
		return
	}
	st, err := svc.Izmjena(r.Context(), r.PathValue("propis"), r.PathValue("izmjena"))
	if err != nil {
		h.greska(w, r, svc, err, "izmjene „"+r.PathValue("izmjena")+"”")
		return
	}
	d := h.podaci(r)
	d.Izmjena = st
	iscrtajPropise(w, h.tIzmjena, "propis_izmjena.html", http.StatusOK, d)
}

// greska odgovara na grešku čitanja: propisa nema (404 s popisom), propis
// nema dijela ili izmjene (404 s naslovnicom propisa) ili kvar (500).
// Sto je ono čega nema, u genitivu: „dijela „clanak-9””.
func (h *PropisiHandler) greska(w http.ResponseWriter, r *http.Request, svc *service.PropisiService, err error, sto string) {
	d := h.podaci(r)
	switch {
	case errors.Is(err, service.ErrNemaPropisa):
		d.ErrorMessage = porukaNemaPropisa
		h.popisSkupina(w, r, svc, d, http.StatusNotFound)
		return
	case errors.Is(err, service.ErrNemaDijela), errors.Is(err, service.ErrNemaIzmjene):
		st, errP := svc.Propis(r.Context(), r.PathValue("propis"))
		if errP == nil {
			d.Stranica = st
			d.ErrorMessage = "U propisu „" + st.Propis.Naziv + "” nema " + sto + ". Pogledajte sadržaj propisa."
			iscrtajPropise(w, h.tPropis, "propis.html", http.StatusNotFound, d)
			return
		}
		err = errP
	}
	log.Printf("propisi (%s): %v", r.URL.Path, err)
	http.Error(w, porukaPropisNeCita, http.StatusInternalServerError)
}

// iscrtajPropise iscrtava stranicu u memoriju, pa je šalje sa statusom;
// kvar predloška ne ostavlja pola stranice
func iscrtajPropise(w http.ResponseWriter, t *template.Template, ime string, status int, d PropisiPageData) {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, ime, d); err != nil {
		log.Printf("predložak %s: %v", ime, err)
		http.Error(w, porukaPropisNeCita, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w) // zaglavlje je već poslano; prekinutu vezu nema kome javiti
}
