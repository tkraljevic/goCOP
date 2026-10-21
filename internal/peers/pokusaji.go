package peers

// Pokušaji spajanja po adresi drugog računala i put zadnje veze s njim.
// Pločica razmjene to će pokazati uz računalo (plan WEB synca, F1): kad je
// koja adresa zadnji put zvana, s kojom greškom i kojim je putem išla
// zadnja veza. Sve je samo u memoriji i ne putuje. Greška koja se dogodi u
// razgovoru, kad je veza već uspostavljena, nije ovdje: ona ide u bilješku
// o razmjeni (peer_sync.last_error). Iznimka je odbijenica na Tailscale
// adresi druge strane (SyncWith): tada ta adresa ne radi, pa se pamti uz nju.

import (
	"slices"
	"sync"
	"time"

	"gocop/internal/razmjena"
)

// najvisePokusaja je koliko adresa jednog računala ovo pamti. Adresa iz
// pronalaženja u lokalnoj mreži stiže bez ikakve provjere, pa bi je
// stranac mogao svaki put javiti drugu; ostaju najnovije.
const najvisePokusaja = 16

// pokusajAdrese je zadnji pokušaj spajanja na jednu adresu
type pokusajAdrese struct {
	Kada   time.Time
	Greska string // prazno: veza je uspostavljena
}

// pokusajNa je pokušaj uz adresu na koju je išao
type pokusajNa struct {
	adresa string
	pokusajAdrese
}

// pamcenjeVeza je ono što ovaj čvor o vezama s drugim računalima pamti samo
// u memoriji: pokušaje spajanja po računalu, najstariji prvi, i put zadnje
// veze. Oboje je na jednom mjestu, pa zaborav računala (ForgetPeer) briše
// oboje odjednom.
type pamcenjeVeza struct {
	mu       sync.Mutex
	pokusaji map[string][]pokusajNa  // nodeID → pokušaji
	put      map[string]razmjena.Put // nodeID → put zadnje veze
}

// zapamtiPokusaj pamti kako je prošlo spajanje na adresu računala (nazovi),
// kao najnoviji pokušaj: raniji pokušaj na istu adresu ode, a iznad
// najvisePokusaja odlaze najstariji
func (s *Service) zapamtiPokusaj(nodeID, adresa string, err error) {
	p := pokusajNa{adresa: adresa, pokusajAdrese: pokusajAdrese{Kada: time.Now()}}
	if err != nil {
		p.Greska = err.Error()
	}
	s.veze.mu.Lock()
	defer s.veze.mu.Unlock()
	if s.veze.pokusaji == nil {
		s.veze.pokusaji = map[string][]pokusajNa{}
	}
	l := slices.DeleteFunc(s.veze.pokusaji[nodeID], func(r pokusajNa) bool { return r.adresa == adresa })
	l = append(l, p)
	if visak := len(l) - najvisePokusaja; visak > 0 {
		l = slices.Delete(l, 0, visak)
	}
	s.veze.pokusaji[nodeID] = l
}

// pokusajiAdresa su zadnji pokušaji spajanja na adrese računala, po adresi
func (s *Service) pokusajiAdresa(nodeID string) map[string]pokusajAdrese {
	s.veze.mu.Lock()
	defer s.veze.mu.Unlock()
	out := map[string]pokusajAdrese{}
	for _, p := range s.veze.pokusaji[nodeID] {
		out[p.adresa] = p.pokusajAdrese
	}
	return out
}

// zapamtiPut pamti put zadnje uspostavljene veze s računalom, u oba smjera
// (zabiljezi), i kad razgovor na njoj nije uspio. Kad veze nije bilo ili
// računalo nije poznato, ne pamti ništa.
func (s *Service) zapamtiPut(peer *Peer, c *razmjena.Conn) {
	if peer == nil || c == nil {
		return
	}
	s.veze.mu.Lock()
	defer s.veze.mu.Unlock()
	if s.veze.put == nil {
		s.veze.put = map[string]razmjena.Put{}
	}
	s.veze.put[peer.NodeID] = c.Put
}

// putZadnjeVeze je put zadnje veze s računalom; prazno dok je nije bilo
func (s *Service) putZadnjeVeze(nodeID string) razmjena.Put {
	s.veze.mu.Lock()
	defer s.veze.mu.Unlock()
	return s.veze.put[nodeID]
}

// zaboraviPokusaje zaboravi pokušaje i put zadnje veze računala koje je
// ovaj čvor zaboravio (ForgetPeer)
func (s *Service) zaboraviPokusaje(nodeID string) {
	s.veze.mu.Lock()
	defer s.veze.mu.Unlock()
	delete(s.veze.pokusaji, nodeID)
	delete(s.veze.put, nodeID)
}
