package web

import (
	"html/template"
	"io"
	"net/http"
	"time"

	"gocop/internal/models"
)

// Žig centra: sken žiga po sektoru, u Administraciji; ide na PDF ovjerenog akta

// ZigData je stranica žiga
type ZigData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage string
	ErrorMessage   string

	Sektori []models.Sector // sektori kojima korisnik smije mijenjati žig
	Sektor  string
	Zig     *models.Zig
	Ima     bool
	Kad     time.Time
}

// SetZig daje rukovatelju predložak stranice žiga
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

// ShowZig prikazuje žig sektora i obrazac za novi sken
func (h *AktiHandler) ShowZig(w http.ResponseWriter, r *http.Request) {
	u, perms, base := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	d := ZigData{CurrentUser: u, Permissions: perms, ActiveNav: "admin", ViewAsBanner: viewBanner(r), SuccessMessage: base.SuccessMessage, ErrorMessage: base.ErrorMessage,
		Sektori: h.sektoriZaZig(perms, base.Sektori), Sektor: r.URL.Query().Get("sektor")}
	if d.Sektor == "" && len(d.Sektori) > 0 {
		d.Sektor = d.Sektori[0].ID
	}
	if d.Sektor != "" {
		d.Zig = s.Zig(r.Context(), d.Sektor)
		d.Ima = d.Zig != nil
		if d.Zig != nil {
			d.Kad = d.Zig.UpdatedAt
		}
	}
	if err := h.tmplZig.ExecuteTemplate(w, "administracija_zig.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleZig sprema novi sken ili briše žig
func (h *AktiHandler) HandleZig(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		redirectWith(w, r, "/administracija/zig", "error", "Slika je prevelika ili obrazac nije ispravan")
		return
	}
	sektor := r.FormValue("sektor")
	natrag := "/administracija/zig?sektor=" + sektor
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
	slika, _ := io.ReadAll(io.LimitReader(f, models.ZigMaxBytes+1))
	if err := s.SpremiZig(r.Context(), perms, u, sektor, slika); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Žig je spremljen i od sada stoji na PDF-u svakog ovjerenog akta sektora.")
}

// ZigSlika daje sliku žiga za pregled
func (h *AktiHandler) ZigSlika(w http.ResponseWriter, r *http.Request) {
	s := h.svc(w)
	if s == nil {
		return
	}
	z := s.Zig(r.Context(), r.URL.Query().Get("sektor"))
	if z == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", z.Mime)
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
	w.Header().Set("Content-Type", z.Mime)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(z.Slika)
}
