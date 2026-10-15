package arhiva

import (
	"fmt"
	"os"

	"gocop/internal/config"
)

// Relativna putanja stabla ili mape paketa računa se od mape iz koje je
// goCOP pokrenut. Kad se ta mapa promijeni (laptop je 12. 10. 2026. prešao
// pod Postavu, koja čvor pokreće iz mape baze), ista postavka pokazuje na
// mjesto gdje stabla nema. Upis bi ondje tiho započeo novo stablo, a
// izdavanje novu mapu paketa od prvog izdanja, dok pravo stablo i pravi
// katalog stoje u mapi iznad.
//
// Zato se ne stvara samo relativna mapa koje nema kad mapa istog imena stoji
// iznad radne mape (config.Mapa.Drugdje). Svaka druga nastaje kao i dosad:
// novi čvor na Linuxu ili pod Postavom stablo i pakete počinje u mapi baze, a
// apsolutna putanja je izričita.

// NemaMape je odbijen upis ili izdavanje u relativnu mapu koje nema, a mapa
// istog imena stoji iznad radne mape. Uputa je ista kao na stranici Unos u
// arhivu i u dnevniku pri pokretanju (config.Mapa.Uputa): s datotekom
// postavki i zastavicom koje je čvor zapamtio pri pokretanju.
type NemaMape struct {
	config.Mapa
}

func (e *NemaMape) Error() string {
	ishod, sto := "U stablo arhive nije ništa zapisano", "stabla arhive"
	if e.Kljuc == "pakete" {
		ishod, sto = "Paketi nisu izdani", "mape paketa"
	}
	return fmt.Sprintf("%s: %s nema u %s. %s", ishod, sto, e.Puna, e.Uputa())
}

// provjeriMapu odbija relativnu mapu koja je vjerojatno drugdje; svaka druga
// prolazi, kao i prije.
func provjeriMapu(mapa, kljuc string) error {
	if m := config.ProvjeriMapu(kljuc, mapa); m.Drugdje() {
		return &NemaMape{Mapa: m}
	}
	return nil
}

// ProvjeriKoren: stablo mora biti zadano, a relativno koje je vjerojatno
// drugdje ne počinje se iznova. Zovu ga i uvozi koji u stablo pišu sami
// (HIS-2000, godišnjaci RHMZ-a), prije nego što išta zapišu.
func ProvjeriKoren(koren string) error {
	if koren == "" {
		return fmt.Errorf("nije zadano gdje stablo s podacima stoji")
	}
	return provjeriMapu(koren, "podaci")
}

// pripremiMapuIzdanja odbija mapu paketa koja je vjerojatno drugdje, i za
// probno izdavanje: pregled s praznim katalogom pokazao bi „novo · v1” za
// letve koje čvor odavno izdaje. Pravo izdavanje mapu stvara, kao i prije.
func pripremiMapuIzdanja(mapa string, probno bool) error {
	if err := provjeriMapu(mapa, "pakete"); err != nil || probno {
		return err
	}
	return os.MkdirAll(mapa, 0o755)
}
