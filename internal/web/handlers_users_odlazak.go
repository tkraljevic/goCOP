package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/service"
)

// HandleOdlazak bilježi odlazak osobe iz službe (mirovina, odlazak,
// premještaj): prijava se isključi i sva zaduženja opozovu, a račun ostaje.
// Izvana račun ili zaduženje uprave organizacije traži lozinku vlastitog
// računa, kao brisanje i opoziv.
func (h *UsersHandler) HandleOdlazak(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/users?error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	userID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Redirect(w, r, "/users?error=Neispravan+ID+korisnika", http.StatusSeeOther)
		return
	}
	back := "/users/" + userID.String()
	if h.odbijenoBezLozinke(w, r, back, h.diraRacunUprave(userID)) {
		return
	}
	req := service.OdlazakRequest{UserID: userID, Razlog: models.RazlogOdlaska(r.FormValue("razlog")), TudjimOcima: viewBanner(r).Viewing}
	if dan := strings.TrimSpace(r.FormValue("dan")); dan != "" {
		t, err := time.ParseInLocation("2006-01-02", dan, models.Zagreb)
		if err != nil {
			redirectWith(w, r, back, "error", "Dan odlaska nije ispravan datum.")
			return
		}
		req.Dan = t
	}
	if err := h.userService.OdlazakIzSluzbe(perms, req); err != nil {
		redirectWith(w, r, back, "error", porukaDjelatnika(err))
		return
	}
	redirectWith(w, r, back, "success", "Odlazak je zabilježen: prijava je isključena, a zaduženja opozvana. Račun i upisi ostaju.")
}
