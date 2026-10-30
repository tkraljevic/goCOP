package peers

// Verzija s vremenom ispred sata ovog računala (ledger.ErrIzBuducnosti,
// ledger.ErrVlastitaUnaprijed) nije pokvarena: pošiljatelj je šalje svakim
// razgovorom dok joj vrijeme ne dođe, i tada se upiše. Uz sat koji kasni ili
// žuri takvih stiže mnogo, pa se ne pamte jedna po jedna (preskocene), nego
// se u dnevnik piše jedan redak po autoru i poruci. Autor čije verzije
// stižu iz budućnosti pamti se kratko, samo u memoriji, da ploča javi da
// sat jednog od dva računala odstupa: podaci s tog računala inače kasne bez
// ikakvog znaka.

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"gocop/internal/ledger"
)

// najviseSatiDrugih ograđuje koliko se autora pamti: ime autora piše druga
// strana
const najviseSatiDrugih = 32

// satOdstupaDo je koliko dugo ploča javlja da sat odstupa nakon zadnje
// verzije koja je stigla iz budućnosti: tri kruga automatske razmjene
const satOdstupaDo = 15 * time.Minute

// kasni javlja da je verzija preskočena samo zbog vremena u oznaci
func kasni(zasto error) bool {
	return errors.Is(zasto, ledger.ErrIzBuducnosti) || errors.Is(zasto, ledger.ErrVlastitaUnaprijed)
}

// kasneVerzije su, po autoru, verzije iz jedne poruke preskočene zbog
// vremena: prva od njih, razlog i koliko ih je
type kasneVerzije map[string]kasnaVerzija

type kasnaVerzija struct {
	prva  ledger.Version
	zasto error
	broj  int
}

// dodaj broji verziju njezinu autoru
func (k *kasneVerzije) dodaj(v ledger.Version, zasto error) {
	if *k == nil {
		*k = kasneVerzije{}
	}
	z := (*k)[v.NodeID]
	if z.broj == 0 {
		z.prva, z.zasto = v, zasto
	}
	z.broj++
	(*k)[v.NodeID] = z
}

// zabiljeziKasne zapiše u dnevnik jedan redak po autoru i zapamti autora
// čije su verzije stigle iz budućnosti
func (s *Service) zabiljeziKasne(put string, kasne kasneVerzije) {
	sada := time.Now()
	for autor, z := range kasne {
		log.Printf("%s: primljena verzija %.80q autora %.80q %v i ne upisuje se; takvih u poruci %d, a upisat će se kad im vrijeme dođe",
			put, z.prva.VersionID, autor, z.zasto, z.broj)
		if errors.Is(z.zasto, ledger.ErrIzBuducnosti) {
			s.satiDrugih.zapamti(autor, sada)
		}
	}
}

// satiDrugih su autori čije su verzije stigle s vremenom predaleko ispred
// sata ovog računala, uz vrijeme kad je stigla zadnja
type satiDrugih struct {
	mu  sync.Mutex
	kad map[string]time.Time
}

// zapamti zabilježi autora; stare zapise usput izbaci, a novog autora ne
// dodaje kad ih je već najviseSatiDrugih
func (z *satiDrugih) zapamti(autor string, sada time.Time) {
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.kad == nil {
		z.kad = map[string]time.Time{}
	}
	for a, t := range z.kad {
		if sada.Sub(t) > satOdstupaDo {
			delete(z.kad, a)
		}
	}
	if _, ima := z.kad[autor]; ima || len(z.kad) < najviseSatiDrugih {
		z.kad[autor] = sada
	}
}

// nedavno su autori, poredani, čija je verzija iz budućnosti stigla u
// zadnjih satOdstupaDo
func (z *satiDrugih) nedavno(sada time.Time) []string {
	z.mu.Lock()
	defer z.mu.Unlock()
	var out []string
	for a, t := range z.kad {
		if sada.Sub(t) <= satOdstupaDo {
			out = append(out, a)
		}
	}
	slices.Sort(out)
	return out
}

// upozorenjaSata su retci za ploču razmjene: za svako poznato računalo čije
// su verzije nedavno stigle s vremenom predaleko ispred sata ovog računala.
// Autor kojeg ovo računalo ne poznaje ne ide na ploču, jer ime piše druga
// strana; on je samo u dnevniku.
func (s *Service) upozorenjaSata(st *Status, now time.Time) []string {
	imena := map[string]string{}
	for _, p := range st.Peers {
		imena[p.NodeID] = label(p.Peer)
	}
	var out []string
	for _, autor := range s.satiDrugih.nedavno(now) {
		if ime, ok := imena[autor]; ok {
			out = append(out, fmt.Sprintf("Sat računala %s ili ovog računala odstupa više od %d minuta, pa njegove nove podatke ovo računalo prima sa zakašnjenjem. Provjerite sat na oba računala.",
				ime, int(ledger.NajviseUnaprijed/time.Minute)))
		}
	}
	return out
}
