package models

import (
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

// Ustroj obrane u tri razine i riječi kojima ga organizacija zove.
//
// Razina 1 je organizacija sama: Hrvatske vode, s Direkcijom i Glavnim
// centrom obrane od poplava (GCOP). Nema množine, jedna je.
// Razina 2 je vodnogospodarski odjel (VGO) sa svojim sektorom i centrom
// obrane (COP). Razina 3 je vodnogospodarska ispostava (VGI) s podcentrom
// obrane i svojim branjenim područjem; poneko područje nema ispostavu nego
// pripada izravno sektoru (npr. B.34, međudržavne rijeke).
//
// Druga vodnogospodarska organizacija istu podjelu zove drukčije, pa su
// riječi postavka. Šifre dionica i ovlasti rade isto. Zapis je jedan i
// putuje razmjenom, pa svi čvorovi jedne mreže govore istim jezikom.
type OrgTerms struct {
	ID string `json:"id"` // uvijek "terms"

	// razina 1: organizacija (nazivi i podaci)
	OrgName           string `json:"org_name"`            // Hrvatske vode
	OrgLegalForm      string `json:"org_legal_form"`      // pravna osoba za upravljanje vodama
	OrgRegistryNo     string `json:"org_registry_no"`     // matični broj
	OrgTaxID          string `json:"org_tax_id"`          // OIB ili porezni broj
	Level1Unit        string `json:"level1_unit"`         // Direkcija
	Level1Center      string `json:"level1_center"`       // Glavni centar obrane od poplava
	Level1CenterShort string `json:"level1_center_short"` // GCOP

	// znak organizacije i tekst koji stoji na stranici prijave
	LogoMime  string `json:"logo_mime,omitempty"` // image/svg+xml, image/png…
	Logo      []byte `json:"logo,omitempty"`      // sam znak, najviše LogoMaxBytes
	LoginInfo string `json:"login_info"`          // upute ispod obrasca prijave

	// nazivi sudionika obrane po šifri uloge; prazno = zadani naziv
	RoleLabels map[string]string `json:"role_labels,omitempty"`

	// razina 2: odjel sa sektorom
	Sector            string `json:"sector"`              // Sektor
	Sectors           string `json:"sectors"`             // Sektori
	SectorOffice      string `json:"sector_office"`       // Vodnogospodarski odjel
	SectorOfficeShort string `json:"sector_office_short"` // VGO
	Center            string `json:"center"`              // Centar obrane od poplava
	CenterShort       string `json:"center_short"`        // COP

	// razina 3: ispostava s branjenim područjem
	Area            string `json:"area"`              // Branjeno područje
	Areas           string `json:"areas"`             // Branjena područja
	AreaShort       string `json:"area_short"`        // BP
	AreaOffice      string `json:"area_office"`       // Vodnogospodarska ispostava
	AreaOfficeShort string `json:"area_office_short"` // VGI
	Subcenter       string `json:"subcenter"`         // Podcentar obrane

	UpdatedAt time.Time `json:"updated_at"`
}

// TermsID je identitet jedinog zapisa naziva
const TermsID = "terms"

// LogoMaxBytes je najveći dopušteni znak; putuje knjigom verzija na sve čvorove
const LogoMaxBytes = 512 * 1024

// HasLogo javlja ima li organizacija vlastiti znak
func (t OrgTerms) HasLogo() bool { return len(t.Logo) > 0 && t.LogoMime != "" }

// DefaultTerms su nazivi Hrvatskih voda
func DefaultTerms() OrgTerms {
	return OrgTerms{
		ID:      TermsID,
		OrgName: "Hrvatske vode", Level1Unit: "Direkcija", Level1Center: "Glavni centar obrane od poplava", Level1CenterShort: "GCOP",
		Sector: "Sektor", Sectors: "Sektori", SectorOffice: "Vodnogospodarski odjel", SectorOfficeShort: "VGO",
		Center: "Centar obrane od poplava", CenterShort: "COP",
		Area: "Branjeno područje", Areas: "Branjena područja", AreaShort: "BP",
		AreaOffice: "Vodnogospodarska ispostava", AreaOfficeShort: "VGI", Subcenter: "Podcentar obrane",
	}
}

// Filled vraća nazive s praznima popunjenim iz zadanih, da sučelje nikad ne
// ostane bez riječi. Podaci razine 1 (naziv jedinice, adresa, kontakti) su
// vrijednosti, ne nazivi: prazno ostaje prazno.
func (t OrgTerms) Filled() OrgTerms {
	d := DefaultTerms()
	pick := func(v, def string) string {
		if strings.TrimSpace(v) == "" {
			return def
		}
		return strings.TrimSpace(v)
	}
	return OrgTerms{
		ID:      TermsID,
		OrgName: pick(t.OrgName, d.OrgName), Level1Unit: pick(t.Level1Unit, d.Level1Unit),
		OrgLegalForm: strings.TrimSpace(t.OrgLegalForm), OrgRegistryNo: strings.TrimSpace(t.OrgRegistryNo), OrgTaxID: strings.TrimSpace(t.OrgTaxID),
		Level1Center: pick(t.Level1Center, d.Level1Center), Level1CenterShort: pick(t.Level1CenterShort, d.Level1CenterShort),
		Sector: pick(t.Sector, d.Sector), Sectors: pick(t.Sectors, d.Sectors),
		SectorOffice: pick(t.SectorOffice, d.SectorOffice), SectorOfficeShort: pick(t.SectorOfficeShort, d.SectorOfficeShort),
		Center: pick(t.Center, d.Center), CenterShort: pick(t.CenterShort, d.CenterShort),
		Area: pick(t.Area, d.Area), Areas: pick(t.Areas, d.Areas), AreaShort: pick(t.AreaShort, d.AreaShort),
		AreaOffice: pick(t.AreaOffice, d.AreaOffice), AreaOfficeShort: pick(t.AreaOfficeShort, d.AreaOfficeShort),
		Subcenter: pick(t.Subcenter, d.Subcenter), UpdatedAt: t.UpdatedAt,
		LogoMime: t.LogoMime, Logo: t.Logo, LoginInfo: strings.TrimSpace(t.LoginInfo),
		RoleLabels: cleanLabels(t.RoleLabels),
	}
}

// cleanLabels zadržava samo uloge koje program poznaje i nazive koji nisu prazni
func cleanLabels(in map[string]string) map[string]string {
	out := map[string]string{}
	for _, d := range RoleCatalog {
		if v := strings.TrimSpace(in[string(d.Role)]); v != "" && v != d.Name {
			out[string(d.Role)] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Get vraća naziv po ključu kakav se piše u predlošcima
func (t OrgTerms) Get(key string) string {
	switch key {
	case "org":
		return t.OrgName
	case "direkcija":
		return t.Level1Unit
	case "gcop":
		return t.Level1Center
	case "gcop_k":
		return t.Level1CenterShort
	case "sektor":
		return t.Sector
	case "sektori":
		return t.Sectors
	case "vgo":
		return t.SectorOffice
	case "vgo_k":
		return t.SectorOfficeShort
	case "cop":
		return t.Center
	case "cop_k":
		return t.CenterShort
	case "podrucje":
		return t.Area
	case "podrucja":
		return t.Areas
	case "bp":
		return t.AreaShort
	case "vgi":
		return t.AreaOffice
	case "vgi_k":
		return t.AreaOfficeShort
	case "podcentar":
		return t.Subcenter
	case "prijava":
		return t.LoginInfo
	}
	return key
}

// Lower piše naziv s malim početnim slovom, za sredinu rečenice
func (t OrgTerms) Lower(key string) string {
	s := t.Get(key)
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToLower(r)) + s[size:]
}

var currentTerms atomic.Pointer[OrgTerms]

// Terms vraća nazive koji trenutno vrijede na ovom čvoru
func Terms() OrgTerms {
	if t := currentTerms.Load(); t != nil {
		return *t
	}
	return DefaultTerms()
}

// SetTerms postavlja nazive; zove se pri startu, nakon upisa i nakon primitka razmjenom
func SetTerms(t OrgTerms) {
	f := t.Filled()
	currentTerms.Store(&f)
}
