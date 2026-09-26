package web

import (
	"context"
	"html/template"
	"net/http"

	"gocop/internal/models"
	"gocop/internal/poslovi"
)

// PripremaModela namjesti lanac prognoze iz arhive, zapiše promašaje
// provjere unatrag i ponovno izda prognozu. Postupak traje minutama, pa se
// vrti kao posao; poslužitelj ga sastavlja jer ima otvorene baze.
type PripremaModela func(ctx context.Context, p *poslovi.Posao) error

// SetPripremaModela daje stranici prognoza posao pripreme modela.
// radi kaže je li priprema na ovom čvoru uključena; pita se pri svakom
// prikazu, jer je poslužitelj uključuje tek kad otvori baze.
func (h *PrognozeHandler) SetPripremaModela(f PripremaModela, radi func() bool, reg *poslovi.Registar, tmplPosao *template.Template) {
	h.priprema, h.pripremaRadi, h.poslovi, h.tmplPosao = f, radi, reg, tmplPosao
}

func (h *PrognozeHandler) mozePripremiti() bool {
	return h.priprema != nil && h.poslovi != nil && h.tmplPosao != nil &&
		(h.pripremaRadi == nil || h.pripremaRadi())
}

// PripremiModel pokreće pripremu modela: to mijenja brojke svih prognoza, pa
// ga smije samo globalni administrator (ruta je iza samoAdmin).
func (h *PrognozeHandler) PripremiModel(w http.ResponseWriter, r *http.Request) {
	if !h.mozePripremiti() {
		redirectWith(w, r, "/prognoze#priprema", "error", "Priprema modela nije uključena na ovom čvoru.")
		return
	}
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	korisnik := ""
	if u != nil {
		korisnik = u.ID.String()
	}
	p := h.poslovi.Pokreni("Priprema modela prognoze", korisnik, "/prognoze/o-prognozi",
		func(p *poslovi.Posao) error {
			ctx, otkazi := context.WithTimeout(context.Background(), 3*60*60e9)
			defer otkazi()
			return h.priprema(ctx, p)
		})
	d := PosaoData{CurrentUser: u, Permissions: perms, ActiveNav: "prognoze", ViewAsBanner: viewBanner(r),
		PosaoID: p.ID, PosaoNaziv: p.Naziv, Natrag: "/prognoze/o-prognozi"}
	if err := h.tmplPosao.ExecuteTemplate(w, "posao.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
