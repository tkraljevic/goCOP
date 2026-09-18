package web

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/service"
)

// Sandučić: korisnikova ulazna pošta iz Exchangea, čitanje pisama i
// privitaka, i učitavanje potpisanog PDF-a iz SIGNATOR-a ravno u nacrt akta.

const pisamaPoStranici = 50

// SetSanducic daje rukovatelju predloške sandučića
func (h *AktiHandler) SetSanducic(popis, pismo *template.Template) {
	h.tmplSanducic, h.tmplPismo = popis, pismo
}

// SanducicData je stranica popisa pisama ili jednog pisma
type SanducicData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage string
	ErrorMessage   string

	Pisma           []posta.Pismo
	Ukupno          int
	Stranica        int
	Stranica_       int // sljedeća, 0 kad je nema
	Prethodna       int
	Pismo           *posta.Pismo
	Nacrti          []models.Akt // nacrti koji čekaju potpisani PDF
	OdabraniAkt     string
	TrebaLozinku    bool
	NijeUkljuceno   bool
	LozinkaOdbijena bool
}

func (h *AktiHandler) sanducicData(r *http.Request) (*service.AktService, SanducicData) {
	u, perms, _ := h.base(r)
	q := r.URL.Query()
	return h.akti(), SanducicData{CurrentUser: u, Permissions: perms, ActiveNav: "posta", ViewAsBanner: viewBanner(r),
		SuccessMessage: q.Get("success"), ErrorMessage: q.Get("error"), OdabraniAkt: q.Get("akt")}
}

// greskaSanducica pretvori grešku u stanje stranice
func (d *SanducicData) greska(err error) {
	switch {
	case errors.Is(err, service.ErrNemaLozinkePoste):
		d.TrebaLozinku = true
	case errors.Is(err, posta.ErrPrijava):
		d.LozinkaOdbijena = true
	case strings.Contains(err.Error(), "nije uključena"):
		d.NijeUkljuceno = true
	default:
		d.ErrorMessage = err.Error()
	}
}

// ShowSanducic prikazuje ulaznu poštu
func (h *AktiHandler) ShowSanducic(w http.ResponseWriter, r *http.Request) {
	s, d := h.sanducicData(r)
	if s == nil || d.CurrentUser == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	d.Stranica, _ = strconv.Atoi(r.URL.Query().Get("stranica"))
	if d.Stranica < 1 {
		d.Stranica = 1
	}
	pisma, ukupno, err := s.Sanducic(r.Context(), d.CurrentUser, d.Stranica, pisamaPoStranici)
	if err != nil {
		d.greska(err)
	}
	d.Pisma, d.Ukupno = pisma, ukupno
	if d.Stranica*pisamaPoStranici < ukupno {
		d.Stranica_ = d.Stranica + 1
	}
	d.Prethodna = d.Stranica - 1
	if err := h.tmplSanducic.ExecuteTemplate(w, "posta_sanducic.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowPismo prikazuje jedno pismo
func (h *AktiHandler) ShowPismo(w http.ResponseWriter, r *http.Request) {
	s, d := h.sanducicData(r)
	if s == nil || d.CurrentUser == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	pismo, err := s.Pismo(r.Context(), d.CurrentUser, r.URL.Query().Get("id"))
	if err != nil {
		d.greska(err)
	}
	d.Pismo = pismo
	if pismo != nil {
		for _, p := range pismo.Privitci {
			if p.JePDF() {
				d.Nacrti = s.NacrtiZaPotpis(r.Context(), d.Permissions, d.CurrentUser)
				break
			}
		}
	}
	if err := h.tmplPismo.ExecuteTemplate(w, "posta_pismo.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Privitak daje datoteku privitka
func (h *AktiHandler) Privitak(w http.ResponseWriter, r *http.Request) {
	u, _, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	p, podaci, err := s.Privitak(r.Context(), u, r.URL.Query().Get("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	vrsta := p.Vrsta
	if vrsta == "" {
		vrsta = "application/octet-stream"
	}
	w.Header().Set("Content-Type", vrsta)
	w.Header().Set("Content-Disposition", `attachment; filename="`+sigurnoIme(strings.TrimSuffix(p.Ime, ".pdf"))+pathExt(p.Ime)+`"`)
	_, _ = w.Write(podaci)
}

func pathExt(ime string) string {
	if i := strings.LastIndex(ime, "."); i >= 0 {
		return strings.ToLower(ime[i:])
	}
	return ""
}

// HandlePotpisaniIzPoste učita PDF privitak iz sandučića kao potpisani akt
func (h *AktiHandler) HandlePotpisaniIzPoste(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	pismoID, privitakID, aktID := r.FormValue("pismo"), r.FormValue("privitak"), r.FormValue("akt")
	natrag := "/posta/pismo?" + url.Values{"id": {pismoID}}.Encode()
	if aktID == "" {
		redirectWith(w, r, natrag, "error", "Odaberite nacrt akta u koji se potpisani PDF učitava")
		return
	}
	_, pdf, err := s.Privitak(r.Context(), u, privitakID)
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	a, upozorenja, err := s.UcitajPotpisani(r.Context(), perms, u, aktID, pdf)
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	poruka := "Akt " + a.Oznaka() + " je ovjeren kvalificiranim potpisom (" + a.Kvalificirani.Ime + ") iz sandučića; potpisani PDF je izvornik."
	if len(upozorenja) > 0 {
		poruka += " Stanje obrane na dionicama: " + strings.Join(upozorenja, "; ")
	}
	redirectWith(w, r, "/akti/"+a.ID, "success", poruka)
}
