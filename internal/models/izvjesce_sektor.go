package models

import (
	"strings"
	"time"
)

// Dnevno izvješće rukovoditelja sektora — točka 1.3 Privitka 4. Slaže ga
// voditelj COP-a iz predanih izvješća dionica za taj dan i iz zapisa
// dnevnika COP-a, dopiše hidrometeorološke uvjete i ocjenu, a potpisuje
// rukovoditelj sektora. Predaje se Glavnom centru do 10:00.
//
// Tablice iz izvješća dionica snimaju se u sadržaj kad se izvješće spremi:
// dokument koji je otišao u GCOP mora ostati isti i kad netko poslije
// popravi izvješće dionice.
type SektorskoIzvjesce struct {
	ID        string    `json:"id"`
	JournalID string    `json:"journal_id,omitempty"` // dnevnik COP-a obrane
	Sektor    string    `json:"sektor"`
	Dan       time.Time `json:"dan"`

	Sadrzaj SektorskiSadrzaj `json:"sadrzaj"`

	IzradioID  string     `json:"izradio_id"`
	Izradio    string     `json:"izradio"`
	IzradenoAt time.Time  `json:"izradeno_at"`
	PredanoAt  *time.Time `json:"predano_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SektorskiSadrzaj je tekst koji voditelj piše, plus snimka onoga što je
// zbrojeno iz dionica i preuzeto iz dnevnika
type SektorskiSadrzaj struct {
	Hidrometeo  string `json:"hidrometeo"`  // opis hidrometeoroloških uvjeta po branjenim područjima
	Ostecenja   string `json:"ostecenja"`   // vrijeme uočavanja oštećenja, stanje i lokacije kritičnih mjesta
	Mjere       string `json:"mjere"`       // zbirni opis provedenih mjera po branjenim područjima
	Objekti     string `json:"objekti"`     // aktiviranje objekata za rasterećenje
	Poplavljeno string `json:"poplavljeno"` // opis poplavljenih područja po BP i županijama
	Evakuacija  string `json:"evakuacija"`  // provedene evakuacije
	Napomena    string `json:"napomena"`    // ocjena stanja, najava, što GCOP treba znati

	Pregled SektorskiPregled `json:"pregled"`
	Zapisi  []ZapisUIzvjescu `json:"zapisi"`
}

// SektorskiPregled je snimka izvješća dionica: po branjenim područjima, sa
// zbrojevima, i po vodotocima
type SektorskiPregled struct {
	Podrucja []PodrucjeUPregledu `json:"podrucja"`
	Ukupno   ZbrojSudionika      `json:"ukupno"`
	Vodotoci []VodotokUPregledu  `json:"vodotoci"`
	Izvjesca int                 `json:"izvjesca"` // koliko je izvješća dionica ušlo
	Nacrta   int                 `json:"nacrta"`   // koliko ih je za taj dan ostalo nepredano, izvan pregleda
}

// PodrucjeUPregledu je jedno branjeno područje s dionicama koje su javile
type PodrucjeUPregledu struct {
	AreaID  int                `json:"area_id"`
	Naziv   string             `json:"naziv"`
	Dionice []DionicaUPregledu `json:"dionice"`
	Zbroj   ZbrojSudionika     `json:"zbroj"`
}

// DionicaUPregledu je redak iz izvješća jedne dionice
type DionicaUPregledu struct {
	IzvjesceID string       `json:"izvjesce_id"`
	Code       string       `json:"code"`
	Vodotok    string       `json:"vodotok"`
	Stadij     DefensePhase `json:"stadij"`
	Vodostaji  string       `json:"vodostaji"` // "Batina 07:00 +571 cm; 12:00 +583 cm"
	Tendencija string       `json:"tendencija"`
	Vrece      string       `json:"vrece"`
	Materijal  string       `json:"materijal"`
	Nasipi     string       `json:"nasipi"`
	Crpke      string       `json:"crpke"`
	Izradio    string       `json:"izradio"`
}

// ZbrojSudionika su zbrojeni ljudi, strojevi i stanje; tekstualna polja
// (ostalo, drugi, naselja…) spajaju se točkom sa zarezom
type ZbrojSudionika struct {
	Pravne      SudioniciPravne `json:"pravne"`
	Ostali      SudioniciOstali `json:"ostali"`
	Poplavljeno Poplavljeno     `json:"poplavljeno"`
	Evakuacija  Evakuacija      `json:"evakuacija"`
}

// Dodaj zbraja jedno izvješće dionice
func (z *ZbrojSudionika) Dodaj(s IzvjesceSadrzaj) {
	p, o := s.Pravne, s.Ostali
	z.Pravne.Ljudi += p.Ljudi
	z.Pravne.Kamioni += p.Kamioni
	z.Pravne.Bageri += p.Bageri
	z.Pravne.KombStrojevi += p.KombStrojevi
	z.Pravne.Utovarivaci += p.Utovarivaci
	z.Pravne.Buldozeri += p.Buldozeri
	z.Pravne.Traktori += p.Traktori
	z.Pravne.Brodovi += p.Brodovi
	z.Pravne.Camci += p.Camci
	z.Pravne.Ostalo = spoji(z.Pravne.Ostalo, p.Ostalo)
	z.Ostali.Policija += o.Policija
	z.Ostali.Vatrogasci += o.Vatrogasci
	z.Ostali.Vojska += o.Vojska
	z.Ostali.HGSS += o.HGSS
	z.Ostali.CivilnaZastita += o.CivilnaZastita
	z.Ostali.CrveniKriz += o.CrveniKriz
	z.Ostali.Drugi = spoji(z.Ostali.Drugi, o.Drugi)
	pp, e := s.Poplavljeno, s.Evakuacija
	z.Poplavljeno.Naselja = spoji(z.Poplavljeno.Naselja, pp.Naselja)
	z.Poplavljeno.Ljudi += pp.Ljudi
	z.Poplavljeno.Stambeni += pp.Stambeni
	z.Poplavljeno.Industrijski += pp.Industrijski
	z.Poplavljeno.Farme += pp.Farme
	z.Poplavljeno.Infrastruktura = spoji(z.Poplavljeno.Infrastruktura, pp.Infrastruktura)
	z.Poplavljeno.SumskeHa += pp.SumskeHa
	z.Poplavljeno.PoljoprivredneHa += pp.PoljoprivredneHa
	z.Poplavljeno.OstaleHa += pp.OstaleHa
	z.Evakuacija.Naselja = spoji(z.Evakuacija.Naselja, e.Naselja)
	z.Evakuacija.Ljudi += e.Ljudi
	z.Evakuacija.Kucanstava += e.Kucanstava
	z.Evakuacija.Zivotinje = spoji(z.Evakuacija.Zivotinje, e.Zivotinje)
}

// Prazan javlja da nitko nije javio ni ljude ni stanje
func (z ZbrojSudionika) Prazan() bool {
	return z.Pravne == (SudioniciPravne{}) && z.Ostali == (SudioniciOstali{}) && z.Poplavljeno == (Poplavljeno{}) && z.Evakuacija == (Evakuacija{})
}

func spoji(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	switch {
	case b == "":
		return a
	case a == "":
		return b
	}
	return a + "; " + b
}

// VodotokUPregledu je stanje jednog vodotoka kroz sektor: najviši stadij
// na njemu i tendencija kako je javila dionica s najvišim stadijem
type VodotokUPregledu struct {
	Vodotok    string       `json:"vodotok"`
	Stadij     DefensePhase `json:"stadij"`
	Tendencija string       `json:"tendencija"`
	Dionice    []string     `json:"dionice"`
	Vodostaji  string       `json:"vodostaji"`
}

// ZapisUIzvjescu je zapis iz dnevnika COP-a preuzet u izvješće
type ZapisUIzvjescu struct {
	ID       string `json:"id"`
	Vrijeme  string `json:"vrijeme"` // "07:15"
	Podrucje string `json:"podrucje"`
	Vrsta    string `json:"vrsta"`
	Javio    string `json:"javio"`
	Tekst    string `json:"tekst"`
}

// Predano javlja je li izvješće predano Glavnom centru
func (i SektorskoIzvjesce) Predano() bool { return i.PredanoAt != nil }

// DanKey je dan u obliku 2006-01-02
func (i SektorskoIzvjesce) DanKey() string { return i.Dan.In(Zagreb).Format("2006-01-02") }

// NajvisiStadij je najviši stadij obrane na sektoru toga dana
func (i SektorskoIzvjesce) NajvisiStadij() DefensePhase {
	naj := PhaseNormal
	for _, v := range i.Sadrzaj.Pregled.Vodotoci {
		if v.Stadij.Severity() > naj.Severity() {
			naj = v.Stadij
		}
	}
	return naj
}

// Prazno javlja ima li išta osim zaglavlja: bar jedno izvješće dionice ili
// zapis, ili neki tekst
func (s SektorskiSadrzaj) Prazno() bool {
	return s.Pregled.Izvjesca == 0 && len(s.Zapisi) == 0 &&
		strings.TrimSpace(s.Hidrometeo+s.Ostecenja+s.Mjere+s.Objekti+s.Poplavljeno+s.Evakuacija+s.Napomena) == ""
}
