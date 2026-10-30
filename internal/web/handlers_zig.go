package web

import (
	"html/template"
	"io"
	"net/http"
	"net/url"
	"slices"
	"time"

	"gocop/internal/models"
)

// Žig centra: sken žiga po sektoru; ide na PDF ovjerenog akta. Mijenja ga
// uprava sektora, a ona modul Administracija zadano nema, pa žig stoji na
// stranici sektora, uz njegove podatke (Odluke (30. 10. 2026.)). Stare
// adrese pod /administracija/zig i dalje rade.

// SektorData je stranica sektora: podaci svima koji vide Registre, a žig
// samo upravi sektora
type SektorData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage string
	ErrorMessage   string

	Sektor       models.Sector
	SmijeZig     bool            // uprava sektora: vidi i mijenja žig
	DrugiSektori []models.Sector // svi sektori kojima korisnik smije mijenjati žig
	Zig          *models.Zig
	Kad          time.Time
}

// SetZig daje rukovatelju predložak stranice sektora
func (h *AktiHandler) SetZig(t *template.Template) { h.tmplZig = t }

func (h *AktiHandler) sektoriZaZig(perms *models.UserPermissions, svi []models.Sector) []models.Sector {
	var out []models.Sector
	for _, sek := range svi {
		if perms != nil && perms.CanAdminister(sek.ID, 0) {
			out = append(out, sek)
		}
	}
	return out
}

// stranicaSektora je adresa stranice sektora; bez sektora popis organizacije
func stranicaSektora(sektor string) string {
	if sektor == "" {
		return "/organizacija"
	}
	return "/organizacija/sektori/" + url.PathEscape(sektor)
}

// sektorZiga čita sektor iz adrese, a sa starih adresa iz obrasca ili upita
func sektorZiga(r *http.Request) string {
	if id := r.PathValue("id"); id != "" {
		return id
	}
	return r.FormValue("sektor")
}

// ShowSektor prikazuje podatke sektora i, upravi sektora, njegov žig
func (h *AktiHandler) ShowSektor(w http.ResponseWriter, r *http.Request) {
	u, perms, base := h.base(r)
	if u == nil {
		return
	}
	// sektori se čitaju ovdje, a ne iz base(), koji grešku baze prešuti: bez
	// toga bi kvar baze izgledao kao sektor koji ne postoji
	sektori, err := h.users.ListSectors()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	i := slices.IndexFunc(sektori, func(s models.Sector) bool { return s.ID == r.PathValue("id") })
	if i < 0 {
		http.NotFound(w, r)
		return
	}
	d := SektorData{CurrentUser: u, Permissions: perms, ActiveNav: "organizacija", ViewAsBanner: viewBanner(r),
		SuccessMessage: base.SuccessMessage, ErrorMessage: base.ErrorMessage, Sektor: sektori[i]}
	if s := h.akti(); s != nil && perms.CanAdminister(d.Sektor.ID, 0) {
		d.SmijeZig, d.DrugiSektori = true, h.sektoriZaZig(perms, sektori)
		if d.Zig = s.Zig(r.Context(), d.Sektor.ID); d.Zig != nil {
			d.Kad = d.Zig.UpdatedAt
		}
	}
	if err := h.tmplZig.ExecuteTemplate(w, "sektor.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowZig je stara adresa stranice žiga: vodi na žig na stranici sektora
func (h *AktiHandler) ShowZig(w http.ResponseWriter, r *http.Request) {
	_, perms, base := h.base(r)
	sektor := r.URL.Query().Get("sektor")
	if moji := h.sektoriZaZig(perms, base.Sektori); sektor == "" && len(moji) > 0 {
		sektor = moji[0].ID
	}
	if sektor == "" {
		http.Redirect(w, r, "/organizacija", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, stranicaSektora(sektor)+"#zig", http.StatusSeeOther)
}

// HandleZig sprema novi sken ili briše žig
func (h *AktiHandler) HandleZig(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		redirectWith(w, r, stranicaSektora(r.PathValue("id")), "error", "Slika je prevelika ili obrazac nije ispravan")
		return
	}
	sektor := sektorZiga(r)
	// bez sidra #zig: poruka o uspjehu ili grešci stoji na vrhu stranice
	natrag := stranicaSektora(sektor)
	if r.FormValue("obrisi") == "1" {
		if err := s.ObrisiZig(r.Context(), perms, sektor); err != nil {
			redirectWith(w, r, natrag, "error", err.Error())
			return
		}
		redirectWith(w, r, natrag, "success", "Žig je uklonjen; akti se izdaju bez otiska žiga.")
		return
	}
	f, _, err := r.FormFile("slika")
	if err != nil {
		redirectWith(w, r, natrag, "error", "Odaberite sliku žiga (PNG ili JPEG)")
		return
	}
	defer f.Close()
	slika, _ := io.ReadAll(io.LimitReader(f, models.ZigMaxBytes+1)) // djelomično pročitanu sliku odbije provjera slike u SpremiZig
	if err := s.SpremiZig(r.Context(), perms, u, sektor, slika); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Žig je spremljen i od sada stoji na PDF-u svakog ovjerenog akta sektora.")
}

// ZigSlika daje sliku žiga za pregled, samo upravi sektora
func (h *AktiHandler) ZigSlika(w http.ResponseWriter, r *http.Request) {
	_, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	sektor := r.PathValue("id")
	if sektor == "" {
		sektor = r.URL.Query().Get("sektor") // stara adresa /administracija/zig/slika
	}
	if perms == nil || !perms.CanAdminister(sektor, 0) {
		http.Error(w, "žig smije pregledati samo uprava sektora", http.StatusForbidden)
		return
	}
	z := s.Zig(r.Context(), sektor)
	if z == nil {
		http.NotFound(w, r)
		return
	}
	posluziTudjuDatoteku(w, z.Mime, "", true)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(z.Slika)
}

// HandlePotpisSlika sprema ili briše sken vlastitog potpisa
func (h *AktiHandler) HandlePotpisSlika(w http.ResponseWriter, r *http.Request) {
	u, _, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		redirectWith(w, r, "/profile#vlastorucni", "error", "Slika je prevelika ili obrazac nije ispravan")
		return
	}
	if r.FormValue("obrisi") == "1" {
		if err := s.ObrisiPotpisSliku(r.Context(), u); err != nil {
			redirectWith(w, r, "/profile#vlastorucni", "error", err.Error())
			return
		}
		redirectWith(w, r, "/profile#vlastorucni", "success", "Sken potpisa je uklonjen.")
		return
	}
	f, _, err := r.FormFile("slika")
	if err != nil {
		redirectWith(w, r, "/profile#vlastorucni", "error", "Odaberite sliku potpisa (PNG ili JPEG)")
		return
	}
	defer f.Close()
	slika, _ := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if err := s.SpremiPotpisSliku(r.Context(), u, slika); err != nil {
		redirectWith(w, r, "/profile#vlastorucni", "error", err.Error())
		return
	}
	redirectWith(w, r, "/profile#vlastorucni", "success", "Sken potpisa je spremljen i stoji na PDF-u svakog akta koji ovjerite.")
}

// PotpisSlika daje sken vlastitog potpisa za pregled
func (h *AktiHandler) PotpisSlika(w http.ResponseWriter, r *http.Request) {
	u, _, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	z := s.PotpisSlika(r.Context(), u.ID.String())
	if z == nil {
		http.NotFound(w, r)
		return
	}
	posluziTudjuDatoteku(w, z.Mime, "", true)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(z.Slika)
}
