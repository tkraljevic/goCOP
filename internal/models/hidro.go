package models

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Hidrološka arhiva: povijesni nizovi, oblik korita i krivulje protoka.
//
// Arhiva stoji izvan operativne baze i ne sinkronizira se — povijesni niz se
// ne uređuje i uvijek se može ponovno napraviti iz datoteka. Program iz nje
// računa, ali u nju ne piše.

// HidroNiz je jedan niz u arhivi: jedna letva, jedan izvor, jedna veličina i
// jedna gustoća zapisa.
type HidroNiz struct {
	ID       int64
	Sliv     string
	Letva    string
	Izvor    string // his2000 | cop | letva-hv | letva-dhmz | vituki | preracun-*
	Velicina string // vodostaj | protok | temperatura | koncentracija | pronos
	Vrsta    string // satni | dvokratni | jutarnji | srednjak | dnevni
	PoDanu   bool   // izvor daje samo datum, bez sata
	Od, Do   string
	Zapisa   int
	Otisak   string
}

// Jedinica je mjerna jedinica veličine.
func (n HidroNiz) Jedinica() string { return JedinicaVelicine(n.Velicina) }

// JedinicaVelicine vraća jedinicu u kojoj se veličina mjeri.
func JedinicaVelicine(v string) string {
	switch v {
	case "vodostaj":
		return "cm"
	case "protok":
		return "m³/s"
	case "temperatura":
		return "°C"
	case "koncentracija":
		return "g/m³"
	case "pronos":
		return "t"
	}
	return ""
}

// NazivVelicine je veličina za ispis.
func NazivVelicine(v string) string {
	switch v {
	case "vodostaj":
		return "Vodostaj"
	case "protok":
		return "Protok"
	case "temperatura":
		return "Temperatura vode"
	case "koncentracija":
		return "Koncentracija nanosa"
	case "pronos":
		return "Pronos nanosa"
	}
	return v
}

// NazivIzvora objašnjava odakle niz dolazi i koliko mu se vjeruje.
func NazivIzvora(i string) string {
	switch {
	case i == "his2000":
		return "DHMZ, ovjereno"
	case i == "cop":
		return "COP Osijek, odabrana jutarnja vrijednost"
	case i == "letva-hv":
		return "telemetrija, Hrvatske vode"
	case i == "letva-dhmz":
		return "telemetrija, DHMZ"
	case i == "vituki":
		return "vizugy.hu, Mađarska"
	case len(i) > 9 && i[:9] == "preracun-":
		return "preračunato iz " + i[9:]
	}
	return i
}

// JeIzmjereno govori je li niz mjeren na toj letvi ili izveden računom.
func (n HidroNiz) JeIzmjereno() bool { return len(n.Izvor) < 9 || n.Izvor[:9] != "preracun-" }

// NazivVrste objašnjava koja je to vrijednost dana. Razlika jutarnjeg
// očitanja i dnevnog srednjaka nije sitnica: na naglom porastu razilaze se i
// po više od metra.
func NazivVrste(v string) string {
	switch v {
	case "satni":
		return "svaki sat"
	case "dvokratni":
		return "dva puta dnevno"
	case "jutarnji":
		return "jutarnje očitanje"
	case "srednjak":
		return "dnevni srednjak"
	case "dnevni":
		return "jednom dnevno"
	}
	return v
}

// SpojenaVrijednost je jedna vrijednost spojenog niza: broj, odakle je i
// koliko odstupa. Spojeni niz je jedan po letvi i veličini, satni i dnevni —
// da se brzi podatak može uzeti bez biranja izvora, a podrijetlo se ne izgubi.
type SpojenaVrijednost struct {
	Kad        time.Time
	Vrijednost float64
	Izvor      string
	Vrsta      string  // trenutna | srednjak | jutarnji
	Tocnost    float64 // ± u jedinici veličine, 68 % vrijednosti

	// Ispravak stavljen preko arhivske vrijednosti. Izvorna vrijednost ostaje
	// zapisana, jer se ispravak mora moći provjeriti i povući.
	Ispravljeno bool
	Izvorno     float64
	Razlog      string
}

// TocnostLabel je odstupanje za ispis; ovjereni izvor nema ±.
func (v SpojenaVrijednost) TocnostLabel() string {
	if v.Tocnost <= 0 {
		return "ovjereno"
	}
	return "±" + strconv.FormatFloat(v.Tocnost, 'f', 0, 64)
}

// SazetakVelicine je ono što se o jednoj veličini kaže u jednom retku:
// razdoblje, srednjak i krajnosti. Za dežurnog je to cijela priča; ostalo je
// razrada.
type SazetakVelicine struct {
	Velicina     string
	Od, Do       string
	Zapisa       int
	Srednjak     float64
	Min, Max     float64
	MinNa, MaxNa string
	ZbrojIma     bool
	Zbroj        float64
}

// Jedinica je mjerna jedinica veličine.
func (s SazetakVelicine) Jedinica() string { return JedinicaVelicine(s.Velicina) }

// Naziv je veličina za ispis.
func (s SazetakVelicine) Naziv() string { return NazivVelicine(s.Velicina) }

// Decimala govori s koliko se decimala veličina ispisuje.
func (s SazetakVelicine) Decimala() int {
	switch s.Velicina {
	case "temperatura", "koncentracija":
		return 1
	}
	return 0
}

// SpojDoseg je što spojeni niz pokriva i iz čega je sastavljen.
type SpojDoseg struct {
	Letva    string
	Velicina string
	Korak    string // satni | dnevni
	Od, Do   string
	Zapisa   int
	Dijelovi []SpojDio
}

// SpojDio je jedan izvor koji sudjeluje u spojenom nizu.
type SpojDio struct {
	Izvor   string
	Vrsta   string
	Zapisa  int
	Od, Do  string
	Tocnost float64
}

// Udio je koliki dio spojenog niza dolazi iz ovog izvora, u postocima.
func (d SpojDio) Udio(ukupno int) int {
	if ukupno <= 0 {
		return 0
	}
	return d.Zapisa * 100 / ukupno
}

// HidroTocka je jedna vrijednost niza u trenutku.
type HidroTocka struct {
	Kad        time.Time
	Vrijednost float64
}

// HidroGodina su karakteristične vrijednosti jedne godine, izračunate iz niza.
type HidroGodina struct {
	Godina       int
	Zapisa       int
	Min, Max     float64
	MinNa, MaxNa string
	Srednjak     float64
	Zbroj        float64 // za veličine koje se gomilaju, npr. pronos nanosa
	Nepotpuna    bool    // godina nije pokrivena cijela
}

// HidroMjesec su vrijednosti jednog mjeseca kroz sve godine niza. Otud se
// vidi godišnji hod: kad voda redovito raste, a kad presuši.
type HidroMjesec struct {
	Mjesec   int
	Zapisa   int
	Min, Max float64
	Srednjak float64
}

// Naziv je mjesec ispisan.
func (m HidroMjesec) Naziv() string {
	mj := []string{"siječanj", "veljača", "ožujak", "travanj", "svibanj", "lipanj",
		"srpanj", "kolovoz", "rujan", "listopad", "studeni", "prosinac"}
	if m.Mjesec >= 1 && m.Mjesec <= 12 {
		return mj[m.Mjesec-1]
	}
	return ""
}

// TrajanjeTocka je vrijednost koja je dosegnuta ili premašena zadani postotak
// vremena. Krivulja trajanja govori ono što ekstremi ne mogu: koliko je često
// voda bila visoka, a ne samo koliko je najviše bila.
type TrajanjeTocka struct {
	Postotak   int
	Vrijednost float64
}

// HidroPregled je ono što se o nizu može reći bez ijedne odluke: raspon,
// ekstremi, godišnji hod i trajanje.
type HidroPregled struct {
	Niz          HidroNiz
	Godine       []HidroGodina
	Mjeseci      []HidroMjesec
	Trajanje     []TrajanjeTocka
	Min, Max     float64
	MinNa, MaxNa string
	Srednjak     float64

	// Zbroj ima smisla samo za veličine koje se gomilaju — pronos nanosa se
	// zbraja, vodostaj se ne. Zato stoji odvojeno od srednjaka.
	ZbrojIma bool
	Zbroj    float64
}

// SeZbraja govori gomila li se veličina kroz vrijeme.
func SeZbraja(velicina string) bool { return velicina == "pronos" }

// ProfilKorita je snimak poprečnog profila u jednom danu.
type ProfilKorita struct {
	ID       int64
	Datum    string
	Vodostaj int     // vodostaj pri snimanju, cm
	KotaNule float64 // apsolutna kota nule letve, m
	Tocke    []TockaProfila
}

// TockaProfila je jedna izmjerena točka. Stacionaža se mjeri od lijeve
// obale: tako su krajevi označeni na izvornim listovima HIS-2000, okomitim
// natpisima „Lijeva obala“ i „Desna obala“.
//
// Snimak ne seže uvijek do vrha obale — Batinin iz 2015. počinje tek na 110.
// metru, a iz 2020. na koti +166 cm — pa se iz njega ne smije čitati koliko
// korita ima iznad te razine.
type TockaProfila struct {
	Stacionaza float64 // m od lijeve obale
	Visina     float64 // apsolutna kota, m
}

// Sirina je razmak između prve i zadnje izmjerene točke.
func (p ProfilKorita) Sirina() float64 {
	if len(p.Tocke) < 2 {
		return 0
	}
	return p.Tocke[len(p.Tocke)-1].Stacionaza - p.Tocke[0].Stacionaza
}

// Dno je najniža izmjerena kota korita.
func (p ProfilKorita) Dno() float64 {
	if len(p.Tocke) == 0 {
		return 0
	}
	n := p.Tocke[0].Visina
	for _, t := range p.Tocke {
		if t.Visina < n {
			n = t.Visina
		}
	}
	return n
}

// DubinaPri je dubina nad najnižom točkom korita pri zadanom vodostaju.
func (p ProfilKorita) DubinaPri(vodostajCm int) float64 {
	return p.KotaNule + float64(vodostajCm)/100 - p.Dno()
}

// HQKrivulja pretvara vodostaj u protok. Sastoji se od odsječaka: svaki
// vrijedi u svom rasponu vodostaja, jer odnos visine i protoka nije isti dok
// voda teče glavnim koritom i kad se razlije u širi profil.
//
// Oblik je onaj kojim ga DHMZ i objavljuje — kvadratni polinom po vodostaju u
// metrima. Isti zapis nosi i potenciju, za nizove koje smo sami preračunali
// ondje gdje službene krivulje nema.
//
// Krivulja vrijedi za razdoblje: DHMZ je postavlja iznova, najčešće svake
// godine, jer se korito mijenja.
type HQKrivulja struct {
	ID        int64
	Letva     string
	VrijediOd string
	VrijediDo string // prazno = do sljedeće izmjere
	Izvor     string // tko ju je postavio
	Napomena  string
	Odsjecci  []HQOdsjecak
}

// Oblici odsječka.
const (
	OblikPolinom   = "polinom"   // Q = p1·H² + p2·H + p3, H u metrima na letvi
	OblikPotencija = "potencija" // Q = p1·(H + p3)^p2, H u metrima na letvi
)

// HQOdsjecak je jedan dio krivulje, s rasponom vodostaja u kojem vrijedi.
// Izvan raspona se ne računa ništa: DHMZ ga objavljuje s razlogom, a protok
// izvan njega bio bi produljenje krivulje ondje gdje je nitko nije mjerio.
type HQOdsjecak struct {
	OdCm, DoCm int
	Oblik      string
	P1, P2, P3 float64
}

// Protok računa protok iz odsječka.
func (o HQOdsjecak) Protok(vodostajCm int) (float64, bool) {
	h := float64(vodostajCm) / 100
	if o.Oblik == OblikPotencija {
		if h+o.P3 <= 0 {
			return 0, false
		}
		return o.P1 * math.Pow(h+o.P3, o.P2), true
	}
	return o.P1*h*h + o.P2*h + o.P3, true
}

// Zapis je odsječak ispisan onako kako se i citira, s decimalnim zarezom.
func (o HQOdsjecak) Zapis() string {
	if o.Oblik == OblikPotencija {
		return "Q = " + zarezHR(o.P1, 4) + " · (H + " + zarezHR(o.P3, 2) + ")^" + zarezHR(o.P2, 6)
	}
	znak := func(v float64) string {
		if v < 0 {
			return " − " + zarezHR(-v, 4) + "·"
		}
		return " + " + zarezHR(v, 4) + "·"
	}
	return "Q = " + zarezHR(o.P1, 4) + "·H²" + znak(o.P2) + "H" +
		strings.TrimSuffix(znak(o.P3), "·")
}

// Raspon je raspon vodostaja u kojem odsječak vrijedi, ispisan.
func (o HQOdsjecak) Raspon() string {
	return strconv.Itoa(o.OdCm) + " do " + strconv.Itoa(o.DoCm) + " cm"
}

// zarezHR ispisuje broj s decimalnim zarezom, bez suvišnih nula.
func zarezHR(f float64, najvise int) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if p := strings.IndexByte(s, '.'); p >= 0 && len(s)-p-1 > najvise {
		s = strconv.FormatFloat(f, 'f', najvise, 64)
	}
	return strings.Replace(s, ".", ",", 1)
}

// Protok pretvara vodostaj u protok odsječkom koji za njega vrijedi. Vraća
// false kad vodostaj izlazi iz raspona krivulje — tada se ne pogađa.
func (k HQKrivulja) Protok(vodostajCm int) (float64, bool) {
	for _, o := range k.Odsjecci {
		if vodostajCm >= o.OdCm && vodostajCm <= o.DoCm {
			return o.Protok(vodostajCm)
		}
	}
	return 0, false
}

// Raspon je najniži i najviši vodostaj koji krivulja pokriva.
func (k HQKrivulja) Raspon() (int, int, bool) {
	if len(k.Odsjecci) == 0 {
		return 0, 0, false
	}
	najn, najv := k.Odsjecci[0].OdCm, k.Odsjecci[0].DoCm
	for _, o := range k.Odsjecci {
		if o.OdCm < najn {
			najn = o.OdCm
		}
		if o.DoCm > najv {
			najv = o.DoCm
		}
	}
	return najn, najv, true
}

// NajveciSkok je najveća razlika dvaju susjednih odsječaka na njihovoj
// granici, u postotku. DHMZ-ove krivulje ondje imaju dvije desetinke;
// veći skok znači da se protok na jednom centimetru mijenja skokovito.
func (k HQKrivulja) NajveciSkok() float64 {
	var naj float64
	for i := 1; i < len(k.Odsjecci); i++ {
		g := k.Odsjecci[i].OdCm
		a, ok1 := k.Odsjecci[i-1].Protok(g)
		b, ok2 := k.Odsjecci[i].Protok(g)
		if !ok1 || !ok2 || a == 0 {
			continue
		}
		if s := math.Abs(b/a-1) * 100; s > naj {
			naj = s
		}
	}
	return naj
}

// Zapis je cijela krivulja u jednom retku, za mjesta gdje nema prostora za
// tablicu odsječaka.
func (k HQKrivulja) Zapis() string {
	var d []string
	for _, o := range k.Odsjecci {
		d = append(d, o.Zapis()+" ("+o.Raspon()+")")
	}
	return strings.Join(d, "; ")
}
