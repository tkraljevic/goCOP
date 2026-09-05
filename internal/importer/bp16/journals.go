package bp16

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/db"
	"gocop/internal/hydro"
	"gocop/internal/importer/ugovor"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Uvoz evidencija radova A.02 i A.03 iz Directusa u građevinske dnevnike.
//
// U evidenciji su upisi po danu, vodi, stacionaži i tekstu rada; listova u
// njoj nije bilo, oni su se slagali u Excelu i ovjeravali potpisima. Zato je
// ovo REKONSTRUKCIJA: dnevnik po programu i godini, listovi složeni po danu i
// po šest upisa, a na svakom listu prvi upis kaže da list nije istovjetan
// ovjerenom i da služi samo kao primjer.

// UserSource daje korisnike Directusa, da upis nosi ime onoga tko ga je napravio
type UserSource interface {
	Users(ctx context.Context) ([]json.RawMessage, error)
}

// Users čita /users; token mora smjeti čitati korisnike
func (s HTTPSource) Users(ctx context.Context) ([]json.RawMessage, error) {
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	u := strings.TrimRight(s.URL, "/") + "/users?limit=1000&fields=id,first_name,last_name,email"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("User-Agent", "gocop-import/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("users: Directus odgovorio %d: %.200s", resp.StatusCode, body)
	}
	var env struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

// Users čita directus_users.json iz mape
func (s DirSource) Users(ctx context.Context) ([]json.RawMessage, error) {
	return s.Items(ctx, "directus_users")
}

// JournalDeps su spremišta i područje u koje uvoz piše
type JournalDeps struct {
	Journals    *repository.JournalRepository
	Maintenance *repository.MaintenanceRepository
	Waters      *repository.WatercourseRepository
	Structures  *repository.StructureRepository
	Areas       []models.Area
	AreaID      int
	DryRun      bool
	Log         func(string, ...any)
}

// JournalReport je izvješće uvoza dnevnika
type JournalReport struct {
	Records      int
	Journals     int
	Sheets       int
	Entries      int
	Notes        int
	Supervisor   int
	NewLocations []string
	Unmatched    int // upisa bez lokacije (opći upisi)
	NoUser       int
	PerYear      map[string]int
	DryRun       bool
}

// Summary sažima izvješće u jedan redak
func (r JournalReport) Summary() string {
	return fmt.Sprintf("%d zapisa → %d dnevnika, %d listova, %d upisa (%d nadzora) + %d napomena o rekonstrukciji; %d novih lokacija, %d upisa bez lokacije",
		r.Records, r.Journals, r.Sheets, r.Entries, r.Supervisor, r.Notes, len(r.NewLocations), r.Unmatched)
}

// ReconstructionNote je tekst prvog upisa na svakom rekonstruiranom listu
const ReconstructionNote = "REKONSTRUKCIJA: upisi na ovom listu preneseni su iz evidencije radova app.bp16.xyz radi primjera. " +
	"Stvarni listovi dnevnika vođeni su u Excelu i ovjereni potpisima; ovaj list im nije istovjetan i ne zamjenjuje ih."

type vodaRow struct {
	ID      int     `json:"id"`
	Voda    string  `json:"voda"`
	Vrsta   *string `json:"vrsta"`
	Red     *string `json:"red"`
	Stavka  *string `json:"stavka"`
	Program *string `json:"program_radova"`
}

type evRow struct {
	ID          int     `json:"id"`
	Datum       string  `json:"datum"`
	NazivVode   *int    `json:"naziv_vode"`
	Lokacija    *string `json:"lokacija"`
	Upis        *string `json:"upis"`
	UserCreated *string `json:"user_created"`
	DateCreated *string `json:"date_created"`
}

// stavkaRow je redak aNN_stavke: na njega pokazuje upis (naziv_vode), a on na vodu
type stavkaRow struct {
	ID     int     `json:"id_stavke"`
	Voda   *int    `json:"voda"`
	Stavka *string `json:"stavka"`
}

type weatherRow struct {
	Datum     string  `json:"datum"`
	Vrijeme   string  `json:"vrijeme"`
	Opisno    *string `json:"vrijeme_opisno"`
	Vodostaji *string `json:"vodostaji_opisno"`
}

type userRow struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
}

var (
	reTemp   = regexp.MustCompile(`temperatura zraka:\s*(-?\d+(?:[.,]\d+)?)`)
	reWind   = regexp.MustCompile(`brzina vjetra:\s*(\d+(?:[.,]\d+)?)\s*-\s*(\d+(?:[.,]\d+)?)`)
	rePress  = regexp.MustCompile(`tlak zraka:\s*(\d+(?:[.,]\d+)?)`)
	rePrecip = regexp.MustCompile(`oborine:\s*(\d+(?:[.,]\d+)?)`)
)

func num(s string) *float64 {
	v, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
	if err != nil {
		return nil
	}
	return &v
}

// parseWeather čita opisne prilike s lista u brojeve
func parseWeather(sh *models.JournalSheet, text string) {
	if m := reTemp.FindStringSubmatch(text); m != nil {
		sh.Temperature = num(m[1])
	}
	if m := reWind.FindStringSubmatch(text); m != nil {
		sh.WindFrom, sh.WindTo = num(m[1]), num(m[2])
	}
	if m := rePress.FindStringSubmatch(text); m != nil {
		sh.Pressure = num(m[1])
	}
	if m := rePrecip.FindStringSubmatch(text); m != nil {
		sh.Precipitation = num(m[1])
	}
}

// classify izvodi program, red, skupinu i vrstu iz zapisa vode
func classify(v vodaRow) (program, order, group, kind string, embankment bool) {
	red := ""
	if v.Red != nil {
		red = *v.Red
	}
	switch {
	case strings.HasPrefix(red, "I. red - Među"):
		order, group = models.WaterOrderFirst, models.WaterGroupInterstate
	case strings.HasPrefix(red, "I. red"):
		order, group = models.WaterOrderFirst, models.WaterGroupOtherState
	case strings.HasPrefix(red, "II."):
		order = models.WaterOrderSecond
	case strings.HasPrefix(red, "III."):
		order = models.WaterOrderThird
	case strings.HasPrefix(red, "IV."):
		order = models.WaterOrderFourth
	}
	vrsta := ""
	if v.Vrsta != nil {
		vrsta = strings.ToLower(*v.Vrsta)
	}
	switch vrsta {
	case "vodotok":
		kind = models.MaintenanceKindWatercourse
	case "bujica":
		kind = models.MaintenanceKindTorrent
	case "retencija", "akumulacija":
		kind = models.MaintenanceKindReservoir
	case "nasip":
		kind, embankment = models.MaintenanceKindReservoir, true
	default:
		kind = models.MaintenanceKindDrainage
	}
	program = models.ProgramA02
	if v.Program != nil && strings.HasPrefix(*v.Program, "A.03") {
		program = models.ProgramA03
	} else if v.Program == nil && (order == models.WaterOrderThird || order == models.WaterOrderFourth) {
		program = models.ProgramA03
	}
	return
}

// nameAliases veže nazive iz evidencije koji se od ugovornih razlikuju samo
// u pisanju, da se ne bi stvorile dvostruke lokacije
var nameAliases = map[string]string{
	"Stara Drava Bilje":                   "Retencija Stara Drava - Bilje",
	"GDK za CS Podunavlje - kanal Bojana": "Kanal Bojana - GDK za CS Podunavlje",
	"Kanal Dunavac-Bodorfok i K29":        "Kanal Dunavac-Bodorfok i K-29",
	"Kanal Mala Dolina":                   "Bujica Mala Dolina",
	"GDK CS Draž - OK Karašica":           "G.D.K. CS Draž - O.K. Karašica",
}

// placeholder su "vode" koje nisu vode: opći upis i prazno mjesto za naziv
func placeholder(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "upis" || n == "naziv kanala" || n == ""
}

// RunJournals uvozi evidencije radova u dnevnike; bez DryRun piše
func RunJournals(ctx context.Context, src Source, deps JournalDeps) (JournalReport, error) {
	rep := JournalReport{DryRun: deps.DryRun, PerYear: map[string]int{}}
	logf := func(f string, a ...any) {
		if deps.Log != nil {
			deps.Log(f, a...)
		}
	}
	var area *models.Area
	for i := range deps.Areas {
		if deps.Areas[i].ID == deps.AreaID {
			area = &deps.Areas[i]
		}
	}
	if area == nil {
		return rep, fmt.Errorf("područje %d nije u registru", deps.AreaID)
	}

	var vode []vodaRow
	if err := fetchInto(ctx, src, "vode", &vode); err != nil {
		return rep, err
	}
	vodaByID := map[int]vodaRow{}
	for _, v := range vode {
		vodaByID[v.ID] = v
	}
	users := map[string]userRow{}
	if us, ok := src.(UserSource); ok {
		raw, err := us.Users(ctx)
		if err != nil {
			logf("Korisnici Directusa nisu dostupni (%v): upisi ostaju bez imena", err)
		}
		for _, r := range raw {
			var u userRow
			if json.Unmarshal(r, &u) == nil {
				users[u.ID] = u
			}
		}
	}
	var weather []weatherRow
	if err := fetchInto(ctx, src, "vrijeme_za_list", &weather); err != nil {
		logf("vrijeme_za_list nije dostupno (%v): listovi bez prilika", err)
	}
	// prilike za dan: zapis najbliži sedam sati ujutro
	weatherFor := map[string]weatherRow{}
	for _, w := range weather {
		cur, ok := weatherFor[w.Datum]
		if !ok || distanceFromSeven(w.Vrijeme) < distanceFromSeven(cur.Vrijeme) {
			weatherFor[w.Datum] = w
		}
	}

	// Lokacije područja: postojeće po ključu naziva, nove se stvaraju
	existing, err := deps.Maintenance.ListWaters(ctx, deps.AreaID)
	if err != nil {
		return rep, err
	}
	locByKey := map[string]string{}
	for _, mw := range existing {
		locByKey[ugovor.MatchKey(mw.Name)] = mw.ID
	}
	embByKey := map[string]string{}
	if embs, err := deps.Structures.ListStructures(ctx, "", deps.AreaID, models.StructureKindEmbankment, ""); err == nil {
		for _, s := range embs {
			embByKey[ugovor.MatchKey(s.Name)] = s.ID.String()
		}
	}
	waterCodes := map[string]bool{}
	if ws, err := deps.Waters.ListWatercourses(ctx, "", "", false); err == nil {
		for _, w := range ws {
			waterCodes[w.Code] = true
		}
	}
	locFor := map[int]string{} // Directus id vode → maintained water ID
	ensureLocation := func(id int) (string, error) {
		if loc, ok := locFor[id]; ok {
			return loc, nil
		}
		v, ok := vodaByID[id]
		if !ok || placeholder(v.Voda) {
			locFor[id] = ""
			return "", nil
		}
		name := strings.Join(strings.Fields(v.Voda), " ")
		key := ugovor.MatchKey(name)
		if alias, ok := nameAliases[name]; ok {
			if loc, ok := locByKey[ugovor.MatchKey(alias)]; ok {
				locFor[id] = loc
				return loc, nil
			}
		}
		if loc, ok := locByKey[key]; ok {
			locFor[id] = loc
			return loc, nil
		}
		program, order, group, kind, embankment := classify(v)
		mw := models.MaintainedWater{
			AreaID: deps.AreaID, Program: program, Name: name, Order: order, Group: group, Kind: kind,
			Source: "evidencija app.bp16.xyz (vode)",
		}
		if v.Stavka != nil {
			mw.Seq = strings.TrimSpace(*v.Stavka)
		}
		mw.ID = repository.MaintainedWaterID(deps.AreaID, name)
		rep.NewLocations = append(rep.NewLocations, fmt.Sprintf("%s %s [%s %s]", program, name, order, kind))
		if !deps.DryRun {
			if embankment {
				if sid, ok := embByKey[key]; ok {
					mw.StructureID = sid
				} else {
					st := models.Structure{
						Code: fmt.Sprintf("bp%d-%s", deps.AreaID, hydro.Slug(name)), Name: name,
						Kind: models.StructureKindEmbankment, SectorID: area.SectorID, AreaID: deps.AreaID, Origin: "DIRECTUS_BP16",
					}
					if err := deps.Structures.CreateStructure(ctx, &st); err != nil {
						return "", err
					}
					mw.StructureID = st.ID.String()
					embByKey[key] = mw.StructureID
				}
			} else {
				code := hydro.Slug(name)
				if waterCodes[code] {
					code += fmt.Sprintf("-bp%d", deps.AreaID)
				}
				wkind := ""
				if v.Vrsta != nil {
					wkind = strings.ToLower(*v.Vrsta)
				}
				w := models.Watercourse{Code: code, OfficialName: name, Name: name, Kind: wkind, Origin: "DIRECTUS_BP16"}
				if first, rest, ok := strings.Cut(name, " "); ok && hydro.NormalizeWaterKind(first) != "" && rest != "" {
					w.Name, w.Kind = rest, hydro.NormalizeWaterKind(first)
				}
				if err := deps.Waters.CreateWatercourse(ctx, &w); err != nil {
					return "", err
				}
				waterCodes[code] = true
				mw.WatercourseCode = code
			}
			if err := deps.Maintenance.UpsertWater(ctx, &mw); err != nil {
				return "", err
			}
		}
		locByKey[key] = mw.ID
		locFor[id] = mw.ID
		return mw.ID, nil
	}

	gauges := "belisce, osijek, batina"
	for _, prog := range []struct{ collection, stavke, program, kind, title string }{
		{"a02_evidencija_radova", "a02_stavke", models.ProgramA02, models.JournalKindMaintenanceA02, "A.02. Kanali I. i II. reda"},
		{"a03_evidencija_radova", "a03_stavke", models.ProgramA03, models.JournalKindMaintenanceA03, "A.03. Kanali III. i IV. reda"},
	} {
		// upis pokazuje na stavku godine, stavka na vodu
		var stavke []stavkaRow
		if err := fetchInto(ctx, src, prog.stavke, &stavke); err != nil {
			return rep, err
		}
		vodaOfStavka := map[int]int{}
		for _, st := range stavke {
			if st.Voda != nil {
				vodaOfStavka[st.ID] = *st.Voda
			}
		}
		var rows []evRow
		if err := fetchInto(ctx, src, prog.collection, &rows); err != nil {
			return rep, err
		}
		for i := range rows {
			if rows[i].NazivVode != nil {
				if v, ok := vodaOfStavka[*rows[i].NazivVode]; ok {
					rows[i].NazivVode = &v
				} else {
					rows[i].NazivVode = nil
				}
			}
		}
		rep.Records += len(rows)
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Datum != rows[j].Datum {
				return rows[i].Datum < rows[j].Datum
			}
			return rows[i].ID < rows[j].ID
		})

		// po godini jedan dnevnik
		byYear := map[string][]evRow{}
		var years []string
		for _, r := range rows {
			y := r.Datum[:4]
			if _, ok := byYear[y]; !ok {
				years = append(years, y)
			}
			byYear[y] = append(byYear[y], r)
		}
		sort.Strings(years)
		for _, y := range years {
			yearN, _ := strconv.Atoi(y)
			j := models.Journal{
				ID: db.StableID("bp16-journal", prog.program+"-"+y).String(), AreaID: deps.AreaID, Kind: prog.kind,
				Title: prog.title + " — rekonstrukcija " + y + ".", Year: yearN, Reconstruction: true, Gauges: gauges,
				Investor: "Hrvatske vode, Ulica grada Vukovara 220, 10000 Zagreb",
				Notes:    "Rekonstrukcija iz evidencije radova app.bp16.xyz. Stvarni listovi dnevnika vođeni su u Excelu i ovjereni; ovaj dnevnik služi samo kao primjer.",
			}
			first, _ := time.ParseInLocation("2006-01-02", byYear[y][0].Datum, models.Zagreb)
			last, _ := time.ParseInLocation("2006-01-02", byYear[y][len(byYear[y])-1].Datum, models.Zagreb)
			j.StartedAt, j.EndedAt = &first, &last
			rep.Journals++
			if !deps.DryRun {
				if err := deps.Journals.SaveJournal(ctx, &j); err != nil {
					return rep, err
				}
			}

			sheetNo, entryNo := 0, 0
			var curSheet *models.JournalSheet
			curCount, chunk := 0, 0
			curDay := ""
			openSheet := func(day string) error {
				sheetNo++
				chunk++
				d, _ := time.ParseInLocation("2006-01-02", day, models.Zagreb)
				sh := &models.JournalSheet{
					ID:        db.StableID("bp16-sheet", prog.program+"-"+day+"-"+strconv.Itoa(chunk)).String(),
					JournalID: j.ID, Number: sheetNo, Date: d, WeatherSource: "UVOZ", CreatedBy: "uvoz",
				}
				if w, ok := weatherFor[day]; ok {
					if w.Opisno != nil {
						parseWeather(sh, *w.Opisno)
					}
					if w.Vodostaji != nil {
						sh.WaterLevels = strings.TrimSpace(*w.Vodostaji)
					}
				}
				rep.Sheets++
				if !deps.DryRun {
					if err := deps.Journals.SaveSheet(ctx, sh); err != nil {
						return err
					}
				}
				curSheet, curCount = sh, 0
				// prvi upis: napomena o rekonstrukciji
				entryNo++
				rep.Notes++
				if !deps.DryRun {
					note := models.JournalEntry{
						ID: db.StableID("bp16-note", sh.ID).String(), JournalID: j.ID, SheetID: sh.ID, Number: entryNo, Date: d,
						Kind: models.EntryKindNote, Side: models.EntrySideSupervisor, Text: ReconstructionNote,
						UserName: "goCOP — uvoz iz evidencije", CreatedAt: d.Add(6 * time.Hour).UTC(),
					}
					if err := deps.Journals.SaveEntry(ctx, &note); err != nil {
						return err
					}
				}
				return nil
			}

			for _, r := range byYear[y] {
				if r.Datum != curDay {
					curDay, chunk = r.Datum, 0
					curSheet = nil
				}
				text := ""
				if r.Upis != nil {
					text = strings.TrimSpace(*r.Upis)
				}
				user, hasUser := userRow{}, false
				if r.UserCreated != nil {
					user, hasUser = users[*r.UserCreated]
				}
				if !hasUser {
					rep.NoUser++
				}
				loc := ""
				if r.NazivVode != nil {
					l, err := ensureLocation(*r.NazivVode)
					if err != nil {
						return rep, err
					}
					loc = l
				}
				if loc == "" {
					rep.Unmatched++
				}
				side, kind := models.EntrySideContractor, models.EntryKindWork
				// nadzor: osoba Hrvatskih voda, ili opći upis (voda "Upis"); prazan
				// naziv kanala je izvođačev rad bez lokacije
				general := r.NazivVode != nil && strings.EqualFold(strings.TrimSpace(vodaByID[*r.NazivVode].Voda), "Upis")
				if strings.HasSuffix(strings.ToLower(user.Email), "@voda.hr") || general {
					side, kind = models.EntrySideSupervisor, models.EntryKindNote
				}
				// nadzor ne troši mjesta, izvođač otvara list po šest
				if curSheet == nil || (side == models.EntrySideContractor && curCount >= 6) {
					if err := openSheet(r.Datum); err != nil {
						return rep, err
					}
				}
				if side == models.EntrySideContractor {
					curCount++
				} else {
					rep.Supervisor++
				}
				entryNo++
				rep.Entries++
				rep.PerYear[prog.program+" "+y]++
				if deps.DryRun {
					continue
				}
				name := strings.TrimSpace(user.FirstName + " " + user.LastName)
				if name == "" {
					name = "nepoznat upisivač"
				}
				created := curSheet.Date.Add(7 * time.Hour).UTC()
				if r.DateCreated != nil {
					if t, err := time.Parse(time.RFC3339, *r.DateCreated); err == nil {
						created = t.UTC()
					}
				}
				place := ""
				if r.Lokacija != nil {
					place = strings.TrimSpace(*r.Lokacija)
				}
				e := models.JournalEntry{
					ID: db.StableID("bp16-entry", prog.collection+"-"+strconv.Itoa(r.ID)).String(), JournalID: j.ID, SheetID: curSheet.ID,
					Number: entryNo, Date: curSheet.Date, Kind: kind, Side: side, MaintainedWaterID: loc, Place: place, Text: text,
					UserName: name, CreatedAt: created,
				}
				if err := deps.Journals.SaveEntry(ctx, &e); err != nil {
					return rep, err
				}
			}
		}
	}
	return rep, nil
}

// distanceFromSeven mjeri koliko je sat zapisa daleko od sedam ujutro
func distanceFromSeven(hhmmss string) int {
	h, _ := strconv.Atoi(strings.SplitN(hhmmss, ":", 2)[0])
	d := h - 7
	if d < 0 {
		d = -d
	}
	return d
}
