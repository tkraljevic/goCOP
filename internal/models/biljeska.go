package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Bilješka uz arhivsku vrijednost.
//
// Ispravak kaže da je vrijednost pogrešna i mijenja ju. Bilješka ne mijenja
// ništa — govori što ta vrijednost jest. „Očitan maksimum" uz 772 cm u 11:11
// nije ispravak nego ono najvrednije što o toj vrijednosti postoji: čovjek je
// stajao pred letvom i vidio vrh.
//
// Stoji odvojeno od arhive, kao i ispravak: arhiva je obnovljiva iz datoteka i
// mora ostati onakva kakvu su je izvori dali. Bilješka je naša, ide kroz knjigu
// verzija i preživljava ulaganje očitanja u arhivu — inače bi se pri
// pospremanju baze izgubilo upravo ono zbog čega se očitanje i pamti.
type ArhivaBiljeska struct {
	ID       uuid.UUID `json:"id"`
	Letva    string    `json:"letva"`
	Velicina string    `json:"velicina"`
	Korak    string    `json:"korak"` // satni | dnevni
	Vrijeme  time.Time `json:"vrijeme"`
	// Vrsta je tvrdnja s posljedicom — vrh, dno, granica, procjena. Prazno
	// znači obična napomena. Prije se to pogađalo iz teksta; pogađanje sada
	// služi samo da obrazac predloži vrstu, a odlučuje čovjek.
	Vrsta string `json:"vrsta,omitempty"`
	Tekst string `json:"tekst"`
	// Tko je bilješku ostavio: promatrač s terena, ili korisnik koji je upisao.
	Tko       string    `json:"tko,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Vrste bilježaka. Zatvoren popis, i to namjerno: ovo nisu oznake nego
// tvrdnje s posljedicom na brojke, pa svaka mora imati određeno značenje.
// Sve ostalo što čovjek ima reći ide u slobodan tekst.
const (
	BiljeskaVrh        = "VRH"        // ovo je bila kulminacija
	BiljeskaDno        = "DNO"        // ovo je bio najniži vodostaj
	BiljeskaIznad      = "IZNAD"      // voda preko letve — vrijednost je donja granica
	BiljeskaIspod      = "ISPOD"      // letva na suhom — vrijednost je gornja granica
	BiljeskaProcjena   = "PROCJENA"   // nije očitano nego procijenjeno
	BiljeskaNepouzdano = "NEPOUZDANO" // zaleđeno, val, zatrpano, oštećeno
	BiljeskaDogadaj    = "DOGADAJ"    // proboj, izljev, zatvorena ustava
)

// VrsteBiljeske su ponuđene vrste s objašnjenjem, po redu kojim se nude.
var VrsteBiljeske = []struct{ Vrsta, Naziv, Opis string }{
	{"", "obična napomena", "samo tekst, bez posljedice na brojke"},
	{BiljeskaVrh, "očitan maksimum", "ulazi u zabilježene ekstreme; jače od nagađanja iz dojava"},
	{BiljeskaDno, "očitan minimum", "ulazi u zabilježene ekstreme"},
	{BiljeskaIznad, "voda preko letve", "vrijednost je donja granica, ne mjerenje"},
	{BiljeskaIspod, "letva na suhom", "vrijednost je gornja granica"},
	{BiljeskaProcjena, "procjena, nije očitano", "vrijednost je čovjekova procjena"},
	{BiljeskaNepouzdano, "nepouzdano očitanje", "ne odlučuje o ekstremu ni o fazi obrane"},
	{BiljeskaDogadaj, "događaj", "kontekst; ne dira brojku"},
}

// NazivVrsteBiljeske je vrsta ispisana za čovjeka.
func NazivVrsteBiljeske(v string) string {
	for _, x := range VrsteBiljeske {
		if x.Vrsta == v {
			return x.Naziv
		}
	}
	return v
}

// JeVrsta javlja je li vrijednost iz zatvorenog popisa.
func JeVrstaBiljeske(v string) bool {
	for _, x := range VrsteBiljeske {
		if x.Vrsta == v {
			return true
		}
	}
	return false
}

// JeVrh i JeDno javljaju tvrdi li bilješka da je to bila krajnost. Takva
// tvrdnja vrijedi više od svakog nagađanja iz razilaženja dojava: program iz
// njihova neslaganja tek pokušava zaključiti ono što je ovdje netko vidio.
func (b ArhivaBiljeska) JeVrh() bool { return b.Vrsta == BiljeskaVrh }
func (b ArhivaBiljeska) JeDno() bool { return b.Vrsta == BiljeskaDno }

// JeKrajnost javlja tvrdi li bilješka bilo koju krajnost.
func (b ArhivaBiljeska) JeKrajnost() bool { return b.JeVrh() || b.JeDno() }

// Pouzdana javlja smije li vrijednost odlučivati o ekstremu i o fazi obrane.
// Procjena, granica i nepouzdano očitanje ne smiju — svako iz svog razloga.
func (b ArhivaBiljeska) Pouzdana() bool {
	switch b.Vrsta {
	case BiljeskaIznad, BiljeskaIspod, BiljeskaProcjena, BiljeskaNepouzdano:
		return false
	}
	return true
}

// oznakeVrha i oznakeDna služe SAMO da se pri upisu predloži vrsta. Odluka je
// čovjekova; pogađanje iz teksta ne smije odlučivati o brojkama, jer "nije bio
// maksimum" sadrži istu riječ kao i "očitan maksimum".
var (
	oznakeVrha = []string{"maksimum", "maximum", "kulminacij", "vrh vala", "vrhunac", "najviš"}
	oznakeDna  = []string{"minimum", "najniž", "dno vala", "mala voda"}
)

// PredloziVrstu pogađa vrstu iz teksta, za predodabir u obrascu.
func PredloziVrstu(tekst string) string {
	t := strings.ToLower(tekst)
	for _, o := range oznakeDna {
		if strings.Contains(t, o) {
			return BiljeskaDno
		}
	}
	for _, o := range oznakeVrha {
		if strings.Contains(t, o) {
			return BiljeskaVrh
		}
	}
	return ""
}
