package models

import (
	"strings"
	"time"
)

// Dnevno izvješće rukovoditelja dionice — standardni obrazac iz Privitka 4
// Državnog plana obrane od poplava. Rukovoditelj dionice piše ga za svaki
// dan dok traje obrana i predaje do 08:00 u podcentar; iz njih voditelj
// COP-a slaže dnevno izvješće sektora za GCOP. To je interni dokument
// Hrvatskih voda, bez izvođača, listova i naloga — zato nije dnevnik, iako
// stoji uz njih.
//
// Zaglavlje (dan, dionica, stadij, tko je izradio) je u stupcima, a sam
// obrazac u jednom JSON polju: obrazac je propisan i mijenja se cijeli, a
// nijedno od tih polja se ne pretražuje.
type DnevnoIzvjesce struct {
	ID          string       `json:"id"`
	JournalID   string       `json:"journal_id,omitempty"` // dnevnik COP-a (obrana) uz koju izvješće stoji; prazno kad nije otvoren
	SectionCode string       `json:"section_code"`
	Dan         time.Time    `json:"dan"`
	Stadij      DefensePhase `json:"stadij"` // najviši stadij obrane tijekom dana

	Sadrzaj IzvjesceSadrzaj `json:"sadrzaj"`

	IzradioID  string     `json:"izradio_id"`
	Izradio    string     `json:"izradio"` // ime u trenutku izrade
	IzradenoAt time.Time  `json:"izradeno_at"`
	PredanoAt  *time.Time `json:"predano_at,omitempty"` // kad je predano u podcentar; prazno dok je nacrt

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Izvedeno pri čitanju
	Vodotok  string `json:"-"`
	Podrucje string `json:"-"`
	Sektor   string `json:"-"`
}

// IzvjesceSadrzaj je sam obrazac, odjeljak po odjeljak
type IzvjesceSadrzaj struct {
	Vodotok    string              `json:"vodotok"`
	Vodostaji  []VodostajUIzvjescu `json:"vodostaji"`
	Tendencija string              `json:"tendencija"` // Tendencija*

	// Mjere obrane od poplava
	Pregled   string `json:"pregled"`   // pregled i ocjena stanja; vrijeme uočavanja oštećenja, stanje i lokacija kritičnih mjesta
	Radnje    string `json:"radnje"`    // provedene mjere i radnje
	Vrece     string `json:"vrece"`     // ugrađene vreće
	Materijal string `json:"materijal"` // ugrađeni materijal
	Nasipi    string `json:"nasipi"`    // dužina nadvišenja i privremenih nasipa
	Crpke     string `json:"crpke"`     // broj i kapacitet mobilnih crpki
	Objekti   string `json:"objekti"`   // vrijeme aktiviranja objekata za rasterećenje, s tehničkim podacima

	// Sudjelovanje ljudi i materijalnih sredstava
	Pravne SudioniciPravne `json:"pravne"`
	Ostali SudioniciOstali `json:"ostali"`

	// Stanje na poplavljenom području
	Poplavljeno Poplavljeno `json:"poplavljeno"`
	Evakuacija  Evakuacija  `json:"evakuacija"`
}

// VodostajUIzvjescu je jedan vodostaj (protok) iz obrasca: na kojoj letvi,
// u koji sat, koliko i u čemu
type VodostajUIzvjescu struct {
	Postaja    string `json:"postaja"`
	StationID  string `json:"station_id,omitempty"`
	Sat        string `json:"sat"` // "07:00"
	Vrijednost string `json:"vrijednost"`
	Jedinica   string `json:"jedinica"`        // cm, m n.m., m³/s
	Izvor      string `json:"izvor,omitempty"` // "očitanja" kad je popunjeno iz programa
}

// Tendencija vodostaja, kako obrazac nudi
const (
	TendencijaOpadanje    = "OPADANJE"
	TendencijaStagnacija  = "STAGNACIJA"
	TendencijaPorast      = "PORAST"
	TendencijaNagliPorast = "NAGLI_PORAST"
)

// Tendencije redom obrasca
var Tendencije = []struct{ Kod, Naziv string }{
	{TendencijaOpadanje, "opadanje"}, {TendencijaStagnacija, "stagnacija"}, {TendencijaPorast, "porast"}, {TendencijaNagliPorast, "nagli porast"},
}

// TendencijaNaziv vraća naziv za prikaz
func TendencijaNaziv(kod string) string {
	for _, t := range Tendencije {
		if t.Kod == kod {
			return t.Naziv
		}
	}
	return ""
}

// SudioniciPravne su pravne osobe za provedbu preventivne, redovne i
// izvanredne obrane od poplava: ljudi i strojevi, po obrascu
type SudioniciPravne struct {
	Ljudi        int    `json:"ljudi"`
	Kamioni      int    `json:"kamioni"`
	Bageri       int    `json:"bageri"`
	KombStrojevi int    `json:"komb_strojevi"`
	Utovarivaci  int    `json:"utovarivaci"`
	Buldozeri    int    `json:"buldozeri"`
	Traktori     int    `json:"traktori"`
	Brodovi      int    `json:"brodovi"`
	Camci        int    `json:"camci"`
	Ostalo       string `json:"ostalo"`
}

// SudioniciOstali su ostali sudionici obrane
type SudioniciOstali struct {
	Policija       int    `json:"policija"`
	Vatrogasci     int    `json:"vatrogasci"`
	Vojska         int    `json:"vojska"`
	HGSS           int    `json:"hgss"`
	CivilnaZastita int    `json:"civilna_zastita"`
	CrveniKriz     int    `json:"crveni_kriz"`
	Drugi          string `json:"drugi"` // druge pravne osobe i građani
}

// Poplavljeno je stanje na poplavljenom području
type Poplavljeno struct {
	Naselja          string  `json:"naselja"`
	Ljudi            int     `json:"ljudi"`
	Stambeni         int     `json:"stambeni"`
	Industrijski     int     `json:"industrijski"`
	Farme            int     `json:"farme"`
	Infrastruktura   string  `json:"infrastruktura"` // ceste, mostovi…
	SumskeHa         float64 `json:"sumske_ha"`
	PoljoprivredneHa float64 `json:"poljoprivredne_ha"`
	OstaleHa         float64 `json:"ostale_ha"`
}

// Evakuacija je provedena evakuacija
type Evakuacija struct {
	Naselja    string `json:"naselja"`
	Ljudi      int    `json:"ljudi"`
	Kucanstava int    `json:"kucanstava"`
	Zivotinje  string `json:"zivotinje"` // broj i vrsta
}

// Predano javlja je li izvješće predano u podcentar
func (i DnevnoIzvjesce) Predano() bool { return i.PredanoAt != nil }

// DanKey je dan u obliku 2006-01-02
func (i DnevnoIzvjesce) DanKey() string { return i.Dan.In(Zagreb).Format("2006-01-02") }

// StadijKratica je oznaka stadija kako stoji na obrascu: PS, R, I, IS
func (i DnevnoIzvjesce) StadijKratica() string { return StadijKratica(i.Stadij) }

// StadijKratica vraća oznaku s obrasca za stadij obrane
func StadijKratica(p DefensePhase) string {
	switch p {
	case PhasePrep:
		return "PS"
	case PhaseRegular:
		return "R"
	case PhaseEmergency:
		return "I"
	case PhaseState:
		return "IS"
	}
	return "–"
}

// StadijiObrane su stadiji koje obrazac nudi, redom težine
var StadijiObrane = []DefensePhase{PhasePrep, PhaseRegular, PhaseEmergency, PhaseState}

// Prazno javlja ima li obrazac išta upisano osim zaglavlja
func (s IzvjesceSadrzaj) Prazno() bool {
	return strings.TrimSpace(s.Pregled+s.Radnje+s.Objekti+s.Vrece+s.Materijal+s.Nasipi+s.Crpke) == "" && len(s.Vodostaji) == 0 &&
		s.Pravne == (SudioniciPravne{}) && s.Ostali == (SudioniciOstali{}) && s.Poplavljeno == (Poplavljeno{}) && s.Evakuacija == (Evakuacija{})
}
