package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Razlozi odlaska imaju naziv u obrascu i opis u rečenici, a nepoznat
// razlog nije valjan
func TestRazloziOdlaska(t *testing.T) {
	ocekivano := map[RazlogOdlaska][2]string{
		OdlazakMirovina:   {"Mirovina", "odlazak u mirovinu"},
		OdlazakOdlazak:    {"Odlazak", "odlazak"},
		OdlazakPremjestaj: {"Premještaj", "premještaj"},
		OdlazakDrugo:      {"Drugo", "odlazak"},
	}
	for _, r := range RazloziOdlaska {
		if !r.Valjan() || r.Label() != ocekivano[r][0] || r.Opis() != ocekivano[r][1] {
			t.Errorf("%s: valjan %v, %q, %q", r, r.Valjan(), r.Label(), r.Opis())
		}
	}
	if RazlogOdlaska("otkaz").Valjan() || RazlogOdlaska("otkaz").Label() != "otkaz" {
		t.Error("nepoznat razlog")
	}
}

// Opis odlaska je prazan dok osoba radi, a inače razlog s danom
func TestOpisOdlaska(t *testing.T) {
	u := &User{}
	if u.IzvanSluzbe() || u.OpisOdlaska() != "" {
		t.Error("osoba koja radi je izvan službe")
	}
	u.Odlazak = OdlazakPremjestaj
	if !u.IzvanSluzbe() || u.OpisOdlaska() != "Premještaj" {
		t.Errorf("bez dana: %q", u.OpisOdlaska())
	}
	dan := time.Date(2026, 12, 1, 0, 0, 0, 0, Zagreb)
	u.OdlazakDan = &dan
	if u.OpisOdlaska() != "Premještaj od 1. 12. 2026." {
		t.Errorf("s danom: %q", u.OpisOdlaska())
	}
}

// Ime izmjene vrijedi samo za verziju u kojoj je zapisano: stariji program
// prepiše polje iz prethodne verzije, a tada se vremena ne slažu
func TestIzmjenaVrijediSamoZaSvojuVerziju(t *testing.T) {
	sad := time.Now()
	i := &IzmjenaZapisa{Tko: uuid.New(), Kad: sad}
	if !i.VrijediZa(sad.Add(20 * time.Millisecond)) {
		t.Error("izmjena ne vrijedi za svoju verziju")
	}
	if i.VrijediZa(sad.Add(time.Hour)) || i.VrijediZa(sad.Add(-time.Hour)) {
		t.Error("izmjena vrijedi za drugu verziju")
	}
	var nema *IzmjenaZapisa
	if nema.VrijediZa(sad) || (&IzmjenaZapisa{Kad: sad}).VrijediZa(sad) {
		t.Error("izmjena bez osobe vrijedi")
	}
}
