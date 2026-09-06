package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// JournalsHandler: građevinski dnevnici — popis po području, naslovnica,
// listovi po danu, upisi, nalozi i ispis
type JournalsHandler struct {
	journals    *service.JournalService
	users       *service.UserService
	maintenance *service.MaintenanceService
	sections    *service.SectionService
	stations    *service.StationService
	tmplList    *template.Template
	tmplForm    *template.Template
	tmplJournal *template.Template
	tmplSheet   *template.Template
	tmplPrint   *template.Template
}

func NewJournalsHandler(j *service.JournalService, users *service.UserService, m *service.MaintenanceService,
	sections *service.SectionService, stations *service.StationService,
	list, form, journal, sheet, print *template.Template) *JournalsHandler {
	return &JournalsHandler{journals: j, users: users, maintenance: m, sections: sections, stations: stations,
		tmplList: list, tmplForm: form, tmplJournal: journal, tmplSheet: sheet, tmplPrint: print}
}

// JournalPageData su podaci svih stranica dnevnika; što stranica ne treba ostaje prazno
type JournalPageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	Areas          []models.Area
	Area           *models.Area
	Journals       []models.Journal
	Journal        *models.Journal
	Sheets         []models.JournalSheet
	Sheet          *models.JournalSheet
	Entries        []models.JournalEntry
	OpenTasks      []models.JournalEntry
	Gaps           []int
	Kinds          []string
	Locations      []models.MaintainedWater
	WorkItems      []models.WorkItem
	Sections       []models.Section
	Stations       []models.Station
	AllowedKinds   []string
	StaffRoles     []string
	MachineTypes   []string
	ConditionWords []string
	Ratings        []int
	StaffRows      []models.Count
	MachineRows    []models.Count
	Capacity       int
	Used           int  // izvođačevih upisa na listu
	IsFull         bool // za izvođača: nema mjesta, otvara novi list
	Today          string
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
		CurrentUser: u, Permissions: perms, Kinds: models.JournalKinds,
		StaffRoles: models.StaffRoles, MachineTypes: models.MachineTypes, ConditionWords: models.ConditionWords,
		Ratings: models.Ratings, Today: time.Now().In(models.Zagreb).Format("2006-01-02"),
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
	area, _ := h.areaOf(perms, j.AreaID)
	if area == nil {
		http.Error(w, "dnevnik pokazuje na nepoznato područje", http.StatusInternalServerError)
		return nil, nil, false
	}
	return j, area, true
}

func (h *JournalsHandler) fillRights(d *JournalPageData) {
	if d.Area == nil {
		return
	}
	d.CanWrite = h.journals.CanWrite(d.Permissions, *d.Area)
	d.CanSupervise = h.journals.CanSupervise(d.CurrentUser, d.Permissions, *d.Area)
	d.CanManage = h.journals.CanManage(d.CurrentUser, d.Permissions, *d.Area)
	d.AllowedKinds = h.journals.AllowedKinds(d.CurrentUser, d.Permissions, *d.Area)
}

// ShowJournals prikazuje dnevnike područja
func (h *JournalsHandler) ShowJournals(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	want, _ := strconv.Atoi(r.URL.Query().Get("area"))
	data.Area, data.Areas = h.areaOf(data.Permissions, want)
	h.fillRights(&data)
	if data.Area != nil {
		js, err := h.journals.ListJournals(r.Context(), data.Area.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
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
		data.Journal, data.Area, data.IsEdit = j, area, true
	} else {
		want, _ := strconv.Atoi(r.URL.Query().Get("area"))
		data.Area, data.Areas = h.areaOf(data.Permissions, want)
		data.Journal = &models.Journal{Kind: models.JournalKindMaintenanceA02, Year: time.Now().In(models.Zagreb).Year(),
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
	sh, err := h.journals.NewSheet(r.Context(), u, perms, *area, j, day, r.FormValue("label"))
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
	if err := h.journals.UpdateSheet(r.Context(), perms, *area, &upd); err != nil {
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
	if err := h.journals.RefreshWeather(r.Context(), perms, *area, j, sh); err != nil {
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
	if err := h.journals.ConfirmSheet(r.Context(), u, perms, *area, sh.ID); err != nil {
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
	target, err := h.journals.AddEntry(r.Context(), u, perms, *area, j, sh, &e)
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
	if err := h.journals.VoidEntry(r.Context(), u, perms, *area, r.PathValue("entry"), r.FormValue("reason")); err != nil {
		redirectWith(w, r, back, "error", err.Error())
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
	if err := h.journals.SetTaskStatus(r.Context(), u, perms, *area, r.PathValue("entry"), r.FormValue("status")); err != nil {
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
