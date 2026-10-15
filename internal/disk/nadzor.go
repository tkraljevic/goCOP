package disk

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"sync"
	"time"
)

// Nadzor povremeno mjeri diskove i pamti zadnje mjerenje za pločicu
// razmjene. U dnevnik piše samo kad mjesta na disku postane malo i kad ga
// opet bude dovoljno, ne pri svakoj provjeri.
type Nadzor struct {
	mjerac Mjerac
	mjesta []Mjesto
	javi   func(format string, args ...any) // dnevnik
	nakon  func([]Disk)                     // poslije svake provjere; nil = ništa

	krug   sync.Mutex        // jedna provjera u isto vrijeme; čuva malo i greske
	malo   map[string]bool   // disk → bilo je malo mjesta pri zadnjoj provjeri
	greske map[string]string // mapa → greška mjerenja koja je već u dnevniku

	mu     sync.Mutex
	zadnje []Disk // diskovi zadnje provjere
}

// NoviNadzor prati zadana mjesta; mjesta na istom disku daju jedno upozorenje
func NoviNadzor(m Mjerac, javi func(format string, args ...any), mjesta ...Mjesto) *Nadzor {
	return &Nadzor{mjerac: m, javi: javi, mjesta: mjesta, malo: map[string]bool{}, greske: map[string]string{}}
}

// NakonProvjere zadaje što se radi s diskovima poslije svake provjere, npr.
// e-pošta kad je mjesta malo. Zadaje se prije Vrti.
func (n *Nadzor) NakonProvjere(f func([]Disk)) { n.nakon = f }

// Vrti provjerava odmah i zatim svakih toliko, dok ctx ne završi
func (n *Nadzor) Vrti(ctx context.Context, svakih time.Duration) {
	t := time.NewTicker(svakih)
	defer t.Stop()
	for {
		n.Provjeri()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Provjeri izmjeri diskove, zapamti ih i javi u dnevnik disk na kojem je
// mjesta upravo postalo malo ili ga opet ima dovoljno
func (n *Nadzor) Provjeri() {
	n.krug.Lock()
	defer n.krug.Unlock()
	diskovi := n.diskovi()
	for _, d := range diskovi {
		if malo := d.Malo(); malo != n.malo[d.Disk] {
			n.malo[d.Disk] = malo
			n.javiPrijelaz(d, malo)
		}
	}
	n.mu.Lock()
	n.zadnje = diskovi
	n.mu.Unlock()
	if n.nakon != nil {
		n.nakon(slices.Clone(diskovi))
	}
}

// Stanje su diskovi zadnje provjere, i oni na kojima mjesta ima dovoljno,
// redom kojim su mjesta zadana; nil bez nadzora. Je li na disku mjesta malo,
// kaže Disk.Malo.
func (n *Nadzor) Stanje() []Disk {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	out := slices.Clone(n.zadnje)
	for i := range out {
		out[i].Nazivi = slices.Clone(out[i].Nazivi)
	}
	return out
}

func (n *Nadzor) javiPrijelaz(d Disk, malo bool) {
	if malo {
		n.javi("disk: %s", d.Upozorenje())
		return
	}
	n.javi("disk: %s", d.dovoljno())
}

// diskovi mjeri svako mjesto i spaja mjesta na istom disku, redom kojim su
// zadana
func (n *Nadzor) diskovi() []Disk {
	var out []Disk
	gdje := map[string]int{}
	for _, m := range n.mjesta {
		mj, ok := n.izmjeri(m.Put)
		if !ok {
			continue
		}
		i, ima := gdje[mj.Disk]
		if !ima {
			i = len(out)
			gdje[mj.Disk] = i
			out = append(out, Disk{Mjera: mj})
		}
		out[i].dodaj(m)
	}
	return out
}

// izmjeri mjeri jedno mjesto. Mapu koje nema preskače: čvor ondje još ništa
// ne drži. Grešku mjerenja zapiše u dnevnik jednom, dok se ne promijeni.
func (n *Nadzor) izmjeri(put string) (Mjera, bool) {
	if put == "" {
		return Mjera{}, false
	}
	mj, err := n.mjerac.Izmjeri(put)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		delete(n.greske, put)
		return mj, err == nil && mj.Ukupno > 0
	}
	if n.greske[put] != err.Error() {
		n.greske[put] = err.Error()
		n.javi("disk: ne zna se koliko je mjesta ostalo na disku s mapom %s, pa za taj disk nema upozorenja: %v", put, bezPutanje(err))
	}
	return Mjera{}, false
}

// bezPutanje skida putanju s greške mjerenja: redak dnevnika već kaže mapu
func bezPutanje(err error) error {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return pe.Err
	}
	return err
}
