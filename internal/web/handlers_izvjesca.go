package web

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// IzvjescaHandler: dnevna izvješća rukovoditelja dionica — popis, obrazac,
// pregled kao dokument, predaja. Obrazac je standardni iz Privitka 4, a
// popunjava se iz onoga što program zna.
type IzvjescaHandler struct {
	zaglavlje func(sektor string) ZaglavljeIzvoza // za izvoz; nil dok se ne postavi

	svc       func() *service.IzvjescaService
	tmplPopis *template.Template
	tmplObr   *template.Template
	tmplDok   *template.Template
}

func NewIzvjescaHandler(svc func() *service.IzvjescaService, popis, obrazac, dokument *template.Template) *IzvjescaHandler {
	return &IzvjescaHandler{svc: svc, tmplPopis: popis, tmplObr: obrazac, tmplDok: dokument}
}

type IzvjescaPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions

	Izvjesca    []models.DnevnoIzvjesce
	Izvjesce    *models.DnevnoIzvjesce
	Dionica     *models.Section
	Dionice     []models.Section // za koje osoba smije pisati
	Stadiji     []models.DefensePhase
	Tendencije  []struct{ Kod, Naziv string }
	IsEdit      bool
	SmijePisati bool
	Danas       string
	Dan         string // filtar popisa
	Sifra       string // filtar popisa: dionica

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

func (h *IzvjescaHandler) pageData(r *http.Request) IzvjescaPageData {
	ctx := r.Context()
	u, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return IzvjescaPageData{
		CurrentUser: u, Permissions: perms,
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "journals", ViewAsBanner: viewBanner(r),
		Stadiji: models.StadijiObrane, Tendencije: models.Tendencije,
		Danas: time.Now().In(models.Zagreb).Format("2006-01-02"),
	}
}

func (h *IzvjescaHandler) render(w http.ResponseWriter, t *template.Template, name string, data IzvjescaPageData) {
	if err := t.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowPopis prikazuje izvješća koja osoba smije vidjeti: po danu i dionici
func (h *IzvjescaHandler) ShowPopis(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	svc := h.svc()
	if svc == nil {
		http.Error(w, "izvješća nisu dostupna", http.StatusServiceUnavailable)
		return
	}
	data.Dionice, _ = svc.DioniceZaPisanje(data.Permissions)
	data.Sifra = strings.TrimSpace(r.URL.Query().Get("dionica"))
	var dan *time.Time
	if t, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("dan"), models.Zagreb); err == nil {
		dan = &t
		data.Dan = t.Format("2006-01-02")
	}
	sva, err := svc.List(r.Context(), "", data.Sifra, "", dan)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// vidi se ono što je u dosegu: pravo se provjerava po dionici
	for _, iz := range sva {
		sec := &models.Section{Code: iz.SectionCode, SectorID: iz.Sektor}
		if d, err := svc.Dionica(iz.SectionCode); err == nil && d != nil {
			sec = d
		}
		if svc.SmijeVidjeti(data.Permissions, sec) {
			data.Izvjesca = append(data.Izvjesca, iz)
		}
	}
	h.render(w, h.tmplPopis, "izvjesca.html", data)
}

// ShowNovo prikazuje obrazac za dionicu i dan, popunjen iz programa; kad
// izvješće za taj dan postoji, vodi na njega
func (h *IzvjescaHandler) ShowNovo(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	svc := h.svc()
	data.Dionice, _ = svc.DioniceZaPisanje(data.Permissions)
	code := strings.TrimSpace(r.URL.Query().Get("dionica"))
	if code == "" {
		if len(data.Dionice) == 0 {
			redirectWith(w, r, "/izvjesca", "error", "Nemate dionicu za koju biste pisali izvješće")
			return
		}
		code = data.Dionice[0].Code
	}
	sec, err := svc.Dionica(code)
	if err != nil || sec == nil {
		redirectWith(w, r, "/izvjesca", "error", "Nepoznata dionica "+code)
		return
	}
	if !svc.SmijePisati(data.Permissions, sec) {
		http.Error(w, "Dnevno izvješće piše rukovoditelj dionice ili zamjenik, ili uprava", http.StatusForbidden)
		return
	}
	dan := time.Now().In(models.Zagreb)
	if t, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("dan"), models.Zagreb); err == nil {
		dan = t
	}
	iz, err := svc.Predlozak(r.Context(), sec, dan)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if iz.ID != "" {
		http.Redirect(w, r, "/izvjesca/"+iz.ID+"/uredi", http.StatusSeeOther)
		return
	}
	data.Izvjesce, data.Dionica, data.SmijePisati = iz, sec, true
	h.render(w, h.tmplObr, "izvjesce_form.html", data)
}

// ucitaj čita izvješće iz putanje i njegovu dionicu; nil kad ga nema ili se ne smije vidjeti
func (h *IzvjescaHandler) ucitaj(w http.ResponseWriter, r *http.Request, data *IzvjescaPageData) (*models.DnevnoIzvjesce, *models.Section, bool) {
	svc := h.svc()
	iz, err := svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, nil, false
	}
	if iz == nil {
		http.NotFound(w, r)
		return nil, nil, false
	}
	sec, err := svc.Dionica(iz.SectionCode)
	if err != nil || sec == nil {
		sec = &models.Section{Code: iz.SectionCode, SectorID: iz.Sektor}
	}
	if !svc.SmijeVidjeti(data.Permissions, sec) {
		http.Error(w, "Izvješće vide rukovoditelji dionice, područja i sektora", http.StatusForbidden)
		return nil, nil, false
	}
	data.Izvjesce, data.Dionica, data.SmijePisati = iz, sec, svc.SmijePisati(data.Permissions, sec)
	return iz, sec, true
}

// ShowIzvjesce prikazuje izvješće kao dokument
func (h *IzvjescaHandler) ShowIzvjesce(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if _, _, ok := h.ucitaj(w, r, &data); !ok {
		return
	}
	h.render(w, h.tmplDok, "izvjesce.html", data)
}

// ShowUredi prikazuje obrazac postojećeg izvješća
func (h *IzvjescaHandler) ShowUredi(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if _, _, ok := h.ucitaj(w, r, &data); !ok {
		return
	}
	if !data.SmijePisati {
		http.Error(w, "Dnevno izvješće piše rukovoditelj dionice ili zamjenik, ili uprava", http.StatusForbidden)
		return
	}
	data.IsEdit = true
	h.render(w, h.tmplObr, "izvjesce_form.html", data)
}

// HandleSpremi upisuje obrazac (novo ili izmjenu)
func (h *IzvjescaHandler) HandleSpremi(w http.ResponseWriter, r *http.Request) {
	u, perms := func() (*models.User, *models.UserPermissions) {
		d := h.pageData(r)
		return d.CurrentUser, d.Permissions
	}()
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/izvjesca", "error", "Neispravan zahtjev")
		return
	}
	svc := h.svc()
	iz := izvjesceIzObrasca(r)
	if id := r.PathValue("id"); id != "" {
		iz.ID = id
	}
	sec, err := svc.Dionica(iz.SectionCode)
	if err != nil || sec == nil {
		redirectWith(w, r, "/izvjesca", "error", "Nepoznata dionica")
		return
	}
	if err := svc.Spremi(r.Context(), u, perms, sec, iz); err != nil {
		back := "/izvjesca/novo?dionica=" + iz.SectionCode + "&dan=" + iz.Dan.Format("2006-01-02")
		if iz.ID != "" {
			back = "/izvjesca/" + iz.ID + "/uredi"
		}
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	if r.FormValue("predaj") == "1" {
		if err := svc.Predaj(r.Context(), u, perms, sec, iz.ID); err != nil {
			redirectWith(w, r, "/izvjesca/"+iz.ID+"/uredi", "error", "Spremljeno kao nacrt; "+err.Error())
			return
		}
		redirectWith(w, r, "/izvjesca/"+iz.ID, "success", "Izvješće je predano.")
		return
	}
	redirectWith(w, r, "/izvjesca/"+iz.ID, "success", "Izvješće je spremljeno kao nacrt.")
}

// SetZaglavlje daje rukovatelju ono što na izvozu stoji o organizaciji
func (h *IzvjescaHandler) SetZaglavlje(f func(sektor string) ZaglavljeIzvoza) { h.zaglavlje = f }

// IzvoziIzvjesce piše izvješće kao .xlsx u obliku propisanog obrasca
func (h *IzvjescaHandler) IzvoziIzvjesce(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	iz, sec, ok := h.ucitaj(w, r, &data)
	if !ok {
		return
	}
	z := ZaglavljeIzvoza{Organizacija: models.Terms().OrgName, Datum: time.Now().In(models.Zagreb)}
	if h.zaglavlje != nil {
		z = h.zaglavlje(sec.SectorID)
	}
	posaljiXLSX(w, "dnevno-izvjesce-"+strings.ReplaceAll(sec.Code, ".", "-")+"-"+iz.Dan.Format("2006-01-02")+".xlsx", KnjigaIzvjesca(iz, sec, z))
}

// HandlePredaj označava izvješće predanim
func (h *IzvjescaHandler) HandlePredaj(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	iz, sec, ok := h.ucitaj(w, r, &data)
	if !ok {
		return
	}
	if err := h.svc().Predaj(r.Context(), data.CurrentUser, data.Permissions, sec, iz.ID); err != nil {
		redirectWith(w, r, "/izvjesca/"+iz.ID, "error", err.Error())
		return
	}
	redirectWith(w, r, "/izvjesca/"+iz.ID, "success", "Izvješće je predano.")
}

// HandleObrisi arhivira izvješće
func (h *IzvjescaHandler) HandleObrisi(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	iz, sec, ok := h.ucitaj(w, r, &data)
	if !ok {
		return
	}
	if err := h.svc().Obrisi(r.Context(), data.CurrentUser, data.Permissions, sec, iz.ID); err != nil {
		redirectWith(w, r, "/izvjesca/"+iz.ID, "error", err.Error())
		return
	}
	redirectWith(w, r, "/izvjesca", "success", "Izvješće je obrisano; u knjizi verzija ostaje arhivirano.")
}

// izvjesceIzObrasca čita obrazac iz zahtjeva; polja su nazvana po odjeljcima
func izvjesceIzObrasca(r *http.Request) *models.DnevnoIzvjesce {
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	broj := func(k string) int { n, _ := strconv.Atoi(strings.ReplaceAll(f(k), ",", ".")); return n }
	dec := func(k string) float64 { v, _ := strconv.ParseFloat(strings.ReplaceAll(f(k), ",", "."), 64); return v }
	iz := &models.DnevnoIzvjesce{SectionCode: f("dionica"), Stadij: models.DefensePhase(f("stadij"))}
	if t, err := time.ParseInLocation("2006-01-02", f("dan"), models.Zagreb); err == nil {
		iz.Dan = t
	}
	s := &iz.Sadrzaj
	s.Vodotok, s.Tendencija = f("vodotok"), f("tendencija")
	postaje, sati, vrijednosti, jedinice, ids := r.Form["vodostaj_postaja"], r.Form["vodostaj_sat"], r.Form["vodostaj_vrijednost"], r.Form["vodostaj_jedinica"], r.Form["vodostaj_station"]
	for i := range postaje {
		v := models.VodostajUIzvjescu{Postaja: strings.TrimSpace(postaje[i])}
		if i < len(sati) {
			v.Sat = strings.TrimSpace(sati[i])
		}
		if i < len(vrijednosti) {
			v.Vrijednost = strings.TrimSpace(vrijednosti[i])
		}
		if i < len(jedinice) {
			v.Jedinica = strings.TrimSpace(jedinice[i])
		}
		if i < len(ids) {
			v.StationID = strings.TrimSpace(ids[i])
		}
		if v.Postaja == "" && v.Vrijednost == "" {
			continue
		}
		if v.Jedinica == "" {
			v.Jedinica = "cm"
		}
		s.Vodostaji = append(s.Vodostaji, v)
	}
	s.Pregled, s.Radnje, s.Objekti = r.FormValue("pregled"), r.FormValue("radnje"), r.FormValue("objekti")
	s.Vrece, s.Materijal, s.Nasipi, s.Crpke = f("vrece"), f("materijal"), f("nasipi"), f("crpke")
	s.Pravne = models.SudioniciPravne{Ljudi: broj("pravne_ljudi"), Kamioni: broj("pravne_kamioni"), Bageri: broj("pravne_bageri"), KombStrojevi: broj("pravne_komb"),
		Utovarivaci: broj("pravne_utovarivaci"), Buldozeri: broj("pravne_buldozeri"), Traktori: broj("pravne_traktori"), Brodovi: broj("pravne_brodovi"), Camci: broj("pravne_camci"), Ostalo: f("pravne_ostalo")}
	s.Ostali = models.SudioniciOstali{Policija: broj("ostali_policija"), Vatrogasci: broj("ostali_vatrogasci"), Vojska: broj("ostali_vojska"), HGSS: broj("ostali_hgss"),
		CivilnaZastita: broj("ostali_cz"), CrveniKriz: broj("ostali_ck"), Drugi: f("ostali_drugi")}
	s.Poplavljeno = models.Poplavljeno{Naselja: f("popl_naselja"), Ljudi: broj("popl_ljudi"), Stambeni: broj("popl_stambeni"), Industrijski: broj("popl_industrijski"), Farme: broj("popl_farme"),
		Infrastruktura: f("popl_infrastruktura"), SumskeHa: dec("popl_sumske"), PoljoprivredneHa: dec("popl_poljoprivredne"), OstaleHa: dec("popl_ostale")}
	s.Evakuacija = models.Evakuacija{Naselja: f("evak_naselja"), Ljudi: broj("evak_ljudi"), Kucanstava: broj("evak_kucanstava"), Zivotinje: f("evak_zivotinje")}
	return iz
}
