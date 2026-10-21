// Package utrka glumi tuđi paket (modernc.org/utrka) s namjernom utrkom.
package utrka

import "sync"

var brojac int

// Utrka iz dvije gorutine bez zaključavanja povećava isti brojač
func Utrka() int {
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			brojac++
		}()
	}
	wg.Wait()
	return brojac
}
