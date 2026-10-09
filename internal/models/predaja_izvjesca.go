package models

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// TragPredaje je trag predaje dnevnog izvješća — izvješća sektora Glavnom
// centru i izvješća dionice podcentru obrane. Predano izvješće ne mijenja se
// potiho: spremljena izmjena vraća ga u nacrt, a nova predaja dobiva novo
// vrijeme predaje. Prvo vrijeme predaje ostaje zapisano, jer se po njemu
// gleda rok (sektor do 10:00, dionica do 08:00), uz imena svih koji su
// izvješće mijenjali nakon predaje i onoga tko ga je predao.
//
// Čvor sa starijim programom (0.0.34) ova polja ne poznaje, ali ih knjiga
// verzija pri njegovoj izmjeni prepiše iz prethodne inačice; izmjena na
// takvom čvoru predano izvješće i dalje ostavlja predanim. Predaju na
// takvom čvoru program ne pripisuje onome tko je predao prije: PredaoAt
// kaže za koju je predaju zapisano tko je predao.
type TragPredaje struct {
	PrvoPredanoAt *time.Time `json:"prvo_predano_at,omitempty"`
	PredaoID      string     `json:"predao_id,omitempty"` // tko je predao zadnji put
	Predao        string     `json:"predao,omitempty"`
	PredaoAt      *time.Time `json:"predao_at,omitempty"` // vrijeme predaje za koju su zapisani PredaoID i Predao
	// svi koji su izvješće mijenjali nakon prve predaje, redom prve izmjene
	Izmijenili []Izmjenitelj `json:"izmijenili,omitempty"`
}

// Izmjenitelj je onaj tko je izvješće mijenjao nakon predaje
type Izmjenitelj struct {
	ID  string `json:"id"`
	Ime string `json:"ime"`
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

// IzmjenaPoslijePredaje bilježi spremanje izvješća koje je već bilo
// predano (sada ili prije): prvo vrijeme predaje ostaje, a kad je spremanje
// izvješće stvarno promijenilo, onaj tko sprema dodaje se onima koji su ga
// mijenjali — uz njih, ne umjesto njih. Spremanje bez promjene ne dodaje
// nikoga. Za izvješće koje nikad nije predano nema što bilježiti. Da
// izmjena izvješće vraća u nacrt, brine onaj tko sprema.
func (t *TragPredaje) IzmjenaPoslijePredaje(predano *time.Time, u *User, promijenjeno bool) {
	prvo := t.prvo(predano)
	if prvo == nil {
		return
	}
	t.PrvoPredanoAt = prvo
	if promijenjeno {
		t.dodajIzmjenitelja(u)
	}
}

// dodajIzmjenitelja dodaje osobu onima koji su izvješće mijenjali, jednom
func (t *TragPredaje) dodajIzmjenitelja(u *User) {
	if u == nil {
		return
	}
	id := u.ID.String()
	for _, i := range t.Izmijenili {
		if i.ID == id {
			return
		}
	}
	t.Izmijenili = append(t.Izmijenili, Izmjenitelj{ID: id, Ime: u.FullName})
}

// TkoJeMijenjao su imena svih koji su izvješće mijenjali nakon predaje,
// redom prve izmjene („Ivo Ivić, Marko Markić”); prazno kad nitko
func (t TragPredaje) TkoJeMijenjao() string {
	imena := make([]string, len(t.Izmijenili))
	for k, i := range t.Izmijenili {
		imena[k] = i.Ime
	}
	return strings.Join(imena, ", ")
}

// izmjeneZaIspis: „izmijenio poslije predaje: Ivo Ivić”, a kad ih je više
// „izmijenili poslije predaje: Ivo Ivić, Marko Markić”; prazno kad nitko
func (t TragPredaje) izmjeneZaIspis() string {
	switch len(t.Izmijenili) {
	case 0:
		return ""
	case 1:
		return "izmijenio poslije predaje: " + t.TkoJeMijenjao()
	}
	return "izmijenili poslije predaje: " + t.TkoJeMijenjao()
}

// istiJSON: dvije vrijednosti su iste kad se jednako zapisuju; što se ne da
// zapisati, nije isto
func istiJSON(a, b any) bool {
	ja, ea := json.Marshal(a)
	jb, eb := json.Marshal(b)
	return ea == nil && eb == nil && bytes.Equal(ja, jb)
}

// ZabiljeziPredaju pamti tko predaje, a kod prve predaje i njezino vrijeme
func (t *TragPredaje) ZabiljeziPredaju(kad time.Time, u *User) {
	if t.PrvoPredanoAt == nil {
		t.PrvoPredanoAt = &kad
	}
	if u != nil {
		t.PredaoID, t.Predao, t.PredaoAt = u.ID.String(), u.FullName, &kad
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
// Sastavio …”): tko je sve izvješće mijenjao nakon predaje, kad je predano i,
// kad se zna i nije onaj tko ga je sastavio, tko ga je predao
func (t TragPredaje) trag(predano *time.Time, izradio, kome, format string) string {
	var dijelovi []string
	if iz := t.izmjeneZaIspis(); iz != "" {
		dijelovi = append(dijelovi, iz)
	}
	if o := t.opisPredaje(predano, kome, format); o != "" {
		if p := t.predaoIli(predano, izradio); predano != nil && p != "" && p != izradio {
			o += " (predao " + p + ")"
		}
		dijelovi = append(dijelovi, o)
	}
	if len(dijelovi) == 0 {
		return ""
	}
	return " · " + strings.Join(dijelovi, " · ")
}

// predaoIli je tko je izvješće predao u predaji s vremenom predano.
// Zapisani predavatelj vrijedi samo za predaju za koju je zapisan. Ponovnu
// predaju na čvoru sa starijim programom (0.0.34), koji predavatelja ne
// pamti, nije predao onaj tko je predao prije, pa se tada ne zna tko ju je
// predao (prazno). Za izvješće predano prije nego što se to pamtilo, onaj
// tko ga je sastavio.
func (t TragPredaje) predaoIli(predano *time.Time, izradio string) string {
	switch {
	case t.predaoZa(predano):
		return t.Predao
	case t.ponovno(predano):
		return ""
	}
	return izradio
}

// predaoZa javlja je li zapisani predavatelj predao baš predaju s vremenom predano
func (t TragPredaje) predaoZa(predano *time.Time) bool {
	return t.Predao != "" && predano != nil && t.PredaoAt != nil && predano.Equal(*t.PredaoAt)
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

// TkoJePredao je tko je izvješće predao zadnji put; prazno kad se ne zna
func (i SektorskoIzvjesce) TkoJePredao() string { return i.predaoIli(i.PredanoAt, i.Izradio) }

// PovuciPredaju vraća predano izvješće u nacrt bez izmjene sadržaja; prvo
// vrijeme predaje ostaje
func (i *SektorskoIzvjesce) PovuciPredaju() {
	i.PrvoPredanoAt, i.PredanoAt = i.PrviPutPredano(), nil
}

// ZabiljeziSpremanje bilježi u trag spremanje izvješća koje je već bilo
// predano: prvo vrijeme predaje ostaje, a onaj tko sprema pamti se među
// onima koji su ga mijenjali samo kad je promijenio dan ili sadržaj. prije
// je spremljena inačica (trag je već prepisan iz nje); nil za novo izvješće.
func (i *SektorskoIzvjesce) ZabiljeziSpremanje(prije *SektorskoIzvjesce, u *User) {
	if prije == nil {
		return
	}
	i.IzmjenaPoslijePredaje(prije.PredanoAt, u, prije.DanKey() != i.DanKey() || !istiJSON(prije.Sadrzaj, i.Sadrzaj))
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

// TkoJePredao je tko je izvješće predao zadnji put; prazno kad se ne zna
func (i DnevnoIzvjesce) TkoJePredao() string { return i.predaoIli(i.PredanoAt, i.Izradio) }

// ZabiljeziSpremanje bilježi u trag spremanje izvješća koje je već bilo
// predano: prvo vrijeme predaje ostaje, a onaj tko sprema pamti se među
// onima koji su ga mijenjali samo kad je promijenio dan, stadij ili
// sadržaj. prije je spremljena inačica (trag je već prepisan iz nje); nil
// za novo izvješće.
func (i *DnevnoIzvjesce) ZabiljeziSpremanje(prije *DnevnoIzvjesce, u *User) {
	if prije == nil {
		return
	}
	promijenjeno := prije.DanKey() != i.DanKey() || prije.Stadij != i.Stadij || !istiJSON(prije.Sadrzaj, i.Sadrzaj)
	i.IzmjenaPoslijePredaje(prije.PredanoAt, u, promijenjeno)
}
