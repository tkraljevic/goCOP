package web

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/service"
)

// Adresar tvrtke iz Exchangea: traženje kolega i usporedba imenika
// goCOP-a s adresama i telefonima iz sustava Windows.

// SetImenik daje rukovatelju predložak stranice adresara
func (h *AktiHandler) SetImenik(t *template.Template) { h.tmplImenik = t }

// ImenikData je stranica adresara
type ImenikData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage string
	ErrorMessage   string

	Upit           string
	Kontakti       []posta.Kontakt
	Usporedba      []service.UsporedbaKontakta
	Usporedio      bool
	Sektor         string
	Sektori        []models.Sector
	SRazlikom      int
	NijeNadjeno    int
	TrebaLozinku   bool
	SmijeUskladiti bool
}

// ShowImenik traži u adresaru i, na zahtjev, uspoređuje cijeli imenik
func (h *AktiHandler) ShowImenik(w http.ResponseWriter, r *http.Request) {
	u, perms, base := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	q := r.URL.Query()
	d := ImenikData{CurrentUser: u, Permissions: perms, ActiveNav: "users", ViewAsBanner: viewBanner(r), SuccessMessage: base.SuccessMessage, ErrorMessage: base.ErrorMessage,
		Upit: strings.TrimSpace(q.Get("trazi")), Sektor: q.Get("sektor"), Sektori: base.Sektori,
		SmijeUskladiti: perms != nil && (perms.IsGlobalAdmin || len(perms.AdminSectors) > 0)}
	if _, kad := s.RacunPoste(r.Context(), u.ID.String()); kad.IsZero() {
		d.TrebaLozinku = true
	}
	var err error
	switch {
	case d.Upit != "":
		d.Kontakti, err = s.Imenik(r.Context(), u, d.Upit)
	case q.Get("usporedi") == "1" && d.SmijeUskladiti:
		d.Usporedio = true
		d.Usporedba, err = s.UsporediImenik(r.Context(), perms, u, d.Sektor)
		for _, x := range d.Usporedba {
			if x.Kontakt == nil {
				d.NijeNadjeno++
			} else if len(x.Razlike) > 0 {
				d.SRazlikom++
			}
		}
	}
	if err != nil && d.ErrorMessage == "" {
		d.ErrorMessage = err.Error()
	}
	if err := h.tmplImenik.ExecuteTemplate(w, "imenik_exchange.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleImenikPrimijeni upisuje odabrane vrijednosti iz adresara u djelatnike
func (h *AktiHandler) HandleImenikPrimijeni(w http.ResponseWriter, r *http.Request) {
	_, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/users/exchange", "error", "Neispravan obrazac")
		return
	}
	// odabir: "p" = userID|polje; vrijednost u v_userID_polje
	poUseru := map[string]map[string]string{}
	for _, o := range r.Form["p"] {
		dio := strings.SplitN(o, "|", 2)
		if len(dio) != 2 {
			continue
		}
		v := r.FormValue("v_" + dio[0] + "_" + dio[1])
		if v == "" {
			continue
		}
		if poUseru[dio[0]] == nil {
			poUseru[dio[0]] = map[string]string{}
		}
		poUseru[dio[0]][dio[1]] = v
	}
	n, greske := 0, 0
	for id, polja := range poUseru {
		if err := s.PrimijeniKontakt(r.Context(), perms, id, polja); err != nil {
			greske++
			continue
		}
		n++
	}
	natrag := "/users/exchange?usporedi=1&sektor=" + r.FormValue("sektor")
	if greske > 0 {
		redirectWith(w, r, natrag, "error", "Ažurirano djelatnika: "+strconv.Itoa(n)+", nije uspjelo: "+strconv.Itoa(greske)+" (nemate pravo uređivati te račune)")
		return
	}
	redirectWith(w, r, natrag, "success", "Iz adresara tvrtke ažurirano djelatnika: "+strconv.Itoa(n)+".")
}
