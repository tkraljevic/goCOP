package models

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DefensePhase predstavlja fazu obrane od poplava
type DefensePhase string

const (
	PhaseUnknown   DefensePhase = "NEPOZNATO"
	PhaseNormal    DefensePhase = "NORMALNO"
	PhasePrep      DefensePhase = "PRIPREMNO"
	PhaseRegular   DefensePhase = "REDOVNA"
	PhaseEmergency DefensePhase = "IZVANREDNA"
	PhaseState     DefensePhase = "IZVANREDNO_STANJE"
)

// Tendency opisuje smjer kretanja vodostaja
type Tendency string

const (
	TendencyRising   Tendency = "RASTE"
	TendencyFalling  Tendency = "OPADA"
	TendencyStagnant Tendency = "STAGNIRA"
	TendencyUnknown  Tendency = "NEPOZNATO"
)

// Threshold predstavlja jedan prag obrane od poplava.
//
// Cm je popunjen isključivo kad je prag izražen u centimetrima na vodomjeru i
// smije ući u automatski izračun faze obrane. Pragovi zadani kao apsolutna kota
// ("206,30 m n. m.") ili kao uputa ("Prema Pravilniku akumulacije Borovik")
// ostaju sačuvani u Raw, ali se NE preračunavaju — kota nule ponegdje nedostaje
// ili je upisana kao 0,00, pa bi preračun dao krivu fazu obrane na ekranu.
type Threshold struct {
	Cm  *int   `json:"cm,omitempty"`  // vrijednost u cm na vodomjeru
	Raw string `json:"raw,omitempty"` // izvorni zapis iz dokumentacije dionice
}

// IsUsable govori smije li se prag koristiti za automatski izračun faze obrane
func (t Threshold) IsUsable() bool {
	return t.Cm != nil
}

// Label vraća zapis praga za prikaz — brojčanu vrijednost ili izvorni tekst
func (t Threshold) Label() string {
	if t.Cm != nil {
		return fmt.Sprintf("%+d cm", *t.Cm)
	}
	if raw := strings.TrimSpace(t.Raw); raw != "" {
		return raw
	}
	return "—"
}

// Station predstavlja hidrološku mjernu postaju (vodomjer) Hrvatskih voda.
// Jedna postaja mjerodavna je za više štićenih dionica — veza se drži u
// tablici section_stations, a SectionCodes popunjava repozitorij pri čitanju.
type Station struct {
	ID   uuid.UUID `json:"id"`
	Code string    `json:"code"`
	Name string    `json:"name"`
	// Watercourse je voda na kojoj vodomjer FIZIČKI STOJI (Batina i Vukovar na
	// Dunavu). To nije isto što i vode za koje je postaja mjerodavna — Batina je
	// mjerodavna i za dionice potoka Karašice, osobito na ušću u Dunav; ta veza
	// se drži u section_stations i ne smije prepisati lokaciju postaje.
	Watercourse       string `json:"watercourse"`        // naziv vode za prikaz; prazno kad nije utvrđena
	WatercourseCode   string `json:"watercourse_code"`   // veza na registar vodnih tijela; prazno kad nije uspostavljena
	WatercourseSource string `json:"watercourse_source"` // odakle je voda utvrđena (models.WatercourseFrom*)
	WaterArea         string `json:"water_area"`         // npr. Srednja Sava, Sliv Drave i Dunava
	Stationing        string `json:"stationing"`         // stacionaža vodomjera, npr. "rkm 271+900"

	// Kota nule vodomjera vodi se u dva visinska sustava. ZeroDatum je kota
	// preuzeta iz dokumentacije dionica i zapisana je u starom visinskom sustavu;
	// ZeroDatumNew je kota u novom sustavu i upisuje se ručno.
	//
	// Jedna se NE izvodi iz druge: razlika visinskih sustava je konstanta koja se
	// upisuje iz službenog izvora, a pogrešna kota nule pomiče cijelu ljestvicu
	// pragova obrane.
	ZeroDatum             *float64 `json:"zero_datum,omitempty"`
	ZeroDatumSystem       string   `json:"zero_datum_system"`
	ZeroDatumNew          *float64 `json:"zero_datum_new,omitempty"`
	ZeroDatumNewSystem    string   `json:"zero_datum_new_system"`
	ZeroDatumSource       string   `json:"zero_datum_source,omitempty"`
	ZeroDatumMethod       string   `json:"zero_datum_method,omitempty"`
	ZeroDatumSurveyDate   string   `json:"zero_datum_survey_date,omitempty"`
	ZeroDatumDocumentDate string   `json:"zero_datum_document_date,omitempty"`

	// Extremes su zabilježeni ekstremi letve: najviši i najniži vodostaj s
	// datumom. Vode se odvojeno od pragova jer nisu svi izmjereni na ovoj
	// letvi — Batina najviši vodostaj iz 1965. nema izmjeren nego preračunat iz
	// Bezdana, a prikazan kao mjerenje tvrdio bi nešto što se nije dogodilo.
	Extremes []StationExtreme `json:"extremes,omitempty"`

	// ReturnLevels su povratni vodostaji: koliko visoko voda dođe jednom u T
	// godina. Nisu mjerenje nego procjena iz niza, pa svaki nosi metodu, niz na
	// kojem je računat i granice pouzdanosti — bez toga je brojka samo tvrdnja.
	// Ne ulaze u pragove obrane: prag je propisan, povratni vodostaj proračunat.
	ReturnLevels []StationReturnLevel `json:"return_levels,omitempty"`

	// ZeroDatumHistory su promjene kote nule kroz vrijeme, od najstarije.
	// Vodostaji u bazi svi su svedeni na zadnju kotu, pa se ne preračunavaju;
	// povijest služi da se zna što je koja stara evidencija zapravo mjerila i
	// kad je letva premještena ili obnovljena.
	ZeroDatumHistory []ZeroDatumChange `json:"zero_datum_history,omitempty"`

	// OgradeNiza je ono što o pojedinom nizu iz arhive znamo, a iz brojki se
	// ne vidi: zaleđen mjerač, sumnjive zimske vrijednosti, prekid u mjerenju.
	//
	// Izdavačeva ograda dolazi s paketom i stoji uz sam niz u arhivi. Ova je
	// naša: arhiva se otvara samo za čitanje i pregrađuje se pri svakoj
	// obnovi, pa bi ondje nestala i postojala na jednom jedinom čvoru.
	OgradeNiza []OgradaNiza `json:"ograde_niza,omitempty"`

	Prep      Threshold `json:"prep"`      // Pripremno stanje
	Regular   Threshold `json:"regular"`   // Redovna obrana od poplava
	Emergency Threshold `json:"emergency"` // Izvanredna obrana od poplava
	State     Threshold `json:"state"`     // Izvanredno stanje
	Record    Threshold `json:"record"`    // Najviši zabilježeni vodostaj

	Notes      string `json:"notes"`
	SourceName string `json:"source_name"` // izvorni zapis naziva iz dokumentacije dionice

	// NeedsReview označava postaju čiji podaci nisu u cijelosti strojno
	// pročitani i traže potvrdu operatera prije oslanjanja na automatiku.
	NeedsReview bool   `json:"needs_review"`
	ReviewNote  string `json:"review_note,omitempty"`

	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	SectionCodes []string `json:"section_codes,omitempty"`
}

// WaterLevel predstavlja zapis očitanja vodostaja i protoka
type WaterLevel struct {
	ID          uuid.UUID `json:"id"`
	StationID   uuid.UUID `json:"station_id"`
	StationName string    `json:"station_name,omitempty"`
	Watercourse string    `json:"watercourse,omitempty"`
	MeasuredAt  time.Time `json:"measured_at"`
	LevelCm     int       `json:"level_cm"`
	FlowM3S     *float64  `json:"flow_m3s,omitempty"`
	Tendency    Tendency  `json:"tendency"`
	Source      string    `json:"source"` // npr. "Terensko očitanje", "Telemetrija", "DHMZ uvoz"
	CreatedAt   time.Time `json:"created_at"`
}

// StationStatus sažima trenutno operativno stanje hidrološke postaje
type StationStatus struct {
	Station           Station      `json:"station"`
	LatestMeasurement *WaterLevel  `json:"latest_measurement,omitempty"`
	CurrentPhase      DefensePhase `json:"current_phase"`
	LevelDifference   int          `json:"level_difference"` // razlika u cm u odnosu na prethodno mjerenje
}

// DiaryEntry predstavlja zapis u operativnom dnevniku Centra obrane od poplava
type DiaryEntry struct {
	ID          uuid.UUID    `json:"id"`
	Timestamp   time.Time    `json:"timestamp"`
	Author      string       `json:"author"`
	Category    string       `json:"category"` // "Mjera", "Zapovijed", "Ophodnja", "Crpna stanica", "Vreće", "Incident", "Opažanje"
	Location    string       `json:"location"` // Dionica, stacionaža ili lokacija
	StationID   *uuid.UUID   `json:"station_id,omitempty"`
	StationName string       `json:"station_name,omitempty"`
	Phase       DefensePhase `json:"phase"`
	Description string       `json:"description"`
	CreatedAt   time.Time    `json:"created_at"`
}

// Visinski sustavi u kojima se vodi kota nule vodomjera.
//
// Kote iz dokumentacije dionica su u starom sustavu (Trst, prema mareografu
// u Trstu). Novi sustav je HVRS71; te kote se tek trebaju izmjeriti i
// upisuju se ručno — ne izvode se preračunom.
const (
	ZeroDatumSystemOld = "TRST"
	ZeroDatumSystemNew = "HVRS71"
)

// Odakle je utvrđena voda na kojoj postaja stoji. Vodotok se upisuje samo kad ga
// dokumentacija tvrdi; kad je neodređen, ostaje prazan umjesto da se pogodi iz
// dionice, jer dionica govori za što je postaja mjerodavna, a ne gdje stoji.
const (
	WatercourseFromName       = "NAZIV"      // rijeka navedena u nazivu vodomjera
	WatercourseFromStationing = "STACIONAŽA" // stacionaža upada u raspon dionica jedne vode
	WatercourseFromSections   = "DIONICE"    // sve dionice postaje su na istoj vodi
	WatercourseFromOperator   = "OPERATER"   // ručno potvrdio operater
	WatercourseUndetermined   = ""           // nije utvrđeno — popunjava operater
)

// PodrijetloVodotoka kaže kako je utvrđeno na kojoj vodi letva stoji. Stoji uz
// naziv vode jer nije svejedno je li ga netko potvrdio ili ga je program
// pogodio iz naziva vodomjera.
func (s Station) PodrijetloVodotoka() string {
	switch s.WatercourseSource {
	case WatercourseFromName:
		return "iz naziva vodomjera"
	case WatercourseFromStationing:
		return "iz stacionaže"
	case WatercourseFromSections:
		return "izvedeno iz dionica"
	case WatercourseFromOperator:
		return "potvrdio operater"
	}
	return s.WatercourseSource
}

// HasWatercourse govori je li utvrđeno na kojoj vodi postaja stoji
func (s Station) HasWatercourse() bool {
	return strings.TrimSpace(s.Watercourse) != ""
}

// HasNewZeroDatum govori je li kota nule prenesena u novi visinski sustav
func (s Station) HasNewZeroDatum() bool {
	return s.ZeroDatumNew != nil
}

// KotaVode je vodna ploha u apsolutnoj visini, u jednom visinskom sustavu.
type KotaVode struct {
	Kota   float64 // metara nad morem
	Sustav string  // HVRS71 ili TRST
	Nova   bool    // je li to novi sustav
}

// Kote pretvara očitani vodostaj u apsolutnu visinu vodne plohe, u svakom
// visinskom sustavu koji letva ima. Novi sustav dolazi prvi.
//
// Zašto oba: na Batini se TRST i HVRS71 razlikuju 26,1 cm. Tko na terenu
// nivelira prema reperu u jednom sustavu, a čita kotu izračunatu u drugom,
// promašuje za tu razliku — a upravo se po toj brojci određuje koliko vreća
// treba nadvisiti obranu.
func (s Station) Kote(vodostajCm int) []KotaVode {
	var out []KotaVode
	if s.ZeroDatumNew != nil {
		sustav := s.ZeroDatumNewSystem
		if sustav == "" {
			sustav = "HVRS71"
		}
		out = append(out, KotaVode{Kota: *s.ZeroDatumNew + float64(vodostajCm)/100, Sustav: sustav, Nova: true})
	}
	if s.ZeroDatum != nil {
		sustav := s.ZeroDatumSystem
		if sustav == "" {
			sustav = "TRST"
		}
		out = append(out, KotaVode{Kota: *s.ZeroDatum + float64(vodostajCm)/100, Sustav: sustav})
	}
	return out
}

// ImaKotuNule govori može li se vodostaj uopće pretvoriti u apsolutnu visinu.
func (s Station) ImaKotuNule() bool { return s.ZeroDatumNew != nil || s.ZeroDatum != nil }

// HasUsableThresholds govori ima li postaja ijedan prag u centimetrima, tj.
// može li se za nju uopće automatski odrediti faza obrane
func (s Station) HasUsableThresholds() bool {
	return s.Prep.IsUsable() || s.Regular.IsUsable() || s.Emergency.IsUsable() || s.State.IsUsable()
}

// CalculateDefensePhase izračunava fazu obrane za očitani vodostaj.
//
// Vraća PhaseUnknown kad postaja nema nijedan prag izražen u centimetrima —
// radije nego da tišinom prijavi redovno stanje na postaji čiji pragovi nisu
// strojno čitljivi.
func (s Station) CalculateDefensePhase(levelCm int) DefensePhase {
	if !s.HasUsableThresholds() {
		return PhaseUnknown
	}
	if s.State.IsUsable() && levelCm >= *s.State.Cm {
		return PhaseState
	}
	if s.Emergency.IsUsable() && levelCm >= *s.Emergency.Cm {
		return PhaseEmergency
	}
	if s.Regular.IsUsable() && levelCm >= *s.Regular.Cm {
		return PhaseRegular
	}
	if s.Prep.IsUsable() && levelCm >= *s.Prep.Cm {
		return PhasePrep
	}
	return PhaseNormal
}

// Severity vraća težinski rang faze radi sortiranja po kritičnosti.
// Nepoznata faza dobiva -1 kako ne bi bila izjednačena s redovnim stanjem.
func (p DefensePhase) Severity() int {
	switch p {
	case PhaseState:
		return 4
	case PhaseEmergency:
		return 3
	case PhaseRegular:
		return 2
	case PhasePrep:
		return 1
	case PhaseUnknown:
		return -1
	default:
		return 0
	}
}

// BadgeClass vraća CSS klasu stila za fazu
func (p DefensePhase) BadgeClass() string {
	switch p {
	case PhaseUnknown:
		return "badge-unknown"
	case PhaseState:
		return "badge-state"
	case PhaseEmergency:
		return "badge-emergency"
	case PhaseRegular:
		return "badge-regular"
	case PhasePrep:
		return "badge-prep"
	default:
		return "badge-normal"
	}
}

// PillClass je razred pilule kojom se stupanj prikazuje. Isti raspored boja
// kao značka, ali drugo ime razreda — pilula stoji u tablici, značka u tekstu.
func (p DefensePhase) PillClass() string {
	switch p {
	case PhaseState:
		return "crit"
	case PhaseEmergency:
		return "emerg"
	case PhaseRegular:
		return "regular"
	case PhasePrep:
		return "prep"
	}
	return "none"
}

// Label vraća human-readable naziv faze na hrvatskom
func (p DefensePhase) Label() string {
	switch p {
	case PhaseUnknown:
		return "Pragovi nisu određeni"
	case PhaseState:
		return "Izvanredno stanje"
	case PhaseEmergency:
		return "Izvanredna obrana"
	case PhaseRegular:
		return "Redovna obrana"
	case PhasePrep:
		return "Pripremno stanje"
	default:
		// Ispod pripremnog stanja nema mjera obrane. Zakon poznaje četiri
		// faze — pripremno stanje, redovnu i izvanrednu obranu i izvanredno
		// stanje — pa ovo nije peta, nego njihov izostanak.
		return "bez mjera obrane"
	}
}

// InForce javlja je li na snazi neka od četiriju faza obrane. Vodostaj ispod
// pripremnog stanja i postaja bez pragova nisu faza, pa ih sučelje ne ističe.
func (p DefensePhase) InForce() bool { return p.Severity() > 0 }

// ZeroDatumChange je jedna kota nule s datumom od kojeg vrijedi.
type ZeroDatumChange struct {
	ValidFrom string   `json:"valid_from"`       // datum, YYYY-MM-DD; prazno = od početka mjerenja
	Datum     *float64 `json:"datum,omitempty"`  // kota nule u metrima
	System    string   `json:"system,omitempty"` // TRST, HVRS71 …
	Note      string   `json:"note,omitempty"`   // razlog: premještaj letve, obnova, novi elaborat
}

// ZeroDatumAt vraća kotu koja je vrijedila na dan; nil kad povijest za taj
// dan ne zna ništa. Promjene se čitaju od najstarije, pa pobjeđuje zadnja
// koja je počela vrijediti prije ili na taj dan.
func (s Station) ZeroDatumAt(day string) *ZeroDatumChange {
	var out *ZeroDatumChange
	for i := range s.ZeroDatumHistory {
		c := &s.ZeroDatumHistory[i]
		if c.ValidFrom == "" || c.ValidFrom <= day {
			out = c
		}
	}
	return out
}

// Kvaliteta zabilježene vrijednosti: je li izmjerena na ovoj letvi ili
// dobivena računom iz druge postaje.
const (
	QualityMeasured      = "IZMJERENO"
	QualityReconstructed = "REKONSTRUIRANO"
	QualityUncertain     = "SUMNJIVO"
)

// StationExtreme je zabilježeni najviši ili najniži vodostaj letve.
type StationExtreme struct {
	Kind    string `json:"kind"` // MAX ili MIN
	LevelCm *int   `json:"level_cm,omitempty"`
	OnDate  string `json:"on_date,omitempty"` // YYYY-MM-DD ili YYYY kad se zna samo godina
	Quality string `json:"quality,omitempty"` // Quality*
	Source  string `json:"source,omitempty"`  // odakle podatak: DHMZ, postaja Bezdan …
	Method  string `json:"method,omitempty"`  // kako je dobiven, kad nije izmjeren
	Note    string `json:"note,omitempty"`
}

const (
	ExtremeMax = "MAX"
	ExtremeMin = "MIN"
)

// StationReturnLevel je vodostaj koji se u prosjeku dosegne ili premaši jednom
// u Years godina. Statistička procjena iz niza, ne mjerenje: LowCm i HighCm su
// granice pouzdanosti, a Method i Series kažu čime je i na čemu računato.
//
// Stoji odvojeno od pragova obrane. Prag je propisana granica pri kojoj se
// poduzimaju mjere; povratni vodostaj govori koliko je koja visina rijetka.
// Miješanje to dvoje značilo bi da program sam sebi propisuje obranu.
type StationReturnLevel struct {
	Years      int    `json:"years"`              // povratno razdoblje T, u godinama
	LevelCm    *int   `json:"level_cm,omitempty"` // procijenjeni vodostaj na letvi
	LowCm      *int   `json:"low_cm,omitempty"`   // donja granica pouzdanosti
	HighCm     *int   `json:"high_cm,omitempty"`  // gornja granica
	Method     string `json:"method,omitempty"`   // npr. „POT, generalizirana Pareto, L-momenti"
	Series     string `json:"series,omitempty"`   // niz na kojem je računato, npr. „1902.–2026."
	Source     string `json:"source,omitempty"`   // tko je računao
	Note       string `json:"note,omitempty"`
	ComputedOn string `json:"computed_on,omitempty"` // datum izračuna, YYYY-MM-DD
}

// Label je procijenjeni vodostaj s predznakom, kako se vodostaj i inače piše.
func (r StationReturnLevel) Label() string {
	if r.LevelCm == nil {
		return "—"
	}
	return fmt.Sprintf("%+d cm", *r.LevelCm)
}

// ImaRaspon govori jesu li upisane obje granice pouzdanosti. Jedna sama ne
// znači ništa, pa se raspon ili prikazuje cijeli ili nikako.
func (r StationReturnLevel) ImaRaspon() bool { return r.LowCm != nil && r.HighCm != nil }

// RasponLabel je interval pouzdanosti; prazno kad nije upisan.
func (r StationReturnLevel) RasponLabel() string {
	if !r.ImaRaspon() {
		return ""
	}
	return fmt.Sprintf("%+d do %+d cm", *r.LowCm, *r.HighCm)
}

// GodineLabel je povratno razdoblje kako se čita: „100 godina".
func (r StationReturnLevel) GodineLabel() string {
	if r.Years <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d %s", r.Years, godina(r.Years))
}

// SansaLabel je ista brojka gledana s druge strane: povratno razdoblje od 100
// godina znači 1 % izgleda u svakoj pojedinoj godini. Ljudi povratno razdoblje
// redovito čitaju kao „neće se ponoviti idućih 100 godina", što nije isto.
func (r StationReturnLevel) SansaLabel() string {
	if r.Years <= 0 {
		return ""
	}
	p := 100 / float64(r.Years)
	// Jedna decimala je dosta i za T=1000 (0,1 %), a bez zaokruživanja bi
	// T=3 ispalo kao 33,333333333333336 %.
	txt := strconv.FormatFloat(p, 'f', 1, 64)
	txt = strings.TrimSuffix(txt, ".0")
	return strings.Replace(txt, ".", ",", 1) + " % svake godine"
}

// godina bira oblik imenice uz broj: 1 godina, 2 godine, 5 godina.
func godina(n int) string {
	if n%100 >= 11 && n%100 <= 14 {
		return "godina"
	}
	switch n % 10 {
	case 1:
		return "godina"
	case 2, 3, 4:
		return "godine"
	}
	return "godina"
}

// PovratniVodostaji vraća povratne vodostaje složene po povratnom razdoblju,
// od najčešćeg prema najrjeđem, kako se i čitaju.
func (s Station) PovratniVodostaji() []StationReturnLevel {
	out := append([]StationReturnLevel(nil), s.ReturnLevels...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Years < out[j].Years })
	return out
}

// ImaPovratne govori ima li letva ijedan izračunat povratni vodostaj.
func (s Station) ImaPovratne() bool { return len(s.ReturnLevels) > 0 }

// IsMeasured govori smije li se vrijednost predstaviti kao mjerenje ove letve.
// Prazna kvaliteta znači izmjereno: takvi su zapisi zatečeni prije nego što se
// razlika počela bilježiti.
func (e StationExtreme) IsMeasured() bool {
	return e.Quality == "" || e.Quality == QualityMeasured
}

// Label je vrijednost s predznakom, kako se vodostaj i inače piše.
func (e StationExtreme) Label() string {
	if e.LevelCm == nil {
		return "—"
	}
	return fmt.Sprintf("%+d cm", *e.LevelCm)
}

// QualityLabel je kratko objašnjenje odakle vrijednost dolazi.
func QualityLabel(q string) string {
	switch q {
	case QualityReconstructed:
		return "rekonstruirano"
	case QualityUncertain:
		return "sumnjivo"
	default:
		return "izmjereno"
	}
}

// NajviseIzmjereno vraća najviši vodostaj koji je doista izmjeren na ovoj
// letvi. To je jedina brojka koja smije ući u pragove i u izračun faze.
func (s Station) NajviseIzmjereno() *StationExtreme {
	var naj *StationExtreme
	for i, e := range s.Extremes {
		if e.Kind != ExtremeMax || !e.IsMeasured() || e.LevelCm == nil {
			continue
		}
		if naj == nil || *e.LevelCm > *naj.LevelCm {
			naj = &s.Extremes[i]
		}
	}
	return naj
}

// NajviseZabiljezeno vraća najviši vodostaj koji se za ovu letvu vodi, bez
// obzira je li mjeren ili preračunat s druge postaje.
//
// Batina ima oba: +775 cm izmjereno 14.6.2013. i +795 cm 24.6.1965.,
// preračunato iz Bezdana. Oba su točna, samo odgovaraju na različita pitanja —
// „koliko je najviše izmjereno" i „koliko je najviše bilo".
func (s Station) NajviseZabiljezeno() *StationExtreme {
	var naj *StationExtreme
	for i, e := range s.Extremes {
		if e.Kind != ExtremeMax || e.LevelCm == nil {
			continue
		}
		if naj == nil || *e.LevelCm > *naj.LevelCm {
			naj = &s.Extremes[i]
		}
	}
	return naj
}

// ImaKoordinate javlja zna li se gdje letva stoji.
func (s Station) ImaKoordinate() bool { return s.Latitude != nil && s.Longitude != nil }

// KoordinateHR ispisuje položaj u stupnjevima, minutama i sekundama, kako
// stoji u tehničkim zapisnicima DHMZ-a.
func (s Station) KoordinateHR() string {
	if !s.ImaKoordinate() {
		return ""
	}
	dms := func(v float64, poz, neg string) string {
		strana := poz
		if v < 0 {
			v, strana = -v, neg
		}
		st := int(v)
		m := int((v - float64(st)) * 60)
		sek := (v - float64(st) - float64(m)/60) * 3600
		return fmt.Sprintf("%d° %d′ %s″ %s", st, m,
			strings.Replace(strconv.FormatFloat(sek, 'f', 0, 64), ".", ",", 1), strana)
	}
	return dms(*s.Latitude, "S", "J") + "  " + dms(*s.Longitude, "I", "Z")
}

// KartaURL vodi na kartu s označenim položajem letve. Vanjska poveznica, ne
// ugrađena karta: program radi bez interneta, pa se karta otvara tek kad je
// čovjek zatraži i kad ga ima.
func (s Station) KartaURL() string {
	if !s.ImaKoordinate() {
		return ""
	}
	return fmt.Sprintf("https://www.openstreetmap.org/?mlat=%.6f&mlon=%.6f#map=16/%.6f/%.6f",
		*s.Latitude, *s.Longitude, *s.Latitude, *s.Longitude)
}

// RazlikaVisinskihSustava je koliko se dvije kote nule iste letve razlikuju.
// Računa se iz same postaje, a ne izvana: dok je stajala u podacima stranice,
// jedan pogrešan redoslijed u rukovatelju značio je da nikad ne stigne do
// prikaza i da na letvi piše 0,000 m.
func (s Station) RazlikaVisinskihSustava() float64 {
	k := s.Kote(0)
	if len(k) != 2 {
		return 0
	}
	return k[1].Kota - k[0].Kota
}

// NajnizeIzmjereno vraća najniži vodostaj doista izmjeren na ovoj letvi.
func (s Station) NajnizeIzmjereno() *StationExtreme {
	var naj *StationExtreme
	for i, e := range s.Extremes {
		if e.Kind != ExtremeMin || !e.IsMeasured() || e.LevelCm == nil {
			continue
		}
		if naj == nil || *e.LevelCm < *naj.LevelCm {
			naj = &s.Extremes[i]
		}
	}
	return naj
}

// NajnizeRekonstruirano vraća najniži vodostaj koji se vodi, a nije mjeren na
// ovoj letvi. Za Batinu je to -127 cm 7.1.1909., preračunato iz Bezdana —
// letva tada nije postojala, utemeljena je 2001.
//
// Stoji uz izmjereni, ne umjesto njega: dvije brojke odgovaraju na različita
// pitanja, „koliko je najniže izmjereno" i „koliko je najniže bilo".
func (s Station) NajnizeRekonstruirano() *StationExtreme {
	var naj *StationExtreme
	for i, e := range s.Extremes {
		if e.Kind != ExtremeMin || e.IsMeasured() || e.LevelCm == nil {
			continue
		}
		if naj == nil || *e.LevelCm < *naj.LevelCm {
			naj = &s.Extremes[i]
		}
	}
	return naj
}

// ImaNajnize govori ima li letva ijedan zabilježeni najniži vodostaj.
func (s Station) ImaNajnize() bool {
	return s.NajnizeIzmjereno() != nil || s.NajnizeRekonstruirano() != nil
}

// RekordSeRazlikuje govori je li najviši zabilježeni viši od najvišeg
// izmjerenog — tada se moraju prikazati oba, jer bi jedan bez drugoga lagao.
func (s Station) RekordSeRazlikuje() bool {
	iz, zab := s.NajviseIzmjereno(), s.NajviseZabiljezeno()
	return iz != nil && zab != nil && *zab.LevelCm > *iz.LevelCm
}

// ExtremesOf vraća ekstreme zadane vrste, redom kojim su upisani.
func (s Station) ExtremesOf(kind string) []StationExtreme {
	var out []StationExtreme
	for _, e := range s.Extremes {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// OgradaNiza je vlastita napomena uz jedan niz iz arhive. Niz se prepoznaje po
// izvoru i veličini — id se pri obnovi arhive mijenja, pa se na njega ne može
// vezati.
type OgradaNiza struct {
	Izvor    string `json:"izvor"`        // his2000, letva-dhmz, preracun-mohacs …
	Velicina string `json:"velicina"`     // vodostaj, protok, temperatura …
	Od       string `json:"od,omitempty"` // na koje se razdoblje odnosi; prazno = na cijeli niz
	Do       string `json:"do,omitempty"`
	// Ispod i Iznad sužavaju ogradu na raspon vrijednosti, u jedinici veličine.
	// Tlačna sonda ne laže cijelo vrijeme nego tek kad joj voda pobjegne ispod
	// usisa; bez te granice ograda bi obezvrijedila i ono što je niz dobro
	// izmjerio. Vukovar 2026.: ispod 100 cm sonda je bila na suhom.
	Ispod *float64 `json:"ispod,omitempty"`
	Iznad *float64 `json:"iznad,omitempty"`
	Tekst string   `json:"tekst"`
}

// VrijediZa javlja odnosi li se ograda na zadani niz.
func (o OgradaNiza) VrijediZa(izvor, velicina string) bool {
	if !strings.EqualFold(o.Izvor, izvor) {
		return false
	}
	return o.Velicina == "" || strings.EqualFold(o.Velicina, velicina)
}

// VrijediZaVrijednost javlja dira li ograda zadanu vrijednost. Ograda bez
// granica vrijedi za sve; s granicama samo za ono što u njih upada.
func (o OgradaNiza) VrijediZaVrijednost(v float64) bool {
	if o.Ispod != nil && v >= *o.Ispod {
		return false
	}
	if o.Iznad != nil && v <= *o.Iznad {
		return false
	}
	return true
}

// ImaGranice javlja sužava li se ograda na raspon vrijednosti.
func (o OgradaNiza) ImaGranice() bool { return o.Ispod != nil || o.Iznad != nil }

// Raspon je granica ispisana uz tekst, kad ograda ne vrijedi za cijeli niz.
func (o OgradaNiza) Raspon() string {
	switch {
	case o.Ispod != nil && o.Iznad != nil:
		return fmt.Sprintf("%g – %g", *o.Iznad, *o.Ispod)
	case o.Ispod != nil:
		return fmt.Sprintf("ispod %g", *o.Ispod)
	case o.Iznad != nil:
		return fmt.Sprintf("iznad %g", *o.Iznad)
	}
	return ""
}

// Razdoblje je ograda ispisana uz tekst, kad se odnosi samo na dio niza.
func (o OgradaNiza) Razdoblje() string {
	switch {
	case o.Od != "" && o.Do != "":
		return o.Od + " – " + o.Do
	case o.Od != "":
		return "od " + o.Od
	case o.Do != "":
		return "do " + o.Do
	}
	return ""
}

// Ograda je jedna napomena uz niz, spremna za prikaz. Izdavačeva stiže s
// paketom i vrijedi za sve čvorove; ostale je upisao operater ovog čvora.
type Ograda struct {
	Tekst      string
	Razdoblje  string
	Raspon     string // na koje vrijednosti se odnosi, kad ne vrijedi za sve
	Izdavaceva bool
}

// ImaGranice javlja sužava li se ograda na raspon vrijednosti.
func (o Ograda) ImaGranice() bool { return o.Raspon != "" }

// KrajnostIzNiza je najviša ili najniža vrijednost koju program ima u
// podacima — iz arhive ili iz operativnih očitanja.
//
// Ne zamjenjuje zabilježeni ekstrem. Zabilježeni je tvrdnja s podrijetlom
// („775 cm, izmjereno, DHMZ"), ova je najveće što u nizu stoji. Kod Batine se
// razilaze: niz kaže 797 cm 1956., ali preračunato iz Mohácsa, a zabilježeno
// je 775 cm izmjereno. Razlika je podatak, ne pogreška.
type KrajnostIzNiza struct {
	Kind    string // ExtremeMax ili ExtremeMin
	LevelCm int
	OnDate  string
	Izvor   string // his2000, preracun-mohacs, letva-dhmz …
	Odakle  string // "arhiva" ili "očitanja"
}

// JeMjerena javlja je li krajnost izmjerena na ovoj letvi, a ne preračunata iz
// susjedne. Niz seže dalje unatrag nego što letva postoji.
func (k KrajnostIzNiza) JeMjerena() bool {
	return !strings.HasPrefix(k.Izvor, "preracun")
}

// Naslov je "najviši" ili "najniži", za ispis.
func (k KrajnostIzNiza) Naslov() string {
	if k.Kind == ExtremeMin {
		return "najniži"
	}
	return "najviši"
}

// SazetakEkstrema daje najviši i najniži u jednom retku, za sklopljeni prikaz.
// Prazno kad ekstrema nema.
func (s Station) SazetakEkstrema() string {
	var naj, min *StationExtreme
	for i := range s.Extremes {
		e := &s.Extremes[i]
		if e.LevelCm == nil {
			continue
		}
		if e.Kind == ExtremeMax && (naj == nil || *e.LevelCm > *naj.LevelCm) {
			naj = e
		}
		if e.Kind == ExtremeMin && (min == nil || *e.LevelCm < *min.LevelCm) {
			min = e
		}
	}
	var dj []string
	opis := func(oznaka string, e *StationExtreme) {
		if e == nil {
			return
		}
		t := oznaka + " " + e.Label()
		if e.OnDate != "" {
			t += " (" + e.OnDate + ")"
		}
		dj = append(dj, t)
	}
	opis("najviši", naj)
	opis("najniži", min)
	return strings.Join(dj, " · ")
}

// ZajednickaNapomenaEkstrema vraća napomenu ako je ista uz sve ekstreme.
//
// Ograda „provjeriti prije objave“ stoji uz svaki zapis, pa se u tablici od
// četiri retka ispisivala tri puta. To je sistemska ograda, ne podatak o toj
// vrijednosti — kad je svugdje ista, piše jednom ispod tablice.
func (s Station) ZajednickaNapomenaEkstrema() string {
	if len(s.Extremes) == 0 {
		return ""
	}
	prva := s.Extremes[0].Note
	if prva == "" {
		return ""
	}
	for _, e := range s.Extremes[1:] {
		if e.Note != prva {
			return ""
		}
	}
	return prva
}
