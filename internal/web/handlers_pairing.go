package web

import (
	"html/template"
	"net/http"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// Uparivanje za svakoga, ne samo administratora. Na svježem računalu ne
// postoji nijedan račun osim admina, jer imenik stiže tek sinkronizacijom;
// osoba koja je program upravo pokrenula mora ga prvo spojiti s uredom. Zato
// se čarobnjak otvara i bez prijave, ali samo dok je čvor svjež: dok u bazi
// nema drugih računa. Sigurnost ne leži u prijavi nego u uparivanju samom:
// obje strane potvrđuju isti kod, a mreža prima samo koga potvrdi nositelj
// ključa. Prijavljenima čarobnjak stoji u profilu, za dodatna računala i
// ručnu sinkronizaciju. Osnivanje mreže, opoziv i zaboravljanje čvorova
// ostaju administratoru.

type PairHandler struct {
	peers *peers.Service
	auth  *service.AuthService
	users *service.UserService
	tmpl  *template.Template
}

func NewPairHandler(peersSvc *peers.Service, auth *service.AuthService, users *service.UserService, tmpl *template.Template) *PairHandler {
	return &PairHandler{peers: peersSvc, auth: auth, users: users, tmpl: tmpl}
}

// Fresh javlja je li čvor svjež: bez ijednog računa osim početnog admina
func (h *PairHandler) Fresh() bool {
	list, err := h.users.ListUsers("", 0, "", "", "")
	if err != nil {
		return false
	}
	return len(list) <= 1
}

// sessionUser vraća prijavljenu osobu, ili nil bez valjane sesije
func (h *PairHandler) sessionUser(r *http.Request) *models.User {
	cookie, err := r.Cookie("gocop_session")
	if err != nil {
		return nil
	}
	id, err := uuid.Parse(cookie.Value)
	if err != nil {
		return nil
	}
	view, err := h.auth.AuthenticateSessionView(id)
	if err != nil || view == nil {
		return nil
	}
	return view.RealUser
}

// Gate pušta prijavljene, a neprijavljene samo dok je čvor svjež
func (h *PairHandler) Gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.sessionUser(r) == nil && !h.Fresh() {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, "Uparivanje bez prijave moguće je samo na računalu koje još nema djelatnike", http.StatusForbidden)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type PairPageData struct {
	User      *models.User // nil kad se uparuje bez prijave
	Fresh     bool
	NodeID    string
	NodeName  string
	PairPort  int
	InNetwork bool
	Network   string
	Peers     []peers.Peer
}

// ShowWizard prikazuje čarobnjak uparivanja
func (h *PairHandler) ShowWizard(w http.ResponseWriter, r *http.Request) {
	node := h.peers.Node()
	data := PairPageData{
		User: h.sessionUser(r), Fresh: h.Fresh(),
		NodeID: node.ID, NodeName: node.Name, PairPort: h.peers.Ports().Pair,
	}
	if n := h.peers.NetworkInfo(); n != nil {
		data.InNetwork, data.Network = true, n.Name
	}
	if list, err := h.peers.ListPeers(r.Context()); err == nil {
		for _, p := range list {
			if p.NodeID != node.ID { // vlastiti zapis stiže razmjenom, a nije partner
				data.Peers = append(data.Peers, p)
			}
		}
	}
	if err := h.tmpl.ExecuteTemplate(w, "uparivanje.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// --- API čarobnjaka: isto što i administratorske radnje, bez uvjeta admina ---

func (h *PairHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	st := h.peers.PairStatus()
	writeJSON(w, map[string]any{
		"waiting": st.Waiting, "pending": st.Pending, "sas": st.SAS, "peer": st.Peer, "peer_host": st.PeerHost, "error": st.Error,
		"in_network": h.peers.NetworkInfo() != nil, "fresh": h.Fresh(),
	})
}

func (h *PairHandler) HandleListen(w http.ResponseWriter, r *http.Request) {
	if err := h.peers.StartListening(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.HandleStatus(w, r)
}

func (h *PairHandler) HandleStop(w http.ResponseWriter, r *http.Request) {
	h.peers.StopListening()
	h.HandleStatus(w, r)
}

func (h *PairHandler) HandleDial(w http.ResponseWriter, r *http.Request) {
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
	h.HandleStatus(w, r)
}

func (h *PairHandler) HandleConfirm(w http.ResponseWriter, r *http.Request) {
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

func (h *PairHandler) HandleDiscover(w http.ResponseWriter, r *http.Request) {
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

// HandleSync razmjenjuje podatke sa svim poznatim čvorovima; nakon prvog
// uparivanja time stižu imenik i registri
func (h *PairHandler) HandleSync(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 120*time.Second)
	defer cancel()
	results := h.peers.SyncAll(ctx)
	writeJSON(w, map[string]any{"success": true, "results": results, "fresh": h.Fresh()})
}
