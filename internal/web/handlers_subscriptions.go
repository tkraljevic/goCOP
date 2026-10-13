package web

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/service"
)

// Što ovo računalo prati: pretplate na očitanja i dnevnike po sektoru,
// području i godinama. Ustroj, registri i djelatnici stižu svima; ovo je
// za podatke koji rastu godinama. Svaki prijavljeni bira za računalo za
// kojim sjedi: laptop uzme svoje područje i zadnje dvije godine, a što mu
// više ne treba obriše. Uredski čvor prati sve (postavka sve = true).

type SubscriptionsHandler struct {
	peers         *peers.Service
	org           *service.OrgService
	tmpl          *template.Template
	postavkeCvora izvorPostavki // gocop.toml: sve i povijest očitanja
}

func NewSubscriptionsHandler(peersSvc *peers.Service, org *service.OrgService, tmpl *template.Template) *SubscriptionsHandler {
	return &SubscriptionsHandler{peers: peersSvc, org: org, tmpl: tmpl}
}

type SubscriptionsPageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	All            bool // čvor prati sve, iz postavki
	Rules          []peers.Subscription
	Channels       []KanalStranice
	Sectors        []models.Sector
	Areas          []models.Area
	ThisYear       int
	Predlog        *peers.Subscription // pravilo iz zaduženja korisnika, ako ga još nema
	Postavke       *PostavkeStranice   // postavke ovog čvora iz gocop.toml; nil za ne-administratora
	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// ShowSubscriptions prikazuje što računalo prati i što drži
func (h *SubscriptionsHandler) ShowSubscriptions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	data := SubscriptionsPageData{
		CurrentUser: currUser, Permissions: perms, All: h.peers.WantsAll(), ThisYear: time.Now().Year(),
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "profile", ViewAsBanner: viewBanner(r),
	}
	data.Rules, _ = h.peers.ListSubscriptions(ctx)
	if ch, err := h.kanaliStranice(r); err == nil {
		data.Channels = ch
	} else {
		data.ErrorMessage = err.Error()
	}
	data.Sectors, _ = h.org.ListSectors(ctx)
	data.Areas, _ = h.org.ListAreas(ctx, "")
	data.Predlog = predlogPretplate(currUser, data.Rules, data.ThisYear)
	data.Postavke = h.postavkeCvora.za(r, "/pretplate")
	if err := h.tmpl.ExecuteTemplate(w, "pretplate.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// kanaliStranice su kanali koje računalo drži; uz one koje pretplata više
// ne pokriva ide pregled brisanja, isti koji brisanje poslije učini
func (h *SubscriptionsHandler) kanaliStranice(r *http.Request) ([]KanalStranice, error) {
	popis, err := h.peers.Channels(r.Context())
	if err != nil {
		return nil, err
	}
	out := make([]KanalStranice, 0, len(popis))
	for _, c := range popis {
		k := KanalStranice{ChannelStat: c, Naziv: nazivVrste(c.Kind)}
		if !c.Wanted {
			b, err := h.peers.PregledBrisanja(r.Context(), c.Channel)
			if err != nil {
				return nil, err
			}
			k.Brisanje = &b
		}
		out = append(out, k)
	}
	return out, nil
}

// HandleAdd dodaje pravilo i odmah pokrene razmjenu da podaci stignu
func (h *SubscriptionsHandler) HandleAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/pretplate", "error", "Neispravan zahtjev")
		return
	}
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	rule := peers.Subscription{Kind: f("kind"), SectorID: f("sector_id"), Razina: f("razina")}
	rule.AreaID, _ = strconv.Atoi(f("area_id"))
	rule.YearFrom, _ = strconv.Atoi(f("year_from"))
	rule.YearTo, _ = strconv.Atoi(f("year_to"))
	rule.DrziDana, _ = strconv.Atoi(f("drzi_dana"))
	if rule.AreaID > 0 {
		rule.SectorID = "" // područje je uže od sektora
	}
	// prijedlog iz zaduženja: dnevnici i prijave vlastitog područja, u dva pravila
	vrste := []string{rule.Kind}
	if f("predlog") == "1" {
		vrste = []string{ledger.ChannelJournals, ledger.ChannelPrijave}
	}
	var upisano peers.Subscription
	for _, vrsta := range vrste {
		rule.Kind = vrsta
		var err error
		// poruka opisuje pravilo kako je upisano: pravilo samo za arhivu
		// pamti se sa „Sve” i kad je u obrascu izabrano „Samo kazalo”
		if upisano, err = h.peers.AddSubscription(r.Context(), rule); err != nil {
			redirectWith(w, r, "/pretplate", "error", err.Error())
			return
		}
	}
	if r.FormValue("sync") == "1" {
		ctx, cancel := contextWithTimeout(r, 120*time.Second)
		defer cancel()
		h.peers.SyncAll(ctx)
	}
	redirectWith(w, r, "/pretplate", "success", "Pretplata je dodana: "+upisano.Label())
}

// predlogPretplate slaže pravilo iz zaduženja: vodočuvar ili djelatnik s
// područjem dobije prijedlog za svoje područje, dnevnike i prijave, od
// prošle godine, s pregledima uvijek a izvornicima 90 dana. Bez zaduženog
// područja ili s već postojećim pravilom za njega prijedloga nema.
func predlogPretplate(u *models.User, postojeca []peers.Subscription, godina int) *peers.Subscription {
	if u == nil {
		return nil
	}
	// područje iz primarne dužnosti, inače prve dužnosti koja ga ima
	var area int
	var sektor string
	if d := u.PrimaryDuty(); d != nil && d.AreaID != nil && *d.AreaID > 0 {
		area = *d.AreaID
	}
	for _, d := range u.Duties {
		if area == 0 && d.IsActive && d.AreaID != nil && *d.AreaID > 0 {
			area = *d.AreaID
		}
		if sektor == "" && d.IsActive && d.SectorID != nil {
			sektor = *d.SectorID
		}
	}
	if area == 0 {
		return nil
	}
	for _, r := range postojeca {
		if r.AreaID == area || (r.AreaID == 0 && sektor != "" && r.SectorID == sektor) {
			return nil
		}
	}
	return &peers.Subscription{AreaID: area, YearFrom: godina - 1, Razina: peers.RazinaPregled, DrziDana: 90}
}

// HandleRemove miče pravilo. Ništa se ne briše: što je pravilo donijelo
// ostaje dok ga korisnik ne obriše u tablici „Što računalo drži”, gdje vidi
// koliko se briše i što ostaje jer je nastalo ovdje. „Makni i obriši” je do
// 0.0.38 jednim klikom brisao sve što više nijedno pravilo nije pokrivalo.
func (h *SubscriptionsHandler) HandleRemove(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/pretplate", "error", "Neispravan zahtjev")
		return
	}
	id, _ := strconv.Atoi(r.FormValue("id"))
	if err := h.peers.RemoveSubscription(r.Context(), id); err != nil {
		redirectWith(w, r, "/pretplate", "error", err.Error())
		return
	}
	redirectWith(w, r, "/pretplate", "success", "Pretplata je maknuta. Što je već na ovom računalu, ostaje dok to ne obrišete u tablici „Što računalo drži”.")
}

// HandlePurge briše jedan kanal koji pretplata više ne pokriva i kaže što
// je obrisano i što je ostalo jer je nastalo ovdje
func (h *SubscriptionsHandler) HandlePurge(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/pretplate", "error", "Neispravan zahtjev")
		return
	}
	b, err := h.peers.PurgeChannel(r.Context(), strings.TrimSpace(r.FormValue("channel")))
	if err != nil {
		redirectWith(w, r, "/pretplate", "error", err.Error())
		return
	}
	redirectWith(w, r, "/pretplate", "success", porukaObrisano(b))
}
