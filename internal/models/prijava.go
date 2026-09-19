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

	// Slike su podaci o fotografijama; same slike stoje lokalno u
	// prijave_slike i brišu se nakon roka, a PDF ih nosi trajno
	Slike []SlikaPrijave `json:"slike,omitempty"`

	Status       string     `json:"status"` // PrijavaNacrt, PrijavaObjavljena, PrijavaArhivirana
	ObjavljenoAt *time.Time `json:"objavljeno_at,omitempty"`
	ListID       string     `json:"list_id,omitempty"` // dnevni list na koji je prijava upisana
	ListBroj     int        `json:"list_broj,omitempty"`
	ArhiviraoID  string     `json:"arhivirao_id,omitempty"`
	Arhivirao    string     `json:"arhivirao,omitempty"`
	ArhiviranoAt *time.Time `json:"arhivirano_at,omitempty"`

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
	PrijavaArhivirana = "ARHIVIRANA"
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

// Objavljena javlja je li prijava objavljena (ili poslije arhivirana)
func (p PrijavaSTerena) Objavljena() bool { return p.ObjavljenoAt != nil }

// Arhivirana javlja je li prijava arhivirana
func (p PrijavaSTerena) Arhivirana() bool { return p.Status == PrijavaArhivirana }

// StanjeLabel vraća stanje za prikaz
func (p PrijavaSTerena) StanjeLabel() string {
	switch p.Status {
	case PrijavaObjavljena:
		return "objavljena"
	case PrijavaArhivirana:
		return "arhivirana"
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
