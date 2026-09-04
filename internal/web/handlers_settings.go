package web

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
)

// SettingsHandler je stranica Postavke: ovaj čvor, poznati čvorovi,
// uparivanje, pronalaženje i sinkronizacija
type SettingsHandler struct {
	peers *peers.Service
	rec   *ledger.Recorder
	tmpl  *template.Template
}

func NewSettingsHandler(peersSvc *peers.Service, rec *ledger.Recorder, tmpl *template.Template) *SettingsHandler {
	return &SettingsHandler{peers: peersSvc, rec: rec, tmpl: tmpl}
}

type SettingsPageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	NodeID         string
	NodeName       string
	NodeVersion    string
	NodePublicKey  string
	SchemaVersion  int
	Ports          peers.Ports
	Peers          []peers.Peer
	Network        *peers.Network
	Members        []peers.Member
	VersionCounts  map[string]int
	TotalVersions  int
	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

func (h *SettingsHandler) ShowSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)

	node := h.peers.Node()
	data := SettingsPageData{
		CurrentUser:   currUser,
		Permissions:   perms,
		NodeID:        node.ID,
		NodeName:      node.Name,
		NodeVersion:   node.Version,
		NodePublicKey: node.PublicKey(),
		SchemaVersion: ledger.SchemaVersion,
		Ports:         h.peers.Ports(),
		ActiveNav:     "settings",
		ViewAsBanner:  viewBanner(r),
	}

	if list, err := h.peers.ListPeers(ctx); err == nil {
		data.Peers = list
	} else {
		data.ErrorMessage = err.Error()
	}
	data.Network = h.peers.NetworkInfo()
	if members, err := h.peers.ListMembers(ctx); err == nil {
		data.Members = members
	}
	if counts, err := h.rec.Count(ctx); err == nil {
		data.VersionCounts = counts
		for _, n := range counts {
			data.TotalVersions += n
		}
	}

	if err := h.tmpl.ExecuteTemplate(w, "settings.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// --- uparivanje ---

func (h *SettingsHandler) HandlePairStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.peers.PairStatus())
}

func (h *SettingsHandler) HandlePairListen(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if err := h.peers.StartListening(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, h.peers.PairStatus())
}

func (h *SettingsHandler) HandlePairStop(w http.ResponseWriter, r *http.Request) {
	h.peers.StopListening()
	writeJSON(w, h.peers.PairStatus())
}

func (h *SettingsHandler) HandlePairDial(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	var req struct {
		Addr string `json:"addr"`
	}
	if err := decodeBody(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Addr) == "" {
		http.Error(w, "Adresa čvora je obavezna", http.StatusBadRequest)
		return
	}
	if err := h.peers.DialPair(r.Context(), strings.TrimSpace(req.Addr)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, h.peers.PairStatus())
}

func (h *SettingsHandler) HandlePairConfirm(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	var req struct {
		Approved bool `json:"approved"`
	}
	if err := decodeBody(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	outcome, err := h.peers.ConfirmPair(r.Context(), req.Approved)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"success": true, "paired": outcome.Paired, "member": outcome.Member, "message": outcome.Message})
}

// --- mreža i članstvo ---

// HandleCreateNetwork osniva mrežu: ovaj čvor dobiva ključ mreže
func (h *SettingsHandler) HandleCreateNetwork(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeBody(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.peers.CreateNetwork(r.Context(), strings.TrimSpace(req.Name)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"success": true, "network": h.peers.NetworkInfo()})
}

func (h *SettingsHandler) HandleMembers(w http.ResponseWriter, r *http.Request) {
	members, err := h.peers.ListMembers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if members == nil {
		members = []peers.Member{}
	}
	writeJSON(w, map[string]any{"success": true, "network": h.peers.NetworkInfo(), "members": members})
}

func (h *SettingsHandler) HandleRevokeMember(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if err := h.peers.RevokeMembership(r.Context(), r.PathValue("node")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

// --- pronalaženje i sinkronizacija ---

func (h *SettingsHandler) HandleDiscover(w http.ResponseWriter, r *http.Request) {
	found, err := h.peers.Discover(r.Context(), 1500*time.Millisecond)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if found == nil {
		found = []peers.Discovered{}
	}
	writeJSON(w, map[string]any{"success": true, "found": found})
}

func (h *SettingsHandler) HandleSyncNow(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	nodeID := r.PathValue("node")
	ctx, cancel := contextWithTimeout(r, 60*time.Second)
	defer cancel()

	if nodeID == "" || nodeID == "all" {
		writeJSON(w, map[string]any{"success": true, "results": h.peers.SyncAll(ctx)})
		return
	}
	applied, sent, err := h.peers.SyncWith(ctx, nodeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"success": true, "applied": applied, "sent": sent})
}

func (h *SettingsHandler) HandleForgetPeer(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if err := h.peers.ForgetPeer(r.Context(), r.PathValue("node")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

// HandleSetBootstrap označava čvor kao stalno izložen (domena) ili to skida
func (h *SettingsHandler) HandleSetBootstrap(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	var req struct {
		Bootstrap bool     `json:"bootstrap"`
		Addresses []string `json:"addresses"`
	}
	if err := decodeBody(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p, err := h.peers.GetPeer(r.Context(), r.PathValue("node"))
	if err != nil || p == nil {
		http.Error(w, "čvor nije poznat", http.StatusNotFound)
		return
	}
	p.IsBootstrap = req.Bootstrap
	if len(req.Addresses) > 0 {
		p.Addresses = req.Addresses
	}
	if err := h.peers.SavePeer(r.Context(), *p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

// --- povijest zapisa ---

// HandleHistory vraća verzije jednog zapisa, od najnovije
func (h *SettingsHandler) HandleHistory(w http.ResponseWriter, r *http.Request) {
	entity, id := r.PathValue("entity"), r.PathValue("id")
	history, err := h.rec.History(r.Context(), entity, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if history == nil {
		history = []ledger.Version{}
	}
	writeJSON(w, map[string]any{"success": true, "entity": entity, "id": id, "versions": history})
}

// --- pomoćne ---

func requireAdmin(r *http.Request) error {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if perms == nil || !perms.IsGlobalAdmin {
		return errForbidden
	}
	return nil
}

var errForbidden = &forbiddenError{}

type forbiddenError struct{}

func (*forbiddenError) Error() string {
	return "ovu radnju smije obaviti samo globalni administrator"
}

func decodeBody(r *http.Request, v any) error {
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(v); err != nil {
			return errBadJSON(err)
		}
		return nil
	}
	return r.ParseForm()
}

func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
