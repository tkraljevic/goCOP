package web

import (
	"net/http"
	"strings"
	"time"

	"gocop/internal/models"
)

// Proglašenje i prekid obrane na dionici.
//
// Obranu proglašava čovjek. Program zna kad je vodostaj prešao prag i to
// zabilježi uz epizodu, ali odluku ne donosi umjesto rukovoditelja — obrana se
// zna proglasiti i dan prije nego što voda dođe do praga, po prognozi ili po
// uzvodnim vodostajima, a zna se i držati nakon što voda padne.

// mjerodavnaLetva vraća letvu po kojoj se dionica vodi: prvu upisanu na
// poddionicama. Dionica u pravilu ima jednu, a ista letva vrijedi za više
// dionica — Batina za cijelo BP 34.
func (h *SectionsHandler) mjerodavnaLetva(r *http.Request, code string) models.Station {
	if h.stationService == nil {
		return models.Station{}
	}
	st, err := h.stationService.GetSectionStations(r.Context(), code)
	if err != nil || len(st) == 0 {
		return models.Station{}
	}
	return st[0]
}

// trenutakIzObrasca čita datum i vrijeme iz polja tipa datetime-local. Prazno
// polje znači sada, jer se obrana najčešće upisuje u trenutku u kojem se i
// proglašava.
func trenutakIzObrasca(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Now(), nil
	}
	return time.ParseInLocation("2006-01-02T15:04", s, time.Local)
}

// HandleDeclareDefense otvara epizodu obrane na dionici
func (h *SectionsHandler) HandleDeclareDefense(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	back := "/sections/" + code + "#obrana"
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, back, "error", "Neispravan zahtjev")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	user, _ := r.Context().Value(contextKeyUser).(*models.User)
	if h.episodeService == nil || user == nil {
		redirectWith(w, r, back, "error", "Obrana se ne može proglasiti")
		return
	}
	at, err := trenutakIzObrasca(r.FormValue("started_at"))
	if err != nil {
		redirectWith(w, r, back, "error", "Datum i vrijeme nisu čitljivi")
		return
	}
	faza := models.DefensePhase(r.FormValue("phase"))
	e, err := h.episodeService.Declare(r.Context(), perms, user.ID.String(), code,
		h.mjerodavnaLetva(r, code), at, faza,
		r.FormValue("basis"), strings.TrimSpace(r.FormValue("note")))
	if err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	poruka := "Proglašena je " + strings.ToLower(faza.Label()) + " od " + e.StartedAt.Format("2.1.2006. 15:04") + "."
	if e.DeclaredBeforeThreshold() {
		poruka += " Vodostaj u tom trenutku još nije bio preko praga."
	}
	redirectWith(w, r, back, "success", poruka)
}

// HandleRaiseDefense podiže stupanj obrane koja traje
func (h *SectionsHandler) HandleRaiseDefense(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	back := "/sections/" + code + "#obrana"
	if err := r.ParseForm(); err != nil || h.episodeService == nil {
		redirectWith(w, r, back, "error", "Neispravan zahtjev")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	faza := models.DefensePhase(r.FormValue("phase"))
	if err := h.episodeService.Raise(r.Context(), perms, code, faza, strings.TrimSpace(r.FormValue("note"))); err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, back, "success", "Obrana je podignuta na "+strings.ToLower(faza.Label())+".")
}

// HandleEndDefense prekida obranu na dionici
func (h *SectionsHandler) HandleEndDefense(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	back := "/sections/" + code + "#obrana"
	if err := r.ParseForm(); err != nil || h.episodeService == nil {
		redirectWith(w, r, back, "error", "Neispravan zahtjev")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	user, _ := r.Context().Value(contextKeyUser).(*models.User)
	if user == nil {
		redirectWith(w, r, back, "error", "Obrana se ne može prekinuti")
		return
	}
	at, err := trenutakIzObrasca(r.FormValue("ended_at"))
	if err != nil {
		redirectWith(w, r, back, "error", "Datum i vrijeme nisu čitljivi")
		return
	}
	if err := h.episodeService.End(r.Context(), perms, user.ID.String(), code, at,
		strings.TrimSpace(r.FormValue("note"))); err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, back, "success", "Obrana je prekinuta "+at.Format("2.1.2006. 15:04")+".")
}
