package models

import (
	"sort"
	"strings"
	"time"
)

// Moduli su dijelovi programa koje račun vidi u izborniku i smije otvoriti.
// Vidljivost je zaseban sloj od prava pisanja: dužnosti kažu što tko SMIJE
// upisati, moduli kažu što tko VIDI. Zadano se izvodi iz uloge, administrator
// mijenja pravilo za ulogu ili napravi iznimku za pojedini račun. Početna
// stranica, profil i odjava vidljivi su uvijek.
type Module struct {
	ID    string
	Label string
	Desc  string
}

const (
	ModuleField     = "teren"
	ModuleReadings  = "vodostaji"
	ModuleRegisters = "registri"
	ModuleJournals  = "dnevnici"
	ModuleUsers     = "djelatnici"
	ModuleSettings  = "postavke"
)

// Modules su moduli redom kojim stoje u izborniku
var Modules = []Module{
	{ModuleField, "Teren", "letve koje osoba obilazi i upis očitanja na jedan dodir"},
	{ModuleReadings, "Vodostaji", "zadnja očitanja svih letvi, povijest i graf"},
	{ModuleJournals, "Dnevnici", "građevinski dnevnici održavanja i obrane: listovi, upisi, nalozi"},
	{ModuleRegisters, "Registri", "dionice, područja, postaje, objekti, vodotoci, održavanje"},
	{ModuleUsers, "Djelatnici", "imenik, dužnosti i ovlasti"},
	{ModuleSettings, "Postavke", "čvor, mreža, uparivanje, moduli"},
}

// ModuleLabel je naziv modula u sučelju
func ModuleLabel(id string) string {
	for _, m := range Modules {
		if m.ID == id {
			return m.Label
		}
	}
	return id
}

// IsModule javlja postoji li modul s tim nazivom
func IsModule(id string) bool {
	for _, m := range Modules {
		if m.ID == id {
			return true
		}
	}
	return false
}

// DefaultModules su moduli koje uloga vidi dok administrator ne odluči drukčije
func DefaultModules(r Role) []string {
	switch {
	case r == RoleGlobalAdmin:
		return []string{ModuleField, ModuleReadings, ModuleJournals, ModuleRegisters, ModuleUsers, ModuleSettings}
	case r == RoleServiceLeaderForeman:
		// izvođač: početni pregled terena i vlastiti dnevnici
		return []string{ModuleField, ModuleJournals}
	case r.IsField():
		return []string{ModuleField, ModuleReadings}
	case r == RoleViewer:
		return []string{ModuleField, ModuleReadings, ModuleRegisters}
	case r == RoleCopLeader || r == RoleCopDeputy || r == RoleAreaAdmin ||
		r == RoleNationalLeader || r == RoleNationalDeputy:
		return []string{ModuleField, ModuleReadings, ModuleJournals, ModuleRegisters, ModuleUsers, ModuleSettings}
	}
	// rukovoditelji sektora, područja i dionica, operateri, ovlaštenici
	return []string{ModuleField, ModuleReadings, ModuleJournals, ModuleRegisters, ModuleUsers}
}

// RoleModules je pravilo vidljivosti za jednu ulogu; putuje među čvorovima
type RoleModules struct {
	Role      string    `json:"role"`
	Modules   []string  `json:"modules"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UserModules je iznimka za jedan račun: što se prikazuje i što se skriva
// bez obzira na ulogu
type UserModules struct {
	UserID    string    `json:"user_id"`
	Shown     []string  `json:"shown,omitempty"`
	Hidden    []string  `json:"hidden,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Visibility je skup modula koje račun vidi
type Visibility map[string]bool

// Sees javlja vidi li račun modul; prazan skup (nema prijave) ne skriva ništa,
// da se stranice bez konteksta ne bi slomile
func (v Visibility) Sees(module string) bool {
	if v == nil {
		return true
	}
	return v[module]
}

// List vraća vidljive module redom izbornika
func (v Visibility) List() []string {
	var out []string
	for _, m := range Modules {
		if v[m.ID] {
			out = append(out, m.ID)
		}
	}
	return out
}

// ResolveModules slaže što račun vidi: unija pravila svih aktivnih dužnosti
// (ili zadanog za ulogu), pa iznimke računa. Globalni administrator vidi sve,
// da nitko ne može zaključati ni sebe ni program.
func ResolveModules(u *User, perms *UserPermissions, rules map[string][]string, override *UserModules) Visibility {
	v := Visibility{}
	if u == nil {
		return nil
	}
	if u.IsGlobalAdmin || (perms != nil && perms.IsGlobalAdmin) {
		for _, m := range Modules {
			v[m.ID] = true
		}
		return v
	}
	roles := map[Role]bool{}
	for _, d := range u.Duties {
		if d.IsActive {
			roles[d.Role] = true
		}
	}
	if len(roles) == 0 {
		roles[RoleViewer] = true
	}
	for r := range roles {
		mods, ok := rules[string(r)]
		if !ok {
			mods = DefaultModules(r)
		}
		for _, m := range mods {
			v[m] = true
		}
	}
	if override != nil {
		for _, m := range override.Shown {
			v[m] = true
		}
		for _, m := range override.Hidden {
			delete(v, m)
		}
	}
	return v
}

// JoinModules i SplitModules su zapis popisa u jednom stupcu
func JoinModules(mods []string) string {
	var out []string
	for _, m := range mods {
		if IsModule(m) {
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func SplitModules(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" && IsModule(p) {
			out = append(out, p)
		}
	}
	return out
}
