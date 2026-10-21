package peers

// Put zadnje veze s drugim računalom. Pločica razmjene uz računalo pokaže
// kojim je putem išla zadnja veza (plan WEB synca, F1). Sve je samo u
// memoriji i ne putuje.

import (
	"sync"

	"gocop/internal/razmjena"
)

// pamcenjePokusaja je put zadnje veze s računalom
type pamcenjePokusaja struct {
	mu  sync.Mutex
	put map[string]razmjena.Put // nodeID → put zadnje veze
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

// zaboraviPokusaje zaboravi put zadnje veze računala koje je ovaj čvor
// zaboravio (ForgetPeer)
func (s *Service) zaboraviPokusaje(nodeID string) {
	s.pokusaji.mu.Lock()
	defer s.pokusaji.mu.Unlock()
	delete(s.pokusaji.put, nodeID)
}
