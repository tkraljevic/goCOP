package main

// Svježa arhiva: računalo koje preuzima izvore jednom dnevno, u prvom krugu
// nakon ponoći, uloži završene dane u arhivsko stablo — vodostaje letvi
// čvor koji preuzima vodostaje, a Open-Meteo oborinskih točaka čvor koji
// izdaje prognozu — pa letve kojima je paket na redu izgradi i izda (vidi
// svjeza.NaRedu). Ostali čvorovi svježu arhivu dobiju paketima.

import (
	"database/sql"
	"log"
	"os"
	"sync"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/config"
	"gocop/internal/db"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/svjeza"
	"gocop/internal/web"
)

// svjezaArhiva drži ono o čemu ovisi dnevno ulaganje svježih podataka.
type svjezaArhiva struct {
	ulagac      *svjeza.Ulagac
	uloge       func() peers.Uloge
	arh         arhivaKisomjera // gradnja i izdavanje; nil bez poslužitelja
	razmjenaArh *razmjenaArhive // nil bez arhive

	radi     sync.Mutex // jedno ulaganje u isto vrijeme
	mu       sync.Mutex
	dan      string    // dan zadnjeg ulaganja
	ostalo   bool      // neka letva na redu nije izgrađena ili izdana
	javljeno sync.Once // „nema stabla” ide u dnevnik jednom
}

// novaSvjezaArhiva slaže dnevno ulaganje čvora; oborine su nil bez baze
// oborina, a arh (gradnja i izdavanje, na čvoru poslužitelj) nil bez njih.
func novaSvjezaArhiva(baza, oborineDB *sql.DB, podaci string, uloge func() peers.Uloge, arh arhivaKisomjera, ra *razmjenaArhive) *svjezaArhiva {
	return &svjezaArhiva{ulagac: &svjeza.Ulagac{Baza: baza, Oborine: oborineDB, Koren: podaci},
		uloge: uloge, arh: arh, razmjenaArh: ra}
}

// jednomDnevno uloži svježe podatke ako danas još nisu uloženi, pa izgradi
// i izda letve kojima je paket na redu; letve kojima gradnja ili izdavanje
// nije uspjelo (npr. arhiva je bila zauzeta) pokušaju se idući krug. Drugo
// ulaganje koje upravo traje se preskače. Zove se iz satnog kruga, u
// pozadini, jer gradnja i izdavanje traju.
func (s *svjezaArhiva) jednomDnevno(sada time.Time) {
	if s == nil || s.ulagac == nil {
		return
	}
	dan := sada.In(models.Zagreb).Format("2006-01-02")
	s.mu.Lock()
	ulozeno, ostalo := s.dan == dan, s.ostalo
	s.mu.Unlock()
	if (ulozeno && !ostalo) || !s.radi.TryLock() {
		return
	}
	defer s.radi.Unlock()
	if !ulozeno {
		ulaze, gotovo := s.ulozi(sada)
		if !gotovo {
			return
		}
		s.mu.Lock()
		s.dan = dan
		s.mu.Unlock()
		if !ulaze {
			return
		}
	}
	sve := s.izdajNaRedu(sada)
	s.mu.Lock()
	s.ostalo = !sve
	s.mu.Unlock()
}

// ulozi je ulaganje završenih dana. ulaze javlja ulaže li ovaj čvor uopće
// (uloga, stablo i poslužitelj), a gotovo da je za danas gotovo.
func (s *svjezaArhiva) ulozi(sada time.Time) (ulaze, gotovo bool) {
	u := s.uloge()
	letve, tocke := u.Preuzima, u.Izdaje && s.ulagac.Oborine != nil
	if (!letve && !tocke) || s.ulagac.Koren == "" || s.arh == nil {
		return false, true
	}
	if !imaStablo(s.ulagac.Koren) {
		s.javljeno.Do(func() {
			log.Printf("svježa arhiva: na ovom čvoru nema stabla izvornih datoteka (%s), pa se ne ulaže u arhivu; svježa arhiva stiže paketima",
				config.PunaPutanja(s.ulagac.Koren))
		})
		return false, true
	}
	iz, err := s.ulagac.Ulozi(sada, letve, tocke)
	javiGresku("svježa arhiva, ulaganje", err)
	if len(iz.BezMape) > 0 {
		log.Printf("svježa arhiva: %d letvi i točaka nema mapu u stablu, pa se ne ulažu (npr. %s)", len(iz.BezMape), iz.BezMape[0])
	}
	log.Printf("svježa arhiva: uloženo %d letvi i %d točaka, velika promjena na %d", len(iz.Letve), len(iz.Tocke), len(iz.Velike))
	return true, true
}

// izdajNaRedu izgradi i izda letve i točke kojima je paket na redu i
// javlja jesu li sve prošle.
func (s *svjezaArhiva) izdajNaRedu(sada time.Time) bool {
	naRedu, err := svjeza.NaRedu(s.ulagac.Baza, sada)
	if err != nil {
		log.Printf("svježa arhiva, letve za paket: %v", err)
		return false
	}
	if len(naRedu) == 0 {
		return true
	}
	izdane, paketa := s.izdaj(naRedu)
	javiGresku("svježa arhiva, zapis izdavanja", svjeza.Izdano(s.ulagac.Baza, izdane, paketa, sada))
	if paketa > 0 && s.razmjenaArh != nil {
		s.razmjenaArh.potakniKrug() // kazalo novih paketa ide u razmjenu odmah
	}
	log.Printf("svježa arhiva: izgrađeno %d od %d letvi i točaka na redu, novih paketa %d", len(izdane), len(naRedu), paketa)
	return len(izdane) == len(naRedu)
}

// javiGresku zapiše grešku u dnevnik programa, kad je ima.
func javiGresku(opis string, err error) {
	if err != nil {
		log.Printf("%s: %v", opis, err)
	}
}

// izdaj izgradi i izda letve redom; vraća izgrađene i koliko je novih
// paketa. Letva kojoj gradnja ili izdavanje ne uspije ostaje na redu.
func (s *svjezaArhiva) izdaj(letve []string) ([]string, int) {
	var izdane []string
	paketa := 0
	for _, l := range letve {
		if _, err := s.arh.IzgradiLetvu(l, nil); err != nil {
			log.Printf("svježa arhiva, gradnja %s: %v", l, err)
			continue
		}
		iz, err := s.arh.IzdajArhivu(l, false, nil)
		if err != nil {
			log.Printf("svježa arhiva, izdavanje %s: %v", l, err)
			continue
		}
		izdane = append(izdane, l)
		paketa += iz.Promijenjenih
	}
	return izdane, paketa
}

// stanjeSvjezeZa daje stranici Izvori arhive stanje svježe arhive čvora.
func stanjeSvjezeZa(baza *sql.DB, arhivaPut, podaci string, uloge func() peers.Uloge) func() web.SvjezaArhiva {
	return func() web.SvjezaArhiva { return stanjeSvjezeArhive(baza, arhivaPut, podaci, uloge) }
}

// stanjeSvjezeArhive je sažetak za karticu Svježa arhiva u Izvorima arhive:
// na čvoru koji ulaže zadnje ulaganje i izdavanje, a na ostalima zadnji
// primljeni paket arhive.
func stanjeSvjezeArhive(baza *sql.DB, arhivaPut, podaci string, uloge func() peers.Uloge) web.SvjezaArhiva {
	u := uloge()
	out := web.SvjezaArhiva{Ulaze: (u.Preuzima || u.Izdaje) && podaci != "" && imaStablo(podaci)}
	if out.Ulaze {
		s, err := svjeza.ProcitajStanje(baza)
		if err != nil {
			log.Printf("svježa arhiva, stanje: %v", err)
		}
		out.Ulozeno, out.Letvi, out.Tocaka, out.Izdano, out.Paketa = s.Ulozeno, s.Letvi, s.Tocaka, s.Izdano, s.Paketa
		return out
	}
	out.Primljeno, out.PrimljenoLetvi = zadnjiPrimljeniPaket(arhivaPut)
	return out
}

// zadnjiPrimljeniPaket je dan zadnjeg ugrađenog paketa arhive i koliko je
// letvi toga dana stiglo; nula kad paketa nije bilo.
func zadnjiPrimljeniPaket(arhivaPut string) (time.Time, int) {
	if arhivaPut == "" {
		return time.Time{}, 0
	}
	if _, err := os.Stat(arhivaPut); err != nil {
		return time.Time{}, 0
	}
	ro, err := sql.Open("sqlite", db.SamoZaCitanje(arhivaPut))
	if err != nil {
		return time.Time{}, 0
	}
	defer ro.Close() //nolint:errcheck // veza samo za čitanje; zatvaranje ne mijenja pročitano
	return arhiva.ZadnjePrimljeno(ro, models.Zagreb)
}
