package web

import (
	"sync"
	"sync/atomic"
	"testing"

	"gocop/internal/repository"
)

// ponor postoji samo da pročitana vrijednost negdje završi.
var ponor atomic.Int64

// Gradnja, ugradnja i micanje niza zamjenjuju čitača arhive. Otkad ti poslovi
// idu u pozadini, zamjena se događa iz druge dretve dok HTTP zahtjevi čitaju
// isti pokazivač. Bez brave je to utrka, a zatvaranje starog čitača može
// srušiti zahtjev koji ga još drži.
//
// Pokreće se s -race; bez brave test pada na detektoru utrke.
func TestCitacArhiveSePodnosiZamjenuUsredCitanja(t *testing.T) {
	s := &Server{}

	var wg sync.WaitGroup
	kraj := make(chan struct{})

	// Čitatelji, kao HTTP zahtjevi.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-kraj:
					return
				default:
					// Vrijednost se mora upotrijebiti: čitanje čiji se
					// rezultat odbacuje prevoditelj smije ukloniti, pa
					// detektor utrke ne bi imao što vidjeti.
					if s.Arhiva() != nil {
						ponor.Add(1)
					}
				}
			}
		}()
	}

	// Pisac, kao pozadinski posao koji je upravo dogradio letvu.
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Izmjenjuju se dvije različite vrijednosti, da pisanje doista
		// mijenja memoriju. Zatvaranje prave veze tražilo bi bazu koju ovaj
		// test ne treba, pa se koristi prazan repozitorij.
		dva := []*repository.ArhivaRepository{nil, {}}
		for i := 0; i < 2000; i++ {
			s.zamijeniArhivu(dva[i%2])
		}
		close(kraj)
	}()

	wg.Wait()
}

// Zamjena istim čitačem ne smije ga zatvoriti: SetArhiva se pri pokretanju zna
// pozvati i kad se ništa nije promijenilo.
func TestZamjenaIstimCitacemGaNeZatvara(t *testing.T) {
	s := &Server{}
	var a *repository.ArhivaRepository
	s.zamijeniArhivu(a)
	s.zamijeniArhivu(a)
	if s.Arhiva() != a {
		t.Error("čitač se izgubio pri zamjeni samim sobom")
	}
}
