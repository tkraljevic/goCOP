// Package bp16 uvozi očitanja vodostaja iz Directus evidencije VGI Baranja
// (BP 16). Evidencija ima tri tablice očitanja — na crpnim stanicama, na
// ustavama i na ostalim vodomjerima — koje ovdje postaju jedan tok očitanja
// vezan na objekte i postaje iz naših registara.
//
// Uvoz se smije ponavljati: identitet svakog očitanja izveden je iz
// Directus identifikatora, pa ponovni uvoz preskače što već ima i donosi
// samo nova jutarnja očitanja dok stari sustav još radi.
package bp16

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gocop/internal/db"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Source daje zapise jedne Directus zbirke
type Source interface {
	Items(ctx context.Context, collection string) ([]json.RawMessage, error)
}

// HTTPSource čita izravno iz Directusa, samo GET, stranicu po stranicu
type HTTPSource struct {
	URL    string
	Token  string
	Client *http.Client
}

func (s HTTPSource) Items(ctx context.Context, collection string) ([]json.RawMessage, error) {
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	const page = 1000
	var out []json.RawMessage
	for offset := 0; ; offset += page {
		u := strings.TrimRight(s.URL, "/") + "/items/" + url.PathEscape(collection) +
			"?limit=" + strconv.Itoa(page) + "&offset=" + strconv.Itoa(offset) + "&sort=" + url.QueryEscape(primaryKey(collection))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+s.Token)
		req.Header.Set("User-Agent", "gocop-import/0.1")
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", collection, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s: Directus odgovorio %d: %.200s", collection, resp.StatusCode, body)
		}
		var env struct {
			Data []json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			return nil, fmt.Errorf("%s: %w", collection, err)
		}
		out = append(out, env.Data...)
		if len(env.Data) < page {
			return out, nil
		}
	}
}

// primaryKey je stupac po kojem se zbirka listа; većina ih ima "id", a
// tablice stavki vlastiti ključ
func primaryKey(collection string) string {
	switch collection {
	case "a02_stavke", "a03_stavke":
		return "id_stavke"
	case "evidencije_obilaska":
		return "id_obilasci"
	}
	return "id"
}

// DirSource čita ranije skinute datoteke <dir>/<zbirka>.json (za probu i rad bez mreže)
type DirSource struct{ Dir string }

func (s DirSource) Items(_ context.Context, collection string) ([]json.RawMessage, error) {
	raw, err := os.ReadFile(filepath.Join(s.Dir, collection+".json"))
	if err != nil {
		return nil, err
	}
	var out []json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", collection, err)
	}
	return out, nil
}

// LoadEnv čita DIRECTUS_URL i DIRECTUS_TOKEN iz datoteke oblika KLJUČ=vrijednost
func LoadEnv(path string) (HTTPSource, error) {
	f, err := os.Open(path)
	if err != nil {
		return HTTPSource{}, err
	}
	defer f.Close()
	var src HTTPSource
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.TrimSpace(k) {
		case "DIRECTUS_URL":
			src.URL = v
		case "DIRECTUS_TOKEN":
			src.Token = v
		}
	}
	if src.URL == "" || src.Token == "" {
		return src, fmt.Errorf("%s ne sadrži DIRECTUS_URL i DIRECTUS_TOKEN", path)
	}
	return src, nil
}

// Deps su spremišta u koja uvoz piše
type Deps struct {
	Readings   *repository.ReadingRepository
	Stations   *repository.StationRepository
	Structures *repository.StructureRepository
	Log        func(format string, args ...any)
}

// Report je sažetak jednog uvoza
type Report struct {
	Fetched     int
	Inserted    int
	Skipped     int // već postojala
	Unmapped    map[string]int
	NewStations []string
}

type lookupRow struct {
	ID     int    `json:"id"`
	Naziv  string `json:"naziv"`
	Status string `json:"status"`
}

type csRow struct {
	ID          int     `json:"id"`
	CS          int     `json:"crpna_stanica"`
	Datum       string  `json:"datum"`
	Vrijeme     string  `json:"vrijeme"`
	Stanje      string  `json:"stanje_cs"`
	Vodostaj    *int    `json:"vodostaj"`
	Ag1         *int    `json:"ag_1"`
	Ag2         *int    `json:"ag_2"`
	Ag3         *int    `json:"ag_3"`
	Napomena    *string `json:"napomena"`
	DateCreated string  `json:"date_created"`
}

type ustavaRow struct {
	ID          int     `json:"id"`
	Ustava      int     `json:"ustava"`
	Datum       string  `json:"datum"`
	Vrijeme     string  `json:"vrijeme"`
	Uzvodni     *int    `json:"vodostaj_uzvodni"`
	Nizvodni    *int    `json:"vodostaj_nizvodni"`
	Zapornica   *string `json:"zapornica"`
	Napomena    *string `json:"napomena"`
	DateCreated string  `json:"date_created"`
}

type ostaliRow struct {
	ID          int     `json:"id"`
	Vodomjer    int     `json:"vodomjer"`
	Datum       string  `json:"datum"`
	Vrijeme     string  `json:"vrijeme"`
	Vodostaj    *int    `json:"vodostaj"`
	Napomena    *string `json:"napomena"`
	DateCreated string  `json:"date_created"`
}

// stationSpec kaže na koju našu postaju ide Directus vodomjer; postaje kojih
// nema u registru (mađarske uzvodne, letve na Dunavu) uvoz stvara.
type stationSpec struct {
	Code        string
	Name        string
	Watercourse string
	Note        string
}

var gaugeStations = map[string]stationSpec{
	"Dunav - Batina":                     {Code: "batina"},
	"Dunav - Tikveš":                     {Code: "tikves"},
	"Drava - Belišće":                    {Code: "belisce"},
	"Drava - Osijek":                     {Code: "osijek"},
	"P. Karašica - Branjin vrh":          {Code: "branjin-vrh"},
	"P. Karašica - Popovac":              {Code: "popovac"},
	"O.K. Karašica - Luč":                {Code: "luc"},
	"Drava - CS Velika":                  {Code: "cs-velika"},
	"Dunav - Mohacs (HU)":                {Code: "mohacs", Name: "Mohács (HU)", Watercourse: "Dunav", Note: "Mađarska uzvodna postaja na Dunavu; očitanja se prepisuju iz mađarskog hidrološkog servisa."},
	"Dunav - Budapest (HU)":              {Code: "budapest", Name: "Budapest (HU)", Watercourse: "Dunav", Note: "Mađarska uzvodna postaja na Dunavu; očitanja se prepisuju iz mađarskog hidrološkog servisa."},
	"Menetfok":                           {Code: "menetfok", Name: "Menetfok (HU)", Note: "Mađarska uzvodna postaja iz evidencije VGI Baranja; voda nije utvrđena."},
	"Dunav - Zlatna Greda":               {Code: "zlatna-greda", Name: "Zlatna Greda", Watercourse: "Dunav", Note: "Letva na Dunavu kod CS Zlatna Greda, iz evidencije VGI Baranja."},
	"Dunav - Zmajevac":                   {Code: "dunav-zmajevac", Name: "Zmajevac (Dunav)", Watercourse: "Dunav", Note: "Letva na Dunavu kod Zmajevca, iz evidencije VGI Baranja; nije isto što i letva na lateralnom kanalu."},
	"P. Karašica - Villány (HU)":         {Code: "villany", Name: "Villány (HU)", Watercourse: "Karašica", Note: "Mađarska uzvodna postaja na Karašici."},
	"P. Karašica - Szederkény (HU)":      {Code: "szederkeny", Name: "Szederkény (HU)", Watercourse: "Karašica", Note: "Mađarska uzvodna postaja na Karašici."},
	"Dunav - Sakadaš (Kopačevo vanjski)": {Code: "sakadas", Name: "Sakadaš (Kopačevo vanjski)", Watercourse: "Dunav", Note: "Vanjska letva kod Kopačeva, iz evidencije VGI Baranja."},
}

var (
	// "Očitao Ime Prezime. ostatak" — osoba su prve dvije riječi (ili jedna,
	// kad druga ne počinje velikim slovom), ostatak je napomena
	reObserver      = regexp.MustCompile(`^[Oo]čita[ol]a?\s+(\p{L}+(?:\s+\p{Lu}\p{L}*)?)\s*(.*)$`)
	reObserverRadio = regexp.MustCompile(`^[Oo]čita[ol]a?\s+i\s+(radio|pokrenuo crpku|zaustavio crpku|pokrenuo CS|pokrenio|pokrenu crpku)\s+(\p{L}+(?:\s+\p{Lu}\p{L}*)?)\s*(.*)$`)
)

// splitNote razdvaja "Očitao Ime Prezime. ostatak" na osobu i napomenu
func splitNote(raw *string) (observer, note string) {
	if raw == nil {
		return "", ""
	}
	s := strings.TrimSpace(*raw)
	if s == "" || s == "0" {
		return "", ""
	}
	if m := reObserverRadio.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[2]), joinNote("i "+m[1], tidyRest(m[3]))
	}
	if m := reObserver.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1]), tidyRest(m[2])
	}
	return "", s
}

// tidyRest skida uvodnu interpunkciju i zagrade oko ostatka napomene
func tidyRest(rest string) string {
	rest = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(rest), ".,;:-–"))
	if strings.HasPrefix(rest, "(") && strings.HasSuffix(rest, ")") {
		rest = strings.TrimSpace(rest[1 : len(rest)-1])
	}
	return strings.TrimSpace(strings.TrimRight(rest, "."))
}

func mapState(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "mirovanje":
		return models.StructureStateIdle
	case "pokretanje cs", "pokretanje":
		return models.StructureStateStarting
	case "zaustavljanje cs", "zaustavljanje":
		return models.StructureStateStopping
	case "sifoniranje cs", "sifoniranje":
		return models.StructureStateSiphoning
	case "kvar":
		return models.StructureStateFault
	}
	return ""
}

func mapGate(raw *string) (gate, extra string) {
	if raw == nil {
		return "", ""
	}
	switch strings.ToLower(strings.TrimSpace(*raw)) {
	case "z":
		return models.GateClosed, ""
	case "o":
		return models.GateOpen, ""
	case "do", "dz":
		return models.GatePartial, ""
	case "":
		return "", ""
	}
	return "", "zapornica: " + *raw
}

func measured(datum, vrijeme string) (time.Time, error) {
	if vrijeme == "" {
		vrijeme = "07:00:00"
	}
	return time.ParseInLocation("2006-01-02 15:04:05", datum+" "+vrijeme, models.Zagreb)
}

func created(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return time.Now().UTC()
}

func joinNote(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "; ")
}

func sourceFor(note string) string {
	if strings.EqualFold(strings.TrimSpace(note), "telemetrija") {
		return models.ReadingSourceAutomatic
	}
	return models.ReadingSourceManual
}

// Run izvodi uvoz: pročita šifrarnike, preslika ih na naše registre, pa
// očitanja upiše u serijama po 1000 s verzijama u knjizi
func Run(ctx context.Context, src Source, deps Deps) (Report, error) {
	rep := Report{Unmapped: map[string]int{}}
	logf := deps.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}

	structures, err := deps.Structures.ListStructures(ctx, "", 16, "", "")
	if err != nil {
		return rep, err
	}
	structByName := map[string]string{}
	for _, st := range structures {
		structByName[strings.ToLower(st.Name)] = st.ID.String()
	}
	resolveStructure := func(collection string, rows []lookupRow) (map[int]string, error) {
		out := map[int]string{}
		for _, r := range rows {
			if id, ok := structByName[strings.ToLower(strings.TrimSpace(r.Naziv))]; ok {
				out[r.ID] = id
			} else if r.Status != "archived" {
				logf("Uvoz BP16: %s „%s“ nema objekt u registru — očitanja se preskaču", collection, r.Naziv)
			}
		}
		return out, nil
	}

	var csLookup, ustavaLookup, gaugeLookup []lookupRow
	if err := fetchInto(ctx, src, "crpne_stanice", &csLookup); err != nil {
		return rep, err
	}
	if err := fetchInto(ctx, src, "ustave", &ustavaLookup); err != nil {
		return rep, err
	}
	if err := fetchInto(ctx, src, "vodomjerna_letva", &gaugeLookup); err != nil {
		return rep, err
	}
	csMap, _ := resolveStructure("crpna stanica", csLookup)
	ustavaMap, _ := resolveStructure("ustava", ustavaLookup)

	gaugeMap := map[int]string{}
	for _, g := range gaugeLookup {
		spec, ok := gaugeStations[strings.TrimSpace(g.Naziv)]
		if !ok {
			logf("Uvoz BP16: vodomjer „%s“ nije preslikan na postaju — očitanja se preskaču", g.Naziv)
			continue
		}
		st, err := deps.Stations.GetStationByCode(ctx, spec.Code)
		if err != nil {
			return rep, err
		}
		if st == nil {
			if spec.Name == "" {
				logf("Uvoz BP16: postaja „%s“ (%s) ne postoji u registru — očitanja se preskaču", g.Naziv, spec.Code)
				continue
			}
			st = &models.Station{
				ID: db.StableID("station", spec.Code), Code: spec.Code, Name: spec.Name,
				Watercourse: spec.Watercourse, WatercourseSource: models.WatercourseFromName,
				SourceName: g.Naziv, Notes: spec.Note, NeedsReview: true,
				ReviewNote:      "postaja uvezena iz evidencije VGI Baranja bez pragova obrane",
				ZeroDatumSystem: "TRST", ZeroDatumNewSystem: "HVRS71",
			}
			if err := deps.Stations.CreateStation(ctx, st); err != nil {
				return rep, fmt.Errorf("stvaranje postaje %s: %w", spec.Code, err)
			}
			rep.NewStations = append(rep.NewStations, spec.Name)
			logf("Uvoz BP16: stvorena postaja „%s“ (%s)", spec.Name, spec.Code)
		}
		gaugeMap[g.ID] = st.ID.String()
	}

	existing, err := deps.Readings.ExistingIDs(ctx, models.ReadingOriginBP16)
	if err != nil {
		return rep, err
	}

	var batch []models.Reading
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n, err := deps.Readings.ImportBatch(ctx, batch)
		rep.Inserted += n
		batch = batch[:0]
		return err
	}
	add := func(rd models.Reading) error {
		rep.Fetched++
		if existing[rd.ID.String()] {
			rep.Skipped++
			return nil
		}
		batch = append(batch, rd)
		if len(batch) >= 1000 {
			return flush()
		}
		return nil
	}

	// 1. Crpne stanice
	var csRows []csRow
	if err := fetchInto(ctx, src, "vodostaji_na_crpnim_stanicama", &csRows); err != nil {
		return rep, err
	}
	for _, r := range csRows {
		sid, ok := csMap[r.CS]
		if !ok {
			rep.Unmapped["crpna_stanica:"+strconv.Itoa(r.CS)]++
			continue
		}
		at, err := measured(r.Datum, r.Vrijeme)
		if err != nil {
			rep.Unmapped["neispravan datum"]++
			continue
		}
		observer, note := splitNote(r.Napomena)
		rd := models.Reading{
			ID: db.StableID("reading-bp16-cs", strconv.Itoa(r.ID)), StructureID: sid, MeasuredAt: at.UTC(),
			LevelCm: r.Vodostaj, Source: sourceFor(note), Origin: models.ReadingOriginBP16,
			SourceRef: "directus:vodostaji_na_crpnim_stanicama:" + strconv.Itoa(r.ID),
			Observer:  observer, StructureState: mapState(r.Stanje), AgHours1: r.Ag1, AgHours2: r.Ag2, AgHours3: r.Ag3,
			Note: note, CreatedAt: created(r.DateCreated),
		}
		rd.UpdatedAt = rd.CreatedAt
		if err := add(rd); err != nil {
			return rep, err
		}
	}

	// 2. Ustave
	var ustavaRows []ustavaRow
	if err := fetchInto(ctx, src, "vodostaji_na_ustavama", &ustavaRows); err != nil {
		return rep, err
	}
	for _, r := range ustavaRows {
		sid, ok := ustavaMap[r.Ustava]
		if !ok {
			rep.Unmapped["ustava:"+strconv.Itoa(r.Ustava)]++
			continue
		}
		at, err := measured(r.Datum, r.Vrijeme)
		if err != nil {
			rep.Unmapped["neispravan datum"]++
			continue
		}
		observer, note := splitNote(r.Napomena)
		gate, extra := mapGate(r.Zapornica)
		rd := models.Reading{
			ID: db.StableID("reading-bp16-ustava", strconv.Itoa(r.ID)), StructureID: sid, MeasuredAt: at.UTC(),
			LevelCm: r.Uzvodni, Level2Cm: r.Nizvodni, Source: sourceFor(note), Origin: models.ReadingOriginBP16,
			SourceRef: "directus:vodostaji_na_ustavama:" + strconv.Itoa(r.ID),
			Observer:  observer, Gate: gate, Note: joinNote(note, extra), CreatedAt: created(r.DateCreated),
		}
		rd.UpdatedAt = rd.CreatedAt
		if err := add(rd); err != nil {
			return rep, err
		}
	}

	// 3. Ostali vodomjeri
	var ostaliRows []ostaliRow
	if err := fetchInto(ctx, src, "ostali_vodostaji", &ostaliRows); err != nil {
		return rep, err
	}
	for _, r := range ostaliRows {
		sid, ok := gaugeMap[r.Vodomjer]
		if !ok {
			rep.Unmapped["vodomjer:"+strconv.Itoa(r.Vodomjer)]++
			continue
		}
		at, err := measured(r.Datum, r.Vrijeme)
		if err != nil {
			rep.Unmapped["neispravan datum"]++
			continue
		}
		observer, note := splitNote(r.Napomena)
		rd := models.Reading{
			ID: db.StableID("reading-bp16-vodomjer", strconv.Itoa(r.ID)), StationID: sid, MeasuredAt: at.UTC(),
			LevelCm: r.Vodostaj, Source: sourceFor(note), Origin: models.ReadingOriginBP16,
			SourceRef: "directus:ostali_vodostaji:" + strconv.Itoa(r.ID),
			Observer:  observer, Note: note, CreatedAt: created(r.DateCreated),
		}
		rd.UpdatedAt = rd.CreatedAt
		if err := add(rd); err != nil {
			return rep, err
		}
	}
	if err := flush(); err != nil {
		return rep, err
	}
	return rep, nil
}

func fetchInto(ctx context.Context, src Source, collection string, dst any) error {
	items, err := src.Items(ctx, collection)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

// Summary je jedan redak izvješća za dnevnik
func (r Report) Summary() string {
	s := fmt.Sprintf("pročitano %d, upisano %d, već postojalo %d", r.Fetched, r.Inserted, r.Skipped)
	if len(r.NewStations) > 0 {
		s += ", nove postaje: " + strings.Join(r.NewStations, ", ")
	}
	if len(r.Unmapped) > 0 {
		var parts []string
		for k, v := range r.Unmapped {
			parts = append(parts, fmt.Sprintf("%s (%d)", k, v))
		}
		s += ", preskočeno bez veze: " + strings.Join(parts, ", ")
	}
	return s
}
