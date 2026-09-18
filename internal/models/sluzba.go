package models

import "time"

// Sluzba je kontakt hitne ili nadležne službe uz županiju, i po potrebi uz
// grad ili općinu: područni ured civilne zaštite, služba za prevenciju i
// pripravnost, županijski centar 112, policijska uprava i postaja, lučka
// kapetanija. Akt o obrani ih obavještava po županijama ugroženog područja
// svojih dionica, kako stoje na dosadašnjim aktima.
type Sluzba struct {
	ID             string    `json:"id"`
	CountyID       int       `json:"county_id"`
	MunicipalityID int       `json:"municipality_id,omitempty"` // 0 = cijela županija
	Vrsta          string    `json:"vrsta"`                     // Sluzba*
	Naziv          string    `json:"naziv"`
	Email          string    `json:"email,omitempty"`
	Phone          string    `json:"phone,omitempty"`
	Napomena       string    `json:"napomena,omitempty"`
	Redoslijed     int       `json:"redoslijed"`
	UpdatedAt      time.Time `json:"updated_at"`

	// Izvedeno pri čitanju
	MunicipalityName string `json:"-"`
}

// Vrste službi, redom kako stoje na aktu
const (
	SluzbaCivilnaZastita  = "CIVILNA_ZASTITA"
	SluzbaPrevencija      = "PREVENCIJA"
	SluzbaCentar112       = "CENTAR_112"
	SluzbaPolicija        = "POLICIJA"
	SluzbaPolicijskaPost  = "POLICIJSKA_POSTAJA"
	SluzbaLuckaKapetanija = "LUCKA_KAPETANIJA"
	SluzbaStozerCZ        = "STOZER_CZ"
	SluzbaVatrogasci      = "VATROGASCI"
	SluzbaCrveniKriz      = "CRVENI_KRIZ"
	SluzbaOstalo          = "OSTALO"
)

// VrsteSluzbi su vrste redom kojim ih obrazac nudi i akt ispisuje
var VrsteSluzbi = []string{SluzbaCivilnaZastita, SluzbaPrevencija, SluzbaCentar112, SluzbaPolicija, SluzbaPolicijskaPost, SluzbaLuckaKapetanija,
	SluzbaStozerCZ, SluzbaVatrogasci, SluzbaCrveniKriz, SluzbaOstalo}

// SluzbaLabel je naziv vrste za prikaz
func SluzbaLabel(v string) string {
	switch v {
	case SluzbaCivilnaZastita:
		return "Područni ured civilne zaštite"
	case SluzbaPrevencija:
		return "Služba za prevenciju i pripravnost"
	case SluzbaCentar112:
		return "Županijski centar 112"
	case SluzbaPolicija:
		return "Policijska uprava"
	case SluzbaPolicijskaPost:
		return "Policijska postaja"
	case SluzbaLuckaKapetanija:
		return "Lučka kapetanija"
	case SluzbaStozerCZ:
		return "Stožer civilne zaštite županije"
	case SluzbaVatrogasci:
		return "Vatrogasna zajednica"
	case SluzbaCrveniKriz:
		return "Crveni križ"
	case SluzbaOstalo:
		return "ostalo"
	}
	return v
}

// RedVrste je redni broj vrste, za poredak
func RedVrste(v string) int {
	for i, x := range VrsteSluzbi {
		if x == v {
			return i
		}
	}
	return len(VrsteSluzbi)
}

// PodCivilnomZastitom javlja stoji li vrsta na aktu kao podstavka
// područnog ureda civilne zaštite (prevencija, 112), kao na dosadašnjim aktima
func PodCivilnomZastitom(v string) bool {
	return v == SluzbaPrevencija || v == SluzbaCentar112
}

// NaAktu javlja ide li služba te vrste na akt o obrani tog stupnja, kako je
// na dosadašnjim aktima: civilna zaštita, 112, policija i lučke kapetanije
// uvijek; stožer civilne zaštite od izvanrednog stanja, kad jedinice
// samouprave aktiviraju stožere (Državni plan, XXV); vatrogasci i Crveni križ
// stoje u imeniku, a na akt ne idu
func NaAktu(vrsta string, stupanj DefensePhase) bool {
	switch vrsta {
	case SluzbaVatrogasci, SluzbaCrveniKriz:
		return false
	case SluzbaStozerCZ:
		return stupanj == PhaseState
	}
	return true
}

// JeVrstaSluzbe javlja je li vrsta s popisa
func JeVrstaSluzbe(v string) bool { return RedVrste(v) < len(VrsteSluzbi) }
