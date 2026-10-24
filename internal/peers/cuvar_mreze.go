package peers

// Buđenje računala i promjena mreže (plan WEB synca, F2b). Svakih 15 s
// čuvar gleda koliko je prošlo po zidnom satu: razmak veći od 22,5 s znači
// da je računalo spavalo. Nakon kratkog spavanja ide samo puls prema
// sastajalištu (macOS se i u snu kratko budi, pa se veza ne otvara iznova).
// Nakon spavanja duljeg od 10 min, kad se promijene lokalne adrese ili kad
// slanje s UDP utičnice javi da nema rute, sve kreće iznova: nova prijava s
// odgodom ispočetka, nova UDP utičnica i nova provjera mreže. Adrese
// sučelja gledaju se svakih 10 s. Sat i adrese dolaze izvana, pa se sve
// ispituje lažnim satom.

import (
	"context"
	"slices"
	"time"
)

// Razmaci čuvara mreže
const (
	razmakSataCuvara   = 15 * time.Second
	razmakAdresaCuvara = 10 * time.Second
	granicaSpavanja    = 22500 * time.Millisecond
	dugoSpavanje       = 10 * time.Minute
)

// Razlozi ponovnog spajanja, za dnevnik
const (
	RazlogDugoSpavanje = "računalo je spavalo dulje od 10 min"
	RazlogNovaMreza    = "promijenile su se adrese ovog računala"
	RazlogBezRute      = "UDP utičnica nema rutu"
)

// CuvarMreze prati spavanje računala i promjenu mreže
type CuvarMreze struct {
	Sat     func() time.Time    // zidni sat
	Adrese  func() []string     // lokalne adrese, poredane (razmjena.LokalneAdrese)
	BezRute func() bool         // slanje s UDP utičnice javilo je da nema rute; nil: nema utičnice
	Puls    func()              // nakon kratkog spavanja
	Ponovno func(razlog string) // nakon dugog spavanja ili promjene mreže
}

// Radi prati dok ctx traje
func (c *CuvarMreze) Radi(ctx context.Context) {
	sat := time.NewTicker(razmakSataCuvara)
	defer sat.Stop()
	adr := time.NewTicker(razmakAdresaCuvara)
	defer adr.Stop()
	prije, adrese := c.Sat().Round(0), c.Adrese()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sat.C:
			sad := c.Sat().Round(0)
			c.nakonRazmaka(sad.Sub(prije))
			prije = sad
		case <-adr.C:
			adrese = c.nakonAdresa(adrese)
		}
	}
}

// nakonRazmaka: korak prema razmaku zidnog sata između dva otkucaja (bez
// monotonog sata, koji na nekim sustavima u snu stoji)
func (c *CuvarMreze) nakonRazmaka(razmak time.Duration) {
	switch {
	case razmak > dugoSpavanje:
		c.Ponovno(RazlogDugoSpavanje)
	case razmak > granicaSpavanja:
		c.Puls()
	}
}

// nakonAdresa: nove adrese ili utičnica bez rute kreću iznova; vraća
// adrese koje sad vrijede
func (c *CuvarMreze) nakonAdresa(prije []string) []string {
	sad := c.Adrese()
	switch {
	case !slices.Equal(sad, prije):
		c.Ponovno(RazlogNovaMreza)
	case c.BezRute != nil && c.BezRute():
		c.Ponovno(RazlogBezRute)
	}
	return sad
}
