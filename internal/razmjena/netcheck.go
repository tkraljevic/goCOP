package razmjena

// Provjera mreže (plan WEB synca, F1a; Odluke (23. 10. 2026.)): s iste UDP
// utičnice pitaju se usporedno dva STUN poslužitelja različitih operatera
// (Google i Cloudflare) kojom javnom adresom vide računalo. Ista adresa i
// port od oba znače lak NAT, različiti NAT ovisan o odredištu, a bez ijednog
// odgovora UDP je blokiran. Čuva li NAT lokalni port samo se bilježi.
//
// Upit se šalje u 0, 250 i 750 ms, svaki put s novim ID-om transakcije, a
// rok je 2 s. Tako je RTT točan i kad stigne odgovor na raniji upit.
// Poslužitelj čija se adresa nije dala razriješiti (DNS) ne znači da je UDP
// blokiran, nego da se ne zna.

import (
	"context"
	"crypto/rand"
	"net"
	"net/netip"
	"sync"
	"time"
)

// Vrste mreže koje provjera može utvrditi
const (
	NATLak      = "lak"       // isti javni port prema oba poslužitelja
	NATOvisan   = "ovisan"    // javni port ovisi o odredištu
	NATNepoznat = "nepoznato" // odgovorio je samo jedan, ili se adresa nije dala razriješiti
	UDPBlokiran = "blokiran"  // nijedan razriješeni poslužitelj nije odgovorio
)

// razmaciSTUN su trenuci slanja upita od početka provjere, a rokSTUN
// najdulje čekanje odgovora
var (
	razmaciSTUN = []time.Duration{0, 250 * time.Millisecond, 750 * time.Millisecond}
	rokSTUN     = 2 * time.Second
)

// PosluziteljSTUN je STUN poslužitelj s imenom i razriješenom adresom (nil:
// adresa se nije dala razriješiti)
type PosluziteljSTUN struct {
	Ime    string
	Adresa net.Addr
}

// OdgovorPosluzitelja je ono što je jedan poslužitelj odgovorio
type OdgovorPosluzitelja struct {
	Ime        string
	Razrijesen bool
	Odgovorio  bool
	Javna      netip.AddrPort
	RTT        time.Duration
}

// MjerenjeMreze je ishod jedne provjere mreže
type MjerenjeMreze struct {
	Vrsta    string
	Javna    netip.AddrPort // javna adresa iz prvog odgovora; samo za stranicu Sinkronizacija
	CuvaPort bool
	Odgovori []OdgovorPosluzitelja
	Kad      time.Time
}

// ProvjeriMrezu pita sve poslužitelje usporedno i zaključi vrstu mreže
func (u *UDP) ProvjeriMrezu(ctx context.Context, posluzitelji []PosluziteljSTUN) MjerenjeMreze {
	odgovori := make([]OdgovorPosluzitelja, len(posluzitelji))
	var wg sync.WaitGroup
	for i, p := range posluzitelji {
		wg.Go(func() { odgovori[i] = u.pitaj(ctx, p) })
	}
	wg.Wait()
	m := zakljuci(u.Port(), odgovori)
	m.Kad = time.Now()
	return m
}

// pitaj šalje upite jednom poslužitelju u trenucima razmaciSTUN, dok ne
// stigne prvi ispravan odgovor ili ne prođe rok
func (u *UDP) pitaj(ctx context.Context, p PosluziteljSTUN) OdgovorPosluzitelja {
	if p.Adresa == nil {
		return OdgovorPosluzitelja{Ime: p.Ime}
	}
	ctx, otkazi := context.WithTimeout(ctx, rokSTUN)
	defer otkazi()
	upit := noviUpit(u, p.Adresa)
	defer upit.odjavi()
	o := upit.cekajOdgovor(ctx)
	o.Ime, o.Razrijesen = p.Ime, true
	return o
}

// upitSTUN su upiti jednom poslužitelju: ID-ovi, kad je koji poslan i kanal
// za odgovore
type upitSTUN struct {
	u        *UDP
	na       net.Addr
	ids      [][12]byte
	poslano  []time.Time
	pocetak  time.Time
	odgovori chan odgovorSTUN
	odjavi   func()
}

func noviUpit(u *UDP, na net.Addr) *upitSTUN {
	q := &upitSTUN{u: u, na: na, ids: make([][12]byte, len(razmaciSTUN)), pocetak: time.Now(), odgovori: make(chan odgovorSTUN, len(razmaciSTUN))}
	for i := range q.ids {
		_, _ = rand.Read(q.ids[i][:]) // crypto/rand ne vraća grešku
	}
	q.odjavi = u.cekaj(q.odgovori, q.ids...)
	return q
}

// cekajOdgovor šalje upite i čeka prvi ispravan odgovor dok ctx traje
func (q *upitSTUN) cekajOdgovor(ctx context.Context) OdgovorPosluzitelja {
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return OdgovorPosluzitelja{}
		case <-t.C:
			if sljedeci, ima := q.posaljiSljedeci(); ima {
				t.Reset(sljedeci)
			}
		case o := <-q.odgovori:
			if a, rtt, ok := q.procitaj(o); ok {
				return OdgovorPosluzitelja{Odgovorio: true, Javna: a, RTT: rtt}
			}
		}
	}
}

// posaljiSljedeci pošalje idući upit; vraća koliko do sljedećeg i ima li ga
func (q *upitSTUN) posaljiSljedeci() (time.Duration, bool) {
	i := len(q.poslano)
	q.poslano = append(q.poslano, time.Now())
	_, _ = q.u.tr.WriteTo(zahtjevBinding(q.ids[i]), q.na) // izgubljen upit nadoknadi sljedeći ili rok
	if i+1 >= len(q.ids) {
		return 0, false
	}
	return razmaciSTUN[i+1] - time.Since(q.pocetak), true
}

// procitaj čita javnu adresu iz odgovora i RTT prema upitu na koji je stigao
func (q *upitSTUN) procitaj(o odgovorSTUN) (netip.AddrPort, time.Duration, bool) {
	id := idTransakcije(o.paket)
	a, err := procitajOdgovor(o.paket, id)
	for i, poslan := range q.ids {
		if poslan == id && err == nil && i < len(q.poslano) {
			return a, o.kad.Sub(q.poslano[i]), true
		}
	}
	return netip.AddrPort{}, 0, false
}

// zakljuci je vrsta mreže iz odgovora poslužitelja: bez ijednog odgovora,
// a uz razriješene adrese, UDP je blokiran; uz jedan odgovor vrsta se ne
// zna; inače je NAT lak kad su sve javne adrese iste
func zakljuci(lokalniPort int, odgovori []OdgovorPosluzitelja) MjerenjeMreze {
	m := MjerenjeMreze{Odgovori: odgovori, Vrsta: NATNepoznat}
	javne, razrijesenih := javneAdrese(odgovori)
	switch {
	case len(javne) == 0 && razrijesenih > 0:
		m.Vrsta = UDPBlokiran
	case len(javne) >= 2 && sveIste(javne):
		m.Vrsta = NATLak
	case len(javne) >= 2:
		m.Vrsta = NATOvisan
	}
	if len(javne) > 0 {
		m.Javna, m.CuvaPort = javne[0], int(javne[0].Port()) == lokalniPort
	}
	return m
}

// javneAdrese su javne adrese iz odgovora i broj razriješenih poslužitelja
func javneAdrese(odgovori []OdgovorPosluzitelja) (javne []netip.AddrPort, razrijesenih int) {
	for _, o := range odgovori {
		if o.Razrijesen {
			razrijesenih++
		}
		if o.Odgovorio {
			javne = append(javne, o.Javna)
		}
	}
	return javne, razrijesenih
}

func sveIste(a []netip.AddrPort) bool {
	for _, x := range a[1:] {
		if x != a[0] {
			return false
		}
	}
	return true
}

// StanjeMreze je vrsta mreže s histerezom: nova vrsta vrijedi tek nakon dva
// ista mjerenja zaredom, a „nepoznato” ne briše poznatu. Prvo mjerenje koje
// nešto kaže vrijedi odmah, jer prije njega se ništa ne zna.
type StanjeMreze struct {
	Vrsta    string
	Zadnje   MjerenjeMreze
	kandidat string
	zaredom  int
}

// Dodaj upiše mjerenje i javlja je li se vrsta promijenila
func (s *StanjeMreze) Dodaj(m MjerenjeMreze) bool {
	s.Zadnje = m
	switch {
	case m.Vrsta == NATNepoznat || m.Vrsta == s.Vrsta:
		s.kandidat, s.zaredom = "", 0
		return false
	case m.Vrsta == s.kandidat:
		s.zaredom++
	default:
		s.kandidat, s.zaredom = m.Vrsta, 1
	}
	if s.Vrsta != "" && s.zaredom < 2 {
		return false
	}
	s.Vrsta, s.kandidat, s.zaredom = m.Vrsta, "", 0
	return true
}
