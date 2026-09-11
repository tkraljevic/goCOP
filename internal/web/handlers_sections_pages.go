package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"gocop/internal/hydro"
	"gocop/internal/models"
	"gocop/internal/service"
)

// Stranice registra dionica: jedna dionica sa svime što se na nju veže, i
// obrazac. Dionica je središnji zapis programa — na nju se vežu vodomjeri,
// teritorij, objekti, nasipi i ljudi — pa joj puna stranica pripada više
// nego ijednom drugom registru.

// PartView je poddionica s razriješenim vezama za prikaz
type PartView struct {
	models.SectionPart
	Stations    []models.Station
	Territories []models.SectionTerritory
	Criteria    []models.GaugeItem // zapisi iz dokumentacije koji nisu postaje
	Rows        []EmbankmentRow    // nasipi s objektima koji na njima leže, kao u Privitku
	// LetveBlokovi su pragovi, kota nule i ekstremi letvi ove poddionice,
	// povučeni s letve. Stoje ovdje jer letva pripada poddionici; ista letva na
	// dvije poddionice iscrtava se jednom, a drugi put kao kratka uputa.
	LetveBlokovi []LetvaBlok
}

// SectionPageData je stranica jedne dionice ili njezina obrasca
type SectionPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	Section     models.Section
	Parts       []PartView
	Episodes    []models.DefenseEpisode // epizode obrane na ovoj dionici, najnovija prva
	OpenEpisode *models.DefenseEpisode  // obrana koja upravo traje, ako je ima
	Gauge       *models.Station         // letva po kojoj se dionica vodi
	CanEdit     bool
	// PredlozenaSifra je prvi slobodan broj u odabranom području; upisuje se u
	// obrazac unaprijed jer se dionice unose u nizu.
	PredlozenaSifra string

	// obrazac
	Sectors         []models.Sector
	Areas           []models.Area
	IsEdit          bool
	Watercourses    []models.Watercourse
	Stations        []models.Station
	Structures      []models.Structure // objekti područja koji nisu nasipi
	Embankments     []models.Structure // nasipi i brane područja
	Counties        []models.County
	StationingKinds []string
	Banks           []struct{ Code, Label string }
	SectionJSON     template.JS // dionica za obrazac
	TerritoryLabels template.JS // ključ → naziv, za čipove u obrascu

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// SetStructureService daje rukovatelju registar objekata
func (h *SectionsHandler) SetStructureService(structures *service.StructureService) {
	h.structureService = structures
}

// SetWatercourseService daje rukovatelju registar voda
func (h *SectionsHandler) SetWatercourseService(waters *service.WatercourseService) {
	h.watercourseService = waters
}

// SetPageTemplates daje rukovatelju predloške stranica i servise koje one trebaju
func (h *SectionsHandler) SetPageTemplates(detail, form *template.Template,
	stations *service.StationService, territories *service.TerritoryService) {
	h.tmplDetail = detail
	h.tmplForm = form
	h.stationService = stations
	h.territoryService = territories
}

func (h *SectionsHandler) pageData(r *http.Request) SectionPageData {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return SectionPageData{
		CurrentUser: currUser, Permissions: perms,
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "sections", ViewAsBanner: viewBanner(r),
		StationingKinds: hydro.StationingKinds, Banks: models.Banks,
	}
}

// ShowSection prikazuje jednu dionicu sa svime što se na nju veže
func (h *SectionsHandler) ShowSection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.pageData(r)

	sec, err := h.sectionService.GetSectionWithDetails(strings.TrimSpace(r.PathValue("code")))
	if err != nil || sec == nil {
		http.NotFound(w, r)
		return
	}
	data.Section = *sec
	data.CanEdit = h.sectionService.CanEditSection(data.Permissions, sec)

	// registri koje poddionice spominju, dohvaćeni jednom
	stationByID := map[string]models.Station{}
	if h.stationService != nil {
		if st, err := h.stationService.GetSectionStations(ctx, sec.Code); err == nil {
			for _, s := range st {
				stationByID[s.ID.String()] = s
			}
		}
	}
	terrByKey := map[string]models.SectionTerritory{}
	if h.territoryService != nil {
		if terr, err := h.territoryService.GetSectionTerritories(ctx, sec.Code); err == nil {
			for _, t := range terr {
				terrByKey[models.PartTerritory{CountyID: t.CountyID, MunicipalityID: t.MunicipalityID, SettlementID: t.SettlementID}.Key()] = t
			}
		}
	}
	for _, p := range sec.Parts {
		v := PartView{SectionPart: p, Rows: embankmentRows(p)}
		for _, id := range p.StationIDs {
			if s, ok := stationByID[id]; ok {
				v.Stations = append(v.Stations, s)
			}
		}
		// naselja uz svoj nasip; ostatak — i sve na dionici bez nasipa —
		// ostaje na poddionici
		v.Territories = razvrstajUgrozeno(v.Rows, p, terrByKey)
		for _, g := range p.Gauges {
			if !g.IsGauge() || !coveredBy(v.Stations, g) {
				v.Criteria = append(v.Criteria, g)
			}
		}
		data.Parts = append(data.Parts, v)
	}

	if h.episodeService != nil {
		data.Episodes, _ = h.episodeService.List(ctx, sec.Code)
		imena := map[string]string{}
		for i := range data.Episodes {
			e := &data.Episodes[i]
			e.DeclaredByName = h.imeDjelatnika(imena, e.DeclaredBy)
			e.EndedByName = h.imeDjelatnika(imena, e.EndedBy)
			if e.IsOpen() && data.OpenEpisode == nil {
				kopija := *e
				data.OpenEpisode = &kopija
			}
		}
	}
	// Obrana se proglašava uz letvu, ondje gdje se vodostaj i čita. Stranica
	// dionice pokazuje obranu koja traje i upućuje na letvu, ali je ne
	// proglašava — jedan te isti stupanj vrijedi za sve dionice koje se po toj
	// letvi vode, pa mu je mjesto ondje, a ne na svakoj dionici posebno.
	napuniLetveBlokove(data.Parts)
	for _, p := range data.Parts {
		if len(p.Stations) > 0 {
			st := p.Stations[0]
			data.Gauge = &st
			break
		}
	}
	if err := h.tmplDetail.ExecuteTemplate(w, "section_detail.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// imeDjelatnika razrješava identifikator u ime, pamteći već potražene da se
// isti čovjek ne dohvaća za svaku epizodu iznova.
func (h *SectionsHandler) imeDjelatnika(cache map[string]string, id string) string {
	if id == "" || h.userService == nil {
		return ""
	}
	if ime, ok := cache[id]; ok {
		return ime
	}
	ime := ""
	if uid, err := uuid.Parse(id); err == nil {
		if u, err := h.userService.GetUserByID(uid); err == nil && u != nil {
			ime = u.FullName
		}
	}
	cache[id] = ime
	return ime
}

// coveredBy javlja je li vodomjer iz dokumentacije već prikazan kao postaja
func coveredBy(stations []models.Station, g models.GaugeItem) bool {
	name, _ := hydro.ParseStationName(g.StationName)
	key := hydro.StationKey(name)
	if key == "" {
		key = hydro.StationKey(g.StationName)
	}
	for _, s := range stations {
		if hydro.StationKey(s.Name) == key || strings.EqualFold(strings.TrimSpace(s.SourceName), strings.TrimSpace(g.StationName)) {
			return true
		}
	}
	return false
}

// ShowSectionForm prikazuje obrazac za novu dionicu ili izmjenu postojeće
// LetvaBlok nosi ono što zajednički predlošci trebaju o jednoj letvi. Imena
// polja su ista kao na kartici letve, pa isti predložak radi s oboje.
type LetvaBlok struct {
	Station     models.Station
	PragoviKote []PragKota

	// Ovo dvoje dolazi iz arhive i na kartici dionice stoji prazno: pragovi se
	// računaju iz same postaje, a krajnosti iz niza traže arhivsku bazu i
	// sažetak po letvi. Polja postoje da zajednički predložak radi s oboje —
	// prazno znači da se taj dio ne iscrtava, a potpuna slika je na kartici
	// letve, kamo i vodi poveznica u zaglavlju.
	KrajnostiIzNiza []models.KrajnostIzNiza
	VisiVrh         *models.VisiVrh

	// Ponovljena znači da je ista letva već iscrtana uz raniju poddionicu, pa
	// se ovdje pokazuje samo uputa.
	Ponovljena bool
}

// napuniLetveBlokove slaže prikaz letvi po poddionicama.
//
// Letva pripada poddionici, pa blok stoji ondje — inače se na dionici s više
// voda ne vidi koja letva vlada kojom. Ista letva na dvije poddionice iscrtava
// se jednom u cijelosti; drugi put samo kao uputa, da se iste brojke ne
// ponavljaju.
//
// Pragovi se računaju iz same postaje i ne traže arhivu. Protok uz prag traži
// krivulju, a krajnosti iz niza arhivu — oboje stoji na kartici letve, kamo i
// vodi poveznica u zaglavlju bloka.
func napuniLetveBlokove(parts []PartView) {
	vidjene := map[string]bool{}
	for i := range parts {
		for _, st := range parts[i].Stations {
			id := st.ID.String()
			b := LetvaBlok{Station: st, PragoviKote: pragoviUKotama(st)}
			if vidjene[id] {
				b.Ponovljena = true
			}
			vidjene[id] = true
			parts[i].LetveBlokovi = append(parts[i].LetveBlokovi, b)
		}
	}
}

// mustAreas vraća područja; prazan popis nije razlog da obrazac ne radi.
func mustAreas(h *SectionsHandler) []models.Area {
	a, _ := h.userService.ListAreas("")
	return a
}

func (h *SectionsHandler) ShowSectionForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.pageData(r)

	if code := strings.TrimSpace(r.PathValue("code")); code != "" {
		sec, err := h.sectionService.GetSectionWithDetails(code)
		if err != nil || sec == nil {
			http.NotFound(w, r)
			return
		}
		if !h.sectionService.CanEditSection(data.Permissions, sec) {
			http.Error(w, "Nemate pravo uređivati ovu dionicu", http.StatusForbidden)
			return
		}
		data.Section = *sec
		data.IsEdit = true
	} else {
		perms := data.Permissions
		canCreate := perms != nil && (perms.IsGlobalAdmin || len(perms.AdminSectors) > 0 ||
			len(perms.AdminAreas) > 0 || len(perms.AllowedSectors) > 0)
		if !canCreate {
			http.Error(w, "Nemate pravo dodavati dionice", http.StatusForbidden)
			return
		}
		data.Section = models.Section{Parts: []models.SectionPart{{Seq: 1}}}
		if a, _ := strconv.Atoi(r.URL.Query().Get("area")); a > 0 {
			data.Section.AreaID = a
		}
		data.Sectors, _ = h.userService.ListSectors()
		if data.Section.AreaID > 0 {
			for _, a := range mustAreas(h) {
				if a.ID == data.Section.AreaID {
					data.Section.SectorID = a.SectorID
					data.PredlozenaSifra = h.sectionService.SljedecaSifra(a.SectorID, a.ID)
					break
				}
			}
		}
	}
	data.Areas, _ = h.userService.ListAreas("")

	if h.watercourseService != nil {
		data.Watercourses, _ = h.watercourseService.ListWatercourses(ctx, "", "", false)
	}
	if h.stationService != nil {
		data.Stations, _ = h.stationService.ListStations(ctx, "", "", false)
	}
	if h.structureService != nil && data.Section.AreaID > 0 {
		if all, err := h.structureService.List(ctx, "", data.Section.AreaID, "", ""); err == nil {
			for _, s := range all {
				if s.Kind == models.StructureKindEmbankment || s.Kind == models.StructureKindDam {
					data.Embankments = append(data.Embankments, s)
				} else {
					data.Structures = append(data.Structures, s)
				}
			}
		}
	}
	if h.territoryService != nil {
		data.Counties, _ = h.territoryService.ListCounties(ctx)
		labels := map[string]string{}
		if data.IsEdit {
			if terr, err := h.territoryService.GetSectionTerritories(ctx, data.Section.Code); err == nil {
				for _, t := range terr {
					key := models.PartTerritory{CountyID: t.CountyID, MunicipalityID: t.MunicipalityID, SettlementID: t.SettlementID}.Key()
					labels[key] = models.TerritoryLabel(t.SettlementName, t.MunicipalityType, t.MunicipalityName, t.CountyName)
				}
			}
		}
		if b, err := json.Marshal(labels); err == nil {
			data.TerritoryLabels = template.JS(b)
		}
	}
	if b, err := json.Marshal(data.Section); err == nil {
		data.SectionJSON = template.JS(b)
	}

	if err := h.tmplForm.ExecuteTemplate(w, "section_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
