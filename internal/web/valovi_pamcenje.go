package web

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Valovi se računaju iz cijelog niza — za Batinu je to 260.000 mjerenja od
// 1901. Sam račun traje milisekundu, ali čitanje niza iz arhive stotinjak, pa
// se rezultat pamti dok se ne promijene ni podaci ni pragovi.
//
// Pamti se samo rezultat (stotinjak valova), ne i niz: niz je desetak megabajta
// i ne treba ga držati između zahtjeva.
type valoviPamcenje struct {
	mu    sync.Mutex
	zapis map[string]valoviZapis
}

type valoviZapis struct {
	otisak    string
	provjeren time.Time // kad je otisak zadnji put provjeren u arhivi
	valovi    []models.Val
	niz       models.RazdobljeNiza
}

// razmakProvjere je koliko se dugo vjeruje zapamćenom izračunu bez ponovnog
// pitanja arhivi. Sam upit skenira sve zapise letve i traje tridesetak
// milisekundi — premalo da smeta jednom u minuti, previše za svako otvaranje
// stranice. Arhiva se ionako mijenja samo pri obnovi, ne u radu.
const razmakProvjere = time.Minute

func novoValoviPamcenje() *valoviPamcenje {
	return &valoviPamcenje{zapis: make(map[string]valoviZapis)}
}

// otisakValova opisuje ulaz izračuna: promijeni li se ijedan prag ili stigne
// li novo mjerenje, otisak se razlikuje i valovi se računaju iznova.
// Pragovi su na kraju otiska, da se sam njihov dio može usporediti zasebno
// kad se podaci ne provjeravaju.
func otisakValova(pragovi []models.PragObrane, zadnje time.Time, zapisa int) string {
	s := ""
	if zapisa > 0 {
		s = fmt.Sprintf("%d|%d", zapisa, zadnje.Unix())
	}
	for _, p := range pragovi {
		s += fmt.Sprintf("|%s=%d", p.Faza, p.Cm)
	}
	return s
}

// Valovi vraća valove obrane za letvu, računajući ih samo kad treba.
func (p *valoviPamcenje) Valovi(ctx context.Context, a *repository.ArhivaRepository,
	letva string, pragovi []models.PragObrane) ([]models.Val, models.RazdobljeNiza) {
	if a == nil || letva == "" || len(pragovi) == 0 {
		return nil, models.RazdobljeNiza{}
	}
	// Nedavno provjeren izračun s istim pragovima vrijedi bez pitanja arhivi.
	sada := time.Now()
	otisakPragova := otisakValova(pragovi, time.Time{}, 0)
	p.mu.Lock()
	if z, ok := p.zapis[letva]; ok && sada.Sub(z.provjeren) < razmakProvjere &&
		strings.HasSuffix(z.otisak, otisakPragova) {
		p.mu.Unlock()
		return z.valovi, z.niz
	}
	p.mu.Unlock()

	zapisa, zadnje := a.SpojOtisak(ctx, letva, "vodostaj")
	if zapisa < 2 {
		return nil, models.RazdobljeNiza{}
	}
	otisak := otisakValova(pragovi, zadnje, zapisa)

	p.mu.Lock()
	if z, ok := p.zapis[letva]; ok && z.otisak == otisak {
		z.provjeren = sada
		p.zapis[letva] = z
		p.mu.Unlock()
		return z.valovi, z.niz
	}
	p.mu.Unlock()

	niz, err := a.SpojNajbolji(ctx, letva, "vodostaj")
	if err != nil || len(niz) < 2 {
		return nil, models.RazdobljeNiza{}
	}
	razdoblje := models.RazdobljeNiza{Od: niz[0].Kad, Do: niz[len(niz)-1].Kad, Zapisa: len(niz)}
	valovi := models.Valovi(niz, pragovi)

	p.mu.Lock()
	p.zapis[letva] = valoviZapis{otisak: otisak, provjeren: sada, valovi: valovi, niz: razdoblje}
	p.mu.Unlock()
	return valovi, razdoblje
}
