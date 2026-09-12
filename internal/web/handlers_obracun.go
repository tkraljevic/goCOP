package web

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/obracun"
	"gocop/internal/service"
)

// Postavke obračuna sati: blagdani i koeficijenti. Podatak organizacije —
// zakon o blagdanima se mijenja, koeficijente određuje kolektivni ugovor —
// pa se uređuju ovdje, a ne novom verzijom programa. Samo globalni
// administrator; ruta je ograđena kroz samoAdmin.
type ObracunPostavkeHandler struct {
	svc  func() *service.ObracunService
	tmpl *template.Template
}

func NewObracunPostavkeHandler(svc func() *service.ObracunService, tmpl *template.Template) *ObracunPostavkeHandler {
	return &ObracunPostavkeHandler{svc: svc, tmpl: tmpl}
}

type ObracunPostavkePageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions

	Blagdani     obracun.Pravila
	Godina       int
	OveGodine    []string // datumi blagdana ove godine, za provjeru pravila
	Koeficijenti obracun.Koeficijenti
	RadnoVrijeme obracun.RadnoVrijeme
	Razredi      []obracun.Razred
	Mjesta       []obracun.Mjesto

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// K vraća koeficijent za prikaz u obrascu
func (d ObracunPostavkePageData) K(m obracun.Mjesto, r obracun.Razred) string {
	return strings.ReplaceAll(strconv.FormatFloat(d.Koeficijenti[m][r], 'f', -1, 64), ".", ",")
}

func (h *ObracunPostavkeHandler) pageData(r *http.Request) ObracunPostavkePageData {
	ctx := r.Context()
	u, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return ObracunPostavkePageData{
		CurrentUser: u, Permissions: perms,
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "admin", ViewAsBanner: viewBanner(r),
		Razredi: obracun.Razredi, Mjesta: []obracun.Mjesto{obracun.Ured, obracun.Teren},
	}
}

// ShowPostavke prikazuje blagdane, njihov pad u tekućoj godini i koeficijente
func (h *ObracunPostavkeHandler) ShowPostavke(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	svc := h.svc()
	if svc == nil {
		http.Error(w, "postavke obračuna nisu dostupne", http.StatusServiceUnavailable)
		return
	}
	var err error
	if data.Blagdani, err = svc.Blagdani(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.Koeficijenti = svc.Koeficijenti(r.Context())
	data.RadnoVrijeme = svc.RadnoVrijeme(r.Context())
	data.Godina = time.Now().In(models.Zagreb).Year()
	if g, err := strconv.Atoi(r.URL.Query().Get("godina")); err == nil && g > 1900 && g < 2200 {
		data.Godina = g
	}
	for _, d := range data.Blagdani.Blagdani(data.Godina) {
		data.OveGodine = append(data.OveGodine, danTjednaHR(d)+" "+d.Format("2.1."))
	}
	if err := h.tmpl.ExecuteTemplate(w, "obracun_postavke.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleSpremiBlagdan upisuje novo ili izmijenjeno pravilo
func (h *ObracunPostavkeHandler) HandleSpremiBlagdan(w http.ResponseWriter, r *http.Request) {
	back := "/administracija/obracun"
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, back, "error", "Neispravan zahtjev")
		return
	}
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	broj := func(k string) int { n, _ := strconv.Atoi(f(k)); return n }
	p := obracun.Pravilo{ID: f("id"), Naziv: f("naziv"), Vrsta: obracun.VrstaPravila(f("vrsta")),
		Mjesec: broj("mjesec"), Dan: broj("dan"), Pomak: broj("pomak"), Datum: f("datum"),
		OdGodine: broj("od_godine"), DoGodine: broj("do_godine")}
	if err := h.svc().SpremiBlagdan(r.Context(), p); err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, back, "success", "Blagdan je upisan.")
}

// HandleMakniBlagdan miče pravilo
func (h *ObracunPostavkeHandler) HandleMakniBlagdan(w http.ResponseWriter, r *http.Request) {
	back := "/administracija/obracun"
	if err := h.svc().MakniBlagdan(r.Context(), r.PathValue("id")); err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, back, "success", "Blagdan je maknut.")
}

// HandleSpremiKoeficijente upisuje cijelu tablicu množitelja
func (h *ObracunPostavkeHandler) HandleSpremiKoeficijente(w http.ResponseWriter, r *http.Request) {
	back := "/administracija/obracun"
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, back, "error", "Neispravan zahtjev")
		return
	}
	k := obracun.Koeficijenti{}
	for _, m := range []obracun.Mjesto{obracun.Ured, obracun.Teren} {
		k[m] = map[obracun.Razred]float64{}
		for _, razred := range obracun.Razredi {
			v, ok := parseBroj(r.FormValue("k_" + string(m) + "_" + string(razred)))
			if !ok {
				redirectWith(w, r, back, "error", "Koeficijent "+string(m)+" "+string(razred)+" nije broj")
				return
			}
			k[m][razred] = v
		}
	}
	if err := h.svc().SpremiKoeficijente(r.Context(), k); err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, back, "success", "Koeficijenti su upisani.")
}

// HandleSpremiRadnoVrijeme upisuje redovno radno vrijeme radnim danom
func (h *ObracunPostavkeHandler) HandleSpremiRadnoVrijeme(w http.ResponseWriter, r *http.Request) {
	back := "/administracija/obracun"
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, back, "error", "Neispravan zahtjev")
		return
	}
	rv, err := obracun.ParseRadnoVrijeme(r.FormValue("od"), r.FormValue("do"))
	if err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	if err := h.svc().SpremiRadnoVrijeme(r.Context(), rv); err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, back, "success", "Redovno radno vrijeme je "+rv.Tekst()+".")
}
