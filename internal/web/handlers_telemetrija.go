package web

import (
	"context"
	"html/template"
	"net/http"
	"strings"
	"time"

	"gocop/internal/hidroview"
	"gocop/internal/mletva"
	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Stranica Administracija › Telemetrija. Letve koje nemaju javnu stranicu
// vodostaja čitaju se s Geolux HydroViewa, a taj sustav traži prijavu.
// Račun stoji na čvoru: upiše se jednom i vrijedi za sve takve letve.
//
// Račun je ovdje, a ne među postavkama čvora, jer je posao onoga tko vodi
// letve, kao i poslužitelj e-pošte — s čvorom ga veže samo to što se lozinka
// zaključava njegovim ključem.
type TelemetrijaHandler struct {
	racuni  func() *repository.HidroViewRepository
	sustavi func() *repository.RacuniSustavaRepository
	// racunPoste daje račun e-pošte prijavljenog korisnika, s lozinkom
	racunPoste func(context.Context, *models.User) (string, string, error)
	kljuc      func() []byte
	postaje    *service.StationService
	tmpl       *template.Template
	zapisnik   func(string, ...any)
	// preuzima javlja ima li ovaj čvor ulogu preuzimanja vodostaja: računi
	// trebaju samo ondje
	preuzima func() bool
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
	MLetva         string // adresa sustava
	MLetvaKorisnik string
	MLetvaUpisano  time.Time
	Preuzima       bool // čvor preuzima vodostaje s izvora (uloga čvora)
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
		ActiveNav:    "admin",
		Adresa:       hidroview.ZadanaAdresa,
		ViewAsBanner: viewBanner(r),
		Preuzima:     h.preuzima == nil || h.preuzima(),
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
	data.MLetva = mletva.ZadanaAdresa
	if sr := h.sustaviRepo(); sr != nil {
		if racun, err := sr.Racun(ctx, mletva.Podrijetlo); err == nil && racun != nil {
			data.MLetvaKorisnik, data.MLetvaUpisano = racun.Korisnik, racun.UpdatedAt
		}
	}
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
// gleda. Račun se briše samo posebnim gumbom; prazno korisničko ime se
// odbija, da se račun ne izgubi slučajno.
func (h *TelemetrijaHandler) Spremi(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	const natrag = "/administracija/telemetrija"
	repo := h.repo()
	if repo == nil || h.kljuc == nil {
		redirectWith(w, r, natrag, "error", "Spremište računa nije dostupno.")
		return
	}
	if r.FormValue("radnja") == "obrisi" {
		obrisiRacun(w, r, "Račun za HydroView", func() error { return repo.Obrisi(ctx, "") })
		return
	}
	korisnik := strings.TrimSpace(r.FormValue("hidroview_korisnik"))
	lozinka := r.FormValue("hidroview_lozinka")
	adresa := strings.TrimSpace(r.FormValue("hidroview_adresa"))
	if korisnik == "" {
		redirectWith(w, r, natrag, "error", porukaPraznogKorisnika)
		return
	}
	if lozinka == "" {
		spremiBezLozinke(w, r, repo, korisnik, adresa)
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
	z, greska := h.zakljucaj(lozinka)
	if greska != "" {
		redirectWith(w, r, natrag, "error", greska)
		return
	}
	if err := repo.Spremi(ctx, &repository.RacunHidroView{Korisnik: korisnik, Adresa: adresa, Lozinka: z}); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Prijava je provjerena i račun je spremljen na ovaj čvor.")
}

// porukaPraznogKorisnika odbija spremanje bez korisničkog imena; prije je
// ono brisalo račun, pa letve tiho prestanu preuzimati
const porukaPraznogKorisnika = "Račun nije spremljen: korisničko ime je prazno. Upišite korisničko ime i lozinku."

// spremiBezLozinke mijenja samo korisničko ime i adresu već upisanog računa
// čvora; lozinka ostaje stara.
func spremiBezLozinke(w http.ResponseWriter, r *http.Request, repo *repository.HidroViewRepository, korisnik, adresa string) {
	const natrag = "/administracija/telemetrija"
	postojeci, _ := repo.Racun(r.Context(), "") // nepročitan račun vrijedi kao neupisan: traži se lozinka
	if postojeci == nil || postojeci.Letva != "" {
		redirectWith(w, r, natrag, "error", "Upišite i lozinku.")
		return
	}
	postojeci.Korisnik, postojeci.Adresa = korisnik, adresa
	if err := repo.Spremi(r.Context(), postojeci); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Račun je spremljen.")
}

// obrisiRacun je gumb „Obriši račun”: račun nestaje s ovog čvora, a poruka
// kaže koji.
func obrisiRacun(w http.ResponseWriter, r *http.Request, koji string, obrisi func() error) {
	const natrag = "/administracija/telemetrija"
	if err := obrisi(); err != nil {
		redirectWith(w, r, natrag, "error", koji+" nije obrisan: "+err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", koji+" je obrisan s ovog čvora.")
}

// zakljucaj zaključava lozinku ključem čvora; uz neuspjeh vraća poruku za
// korisnika.
func (h *TelemetrijaHandler) zakljucaj(lozinka string) ([]byte, string) {
	kljuc := h.kljuc()
	if len(kljuc) == 0 {
		return nil, "Ključ čvora nije učitan, pa se lozinka ne može sigurno spremiti."
	}
	z, err := posta.Zakljucaj(kljuc, lozinka)
	if err != nil {
		return nil, err.Error()
	}
	return z, ""
}

// SpremiMLetvu upisuje račun čvora za mletva.voda.hr. Prijava se provjeri na
// samom sustavu prije spremanja. „Iz e-pošte“ uzima račun e-pošte prijavljenog
// korisnika — isti je račun domene — pa se lozinka ne upisuje dvaput;
// korisničko ime ondje ide bez „@voda.hr“. Račun se briše samo posebnim
// gumbom, kao i račun HydroViewa.
func (h *TelemetrijaHandler) SpremiMLetvu(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	const natrag = "/administracija/telemetrija"
	sr := h.sustaviRepo()
	if sr == nil || h.kljuc == nil {
		redirectWith(w, r, natrag, "error", "Spremište računa nije dostupno.")
		return
	}
	if r.FormValue("radnja") == "obrisi" {
		obrisiRacun(w, r, "Račun za mobilnu stranicu Hrvatskih voda", func() error { return sr.Obrisi(ctx, mletva.Podrijetlo) })
		return
	}
	korisnik, lozinka, greska := h.racunZaMLetvu(r)
	if greska != "" {
		redirectWith(w, r, natrag, "error", greska)
		return
	}
	if korisnik == "" {
		redirectWith(w, r, natrag, "error", porukaPraznogKorisnika)
		return
	}
	if lozinka == "" {
		redirectWith(w, r, natrag, "error", "Upišite i lozinku.")
		return
	}
	provjera, otkazi := context.WithTimeout(ctx, 45*time.Second)
	defer otkazi()
	if err := (&mletva.Klijent{}).Prijava(provjera, korisnik, lozinka); err != nil {
		redirectWith(w, r, natrag, "error", "Prijava na "+mletva.Podrijetlo+" nije prošla, ništa nije spremljeno: "+err.Error())
		return
	}
	z, greska := h.zakljucaj(lozinka)
	if greska != "" {
		redirectWith(w, r, natrag, "error", greska)
		return
	}
	if err := sr.Spremi(ctx, &repository.RacunSustava{Sustav: mletva.Podrijetlo, Korisnik: korisnik, Lozinka: z}); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Prijava na "+mletva.Podrijetlo+" je provjerena i račun "+korisnik+" je spremljen na ovaj čvor.")
}

// racunZaMLetvu čita korisnika i lozinku iz obrasca, ili ih uz „Uzmi moj
// račun e-pošte“ uzima iz profila prijavljenog korisnika; uz neuspjeh vraća
// poruku za korisnika.
func (h *TelemetrijaHandler) racunZaMLetvu(r *http.Request) (korisnik, lozinka, greska string) {
	if r.FormValue("iz_poste") == "" {
		return strings.TrimSpace(r.FormValue("mletva_korisnik")), r.FormValue("mletva_lozinka"), ""
	}
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	if h.racunPoste == nil || u == nil {
		return "", "", "Račun e-pošte nije dostupan."
	}
	k, l, err := h.racunPoste(r.Context(), u)
	if err != nil {
		return "", "", "Račun e-pošte se ne da uzeti: " + err.Error()
	}
	return KorisnikDomene(k), l, ""
}

// KorisnikDomene svodi adresu e-pošte na korisničko ime domene:
// „pero.peric@voda.hr“ → „pero.peric“. Ime bez „@“ ostaje kakvo jest.
func KorisnikDomene(k string) string {
	k = strings.TrimSpace(k)
	if i := strings.Index(k, "@"); i > 0 {
		return k[:i]
	}
	return k
}

func (h *TelemetrijaHandler) sustaviRepo() *repository.RacuniSustavaRepository {
	if h.sustavi == nil {
		return nil
	}
	return h.sustavi()
}

func (h *TelemetrijaHandler) repo() *repository.HidroViewRepository {
	if h.racuni == nil {
		return nil
	}
	return h.racuni()
}
