package models

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Epizoda obrane je jedno razdoblje u kojem je na dionici vrijedila neka mjera
// obrane od poplava, od prelaska pripremnog stanja do povratka ispod njega.
//
// Vodostaji, pragovi i dionica dosad su u programu stajali odvojeno: iz
// očitanja se znala faza u trenutku, ali ne i koliko je obrana trajala ni tko
// je na njoj bio. Epizoda to spaja u zapis koji se poslije citira — "rujan
// 2024., izvanredna obrana, 36 dana".
//
// Građa je uzeta iz stvarnih podataka Batine 2013.–2025.: od 114 epizoda
// najkraća traje 5 dana, najduža 54, a neke prelaze granicu godine.
type DefenseEpisode struct {
	ID          uuid.UUID `json:"id"`
	SectionCode string    `json:"section_code"`
	StationID   string    `json:"station_id,omitempty"` // letva po kojoj je epizoda utvrđena

	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"` // prazno dok obrana traje

	// Najviši dosegnuti stupanj i vodostaj na kojem je dosegnut
	Phase  DefensePhase `json:"phase"`
	PeakCm *int         `json:"peak_cm,omitempty"`
	PeakAt *time.Time   `json:"peak_at,omitempty"`

	// Proglašenje je odluka rukovoditelja, a ne izvod iz brojeva. Obrana se zna
	// proglasiti i prije nego što vodostaj prijeđe prag — po prognozi, po
	// uzvodnim vodostajima, zbog leda ili po nalogu — pa se odluka bilježi
	// odvojeno od onoga što kažu očitanja.
	DeclaredBy  string     `json:"declared_by,omitempty"`  // tko je proglasio
	Basis       string     `json:"basis,omitempty"`        // po čemu: Basis*
	ThresholdAt *time.Time `json:"threshold_at,omitempty"` // kad je prag prijeđen po očitanjima
	EndedBy     string     `json:"ended_by,omitempty"`     // tko je prekinuo obranu

	// Podrijetlo: epizoda utvrđena računom iz očitanja ili upisana rukom.
	// Računom utvrđena epizoda smije se preračunati; upisanu ne dira se.
	Origin string `json:"origin,omitempty"` // EpisodeFrom*
	Note   string `json:"note,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Izvedeno pri čitanju
	SectionName    string `json:"-"`
	StationName    string `json:"-"`
	DeclaredByName string `json:"-"`
	EndedByName    string `json:"-"`
}

const (
	EpisodeFromReadings = "OČITANJA" // utvrđena računom iz niza očitanja
	EpisodeFromOperator = "OPERATER" // upisana rukom
)

// IsOpen govori traje li obrana još uvijek.
func (e DefenseEpisode) IsOpen() bool { return e.EndedAt == nil }

// Days je trajanje u danima, računajući i prvi i zadnji dan; otvorena epizoda
// mjeri se do sada.
func (e DefenseEpisode) Days() int {
	kraj := time.Now()
	if e.EndedAt != nil {
		kraj = *e.EndedAt
	}
	d := int(kraj.Sub(e.StartedAt).Hours()/24) + 1
	if d < 1 {
		return 1
	}
	return d
}

// PeakLabel je vrh vala s predznakom, kako se vodostaj i piše.
func (e DefenseEpisode) PeakLabel() string {
	if e.PeakCm == nil {
		return "—"
	}
	return fmt.Sprintf("%+d cm", *e.PeakCm)
}

// Overlaps govori preklapa li se epizoda sa zadanim razdobljem; otvorena
// epizoda traje do sada.
func (e DefenseEpisode) Overlaps(from, to time.Time) bool {
	kraj := time.Now()
	if e.EndedAt != nil {
		kraj = *e.EndedAt
	}
	return !e.StartedAt.After(to) && !kraj.Before(from)
}

// Osnova proglašenja: što je rukovoditelja navelo na odluku. Vodi se jer se
// obrana često proglasi prije praga, pa iz samog vodostaja ne bi bilo vidljivo
// zašto je epizoda počela kad je počela.
const (
	BasisThreshold = "PRAG"     // vodostaj je prešao prag
	BasisForecast  = "PROGNOZA" // najavljen porast
	BasisUpstream  = "UZVODNO"  // vodostaji uzvodnih postaja
	BasisIce       = "LED"      // ledene pojave
	BasisOrder     = "NALOG"    // nalog nadređene razine
	BasisOther     = "OSTALO"
)

// BasisOptions je popis osnova za obrazac, redom kojim se nude.
func BasisOptions() []string {
	return []string{BasisThreshold, BasisForecast, BasisUpstream, BasisIce, BasisOrder, BasisOther}
}

// BasisLabel je osnova proglašenja za ispis.
func BasisLabel(b string) string {
	switch b {
	case BasisThreshold:
		return "prijeđen prag"
	case BasisForecast:
		return "prognoza"
	case BasisUpstream:
		return "uzvodni vodostaji"
	case BasisIce:
		return "ledene pojave"
	case BasisOrder:
		return "nalog"
	case BasisOther:
		return "ostalo"
	}
	return b
}

// BasisLabel je osnova ove epizode za ispis.
func (e DefenseEpisode) BasisLabel() string { return BasisLabel(e.Basis) }

// DeclaredBeforeThreshold govori je li obrana proglašena prije nego što je
// vodostaj prešao prag. Prazno kad prag nije prijeđen ili nije zabilježen.
func (e DefenseEpisode) DeclaredBeforeThreshold() bool {
	return e.ThresholdAt != nil && e.StartedAt.Before(*e.ThresholdAt)
}

// LeadHours je koliko je sati obrana proglašena unaprijed; nula kad nije.
func (e DefenseEpisode) LeadHours() int {
	if !e.DeclaredBeforeThreshold() {
		return 0
	}
	return int(e.ThresholdAt.Sub(e.StartedAt).Hours())
}
