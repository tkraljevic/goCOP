package arhiva

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"gocop/internal/trajno"
)

// ZadrziIzdanja je koliko izdanja svake letve ostaje u mapi paketa i u
// spremištu sadržaja. Kućni server kišomjere izdaje nekoliko puta na dan, pa
// se bez brisanja godišnje skupilo oko 30.000 paketa. Dva su dovoljna: čvor
// kojem još nije stiglo zadnje kazalo traži prethodno izdanje, a starija ne
// traži nitko.
const ZadrziIzdanja = 2

// imePaketa je ime paketa letve u mapi paketa: letva_vN.cop
var imePaketa = regexp.MustCompile(`^(.+)_v([0-9]+)\.cop$`)

// paketUMapi je jedna datoteka paketa letve
type paketUMapi struct {
	izdanje int
	ime     string
}

// PospremiMapu briše iz mape paketa starija izdanja letava iz kataloga, tako
// da svakoj ostane ZadrziIzdanja najnovijih. Dira samo datoteke letve koju
// katalog vodi; druge .cop datoteke u mapi (npr. paketi očitanja) i izdanje
// novije od kataloga ostaju. Koliko je obrisano i što nije, javlja u zapisi.
func PospremiMapu(uMapu string, k Katalog, zapisi io.Writer) (int, error) {
	if zapisi == nil {
		zapisi = io.Discard
	}
	ulazi, err := os.ReadDir(uMapu)
	obrisano := 0
	if err == nil {
		obrisano, err = obrisiStarija(uMapu, paketiLetvi(ulazi, k))
	}
	if obrisano > 0 {
		trajno.SpustiMapu(uMapu)
		_, _ = fmt.Fprintf(zapisi, "obrisano starih paketa: %d (ostaju zadnja %d izdanja svake letve)\n", obrisano, ZadrziIzdanja) // dnevnik izdavanja, ne odlučuje ništa
	}
	if err != nil {
		_, _ = fmt.Fprintf(zapisi, "stari paketi nisu svi obrisani: %v\n", err) // dnevnik izdavanja, ne odlučuje ništa
	}
	return obrisano, err
}

// paketiLetvi razvrstava pakete u mapi po letvama iz kataloga, bez izdanja
// novijih od onog u katalogu
func paketiLetvi(ulazi []fs.DirEntry, k Katalog) map[string][]paketUMapi {
	poLetvi := k.PoLetvi()
	po := map[string][]paketUMapi{}
	for _, e := range ulazi {
		m := imePaketa.FindStringSubmatch(e.Name())
		if e.IsDir() || m == nil {
			continue
		}
		p, ima := poLetvi[m[1]]
		n, err := strconv.Atoi(m[2])
		if ima && err == nil && n <= p.Izdanje {
			po[m[1]] = append(po[m[1]], paketUMapi{izdanje: n, ime: e.Name()})
		}
	}
	return po
}

// obrisiStarija briše sve osim ZadrziIzdanja najnovijih paketa svake letve.
// Izdanje iz kataloga je među njima najveće, pa uvijek ostaje.
func obrisiStarija(uMapu string, po map[string][]paketUMapi) (int, error) {
	obrisano := 0
	var greske error
	for _, paketi := range po {
		sort.Slice(paketi, func(a, b int) bool { return paketi[a].izdanje > paketi[b].izdanje })
		for _, p := range paketi[min(ZadrziIzdanja, len(paketi)):] {
			err := os.Remove(filepath.Join(uMapu, p.ime))
			if err == nil {
				obrisano++
			} else if !errors.Is(err, fs.ErrNotExist) {
				greske = errors.Join(greske, err)
			}
		}
	}
	return obrisano, greske
}
