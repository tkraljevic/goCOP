package models

import (
	"sort"
	"strings"
	"time"
)

// Materijalno-tehnička sredstva za obranu od poplava: što stoji po
// skladištima, kako se tijekom obrane troši i gdje ode, i koliko ga je na
// dan godišnjeg popisa. Popis sredstava po skladištima jednom godišnje ide
// Glavnom centru, sa stanjem na 31. prosinca i dodatnim potrebama za
// nabavom — isti obrazac za sve sektore.
//
// Količine se ne upisuju kao broj koji netko prepravlja, nego se izvode iz
// prometa: svaki ulaz i izlaz je redak, a stanje je njihov zbroj. Tako se
// zna ne samo koliko ima, nego i kad je i po čemu se promijenilo — a
// godišnji popis je usklađenje, ne brisanje povijesti.

// Skupine sredstava, kako ih popis dijeli
const (
	GrupaOprema    = "OPREMA"
	GrupaAlat      = "ALAT"
	GrupaMaterijal = "MATERIJAL"
	GrupaPribor    = "PRIBOR"
)

// GrupeSredstava su skupine redom kojim stoje u popisu, s rimskom oznakom
var GrupeSredstava = []struct{ ID, Rimski, Naziv string }{
	{GrupaOprema, "I", "Oprema"},
	{GrupaAlat, "II", "Alat"},
	{GrupaMaterijal, "III", "Materijal"},
	{GrupaPribor, "IV", "Pribor i osobna zaštitna sredstva"},
}

// GrupaNaziv je naziv skupine za prikaz
func GrupaNaziv(id string) string {
	for _, g := range GrupeSredstava {
		if g.ID == id {
			return g.Naziv
		}
	}
	return id
}

// GrupaRimski je oznaka skupine u popisu: I, II, III, IV
func GrupaRimski(id string) string {
	for _, g := range GrupeSredstava {
		if g.ID == id {
			return g.Rimski
		}
	}
	return ""
}

// Oblik u kojem sredstvo stoji. Vreća je ista stvar prazna i napunjena, ali
// se ne broji zajedno: prazne čekaju u skladištu, pune idu na nasip. Isto
// vrijedi za sredstvo koje je neispravno — u skladištu je, ali se na njega
// ne računa.
const (
	OblikOsnovni     = ""            // sredstvo bez oblika; jedan redak
	OblikPrazno      = "PRAZNO"      // vreća prazna, spremna za punjenje
	OblikPunjeno     = "PUNJENO"     // vreća napunjena
	OblikPostavljeno = "POSTAVLJENO" // barijera postavljena, nije u skladištu slobodna
	OblikNeispravno  = "NEISPRAVNO"  // u skladištu, ali se ne može upotrijebiti
)

// OblikNaziv je naziv oblika za prikaz
func OblikNaziv(o string) string {
	switch o {
	case OblikPrazno:
		return "prazno"
	case OblikPunjeno:
		return "napunjeno"
	case OblikPostavljeno:
		return "postavljeno"
	case OblikNeispravno:
		return "neispravno"
	}
	return "osnovno"
}

// VrstaSredstva je jedan redak popisa: što je, u čemu se mjeri i u kojim
// oblicima stoji. Popis je propisan i isti za sve sektore, pa se vrste ne
// izmišljaju po centru; organizacija ipak smije dodati svoju i ugasiti onu
// koju više ne vodi.
type VrstaSredstva struct {
	ID         string    `json:"id"`         // "vrece-50x80"
	Grupa      string    `json:"grupa"`      // Grupa*
	Redoslijed int       `json:"redoslijed"` // mjesto u skupini, od 1
	Naziv      string    `json:"naziv"`
	Jedinica   string    `json:"jedinica"`         // kom, kg, m³, m², m', par
	Oblici     []string  `json:"oblici,omitempty"` // prazno = sredstvo bez oblika
	Aktivna    bool      `json:"aktivna"`
	Napomena   string    `json:"napomena,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// SviOblici su oblici u kojima se vrsta vodi; sredstvo bez oblika ima jedan
// prazan, da se stanje uvijek broji na isti način
func (v VrstaSredstva) SviOblici() []string {
	if len(v.Oblici) == 0 {
		return []string{OblikOsnovni}
	}
	return v.Oblici
}

// ImaOblike javlja vodi li se vrsta u više oblika
func (v VrstaSredstva) ImaOblike() bool { return len(v.Oblici) > 0 }

// Skladiste je mjesto na kojem sredstva stoje. Vezuje se na branjeno
// područje, a kad stoji u tuđem krugu ili u našem objektu, i na firmu
// odnosno objekt iz registra — pa se adresa i kontakt ne prepisuju.
type Skladiste struct {
	ID           string `json:"id"`
	Sektor       string `json:"sektor"`
	AreaID       int    `json:"area_id"`
	Naziv        string `json:"naziv"`
	Adresa       string `json:"adresa,omitempty"`
	StructureID  string `json:"structure_id,omitempty"`  // objekt iz registra (crpna stanica)
	ContractorID string `json:"contractor_id,omitempty"` // firma u čijem krugu stoji
	Centralno    bool   `json:"centralno"`               // središnje skladište sektora
	Aktivno      bool   `json:"aktivno"`
	Napomena     string `json:"napomena,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Izvedeno pri čitanju
	AreaName       string `json:"-"`
	StructureName  string `json:"-"`
	ContractorName string `json:"-"`
}

// PunNaziv je naziv skladišta s branjenim područjem, kako stoji u popisu
func (s Skladiste) PunNaziv() string {
	if s.AreaName == "" {
		return s.Naziv
	}
	return "BP " + itoa(s.AreaID) + " – " + s.AreaName + " · " + s.Naziv
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// Vrste prometa. Promet je uvijek redak s količinom sa znakom: u mjesto
// pozitivno, iz mjesta negativno. Stanje mjesta je zbroj njegovih redaka,
// pa se nikad ne pita je li netko zaboravio prepraviti broj.
const (
	PrometPocetno  = "POCETNO"  // stanje zatečeno pri uvođenju u program
	PrometPrimka   = "PRIMKA"   // nabavljeno, primljeno u skladište
	PrometIzdano   = "IZDANO"   // izdano iz skladišta na teren
	PrometPovrat   = "POVRAT"   // vraćeno s terena u skladište
	PrometUtrosak  = "UTROSAK"  // ugrađeno ili potrošeno na terenu
	PrometPrijenos = "PRIJENOS" // premješteno među skladištima
	PrometPunjenje = "PUNJENJE" // vreće napunjene: prazne u punjene
	PrometPopis    = "POPIS"    // usklađenje po godišnjem popisu
	PrometOtpis    = "OTPIS"    // rashodovano, uništeno
)

// VrstePrometa redom kojim se nude, s nazivom i time troši li se stanje
var VrstePrometa = []struct {
	ID, Naziv, Opis string
}{
	{PrometPrimka, "Primka", "nabavljeno ili primljeno u skladište"},
	{PrometIzdano, "Izdano na teren", "iz skladišta na dionicu, tijekom obrane"},
	{PrometPovrat, "Povrat s terena", "vraćeno u skladište, neutrošeno"},
	{PrometUtrosak, "Utrošak na terenu", "ugrađeno u nasip ili potrošeno"},
	{PrometPrijenos, "Prijenos", "premješteno u drugo skladište"},
	{PrometPunjenje, "Punjenje", "prazno postaje napunjeno, u skladištu ili na dionici (jumbo vreće i box barijere pune se na terenu); pražnjenje obrnuto"},
	{PrometOtpis, "Otpis", "rashodovano ili uništeno"},
	{PrometPopis, "Usklađenje po inventuri", "razlika utvrđena inventurom"},
	{PrometPocetno, "Početno stanje", "zatečeno pri uvođenju u program"},
}

// PrometNaziv je naziv vrste prometa za prikaz
func PrometNaziv(id string) string {
	for _, v := range VrstePrometa {
		if v.ID == id {
			return v.Naziv
		}
	}
	return id
}

// Promet je jedan redak knjige: koliko je čega ušlo ili izašlo s jednog
// mjesta. Mjesto je skladište ili teren — sredstvo izdano na dionicu nije
// nestalo, stoji na nasipu dok se ne ugradi ili vrati.
type Promet struct {
	ID    string    `json:"id"`
	Datum time.Time `json:"datum"`

	VrstaID  string  `json:"vrsta_id"`
	Oblik    string  `json:"oblik"`
	Kolicina float64 `json:"kolicina"` // + u mjesto, − iz mjesta

	Vrsta  string `json:"vrsta"`  // Promet*
	Sektor string `json:"sektor"` // sektor skladišta; teren nosi sektor skladišta koje je izdalo

	// Mjesto: točno jedno od dvoje. Skladište je mjesto s adresom, teren je
	// dionica na kojoj se sredstvo nalazi dok obrana traje.
	SkladisteID string `json:"skladiste_id,omitempty"`
	SectionCode string `json:"section_code,omitempty"`

	VezaID    string `json:"veza_id,omitempty"`    // redci jednog poteza: prijenos, punjenje, izdavanje
	JournalID string `json:"journal_id,omitempty"` // obrana uz koju promet stoji
	PopisID   string `json:"popis_id,omitempty"`   // godišnji popis koji je usklađenje napravio

	// Trag dokumenta: tko je naložio, tko je robu preuzeo ili dopremio i po
	// kojem papiru. Skladištar koji je upisao stoji u UserName — to su tri
	// različite osobe i kad se poslije pita tko je što uzeo, sve tri trebaju.
	Nalozio  string `json:"nalozio,omitempty"`  // tko je dao nalog
	Preuzeo  string `json:"preuzeo,omitempty"`  // tko je preuzeo (izdavanje) ili dopremio (primka)
	Dokument string `json:"dokument,omitempty"` // broj otpremnice, računa ili naloga

	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
	Napomena string `json:"napomena,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Izvedeno pri čitanju
	VrstaNaziv     string `json:"-"`
	Jedinica       string `json:"-"`
	SkladisteNaziv string `json:"-"`
}

// StranaOznaka je naziv polja „preuzeo“ za tu vrstu prometa: kod primke je
// to onaj tko je robu dopremio, kod izdavanja onaj tko ju je odnio
func StranaOznaka(vrsta string) string {
	switch vrsta {
	case PrometPrimka:
		return "Dopremio"
	case PrometIzdano, PrometPrijenos:
		return "Preuzeo"
	case PrometPovrat:
		return "Vratio"
	case PrometUtrosak:
		return "Ugradio"
	case PrometOtpis:
		return "Rashodovao"
	}
	return "Osoba"
}

// NaTerenu javlja stoji li redak na terenu, a ne u skladištu
func (p Promet) NaTerenu() bool { return p.SkladisteID == "" }

// MjestoNaziv je mjesto retka za prikaz
func (p Promet) MjestoNaziv() string {
	if p.SkladisteID != "" {
		if p.SkladisteNaziv != "" {
			return p.SkladisteNaziv
		}
		return "skladište"
	}
	if p.SectionCode != "" {
		return "teren · " + p.SectionCode
	}
	return "teren"
}

// Stanje je količina jedne vrste u jednom obliku na jednom mjestu
type Stanje struct {
	SkladisteID string  `json:"skladiste_id,omitempty"`
	SectionCode string  `json:"section_code,omitempty"`
	VrstaID     string  `json:"vrsta_id"`
	Oblik       string  `json:"oblik"`
	Kolicina    float64 `json:"kolicina"`
}

// StanjeVrste su količine jedne vrste po oblicima, sa zbrojem
type StanjeVrste struct {
	Vrsta      VrstaSredstva      `json:"vrsta"`
	PoOblicima map[string]float64 `json:"po_oblicima"`
	Ukupno     float64            `json:"ukupno"`
	Potrebno   float64            `json:"potrebno,omitempty"` // dodatne potrebe za nabavom, iz zadnjeg popisa
}

// Ima javlja vodi li se išta od te vrste
func (s StanjeVrste) Ima() bool { return s.Ukupno != 0 || s.Potrebno != 0 }

// Oblik vraća količinu u traženom obliku
func (s StanjeVrste) Oblik(o string) float64 { return s.PoOblicima[o] }

// Popis je godišnja inventura jednog skladišta: što je prebrojano na dan,
// koliko je knjiga pokazivala i koliko još treba nabaviti. Zaključenjem se
// razlika proknjiži kao promet, pa knjiga i police kažu isto.
type Popis struct {
	ID          string    `json:"id"`
	SkladisteID string    `json:"skladiste_id"`
	Sektor      string    `json:"sektor"`
	Dan         time.Time `json:"dan"` // stanje na dan, obično 31.12.
	Godina      int       `json:"godina"`

	Stavke []PopisnaStavka `json:"stavke"`

	IzradioID    string     `json:"izradio_id"`
	Izradio      string     `json:"izradio"`
	IzradenoAt   time.Time  `json:"izradeno_at"`
	ZakljucenoAt *time.Time `json:"zakljuceno_at,omitempty"`
	Napomena     string     `json:"napomena,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Izvedeno pri čitanju
	SkladisteNaziv string `json:"-"`
	AreaID         int    `json:"-"`
}

// PopisnaStavka je jedan redak popisa
type PopisnaStavka struct {
	VrstaID   string  `json:"vrsta_id"`
	Oblik     string  `json:"oblik"`
	Utvrdjeno float64 `json:"utvrdjeno"`          // prebrojano na polici
	Knjizno   float64 `json:"knjizno"`            // što je knjiga pokazivala pri zaključenju
	Potrebno  float64 `json:"potrebno,omitempty"` // dodatne potrebe za nabavom u godini
	Napomena  string  `json:"napomena,omitempty"`

	// Izvedeno pri čitanju
	VrstaNaziv string `json:"-"`
	Jedinica   string `json:"-"`
	Grupa      string `json:"-"`
}

// Razlika je koliko popis odstupa od knjige: pozitivno višak, negativno manjak
func (s PopisnaStavka) Razlika() float64 { return s.Utvrdjeno - s.Knjizno }

// Zakljucen javlja je li inventura zaključena i proknjižen
func (p Popis) Zakljucen() bool { return p.ZakljucenoAt != nil }

// DanKey je dan popisa u obliku 2006-01-02
func (p Popis) DanKey() string { return p.Dan.In(Zagreb).Format("2006-01-02") }

// Razlika je koliko redaka popisa odstupa od knjige
func (p Popis) Razlika() int {
	n := 0
	for _, s := range p.Stavke {
		if s.Razlika() != 0 {
			n++
		}
	}
	return n
}

// Prazan javlja da na popisu nema nijedne količine ni potrebe
func (p Popis) Prazan() bool {
	for _, s := range p.Stavke {
		if s.Utvrdjeno != 0 || s.Potrebno != 0 {
			return false
		}
	}
	return true
}

// StavkaZa vraća redak popisa za vrstu i oblik
func (p Popis) StavkaZa(vrstaID, oblik string) PopisnaStavka {
	for _, s := range p.Stavke {
		if s.VrstaID == vrstaID && s.Oblik == oblik {
			return s
		}
	}
	return PopisnaStavka{VrstaID: vrstaID, Oblik: oblik}
}

// SortirajVrste slaže vrste redom popisa: skupina, pa redni broj
func SortirajVrste(v []VrstaSredstva) {
	poredak := map[string]int{}
	for i, g := range GrupeSredstava {
		poredak[g.ID] = i
	}
	sort.SliceStable(v, func(a, b int) bool {
		ga, gb := poredak[v[a].Grupa], poredak[v[b].Grupa]
		if ga != gb {
			return ga < gb
		}
		if v[a].Redoslijed != v[b].Redoslijed {
			return v[a].Redoslijed < v[b].Redoslijed
		}
		return strings.ToLower(v[a].Naziv) < strings.ToLower(v[b].Naziv)
	})
}

// PostojiGrupa javlja je li skupina poznata
func PostojiGrupa(id string) bool {
	for _, g := range GrupeSredstava {
		if g.ID == id {
			return true
		}
	}
	return false
}

// OznakaSredstva slaže oznaku iz naziva: "Vreće 50x80 cm" → "vrece-50x80-cm"
func OznakaSredstva(naziv string) string {
	zamjene := strings.NewReplacer("č", "c", "ć", "c", "š", "s", "đ", "d", "ž", "z",
		"Č", "c", "Ć", "c", "Š", "s", "Đ", "d", "Ž", "z")
	var b strings.Builder
	crtica := false
	for _, r := range zamjene.Replace(strings.ToLower(strings.TrimSpace(naziv))) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			crtica = false
		default:
			if !crtica && b.Len() > 0 {
				b.WriteByte('-')
				crtica = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
