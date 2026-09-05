package web

import (
	"html/template"
	"net/http"
	"time"

	"gocop/internal/models"
	"gocop/internal/peers"
)

// Nadzorna ploča sinkronizacije: tko je na mreži, koliko ih odgovara, s kim
// je zadnja razmjena uspjela, tko zaostaje i što ne štima. Brojke skuplja
// peers.Status; stranica ih samo prikazuje i osvježava.

type SyncHandler struct {
	peers *peers.Service
	tmpl  *template.Template
}

func NewSyncHandler(peersSvc *peers.Service, tmpl *template.Template) *SyncHandler {
	return &SyncHandler{peers: peersSvc, tmpl: tmpl}
}

type SyncPageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	Status         *peers.Status
	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// ShowDashboard prikazuje nadzornu ploču sinkronizacije
func (h *SyncHandler) ShowDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	data := SyncPageData{
		CurrentUser: currUser, Permissions: perms, ActiveNav: "sync", ViewAsBanner: viewBanner(r),
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
	}
	st, err := h.peers.Status(ctx, false)
	if err != nil {
		data.ErrorMessage = err.Error()
	}
	data.Status = st
	if err := h.tmpl.ExecuteTemplate(w, "sinkronizacija.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleStatus vraća stanje kao JSON; uz ?lan=1 pita i lokalnu mrežu
func (h *SyncHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	st, err := h.peers.Status(r.Context(), r.URL.Query().Get("lan") == "1")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, st)
}

// HandleSyncAll razmjenjuje sa svim poznatim čvorovima odjednom, po četiri
// istodobno, i vraća ishod po čvoru
func (h *SyncHandler) HandleSyncAll(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	ctx, cancel := contextWithTimeout(r, 120*time.Second)
	defer cancel()
	results := h.peers.SyncAll(ctx)
	st, _ := h.peers.Status(r.Context(), false)
	writeJSON(w, map[string]any{"success": true, "results": results, "status": st})
}
