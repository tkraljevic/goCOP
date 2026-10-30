package web

import (
	"archive/zip"
	"strings"

	"gocop/internal/raspakiraj"
)

// Granica raspakiranja jedne .xlsx datoteke. Tablica ima desetak stavki, a
// čitaju se samo dvije: zajednička tablica nizova i prvi list, svaka do 64 MB.
const (
	najviseStavkiXLSX = 1000
	najviseStavkeXLSX = 64 << 20
)

var granicaXLSX = func() *raspakiraj.Granica {
	return raspakiraj.Nova(najviseStavkiXLSX, 2*najviseStavkeXLSX)
}

// stavkeXLSX bira stavke koje se čitaju: zajedničku tablicu nizova i prvi
// list po nazivu (sheet1.xml prije sheet2.xml). Ostale se ne otvaraju.
func stavkeXLSX(stavke []*zip.File) (sst, list *zip.File) {
	for _, f := range stavke {
		switch {
		case f.Name == "xl/sharedStrings.xml":
			if sst == nil {
				sst = f
			}
		case strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml"):
			if list == nil || f.Name < list.Name {
				list = f
			}
		}
	}
	return sst, list
}

func citajIzZipa(f *zip.File, g *raspakiraj.Granica) ([]byte, error) {
	rc, err := g.Otvori(f)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return procitajDatoteku(rc, najviseStavkeXLSX)
}
