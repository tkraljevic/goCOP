// Package raspakiraj čita stavke ZIP-a uz granicu koja vrijedi za cijeli
// uvoz: koliko stavki i koliko raspakiranih bajtova smiju imati svi ZIP-ovi
// i sve datoteke jednog uvoza zajedno. ZIP od nekoliko MB zna se raspakirati
// u mnogo puta više, pa se broje stvarno pročitani bajtovi; veličina iz
// zaglavlja stavke je samo brza provjera prije čitanja.
package raspakiraj

import (
	"archive/zip"
	"fmt"
	"io"
)

// Zadana granica jednog uvoza. Izvoz postaje iz HIS-2000 ima desetke
// datoteka, a raspakirano smije biti koliko i sve poslane datoteke zajedno.
const (
	ZadanoStavki  = 1000
	ZadanoBajtova = 256 << 20
)

// Granica je ostatak onoga što uvoz još smije raspakirati. Ista granica ide
// kroz sve ZIP-ove i datoteke istog uvoza; nije za istodobnu upotrebu.
type Granica struct {
	stavki, najviseStavki   int
	bajtova, najviseBajtova int64
}

// Nova je granica od najviše stavki i bajtova.
func Nova(stavki int, bajtova int64) *Granica {
	return &Granica{stavki: stavki, najviseStavki: stavki, bajtova: bajtova, najviseBajtova: bajtova}
}

// Zadana je granica jednog uvoza (ZadanoStavki, ZadanoBajtova).
func Zadana() *Granica { return Nova(ZadanoStavki, ZadanoBajtova) }

// Stavke uzima n stavki od granice, prije čitanja ijedne od njih.
func (g *Granica) Stavke(n int) error {
	if n > g.stavki {
		return fmt.Errorf("u arhivi je previše datoteka: jedan uvoz prima najviše %d", g.najviseStavki)
	}
	g.stavki -= n
	return nil
}

// Potrosi uzima n bajtova za datoteku koja je stigla nesažeta.
func (g *Granica) Potrosi(n int64) error {
	if n > g.bajtova {
		return g.preveliko()
	}
	g.bajtova -= n
	return nil
}

func (g *Granica) preveliko() error {
	return fmt.Errorf("datoteke uvoza zajedno su veće od %d MB kad se raspakiraju", g.najviseBajtova>>20)
}

// Otvori otvara stavku za čitanje. Čitač vrati grešku čim stavka prijeđe
// ostatak granice, pa se odrezana datoteka nikad ne čita kao cijela.
func (g *Granica) Otvori(f *zip.File) (io.ReadCloser, error) {
	if f.UncompressedSize64 > uint64(g.bajtova) {
		return nil, g.preveliko()
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	return &citac{rc: rc, g: g}, nil
}

// Procitaj čita cijelu stavku, ako stane u ostatak granice.
func (g *Granica) Procitaj(f *zip.File) ([]byte, error) {
	rc, err := g.Otvori(f)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

type citac struct {
	rc io.ReadCloser
	g  *Granica
}

// Read nikad ne traži više od jednog bajta preko ostatka: taj bajt kaže da
// je stavka prevelika.
func (c *citac) Read(p []byte) (int, error) {
	if int64(len(p)) > c.g.bajtova+1 {
		p = p[:c.g.bajtova+1]
	}
	n, err := c.rc.Read(p)
	if int64(n) > c.g.bajtova {
		c.g.bajtova = 0
		return 0, c.g.preveliko()
	}
	c.g.bajtova -= int64(n)
	return n, err
}

func (c *citac) Close() error { return c.rc.Close() }
