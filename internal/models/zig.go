package models

import "time"

// Zig je skenirani žig centra obrane od poplava, jedan po sektoru. Ide na
// PDF akta ovjerenog u goCOP-u, na mjesto pečata uz potpis; na ispisu za
// vlastoručni potpis ostaje prazno mjesto za pravi žig.
type Zig struct {
	Sektor    string    `json:"sektor"`
	Mime      string    `json:"mime"`  // image/png ili image/jpeg
	Slika     []byte    `json:"slika"` // najviše ZigMaxBytes
	Uredio    string    `json:"uredio,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ZigMaxBytes je najveća dopuštena veličina skena žiga
const ZigMaxBytes = 2 << 20

// PotpisSlika je skenirani vlastoručni potpis korisnika: stoji na PDF-u
// akta koji je taj korisnik ovjerio, iznad crte za potpis
type PotpisSlika struct {
	UserID    string    `json:"user_id"`
	Mime      string    `json:"mime"`
	Slika     []byte    `json:"slika"`
	UpdatedAt time.Time `json:"updated_at"`
}

// OtisciAkta su slike koje idu na PDF ovjerenog akta
type OtisciAkta struct {
	Zig    *Zig
	Potpis *PotpisSlika
}
