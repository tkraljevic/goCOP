package web

import (
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/repository"
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
	CurrentUser     *models.User
	Permissions     *models.UserPermissions
	All             bool   // čvor prati sve, iz postavki
	Cvor            string // ime ovog računala, da se zna čije se praćenje mijenja
	Rules           []peers.Subscription
	Channels        []KanalStranice
	Sectors         []models.Sector
	Areas           []models.Area
	ThisYear        int
	OdGodine        int                 // zadana početna godina u obrascu
	Predlog         *PrijedlogPretplate // pravila iz zaduženja korisnika koja još ne postoje
	PovijestMjeseci int                 // koliko mjeseci tuđih očitanja čvor prima; 0 = sva
	Postavke        *PostavkeStranice   // postavke ovog čvora iz gocop.toml; nil za ne-administratora
	SuccessMessage  string
	ErrorMessage    string
	ActiveNav       string
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
	data.PovijestMjeseci = repository.MjeseciPovijestiOcitanja()
	if ch, err := h.kanaliStranice(r, data.PovijestMjeseci); err == nil {
		data.Channels = ch
	} else {
		log.Printf("Stranica pretplata: tablica kanala nije složena: %v", err)
		data.ErrorMessage = porukaTabliceKanala
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

// porukaTabliceKanala: pregled brisanja ili popis kanala nije se dao
// pročitati, pa stranica brisanje ne nudi; tehnički razlog ide u dnevnik
const porukaTabliceKanala = "Tablica „Što računalo drži” nije se mogla složiti, pa brisanje s računala nije ponuđeno. Osvježite stranicu, a ako se ponovi, ponovno pokrenite program."

// kanaliStranice su kanali koje računalo drži; uz one koje pretplata više
// ne pokriva ide pregled brisanja, isti koji brisanje poslije učini
func (h *SubscriptionsHandler) kanaliStranice(r *http.Request, povijestMjeseci int) ([]KanalStranice, error) {
	popis, err := h.peers.Channels(r.Context())
	if err != nil {
		return nil, err
	}
	out := make([]KanalStranice, 0, len(popis))
	for _, c := range popis {
		k := KanalStranice{ChannelStat: c, Naziv: nazivVrste(c.Kind), PovijestMjeseci: povijestMjeseci}
		if !c.Wanted {
			k.Brisanje = h.pregledBrisanja(r, c.Channel)
		}
		out = append(out, k)
	}
	return out, nil
}

// pregledBrisanja je pregled brisanja jednog kanala; nil kad se nije dao
// složiti, pa stranica brisanje tog kanala ne nudi, a ostale kanale
// pokaže. Tehnički razlog ide u dnevnik.
func (h *SubscriptionsHandler) pregledBrisanja(r *http.Request, kanal string) *peers.Brisanje {
	b, err := h.peers.PregledBrisanja(r.Context(), kanal)
	if err != nil {
		log.Printf("Stranica pretplata: pregled brisanja kanala %s nije složen: %v", kanal, err)
		return nil
	}
	return &b
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
	pravila, err := h.pravilaZaDodati(r, rule, f("predlog") == "1")
	if err != nil {
		redirectWith(w, r, "/pretplate", "error", err.Error())
		return
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

// Poruke o krivo upisanom polju obrasca: što nije dodano, zašto (što piše
// u polju i kako se upisuje) i što upisati. Do 0.0.38 „2025.” s točkom
// tiho je postajalo „sve godine”, a „90 dana” „trajno”.
const (
	porukaOdGodine = "Pretplata nije dodana: u polju „Od godine” piše „%s”, a godina se upisuje s četiri znamenke i bez točke. Upišite npr. 2025 ili ostavite polje prazno za sve godine od početka."
	porukaDoGodine = "Pretplata nije dodana: u polju „Do godine” piše „%s”, a godina se upisuje s četiri znamenke i bez točke. Upišite npr. 2026 ili ostavite polje prazno za sve godine ubuduće."
	porukaDana     = "Pretplata nije dodana: u polju „Primljeni sadržaj drži (dana)” piše „%s”, a upisuje se cijeli broj dana veći od nule. Upišite npr. 90 ili ostavite polje prazno da sadržaj ostane trajno."
)

// upisanoUPoruci je upisana vrijednost za poruku o grešci, skraćena da
// poruka ostane čitljiva
func upisanoUPoruci(v string) string {
	if r := []rune(v); len(r) > 24 {
		return string(r[:24]) + "…"
	}
	return v
}

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
		return 0, fmt.Errorf(poruka, upisanoUPoruci(v))
	}
	return cijeliBrojIzObrasca(v, 1000, poruka)
}

// cijeliBrojIzObrasca: prazno je 0, inače samo znamenke i barem najmanje
func cijeliBrojIzObrasca(v string, najmanje int, poruka string) (int, error) {
	if v == "" {
		return 0, nil
	}
	if strings.Trim(v, "0123456789") != "" {
		return 0, fmt.Errorf(poruka, upisanoUPoruci(v))
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < najmanje {
		return 0, fmt.Errorf(poruka, upisanoUPoruci(v))
	}
	return n, nil
}

// dodajPravila upisuje pravila redom. Pravilo koje već postoji je greška
// (peers.ErrIstoPravilo); prijedlog iz zaduženja ono što se već prati ni ne
// nudi (pravilaPrijedloga).
func (h *SubscriptionsHandler) dodajPravila(r *http.Request, pravila []peers.Subscription) ([]peers.Subscription, error) {
	var dodana []peers.Subscription
	for _, p := range pravila {
		upisano, err := h.peers.AddSubscription(r.Context(), p)
		if err != nil {
			return dodana, err
		}
		dodana = append(dodana, upisano)
	}
	return dodana, nil
}

// vrstePrijedloga: vodočuvaru na laptopu treba njegov dnevnik (listovi,
// zadaci iz ureda, ovjere i parafe) i očitanja njegova područja, a ne samo
// dnevnici radova i prijave, koje je prijedlog donosio do 0.0.38
var vrstePrijedloga = []string{ledger.ChannelVodocuvar, ledger.ChannelReadings, ledger.ChannelPrijave, ledger.ChannelJournals}

// pravilaPrijedloga su pravila prijedloga iz zaduženja koja postojeća
// pravila još ne pokrivaju. Istom provjerom stranica prijedlog nudi i
// prihvaćen prijedlog dodaje pravila, pa vodočuvar koji je prihvatio
// prijedlog do 0.0.38 (dnevnici i prijave) dobije ponudu za ono što mu
// nedostaje. Očitanja nemaju dokumenata, pa ni razine ni roka.
func pravilaPrijedloga(osnova peers.Subscription, postojeca []peers.Subscription, sektor string, godina int) []peers.Subscription {
	var out []peers.Subscription
	for _, vrsta := range vrstePrijedloga {
		p := osnova
		p.Kind = vrsta
		if vrsta == ledger.ChannelReadings {
			p.Razina, p.DrziDana = "", 0
		}
		if !pokrivenoPravilima(postojeca, p, sektor, godina) {
			out = append(out, p)
		}
	}
	return out
}

// pokrivenoPravilima javlja pokrivaju li postojeća pravila vrstu i područje
// pravila p za svaku godinu od njegove početne do ove, istim pravilom po
// kojem računalo traži kanale (peers.Wants.Match). Razina i rok se ne
// gledaju: što je korisnik već sam odabrao, prijedlog ne mijenja.
func pokrivenoPravilima(postojeca []peers.Subscription, p peers.Subscription, sektor string, godina int) bool {
	w := peers.Wants{Rules: postojeca}
	sektorPodrucja := func(int) string { return sektor }
	for g := max(p.YearFrom, 1); g <= godina; g++ {
		if !w.Match(ledger.ChannelFor(p.Kind, p.AreaID, g), sektorPodrucja) {
			return false
		}
	}
	return true
}

// pravilaZaDodati: obrazac dodaje jedno pravilo, a prihvaćen prijedlog iz
// zaduženja ono što od njega nedostaje
func (h *SubscriptionsHandler) pravilaZaDodati(r *http.Request, rule peers.Subscription, prijedlog bool) ([]peers.Subscription, error) {
	if prijedlog {
		return h.pravilaIzPrijedloga(r, rule)
	}
	return []peers.Subscription{rule}, nil
}

// pravilaIzPrijedloga: prihvaćen prijedlog dodaje ono što postojeća
// pravila u tom trenutku još ne pokrivaju
func (h *SubscriptionsHandler) pravilaIzPrijedloga(r *http.Request, osnova peers.Subscription) ([]peers.Subscription, error) {
	postojeca, err := h.peers.ListSubscriptions(r.Context())
	if err != nil {
		return nil, fmt.Errorf("Pretplate nisu dodane: postojeće pretplate nisu se mogle pročitati (%w).", err)
	}
	sektor := ""
	if u, _ := r.Context().Value(contextKeyUser).(*models.User); u != nil {
		_, sektor = zaduzenoPodrucje(u)
	}
	pravila := pravilaPrijedloga(osnova, postojeca, sektor, time.Now().In(models.Zagreb).Year())
	if len(pravila) == 0 {
		return nil, errors.New("Ništa nije dodano: ovo računalo već prati sve što prijedlog donosi.")
	}
	return pravila, nil
}

// porukaDodano nabroji sva dodana pravila; rečenica završava točkom, a
// opis koji već završava godinom s točkom („od 2025.”) ne dobiva drugu
func porukaDodano(dodana []peers.Subscription) string {
	opisi := make([]string, len(dodana))
	for i, p := range dodana {
		opisi[i] = p.Label()
	}
	n := len(dodana)
	if n == 0 {
		return "Ništa nije dodano."
	}
	opisi[n-1] = strings.TrimSuffix(opisi[n-1], ".") + "."
	if n == 1 {
		return "Pretplata je dodana: " + opisi[0]
	}
	return fmt.Sprintf(models.UzBrojHR(n, "Dodana je %d pretplata: %s", "Dodane su %d pretplate: %s", "Dodano je %d pretplata: %s"), n, strings.Join(opisi, "; "))
}

// PrijedlogPretplate je ono što stranica predlaže iz zaduženja: osnova
// (područje, početna godina, razina i rok) i pravila koja još ne postoje
type PrijedlogPretplate struct {
	peers.Subscription
	Pravila []peers.Subscription
}

// Sto su vrste prijedloga riječima, za „pratiti …”
func (p PrijedlogPretplate) Sto() string {
	nazivi := make([]string, len(p.Pravila))
	for i, r := range p.Pravila {
		nazivi[i] = vrstaZaPracenje(r.Kind)
	}
	if n := len(nazivi); n > 1 {
		return strings.Join(nazivi[:n-1], ", ") + " i " + nazivi[n-1]
	}
	return strings.Join(nazivi, "")
}

// SaSlikama javlja donosi li prijedlog išta uz što stižu slike: očitanja
// nemaju dokumenata
func (p PrijedlogPretplate) SaSlikama() bool {
	for _, r := range p.Pravila {
		if r.Kind != ledger.ChannelReadings {
			return true
		}
	}
	return false
}

// vrstaZaPracenje je vrsta kanala u akuzativu, za „pratiti …”
func vrstaZaPracenje(vrsta string) string {
	switch vrsta {
	case ledger.ChannelVodocuvar:
		return "vodočuvarski dnevnik"
	case ledger.ChannelReadings:
		return "očitanja"
	case ledger.ChannelPrijave:
		return "prijave s terena"
	}
	return "dnevnike radova"
}

// predlogPretplate slaže prijedlog iz zaduženja: vodočuvar ili djelatnik s
// područjem dobije prijedlog za svoje područje od prošle godine, sa slikama
// i rokom od 90 dana, za vrste koje još ne prati (pravilaPrijedloga). Bez
// zaduženog područja ili kad već prati sve vrste prijedloga, prijedloga
// nema.
func predlogPretplate(u *models.User, postojeca []peers.Subscription, godina int) *PrijedlogPretplate {
	if u == nil {
		return nil
	}
	area, sektor := zaduzenoPodrucje(u)
	if area == 0 {
		return nil
	}
	osnova := peers.Subscription{AreaID: area, YearFrom: godina - 1, Razina: peers.RazinaPregled, DrziDana: 90}
	pravila := pravilaPrijedloga(osnova, postojeca, sektor, godina)
	if len(pravila) == 0 {
		return nil
	}
	return &PrijedlogPretplate{Subscription: osnova, Pravila: pravila}
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
