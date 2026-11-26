package web

import (
	"context"
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
	return otkljucajUzOgranicenje(r.Context(), u, func() (*potpis.Potpisnik, error) { return h.potpis().Potpisnik(r.Context(), u, lozinka) })
}

// pripremiOvjeruCOP priprema dnevnik za prvi potpis i provjerava da je plan
// dežurstava spreman za zaključavanje. Najprije se gleda tko ovjerava, pa
// tek onda stanje plana.
func (h *JournalsHandler) pripremiOvjeruCOP(ctx context.Context, u *models.User, perms *models.UserPermissions, j *models.Journal, sad time.Time) error {
	if err := h.journals.PripremiOvjeruCOP(u, perms, j, sad); err != nil {
		return err
	}
	return h.journals.PlanSpremanZaOvjeru(ctx, j)
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
	// Vrijeme ovjere uzima se prije čitanja zapisa: zapis spremljen između
	// ta dva trenutka ulazi u izvornik, a nijedan zapis koji u izvornik nije
	// ušao nema vrijeme upisa prije ovjere (oznaka „nastao nakon ovjere”)
	sad := time.Now()
	zapisi, err := h.journals.EntriesForJournal(r.Context(), j.ID)
	if err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	radnja := r.FormValue("radnja")
	var pdf []byte
	var m mjestaPotpisaCOP
	if radnja == "zakljuci" {
		if err = h.pripremiOvjeruCOP(r.Context(), u, perms, j, sad); err == nil {
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
	pdf, gotovo := h.izvornikDnevnikaZaIspis(w, r, j)
	if gotovo {
		return
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

// porukaDnevnikPDFNijeOvdje: izvornik ovjerenog dnevnika postoji, a PDF nije
// na ovom računalu. Dnevnik COP-a je u zajedničkom kanalu, pa ga svako
// računalo traži i ne otpušta; ipak ne obećava da će stići, jer ga možda
// nema nijedno računalo do kojeg ovo dopire.
const porukaDnevnikPDFNijeOvdje = "PDF dnevnika nije otvoren: na ovom računalu nema izvornika dnevnika, PDF-a kakav je ovjeren. " +
	"Ovo računalo ga traži razmjenom; ako vam treba sada, otvorite ga na računalu na kojem je dnevnik ovjeren."

// porukaDnevnikNijeProcitan: zapis izvornika se ne da pročitati iz baze
const porukaDnevnikNijeProcitan = "PDF dnevnika nije otvoren: izvornik dnevnika nije se dao pročitati. " +
	"Pokušajte ponovno; ako se ponovi, javite administratoru."

// izvornikDnevnikaZaIspis daje potpisani izvornik ovjerenog dnevnika, a za
// neovjeren dnevnik izvornik ako ga ima, inače nil, pa se dnevnik crta
// iznova. Ovjeren dnevnik ima samo jedan PDF: kad izvornik nije ovdje, nije
// ispravan ili se ne da pročitati, sam odgovori i javi gotovo, jer crtež
// bi izgledao kao izvornik, a potpisa nema.
func (h *JournalsHandler) izvornikDnevnikaZaIspis(w http.ResponseWriter, r *http.Request, j *models.Journal) (pdf []byte, gotovo bool) {
	if !j.Ovjeren() {
		b, err := h.journals.IzvornikCOP(r.Context(), j.ID)
		if err != nil {
			http.Error(w, porukaDnevnikNijeProcitan, http.StatusInternalServerError)
			return nil, true
		}
		return b, false
	}
	b, st, err := h.journals.IzvornikOvjerenogCOP(r.Context(), j)
	switch {
	case err != nil:
		http.Error(w, porukaDnevnikNijeProcitan, http.StatusInternalServerError)
	case st.NijeOvdje:
		redirectWith(w, r, "/dnevnici/"+j.ID, "error", porukaDnevnikPDFNijeOvdje)
	case !st.Ispravan:
		http.Error(w, st.Greska, http.StatusConflict)
	default:
		return b, false
	}
	return nil, true
}
