package web

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	peers *peers.Service
	org   *service.OrgService
	tmpl  *template.Template
}

func NewSubscriptionsHandler(peersSvc *peers.Service, org *service.OrgService, tmpl *template.Template) *SubscriptionsHandler {
	return &SubscriptionsHandler{peers: peersSvc, org: org, tmpl: tmpl}
}

type SubscriptionsPageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	All            bool // čvor prati sve, iz postavki
	Rules          []peers.Subscription
	Channels       []peers.ChannelStat
	Sectors        []models.Sector
	Areas          []models.Area
	ThisYear       int
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
	if ch, err := h.peers.Channels(ctx); err == nil {
		data.Channels = ch
	} else {
		data.ErrorMessage = err.Error()
	}
	data.Sectors, _ = h.org.ListSectors(ctx)
	data.Areas, _ = h.org.ListAreas(ctx, "")
	if err := h.tmpl.ExecuteTemplate(w, "pretplate.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleAdd dodaje pravilo i odmah pokrene razmjenu da podaci stignu
func (h *SubscriptionsHandler) HandleAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/pretplate", "error", "Neispravan zahtjev")
		return
	}
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	rule := peers.Subscription{Kind: f("kind"), SectorID: f("sector_id")}
	rule.AreaID, _ = strconv.Atoi(f("area_id"))
	rule.YearFrom, _ = strconv.Atoi(f("year_from"))
	rule.YearTo, _ = strconv.Atoi(f("year_to"))
	if rule.AreaID > 0 {
		rule.SectorID = "" // područje je uže od sektora
	}
	if _, err := h.peers.AddSubscription(r.Context(), rule); err != nil {
		redirectWith(w, r, "/pretplate", "error", err.Error())
		return
	}
	if r.FormValue("sync") == "1" {
		ctx, cancel := contextWithTimeout(r, 120*time.Second)
		defer cancel()
		h.peers.SyncAll(ctx)
	}
	redirectWith(w, r, "/pretplate", "success", "Pretplata je dodana: "+rule.Label())
}

// HandleRemove miče pravilo; uz purge=1 briše i podatke koje više ništa ne pokriva
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
	msg := "Pretplata je maknuta; podaci ostaju dok ih ne obrišete."
	if r.FormValue("purge") == "1" {
		removed, err := h.peers.PurgeUnwanted(r.Context())
		if err != nil {
			redirectWith(w, r, "/pretplate", "error", err.Error())
			return
		}
		var n int64
		for _, c := range removed {
			n += c
		}
		msg = "Pretplata je maknuta i s računala je obrisano " + strconv.FormatInt(n, 10) + " verzija."
	}
	redirectWith(w, r, "/pretplate", "success", msg)
}

// HandlePurge briše jedan kanal koji pretplata više ne pokriva
func (h *SubscriptionsHandler) HandlePurge(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/pretplate", "error", "Neispravan zahtjev")
		return
	}
	channel := strings.TrimSpace(r.FormValue("channel"))
	n, err := h.peers.PurgeChannel(r.Context(), channel)
	if err != nil {
		redirectWith(w, r, "/pretplate", "error", err.Error())
		return
	}
	redirectWith(w, r, "/pretplate", "success", "Obrisano s računala: "+channel+" ("+strconv.FormatInt(n, 10)+" verzija)")
}
