package models

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// PrijavaSTerena je dokument koji vodočuvar sastavlja na terenu: izvješće,
// prijava, obavijest ili zahtjev o onome što je zatekao (otpad u kanalu,
// oštećena rampa na nasipu, provala u čuvarnicu), s vodotokom ili objektom,
// mjestom na karti i fotografijama. Vodočuvar je objavi i elektronički
// potpiše: nastaje PDF s ugrađenim slikama koji ostaje trajno kao izvornik, a
// upis o prijavi ide na njegov dnevni list, jer je dnevnik dokaz rada.
// Izvorne fotografije čuvaju se ograničeno vrijeme, pa se brišu: PDF ih nosi.
type PrijavaSTerena struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	Ime    string `json:"ime"`
	Sektor string `json:"sektor"`
	AreaID int    `json:"area_id"`
	Broj   int    `json:"broj"` // redni broj u godini po sektoru, dodijeljen pri objavi
	Godina int    `json:"godina"`

	Vrsta  string    `json:"vrsta"` // PrijavaIzvjesce, PrijavaPrijava, PrijavaObavijest, PrijavaZahtjev
	Naslov string    `json:"naslov"`
	Opis   string    `json:"opis"`
	Datum  time.Time `json:"datum"` // dan događaja na terenu

	// Gdje: voda iz registra (šifra i naziv), dionica, objekt; i točka na karti
	VodotokCode string   `json:"vodotok_code,omitempty"`
	Vodotok     string   `json:"vodotok,omitempty"`
	DionicaCode string   `json:"dionica_code,omitempty"`
	ObjektID    string   `json:"objekt_id,omitempty"`
	Objekt      string   `json:"objekt,omitempty"`
	Latitude    *float64 `json:"latitude,omitempty"`
	Longitude   *float64 `json:"longitude,omitempty"`
	Stacionaza  string   `json:"stacionaza,omitempty"` // npr. "25+500"
	Element     string   `json:"element,omitempty"`    // konstrukcijski element: nasip, ustava, propust…
	Vaznost     string   `json:"vaznost,omitempty"`    // važnost objekta: javno vodno dobro (JVD)…

	// Urudžba: kad tiskana i potpisana prijava uđe u urudžbeni zapisnik kao
	// dolazni akt, pisarnica joj da klasu i urbroj; upisuju se naknadno
	Klasa       string     `json:"klasa,omitempty"`
	Urbroj      string     `json:"urbroj,omitempty"`
	PrimljenoAt *time.Time `json:"primljeno_at,omitempty"`

	// Rekonstrukcija: prenesena iz ranije evidencije (Directus BP16); nema
	// elektroničkog potpisa, sken potpisanog ispisa je izvornik gdje ga ima.
	// Izvor je oznaka zapisa u toj evidenciji, da ponovni uvoz preskoči što ima.
	Rekonstrukcija bool   `json:"rekonstrukcija,omitempty"`
	Izvor          string `json:"izvor,omitempty"`
	// Sken je naziv datoteke skena potpisanog ispisa iz ranije evidencije,
	// spremljene uz bazu (lokalno, ne putuje knjigom): izvornik u programu je
	// PDF iz podataka, sken je prilog
	Sken string `json:"sken,omitempty"`

	// Slike su podaci o fotografijama; same slike stoje lokalno u
	// prijave_slike i brišu se nakon roka, a PDF ih nosi trajno
	Slike []SlikaPrijave `json:"slike,omitempty"`

	Status       string     `json:"status"` // PrijavaNacrt, PrijavaObjavljena, PrijavaRijesena
	ObjavljenoAt *time.Time `json:"objavljeno_at,omitempty"`
	ListID       string     `json:"list_id,omitempty"` // dnevni list na koji je prijava upisana
	ListBroj     int        `json:"list_broj,omitempty"`
	// rukovoditelj je prijavu pregledao i riješio; to nije arhiviranje u
	// smislu repozitorija (službena je od objave), nego zatvaranje predmeta
	RijesioID  string     `json:"rijesio_id,omitempty"`
	Rijesio    string     `json:"rijesio,omitempty"`
	RijesenoAt *time.Time `json:"rijeseno_at,omitempty"`

	Cvor      string    `json:"cvor,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SlikaPrijave su podaci o jednoj fotografiji uz prijavu
type SlikaPrijave struct {
	ID      string `json:"id"`
	Naziv   string `json:"naziv,omitempty"`
	Sirina  int    `json:"sirina"`
	Visina  int    `json:"visina"`
	Bajtova int    `json:"bajtova"`
	// što je fotoaparat zapisao: kad je snimljena, gdje i čime; prazno kad
	// slika to ne nosi
	Snimljeno *time.Time `json:"snimljeno,omitempty"`
	Lat       float64    `json:"lat,omitempty"`
	Lon       float64    `json:"lon,omitempty"`
	Uredjaj   string     `json:"uredjaj,omitempty"`
	// izvorna datoteka kakva je zaprimljena: veličina i SHA-256 otisak, da
	// se smanjena slika u dokumentu može vezati uz original ako se pojavi
	IzvornoBajtova int    `json:"izvorno_bajtova,omitempty"`
	Otisak         string `json:"otisak,omitempty"`
	// Sadrzaj je otisak smanjene slike kakva stoji u spremištu sadržaja: po
	// njemu je čvor koji prijavu primi razmjenom može dohvatiti
	Sadrzaj string `json:"sadrzaj,omitempty"`
}

// ImaPolozaj javlja je li uz fotografiju zapisan položaj
func (s SlikaPrijave) ImaPolozaj() bool { return s.Lat != 0 || s.Lon != 0 }

// Podaci sažimaju zapis fotoaparata u jedan redak ispod slike:
// "Snimljeno 5.6.2024. u 11:43 · samsung SM-J415FN · 45.65120 N, 18.77340 E".
// Kad fotoaparat nije zapisao ni vrijeme ni položaj, kaže se i to, jer je
// za dokaz važno što slika nosi, a što ne.
func (s SlikaPrijave) Podaci() string {
	var d []string
	if s.Snimljeno != nil && !s.Snimljeno.IsZero() {
		d = append(d, "Snimljeno "+s.Snimljeno.In(Zagreb).Format("2.1.2006. u 15:04:05"))
	}
	if s.ImaPolozaj() {
		d = append(d, Koordinate(s.Lat, s.Lon))
	}
	if s.Uredjaj != "" {
		d = append(d, "uređaj "+s.Uredjaj)
	}
	if len(d) == 0 {
		return "Fotoaparat uz sliku nije zapisao vrijeme, položaj ni uređaj."
	}
	return strings.Join(d, " · ")
}

// Izvornik opisuje zaprimljenu datoteku: "Izvorna datoteka IMG_1234.jpg,
// 7,3 MB, SHA-256 ab12…"; prazno kad otisak nije uzet
func (s SlikaPrijave) Izvornik() string {
	if s.Otisak == "" {
		return ""
	}
	z := "Izvorna datoteka"
	if s.Naziv != "" {
		z += " " + s.Naziv
	}
	if s.IzvornoBajtova > 0 {
		z += fmt.Sprintf(", %s", VelicinaHR(s.IzvornoBajtova))
	}
	return z + ", SHA-256 " + s.Otisak
}

// Koordinate ispisuju položaj sa stranama svijeta: "45.65120 N, 18.77340 E"
func Koordinate(lat, lon float64) string {
	ns, ew := "N", "E"
	if lat < 0 {
		ns, lat = "S", -lat
	}
	if lon < 0 {
		ew, lon = "W", -lon
	}
	return fmt.Sprintf("%.5f %s, %.5f %s", lat, ns, lon, ew)
}

// VelicinaHR ispisuje veličinu datoteke po naški: "7,3 MB", "248 KB"
func VelicinaHR(b int) string {
	switch {
	case b >= 1<<20:
		return strings.ReplaceAll(fmt.Sprintf("%.1f MB", float64(b)/(1<<20)), ".", ",")
	case b >= 1<<10:
		return fmt.Sprintf("%d KB", b>>10)
	}
	return fmt.Sprintf("%d B", b)
}

// Vrste prijava, kako ih vodočuvar bira
const (
	PrijavaIzvjesce  = "IZVJESCE"
	PrijavaPrijava   = "PRIJAVA"
	PrijavaObavijest = "OBAVIJEST"
	PrijavaZahtjev   = "ZAHTJEV"
)

// VrstePrijava redom kojim ih obrazac nudi
var VrstePrijava = []string{PrijavaObavijest, PrijavaPrijava, PrijavaIzvjesce, PrijavaZahtjev}

// Stanja prijave
const (
	PrijavaNacrt      = "NACRT"
	PrijavaObjavljena = "OBJAVLJENA"
	PrijavaRijesena   = "RIJESENA"
)

// NajviseSlika je koliko fotografija prijava nosi
const NajviseSlika = 6

// VrstaPrijaveLabel vraća vrstu za prikaz
func VrstaPrijaveLabel(v string) string {
	switch v {
	case PrijavaIzvjesce:
		return "Izvješće"
	case PrijavaPrijava:
		return "Prijava"
	case PrijavaObavijest:
		return "Obavijest"
	case PrijavaZahtjev:
		return "Zahtjev"
	}
	return v
}

// VrstaLabel vraća vrstu prijave za prikaz
func (p PrijavaSTerena) VrstaLabel() string { return VrstaPrijaveLabel(p.Vrsta) }

// Oznaka je kratka oznaka: sektor, redni broj i godina; nacrt je bez broja
func (p PrijavaSTerena) Oznaka() string {
	if p.Broj == 0 {
		return "nacrt"
	}
	return fmt.Sprintf("%s-T-%d/%d", p.Sektor, p.Broj, p.Godina)
}

// Objavljena javlja je li prijava objavljena (ili poslije riješena)
func (p PrijavaSTerena) Objavljena() bool { return p.ObjavljenoAt != nil }

// Rijesena javlja je li rukovoditelj prijavu pregledao i riješio
func (p PrijavaSTerena) Rijesena() bool { return p.Status == PrijavaRijesena }

// StanjeLabel vraća stanje za prikaz
func (p PrijavaSTerena) StanjeLabel() string {
	switch p.Status {
	case PrijavaObjavljena:
		return "objavljena"
	case PrijavaRijesena:
		return "riješena"
	}
	return "nacrt"
}

// Urudzbirana javlja je li prijava dobila klasu ili urbroj u urudžbenom zapisniku
func (p PrijavaSTerena) Urudzbirana() bool { return p.Klasa != "" || p.Urbroj != "" }

// ImaKoordinate javlja je li mjesto označeno na karti
func (p PrijavaSTerena) ImaKoordinate() bool { return p.Latitude != nil && p.Longitude != nil }

// Mjesto je opis mjesta u jednom retku: voda, dionica, objekt, stacionaža
func (p PrijavaSTerena) Mjesto() string {
	var d []string
	if p.Vodotok != "" {
		d = append(d, p.Vodotok)
	}
	if p.Objekt != "" {
		d = append(d, p.Objekt)
	}
	if p.DionicaCode != "" {
		d = append(d, "dionica "+p.DionicaCode)
	}
	if p.Stacionaza != "" {
		d = append(d, "st. "+p.Stacionaza)
	}
	return join(d, ", ")
}

// ZadanoCuvanjeSlikaDana je koliko se dana izvorne fotografije čuvaju
// nakon objave kad opcija nije postavljena; PDF ih nosi trajno
const ZadanoCuvanjeSlikaDana = 180

// Kod je kratki sažetak prijave i objave, za ispis uz potpis
func (p PrijavaSTerena) Kod() string {
	kad := ""
	if p.ObjavljenoAt != nil {
		kad = p.ObjavljenoAt.UTC().Format(time.RFC3339)
	}
	h := sha256.Sum256([]byte(p.ID + "|" + p.UserID + "|" + p.Naslov + "|" + p.Opis + "|" + kad))
	return strings.ToUpper(hex.EncodeToString(h[:5]))
}

// PrijavaNaListu je upis prijave na dnevnom listu vodočuvara
type PrijavaNaListu struct {
	ID     string    `json:"id"`
	Oznaka string    `json:"oznaka"`
	Vrsta  string    `json:"vrsta"`
	Naslov string    `json:"naslov"`
	Kad    time.Time `json:"kad"`
}

// Tekst je redak kako stoji na listu
func (p PrijavaNaListu) Tekst() string {
	return VrstaPrijaveLabel(p.Vrsta) + " s terena " + p.Oznaka + ": " + p.Naslov
}

func join(d []string, sep string) string {
	s := ""
	for i, x := range d {
		if i > 0 {
			s += sep
		}
		s += x
	}
	return s
}
