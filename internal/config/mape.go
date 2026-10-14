package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
)

// Mapa je stablo arhive (podaci) ili mapa paketa (pakete) onako kako je čvor
// vidi na disku.
//
// Relativna putanja računa se od mape iz koje je goCOP pokrenut. Ista
// postavka zato pokazuje drugamo kad se program pokreće ručno iz mape
// projekta, a drugamo pod Postavom ili kao usluga sustava, gdje je radna mapa
// mapa baze. Tako je 12. 10. 2026. laptop pod Postavom tražio stablo u
// data/vodostaji, a ono je stajalo u vodostaji uz mapu data.
//
// Istu provjeru i istu uputu (Uputa) imaju upis u stablo i izdavanje
// (internal/arhiva), stranica Unos u arhivu i dnevnik pri pokretanju, pa se
// ne mogu razići.
type Mapa struct {
	Kljuc   string // postavka: podaci (stablo arhive) ili pakete (mapa paketa)
	Zadano  string // kako piše u postavkama ili zastavici; prazno = isključeno
	Puna    string // puna putanja koju čvor koristi
	Postoji bool   // ondje stoji mapa
	// Prijedlog je mapa istog imena u mapi iznad radne, kad relativne mape
	// nema, a ondje je ima: ondje je relativna putanja pokazivala dok se
	// program pokretao iz mape projekta, a ne iz mape baze. Samo prijedlog:
	// čvor se ne prebacuje sam.
	Prijedlog string
	// Gdje se putanja mijenja, za uputu: puna putanja datoteke postavki koju
	// čvor čita (ili bi čitao) i zastavica pokretanja jača od nje ("-podaci"),
	// ako je zadana. Prazno kad se ne zna.
	Postavke, Zastavica string
}

// IzvorMapa kaže odakle čvor uzima putanje stabla i paketa: iz datoteke
// postavki ili iz zastavica pokretanja, koje su jače od nje.
type IzvorMapa struct {
	Postavke  string            // datoteka postavki
	Zastavice map[string]string // postavka → zastavica pokretanja ("podaci" → "-podaci")
}

// izvorMapa je izvor ovog čvora; prazan dok ga čvor ne zapamti pri
// pokretanju (testovi, alati), pa uputa kaže samo gocop.toml uz bazu.
var izvorMapa atomic.Pointer[IzvorMapa]

// ZapamtiIzvorMapa zove čvor pri pokretanju. Upis u stablo i izdavanje znaju
// samo putanju, a ovako i oni u poruci kažu gdje se ona mijenja.
func ZapamtiIzvorMapa(iz IzvorMapa) { izvorMapa.Store(&iz) }

// ProvjeriMapu gleda gdje je mapa iz postavke kljuc i postoji li, uz izvor
// koji je čvor zapamtio pri pokretanju.
func ProvjeriMapu(kljuc, zadano string) Mapa {
	var iz IzvorMapa
	if p := izvorMapa.Load(); p != nil {
		iz = *p
	}
	return iz.Provjeri(kljuc, zadano)
}

// Provjeri gleda gdje je mapa iz postavke kljuc i postoji li.
func (iz IzvorMapa) Provjeri(kljuc, zadano string) Mapa {
	m := Mapa{Kljuc: kljuc, Zadano: zadano, Zastavica: iz.Zastavice[kljuc]}
	if iz.Postavke != "" {
		m.Postavke = PunaPutanja(iz.Postavke)
	}
	if zadano == "" {
		return m
	}
	m.Puna = PunaPutanja(zadano)
	m.Postoji = jeMapa(m.Puna)
	if m.Nedostaje() {
		m.Prijedlog = prijedlogMape(zadano)
	}
	return m
}

// Izricita javlja pokazuje li putanja uvijek isto mjesto, bez obzira na mapu
// iz koje je goCOP pokrenut. To je apsolutna putanja, a na Windowsu i putanja
// od korijena diska bez slova (\goCOP\vodostaji ili /arhiva/vodostaji), koju
// filepath.IsAbs ne smatra apsolutnom.
func Izricita(put string) bool {
	return filepath.IsAbs(put) || (put != "" && os.IsPathSeparator(put[0]))
}

// odRadneMape je filepath.Abs: puna putanja od mape iz koje je goCOP
// pokrenut. Testovi ga zamijene da na svakom sustavu vide što biva kad sustav
// radnu mapu ne zna (obrisana je); Windows je ne da obrisati, a macOS je i
// dalje zna.
var odRadneMape = filepath.Abs

// PunaPutanja je putanja kako je sustav otvara; bez radne mape ostaje kako
// piše.
func PunaPutanja(put string) string {
	if pun, err := odRadneMape(put); err == nil {
		return pun
	}
	return put
}

// Relativna javlja računa li se putanja od radne mape.
func (m Mapa) Relativna() bool { return m.Zadano != "" && !Izricita(m.Zadano) }

// Nedostaje: relativne mape nema. Bez prijedloga mapa nastaje pri prvom upisu
// ili izdavanju, kao i dosad; tako počinje svaki novi čvor.
func (m Mapa) Nedostaje() bool { return m.Relativna() && !m.Postoji }

// Drugdje: relativne mape nema, a mapa istog imena stoji iznad radne mape.
// Stablo ili paketi su tada vjerojatno ondje, pa se u ovu mapu ne upisuje i
// ne izdaje: tiho bi nastalo novo, prazno stablo ili nova mapa paketa.
func (m Mapa) Drugdje() bool { return m.Nedostaje() && m.Prijedlog != "" }

// Pregledna: mapa je zadana i ne nedostaje, pa ima smisla gledati što je u
// njoj.
func (m Mapa) Pregledna() bool { return m.Zadano != "" && !m.Nedostaje() }

func jeMapa(put string) bool {
	st, err := os.Stat(put)
	return err == nil && st.IsDir()
}

// prijedlogMape traži mapu istog imena u mapi iznad radne. Pod Postavom i kao
// usluga sustava radna mapa je mapa baze, pa je to mapa iznad baze. U
// korijenu diska to je ista mapa koje nema, pa prijedloga nema.
func prijedlogMape(zadano string) string {
	radna, err := odRadneMape(".")
	if err != nil {
		return ""
	}
	if k := filepath.Join(filepath.Dir(radna), zadano); jeMapa(k) {
		return k
	}
	return ""
}

// rijeciMape su imena mape u uputi
var rijeciMape = map[string]struct{ ovo, novo string }{
	"podaci": {ovo: "stablo", novo: "novo stablo"},
	"pakete": {ovo: "mapa paketa", novo: "novu mapu paketa"},
}

// Uputa kaže zašto čvor relativnu mapu traži baš ondje i što učiniti: zadati
// punu putanju (onu iznad radne mape, ako je ima) ili napraviti mapu. Isti
// tekst ide u poruku odbijenog upisa ili izdavanja, u dnevnik pri pokretanju
// i na stranicu Unos u arhivu.
func (m Mapa) Uputa() string {
	r := rijeciMape[m.Kljuc]
	uvod := m.odakle() + " ne počinje od korijena diska, pa se računa od mape iz koje je goCOP pokrenut."
	if m.Prijedlog != "" {
		return fmt.Sprintf("%s Mapa %s postoji. Ako je to %s ovog čvora, %s. Ako ovdje treba početi %s, napravite mapu %s.",
			uvod, m.Prijedlog, r.ovo, m.upisi(m.Prijedlog), r.novo, m.Puna)
	}
	return fmt.Sprintf("%s Ako %s stoji drugdje, %s.", uvod, r.ovo, m.upisi("…"))
}

// odakle je putanja: iz zastavice pokretanja ili iz datoteke postavki
func (m Mapa) odakle() string {
	if m.Zastavica != "" {
		return fmt.Sprintf("Zastavica %s '%s'", m.Zastavica, m.Zadano)
	}
	return fmt.Sprintf("Postavka %s = '%s'", m.Kljuc, m.Zadano)
}

// upisi kaže gdje se putanja mijenja: u zastavici kad je zadana, jer je jača
// od datoteke postavki, inače u datoteci postavki
func (m Mapa) upisi(vrijednost string) string {
	datoteka := m.Postavke
	if datoteka == "" {
		datoteka = FileName + " uz bazu"
	}
	if m.Zastavica != "" {
		return fmt.Sprintf("zadajte punu putanju zastavicom %s '%s' pri pokretanju. Zastavica je jača od "+
			"datoteke %s, pa upis u datoteku ne pomaže", m.Zastavica, vrijednost, datoteka)
	}
	return fmt.Sprintf("upišite %s = '%s' u %s i ponovno pokrenite čvor", m.Kljuc, vrijednost, datoteka)
}
