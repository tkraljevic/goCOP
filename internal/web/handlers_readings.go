package web

import (
	"fmt"
	"html/template"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// Vodostaji: pregled letvi sa zadnjim očitanjem, povijest jedne letve i
// obrazac za upis. Obrazac je građen za telefon na nasipu: jedan broj,
// vrijeme je sad, sve ostalo je izborno.

type ReadingsHandler struct {
	readingService   *service.ReadingService
	stationService   *service.StationService
	structureService *service.StructureService
	userService      *service.UserService
	tmplOverview     *template.Template
	tmplHistory      *template.Template
	tmplForm         *template.Template
}

func NewReadingsHandler(readings *service.ReadingService, stations *service.StationService,
	structures *service.StructureService, users *service.UserService, overview, history, form *template.Template) *ReadingsHandler {
	return &ReadingsHandler{readingService: readings, stationService: stations, structureService: structures,
		userService: users, tmplOverview: overview, tmplHistory: history, tmplForm: form}
}

const readingsPerPage = 30

type ReadingsOverviewData struct {
	CurrentUser  *models.User
	Permissions  *models.UserPermissions
	Gauges       []models.GaugeSummary
	Areas        []models.Area
	SelectedArea int
	SearchQuery  string
	ShowAll      bool
	WithReadings int
	TotalGauges  int
	TotalCount   int
	LastAt       time.Time
	Pager        Pager

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

type ReadingHistoryData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	Station     *models.Station
	Structure   *models.Structure
	GaugeName   string
	GaugeSub    string
	NewURL      string
	Readings    []models.Reading
	Latest      *models.Reading
	Count       int
	Years       []int
	Year        int
	Chart       *Chart
	CanRecord   bool
	CanEdit     bool
	Pager       Pager

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

type ReadingFormData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	Reading     *models.Reading
	Station     *models.Station
	Structure   *models.Structure
	GaugeName   string
	GaugeSub    string
	BackURL     string
	IsEdit      bool
	IsPump      bool
	IsSluice    bool
	LocalValue  string // vrijednost za datetime-local
	Sources     []string
	States      []string
	Gates       []string
	Latest      *models.Reading

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// Chart je jednostavan linijski graf zadnjih očitanja, crtan kao SVG u predlošku
type Chart struct {
	Width, Height int
	Path          string
	Area          string
	Points        []ChartPoint
	YTicks        []ChartTick
	XTicks        []ChartTick
	Thresholds    []ChartLine
	From, To      time.Time
	Min, Max      int
}

type ChartPoint struct {
	X, Y  float64
	Level int
	At    time.Time
}

type ChartTick struct {
	Pos   float64
	Label string
}

type ChartLine struct {
	Y     float64
	Label string
	Class string
}

func (h *ReadingsHandler) base(r *http.Request) (*models.User, *models.UserPermissions) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	p, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	return u, p
}

// ShowOverview prikazuje sve letve sa zadnjim očitanjem
func (h *ReadingsHandler) ShowOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, perms := h.base(r)
	q := r.URL.Query()
	areaID, _ := strconv.Atoi(q.Get("area"))
	data := ReadingsOverviewData{
		CurrentUser: u, Permissions: perms, SelectedArea: areaID,
		SearchQuery: strings.TrimSpace(q.Get("q")), ShowAll: q.Get("all") == "1",
		SuccessMessage: q.Get("success"), ErrorMessage: q.Get("error"),
		ActiveNav: "readings", ViewAsBanner: viewBanner(r),
	}
	all, err := h.readingService.Overview(ctx)
	if err != nil {
		data.ErrorMessage = err.Error()
	}
	data.TotalGauges = len(all)
	var shown []models.GaugeSummary
	needle := strings.ToLower(data.SearchQuery)
	for _, g := range all {
		if g.Count > 0 {
			data.WithReadings++
		}
		if !data.ShowAll && g.Count == 0 {
			continue
		}
		if areaID > 0 && g.AreaID != areaID && g.StructureID != "" {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(g.Name+" "+g.Sub), needle) {
			continue
		}
		shown = append(shown, g)
	}
	data.Gauges, data.Pager = paginate(shown, r, registryPerPage)
	data.TotalCount, _, data.LastAt, _ = h.readingService.Stats(ctx)
	data.Areas, _ = h.userService.ListAreas("")

	if err := h.tmplOverview.ExecuteTemplate(w, "readings.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// gauge učitava letvu iz putanje ili upita: postaju ili objekt
func (h *ReadingsHandler) gauge(r *http.Request, stationID, structureID string) (*models.Station, *models.Structure) {
	if structureID != "" {
		if id, err := uuid.Parse(structureID); err == nil {
			if st, err := h.structureService.Get(r.Context(), id); err == nil && st != nil {
				return nil, st
			}
		}
		return nil, nil
	}
	if id, err := uuid.Parse(stationID); err == nil {
		if st, err := h.stationService.GetStation(r.Context(), id); err == nil && st != nil {
			return st, nil
		}
	}
	return nil, nil
}

func gaugeNames(station *models.Station, structure *models.Structure) (name, sub string) {
	if structure != nil {
		return structure.Name, structure.KindLabel() + " · BP " + strconv.Itoa(structure.AreaID)
	}
	if station != nil {
		sub = strings.Trim(station.Watercourse+" · "+station.Stationing, " ·")
		return station.Name, sub
	}
	return "", ""
}

// ShowHistory prikazuje očitanja jedne letve
func (h *ReadingsHandler) ShowHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, perms := h.base(r)
	var station *models.Station
	var structure *models.Structure
	if strings.HasPrefix(r.URL.Path, "/readings/structure/") {
		station, structure = h.gauge(r, "", r.PathValue("id"))
	} else {
		station, structure = h.gauge(r, r.PathValue("id"), "")
	}
	if station == nil && structure == nil {
		http.NotFound(w, r)
		return
	}
	data := ReadingHistoryData{
		CurrentUser: u, Permissions: perms, Station: station, Structure: structure,
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "readings", ViewAsBanner: viewBanner(r),
	}
	data.GaugeName, data.GaugeSub = gaugeNames(station, structure)
	f := repository.ReadingFilter{}
	if structure != nil {
		f.StructureID = structure.ID.String()
		data.NewURL = "/readings/new?structure=" + f.StructureID
		data.CanRecord = h.readingService.CanRecordStructure(perms, structure)
	} else {
		f.StationID = station.ID.String()
		data.NewURL = "/readings/new?station=" + f.StationID
		data.CanRecord = h.readingService.CanRecordStation(perms, station)
	}
	data.CanEdit = data.CanRecord
	all, err := h.readingService.List(ctx, f)
	if err != nil {
		data.ErrorMessage = err.Error()
	}
	data.Count = len(all)
	if len(all) > 0 {
		latest := all[0]
		data.Latest = &latest
	}

	// Pragovi vodomjera: postaja svoje, objekt od pridruženog vodomjera
	thresholdStation := station
	if structure != nil && structure.StationID != "" {
		if id, err := uuid.Parse(structure.StationID); err == nil {
			thresholdStation, _ = h.stationService.GetStation(ctx, id)
		}
	}
	for i := range all {
		all[i].Phase = h.readingService.PhaseFor(thresholdStation, all[i].LevelCm)
	}

	years := map[int]bool{}
	for _, rd := range all {
		years[rd.LocalTime().Year()] = true
	}
	for y := range years {
		data.Years = append(data.Years, y)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(data.Years)))
	data.Year, _ = strconv.Atoi(r.URL.Query().Get("year"))
	shown := all
	if data.Year > 0 {
		shown = shown[:0:0]
		for _, rd := range all {
			if rd.LocalTime().Year() == data.Year {
				shown = append(shown, rd)
			}
		}
	}
	data.Count = len(shown)
	data.Chart = buildChart(shown, thresholdStation, data.Year == 0)
	data.Readings, data.Pager = paginate(shown, r, readingsPerPage)

	if err := h.tmplHistory.ExecuteTemplate(w, "reading_history.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// buildChart crta zadnjih 120 očitanja (ili cijelu godinu) s vodostajem
func buildChart(readings []models.Reading, station *models.Station, limitRecent bool) *Chart {
	var pts []models.Reading
	for _, rd := range readings {
		if rd.LevelCm != nil {
			pts = append(pts, rd)
		}
	}
	if limitRecent && len(pts) > 120 {
		pts = pts[:120]
	}
	if len(pts) < 2 {
		return nil
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].MeasuredAt.Before(pts[j].MeasuredAt) })
	c := &Chart{Width: 640, Height: 220, From: pts[0].MeasuredAt, To: pts[len(pts)-1].MeasuredAt}
	c.Min, c.Max = *pts[0].LevelCm, *pts[0].LevelCm
	for _, rd := range pts {
		c.Min = min(c.Min, *rd.LevelCm)
		c.Max = max(c.Max, *rd.LevelCm)
	}
	if station != nil {
		for _, t := range []models.Threshold{station.Prep, station.Regular, station.Emergency, station.State} {
			if t.IsUsable() && *t.Cm <= c.Max+50 && *t.Cm >= c.Min-50 {
				c.Min = min(c.Min, *t.Cm)
				c.Max = max(c.Max, *t.Cm)
			}
		}
	}
	if c.Max == c.Min {
		c.Max++
	}
	pad := (c.Max - c.Min) / 10
	if pad < 5 {
		pad = 5
	}
	c.Min -= pad
	c.Max += pad
	const left, right, top, bottom = 44.0, 12.0, 12.0, 28.0
	plotW := float64(c.Width) - left - right
	plotH := float64(c.Height) - top - bottom
	span := c.To.Sub(c.From).Seconds()
	if span <= 0 {
		span = 1
	}
	yOf := func(cm int) float64 {
		return top + plotH - (float64(cm-c.Min)/float64(c.Max-c.Min))*plotH
	}
	xOf := func(t time.Time) float64 { return left + t.Sub(c.From).Seconds()/span*plotW }

	var sb strings.Builder
	for i, rd := range pts {
		p := ChartPoint{X: xOf(rd.MeasuredAt), Y: yOf(*rd.LevelCm), Level: *rd.LevelCm, At: rd.MeasuredAt}
		c.Points = append(c.Points, p)
		if i == 0 {
			fmt.Fprintf(&sb, "M%.1f %.1f", p.X, p.Y)
		} else {
			fmt.Fprintf(&sb, " L%.1f %.1f", p.X, p.Y)
		}
	}
	c.Path = sb.String()
	first, last := c.Points[0], c.Points[len(c.Points)-1]
	c.Area = fmt.Sprintf("%s L%.1f %.1f L%.1f %.1f Z", c.Path, last.X, top+plotH, first.X, top+plotH)

	// Y osi: 4 oznake zaokružene na lijep korak
	step := niceStep(float64(c.Max-c.Min) / 5)
	for v := math.Ceil(float64(c.Min)/step) * step; v <= float64(c.Max); v += step {
		c.YTicks = append(c.YTicks, ChartTick{Pos: yOf(int(v)), Label: strconv.Itoa(int(v))})
	}
	// X osi: početak, sredina, kraj
	for _, t := range []time.Time{c.From, c.From.Add(time.Duration(span/2) * time.Second), c.To} {
		layout := "02.01.2006."
		if span < 3*24*3600 {
			layout = "02.01. 15:04"
		}
		c.XTicks = append(c.XTicks, ChartTick{Pos: xOf(t), Label: t.In(models.Zagreb).Format(layout)})
	}
	if station != nil {
		for _, l := range []struct {
			t     models.Threshold
			label string
			class string
		}{{station.Prep, "P", "prep"}, {station.Regular, "R", "regular"}, {station.Emergency, "I", "emerg"}, {station.State, "IS", "crit"}} {
			if l.t.IsUsable() && *l.t.Cm >= c.Min && *l.t.Cm <= c.Max {
				c.Thresholds = append(c.Thresholds, ChartLine{Y: yOf(*l.t.Cm), Label: l.label, Class: l.class})
			}
		}
	}
	return c
}

func niceStep(raw float64) float64 {
	if raw <= 0 {
		return 1
	}
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 5, 10} {
		if raw <= m*mag {
			return m * mag
		}
	}
	return 10 * mag
}

// ShowForm prikazuje obrazac za novo očitanje ili uređivanje postojećeg
func (h *ReadingsHandler) ShowForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, perms := h.base(r)
	data := ReadingFormData{
		CurrentUser: u, Permissions: perms, Sources: models.ReadingSources,
		States: models.StructureStates, Gates: models.Gates,
		ErrorMessage: r.URL.Query().Get("error"), ActiveNav: "readings", ViewAsBanner: viewBanner(r),
	}
	now := time.Now().In(models.Zagreb)
	rd := &models.Reading{MeasuredAt: now, Source: models.ReadingSourceManual}
	if idStr := r.PathValue("id"); idStr != "" {
		id, err := uuid.Parse(idStr)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		existing, err := h.readingService.Get(ctx, id)
		if err != nil || existing == nil {
			http.NotFound(w, r)
			return
		}
		if !h.readingService.CanEdit(ctx, perms, existing) {
			http.Error(w, "Nemate pravo mijenjati ovo očitanje", http.StatusForbidden)
			return
		}
		rd = existing
		data.IsEdit = true
	} else {
		q := r.URL.Query()
		rd.StationID, rd.StructureID = q.Get("station"), q.Get("structure")
	}
	data.Station, data.Structure = h.gauge(r, rd.StationID, rd.StructureID)
	if data.Station == nil && data.Structure == nil {
		http.NotFound(w, r)
		return
	}
	if !data.IsEdit {
		if data.Structure != nil && !h.readingService.CanRecordStructure(perms, data.Structure) ||
			data.Station != nil && !h.readingService.CanRecordStation(perms, data.Station) {
			http.Error(w, "Nemate pravo upisivati očitanja na ovu letvu", http.StatusForbidden)
			return
		}
	}
	data.Reading = rd
	data.GaugeName, data.GaugeSub = gaugeNames(data.Station, data.Structure)
	data.BackURL = rd.GaugeURL()
	if data.Structure != nil {
		data.IsPump = data.Structure.IsPumpingStation()
		data.IsSluice = data.Structure.Kind == models.StructureKindSluice
		if latest, err := h.readingService.List(ctx, repository.ReadingFilter{StructureID: data.Structure.ID.String(), Limit: 1}); err == nil && len(latest) > 0 {
			data.Latest = &latest[0]
		}
	} else if latest, err := h.readingService.List(ctx, repository.ReadingFilter{StationID: data.Station.ID.String(), Limit: 1}); err == nil && len(latest) > 0 {
		data.Latest = &latest[0]
	}
	data.LocalValue = rd.MeasuredAt.In(models.Zagreb).Format("2006-01-02T15:04")

	if err := h.tmplForm.ExecuteTemplate(w, "reading_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func parseOptionalInt(s string) (*int, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	if s == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, fmt.Errorf("„%s“ nije broj", s)
	}
	v := int(math.Round(f))
	return &v, nil
}

func readingFromForm(r *http.Request) (*models.Reading, error) {
	rd := &models.Reading{
		StationID:      strings.TrimSpace(r.FormValue("station_id")),
		StructureID:    strings.TrimSpace(r.FormValue("structure_id")),
		Source:         strings.TrimSpace(r.FormValue("source")),
		StructureState: strings.TrimSpace(r.FormValue("structure_state")),
		Gate:           strings.TrimSpace(r.FormValue("gate")),
		Observer:       strings.TrimSpace(r.FormValue("observer")),
		Note:           strings.TrimSpace(r.FormValue("note")),
	}
	if idStr := r.FormValue("id"); idStr != "" {
		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("neispravan identifikator očitanja")
		}
		rd.ID = id
	}
	at := strings.TrimSpace(r.FormValue("measured_at"))
	if at == "" {
		rd.MeasuredAt = time.Now().UTC()
	} else {
		t, err := time.ParseInLocation("2006-01-02T15:04", at, models.Zagreb)
		if err != nil {
			return nil, fmt.Errorf("vrijeme očitanja nije čitljivo")
		}
		rd.MeasuredAt = t.UTC()
	}
	var err error
	if rd.LevelCm, err = parseOptionalInt(r.FormValue("level_cm")); err != nil {
		return nil, fmt.Errorf("vodostaj: %w", err)
	}
	if rd.Level2Cm, err = parseOptionalInt(r.FormValue("level2_cm")); err != nil {
		return nil, fmt.Errorf("nizvodni vodostaj: %w", err)
	}
	if rd.AgHours1, err = parseOptionalInt(r.FormValue("ag_hours_1")); err != nil {
		return nil, fmt.Errorf("agregat 1: %w", err)
	}
	if rd.AgHours2, err = parseOptionalInt(r.FormValue("ag_hours_2")); err != nil {
		return nil, fmt.Errorf("agregat 2: %w", err)
	}
	if rd.AgHours3, err = parseOptionalInt(r.FormValue("ag_hours_3")); err != nil {
		return nil, fmt.Errorf("agregat 3: %w", err)
	}
	return rd, nil
}

func (h *ReadingsHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	_, perms := h.base(r)
	rd, err := readingFromForm(r)
	if err != nil {
		redirectWith(w, r, "/readings/new?station="+r.FormValue("station_id")+"&structure="+r.FormValue("structure_id"), "error", err.Error())
		return
	}
	if err := h.readingService.Create(r.Context(), perms, rd); err != nil {
		redirectWith(w, r, "/readings/new?station="+rd.StationID+"&structure="+rd.StructureID, "error", err.Error())
		return
	}
	redirectWith(w, r, rd.GaugeURL(), "success", "Očitanje je upisano.")
}

func (h *ReadingsHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	_, perms := h.base(r)
	rd, err := readingFromForm(r)
	if err != nil {
		redirectWith(w, r, "/readings/edit/"+r.FormValue("id"), "error", err.Error())
		return
	}
	if rd.ID == uuid.Nil {
		http.Error(w, "Nedostaje identifikator očitanja", http.StatusBadRequest)
		return
	}
	if err := h.readingService.Update(r.Context(), perms, rd); err != nil {
		redirectWith(w, r, "/readings/edit/"+rd.ID.String(), "error", err.Error())
		return
	}
	redirectWith(w, r, rd.GaugeURL(), "success", "Očitanje je izmijenjeno.")
}

func (h *ReadingsHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	_, perms := h.base(r)
	id, err := uuid.Parse(r.FormValue("id"))
	if err != nil {
		http.Error(w, "Neispravan identifikator očitanja", http.StatusBadRequest)
		return
	}
	deleted, err := h.readingService.Delete(r.Context(), perms, id)
	if err != nil {
		redirectWith(w, r, "/readings", "error", err.Error())
		return
	}
	redirectWith(w, r, deleted.GaugeURL(), "success", "Očitanje je obrisano; zapis ostaje u povijesti.")
}
