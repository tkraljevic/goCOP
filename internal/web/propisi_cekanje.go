package web

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Pregled propisa ili izmjene čeka potvrdu na poslužitelju, a ne u
// skrivenom polju stranice: zalijepljen zakon zna imati i nekoliko MB.
// Stranica provjere šalje samo ključ, a ključ vrijedi samo za osobu koja je
// učitala propis (Odluke (25. 11. 2026.)).

const (
	rokPregledaPropisa = 30 * time.Minute // koliko pregled čeka potvrdu
	najvisePregleda    = 8                // najviše pregleda na čekanju; najstariji ispada
)

// pregledNaCekanju je jedan pregled s osobom koja ga je učitala
type pregledNaCekanju[T any] struct {
	v        T
	korisnik string
	stavljen time.Time
}

// cekaonica čuva preglede koji čekaju potvrdu, pod slučajnim ključem
type cekaonica[T any] struct {
	mu      sync.Mutex
	sada    func() time.Time
	rok     time.Duration
	najvise int
	stavke  map[string]pregledNaCekanju[T]
}

func novaCekaonica[T any]() *cekaonica[T] {
	return &cekaonica[T]{sada: time.Now, rok: rokPregledaPropisa, najvise: najvisePregleda, stavke: map[string]pregledNaCekanju[T]{}}
}

// stavi sprema pregled osobe i vraća ključ: 16 slučajnih bajtova, heks.
// Istekli pregledi se čiste, a kad ih je previše, ispada najstariji.
func (c *cekaonica[T]) stavi(korisnik string, v T) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read ne vraća grešku i uvijek popuni b
	kljuc := hex.EncodeToString(b)
	c.mu.Lock()
	defer c.mu.Unlock()
	sada := c.sada()
	for k, s := range c.stavke {
		if sada.Sub(s.stavljen) > c.rok {
			delete(c.stavke, k)
		}
	}
	for len(c.stavke) >= c.najvise {
		delete(c.stavke, c.najstariji())
	}
	c.stavke[kljuc] = pregledNaCekanju[T]{v: v, korisnik: korisnik, stavljen: sada}
	return kljuc
}

// najstariji je ključ pregleda koji najdulje čeka
func (c *cekaonica[T]) najstariji() string {
	var kljuc string
	var kad time.Time
	for k, s := range c.stavke {
		if kljuc == "" || s.stavljen.Before(kad) {
			kljuc, kad = k, s.stavljen
		}
	}
	return kljuc
}

// uzmi vraća pregled osobe koji još nije istekao; ne briše ga, pa se
// provjera može otvoriti ponovno
func (c *cekaonica[T]) uzmi(kljuc, korisnik string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.stavke[kljuc]
	if !ok || s.korisnik != korisnik || c.sada().Sub(s.stavljen) > c.rok {
		var prazno T
		return prazno, false
	}
	return s.v, true
}

// makni briše pregled nakon uspješnog spremanja
func (c *cekaonica[T]) makni(kljuc string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.stavke, kljuc)
}
