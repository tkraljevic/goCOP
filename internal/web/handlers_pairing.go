package web

import (
	"context"
	"html/template"
	"log"
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
// samo dok je čvor svjež (u bazi nema drugih računa) i samo osobi za tim
// računalom: izravno s njega, ili iz lokalne mreže uz jednokratni kod iz
// dnevnika programa, isto kao Postavljanje, i kad čvor ne radi pod Postavom
// (Odluke (30. 10. 2026.), 15). Kroz tunel nikad. Bez prijave se računala samo
// upare: u mrežu drugo računalo prima samo prijavljeni administrator, a
// čvor koji drži ključ mreže ili ovlast za primanje čarobnjak bez prijave
// ne otvara. Prijavljenom čarobnjak otvara samo globalni administrator s
// promijenjenom lozinkom, koji ne gleda tuđim očima: njegova potvrda
// uparivanja na čvoru koji drži ključ mreže prima drugi čvor u mrežu, a
// izvana uz lozinku računa (Odluke (30. 10. 2026.), 1). Izvana lozinku
// traži svaka potvrda uparivanja, i na računalu koje još nije ni u jednoj
// mreži, jer njome ulazi u tuđu (Odluke (30. 10. 2026.), 12). Osnivanje mreže,
// opoziv i zaboravljanje čvorova ostaju u postavkama.

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

	// primaClanove javlja drži li čvor ključ mreže ili ovlast za primanje
	primaClanove func() bool
	// potvrdi prenosi odluku o kodu uparivanja; zadano razmjena čvorova
	potvrdi func(ctx context.Context, approved, primi bool) (peers.PairOutcome, error)
	// kodPostavljanja provjerava osobu za računalom: izravno s njega ili
	// jednokratni kod iz dnevnika (PostavljanjeHandler.dopusteno)
	kodPostavljanja func(r *http.Request, kod string) bool
	// lozinkaRacuna provjerava lozinku računa za primanje izvana
	lozinkaRacuna func(uuid.UUID, string) error
}

// zaglavljeKoda nosi kod iz dnevnika u pozivima čarobnjaka bez prijave
const zaglavljeKoda = "X-Kod-Postavljanja"

// SetKodPostavljanja daje provjeru jednokratnog koda iz dnevnika
func (h *PairHandler) SetKodPostavljanja(f func(r *http.Request, kod string) bool) {
	h.kodPostavljanja = f
}

// kodIz je kod iz dnevnika iz zaglavlja poziva ili iz adrese stranice
func kodIz(r *http.Request) string {
	if k := r.Header.Get(zaglavljeKoda); k != "" {
		return k
	}
	return r.URL.Query().Get("kod")
}

// zaRacunalom javlja je li zahtjev bez prijave od osobe za ovim računalom
func (h *PairHandler) zaRacunalom(r *http.Request) bool {
	return h.kodPostavljanja != nil && h.kodPostavljanja(r, kodIz(r))
}

// svjezRok je koliko dugo vrijedi odgovor "čvor je svjež"
const svjezRok = 5 * time.Second

func NewPairHandler(peersSvc *peers.Service, auth *service.AuthService, users *service.UserService, tmpl *template.Template) *PairHandler {
	h := &PairHandler{peers: peersSvc, auth: auth, users: users, tmpl: tmpl}
	h.brojiRacun = func() (int, error) {
		list, err := h.users.ListUsers("", 0, "", "", "")
		return len(list), err
	}
	h.primaClanove = func() bool {
		n := h.peers.NetworkInfo()
		return n != nil && n.CanAdmit
	}
	h.potvrdi = func(ctx context.Context, approved, primi bool) (peers.PairOutcome, error) {
		return h.peers.ConfirmPair(ctx, approved, primi)
	}
	if auth != nil {
		h.lozinkaRacuna = auth.ProvjeriLozinku
	}
	return h
}

// NijeViseSvjez: čvor je upravo dobio administratora (Postavljanje), pa
// uparivanje bez prijave odmah prestaje, ne tek kad istekne pričuva
func (h *PairHandler) NijeViseSvjez() { h.nijeSvjez.Store(true) }

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

// bezPrijave javlja smije li se uparivati bez prijave: čvor je svjež i ne
// drži ključ mreže ni ovlast za primanje. Takav ključ čvor dobije tek od
// administratora (osnivanje mreže, primanje s ovlašću), pa ga od tada vodi
// samo prijavljeni administrator.
func (h *PairHandler) bezPrijave() bool {
	return h.Fresh() && (h.primaClanove == nil || !h.primaClanove())
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

// Gate pušta globalnog administratora, a bez toga samo osobu za računalom
// dok je čvor svjež
func (h *PairHandler) Gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		view := h.sessionView(r)
		var o ovlastUparivanja
		switch {
		case smijeUparivati(view):
			o.korisnik = view.RealUser
		case h.bezPrijave() && h.zaRacunalom(r):
			// svjež čvor, osoba za računalom: bez prijave
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
	case view == nil && api:
		http.Error(w, "Uparivanje bez prijave moguće je samo na računalu koje još nema djelatnike: s tog računala ili iz lokalne mreže uz kod iz dnevnika programa.", http.StatusForbidden)
	case view == nil:
		http.Redirect(w, r, h.prijavaIliKod(r), http.StatusSeeOther)
	case view.RealUser.MustChangePassword && !api:
		http.Redirect(w, r, "/profile?force=1#lozinka", http.StatusSeeOther)
	case view.RealUser.MustChangePassword:
		http.Error(w, "Prije uparivanja promijenite privremenu lozinku", http.StatusForbidden)
	case view.Viewing:
		http.Error(w, "Tuđim očima se ne uparuje; vratite se sebi pa ponovite", http.StatusForbidden)
	default:
		http.Error(w, "Uparivanje i ručnu razmjenu vodi globalni administrator", http.StatusForbidden)
	}
}

// prijavaIliKod je kamo stranica čarobnjaka vodi neprijavljenog: na svježem
// računalu iz lokalne mreže na Postavljanje, gdje se upisuje kod iz
// dnevnika, a inače na prijavu
func (h *PairHandler) prijavaIliKod(r *http.Request) string {
	if klijentIz(r).IzLokalneMreze() && h.bezPrijave() {
		return "/postavljanje?put=postojeca"
	}
	return "/login"
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
	// Kod je kod iz dnevnika s kojim je stranica otvorena bez prijave; ide
	// uz svaki poziv čarobnjaka
	Kod string
	// TraziLozinku: administrator izvana potvrđuje kod uz lozinku računa
	// (Odluke (30. 10. 2026.), 1 i 12)
	TraziLozinku bool
}

// ShowWizard prikazuje čarobnjak uparivanja
func (h *PairHandler) ShowWizard(w http.ResponseWriter, r *http.Request) {
	node := h.peers.Node()
	o, _ := ovlastIz(r)
	data := PairPageData{
		User: o.korisnik, Fresh: h.Fresh(),
		NodeID: node.ID, NodeName: node.Name, PairPort: h.peers.Ports().Pair,
		TraziLozinku: h.primaIzvana(r, o),
	}
	if o.korisnik == nil {
		data.Kod = kodIz(r)
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
	st := stanjeUparivanja(r, h.peers.PairStatus())
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
		http.Error(w, porukaPoziva(r, err), http.StatusBadRequest)
		return
	}
	h.HandleStatus(w, r)
}

// Greške veze s drugim računalom nose adrese iz lokalne mreže. Iz lokalne
// mreže pokazuju se cijele, a izvana opća poruka; cijela greška tada ide u
// dnevnik programa.
const (
	opcaPorukaPoziva     = "Veza s tim računalom nije uspostavljena. Provjerite adresu ili pokrenite uparivanje s računala u lokalnoj mreži."
	opcaPorukaRazmjene   = "Veza s tim računalom nije uspostavljena. Pojedinosti su u dnevniku programa."
	opcaPorukaCekanja    = "Uparivanje nije uspjelo. Pojedinosti vidi administrator s računala u lokalnoj mreži."
	oznakaGreskeBiljeske = "greška: "
)

// porukaPoziva je odgovor na neuspio poziv drugog računala
func porukaPoziva(r *http.Request, err error) string {
	return porukaVeze(r, err, opcaPorukaPoziva)
}

// porukaVeze je cijela greška za klijenta iz lokalne mreže, a opća poruka
// za klijenta izvana
func porukaVeze(r *http.Request, err error, opca string) string {
	if !klijentIz(r).Izvana() {
		return err.Error()
	}
	log.Printf("veza s drugim računalom, zahtjev izvana: %q", err.Error())
	return opca
}

// rezultatiRazmjene su bilješke razmjene po računalu; izvana grešku
// zamijeni opća poruka
func rezultatiRazmjene(r *http.Request, rez map[string]string) map[string]string {
	if !klijentIz(r).Izvana() {
		return rez
	}
	out := make(map[string]string, len(rez))
	for cvor, biljeska := range rez {
		if strings.HasPrefix(biljeska, oznakaGreskeBiljeske) {
			log.Printf("razmjena s %s, zahtjev izvana: %q", cvor, biljeska)
			biljeska = oznakaGreskeBiljeske + opcaPorukaRazmjene
		}
		out[cvor] = biljeska
	}
	return out
}

// stanjeUparivanja je stanje uparivanja za klijenta; izvana grešku čekanja
// zamijeni opća poruka
func stanjeUparivanja(r *http.Request, st peers.PairStatus) peers.PairStatus {
	if st.Error != "" && klijentIz(r).Izvana() {
		st.Error = opcaPorukaCekanja
	}
	return st
}

// primaIzvana javlja treba li potvrda koda lozinku računa: prijavljeni
// administrator izvana. Potvrda prima drugo računalo u mrežu, ovo računalo
// njome ulazi u tuđu, a i samo uparivanje upisuje poznati čvor i njegove
// adrese, pa izvana svaka potvrda traži lozinku.
func (h *PairHandler) primaIzvana(r *http.Request, o ovlastUparivanja) bool {
	return o.korisnik != nil && klijentIz(r).Izvana()
}

// uparuje je oznaka računala koje čeka odluku o kodu, za dnevnik programa
func (h *PairHandler) uparuje() string {
	if h.peers == nil {
		return ""
	}
	return h.peers.PairStatus().Peer.DeviceID
}

func (h *PairHandler) HandleConfirm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Approved      bool   `json:"approved"`
		LozinkaRacuna string `json:"lozinka_racuna"`
	}
	if err := decodeBody(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// u mrežu prima samo prijavljeni administrator; bez prijave drugi čvor
	// ostaje uparen, ali ne postaje član
	o, _ := ovlastIz(r)
	if req.Approved && h.primaIzvana(r, o) {
		if err := lozinkaIzvana(r, h.lozinkaRacuna, o.korisnik, req.LozinkaRacuna); err != nil {
			odbijBezLozinke(w, r, o.korisnik, err)
			return
		}
	}
	racunalo := h.uparuje()
	outcome, err := h.potvrdi(r.Context(), req.Approved, o.korisnik != nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if outcome.Primljen {
		zapisiClanstvo(r, o.korisnik, "primio je u mrežu računalo %q uparivanjem", racunalo)
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
	results := rezultatiRazmjene(r, h.peers.SyncAll(ctx))
	writeJSON(w, map[string]any{"success": true, "results": results, "fresh": h.Fresh()})
}
