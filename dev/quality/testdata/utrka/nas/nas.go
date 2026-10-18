// Package nas glumi paket goCOP-a s namjernom utrkom. Race je mora javiti.
package nas

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
