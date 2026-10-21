package service

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
)

// PricekajPozadinu se vraća tek kad posao iz pozadine završi
func TestPricekajPozadinuCekaPosao(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &UserService{}
		pusti := make(chan struct{})
		s.pozadina.Go(func() { <-pusti })
		var docekao atomic.Bool
		go func() {
			s.PricekajPozadinu()
			docekao.Store(true)
		}()
		synctest.Wait()
		if docekao.Load() {
			t.Fatal("PricekajPozadinu se vratio dok posao još radi")
		}
		close(pusti)
		synctest.Wait()
		if !docekao.Load() {
			t.Fatal("PricekajPozadinu se nije vratio kad je posao završio")
		}
	})
}
