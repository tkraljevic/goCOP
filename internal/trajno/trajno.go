// Paket trajno piše datoteke tako da zapisano preživi pad sustava ili
// nestanak struje. Datoteka se spusti na disk prije nego što dobije pravo
// ime, a mapa nakon toga. Bez prvoga nakon nestanka struje pod pravim imenom
// može ostati prazna ili napola zapisana datoteka, a bez drugoga stara
// datoteka ili nijedna, iako je program javio da je zapisao.
package trajno

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// SpustiDatoteku spušta otvorenu datoteku na disk. Zove se prije Close i
// prije preimenovanja.
func SpustiDatoteku(f *os.File) error {
	if err := f.Sync(); err != nil {
		return err
	}
	javi(f.Name())
	return nil
}

// SpustiIZatvori spušta datoteku na disk i zatvara je; vraća prvu grešku
func SpustiIZatvori(f *os.File) error {
	err := SpustiDatoteku(f)
	if errZ := f.Close(); err == nil {
		err = errZ
	}
	return err
}

// Preimenuj preimenuje datoteku i spušta mapu odredišta na disk
func Preimenuj(iz, u string) error {
	if err := os.Rename(iz, u); err != nil {
		return err
	}
	SpustiMapu(filepath.Dir(u))
	return nil
}

// SpustiMapu spušta mapu na disk, da novo ime datoteke u njoj (ili
// brisanje) preživi nestanak struje. Windows mapu ne da spustiti, a
// datoteka je tada već na disku, pa se greška ne javlja.
func SpustiMapu(mapa string) {
	if d, err := os.Open(mapa); err == nil {
		_ = d.Sync()  // Windows mapu ne da spustiti, a datoteka je već na disku
		_ = d.Close() // mapa je otvorena samo za čitanje
	}
	javi(mapa)
}

// Zapisi piše sadržaj u privremenu datoteku u istoj mapi, spušta je na disk,
// preimenuje je preko odredišta i spušta mapu. Prekid usred pisanja ne
// ostavlja pola datoteke pod pravim imenom.
func Zapisi(put string, sadrzaj []byte, dopustenja os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(put), "."+filepath.Base(put)+"-*")
	if err != nil {
		return err
	}
	privremena := f.Name()
	_, err = f.Write(sadrzaj)
	if errZ := SpustiIZatvori(f); err == nil {
		err = errZ
	}
	if err == nil {
		err = os.Chmod(privremena, dopustenja)
	}
	if err == nil {
		err = Preimenuj(privremena, put)
	}
	if err != nil {
		_ = os.Remove(privremena) // nakon greške; privremene možda ni nema
	}
	return err
}

// Popis bilježi datoteke i mape spuštene na disk dok je praćenje uključeno.
// Služi testovima drugih paketa, da provjere da njihov upis ide ovuda.
type Popis struct {
	mu     sync.Mutex
	putevi []string
}

// Ima javlja je li put spušten na disk
func (p *Popis) Ima(put string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Contains(p.putevi, put)
}

// ImaUMapi javlja je li spuštena barem jedna datoteka iz mape
func (p *Popis) ImaUMapi(mapa string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.ContainsFunc(p.putevi, func(put string) bool { return filepath.Dir(put) == mapa })
}

var pracenje struct {
	sync.Mutex
	popisi []*Popis
}

// Prati uključuje praćenje; prestani ga isključuje
func Prati() (p *Popis, prestani func()) {
	p = &Popis{}
	pracenje.Lock()
	pracenje.popisi = append(pracenje.popisi, p)
	pracenje.Unlock()
	return p, func() {
		pracenje.Lock()
		defer pracenje.Unlock()
		pracenje.popisi = slices.DeleteFunc(pracenje.popisi, func(q *Popis) bool { return q == p })
	}
}

func javi(put string) {
	pracenje.Lock()
	defer pracenje.Unlock()
	for _, p := range pracenje.popisi {
		p.mu.Lock()
		p.putevi = append(p.putevi, put)
		p.mu.Unlock()
	}
}
