package web

import (
	"context"
	"html/template"
	"net/http"
	"strings"
	"time"

	"gocop/internal/hidroview"
	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Stranica Administracija › Telemetrija. Letve koje nemaju javnu stranicu
// vodostaja čitaju se s Geolux HydroViewa, a taj sustav traži prijavu.
// Račun stoji na čvoru: upiše se jednom i vrijedi za sve takve letve, a
// letva koja treba drugi upisuje ga u svojoj kartici.
//
// Račun je ovdje, a ne među postavkama čvora, jer je posao onoga tko vodi
// letve, kao i poslužitelj e-pošte — s čvorom ga veže samo to što se lozinka
// zaključava njegovim ključem.
type TelemetrijaHandler struct {
	racuni   func() *repository.HidroViewRepository
	kljuc    func() []byte
	postaje  *service.StationService
	tmpl     *template.Template
	zapisnik func(string, ...any)
}

// NewTelemetrijaHandler sastavlja stranicu.
func NewTelemetrijaHandler(racuni func() *repository.HidroViewRepository, kljuc func() []byte,
	postaje *service.StationService, tmpl *template.Template) *TelemetrijaHandler {
	return &TelemetrijaHandler{racuni: racuni, kljuc: kljuc, postaje: postaje, tmpl: tmpl}
}

// LetvaTelemetrije je jedan redak popisa letava koje se čitaju s telemetrije.
type LetvaTelemetrije struct {
	ID       string
	Naziv    string
	Uvoz     bool
	Svoj     bool // ima svoj račun, ne čvorov
	Korisnik string
}

// TelemetrijaPageData je ono što stranica prikazuje.
type TelemetrijaPageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	ActiveNav      string
	Adresa         string
	Korisnik       string
	Upisano        time.Time
	Letve          []LetvaTelemetrije
	SuccessMessage string
	ErrorMessage   string
	ViewAsBanner
}

// Prikazi ispisuje račun i letve koje se odande čitaju.
func (h *TelemetrijaHandler) Prikazi(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	data := TelemetrijaPageData{
		CurrentUser:  currUser,
		Permissions:  perms,
		ActiveNav:    "administracija",
		Adresa:       hidroview.ZadanaAdresa,
		ViewAsBanner: viewBanner(r),
	}
	data.SuccessMessage = r.URL.Query().Get("success")
	data.ErrorMessage = r.URL.Query().Get("error")

	repo := h.repo()
	if repo != nil {
		if racun, err := repo.Racun(ctx, ""); err == nil && racun != nil {
			data.Korisnik, data.Upisano = racun.Korisnik, racun.UpdatedAt
			if racun.Adresa != "" {
				data.Adresa = racun.Adresa
			}
		}
	}
	data.Letve = h.letve(ctx, repo)
	if err := h.tmpl.ExecuteTemplate(w, "administracija_telemetrija.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// letve su postaje čija javna adresa vodi na telemetriju.
func (h *TelemetrijaHandler) letve(ctx context.Context, repo *repository.HidroViewRepository) []LetvaTelemetrije {
	if h.postaje == nil {
		return nil
	}
	sve, err := h.postaje.ListStations(ctx, "", "", "", false)
	if err != nil {
		return nil
	}
	var out []LetvaTelemetrije
	for _, st := range sve {
		if !strings.Contains(strings.ToLower(st.JavniURL), "hdv.voda.hr") {
			continue
		}
		l := LetvaTelemetrije{ID: st.ID.String(), Naziv: st.Name, Uvoz: st.JavniUvoz}
		if repo != nil {
			if r, err := repo.Racun(ctx, st.Code); err == nil && r != nil && r.Letva == st.Code {
				l.Svoj, l.Korisnik = true, r.Korisnik
			}
		}
		out = append(out, l)
	}
	return out
}

// Spremi upisuje račun čvora. Prijava se prvo provjeri na samom sustavu:
// bolje odbiti odmah nego da letve svaki sat javljaju grešku koju nitko ne
// gleda. Prazno korisničko ime briše račun s ovog čvora.
func (h *TelemetrijaHandler) Spremi(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	const natrag = "/administracija/telemetrija"
	repo := h.repo()
	if repo == nil || h.kljuc == nil {
		redirectWith(w, r, natrag, "error", "Spremište računa nije dostupno.")
		return
	}
	korisnik := strings.TrimSpace(r.FormValue("hidroview_korisnik"))
	lozinka := r.FormValue("hidroview_lozinka")
	adresa := strings.TrimSpace(r.FormValue("hidroview_adresa"))

	if korisnik == "" {
		if err := repo.Obrisi(ctx, ""); err != nil {
			redirectWith(w, r, natrag, "error", "Račun nije obrisan: "+err.Error())
			return
		}
		redirectWith(w, r, natrag, "success", "Račun je obrisan s ovog čvora.")
		return
	}
	if lozinka == "" {
		postojeci, _ := repo.Racun(ctx, "")
		if postojeci == nil || postojeci.Letva != "" {
			redirectWith(w, r, natrag, "error", "Upišite i lozinku.")
			return
		}
		postojeci.Korisnik, postojeci.Adresa = korisnik, adresa
		if err := repo.Spremi(ctx, postojeci); err != nil {
			redirectWith(w, r, natrag, "error", err.Error())
			return
		}
		redirectWith(w, r, natrag, "success", "Račun je spremljen.")
		return
	}
	provjera, otkazi := context.WithTimeout(ctx, 45*time.Second)
	defer otkazi()
	k := &hidroview.Klijent{Adresa: adresa}
	if err := k.Prijava(provjera, korisnik, lozinka); err != nil {
		redirectWith(w, r, natrag, "error",
			"Prijava na "+hidroview.Podrijetlo+" nije prošla, ništa nije spremljeno: "+err.Error())
		return
	}
	kljuc := h.kljuc()
	if len(kljuc) == 0 {
		redirectWith(w, r, natrag, "error", "Ključ čvora nije učitan, pa se lozinka ne može sigurno spremiti.")
		return
	}
	z, err := posta.Zakljucaj(kljuc, lozinka)
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	if err := repo.Spremi(ctx, &repository.RacunHidroView{Korisnik: korisnik, Adresa: adresa, Lozinka: z}); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Prijava je provjerena i račun je spremljen na ovaj čvor.")
}

func (h *TelemetrijaHandler) repo() *repository.HidroViewRepository {
	if h.racuni == nil {
		return nil
	}
	return h.racuni()
}
