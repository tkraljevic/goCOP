package peers

import (
	"context"
	"errors"
	"net"

	"gocop/internal/razmjena"
)

// SlusajNa daje čvoru slušalice koje je test već otvorio i drži ih do kraja
// testa: razmjene (Serve) stižu na prvu, a pozivi za uparivanje
// (StartListening) na drugu. Port koji test zatvori da bi ga čvor kasnije
// otvorio zna u međuvremenu uzeti netko drugi (test iz drugog paketa ili
// druge kopije), pa čvor tada ne sluša, a test padne kasnije, kao da se
// druga strana ne javlja. Ovo vide i testovi iz paketa peers_test.
//
// Slušalica uparivanja ostaje otvorena i kad čvor ne čeka uparivanje. Vezu
// koja tada stigne, ili stigne dok čekanje još obrađuje prethodnu, odmah
// zatvori, kao zatvoren port. Zove se prije Serve i StartListening.
func (s *Service) SlusajNa(razmjene, uparivanje net.Listener) {
	s.slusalica = razmjene
	veze := make(chan net.Conn)
	go func() {
		for {
			v, err := uparivanje.Accept()
			if err != nil {
				return // test je zatvorio slušalicu
			}
			select {
			case veze <- v:
			default:
				v.Close() // nitko ne čeka uparivanje
			}
		}
	}()
	s.slusaj = func(ctx context.Context) (*razmjena.PairResult, error) {
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case v := <-veze:
				// kao razmjena.Listen: veza bez rukovanja nije čvor, čeka se dalje
				res, err := razmjena.AcceptOn(v, s.node.key, s.identity())
				if !errors.Is(err, razmjena.ErrNotAPeer) {
					return res, err
				}
			}
		}
	}
}
