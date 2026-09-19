package web

import (
	"fmt"
	"net/http"
	"time"

	"gocop/internal/models"
	"gocop/internal/potpis"
)

func (h *JournalsHandler) potpisnikCOP(r *http.Request, u *models.User) (*potpis.Potpisnik, error) {
	if h.potpis == nil || h.potpis() == nil {
		return nil, fmt.Errorf("servis elektroničkog potpisa nije dostupan")
	}
	viewing, _ := r.Context().Value(contextKeyViewing).(bool)
	if viewing {
		if h.opcije != nil && h.opcije(r.Context()).SimulacijaKljuca {
			return h.potpis().Simulirani(r.Context(), u)
		}
		return nil, fmt.Errorf("gledanjem tuđim očima nije dopušteno potpisivanje; za test uključite Simulaciju ključa u postavkama")
	}
	if !h.potpis().Ima(r.Context(), u.ID.String()) {
		return nil, fmt.Errorf("prije ovjere izradite svoj potpisni ključ na profilu")
	}
	lozinka := r.FormValue("lozinka")
	if lozinka == "" {
		return nil, fmt.Errorf("upišite lozinku: njome otključavate svoj potpisni ključ")
	}
	return h.potpis().Potpisnik(r.Context(), u, lozinka)
}

func (h *JournalsHandler) HandleOvjeraCOP(w http.ResponseWriter, r *http.Request) {
	j, _, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	u, perms := h.base(r)
	back := "/dnevnici/" + j.ID
	if j.CentarSektor == "" {
		redirectWith(w, r, back, "error", "Ovo nije dnevnik COP-a")
		return
	}
	p, err := h.potpisnikCOP(r, u)
	if err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	zapisi, err := h.journals.EntriesForJournal(r.Context(), j.ID)
	if err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	sad := time.Now()
	radnja := r.FormValue("radnja")
	var pdf []byte
	var m mjestaPotpisaCOP
	if radnja == "zakljuci" {
		if err = h.journals.PripremiOvjeruCOP(u, perms, j, sad); err == nil {
			pdf, m = PDFDnevnikCOP(j, zapisi)
		}
	} else {
		err = fmt.Errorf("nepoznata radnja ovjere")
	}
	if err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	x, kad := m.xZakljucio, *j.ZakljucenoAt
	dod := dodatakPotpisaCOP(m, x, j, u.FullName, "zaključio i ovjerio", kad, p)
	pdf, err = p.PotpisiPDF(pdf, dod)
	if err == nil {
		err = h.journals.SpremiOvjeruCOP(r.Context(), j, pdf)
	}
	if err != nil {
		redirectWith(w, r, back, "error", "Dnevnik nije ovjeren: "+err.Error())
		return
	}
	poruka := "Dnevnik je zaključan, renderiran i elektronički potpisan; rukovoditelj sektora dobiva ga na znanje."
	redirectWith(w, r, back, "success", poruka)
}

func (h *JournalsHandler) IzvoziDnevnikPDF(w http.ResponseWriter, r *http.Request) {
	j, _, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	if j.CentarSektor == "" {
		http.NotFound(w, r)
		return
	}
	pdf, err := h.journals.IzvornikCOP(r.Context(), j.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if j.Ovjeren() {
		// ovjeren dnevnik ima samo jedan PDF: potpisani izvornik; bez njega ili
		// s izmijenjenim ne daje se ništa što bi se moglo držati za izvornik
		if st := h.journals.ProvjeriIzvornikCOP(r.Context(), j); !st.Ispravan {
			http.Error(w, st.Greska, http.StatusConflict)
			return
		}
	}
	if len(pdf) == 0 {
		zapisi, e := h.journals.EntriesForJournal(r.Context(), j.ID)
		if e != nil {
			http.Error(w, e.Error(), http.StatusInternalServerError)
			return
		}
		pdf, _ = PDFDnevnikCOP(j, zapisi)
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="dnevnik-cop.pdf"`)
	_, _ = w.Write(pdf)
}
