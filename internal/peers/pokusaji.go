package peers

// Pokušaji spajanja po adresi drugog računala i put zadnje veze s njim.
// Pločica razmjene uz računalo pokaže koja je adresa kada zadnji put zvana
// i s kojom greškom, i kojim je putem išla zadnja veza (plan WEB synca, F1).
// Sve je samo u memoriji i ne putuje. Greška koja se dogodi u razgovoru,
// kad je veza već uspostavljena, nije ovdje: ona ide u bilješku o razmjeni
// (peer_sync.last_error).

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

// pamcenjePokusaja su pokušaji po računalu, najstariji prvi, i put zadnje
// veze s računalom
type pamcenjePokusaja struct {
	mu  sync.Mutex
	po  map[string][]pokusajNa  // nodeID → pokušaji
	put map[string]razmjena.Put // nodeID → put zadnje veze
}

// zapamtiPokusaj pamti kako je prošlo spajanje na adresu računala (nazovi)
func (s *Service) zapamtiPokusaj(nodeID, adresa string, err error) {
	p := pokusajNa{adresa: adresa, pokusajAdrese: pokusajAdrese{Kada: time.Now()}}
	if err != nil {
		p.Greska = err.Error()
	}
	s.pokusaji.upisi(nodeID, p)
}

// upisi dodaje pokušaj kao najnoviji; raniji pokušaj na istu adresu ode,
// a iznad najvisePokusaja odlaze najstariji
func (m *pamcenjePokusaja) upisi(nodeID string, p pokusajNa) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.po == nil {
		m.po = map[string][]pokusajNa{}
	}
	l := slices.DeleteFunc(m.po[nodeID], func(r pokusajNa) bool { return r.adresa == p.adresa })
	l = append(l, p)
	if visak := len(l) - najvisePokusaja; visak > 0 {
		l = slices.Delete(l, 0, visak)
	}
	m.po[nodeID] = l
}

// pokusajiAdresa su zadnji pokušaji spajanja na adrese računala, po adresi
func (s *Service) pokusajiAdresa(nodeID string) map[string]pokusajAdrese {
	s.pokusaji.mu.Lock()
	defer s.pokusaji.mu.Unlock()
	out := map[string]pokusajAdrese{}
	for _, p := range s.pokusaji.po[nodeID] {
		out[p.adresa] = p.pokusajAdrese
	}
	return out
}

// zapamtiPut pamti put veze kojom je ovaj čvor razmijenio s računalom, u
// oba smjera (zabiljezi). Kad veze nije bilo ili računalo nije poznato, ne
// pamti ništa.
func (s *Service) zapamtiPut(peer *Peer, c *razmjena.Conn) {
	if peer == nil || c == nil {
		return
	}
	s.pokusaji.mu.Lock()
	defer s.pokusaji.mu.Unlock()
	if s.pokusaji.put == nil {
		s.pokusaji.put = map[string]razmjena.Put{}
	}
	s.pokusaji.put[peer.NodeID] = c.Put
}

// putZadnjeVeze je put zadnje veze s računalom; prazno dok je nije bilo
func (s *Service) putZadnjeVeze(nodeID string) razmjena.Put {
	s.pokusaji.mu.Lock()
	defer s.pokusaji.mu.Unlock()
	return s.pokusaji.put[nodeID]
}

// zaboraviPokusaje zaboravi pokušaje i put zadnje veze računala koje je
// ovaj čvor zaboravio (ForgetPeer)
func (s *Service) zaboraviPokusaje(nodeID string) {
	s.pokusaji.mu.Lock()
	defer s.pokusaji.mu.Unlock()
	delete(s.pokusaji.po, nodeID)
	delete(s.pokusaji.put, nodeID)
}
