package web

import (
	"errors"
	"fmt"
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
	All            bool   // čvor prati sve, iz postavki
	Cvor           string // ime ovog računala, da se zna čije se praćenje mijenja
	Rules          []peers.Subscription
	Channels       []KanalStranice
	Sectors        []models.Sector
	Areas          []models.Area
	ThisYear       int
	OdGodine       int                 // zadana početna godina u obrascu
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
	godina := time.Now().In(models.Zagreb).Year()
	data := SubscriptionsPageData{
		CurrentUser: currUser, Permissions: perms, All: h.peers.WantsAll(), Cvor: opisCvora(h.peers.Node()),
		ThisYear: godina, OdGodine: godina - 1,
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

// opisCvora je ime ovog računala za vrh stranice: naziv i ime u mreži
func opisCvora(n *peers.Node) string {
	if n == nil {
		return ""
	}
	if n.Name == "" || n.Name == n.ID {
		return n.ID
	}
	return n.Name + " (" + n.ID + ")"
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
	rule, err := praviloIzObrasca(f)
	if err != nil {
		redirectWith(w, r, "/pretplate", "error", err.Error())
		return
	}
	pravila := []peers.Subscription{rule}
	if f("predlog") == "1" {
		pravila = pravilaPrijedloga(rule)
	}
	dodana, err := h.dodajPravila(r, pravila)
	if err != nil {
		redirectWith(w, r, "/pretplate", "error", err.Error())
		return
	}
	if r.FormValue("sync") == "1" {
		ctx, cancel := contextWithTimeout(r, 120*time.Second)
		defer cancel()
		h.peers.SyncAll(ctx)
	}
	redirectWith(w, r, "/pretplate", "success", porukaDodano(dodana))
}

// Poruke o krivo upisanom polju obrasca: što nije dodano, zašto i što
// upisati. Do 0.0.38 „2025.” s točkom tiho je postajalo „sve godine”, a
// „90 dana” „trajno”.
const (
	porukaOdGodine = "Pretplata nije dodana: u polje „Od godine” upišite godinu s četiri znamenke i bez točke, kao 2025, ili ga ostavite prazno za sve godine od početka."
	porukaDoGodine = "Pretplata nije dodana: u polje „Do godine” upišite godinu s četiri znamenke i bez točke, kao 2026, ili ga ostavite prazno za sve godine ubuduće."
	porukaDana     = "Pretplata nije dodana: u polje „Primljeni sadržaj drži (dana)” upišite cijeli broj dana veći od nule, kao 90, ili ga ostavite prazno da sadržaj ostane trajno."
)

// praviloIzObrasca čita pravilo iz obrasca. Godine i rok moraju biti
// upisani kako uputa kaže, inače se pravilo ne dodaje.
func praviloIzObrasca(f func(string) string) (peers.Subscription, error) {
	rule := peers.Subscription{Kind: f("kind"), SectorID: f("sector_id"), Razina: f("razina")}
	rule.AreaID, _ = strconv.Atoi(f("area_id"))
	if rule.AreaID > 0 {
		rule.SectorID = "" // područje je uže od sektora
	}
	var err error
	if rule.YearFrom, err = godinaIzObrasca(f("year_from"), porukaOdGodine); err != nil {
		return rule, err
	}
	if rule.YearTo, err = godinaIzObrasca(f("year_to"), porukaDoGodine); err != nil {
		return rule, err
	}
	rule.DrziDana, err = cijeliBrojIzObrasca(f("drzi_dana"), 1, porukaDana)
	return rule, err
}

// godinaIzObrasca: prazno je bez ograde, inače četiri znamenke bez točke
func godinaIzObrasca(v, poruka string) (int, error) {
	if v != "" && len(v) != 4 {
		return 0, errors.New(poruka)
	}
	return cijeliBrojIzObrasca(v, 1000, poruka)
}

// cijeliBrojIzObrasca: prazno je 0, inače samo znamenke i barem najmanje
func cijeliBrojIzObrasca(v string, najmanje int, poruka string) (int, error) {
	if v == "" {
		return 0, nil
	}
	if strings.Trim(v, "0123456789") != "" {
		return 0, errors.New(poruka)
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < najmanje {
		return 0, errors.New(poruka)
	}
	return n, nil
}

// dodajPravila upisuje pravila redom. Jedno pravilo koje već postoji je
// greška; od više pravila (prijedlog iz zaduženja) preskaču se ona koja već
// postoje, a greška je tek kad nijedno nije novo.
func (h *SubscriptionsHandler) dodajPravila(r *http.Request, pravila []peers.Subscription) ([]peers.Subscription, error) {
	var dodana []peers.Subscription
	for _, p := range pravila {
		upisano, err := h.peers.AddSubscription(r.Context(), p)
		if errors.Is(err, peers.ErrIstoPravilo) && len(pravila) > 1 {
			continue
		}
		if err != nil {
			return dodana, err
		}
		dodana = append(dodana, upisano)
	}
	if len(dodana) == 0 {
		return nil, peers.ErrIstoPravilo
	}
	return dodana, nil
}

// pravilaPrijedloga: vodočuvaru na laptopu treba njegov dnevnik (listovi,
// zadaci iz ureda, ovjere i parafe) i očitanja njegova područja, a ne samo
// dnevnici radova i prijave, koje je prijedlog donosio do 0.0.38. Očitanja
// nemaju dokumenata, pa ni razine ni roka.
func pravilaPrijedloga(osnova peers.Subscription) []peers.Subscription {
	var out []peers.Subscription
	for _, vrsta := range []string{ledger.ChannelVodocuvar, ledger.ChannelReadings, ledger.ChannelPrijave, ledger.ChannelJournals} {
		p := osnova
		p.Kind = vrsta
		if vrsta == ledger.ChannelReadings {
			p.Razina, p.DrziDana = "", 0
		}
		out = append(out, p)
	}
	return out
}

// porukaDodano nabroji sva dodana pravila
func porukaDodano(dodana []peers.Subscription) string {
	opisi := make([]string, len(dodana))
	for i, p := range dodana {
		opisi[i] = p.Label()
	}
	n := len(dodana)
	if n == 1 {
		return "Pretplata je dodana: " + opisi[0]
	}
	return fmt.Sprintf(models.UzBrojHR(n, "Dodana je %d pretplata: %s", "Dodane su %d pretplate: %s", "Dodano je %d pretplata: %s"), n, strings.Join(opisi, "; "))
}

// predlogPretplate slaže osnovu prijedloga iz zaduženja: vodočuvar ili
// djelatnik s područjem dobije prijedlog za svoje područje od prošle
// godine, sa slikama i rokom od 90 dana; vrste dodaje pravilaPrijedloga.
// Bez zaduženog područja ili s već postojećim pravilom za njega prijedloga
// nema.
func predlogPretplate(u *models.User, postojeca []peers.Subscription, godina int) *peers.Subscription {
	if u == nil {
		return nil
	}
	area, sektor := zaduzenoPodrucje(u)
	if area == 0 || pokrivenoPravilom(postojeca, area, sektor) {
		return nil
	}
	return &peers.Subscription{AreaID: area, YearFrom: godina - 1, Razina: peers.RazinaPregled, DrziDana: 90}
}

// zaduzenoPodrucje: područje iz primarne dužnosti, inače iz prve aktivne
// dužnosti koja ga ima, i sektor prve aktivne dužnosti sa sektorom
func zaduzenoPodrucje(u *models.User) (area int, sektor string) {
	area = podrucjeDuznosti(u.PrimaryDuty(), false)
	for _, d := range u.Duties {
		if area == 0 {
			area = podrucjeDuznosti(&d, true)
		}
		if sektor == "" && d.IsActive && d.SectorID != nil {
			sektor = *d.SectorID
		}
	}
	return area, sektor
}

// podrucjeDuznosti je područje dužnosti; 0 kad ga nema ili kad se traži
// aktivna dužnost, a ova to nije
func podrucjeDuznosti(d *models.Duty, samoAktivna bool) int {
	if d == nil || d.AreaID == nil || *d.AreaID <= 0 || (samoAktivna && !d.IsActive) {
		return 0
	}
	return *d.AreaID
}

// pokrivenoPravilom javlja ima li već pravilo za područje ili njegov sektor
func pokrivenoPravilom(postojeca []peers.Subscription, area int, sektor string) bool {
	for _, r := range postojeca {
		if r.AreaID == area || (r.AreaID == 0 && sektor != "" && r.SectorID == sektor) {
			return true
		}
	}
	return false
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
