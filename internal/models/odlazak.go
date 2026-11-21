package models

import (
	"slices"
	"time"

	"github.com/google/uuid"
)

// RazlogOdlaska je zašto je osoba otišla iz službe. Račun osobe koja je
// otišla ostaje, s isključenom prijavom i bez zaduženja, da njezini upisi i
// očitanja zadrže ime; briše se samo račun kojim se nitko nikad nije
// prijavio.
type RazlogOdlaska string

const (
	OdlazakMirovina   RazlogOdlaska = "mirovina"
	OdlazakOdlazak    RazlogOdlaska = "odlazak"
	OdlazakPremjestaj RazlogOdlaska = "premjestaj"
	OdlazakDrugo      RazlogOdlaska = "drugo"
)

// RazloziOdlaska su razlozi redom kao u obrascu
var RazloziOdlaska = []RazlogOdlaska{OdlazakMirovina, OdlazakOdlazak, OdlazakPremjestaj, OdlazakDrugo}

// Valjan javlja je li razlog jedan od poznatih
func (r RazlogOdlaska) Valjan() bool { return slices.Contains(RazloziOdlaska, r) }

// Label je razlog u obrascu
func (r RazlogOdlaska) Label() string {
	switch r {
	case OdlazakMirovina:
		return "Mirovina"
	case OdlazakOdlazak:
		return "Odlazak"
	case OdlazakPremjestaj:
		return "Premještaj"
	case OdlazakDrugo:
		return "Drugo"
	}
	return string(r)
}

// Opis je razlog u rečenici: „odlazak u mirovinu”
func (r RazlogOdlaska) Opis() string {
	switch r {
	case OdlazakMirovina:
		return "odlazak u mirovinu"
	case OdlazakPremjestaj:
		return "premještaj"
	}
	return "odlazak"
}

// IzvanSluzbe javlja je li osoba otišla iz službe
func (u *User) IzvanSluzbe() bool { return u.Odlazak != "" }

// OpisOdlaska je „Mirovina od 1. 12. 2026.” za stranicu osobe; prazno dok
// osoba radi
func (u *User) OpisOdlaska() string {
	if !u.IzvanSluzbe() {
		return ""
	}
	s := u.Odlazak.Label()
	if u.OdlazakDan != nil {
		s += " od " + u.OdlazakDan.In(Zagreb).Format("2. 1. 2006.")
	}
	return s
}

// IzmjenaZapisa je tko je napisao verziju računa ili zaduženja i kada. Ide
// samo u knjigu verzija (zapis na površini je nema), da Događanja pokažu
// tko je dao, promijenio ili opozvao ovlasti. Kad je isto vrijeme koje
// verzija nosi: stariji program koji polje ne zna prepiše ga u svoju novu
// verziju iz prethodne, pa ime vrijedi samo kad se vremena slažu
// (VrijediZa).
type IzmjenaZapisa struct {
	Tko uuid.UUID `json:"tko"`
	Kad time.Time `json:"kad"`
}

// VrijediZa javlja pripada li izmjena verziji nastaloj u trenutku nastala:
// upisuju se u istoj transakciji, pa se razlikuju za djelić sekunde
func (i *IzmjenaZapisa) VrijediZa(nastala time.Time) bool {
	if i == nil || i.Tko == uuid.Nil {
		return false
	}
	d := i.Kad.Sub(nastala)
	return d > -time.Minute && d < time.Minute
}
