package peers

// Uparivanje poništava odgodu razmjene. Čvor koji redom ne odgovara
// automatska razmjena zove sve rjeđe, do osam razmaka (due u status.go).
// Uspješno uparivanje dokazuje da je drugi čvor tu, često na adresi koju
// ovaj dotad nije znao. 13. 10. 2026. laptop je od kuće 25 puta zaredom
// zvao uredsku adresu koja se izvana ne vidi; zatim su se laptop i ured
// uparili preko Tailscalea, a automatska razmjena ured nije zvala još
// četrdesetak minuta. Sad uparivanje briše niz neuspjeha za taj čvor, pa
// ga zove već sljedeći krug, a dok automatska razmjena radi, razmjena s
// njim kreće odmah, u pozadini.
//
// Niz neuspjeha drugih čvorova ostaje, a obična neuspjela razmjena broji
// se kao i prije (recordSyncState). Stanje razmjene je lokalno
// (peer_sync), pa u knjigu ne ide ništa.

import (
	"context"
	"log"
)

// nakonUparivanja: uparivanje s čvorom je uspjelo. Odgoda zbog prijašnjih
// neuspjeha za njega više ne vrijedi, a kad je drugi čvor član mreže i
// automatska razmjena radi, razmjena s njim kreće odmah.
func (s *Service) nakonUparivanja(ctx context.Context, nodeID string, clan bool) {
	s.ponistiOdgodu(ctx, nodeID)
	auto := s.kontekstAutomatske()
	if !clan || auto == nil {
		return
	}
	s.nakonUparenja.Go(func() {
		for cvor, ishod := range s.syncPeers(auto, []Peer{{NodeID: nodeID}}) {
			log.Printf("sinkronizacija s %s nakon uparivanja: %s", cvor, ishod)
		}
	})
}

// ponistiOdgodu briše niz neuspjelih razmjena s čvorom, pa ga automatska
// razmjena zove već sljedećim krugom. Piše pod bravom upisa, kao i
// bilješka o razmjeni (zabiljezi).
func (s *Service) ponistiOdgodu(ctx context.Context, nodeID string) {
	if err := s.uzmiBravu(ctx); err != nil {
		log.Printf("razmjena: odgoda za čvor %s nije poništena: %v", nodeID, err)
		return
	}
	defer s.pustiBravu()
	if _, err := s.db.ExecContext(ctx, `UPDATE peer_sync SET fails = 0 WHERE node_id = ?`, nodeID); err != nil {
		log.Printf("razmjena: odgoda za čvor %s nije poništena: %v", nodeID, err)
	}
}

// kontekstAutomatske je kontekst automatske sinkronizacije; nil dok nije
// uključena
func (s *Service) kontekstAutomatske() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autoCtx
}
