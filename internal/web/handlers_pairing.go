package web

import (
	"context"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// Uparivanje vodi globalni administrator, ili bilo tko za računalom svježeg
// čvora. Na svježem računalu ne postoji nijedan račun osim admina, jer
// imenik stiže tek sinkronizacijom; osoba koja je program upravo pokrenula
// mora ga prvo spojiti s uredom. Zato se čarobnjak otvara i bez prijave, ali
// samo dok je čvor svjež (u bazi nema drugih računa) i samo izravnom
// klijentu iz lokalne mreže, nikad kroz tunel. Prijavljenom čarobnjak
// otvara samo globalni administrator s promijenjenom lozinkom, koji ne gleda
// tuđim očima: potvrda uparivanja na čvoru koji drži ključ mreže prima drugi
// čvor u mrežu. Osnivanje mreže, opoziv i zaboravljanje čvorova ostaju u
// postavkama.

type PairHandler struct {
	peers *peers.Service
	auth  *service.AuthService
	users *service.UserService
	tmpl  *template.Template

	// Fresh se pita pri svakom neprijavljenom zahtjevu, pa se pamti: čvor
	// koji jednom ima djelatnike više nije svjež, a svjež se provjerava
	// najviše jednom u svjezRok
	nijeSvjez  atomic.Bool
	svjezMu    sync.Mutex
	svjezBio   bool
	svjezKad   time.Time
	brojiRacun func() (int, error) // broj računa; zadano iz registra djelatnika
}

// svjezRok je koliko dugo vrijedi odgovor "čvor je svjež"
const svjezRok = 5 * time.Second

func NewPairHandler(peersSvc *peers.Service, auth *service.AuthService, users *service.UserService, tmpl *template.Template) *PairHandler {
	h := &PairHandler{peers: peersSvc, auth: auth, users: users, tmpl: tmpl}
	h.brojiRacun = func() (int, error) {
		list, err := h.users.ListUsers("", 0, "", "", "")
		return len(list), err
	}
	return h
}

// Fresh javlja je li čvor svjež: bez ijednog računa osim početnog admina
func (h *PairHandler) Fresh() bool {
	if h.nijeSvjez.Load() {
		return false
	}
	h.svjezMu.Lock()
	defer h.svjezMu.Unlock()
	if !h.svjezKad.IsZero() && time.Since(h.svjezKad) < svjezRok {
		return h.svjezBio
	}
	n, err := h.brojiRacun()
	if err != nil {
		return false
	}
	svjez := n <= 1
	if !svjez {
		h.nijeSvjez.Store(true)
	}
	h.svjezBio, h.svjezKad = svjez, time.Now()
	return svjez
}

// sessionView vraća prijavu iz kolačića, ili nil bez valjane sesije
func (h *PairHandler) sessionView(r *http.Request) *service.SessionView {
	cookie, err := r.Cookie(imeKolacicaSesije)
	if err != nil {
		return nil
	}
	id, err := uuid.Parse(cookie.Value)
	if err != nil {
		return nil
	}
	view, err := h.auth.AuthenticateSessionView(id)
	if err != nil || view == nil || view.RealUser == nil {
		return nil
	}
	return view
}

// smijeUparivati javlja smije li prijavljeni voditi uparivanje: globalni
// administrator vlastitim očima, s promijenjenom lozinkom
func smijeUparivati(view *service.SessionView) bool {
	return view != nil && !view.Viewing && view.Perms != nil && view.Perms.IsGlobalAdmin && !view.RealUser.MustChangePassword
}

// ovlastUparivanja je ono što je Gate utvrdio o zahtjevu
type ovlastUparivanja struct {
	korisnik *models.User // nil = neprijavljen na svježem čvoru iz lokalne mreže
}

type kljucUparivanja struct{}

// ovlastIz vraća ovlast koju je Gate upisao; bez nje zahtjev nije ovlašten
func ovlastIz(r *http.Request) (ovlastUparivanja, bool) {
	o, ok := r.Context().Value(kljucUparivanja{}).(ovlastUparivanja)
	return o, ok
}

// Gate pušta globalnog administratora, a bez toga samo izravnog klijenta
// dok je čvor svjež
func (h *PairHandler) Gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		view := h.sessionView(r)
		var o ovlastUparivanja
		switch {
		case smijeUparivati(view):
			o.korisnik = view.RealUser
		case !klijentIz(r).KrozPosrednika && h.Fresh():
			// svjež čvor, izravan klijent: kao i dosad, bez prijave
		default:
			h.odbij(w, r, view)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), kljucUparivanja{}, o)))
	})
}

// odbij objašnjava zašto uparivanje nije dopušteno
func (h *PairHandler) odbij(w http.ResponseWriter, r *http.Request, view *service.SessionView) {
	api := strings.HasPrefix(r.URL.Path, "/api/")
	switch {
	case view == nil:
		if api {
			http.Error(w, "Uparivanje bez prijave moguće je samo iz lokalne mreže, na računalu koje još nema djelatnike", http.StatusForbidden)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	case view.RealUser.MustChangePassword && !api:
		http.Redirect(w, r, "/profile?force=1#lozinka", http.StatusSeeOther)
	case view.RealUser.MustChangePassword:
		http.Error(w, "Prije uparivanja promijenite zadanu lozinku", http.StatusForbidden)
	case view.Viewing:
		http.Error(w, "Tuđim očima se ne uparuje; vratite se sebi pa ponovite", http.StatusForbidden)
	default:
		http.Error(w, "Uparivanje i ručnu razmjenu vodi globalni administrator", http.StatusForbidden)
	}
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
	o, _ := ovlastIz(r)
	data := PairPageData{
		User: o.korisnik, Fresh: h.Fresh(),
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

// --- API čarobnjaka: isto što i administratorske radnje u postavkama ---

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
	// u mrežu prima samo onaj koga je Gate pustio; bez toga čvor ostaje uparen,
	// ali ne i član
	_, primi := ovlastIz(r)
	outcome, err := h.peers.ConfirmPair(r.Context(), req.Approved, primi)
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
