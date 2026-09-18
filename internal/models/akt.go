package models

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Akt je rješenje ili obavijest o uspostavi ili prekidu stupnja obrane od
// poplava, kako ga COP izdaje: po mjerodavnom vodomjeru i branjenom
// području, s dionicama koje iz toga slijede. Isti obrazac kao u mapi
// rješenja sektora B: zaglavlje, pravna osnova s vodostajem, naslov, dionice,
// datum i sat, standardna rečenica, potpisnik, primatelji.
//
// Akt se prvo sastavi kao nacrt, pa ga ovjeri onaj tko ga po stupnju smije
// donijeti. Ovjeren akt se više ne mijenja: ispravak je novi akt. Hitni akti
// COP-a nemaju klasu ni urudžbeni broj, pa ga nema ni ovdje; akt nosi redni
// broj u godini po sektoru, da se u popisu i u razgovoru zna o kojem je riječ.
type Akt struct {
	ID     string `json:"id"`
	Sektor string `json:"sektor"`
	AreaID int    `json:"area_id"`
	Broj   int    `json:"broj"` // redni broj u godini po sektoru, dodijeljen pri ovjeri; 0 na nacrtu
	Godina int    `json:"godina"`

	Radnja  string       `json:"radnja"`  // AktUspostava, AktPrekid
	Stupanj DefensePhase `json:"stupanj"` // PRIPREMNO, REDOVNA, IZVANREDNA, IZVANREDNO_STANJE

	StationID   string `json:"station_id"`
	StationName string `json:"station_name"`
	Watercourse string `json:"watercourse"` // npr. "r. Dunav"

	// Po čemu se donosi: izmjeren vodostaj s tendencijom, ili prognoza
	VodostajCm  *int      `json:"vodostaj_cm,omitempty"`
	VodostajKad time.Time `json:"vodostaj_kad,omitempty"`
	Tendencija  string    `json:"tendencija,omitempty"` // TendencijaPorast, TendencijaOpadanje, TendencijaStagnacija
	Prognoza    string    `json:"prognoza,omitempty"`   // tekst prognoze kad se donosi po njoj, umjesto vodostaja

	// Uvod i Zavrsno su tekst akta sastavljen iz špranče pri sastavljanju
	// nacrta; do ovjere se smiju ispraviti, poslije ovjere stoje kako su
	// ovjereni. Prazno (akti prije špranče) znači da se tekst sastavlja iz
	// zadane špranče pri prikazu.
	Uvod    string `json:"uvod,omitempty"`
	Zavrsno string `json:"zavrsno,omitempty"`
	// Akt o prekidu stavlja izvan snage akt o uspostavi: PrekidaAktID je
	// taj akt, a IzvanSnage rečenica kako stoji na aktu ("Stavlja se izvan
	// snage Obavijest o uspostavi … B-1/2026 od …"); do ovjere se ispravlja
	PrekidaAktID string `json:"prekida_akt_id,omitempty"`
	IzvanSnage   string `json:"izvan_snage,omitempty"`
	// Poveznice su retci na dnu akta, "naziv: adresa", kao na dosadašnjim
	// aktima (Glavni provedbeni plan, Državni plan)
	Poveznice string `json:"poveznice,omitempty"`

	Dionice    []AktDionica   `json:"dionice"`
	Vrijedi    time.Time      `json:"vrijedi"` // dan i sat od kojeg stupanj vrijedi
	Napomena   string         `json:"napomena,omitempty"`
	Potpisnik  string         `json:"potpisnik"` // funkcija potpisnika kako stoji na aktu
	Primatelji []AktPrimatelj `json:"primatelji"`

	Status     string     `json:"status"` // AktNacrt, AktOvjeren
	IzradioID  string     `json:"izradio_id"`
	Izradio    string     `json:"izradio"`
	IzradenoAt time.Time  `json:"izradeno_at"`
	OvjerioID  string     `json:"ovjerio_id,omitempty"`
	Ovjerio    string     `json:"ovjerio"` // ime i prezime onoga tko je ovjerio
	OvjerenoAt *time.Time `json:"ovjereno_at,omitempty"`
	OvjeraKod  string     `json:"ovjera_kod,omitempty"` // sažetak sadržaja pri ovjeri, za provjeru ispisa
	// UZamjeni: ovjerio je zamjenik ili druga razina, ne nositelj funkcije
	// potpisnika; na aktu uz ime stoji "u.z." (u zamjeni)
	UZamjeni bool   `json:"u_zamjeni,omitempty"`
	Cvor     string `json:"cvor,omitempty"` // čvor na kojem je ovjeren
	// Potpis je Ed25519 potpis sadržaja i ovjere ključem čvora na kojem je
	// akt ovjeren (base64); KljucCvora je javni ključ tog čvora. Po njima se
	// na svakom čvoru provjerava da akt nije mijenjan nakon ovjere.
	Potpis     string `json:"potpis,omitempty"`
	KljucCvora string `json:"kljuc_cvora,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AktDionica je dionica na koju se akt odnosi, s opisom kakav stoji na aktu
type AktDionica struct {
	Code string `json:"code"`
	Opis string `json:"opis"` // npr. "d.o. r. Dunav, rkm 1433+060 – 1421+770 (državna granica – Zeleni otok)"
}

// AktPrimatelj je jedan primatelj na aktu; skupina drži redoslijed kao na
// dosadašnjim aktima: Direkcija i GCOP, VGI i izvođač, službe, rukovoditelji
type AktPrimatelj struct {
	Naziv   string `json:"naziv"`
	Email   string `json:"email,omitempty"`
	Skupina string `json:"skupina,omitempty"`
}

// Radnje akta
const (
	AktUspostava = "USPOSTAVA"
	AktPrekid    = "PREKID"
)

// Stanja akta
const (
	AktNacrt   = "NACRT"
	AktOvjeren = "OVJEREN"
)

// Tendencije vodostaja su iste kao u dnevnom izvješću (TendencijaPorast,
// TendencijaOpadanje, TendencijaStagnacija, TendencijaNagliPorast).

// TendencijaLabel je tendencija kako se ispisuje u rečenici akta
func TendencijaLabel(t string) string {
	switch t {
	case TendencijaPorast:
		return "s tendencijom daljnjeg porasta"
	case TendencijaOpadanje:
		return "s tendencijom daljnjeg opadanja"
	case TendencijaStagnacija:
		return "sa stagnacijom"
	case TendencijaNagliPorast:
		return "s tendencijom daljnjeg naglog porasta"
	}
	return ""
}

// StupnjeviAkta su stupnjevi koje akt proglašava, od najnižeg
var StupnjeviAkta = []DefensePhase{PhasePrep, PhaseRegular, PhaseEmergency, PhaseState}

// JeRjesenje javlja donosi li se stupanj rješenjem; pripremno stanje ide
// obaviješću, i uspostava i prekid, kako je i na dosadašnjim aktima
func (a Akt) JeRjesenje() bool { return a.Stupanj != PhasePrep }

// VrstaNaziv je vrsta akta u rečenici: Rješenje ili Obavijest
func (a Akt) VrstaNaziv() string {
	if a.JeRjesenje() {
		return "Rješenje"
	}
	return "Obavijest"
}

// RecenicaIzvanSnage je rečenica akta o prekidu kojom se akt o uspostavi
// stavlja izvan snage
func RecenicaIzvanSnage(u Akt) string {
	v := u.Vrijedi.In(Zagreb)
	return "Stavlja se izvan snage " + u.VrstaNaziv() + " o uspostavi " + u.Predmet() + " oznake " + u.Oznaka() +
		" od " + v.Format("02.01.2006.") + " u " + v.Format("15:04") + " sati."
}

// Vrsta je naziv akta: RJEŠENJE ili OBAVIJEST
func (a Akt) Vrsta() string {
	if a.JeRjesenje() {
		return "RJEŠENJE"
	}
	return "OBAVIJEST"
}

// Clanak je članak Državnog plana obrane od poplava (NN 84/10) koji uređuje
// stupanj: XXII pripremno stanje, XXIII redovita obrana, XXIV izvanredna
// obrana, XXV izvanredno stanje. Dosadašnji akti za redovnu obranu zvali su
// se na XXII, a redovitu uređuje XXIII.
func (a Akt) Clanak() string { return ZadaniClanak(a.Stupanj) }

// ZadaniClanak je članak Državnog plana za stupanj
func ZadaniClanak(p DefensePhase) string {
	switch p {
	case PhaseRegular:
		return "XXIII"
	case PhaseEmergency:
		return "XXIV"
	case PhaseState:
		return "XXV"
	}
	return "XXII"
}

// TekstUvoda je prvi odlomak akta: spremljen, ili iz zadane špranče
func (a Akt) TekstUvoda() string {
	if strings.TrimSpace(a.Uvod) != "" {
		return a.Uvod
	}
	return ZadanaSpranca(a.Sektor).Uvod(a)
}

// TekstZavrsni je rečenica o postupanju na kraju akta
func (a Akt) TekstZavrsni() string {
	if strings.TrimSpace(a.Zavrsno) != "" {
		return a.Zavrsno
	}
	return ZadanaSpranca(a.Sektor).Zavrsno
}

// Spranca je predložak teksta akata za sektor: pravna osnova, članci
// Državnog plana po stupnju i završna rečenica. Uređuje je uprava sektora
// kad se propis promijeni; pojedini nacrt se uz to smije ispraviti do ovjere.
type Spranca struct {
	Sektor string `json:"sektor"`
	// Osnova je uvod do rečenice o vodostaju; {clanak} se zamjenjuje
	// člankom Državnog plana za stupanj akta
	Osnova    string                  `json:"osnova"`
	Clanci    map[DefensePhase]string `json:"clanci"`
	Zavrsno   string                  `json:"zavrsno"`
	Poveznice string                  `json:"poveznice"`
	// Uredio i kada, za prikaz; putuje knjigom verzija
	Uredio    string    `json:"uredio,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ZadanaSpranca je špranca prema važećim propisima (provjereno 9/2026):
// Zakon o vodama NN 66/19, 84/21, 47/23, čl. 130; Državni plan obrane od
// poplava NN 84/10; Glavni provedbeni plan kako je objavljen na voda.hr
func ZadanaSpranca(sektor string) Spranca {
	return Spranca{
		Sektor: sektor,
		Osnova: "Na temelju Zakona o vodama, članak 130. (N.N. br. 66/19, 84/21 i 47/23) te odredbi članka {clanak} " +
			"Državnog plana obrane od poplava (N.N. br. 84/10) i Glavnog provedbenog plana obrane od poplava (Hrvatske vode, ožujak 2022.),",
		Clanci: map[DefensePhase]string{PhasePrep: "XXII", PhaseRegular: "XXIII", PhaseEmergency: "XXIV", PhaseState: "XXV"},
		Zavrsno: "Za vrijeme provođenja mjera obrane od poplava treba postupiti prema odredbama Državnog plana obrane od poplava " +
			"(N.N. br. 84/10) i Glavnog provedbenog plana obrane od poplava (Hrvatske vode, ožujak 2022.)!",
		Poveznice: "Glavni provedbeni plan obrane od poplava: https://www.voda.hr/hr/novost/glavni-provedbeni-plan-obrane-od-poplava\n" +
			"Državni plan obrane od poplava: https://narodne-novine.nn.hr/clanci/sluzbeni/2010_07_84_2389.html",
	}
}

// ClanakZa je članak za stupanj po ovoj špranči, ili zadani
func (sp Spranca) ClanakZa(p DefensePhase) string {
	if c := strings.TrimSpace(sp.Clanci[p]); c != "" {
		return c
	}
	return ZadaniClanak(p)
}

// Uvod sastavlja prvi odlomak akta: osnova s člankom, rečenica o vodostaju
// ili prognozi, i "donosim"
func (sp Spranca) Uvod(a Akt) string {
	osnova := strings.TrimSpace(strings.ReplaceAll(sp.Osnova, "{clanak}", sp.ClanakZa(a.Stupanj)))
	osnova = strings.TrimSuffix(osnova, ",")
	return osnova + ", " + a.Osnova() + ", donosim"
}

// Predmet je ono što akt uspostavlja ili prekida, u genitivu, kako stoji u
// naslovu: "pripremnog stanja obrane od poplava", "izvanrednih mjera obrane
// od poplava"
func (a Akt) Predmet() string {
	switch a.Stupanj {
	case PhasePrep:
		return "pripremnog stanja obrane od poplava"
	case PhaseRegular:
		if a.Radnja == AktPrekid {
			return "redovnih mjera obrane od poplava"
		}
		return "redovne obrane od poplava"
	case PhaseEmergency:
		if a.Radnja == AktPrekid {
			return "izvanrednih mjera obrane od poplava"
		}
		return "izvanredne obrane od poplava"
	case PhaseState:
		return "izvanrednog stanja obrane od poplava na zaštitnim vodnim građevinama"
	}
	return a.Stupanj.Label()
}

// RadnjaLabel je "uspostavi" ili "prekidu", kako stoji u naslovu "o uspostavi …"
func (a Akt) RadnjaLabel() string {
	if a.Radnja == AktPrekid {
		return "prekidu"
	}
	return "uspostavi"
}

// Naslov je naslov akta u jednom retku, za popis i za naziv datoteke
func (a Akt) Naslov() string {
	return a.Vrsta() + " o " + a.RadnjaLabel() + " " + a.Predmet()
}

// Oznaka je kratka oznaka akta: sektor, redni broj i godina; nacrt je bez broja
func (a Akt) Oznaka() string {
	if a.Broj == 0 {
		return "nacrt"
	}
	return fmt.Sprintf("%s-%d/%d", a.Sektor, a.Broj, a.Godina)
}

// Ovjeren javlja je li akt ovjeren
func (a Akt) Ovjeren() bool { return a.Status == AktOvjeren }

// ImePotpisa je ime kako stoji ispod crte za potpis: s "u.z." kad je
// ovjerio zamjenik
func (a Akt) ImePotpisa() string {
	if a.UZamjeni {
		return "u.z. " + a.Ovjerio
	}
	return a.Ovjerio
}

// NositeljFunkcije javlja je li osoba s tim zaduženjima nositelj funkcije
// potpisnika akta: rukovoditelj sektora za izvanrednu i izvanredno stanje,
// rukovoditelj branjenog područja za pripremno i redovnu. Svi ostali koji
// smiju ovjeriti potpisuju u zamjeni.
func (a Akt) NositeljFunkcije(duties []Duty) bool {
	for _, d := range duties {
		if !d.IsActive {
			continue
		}
		if a.Stupanj == PhaseEmergency || a.Stupanj == PhaseState {
			if d.Role == RoleSectorLeader && d.SectorID != nil && *d.SectorID == a.Sektor {
				return true
			}
			continue
		}
		if d.Role == RoleAreaLeader && d.AreaID != nil && *d.AreaID == a.AreaID {
			return true
		}
	}
	return false
}

// Osnova je rečenica o vodostaju ili prognozi po kojoj se akt donosi, bez
// uvodnog "Na temelju…" i bez završnog "donosim"
func (a Akt) Osnova() string {
	voda := a.Watercourse
	if voda == "" {
		voda = "vodotoka"
	}
	if a.Prognoza != "" {
		return "a vezano na prognozu vodostaja " + voda + " na mjerodavnom vodomjeru " + a.StationName + ": " + strings.TrimSpace(a.Prognoza)
	}
	if a.VodostajCm == nil {
		return "a vezano na stanje vodostaja " + voda + " na mjerodavnom vodomjeru " + a.StationName
	}
	s := fmt.Sprintf("a vezano na visinu vodostaja %s na mjerodavnom vodomjeru %s, na kojem je zabilježen vodostaj od %d cm", voda, a.StationName, *a.VodostajCm)
	if !a.VodostajKad.IsZero() {
		s += " u " + a.VodostajKad.In(Zagreb).Format("15:04") + " sati"
	}
	if t := TendencijaLabel(a.Tendencija); t != "" {
		s += ", " + t
	}
	return s
}

// Sifre su šifre dionica akta, poredane
func (a Akt) Sifre() []string {
	out := make([]string, 0, len(a.Dionice))
	for _, d := range a.Dionice {
		out = append(out, d.Code)
	}
	sort.Strings(out)
	return out
}

// Sazetak je kanonski tekst iz kojeg se računa kod ovjere: sve što na aktu
// piše i što se ne smije promijeniti poslije ovjere
func (a Akt) Sazetak() string {
	var b strings.Builder
	b.WriteString(a.Sektor + "|" + fmt.Sprint(a.AreaID) + "|" + a.Radnja + "|" + string(a.Stupanj) + "|" + a.StationID + "|")
	if a.VodostajCm != nil {
		fmt.Fprintf(&b, "%d@%s|", *a.VodostajCm, a.VodostajKad.UTC().Format(time.RFC3339))
	}
	b.WriteString(a.Tendencija + "|" + a.Prognoza + "|" + a.Vrijedi.UTC().Format(time.RFC3339) + "|" + a.Napomena + "|" + a.Potpisnik + "|")
	b.WriteString(a.TekstUvoda() + "|" + a.TekstZavrsni() + "|" + a.Poveznice + "|" + a.PrekidaAktID + "|" + a.IzvanSnage + "|")
	for _, d := range a.Dionice {
		b.WriteString(d.Code + "=" + d.Opis + ";")
	}
	b.WriteString("|")
	for _, p := range a.Primatelji {
		b.WriteString(p.Naziv + "<" + p.Email + ">;")
	}
	return b.String()
}

// PorukaPotpisa je ono što ključ čvora potpisuje: sav sadržaj akta, tko ga
// je ovjerio i kada
func (a Akt) PorukaPotpisa() []byte {
	kad := ""
	if a.OvjerenoAt != nil {
		kad = a.OvjerenoAt.UTC().Format(time.RFC3339)
	}
	return []byte("goCOP-akt-v1|" + a.ID + "|" + a.Oznaka() + "|" + a.Sazetak() + "|" + a.OvjerioID + "|" + a.Ovjerio + "|" + kad)
}

// OtisakKljuca je kratki otisak javnog ključa čvora za ispis
func (a Akt) OtisakKljuca() string {
	if a.KljucCvora == "" {
		return ""
	}
	h := sha256.Sum256([]byte(a.KljucCvora))
	x := strings.ToUpper(hex.EncodeToString(h[:6]))
	return x[:4] + " " + x[4:8] + " " + x[8:]
}

// KodOvjere je kratki sažetak sadržaja i ovjere, za ispis na aktu i provjeru
// da ispisani akt odgovara ovjerenome
func (a Akt) KodOvjere(ovjerioID string, kad time.Time) string {
	h := sha256.Sum256([]byte(a.Sazetak() + "|" + ovjerioID + "|" + kad.UTC().Format(time.RFC3339)))
	return strings.ToUpper(hex.EncodeToString(h[:5]))
}

// Primatelj je stalni primatelj akata u registru: kome se šalje za sektor
// ili za pojedino branjeno područje, i od kojeg stupnja. Županije, općine i
// rukovoditelji dionica ne stoje ovdje nego dolaze iz registara.
type Primatelj struct {
	ID         string       `json:"id"`
	Sektor     string       `json:"sektor"`
	AreaID     int          `json:"area_id"` // 0 = cijeli sektor
	Naziv      string       `json:"naziv"`
	Email      string       `json:"email,omitempty"`
	Skupina    string       `json:"skupina,omitempty"`    // za redoslijed na aktu: SkupinaPrimatelja*
	OdStupnja  DefensePhase `json:"od_stupnja,omitempty"` // prazno = uvijek
	Redoslijed int          `json:"redoslijed"`
	Aktivan    bool         `json:"aktivan"`
	UpdatedAt  time.Time    `json:"updated_at"`
	Archived   bool         `json:"archived,omitempty"`
}

// Podstavka javlja je li primatelj podstavka prethodnoga, kao "– Ured
// generalnog direktora" ispod "Hrvatske vode, Direkcija Zagreb": na aktu je
// uvučen i bez rednog broja
func (p AktPrimatelj) Podstavka() bool {
	return strings.HasPrefix(p.Naziv, "–") || strings.HasPrefix(p.Naziv, "-")
}

// NazivBezCrtice je naziv podstavke bez crtice na početku
func (p AktPrimatelj) NazivBezCrtice() string {
	return strings.TrimSpace(strings.TrimLeft(p.Naziv, "–- "))
}

// RedniBrojevi daje redni broj svakom primatelju koji nije podstavka; 0 za podstavke
func RedniBrojevi(ps []AktPrimatelj) []int {
	out := make([]int, len(ps))
	n := 0
	for i, p := range ps {
		if !p.Podstavka() {
			n++
			out[i] = n
		}
	}
	return out
}

// Retci su poveznice na dnu akta, rastavljene na naziv i adresu
func (a Akt) Retci() [][2]string {
	var out [][2]string
	for _, l := range strings.Split(a.Poveznice, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if i := strings.Index(l, ": "); i > 0 {
			out = append(out, [2]string{l[:i+1], strings.TrimSpace(l[i+2:])})
		} else {
			out = append(out, [2]string{l, ""})
		}
	}
	return out
}

// Skupine primatelja, redom kako stoje na aktu
const (
	SkupinaUprava     = "UPRAVA"     // Direkcija, GCOP
	SkupinaIspostava  = "ISPOSTAVA"  // VGI, ugovorna pravna osoba
	SkupinaSluzbe     = "SLUZBE"     // civilna zaštita, 112, policija, lučke kapetanije
	SkupinaSamouprava = "SAMOUPRAVA" // županije, gradovi i općine
	SkupinaOsobe      = "OSOBE"      // rukovoditelji dionica i zamjenici
	SkupinaPismohrana = "PISMOHRANA"
)

// SkupinePrimatelja su skupine redom kojim stoje na aktu
var SkupinePrimatelja = []string{SkupinaUprava, SkupinaIspostava, SkupinaSluzbe, SkupinaSamouprava, SkupinaOsobe, SkupinaPismohrana}

// SkupinaLabel je naziv skupine za prikaz
func SkupinaLabel(s string) string {
	switch s {
	case SkupinaUprava:
		return "uprava organizacije"
	case SkupinaIspostava:
		return "ispostava i izvođač"
	case SkupinaSluzbe:
		return "službe"
	case SkupinaSamouprava:
		return "županije, gradovi i općine"
	case SkupinaOsobe:
		return "rukovoditelji dionica"
	case SkupinaPismohrana:
		return "pismohrana"
	}
	return s
}

// Vrijedi javlja ide li primatelj na akt tog stupnja i područja
func (p Primatelj) Vrijedi(areaID int, stupanj DefensePhase) bool {
	if !p.Aktivan || p.Archived {
		return false
	}
	if p.AreaID != 0 && p.AreaID != areaID {
		return false
	}
	if p.OdStupnja != "" && stupanj.Severity() < p.OdStupnja.Severity() {
		return false
	}
	return true
}
