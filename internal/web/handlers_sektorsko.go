package web

import (
	"html/template"
	"net/http"
	"strings"
	"time"

	"gocop/internal/models"
)

// Sektorsko dnevno izvješće: voditelj COP-a slaže ga iz izvješća dionica i
// zapisa dnevnika, pod /sektorsko-izvjesce. Rukovatelj je isti kao za
// izvješća dionica, s dva svoja predloška.

// SektorskoPageData je ono što stranice sektorskog izvješća trebaju
type SektorskoPageData struct {
	IzvjescaPageData
	Sektorsko *models.SektorskoIzvjesce
	Sektor    string
	Centar    string

	// obrazac: što je dostupno za taj dan i što je uključeno
	Dostupna        []models.DnevnoIzvjesce
	Ukljucena       map[string]bool
	DostupniZapisi  []models.ZapisUIzvjescu
	UkljuceniZapisi map[string]bool
	Uprava          bool
}

// SetSektorsko daje rukovatelju predloške sektorskog izvješća
func (h *IzvjescaHandler) SetSektorsko(obrazac, dokument *template.Template) {
	h.tmplSektObr, h.tmplSektDok = obrazac, dokument
}

func (h *IzvjescaHandler) sektorskoData(r *http.Request) SektorskoPageData {
	return SektorskoPageData{IzvjescaPageData: h.pageData(r)}
}

func (h *IzvjescaHandler) renderSektorsko(w http.ResponseWriter, t *template.Template, name string, data SektorskoPageData) {
	if err := t.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// nazivCentra vraća naziv centra sektora iz dnevnika, kad ga ima
func (h *IzvjescaHandler) nazivCentra(sektor string) string {
	if h.zaglavlje != nil {
		return h.zaglavlje(sektor).Centar
	}
	return models.Terms().Sector + " " + sektor
}

// ShowSektorskoNovo prikazuje obrazac za sektor i dan, s predloženim
// tekstom; kad izvješće za taj dan postoji, vodi na uređivanje
func (h *IzvjescaHandler) ShowSektorskoNovo(w http.ResponseWriter, r *http.Request) {
	data := h.sektorskoData(r)
	svc := h.svc()
	sektor := strings.TrimSpace(r.URL.Query().Get("sektor"))
	if sektor == "" {
		if s := svc.SektoriZaSastavljanje(data.Permissions); len(s) > 0 {
			sektor = s[0]
		}
	}
	if sektor == "" || !svc.UpravaSektora(data.Permissions, sektor) {
		http.Error(w, "Izvješće sektora slaže voditelj centra ili rukovoditelj sektora", http.StatusForbidden)
		return
	}
	dan := time.Now().In(models.Zagreb)
	if t, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("dan"), models.Zagreb); err == nil {
		dan = t
	}
	iz, err := svc.PredlozakSektora(r.Context(), sektor, dan)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if iz.ID != "" {
		http.Redirect(w, r, "/sektorsko-izvjesce/"+iz.ID+"/uredi", http.StatusSeeOther)
		return
	}
	h.popuniObrazacSektorskog(r, &data, iz, nil)
	h.renderSektorsko(w, h.tmplSektObr, "sektorsko_form.html", data)
}

// popuniObrazacSektorskog skuplja što obrazac nudi: izvješća dionica dana i
// zapise dnevnika, s oznakom što je uključeno (nil = predana i svi zapisi)
func (h *IzvjescaHandler) popuniObrazacSektorskog(r *http.Request, data *SektorskoPageData, iz *models.SektorskoIzvjesce, ukljucena map[string]bool) {
	svc := h.svc()
	data.Sektorsko, data.Sektor, data.Uprava = iz, iz.Sektor, true
	data.Centar = h.nazivCentra(iz.Sektor)
	data.Dostupna, _ = svc.IzvjescaDana(r.Context(), iz.Sektor, iz.Dan)
	data.Ukljucena = map[string]bool{}
	if ukljucena == nil {
		for _, d := range data.Dostupna {
			data.Ukljucena[d.ID] = d.Predano()
		}
	} else {
		data.Ukljucena = ukljucena
	}
	data.DostupniZapisi, _ = svc.ZapisiDana(r.Context(), iz.JournalID, iz.Dan)
	data.UkljuceniZapisi = map[string]bool{}
	if iz.ID == "" {
		for _, z := range data.DostupniZapisi {
			data.UkljuceniZapisi[z.ID] = true
		}
	} else {
		for _, z := range iz.Sadrzaj.Zapisi {
			data.UkljuceniZapisi[z.ID] = true
		}
	}
}

// ucitajSektorsko čita izvješće iz putanje; nil kad ga nema ili se ne smije vidjeti
func (h *IzvjescaHandler) ucitajSektorsko(w http.ResponseWriter, r *http.Request, data *SektorskoPageData) (*models.SektorskoIzvjesce, bool) {
	svc := h.svc()
	iz, err := svc.GetSektorsko(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, false
	}
	if iz == nil {
		http.NotFound(w, r)
		return nil, false
	}
	if !svc.SmijeVidjetiSektor(data.Permissions, iz.Sektor) {
		http.Error(w, "Izvješće sektora vide oni koji rade u sektoru", http.StatusForbidden)
		return nil, false
	}
	data.Sektorsko, data.Sektor, data.Uprava = iz, iz.Sektor, svc.UpravaSektora(data.Permissions, iz.Sektor)
	data.Centar = h.nazivCentra(iz.Sektor)
	return iz, true
}

// ShowSektorsko prikazuje izvješće sektora kao dokument
func (h *IzvjescaHandler) ShowSektorsko(w http.ResponseWriter, r *http.Request) {
	data := h.sektorskoData(r)
	if _, ok := h.ucitajSektorsko(w, r, &data); !ok {
		return
	}
	h.renderSektorsko(w, h.tmplSektDok, "sektorsko.html", data)
}

// ShowSektorskoUredi prikazuje obrazac postojećeg izvješća
func (h *IzvjescaHandler) ShowSektorskoUredi(w http.ResponseWriter, r *http.Request) {
	data := h.sektorskoData(r)
	iz, ok := h.ucitajSektorsko(w, r, &data)
	if !ok {
		return
	}
	if !data.Uprava {
		http.Error(w, "Izvješće sektora slaže voditelj centra ili rukovoditelj sektora", http.StatusForbidden)
		return
	}
	ukljucena := map[string]bool{}
	for _, pod := range iz.Sadrzaj.Pregled.Podrucja {
		for _, d := range pod.Dionice {
			ukljucena[d.IzvjesceID] = true
		}
	}
	h.popuniObrazacSektorskog(r, &data, iz, ukljucena)
	h.renderSektorsko(w, h.tmplSektObr, "sektorsko_form.html", data)
}

// HandleSektorskoSpremi upisuje obrazac (novo ili izmjenu)
func (h *IzvjescaHandler) HandleSektorskoSpremi(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/izvjesca", "error", "Neispravan zahtjev")
		return
	}
	svc := h.svc()
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	iz := &models.SektorskoIzvjesce{ID: r.PathValue("id"), Sektor: f("sektor")}
	if t, err := time.ParseInLocation("2006-01-02", f("dan"), models.Zagreb); err == nil {
		iz.Dan = t
	}
	iz.Sadrzaj = models.SektorskiSadrzaj{Hidrometeo: r.FormValue("hidrometeo"), Ostecenja: r.FormValue("ostecenja"), Mjere: r.FormValue("mjere"),
		Objekti: r.FormValue("objekti"), Poplavljeno: r.FormValue("poplavljeno"), Evakuacija: r.FormValue("evakuacija"), Napomena: r.FormValue("napomena")}
	if err := svc.SpremiSektorsko(r.Context(), data.CurrentUser, data.Permissions, iz, r.Form["izvjesce"], r.Form["zapis"]); err != nil {
		back := "/sektorsko-izvjesce/novo?sektor=" + iz.Sektor + "&dan=" + iz.Dan.Format("2006-01-02")
		if iz.ID != "" {
			back = "/sektorsko-izvjesce/" + iz.ID + "/uredi"
		}
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	if r.FormValue("predaj") == "1" {
		if err := svc.PredajSektorsko(r.Context(), data.CurrentUser, data.Permissions, iz.ID); err != nil {
			redirectWith(w, r, "/sektorsko-izvjesce/"+iz.ID+"/uredi", "error", "Spremljeno kao nacrt; "+err.Error())
			return
		}
		redirectWith(w, r, "/sektorsko-izvjesce/"+iz.ID, "success", "Izvješće sektora je predano Glavnom centru.")
		return
	}
	redirectWith(w, r, "/sektorsko-izvjesce/"+iz.ID, "success", "Izvješće sektora je spremljeno kao nacrt.")
}

// HandleSektorskoPredaj označava izvješće predanim
func (h *IzvjescaHandler) HandleSektorskoPredaj(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	id := r.PathValue("id")
	if err := h.svc().PredajSektorsko(r.Context(), data.CurrentUser, data.Permissions, id); err != nil {
		redirectWith(w, r, "/sektorsko-izvjesce/"+id, "error", err.Error())
		return
	}
	redirectWith(w, r, "/sektorsko-izvjesce/"+id, "success", "Izvješće sektora je predano Glavnom centru.")
}

// HandleSektorskoObrisi arhivira izvješće
func (h *IzvjescaHandler) HandleSektorskoObrisi(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	id := r.PathValue("id")
	if err := h.svc().ObrisiSektorsko(r.Context(), data.CurrentUser, data.Permissions, id); err != nil {
		redirectWith(w, r, "/sektorsko-izvjesce/"+id, "error", err.Error())
		return
	}
	redirectWith(w, r, "/izvjesca", "success", "Izvješće sektora je obrisano; u knjizi verzija ostaje arhivirano.")
}

// IzvoziSektorsko piše izvješće sektora kao .xlsx
func (h *IzvjescaHandler) IzvoziSektorsko(w http.ResponseWriter, r *http.Request) {
	data := h.sektorskoData(r)
	iz, ok := h.ucitajSektorsko(w, r, &data)
	if !ok {
		return
	}
	z := ZaglavljeIzvoza{Organizacija: models.Terms().OrgName, Datum: time.Now().In(models.Zagreb)}
	if h.zaglavlje != nil {
		z = h.zaglavlje(iz.Sektor)
	}
	posaljiXLSX(w, "dnevno-izvjesce-sektor-"+iz.Sektor+"-"+iz.Dan.Format("2006-01-02")+".xlsx", KnjigaSektorskog(iz, z))
}
