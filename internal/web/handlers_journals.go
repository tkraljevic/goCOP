package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/obracun"
	"gocop/internal/service"
)

// JournalsHandler: građevinski dnevnici — popis po području, naslovnica,
// listovi po danu, upisi, nalozi i ispis
type JournalsHandler struct {
	journals      *service.JournalService
	users         *service.UserService
	maintenance   *service.MaintenanceService
	sections      *service.SectionService
	stations      *service.StationService
	tmplIzbor     *template.Template
	tmplCOP       *template.Template
	tmplCOPForm   *template.Template
	tmplList      *template.Template
	tmplForm      *template.Template
	tmplJournal   *template.Template
	tmplSheet     *template.Template
	tmplPrint     *template.Template
	tmplObracun   *template.Template
	tmplDezurstva *template.Template
	tmplIORS      *template.Template
	// obracun daje blagdane i koeficijente iz baze; nil znači ono što program nosi u sebi
	obracun func() *service.ObracunService
}

// SetObracun spaja rukovatelja s postavkama obračuna
func (h *JournalsHandler) SetObracun(f func() *service.ObracunService) { h.obracun = f }

func (h *JournalsHandler) postavkeObracuna() *service.ObracunService {
	if h.obracun == nil {
		return nil
	}
	return h.obracun()
}

func NewJournalsHandler(j *service.JournalService, users *service.UserService, m *service.MaintenanceService,
	sections *service.SectionService, stations *service.StationService,
	izbor, list, form, journal, cop, copForm, sheet, print, obracun, dezurstva, iors *template.Template) *JournalsHandler {
	return &JournalsHandler{journals: j, users: users, maintenance: m, sections: sections, stations: stations,
		tmplIzbor: izbor, tmplCOP: cop, tmplCOPForm: copForm, tmplList: list, tmplForm: form, tmplJournal: journal, tmplSheet: sheet, tmplPrint: print, tmplObracun: obracun, tmplDezurstva: dezurstva, tmplIORS: iors}
}

// JournalPageData su podaci svih stranica dnevnika; što stranica ne treba ostaje prazno
type JournalPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	Areas       []models.Area
	Area        *models.Area
	// Opseg je doseg dnevnika iz kojeg se računaju prava: sektor i njegova
	// područja za COP, jedno područje za uslugu.
	Opseg    models.Opseg
	Journals []models.Journal
	// Vrsta je dnevnik koji se gleda; prazno na razdjelnici.
	Vrsta   string
	BrojCOP int
	// BrojDezurstava je broj dežurstava u svim planovima, za karticu
	BrojDezurstava int
	BrojA02        int
	BrojA03        int
	// Dani su zapisi dežurstva složeni po danima; samo u dnevniku COP-a.
	Dani []DanZapisa
	// Centri i Centar su birač na popisu dnevnika COP-a: dnevnik se vodi po
	// centru, pa se i bira po centru a ne po području.
	Centri []models.Centar
	Centar string
	// CentriZaOtvaranje su centri u kojima osoba smije otvoriti dnevnik:
	// uprava sektora, ne svatko tko piše.
	CentriZaOtvaranje []models.Centar
	Journal           *models.Journal
	Sheets            []models.JournalSheet
	Sheet             *models.JournalSheet
	Entries           []models.JournalEntry
	OpenTasks         []models.JournalEntry
	Gaps              []int
	Kinds             []string
	Locations         []models.MaintainedWater
	WorkItems         []models.WorkItem
	Sections          []models.Section
	Stations          []models.Station
	AllowedKinds      []string
	StaffRoles        []string
	MachineTypes      []string
	ConditionWords    []string
	Ratings           []int
	StaffRows         []models.Count
	MachineRows       []models.Count
	Capacity          int
	Used              int  // izvođačevih upisa na listu
	IsFull            bool // za izvođača: nema mjesta, otvara novi list
	Today             string
	Sada              string // sat i minuta sad, za vrijeme novog zapisa
	// Plan dežurstava uz dnevnik COP-a i obračun sati iz njega
	Dezurstva []models.Dezurstvo
	Osobe     []models.User // koga se može staviti u plan; samo za upravu centra
	// UpravaCentra slaže plan dežurstava; CanManage (nadzor) za to nije dovoljan
	UpravaCentra   bool
	MozeSebe       bool // smije upisati vlastito dežurstvo
	Podrucje       int  // filtar dnevnika COP-a po području; 0 = sve
	ImaDanas       bool // dnevnik COP-a ima zapise za danas, za skok
	OpisiRada      []models.OpisRada
	Obracun        service.Obracun
	IORS           service.IORS
	Razredi        []obracun.Razred
	CanWrite       bool
	CanSupervise   bool
	CanManage      bool
	IsContractor   bool
	IsEdit         bool
	PrintSheets    []PrintSheet
	From, To       string
	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// PrintSheet je list s upisima, za ispis
type PrintSheet struct {
	Sheet   models.JournalSheet
	Entries []models.JournalEntry
}

func (h *JournalsHandler) base(r *http.Request) (*models.User, *models.UserPermissions) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	p, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	return u, p
}

func (h *JournalsHandler) pageData(r *http.Request) JournalPageData {
	u, perms := h.base(r)
	return JournalPageData{
		CurrentUser: u, Permissions: perms, Kinds: models.JournalKindsUsluga,
		StaffRoles: models.StaffRoles, MachineTypes: models.MachineTypes, ConditionWords: models.ConditionWords,
		Ratings: models.Ratings, Today: time.Now().In(models.Zagreb).Format("2006-01-02"),
		Sada:           time.Now().In(models.Zagreb).Format("15:04"),
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "journals", ViewAsBanner: viewBanner(r), IsContractor: service.IsContractor(u),
	}
}

// areaOf vraća područje po broju, ili prvo u kojem osoba ima ovlasti
func (h *JournalsHandler) areaOf(perms *models.UserPermissions, want int) (*models.Area, []models.Area) {
	areas, _ := h.users.ListAreas("")
	if want == 0 && perms != nil {
		for _, a := range areas {
			if perms.AdminAreas[a.ID] || perms.AllowedAreas[a.ID] || perms.AllowedSectors[a.SectorID] || perms.AdminSectors[a.SectorID] {
				want = a.ID
				break
			}
		}
	}
	for i := range areas {
		if areas[i].ID == want {
			return &areas[i], areas
		}
	}
	if want == 0 && len(areas) > 0 {
		return &areas[0], areas
	}
	return nil, areas
}

// loadJournal čita dnevnik iz putanje i njegovo područje; nil kad ga nema
func (h *JournalsHandler) loadJournal(w http.ResponseWriter, r *http.Request) (*models.Journal, *models.Area, bool) {
	j, err := h.journals.GetJournal(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, nil, false
	}
	if j == nil {
		http.NotFound(w, r)
		return nil, nil, false
	}
	_, perms := h.base(r)
	// Dnevnik COP-a vezan je na centar i područja nema — sektorski COP pokriva
	// pet područja i nijedno nije "njegovo". Samo dnevnik usluge mora imati
	// područje, jer se po njemu i vodi.
	if j.CentarSektor != "" {
		area, _ := h.areaOf(perms, 0)
		return j, area, true
	}
	area, _ := h.areaOf(perms, j.AreaID)
	if area == nil {
		http.Error(w, "dnevnik pokazuje na nepoznato područje", http.StatusInternalServerError)
		return nil, nil, false
	}
	return j, area, true
}

// opseg je doseg dnevnika: po centru za COP, po području za uslugu. Popis
// područja treba samo sektorskom COP-u, da zna koja su mu područja.
func (h *JournalsHandler) opseg(j *models.Journal, area *models.Area) models.Opseg {
	if j == nil {
		if area == nil {
			return models.Opseg{}
		}
		return models.OpsegPodrucja(*area)
	}
	sva, _ := h.users.ListAreas("")
	return models.OpsegDnevnika(*j, area, sva)
}

func (h *JournalsHandler) fillRights(d *JournalPageData) {
	d.Opseg = h.opseg(d.Journal, d.Area)
	if d.Opseg.Prazan() {
		return
	}
	d.CanWrite = h.journals.CanWrite(d.Permissions, d.Opseg)
	d.CanSupervise = h.journals.CanSupervise(d.CurrentUser, d.Permissions, d.Opseg)
	d.CanManage = h.journals.CanManage(d.CurrentUser, d.Permissions, d.Opseg)
	d.AllowedKinds = h.journals.AllowedKinds(d.CurrentUser, d.Permissions, d.Opseg, d.Journal)
	// Zaglavlje dnevnika COP-a mijenja uprava centra, ne svatko tko piše.
	if d.Journal != nil && d.Journal.CentarSektor != "" {
		d.CanManage = h.journals.MozeOtvoritiCOP(d.Permissions, d.Journal.CentarSektor)
	}
}

// ShowJournals prikazuje dnevnike područja
// ShowJournalKinds je razdjelnica: tri vrste dnevnika koje se ne miješaju.
//
// Dežurni zapisnik COP-a i dnevnik usluge održavanja nemaju isti sadržaj ni
// istog voditelja, pa ni ne stoje na istom popisu. Prije je /dnevnici odmah
// otvarao popis jednog područja sa svim vrstama pomiješanim.
func (h *JournalsHandler) ShowJournalKinds(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	h.fillRights(&data)
	if broj, err := h.journals.BrojPoVrstama(r.Context()); err == nil {
		data.BrojCOP = broj[models.JournalKindDefense]
		data.BrojDezurstava, _ = h.journals.BrojDezurstava(r.Context())
		data.BrojA02 = broj[models.JournalKindMaintenanceA02]
		data.BrojA03 = broj[models.JournalKindMaintenanceA03]
	}
	h.render(w, h.tmplIzbor, "dnevnici_izbor.html", data)
}

func (h *JournalsHandler) ShowJournals(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	data.Vrsta = r.URL.Query().Get("vrsta")

	// Dnevnici COP-a ne idu preko branjenog područja: vezani su na centar i
	// područje im je prazno, pa ih popis po području nikad ne bi našao.
	// Plan dežurstava je isti popis dnevnika COP-a, samo svaka kartica vodi
	// na plan umjesto na zapisnik.
	if data.Vrsta == models.JournalKindDefense || data.Vrsta == models.PopisDezurstava {
		h.fillRights(&data)
		data.Centri, _ = h.journals.CentriSDnevnicima(r.Context())
		data.Centar = r.URL.Query().Get("centar")
		data.CentriZaOtvaranje = h.centriZaOtvaranje(data.Permissions)
		js, err := h.journals.ListCOPJournals(r.Context(), data.Centar)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data.Journals = js
		h.render(w, h.tmplList, "dnevnici.html", data)
		return
	}

	want, _ := strconv.Atoi(r.URL.Query().Get("area"))
	data.Area, data.Areas = h.areaOf(data.Permissions, want)
	h.fillRights(&data)
	if data.Area != nil {
		js, err := h.journals.ListJournals(r.Context(), data.Area.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Popis je po vrsti: vrste se ne miješaju jer nemaju isti sadržaj.
		if data.Vrsta != "" {
			var samo []models.Journal
			for _, j := range js {
				if j.Kind == data.Vrsta {
					samo = append(samo, j)
				}
			}
			js = samo
		}
		data.Journals = js
	}
	h.render(w, h.tmplList, "dnevnici.html", data)
}

func (h *JournalsHandler) render(w http.ResponseWriter, t *template.Template, name string, data JournalPageData) {
	if err := t.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowJournalForm prikazuje naslovnicu za novi dnevnik ili izmjenu
func (h *JournalsHandler) ShowJournalForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if id := r.PathValue("id"); id != "" {
		j, area, ok := h.loadJournal(w, r)
		if !ok {
			return
		}
		if j.CentarSektor != "" {
			h.showCOPJournalForm(w, r, j)
			return
		}
		data.Journal, data.Area, data.IsEdit = j, area, true
	} else {
		want, _ := strconv.Atoi(r.URL.Query().Get("area"))
		data.Area, data.Areas = h.areaOf(data.Permissions, want)
		vrsta := r.URL.Query().Get("kind")
		if vrsta == models.JournalKindDefense {
			http.Redirect(w, r, "/dnevnici/novi-cop", http.StatusSeeOther)
			return
		}
		if !models.IsJournalKind(vrsta) {
			vrsta = models.JournalKindMaintenanceA02
		}
		data.Journal = &models.Journal{Kind: vrsta, Year: time.Now().In(models.Zagreb).Year(),
			Investor: "Hrvatske vode, Ulica grada Vukovara 220, 10000 Zagreb"}
		if data.Area != nil {
			data.Journal.AreaID = data.Area.ID
		}
	}
	h.fillRights(&data)
	if !data.CanManage {
		http.Error(w, "Naslovnicu dnevnika uređuje ovlaštenik ili rukovoditelj područja", http.StatusForbidden)
		return
	}
	if data.Area != nil {
		data.Sections, _ = h.sections.ListSections("", data.Area.ID, "")
	}
	data.Stations, _ = h.stations.ListStations(r.Context(), "", "", false)
	h.render(w, h.tmplForm, "dnevnik_form.html", data)
}

func journalFromForm(r *http.Request) models.Journal {
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	j := models.Journal{
		ID: f("id"), Kind: f("kind"), Title: f("title"), Contract: f("contract"), SectionCode: f("section_code"),
		StructureID: f("structure_id"), Contractor: f("contractor"), ContractorLead: f("contractor_lead"),
		ContractorLeadAct: f("contractor_lead_act"), Supervisor: f("supervisor"), SupervisorAct: f("supervisor_act"),
		SupervisorDeputy: f("supervisor_deputy"), ChiefSupervisor: f("chief_supervisor"), Investor: f("investor"),
		Gauges: f("gauges"), Notes: f("notes"),
	}
	j.Year, _ = strconv.Atoi(f("year"))
	if v, err := strconv.ParseFloat(strings.ReplaceAll(f("latitude"), ",", "."), 64); err == nil {
		j.Latitude = &v
	}
	if v, err := strconv.ParseFloat(strings.ReplaceAll(f("longitude"), ",", "."), 64); err == nil {
		j.Longitude = &v
	}
	if t, err := time.ParseInLocation("2006-01-02", f("started_at"), models.Zagreb); err == nil {
		j.StartedAt = &t
	}
	if t, err := time.ParseInLocation("2006-01-02", f("ended_at"), models.Zagreb); err == nil {
		j.EndedAt = &t
	}
	return j
}

// HandleSaveJournal upisuje naslovnicu (novu ili izmijenjenu)
func (h *JournalsHandler) HandleSaveJournal(w http.ResponseWriter, r *http.Request) {
	u, perms := h.base(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/dnevnici", "error", "Neispravan zahtjev")
		return
	}
	j := journalFromForm(r)
	if id := r.PathValue("id"); id != "" {
		j.ID = id
	}
	areaID, _ := strconv.Atoi(r.FormValue("area"))
	if j.ID != "" {
		if cur, _ := h.journals.GetJournal(r.Context(), j.ID); cur != nil {
			if cur.CentarSektor != "" {
				h.HandleSaveCOPJournal(w, r)
				return
			}
			areaID = cur.AreaID
		}
	}
	area, _ := h.areaOf(perms, areaID)
	if area == nil || areaID == 0 {
		redirectWith(w, r, "/dnevnici", "error", "Nepoznato područje")
		return
	}
	if err := h.journals.SaveJournal(r.Context(), u, perms, *area, &j); err != nil {
		back := "/dnevnici/new?area=" + strconv.Itoa(area.ID)
		if r.PathValue("id") != "" {
			back = "/dnevnici/" + j.ID + "/edit"
		}
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, "/dnevnici/"+j.ID, "success", "Naslovnica dnevnika je spremljena.")
}

// ShowJournal prikazuje dnevnik: naslovnicu, listove, otvorene naloge i kontrolu brojeva
func (h *JournalsHandler) ShowJournal(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	data := h.pageData(r)
	data.Journal, data.Area = j, area
	h.fillRights(&data)
	ctx := r.Context()
	// Dnevnik COP-a nema listova ni naloga: to je zapisnik dežurstva, a zapisi
	// teku po danima izravno uz dnevnik.
	if j.CentarSektor != "" {
		zapisi, err := h.journals.EntriesForJournal(ctx, j.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Dnevnik se čita i po području: ?podrucje=16 ostavlja zapise tog
		// područja i one za cijeli sektor, jer se sektorski tiču svakoga.
		data.Areas, _ = h.users.ListAreas(j.CentarSektor)
		if data.Podrucje, _ = strconv.Atoi(r.URL.Query().Get("podrucje")); data.Podrucje > 0 {
			var samo []models.JournalEntry
			for _, z := range zapisi {
				if !z.ZaPodrucje() || z.PodrucjeID() == data.Podrucje {
					samo = append(samo, z)
				}
			}
			zapisi = samo
		}
		data.Dani = poDanima(zapisi)
		for _, d := range data.Dani {
			if d.Dan.Format("2006-01-02") == data.Today {
				data.ImaDanas = true
			}
		}
		// Plan dežurstava ima svoju stranicu; ovdje samo koliko ih je, za gumb.
		data.Dezurstva, _ = h.journals.Dezurstva(ctx, j.ID)
		h.render(w, h.tmplCOP, "dnevnik_cop.html", data)
		return
	}
	data.Sheets, _ = h.journals.ListSheets(ctx, j.ID)
	data.OpenTasks, _ = h.journals.OpenTasks(ctx, j.ID)
	data.Gaps, _ = h.journals.NumberGaps(ctx, j.ID)
	h.render(w, h.tmplJournal, "dnevnik.html", data)
}

// HandleOpenSheet otvara novi list za dan i vodi na njega
func (h *JournalsHandler) HandleOpenSheet(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	u, perms := h.base(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/dnevnici/"+j.ID, "error", "Neispravan zahtjev")
		return
	}
	day, err := time.ParseInLocation("2006-01-02", r.FormValue("date"), models.Zagreb)
	if err != nil {
		day = time.Now().In(models.Zagreb).Truncate(24 * time.Hour)
	}
	sh, err := h.journals.NewSheet(r.Context(), u, perms, h.opseg(j, area), j, day, r.FormValue("label"))
	if err != nil {
		redirectWith(w, r, "/dnevnici/"+j.ID, "error", err.Error())
		return
	}
	http.Redirect(w, r, "/dnevnici/"+j.ID+"/listovi/"+sh.ID, http.StatusSeeOther)
}

func (h *JournalsHandler) loadSheet(w http.ResponseWriter, r *http.Request, j *models.Journal) *models.JournalSheet {
	sh, err := h.journals.GetSheet(r.Context(), r.PathValue("sheet"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	if sh == nil || sh.JournalID != j.ID {
		http.NotFound(w, r)
		return nil
	}
	return sh
}

// ShowSheet prikazuje list: uvjete, osoblje i strojeve, upise i obrazac za novi upis
func (h *JournalsHandler) ShowSheet(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	sh := h.loadSheet(w, r, j)
	if sh == nil {
		return
	}
	data := h.pageData(r)
	data.Journal, data.Area, data.Sheet = j, area, sh
	h.fillRights(&data)
	ctx := r.Context()
	data.Entries, _ = h.journals.EntriesForSheet(ctx, sh.ID)
	data.StaffRows, data.MachineRows = models.ParseCounts(sh.Staff), models.ParseCounts(sh.Machines)
	data.Capacity = service.SheetCapacity
	data.Used = service.ContractorEntries(data.Entries)
	data.IsFull = data.Used >= service.SheetCapacity && data.IsContractor
	if j.IsDefense() {
		data.Sections, _ = h.sections.ListSections("", area.ID, "")
	} else {
		all, _ := h.maintenance.ListWaters(ctx, area.ID)
		for _, mw := range all {
			if mw.ProgramOf() == j.Program() {
				data.Locations = append(data.Locations, mw)
			}
		}
	}
	data.WorkItems, _ = h.maintenance.ListItems(ctx, area.ID, false)
	h.render(w, h.tmplSheet, "dnevnik_list.html", data)
}

func sheetPath(j *models.Journal, sh *models.JournalSheet) string {
	return "/dnevnici/" + j.ID + "/listovi/" + sh.ID
}

func floatField(r *http.Request, key string) *float64 {
	v, ok := parseBroj(r.FormValue(key))
	if !ok {
		return nil
	}
	return &v
}

// HandleSheetConditions sprema uvjete, osoblje i strojeve
func (h *JournalsHandler) HandleSheetConditions(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	sh := h.loadSheet(w, r, j)
	if sh == nil {
		return
	}
	_, perms := h.base(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, sheetPath(j, sh), "error", "Neispravan zahtjev")
		return
	}
	upd := *sh
	upd.Label = strings.TrimSpace(r.FormValue("label"))
	upd.Conditions = strings.TrimSpace(r.FormValue("conditions"))
	upd.Temperature, upd.WindFrom, upd.WindTo = floatField(r, "temperature"), floatField(r, "wind_from"), floatField(r, "wind_to")
	upd.Pressure, upd.Precipitation = floatField(r, "pressure"), floatField(r, "precipitation")
	upd.WaterLevels = strings.TrimSpace(r.FormValue("water_levels"))
	upd.Rating, _ = strconv.Atoi(r.FormValue("rating"))
	upd.RatingNote = strings.TrimSpace(r.FormValue("rating_note"))
	if r.FormValue("weather_edited") == "1" {
		upd.WeatherSource = "RUČNO"
	}
	upd.Staff = models.JoinCounts(countRows(r, "staff_name", "staff_n"))
	upd.Machines = models.JoinCounts(countRows(r, "machine_name", "machine_n"))
	if err := h.journals.UpdateSheet(r.Context(), perms, h.opseg(j, area), &upd); err != nil {
		redirectWith(w, r, sheetPath(j, sh), "error", err.Error())
		return
	}
	redirectWith(w, r, sheetPath(j, sh), "success", "Uvjeti na listu su spremljeni.")
}

// countRows čita retke "naziv, broj" iz obrasca; prazni nazivi i nule se
// preskaču, isti naziv dvaput se zbraja
func countRows(r *http.Request, nameKey, nKey string) []models.Count {
	names, ns := r.Form[nameKey], r.Form[nKey]
	var out []models.Count
	idx := map[string]int{}
	for i, name := range names {
		name = strings.Join(strings.Fields(name), " ")
		if name == "" || i >= len(ns) {
			continue
		}
		n, _ := strconv.Atoi(strings.TrimSpace(ns[i]))
		if n <= 0 {
			continue
		}
		key := strings.ToUpper(name)
		if j, ok := idx[key]; ok {
			out[j].N += n
			continue
		}
		idx[key] = len(out)
		out = append(out, models.Count{Name: name, N: n})
	}
	return out
}

// HandleSheetWeather ponovno povlači vremenske prilike i vodostaje
func (h *JournalsHandler) HandleSheetWeather(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	sh := h.loadSheet(w, r, j)
	if sh == nil {
		return
	}
	_, perms := h.base(r)
	if err := h.journals.RefreshWeather(r.Context(), perms, h.opseg(j, area), j, sh); err != nil {
		redirectWith(w, r, sheetPath(j, sh), "error", err.Error())
		return
	}
	redirectWith(w, r, sheetPath(j, sh), "success", "Vremenske prilike i vodostaji su osvježeni.")
}

// HandleConfirmSheet potvrđuje list za stranu koja potvrđuje
func (h *JournalsHandler) HandleConfirmSheet(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	sh := h.loadSheet(w, r, j)
	if sh == nil {
		return
	}
	u, perms := h.base(r)
	if err := h.journals.ConfirmSheet(r.Context(), u, perms, h.opseg(j, area), sh.ID); err != nil {
		redirectWith(w, r, sheetPath(j, sh), "error", err.Error())
		return
	}
	redirectWith(w, r, sheetPath(j, sh), "success", "List je potvrđen.")
}

// HandleAddEntry upisuje na list
func (h *JournalsHandler) HandleAddEntry(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	sh := h.loadSheet(w, r, j)
	if sh == nil {
		return
	}
	u, perms := h.base(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, sheetPath(j, sh), "error", "Neispravan zahtjev")
		return
	}
	e := models.JournalEntry{
		Kind: r.FormValue("kind"), MaintainedWaterID: r.FormValue("location"), SectionCode: strings.TrimSpace(r.FormValue("section_code")),
		Place: strings.TrimSpace(r.FormValue("place")), WorkItemID: r.FormValue("work_item"), Text: r.FormValue("text"),
		ParentID: r.FormValue("parent_id"), Hours: floatField(r, "hours"),
	}
	if t, err := time.ParseInLocation("2006-01-02", r.FormValue("due_date"), models.Zagreb); err == nil {
		e.DueDate = &t
	}
	target, err := h.journals.AddEntry(r.Context(), u, perms, h.opseg(j, area), j, sh, &e)
	if err != nil {
		redirectWith(w, r, sheetPath(j, sh), "error", err.Error())
		return
	}
	redirectWith(w, r, sheetPath(j, target)+"#upisi", "success", fmt.Sprintf("Upis br. %d je dodan.", e.Number))
}

// HandleVoidEntry stornira upis
func (h *JournalsHandler) HandleVoidEntry(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	u, perms := h.base(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/dnevnici/"+j.ID, "error", "Neispravan zahtjev")
		return
	}
	back := "/dnevnici/" + j.ID
	if e, _ := h.journals.GetJournal(r.Context(), j.ID); e != nil && r.FormValue("sheet") != "" {
		back += "/listovi/" + r.FormValue("sheet")
	}
	if err := h.journals.VoidEntry(r.Context(), u, perms, h.opseg(j, area), r.PathValue("entry"), r.FormValue("reason")); err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	if j.CentarSektor != "" {
		redirectWith(w, r, back+"#novi-zapis", "success", "Zapis je storniran; ostaje u dnevniku s razlogom.")
		return
	}
	redirectWith(w, r, back+"#upisi", "success", "Upis je storniran; ostaje na listu s razlogom.")
}

// HandleTaskStatus mijenja stanje naloga
func (h *JournalsHandler) HandleTaskStatus(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	u, perms := h.base(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/dnevnici/"+j.ID, "error", "Neispravan zahtjev")
		return
	}
	back := "/dnevnici/" + j.ID
	if r.FormValue("sheet") != "" {
		back += "/listovi/" + r.FormValue("sheet")
	}
	if err := h.journals.SetTaskStatus(r.Context(), u, perms, h.opseg(j, area), r.PathValue("entry"), r.FormValue("status")); err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, back+"#upisi", "success", "Stanje naloga je promijenjeno.")
}

// ShowPrint prikazuje listove za ispis u obliku obrasca, u zadanom razdoblju
func (h *JournalsHandler) ShowPrint(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	data := h.pageData(r)
	data.Journal, data.Area = j, area
	data.From, data.To = r.URL.Query().Get("od"), r.URL.Query().Get("do")
	ctx := r.Context()
	sheets, _ := h.journals.ListSheets(ctx, j.ID)
	for i := len(sheets) - 1; i >= 0; i-- { // od najstarijeg
		sh := sheets[i]
		key := sh.DateKey()
		if (data.From != "" && key < data.From) || (data.To != "" && key > data.To) {
			continue
		}
		entries, _ := h.journals.EntriesForSheet(ctx, sh.ID)
		data.PrintSheets = append(data.PrintSheets, PrintSheet{Sheet: sh, Entries: entries})
	}
	h.render(w, h.tmplPrint, "dnevnik_ispis.html", data)
}

// DanZapisa su zapisi jednog dana dežurstva.
type DanZapisa struct {
	Dan    time.Time
	Zapisi []models.JournalEntry
}

// poDanima slaže zapisnik po danima, redom kojim je i pisan.
//
// Dnevnik COP-a nema listova: dežurstvo teče danima i zapis se veže izravno na
// dnevnik. Dan je jedina podjela koju zapisnik ima, i ona dolazi iz samog
// zapisa, ne iz nekog omota oko njega.
func poDanima(zapisi []models.JournalEntry) []DanZapisa {
	var out []DanZapisa
	for _, z := range zapisi {
		dan := z.Date
		if n := len(out); n > 0 && out[n-1].Dan.Equal(dan) {
			out[n-1].Zapisi = append(out[n-1].Zapisi, z)
			continue
		}
		out = append(out, DanZapisa{Dan: dan, Zapisi: []models.JournalEntry{z}})
	}
	return out
}

// centriZaOtvaranje su centri u kojima osoba smije otvoriti dnevnik COP-a.
func (h *JournalsHandler) centriZaOtvaranje(perms *models.UserPermissions) []models.Centar {
	sektori, _ := h.users.ListSectors()
	return h.journals.CentriZaOtvaranje(perms, sektori)
}

// ShowCOPJournalForm prikazuje obrazac za novi dnevnik COP-a: centar i
// početak dežurstva. Izvođača i nadzora nema — to je zapisnik centra.
func (h *JournalsHandler) ShowCOPJournalForm(w http.ResponseWriter, r *http.Request) {
	h.showCOPJournalForm(w, r, nil)
}

func (h *JournalsHandler) showCOPJournalForm(w http.ResponseWriter, r *http.Request, j *models.Journal) {
	data := h.pageData(r)
	data.CentriZaOtvaranje = h.centriZaOtvaranje(data.Permissions)
	if j != nil {
		if !h.journals.MozeOtvoritiCOP(data.Permissions, j.CentarSektor) {
			http.Error(w, "Zaglavlje dnevnika COP-a mijenja voditelj ili zamjenik centra", http.StatusForbidden)
			return
		}
		data.Journal, data.IsEdit = j, true
	} else {
		if len(data.CentriZaOtvaranje) == 0 {
			http.Error(w, "Dnevnik COP-a otvara voditelj ili zamjenik centra", http.StatusForbidden)
			return
		}
		danas := time.Now().In(models.Zagreb)
		pocetak := time.Date(danas.Year(), danas.Month(), danas.Day(), 0, 0, 0, 0, models.Zagreb)
		data.Journal = &models.Journal{Kind: models.JournalKindDefense, Year: danas.Year(), StartedAt: &pocetak,
			CentarSektor: r.URL.Query().Get("centar")}
		if data.Journal.CentarSektor == "" && len(data.CentriZaOtvaranje) == 1 {
			data.Journal.CentarSektor = data.CentriZaOtvaranje[0].Sektor
		}
	}
	h.render(w, h.tmplCOPForm, "dnevnik_cop_form.html", data)
}

// HandleSaveCOPJournal otvara dnevnik COP-a ili mu sprema zaglavlje.
func (h *JournalsHandler) HandleSaveCOPJournal(w http.ResponseWriter, r *http.Request) {
	u, perms := h.base(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/dnevnici/popis?vrsta=OBRANA", "error", "Neispravan zahtjev")
		return
	}
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	j := models.Journal{ID: r.PathValue("id"), CentarSektor: f("centar"), Title: f("title"), Notes: f("notes")}
	if t, err := time.ParseInLocation("2006-01-02", f("started_at"), models.Zagreb); err == nil {
		j.StartedAt = &t
	}
	if t, err := time.ParseInLocation("2006-01-02", f("ended_at"), models.Zagreb); err == nil {
		j.EndedAt = &t
	}
	back := "/dnevnici/novi-cop"
	if j.ID != "" {
		back = "/dnevnici/" + j.ID + "/edit"
	}
	if err := h.journals.SpremiCOPDnevnik(r.Context(), u, perms, &j); err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	poruka := "Dnevnik COP-a je otvoren."
	if r.PathValue("id") != "" {
		poruka = "Zaglavlje dnevnika je spremljeno."
	}
	redirectWith(w, r, "/dnevnici/"+j.ID, "success", poruka)
}

// HandleAddCOPEntry upisuje zapis u zapisnik dežurstva. Dan i vrijeme dolaze
// odvojeno: dan je obvezan, vrijeme se upisuje kad se zna kad se dogodilo.
func (h *JournalsHandler) HandleAddCOPEntry(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	u, perms := h.base(r)
	back := "/dnevnici/" + j.ID + "#novi-zapis"
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, back, "error", "Neispravan zahtjev")
		return
	}
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	e := models.JournalEntry{Kind: f("kind"), ReportedBy: f("reported_by"), Text: r.FormValue("text")}
	if n, err := strconv.Atoi(f("podrucje")); err == nil && n > 0 {
		e.Podrucje = &n
	}
	dan, err := time.ParseInLocation("2006-01-02", f("date"), models.Zagreb)
	if err != nil {
		redirectWith(w, r, back, "error", "Upišite dan zapisa")
		return
	}
	e.Date = dan
	if kad, err := time.ParseInLocation("2006-01-02 15:04", f("date")+" "+f("time"), models.Zagreb); err == nil {
		e.HappenedAt = &kad
	}
	if err := h.journals.DodajZapisCOP(r.Context(), u, perms, h.opseg(j, area), j, &e); err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	// Natrag na sam zapis: on je iznad trake za unos i nakratko zasvijetli.
	redirectWith(w, r, "/dnevnici/"+j.ID+"#zapis-"+strconv.Itoa(e.Number), "success", fmt.Sprintf("Zapis br. %d je upisan.", e.Number))
}

// HandleIspraviPrijepis ispravlja krivo pročitan zapis prijepisa na mjestu;
// vraća na taj zapis, ne na kraj dnevnika
func (h *JournalsHandler) HandleIspraviPrijepis(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	u, perms := h.base(r)
	back := "/dnevnici/" + j.ID
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, back, "error", "Neispravan zahtjev")
		return
	}
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	ispravak := models.JournalEntry{Kind: f("kind"), ReportedBy: f("reported_by"), Text: r.FormValue("text")}
	if n, err := strconv.Atoi(f("podrucje")); err == nil && n > 0 {
		ispravak.Podrucje = &n
	}
	// Dan ostaje dan zapisa; mijenja se samo sat u tom danu.
	if e, _ := h.journals.GetEntry(r.Context(), r.PathValue("entry")); e != nil {
		back += "#zapis-" + strconv.Itoa(e.Number)
		if kad, err := time.ParseInLocation("2006-01-02 15:04", e.Date.In(models.Zagreb).Format("2006-01-02")+" "+f("time"), models.Zagreb); err == nil {
			ispravak.HappenedAt = &kad
		}
	}
	if err := h.journals.IspraviPrijepis(r.Context(), u, perms, h.opseg(j, area), j, r.PathValue("entry"), ispravak); err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, back, "success", "Zapis je ispravljen; prijašnje čitanje ostaje u knjizi verzija.")
}
