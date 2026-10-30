package service

import "github.com/google/uuid"

// UnosaUTijekuZaTest postavlja koliko je unosa koda osobe u tijeku, kao da
// ih istodobno šalje više prozora; 0 ih briše. Samo za testove, da granica
// istodobnih unosa ne ovisi o redoslijedu gorutina.
func UnosaUTijekuZaTest(d *DrugiKorak, userID uuid.UUID, n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if n == 0 {
		delete(d.uTijeku, userID.String())
		return
	}
	d.uTijeku[userID.String()] = n
}

// KrivihNaSatZaTest je granica krivih i istodobnih unosa osobe u satu
const KrivihNaSatZaTest = krivihNaSat
