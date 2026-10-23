package peers

// Pronalaženje u lokalnoj mreži (plan WEB synca, LAN1). Glasnik koji padne
// sam se ponovno pokrene, a u jednom krugu razmjene pronalaženje ide jednom:
// nalaz vrijedi 60 s za sva računala. Do LAN1 je glasnik nakon greške tiho
// stao, a svaka razmjena probala je mrežu za sebe i čekala punih 700 ms.

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"gocop/internal/razmjena"
)

const (
	// vrijediNalaz je koliko dugo nalaz pronalaženja služi svim razmjenama
	vrijediNalaz = 60 * time.Second
	// rokPronalazenja je najdulje čekanje na odgovore; pronalaženje završi i
	// prije, čim se jave sva tražena računala
	rokPronalazenja = 700 * time.Millisecond
	// najduljeDoGlasnika je najveći razmak prije ponovnog pokretanja glasnika
	najduljeDoGlasnika = time.Minute
)

// Announce odgovara na probe drugih goCOP čvorova dok ctx traje. Glasnik
// koji padne (greška čitanja ili slanja, zauzet port) sam se ponovno
// pokrene: nakon 1 s, pa sve dulje, najviše nakon 1 min, a glasnik koji je
// radio barem minutu opet nakon 1 s. Svaki pad ide u dnevnik.
func (s *Service) Announce(ctx context.Context) error {
	var cekaj time.Duration
	for {
		pocetak := time.Now()
		err := s.glasnik(ctx)
		if ctx.Err() != nil {
			return nil
		}
		cekaj = razmakGlasnika(cekaj, time.Since(pocetak))
		log.Printf("pronalaženje na lokalnoj mreži: glasnik je stao (%v), ponovno za %v", err, cekaj)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(cekaj):
		}
	}
}

// razmakGlasnika je čekanje prije ponovnog pokretanja glasnika: 1 s nakon
// prvog pada ili nakon glasnika koji je radio barem minutu, inače dvostruko
// od prošlog, najviše minuta
func razmakGlasnika(prosli, trajao time.Duration) time.Duration {
	if prosli == 0 || trajao >= najduljeDoGlasnika {
		return time.Second
	}
	return min(2*prosli, najduljeDoGlasnika)
}

// glasnik je jedan život glasnika (s.objavi, inače razmjena.Announce)
func (s *Service) glasnik(ctx context.Context) error {
	if s.objavi != nil {
		return s.objavi(ctx)
	}
	return razmjena.Announce(ctx, s.protokol(), s.ports.Discovery, s.beacon)
}

// nalazMreze je zadnje pronalaženje: za koji protokol i port, kad, koja su
// se računala tražila i koja su se javila. Dok proba traje, tece je otvoren
// kanal: razmjene koje istodobno pitaju čekaju na njemu isti nalaz, a brava
// se drži samo dok se nalaz čita ili upisuje.
type nalazMreze struct {
	mu      sync.Mutex
	kljuc   string
	kad     time.Time
	trazeni map[string]bool
	nadjeni map[string]razmjena.Found
	tece    chan struct{}
}

// odgovorNalaza je ono što nalaz kaže o jednom računalu
type odgovorNalaza struct {
	f  razmjena.Found
	ok bool
}

// nadjiUMrezi pita lokalnu mrežu gdje je čvor: iz nalaza mlađeg od 60 s,
// ako se u njemu čvor tražio, a inače novim pronalaženjem svih poznatih
// računala, koje završi čim se sva jave. Dok jedna razmjena proba mrežu,
// ostale čekaju njezin nalaz.
func (s *Service) nadjiUMrezi(ctx context.Context, nodeID string) (razmjena.Found, bool) {
	kljuc := fmt.Sprintf("%s|%d", s.protokol(), s.ports.Discovery)
	for {
		odgovor, ceka := s.lan.pogledaj(kljuc, nodeID, time.Now())
		if odgovor != nil {
			return odgovor.f, odgovor.ok
		}
		if ceka == nil {
			return s.probajMrezu(ctx, kljuc, nodeID)
		}
		<-ceka
	}
}

// pogledaj: odgovor iz svježeg nalaza, ili kanal pronalaženja koje upravo
// teče, ili (oboje nil) da proba ide na pozivatelja, koji je time preuzima
func (n *nalazMreze) pogledaj(kljuc, nodeID string, sada time.Time) (*odgovorNalaza, chan struct{}) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.svjez(kljuc, nodeID, sada) {
		f, ok := n.nadjeni[nodeID]
		return &odgovorNalaza{f, ok}, nil
	}
	if n.tece != nil {
		return nil, n.tece
	}
	n.tece = make(chan struct{})
	return nil, nil
}

// probajMrezu proba lokalnu mrežu za sva poznata računala i nodeID, upiše
// nalaz i pusti razmjene koje su ga čekale
func (s *Service) probajMrezu(ctx context.Context, kljuc, nodeID string) (razmjena.Found, bool) {
	trazeni := s.trazeniUMrezi(ctx, nodeID)
	nadjeni := s.pronadjiTrazene(trazeni)
	n := &s.lan
	n.mu.Lock()
	defer n.mu.Unlock()
	n.zapamti(kljuc, trazeni, nadjeni, time.Now())
	close(n.tece)
	n.tece = nil
	f, ok := n.nadjeni[nodeID]
	return f, ok
}

// svjez: nalaz je za isti protokol i port, mlađi od 60 s i u njemu se čvor
// tražio
func (n *nalazMreze) svjez(kljuc, nodeID string, sada time.Time) bool {
	return n.kljuc == kljuc && sada.Sub(n.kad) < vrijediNalaz && n.trazeni[nodeID]
}

func (n *nalazMreze) zapamti(kljuc string, trazeni []string, nadjeni []razmjena.Found, sada time.Time) {
	n.kljuc, n.kad = kljuc, sada
	n.trazeni, n.nadjeni = map[string]bool{}, map[string]razmjena.Found{}
	for _, id := range trazeni {
		n.trazeni[id] = true
	}
	for _, f := range nadjeni {
		n.nadjeni[f.DeviceID] = f
	}
}

// zaboravi briše nalaz, pa sljedeće pitanje ponovno proba mrežu
func (n *nalazMreze) zaboravi() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.kad = time.Time{}
}

// trazeniUMrezi su računala koja pronalaženje traži: čvor za koji se pita i
// sva poznata računala, da cijeli krug razmjene dobije nalaz iz jedne probe
func (s *Service) trazeniUMrezi(ctx context.Context, nodeID string) []string {
	trazeni := []string{nodeID}
	poznati, _ := s.ListPeers(ctx) // bez popisa se traži samo nodeID
	for _, p := range poznati {
		if p.NodeID != nodeID && p.NodeID != s.node.ID {
			trazeni = append(trazeni, p.NodeID)
		}
	}
	return trazeni
}

// pronadjiTrazene proba lokalnu mrežu (s.trazi, inače razmjena.Pronadji);
// greška probe znači da se nitko nije javio
func (s *Service) pronadjiTrazene(trazeni []string) []razmjena.Found {
	trazi := s.trazi
	if trazi == nil {
		trazi = razmjena.Pronadji
	}
	nadjeni, _ := trazi(s.protokol(), rokPronalazenja, s.ports.Discovery, trazeni) // utičnica koja se ne otvori: nitko nije nađen
	return nadjeni
}
