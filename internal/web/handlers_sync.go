package web

import (
	"context"
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
	// dohvatljivost je blok Dohvatljivost (provjera_veze.go); čita stanje
	// poslužitelja u trenutku zahtjeva
	dohvatljivost func() *DohvatljivostPrikaz
	// mjeriMrezu je mjerenje mreže ovog računala uz provjeru veze s
	// računalom (provjera_racunala.go); nil: ne mjeri se
	mjeriMrezu func(context.Context) *ProvjeraVeze
	// provjeriVezuS je peers.ProvjeriVezuS; test zadaje svoj ishod
	provjeriVezuS func(context.Context, string) (peers.ProvjeraRacunala, error)
	provjera      zadnjaSRacunalom
}

func NewSyncHandler(peersSvc *peers.Service, tmpl *template.Template) *SyncHandler {
	return &SyncHandler{peers: peersSvc, tmpl: tmpl, provjeriVezuS: peersSvc.ProvjeriVezuS}
}

type SyncPageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	Status         *peers.Status
	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	// Odbijeno je zapis odbijenih i djelomično razumljenih poruka (G7),
	// samo za administratora
	Odbijeno       []RedakOdbijenog
	OdbijenoGreska string
	// Dohvatljivost je mreža ovog računala za WEB sync, samo za administratora
	Dohvatljivost *DohvatljivostPrikaz
	// ProvjeraRacunala je provjera veze s računalom, samo za administratora
	ProvjeraRacunala *ProvjeraRacunalaPrikaz
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
	h.dodajOdbijeno(r.Context(), perms, &data)
	h.dodajDohvatljivost(perms, &data)
	h.dodajProvjeruRacunala(r, &data)
	if err := h.tmpl.ExecuteTemplate(w, "sinkronizacija.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// dodajDohvatljivost puni blok Dohvatljivost, samo za administratora
func (h *SyncHandler) dodajDohvatljivost(perms *models.UserPermissions, data *SyncPageData) {
	if perms == nil || !perms.IsGlobalAdmin || h.dohvatljivost == nil {
		return
	}
	data.Dohvatljivost = h.dohvatljivost()
}

// HandleStatus vraća stanje kao JSON; uz ?lan=1 pita i lokalnu mrežu
func (h *SyncHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	st, err := h.peers.Status(r.Context(), r.URL.Query().Get("lan") == "1")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.zaPrikaz(r, st))
}

// stanjeZaPrikaz je stanje sinkronizacije za stranicu. Administrator uz
// njega dobije pokušaje spajanja po adresi, a drugi korisnik s modulom
// Administracija stanje bez adresa i bez sirovih grešaka, koje nose adrese
// (plan WEB synca, F1).
type stanjeZaPrikaz struct {
	*peers.Status
	Pokusaji map[string][]peers.PokusajSpajanja `json:"pokusaji,omitempty"`
}

func (h *SyncHandler) zaPrikaz(r *http.Request, st *peers.Status) stanjeZaPrikaz {
	if st == nil {
		return stanjeZaPrikaz{}
	}
	if !jeAdministrator(r) {
		bezAdresa(st)
		return stanjeZaPrikaz{Status: st}
	}
	out := stanjeZaPrikaz{Status: st, Pokusaji: map[string][]peers.PokusajSpajanja{}}
	for _, p := range st.Peers {
		if l := h.peers.PokusajiSpajanja(p.NodeID); len(l) > 0 {
			out.Pokusaji[p.NodeID] = l
		}
	}
	return out
}

// bezAdresa briše iz stanja adrese računala i sirove greške razmjene
func bezAdresa(st *peers.Status) {
	st.LanDiscovered = nil
	for i := range st.Peers {
		p := &st.Peers[i]
		p.Addresses, p.LastSyncNote = nil, ""
		p.State.LastError, p.State.Tailscale, p.State.Naucene = "", nil, nil
	}
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
	writeJSON(w, map[string]any{"success": true, "results": results, "status": h.zaPrikaz(r, st)})
}
