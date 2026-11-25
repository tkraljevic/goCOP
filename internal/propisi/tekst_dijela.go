package propisi

import (
	"strings"

	"gocop/internal/models"
)

// TekstDijela spljošti dio propisa u običan tekst za kazalo pretrage:
// naslov, stavke, točke svih razina, rečenicu iza nabrajanja i ćelije
// tablice, svaki komad u svom retku. Oznake („(1)”, „a)”) ne ulaze, jer se
// po njima ne traži.
func TekstDijela(d models.DioPropisa) string {
	var komadi []string
	dodaj := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			komadi = append(komadi, s)
		}
	}
	dodaj(d.Naslov)
	for _, s := range d.Stavci {
		dodaj(s.Tekst)
		dodajTocke(s.Tocke, dodaj)
		dodaj(s.Nastavak)
		if s.Tablica != nil {
			for _, redak := range s.Tablica.Retci {
				for _, c := range redak {
					dodaj(c.Tekst)
				}
			}
		}
	}
	return strings.Join(komadi, "\n")
}

func dodajTocke(tocke []models.TockaPropisa, dodaj func(string)) {
	for _, t := range tocke {
		dodaj(t.Tekst)
		dodajTocke(t.Tocke, dodaj)
	}
}
