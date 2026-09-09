package web

import (
	"strings"

	"gocop/internal/docx"
	"gocop/internal/models"
)

// Memorandum centra obrane na izvješću. Ništa se ovdje ne upisuje u kod: sve
// što zaglavlje ispisuje program već vodi — naziv ustanove i znak u nazivlju
// organizacije, ustrojstvenu jedinicu i kontakte u zapisu sektora. Zato svaki
// centar iz istog programa dobiva svoj memorandum, a promjena adrese ili
// telefona ide kroz Postavke, ne kroz novu inačicu.
func zaglavljeIzvjesca(t models.OrgTerms, s *models.Sector) docx.Zaglavlje {
	z := docx.Zaglavlje{Ustanova: strings.ToUpper(strings.TrimSpace(t.OrgName))}
	if t.HasLogo() {
		if vrsta := docx.VrstaZnaka(t.LogoMime); vrsta != "" {
			z.Znak, z.ZnakVrsta = t.Logo, vrsta
		}
	}
	if s == nil {
		return z
	}
	if red := jedinicaURetke(t, *s); len(red) > 0 {
		z.Jedinica = append(z.Jedinica, red...)
	}
	if ime := imeCentra(t, *s); ime != "" {
		z.Jedinica = append(z.Jedinica, ime)
	}
	z.Adresa = strings.TrimSpace(s.Address)
	z.Kontakt = kontaktURetke(s.Phone, s.Email)
	return z
}

// jedinicaURetke rastavlja naziv ustrojstvene jedinice na dva retka, kako
// stoji na obrascu: vrsta jedinice pa ono za što je nadležna. Zapis je
// pokraćen ("VGO za Dunav i donju Dravu, Osijek"), pa se kratica zamjenjuje
// punim nazivom iz nazivlja, a sjedište izostavlja — ono je već u adresi.
func jedinicaURetke(t models.OrgTerms, s models.Sector) []string {
	naziv := strings.TrimSpace(s.VgoName)
	vrsta := strings.TrimSpace(t.SectorOffice)
	if naziv == "" {
		if vrsta == "" {
			return nil
		}
		return []string{strings.ToUpper(vrsta)}
	}
	ostatak := naziv
	if kratica := strings.TrimSpace(t.SectorOfficeShort); kratica != "" &&
		strings.HasPrefix(ostatak, kratica+" ") {
		ostatak = strings.TrimPrefix(ostatak, kratica+" ")
	} else if vrsta != "" && strings.HasPrefix(strings.ToLower(ostatak), strings.ToLower(vrsta)+" ") {
		ostatak = ostatak[len(vrsta)+1:]
	} else {
		// Naziv nije u očekivanom obliku; ispisuje se kakav jest, u jednom retku.
		return []string{strings.ToUpper(ostatak)}
	}
	if zarez := strings.LastIndex(ostatak, ","); zarez > 0 {
		ostatak = ostatak[:zarez]
	}
	retci := []string{strings.ToUpper(ostatak)}
	if vrsta != "" {
		retci = append([]string{strings.ToUpper(vrsta)}, retci...)
	}
	return retci
}

// imeCentra je redak "Centar obrane od poplava Sektora B".
func imeCentra(t models.OrgTerms, s models.Sector) string {
	centar := strings.TrimSpace(t.Center)
	if centar == "" || strings.TrimSpace(s.ID) == "" {
		return centar
	}
	if s.IsLevel1() {
		// Krovna jedinica nema oznaku sektora; njezin centar ima vlastito ime.
		if c := strings.TrimSpace(t.Level1Center); c != "" {
			return c
		}
		return centar
	}
	jedinica := strings.TrimSpace(t.Sector)
	if jedinica == "" {
		return centar + " " + s.ID
	}
	return centar + " " + genitivJedinice(jedinica) + " " + s.ID
}

// genitivJedinice: na memorandumu centar stoji u genitivu — "Sektora B", ne
// "Sektor B". Nazivlje čuva nominativ jer se tako pojam svugdje drugdje
// ispisuje, a hrvatsku sklonidbu za proizvoljan naziv nema smisla pogađati:
// preimenuje li ustanova taj pojam, redak ide nepromijenjen.
func genitivJedinice(naziv string) string {
	if naziv == "Sektor" {
		return "Sektora"
	}
	return naziv
}

// kontaktURetke slaže desni stupac memoranduma. Više brojeva odvojenih
// zarezom ide u zasebne retke, kao na obrascu.
func kontaktURetke(telefon, epošta string) []string {
	var retci []string
	var brojevi []string
	for _, dio := range strings.FieldsFunc(telefon, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	}) {
		if d := strings.TrimSpace(dio); d != "" {
			brojevi = append(brojevi, d)
		}
	}
	if len(brojevi) > 0 {
		retci = append(retci, "Telefon:")
		retci = append(retci, brojevi...)
	}
	if e := strings.TrimSpace(epošta); e != "" {
		retci = append(retci, e)
	}
	return retci
}
