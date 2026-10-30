package web

import (
	"bytes"
	"context"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

type DashboardHandler struct {
	userService  *service.UserService
	tmpl         *template.Template
	registriTmpl *template.Template

	zid       func() *service.ZidService
	izvjesca  func() *service.IzvjescaService
	mts       func() *service.MtsService
	vodocuvar func() *service.VodocuvarService
}

// SetZid daje naslovnoj zid događanja i traku stanja
func (h *DashboardHandler) SetZid(zid func() *service.ZidService, izvjesca func() *service.IzvjescaService, mts func() *service.MtsService) {
	h.zid, h.izvjesca, h.mts = zid, izvjesca, mts
}

// SetVodocuvar daje naslovnoj vodočuvarski dnevnik: vodočuvar na njoj vidi
// upozorenje na dnevne listove koje nije potpisao i predao
func (h *DashboardHandler) SetVodocuvar(f func() *service.VodocuvarService) { h.vodocuvar = f }

// nepredani je upozorenje osobi na njezine nepredane listove; nil bez
// vodočuvarskog dnevnika
func (h *DashboardHandler) nepredani(ctx context.Context, u *models.User) *UpozorenjeNepredanih {
	if h.vodocuvar == nil {
		return nil
	}
	return nepredaniZa(ctx, h.vodocuvar(), u)
}

func NewDashboardHandler(userService *service.UserService, tmpl, registriTmpl *template.Template) *DashboardHandler {
	return &DashboardHandler{
		userService:  userService,
		tmpl:         tmpl,
		registriTmpl: registriTmpl,
	}
}

// ShowRegisters prikazuje ulaznu stranicu za registre dostupne korisniku.
func (h *DashboardHandler) ShowRegisters(w http.ResponseWriter, r *http.Request) {
	user, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	data := DashboardViewData{
		CurrentUser:  user,
		Perms:        perms,
		ActiveNav:    "registers",
		ViewAsBanner: viewBanner(r),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.registriTmpl.ExecuteTemplate(w, "registri.html", data); err != nil {
		http.Error(w, "Greška pri renderiranju registara: "+err.Error(), http.StatusInternalServerError)
	}
}

type DashboardViewData struct {
	CurrentUser    *models.User
	Perms          *models.UserPermissions
	Stats          repository.DashboardStats
	Stanja         []service.StanjeObrane
	Zid            []service.Dogadjaj
	Moduli         []struct{ ID, Naziv, Ikona string }
	Precaci        []Precac
	Nepredani      *UpozorenjeNepredanih // vlastiti listovi koje vodočuvar nije predao
	CekaUlogu      bool                  // račun još nema ulogu (cekaUlogu)
	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

func (h *DashboardHandler) ShowDashboard(w http.ResponseWriter, r *http.Request) {
	user, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)

	stats, err := h.userService.GetDashboardStats()
	if err != nil {
		stats = repository.DashboardStats{
			TotalUsers:   545,
			TotalDuties:  746,
			TotalSectors: 7,
			TotalAreas:   34,
		}
	}

	data := DashboardViewData{
		CurrentUser:  user,
		Perms:        perms,
		Stats:        stats,
		Moduli:       service.ModuliZida,
		ActiveNav:    "dashboard",
		ViewAsBanner: viewBanner(r),
	}
	h.stanjeIZid(r, perms, &data)
	data.Precaci = vidljiviPrecaci(precaciZa(perms), data.Modules)
	data.CekaUlogu = cekaUlogu(user)
	data.Nepredani = h.nepredani(r.Context(), user)

	// Stranica se prvo iscrta cijela, pa tek onda šalje. Predložak koji
	// padne usred iscrtavanja tako dobije pravu grešku, a ne pola stranice s
	// porukom na dnu i drugo zaglavlje („superfluous WriteHeader”).
	var b bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&b, "dashboard.html", data); err != nil {
		log.Printf("početna stranica: %v", err)
		http.Error(w, "Greška pri renderiranju početne stranice: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = b.WriteTo(w) // greška znači samo da je preglednik otišao; drugog odgovora nema
}

// stanjeIZid puni stanje sektora i zid. Izvješća na pločici sektora vidi
// samo račun s Dokumentacijom, sredstva na terenu samo račun s Registrima, a
// zid preskače module koje račun ne vidi.
func (h *DashboardHandler) stanjeIZid(r *http.Request, perms *models.UserPermissions, data *DashboardViewData) {
	if h.zid == nil {
		return
	}
	zid := h.zid()
	if zid == nil {
		return
	}
	vis := data.Modules
	sektori, _ := h.userService.ListSectors() // bez popisa sektora pločica stanja ostane prazna, zid se i dalje prikaže
	var izv *service.IzvjescaService
	var mts *service.MtsService
	if h.izvjesca != nil && vis.Sees(models.ModuleJournals) {
		izv = h.izvjesca()
	}
	if h.mts != nil && vis.Sees(models.ModuleRegisters) {
		mts = h.mts()
	}
	data.Stanja = zid.Stanje(r.Context(), perms, sektori, izv, mts)
	f := service.FiltarZida{Modul: r.URL.Query().Get("modul"), Limit: 40, Skriveni: service.SkriveniModuliZida(vis.Sees)}
	data.Zid, _, _ = zid.Zadnji(r.Context(), perms, f) // bez knjige zid ostane prazan, a početna se i dalje prikaže
}

// vidljiviPrecaci izbacuje prečace koji vode u modul koji račun ne vidi;
// takva bi stranica dala 403
func vidljiviPrecaci(svi []Precac, vis models.Visibility) []Precac {
	var out []Precac
	for _, p := range svi {
		putanja, _, _ := strings.Cut(p.Link, "?")
		if m := moduleForPath(putanja); m == "" || vis.Sees(m) {
			out = append(out, p)
		}
	}
	return out
}

// cekaUlogu: račun nema nijednu dužnost koja sada vrijedi ni zastavicu
// globalnog administratora. Novi račun nema zadanu ulogu, pa se osoba
// prijavi i samo gleda dok joj je administrator ne dodijeli (Odluke
// (30. 10. 2026.), 15).
func cekaUlogu(u *models.User) bool {
	return u != nil && !u.IsGlobalAdmin && len(models.VazecaZaduzenja(u.Duties, time.Now())) == 0
}

// Precac je gumb na naslovnoj za ono što se u toj ulozi radi svaki dan
type Precac struct {
	Naslov, Link, Ikona string
}

// precaciZa slaže prečace po dužnostima osobe
func precaciZa(perms *models.UserPermissions) []Precac {
	if perms == nil {
		return nil
	}
	var out []Precac
	dodaj := func(naslov, link, ikona string) {
		for _, p := range out {
			if p.Link == link {
				return
			}
		}
		out = append(out, Precac{Naslov: naslov, Link: link, Ikona: ikona})
	}
	uprava := perms.IsGlobalAdmin || len(perms.AdminSectors) > 0
	if uprava {
		dodaj("Dnevnik COP-a", "/dnevnici/popis?vrsta=OBRANA", "notebook")
		dodaj("Izvješće sektora", "/izvjesca", "file-text")
	}
	if perms.VodiDionice() {
		dodaj("Novo dnevno izvješće", "/izvjesca/novo", "file-text")
	}
	skladistar := false
	for _, d := range perms.User.Duties {
		if d.IsActive && d.Role == models.RoleWarehouseKeeper {
			skladistar = true
		}
	}
	if skladistar || uprava || len(perms.AdminAreas) > 0 {
		dodaj("Skladišta i sredstva", "/sredstva", "package")
	}
	if perms.User.IsFieldUser() {
		dodaj("Teren", "/teren", "map-pin")
	}
	dodaj("Događanja", "/dogadjanja", "activity")
	return out
}

// PoDanima grupira zid po danima
func (d DashboardViewData) PoDanima() []DanDogadjaja { return grupirajPoDanima(d.Zid) }
