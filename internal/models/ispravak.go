package models

import (
	"time"

	"github.com/google/uuid"
)

// Ispravak arhivske vrijednosti.
//
// Arhiva se ne dira: ona je obnovljiva iz datoteka i mora ostati onakva kakvu
// su je izvori dali. Kad čovjek utvrdi da je vrijednost pogrešna, ispravak
// stoji odvojeno i pri čitanju se stavlja preko arhive — pa se u svakom
// trenutku vidi i što je izvor rekao i što je čovjek odlučio.
//
// Zato ispravak nosi i staru vrijednost i razlog: bez razloga to nije ispravak
// nego promjena bez traga.
type ArhivaIspravak struct {
	ID         uuid.UUID `json:"id"`
	Letva      string    `json:"letva"`
	Velicina   string    `json:"velicina"`
	Korak      string    `json:"korak"` // satni | dnevni
	Vrijeme    time.Time `json:"vrijeme"`
	Vrijednost float64   `json:"vrijednost"`
	Staro      *float64  `json:"staro,omitempty"`
	Razlog     string    `json:"razlog"`
	Ispravio   string    `json:"ispravio,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`

	// Izvedeno pri čitanju
	IspravioIme string `json:"-"`
}

// Razlika je koliko ispravak mijenja vrijednost.
func (i ArhivaIspravak) Razlika() float64 {
	if i.Staro == nil {
		return 0
	}
	return i.Vrijednost - *i.Staro
}
