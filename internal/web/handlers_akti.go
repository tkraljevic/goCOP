package web

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/poslovi"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Akti: rješenja i obavijesti o stupnju obrane. Sastave se po vodomjeru,
// ovjere u programu, ispišu kao PDF; ovjerene vide svi.

type AktiHandler struct {
	akti                                         func() *service.AktService
	users                                        *service.UserService
	stations                                     *service.StationService
	tmplPopis, tmplForm, tmplAkt, tmplPrimatelji *template.Template
	tmplSpranca                                  *template.Template
	postavkeCvora                                izvorPostavki // gocop.toml na stranici Karte (obična karta)
	tmplPosta, tmplPostaAdmin                    *template.Template
	tmplSanducic, tmplPismo, tmplNovoPismo       *template.Template
	tmplImenik                                   *template.Template
	poslovi                                      *poslovi.Registar
	tmplPotpis                                   *template.Template
	tmplZig                                      *template.Template
	tmplOpcije                                   *template.Template
	tmplTema                                     *template.Template
	tmplKarte                                    *template.Template
	peers                                        *peers.Service // paketi karte
	// drugiKorak daje drugi korak prijave izvana (račun koji šalje PIN,
	// sklopka); nil dok se ne spoji
	drugiKorak func() *service.DrugiKorak
}

// SetSpranca daje rukovatelju predložak stranice špranče
func (h *AktiHandler) SetSpranca(t *template.Template) { h.tmplSpranca = t }

func NewAktiHandler(akti func() *service.AktService, users *service.UserService, stations *service.StationService,
	popis, form, akt, primatelji *template.Template) *AktiHandler {
	return &AktiHandler{akti: akti, users: users, stations: stations, tmplPopis: popis, tmplForm: form, tmplAkt: akt, tmplPrimatelji: primatelji}
}

// PapirAkta je sve što treba predlošku papira akta (akt_papir.html)
type PapirAkta struct {
	Akt      *models.Akt
	Sektor   *models.Sector
	Podrucje *models.Area
}

// Papir vraća podatke za papir akta na stranici jednog akta
func (d AktiPageData) Papir() PapirAkta {
	return PapirAkta{Akt: d.Akt, Sektor: d.Sektor, Podrucje: d.Podrucje}
}

// AktiPageData je stranica popisa, obrasca ili jednog akta
type AktiPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions

	Akti      []models.Akt
	Akt       *models.Akt
	Filtar    repository.FiltarAkata
	Sektori   []models.Sector
	Podrucja  []models.Area
	Godine    []int
	Stupnjevi []models.DefensePhase
	Obrana    []ObranaSektora // način rada po sektoru: aktivna (otvoren dnevnik COP-a) ili preventivna
	// SmijeSastavljati: osoba sastavlja nacrte akata u nekom sektoru, pa joj
	// se nudi „Novi akt” (Odluke (8. 10. 2026.), pitanje 18)
	SmijeSastavljati bool
	// Preventivna je sektor letve kad je u preventivnoj obrani: umjesto
	// obrasca stoji poruka, a OtvaraDnevnik daje gumb za dnevnik COP-a
	Preventivna   string
	OtvaraDnevnik bool
	// popis: čipovi sektora i stanja i rečenica trake o sukobu oznaka
	CipoviSektora []CipPopisa
	CipoviStanja  []CipPopisa
	SukobNaPopisu string

	// obrazac
	Station    *models.Station
	Stanice    []models.Station
	Dionice    []models.Section
	Zadnje     *models.Reading
	Ocitanja   []models.Reading // za izbor očitanja na koje se akt poziva
	ZaPrekid   []models.Akt     // ovjereni akti o uspostavi po vodomjeru, za prekid
	Tendencije []struct{ Kod, Naziv string }
	LocalValue string
	// VrijediUpisan: „Vrijedi od” je došao s adresom (povratak s pitanjem ili
	// promjena letve), pa ga i nova letva nosi dalje
	VrijediUpisan bool
	Radnja        string
	Stupanj       models.DefensePhase
	// TraziPotvrduVrijedi: nacrt je odbijen jer „Vrijedi od” traži izričitu
	// potvrdu (Odluke (8. 10. 2026.), pitanje 17); obrazac je nudi
	TraziPotvrduVrijedi bool
	// Unos je ono što je osoba upisala prije pitanja; obrazac ga zadrži
	Unos UnosObrasca

	// jedan akt
	Sektor              *models.Sector
	Podrucje            *models.Area
	SmijeOvjeriti       bool
	SmijeObrisati       bool
	SmijeObrisatiTrajno bool   // ovjeren akt, uz uključenu opciju
	SmijePonistiti      bool   // ovjeren akt: autor ili tko ga smije ovjeriti
	Potpis              string // stanje elektroničkog potpisa: VRIJEDI, NE_VRIJEDI, NEMA
	SmijePripremiti     bool
	// SmijeUreditiNacrt: ispraviti tekst i ponovno složiti primatelje smije
	// samo autor nacrta dok smije sastavljati (pitanje 18)
	SmijeUreditiNacrt bool
	MoguPotpisati     []models.User // za izbor potpisnika uz sken
	Slanje            *SlanjeData   // slanje izvornika primateljima "na znanje"
	Upozorenja        []string

	// StornoOdbijen je poruka odbijenog poništenja, RazlogStorna razlog koji
	// je osoba upisala, a KasnijiAkt akt koji treba najprije poništiti (crtež 4h)
	StornoOdbijen string
	RazlogStorna  string
	KasnijiAkt    *models.Akt

	// Sukobi oznaka (Odluke (8. 10. 2026.), pitanje 4): u popisu po ID-u
	// akta, a na stranici akta njegov sukob i smije li ga osoba riješiti
	Sukobi              map[string]models.SukobOznake
	Sukob               models.SukobOznake
	SmijeRijesitiOznaku bool
	// PotpisiOvjera su stanja elektroničkog potpisa drugih ovjera akta
	// (isti nacrt ovjeren i na drugom čvoru), po kodu ovjere
	PotpisiOvjera map[string]string
	// Pregled je popis primatelja nacrta složen iznova, kakav bi akt dobio
	// ovjerom (Odluke (8. 10. 2026.), pitanje 19); nil za ovjeren akt i za
	// onoga tko s nacrtom ne radi
	Pregled *service.PregledPrimatelja
	// PitanjeVrijedi je pitanje uz „Vrijedi od” izvan uobičajenog raspona
	// koje ovjera nacrta postavlja (pitanje 17); prazno kad potvrda ne treba
	PitanjeVrijedi string

	// špranca
	Spranca models.Spranca
	Zadana  models.Spranca

	// registar primatelja
	Primatelji   []models.Primatelj
	Skupine      []string
	SektorID     string
	SmijeUrediti bool

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

func (h *AktiHandler) base(r *http.Request) (*models.User, *models.UserPermissions, AktiPageData) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	q := r.URL.Query()
	data := AktiPageData{CurrentUser: u, Permissions: perms, SuccessMessage: q.Get("success"), ErrorMessage: q.Get("error"),
		ActiveNav: "journals", ViewAsBanner: viewBanner(r), Stupnjevi: models.StupnjeviAkta, Skupine: models.SkupinePrimatelja,
		SmijeSastavljati: service.SastavljaNacrte(perms)}
	data.Sektori, _ = h.users.ListSectors()
	if s := h.akti(); s != nil && perms != nil {
		for _, sk := range data.Sektori {
			if sk.ID == "DIREKCIJA" || !(perms.IsGlobalAdmin || perms.AllowedSectors[sk.ID] || perms.AdminSectors[sk.ID] || sektorPodrucja(perms, sk.ID)) {
				continue
			}
			data.Obrana = append(data.Obrana, ObranaSektora{Sektor: sk.ID, Centar: sk.CenterCop, Dnevnik: s.AktivnaObrana(r.Context(), sk.ID)})
		}
	}
	return u, perms, data
}

func (h *AktiHandler) svc(w http.ResponseWriter) *service.AktService {
	s := h.akti()
	if s == nil {
		http.Error(w, "akti nisu dostupni", http.StatusServiceUnavailable)
	}
	return s
}

// ShowPopis je popis akata s filtrima i pretragom
func (h *AktiHandler) ShowPopis(w http.ResponseWriter, r *http.Request) {
	_, perms, data := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	q := r.URL.Query()
	f := repository.FiltarAkata{Sektor: q.Get("sektor"), Radnja: q.Get("radnja"), Status: q.Get("status"), Trazi: strings.TrimSpace(q.Get("q")), Limit: 500}
	f.AreaID, _ = strconv.Atoi(q.Get("podrucje"))
	f.Godina, _ = strconv.Atoi(q.Get("godina"))
	f.Stupanj = models.DefensePhase(q.Get("stupanj"))
	if f.Sektor == "" && jedanSektorRada(perms) {
		f.Sektor = perms.SektoriRada()[0]
	}
	data.Filtar = f
	data.Podrucja, _ = h.users.ListAreas(f.Sektor)
	akti, err := s.List(r.Context(), perms, f)
	if err != nil {
		data.ErrorMessage = err.Error()
	}
	data.Akti = akti
	h.sukobiOznaka(r.Context(), s, &data)
	izborPopisa(&data, perms, q)
	g := time.Now().In(models.Zagreb).Year()
	for y := g; y >= g-5; y-- {
		data.Godine = append(data.Godine, y)
	}
	if err := h.tmplPopis.ExecuteTemplate(w, "akti.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// sukobiOznaka upisuje u stranicu sukobe oznaka akata; kad se ne daju
// provjeriti, to piše na stranici
func (h *AktiHandler) sukobiOznaka(ctx context.Context, s *service.AktService, data *AktiPageData) {
	sukobi, err := s.SukobiOznaka(ctx)
	data.Sukobi = sukobi
	data.ErrorMessage = uzGresku(data.ErrorMessage, "Akti s istom oznakom nisu provjereni", err)
}

// uzGresku dodaje poruci stranice grešku koja nije spriječila prikaz
func uzGresku(poruka, sto string, err error) string {
	if err == nil {
		return poruka
	}
	return strings.TrimSpace(poruka + " " + sto + ": " + err.Error())
}

// ShowForm je obrazac za novi akt: vodomjer, radnja, stupanj, vrijeme.
// Otvara se samo onome tko sastavlja nacrte (Odluke (8. 10. 2026.), pitanje
// 18); drugoga vraća na popis akata s objašnjenjem.
func (h *AktiHandler) ShowForm(w http.ResponseWriter, r *http.Request) {
	_, perms, data := h.base(r)
	if h.svc(w) == nil {
		return
	}
	if !data.SmijeSastavljati {
		redirectWith(w, r, "/akti", "error", "Nacrt akta o stupnju obrane sastavlja Centar obrane od poplava: voditelj centra, njegov zamjenik ili dežurni operater.")
		return
	}
	q := r.URL.Query()
	obrazacRadnje(q, &data)
	obrazacVrijedi(q, &data)
	data.Unos = unosObrasca(q)
	h.letvaObrasca(r.Context(), q.Get("station"), &data)
	preventivnaObrana(r.Context(), h.akti(), perms, &data)
	data.Stanice = h.letveZaAkt(r.Context(), perms, data.Stupanj, data.Station)
	if err := h.tmplForm.ExecuteTemplate(w, "akt_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// preventivnaObrana: kad je sektor letve u preventivnoj obrani, obrazac akta
// se ne nudi, nego poruka i gumb „Otvori dnevnik COP-a” onome tko ga smije
// otvoriti (Odluke (6. 11. 2026.)). Sektor se čita iz dionica letve.
func preventivnaObrana(ctx context.Context, s *service.AktService, perms *models.UserPermissions, data *AktiPageData) {
	if data.Station == nil || len(data.Dionice) == 0 {
		return
	}
	if sektor := data.Dionice[0].SectorID; s.UPreventivnoj(ctx, sektor) {
		data.Preventivna, data.OtvaraDnevnik = sektor, service.OtvaraDnevnikCOP(perms, sektor)
	}
}

// obrazacRadnje upisuje u obrazac radnju i stupanj iz adrese; zadano je
// uspostava pripremnog stanja
func obrazacRadnje(q url.Values, data *AktiPageData) {
	data.Radnja = q.Get("radnja")
	if data.Radnja == "" {
		data.Radnja = models.AktUspostava
	}
	data.Stupanj = models.DefensePhase(q.Get("stupanj"))
	if data.Stupanj == "" {
		data.Stupanj = models.PhasePrep
	}
}

// letvaObrasca učitava izabranu letvu i sve što obrazac o njoj pokazuje:
// dionice, zadnje očitanje, očitanja za izbor i akte za prekid
func (h *AktiHandler) letvaObrasca(ctx context.Context, id string, data *AktiPageData) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return
	}
	st, err := h.stations.GetStation(ctx, uid)
	if err != nil || st == nil {
		return
	}
	data.Station = st
	data.Dionice = h.dioniceLetve(ctx, st)
	s := h.akti()
	if s == nil {
		return
	}
	if zadnja, err := s.ZadnjeOcitanje(ctx, st.ID.String()); err == nil {
		data.Zadnje = zadnja
	}
	data.Ocitanja, _ = s.OcitanjaZaAkt(ctx, st.ID.String(), 200) // bez njih obrazac kaže da se akt donosi po prognozi, kao i prije
	data.ZaPrekid, _ = s.AktiZaPrekid(ctx, st.ID.String(), "")   // bez njih prekid ide na zadnji ovjereni akt, kao i prije
	data.Tendencije = models.Tendencije
}

// letveZaAkt su letve po kojima bi priprema prihvatila akt (ista provjera).
// Izabrana letva je u popisu i kad nije među njima, da je izbor pokaže;
// nacrt po njoj priprema i dalje odbije s objašnjenjem.
func (h *AktiHandler) letveZaAkt(ctx context.Context, perms *models.UserPermissions, stupanj models.DefensePhase, izabrana *models.Station) []models.Station {
	sve, _ := h.stations.ListStations(ctx, "", "", "", false) // bez popisa ostaje izabrana letva, a bez nje izbor je prazan, kao i prije
	var out []models.Station
	ima := izabrana == nil
	for _, st := range sve {
		if len(st.SectionCodes) > 0 && h.akti().SmijeAktZaLetvu(ctx, perms, &st, stupanj) {
			out = append(out, st)
			ima = ima || st.ID == izabrana.ID
		}
	}
	if !ima {
		out = append([]models.Station{*izabrana}, out...)
	}
	return out
}

// ZadnjiVodostaj je zadnje očitanje letve s vodostajem, za karticu letve;
// nil kad ga letva nema
func (d AktiPageData) ZadnjiVodostaj() *models.Reading {
	if d.Zadnje != nil && d.Zadnje.LevelCm != nil {
		return d.Zadnje
	}
	if len(d.Ocitanja) > 0 {
		return &d.Ocitanja[0]
	}
	return nil
}

// PodrucjaLetve su branjena područja dionica letve, svako jednom, redom
// kojim dionice dolaze
func (d AktiPageData) PodrucjaLetve() []models.Area {
	var out []models.Area
	for _, sec := range d.Dionice {
		if !slices.ContainsFunc(out, func(a models.Area) bool { return a.ID == sec.AreaID }) {
			out = append(out, models.Area{ID: sec.AreaID, SectorID: sec.SectorID, Name: sec.AreaName})
		}
	}
	return out
}

// PodrucjeIzbora je branjeno područje letve u izboru na obrascu akta (crtež
// „Novi akt · letva za dva BP-a”): naziv, oznaka akta o uspostavi koji u
// njemu po letvi već vrijedi i je li izabrano
type PodrucjeIzbora struct {
	models.Area
	NaSnazi string
	Izabran bool
}

// PodrucjaIzbora su područja letve za izbor na obrascu. Izabrano je ono iz
// unosa, inače prvo bez akta na snazi, inače prvo (Odluke (9. 11. 2026.):
// akt je uvijek za jedno područje).
func (d AktiPageData) PodrucjaIzbora() []PodrucjeIzbora {
	var out []PodrucjeIzbora
	for _, p := range d.PodrucjaLetve() {
		x := PodrucjeIzbora{Area: p}
		if i := slices.IndexFunc(d.ZaPrekid, func(a models.Akt) bool { return a.AreaID == p.ID }); i >= 0 {
			x.NaSnazi = d.ZaPrekid[i].Oznaka()
		}
		out = append(out, x)
	}
	izabrano := slices.IndexFunc(out, func(x PodrucjeIzbora) bool { return x.ID == d.Unos.Podrucje })
	if izabrano < 0 {
		izabrano = max(0, slices.IndexFunc(out, func(x PodrucjeIzbora) bool { return x.NaSnazi == "" }))
	}
	if len(out) > 0 {
		out[izabrano].Izabran = true
	}
	return out
}

// IzabranoPodrucje je broj izabranog područja na obrascu; 0 bez letve
func (d AktiPageData) IzabranoPodrucje() int {
	for _, p := range d.PodrucjaIzbora() {
		if p.Izabran {
			return p.ID
		}
	}
	return 0
}

// PodrucjaNaSnazi je rečenica uz izbor područja: u kojem području po letvi
// već vrijedi akt o uspostavi, npr. „Za BP 34 već vrijedi B-7/2026.”
func (d AktiPageData) PodrucjaNaSnazi() string {
	var dijelovi []string
	for _, p := range d.PodrucjaIzbora() {
		if p.NaSnazi != "" {
			dijelovi = append(dijelovi, "Za "+models.Terms().Get("bp")+" "+strconv.Itoa(p.ID)+" već vrijedi "+p.NaSnazi+".")
		}
	}
	return strings.Join(dijelovi, " ")
}

// OvjeraStupnja je rečenica „Tko ovjerava” za jedan stupanj obrane
type OvjeraStupnja struct {
	Stupanj models.DefensePhase
	Tekst   string
}

// UpozoriNaOznaku: akt ima istu oznaku kao drugi akt i sukob nije riješen.
// Slati se smije, ali uz upozorenje u kartici Slanje (crtež 4c, Odluke
// (6. 11. 2026.)). Isti nacrt ovjeren na dva čvora ovdje se ne broji.
func (d AktiPageData) UpozoriNaOznaku() bool {
	return len(d.Sukob.Drugi) > 0 && !d.Sukob.Rijesen()
}

// JosNeVrijedi javlja je li ovjeren akt još prije trenutka u kojem stupa na
// snagu: dotad stanje dionica ostaje kakvo jest, a slati se smije (crtež 4d)
func (d AktiPageData) JosNeVrijedi() bool {
	a := d.Akt
	return a != nil && a.Ovjeren() && !a.Storniran() && a.Vrijedi.After(time.Now())
}

// OvjeravaAkt je rečenica „Tko ovjerava” za stupanj i sektor ovog akta, u
// kartici Ovjera na stranici nacrta
func (d AktiPageData) OvjeravaAkt() string {
	if d.Akt == nil {
		return ""
	}
	return service.TkoOvjerava(d.Akt.Stupanj, d.Akt.Sektor)
}

// TkoOvjerava su rečenice „Tko ovjerava” za svaki stupanj koji obrazac
// nudi, u sektoru akta (sektor prve dionice letve, kao i u pripremi akta).
// Obrazac pokazuje onu za izabrani stupanj.
func (d AktiPageData) TkoOvjerava() []OvjeraStupnja {
	sektor := ""
	if len(d.Dionice) > 0 {
		sektor = d.Dionice[0].SectorID
	}
	out := make([]OvjeraStupnja, 0, len(d.Stupnjevi))
	for _, s := range d.Stupnjevi {
		out = append(out, OvjeraStupnja{Stupanj: s, Tekst: service.TkoOvjerava(s, sektor)})
	}
	return out
}

// obrazacVrijedi upisuje u obrazac „Vrijedi od”: sada, ili vrijeme upisano
// prije, kad se obrazac vraća s pitanjem za potvrdu ili kad je osoba
// promijenila letvu
func obrazacVrijedi(q url.Values, data *AktiPageData) {
	data.LocalValue = time.Now().In(models.Zagreb).Format("2006-01-02T15:04")
	if _, err := time.ParseInLocation("2006-01-02T15:04", q.Get("vrijedi"), models.Zagreb); err == nil {
		data.LocalValue = q.Get("vrijedi")
		data.VrijediUpisan = true
	}
	data.TraziPotvrduVrijedi = q.Get("potvrdi") == "vrijedi"
}

// UnosObrasca je ono što je osoba upisala u obrazac novog akta. Prognoza i
// napomena nose se i kad osoba promijeni letvu. Kad se obrazac vraća s
// pitanjem za potvrdu „Vrijedi od”, zadrži se sav unos, pa nacrt uz potvrdu
// nastaje iz istog unosa (Odluke (8. 10. 2026.), pitanje 17); dionice,
// očitanje i akt za prekid pripadaju letvi, pa se uz drugu letvu slože iznova.
type UnosObrasca struct {
	Zadrzan      bool
	Podrucje     int
	Dionice      []string
	OcitanjeID   string
	Tendencija   string
	PrekidaAktID string
	Prognoza     string
	Napomena     string
}

// unosObrasca čita unos iz adrese: prognozu i napomenu uvijek, a ostalo
// samo iz povratka na obrazac s pitanjem
func unosObrasca(q url.Values) UnosObrasca {
	u := UnosObrasca{Prognoza: q.Get("prognoza"), Napomena: q.Get("napomena"), PrekidaAktID: q.Get("prekida_akt_id")}
	// područje i akt za prekid nosi i novi akt umjesto poništenog
	u.Podrucje, _ = strconv.Atoi(q.Get("podrucje")) // bez broja područje se bira iznova
	if q.Get("potvrdi") != "vrijedi" {
		return u
	}
	u.Zadrzan, u.Dionice, u.OcitanjeID = true, q["dionica"], q.Get("ocitanje_id")
	u.Tendencija = q.Get("tendencija")
	return u
}

// ImaDionicu javlja je li dionica označena; bez zadržanog unosa jesu sve
func (u UnosObrasca) ImaDionicu(kod string) bool {
	return !u.Zadrzan || slices.Contains(u.Dionice, kod)
}

// Ocitanje javlja je li očitanje odabrano; bez zadržanog izbora odabrano je
// zadnje, prvo na popisu
func (u UnosObrasca) Ocitanje(id string, zadnje bool) bool {
	if u.OcitanjeID == "" {
		return zadnje
	}
	return u.OcitanjeID == id
}

// natragNaObrazac je povratak na obrazac nakon odbijenog nacrta: kad nacrt
// traži potvrdu „Vrijedi od”, obrazac zadrži upisano vrijeme i sav ostali
// unos (unosObrasca) i ponudi potvrdu
func natragNaObrazac(natrag string, r *http.Request, err error) string {
	var p *service.TrebaPotvrduError
	if !errors.As(err, &p) {
		return natrag
	}
	q := url.Values{"vrijedi": {r.FormValue("vrijedi")}, "potvrdi": {"vrijedi"}, "dionica": r.Form["dionica"]}
	for _, k := range []string{"podrucje", "ocitanje_id", "tendencija", "prekida_akt_id", "prognoza", "napomena"} {
		q.Set(k, r.FormValue(k))
	}
	return natrag + "&" + q.Encode()
}

// NoviUmjestoPonistenog je adresa obrasca za akt koji ispravlja poništen akt:
// ista letva, radnja, stupanj, područje i akt za prekid, a u napomeni piše
// koji akt zamjenjuje (Odluke (9. 11. 2026.))
func (d AktiPageData) NoviUmjestoPonistenog() string {
	a := d.Akt
	if a == nil || !a.Storniran() {
		return ""
	}
	q := url.Values{"station": {a.StationID}, "radnja": {a.Radnja}, "stupanj": {string(a.Stupanj)},
		"podrucje": {strconv.Itoa(a.AreaID)}, "napomena": {models.RecenicaZamjene(*a)}}
	if a.PrekidaAktID != "" {
		q.Set("prekida_akt_id", a.PrekidaAktID)
	}
	return "/akti/novi?" + q.Encode()
}

// ObranaSektora je način rada sektora za stranice akata: otvoren dnevnik
// COP-a znači aktivnu obranu, bez njega je preventivna
type ObranaSektora struct {
	Sektor  string
	Centar  string
	Dnevnik *models.Journal
}

// Aktivna javlja je li obrana u sektoru aktivna
func (o ObranaSektora) Aktivna() bool { return o.Dnevnik != nil }

// sektorPodrucja javlja ima li osoba zaduženje na nekom području sektora
func sektorPodrucja(perms *models.UserPermissions, sektor string) bool {
	for _, d := range perms.User.Duties {
		if d.IsActive && d.SectorID != nil && *d.SectorID == sektor {
			return true
		}
	}
	return false
}

func (h *AktiHandler) dioniceLetve(ctx context.Context, st *models.Station) []models.Section {
	s := h.akti()
	if s == nil {
		return nil
	}
	return s.DioniceLetve(ctx, st)
}

// HandleCreate sastavlja nacrt iz obrasca i sprema ga
func (h *AktiHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/akti/novi", "error", "Neispravan zahtjev")
		return
	}
	z := service.ZahtjevAkta{
		StationID: r.FormValue("station_id"), Radnja: r.FormValue("radnja"), Stupanj: models.DefensePhase(r.FormValue("stupanj")),
		Prognoza: r.FormValue("prognoza"), Napomena: r.FormValue("napomena"), Dionice: r.Form["dionica"],
		OcitanjeID: r.FormValue("ocitanje_id"), Tendencija: r.FormValue("tendencija"), PrekidaAktID: r.FormValue("prekida_akt_id"),
		PotvrdaVrijedi: r.FormValue("potvrda_vrijedi") == "1",
	}
	z.Podrucje, _ = strconv.Atoi(r.FormValue("podrucje")) // bez broja servis traži izbor kad letva ima više područja
	if t, err := time.ParseInLocation("2006-01-02T15:04", r.FormValue("vrijedi"), models.Zagreb); err == nil {
		z.Vrijedi = t
	}
	natrag := "/akti/novi?station=" + z.StationID + "&radnja=" + z.Radnja + "&stupanj=" + string(z.Stupanj)
	a, err := s.Pripremi(r.Context(), perms, u, z)
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	if err := s.Spremi(r.Context(), perms, a); err != nil {
		redirectWith(w, r, natragNaObrazac(natrag, r, err), "error", err.Error())
		return
	}
	redirectWith(w, r, "/akti/"+a.ID, "success", "Nacrt je sastavljen. Pregledaj ga i ovjeri.")
}

func (h *AktiHandler) ucitaj(w http.ResponseWriter, r *http.Request) (*service.AktService, *models.Akt) {
	s := h.svc(w)
	if s == nil {
		return nil, nil
	}
	a, err := s.Get(r.Context(), r.PathValue("id"))
	// nacrt drugog sektora otvara se kao da ga nema, kao i na popisu
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if err != nil || !service.VidiAkt(perms, a) {
		http.NotFound(w, r)
		return nil, nil
	}
	return s, a
}

// ovjeraIPonistenje javlja nudi li stranica akta ovjeru (nacrta) i
// poništenje (ovjerenog akta). Tuđim očima ne nudi ni jedno: oboje se
// potpisuje na ime osobe.
func ovjeraIPonistenje(r *http.Request, s *service.AktService, perms *models.UserPermissions, u *models.User, a *models.Akt) (ovjeri, ponisti bool) {
	if viewing, _ := r.Context().Value(contextKeyViewing).(bool); viewing {
		return false, false
	}
	return !a.Ovjeren() && s.SmijeOvjeriti(perms, a), s.SmijePonistiti(perms, u, a)
}

// ShowAkt prikazuje akt: tekst kako će stajati na papiru, primatelje i ovjeru
func (h *AktiHandler) ShowAkt(w http.ResponseWriter, r *http.Request) {
	u, perms, data := h.base(r)
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	data.Akt = a
	data.Potpis = service.ProvjeriPotpis(a)
	data.SmijePripremiti = !a.Ovjeren() && s.SmijePripremiti(perms, u, a)
	data.Slanje = h.slanjeZaStranicu(r, s, perms, u, a)
	data.SmijeOvjeriti, data.SmijePonistiti = ovjeraIPonistenje(r, s, perms, u, a)
	if data.SmijeOvjeriti {
		// sken ovjere učitava samo onaj tko akt smije ovjeriti
		data.MoguPotpisati = s.MoguPotpisati(a)
	}
	data.SmijeObrisati = !a.Ovjeren() && u != nil && (a.IzradioID == u.ID.String() || s.SmijeOvjeriti(perms, a))
	data.SmijeUreditiNacrt = !a.Ovjeren() && service.SmijeUreditiNacrt(perms, u, a)
	data.SmijeObrisatiTrajno = s.SmijeObrisatiTrajno(r.Context(), perms, a)
	odbijenoPonistenje(r, s, perms, &data)
	h.sukobAkta(r.Context(), s, perms, a, &data)
	pregledPrimatelja(r.Context(), s, a, &data)
	for i := range data.Sektori {
		if data.Sektori[i].ID == a.Sektor {
			data.Sektor = &data.Sektori[i]
		}
	}
	if areas, err := h.users.ListAreas(a.Sektor); err == nil {
		for i := range areas {
			if areas[i].ID == a.AreaID {
				data.Podrucje = &areas[i]
			}
		}
	}
	if err := h.tmplAkt.ExecuteTemplate(w, "akt.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// odbijenoPonistenje upisuje u stranicu akta poruku odbijenog poništenja,
// upisani razlog i kasniji akt koji treba najprije poništiti, ako ga osoba
// smije vidjeti
func odbijenoPonistenje(r *http.Request, s *service.AktService, perms *models.UserPermissions, data *AktiPageData) {
	q := r.URL.Query()
	data.StornoOdbijen, data.RazlogStorna = q.Get("storno"), q.Get("razlog")
	if id := q.Get("kasniji"); id != "" && data.StornoOdbijen != "" {
		if k, err := s.Get(r.Context(), id); err == nil && service.VidiAkt(perms, k) {
			data.KasnijiAkt = k
		}
	}
}

// sukobAkta upisuje u stranicu akta sukob njegove oznake i smije li ga
// osoba označiti riješenim
func (h *AktiHandler) sukobAkta(ctx context.Context, s *service.AktService, perms *models.UserPermissions, a *models.Akt, data *AktiPageData) {
	h.sukobiOznaka(ctx, s, data)
	data.Sukob = data.Sukobi[a.ID]
	data.SmijeRijesitiOznaku = data.Sukob.Ima() && service.SmijeRijesitiOznaku(perms, a)
	data.PotpisiOvjera = potpisiOvjera(data.Sukob.Ovjere)
}

// potpisiOvjera su stanja elektroničkog potpisa ovjera, po kodu ovjere
func potpisiOvjera(ovjere []models.Akt) map[string]string {
	out := map[string]string{}
	for i := range ovjere {
		out[ovjere[i].OvjeraKod] = service.ProvjeriPotpis(&ovjere[i])
	}
	return out
}

// pregledPrimatelja upisuje u stranicu nacrta ono što ovjera traži da se
// pogleda: popis primatelja složen iznova (razliku prema nacrtu, upozorenja
// i otisak popisa) i pitanje uz „Vrijedi od”
func pregledPrimatelja(ctx context.Context, s *service.AktService, a *models.Akt, data *AktiPageData) {
	if a.Ovjeren() || !(data.SmijeOvjeriti || data.SmijePripremiti) {
		return
	}
	p := s.PregledPrimatelja(ctx, a)
	data.Pregled = &p
	data.PitanjeVrijedi = service.PitanjeVrijediZaOvjeru(*a, time.Now())
}

// potvrdaOvjere su potvrde iz obrasca ovjere: otisak popisa primatelja koji
// je osoba vidjela, kvačica uz upozorenje o popisu i kvačica uz „Vrijedi
// od” izvan uobičajenog raspona
func potvrdaOvjere(r *http.Request) service.PotvrdaOvjere {
	return service.PotvrdaOvjere{Otisak: r.FormValue("otisak"), Primatelji: r.FormValue("potvrda_primatelja") == "1",
		Vrijedi: r.FormValue("potvrda_vrijedi") == "1"}
}

// ovjeriSaStranice ovjerava nacrt u goCOP-u sa stranice akta, koja pokazuje
// popis primatelja složen iznova: bez otiska tog popisa (osoba ga nije
// vidjela) ovjera se ne izvodi (Odluke (8. 10. 2026.), pitanje 19)
func ovjeriSaStranice(r *http.Request, s *service.AktService, perms *models.UserPermissions, u *models.User, a *models.Akt) (*models.Akt, []string, error) {
	p := potvrdaOvjere(r)
	if p.Otisak == "" && !a.Ovjeren() {
		return nil, nil, errors.New("prije ovjere otvorite stranicu akta i pregledajte popis primatelja koji se slaže pri ovjeri")
	}
	return s.Ovjeri(r.Context(), perms, u, a.ID, p)
}

// HandleSloziPrimatelje slaže popis primatelja nacrta iznova
func (h *AktiHandler) HandleSloziPrimatelje(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	novi, err := s.PonovnoSloziPrimatelje(r.Context(), perms, u, a.ID)
	if err != nil {
		redirectWith(w, r, "/akti/"+a.ID, "error", err.Error())
		return
	}
	poruka := "Popis primatelja ponovno je složen."
	if len(novi.UpozorenjaPrimatelja) > 0 {
		poruka += " Upozorenje uz popis i dalje stoji; pogledajte ga prije ovjere."
	}
	redirectWith(w, r, "/akti/"+a.ID+"#primatelji", "success", poruka)
}

// HandleRijesiOznaku označava sukob oznake akta riješenim, s napomenom
func (h *AktiHandler) HandleRijesiOznaku(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	if err := s.RijesiOznaku(r.Context(), perms, u, a.ID, r.FormValue("napomena")); err != nil {
		redirectWith(w, r, "/akti/"+a.ID, "error", err.Error())
		return
	}
	redirectWith(w, r, "/akti/"+a.ID, "success", "Sukob oznake "+a.Oznaka()+" označen je riješenim; rješenje vide svi čvorovi.")
}

// HandleTekst sprema ispravljeni tekst nacrta
func (h *AktiHandler) HandleTekst(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/akti/"+a.ID+"#uredi", "error", "Neispravan zahtjev")
		return
	}
	// greška vraća na panel ispravka teksta, a uspjeh na stranicu akta
	if _, err := s.UrediTekst(r.Context(), perms, u, a.ID, r.FormValue("uvod"), r.FormValue("izvan_snage"), r.FormValue("zavrsno"), r.FormValue("napomena")); err != nil {
		redirectWith(w, r, "/akti/"+a.ID+"#uredi", "error", err.Error())
		return
	}
	redirectWith(w, r, "/akti/"+a.ID, "success", "Tekst nacrta je spremljen.")
}

// ShowSpranca prikazuje šprancu sektora za uređivanje
func (h *AktiHandler) ShowSpranca(w http.ResponseWriter, r *http.Request) {
	_, perms, data := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	data.SektorID = r.URL.Query().Get("sektor")
	if data.SektorID == "" && perms != nil {
		if moji := perms.SektoriRada(); len(moji) > 0 {
			data.SektorID = moji[0]
		}
		if data.SektorID == "" && len(data.Sektori) > 0 {
			data.SektorID = data.Sektori[0].ID
		}
	}
	data.Spranca, _ = s.Spranca(r.Context(), data.SektorID)
	data.Zadana = models.ZadanaSpranca(data.SektorID)
	data.SmijeUrediti = perms != nil && perms.CanAdminister(data.SektorID, 0)
	data.CipoviSektora = cipoviJednogSektora("/akti/spranca", data.Sektori, data.SektorID)
	if err := h.tmplSpranca.ExecuteTemplate(w, "spranca.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleStorno poništava ovjeren akt (storno), uz razlog: akt ostaje u
// popisu, označen, a stanje i povijest obrane računaju se bez njega
func (h *AktiHandler) HandleStorno(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	razlog := r.FormValue("razlog")
	a, upozorenja, err := s.Storniraj(r.Context(), perms, u, a.ID, razlog)
	if err != nil {
		redirectWith(w, r, natragNaStorno(r.PathValue("id"), razlog, err), "storno", "Akt nije poništen: "+err.Error())
		return
	}
	poruka := "Akt " + a.Oznaka() + " je poništen."
	if len(upozorenja) > 0 {
		poruka += " Povijest obrane: " + strings.Join(upozorenja, "; ")
	}
	redirectWith(w, r, "/akti/"+a.ID, "success", poruka)
}

// natragNaStorno je adresa na koju se vraća odbijeno poništenje (crtež 4h):
// panel ostaje otvoren s upisanim razlogom, a kad je na putu kasniji akt,
// stranica nudi da se otvori on
func natragNaStorno(id, razlog string, err error) string {
	q := url.Values{}
	if razlog = strings.TrimSpace(razlog); razlog != "" {
		q.Set("razlog", razlog)
	}
	if k := (*service.KasnijiAktError)(nil); errors.As(err, &k) {
		q.Set("kasniji", k.Kasniji.ID)
	}
	natrag := "/akti/" + id
	if len(q) > 0 {
		natrag += "?" + q.Encode()
	}
	return natrag + "#ponisti"
}

// HandleSpranca sprema šprancu sektora; "zadano" vraća zadani tekst
func (h *AktiHandler) HandleSpranca(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/akti/spranca", "error", "Neispravan zahtjev")
		return
	}
	sektor := r.FormValue("sektor")
	natrag := "/akti/spranca?sektor=" + sektor
	sp := models.ZadanaSpranca(sektor)
	if r.FormValue("zadano") == "" {
		sp.Osnova, sp.Zavrsno, sp.Poveznice = r.FormValue("osnova"), r.FormValue("zavrsno"), strings.TrimSpace(r.FormValue("poveznice"))
		for _, p := range models.StupnjeviAkta {
			if c := strings.TrimSpace(r.FormValue("clanak_" + string(p))); c != "" {
				sp.Clanci[p] = c
			}
		}
	}
	if err := s.SpremiSprancu(r.Context(), perms, u, &sp); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Špranca je spremljena; vrijedi za nove nacrte.")
}

// HandleOvjeri ovjerava nacrt
func (h *AktiHandler) HandleOvjeri(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	a, upozorenja, err := ovjeriSaStranice(r, s, perms, u, a)
	if err != nil {
		redirectWith(w, r, "/akti/"+r.PathValue("id"), "error", err.Error())
		return
	}
	// PDF s otiskom žiga i skeniranog potpisa postaje izvornik i dijeli se
	sek, area := h.sektorIPodrucje(a)
	if err := s.SpremiIzvornikPDF(r.Context(), a, PDFAktaSaZigom(a, models.Terms(), sek, area, s.OtisciAkta(r.Context(), a))); err != nil {
		redirectWith(w, r, "/akti/"+a.ID, "error", "Akt je ovjeren, ali izvornik nije spremljen: "+err.Error())
		return
	}
	poruka := "Akt " + a.Oznaka() + " je ovjeren."
	if len(upozorenja) > 0 {
		poruka += " Stanje obrane na dionicama: " + strings.Join(upozorenja, "; ")
	}
	redirectWith(w, r, "/akti/"+a.ID, "success", poruka)
}

// HandleObrisi briše nacrt
func (h *AktiHandler) HandleObrisi(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	if a.Ovjeren() {
		if err := s.ObrisiAktTrajno(r.Context(), perms, a.ID); err != nil {
			redirectWith(w, r, "/akti/"+a.ID, "error", err.Error())
			return
		}
		redirectWith(w, r, "/akti", "success", "Akt "+a.Oznaka()+" je trajno obrisan, s izvornikom i dnevnikom slanja. Stanje obrane na dionicama provjerite i po potrebi zaključite ručno.")
		return
	}
	if err := s.Obrisi(r.Context(), perms, u, a.ID); err != nil {
		redirectWith(w, r, "/akti/"+a.ID, "error", err.Error())
		return
	}
	redirectWith(w, r, "/akti", "success", "Nacrt je obrisan.")
}

// IzvoziPDF daje akt kao PDF
func (h *AktiHandler) IzvoziPDF(w http.ResponseWriter, r *http.Request) {
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	// ovjeren akt ima izvornik: PDF spremljen pri ovjeri ili sken s potpisom
	// i žigom; služi se bajt za bajt, isti na svakom čvoru
	if a.Ovjeren() {
		if pdf, err := s.Izvornik(r.Context(), a.ID); err == nil && pdf != nil {
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", `inline; filename="`+imeDatotekeAkta(a)+`"`)
			_, _ = w.Write(pdf)
			return
		}
	}
	// bez izvornika akt se crta iznova, sa žigom, ali bez skena potpisa
	sek, area := h.sektorIPodrucje(a)
	ime := imeDatotekeAkta(a)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="`+ime+`"`)
	_, _ = w.Write(PDFAktaSaZigom(a, models.Terms(), sek, area, s.OtisciIspisa(r.Context(), a)))
}

// IzvoziZaIspis daje PDF nacrta za ispis, vlastoručni potpis i žig
func (h *AktiHandler) IzvoziZaIspis(w http.ResponseWriter, r *http.Request) {
	_, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	sek, area := h.sektorIPodrucje(a)
	ime := strings.TrimSuffix(strings.TrimSuffix(imeDatotekeAkta(a), ".pdf"), "-nacrt")
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="`+ime+`-za-ispis.pdf"`)
	_, _ = w.Write(PDFAktaZaIspis(a, models.Terms(), sek, area))
}

// HandleUcitajSken prima sken ispisa potpisanog vlastoručno i ovjerenog žigom
func (h *AktiHandler) HandleUcitajSken(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	// greška vraća na panel ispisa i skena, a ovjera na stranicu akta
	natrag := "/akti/" + a.ID + "#ispis"
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		redirectWith(w, r, natrag, "error", "Odaberite sken potpisanog akta")
		return
	}
	f, _, err := r.FormFile("sken")
	if err != nil {
		redirectWith(w, r, natrag, "error", "Odaberite sken potpisanog akta")
		return
	}
	defer f.Close()
	podaci, err := procitajDatoteku(f, 32<<20)
	if err != nil {
		redirectWith(w, r, natrag, "error", "Sken nije čitljiv: "+err.Error())
		return
	}
	a, upozorenja, err := s.UcitajSkenirani(r.Context(), perms, u, a.ID, podaci, r.FormValue("potpisnik_id"), potvrdaOvjere(r))
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	poruka := "Akt " + a.Oznaka() + " je ovjeren: sken s potpisom i žigom (" + a.Rucno.Potpisnik + ") je izvornik."
	if len(upozorenja) > 0 {
		poruka += " Stanje obrane na dionicama: " + strings.Join(upozorenja, "; ")
	}
	redirectWith(w, r, "/akti/"+a.ID, "success", poruka)
}

// sektorIPodrucje su podaci sektora i branjenog područja za zaglavlje akta
func (h *AktiHandler) sektorIPodrucje(a *models.Akt) (*models.Sector, *models.Area) {
	var sek *models.Sector
	if sektori, err := h.users.ListSectors(); err == nil {
		for i := range sektori {
			if sektori[i].ID == a.Sektor {
				sek = &sektori[i]
			}
		}
	}
	var area *models.Area
	if areas, err := h.users.ListAreas(a.Sektor); err == nil {
		for i := range areas {
			if areas[i].ID == a.AreaID {
				area = &areas[i]
			}
		}
	}
	return sek, area
}

// imeDatotekeAkta je naziv PDF-a po uzoru na dosadašnje: vodomjer, radnja,
// stupanj, datum
func imeDatotekeAkta(a *models.Akt) string {
	stupanj := map[models.DefensePhase]string{models.PhasePrep: "PS", models.PhaseRegular: "RO", models.PhaseEmergency: "IO", models.PhaseState: "IS"}[a.Stupanj]
	radnja := "uspostava"
	if a.Radnja == models.AktPrekid {
		radnja = "prekid"
	}
	ime := a.StationName + "_" + radnja + "_" + stupanj + "_" + a.Vrijedi.In(models.Zagreb).Format("2006_01_02")
	if !a.Ovjeren() {
		ime += "_nacrt"
	}
	return sigurnoIme(ime) + ".pdf"
}

// ---- registar primatelja ----

// ShowPrimatelji prikazuje registar primatelja sektora
func (h *AktiHandler) ShowPrimatelji(w http.ResponseWriter, r *http.Request) {
	_, perms, data := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	data.SektorID = r.URL.Query().Get("sektor")
	if data.SektorID == "" && perms != nil {
		if moji := perms.SektoriRada(); len(moji) > 0 {
			data.SektorID = moji[0]
		}
		if data.SektorID == "" && len(data.Sektori) > 0 {
			data.SektorID = data.Sektori[0].ID
		}
	}
	data.Podrucja, _ = h.users.ListAreas(data.SektorID)
	data.Primatelji, _ = s.Primatelji(r.Context(), data.SektorID)
	data.SmijeUrediti = perms != nil && perms.CanAdminister(data.SektorID, 0)
	data.CipoviSektora = cipoviJednogSektora("/akti/primatelji", data.Sektori, data.SektorID)
	if err := h.tmplPrimatelji.ExecuteTemplate(w, "primatelji.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleSavePrimatelj dodaje ili mijenja primatelja
func (h *AktiHandler) HandleSavePrimatelj(w http.ResponseWriter, r *http.Request) {
	_, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/akti/primatelji", "error", "Neispravan zahtjev")
		return
	}
	p := models.Primatelj{ID: r.FormValue("id"), Sektor: r.FormValue("sektor"), Naziv: r.FormValue("naziv"), Email: strings.TrimSpace(r.FormValue("email")),
		Skupina: r.FormValue("skupina"), OdStupnja: models.DefensePhase(r.FormValue("od_stupnja")), Aktivan: r.FormValue("aktivan") != "0"}
	p.AreaID, _ = strconv.Atoi(r.FormValue("area_id"))
	p.Redoslijed, _ = strconv.Atoi(r.FormValue("redoslijed"))
	natrag := "/akti/primatelji?sektor=" + p.Sektor
	if err := s.SpremiPrimatelja(r.Context(), perms, &p); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Primatelj je upisan.")
}

// HandleObrisiPrimatelja briše primatelja
func (h *AktiHandler) HandleObrisiPrimatelja(w http.ResponseWriter, r *http.Request) {
	_, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	natrag := "/akti/primatelji?sektor=" + r.FormValue("sektor")
	if err := s.ObrisiPrimatelja(r.Context(), perms, r.PathValue("id")); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Primatelj je obrisan.")
}
