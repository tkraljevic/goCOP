package models

import (
	"strings"
	"time"
)

// TragPredaje je trag predaje dnevnog izvješća — izvješća sektora Glavnom
// centru i izvješća dionice podcentru obrane. Predano izvješće ne mijenja se
// potiho: spremljena izmjena vraća ga u nacrt, a nova predaja dobiva novo
// vrijeme predaje. Prvo vrijeme predaje ostaje zapisano, jer se po njemu
// gleda rok (sektor do 10:00, dionica do 08:00), uz ime onoga tko je
// izvješće mijenjao nakon predaje i onoga tko ga je predao.
//
// Čvor sa starijim programom (0.0.34) ova polja ne poznaje, ali ih knjiga
// verzija pri njegovoj izmjeni prepiše iz prethodne inačice; izmjena na
// takvom čvoru predano izvješće i dalje ostavlja predanim.
type TragPredaje struct {
	PrvoPredanoAt *time.Time `json:"prvo_predano_at,omitempty"`
	PredaoID      string     `json:"predao_id,omitempty"` // tko je predao zadnji put
	Predao        string     `json:"predao,omitempty"`
	IzmijenioID   string     `json:"izmijenio_id,omitempty"` // tko je zadnji mijenjao nakon predaje
	Izmijenio     string     `json:"izmijenio,omitempty"`
}

// prvo je vrijeme prve predaje. Izvješće predano prije nego što se prvo
// vrijeme pamtilo nosi samo vrijeme predaje, i ono je ujedno prvo.
func (t TragPredaje) prvo(predano *time.Time) *time.Time {
	if t.PrvoPredanoAt != nil {
		return t.PrvoPredanoAt
	}
	return predano
}

// ponovno javlja da je izvješće predano, i to ne prvi put
func (t TragPredaje) ponovno(predano *time.Time) bool {
	return predano != nil && t.PrvoPredanoAt != nil && !predano.Equal(*t.PrvoPredanoAt)
}

// IzmjenaPoslijePredaje bilježi spremanje izmjene izvješća koje je već bilo
// predano (sada ili prije): prvo vrijeme predaje ostaje, a pamti se tko je
// mijenjao. Za izvješće koje nikad nije predano nema što bilježiti. Da
// izmjena izvješće vraća u nacrt, brine onaj tko sprema.
func (t *TragPredaje) IzmjenaPoslijePredaje(predano *time.Time, u *User) {
	prvo := t.prvo(predano)
	if prvo == nil || u == nil {
		return
	}
	t.PrvoPredanoAt = prvo
	t.IzmijenioID, t.Izmijenio = u.ID.String(), u.FullName
}

// ZabiljeziPredaju pamti tko predaje, a kod prve predaje i njezino vrijeme
func (t *TragPredaje) ZabiljeziPredaju(kad time.Time, u *User) {
	if t.PrvoPredanoAt == nil {
		t.PrvoPredanoAt = &kad
	}
	if u != nil {
		t.PredaoID, t.Predao = u.ID.String(), u.FullName
	}
}

// opisPredaje: „predano GCOP-u 7.10. 09:40”; nakon ponovne predaje „prvi put
// predano GCOP-u 7.10. 09:40, ponovno 7.10. 14:20”; dok izmijenjeno čeka
// novu predaju „prvi put predano GCOP-u 7.10. 09:40, čeka ponovnu predaju”.
// Prazno dok izvješće nije predano nijednom.
func (t TragPredaje) opisPredaje(predano *time.Time, kome, format string) string {
	prvo := t.prvo(predano)
	if prvo == nil {
		return ""
	}
	glagol := strings.TrimSpace("predano " + kome)
	switch {
	case predano == nil:
		return "prvi put " + glagol + " " + prvo.In(Zagreb).Format(format) + ", čeka ponovnu predaju"
	case t.ponovno(predano):
		return "prvi put " + glagol + " " + prvo.In(Zagreb).Format(format) + ", ponovno " + predano.In(Zagreb).Format(format)
	}
	return glagol + " " + predano.In(Zagreb).Format(format)
}

// trag je nastavak retka o izradi dokumenta („Datum i sat izrade … ·
// Sastavio …”): tko je izvješće mijenjao nakon predaje, kad je predano i,
// kad to nije onaj tko ga je sastavio, tko ga je predao
func (t TragPredaje) trag(predano *time.Time, izradio, kome, format string) string {
	var dijelovi []string
	if t.Izmijenio != "" {
		dijelovi = append(dijelovi, "izmijenio poslije predaje: "+t.Izmijenio)
	}
	if o := t.opisPredaje(predano, kome, format); o != "" {
		if predano != nil && t.Predao != "" && t.Predao != izradio {
			o += " (predao " + t.Predao + ")"
		}
		dijelovi = append(dijelovi, o)
	}
	if len(dijelovi) == 0 {
		return ""
	}
	return " · " + strings.Join(dijelovi, " · ")
}

// predaoIli je tko je izvješće predao; za izvješće predano prije nego što se
// to pamtilo, onaj tko ga je sastavio
func (t TragPredaje) predaoIli(izradio string) string {
	if t.Predao != "" {
		return t.Predao
	}
	return izradio
}

// PrviPutPredano je vrijeme prve predaje; nil dok nije predano nijednom
func (i SektorskoIzvjesce) PrviPutPredano() *time.Time { return i.prvo(i.PredanoAt) }

// BiloPredano javlja je li izvješće bilo predano barem jednom
func (i SektorskoIzvjesce) BiloPredano() bool { return i.PrviPutPredano() != nil }

// PonovnoPredano javlja je li predano nakon izmjene, ne prvi put
func (i SektorskoIzvjesce) PonovnoPredano() bool { return i.ponovno(i.PredanoAt) }

// OpisPredaje je predaja za ispis, npr. „prvi put predano GCOP-u 09:40, ponovno 14:20”
func (i SektorskoIzvjesce) OpisPredaje(kome, format string) string {
	return i.opisPredaje(i.PredanoAt, kome, format)
}

// TragUDokumentu je nastavak retka o izradi na dokumentu i u izvozu
func (i SektorskoIzvjesce) TragUDokumentu(kome, format string) string {
	return i.trag(i.PredanoAt, i.Izradio, kome, format)
}

// TkoJePredao je tko je izvješće predao zadnji put
func (i SektorskoIzvjesce) TkoJePredao() string { return i.predaoIli(i.Izradio) }

// PovuciPredaju vraća predano izvješće u nacrt bez izmjene sadržaja; prvo
// vrijeme predaje ostaje
func (i *SektorskoIzvjesce) PovuciPredaju() {
	i.PrvoPredanoAt, i.PredanoAt = i.PrviPutPredano(), nil
}

// PrviPutPredano je vrijeme prve predaje; nil dok nije predano nijednom
func (i DnevnoIzvjesce) PrviPutPredano() *time.Time { return i.prvo(i.PredanoAt) }

// BiloPredano javlja je li izvješće bilo predano barem jednom
func (i DnevnoIzvjesce) BiloPredano() bool { return i.PrviPutPredano() != nil }

// PonovnoPredano javlja je li predano nakon izmjene, ne prvi put
func (i DnevnoIzvjesce) PonovnoPredano() bool { return i.ponovno(i.PredanoAt) }

// OpisPredaje je predaja za ispis, npr. „prvi put predano 07:40, ponovno 11:05”
func (i DnevnoIzvjesce) OpisPredaje(kome, format string) string {
	return i.opisPredaje(i.PredanoAt, kome, format)
}

// TragUDokumentu je nastavak retka o izradi na dokumentu i u izvozu
func (i DnevnoIzvjesce) TragUDokumentu(kome, format string) string {
	return i.trag(i.PredanoAt, i.Izradio, kome, format)
}

// TkoJePredao je tko je izvješće predao zadnji put
func (i DnevnoIzvjesce) TkoJePredao() string { return i.predaoIli(i.Izradio) }
