package propisi

import "strings"

// ProcitajTekst čita zalijepljen tekst akta ili datoteku .txt: skida BOM,
// prijelome CRLF i CR pretvara u „\n”, a svaki neprazan redak postaje
// odlomak bez klase. Vrstu odlomaka bez klase prepoznaje Podijeli.
func ProcitajTekst(tekst string) *Procitano {
	tekst = strings.TrimPrefix(tekst, "\ufeff")
	tekst = strings.ReplaceAll(tekst, "\r\n", "\n")
	tekst = strings.ReplaceAll(tekst, "\r", "\n")
	p := &Procitano{}
	for _, r := range strings.Split(tekst, "\n") {
		if t := ocisti(r); t != "" {
			p.Odlomci = append(p.Odlomci, Odlomak{Tekst: t})
		}
	}
	return p
}
