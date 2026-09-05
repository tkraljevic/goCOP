package models

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Građevinski dnevnik: onaj koji izvođač vodi svaki dan, a Hrvatske vode
// nadziru. Tri su vrste, sve po branjenom području: dnevnik održavanja voda
// I. i II. reda (A.02), dnevnik održavanja kanala III. i IV. reda (A.03) i
// dnevnik obrane od poplava, koji se vodi po dionici dok traju mjere.
//
// Dnevnik ima naslovnicu (tko izvodi, tko nadzire, po kojem aktu), za svaki
// dan jedan list s uvjetima, osobljem i strojevima, a na listu upise: rad
// izvođača, te napomene, naloge i ocjene ovlaštenika ili rukovoditelja.
// Količine se ne upisuju — one idu u građevinsku knjigu.
type Journal struct {
	ID       string `json:"id"`
	AreaID   int    `json:"area_id"`
	Kind     string `json:"kind"`  // JournalKind*
	Title    string `json:"title"` // naziv radova, npr. "A.02. Kanali I. i II. reda"
	Year     int    `json:"year"`
	Contract string `json:"contract"` // klasa/urbroj ugovora ili naziv

	// Rekonstrukcija: upisi preneseni iz starije evidencije radi primjera;
	// stvarni, ovjereni listovi postoje zasebno i ovaj ih dnevnik ne zamjenjuje
	Reconstruction bool `json:"reconstruction"`

	// Dnevnik obrane vodi se po dionici, po potrebi i po objektu
	SectionCode string `json:"section_code,omitempty"`
	StructureID string `json:"structure_id,omitempty"`

	// Naslovnica
	Contractor        string `json:"contractor"`          // naziv i sjedište izvođača
	ContractorLead    string `json:"contractor_lead"`     // voditelj usluga / osoba koja vodi izvođenje
	ContractorLeadAct string `json:"contractor_lead_act"` // akt o imenovanju
	Supervisor        string `json:"supervisor"`          // ovlaštenik za praćenje ugovora / nadzorni inženjer
	SupervisorAct     string `json:"supervisor_act"`
	SupervisorDeputy  string `json:"supervisor_deputy"`
	ChiefSupervisor   string `json:"chief_supervisor"` // glavni nadzorni inženjer
	Investor          string `json:"investor"`

	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`

	// Za vremenske prilike i vodostaje na listu
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
	Gauges    string   `json:"gauges"` // šifre postaja odvojene zarezom

	Notes     string    `json:"notes"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Izvedeno pri čitanju
	AreaName    string `json:"-"`
	SheetCount  int    `json:"-"`
	LastSheetOn string `json:"-"`
	OpenTasks   int    `json:"-"`
}

// Vrste dnevnika
const (
	JournalKindMaintenanceA02 = "ODRZAVANJE_A02"
	JournalKindMaintenanceA03 = "ODRZAVANJE_A03"
	JournalKindDefense        = "OBRANA"
)

// JournalKinds su vrste redom kojim ih obrazac nudi
var JournalKinds = []string{JournalKindMaintenanceA02, JournalKindMaintenanceA03, JournalKindDefense}

// IsJournalKind javlja je li vrsta poznata
func IsJournalKind(kind string) bool {
	for _, k := range JournalKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// JournalKindLabel vraća vrstu za prikaz
func JournalKindLabel(kind string) string {
	switch kind {
	case JournalKindMaintenanceA02:
		return "Održavanje A.02 — vode I. i II. reda"
	case JournalKindMaintenanceA03:
		return "Održavanje A.03 — kanali III. i IV. reda"
	case JournalKindDefense:
		return "Obrana od poplava"
	}
	return kind
}

// KindLabel vraća vrstu dnevnika za prikaz
func (j Journal) KindLabel() string { return JournalKindLabel(j.Kind) }

// IsDefense govori vodi li se dnevnik po dionici dok traju mjere obrane
func (j Journal) IsDefense() bool { return j.Kind == JournalKindDefense }

// Program vraća program radova kojem dnevnik pripada
func (j Journal) Program() string {
	switch j.Kind {
	case JournalKindMaintenanceA02, JournalKindDefense:
		return "A.02"
	case JournalKindMaintenanceA03:
		return "A.03"
	}
	return ""
}

// GaugeCodes vraća šifre postaja s naslovnice
func (j Journal) GaugeCodes() []string {
	var out []string
	for _, c := range strings.Split(j.Gauges, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// DisplayTitle je naziv dnevnika u popisu
func (j Journal) DisplayTitle() string {
	if j.Title != "" {
		return j.Title
	}
	t := j.KindLabel()
	if j.SectionCode != "" {
		t += " · " + j.SectionCode
	}
	return t
}

// JournalSheet je jedan list dnevnika: jedna ekipa jednog dana. Kad na
// istom danu radi više ekipa s različitim strojevima, svaka ima svoj list,
// kao i u tiskanom dnevniku (listovi 112 i 113 istog datuma).
type JournalSheet struct {
	ID        string    `json:"id"`
	JournalID string    `json:"journal_id"`
	Number    int       `json:"number"` // redni broj lista
	Date      time.Time `json:"date"`   // dan, bez sata
	Label     string    `json:"label"`  // ekipa ili gradilište, kad ih je više u danu

	// Uvjeti
	Conditions    string   `json:"conditions"` // opisno: toplo, kišovito, visok vodostaj...
	Temperature   *float64 `json:"temperature,omitempty"`
	WindFrom      *float64 `json:"wind_from,omitempty"` // m/s
	WindTo        *float64 `json:"wind_to,omitempty"`
	Pressure      *float64 `json:"pressure,omitempty"`      // hPa
	Precipitation *float64 `json:"precipitation,omitempty"` // mm
	WeatherSource string   `json:"weather_source"`          // odakle vremenske prilike: OPEN_METEO, RUČNO
	WaterLevels   string   `json:"water_levels"`            // "Drava - Belišće: 276 cm, ..."
	Rating        int      `json:"rating"`                  // 5 odlični … 1 nemogući, 0 neocijenjeno
	RatingNote    string   `json:"rating_note"`             // obrazloženje, obvezno kad su uvjeti nemogući

	// Struktura osoblja i strojeva: "ULOGA:broj;ULOGA:broj"
	Staff    string `json:"staff"`
	Machines string `json:"machines"`

	// Potvrde lista: izvođač i nadzor
	ContractorConfirmedBy string     `json:"contractor_confirmed_by"`
	ContractorConfirmedAt *time.Time `json:"contractor_confirmed_at,omitempty"`
	SupervisorConfirmedBy string     `json:"supervisor_confirmed_by"`
	SupervisorConfirmedAt *time.Time `json:"supervisor_confirmed_at,omitempty"`

	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Izvedeno pri čitanju
	EntryCount int `json:"-"`
}

// DateKey je dan lista u obliku 2006-01-02
func (s JournalSheet) DateKey() string { return s.Date.Format("2006-01-02") }

// WeatherText slaže vremenske prilike u redak lista
func (s JournalSheet) WeatherText() string {
	var parts []string
	if s.Temperature != nil {
		parts = append(parts, fmt.Sprintf("temperatura zraka: %.0f°C", *s.Temperature))
	}
	if s.WindFrom != nil || s.WindTo != nil {
		from, to := 0.0, 0.0
		if s.WindFrom != nil {
			from = *s.WindFrom
		}
		if s.WindTo != nil {
			to = *s.WindTo
		}
		parts = append(parts, fmt.Sprintf("brzina vjetra: %.0f - %.0f m/s", from, to))
	}
	if s.Pressure != nil {
		parts = append(parts, fmt.Sprintf("tlak zraka: %.0f hPa", *s.Pressure))
	}
	if s.Precipitation != nil {
		parts = append(parts, fmt.Sprintf("oborine: %.1f mm", *s.Precipitation))
	}
	return strings.Join(parts, ", ")
}

// RatingLabel vraća ocjenu uvjeta za prikaz
func RatingLabel(n int) string {
	switch n {
	case 5:
		return "5. odlični"
	case 4:
		return "4. vrlo dobri"
	case 3:
		return "3. dobri"
	case 2:
		return "2. dovoljni"
	case 1:
		return "1. nemogući"
	}
	return "neocijenjeno"
}

// RatingLabel vraća ocjenu uvjeta lista
func (s JournalSheet) RatingLabel() string { return RatingLabel(s.Rating) }

// Ratings su ocjene uvjeta redom kojim ih obrazac nudi
var Ratings = []int{5, 4, 3, 2, 1}

// IsConfirmed govori je li list potvrdio i izvođač i nadzor
func (s JournalSheet) IsConfirmed() bool {
	return s.ContractorConfirmedAt != nil && s.SupervisorConfirmedAt != nil
}

// Count je jedna stavka strukture osoblja ili strojeva
type Count struct {
	Name string
	N    int
}

// ParseCounts čita "ULOGA:broj;ULOGA:broj"
func ParseCounts(s string) []Count {
	var out []Count
	for _, p := range strings.Split(s, ";") {
		name, n, ok := strings.Cut(p, ":")
		if !ok {
			continue
		}
		v, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil || v <= 0 {
			continue
		}
		out = append(out, Count{Name: strings.TrimSpace(name), N: v})
	}
	return out
}

// JoinCounts slaže stavke natrag u tekst; nule se ispuštaju
func JoinCounts(counts []Count) string {
	var parts []string
	for _, c := range counts {
		if c.N > 0 && c.Name != "" {
			parts = append(parts, c.Name+":"+strconv.Itoa(c.N))
		}
	}
	return strings.Join(parts, ";")
}

// CountsText slaže stavke za čitanje: "VODITELJ USLUGA: 1, STROJAR: 4"
func CountsText(s string) string {
	var parts []string
	for _, c := range ParseCounts(s) {
		parts = append(parts, fmt.Sprintf("%s: %d", c.Name, c.N))
	}
	return strings.Join(parts, ", ")
}

// StaffRoles su uloge osoblja izvođača, kako ih obrazac lista nabraja
var StaffRoles = []string{
	"VODITELJ USLUGA", "POSLOVOĐA", "VSS TEHNIČKE STRUKE", "VŠS TEHNIČKE STRUKE", "SSS TEHNIČKE STRUKE",
	"STROJAR", "VOZAČ", "VKV RADNIK", "KV RADNIK", "PKV RADNIK", "NKV RADNIK",
	"ZAVARIVAČ", "MEHANIČAR", "ELEKTRIČAR", "ZIDAR", "TESAR", "STOLAR",
}

// MachineTypes su vrste strojeva, alata i opreme iz obrasca lista
var MachineTypes = []string{
	"BAGER do 70kW", "BAGER do 100kW", "BAGER preko 100kW - duga ruka", "BAGER do 120kW", "BAGER preko 120kW",
	"KOMBINIRANI STROJ do 75kW", "UTOVARIVAČ do 75kW", "UTOVARIVAČ preko 75kW", "BULDOZER do 85kW", "BULDOZER preko 85kW",
	"VALJAK do 123 kW", "VALJAK preko 123 kW", "PUMPA ZA VODU", "KAMION DO 100kW", "KAMIONI i DAMPER preko 100kW",
	"PRIKOLICA ZA PRIJEVOZ STROJEVA", "STROJNA KRČILICA", "STROJNA MLATILICA s traktorom do 70kW",
	"STROJNA MLATILICA s traktorom preko 70kW", "TRAKTOR do 70kW", "TRAKTOR preko 70kW", "ŠKARE ZA SJEČU strojni priključak",
	"KOSILICA do 30kW", "KOSILICA preko 30kW", "PLOVNA KOSILICA", "REFULER", "VIBRONABIJAČI", "VIBRATOR",
	"AGREGAT ZA STRUJU", "MOTORNA PILA", "SJEKAČ", "TRIMER", "ČAMAC", "KOMPLET GEODETSKE OPREME",
	"PNEUMATSKI ČEKIĆ strojni priključak",
}

// ConditionWords su riječi kojima obrazac opisuje hidrometeorološke uvjete
var ConditionWords = []string{"sunčano", "toplo", "hladno", "oblačno", "vjetrovito", "kiša", "snijeg", "magla", "visok vodostaj", "nizak vodostaj"}

// JournalEntry je jedan upis na listu dnevnika.
//
// Upis nosi redni broj unutar dnevnika, bez rupa: to je kontrola da ništa
// nije obrisano. Zato se upis ne briše nego stornira — ostaje na listu s
// razlogom storniranja.
type JournalEntry struct {
	ID        string    `json:"id"`
	JournalID string    `json:"journal_id"`
	SheetID   string    `json:"sheet_id"`
	Number    int       `json:"number"` // redni broj u dnevniku
	Date      time.Time `json:"date"`
	Kind      string    `json:"kind"` // EntryKind*
	Side      string    `json:"side"` // EntrySide*: tko je upisao, izvođač ili nadzor

	// Gdje: lokacija iz popisa održavanja, ili dionica u obrani; dio i stacionaža
	MaintainedWaterID string `json:"maintained_water_id,omitempty"`
	SectionCode       string `json:"section_code,omitempty"`
	Place             string `json:"place"` // "Lijevoobalni nasip 19+430 - 23+260", "CS Draž"

	// Što: stavka radova, ili slobodan tekst
	WorkItemID string   `json:"work_item_id,omitempty"`
	Text       string   `json:"text"`
	Hours      *float64 `json:"hours,omitempty"` // trajanje, kad se bilježi

	// Nalog: rok i stanje
	DueDate *time.Time `json:"due_date,omitempty"`
	Status  string     `json:"status,omitempty"` // TaskStatus*

	ParentID string `json:"parent_id,omitempty"` // odgovor na drugi upis

	Voided     bool   `json:"voided"`
	VoidReason string `json:"void_reason,omitempty"`
	VoidedBy   string `json:"voided_by,omitempty"`

	UserID    string    `json:"user_id"`
	UserName  string    `json:"user_name"` // ime u trenutku upisa, da list ostane čitljiv i bez imenika
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Izvedeno pri čitanju
	LocationName string `json:"-"`
	WorkItemText string `json:"-"`
	WorkItemNo   string `json:"-"`
}

// Strana upisa: izvođač popunjava list, a za nadzor (ovlaštenik,
// rukovoditelj) na svakom listu ostaje prostor, kao na tiskanom obrascu
const (
	EntrySideContractor = "IZVOĐAČ"
	EntrySideSupervisor = "NADZOR"
)

// IsSupervisor govori je li upis od nadzora
func (e JournalEntry) IsSupervisor() bool { return e.Side == EntrySideSupervisor }

// Vrste upisa
const (
	EntryKindWork   = "RAD"      // rad izvođača
	EntryKindNote   = "NAPOMENA" // napomena bilo koga
	EntryKindTask   = "NALOG"    // nalog izvođaču s rokom
	EntryKindReview = "OCJENA"   // ocjena izvedenog od nadzora
)

// EntryKinds su vrste upisa redom kojim ih obrazac nudi
var EntryKinds = []string{EntryKindWork, EntryKindNote, EntryKindTask, EntryKindReview}

// EntryKindLabel vraća vrstu upisa za prikaz
func EntryKindLabel(kind string) string {
	switch kind {
	case EntryKindWork:
		return "Rad"
	case EntryKindNote:
		return "Napomena"
	case EntryKindTask:
		return "Nalog"
	case EntryKindReview:
		return "Ocjena"
	}
	return kind
}

// KindLabel vraća vrstu upisa za prikaz
func (e JournalEntry) KindLabel() string { return EntryKindLabel(e.Kind) }

// Stanja naloga
const (
	TaskOpen      = "OTVOREN"
	TaskDone      = "IZVEDEN"
	TaskCancelled = "OTKAZAN"
)

// TaskStatusLabel vraća stanje naloga za prikaz
func TaskStatusLabel(s string) string {
	switch s {
	case TaskOpen:
		return "otvoren"
	case TaskDone:
		return "izveden"
	case TaskCancelled:
		return "otkazan"
	}
	return s
}

// StatusLabel vraća stanje naloga za prikaz
func (e JournalEntry) StatusLabel() string { return TaskStatusLabel(e.Status) }

// IsTask govori je li upis nalog
func (e JournalEntry) IsTask() bool { return e.Kind == EntryKindTask }

// IsOverdue govori je li rok naloga prošao, a nalog je još otvoren
func (e JournalEntry) IsOverdue(now time.Time) bool {
	return e.IsTask() && e.Status == TaskOpen && e.DueDate != nil && e.DueDate.Before(now.Truncate(24*time.Hour))
}

// Where slaže lokaciju i dio u jedan redak
func (e JournalEntry) Where() string {
	loc := e.LocationName
	if loc == "" {
		loc = e.SectionCode
	}
	if e.Place == "" {
		return loc
	}
	if loc == "" {
		return e.Place
	}
	return loc + " · " + e.Place
}

// What vraća opis rada: stavku kad je vezana, inače tekst
func (e JournalEntry) What() string {
	if e.WorkItemText != "" && e.Text == "" {
		return e.WorkItemText
	}
	return e.Text
}
